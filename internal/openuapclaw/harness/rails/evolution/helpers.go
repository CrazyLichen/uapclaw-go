package evolution

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
)

// ──────────────────────────── 非导出函数 ────────────────────────────

// splitResponseTokenFields 从 LLM 响应中分离 token 级字段。
// 返回 (response_for_detail, prompt_token_ids, completion_token_ids, logprobs)。
// 返回的 response_for_detail 已移除这三个字段，避免在轨迹中重复存储。
// Python: _split_response_token_fields(response)
//
// Python 实现：
//   - 如果 response 有 model_dump() 方法，调用得到 dict
//   - 从 dict 中 pop 出 prompt_token_ids / completion_token_ids / logprobs
//   - 如果 response 是 dict 直接操作
//   - 其他形状原样返回
//
// Go 中 AssistantMessage 直接暴露 PromptTokenIDs / CompletionTokenIDs / Logprobs 字段，
// 同时有 ToOpenAIDict() 方法获取完整 map 表示。
func splitResponseTokenFields(response *llmschema.AssistantMessage) (map[string]any, []int, []int, []map[string]any) {
	if response == nil {
		return nil, nil, nil, nil
	}

	// 从结构体字段直接提取 token 级数据
	promptTokenIDs := response.PromptTokenIDs
	completionTokenIDs := response.CompletionTokenIDs
	// 尝试将 Logprobs 转为 []map[string]any
	var logprobs []map[string]any
	if response.Logprobs != nil {
		if lp, ok := response.Logprobs.([]map[string]any); ok {
			logprobs = lp
		}
		// 非标准格式不赋值，丢弃
	}

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

// normalizeSkillNames 将技能名称切片规范化为集合。
// 前后空格会被裁剪，空项会被跳过。
// Python: _normalize_skill_names(raw)
// Python 中 raw 可以是 str 或 list，Go 版本统一接受 []string（单个 string 包裹为 []string{s}）。
func normalizeSkillNames(names []string) map[string]bool {
	result := map[string]bool{}
	for _, s := range names {
		name := strings.TrimSpace(s)
		if name != "" {
			result[name] = true
		}
	}
	return result
}

// normalizeMemberRole 将成员角色规范化为稳定字符串值。
// Python: _normalize_member_role(role)
//
// Python 实现：role 为 None 返回 None；getattr(role, "value", role) 获取枚举值；
// str(role_value) 转字符串；空字符串返回 None。
// Go 版本统一接受 string（枚举类型调用方自行 String() 转换后传入）。
func normalizeMemberRole(role string) *string {
	if role == "" {
		return nil
	}
	return &role
}

// trajectoryToMessages 把规范轨迹投影为脱离的消息列表（显式字段集）。
// Python: EvolutionRail._trajectory_to_messages（evolution_rail.py:1008）——
// v2 切换后消息唯一来源是 span 重放（trajectory_to_messages），
// 旧 _collect_messages_from_trajectory（signal detector + 规范化 + 去重）
// 已在 Python v2 切换（09-24 cutover）中删除，Go 同改动删除（规则 8）。
func trajectoryToMessages(traj *trajectory.Trajectory) []map[string]any {
	if traj == nil {
		return []map[string]any{}
	}
	messages, err := trajectory.TrajectoryToMessages(traj, trajectory.DefaultEvolutionMessageFields, false)
	if err != nil {
		// Python trajectory_to_messages 无错误路径（字段集为闭集常量）；
		// Go 侧错误仅在字段集非法时出现，防御性记录并返回空。
		logger.Warn(logComponent).Err(err).Msg("trajectory message replay failed")
		return []map[string]any{}
	}
	return messages
}

// formatSkillName 格式化技能名称用于日志和快照键。
func formatSkillName(snapshot *EvolutionSnapshot) string {
	if snapshot == nil || snapshot.SkillName == nil {
		return "unknown"
	}
	return *snapshot.SkillName
}

// ensureNonNilMap 确保映射不为 nil（返回空映射替代 nil）。
func ensureNonNilMap(m map[string]any) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	return m
}

// baseMessageToMap 将 BaseMessage 转换为 map[string]any 表示。
// BaseMessage 接口没有 ToMap/ToDict 方法，需要从接口字段手动构建。
// Python: BaseMessage.model_dump() 或 OpenAI dict 格式。
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
// ToolInfoInterface 接口没有 ToMap 方法，需要从接口字段手动构建。
// Python: ToolInfo.model_dump() 或 OpenAI tool dict 格式。
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
