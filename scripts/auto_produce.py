#!/usr/bin/env python3
"""Drive Danmo Novel unit production end-to-end over the local HTTP API.

Per unit, in order:

    outline (if still proposed)  -> accept
    write   (if no prose yet)    -> accept
    review  (supersede old lists)-> accept (archive only)
    revise  (every prose-fixable finding) -> accept (writes prose)
    finalize                     -> accept (writes ledger, advances status)

Every step is the same three calls the UI makes:
    POST /api/runs                      -> run id
    GET  /api/runs/{id}  (poll)         -> status / candidateId / error
    POST /api/candidates/{id}/accept    -> write + advance status

This script never touches the book tree directly; all writes go through Accept,
so the "candidates, not commits" rule still holds.

Examples
--------
    # 看它会做什么，不发任何 LLM 调用
    python3 scripts/auto_produce.py --book zhong-an-94 --from v01-U2 --dry-run

    # 只跑卷 1 剩下的单元
    python3 scripts/auto_produce.py --book zhong-an-94 --from v01-U2 --to v01-U8

    # 从卷 1 第 2 单元一路写到最后
    python3 scripts/auto_produce.py --book zhong-an-94 --from v01-U2

It is resumable: every iteration re-reads the book from disk, so re-running the
same command after a crash/stop continues where the pipeline actually is.
"""

from __future__ import annotations

import argparse
import json
import os
import re
import sys
import time
import urllib.error
import urllib.request
from datetime import datetime, timezone
from typing import Any
from urllib.parse import quote

# Finding ids that mean "this chapter was not reviewed", not "this is a defect".
UNFINISHED_FINDINGS = {"review-truncated", "review-incomplete"}
# 阿里云内容审核（data_inspection_failed）会把罪案正文的暴力细节判成
# 「Input text data may contain inappropriate content」而整章 400。它既不是
# 网络抖动也不是真的写失败，单独识别出来，日志里说清楚原因，别让作者以为
# 是正文写坏了。默认仍按原模型重试；确实要换模型时用 --content-fallback-model。
CONTENT_BLOCK_NEEDLES = ("data_inspection_failed", "inappropriate content",
                         "content moderation", "content_policy")
TERMINAL_RUN = {"succeeded", "failed", "cancelled", "waiting_accept", "rejected", "expired"}
RUNNING_RUN = {"pending", "running"}


class Abort(Exception):
    """Stop the whole run: a stage cannot be recovered by retrying."""


class AuthorBlocked(Abort):
    """Stop and ask the author: canon data is missing, retrying cannot help.

    这类失败必须穿透 --keep-going：后面的单元都建立在它之上（本章没落盘，
    下一章就没有上一章承接）。把它当普通失败跳过，只会写出一堆断链的正文。
    """

    def __init__(self, message: str, stems: list[str] | None = None, code: str = ""):
        super().__init__(message)
        self.stems = stems or []
        self.code = code


def now() -> str:
    return datetime.now().strftime("%H:%M:%S")


def daemonize(out_path: str) -> int:
    """Double-fork into a detached session and return the child pid.

    在这个环境里直接 `nohup ... &` 会被调用方的进程组回收：脚本刚接管到
    一个在跑的任务就被 SIGTERM 掉，而服务端的 run 还在继续——于是"自动跑"
    表现为静默停下。脱离会话（setsid 由 os.setsid() 完成）才真正活下来。
    """
    if os.fork() > 0:
        os._exit(0)
    os.setsid()
    if os.fork() > 0:
        os._exit(0)
    fd = os.open(out_path, os.O_WRONLY | os.O_CREAT | os.O_APPEND, 0o644)
    os.dup2(fd, 1)
    os.dup2(fd, 2)
    devnull = os.open(os.devnull, os.O_RDONLY)
    os.dup2(devnull, 0)
    if fd > 2:
        os.close(fd)
    if devnull > 2:
        os.close(devnull)
    return os.getpid()


class Client:
    def __init__(self, base: str, log, timeout: int = 180):
        self.base = base.rstrip("/")
        self.log = log
        self.timeout = timeout

    def call(self, method: str, path: str, body: Any = None) -> Any:
        data = None
        headers = {"Accept": "application/json"}
        if body is not None:
            data = json.dumps(body).encode("utf-8")
            headers["Content-Type"] = "application/json"
        req = urllib.request.Request(self.base + path, data=data, headers=headers, method=method)
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as resp:
                raw = resp.read().decode("utf-8") or "null"
                return json.loads(raw)
        except urllib.error.HTTPError as e:
            raw = e.read().decode("utf-8", "replace")
            try:
                payload = json.loads(raw)
                msg = payload.get("error") or payload
            except Exception:
                msg = raw
            raise ApiError(method, path, e.code, msg) from None
        except urllib.error.URLError as e:
            raise ApiError(method, path, 0, f"连接失败：{e.reason}") from None

    def get(self, path: str) -> Any:
        return self.call("GET", path)

    def post(self, path: str, body: Any) -> Any:
        return self.call("POST", path, body)


class ApiError(Exception):
    def __init__(self, method: str, path: str, code: int, message: Any):
        self.method, self.path, self.code, self.message = method, path, code, message
        super().__init__(f"{method} {path} -> {code}: {message}")


class Driver:
    def __init__(self, args):
        self.args = args
        self.client = Client(args.base, self.note)
        self.report: list[dict[str, Any]] = []
        self.log_path = ""

    # ---- logging -------------------------------------------------------

    def note(self, msg: str) -> None:
        line = f"[{now()}] {msg}"
        print(line, flush=True)
        if self.log_path:
            with open(self.log_path, "a", encoding="utf-8") as fh:
                fh.write(line + "\n")

    def event(self, name: str, **kv: Any) -> None:
        if not self.log_path:
            return
        rec = {"at": datetime.now(timezone.utc).isoformat(), "event": name, **kv}
        with open(self.log_path + ".jsonl", "a", encoding="utf-8") as fh:
            fh.write(json.dumps(rec, ensure_ascii=False) + "\n")

    # ---- book state ----------------------------------------------------

    def book(self) -> dict[str, Any]:
        return self.client.get(f"/api/books/{self.args.book}")

    def revision(self) -> str:
        return self.book()["book"]["revision"]

    def units(self) -> list[dict[str, Any]]:
        book = self.book()["book"]
        return sorted(book["units"], key=lambda u: (u["volume"], u["index"]))

    def open_candidates(self) -> list[dict[str, Any]]:
        return self.client.get(f"/api/books/{self.args.book}/candidates")

    def unit(self, unit_id: str) -> dict[str, Any]:
        for u in self.book()["book"]["units"]:
            if u["unitId"] == unit_id:
                return u
        raise Abort(f"书籍里找不到单元 {unit_id}")

    # ---- run lifecycle -------------------------------------------------

    # 参考知识只在「真的在写字」的阶段注入。`stages` 标记只是选择器的过滤条件，
    # 服务端 loadKnowledge 只按 audience 过滤（作者说明类不进提示词），所以传了
    # 就一定进 payload —— 阶段取舍只能在这里做：
    #   write/review  13-crime-human-flavor 自带这两个阶段标记
    #   revise        输出的是正文补丁，同样的文风纪律直接适用（界面没有这条
    #                 入口，因为它从审校页直接起，不经过确认弹窗）
    #   finalize      不注入：它只把写好的正文读成账本，不写字也不评判文风；
    #                 前端确认弹窗本身就是 `v-if="!isFinalize"` 把它藏起来的。
    #                 给一个机械核对任务塞 2.8KB「怎么把人物写活」纯属误导。
    KNOWLEDGE_KINDS = ("write", "review", "revise")

    def knowledge_for(self, kind: str) -> list[str]:
        """作者勾选的参考知识文章：整批固定，不按单元变化（变了就丢前缀缓存）。"""
        if kind not in self.KNOWLEDGE_KINDS:
            return []
        return self.args.knowledge or []

    def start_run(self, kind: str, unit_id: str, **extra: Any) -> dict[str, Any]:
        spec = {
            "kind": kind,
            "bookId": self.args.book,
            "unitId": unit_id,
            "baseRevision": self.revision(),
        }
        kn = self.knowledge_for(kind)
        if kn:
            spec["knowledgeIds"] = kn
        spec.update(extra)
        run = self.client.post("/api/runs", spec)
        self.note(f"  起任务 {kind} · run {run['id'][:8]} · model {run.get('modelId','?')}")
        self.event("run_start", kind=kind, unit=unit_id, runId=run["id"], model=run.get("modelId"))
        return run

    def wait_run(self, run: dict[str, Any]) -> dict[str, Any]:
        run_id = run["id"]
        delay = max(1.0, float(self.args.poll_interval))
        stall = max(60.0, float(self.args.stall_timeout))

        # 「没有心跳」的判据：callsUsed 与 updatedAt 都不再前进。
        # 只按总时长判会误杀合法长任务（6 章正文约 40 分钟，期间 callsUsed 会一
        # 直增长）；只看 callsUsed 又会被「正在等模型返回的那一次调用」误判。
        # 两个都不动超过 stall 秒，才是真的卡住了。
        #
        # 但「停滞」必须先扣掉机器休眠的时间。macOS 上时间源全都不能在休眠期间
        # 冻结：CLOCK_MONOTONIC 与 time.time() 都会照常前进（实测合盖 27 分钟
        # 后，用 CLOCK_MONOTONIC 算出的停滞也是 1673s，直接杀掉了健康任务）。
        # 所以改成**检测冻结**而不是找冻结的时钟：每次轮询比较「墙上时间的增量」
        # 与「预期轮询间隔」，多出来的那部分就是进程被冻住的时长，把它从停滞里扣掉。
        # 这样真正的死任务（醒着时也不前进）仍会被抓到，休眠不再算作停滞。
        last_sig = None
        idle_for = 0.0          # 醒着且没有进展的累计秒数
        wall_prev = time.time()
        while True:
            if idle_for > stall:
                self.client.post(f"/api/runs/{run_id}/cancel", {})
                raise Abort(
                    f"任务 {run_id[:8]} 已 {int(idle_for)}s 没有任何进展"
                    f"（调用数与更新时间都不再变化，且期间机器未休眠），判定为卡死并取消")

            cur = self.client.get(f"/api/runs/{run_id}")
            wall_now = time.time()
            # 真实的醒着时长：墙上增量里扣掉明显超过轮询间隔的「冻结」部分。
            step = wall_now - wall_prev
            frozen = 0.0
            if step > delay * 3 + 5:
                frozen = step - delay
                self.note(f"    …检测到进程被冻结约 {int(frozen)}s（系统休眠），不计入停滞")
                self.event("freeze_detected", runId=run_id, seconds=int(frozen))
            # idle_for 只在没有进展时累加；一旦有进展就清零。
            progressed = False

            status = cur.get("status", "")
            if status not in RUNNING_RUN:
                self.event("run_done", runId=run_id, status=status, calls=cur.get("callsUsed"),
                           error=cur.get("error", ""))
                return cur

            sig = (cur.get("callsUsed", 0), cur.get("updatedAt"))
            if sig != last_sig:
                last_sig = sig
                progressed = True

            if progressed:
                idle_for = 0.0
            else:
                idle_for += max(0.0, step - frozen)

            if float(self.args.verbose_poll) > 0:
                self.note(f"    …{status} · 已用 {cur.get('callsUsed', 0)} 次调用 · 停滞 {int(idle_for)}s")

            wall_prev = wall_now
            time.sleep(delay)

    def run_once(self, kind: str, unit_id: str, **extra: Any) -> dict[str, Any]:
        """Start + wait, retrying transient stage failures."""
        # 服务端同一单元只允许一个同类任务在跑。脚本重启时可能正撞上一次
        # 上一次会话遗留在跑的 run —— 那时 start 会被 "已有任务在跑" 拒掉，
        # 看起来像失败，其实是同一个任务。先接管它，别把成果丢掉重跑。
        active = self.active_run(kind, unit_id)
        if active:
            self.note(f"  接管在跑的任务 {active['id'][:8]}（{kind} · {active['status']}）")
            return self.wait_run(active)
        # 循环上限必须覆盖两种重试里的较大者：以前只写 stage_retries+1（默认 2），
        # 于是 --net-retries 5 实际只跑到第 2 次就退出——限流 60s/120s 退避后
        # 仍然没配额，脚本就判定失败，而它本该继续按 240s/480s 等下去。
        attempts = max(self.args.stage_retries, self.args.net_retries) + 1
        last = None
        switched_model = False
        for i in range(1, attempts + 1):
            try:
                run = self.start_run(kind, unit_id, **extra)
            except ApiError as e:
                msg = str(e.message)
                # 上一次会话留下的同类候选还没处置：能复用就直接复用，
                # 过期的扔掉再起，别把"已有一份待处理"当成任务失败。
                if "待处理" in msg or "already" in msg.lower():
                    cand = self.clear_stale(kind, unit_id)
                    if cand:
                        self.note(f"  复用已生成的候选 {cand['id'][:8]}")
                        # 回读 run 的真实调用数：这里拼的 run 只有候选 id，
                        # 没有 callsUsed；调用方用它做「这次修订到底跑没跑」的判断，
                        # 写成 0 会把一次真实生成的修订误判成没修。
                        calls = 0
                        if cand.get("runId"):
                            try:
                                calls = self.client.get(f"/api/runs/{cand['runId']}").get("callsUsed") or 0
                            except ApiError:
                                pass
                        return {"status": "waiting_accept", "candidateId": cand["id"], "callsUsed": calls}
                    try:
                        run = self.start_run(kind, unit_id, **extra)
                    except ApiError as e2:
                        raise Abort(f"{kind} 起不来：{e2.message}") from None
                else:
                    raise Abort(f"{kind} 起不来：{msg}") from None
            last = self.wait_run(run)
            if last.get("status") in ("succeeded", "waiting_accept"):
                return last
            err = (last.get("error") or "").strip()
            # 网络类失败不能按"阶段失败"草草放过：实测一次断网（DNS 解析失败、
            # TLS 握手超时、连接被重置）会连续打掉好几个单元，而模型侧什么都没
            # 做错。按指数退避多试几轮，等网回来，别把整本书写残。
            net_retry = i < self.args.net_retries + 1 and self.is_network_error(err)
            if net_retry:
                # 限流按分钟退避（配额窗口通常是分钟级），网络抖动按秒。
                if self.is_rate_limit(err):
                    wait = min(900.0, 60.0 * (2 ** (i - 1)))
                else:
                    wait = min(300.0, 20.0 * (2 ** (i - 1)))
                self.note(f"  {'限流' if self.is_rate_limit(err) else '网络异常'}，{int(wait)}s 后重试"
                          f"（{i}/{self.args.net_retries}）：{err[:120]}")
                self.event("run_net_retry", kind=kind, unit=unit_id, attempt=i, error=err)
                time.sleep(wait)
                continue
            # 内容审核拦截：默认只按原模型重试，不擅自换模型（换模型会绕开
            # 作者配好的质量档位）。确实需要换模型时才显式传 --content-fallback-model。
            if (self.is_content_block(err) and self.args.content_fallback_model
                    and not switched_model and "modelId" not in extra):
                switched_model = True
                extra = dict(extra)
                extra["modelId"] = self.args.content_fallback_model
                self.note(f"  内容审核拦截，改用 {self.args.content_fallback_model} 重试：{err[:160]}")
                self.event("run_content_fallback", kind=kind, unit=unit_id, error=err)
                time.sleep(2)
                continue
            if i < attempts:
                self.note(f"  {kind} 第 {i} 次失败，重试：{err[:200]}")
                self.event("run_retry", kind=kind, unit=unit_id, attempt=i, error=err)
                time.sleep(3)
                continue
            self.note(f"  {kind} 失败：{err[:500]}")
            return last
        return last or {}

    @staticmethod
    def is_content_block(msg: str) -> bool:
        lowered = msg.lower()
        return any(n in lowered for n in CONTENT_BLOCK_NEEDLES)

    @staticmethod
    def is_network_error(msg: str) -> bool:
        """是否属于「等一下会好」的失败：网络抖动与限流。

        429（rate limit）必须算进来。它和断网不同——等一会儿就能过——但如果不
        重试，脚本会在每次撞到配额上限时整批停下，而限流恰恰是最容易连续撞上的
        失败：一个单元的正文是逐章调用的，连续 6-8 次突然爆发很容易触发。
        """
        lowered = msg.lower()
        for needle in ("no such host", "tls handshake", "connection reset", "i/o timeout",
                        "context deadline exceeded", "timed out", "network is unreachable",
                        "connection refused", "eof",
                        "rate limit", "429", "too many requests", "quota"):
            if needle in lowered:
                return True
        return False

    @staticmethod
    def is_rate_limit(msg: str) -> bool:
        """限流需要更长的退避：按分钟等，而不是按秒。"""
        lowered = msg.lower()
        for needle in ("rate limit", "429", "too many requests", "quota"):
            if needle in lowered:
                return True
        return False

    # ---- candidates ----------------------------------------------------

    def clear_stale(self, kind: str, unit_id: str) -> dict[str, Any] | None:
        """该单元同类候选：仍在当前 revision 上的返回出来复用，过期的丢掉。

        服务端一个单元同时只允许一份待处理结果，所以"过期候选留着"会让新任务
        根本起不来（报"已有一份待处理的…"）。而过期候选又不可能被接受
        （Accept 会先判 baseRevision），留着只会把整条流水线堵死。
        """
        rev = self.revision()
        reusable = None
        for c in self.open_of(kind, unit_id):
            if c.get("baseRevision") == rev:
                reusable = c
                continue
            self.note(f"  丢弃过期候选 {c['id'][:8]}（{kind} · 生成后书籍已更新）")
            try:
                self.client.post(f"/api/candidates/{c['id']}/reject",
                                 {"reason": "书籍在生成后被修改，候选已过期"})
                self.event("reject_stale", candidateId=c["id"], kind=kind, unitId=unit_id)
            except ApiError as e:
                self.note(f"  丢弃失败：{e.message}")
        return reusable

    def purge_unit(self, unit_id: str, kinds: tuple[str, ...], reason: str = "") -> None:
        """清掉该单元在用的候选。

        两种用途：
        - 重写：旧正文和旧细纲都不算了，任何基于它们的候选项都不能复用也不能
          留着占位（`clear_stale` 只按 revision 判断，而重写发生在 revision 还没
          变的那一刻，旧的 write 候选会被当成「可复用」直接落盘）。
        - 收尾：接受一个候选之后，同单元同类型的孤儿候选（失败重试留下的）仍会
          被服务端算成「待处理」而挡住下一步，必须一并清掉。
        """
        note_reason = reason or "重写：基于旧细纲/旧正文的产物作废"
        for c in self.open_candidates():
            if not self.covers_unit(c, unit_id) or c.get("kind") not in kinds:
                continue
            self.note(f"  作废旧候选 {c['id'][:8]}（{c.get('kind')}，{note_reason}）")
            try:
                self.client.post(f"/api/candidates/{c['id']}/reject", {"reason": note_reason})
                self.event("reject_candidate", candidateId=c["id"], kind=c.get("kind"),
                           unitId=unit_id, reason=note_reason)
            except ApiError as e:
                self.note(f"  作废失败：{e.message}")

    def reject_candidate(self, cand: dict[str, Any], reason: str) -> None:
        try:
            self.client.post(f"/api/candidates/{cand['id']}/reject", {"reason": reason})
        except ApiError as e:
            self.note(f"  作废候选失败：{e.message}")

    def candidate(self, cand_id: str) -> dict[str, Any]:
        return self.client.get(f"/api/candidates/{cand_id}")

    def active_run(self, kind: str, unit_id: str) -> dict[str, Any] | None:
        """找该单元上仍在跑的同种任务（上一次会话留下的，或并发的另一个入口）。"""
        runs = self.client.get(f"/api/books/{self.args.book}/runs")
        for r in runs:
            if r.get("kind") != kind or (r.get("spec") or {}).get("unitId") != unit_id:
                continue
            if r.get("status") in RUNNING_RUN:
                return r
        return None

    @staticmethod
    def covers_unit(c: dict[str, Any], unit_id: str) -> bool:
        """候选是否属于这个单元。

        细纲候选是**卷级**的：`unitId` 为空，单元身份只写在 `unitOutlines[].unitId`
        里。只按 `c.unitId` 比对会漏掉全部细纲候选——于是「作废旧候选」和
        「复用可接受候选」两条路对细纲都失灵，表现为明明有候选却报
        「已有一份待处理」，或者更糟：把**别的单元**的细纲当成本单元的复用它。
        """
        if c.get("unitId"):
            return c.get("unitId") == unit_id
        for u in c.get("unitOutlines") or []:
            if u.get("unitId") == unit_id:
                return True
        return False

    def open_of(self, kind: str, unit_id: str) -> list[dict[str, Any]]:
        return [c for c in self.open_candidates()
                if c.get("kind") == kind and self.covers_unit(c, unit_id)]

    def candidate_from_run(self, run: dict[str, Any], kind: str, unit_id: str) -> dict[str, Any] | None:
        cand_id = run.get("candidateId")
        if cand_id:
            return self.candidate(cand_id)
        # 部分失败的审校仍会落候选，但 response 里不一定带 id，兜底按 unit 找。
        found = self.open_of(kind, unit_id)
        return found[-1] if found else None

    @staticmethod
    def missing_identity_stems(msg: str) -> list[str]:
        """从「v01-U8：zheng-weishan 缺少生效的职务身份段」里取 stem 列表。"""
        return re.findall(r"[：:]\s*([a-z0-9][a-z0-9\-]*)\s*缺少生效的职务身份段", msg)

    def accept(self, cand: dict[str, Any], force: bool = False) -> dict[str, Any]:
        body = {
            "candidateId": cand["id"],
            "baseRevision": self.revision(),
            "forceRiskAccept": bool(force),
        }
        try:
            out = self.client.post(f"/api/candidates/{cand['id']}/accept", body)
        except ApiError as e:
            msg = str(e.message)
            # 只有「canon 里根本没有这块数据」才值得停下来问作者：候选本身合规，
            # 缺的是人物卡或身份段，重跑只会再撞同一堵墙。凡是模型把已有的值写错
            # （引用写错单元、写错 stem），都属于可修复的内容错误，交给修复轮。
            if "缺少生效的职务身份段" in msg:
                # 同一个报错有两种成因，判据是「这个人有没有在时间线里出现过」：
                #   完全没有段 → 作者没登记过这个人，必须问作者
                #   有段但不在本单元 → 模型把还没登场的人放进了 on_stage
                stems = self.missing_identity_stems(msg)
                raise AuthorBlocked(
                    f"canon 需要作者补齐：{msg}\n"
                    f"候选人 {cand['id'][:8]} 已保留（{cand.get('kind')} · {cand.get('unitId')}）；"
                    f"补齐后重跑同一命令会原地接受这份候选，不必再花一次模型调用。",
                    stems=stems, code="identity_missing") from None
            for needle in ("人物卡中登记", "人物卡中没有 stem=", "身份候选需先进行影响确认"):
                if needle in msg:
                    raise AuthorBlocked(
                        f"canon 需要作者补齐：{msg}\n"
                        f"候选人 {cand['id'][:8]} 已保留（{cand.get('kind')} · {cand.get('unitId')}）；"
                        f"补齐后重跑同一命令会原地接受这份候选，不必再花一次模型调用。") from None
            raise
        self.event("accept", candidateId=cand["id"], kind=cand.get("kind"), unitId=cand.get("unitId"),
                   force=bool(force))
        return out

    # ---- findings ------------------------------------------------------

    @staticmethod
    def prose_fixable(f: dict[str, Any], prose_path: str) -> bool:
        """与前端 canPick / 服务端 RunRevise 同一判据：必须落在具体一章上。"""
        if f.get("id") in UNFINISHED_FINDINGS:
            return False
        if int(f.get("chapter") or 0) <= 0:
            return False
        path = (f.get("entityPath") or "").strip()
        if path and prose_path and path != prose_path:
            return False
        return True

    @staticmethod
    def same_prose(cand: dict[str, Any]) -> bool:
        """修订候选是否与原稿逐字节相同（服务端也会查未改章节，这里查全篇）。"""
        base = {c.get("chapter"): c.get("body") for c in cand.get("baseChapters") or []}
        chs = {c.get("chapter"): c.get("body") for c in cand.get("chapters") or []}
        return bool(base) and base == chs

    def select_findings(self, cands: list[dict[str, Any]], prose_path: str) -> list[dict[str, Any]]:
        seen: dict[str, dict[str, Any]] = {}
        for c in cands:
            for f in c.get("findings") or []:
                if not self.prose_fixable(f, prose_path):
                    continue
                seen.setdefault(f["id"], f)
        # 与前端排序一致：先看多轮命中，再看阻断。
        return sorted(seen.values(), key=lambda f: (-(f.get("roundsHit") or 0),
                                                   0 if f.get("severity") == "blocking" else 1,
                                                   f.get("chapter") or 0))

    def write_author_todo(self, unit_id: str, reason: str) -> None:
        """把「需要人处理」写成一份可直接照做的清单。

        脚本一行行往前推的时候，作者看不到终端。停下却不能立刻知道要改哪个文件、
        改什么，等于白停一次。这份清单写进日志目录，同时给出继续运行的命令。
        """
        path = os.path.join(self.args.log_dir, f"NEEDS-AUTHOR-{self.args.book}.md")
        unit = {}
        try:
            unit = self.unit(unit_id)
        except Abort:
            pass
        body = [
            f"# 需要作者处理 · {self.args.book}",
            "",
            f"- 单元：`{unit_id}` {unit.get('title', '')}（{unit.get('status', '?')}）",
            f"- 时间：{datetime.now().strftime('%Y-%m-%d %H:%M:%S')}",
            "",
            "## 原因",
            "",
            "```",
            reason.strip(),
            "```",
            "",
            "## 脚本已尝试过的自动修复",
            "",
            "- 卷纲种子里写错单元的身份转换（能按身份时间线唯一定位时会自动挪正）",
            "- 细纲被接受校验拒绝后带原文重生成（默认 2 轮）",
            "",
            "## 接下来",
            "",
            "1. 按上面的原因修改对应文件（多为 `canon/identity-timeline.yaml` 或 `canon/cast/<stem>.md`）。",
            "2. 重跑同一条命令即可接着推进；已生成的候选会被原地复用，不会重复付费：",
            "",
            "```bash",
            f"python3 scripts/auto_produce.py --book {self.args.book} --from {unit_id}",
            "```",
        ]
        try:
            os.makedirs(self.args.log_dir, exist_ok=True)
            with open(path, "w", encoding="utf-8") as fh:
                fh.write("\n".join(body) + "\n")
            self.note(f"  已写出待办清单：{path}")
        except OSError as e:
            self.note(f"  写待办清单失败：{e}")

    # ---- stages --------------------------------------------------------

    def stage_outline(self, unit_id: str) -> None:
        u = self.unit(unit_id)
        if self.args.rewrite:
            # 重写：不看当前状态，一律重新生成细纲。旧细纲里的章级合同是重写
            # 要替换的对象，跳过它就等于"重写"变成了"复用旧规划写新正文"。
            self.note(f"  重写细纲（当前 {u['status']}）")
            self.purge_unit(unit_id, ("outline",))
        elif u["status"] != "proposed":
            self.note(f"  细纲已是 {u['status']}，跳过")
            return
        cand = None if self.args.rewrite else self.clear_stale("outline", unit_id)
        if cand is None:
            # 写细纲之前先体检这个单元的种子：身份转换写错单元时，模型无论怎么
            # 写都会被两条互斥的校验拒掉。数据问题在生成前修掉，比撞墙后再修省
            # 一整轮调用。
            self.scan_repairs([u])
        if cand:
            self.note(f"  复用已生成的细纲候选 {cand['id'][:8]}")
            self.accept_outline(unit_id, cand, self.stage_instruction())
        else:
            self.accept_outline(unit_id, None, self.stage_instruction())
        self.note(f"  细纲已接受 · {unit_id} → {self.unit(unit_id)['status']}")

    def timeline_stems(self) -> set[str]:
        """canon 身份时间线里出现过的 stem。

        用来区分「这个人根本还没登记」和「这个人有身份段、只是不在本单元」。
        读文件而不是解析错误文本：错误只说"缺少"，不区分两种成因。
        """
        try:
            raw = self.client.get(
                f"/api/books/{self.args.book}/file?path=" + quote("canon/identity-timeline.yaml")
            ).get("content", "")
        except ApiError:
            return set()
        return set(re.findall(r"stem:\s*([A-Za-z0-9][A-Za-z0-9\-]*)", raw))

    def known_stems(self) -> set[str]:
        """人物卡里有的 stem（用于判断"人物卡中没有 stem=" 是否真缺卡）。"""
        try:
            book = self.book()["book"]
        except (Abort, ApiError):
            return set()
        return {c.get("stem") for c in book.get("cast") or [] if c.get("stem")}

    # ---- 数据层：读卷纲 / 身份时间线 ------------------------------------

    def read_book_file(self, rel: str) -> str:
        return self.client.get(f"/api/books/{self.args.book}/file?path=" + quote(rel)).get("content", "")

    def volume_path(self, unit_id: str) -> str:
        vol = str(self.unit(unit_id).get("volume") or 1)
        return f"outline/volumes/v{int(vol):02d}.md"

    def identity_segments(self) -> list[dict[str, str]]:
        """canon/identity-timeline.yaml 的段列表（扁平键值，够用且不引入 YAML 依赖）。"""
        try:
            raw = self.read_book_file("canon/identity-timeline.yaml")
        except ApiError:
            return []
        segs: list[dict[str, str]] = []
        cur: dict[str, str] | None = None
        for line in raw.splitlines():
            stripped = line.strip()
            if stripped.startswith("- stem:"):
                if cur:
                    segs.append(cur)
                cur = {"stem": stripped.split(":", 1)[1].strip()}
                continue
            if cur is None or ":" not in stripped or stripped.startswith("#"):
                continue
            key, _, val = stripped.partition(":")
            key, val = key.strip(), val.strip().strip('"').strip("'")
            if key in ("role_title", "org", "from_unit", "to_unit", "note", "id"):
                cur[key] = val
        if cur:
            segs.append(cur)
        return [s for s in segs if s.get("stem") and s.get("from_unit")]

    @staticmethod
    def unit_order(unit_id: str) -> int:
        m = re.match(r"^v(\d+)-U(\d+)$", unit_id or "", re.I)
        return int(m.group(1)) * 1000 + int(m.group(2)) if m else 0

    def identity_at(self, segs: list[dict[str, str]], stem: str, unit_id: str) -> dict[str, str] | None:
        key = self.unit_order(unit_id)
        best = None
        for s in segs:
            if s["stem"] != stem:
                continue
            start = self.unit_order(s["from_unit"])
            end = self.unit_order(s.get("to_unit", "")) if s.get("to_unit") else 0
            if start > key or (end and end < key):
                continue
            if best is None or start >= self.unit_order(best["from_unit"]):
                best = s
        return best

    def identity_before(self, segs: list[dict[str, str]], stem: str, unit_id: str) -> dict[str, str] | None:
        cur = self.identity_at(segs, stem, unit_id)
        if not cur:
            return None
        start = self.unit_order(cur["from_unit"])
        best = None
        for s in segs:
            if s["stem"] != stem or self.unit_order(s["from_unit"]) >= start:
                continue
            if best is None or self.unit_order(s["from_unit"]) >= self.unit_order(best["from_unit"]):
                best = s
        return best

    def resolve_identity(self, segs: list[dict[str, str]], stem: str, raw: str,
                         at_unit: str) -> dict[str, str] | None:
        """对齐 Go 的 ResolveIdentitySegment：@单元 / stem@单元 按主键，文案按唯一性。"""
        v = (raw or "").strip()
        if not v:
            return None
        if v.startswith("@"):
            return self.identity_at(segs, stem, v[1:].strip())
        if "@" in v:
            for s in segs:
                if s["stem"] == stem and f"{s['stem']}@{s['from_unit']}" == v:
                    return s
            return None
        hits = [s for s in segs if s["stem"] == stem and s.get("role_title") == v]
        if len(hits) == 1:
            return hits[0]
        if hits:
            return None  # 重名，不猜
        cur = self.identity_at(segs, stem, at_unit) if at_unit else None
        if cur and cur.get("role_title") == v:
            return cur
        return None

    def unit_state_changes(self, volume_rel: str, unit_id: str) -> str:
        """从卷纲 Markdown 里取某个单元的 state_changes 原始值。"""
        raw = self.read_book_file(volume_rel)
        current = None
        for line in raw.splitlines():
            stripped = line.strip()
            if stripped.startswith("## "):
                current = stripped[3:].strip()
                if current.startswith("单元"):
                    current = current[len("单元"):].strip(" ·")
                continue
            if current == unit_id and stripped.startswith("- state_changes:"):
                return stripped.split(":", 1)[1].strip()
        return ""

    def patch_unit_field(self, volume_rel: str, unit_id: str, field: str, value: str) -> None:
        if getattr(self, "dry", False):
            self.note(f"  [dry-run] 将把 {volume_rel} {unit_id}.{field} 改为「{value}」")
            return
        rev = self.revision()
        self.client.call("PATCH", f"/api/books/{self.args.book}/outline-field", {
            "path": volume_rel, "kind": "单元", "entityId": unit_id,
            "field": field, "value": value, "baseRevision": rev,
        })

    def repair_seed_identity(self, unit_id: str, err_msg: str) -> str:
        """尝试自动修掉「卷纲种子 ↔ 身份时间线」冲突。返回修了什么（空串=修不了）。

        这是「能修就修」的那一半：冲突的成因是**确定性**的——身份转换被写在了
        错误的单元。判据只有一条：这条迁移的 to 指向的身份段，实际是从哪个单元
        开始生效的。指向本单元 → 本来就对；指向别的单元 → 挪过去；指不到任何段
        → 说明 canon 里根本没有这个职务，只能问人。

        全程只用「引用」和「删除」，不猜文案：修完的种子要么用 @单元 引用，
        要么根本不含 role_title。这样无论作者后来怎么改措辞，都不再复发。
        """
        segs = self.identity_segments()
        if not segs:
            return ""
        volume = self.volume_path(unit_id)
        raw_changes = self.unit_state_changes(volume, unit_id)
        if not raw_changes:
            return ""
        entries = [e.strip() for e in re.split(r"[；;]", raw_changes) if e.strip()]
        kept: list[str] = []
        moves: list[tuple[str, str]] = []
        dropped: list[str] = []
        changed = False
        for entry in entries:
            m = re.match(r"^([A-Za-z0-9][A-Za-z0-9_-]*)\.([a-z_]+)\s*=\s*(.+)$", entry)
            if not m:
                kept.append(entry)
                continue
            stem, field, value = m.group(1), m.group(2), m.group(3).strip()
            if field != "role_title":
                kept.append(entry)
                continue
            to = value.split("->")[-1].strip() if "->" in value else value
            cur = self.identity_at(segs, stem, unit_id)
            if cur is None:
                # 这个人本单元没有生效身份段：既可能是人物卡缺段，也可能时段
                # 边界写错。两种都不是脚本能定的，交给人。
                return ""
            target = self.resolve_identity(segs, stem, to, unit_id)
            if target is None:
                # 职务文案对不上任何身份段。可能是笔误，也可能 canon 真没有这个
                # 职务——删掉会静默丢失作者意图，所以不修。
                return ""
            if target["from_unit"] == cur["from_unit"]:
                # 指向的就是本单元这一段：语义已经正确。
                # 只在写成文案时规范成引用；已经是引用的原样保留，不改作者选择。
                if "@" in value:
                    kept.append(entry)
                    continue
                kept.append(f"{stem}.role_title=@{cur['from_unit']}")
                changed = True
                continue
            if self.unit_order(target["from_unit"]) > self.unit_order(unit_id):
                # 转换属于后面的单元：从本单元删除，挪到真正生效的位置。
                moves.append((target["from_unit"], f"{stem}.role_title=@{target['from_unit']}"))
            else:
                # 该身份在本单元之前就已生效：这条"转换"是冗余陈述，删掉即可。
                dropped.append(entry)
            changed = True
        if not changed:
            return ""
        notes = []
        new_value = "；".join(kept)
        if not new_value.strip():
            # state_changes 是卷纲的必填字段，清空会被服务端拒绝。清理后一条不剩
            # 说明这条身份声明是种子唯一的迁移——这个决定必须由作者做。
            return ""
        self.patch_unit_field(volume, unit_id, "state_changes", new_value)
        notes.append(f"{unit_id} 的 state_changes 改为「{new_value or '（空）'}」")
        # 挪到目标单元：追一条引用式声明。重复添加无意义，先看该单元有没有。
        for target_unit, entry in moves:
            if target_unit == unit_id:
                continue
            exist = self.unit_state_changes(volume, target_unit)
            if ".role_title=" in exist:
                notes.append(f"{target_unit} 已有身份声明，未重复添加")
                continue
            merged = f"{exist}；{entry}" if exist.strip() else entry
            try:
                self.patch_unit_field(volume, target_unit, "state_changes", merged)
                notes.append(f"{target_unit} 补上「{entry}」（身份转换真正生效的位置）")
            except ApiError as e:
                notes.append(f"挪到 {target_unit} 失败：{e.message}")
        if dropped:
            notes.append("删除无法解析的身份条目：" + "；".join(dropped))
        self.event("repair_seed_identity", unitId=unit_id, notes=notes, error=err_msg)
        return "；".join(notes)

    # 这些候选的 Accept 会校验 baseRevision；体检一 PATCH 就会让它们过期。
    # 审校类候选不校验版本（接受只是归档），不算在内。
    REVISION_SENSITIVE = ("outline", "write", "revise", "finalize", "plan", "rewrite_entity", "idea")

    def revision_sensitive_pending(self) -> str:
        """返回会因改书而失效的待处理项（用于体检前的安全检查），空串表示安全。"""
        for r in self.client.get(f"/api/books/{self.args.book}/runs"):
            if r.get("status") in RUNNING_RUN:
                return f"任务在跑（{r['kind']} · {(r.get('spec') or {}).get('unitId')}）"
        for c in self.open_candidates():
            if c.get("kind") in self.REVISION_SENSITIVE:
                return f"有未接受的候选（{c.get('kind')} · {c.get('unitId') or c.get('id', '')[:8]}）"
        return ""

    def scan_repairs(self, units: list[dict[str, Any]]) -> None:
        """开工前把所有卷纲种子的身份冲突一次性修掉。

        为什么放在最前面而不是等它撞墙：这类冲突是**跨单元**的——某个单元的
        身份转换写早了，会让后面每一个引用它的单元都失败。逐个等失败再修，
        每撞一次就白烧一轮调用。先扫一遍的成本是几个本地 GET。
        """
        if self.args.no_repair:
            self.note("--no-repair：跳过数据体检")
            return
        blocker = self.revision_sensitive_pending()
        if blocker:
            self.note(f"  {blocker}；跳过数据体检，以免 PATCH 推进 revision 让候选过期")
            return
        fixed = 0
        for u in units:
            try:
                note = self.repair_seed_identity(u["unitId"], "开工前体检")
            except ApiError as e:
                self.note(f"  {u['unitId']} 体检失败：{e.message}")
                continue
            if note:
                fixed += 1
                self.note(f"  修复 {u['unitId']}：{note}")
        self.note(f"数据体检完成：修复 {fixed} 个单元的卷纲种子" if fixed else "数据体检完成：无需修复")

    def accept_outline(self, unit_id: str, cand: dict[str, Any] | None, instruction: str) -> None:
        """生成并接受细纲；被服务端校验拒绝时带着原因重生成一次。

        Accept 是最后一道校验（父子合同、时钟、身份、state_deltas 词表），而它的
        失败大多是**模型把字段写成了别的东西**：把引用写成一段描述、把 stme 写错、
        把数字写成字符串。这类错误重跑一次就能过，直接判定整批失败等于让一次
        措辞失误停掉整本书。所以这里把服务端的错误原文当成修复指令再生成一次；
        真正缺 canon 的（人物卡里没有这个 stem）由 accept 抛 AuthorBlocked，不进这个循环。
        """
        attempts = max(1, self.args.outline_repairs + 1)
        extra = instruction
        last_err = ""
        for attempt in range(1, attempts + 1):
            if cand is None:
                run = self.run_once("outline", unit_id, instruction=extra)
                if run.get("status") not in ("succeeded", "waiting_accept"):
                    raise Abort(f"细纲生成失败：{(run.get('error') or '')[:300]}")
                cand = self.candidate_from_run(run, "outline", unit_id)
                if not cand:
                    raise Abort("细纲任务成功但没有候选，无法接受")
            try:
                self.accept(cand)
                # 接受之后必须清掉同单元其它 open 的细纲候选：服务端「同一单元
                # 同时只允许一份待处理结果」是按单元判的，而失败重试会留下挂在
                # 旧 run 上的孤儿候选（服务重启中断的那次就是）。不清掉的话，
                # 细纲明明已经是 accepted，接下来的写正文仍会被它挡回去。
                self.purge_unit(unit_id, ("outline",), reason="细纲已接受，清理同类型孤儿候选")
                return
            except AuthorBlocked as e:
                # 「有身份段，只是不在本单元」= 模型让一个还没登场的人上台。
                # 卷纲 cast 里确实列着他，所以这是一次合理的误判，不是 canon 缺失：
                # 身份时间线才是"谁什么时候可被公开识别"的唯一所有者。交给修复轮，
                # 让模型按时间线把这个人从 onStage 里去掉。
                if e.code == "identity_missing" and e.stems:
                    known = self.timeline_stems()
                    unknown = [s for s in e.stems if s not in known]
                    if not unknown:
                        self.note(f"  这些人物有身份段但不在本单元：{', '.join(e.stems)}；按身份时间线重生成")
                        self.event("outline_rejected", unitId=unit_id, candidateId=cand["id"],
                                   attempt=attempt, error=str(e), reason="identity_out_of_span")
                        self.reject_candidate(cand, "身份时间线在该单元没有生效段：人物尚未登场")
                        cand = None
                        if attempt >= attempts:
                            raise Abort(
                                f"细纲连续 {attempts} 次把关卡外的人物放进 onStage：{', '.join(e.stems)}；"
                                f"这些人物在身份时间线里存在，但本单元没有生效段") from None
                        extra = self.repair_instruction(instruction, str(e)) + (
                            f"\n4. 本单元 onStage 只能放 canon/identity-timeline.yaml 在本单元"
                            f"（{unit_id}）有生效身份段的人物。{', '.join(e.stems)} ��有段但不在本单元，"
                            f"说明尚未登场：必须从 onStage 与 stateDeltas 中移除，不要为他安排出场。")
                        continue
                raise
            except ApiError as e:
                msg = str(e.message)
                self.note(f"  细纲被校验拒绝（第 {attempt}/{attempts} 次）：{msg[:240]}")
                self.event("outline_rejected", unitId=unit_id, candidateId=cand["id"],
                           attempt=attempt, error=msg)
                # 拒绝后候选仍是 open，必须作废：blockingRun 会按单元判活跃，
                # 留着它下一次起任务直接报「已有一份待处理」。
                try:
                    self.client.post(f"/api/candidates/{cand['id']}/reject",
                                     {"reason": "细纲未通过接受校验，按修复指令重生成"})
                except ApiError:
                    pass
                cand = None
                # 合同冲突（身份/状态迁移）是数据层问题，重试模型不会改变结果：
                # 先尝试自动修卷纲数据，修不了才问人。
                if self.is_contract_conflict(msg):
                    fix = self.repair_seed_identity(unit_id, msg)
                    if fix:
                        self.note(f"  已自动修复卷纲数据：{fix}")
                        extra = self.repair_instruction(instruction, msg)
                        continue
                    raise AuthorBlocked(
                        f"卷纲/身份时间线冲突，脚本无法自动修复：{msg[:300]}\n"
                        f"修法：身份转换只能写在它生效的那个单元。请检查 "
                        f"outline/volumes/ 里 {unit_id} 的 state_changes，"
                        f"或 canon/identity-timeline.yaml 的段边界。"
                    ) from None
                if msg == last_err:
                    raise Abort(f"同一条校验错误连续 {attempt} 次未变，停止重试：{msg[:300]}") from None
                last_err = msg
                if attempt >= attempts:
                    raise Abort(f"细纲连续 {attempts} 次未通过接受校验：{msg[:400]}") from None
                extra = self.repair_instruction(instruction, msg)

    @staticmethod
    def is_contract_conflict(msg: str) -> bool:
        """是否是「卷纲合同与身份时间线互斥」类错误（而非模型措辞问题）。"""
        for needle in ("与本单元生效段", "缺少父级迁移", "的身份", "状态迁移"):
            if needle in msg:
                return True
        return False

    @staticmethod
    def repair_instruction(base: str, error: str) -> str:
        """把服务端的校验错误变成下一位作者指令。

        只描述**规则**，不描述具体文案：写「必须逐字等于…」这类要求时，
        正文里出现的任何示例都会被模型当成要照抄的值。这里只给结构（字段名、
        引用写法、归属），让模型自己按上下文取值。
        """
        hint = (
            "上一份细纲被服务端接受校验拒绝，原因如下：\n"
            f"{error}\n\n"
            "请只修正上述问题，其余字段与结构保持不变，重新输出完整细纲。\n"
            "修正要求：\n"
            "1. state_deltas 里 field=role_title 的条目是引用，不是文案：to 用 "
            "`@单元ID` 指向该单元生效的身份段，或 `stem@单元ID` 指向段主键；"
            "不要抄写身份段里的职务文字。\n"
            "2. 身份转换只能写在该转换生效的那个单元；本单元内身份恒定。\n"
            "3. 不得为没有人物卡的 stem 写 role_title 或 life 字段。\n"
            "4. 不得新增或改写身份段——身份只由 canon/identity-timeline.yaml 拥有。"
        )
        return (base + "\n\n" + hint) if base else hint

    def stage_instruction(self) -> str:
        """只有显式给了 --instruction 才发；空串不进 payload，避免改动缓存前缀。"""
        return (self.args.instruction or "").strip()

    def previous_unit(self, unit_id: str) -> dict[str, Any] | None:
        ids = [u["unitId"] for u in self.units()]
        if unit_id not in ids:
            return None
        i = ids.index(unit_id)
        return self.units()[i - 1] if i > 0 else None

    def stage_write(self, unit_id: str) -> None:
        u = self.unit(unit_id)
        if u.get("hasProse") and not self.args.rewrite:
            self.reconcile_prose_candidate(unit_id, u)
            self.note("  已有正文，跳过写作")
            return
        if u["status"] not in ("accepted", "drafted", "reviewed", "finalized"):
            raise Abort(f"单元 {unit_id} 状态 {u['status']}，不能写正文")
        # 连续写作是链式的：本章的「上一章末尾」要么来自本单元已写的章，要么来自
        # 上一单元的正文（contextsvc.PreviousChapterTail 找不到时静默退化成语感承接）。
        # 上一单元没正文就往下写，模型拿到的是空的上文，写出来的第一章接不上任何东西，
        # 而且这种断链在事后极难发现——所以这里硬拦。
        prev = self.previous_unit(unit_id)
        if prev and not prev.get("hasProse") and not self.args.allow_gap:
            raise Abort(
                f"上一单元 {prev['unitId']}（{prev['title']}）还没有正文，状态 {prev['status']}。"
                f"连续写作必须按顺序：先补完 {prev['unitId']}，否则 {unit_id} 的第一章接不上任何上文。"
                f"（确需跳写请显式加 --allow-gap）")
        cand = None if self.args.rewrite else self.clear_stale("write", unit_id)
        if cand:
            self.note(f"  复用已生成的正文候选 {cand['id'][:8]}")
        else:
            if u.get("hasProse"):
                self.note("  重写正文：新候选会整单元覆盖现稿")
            run = self.run_once("write", unit_id, instruction=self.stage_instruction())
            if run.get("status") not in ("succeeded", "waiting_accept"):
                raise Abort(f"正文生成失败：{(run.get('error') or '')[:300]}")
            cand = self.candidate_from_run(run, "write", unit_id)
            if not cand:
                raise Abort("写作任务成功但没有候选，无法接受")
        self.accept_write(unit_id, cand)
        u = self.unit(unit_id)
        self.note(f"  正文已落盘 · {unit_id} → {u['status']} · 章 {u['chapterFrom']}-{u['chapterTo']}")

    # 本地硬门里「文风类」的 lens：它们是对生成出来的文字风格的机械判定，
    # 而审校+修订本来就是为了改这些。结构类（空章、字数严重不足）不在此列。
    STYLE_LENSES = ("deslop", "refine")

    def reconcile_prose_candidate(self, unit_id: str, u: dict[str, Any]) -> None:
        """正文已落盘，但还挂着一份没处置的正文候选：把它收尾。

        正常流程里不该出现这个状态——候选要么被接受（写盘+推进状态），要么被
        拒绝（什么都不动）。它会残留，是因为曾经有一个 bug：Accept 在跑校验之前
        就把正文写进了 units/<id>.md，于是被门禁拒绝的那次接受「盘上已有正文、
        候选仍是 open、状态没推进」。后果是整个单元卡死：写正文看到 hasProse 就
        跳过，审校又被那条 open 候选挡住（服务端按单元判活跃）。

        收尾的判据只能是内容：候选的正文与盘上逐章一致 → 这次接受本来就会写出
        同样的内容，补接受即可（顺带把状态规范化）；不一致 → 盘上的版本不是它写
        的，不能替作者决定留哪一份，停下来问人。
        """
        pending = self.open_of("write", unit_id)
        if not pending:
            return
        cand = pending[-1]
        disk = self.prose_chapters(u.get("prosePath") or "")
        cand_ch = {c.get("chapter"): (c.get("body") or "").strip() for c in cand.get("chapters") or []}
        same = bool(cand_ch) and all(disk.get(n) == body for n, body in cand_ch.items()) and set(disk) == set(cand_ch)
        if not same:
            raise AuthorBlocked(
                f"单元 {unit_id} 既有正文，又挂着一份内容不同的正文候选 {cand['id'][:8]}。\n"
                f"脚本不能替你决定保留哪一版：请先在界面里比较两者，接受或拒绝其中之一后再重跑。")
        self.note(f"  盘上正文与残留候选 {cand['id'][:8]} 逐字一致")
        self.event("reconcile_write_candidate", unitId=unit_id, candidateId=cand["id"])
        if cand.get("baseRevision") == self.revision():
            # 版本还对得上：补接受，顺带把细纲里的 status 规范化。
            self.accept_write(unit_id, cand)
            return
        # 版本已过期：这条候选再也接受不了（Accept 会判 baseRevision），
        # 而它想写的内容已经在盘上了——留着只会挡住审校，丢掉没有任何损失。
        self.note("  候选已过期（生成后正文已被写入），作废它")
        self.reject_candidate(cand, "正文已落盘且内容一致，该候选已过期")

    def prose_chapters(self, prose_path: str) -> dict[int, str]:
        """把 units/<id>.md 解析成 {章号: 正文}，用于与候选逐章比对。"""
        if not prose_path:
            return {}
        try:
            raw = self.read_book_file(prose_path)
        except ApiError:
            return {}
        out: dict[int, str] = {}
        cur = None
        buf: list[str] = []
        for line in raw.split("\n"):
            if line.startswith("## "):
                if cur is not None:
                    out[cur] = "\n".join(buf).strip()
                m = re.match(r"##\s*第\s*(\d+)\s*章", line)
                cur = int(m.group(1)) if m else -1
                buf = []
                continue
            # 章之间的分隔线不属于正文：落盘时由 RenderProseAndStatus 插入，
            # 候选里没有。把它算进正文会让"逐章一致"永远判不成立。
            if line.strip() == "---":
                if cur is not None:
                    out[cur] = "\n".join(buf).strip()
                cur = None
                buf = []
                continue
            if cur is not None:
                buf.append(line)
        if cur is not None:
            out[cur] = "\n".join(buf).strip()
        return {k: v for k, v in out.items() if k > 0}

    def accept_write(self, unit_id: str, cand: dict[str, Any]) -> None:
        """接受正文候选；只对「文风类」阻断门禁做一次显式越权。

        这里有一个设计上的死结：正文候选带着本地硬门（去 AI 味、字数、空章）的
        findings，Accept 会因 blocking 而拒绝；但修订只在**正文已落盘之后**才可用。
        于是「文风有硬伤」这条会让流水线永久卡住——修不了，也存不下。

        界面上作者可以勾「强制接受」，因为人知道自己接下来会去审校。自动化里同理，
        但只能对**确定会被后续审校覆盖**的那类放行：
          - 结构问题（空章、字数严重不足）→ 不越权，交给作者
          - 文风问题（deslop/refine）→ 越权接受，随后的审校+修订会逐条修掉
        放行必须留痕：日志与事件流都会写明越权了哪几条。
        """
        try:
            self.accept(cand)
            return
        except AuthorBlocked:
            raise
        except ApiError as e:
            msg = str(e.message)
            if "阻断门禁" not in msg and "blocking" not in msg.lower():
                raise
            findings = cand.get("findings") or []
            blocking = [f for f in findings if f.get("severity") == "blocking"]
            structural = [f for f in blocking
                          if (f.get("lens") or "") not in self.STYLE_LENSES or f.get("chapter") in (None, 0)]
            empty = [c for c in cand.get("chapters") or [] if not (c.get("body") or "").strip()]
            if self.args.no_force_style or structural or empty or not blocking:
                raise AuthorBlocked(
                    f"正文候选带阻断门禁，且不属于可交给修订处理的问题：{msg}\n"
                    f"阻断项：{'；'.join(f.get('title', '') for f in blocking) or '（无）'}\n"
                    f"候选人 {cand['id'][:8]} 已保留在 {unit_id}；请人工判断后重跑。") from None
            titles = "；".join(f.get("title", "") for f in blocking)
            self.note(f"  文风类阻断门禁（{titles}）：按 --no-force-style 之外放行，交由审校+修订修掉")
            self.event("force_style_accept", unitId=unit_id, candidateId=cand["id"], findings=titles)
            self.accept(cand, force=True)
            self.note(f"  正文已落盘（越权：文风门禁）· {unit_id} → {self.unit(unit_id)['status']}")

    def stage_review_and_fix(self, unit_id: str) -> None:
        u = self.unit(unit_id)
        prose_path = u.get("prosePath") or ""
        if self.args.rewrite:
            # 旧清单是按旧正文定位的，重写后 leftover 的 open 清单会让
            # select_findings 拿到一批指向已不存在文字的 anchor。
            self.purge_unit(unit_id, ("review", "revise", "finalize"))
        reviews = self.open_of("review", unit_id)
        need_review = u["status"] == "drafted" or self.args.redo or self.args.rewrite
        if not reviews and need_review:
            run = self.run_once("review", unit_id, supersedeReview=True)
            status = run.get("status")
            if status not in ("succeeded", "waiting_accept", "failed"):
                raise Abort(f"审校失败：{(run.get('error') or '')[:300]}")
            if status == "failed" and not run.get("candidateId"):
                raise Abort(f"审校失败：{(run.get('error') or '')[:300]}")
            cand = self.candidate_from_run(run, "review", unit_id)
            if not cand:
                raise Abort("审校任务结束但没有候选")
            reviews = [cand]
            if status == "failed":
                self.note(f"  审校不完整（{ (run.get('error') or '')[:120] }），先按已有结论处理")
        elif reviews:
            self.note(f"  复用 {len(reviews)} 份未处理审校清单")

        if not reviews:
            self.note("  没有审校清单可处理（单元已复核），直接进入定稿")
            return

        findings = self.select_findings(reviews, prose_path)
        blocking = [f for f in findings if f.get("severity") == "blocking" and (f.get("roundsHit") or 0) >= 2]
        self.note(f"  审校结论 {sum(len(c.get('findings') or []) for c in reviews)} 条，"
                  f"可进正文修复 {len(findings)} 条（其中阻断 {len(blocking)} 条）")

        # 审校候选的 accept 是「归档」，不是「已复核」：一旦归档，它就不再是
        # open candidate，而 revise 的 findings 只从 open 的审校候选里取
        # （runs.execute → ListOpenCandidates）。先归档再修订，等于把要修的
        # 38 条全丢掉——revise 拿到 0 条、0 次调用，产出一份与原稿逐字相同的
        # 候选，接受后单元照样变成 reviewed。所以顺序只能反过来：
        # 先修订（接受 revise 时服务端自己归档旧清单），没东西可修时才归档。
        if not findings:
            self.note("  没有可修的正文问题，归档清单")
            for c in reviews:
                if c.get("status") == "open":
                    self.accept(c)
            return

        for round_no in range(1, max(1, self.args.fix_rounds) + 1):
            ids = [f["id"] for f in findings]
            self.note(f"  自动修复 第 {round_no} 轮 · 提交 {len(ids)} 条")
            run = self.run_once("revise", unit_id, issueIds=ids)
            if run.get("status") not in ("succeeded", "waiting_accept"):
                self.note(f"  修订失败：{(run.get('error') or '')[:300]}")
                raise Abort("修订未产出可落盘候选，正文保持不变")
            cand = self.candidate_from_run(run, "revise", unit_id)
            if not cand:
                raise Abort("修订任务成功但没有候选")
            # 0 次调用的修订候选必然什么都没改：宁可停下，也不要把"没修"写成"已修"。
            if not run.get("callsUsed"):
                raise Abort("修订任务 0 次调用，候选未做任何修改；拒绝写入以免把未修复当成已修复")
            if self.same_prose(cand):
                raise Abort("修订候选与原稿逐字相同：没有一条问题被真正修掉")
            self.accept(cand)
            self.note(f"  修订已落盘 · {unit_id} → {self.unit(unit_id)['status']}")
            if round_no >= self.args.fix_rounds:
                break
            # 落盘后旧清单已归档；只有显式要求多轮时才重审（默认 1 轮，
            # 与产品文档一致：落盘后自动复审会不断挖出旧账）。
            u = self.unit(unit_id)
            if u["status"] != "reviewed":
                break
            run = self.run_once("review", unit_id, supersedeReview=True)
            cand = self.candidate_from_run(run, "review", unit_id)
            if not cand:
                break
            findings = self.select_findings([cand], prose_path)
            if not findings:
                self.accept(cand)
                break

    def stage_finalize(self, unit_id: str) -> None:
        u = self.unit(unit_id)
        if u["status"] == "finalized" and not (self.args.redo or self.args.rewrite):
            self.note("  已定稿，跳过")
            return
        if not u.get("hasProse"):
            raise Abort(f"单元 {unit_id} 没有正文，不能定稿")
        # 定稿候选的账本可能自相矛盾（例如声明了伏笔却漏进 plannedForeshadowHandled），
        # 这种错只在 Accept 时才被拦下。整份作废重跑一次的代价（一次约 1.3 万 output
        # token）远低于卡住整个单元，所以给一轮重生成，仍然不行才交给人。
        for attempt in range(1, 3):
            cand = self.clear_stale("finalize", unit_id)
            if cand:
                self.note(f"  复用已生成的定稿候选 {cand['id'][:8]}")
            else:
                run = self.run_once("finalize", unit_id)
                if run.get("status") not in ("succeeded", "waiting_accept"):
                    raise Abort(f"定稿核对失败：{(run.get('error') or '')[:300]}")
                cand = self.candidate_from_run(run, "finalize", unit_id)
                if not cand:
                    raise Abort("定稿任务成功但没有候选")
                if len(cand.get("actualChapters") or []) != len(cand.get("chapters") or []):
                    raise Abort("定稿候选缺少逐章账本核对，拒绝接受")
            try:
                self.accept(cand)
                u = self.unit(unit_id)
                self.note(f"  已定稿 · {unit_id} → {u['status']}")
                return
            except ApiError as e:
                msg = str(e.message)
                if self.args.force_finalize and "blocking" in msg.lower():
                    self.note(f"  定稿门禁仍阻断，按 --force-finalize 越权接受：{msg[:160]}")
                    self.accept(cand, force=True)
                    u = self.unit(unit_id)
                    self.note(f"  已定稿（越权）· {unit_id} → {u['status']}")
                    return
                # 账本类问题重跑一次有意义：模型每次产出的账本都不一样，
                # 而这类校验只要求前后自洽。其余错误（时钟冲突、身份不一致）
                # 重跑不会变，直接交给作者。
                if attempt == 1 and self.is_ledger_conflict(msg):
                    self.note(f"  定稿账本自相矛盾，作废重生成一次：{msg[:160]}")
                    self.reject_candidate(cand, "账本自洽校验未通过，重生成")
                    continue
                raise Abort(f"定稿被门禁拒绝：{msg}") from None

    @staticmethod
    def is_ledger_conflict(msg: str) -> bool:
        """是否是「账本自身不自洽」类拒绝（重生成可能变好，而非数据层错误）。"""
        for needle in ("未明确核对", "尚未核对", "plannedForeshadowHandled", "账本"):
            if needle in msg:
                return True
        return False

    # ---- driver --------------------------------------------------------

    def pick_units(self) -> list[dict[str, Any]]:
        units = self.units()
        ids = [u["unitId"] for u in units]
        if self.args.frm:
            if self.args.frm not in ids:
                raise Abort(f"--from {self.args.frm} 不在本书单元里")
            units = units[ids.index(self.args.frm):]
        if self.args.to:
            if self.args.to not in ids:
                raise Abort(f"--to {self.args.to} 不在本书单元里")
            cut = [u for u in units]
            units = cut[:[u["unitId"] for u in cut].index(self.args.to) + 1]
        if self.args.max_units:
            units = units[: self.args.max_units]
        return units

    def plan(self, units: list[dict[str, Any]]) -> None:
        mode = "重写" if self.args.rewrite else "续写"
        self.note(f"计划（{mode}）：{self.args.book} · {len(units)} 个单元")
        for u in units:
            if self.args.rewrite:
                # 重写不看当前状态：细纲与正文都要重新生成。
                steps = ["细纲", "正文", "审校", "修订", "定稿"]
                note = f"（现为 {u['status']}）"
            else:
                steps = []
                if u["status"] == "proposed":
                    steps.append("细纲")
                if not u.get("hasProse"):
                    steps.append("正文")
                if u["status"] != "finalized" or self.args.redo:
                    steps += ["审校", "修订", "定稿"]
                note = ""
            self.note(f"  {u['unitId']:<8} {u['status']:<9} 章 {u['chapterFrom']:>3}-{u['chapterTo']:<3} "
                      f"{u['title'][:14]:<16} → {' + '.join(steps) if steps else '已完成'}{note}")

    def run(self) -> int:
        stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
        os.makedirs(self.args.log_dir, exist_ok=True)
        self.log_path = os.path.join(self.args.log_dir, f"auto-produce-{self.args.book}-{stamp}.log")
        self.dry = bool(self.args.dry_run)
        self.note(f"日志：{self.log_path}")

        snap = self.book()
        if snap["book"]["summary"].get("readOnly"):
            raise Abort("这本书是只读的，先 enable-write 才能落盘")
        units = self.pick_units()
        self.plan(units)
        if self.args.dry_run:
            self.scan_repairs(units)
            self.note("dry-run：未发起任何模型调用")
            return 0
        self.scan_repairs(units)

        failures = 0
        started = time.time()
        for idx, u in enumerate(units, 1):
            unit_id = u["unitId"]
            self.note(f"── [{idx}/{len(units)}] {unit_id} {u['title']}（{u['status']}）")
            try:
                if self.args.rewrite:
                    # 一次清干净：任何类型的待处理候选都会占住该单元，让新的
                    # 细纲/正文任务起不来（blockingRun 只按单元判活跃）。
                    self.purge_unit(unit_id, ("outline", "write", "review", "revise", "finalize"))
                self.stage_outline(unit_id)
                self.stage_write(unit_id)
                self.stage_review_and_fix(unit_id)
                self.stage_finalize(unit_id)
                self.report.append({"unitId": unit_id, "ok": True})
            except AuthorBlocked as e:
                failures += 1
                self.note(f"  需要作者处理，整批停下：{e}")
                self.report.append({"unitId": unit_id, "ok": False, "needsAuthor": True, "error": str(e)})
                self.event("author_blocked", unitId=unit_id, error=str(e))
                self.write_author_todo(unit_id, str(e))
                break
            except Abort as e:
                failures += 1
                self.note(f"  中止本单元：{e}")
                self.report.append({"unitId": unit_id, "ok": False, "error": str(e)})
                self.event("unit_failed", unitId=unit_id, error=str(e))
                if not self.args.keep_going:
                    self.note("遇到失败即停（--keep-going 可跳过失败单元继续）")
                    break
                # 即使允许跳过，缺正文的单元也不能跳：后面每个单元的第一章都
                # 要靠上一单元的正文做承接。跳过它 = 把断链写进正文。
                if not self.unit(unit_id).get("hasProse") and not self.args.allow_gap:
                    self.note("该单元还没有正文，后续单元会接不上；仍然停下")
                    break
            except ApiError as e:
                failures += 1
                self.note(f"  API 错误：{e}")
                self.report.append({"unitId": unit_id, "ok": False, "error": str(e)})
                self.event("unit_failed", unitId=unit_id, error=str(e))
                if not self.args.keep_going:
                    break

        took = int(time.time() - started)
        ok = sum(1 for r in self.report if r["ok"])
        self.note(f"结束：{ok}/{len(self.report)} 个单元走完，失败 {failures}，用时 {took // 60} 分 {took % 60} 秒")
        with open(self.log_path + ".report.json", "w", encoding="utf-8") as fh:
            json.dump({"book": self.args.book, "report": self.report}, fh, ensure_ascii=False, indent=2)
        return 0 if failures == 0 else 1


def main() -> int:
    ap = argparse.ArgumentParser(description="Danmo Novel 自动写作流水线（HTTP API 驱动）")
    ap.add_argument("--base", default=os.environ.get("DANMO_BASE", "http://127.0.0.1:7804"))
    ap.add_argument("--book", required=True, help="书籍 id，例如 zhong-an-94")
    ap.add_argument("--from", dest="frm", help="起始单元 id（含），例如 v01-U2")
    ap.add_argument("--to", help="结束单元 id（含）")
    ap.add_argument("--max-units", type=int, default=0, help="本批最多跑几个单元")
    ap.add_argument("--knowledge", default="",
                    help="写作/审校/修订/定稿附带的参考知识文章 id，逗号分隔。"
                         "整批固定同一组，中途改动会丢掉前缀缓存。")
    ap.add_argument("--fix-rounds", type=int, default=1, help="审校→修复循环轮数（默认 1）")
    ap.add_argument("--stage-retries", type=int, default=1, help="单阶段失败重试次数（默认 1）")
    ap.add_argument("--net-retries", type=int, default=4,
                    help="网络类失败（DNS/握手超时/连接重置）的重试次数，指数退避（默认 4）")
    ap.add_argument("--outline-repairs", type=int, default=2,
                    help="细纲被接受校验拒绝时的重生成次数（默认 2，带校验错误原文当修复指令）")
    ap.add_argument("--force-finalize", action="store_true", help="定稿门禁仍阻断时强制接受")
    ap.add_argument("--no-force-style", action="store_true",
                    help="正文候选带文风类阻断门禁时也停下（默认放行，交给后续审校+修订修掉）")
    ap.add_argument("--keep-going", action="store_true",
                    help="某单元失败后继续下一个。注意：若失败单元缺正文，下一个单元的写入仍会被顺序保护拦住")
    ap.add_argument("--allow-gap", action="store_true",
                    help="允许在上一单元还没有正文时跳写本单元（会写出接不上上文的正文）")
    ap.add_argument("--no-repair", action="store_true",
                    help="跳过开工前的卷纲/身份数据体检（体检会自动修正写错单元的身份转换）")
    ap.add_argument("--content-fallback-model", default="",
                    help="撞到内容审核（data_inspection_failed）时改用的模型；留空则只按原模型重试，不换模型")
    ap.add_argument("--redo", action="store_true",
                    help="对已定稿单元重跑 审校→修订→定稿（修复缺陷后重跑受影响单元用）")
    ap.add_argument("--rewrite", action="store_true",
                    help="从 --from 起的每个单元都重写：重新生成细纲 → 重新写正文 → 审校 → 修订 → 定稿"
                         "（不动已定稿在 --from 之前的单元）")
    ap.add_argument("--instruction", default="",
                    help="只对细纲/正文两个阶段追加的作者指令（重写时说明要改什么）")
    ap.add_argument("--dry-run", action="store_true", help="只打印计划，不发任何调用")
    ap.add_argument("--poll-interval", type=float, default=3.0)
    ap.add_argument("--verbose-poll", type=float, default=0.0, help=">0 时每次轮询都打印进度")
    ap.add_argument("--stall-timeout", type=int, default=1200,
                    help="任务既没有新调用、updatedAt 也不动超过这么多秒时判定为卡死（默认 1200）"
                         "；机器休眠的时间会自动扣除，不计入停滞")
    ap.add_argument("--log-dir", default="out/auto-produce")
    ap.add_argument("--daemon", action="store_true",
                    help="脱离当前会话后台运行（输出追加到 --daemon-out，pid 写进同目录 .pid）")
    ap.add_argument("--daemon-out", default="out/auto-produce/run.out")
    args = ap.parse_args()
    args.knowledge = [x.strip() for x in (args.knowledge or "").split(",") if x.strip()]
    if args.daemon:
        os.makedirs(os.path.dirname(args.daemon_out) or ".", exist_ok=True)
        pid = daemonize(args.daemon_out)
        with open(args.daemon_out + ".pid", "w", encoding="utf-8") as fh:
            fh.write(str(pid))
    try:
        return Driver(args).run()
    except Abort as e:
        print(f"[{now()}] 终止：{e}", file=sys.stderr)
        return 2
    except ApiError as e:
        print(f"[{now()}] API 不可用：{e}", file=sys.stderr)
        return 2


if __name__ == "__main__":
    sys.exit(main())
