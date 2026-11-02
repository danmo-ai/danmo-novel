package contextsvc

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/service/books"
)

func TestOutlinePacketIncludesSeedAndExistingCanon(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, OutlinePath: "outline/units/v01-U1.yaml", OutlineRaw: "unit_id: v01-U1\nstatus: proposed\non_stage: [lin]\npov: lin\nstory_day: 3\ngap_from_prev: 次日\n"}
	volumeRaw := "# 卷纲 · v01\n- schema_version: 1\n- volume_id: v01\n\n## 单元 · v01-U1\n- title: 入门\n- chapter_range: 1-3\n- objective: 立足\n- obstacle: 质疑\n- attempt: 复查\n- setback: 证据不足\n- choice: 承担复核\n- cost: 责任\n- outcome: 获得许可\n- entry_cause: 开卷\n- exit_result: 获得许可\n- state_changes: lin.role_title=新人->复核人\n- thread_ids: main\n- event_ids: event-1\n- next_hook: 信息缺口 | 签名不合\n"
	doc := books.ParseStructuredMarkdown(volumeRaw, books.VolumeOutlineProtocol)
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b"}, Volumes: []domain.VolumeInfo{{Volume: 1, Raw: volumeRaw, Cast: []string{"lin"}, Structured: &doc}}, Units: []domain.UnitInfo{unit}, Cast: []domain.CastCard{{Stem: "lin", Name: "林晚", Raw: "# 林晚\n"}}, Files: map[string]string{"outline/volumes/v01.md": volumeRaw, "canon/reveal-schedule.md": "# 揭示计划\n- schema_version: 1\n\n## 揭示 · R1\n- public_summary: 可公开线索\n- earliest_volume: v01\n- hint_unit_ids: v01-U1\n- confirm_unit_id: v01-U1\n- status: planted\n- locked_terms: 无\n"}}
	packet := BuildOutlinePacket(snap, 1, []string{"v01-U1"}, "", 0)
	for _, want := range []string{"不可改写的单元种子", "objective", "state_changes", "人物·林晚", "story_day", "可公开线索"} {
		if !strings.Contains(packet.UserPrompt, want) {
			t.Fatalf("outline packet lost %q", want)
		}
	}
}

func TestStructuredCanonRetainsIdentityClockAndReveal(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v02-U1", Volume: 2, ChapterFrom: 4, ChapterTo: 4, OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md", ProseRaw: "正文", OutlineRaw: "unit_id: v02-U1\nsource_seed_id: v02-U1\ndesire: 立足\nobstacle: 质疑\nchoice: 复查\npayoff: 获得许可\nforbidden: 禁揭\non_stage: [lin]\npov: lin\nstory_day: 12\ngap_from_prev: 三天后\nscenes:\n  - id: S1\n    chapter: 4\n    goal: 复查\n"}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b", Genre: "悬疑"}, Cast: []domain.CastCard{{Stem: "lin", Name: "林晚", Raw: "# 林晚\n\n## 三锚点\n- 视觉：先看手\n\n## 知识边界\n- 不知未来\n\n## 台词\n- 先复核。\n"}}, IdentitySegments: []domain.IdentitySegment{{Stem: "lin", RoleTitle: "重案队试用", Org: "分局", FromUnit: "v02-U1"}}, Files: map[string]string{
		"canon/reveal-schedule.md": "# 揭示计划\n- schema_version: 1\n\n## 揭示 · R-ORIGIN\n- public_summary: 旧刀来源\n- earliest_volume: v03\n- hint_unit_ids: v03-U1\n- confirm_unit_id: v03-U2\n- status: locked\n- locked_terms: 原主\n",
		"canon/author-lore.md":     "未来真相禁止注入",
	}}
	for name, packet := range map[string]domain.ContextPacket{"write": BuildWritePacket(snap, unit, "", 0), "review": BuildReviewPacket(snap, unit, nil, 0, "")} {
		if !strings.Contains(packet.UserPrompt, "重案队试用") || !strings.Contains(packet.UserPrompt, "先看手") {
			t.Fatalf("%s lost cast/identity: %s", name, packet.UserPrompt)
		}
		if !strings.Contains(packet.UserPrompt, "旧刀来源") || !strings.Contains(packet.UserPrompt, "禁止写穿") {
			t.Fatalf("%s lost structured reveal: %s", name, packet.UserPrompt)
		}
		if strings.Contains(packet.UserPrompt, "未来真相禁止注入") {
			t.Fatalf("%s leaked author lore", name)
		}
	}
	write := BuildWritePacket(snap, unit, "", 0)
	if !strings.Contains(write.UserPrompt, "第 12 天") || !strings.Contains(write.UserPrompt, "三天后") {
		t.Fatal("story clock guidance was dropped")
	}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	revise := BuildReviseChapterPacket(snap, unit, domain.ChapterDraft{Chapter: 4, Body: "正文"}, nil, "", baseline, 0)
	if !strings.Contains(revise.UserPrompt, "重案队试用") || !strings.Contains(revise.UserPrompt, "旧刀来源") {
		t.Fatal("revise lost identity or reveal constraints")
	}
}

// 补丁包与兜底包必须看到完全一样的依据，只差 instruction 里的输出契约。
//
// 如果两者上下文不同，"补丁失败后兜底"写出来的正文会因为少了某块约束而与
// 前一次不同，作者面对两个互斥结果无从判断。system 也必须相同——换了 system
// 就等于整份正文重新计费，而兜底恰恰发生在已经失败两次之后。
func TestRevisePacketsDifferOnlyInOutputContract(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v02-U1", Volume: 2, ChapterFrom: 4, ChapterTo: 4,
		OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md",
		OutlineRaw: "unit_id: v02-U1\ndesire: 立足\nobstacle: 质疑\nchoice: 复查\npayoff: 获得许可\non_stage: [lin]\npov: lin\nstory_day: 12\n",
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b", Genre: "悬疑"},
		Cast:    []domain.CastCard{{Stem: "lin", Name: "林晚", Raw: "# 林晚\n\n## 三锚点\n- 视觉：先看手\n"}},
		Files:   map[string]string{"canon/writing-rules.md": "少用破折号"},
	}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	ch := domain.ChapterDraft{Chapter: 4, Title: "门", Body: "第4章正文"}

	patch := BuildReviseChapterPacket(snap, unit, ch, nil, "", baseline, 0)
	fallback := BuildReviseChapterFallbackPacket(snap, unit, ch, nil, "", baseline, 0)

	if len(patch.Blocks) != len(fallback.Blocks) {
		t.Fatalf("block count differs: patch=%d fallback=%d", len(patch.Blocks), len(fallback.Blocks))
	}
	for i := range patch.Blocks {
		if patch.Blocks[i].ID != fallback.Blocks[i].ID || patch.Blocks[i].Content != fallback.Blocks[i].Content {
			t.Fatalf("block %d differs between patch and fallback: %q vs %q",
				i, patch.Blocks[i].ID, fallback.Blocks[i].ID)
		}
	}
	// system 必须逐字相同：兜底轮换的只是 instruction 里的契约，不是整个提示词。
	if patch.SystemPrompt != fallback.SystemPrompt {
		t.Fatal("patch and fallback must share one system prompt (prefix cache)")
	}
	if !strings.Contains(patch.UserPrompt, "edits") || !strings.Contains(patch.UserPrompt, "find") {
		t.Fatal("patch contract must describe the edits/find format")
	}
	if !strings.Contains(patch.UserPrompt, "本章内") {
		t.Fatal("patch contract must state that uniqueness holds inside this chapter")
	}
	// payload 是 JSON，契约里的引号会被转义，所以用不会被转义的中文片段判类型。
	if !strings.Contains(fallback.UserPrompt, "修订后的完整正文") {
		t.Fatal("fallback contract must ask for the whole chapter body")
	}
	if strings.Contains(patch.UserPrompt, "修订后的完整正文") {
		t.Fatal("patch contract must not ask for the whole chapter body")
	}
	if patch.UserPrompt == fallback.UserPrompt {
		t.Fatal("patch and fallback must differ in their output contract")
	}
	// 每章 3 次：补丁、补丁修复、整章兜底。
	for name, p := range map[string]domain.ContextPacket{"patch": patch, "fallback": fallback} {
		if p.Policy.MaxCalls != 3 {
			t.Fatalf("%s maxCalls=%d want 3", name, p.Policy.MaxCalls)
		}
	}
	// 本章正文是锚点定位的唯一依据，两块包都必须带。
	if !strings.Contains(patch.UserPrompt, "第4章正文") {
		t.Fatal("patch packet lost the chapter body")
	}
}

// 审校包与修订包的前缀必须逐字相同：这是勾选后那次修订复用缓存的前提。
//
// 缓存只认最长公共前缀，而 system 是 Messages[0]、blocks 紧随其后。两者任何
// 一处不同（system 措辞、块顺序、块内容），后面数万字的共享正文就全部重新计费。
func TestChapterReviewAndReviseShareTheSamePrefix(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v02-U1", Volume: 2, ChapterFrom: 4, ChapterTo: 4,
		OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md",
		OutlineRaw: "unit_id: v02-U1\ndesire: 立足\nobstacle: 质疑\nchoice: 复查\npayoff: 获得许可\non_stage: [lin]\npov: lin\nstory_day: 12\n",
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b", Genre: "悬疑"},
		Cast:    []domain.CastCard{{Stem: "lin", Name: "林晚", Raw: "# 林晚\n\n## 三锚点\n- 视觉：先看手\n"}},
		Files:   map[string]string{"canon/writing-rules.md": "少用破折号"},
	}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	ch := domain.ChapterDraft{Chapter: 4, Title: "门", Body: "第4章正文"}
	prevTail := "上一章末尾的一段。"

	review := BuildChapterReviewPacket(snap, unit, ch, prevTail, baseline, 0)
	revise := BuildReviseChapterPacket(snap, unit, ch, nil, prevTail, baseline, 0)

	if review.SystemPrompt != revise.SystemPrompt {
		t.Fatal("review and revise must share one system prompt; otherwise the whole payload re-bills")
	}
	if len(review.Blocks) != len(revise.Blocks) {
		t.Fatalf("block count differs: review=%d revise=%d", len(review.Blocks), len(revise.Blocks))
	}
	for i := range review.Blocks {
		if review.Blocks[i].ID != revise.Blocks[i].ID || review.Blocks[i].Content != revise.Blocks[i].Content {
			t.Fatalf("block %d differs (%q vs %q); prefix cache would miss",
				i, review.Blocks[i].ID, revise.Blocks[i].ID)
		}
	}
	// 正文必须是同一块且内容相同——它占 prompt 的绝大部分。
	var reviewProse, reviseProse string
	for _, b := range review.Blocks {
		if b.ID == "prose" {
			reviewProse = b.Content
		}
	}
	for _, b := range revise.Blocks {
		if b.ID == "prose" {
			reviseProse = b.Content
		}
	}
	if reviewProse == "" || reviewProse != reviseProse {
		t.Fatalf("prose block must be identical: review=%q revise=%q", reviewProse, reviseProse)
	}
}

// 同一单元相邻两章必须共享绝大部分请求字节。
//
// 这是前缀缓存在分章审校上的唯一意义：卡在一起的两章只有 unit-beats 与 prose
// 不同，其余 9 个基线块（审校清单 2600 字、揭示约束 1100 字、在场人物 800 字…）
// 完全相同。曾经 unit-beats 排第 2 块，于是相邻两章的公共前缀只有 19 字节
// （0.1%），基线每章按全价重算。
//
// 这里用真实字节比对，而不是看块是否「存在」：缓存认的是序列化后的最长公共前缀。
func TestAdjacentChapterPacketsShareMostOfTheirBytes(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v02-U1", Volume: 2, ChapterFrom: 4, ChapterTo: 5,
		OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md",
		OutlineRaw: "unit_id: v02-U1\ndesire: 立足\nobstacle: 质疑\nchoice: 复查\npayoff: 获得许可\non_stage: [lin]\npov: lin\nstory_day: 12\n",
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b", Genre: "悬疑"},
		Cast:    []domain.CastCard{{Stem: "lin", Name: "林晚", Raw: "# 林晚\n\n## 三锚点\n- 视觉：先看手\n"}},
		Files: map[string]string{
			"canon/writing-rules.md":      "少用破折号",
			"canon/reveal-schedule.md":    "# 揭示\n- public: 旧刀来源\n",
			"continuity/summaries/v02.md": "# 摘要\n| 章 | 事件 |\n|---|---|\n| 1 | 甲 |\n",
		},
	}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	ch4 := domain.ChapterDraft{Chapter: 4, Title: "门", Body: "第四章的正文内容，写得足够长以便占比明显。"}
	ch5 := domain.ChapterDraft{Chapter: 5, Title: "影", Body: "第五章的正文内容，与第四章完全不同。"}

	p4 := BuildChapterReviewPacket(snap, unit, ch4, baseline.PrevTail, baseline, 0)
	p5 := BuildChapterReviewPacket(snap, unit, ch5, ch4.Body, baseline, 0)

	// 章号不得出现在 payload 的最前面：book 是第一个字段，章号放进去会让
	// 相邻两章从第 19 字节就分叉。
	if a, b := []byte(p4.UserPrompt), []byte(p5.UserPrompt); commonPrefixLen(a, b) < 12 {
		t.Fatalf("adjacent chapters diverge at byte %d; chapter number leaked into book",
			commonPrefixLen(a, b))
	}

	a, b := []byte(p4.UserPrompt), []byte(p5.UserPrompt)
	n := commonPrefixLen(a, b)
	// 共享前缀至少要覆盖除「上一章末 + 场面节拍 + 正文」之外的所有基线。
	if ratio := float64(n) / float64(len(a)); ratio < 0.35 {
		t.Fatalf("shared prefix only %.1f%% of the payload (%d/%d bytes); baseline is re-billed per chapter",
			ratio*100, n, len(a))
	}
	// 每章不同块必须排在最后。
	if lastTwo := p4.Blocks[len(p4.Blocks)-2].ID + "," + p4.Blocks[len(p4.Blocks)-1].ID; lastTwo != "unit-beats,prose" {
		t.Fatalf("per-chapter blocks must be last, got %q", lastTwo)
	}
}

func commonPrefixLen(a, b []byte) int {
	n := len(a)
	if len(b) < n {
		n = len(b)
	}
	i := 0
	for i < n && a[i] == b[i] {
		i++
	}
	return i
}

// 每章只带本章正文 + 上一章末；全局基线共享但不含整份卷摘要。
// 卷摘要可达数万字，整份塞进首章会让那一次调用比别的章贵三倍。
func TestChapterReviewPacketClipsSummaryAndScopesProse(t *testing.T) {
	summary := strings.Repeat("卷摘要行。", 20000) // 远超任何合理上下文预算
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b1", Genre: "悬疑"},
		Files: map[string]string{
			"continuity/summaries/v02.md": summary,
		},
	}
	unit := domain.UnitInfo{
		UnitID: "v02-U1", Volume: 2, ChapterFrom: 1, ChapterTo: 3,
		OutlinePath: "outline/units/v02-U1.yaml",
		ProsePath:   "units/v02-U1.md",
		OutlineRaw:  "unitId: v02-U1\nword_target: 9000\n",
	}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	if len([]rune(baseline.PrevSummary)) > 1300 {
		t.Fatalf("prev summary not clipped: %d runes", len([]rune(baseline.PrevSummary)))
	}

	ch := domain.ChapterDraft{Chapter: 2, Title: "门", Body: "第2章正文"}
	packet := BuildChapterReviewPacket(snap, unit, ch, "上一章末尾", baseline, 128000)

	var prose, prevTail, contract string
	for _, b := range packet.Blocks {
		switch b.ID {
		case "prose":
			prose = b.Content
		case "prev-tail":
			prevTail = b.Content
		case "unit-contract":
			contract = b.Content
		}
		if b.ID == "prev-summary" {
			t.Fatal("non-first chapter must not carry the unit summary")
		}
	}
	if prose != ch.Body {
		t.Fatalf("prose block=%q want only this chapter", prose)
	}
	if prevTail != "上一章末尾" {
		t.Fatalf("prev tail=%q", prevTail)
	}
	if contract == "" {
		t.Fatal("unit contract missing from baseline")
	}
	// 单章审校不再由上层定上限：maxOutputTokens=0 表示交给 client 按模型配置
	// 决定。曾经这里必须给到 32768，因为思考链与答案共用那份预算，思维链开销
	// 由正文长度与 reasoning effort 决定，和 findings 条数无关。
	if packet.Policy.MaxOutputTokens != 0 {
		t.Fatalf("per-chapter budget=%d, want 0 (defer to model config)", packet.Policy.MaxOutputTokens)
	}
}

// 按章审校时上下文只带本章的本地硬门结论。整单元的塞进每一次单章调用，模型会
// 照着重报别章的问题（章号对不上→多烧一次修复重试），同一问题也会在候选里
// 出现两遍（一次 local_gate，一次 ai_review）。
func TestChapterReviewPacketCarriesOnlyItsOwnLocalFindings(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U1.yaml", ProsePath: "units/v01-U1.md",
		OutlineRaw: "unitId: v01-U1\nword_target: 5000\n",
	}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}
	local := []domain.ReviewFinding{
		{ID: "length-1", Severity: "advisory", Chapter: 1, Title: "第1章字数不足", Evidence: "1000 字"},
		{ID: "length-2", Severity: "advisory", Chapter: 2, Title: "第2章字数不足", Evidence: "900 字"},
		{ID: "unit-level", Severity: "advisory", Chapter: 0, Title: "整单元问题", Evidence: "无"},
	}
	baseline := BuildReviewBaseline(snap, unit, local, "")

	ch1 := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 1, Body: "一"}, "", baseline, 0)
	ch2 := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 2, Body: "二"}, "", baseline, 0)
	gate := func(p domain.ContextPacket) string {
		raw, _ := DecodeUserPayload(p.UserPrompt)
		s, _ := raw.Extra["localFindings"].(string)
		return s
	}

	g1, g2 := gate(ch1), gate(ch2)
	if !strings.Contains(g1, "第1章字数不足") || strings.Contains(g1, "第2章字数不足") {
		t.Fatalf("chapter 1 packet has the wrong local findings:\n%s", g1)
	}
	if !strings.Contains(g2, "第2章字数不足") || strings.Contains(g2, "第1章字数不足") {
		t.Fatalf("chapter 2 packet has the wrong local findings:\n%s", g2)
	}
	// chapter=0 是全单元级结论，每章都该带上，否则会彻底从上下文里消失。
	if !strings.Contains(g1, "整单元问题") || !strings.Contains(g2, "整单元问题") {
		t.Fatalf("unit-level finding must reach every chapter:\nch1=%q\nch2=%q", g1, g2)
	}
}

// 分章与整单元两条审校路径必须共享同一套依据：作者的开关只该改变粒度。
// 这里把两者放在同一份快照上比较——风格、锁词、在场关系、揭示、题材约束
// 缺一不可，否则切到整单元审校后 POV/锁词/文风类意见会静默消失。
func TestReviewPathsCarryTheSameBaseline(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v02-U1", Volume: 2, ChapterFrom: 4, ChapterTo: 5,
		OutlinePath: "outline/units/v02-U1.yaml", ProsePath: "units/v02-U1.md",
		OutlineRaw: "unit_id: v02-U1\n" +
			"desire: 立足\nobstacle: 质疑\nchoice: 复查\npayoff: 获得许可\n" +
			"forbidden: 禁揭来源\npleasure: 弱者翻盘\n" +
			"continuity_risks: 作者台账：第三场容易写穿\n" +
			"observations: 历轮观察：上次节奏偏平\n" +
			"on_stage: [lin]\npov: lin\n" +
			"scenes:\n  - id: S1\n    chapter: 4\n    goal: 复查\n  - id: S2\n    chapter: 5\n    goal: 收尾\n",
		ProseRaw: "## 第4章 复查\n\n正文四\n\n## 第5章 收尾\n\n正文五",
	}
	snap := domain.BookSnapshot{
		Summary: domain.BookSummary{ID: "b1", Genre: "悬疑"},
		Cast:    []domain.CastCard{{Stem: "lin", Name: "林晚", Raw: "# 林晚\n\n## 三锚点\n- 视觉：先看手\n\n## 知识边界\n- 不知未来\n\n## 台词\n- 先复核。\n"}},
		Files: map[string]string{
			"canon/style-fingerprint.md":  "# 文风\n\n- 短句为主，不用排比。\n",
			"canon/writing-rules.md":      "- 不写心理总结。\n",
			"canon/locked-terms.yaml":     "- term: 复核\n  note: 固定用词\n",
			"continuity/summaries/v02.md": "第二卷摘要：林晚已调入重案队。",
		},
	}

	whole := BuildReviewPacket(snap, unit, nil, 0, "")
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	narrow := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 4, Body: "正文四"}, "", baseline, 0)

	// 两条路径都必须带上的依据。
	for _, want := range []string{"先看手", "短句为主", "当前卷锁词", "已调入重案队"} {
		if !strings.Contains(whole.UserPrompt, want) {
			t.Errorf("whole-unit review lost %q", want)
		}
		if !strings.Contains(narrow.UserPrompt, want) {
			t.Errorf("per-chapter review lost %q", want)
		}
	}

	// 作者台账不该进审校提示词：它们不是写作要求，进来会被当成规则乱套。
	for _, leaked := range []string{"第三场容易写穿", "上次节奏偏平"} {
		if strings.Contains(whole.UserPrompt, leaked) {
			t.Errorf("whole-unit review leaked author ledger %q", leaked)
		}
	}

	// 整单元路径没有「本章场面节拍」切片，所以合同必须保留 scenes，
	// 否则模型整单元审校时看不到任何章级节拍。
	if !strings.Contains(whole.UserPrompt, "goal: 复查") {
		t.Error("whole-unit review lost scenes; it has no per-chapter beats to fall back on")
	}
	// 分章路径不能出现两遍节拍：合同里剥掉 + 单独的本章节拍块。
	beats := 0
	for _, b := range narrow.Blocks {
		if b.ID == "unit-beats" || b.ID == "unit-contract" {
			if strings.Contains(b.Content, "goal: 复查") {
				beats++
			}
		}
	}
	if beats != 1 {
		t.Errorf("chapter beats appear %d times, want exactly 1", beats)
	}
}

// 审校清单来源优先级：本书文件 > 作者库编辑 > 内置文章。
func TestReviewBaselineCarriesChecklistAndResolvesSource(t *testing.T) {
	unit := domain.UnitInfo{UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1, OutlineRaw: "unitId: v01-U1\n"}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}

	builtin := BuildReviewBaseline(snap, unit, nil, "")
	if !strings.Contains(builtin.Rules, "审校规则") || builtin.RulesSource != RulesFromBuiltin {
		t.Fatalf("built-in checklist missing: source=%q", builtin.RulesSource)
	}

	// 作者在知识库页编辑过 → 编辑内容必须生效（以前被内置副本遮蔽）。
	edited := "# 我的审校规则\n- 只查连续性和 POV"
	fromLib := BuildReviewBaseline(snap, unit, nil, edited)
	if fromLib.Rules != edited || fromLib.RulesSource != RulesFromLibrary {
		t.Fatalf("library edit should win over builtin: source=%q", fromLib.RulesSource)
	}

	// 本书专属文件优先级最高。
	bookRules := "# 本书审校规则\n- 只允许 1 条 blocking"
	snap.Files["canon/review-checklist.md"] = bookRules
	fromBook := BuildReviewBaseline(snap, unit, nil, edited)
	if fromBook.Rules != bookRules || fromBook.RulesSource != RulesFromBook {
		t.Fatalf("book override should win, got source=%q", fromBook.RulesSource)
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// 注入给模型的清单不能带 YAML front matter：`stages:` / `default:` 是应用元数据，
// 对模型是纯噪音，也会让它以为自己在读配置文件。
func TestReviewRulesBlockHasNoFrontMatter(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 1,
		OutlinePath: "outline/units/v01-U1.yaml", ProsePath: "units/v01-U1.md",
		OutlineRaw: "unitId: v01-U1\n",
	}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}
	baseline := BuildReviewBaseline(snap, unit, nil, "")
	if baseline.RulesSource != RulesFromBuiltin {
		t.Fatalf("source=%q", baseline.RulesSource)
	}
	if strings.HasPrefix(strings.TrimSpace(baseline.Rules), "---") {
		t.Fatalf("front matter leaked into the prompt:\n%s", baseline.Rules[:40])
	}
	for _, bad := range []string{"stages:", "default: true", "audience:"} {
		if strings.Contains(baseline.Rules, bad) {
			t.Errorf("metadata %q leaked into the rules block", bad)
		}
	}
	if !strings.Contains(baseline.Rules, "审校规则") {
		t.Fatal("checklist body missing")
	}
}

// scenes 曾经被注入两遍：整份 YAML 合同里一份，「场面节拍」块又一份。
// 实测占写作提示词 35%（70KB → 45KB），必须保证合同里不再含 scenes。
func TestContractWithoutBeatsRemovesScenes(t *testing.T) {
	raw := "unit_id: v01-U1\ndesire: 站住脚\nscenes:\n  id: S1\n  chapter: 1\n  beat: 入场\nnext_hook:\n  out: 门外有人\n"
	got := ContractWithoutBeats(raw)
	if strings.Contains(got, "scenes:") || strings.Contains(got, "beat: 入场") {
		t.Fatalf("scenes leaked into the contract:\n%s", got)
	}
	for _, want := range []string{"unit_id: v01-U1", "desire: 站住脚", "next_hook:"} {
		if !strings.Contains(got, want) {
			t.Errorf("contract lost %q:\n%s", want, got)
		}
	}
}

// 合同只剩 scenes 时不能返回空串——宁可原样返回，也不能让模型拿到空合同。
func TestContractWithoutBeatsFallsBackWhenOnlyScenes(t *testing.T) {
	raw := "scenes:\n  id: S1\n  beat: 入场\n"
	if got := ContractWithoutBeats(raw); strings.TrimSpace(got) == "" {
		t.Fatal("must not blank out a contract that is only scenes")
	}
}

// 分章写作只该带本章的场面：6 章单元每次带全部 26 个场面是纯浪费。
func TestBeatsForChapterNarrowsToOneChapter(t *testing.T) {
	raw := `scenes:
  id: S1
  chapter: 1
  beat: 入场
  id: S2
  chapter: 1
  beat: 加压
  id: S3
  chapter: 2
  beat: 转折
`
	all := ChapterBeats(raw)
	one := BeatsForChapter(raw, 1)
	two := BeatsForChapter(raw, 2)
	if len(one) >= len(all) || len(two) >= len(all) {
		t.Fatalf("filter did not narrow: all=%d ch1=%d ch2=%d", len(all), len(one), len(two))
	}
	if !strings.Contains(one, "beat: 入场") || !strings.Contains(one, "beat: 加压") {
		t.Fatalf("chapter 1 lost its beats:\n%s", one)
	}
	if strings.Contains(one, "beat: 转折") {
		t.Fatalf("chapter 2's beat leaked into chapter 1:\n%s", one)
	}
	if !strings.Contains(two, "beat: 转折") {
		t.Fatalf("chapter 2 beats missing:\n%s", two)
	}
}

// 没有 chapter 字段的场面一律保留：宁可多带，也不要丢掉作者的节拍。
func TestBeatsForChapterKeepsUnlabelledScenes(t *testing.T) {
	raw := `scenes:
  id: S1
  chapter: 1
  beat: 入场
  id: S2
  beat: 无章号
`
	if got := BeatsForChapter(raw, 2); !strings.Contains(got, "无章号") {
		t.Fatalf("unlabelled scene dropped:\n%s", got)
	}
}

// 过滤后为空说明 chapter 标记不可信，退回全量而不是给模型一个空节拍。
func TestBeatsForChapterFallsBackWhenNothingMatches(t *testing.T) {
	raw := `scenes:
  id: S1
  chapter: 9
  beat: 入场
  id: S2
  chapter: 9
  beat: 加压
`
	got := BeatsForChapter(raw, 1)
	if !strings.Contains(got, "beat: 入场") {
		t.Fatalf("should fall back to the full block, got:\n%s", got)
	}
}

// 审校按章调用，所以每章只该带本章的场面节拍。
// 回归：Contract 剥掉 scenes 后必须在 build 时按章补回来，否则模型盲审。
func TestChapterReviewPacketCarriesOwnBeats(t *testing.T) {
	unit := domain.UnitInfo{
		UnitID: "v01-U1", Volume: 1, ChapterFrom: 1, ChapterTo: 2,
		OutlinePath: "outline/units/v01-U1.yaml", ProsePath: "units/v01-U1.md",
		OutlineRaw: `unit_id: v01-U1
desire: 站住脚
scenes:
  id: S1
  chapter: 1
  beat: 第一章的进场
  id: S2
  chapter: 2
  beat: 第二章的转折
`,
	}
	snap := domain.BookSnapshot{Summary: domain.BookSummary{ID: "b1"}, Files: map[string]string{}}
	baseline := BuildReviewBaseline(snap, unit, nil, "")

	if strings.Contains(baseline.Contract, "scenes:") {
		t.Fatal("contract must not repeat scenes")
	}

	ch1 := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 1, Body: "正文一"}, "", baseline, 0)
	ch2 := BuildChapterReviewPacket(snap, unit, domain.ChapterDraft{Chapter: 2, Body: "正文二"}, "", baseline, 0)

	var b1, b2 string
	for _, b := range ch1.Blocks {
		if b.ID == "unit-beats" {
			b1 = b.Content
		}
	}
	for _, b := range ch2.Blocks {
		if b.ID == "unit-beats" {
			b2 = b.Content
		}
	}
	if b1 == "" || b2 == "" {
		t.Fatal("unit-beats block missing: the model would review blind")
	}
	if !strings.Contains(b1, "第一章的进场") || strings.Contains(b1, "第二章的转折") {
		t.Fatalf("chapter 1 beats wrong:\n%s", b1)
	}
	if !strings.Contains(b2, "第二章的转折") || strings.Contains(b2, "第一章的进场") {
		t.Fatalf("chapter 2 beats wrong:\n%s", b2)
	}
}

// 连续性状态按阶段裁剪：写作不读知情范围（会诱导提前揭示），审校才读。
func TestContinuityStageScoping(t *testing.T) {
	root := t.TempDir() + "/book"
	src := filepath.Join("..", "..", "..", "tests", "fixtures", "books", "sample-xuanhuan")
	if err := copyTree(src, root); err != nil {
		t.Fatal(err)
	}
	state := `version: 1
units:
  - unit: v01-U0
    volume: 1
    cast:
      linchen:
        location: 山门
        goal: 入内门
        injury: 左臂扭伤
        knows: [知道刀柄来历]
`
	if err := os.WriteFile(filepath.Join(root, books.StatePath), []byte(state), 0o644); err != nil {
		t.Fatal(err)
	}
	snap, err := books.LoadBook(root, false)
	if err != nil {
		t.Fatal(err)
	}
	unit := snap.Units[0]
	onStage := []string{"linchen"}

	write := ExtractContinuity(snap, unit, onStage, ContinuityForWrite)
	if !strings.Contains(write, "山门") || !strings.Contains(write, "入内门") {
		t.Fatalf("write stage missing state: %s", write)
	}
	if strings.Contains(write, "知情范围") || strings.Contains(write, "刀柄来历") {
		t.Fatalf("write stage must not expose knowledge scope: %s", write)
	}

	review := ExtractContinuity(snap, unit, onStage, ContinuityForReview)
	if !strings.Contains(review, "知情范围") {
		t.Fatalf("review stage must expose knowledge scope: %s", review)
	}
}

// 没有状态账时返回空串而不是「尚无事实」这类占位：那是噪音。
func TestContinuityEmptyWithoutLedger(t *testing.T) {
	snap := loadFixtureCopy(t)
	unit := findUnit(t, snap, "v01-U1")
	if got := ExtractContinuity(snap, unit, nil, ContinuityForWrite); got != "" {
		t.Fatalf("got %q, want empty", got)
	}
}

// 包体积回归：写作单章 ≤ 32KB、审校单章 ≤ 34KB。
//
// 上限用 fixture 校准过（fixture 单元小），主要作用是挡住"把整份 YAML 又塞回去"
// 这类回归——scenes 曾重复注入两次、facts.md 曾把整份账本拖进来，都靠这条线拦住。
func TestPacketSizeBudget(t *testing.T) {
	snap := loadFixtureCopy(t)
	unit := findUnit(t, snap, "v01-U1")

	wp := BuildWritePacket(snap, unit, "", 0)
	narrowed := NarrowBeats(wp, unit, unit.ChapterFrom)
	if sum := blockBytes(narrowed.Blocks); sum > 32*1024 {
		t.Fatalf("write packet %d B exceeds 32KB budget", sum)
	}

	baseline := BuildReviewBaseline(snap, unit, nil, "")
	ch := domain.ChapterDraft{Chapter: unit.ChapterFrom, Body: "正文"}
	rp := BuildChapterReviewPacket(snap, unit, ch, "", baseline, 0)
	if sum := blockBytes(rp.Blocks); sum > 34*1024 {
		t.Fatalf("review packet %d B exceeds 34KB budget", sum)
	}

	// scenes 不得同时出现在合同与节拍块里。
	for _, b := range narrowed.Blocks {
		if b.ID == "unit-contract" && strings.Contains(b.Content, "scenes:") {
			t.Fatal("contract must not repeat scenes")
		}
	}
}

func blockBytes(blocks []domain.ContextBlock) int {
	sum := 0
	for _, b := range blocks {
		sum += b.Bytes
	}
	return sum
}

// 审校合同裁掉规划字段，写作合同裁掉作者台账；两者都不该带 continuity_risks。
func TestTrimContractPerStage(t *testing.T) {
	raw := `unit_id: v01-U1
function: 入门
word_target: "9000"
pleasure: 翻盘
forbidden: 不揭底
info_control: 只谈案子
continuity_risks: 作者登记
observations: 历轮观察
scenes:
  - id: S1
`
	write := TrimContract(raw, domain.TaskWrite)
	for _, gone := range []string{"continuity_risks", "observations", "word_target", "function"} {
		if strings.Contains(write, gone) {
			t.Errorf("write contract kept %q", gone)
		}
	}
	for _, kept := range []string{"forbidden", "info_control", "pleasure"} {
		if !strings.Contains(write, kept) {
			t.Errorf("write contract dropped %q", kept)
		}
	}

	review := TrimContract(raw, domain.TaskReview)
	for _, gone := range []string{"continuity_risks", "word_target", "function", "pleasure"} {
		if strings.Contains(review, gone) {
			t.Errorf("review contract kept %q", gone)
		}
	}
	if !strings.Contains(review, "forbidden") {
		t.Error("review contract must keep forbidden (it judges release boundaries)")
	}
}
