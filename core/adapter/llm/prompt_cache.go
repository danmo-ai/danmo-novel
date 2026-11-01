package llm

import "strings"

// promptCacheRepairMarker 是受控 payload 里 repair 块的分隔点。
// core/service/context 把 Repair 序列化在 UserPayload 的最后一个字段，
// 因此它前面的整份上下文在重试之间逐字节不变。
const promptCacheRepairMarker = `,"repair":`

// splitPromptCachePrefix splits a controlled user payload immediately before
// its repair block, so the stable prefix can carry an Anthropic cache
// breakpoint.
//
// 为什么需要：Anthropic 的前缀缓存是显式断点制，只有打了 cache_control 的
// 位置及其之前的内容会被缓存。system 已有断点，但正文、设定、参考文献都在
// user 消息里——不给 user 打点，校验失败重试时那份正文就得按全价重算。
//
// 切分的安全性：两个 block 会被 Anthropic 无缝拼接，所以 stable+tail 必须
// 与输入逐字节相等（这里只做一次纯字符串切分，不重排、不改写）。模型看到的
// 文本与不切分时完全一致，JSON 也仍然完整。
//
// 找不到 repair 块时不做切分：首轮调用本来就没有可缓存的上一次前缀，
// system 的断点已经覆盖了静态指令。
func splitPromptCachePrefix(content string) (stable, tail string, ok bool) {
	if len(content) == 0 || !strings.HasSuffix(content, "}") {
		return "", "", false
	}
	idx := strings.LastIndex(content, promptCacheRepairMarker)
	// idx<=0 表示 repair 就是第一个字段（不可能，task 在前）；
	// 过短的稳定前缀即使打点也会被 Anthropic 静默忽略，没有收益。
	if idx <= 0 || idx < 64 {
		return "", "", false
	}
	return content[:idx], content[idx:], true
}
