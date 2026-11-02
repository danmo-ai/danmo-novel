package pipeline

import (
	"testing"

	"github.com/danmo-ai/danmo-novel/core/domain"
	"github.com/danmo-ai/danmo-novel/core/port"
)

// 截断只认响应协议，不认"用满没用一个我们自己设的数字"。
//
// 事故现场：上层把 max_tokens 写死 8192，实际用量报 8191 / 8192 时判成"没截断"，
// 重试沿用原预算，在同一处再断一次，整个 run 失败。
func TestTruncationIsDecidedByFinishReasonOnly(t *testing.T) {
	// 协议说 length → 截断，无论 token 数是多少。
	if !truncatedOutput(domain.CallRecord{CompletionTokens: 10, FinishReason: port.FinishReasonLength}) {
		t.Fatal("finish_reason=length must count as truncation")
	}
	// 协议说 stop → 没截断，哪怕用量正好等于某种"预算"。
	if truncatedOutput(domain.CallRecord{CompletionTokens: 8192, FinishReason: "stop"}) {
		t.Fatal("finish_reason=stop must never be treated as truncation")
	}
	// 协议没给这个字段（老 provider / mock）→ 不猜。
	if truncatedOutput(domain.CallRecord{CompletionTokens: 8192}) {
		t.Fatal("without a protocol signal we must not guess truncation")
	}
}
