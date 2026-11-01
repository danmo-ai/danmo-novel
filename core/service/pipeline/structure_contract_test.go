package pipeline

import (
	"context"
	"strings"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
	contextsvc "github.com/danmo-ai/danmo-novel/core/service/context"
	"github.com/danmo-ai/danmo-novel/core/service/prompt"
)

func TestStructureEditorParsesFindings(t *testing.T) {
	raw := `# 结构审校 · v02
- schema_version: 1
- source_revision: abc

## 问题 · v02-U1-causality-1
- entity_path: units[v02-U1].outcome
- severity: blocking
- lens: causality
- evidence: 结果没有由选择产生
- suggestion: 补上申请复查的选择及获得的明确许可
`
	findings, err := ParseStructureEditor(raw)
	if err != nil || len(findings) != 1 {
		t.Fatalf("findings=%+v err=%v", findings, err)
	}
	if findings[0].EntityPath != "units[v02-U1].outcome" || findings[0].Severity != "blocking" {
		t.Fatalf("wrong finding: %+v", findings[0])
	}
}

func TestStructureEditorRejectsUnknownSeverity(t *testing.T) {
	raw := `# 结构审校
- schema_version: 1

## 问题 · one
- entity_path: units[v01-U1]
- severity: fatal
- lens: causality
- evidence: bad
- suggestion: fix
`
	if _, err := ParseStructureEditor(raw); err == nil {
		t.Fatal("unknown severity must not silently become advisory")
	}
}

// A repair retry must preserve every byte before the final repair property.
// This is what makes provider prefix caching useful on large book contexts.
func TestTaskRunnerRepairKeepsStablePrefix(t *testing.T) {
	contract := prompt.NewContract("test/v1", "markdown", "只输出 Markdown", "# 测试\n- schema_version: 1\n")
	codec := testStringCodec{contract: contract}
	def := TaskDefinition[string]{ID: "test", Version: 1, Codec: codec, Candidate: func(value string, in TaskInput) domain.Candidate {
		return domain.Candidate{Replacement: value}
	}}
	llm := &retryCaptureLLM{responses: []string{"bad", "# 测试\n- schema_version: 1\n- value: ok\n"}}
	payload := contextsvc.UserPayload{Task: "test", Book: map[string]any{"id": "book"}, Documents: []contextsvc.UserDoc{{ID: "large", Path: "outline/book_outline.md", Content: strings.Repeat("稳定上下文", 100)}}}
	res, err := ExecuteTask(context.Background(), &TaskRunner{LLM: llm}, Invocation[string]{
		TaskID: "test", StaticPrompt: "系统指令\n输出协议 test/v1（markdown，grammar " + contract.Hash + "）：", DynamicPayload: payload, OutputContract: contract,
		Policy: domain.CallPolicy{MaxCalls: 2, AllowOneRepair: true, MaxOutputTokens: 100}, ModelID: "mock/m", Definition: def,
	})
	if err != nil || res.Candidate.Replacement == "" {
		t.Fatalf("repair failed: %+v %v", res, err)
	}
	if len(llm.requests) != 2 {
		t.Fatalf("calls=%d", len(llm.requests))
	}
	first := llm.requests[0].Messages[1].Content
	second := llm.requests[1].Messages[1].Content
	prefix := strings.TrimSuffix(first, "}")
	if !strings.HasPrefix(second, prefix) || !strings.Contains(second, `"repair":`) {
		t.Fatalf("repair invalidated stable prefix:\nfirst=%s\nsecond=%s", first, second)
	}
	if llm.requests[0].Messages[0].Content != llm.requests[1].Messages[0].Content {
		t.Fatal("system prompt changed between attempts")
	}
}

type testStringCodec struct{ contract OutputProtocol }

func (c testStringCodec) Protocol() OutputProtocol { return c.contract }
func (c testStringCodec) Decode(raw string) (string, error) {
	if !strings.Contains(raw, "value: ok") {
		return "", contractFail("test", []Violation{{Path: "value", Want: "ok", Got: raw}})
	}
	return raw, nil
}

type retryCaptureLLM struct {
	responses []string
	requests  []port.LLMChatRequest
}

func (l *retryCaptureLLM) Chat(_ context.Context, req port.LLMChatRequest) (port.LLMChatResponse, error) {
	l.requests = append(l.requests, req)
	raw := l.responses[0]
	l.responses = l.responses[1:]
	return port.LLMChatResponse{Content: raw, Done: true}, nil
}
