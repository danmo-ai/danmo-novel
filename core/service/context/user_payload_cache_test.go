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
