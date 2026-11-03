package books

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

// DraftInput 是新书立项表单。它是作者意图的第一手记录，不要求结构化：
// 空白字段保持空白，渲染成模板时不会伪造已确定的事实。
type DraftInput struct {
	BookID string `json:"bookId"`
	Title  string `json:"title"`
	Genre  string `json:"genre"`
	// Subgenre 可选；平台/频道、目标读者、篇幅与单章目标字数属于生产约束。
	Subgenre     string `json:"subgenre"`
	Platform     string `json:"platform"`
	Audience     string `json:"audience"`
	Length       string `json:"length"`
	ChapterChars string `json:"chapterChars"`
	Update       string `json:"update"`
	// 创意种子：一句话 idea 与它周边最容易决定方向的选择。
	Idea        string `json:"idea"`
	Tone        string `json:"tone"`
	Pleasure    string `json:"pleasure"`
	Protagonist string `json:"protagonist"`
	Conflict    string `json:"conflict"`
	Ending      string `json:"ending"`
	Taboos      string `json:"taboos"`
	References  string `json:"references"`
	POV         string `json:"pov"`
}

// Normalize 补齐必填项并去空白；CreateBookFromDraft 依赖它保证文件名合法。
func (in DraftInput) Normalize() DraftInput {
	in.BookID = slugify(in.BookID)
	in.Title = strings.TrimSpace(in.Title)
	in.Genre = strings.TrimSpace(in.Genre)
	if in.Title == "" {
		in.Title = in.BookID
	}
	return in
}

// slugify 生成合法 book id：小写字母数字与连字符。
func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastDash := false
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || r == ' ' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

// IdeaMarkdown 渲染立项工作台文件。
//
// 结构固定为「立项输入 / 一句话 idea / 创意提案」三段：前两段由表单生成、
// 由作者维护；第三段由 TaskIdea 生成并整段替换。固定标题让 pipeline 能只
// 替换提案段而不碰作者输入。
func IdeaMarkdown(in DraftInput) string {
	line := func(label, value string) string {
		return fmt.Sprintf("- %s：%s\n", label, strings.TrimSpace(value))
	}
	idea := strings.TrimSpace(in.Idea)
	var b strings.Builder
	fmt.Fprintf(&b, "# 立项 · %s\n\n", in.Title)
	b.WriteString("## 立项输入\n")
	b.WriteString(line("题材 / 子题材", joinNonEmpty(" / ", in.Genre, in.Subgenre)))
	b.WriteString(line("平台 / 频道", in.Platform))
	b.WriteString(line("目标读者", in.Audience))
	b.WriteString(line("预计篇幅 / 更新", joinNonEmpty(" · ", in.Length, in.Update)))
	b.WriteString(line("单章目标字数", in.ChapterChars))
	b.WriteString(line("基调", in.Tone))
	b.WriteString(line("核心爽点 / 读者承诺", in.Pleasure))
	b.WriteString(line("主角种子", in.Protagonist))
	b.WriteString(line("主要对抗", in.Conflict))
	b.WriteString(line("结局倾向", in.Ending))
	b.WriteString(line("禁忌 / 红线", in.Taboos))
	b.WriteString(line("参考作品", in.References))
	b.WriteString(line("POV", in.POV))
	b.WriteString("\n## 一句话 idea\n")
	if idea == "" {
		b.WriteString("> \n")
	} else {
		fmt.Fprintf(&b, "> %s\n", idea)
	}
	b.WriteString("\n## 创意提案\n")
	b.WriteString("<!-- 由「创意扩写」生成；作者可直接编辑 -->\n")
	return b.String()
}

// IdeaProposalSection 是提案段的固定标题，pipeline 按它整段替换。
const IdeaProposalSection = "## 创意提案"

// HasIdeaProposal 判断 author 是否已有一条确认过的创意提案。
// 「有标题但正文为空」不算——那正是模板的初始状态。
func HasIdeaProposal(snap domain.BookSnapshot) bool {
	return strings.TrimSpace(ideaProposalBody(snap.Files[domain.IdeaPath])) != ""
}

// ideaProposalBody 取提案标题之后的正文；没有该标题时返回空。
func ideaProposalBody(raw string) string {
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == IdeaProposalSection {
			start = i + 1
			break
		}
	}
	if start < 0 {
		return ""
	}
	body := strings.Join(lines[start:], "\n")
	// 去掉纯注释占位符，避免模板本身被误判为「已有提案」。
	body = strings.ReplaceAll(body, "<!-- 由「创意扩写」生成；作者可直接编辑 -->", "")
	return strings.TrimSpace(body)
}

// MergeIdeaProposal 把模型输出写进提案段，保留「立项输入 / 一句话 idea」原样。
// raw 为空（作者直接从零写提案）时返回标题 + 正文。
func MergeIdeaProposal(raw, proposal string) string {
	proposal = strings.TrimSpace(proposal)
	lines := strings.Split(strings.ReplaceAll(raw, "\r\n", "\n"), "\n")
	start := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == IdeaProposalSection {
			start = i + 1
			break
		}
	}
	if start < 0 {
		if strings.TrimSpace(raw) == "" {
			return IdeaProposalSection + "\n\n" + proposal + "\n"
		}
		return strings.TrimRight(raw, "\n") + "\n\n" + IdeaProposalSection + "\n\n" + proposal + "\n"
	}
	head := strings.Join(lines[:start], "\n")
	out := strings.TrimRight(head, "\n") + "\n\n" + proposal + "\n"
	return out
}

func joinNonEmpty(sep string, parts ...string) string {
	kept := make([]string, 0, len(parts))
	for _, v := range parts {
		if s := strings.TrimSpace(v); s != "" {
			kept = append(kept, s)
		}
	}
	return strings.Join(kept, sep)
}

// CreateBookFromDraft 只建立「能做立项」的最小树。
//
// 不再铺满 9 个空设定模板：那些占位符会被结构化立项的上下文当成既有 canon，
// 模型于是「优化」空模板而不是从创意提案生成。其余设定文件在立项接受时
// 由候选一次性写入。
func CreateBookFromDraft(libraryRoot string, in DraftInput) (string, error) {
	in = in.Normalize()
	if in.BookID == "" {
		return "", fmt.Errorf("bookId required")
	}
	root := filepath.Join(libraryRoot, "novel", in.BookID)
	if _, err := os.Stat(root); err == nil {
		return "", fmt.Errorf("book already exists: %s", in.BookID)
	}
	dirs := []string{
		root,
		filepath.Join(root, "canon", "cast"),
		filepath.Join(root, "outline", "volumes"),
		filepath.Join(root, "outline", "units"),
		filepath.Join(root, "units"),
		filepath.Join(root, "continuity", "summaries"),
		filepath.Join(root, "continuity", "commits"),
		filepath.Join(root, "reviews"),
	}
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	state := fmt.Sprintf("title: %q\ngenre: %q\nsubgenre: %q\nstage: %s\nlast_committed_ch: 0\nnext_action: idea\nactive_unit: \"\"\ntime_system: story_day\nqc_profile: general\ncontinuation_mode: false\ngates:\n  knowledge: unknown\n  asset: unknown\n  qc: unknown\nblockers: []\n",
		in.Title, in.Genre, in.Subgenre, domain.StageIdea)
	if err := os.WriteFile(filepath.Join(root, "novel-state.yaml"), []byte(state), 0o644); err != nil {
		return "", err
	}
	files := map[string]string{
		"book-bible.md":                 BookBibleFromDraft(in),
		"canon/platform-positioning.md": PlatformFromDraft(in),
		domain.IdeaPath:                 IdeaMarkdown(in),
	}
	for rel, body := range files {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return "", err
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			return "", err
		}
	}
	return root, nil
}

// BookBibleFromDraft 按表单预填作者意图；空白项保持空白，避免伪造事实。
func BookBibleFromDraft(in DraftInput) string {
	quote := func(s string) string {
		s = strings.TrimSpace(s)
		if s == "" {
			return "> "
		}
		return "> " + s
	}
	dash := func(label, value string) string {
		return fmt.Sprintf("- %s: %s\n", label, strings.TrimSpace(value))
	}
	var b strings.Builder
	fmt.Fprintf(&b, "# Book bible — %s\n\n## Reader promise\n", in.Title)
	b.WriteString(dash("Genre / channel", joinNonEmpty(" / ", in.Genre, in.Subgenre)))
	b.WriteString(dash("Core pleasure", in.Pleasure))
	b.WriteString(dash("Tone", in.Tone))
	b.WriteString("\n## One-line logline\n")
	b.WriteString(quote(in.Idea) + "\n")
	b.WriteString("\n## Protagonist\n")
	b.WriteString(dash("Desire", ""))
	b.WriteString(dash("Wound", ""))
	b.WriteString(dash("Method", ""))
	b.WriteString(dash("Lines they won't cross", ""))
	fmt.Fprintf(&b, "\n## Antagonistic force\n%s\n", strings.TrimSpace(in.Conflict))
	b.WriteString("\n## Story engine\nHow conflict renews each arc:\n")
	b.WriteString("\n## Ending direction (spoiler-safe for author)\n")
	if ending := strings.TrimSpace(in.Ending); ending != "" {
		b.WriteString(ending + "\n")
	}
	b.WriteString("\n## 终局储备（约束，不得提前打光）\n")
	b.WriteString("| 底牌 | 内容 | 最早解锁卷 |\n|------|------|-----------|\n| 头号宿敌 | | v |\n| 终极真相 / 金手指上限 | | v |\n| 升级台阶 | 共 档 | 每档约 卷 |\n")
	fmt.Fprintf(&b, "\n## Taboos / constraints\n%s\n", strings.TrimSpace(in.Taboos))
	b.WriteString("\n## Style card\n")
	b.WriteString(dash("POV", in.POV))
	b.WriteString(dash("Voice notes", ""))
	b.WriteString(dash("Anti-patterns to avoid", ""))
	return b.String()
}

// PlatformFromDraft 按表单预填平台定位，后续可独立重写。
func PlatformFromDraft(in DraftInput) string {
	dash := func(label, value string) string {
		return fmt.Sprintf("- %s：%s\n", label, strings.TrimSpace(value))
	}
	var b strings.Builder
	b.WriteString("# Platform positioning\n\n")
	b.WriteString(dash("平台 / 频道", in.Platform))
	b.WriteString(dash("目标读者", in.Audience))
	b.WriteString(dash("篇幅 / 更新", joinNonEmpty(" · ", in.Length, in.Update)))
	b.WriteString(dash("单章目标字数", in.ChapterChars))
	b.WriteString(dash("开篇承诺", ""))
	b.WriteString(dash("内容红线", in.Taboos))
	if refs := strings.TrimSpace(in.References); refs != "" {
		fmt.Fprintf(&b, "\n## 参考作品\n%s\n", refs)
	}
	return b.String()
}
