#!/usr/bin/env python3
"""把《女警她脑子里有个刑侦泰斗》正文里的「第 N 天」改成读者能理解的相对时间。

背景与 zhong-an-94 相同：time-guidance 曾把绝对累计日当成"相对锚点"的示例，
正文于是出现「第 22 天一早」「第106天」这类写法。根因已在代码侧修掉
（禁止写进正文 + 本地硬门拦截），这里只回溯已发布的历史章节。

替换原则（逐条人工确定）：
  1. 句内已有相对锚点的（隔了一夜 / 隔了一天 / 隔天傍晚 / 装车那天），
     直接删掉日计数，不动其余文字。
  2. 其余按「与上一章的间隔天数」改成第二天 / N 天后 / 当天，间隔来自细纲的
     story_day + day_offset。
  3. 两处时钟与正文不一致的（v01-U6 ch33、v04-U2 ch162），不用推算值：
     ch33 原文下一句已写明「头天下午」，按第二天改；ch162 是在叙述更早的事，
     改成事件锚点「后街附属房那晚」。

用法：
    python3 scripts/fix_day_counters_mindvoice.py --dry-run
    python3 scripts/fix_day_counters_mindvoice.py --apply
"""
from __future__ import annotations

import argparse
import os
import re
import shutil
import sys
from datetime import datetime

BOOK = "mindvoice-cop"

# (单元, 原串, 新串, 依据)
REPLACEMENTS = [
    ("v01-U3", "第 22 天一早，温念慈先去了内勤室。",
     "两天后一早，温念慈先去了内勤室。", "与上一章隔 2 天"),
    ("v01-U3", "车窗外天色压下来，第 22 天快黑了。",
     "车窗外天色压下来，天快黑了。", "同章内同日，删计数"),
    ("v01-U4", "隔了一夜，第 23 天清早，温念慈还没来得及去分局调底单",
     "第二天清早，温念慈还没来得及去分局调底单", "原文已有「隔了一夜」"),
    ("v01-U4", "第 28 天凌晨四点，双桥派出所后院的灯全熄着",
     "两天后凌晨四点，双桥派出所后院的灯全熄着", "与上一章隔 2 天"),
    ("v01-U5", "第 38 天，清晨六点二十，天刚蒙蒙亮",
     "第二天清晨六点二十，天刚蒙蒙亮", "与上一章隔 1 天"),
    ("v01-U6", "第 53 天上午，温念慈把楼梯间那个观察点撤了。",
     "第二天上午，温念慈把楼梯间那个观察点撤了。", "下一句写明「头天下午」"),
    ("v01-U6", "第 63 天清晨，观察点的早餐摊刚支起来",
     "清晨，观察点的早餐摊刚支起来", "细纲与账本的间隔不一致，不用推算值"),
    ("v02-U2", "第106天，隔了一天，天刚亮透",
     "隔了一天，天刚亮透", "原文已有「隔了一天」"),
    ("v02-U2", "第117天一早，温念慈把复核表摊在桌上。",
     "三天后一早，温念慈把复核表摊在桌上。", "与上一章隔 3 天"),
    ("v02-U2", "日期落在第117天。",
     "日期落在当天。", "同章内同日"),
    ("v02-U2", "复核说明会排在第120天上午",
     "复核说明会排在三天后的上午", "与上一章隔 3 天"),
    ("v02-U3", "第 129 天，清晨六点，双桥派出所后院的老榆树下",
     "三天后，清晨六点，双桥派出所后院的老榆树下", "与上一章隔 3 天"),
    ("v02-U3", "方子衡交流材料，第 129 天收。",
     "方子衡交流材料，当天收。", "同章内同日"),
    ("v02-U5", "第 149 天一早，裴国栋把温念慈叫到卷宗柜前",
     "第二天一早，裴国栋把温念慈叫到卷宗柜前", "与上一章隔 1 天"),
    ("v02-U5", "第158天上午，鉴定机构的传真先到了。",
     "两天后上午，鉴定机构的传真先到了。", "与上一章隔 2 天"),
    ("v02-U6", "第 166 天，天没亮透，温念慈已经到了城南货站路。",
     "两天后，天没亮透，温念慈已经到了城南货站路。", "与上一章隔 2 天"),
    ("v02-U8", "第190天上午，她接到老办事员的电话",
     "两天后上午，她接到老办事员的电话", "与上一章隔 2 天"),
    ("v03-U2", "第 229 天，温念慈起得比闹钟早。",
     "四天后，温念慈起得比闹钟早。", "与上一章隔 4 天"),
    ("v03-U2", "第 233 天，庐州宾馆的会议室里提前坐进了一个不该这么早出现的人。",
     "四天后，庐州宾馆的会议室里提前坐进了一个不该这么早出现的人。", "与上一章隔 4 天"),
    ("v03-U5", "第 271 天，清晨五点半，临川的天还没亮透。",
     "两天后，清晨五点半，临川的天还没亮透。", "与上一章隔 2 天"),
    ("v03-U5", "回到江州是隔天傍晚，第 276 天。",
     "回到江州是隔天傍晚。", "原文已有「隔天傍晚」"),
    ("v03-U8", "装车那天是第 330 天上午，天阴",
     "装车那天上午，天阴", "原文已有「装车那天」锚点"),
    ("v04-U2", "她从第335天后街的附属房讲起。",
     "她从后街附属房那晚讲起。", "在叙述更早的事，改成事件锚点"),
    ("v04-U3", "第 340 天一早，周卫国的二八大杠停在旧城东巷口",
     "两天后一早，周卫国的二八大杠停在旧城东巷口", "与上一章隔 2 天"),
    ("v04-U8", "第 412 天的光斜进来，照出纸面上一道细白的茬口",
     "那天的光斜进来，照出纸面上一道细白的茬口", "与上一章隔 1 天，用「那天」避免重复"),
    ("v05-U2", "第451天午后，温念慈把三份经手栏的复印件带进询问室",
     "第二天午后，温念慈把三份经手栏的复印件带进询问室", "与上一章隔 1 天"),
    ("v05-U4", "第466天上午，李岩推着一辆借来的平板车进了专案作战室。",
     "第二天上午，李岩推着一辆借来的平板车进了专案作战室。", "与上一章隔 1 天"),
]

DAY_RE = re.compile(r"第\s*[0-9]{1,4}\s*天")

# (单元, 原串, 新串) —— actual 摘要与回证里的日计数。
# 摘要会被投影成「最近章摘要」注入下一章，写进去等于把这种写法传给后文；
# 回证（evidence）是正文的逐字引用，正文改了它必须跟着改，否则会被判
# 「回证未在正文中找到」。同样逐条写死：正则在这批文本上会切出
# 「，温念慈」「起在废品点」这类残句。
SUMMARY_REPLACEMENTS = [
    ("v01-U3", "第22天，温念慈先在内勤室", "温念慈先在内勤室"),
    ("v01-U4", "第23天清早接报沈阿婶家失窃", "第二天清早接报沈阿婶家失窃"),
    ("v01-U4", "第28天起在废品点外蹲守三夜", "此后在废品点外蹲守三夜"),
    ("v01-U4", "第31天上午九点在库房依法开箱查验", "三天后上午九点在库房依法开箱查验"),
    ("v01-U5", "第38天清晨，温念慈到市公司签收", "第二天清晨，温念慈到市公司签收"),
    # 定稿模型自己留下的两处「待确认」自述：第一条的前提（正文写第 53 天）
    # 已被本次修复消除；第二条的实质（超出计划结束日）保留，只去掉日计数。
    ("v01-U6", "（待确认：本章正文写「第 53 天上午」，与前章开场第 53 天不衔接，按推进记为第 58 天。）", ""),
    ("v01-U6", "（待确认：正文写“收册定在一个星期以后”，超出单元计划结束日，按正文记为第 73 天。）",
     "（正文写“收册定在一个星期以后”，超出单元计划结束日。）"),
    ("v02-U3", "第129天，陆沉明说方子衡", "三天后，陆沉明说方子衡"),
    ("v02-U5", "第149天裴国栋把认人线", "第二天裴国栋把认人线"),
    ("v02-U5", "第158天市里鉴定机构的传真先到", "两天后市里鉴定机构的传真先到"),
    ("v02-U6", "第166天清晨温念慈到城南货站路", "两天后清晨温念慈到城南货站路"),
    ("v02-U6", "evidence: 第 166 天，天没亮透，温念慈已经到了城南货站路。",
     "evidence: 两天后，天没亮透，温念慈已经到了城南货站路。"),
    ("v03-U2", "第229天，张宝到庐州辨认", "四天后，张宝到庐州辨认"),
    ("v03-U2", "第233天，娄振声提前到庐州质询", "四天后，娄振声提前到庐州质询"),
    ("v03-U5", "第271天清晨温念慈赶到临川", "两天后清晨温念慈赶到临川"),
    ("v03-U5", "第276天回江州过证据链", "三天后回江州过证据链"),
    ("v04-U3", "第340天复测旧现场", "两天后复测旧现场"),
    ("v04-U8", "第412天，温念慈在临时位系统核对目录", "第二天，温念慈在临时位系统核对目录"),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=os.path.expanduser("~/.danmo-novel/library/novel"))
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    apply = args.apply and not args.dry_run

    root = os.path.join(args.root, BOOK)
    units_dir = os.path.join(root, "units")
    actual_dir = os.path.join(root, "continuity", "actual")
    if not os.path.isdir(units_dir):
        print(f"找不到 {units_dir}", file=sys.stderr)
        return 2

    if apply:
        stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
        for src in (units_dir, actual_dir):
            if os.path.isdir(src):
                dst = f"{src}.bak-daycount-{stamp}"
                shutil.copytree(src, dst)
                print(f"备份 {src} -> {dst}")

    ok = 0
    touched: dict[str, list[str]] = {}
    for unit, old, new, why in REPLACEMENTS:
        path = os.path.join(units_dir, unit + ".md")
        if not os.path.exists(path):
            print(f"!! {unit}: 文件不存在，跳过")
            continue
        raw = open(path, encoding="utf-8").read()
        n = raw.count(old)
        if n != 1:
            print(f"!! {unit}: 命中 {n} 次，跳过：{old[:30]}")
            continue
        print(f"{unit}: {old[:38]}")
        print(f"{'':10}-> {new[:38]}   [{why}]")
        ok += 1
        touched.setdefault(unit, []).append(old)
        if apply:
            open(path, "w", encoding="utf-8").write(raw.replace(old, new, 1))

    # 摘要同步：summary 会被投影成「最近章摘要」注入下一章，日计数必须一并清。
    summary_fixed = 0
    for unit, old, new in SUMMARY_REPLACEMENTS:
        path = os.path.join(actual_dir, unit + ".yaml")
        if not os.path.exists(path):
            print(f"!! 摘要 {unit}: 文件不存在，跳过")
            continue
        raw = open(path, encoding="utf-8").read()
        n = raw.count(old)
        if n != 1:
            print(f"!! 摘要 {unit}: 命中 {n} 次，跳过：{old[:30]}")
            continue
        summary_fixed += 1
        print(f"  摘要 {unit}: {old[:34]}  ->  {new[:30] or '（删除）'}")
        if apply:
            open(path, "w", encoding="utf-8").write(raw.replace(old, new, 1))

    print()
    print(f"正文 {ok}/{len(REPLACEMENTS)} 处；摘要 {summary_fixed} 处；"
          f"{'已写入' if apply else 'dry-run 未写入'}")
    if apply:
        left = 0
        for fname in os.listdir(units_dir):
            if fname.endswith(".md"):
                left += len(DAY_RE.findall(open(os.path.join(units_dir, fname), encoding="utf-8").read()))
        if os.path.isdir(actual_dir):
            for fname in os.listdir(actual_dir):
                if fname.endswith(".yaml"):
                    left += len(DAY_RE.findall(open(os.path.join(actual_dir, fname), encoding="utf-8").read()))
        print(f"复查：剩余「第N天」{left} 处")
    return 0


if __name__ == "__main__":
    sys.exit(main())
