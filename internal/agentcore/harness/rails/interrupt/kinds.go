// Package interrupt 的中断类型判定与交互过滤。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/interrupt/interrupt_helpers.py
// 中与「中断类型 / 来源 / 身份 / 问答交互」相关的助手（L965-1170）。
//
// 这些助手是 9.90e 中断投影链的前置件：
// stream_event_rail 的 _project_* 系列要判断「这条中断是不是权限中断」
// 「哪些交互是真正要问用户的」，都靠这里。
package interrupt

import (
	"strings"

	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

const (
	// PermissionRuntimeQuarantinedKey 会话隔离标记在 session state 中的键。
	//
	// Python: PERMISSION_RUNTIME_QUARANTINED_KEY (permission_interaction.py L10-12)
	//
	// 键名含 v1 后缀用于将来语义变更时平滑迁移；字面量必须与 Python 一字不差，
	// 否则跨语言共享的会话状态读不到。品牌前缀 jiuwenswarm 不映射。
	//
	// 放在本包而非 permissions 包：rails 包（stream_event_rail）需要读写这个键，
	// 但 rails → rails/permissions 会成环（permissions → rails 已存在）。
	PermissionRuntimeQuarantinedKey = "_jiuwenswarm_permission_runtime_quarantined_v1"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sourceByKind 中断类型到来源标记的映射。
	//
	// Python: _SOURCE_BY_KIND (interrupt_helpers.py L1127-1130)
	//
	// 只有 PERMISSION 与 CONFIRM 两个已知审批类；
	// ask_user / client_tool 等不在表内，走措辞判断。
	sourceByKind = map[string]string{
		saschema.InterruptKindPermission: "permission_interrupt",
		saschema.InterruptKindConfirm:    "confirm_interrupt",
	}

	// approvalKinds 审批类中断集合。
	//
	// Python: _APPROVAL_KINDS (interrupt_helpers.py L1131-1136)
	approvalKinds = map[string]bool{
		saschema.InterruptKindPermission:             true,
		saschema.InterruptKindConfirm:                true,
		saschema.InterruptKindSkillEvolutionApproval: true,
		saschema.InterruptKindTeamToolApproval:       true,
	}

	// permissionInterruptMarkers 权限中断的措辞特征。
	//
	// Python: _PERMISSION_INTERRUPT_MARKERS (interrupt_helpers.py L1056-1068)
	//
	// 用于 kind 未声明时的启发式回退——区分 PermissionInterruptRail 的文案
	// 与 ConfirmInterruptRail 的文案。文案本身必须与 Python 一字不差。
	permissionInterruptMarkers = []string{
		"需要授权才能执行",
		"需要授权后才能使用",
		"需要确认后才能执行",
		"检测到受保护的文件路径访问",
		"检测到需确认的网络访问",
		"检测到需确认的命令执行",
		"检测到风险命令结构",
		"操作需要授权",
		"requires permission",
		"Permission denied",
		"安全风险评估",
	}

	// confirmInterruptTools 走确认类中断的工具名白名单。
	//
	// Python: _CONFIRM_INTERRUPT_TOOLS (interrupt_helpers.py L1070)
	confirmInterruptTools = map[string]bool{
		"switch_mode":    true,
		"exit_plan_mode": true,
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// QuestionInteractions 返回一次暂停里真正「问用户」的交互。
//
// Python: question_interactions(state_outputs) (interrupt_helpers.py L1159-1169)
//
// 客户端工具调用不算问题：它有自己的 chat.client_tool_request 事件。
func QuestionInteractions(inputs []map[string]any) []map[string]any {
	result := make([]map[string]any, 0, len(inputs))
	for _, item := range inputs {
		_, valueObj := extractInteractionParts(item)
		if interruptKindOfValue(valueObj) == saschema.InterruptKindClientTool {
			continue
		}
		result = append(result, item)
	}
	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// interruptKindOfValue 返回请求或它 dict 形态的 interrupt_kind，未声明时返回空串。
//
// Python: _interrupt_kind(value_obj) (interrupt_helpers.py L965-976)
//
// 字段存在之前持久化的技能演化审批数据把 kind 只存在 metadata["interrupt_kind"]，
// 故先读字段、读不到再回落 metadata。
func interruptKindOfValue(valueObj any) string {
	kind := readValueField(valueObj, "interrupt_kind")
	if kind == "" {
		metadata := extractInterruptMetadata(valueObj)
		if raw, ok := metadata["interrupt_kind"].(string); ok {
			kind = raw
		}
	}
	return kind
}

// interruptIDOfValue 返回请求或它 dict 形态的 interrupt_id，未分配时返回空串。
//
// Python: _interrupt_id(value_obj) (interrupt_helpers.py L979-982)
func interruptIDOfValue(valueObj any) string {
	return readValueField(valueObj, "interrupt_id")
}

// resolveInterruptSource 返回审批类中断的来源标记。
//
// Python: _resolve_interrupt_source(value_obj) (interrupt_helpers.py L1139-1153)
//
// 先按 kind 查表；kind 未声明（历史会话、不声明 kind 的 rail）时按措辞判断。
func resolveInterruptSource(valueObj any) string {
	if source, ok := sourceByKind[interruptKindOfValue(valueObj)]; ok {
		return source
	}
	toolName, message, _ := readInterruptFields(valueObj)
	if isPermissionInterruptMessage(message, toolName) {
		return "permission_interrupt"
	}
	return "confirm_interrupt"
}

// interruptIdentity 返回宿主应答中断（interrupt_id）与渲染中断（kind）所用的字段。
//
// Python: _interrupt_identity(value_obj) (interrupt_helpers.py L1154-1158)
func interruptIdentity(valueObj any) map[string]string {
	return map[string]string{
		"interrupt_id": interruptIDOfValue(valueObj),
		"kind":         interruptKindOfValue(valueObj),
	}
}

// isPermissionInterruptMessage 启发式判断：这条文案是权限中断还是确认中断。
//
// Python: _is_permission_interrupt_message(message, tool_name)
// (interrupt_helpers.py L1101-1112)
//
// 四段判定顺序不可调换：
//  1. 含权限特征词
//  2. 以 "**工具 `" 或 "**Tool `" 开头
//  3. tool_name 非空且不在确认工具白名单
//  4. 文案为空或恰为 "Please approve or reject?" 时，看工具是否在确认白名单
func isPermissionInterruptMessage(message string, toolName string) bool {
	normalized := strings.TrimSpace(message)
	for _, marker := range permissionInterruptMarkers {
		if strings.Contains(normalized, marker) {
			return true
		}
	}
	if strings.HasPrefix(normalized, "**工具 `") || strings.HasPrefix(normalized, "**Tool `") {
		return true
	}
	if toolName != "" && !confirmInterruptTools[toolName] {
		return true
	}
	if normalized == "" || normalized == "Please approve or reject?" {
		return !confirmInterruptTools[toolName]
	}
	return false
}

// readInterruptFields 从中断值对象中读 (tool_name, message, tool_args)。
//
// Python: _read_interrupt_fields(value_obj) (interrupt_helpers.py L1073-1099)
//
// message 缺失时回落 question 字段（AskUser 类中断用 question）。
func readInterruptFields(valueObj any) (string, string, map[string]any) {
	mapping, ok := valueObj.(map[string]any)
	if !ok {
		return "", "", nil
	}
	toolName := strings.TrimSpace(stringValueOf(mapping["tool_name"]))
	message := strings.TrimSpace(stringValueOf(mapping["message"]))
	if message == "" {
		message = strings.TrimSpace(stringValueOf(mapping["question"]))
	}
	var toolArgs map[string]any
	if typed, ok := mapping["tool_args"].(map[string]any); ok {
		toolArgs = typed
	}
	return toolName, message, toolArgs
}

// readValueField 读取 dict 形态 value 上的字符串字段，非字符串或缺失返回空串。
//
// Python: _read_value_field(value_obj, field_name, default) (interrupt_helpers.py L955-962)
func readValueField(valueObj any, fieldName string) string {
	mapping, ok := valueObj.(map[string]any)
	if !ok {
		return ""
	}
	raw, ok := mapping[fieldName].(string)
	if !ok {
		return ""
	}
	return raw
}

// iterInteractions 从列表或完整 SDK 状态 schema 中展开出交互对象。
//
// Python: _iter_interactions(state_outputs) (interrupt_helpers.py L1361-1373)
//
// 三种展开规则（顺序即优先级）：
//  1. 元素本身是 list/tuple → 递归展开
//  2. 元素是 {"type": "__interaction__", "payload": X} → 递归展开 X
//  3. 其余原样产出
func iterInteractions(stateOutputs []any) []any {
	result := make([]any, 0, len(stateOutputs))
	for _, interaction := range stateOutputs {
		switch typed := interaction.(type) {
		case []any:
			result = append(result, iterInteractions(typed)...)
		case map[string]any:
			if typed["type"] == "__interaction__" {
				payload := typed["payload"]
				result = append(result, iterInteractions([]any{payload})...)
				continue
			}
			result = append(result, typed)
		default:
			result = append(result, interaction)
		}
	}
	return result
}

// extractInterruptMetadata 从 dict 形态 value 中取 metadata。//
// Python: _extract_interrupt_metadata(value_obj)
func extractInterruptMetadata(valueObj any) map[string]any {
	mapping, ok := valueObj.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	metadata, ok := mapping["metadata"].(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return metadata
}

// stringValueOf 把值转成字符串，nil 返回空串。
func stringValueOf(value any) string {
	if value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return text
	}
	return ""
}
