package domain

// 单章字数的唯一定义。
//
// 这条规则曾经被复述了七八遍（知识库三篇文章、新书模板、项目文档、细纲
// prompt、硬门阈值、分章判定、finalize 硬门、前端标签），任何一处改动都会
// 和别处打架——最典型的一次是细纲里写 word_target: 30000、单元 6 章，
// 硬门按 5000 字/章判，而知识库说单章 2000–3500。
//
// 所以现在：数字只在这里出现一次；提示词、硬门、UI、模板全部从这里取值。
// 要调标准，改这里的常量，别处不需要动。
const (
	ChapterCharsMin     = 2000
	ChapterCharsMax     = 3500
	ChapterCharsDefault = 2500
)

// ChapterCharsFloor 是单章正文的绝对下界（含）。1800 是发布端的退回线：
// 低于它这一章投出去就要重写，所以提示词和硬门都不能再按 0.6×目标放宽到 1500。
const ChapterCharsFloor = 1800

// 单个场面的字数区间（含两端）。场面数不是固定值：细纲给了几个场面就写几个，
// 没给就按内容切。每场低于 500 字说明这场没写透，高于 1200 字说明该切场了。
const (
	SceneCharsMin = 500
	SceneCharsMax = 1200
)

// ChapterBand 是硬门对单章正文的接受区间（含两端）。
//
// 目标值可调（细纲 chapter_chars），但接受区间永远收在 [ChapterCharsFloor,
// ChapterCharsMax] 内：下限写死 1800，避免目标调大后靠 0.6 系数把明显偏短的
// 章节放过去；上限写死 3500，避免 1.4× 系数把 3000 字的章放到 4200。
func ChapterBand(target int) (floor, ceil int) {
	if target <= 0 {
		target = ChapterCharsDefault
	}
	floor, ceil = target*6/10, target*14/10
	if floor < ChapterCharsFloor {
		floor = ChapterCharsFloor
	}
	if ceil > ChapterCharsMax {
		ceil = ChapterCharsMax
	}
	return floor, ceil
}

// ClampChapterChars 把作者或模型给的单章目标夹进合法区间。
// 0 或负数视为未声明，落到默认值。
func ClampChapterChars(n int) int {
	switch {
	case n <= 0:
		return ChapterCharsDefault
	case n < ChapterCharsMin:
		return ChapterCharsMin
	case n > ChapterCharsMax:
		return ChapterCharsMax
	}
	return n
}
