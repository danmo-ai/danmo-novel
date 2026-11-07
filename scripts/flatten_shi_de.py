#!/usr/bin/env python3
"""把「是 + 状语 + 动词 + 的」这类机械句式压平成普通叙述。

机械修法：删掉「是」和「的」，状语留着。

    王建国是踩着点进的   -> 王建国踩着点进来
    他是在门口等的       -> 他在门口等
    老李是昨天走的       -> 老李昨天走了

为什么只挑这 29 处：粗模式（是…的）在两本书里命中 1757 处，其中绝大多数是
正常用法，不该动。判据是「是」后面必须跟**状语**（时间/处所/方式），且整句是
「主语 + 是 + 状语 + 动词 + 的」结构；下面这些一律排除：

  问句          「是谁说的？」「是从哪儿收的？」「是在哪签的？」
  判断/属格     「是纺织厂的」「是右脚的」「是干净的吗」
  否定对照      「不是半夜倒的」「不是从天上掉下来的」
  熟语/固定说法  「话是这么说」「图是死的」「笑还是先到的」
  定语从句      「照片里那种接机油用的」

替换句逐条写死，执行前断言只命中一次，不做正则改写——正则在这批文本上会切坏
句子（历史上已验证过）。

用法：
    python3 scripts/flatten_shi_de.py --dry-run
    python3 scripts/flatten_shi_de.py --apply
"""
from __future__ import annotations

import argparse
import os
import shutil
import sys
from datetime import datetime

# (书, 单元, 原串, 新串)
REPLACEMENTS = [
    # ---- 重生1994：从派出所到省纪委 ----
    ("zhong-an-94", "v01-U4",
     "回执是照程序开的，谈话也是照程序谈的。",
     "回执照程序开，谈话也照程序谈。"),
    ("zhong-an-94", "v01-U6",
     "复印件是照着别的东西做的。",
     "复印件照着别的东西做。"),
    ("zhong-an-94", "v01-U7",
     "鞋上还带着泥，是早上从镇里取证点回来的。",
     "鞋上还带着泥，早上从镇里取证点回来。"),
    ("zhong-an-94", "v02-U4",
     "电话是下午打来的。",
     "电话下午打来了。"),
    ("zhong-an-94", "v02-U5",
     "陆沉是下午从赵德柱那儿听说的。",
     "陆沉下午从赵德柱那儿听说。"),
    ("zhong-an-94", "v02-U5",
     "纪要是当场写的。",
     "纪要当场写。"),
    ("zhong-an-94", "v02-U5",
     "中标结果是昨天下午四点公布的。",
     "中标结果昨天下午四点公布。"),
    ("zhong-an-94", "v02-U8",
     "会议纪要是当场整理、当场宣读口径的。",
     "会议纪要当场整理、当场宣读口径。"),
    ("zhong-an-94", "v03-U1",
     "变化是下午开始看得见的。",
     "变化下午开始看得见。"),
    ("zhong-an-94", "v03-U2",
     "秦素芬是跟着安置点的车来的。",
     "秦素芬跟着安置点的车来。"),
    ("zhong-an-94", "v03-U4",
     "林晚是自己想办法跟过去的。",
     "林晚自己想办法跟过去。"),
    ("zhong-an-94", "v03-U5",
     "这张是流水账页，是按正式凭证誊的。",
     "这张是流水账页，按正式凭证誊。"),

    # ---- 女警她脑子里有个刑侦泰斗 ----
    ("mindvoice-cop", "v01-U2",
     "周卫国是上午十点到的。",
     "周卫国上午十点到。"),
    ("mindvoice-cop", "v01-U4",
     "函是上午十点通过分局机要渠道发出去的。",
     "函上午十点通过分局机要渠道发出。"),
    ("mindvoice-cop", "v01-U6",
     "移交单是在分局二楼的档案室签的。",
     "移交单在分局二楼的档案室签。"),
    ("mindvoice-cop", "v01-U8",
     "那份说明是在小区物业办公室签的。",
     "那份说明在小区物业办公室签。"),
    ("mindvoice-cop", "v02-U4",
     "成绩是在第五天早上公布的。",
     "成绩第五天早上公布。"),
    ("mindvoice-cop", "v02-U4",
     "裴国栋是下午把她叫去的。",
     "裴国栋下午把她叫去。"),
    ("mindvoice-cop", "v02-U5",
     "搜查是在上午做的。",
     "搜查在上午做。"),
    ("mindvoice-cop", "v03-U3",
     "回江州的车是下午发的。",
     "回江州的车下午发。"),
    ("mindvoice-cop", "v03-U4",
     "张宝是下午两点被带来的。",
     "张宝下午两点被带来。"),
    ("mindvoice-cop", "v03-U4",
     "编号是上午十一点半下来的。",
     "编号上午十一点半下来。"),
    ("mindvoice-cop", "v03-U5",
     "温念慈是在行动前两小时赶到临川的。",
     "温念慈在行动前两小时赶到临川。"),
    ("mindvoice-cop", "v04-U2",
     "见证记录是当场写的。",
     "见证记录当场写。"),
    ("mindvoice-cop", "v04-U5",
     "谈判是在第二天晚上定下来的。",
     "谈判在第二天晚上定下来。"),
    ("mindvoice-cop", "v04-U6",
     "新案子是当天上午派下来的。",
     "新案子当天上午派下来。"),
    ("mindvoice-cop", "v05-U1",
     "回函是当天午后开始到的。",
     "回函当天午后开始到。"),
    ("mindvoice-cop", "v05-U3",
     "张宝是前天晚上被接过来的。",
     "张宝前天晚上被接过来。"),
    ("mindvoice-cop", "v05-U3",
     "说明三份文书是在同一段日子里、从同一个窗口递出去的",
     "说明三份文书在同一段日子里、从同一个窗口递出去"),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--root", default=os.path.expanduser("~/.danmo-novel/library/novel"))
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    apply = args.apply and not args.dry_run

    stamp = datetime.now().strftime("%Y%m%d-%H%M%S")
    by_book: dict[str, list] = {}
    for book, unit, old, new in REPLACEMENTS:
        by_book.setdefault(book, []).append((unit, old, new))

    total_ok = 0
    for book, items in by_book.items():
        units_dir = os.path.join(args.root, book, "units")
        if not os.path.isdir(units_dir):
            print(f"跳过 {book}：找不到 {units_dir}", file=sys.stderr)
            continue
        # 备份一次
        if apply:
            dst = f"{units_dir}.bak-shide-{stamp}"
            shutil.copytree(units_dir, dst)
            print(f"备份 {units_dir} -> {dst}")

        for unit, old, new in items:
            path = os.path.join(units_dir, unit + ".md")
            if not os.path.exists(path):
                print(f"!! {book}/{unit}: 文件不存在，跳过")
                continue
            raw = open(path, encoding="utf-8").read()
            n = raw.count(old)
            if n != 1:
                # 命中 0 次说明原文已被改过；命中多次说明锚点不够长。
                # 两种都不擅自替换。
                print(f"!! {book}/{unit}: 命中 {n} 次，跳过：{old[:26]}")
                continue
            print(f"{book}/{unit}: {old[:34]}")
            print(f"{'':14}-> {new[:34]}")
            total_ok += 1
            if apply:
                open(path, "w", encoding="utf-8").write(raw.replace(old, new, 1))

    print()
    print(f"共 {total_ok}/{len(REPLACEMENTS)} 处{'已写入' if apply else '（dry-run 未写入）'}")
    return 0 if total_ok == len(REPLACEMENTS) else 1


if __name__ == "__main__":
    sys.exit(main())
