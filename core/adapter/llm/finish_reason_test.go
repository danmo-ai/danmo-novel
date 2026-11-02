package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/danmo-ai/danmo-novel/core/port"
)

// 截断判定只看协议：provider 说 finish_reason=length 才算截断。
// 以前靠"completion_tokens 是否等于上层设的预算"反推，报 8191/8192 就漏判。
func TestChatCompletionsExposesFinishReason(t *testing.T) {
	cases := map[string]string{
		"length":     port.FinishReasonLength,
		"MAX_TOKENS": port.FinishReasonLength,
		"stop":       "stop",
		"":           "",
	}
	for raw, want := range cases {
		raw, want := raw, want
		t.Run(raw, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				_ = json.NewEncoder(w).Encode(map[string]any{
					"choices": []map[string]any{{
						"message":       map[string]any{"content": "半截"},
						"finish_reason": raw,
					}},
					"usage": map[string]any{"prompt_tokens": 10, "completion_tokens": 8191, "total_tokens": 8201},
				})
			}))
			defer server.Close()

			p := NewOpenAIChatCompletionsClient(server.URL, "")
			resp, err := p.Chat(context.Background(), port.LLMChatRequest{Model: "gpt-4o"}, "")
			if err != nil {
				t.Fatal(err)
			}
			if resp.FinishReason != want {
				t.Fatalf("finish reason=%q want %q", resp.FinishReason, want)
			}
		})
	}
}

// 输出上限交给模型配置：上层不写 max_tokens 时，client 用目录里的 max_output；
// 写超了也夹回目录值；目录缺失才不发这个字段。
func TestGenParamsDeferToModelCatalog(t *testing.T) {
	registry := &port.ModelGenParams{MaxTokens: 384000}

	if got := mergeGenParams(&port.ModelGenParams{}, registry); got.MaxTokens != 384000 {
		t.Fatalf("max tokens=%d want catalog value", got.MaxTokens)
	}
	if got := mergeGenParams(nil, registry); got == nil || got.MaxTokens != 384000 {
		t.Fatalf("nil request should adopt catalog params")
	}
	if got := mergeGenParams(&port.ModelGenParams{MaxTokens: 999999}, registry); got.MaxTokens != 384000 {
		t.Fatalf("explicit larger budget must clamp to catalog")
	}
	if got := mergeGenParams(&port.ModelGenParams{MaxTokens: 4096}, registry); got.MaxTokens != 4096 {
		t.Fatalf("explicit smaller budget must win, got %d", got.MaxTokens)
	}
	if got := mergeGenParams(&port.ModelGenParams{}, nil); got.MaxTokens != 0 {
		t.Fatalf("without catalog the field must stay unset, got %d", got.MaxTokens)
	}
}
