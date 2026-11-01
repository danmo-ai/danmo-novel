---
audience: author
---

# 知识来源索引

**作者文档，不进提示词。** 各篇方法论的出处、实验规模与改写依据集中在这里，正文只留规则本身。

## 去 AI 味

| 项目 | 规模 / 方法 | 被用在哪 |
|------|-------------|----------|
| [lieflat-less-ai-tone](https://github.com/larashero3-dotcom/lieflat-less-ai-tone) | 1169 篇 / 约 520 万字，两轮人机对照，100+ 指标 | 翻案腔为最强信号（δ=0.88）、结论提示冒号、成簇判定、勿过度去 AI |
| [chinese-de-ai-writing](https://github.com/tangwenwen-md/chinese-de-ai-writing) | HC3-Chinese 12078 篇人类基线，实测项误报率 3.4% | A/B/C 三级分级、空转句引列表（Claude 稿 85% 命中） |
| [chinese-webnovel-skills/deslop](https://github.com/tance-mang/chinese-webnovel-skills) | 网文专项经验清单 | 排比癖、喊情绪、翻译腔、极端词、句号当顿号 |
| [humanizer-chinese](https://github.com/jiji262/humanizer-chinese) | blader/humanizer 中文本土化，36 条模式 + CCL 2023 | 宏大开场、讲义腔、万能动词、句尾升华、聚簇判定 |
| [nabokov](https://github.com/viewflow/nabokov) / [slop-lint](https://github.com/eric-sabe/slop-lint) | per-rule 开关 + em-dash 门禁 | 规则按 id 可开关的工程形态（见 `gates/deslop_rules.yaml`） |

**没有搬进来的**：调句长、删设问、删比喻、按词表替换、加错别字——实测证伪或反向。

## 刑侦人味文风

松本清张（动机社会性）、紫金陈（高能开篇 / 产品思维）、班宇与《漫长的季节》（闲笔与细节库）、
Elmore Leonard（对话纪律 / 删跳读）、CSI 编剧谈（story king + 程序取舍）、网文章末钩参数。
专业材料与请教取舍对照番茄课堂悬疑刑侦专访纪律提炼，不贴案例。

## 网文技法

- 品类拆解｜玄幻作品创作宝典；大神答疑｜传统玄幻遇冷
- 质感提升｜告别重复表达：学会给文字做「减法」
- 质感提升｜告别「出戏感」：写出让读者上头的质感网文
- StoryScope / sepia：叙事特征检测（本篇只借结论，不照搬协议）
- Lorn / chinese-webnovel-skills：OS 拐杖与语域纪律

> 人类语料组间差异极大（破折号极差百倍），上表频率是特定时点的观测，不是恒定阈值。
> 比喻贴切度、案例是否凑数、参差的质量，机器测不了。
