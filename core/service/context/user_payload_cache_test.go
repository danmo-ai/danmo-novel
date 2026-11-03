package contextsvc

import (
	"errors"
	"strings"
	"testing"
)

// 重试要复用 provider 的前缀缓存，需要满足两个字节级条件：
//  1. system prompt 完全不变（由调用方保证：packet.SystemPrompt 是静态的）；
//  2. repair 追加在 user payload 的**最末尾**，前面的字节一个不动。
//
// 第 2 条很容易被字段顺序破坏：把 repair 插在中间，它后面的一切都会整体后移，
// 缓存从断点起全部失效——正文/文档动辄数万 token，这一下就是整份重算。
func TestRepairIsAppendedAtPayloadTail(t *testing.T) {
	base := EncodeUserPayload(UserPayload{
		Task:  "write",
		Book:  map[string]any{"id": "b1", "chapter": 3},
		Extra: map[string]any{"chapter": 3, "previousChapter": "上一章正文……"},
		References: []UserRef{
			{ID: "knowledge-1", Title: "参考知识", Content: "风格参考"},
		},
	})
	repaired := MergeRepairIntoUserPayload(base, errors.New("chapters[1].body：需要 非空正文，实际 （空）"))

	// 稳定前缀：首轮 payload 去掉闭合括号后，重试必须原样沿用。
	prefix := strings.TrimSuffix(base, "}")
	if !strings.HasPrefix(repaired, prefix) {
		t.Fatalf("retry does not preserve the stable prefix (prefix cache would fully miss)\nbase=%s\nretry=%s", base, repaired)
	}
	if !strings.HasPrefix(repaired[len(prefix):], `,"repair":`) {
		t.Fatalf("repair is not the tail block: %s", repaired[len(prefix):])
	}
	// extra 必须在 repair 之前：分章写作里 previousChapter 是整章正文。
	if strings.Index(repaired, `"extra"`) > strings.Index(repaired, `"repair"`) {
		t.Fatal("extra must serialize before repair, otherwise chapter text sits after the cache breakpoint")
	}
	if strings.Index(repaired, `"references"`) > strings.Index(repaired, `"repair"`) {
		t.Fatal("references must serialize before repair")
	}
}

// 稳定前缀必须逐字节可复现：同一份 payload 两次编码结果相同，重试才可能命中。
// map 迭代顺序随机，靠 encoding/json 的 key 排序兜住；这条守住它不被改掉。
func TestUserPayloadEncodingIsDeterministic(t *testing.T) {
	p := UserPayload{
		Task: "review",
		Book: map[string]any{"unitId": "v01-U1", "chapter": 2, "prose": "正文……"},
		Extra: map[string]any{
			"a": 1, "b": 2, "c": 3, "d": 4, "e": 5, "f": 6, "g": 7, "h": 8,
		},
	}
	first := EncodeUserPayload(p)
	for i := 0; i < 20; i++ {
		if got := EncodeUserPayload(p); got != first {
			t.Fatalf("encoding is not deterministic on run %d:\n%s\n%s", i, first, got)
		}
	}
}

// repair 只替换、不叠加：第二次重试不该带着第一轮的错误（那会让 payload
// 无限增长，也会让模型同时看到两条互相矛盾的修复指令）。
func TestRepairReplacesPreviousFeedback(t *testing.T) {
	base := EncodeUserPayload(UserPayload{Task: "write", Book: map[string]any{"id": "b1"}})
	once := MergeRepairIntoUserPayload(base, errors.New("第一轮错误"))
	twice := MergeRepairIntoUserPayload(once, errors.New("第二轮错误"))

	if strings.Contains(twice, "第一轮错误") {
		t.Fatalf("stale repair feedback survived: %s", twice)
	}
	p, err := DecodeUserPayload(twice)
	if err != nil {
		t.Fatal(err)
	}
	msg, _ := p.Repair["message"].(string)
	if !strings.Contains(msg, "第二轮错误") {
		t.Fatalf("repair message=%q", msg)
	}
}

// 老格式（非 JSON）payload 会把 extra 直接拼成自然语言——这是唯一手写遍历
// map 生成提示词的地方。不排序的话同一组 extra 每次得到不同字节序，
// 前缀缓存与「同输入同输出」一起失效。
func TestLegacyExtraMergeIsOrderStable(t *testing.T) {
	legacy := "旧格式正文提示词"
	extra := map[string]any{"chapter": 3, "chapterRule": "只写这一章", "previousChapter": "上一章正文", "aaa": 1}
	first := MergeExtraIntoUserPayload(legacy, extra)
	for i := 0; i < 50; i++ {
		if got := MergeExtraIntoUserPayload(legacy, extra); got != first {
			t.Fatalf("legacy merge is not order stable on run %d:\n%s\n%s", i, first, got)
		}
	}
	// 键必须按字典序出现，否则上面的稳定性只说明"碰巧一样"。
	if strings.Index(first, "aaa: 1") > strings.Index(first, "chapter: 3") {
		t.Fatalf("extra keys are not sorted: %s", first)
	}
}

// 分章写作的公共前缀必须撑过 documents 与整份 references。
//
// 这条守的是构建顺序：场面节拍是唯一按章变化的文档，一旦它留在 documents 里
// （哪怕只是替换内容、位置不变），公共前缀就停在 documents 开头，后面的基线块
// 和作者勾选的参考知识全部按全价重算。实测 4 章单元第 3、4 章命中 896 token
// （≈5%）：恰好是 system + 单元合同 + 节拍开头那一点。
func TestAdjacentWriteChaptersShareDocumentsAndReferences(t *testing.T) {
	snap := loadFixtureCopy(t)
	unit := findUnit(t, snap, "v01-U1")
	packet := BuildWritePacket(snap, unit, "", 0)
	// 参考知识在真实书里占写作 prompt 的大头（单本实测 32KB），这里按比例给。
	refs := []string{
		strings.Repeat("# 参考A\n办案的口语细节与现场质感。", 300),
		strings.Repeat("# 参考B\n删掉 AI 套话与解释腔。", 300),
	}

	build := func(ch int) (system, user string) {
		cp, beats := NarrowBeats(packet, unit, ch)
		u := MergeKnowledgeIntoUserPayload(cp.UserPrompt, refs)
		extra := map[string]any{"chapter": ch, "chapterRule": "本次只写这一章"}
		if beats != "" {
			extra[ChapterBeatsKey] = beats
		}
		return cp.SystemPrompt, MergeExtraIntoUserPayload(u, extra)
	}

	s1, u1 := build(1)
	s3, u3 := build(3)
	if s1 != s3 {
		t.Fatal("system prompt changed between chapters; prefix cache would miss")
	}
	marker := `"extra":{`
	idx := strings.Index(u3, marker)
	if idx < 0 {
		t.Fatalf("payload has no extra block: %s", u3[:200])
	}
	// 分叉点必须不早于 extra：documents / references / task / instruction 全部共享。
	need := len(s3) + idx + len(marker)
	shared := commonPrefixLen([]byte(s1+u1), []byte(s3+u3))
	if shared < need {
		t.Fatalf("shared prefix=%d B, need >=%d B: 有按章变化的内容排在 documents/references 前面", shared, need)
	}
	if refsAt := strings.Index(u3, `"references"`); refsAt >= 0 && len(s3)+refsAt+len(`"references"`) > shared {
		t.Fatalf("references (%d B) fall outside the shared prefix (%d B)", len(refs), shared-len(s3))
	}
	// 这条是本次修复的直接回归：节拍不得进入公共前缀。
	_, beats3 := NarrowBeats(packet, unit, 3)
	if b := strings.Index(u3, beats3); b >= 0 && len(s3)+b < shared {
		t.Fatalf("chapter beats start at %d B, inside the shared prefix (%d B)", len(s3)+b, shared)
	}
}
