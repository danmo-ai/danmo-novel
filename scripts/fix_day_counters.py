#!/usr/bin/env python3
"""把正文里「第 N 天」这类累计日计数改成读者能理解的相对时间。

背景：time-guidance 曾把「第 N 天」当成"相对锚点"的示例，于是正文出现
「第352天上午」这种写法——读者不知道第 1 天是哪一天，它只是生产账本的数字。
实测成稿 33 万字里 25 处，并且被定稿摘要沿用、再通过「最近章摘要」注入下一章，
形成自我强化的循环。

替换原则（逐句人工确定，不做通用正则改写）：
  1. 句子后面已经有事件锚点的（公示期满这天 / 批复下来那天 / 距收网夜过去半个多月），
     直接删掉日计数，不动其余文字。
  2. 其余按「与上一章的间隔天数」改成次日 / N 天后，间隔取自细纲的
     story_day + day_offset，与正文里写的天数已逐条核对一致。
  3. 修饰时间点时避开「第二天天刚亮」这类重复字，能删就删。

用法：
    python3 scripts/fix_day_counters.py --book zhong-an-94 --dry-run
    python3 scripts/fix_day_counters.py --book zhong-an-94 --apply
"""
from __future__ import annotations

import argparse
import os
import re
import shutil
import sys
from datetime import datetime

# (单元, 原串, 新串, 说明)
REPLACEMENTS = [
    ("v01-U2", "第18天早上，陆沉把这七份纸摊在宿舍桌上",
     "七天后的早上，陆沉把这七份纸摊在宿舍桌上", "与上一章隔 7 天"),
    ("v01-U3", "第94天，陆沉起了个大早，赶头一班进县的车。",
     "第二天，陆沉起了个大早，赶头一班进县的车。", "与上一单元末章隔 1 天"),
    ("v01-U3", "第 164 天，县局档案室的日光灯管有一根坏了，闪一下，灭一下。",
     "二十五天后，县局档案室的日光灯管有一根坏了，闪一下，灭一下。", "与上一章隔 25 天"),
    ("v01-U5", "隔了整整三十天，陆沉是第352天上午知道举报信的。",
     "隔了整整三十天，陆沉才知道举报信的事。", "句内已有「三十天」，删日计数"),
    ("v01-U5", "第397天一早，雨从半夜下到天亮，素芬商贸筹备处的门槛外积了一洼浑水。",
     "十七天后的一早，雨从半夜下到天亮，素芬商贸筹备处的门槛外积了一洼浑水。", "与上一章隔 17 天"),
    ("v01-U6", "第 717 天，公示期满这一天，天阴得很平，不雨不晴。",
     "公示期满这一天，天阴得很平，不雨不晴。", "已有「公示期满这一天」锚点"),
    ("v01-U8", "第 804 天上午，陆沉抱着最后一份移交清册走进县局办公楼。",
     "六天后的上午，陆沉抱着最后一份移交清册走进县局办公楼。", "与上一章隔 6 天"),
    ("v02-U1", "第 828 天，清早，积案组办公室还没来人。",
     "五天后，清早，积案组办公室还没来人。", "与上一章隔 5 天"),
    ("v02-U1", "第 845 天早上，陆沉到积案组的时候",
     "十天后的早上，陆沉到积案组的时候", "与上一章隔 10 天"),
    ("v02-U1", "批复下来那天，是第 883 天，陆沉到积案组的第七十九天。",
     "批复下来那天，是陆沉到积案组的第七十九天。", "已有「批复下来那天」锚点"),
    ("v02-U2", "第893天，清早，雾没散。",
     "隔天清早，雾没散。", "与上一章隔 1 天"),
    ("v02-U3", "第 960 天一早，陆沉把流转单、转租合同和维修记录摊在积案组的旧桌上",
     "十二天后的一早，陆沉把流转单、转租合同和维修记录摊在积案组的旧桌上", "与上一章隔 12 天"),
    ("v02-U4", "第1053天一早，车停在修理厂门口",
     "第二天一早，车停在修理厂门口", "与上一章隔 1 天"),
    ("v02-U5", "第1098天早上，陆沉到办公室先没碰电话。",
     "第二天早上，陆沉到办公室先没碰电话。", "与上一章隔 1 天"),
    ("v02-U5", "第1120天一早，王建国把陆沉叫到了县局的小会议室。",
     "八天后的一早，王建国把陆沉叫到了县局的小会议室。", "与上一章隔 8 天"),
    ("v02-U6", "前一晚的雨到后半夜才停。第1145天上午九点，",
     "前一晚的雨到后半夜才停。第二天上午九点，", "与上一章隔 1 天"),
    ("v02-U7", "第 1165 天清晨，距收网夜过去半个多月，雨后的指挥部有一股潮纸味。",
     "距收网夜过去半个多月后的清晨，雨后的指挥部有一股潮纸味。", "已有「半个多月」锚点"),
    ("v03-U3", "审查会才排在第1448天上午，市纪委三楼的小会议室。",
     "审查会才排到七天后的上午，市纪委三楼的小会议室。", "与上一章隔 7 天"),
    ("v03-U3", "第1492天早上八点，陆沉到交易中心门口的时候",
     "二十四天后的早上八点，陆沉到交易中心门口的时候", "与上一章隔 24 天"),
    ("v03-U4", "第1493天上午，雨没下来，天还阴着。",
     "第二天上午，雨没下来，天还阴着。", "与上一章隔 1 天"),
    ("v03-U4", "第 1521 天，涯州，清晨五点半。",
     "十天后，涯州，清晨五点半。", "与上一章隔 10 天"),
    ("v03-U5", "举报件是第 1536 天上午转到专案组的。",
     "举报件是五天后的上午转到专案组的。", "与上一章隔 5 天"),
    ("v03-U5", "第1542天夜里十点，临州市局大会议室的灯一直没熄。",
     "六天后的夜里十点，临州市局大会议室里那盏灯还亮着。", "与上一章隔 6 天"),
    ("v03-U5", "第1549天一早，陆沉把收网报告的副本推到桌角，盯着看了很久。",
     "七天后的一早，陆沉把收网报告的副本推到桌角，盯着看了很久。", "与上一章隔 7 天"),
    ("v03-U6", "第1550天，天刚亮，周明远就动身去省城。",
     "天刚亮，周明远就动身去省城。", "已有「天刚亮」，删日计数"),
]

# actual 摘要里的日计数。summary 会被投影成「最近章摘要」注入下一章的写作包，
# 写进去等于把这种写法继续传给后文，所以必须一起清。
#
# 这里同样逐条写死而不是用正则替换：正则在这批数据上切坏过三处——
#   「第397天雨，退伙信…」→「雨，退伙信…」
#   「第三天（第1148天）上午」→「第三天（）上午」
#   「第1145天上午，3·17…」→「上午，3·17…」（以时间词开头的残句）
SUMMARY_REPLACEMENTS = [
    ("v01-U3", "第164天陆沉在县局档案室校完三表对照", "陆沉在县局档案室校完三表对照"),
    ("v01-U3", "第190天刘三喜心梗脱险后补签口供", "刘三喜心梗脱险后补签口供"),
    ("v01-U5", "第352天上午，王建国把县纪委转来的匿名廉洁举报件复印件交给陆沉",
     "王建国把县纪委转来的匿名廉洁举报件复印件交给陆沉"),
    ("v01-U5", "第397天雨，退伙信已攒了一个多月", "雨天，退伙信已攒了一个多月"),
    ("v01-U8", "第804天上午，陆沉把最后一份移交清册交给王建国签字",
     "陆沉把最后一份移交清册交给王建国签字"),
    ("v02-U1", "第828天，陆沉定下", "陆沉定下"),
    ("v02-U1", "第845天，陆沉与赵德柱再访", "陆沉与赵德柱再访"),
    ("v02-U1", "第883天，重启获批但条件严格", "重启获批但条件严格"),
    ("v02-U6", "第1145天上午，3·17专案联合指挥部首次把", "3·17专案联合指挥部首次把"),
    ("v02-U6", "名单风波后第三天（第1148天）上午，收网方案初稿上会。",
     "名单风波后第三天上午，收网方案初稿上会。"),
    ("v03-U3", "材料补正两回后，第1448天上午在市纪委三楼开任前审查会",
     "材料补正两回后，任前审查会开在市纪委三楼"),
    ("v03-U3", "第1492天上午，签字台已摆在交易中心门口。", "签字台已摆在交易中心门口。"),
    ("v03-U4", "第1521天清晨，涯州货栈街小院按协作程序由当地执行抓捕",
     "涯州货栈街小院按协作程序由当地执行抓捕"),
    # 这条是定稿模型自己的算术错误：正文「第七天」对应第 1550 天，落在合同
    # 1550–1557 内，并没有超限。留着会让后续单元误以为这一章有未解决的冲突。
    ("v03-U6", "按正文「第七天」推算本章为第1561天，超出时间合同第1557天上限，待确认。", ""),
]


def main() -> int:
    ap = argparse.ArgumentParser()
    ap.add_argument("--book", required=True)
    ap.add_argument("--root", default=os.path.expanduser("~/.danmo-novel/library/novel"))
    ap.add_argument("--apply", action="store_true")
    ap.add_argument("--dry-run", action="store_true")
    args = ap.parse_args()
    apply = args.apply and not args.dry_run

    root = os.path.join(args.root, args.book)
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

    changed_units = 0
    applied = 0
    for unit, old, new, why in REPLACEMENTS:
        path = os.path.join(units_dir, unit + ".md")
        if not os.path.exists(path):
            print(f"跳过（无此文件）{unit}")
            continue
        raw = open(path, encoding="utf-8").read()
        n = raw.count(old)
        if n != 1:
            print(f"!! {unit}: 原串命中 {n} 次（必须是 1 次），跳过：{old[:30]}")
            continue
        print(f"{unit}: {old[:34]}  ->  {new[:34]}   [{why}]")
        applied += 1
        if apply:
            open(path, "w", encoding="utf-8").write(raw.replace(old, new, 1))
            changed_units += 1

    # 摘要清理
    summary_hits = 0
    for unit, old, new in SUMMARY_REPLACEMENTS:
        path = os.path.join(actual_dir, unit + ".yaml")
        if not os.path.exists(path):
            print(f"跳过摘要（无此文件）{unit}")
            continue
        raw = open(path, encoding="utf-8").read()
        n = raw.count(old)
        if n != 1:
            print(f"!! 摘要 {unit}: 原串命中 {n} 次（必须是 1 次），跳过：{old[:30]}")
            continue
        summary_hits += 1
        print(f"  摘要 {unit}: {old[:30]}…  ->  {new[:30] or '（删除）'}")
        if apply:
            open(path, "w", encoding="utf-8").write(raw.replace(old, new, 1))

    print()
    print(f"正文替换 {applied} 处；摘要清理 {summary_hits} 处；"
          f"{'已写入' if apply else 'dry-run 未写入'}")
    if apply:
        # 复查
        left = 0
        for fname in os.listdir(units_dir):
            if fname.endswith(".md"):
                left += len(re.findall(r"第\s*[0-9]{1,4}\s*天",
                                       open(os.path.join(units_dir, fname), encoding="utf-8").read()))
        for fname in (os.listdir(actual_dir) if os.path.isdir(actual_dir) else []):
            if fname.endswith(".yaml"):
                left += len(re.findall(r"第\s*[0-9]{1,4}\s*天",
                                       open(os.path.join(actual_dir, fname), encoding="utf-8").read()))
        print(f"复查：剩余「第N天」{left} 处")
    return 0


if __name__ == "__main__":
    sys.exit(main())
