package permissions

import (
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolCapability 工具的安全相关能力事实（不可变）。
//
// 本值只描述「这个工具做什么」，**从不**用于直接授予权限。
//
// Python: @dataclass(frozen=True) ToolCapability
type ToolCapability struct {
	// ToolName 归一化后的规范名
	ToolName string
	// RegisteredName 调用方传入的原始名
	RegisteredName string
	// Aliases 指向同一规范名且不等于自身的别名（已排序）
	Aliases []string
	// Category 工具大类（shell / code / path / network / ...）
	Category string
	// OperationFamily 操作族（dynamic_execution / workspace_read / ...）
	OperationFamily string
	// ResourceClass 资源类别（command / workspace_path / ...）
	ResourceClass string
	// StaticSideEffects 静态副作用集合
	StaticSideEffects map[string]struct{}
	// RiskTier 风险档（low / medium / high）
	RiskTier string
	// HighFlex 是否高弹性（可执行任意操作）
	HighFlex bool
	// DisplayKey 展示用键
	DisplayKey string
	// RedactionKey 脱敏用键
	RedactionKey string
	// FactsSource 事实来源
	FactsSource string
	// FactsVersion 事实版本
	FactsVersion string
	// AliasConflict 别名图是否歧义
	AliasConflict bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// ToolCapabilityFactsVersion 能力事实版本
	//
	// Python: TOOL_CAPABILITY_FACTS_VERSION
	ToolCapabilityFactsVersion = "2"
	// HostStaticFacts 事实来自宿主静态表（权威）
	//
	// Python: HOST_STATIC_FACTS
	HostStaticFacts = "host_static"
	// NameClassificationHint 事实来自名字推断（非权威）
	//
	// Python: NAME_CLASSIFICATION_HINT
	NameClassificationHint = "name_classification_hint"
	// UnknownFacts 事实未知（fail-closed）
	//
	// Python: UNKNOWN_FACTS
	UnknownFacts = "unknown"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// shellTools shell 类工具
	//
	// Python: _SHELL_TOOLS
	shellTools = toSet(
		"bash", "cmd", "powershell", "mcp_exec_command", "create_terminal",
	)

	// codeTools code 类工具
	//
	// Python: _CODE_TOOLS
	codeTools = toSet("code")

	// pathTools 路径类工具（注意比 security.PathTools 多 apply_patch / lsp / read_pdf）
	//
	// Python: _PATH_TOOLS
	pathTools = toSet(
		"apply_patch", "lsp", "read_file", "read_pdf",
		"write_file", "edit_file", "read_text_file", "write_text_file",
		"write", "read", "glob_file_search", "glob",
		"list_dir", "list_files", "grep", "search_replace",
	)

	// publicSearchTools 公开搜索工具
	//
	// Python: _PUBLIC_SEARCH_TOOLS
	publicSearchTools = toSet("mcp_free_search", "mcp_paid_search")

	// publicWebReadTools 公开网页读取工具
	//
	// Python: _PUBLIC_WEB_READ_TOOLS
	publicWebReadTools = toSet("mcp_fetch_webpage")

	// subagentTools subagent 控制工具
	//
	// Python: _SUBAGENT_TOOLS
	subagentTools = toSet("task_tool", "spawn_sub_agent", "session_new")

	// subagentRuntimeControlTools subagent 运行时控制工具
	//
	// Python: _SUBAGENT_RUNTIME_CONTROL_TOOLS
	subagentRuntimeControlTools = toSet(
		"subagent_close", "subagent_list", "subagent_resume",
		"subagent_send_input", "subagent_spawn", "subagent_wait",
	)

	// hostTodoTools 宿主 todo 工具
	//
	// Python: _HOST_TODO_TOOLS
	hostTodoTools = toSet("todo_create", "todo_list", "todo_get", "todo_modify")

	// legacyTodoTools 遗留 todo 工具
	//
	// Python: _LEGACY_TODO_TOOLS
	legacyTodoTools = toSet("todo_insert", "todo_complete", "todo_remove")

	// sessionStatusTools 会话状态工具
	//
	// Python: _SESSION_STATUS_TOOLS
	sessionStatusTools = toSet("session_list", "session_message_list", "session_read")

	// sessionSendTools 跨会话发送工具
	//
	// Python: _SESSION_SEND_TOOLS
	sessionSendTools = toSet("session_send_message")

	// sessionMessageManagementTools 跨会话消息管理工具
	//
	// Python: _SESSION_MESSAGE_MANAGEMENT_TOOLS
	sessionMessageManagementTools = toSet("session_message_resolve", "session_continue_queued")

	// internalReadonlyTools 内部只读工具
	//
	// Python: _INTERNAL_READONLY_TOOLS
	internalReadonlyTools = toSet(
		"cron_list_jobs", "cron_get_job", "cron_preview_job",
		"heartbeat_list_jobs", "heartbeat_get_job", "heartbeat_preview_job",
		"read_terminal_output", "wait_for_terminal_exit", "convert_timestamp_to_utc8_time",
		"tool_search", "skill_index", "list_skill", "get_current_goal",
		"recall_compressed_context",
	)

	// askUserTools 用户交互工具
	//
	// Python: _ASK_USER_TOOLS
	askUserTools = toSet("ask_user")

	// skillReadTools skill 读取工具
	//
	// Python: _SKILL_READ_TOOLS
	skillReadTools = toSet("skill_tool")

	// skillChangeTools skill 变更工具
	//
	// Python: _SKILL_CHANGE_TOOLS
	skillChangeTools = toSet("install_skill", "uninstall_skill")

	// skillDiscoveryTools skill 发现工具
	//
	// Python: _SKILL_DISCOVERY_TOOLS
	skillDiscoveryTools = toSet("search_skill")

	// externalSendTools 外部发送工具
	//
	// Python: _EXTERNAL_SEND_TOOLS
	externalSendTools = toSet(
		"send_file_to_user", "upload_file", "upload_photo", "send_message",
	)

	// deviceTools 设备操作工具
	//
	// Python: _DEVICE_TOOLS
	deviceTools = toSet("call_phone", "save_media_to_gallery", "save_file_to_file_manager")

	// memoryWriteTools 记忆写入工具
	//
	// Python: _MEMORY_WRITE_TOOLS
	memoryWriteTools = toSet(
		"write_memory", "edit_memory", "viking_remember",
		"viking_add_resource", "mem0_conclude",
		"coding_memory_write", "coding_memory_edit",
	)

	// localMemoryReadTools 本地记忆读取工具
	//
	// Python: _LOCAL_MEMORY_READ_TOOLS
	localMemoryReadTools = toSet("memory_get", "memory_search", "read_memory")

	// reviewedMemoryReadTools 需审查的记忆读取工具
	//
	// Python: _REVIEWED_MEMORY_READ_TOOLS
	reviewedMemoryReadTools = toSet(
		"coding_memory_read", "ltm_search", "ltm_search_summary",
		"mem0_profile", "mem0_search",
		"viking_browse", "viking_read", "viking_search",
	)

	// fixedBrowserTools 固定 browser 工具
	//
	// Python: _FIXED_BROWSER_TOOLS
	fixedBrowserTools = toSet(
		"browser_phase", "browser_probe_cards",
		"browser_probe_interactives", "browser_recall_offload",
	)

	// dynamicRepresentativeTools 动态代表工具（参与 staticCanonical 集合构建）
	//
	// Python: _DYNAMIC_REPRESENTATIVE_TOOLS
	dynamicRepresentativeTools = toSet(
		"browser_click", "browser_open", "browser_run_task", "browser_snapshot",
		"cron_create_job", "cron_delete_job", "cron_preview_job",
		"mcp_unknown_tool", "todo_create", "todo_list",
	)

	// trustedBrowserMCPServerPrefixes trusted playwright MCP 服务器前缀
	//
	// Python: _TRUSTED_BROWSER_MCP_SERVER_PREFIXES
	trustedBrowserMCPServerPrefixes = []string{
		"mcp_playwright-official_browser_",
		"mcp_playwright_official_browser_",
	}

	// trustedBrowserMCPToolSuffixes trusted playwright MCP 工具后缀
	//
	// Python: _TRUSTED_BROWSER_MCP_TOOL_SUFFIXES
	trustedBrowserMCPToolSuffixes = toSet(
		"click", "close", "console_messages", "drag", "drop",
		"evaluate", "file_upload", "fill_form", "handle_dialog",
		"hover", "navigate", "navigate_back", "network_request",
		"network_requests", "press_key", "route", "resize",
		"run_code_unsafe", "select_option", "snapshot", "tabs",
		"take_screenshot", "type", "unroute", "wait_for",
	)

	// reviewedMemorySearchTools 归入 memory_search 的需审查工具
	reviewedMemorySearchTools = toSet("ltm_search", "ltm_search_summary", "mem0_search", "viking_search")

	// pathWriteTools 路径写类工具
	pathWriteTools = toSet(
		"apply_patch", "write_file", "edit_file",
		"write_text_file", "write", "search_replace",
	)

	// staticCanonicalToolNames 所有静态已知的规范名。init 中构建。
	staticCanonicalToolNames map[string]struct{}
)

// init 构建 staticCanonicalToolNames。
//
// 构成（对齐 Python _STATIC_CANONICAL_TOOL_NAMES）：24 个集合的并集 +
// permission_tools 的 4 个别名目标 + xiaoyi_gui_agent。
func init() {
	sets := []map[string]struct{}{
		shellTools, codeTools, pathTools, publicSearchTools, publicWebReadTools,
		subagentTools, subagentRuntimeControlTools, hostTodoTools, legacyTodoTools,
		sessionStatusTools, sessionSendTools, sessionMessageManagementTools,
		internalReadonlyTools, askUserTools, skillReadTools, skillChangeTools,
		skillDiscoveryTools, externalSendTools, deviceTools, memoryWriteTools,
		localMemoryReadTools, reviewedMemoryReadTools, fixedBrowserTools,
		dynamicRepresentativeTools,
	}
	total := 5
	for _, m := range sets {
		total += len(m)
	}
	staticCanonicalToolNames = make(map[string]struct{}, total)

	// Python: set(PERMISSION_TOOL_ALIASES.values()) —— 4 个别名目标
	for _, target := range []string{
		"mcp_exec_command", "mcp_fetch_webpage",
		"mcp_free_search", "mcp_paid_search",
	} {
		staticCanonicalToolNames[target] = struct{}{}
	}
	for _, m := range sets {
		for k := range m {
			staticCanonicalToolNames[k] = struct{}{}
		}
	}
	staticCanonicalToolNames["xiaoyi_gui_agent"] = struct{}{}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ShellToolNames 返回 shell 类工具名，已排序。
//
// Python: shell_tool_names()
func ShellToolNames() []string {
	out := make([]string, 0, len(shellTools))
	for name := range shellTools {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// NormalizeToolName 把前端或别名工具名归一化为策略工具名。
//
// Python: normalize_tool_name(tool_name)
func NormalizeToolName(toolName string) string {
	return permission_tools.NormalizePermissionToolName(toolName)
}

// ClassifyTool 返回规范化工具名的能力事实。
//
// 28 个分支的顺序即优先级，不可调换。三处最易错：
//  1. 内部只读（分支 2）先于 cron_ 前缀（分支 14）——cron_list_jobs 走
//     internal_readonly/low，只有非只读的 cron_* 才落到 schedule_change/high
//  2. subagent 运行时控制要求**精确无别名**的名字（分支 15），不能只看集合成员
//  3. 只有 task_tool 的 subagent_control 是权威事实（分支 16），
//     spawn_sub_agent / session_new 都是非权威
//
// Python: classify_tool(tool_name)
func ClassifyTool(toolName string) ToolCapability {
	canonicalNames := make([]string, 0, len(staticCanonicalToolNames))
	for name := range staticCanonicalToolNames {
		canonicalNames = append(canonicalNames, name)
	}
	resolution := permission_tools.ResolvePermissionToolName(toolName, canonicalNames)
	normalized := resolution.CanonicalName
	lowered := strings.ToLower(normalized)

	// 1. 别名冲突 → fail-closed
	if resolution.Conflict {
		return newCapability(resolution, "unknown", "unknown", "unknown", nil, "high", true, true)
	}
	// 2. 内部只读（必须先于 cron_ 前缀分支）
	if _, hit := internalReadonlyTools[toolName]; hit && len(resolution.Aliases) == 0 {
		category := "task_management"
		if strings.HasPrefix(lowered, "cron_") {
			category = "cron"
		}
		return newCapability(resolution, category, "internal_readonly", "internal_state", nil, "low", false, true)
	}
	// 3. shell
	if _, hit := shellTools[lowered]; hit {
		return newCapability(resolution, "shell", "dynamic_execution", "command",
			toSet("dynamic_exec"), "high", true, true)
	}
	// 4. code
	if _, hit := codeTools[lowered]; hit {
		return newCapability(resolution, "code", "dynamic_execution", "code",
			toSet("dynamic_exec"), "high", true, true)
	}
	// 5. path（读写能力不同）
	if _, hit := pathTools[lowered]; hit {
		var sideEffects map[string]struct{}
		risk := "low"
		family := "workspace_read"
		if isPathWriteTool(lowered) {
			sideEffects = toSet("write_local")
			risk = "medium"
			family = "workspace_write"
		}
		return newCapability(resolution, "path", family, "workspace_path", sideEffects, risk, false, true)
	}
	// 6. 公开搜索
	if _, hit := publicSearchTools[lowered]; hit {
		return newCapability(resolution, "network", "public_search", "public_web",
			toSet("network_search"), "medium", false, true)
	}
	// 7. 公开网页读取
	if _, hit := publicWebReadTools[lowered]; hit {
		return newCapability(resolution, "network", "public_web_read", "public_web",
			toSet("network_read"), "medium", false, true)
	}
	// 8. 宿主 todo
	if _, hit := hostTodoTools[lowered]; hit {
		return newCapability(resolution, "task_management", "task_state", "session_task_state", nil, "low", false, true)
	}
	// 9. 遗留 todo（非权威）
	if _, hit := legacyTodoTools[lowered]; hit {
		return newCapability(resolution, "task_management", "task_state", "session_task_state", nil, "medium", true, false)
	}
	// 10. 会话状态
	if _, hit := sessionStatusTools[lowered]; hit {
		return newCapability(resolution, "task_management", "session_status", "session_task_state", nil, "low", false, true)
	}
	// 11. 跨会话发送
	if _, hit := sessionSendTools[lowered]; hit {
		return newCapability(resolution, "task_management", "cross_session_send", "product_session",
			toSet("delegation"), "high", true, true)
	}
	// 12. 跨会话消息管理
	if _, hit := sessionMessageManagementTools[lowered]; hit {
		return newCapability(resolution, "task_management", "cross_session_resolution", "product_session",
			toSet("session_state_write"), "high", true, true)
	}
	// 13. 用户交互
	if _, hit := askUserTools[lowered]; hit {
		return newCapability(resolution, "interaction", "ask_user", "user_interaction", nil, "low", false, true)
	}
	// 14. cron 变更（非权威）
	if strings.HasPrefix(lowered, "cron_") {
		return newCapability(resolution, "cron", "schedule_change", "schedule",
			toSet("schedule"), "high", true, false)
	}
	// 15. subagent 运行时控制（必须精确且无别名）
	if isExactSubagentRuntimeControl(toolName, resolution) {
		return newCapability(resolution, "subagent", "subagent_runtime_control", "agent_runtime",
			toSet("delegation"), "low", false, true)
	}
	// 16. subagent 控制（只有 task_tool 权威）
	if _, hit := subagentTools[lowered]; hit {
		return newCapability(resolution, "subagent", "subagent_control", "agent_runtime",
			toSet("delegation"), "high", true, lowered == "task_tool")
	}
	// 17. 固定 browser 工具（browser_recall_offload 是特例）
	if _, hit := fixedBrowserTools[lowered]; hit {
		family := "browser_session_observe"
		var sideEffects map[string]struct{} = toSet("session_cache_write")
		if lowered == "browser_recall_offload" {
			family = "browser_session_recall"
			sideEffects = nil
		}
		return newCapability(resolution, "browser", family, "browser_session", sideEffects, "low", false, true)
	}
	// 18. 动态 browser 工具（非权威）
	if strings.HasPrefix(lowered, "browser_") ||
		lowered == "xiaoyi_gui_agent" ||
		isTrustedBrowserMCPTool(lowered) {
		return newCapability(resolution, "browser", "browser_action", "browser_session",
			toSet("browser_action"), "high", true, false)
	}
	// 19. skill 发现
	if _, hit := skillDiscoveryTools[lowered]; hit {
		return newCapability(resolution, "skill", "skill_discovery", "skill_catalog", nil, "medium", false, true)
	}
	// 20. skill 读取
	if _, hit := skillReadTools[lowered]; hit {
		return newCapability(resolution, "skill", "skill_read", "installed_skill", nil, "low", false, true)
	}
	// 21. skill 变更
	if _, hit := skillChangeTools[lowered]; hit {
		return newCapability(resolution, "skill", "skill_change", "skill_catalog",
			toSet("skill_change"), "high", true, true)
	}
	// 22. 外部发送
	if _, hit := externalSendTools[lowered]; hit {
		return newCapability(resolution, "external_send", "external_send", "external_recipient",
			toSet("external_send"), "high", true, true)
	}
	// 23. 设备操作
	if _, hit := deviceTools[lowered]; hit {
		return newCapability(resolution, "device", "device_action", "user_device",
			toSet("device_action"), "high", true, true)
	}
	// 24. 记忆写入
	if _, hit := memoryWriteTools[lowered]; hit {
		return newCapability(resolution, "memory", "memory_write", "memory_store",
			toSet("memory_write"), "high", true, true)
	}
	// 25. 本地记忆读取
	if _, hit := localMemoryReadTools[lowered]; hit {
		family := "memory_read"
		if lowered == "memory_search" {
			family = "memory_search"
		}
		return newCapability(resolution, "memory", family, "memory_store", nil, "low", false, true)
	}
	// 26. 需审查的记忆读取（非权威）
	if _, hit := reviewedMemoryReadTools[lowered]; hit {
		family := "memory_read"
		if _, isSearch := reviewedMemorySearchTools[lowered]; isSearch {
			family = "memory_search"
		}
		return newCapability(resolution, "memory", family, "memory_store", nil, "medium", true, false)
	}
	// 27. 未知 mcp（非权威）
	if strings.HasPrefix(lowered, "mcp_") {
		return newCapability(resolution, "mcp", "unknown_mcp", "external_resource", nil, "high", true, false)
	}
	// 28. 兜底（非权威）
	return newCapability(resolution, "unknown", "unknown", "unknown", nil, "high", true, false)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newCapability 组装 ToolCapability。
//
// factsSource 的三元推导（顺序不可调换）：冲突或 unknown → unknown；
// 否则看 authoritative：真 → host_static，假 → name_classification_hint。
//
// Python: _capability(resolution, category, operation_family, resource_class,
//
//	static_side_effects, risk_tier, high_flex, *, authoritative=True)
func newCapability(
	resolution permission_tools.PermissionToolNameResolution,
	category, operationFamily, resourceClass string,
	staticSideEffects map[string]struct{},
	riskTier string,
	highFlex bool,
	authoritative bool,
) ToolCapability {
	factsSource := NameClassificationHint
	if resolution.Conflict || category == "unknown" {
		factsSource = UnknownFacts
	} else if authoritative {
		factsSource = HostStaticFacts
	}
	return ToolCapability{
		ToolName:          resolution.CanonicalName,
		RegisteredName:    resolution.RegisteredName,
		Aliases:           resolution.Aliases,
		Category:          category,
		OperationFamily:   operationFamily,
		ResourceClass:     resourceClass,
		StaticSideEffects: staticSideEffects,
		RiskTier:          riskTier,
		HighFlex:          highFlex,
		DisplayKey:        "tool." + category + "." + operationFamily,
		RedactionKey:      redactionKey(resourceClass),
		FactsSource:       factsSource,
		FactsVersion:      ToolCapabilityFactsVersion,
		AliasConflict:     resolution.Conflict,
	}
}

// redactionKey 按资源类别给出脱敏键。
//
// Python: _redaction_key(resource_class)
func redactionKey(resourceClass string) string {
	switch resourceClass {
	case "command", "code":
		return "arguments_sensitive"
	case "workspace_path", "browser_session", "installed_skill", "memory_store":
		return "resource_sensitive"
	case "public_web", "external_recipient":
		return "network_sensitive"
	case "unknown", "external_resource":
		return "full_sensitive"
	}
	return "default"
}

// isPathWriteTool 判定路径类工具是否为写操作。
//
// Python: _is_path_write_tool(tool_name)
func isPathWriteTool(toolName string) bool {
	_, hit := pathWriteTools[toolName]
	return hit
}

// isTrustedBrowserMCPTool 判定是否为 trusted playwright MCP 工具。
//
// Python: _is_trusted_browser_mcp_tool(tool_name)
func isTrustedBrowserMCPTool(toolName string) bool {
	for _, prefix := range trustedBrowserMCPServerPrefixes {
		if strings.HasPrefix(toolName, prefix) {
			suffix := strings.TrimPrefix(toolName, prefix)
			_, hit := trustedBrowserMCPToolSuffixes[suffix]
			return hit
		}
	}
	return false
}

// isExactSubagentRuntimeControl 只接受无别名的精确 SDK 运行时控制名。
//
// 四个条件（对齐 Python _is_exact_subagent_runtime_control）：
//  1. 原始名是字符串且非空
//  2. 原始名在 runtime control 集合里
//  3. registered_name == 原始名（未被裁剪改变）
//  4. canonical_name == 原始名（未被别名映射改变）且无别名
//
// Python: _is_exact_subagent_runtime_control(tool_name, resolution)
func isExactSubagentRuntimeControl(toolName string, resolution permission_tools.PermissionToolNameResolution) bool {
	if toolName == "" {
		return false
	}
	if _, hit := subagentRuntimeControlTools[toolName]; !hit {
		return false
	}
	if resolution.RegisteredName != toolName {
		return false
	}
	if resolution.CanonicalName != toolName {
		return false
	}
	return len(resolution.Aliases) == 0
}

// toSet 把字面量集合转为 map 形式。
func toSet(items ...string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, item := range items {
		out[item] = struct{}{}
	}
	return out
}
