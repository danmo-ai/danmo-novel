package domain

// 单章字数的唯一定义。
//
// 这条规则曾经被复述了七八遍（知识库三篇文章、新书模板、项目文档、细纲
// prompt、硬门阈值、分章判定、finalize 硬门、前端标签），任何一处改动都会
// 和别处打架——最典型的一次是细纲里写 word_target: 30000、单元 6 章，
// 硬门按 5000 字/章判，而知识库说单章 2000–3500。
//
// 所以现在：数字只在这里出现一次；提示词、硬门、UI、模板全部从这里取值。
// 要调标准，改这三个常量，别处不需要动。
const (
	ChapterCharsMin     = 2000
	ChapterCharsMax     = 3500
	ChapterCharsDefault = 2500
)

// ChapterBand 是硬门对单章正文的接受区间（含两端）。
//
// 下限 0.6×目标沿用旧口径，避免已有书突然变严；上限是新补的——以前只判
// 下限，写多长都算通过。
func ChapterBand(target int) (floor, ceil int) {
	if target <= 0 {
		target = ChapterCharsDefault
	}
	return target * 6 / 10, target * 14 / 10
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
