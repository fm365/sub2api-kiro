package service

import "strings"

// ThinkingProtocol 描述上游对 thinking block 的处理契约。
// 不同上游对历史 thinking block 的语义要求是相反的：
//   - Anthropic 官方：要求 thinking block 携带有效 signature，否则 400
//     "thinking.signature: Field required"
//   - DeepSeek `/anthropic`、Kimi `/coding` 等第三方 Anthropic 兼容上游：
//     要求历史 thinking block 原样回传，否则 400
//     "The content[].thinking in the thinking mode must be passed back to the API"
type ThinkingProtocol int

const (
	// ThinkingProtocolUnknown 表示无法识别协议族（默认保守不剥离）。
	ThinkingProtocolUnknown ThinkingProtocol = iota

	// ThinkingProtocolAnthropicStrict 表示 Anthropic 官方语义：
	// 历史 thinking block 必须携带有效 signature，缺失/非法签名应剥离。
	ThinkingProtocolAnthropicStrict

	// ThinkingProtocolPassbackRequired 表示第三方兼容上游语义：
	// 所有历史 thinking block 必须原样回传，预过滤会破坏契约。
	ThinkingProtocolPassbackRequired
)

// ResolveThinkingProtocol 根据「作为 thinking block 处理参考的模型 ID」推断 thinking 协议族。
//
// 传入参数的语义随调用路径不同：
//   - Anthropic gateway（转发原始 Anthropic 请求）：传 mappedModel（账号级 model mapping
//     后的上游 model ID）。
//   - Gemini messages compat（Anthropic body → Gemini upstream）：传 originalModel。
//
// 匹配规则：
//   - anthropic-strict: claude-* / opus-* / sonnet-* / haiku-*
//   - passback-required: deepseek-* / kimi-* / moonshot-* / glm-* /
//     minimax-m* / (qwen-|qwen2-|qwen3-|qwen4-)*-thinking
//   - unknown: 其他模型（保守不剥离）
func ResolveThinkingProtocol(modelID string) ThinkingProtocol {
	if modelID == "" {
		return ThinkingProtocolUnknown
	}
	id := strings.ToLower(modelID)

	// Passback-required 优先匹配（特定厂商前缀）
	switch {
	case strings.HasPrefix(id, "deepseek-"),
		strings.HasPrefix(id, "kimi-"),
		strings.HasPrefix(id, "moonshot-"),
		strings.HasPrefix(id, "glm-"):
		return ThinkingProtocolPassbackRequired
	}
	// MiniMax M 系列
	if strings.HasPrefix(id, "minimax-m") {
		return ThinkingProtocolPassbackRequired
	}
	// Qwen thinking 变体
	if (strings.HasPrefix(id, "qwen-") ||
		strings.HasPrefix(id, "qwen2-") ||
		strings.HasPrefix(id, "qwen3-") ||
		strings.HasPrefix(id, "qwen4-")) && strings.Contains(id, "-thinking") {
		return ThinkingProtocolPassbackRequired
	}

	switch {
	case strings.HasPrefix(id, "claude-"),
		strings.HasPrefix(id, "opus-"),
		strings.HasPrefix(id, "sonnet-"),
		strings.HasPrefix(id, "haiku-"):
		return ThinkingProtocolAnthropicStrict
	}

	return ThinkingProtocolUnknown
}

// ShouldPreFilterThinkingBlocks 判断是否应在转发前剥离无效 thinking block。
// 仅 anthropic-strict 协议族需要预过滤。
func ShouldPreFilterThinkingBlocks(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}

// ShouldRectifyThinkingSignatureError 判断是否应在 400 后触发 thinking 签名整流 retry。
// 仅 anthropic-strict 触发。
func ShouldRectifyThinkingSignatureError(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}

// ShouldApplyRetryFilters 判断是否应执行 retry 路径的 thinking/tool block 整流。
// 仅 anthropic-strict 走变形。
func ShouldApplyRetryFilters(modelID string) bool {
	return ResolveThinkingProtocol(modelID) == ThinkingProtocolAnthropicStrict
}
