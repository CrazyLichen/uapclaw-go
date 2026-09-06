package evolution

import (
	"encoding/json"
	"fmt"
	"strings"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitResponseTokenFields 从 LLM 响应中分离 token 级字段。
//
// 返回 (response_for_detail, prompt_token_ids, completion_token_ids, logprobs)。
// 返回的 response_for_detail 已移除这三个字段，避免在轨迹中重复存储。
//
// 对齐 Python: _split_response_token_fields(response)
//
// Python 实现：
//   - 如果 response 有 model_dump() 方法，调用得到 dict
//   - 从 dict 中 pop 出 prompt_token_ids / completion_token_ids / logprobs
//   - 如果 response 是 dict 直接操作
//   - 其他形状原样返回
//
// Go 中 AssistantMessage 直接暴露 PromptTokenIDs / CompletionTokenIDs / Logprobs 字段，
// 同时有 ToOpenAIDict() 方法获取完整 map 表示。
func splitResponseTokenFields(response *llmschema.AssistantMessage) (map[string]any, []int, []int, any) {
	if response == nil {
		return nil, nil, nil, nil
	}

	// 从结构体字段直接提取 token 级数据
	promptTokenIDs := response.PromptTokenIDs
	completionTokenIDs := response.CompletionTokenIDs
	logprobs := response.Logprobs

	// 获取 response 的 map 表示，从中移除 token 字段
	dump := response.ToOpenAIDict()
	if dump == nil {
		return nil, promptTokenIDs, completionTokenIDs, logprobs
	}

	result := map[string]any{}
	for k, v := range dump {
		result[k] = v
	}
	delete(result, "prompt_token_ids")
	delete(result, "completion_token_ids")
	delete(result, "logprobs")

	return result, promptTokenIDs, completionTokenIDs, logprobs
}

// normalizeSkillNames 将技能名称规范化为集合。
//
// 字符串视为单个技能名；切片视为多个名称。前后空格会被裁剪。
//
// 对齐 Python: _normalize_skill_names(raw)
func normalizeSkillNames(raw any) map[string]bool {
	if raw == nil {
		return map[string]bool{}
	}
	switch v := raw.(type) {
	case string:
		name := strings.TrimSpace(v)
		if name == "" {
			return map[string]bool{}
		}
		return map[string]bool{name: true}
	case []string:
		result := map[string]bool{}
		for _, s := range v {
			name := strings.TrimSpace(s)
			if name != "" {
				result[name] = true
			}
		}
		return result
	default:
		return map[string]bool{}
	}
}

// normalizeMemberRole 将成员角色规范化为稳定字符串值。
//
// 对齐 Python: _normalize_member_role(role)
//
// Python 实现：
//   - role 为 None → 返回 None
//   - getattr(role, "value", role) 获取枚举值
//   - str(role_value) 转字符串
//   - 空字符串返回 None
func normalizeMemberRole(role any) *string {
	if role == nil {
		return nil
	}
	text := ""
	switch v := role.(type) {
	case string:
		text = v
	default:
		// 对齐 Python: role_value = getattr(role, "value", role)
		// Go 中枚举通常实现 String() 方法
		if stringer, ok := v.(interface{ String() string }); ok {
			text = stringer.String()
		}
	}
	if text == "" {
		return nil
	}
	return &text
}

// collectMessagesFromTrajectory 从轨迹中提取消息列表。
//
// 调用 ConversationSignalDetector.ConvertTrajectoryToMessages 获取消息列表，
// 然后通过 normalizeCallbackMessages 规范化，最后去重。
//
// 对齐 Python: _collect_messages_from_trajectory(trajectory)
//
// Python 实现：
//   1. ConversationSignalDetector.convert_trajectory_to_messages(trajectory) 获取 raw
//   2. _normalize_callback_messages(raw) 规范化为 JSON 安全的 dict
//   3. 去重（message not in deduped）
func collectMessagesFromTrajectory(traj *trajectory.Trajectory) []map[string]any {
	if traj == nil {
		return []map[string]any{}
	}
	// 对齐 Python: raw = ConversationSignalDetector.convert_trajectory_to_messages(trajectory)
	detector := &signal.ConversationSignalDetector{}
	raw := detector.ConvertTrajectoryToMessages(traj)

	// 对齐 Python: normalized = cls._normalize_callback_messages(raw)
	normalized := normalizeCallbackMessages(raw)

	// 对齐 Python: deduped = []; for message in normalized: if message not in deduped: deduped.append(message)
	deduped := make([]map[string]any, 0, len(normalized))
	for _, msg := range normalized {
		if !containsMessage(deduped, msg) {
			deduped = append(deduped, msg)
		}
	}
	return deduped
}

// normalizeCallbackMessages 将回调可见的消息规范化为 JSON 安全的字典列表。
//
// 对齐 Python: _normalize_callback_messages(messages)
//
// Python 实现：
//   - dict 消息直接使用
//   - 对象消息：提取 role/content/tool_calls/name 字段
//   - tool_calls 转为 [{id, name, arguments}] 格式
//
// Go 中 ConvertTrajectoryToMessages 返回的已经是 []map[string]any，
// 所以大部分规范化已在 signal 包完成。这里只做浅拷贝防止外部修改。
func normalizeCallbackMessages(messages []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(messages))
	for _, msg := range messages {
		// 浅拷贝，防止调用方修改原始数据
		copied := map[string]any{}
		for k, v := range msg {
			copied[k] = v
		}
		result = append(result, copied)
	}
	return result
}

// containsMessage 检查消息列表中是否已存在相同消息。
//
// 对齐 Python: message not in deduped（dict 的 __eq__ 比较）
func containsMessage(list []map[string]any, msg map[string]any) bool {
	msgJSON, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	for _, existing := range list {
		existingJSON, err := json.Marshal(existing)
		if err != nil {
			continue
		}
		if string(msgJSON) == string(existingJSON) {
			return true
		}
	}
	return false
}

// formatSkillName 格式化技能名称用于日志和快照键。
func formatSkillName(snapshot *EvolutionSnapshot) string {
	if snapshot == nil || snapshot.SkillName == nil {
		return "unknown"
	}
	return *snapshot.SkillName
}

// ensureNonNilSlice 确保切片不为 nil（返回空切片替代 nil）。
func ensureNonNilSlice[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// ensureNonNilMap 确保映射不为 nil（返回空映射替代 nil）。
func ensureNonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// baseMessageToMap 将 BaseMessage 转换为 map[string]any 表示。
//
// BaseMessage 接口没有 ToMap/ToDict 方法，需要从接口字段手动构建。
// 对齐 Python: BaseMessage.model_dump() 或 OpenAI dict 格式。
func baseMessageToMap(msg llmschema.BaseMessage) map[string]any {
	if msg == nil {
		return map[string]any{}
	}
	result := map[string]any{
		"role":    msg.GetRole().String(),
		"content": msg.GetContent(),
	}
	if name := msg.GetName(); name != "" {
		result["name"] = name
	}
	if meta := msg.GetMetadata(); len(meta) > 0 {
		result["metadata"] = meta
	}
	return result
}

// toolInfoToMap 将 ToolInfoInterface 转换为 map[string]any 表示。
//
// ToolInfoInterface 接口没有 ToMap 方法，需要从接口字段手动构建。
// 对齐 Python: ToolInfo.model_dump() 或 OpenAI tool dict 格式。
func toolInfoToMap(tool cschema.ToolInfoInterface) map[string]any {
	if tool == nil {
		return map[string]any{}
	}
	return map[string]any{
		"type":        tool.GetType(),
		"name":        tool.GetName(),
		"description": tool.GetDescription(),
		"parameters":  tool.GetParameters(),
	}
}

// fmtErrorf 格式化错误（避免导入 fmt 仅用于 Errorf）。
// 此函数仅为代码简洁，如果文件已导入 fmt 则直接使用 fmt.Errorf。
func fmtErrorf(format string, args ...any) error {
	return fmt.Errorf(format, args...)
}
