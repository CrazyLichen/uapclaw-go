package context

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/processor"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ContextMessageIDKey 消息元数据中 ID 的键名
	ContextMessageIDKey = "context_message_id"
	// DefaultContextMaxTokens 默认最大上下文 token 数
	DefaultContextMaxTokens = 200000
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ModelDefaultContextWindowTokens 各模型的默认上下文窗口大小（单位 token），键为模型名。
//
// 逐条对齐上游 MODEL_DEFAULT_CONTEXT_WINDOW_TOKENS（context_utils.py）。
var ModelDefaultContextWindowTokens = map[string]int{
	// GLM
	"glm-5.2":          1048576,
	"glm-5.1":          204800,
	"glm-5":            204800,
	"glm-5-turbo":      202752,
	"glm-4.7":          204800,
	"glm-4.7-flash":    202752,
	"glm-4.7-flashx":   200000,
	"glm-4.6":          204800,
	"glm-4.5":          131072,
	"glm-4.5-air":      131072,
	"glm-4-long":       1000000,
	"glm-4":            128000,
	"glm-4-9b-chat-1m": 1048576,
	// OpenAI GPT
	"gpt-5.6-luna":      1050000,
	"gpt-5.6-luna-pro":  1050000,
	"gpt-5.6-sol":       1050000,
	"gpt-5.6-sol-pro":   1050000,
	"gpt-5.6-terra":     1050000,
	"gpt-5.6-terra-pro": 1050000,
	"gpt-5.5":           1050000,
	"gpt-5.5-pro":       1050000,
	"gpt-5.4":           1050000,
	"gpt-5.4-pro":       1050000,
	"gpt-5.4-mini":      400000,
	"gpt-5.4-nano":      400000,
	"gpt-5.2":           400000,
	"gpt-5.1":           400000,
	"gpt-5":             400000,
	"gpt-5-pro":         400000,
	"gpt-5-mini":        400000,
	"gpt-5-nano":        400000,
	"gpt-4.1":           1047576,
	"gpt-4.1-mini":      1047576,
	"gpt-4.1-nano":      1047576,
	"gpt-4o":            128000,
	"gpt-4o-mini":       128000,
	"gpt-4-turbo":       128000,
	"gpt-3.5-turbo":     16385,
	// OpenAI o-series
	"o1":      200000,
	"o3":      200000,
	"o3-mini": 200000,
	"o3-pro":  200000,
	"o4-mini": 200000,
	// DeepSeek
	"deepseek-v4-pro":        1048576,
	"deepseek-v4-flash":      1048576,
	"deepseek-v3.2":          163840,
	"deepseek-v3.1-terminus": 163840,
	"deepseek-r1":            163840,
	"deepseek-v3":            128000,
	"deepseek-chat":          163840,
	// Anthropic Claude
	"claude-opus-5":     1000000,
	"claude-opus-4-8":   1000000,
	"claude-opus-4.8":   1000000,
	"claude-opus-4-7":   1000000,
	"claude-opus-4.7":   1000000,
	"claude-opus-4-6":   1000000,
	"claude-opus-4.6":   1000000,
	"claude-opus-4-5":   200000,
	"claude-opus-4.5":   200000,
	"claude-sonnet-5":   1000000,
	"claude-sonnet-4-6": 1000000,
	"claude-sonnet-4.6": 1000000,
	"claude-sonnet-4-5": 1000000,
	"claude-sonnet-4.5": 1000000,
	"claude-haiku-4-5":  200000,
	"claude-haiku-4.5":  200000,
	// Google Gemini
	"gemini-3.6-flash":       1048576,
	"gemini-3.5-flash":       1048576,
	"gemini-3.5-flash-lite":  1048576,
	"gemini-3.1-pro-preview": 1048576,
	"gemini-3.1-flash-lite":  1048576,
	"gemini-3-pro-preview":   1048576,
	"gemini-3-flash-preview": 1048576,
	"gemini-2.5-pro":         1048576,
	"gemini-2.5-flash":       1048576,
	"gemini-2.5-flash-lite":  1048576,
	// Meta Llama
	"llama-4-maverick": 1048576,
	"llama-4-scout":    10000000,
	// Qwen
	"qwen3.7-max":         1000000,
	"qwen3.7-plus":        1000000,
	"qwen3.7-flash":       1000000,
	"qwen3.6-max-preview": 262144,
	"qwen3.6-plus":        1000000,
	"qwen3.6-flash":       1000000,
	"qwen3.5-plus":        1000000,
	"qwen3.5-flash":       1000000,
	"qwen3-max":           262144,
	"qwen3-max-thinking":  262144,
	"qwen3-coder-plus":    1000000,
	"qwen3-coder-flash":   1000000,
	"qwen3-coder-next":    262144,
	"qwen-max":            262144,
	"qwen-plus":           1000000,
	"qwen-flash":          1000000,
	"qwen-turbo":          8192,
	"qwen-long":           1000000,
	// Moonshot Kimi
	"kimi-k3":          1048576,
	"kimi-k2.7-code":   262144,
	"kimi-k2.6":        262144,
	"kimi-k2.5":        262144,
	"kimi-k2-thinking": 262144,
	"kimi-k2-0905":     262144,
	"kimi-k2":          131072,
	// MiniMax
	"MiniMax-M3":             1048576,
	"MiniMax-M2.7":           204800,
	"MiniMax-M2.7-highspeed": 204800,
	"MiniMax-M2.5":           204800,
	"MiniMax-M2.5-highspeed": 204800,
	"MiniMax-M2.1":           204800,
	"MiniMax-M2":             204800,
	"MiniMax-M1":             1000000,
	// xAI Grok
	"grok-4.20":             2000000,
	"grok-4.20-multi-agent": 2000000,
	"grok-4.5":              500000,
	"grok-4.3":              1000000,
	"grok-4.3-latest":       1000000,
	"grok-latest":           500000,
}

var (
	// openRouterModelsURL OpenRouter 模型列表地址（测试可覆写）
	openRouterModelsURL = "https://openrouter.ai/api/v1/models"
	// openRouterCacheTTL 拉取结果进程级缓存时长
	openRouterCacheTTL = time.Hour
	// openRouterTokensMu 保护下方缓存字段
	openRouterTokensMu sync.Mutex
	// openRouterTokens 最近一次成功拉取的窗口表
	openRouterTokens map[string]int
	// openRouterTokensFetched 最近一次拉取时间（失败也推进，避免打爆对端）
	openRouterTokensFetched time.Time
)

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildModelContextWindowTokens 合并 OpenRouter 拉取表与用户显式表（用户值优先）。
//
// Python: ContextUtils.build_model_context_window_tokens()
func BuildModelContextWindowTokens(user map[string]int, enableOpenRouter bool, timeoutSeconds float64) map[string]int {
	resolved := make(map[string]int)
	if enableOpenRouter {
		for name, tokens := range fetchOpenRouterContextWindows(timeoutSeconds) {
			resolved[name] = tokens
		}
	}
	for name, tokens := range user {
		if tokens > 0 {
			resolved[name] = tokens
		}
	}
	return resolved
}

// ValidateMessages 校验消息列表中是否存在 nil 元素。
func ValidateMessages(messages []llm_schema.BaseMessage) error {
	for i, msg := range messages {
		if msg == nil {
			return exception.NewBaseError(exception.StatusContextMessageInvalid,
				exception.WithParam("error_msg", fmt.Sprintf("消息列表中索引 %d 的元素为 nil", i)),
			)
		}
	}
	return nil
}

// EnsureContextMessageIDs 为缺少 context_message_id 的消息补充唯一 ID。
func EnsureContextMessageIDs(messages []llm_schema.BaseMessage) []llm_schema.BaseMessage {
	for _, msg := range messages {
		metadata := msg.GetMetadata()
		if metadata == nil {
			metadata = make(map[string]any)
		}
		if _, exists := metadata[ContextMessageIDKey]; !exists {
			metadata[ContextMessageIDKey] = uuid.New().String()
			msg.SetMetadata(metadata)
		}
	}
	return messages
}

// ValidateAndFixContextWindow 校验并修复上下文窗口中开头连续 ToolMessage 的问题。
func ValidateAndFixContextWindow(window *iface.ContextWindow) {
	messages := window.ContextMessages
	if len(messages) == 0 {
		return
	}

	// 找到第一个非 ToolMessage 的索引
	firstNonToolIdx := -1
	for i, msg := range messages {
		if msg.GetRole() != llm_schema.RoleTypeTool {
			firstNonToolIdx = i
			break
		}
	}

	if firstNonToolIdx == -1 {
		// 全部是 ToolMessage → 清空
		window.ContextMessages = make([]llm_schema.BaseMessage, 0)
	} else if firstNonToolIdx > 0 {
		// 开头有 ToolMessage → 截掉开头的 ToolMessage
		window.ContextMessages = messages[firstNonToolIdx:]
	}
}

// ResolveContextMax 按优先级解析最大上下文 token 数：fallback > 自定义映射 > 内置映射 > 默认值。
func ResolveContextMax(modelName string, fallbackContextWindowTokens int, modelContextWindowTokens map[string]int) int {
	// 优先级 1：fallback > 0 直接返回
	if fallbackContextWindowTokens > 0 {
		return fallbackContextWindowTokens
	}

	// 优先级 2 和 3：modelName 非空时查找
	if modelName != "" {
		// 优先级 2：自定义映射
		if modelContextWindowTokens != nil {
			if tokens, ok := modelContextWindowTokens[modelName]; ok {
				return tokens
			}
		}
		// 优先级 3：内置映射
		if tokens, ok := ModelDefaultContextWindowTokens[modelName]; ok {
			return tokens
		}
	}

	// 优先级 4：默认值
	return DefaultContextMaxTokens
}

// IsCompressionProcessor 判断处理器是否为压缩类处理器（类型名含 compressor 或 compact）。
func IsCompressionProcessor(p iface.ContextProcessor) bool {
	processorType := strings.ToLower(p.ProcessorType())
	return strings.Contains(processorType, "compressor") || strings.Contains(processorType, "compact")
}

// FormatReloadedMessages 将重新加载的消息格式化为可读字符串，用于日志输出。
func FormatReloadedMessages(offloadHandle string, messages []llm_schema.BaseMessage) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "reload messages with handle=%s:\n", offloadHandle)
	for i, msg := range messages {
		dump := messageToMap(msg)
		msgJSON, err := json.Marshal(dump)
		if err != nil {
			fmt.Fprintf(&sb, "message %d: {serialization failed}", i+1)
		} else {
			fmt.Fprintf(&sb, "message %d: %s", i+1, string(msgJSON))
		}
		if i != len(messages)-1 {
			sb.WriteByte('\n')
		}
	}
	return sb.String()
}

// FindLastNDialogueRound 找到倒数第 n 轮对话的起始消息索引。
//
// Python: ContextUtils.find_last_n_dialogue_round()
func FindLastNDialogueRound(messages []llm_schema.BaseMessage, n int) int {
	rounds := processor.FindAllDialogueRound(messages)
	if len(rounds) == 0 {
		return -1
	}

	// rounds 从新到旧排列，取 min(n, len(rounds))-1 得到目标轮次索引
	targetIdx := n
	if targetIdx > len(rounds) {
		targetIdx = len(rounds)
	}
	targetRound := rounds[targetIdx-1]

	// 返回 user 消息索引，如果 userIdx 为 nil → 返回 -1
	userIdx := targetRound[0]
	if userIdx == nil {
		return -1
	}
	return *userIdx
}

// FindLastAIAbsentToolCall 从后往前查找最后一条不含 ToolCalls 的 AssistantMessage 索引。
//
// Python: ContextUtils.find_last_ai_message_without_tool_call()
func FindLastAIAbsentToolCall(messages []llm_schema.BaseMessage) int {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.GetRole() == llm_schema.RoleTypeAssistant {
			am, ok := msg.(*llm_schema.AssistantMessage)
			if ok && len(am.ToolCalls) == 0 {
				return i
			}
		}
	}
	return -1
}

// FindMessageIndexByContextMessageID 根据消息元数据中的 context_message_id 查找消息索引。
//
// 返回第一条匹配的消息索引，未找到返回 -1。
//
// Python: ContextUtils.find_message_index_by_context_message_id()
func FindMessageIndexByContextMessageID(messages []llm_schema.BaseMessage, id string) int {
	for i, msg := range messages {
		metadata := msg.GetMetadata()
		if metadata == nil {
			continue
		}
		if msgID, ok := metadata[ContextMessageIDKey].(string); ok && msgID == id {
			return i
		}
	}
	return -1
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fetchOpenRouterContextWindows 拉取 OpenRouter 模型窗口表，进程级 TTL 缓存，失败沿用旧值。
//
// 解析 {"data":[{"id","context_length"}]}；带 "/" 的 id 额外产出无前缀别名，
// 同名别名冲突时丢弃该别名（避免把两家厂商的同名模型混为一谈）。
//
// Python: ContextUtils.fetch_openrouter_model_context_window_tokens()
func fetchOpenRouterContextWindows(timeoutSeconds float64) map[string]int {
	openRouterTokensMu.Lock()
	if !openRouterTokensFetched.IsZero() && time.Since(openRouterTokensFetched) < openRouterCacheTTL {
		cached := copyIntMap(openRouterTokens)
		openRouterTokensMu.Unlock()
		return cached
	}
	openRouterTokensFetched = time.Now()
	openRouterTokensMu.Unlock()

	timeout := timeoutSeconds
	if timeout <= 0 {
		timeout = 3
	}
	client := &http.Client{Timeout: time.Duration(timeout * float64(time.Second))}
	resp, err := client.Get(openRouterModelsURL)
	if err != nil {
		logger.Warn(logComponent).Err(err).
			Msg("failed to fetch openrouter model context windows, use cached or built-in values")
		return copyIntMap(openRouterTokens)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		logger.Warn(logComponent).Int("status_code", resp.StatusCode).
			Msg("failed to fetch openrouter model context windows, use cached or built-in values")
		return copyIntMap(openRouterTokens)
	}

	var payload struct {
		Data []struct {
			ID            string `json:"id"`
			ContextLength int    `json:"context_length"`
		} `json:"data"`
	}
	if decodeErr := json.NewDecoder(resp.Body).Decode(&payload); decodeErr != nil {
		logger.Warn(logComponent).Err(decodeErr).
			Msg("failed to parse openrouter models payload, use cached or built-in values")
		return copyIntMap(openRouterTokens)
	}

	parsed := make(map[string]int, len(payload.Data))
	aliases := make(map[string]int, len(payload.Data))
	ambiguous := make(map[string]struct{})
	for _, m := range payload.Data {
		if m.ID == "" || m.ContextLength <= 0 {
			continue
		}
		parsed[m.ID] = m.ContextLength
		idx := strings.Index(m.ID, "/")
		if idx < 0 {
			continue
		}
		alias := m.ID[idx+1:]
		if _, exists := aliases[alias]; exists {
			ambiguous[alias] = struct{}{}
			continue
		}
		aliases[alias] = m.ContextLength
	}
	for alias, tokens := range aliases {
		if _, dup := ambiguous[alias]; dup {
			continue
		}
		parsed[alias] = tokens
	}

	openRouterTokensMu.Lock()
	openRouterTokens = parsed
	openRouterTokensMu.Unlock()
	return copyIntMap(parsed)
}

// copyIntMap 复制一份 int map，避免调用方改写缓存。
func copyIntMap(src map[string]int) map[string]int {
	if len(src) == 0 {
		return nil
	}
	dst := make(map[string]int, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// resetOpenRouterCache 清空 OpenRouter 缓存（测试用）。
func resetOpenRouterCache() {
	openRouterTokensMu.Lock()
	openRouterTokens = nil
	openRouterTokensFetched = time.Time{}
	openRouterTokensMu.Unlock()
}

// expireOpenRouterCacheTTL 只让缓存过期但保留内容（测试用，验证失败沿用缓存）。
func expireOpenRouterCacheTTL() {
	openRouterTokensMu.Lock()
	openRouterTokensFetched = time.Now().Add(-2 * openRouterCacheTTL)
	openRouterTokensMu.Unlock()
}

// overrideOpenRouterURL 覆写拉取地址（测试用）。
func overrideOpenRouterURL(url string) {
	openRouterModelsURL = url
}

// messageToMap 将 BaseMessage 转换为 map 结构，用于序列化和日志输出。
func messageToMap(msg llm_schema.BaseMessage) map[string]any {
	result := map[string]any{
		"role":    msg.GetRole().String(),
		"content": msg.GetContent().String(),
	}
	if name := msg.GetName(); name != "" {
		result["name"] = name
	}
	if metadata := msg.GetMetadata(); len(metadata) > 0 {
		result["metadata"] = metadata
	}
	// AssistantMessage 特有字段
	if am, ok := msg.(*llm_schema.AssistantMessage); ok {
		if len(am.ToolCalls) > 0 {
			toolCalls := make([]map[string]any, 0, len(am.ToolCalls))
			for _, call := range am.ToolCalls {
				tc := map[string]any{
					"id":   call.ID,
					"type": call.Type,
					"function": map[string]any{
						"name":      call.Name,
						"arguments": call.Arguments,
					},
				}
				toolCalls = append(toolCalls, tc)
			}
			result["tool_calls"] = toolCalls
		}
		if am.UsageMetadata != nil {
			result["usage_metadata"] = am.UsageMetadata
		}
		if am.FinishReason != "" && am.FinishReason != "null" {
			result["finish_reason"] = am.FinishReason
		}
		if am.ParserContent != nil {
			result["parser_content"] = am.ParserContent
		}
		if am.ReasoningContent != "" {
			result["reasoning_content"] = am.ReasoningContent
		}
		if len(am.PromptTokenIDs) > 0 {
			result["prompt_token_ids"] = am.PromptTokenIDs
		}
		if len(am.CompletionTokenIDs) > 0 {
			result["completion_token_ids"] = am.CompletionTokenIDs
		}
		if am.Logprobs != nil {
			result["logprobs"] = am.Logprobs
		}
	}
	// ToolMessage 特有字段
	if tm, ok := msg.(*llm_schema.ToolMessage); ok {
		if tm.ToolCallID != "" {
			result["tool_call_id"] = tm.ToolCallID
		}
	}
	return result
}

// ──────────────────────────── 状态还原 ────────────────────────────

// decodeMessageValue 将状态中的单条消息还原为 BaseMessage。
//
// 兼容两种形态：
//   - 具体消息类型（同进程内上下文状态直传，Python: Message 对象）
//   - map[string]any（经 checkpointer 落盘后 JSON 反序列化的形态，进程重启场景）
//
// 后者按 role 重新反序列化到具体类型，复用各消息类型的 UnmarshalJSON
// （Content 的 string/多模态分片、AssistantMessage 的 tool_calls、ToolMessage 的 tool_call_id）。
func decodeMessageValue(raw any) llm_schema.BaseMessage {
	if msg, ok := raw.(llm_schema.BaseMessage); ok {
		return msg
	}
	fields, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	roleText, _ := fields["role"].(string)
	data, err := json.Marshal(fields)
	if err != nil {
		return nil
	}
	switch strings.ToLower(roleText) {
	case "user":
		msg := &llm_schema.UserMessage{}
		if err := json.Unmarshal(data, msg); err != nil {
			return nil
		}
		return msg
	case "assistant":
		msg := &llm_schema.AssistantMessage{}
		if err := json.Unmarshal(data, msg); err != nil {
			return nil
		}
		return msg
	case "tool":
		msg := &llm_schema.ToolMessage{}
		if err := json.Unmarshal(data, msg); err != nil {
			return nil
		}
		return msg
	case "system":
		msg := &llm_schema.SystemMessage{}
		if err := json.Unmarshal(data, msg); err != nil {
			return nil
		}
		return msg
	}
	return nil
}

// messagesFromState 从状态值还原消息列表，无法识别的元素丢弃。
// Python: SessionModelContext.load_state() 直接使用 messages 列表（无序列化还原步骤）
func messagesFromState(value any) []llm_schema.BaseMessage {
	switch v := value.(type) {
	case []llm_schema.BaseMessage:
		return v
	case []any:
		messages := make([]llm_schema.BaseMessage, 0, len(v))
		for _, item := range v {
			if msg := decodeMessageValue(item); msg != nil {
				messages = append(messages, msg)
			}
		}
		return messages
	}
	return nil
}

// offloadMessagesFromState 从状态值还原卸载消息缓冲区，兼容具体类型与 JSON 反序列化形态。
func offloadMessagesFromState(value any) map[string][]llm_schema.BaseMessage {
	result := make(map[string][]llm_schema.BaseMessage)
	switch v := value.(type) {
	case map[string][]llm_schema.BaseMessage:
		return v
	case map[string]any:
		for handle, listVal := range v {
			if messages := messagesFromState(listVal); messages != nil {
				result[handle] = messages
			}
		}
		return result
	}
	return nil
}

// compressionHistoryFromState 从状态值还原压缩历史，兼容具体类型与 JSON 反序列化形态。
func compressionHistoryFromState(value any) []map[string]any {
	switch v := value.(type) {
	case []map[string]any:
		return v
	case []any:
		history := make([]map[string]any, 0, len(v))
		for _, item := range v {
			if record, ok := item.(map[string]any); ok {
				history = append(history, record)
			}
		}
		return history
	}
	return nil
}
