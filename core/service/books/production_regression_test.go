package books

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
)

func TestActualLedgerDoesNotTurnUnconfirmedPlansIntoFacts(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{{Chapter: 1, Body: "林尘仍在外门。"}, {Chapter: 2, Body: "他没有进入山门。"}}
	actual := []domain.ChapterActual{{Chapter: 1, Summary: "林尘留在外门"}, {Chapter: 2, Summary: "进入山门的计划没有发生"}}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatal(err)
	}
	states, err := LoadState(root)
	if err != nil {
		t.Fatal(err)
	}
	if len(states) != 1 || states[0].Cast["linchen"].Location != "" {
		t.Fatalf("unconfirmed planned relocation must not become a fact: %+v", states)
	}
	if states[0].AfterCh != 2 {
		t.Fatalf("after_ch=%d", states[0].AfterCh)
	}
	raw, _ := os.ReadFile(filepath.Join(root, ActualPath(unit.UnitID)))
	if !strings.Contains(string(raw), "计划没有发生") {
		t.Fatal("actual ledger must retain the confirmed non-event for review")
	}
}

func TestFinalizationSummaryUsesActualRowsAndDoesNotDuplicate(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{{Chapter: 1, Body: "一"}, {Chapter: 2, Body: "二"}}
	actual := []domain.ChapterActual{{Chapter: 1, Summary: "真实事件一", Plant: []string{"FS-01"}, PlannedForeshadowHandled: []string{"FS-01"}}, {Chapter: 2, Summary: "真实事件二", Payoff: []string{"FS-01"}, PlannedForeshadowHandled: []string{"FS-01"}}}
	for i := 0; i < 2; i++ {
		if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(root, "continuity", "summaries", "v01.md")); !os.IsNotExist(err) {
		t.Fatal("finalize must not persist a duplicate summary file")
	}
	projection, err := ProjectVolumeSummary(root, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(projection, "| 1 |") != 1 || strings.Count(projection, "| 2 |") != 1 {
		t.Fatalf("projection not idempotent:\n%s", projection)
	}
	if !strings.Contains(projection, "真实事件一") || !strings.Contains(projection, "FS-01→已收") {
		t.Fatalf("projection must use actual facts:\n%s", projection)
	}
	commit, _ := os.ReadFile(filepath.Join(root, "continuity", "commits", unit.UnitID+".md"))
	if strings.Contains(string(commit), "真实事件一") || !strings.Contains(string(commit), ActualPath(unit.UnitID)) {
		t.Fatal("commit must reference actual facts, not copy chapter summaries")
	}
}

// 回证跨段落时，模型抄的引文往往把正文的段间空行省掉。按字节比较会把
// 「只差一个换行」判成没有证据，整份定稿失败，而预览里看不出任何内容差异。
// 逐字要求的是字符一致，不是版式一致。
func TestFinalizationAcceptsEvidenceThatIgnoresParagraphBreaks(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。\n\n他被点名上场。"},
		{Chapter: 2, Title: "爆发", Body: "名额只剩一个。\n\n他燃烧代价换来一次爆发。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘付代价完成试炼", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "location", From: "外门", To: "山门",
			Evidence: "名额只剩一个。他燃烧代价换来一次爆发。", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("blank line between paragraphs must not fail the evidence check: %v", err)
	}
}

// 编造的引文不再让接受失败，但必须在定稿日志里留痕：账本不能看起来"全都验过了"。
func TestFinalizationLogsFabricatedEvidence(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。\n\n他被点名上场。"},
		{Chapter: 2, Title: "爆发", Body: "名额只剩一个。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘付代价完成试炼", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "location", From: "外门", To: "山门",
			Evidence: "他走进山门大殿。", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("回证对不上只该留痕，不该拦住接受：%v", err)
	}
	log := commitLog(t, root, unit.UnitID)
	if !strings.Contains(log, "未命中正文") || !strings.Contains(log, "linchen.location") {
		t.Fatalf("commit log must record which transition was not verified:\n%s", log)
	}
}

// 回证全部命中时，日志要明确说"全部命中"——否则"没写"和"没核对"分不清。
func TestFinalizationLogsAllEvidenceVerified(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。"},
		{Chapter: 2, Title: "爆发", Body: "名额只剩一个。他燃烧代价换来一次爆发。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘付代价完成试炼", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "location", From: "外门", To: "山门",
			Evidence: "他燃烧代价换来一次爆发", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatal(err)
	}
	if log := commitLog(t, root, unit.UnitID); !strings.Contains(log, "1 条全部命中正文") {
		t.Fatalf("commit log must state that evidence checked out:\n%s", log)
	}
}

func commitLog(t *testing.T, root, unitID string) string {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(root, "continuity", "commits", unitID+".md"))
	if err != nil {
		t.Fatalf("commit log missing: %v", err)
	}
	return string(raw)
}

// 引文把人物名换成代词（「温念慈把信封接过来」→「她把信封接过来」）仍然是
// 同一句话的引用。按逐字比较会把它判成伪造，整份定稿失败，而候选里其余几十条
// 迁移都是对的——作者只能重跑一次 LLM 定稿。
func TestFinalizationAcceptsPronounForSubjectName(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。"},
		{Chapter: 2, Title: "爆发", Body: "林尘把信封接过来，胳膊上的纱布扯了一下，她换成左手托着。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘拿到信封", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "resources", From: "未持信封", To: "持有信封",
			Evidence: "她把信封接过来，胳膊上的纱布扯了一下，她换成左手托着。", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("pronoun substitution for the subject must not fail the evidence check: %v", err)
	}
}

// 但代词换主语不能变成"只要句子像就行"：摘掉主语后的片段必须逐字出现在正文里。
// 不满足时不再拒单，而是记进定稿日志。
func TestFinalizationLogsPronounEvidenceThatDoesNotExist(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。"},
		{Chapter: 2, Title: "爆发", Body: "林尘把信封接过来，胳膊上的纱布扯了一下，她换成左手托着。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘拿到信封", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "resources", From: "未持信封", To: "持有信封",
			Evidence: "她把信封塞进怀里，胳膊上的纱布渗了血，她咬牙忍着。", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("改写过的引文只该留痕：%v", err)
	}
	if log := commitLog(t, root, unit.UnitID); !strings.Contains(log, "未命中正文") {
		t.Fatalf("rewritten quote must be recorded as unverified:\n%s", log)
	}
}

// 摘掉主语后的剩余部分太短（碎片到处撞上）时不算证据：拦的是"编一个短句刚好
// 撞上正文某处"，这是代词容错唯一可能被滥用的口子。
func TestFinalizationFlagsTooShortPronounEvidence(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。"},
		{Chapter: 2, Title: "爆发", Body: "林尘把信封接过来。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘拿到信封", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "resources", From: "未持信封", To: "持有信封",
			Evidence: "她把信封接过来。", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("短碎片只该留痕：%v", err)
	}
	if log := commitLog(t, root, unit.UnitID); !strings.Contains(log, "未命中正文") {
		t.Fatalf("a 6-rune fragment must not count as evidence:\n%s", log)
	}
}

// 标点字形不同不算伪造：正文用 ASCII 双引号的对白，模型回抄时几乎总是写成
// 中文弯引号 “ ”。这类差异曾让一个 34 条迁移的整单元账本整份作废，退回逐章后
// 多花六次调用——而两条"不合格"的引文与正文逐字相同。
func TestFinalizationAcceptsEvidenceWithTypographicQuotes(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。"},
		{Chapter: 2, Title: "爆发", Body: "\"我签字，不是因为你这三行写得好。\"他一边签一边说，\"是因为那晚你先让人上车。\""},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘签了字", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "relation", From: "被审查", To: "被认可",
			Evidence: "“我签字，不是因为你这三行写得好。”他一边签一边说，“是因为那晚你先让人上车。”", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("弯曲引号 vs ASCII 引号必须放行：%v", err)
	}
}

// 但归一不能糊过实词：改一个词仍然会被判成未核对，并记进日志。
func TestFinalizationFlagsPunctuatedButRewrittenEvidence(t *testing.T) {
	root, unit := setupFinalizeBook(t)
	chapters := []domain.ChapterDraft{
		{Chapter: 1, Title: "点名", Body: "林尘握住刀柄。"},
		{Chapter: 2, Title: "爆发", Body: "\"我签字，不是因为你这三行写得好。\"他一边签一边说。"},
	}
	actual := []domain.ChapterActual{
		{Chapter: 1, Summary: "林尘被点名上场"},
		{Chapter: 2, Summary: "林尘签了字", StateDeltas: []domain.ChapterActualDelta{{
			Stem: "linchen", Field: "relation", From: "被审查", To: "被认可",
			Evidence: "“我签字，不是因为你这几页写得好。”他一边签一边说。", Confirmed: true,
		}}},
	}
	if err := ApplyFinalizationLedger(root, unit, chapters, actual, nil); err != nil {
		t.Fatalf("改写过的引文只该留痕：%v", err)
	}
	if log := commitLog(t, root, unit.UnitID); !strings.Contains(log, "未命中正文") {
		t.Fatalf("rewritten quote must be recorded as unverified:\n%s", log)
	}
}

func TestVolumeSummaryProjectionPrefersActualAndKeepsLegacyEvents(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "unit_id: v01-U1\n")
	if err := os.MkdirAll(filepath.Join(root, "continuity", "summaries"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "continuity", "summaries", "v01.md"), []byte("# 旧摘要\n| 章 | 事件 | 状态变化 | 伏笔 |\n|---|---|---|---|\n| 1 | 新事件 | 不应复制 | FS-99 |\n| 2 | 旧事件 | 旧伤势 | FS-98 |\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	actualRaw, err := MarshalActual("v01-U1", []domain.ChapterActual{{Chapter: 1, Summary: "实际新事件", Plant: []string{"FS-01"}}})
	if err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFiles(root, map[string][]byte{ActualPath("v01-U1"): actualRaw}); err != nil {
		t.Fatal(err)
	}
	got, err := ProjectVolumeSummary(root, 1, "")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "实际新事件") || !strings.Contains(got, "旧事件") {
		t.Fatalf("missing actual or legacy event:\n%s", got)
	}
	if strings.Contains(got, "不应复制") || strings.Contains(got, "旧伤势") {
		t.Fatalf("legacy dynamic state must not enter projection:\n%s", got)
	}
}

func TestRebuildActualRejectsGapWithoutDeletingTarget(t *testing.T) {
	root := t.TempDir()
	writeOutline(t, root, "v01-U1", "story_day: 1\nend_day: 1\n")
	writeOutline(t, root, "v01-U2", "story_day: 2\nend_day: 2\n")
	writeOutline(t, root, "v01-U3", "story_day: 3\nend_day: 3\n")
	if err := ReplaceState(root, []domain.UnitState{{Unit: "v01-U1", AfterCh: 1}, {Unit: "v01-U3", AfterCh: 3}}); err != nil {
		t.Fatal(err)
	}
	if _, err := RebuildStateFromActual(root, "v01-U3", []domain.ChapterActual{{Chapter: 3, Summary: "真实"}}); err != nil {
		t.Fatal(err)
	}
	states, _ := LoadState(root)
	if len(states) != 2 || states[1].Unit != "v01-U3" {
		t.Fatalf("target state was lost: %+v", states)
	}
}

func TestAtomicWriteFilesRejectsEscapeAndKeepsExistingFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "outline", "units", "v01-U1.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	old := []byte("unit_id: v01-U1\nstatus: accepted\n")
	if err := os.WriteFile(path, old, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := AtomicWriteFiles(root, map[string][]byte{"outline/units/v01-U1.yaml": []byte("changed"), "../escape": []byte("bad")}); err == nil {
		t.Fatal("path escape must fail")
	}
	got, _ := os.ReadFile(path)
	if string(got) != string(old) {
		t.Fatal("rejected commit must not change existing files")
	}
}

func TestLockedTermSelectionAndAbilityExceptions(t *testing.T) {
	// Context package owns extraction; this test protects the on-disk shape used by it.
	root := t.TempDir()
	if err := AtomicWriteFiles(root, map[string][]byte{"canon/locked-terms.yaml": []byte("locked_until:\n  v03: [秘密]\ncompliance: [红线]\n")}); err != nil {
		t.Fatal(err)
	}
	raw, _ := os.ReadFile(filepath.Join(root, "canon/locked-terms.yaml"))
	if !strings.Contains(string(raw), "秘密") {
		t.Fatal("real lock terms must be retained")
	}
}
