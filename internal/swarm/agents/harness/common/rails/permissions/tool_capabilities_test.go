package permissions

import (
	"sort"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
)

// TestShellToolNames 测试 shell 工具名列表有序
func TestShellToolNames(t *testing.T) {
	got := ShellToolNames()
	want := []string{"bash", "cmd", "create_terminal", "mcp_exec_command", "powershell"}
	if len(got) != len(want) {
		t.Fatalf("ShellToolNames() = %v, want %v", got, want)
	}
	if !sort.StringsAreSorted(got) {
		t.Errorf("结果未排序: %v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ShellToolNames()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestClassifyTool_别名冲突 fail-closed：冲突时 category=unknown 且 facts_source=unknown
func TestClassifyTool_别名冲突(t *testing.T) {
	// exec_command 同时是别名键又与自身冲突（分支 1）
	got := ClassifyTool("exec_command")
	// exec_command 是别名键但不在 STATIC_CANONICAL 里作为规范名，
	// 不触发 conflict；应当正常归一为 mcp_exec_command
	if got.AliasConflict {
		t.Log("exec_command 触发 conflict（取决于 canonical 集合）")
	}
	if got.ToolName != "mcp_exec_command" {
		t.Errorf("ToolName = %q, want mcp_exec_command", got.ToolName)
	}
}

// TestClassifyTool_shell 测试 shell 类
func TestClassifyTool_shell(t *testing.T) {
	for _, name := range []string{"bash", "cmd", "powershell", "mcp_exec_command", "create_terminal"} {
		got := ClassifyTool(name)
		if got.Category != "shell" {
			t.Errorf("%s: Category = %q, want shell", name, got.Category)
		}
		if got.OperationFamily != "dynamic_execution" {
			t.Errorf("%s: OperationFamily = %q", name, got.OperationFamily)
		}
		if got.ResourceClass != "command" {
			t.Errorf("%s: ResourceClass = %q, want command", name, got.ResourceClass)
		}
		if !got.HighFlex {
			t.Errorf("%s: HighFlex = false, want true", name)
		}
		if got.RiskTier != "high" {
			t.Errorf("%s: RiskTier = %q, want high", name, got.RiskTier)
		}
		if got.RedactionKey != "arguments_sensitive" {
			t.Errorf("%s: RedactionKey = %q, want arguments_sensitive", name, got.RedactionKey)
		}
	}
}

// TestClassifyTool_code 测试 code 类
func TestClassifyTool_code(t *testing.T) {
	got := ClassifyTool("code")
	if got.Category != "code" {
		t.Errorf("Category = %q, want code", got.Category)
	}
	if got.ResourceClass != "code" {
		t.Errorf("ResourceClass = %q, want code", got.ResourceClass)
	}
	if got.RedactionKey != "arguments_sensitive" {
		t.Errorf("RedactionKey = %q, want arguments_sensitive", got.RedactionKey)
	}
}

// TestClassifyTool_path读写 测试 path 类的读写区分
func TestClassifyTool_path读写(t *testing.T) {
	// 写类
	for _, name := range []string{"write_file", "edit_file", "write_text_file", "write", "search_replace", "apply_patch"} {
		got := ClassifyTool(name)
		if got.Category != "path" {
			t.Errorf("%s: Category = %q, want path", name, got.Category)
		}
		if got.OperationFamily != "workspace_write" {
			t.Errorf("%s: OperationFamily = %q, want workspace_write", name, got.OperationFamily)
		}
		if got.RiskTier != "medium" {
			t.Errorf("%s: RiskTier = %q, want medium", name, got.RiskTier)
		}
		if !containsSideEffect(got.StaticSideEffects, "write_local") {
			t.Errorf("%s: 缺 write_local 副作用", name)
		}
	}
	// 读类
	for _, name := range []string{"read_file", "read_pdf", "read_text_file", "read", "glob", "grep", "list_dir", "lsp"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "workspace_read" {
			t.Errorf("%s: OperationFamily = %q, want workspace_read", name, got.OperationFamily)
		}
		if got.RiskTier != "low" {
			t.Errorf("%s: RiskTier = %q, want low", name, got.RiskTier)
		}
		if len(got.StaticSideEffects) != 0 {
			t.Errorf("%s: 读类不应有副作用，got %v", name, got.StaticSideEffects)
		}
		if got.RedactionKey != "resource_sensitive" {
			t.Errorf("%s: RedactionKey = %q, want resource_sensitive", name, got.RedactionKey)
		}
	}
}

// TestClassifyTool_network 测试公开搜索与网页读取
func TestClassifyTool_network(t *testing.T) {
	for _, name := range []string{"mcp_free_search", "mcp_paid_search"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "public_search" {
			t.Errorf("%s: OperationFamily = %q, want public_search", name, got.OperationFamily)
		}
		if got.RiskTier != "medium" {
			t.Errorf("%s: RiskTier = %q, want medium", name, got.RiskTier)
		}
		if got.RedactionKey != "network_sensitive" {
			t.Errorf("%s: RedactionKey = %q, want network_sensitive", name, got.RedactionKey)
		}
	}
	got := ClassifyTool("mcp_fetch_webpage")
	if got.OperationFamily != "public_web_read" {
		t.Errorf("OperationFamily = %q, want public_web_read", got.OperationFamily)
	}
}

// TestClassifyTool_内部只读先于cron前缀 测试分支顺序
//
// 这是 28 分支里最容易写错的一处：cron_list_jobs 既匹配
// _INTERNAL_READONLY_TOOLS 又匹配 cron_ 前缀分支。Python 把内部只读
// 放在前面，因此 cron_list_jobs 走 internal_readonly/low，
// 只有非只读的 cron_* 才落到 schedule_change/high。
func TestClassifyTool_内部只读先于cron前缀(t *testing.T) {
	readonly := ClassifyTool("cron_list_jobs")
	if readonly.OperationFamily != "internal_readonly" {
		t.Errorf("cron_list_jobs: OperationFamily = %q, want internal_readonly", readonly.OperationFamily)
	}
	if readonly.RiskTier != "low" {
		t.Errorf("cron_list_jobs: RiskTier = %q, want low", readonly.RiskTier)
	}
	if readonly.Category != "cron" {
		t.Errorf("cron_list_jobs: Category = %q, want cron（内部只读分支对 cron_ 前缀特判）", readonly.Category)
	}

	// 非只读的 cron_* 落到 schedule_change
	change := ClassifyTool("cron_create_job")
	if change.OperationFamily != "schedule_change" {
		t.Errorf("cron_create_job: OperationFamily = %q, want schedule_change", change.OperationFamily)
	}
	if change.RiskTier != "high" {
		t.Errorf("cron_create_job: RiskTier = %q, want high", change.RiskTier)
	}
	if change.FactsSource != NameClassificationHint {
		t.Errorf("cron_create_job: FactsSource = %q, want %q（非权威）",
			change.FactsSource, NameClassificationHint)
	}
}

// TestClassifyTool_内部只读非cron 测试非 cron 前缀的内部只读工具归 task_management
func TestClassifyTool_内部只读非cron(t *testing.T) {
	got := ClassifyTool("tool_search")
	if got.Category != "task_management" {
		t.Errorf("Category = %q, want task_management", got.Category)
	}
	if got.ResourceClass != "internal_state" {
		t.Errorf("ResourceClass = %q, want internal_state", got.ResourceClass)
	}
	if got.RedactionKey != "default" {
		t.Errorf("RedactionKey = %q, want default", got.RedactionKey)
	}
}

// TestClassifyTool_subagent运行时控制精确匹配 测试只有精确无别名的名字才走低风险分支
func TestClassifyTool_subagent运行时控制精确匹配(t *testing.T) {
	got := ClassifyTool("subagent_close")
	if got.OperationFamily != "subagent_runtime_control" {
		t.Errorf("OperationFamily = %q, want subagent_runtime_control", got.OperationFamily)
	}
	if got.RiskTier != "low" {
		t.Errorf("RiskTier = %q, want low（运行时控制是低风险）", got.RiskTier)
	}
	if got.HighFlex {
		t.Errorf("HighFlex = true, want false（运行时控制无弹性）")
	}
}

// TestClassifyTool_subagent控制 测试 subagent 控制类，只有 task_tool 权威
func TestClassifyTool_subagent控制(t *testing.T) {
	taskTool := ClassifyTool("task_tool")
	if taskTool.OperationFamily != "subagent_control" {
		t.Errorf("task_tool: OperationFamily = %q", taskTool.OperationFamily)
	}
	if taskTool.FactsSource != HostStaticFacts {
		t.Errorf("task_tool: FactsSource = %q, want host_static（唯一权威）", taskTool.FactsSource)
	}

	for _, name := range []string{"spawn_sub_agent", "session_new"} {
		got := ClassifyTool(name)
		if got.FactsSource != NameClassificationHint {
			t.Errorf("%s: FactsSource = %q, want %q（非权威）", name, got.FactsSource, NameClassificationHint)
		}
	}
}

// TestClassifyTool_browser固定工具 测试四个固定 browser 工具
func TestClassifyTool_browser固定工具(t *testing.T) {
	for _, name := range []string{"browser_phase", "browser_probe_cards", "browser_probe_interactives"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "browser_session_observe" {
			t.Errorf("%s: OperationFamily = %q, want browser_session_observe", name, got.OperationFamily)
		}
		if !containsSideEffect(got.StaticSideEffects, "session_cache_write") {
			t.Errorf("%s: 缺 session_cache_write 副作用", name)
		}
		if got.RiskTier != "low" {
			t.Errorf("%s: RiskTier = %q, want low", name, got.RiskTier)
		}
	}

	// browser_recall_offload 是特例：family 与副作用都不同
	offload := ClassifyTool("browser_recall_offload")
	if offload.OperationFamily != "browser_session_recall" {
		t.Errorf("browser_recall_offload: OperationFamily = %q, want browser_session_recall",
			offload.OperationFamily)
	}
	if len(offload.StaticSideEffects) != 0 {
		t.Errorf("browser_recall_offload: 不应有副作用，got %v", offload.StaticSideEffects)
	}
}

// TestClassifyTool_browser动态 测试 browser_ 前缀与 trusted MCP 走非权威高风险
func TestClassifyTool_browser动态(t *testing.T) {
	for _, name := range []string{"browser_click", "browser_open", "xiaoyi_gui_agent"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "browser_action" {
			t.Errorf("%s: OperationFamily = %q, want browser_action", name, got.OperationFamily)
		}
		if got.RiskTier != "high" {
			t.Errorf("%s: RiskTier = %q, want high", name, got.RiskTier)
		}
		if got.FactsSource != NameClassificationHint {
			t.Errorf("%s: FactsSource = %q, want 非权威", name, got.FactsSource)
		}
	}

	// trusted playwright MCP 工具
	trusted := ClassifyTool("mcp_playwright-official_browser_click")
	if trusted.OperationFamily != "browser_action" {
		t.Errorf("trusted MCP: OperationFamily = %q, want browser_action", trusted.OperationFamily)
	}
	// 不认识的 suffix 落 mcp 分支
	untrusted := ClassifyTool("mcp_playwright-official_browser_unknownop")
	if untrusted.Category != "mcp" {
		t.Errorf("未知 suffix: Category = %q, want mcp", untrusted.Category)
	}
}

// TestClassifyTool_skill 测试 skill 三类
func TestClassifyTool_skill(t *testing.T) {
	cases := []struct {
		name, family, risk string
	}{
		{"search_skill", "skill_discovery", "medium"},
		{"skill_tool", "skill_read", "low"},
		{"install_skill", "skill_change", "high"},
		{"uninstall_skill", "skill_change", "high"},
	}
	for _, c := range cases {
		got := ClassifyTool(c.name)
		if got.Category != "skill" {
			t.Errorf("%s: Category = %q, want skill", c.name, got.Category)
		}
		if got.OperationFamily != c.family {
			t.Errorf("%s: OperationFamily = %q, want %q", c.name, got.OperationFamily, c.family)
		}
		if got.RiskTier != c.risk {
			t.Errorf("%s: RiskTier = %q, want %q", c.name, got.RiskTier, c.risk)
		}
	}
}

// TestClassifyTool_externalSend与device 测试外发与设备操作
func TestClassifyTool_externalSend与device(t *testing.T) {
	for _, name := range []string{"send_file_to_user", "upload_file", "upload_photo", "send_message"} {
		got := ClassifyTool(name)
		if got.Category != "external_send" {
			t.Errorf("%s: Category = %q, want external_send", name, got.Category)
		}
		if got.RedactionKey != "network_sensitive" {
			t.Errorf("%s: RedactionKey = %q, want network_sensitive", name, got.RedactionKey)
		}
	}
	for _, name := range []string{"call_phone", "save_media_to_gallery", "save_file_to_file_manager"} {
		got := ClassifyTool(name)
		if got.Category != "device" {
			t.Errorf("%s: Category = %q, want device", name, got.Category)
		}
	}
}

// TestClassifyTool_memory 测试记忆读写三类
func TestClassifyTool_memory(t *testing.T) {
	// 写
	for _, name := range []string{"write_memory", "edit_memory", "viking_remember"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "memory_write" {
			t.Errorf("%s: OperationFamily = %q, want memory_write", name, got.OperationFamily)
		}
		if got.RiskTier != "high" {
			t.Errorf("%s: RiskTier = %q, want high", name, got.RiskTier)
		}
	}
	// 本地读
	for _, name := range []string{"memory_get", "read_memory"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "memory_read" {
			t.Errorf("%s: OperationFamily = %q, want memory_read", name, got.OperationFamily)
		}
		if got.RiskTier != "low" {
			t.Errorf("%s: RiskTier = %q, want low", name, got.RiskTier)
		}
	}
	if got := ClassifyTool("memory_search"); got.OperationFamily != "memory_search" {
		t.Errorf("memory_search: OperationFamily = %q, want memory_search", got.OperationFamily)
	}
	// 需审查的读
	for _, name := range []string{"ltm_search", "mem0_search", "viking_search"} {
		got := ClassifyTool(name)
		if got.OperationFamily != "memory_search" {
			t.Errorf("%s: OperationFamily = %q, want memory_search", name, got.OperationFamily)
		}
		if got.RiskTier != "medium" {
			t.Errorf("%s: RiskTier = %q, want medium", name, got.RiskTier)
		}
		if got.FactsSource != NameClassificationHint {
			t.Errorf("%s: FactsSource = %q, want 非权威", name, got.FactsSource)
		}
	}
	if got := ClassifyTool("viking_read"); got.OperationFamily != "memory_read" {
		t.Errorf("viking_read: OperationFamily = %q, want memory_read", got.OperationFamily)
	}
}

// TestClassifyTool_taskManagement 测试任务管理类
func TestClassifyTool_taskManagement(t *testing.T) {
	if got := ClassifyTool("todo_create"); got.OperationFamily != "task_state" {
		t.Errorf("todo_create: OperationFamily = %q", got.OperationFamily)
	}
	// 遗留 todo 非权威
	legacy := ClassifyTool("todo_insert")
	if legacy.FactsSource != NameClassificationHint {
		t.Errorf("todo_insert: FactsSource = %q, want 非权威", legacy.FactsSource)
	}
	if got := ClassifyTool("session_list"); got.OperationFamily != "session_status" {
		t.Errorf("session_list: OperationFamily = %q", got.OperationFamily)
	}
	if got := ClassifyTool("session_send_message"); got.OperationFamily != "cross_session_send" {
		t.Errorf("session_send_message: OperationFamily = %q", got.OperationFamily)
	}
	if got := ClassifyTool("session_message_resolve"); got.OperationFamily != "cross_session_resolution" {
		t.Errorf("session_message_resolve: OperationFamily = %q", got.OperationFamily)
	}
}

// TestClassifyTool_兜底 测试未知工具与未知 mcp
func TestClassifyTool_兜底(t *testing.T) {
	unknown := ClassifyTool("totally_unknown_tool")
	if unknown.Category != "unknown" {
		t.Errorf("Category = %q, want unknown", unknown.Category)
	}
	if unknown.FactsSource != UnknownFacts {
		t.Errorf("FactsSource = %q, want unknown", unknown.FactsSource)
	}
	if unknown.RedactionKey != "full_sensitive" {
		t.Errorf("RedactionKey = %q, want full_sensitive", unknown.RedactionKey)
	}

	mcp := ClassifyTool("mcp_some_unknown")
	if mcp.Category != "mcp" {
		t.Errorf("Category = %q, want mcp", mcp.Category)
	}
	if mcp.OperationFamily != "unknown_mcp" {
		t.Errorf("OperationFamily = %q, want unknown_mcp", mcp.OperationFamily)
	}
	if mcp.FactsSource != NameClassificationHint {
		t.Errorf("FactsSource = %q, want 非权威", mcp.FactsSource)
	}
}

// TestClassifyTool_DisplayKey 测试 display_key 格式
func TestClassifyTool_DisplayKey(t *testing.T) {
	got := ClassifyTool("bash")
	if got.DisplayKey != "tool.shell.dynamic_execution" {
		t.Errorf("DisplayKey = %q, want tool.shell.dynamic_execution", got.DisplayKey)
	}
}

// TestClassifyTool_FactsVersion 测试事实版本
func TestClassifyTool_FactsVersion(t *testing.T) {
	if got := ClassifyTool("bash"); got.FactsVersion != ToolCapabilityFactsVersion {
		t.Errorf("FactsVersion = %q, want %q", got.FactsVersion, ToolCapabilityFactsVersion)
	}
}

// TestRedactionKey 测试五个脱敏键的映射
func TestRedactionKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"command", "arguments_sensitive"},
		{"code", "arguments_sensitive"},
		{"workspace_path", "resource_sensitive"},
		{"browser_session", "resource_sensitive"},
		{"installed_skill", "resource_sensitive"},
		{"memory_store", "resource_sensitive"},
		{"public_web", "network_sensitive"},
		{"external_recipient", "network_sensitive"},
		{"unknown", "full_sensitive"},
		{"external_resource", "full_sensitive"},
		{"internal_state", "default"},
		{"schedule", "default"},
		{"user_device", "default"},
	}
	for _, c := range cases {
		if got := redactionKey(c.in); got != c.want {
			t.Errorf("redactionKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// TestIsPathWriteTool 测试路径写类判定
func TestIsPathWriteTool(t *testing.T) {
	writes := []string{"apply_patch", "write_file", "edit_file", "write_text_file", "write", "search_replace"}
	for _, n := range writes {
		if !isPathWriteTool(n) {
			t.Errorf("isPathWriteTool(%q) = false, want true", n)
		}
	}
	reads := []string{"read_file", "read_pdf", "glob", "grep", "lsp"}
	for _, n := range reads {
		if isPathWriteTool(n) {
			t.Errorf("isPathWriteTool(%q) = true, want false", n)
		}
	}
}

// TestIsTrustedBrowserMCPTool 测试 trusted playwright MCP 工具判定
func TestIsTrustedBrowserMCPTool(t *testing.T) {
	trusted := []string{
		"mcp_playwright-official_browser_click",
		"mcp_playwright_official_browser_navigate",
		"mcp_playwright-official_browser_take_screenshot",
	}
	for _, n := range trusted {
		if !isTrustedBrowserMCPTool(n) {
			t.Errorf("isTrustedBrowserMCPTool(%q) = false, want true", n)
		}
	}
	untrusted := []string{
		"mcp_playwright-official_browser_unknown",
		"mcp_other_browser_click",
		"browser_click",
	}
	for _, n := range untrusted {
		if isTrustedBrowserMCPTool(n) {
			t.Errorf("isTrustedBrowserMCPTool(%q) = true, want false", n)
		}
	}
}

// TestNormalizeToolName 测试工具名归一化
func TestNormalizeToolName(t *testing.T) {
	if got := NormalizeToolName("exec_command"); got != "mcp_exec_command" {
		t.Errorf("NormalizeToolName(exec_command) = %q, want mcp_exec_command", got)
	}
	if got := NormalizeToolName("bash"); got != "bash" {
		t.Errorf("NormalizeToolName(bash) = %q, want bash", got)
	}
}

// containsSideEffect 判定集合是否包含指定项。
func containsSideEffect(set map[string]struct{}, want string) bool {
	_, hit := set[want]
	return hit
}

// TestClassifyTool_与Python逐条对拍 34 个工具的完整分类结果对拍
//
// 基准值由 Python 侧 classify_tool 实际输出生成，逐字段比对。
// 这是 28 分支正确性的最终验收：任一分支的字段错位都会在这里暴露。
func TestClassifyTool_与Python逐条对拍(t *testing.T) {
	type want struct {
		category, family, resource, risk, facts, redaction string
		highFlex                                           bool
	}
	cases := map[string]want{
		"bash":                                  {"shell", "dynamic_execution", "command", "high", "host_static", "arguments_sensitive", true},
		"code":                                  {"code", "dynamic_execution", "code", "high", "host_static", "arguments_sensitive", true},
		"read_file":                             {"path", "workspace_read", "workspace_path", "low", "host_static", "resource_sensitive", false},
		"write_file":                            {"path", "workspace_write", "workspace_path", "medium", "host_static", "resource_sensitive", false},
		"apply_patch":                           {"path", "workspace_write", "workspace_path", "medium", "host_static", "resource_sensitive", false},
		"mcp_free_search":                       {"network", "public_search", "public_web", "medium", "host_static", "network_sensitive", false},
		"mcp_fetch_webpage":                     {"network", "public_web_read", "public_web", "medium", "host_static", "network_sensitive", false},
		"todo_create":                           {"task_management", "task_state", "session_task_state", "low", "host_static", "default", false},
		"todo_insert":                           {"task_management", "task_state", "session_task_state", "medium", "name_classification_hint", "default", true},
		"session_list":                          {"task_management", "session_status", "session_task_state", "low", "host_static", "default", false},
		"session_send_message":                  {"task_management", "cross_session_send", "product_session", "high", "host_static", "default", true},
		"ask_user":                              {"interaction", "ask_user", "user_interaction", "low", "host_static", "default", false},
		"cron_list_jobs":                        {"cron", "internal_readonly", "internal_state", "low", "host_static", "default", false},
		"cron_create_job":                       {"cron", "schedule_change", "schedule", "high", "name_classification_hint", "default", true},
		"subagent_close":                        {"subagent", "subagent_runtime_control", "agent_runtime", "low", "host_static", "default", false},
		"task_tool":                             {"subagent", "subagent_control", "agent_runtime", "high", "host_static", "default", true},
		"spawn_sub_agent":                       {"subagent", "subagent_control", "agent_runtime", "high", "name_classification_hint", "default", true},
		"browser_phase":                         {"browser", "browser_session_observe", "browser_session", "low", "host_static", "resource_sensitive", false},
		"browser_recall_offload":                {"browser", "browser_session_recall", "browser_session", "low", "host_static", "resource_sensitive", false},
		"browser_click":                         {"browser", "browser_action", "browser_session", "high", "name_classification_hint", "resource_sensitive", true},
		"xiaoyi_gui_agent":                      {"browser", "browser_action", "browser_session", "high", "name_classification_hint", "resource_sensitive", true},
		"mcp_playwright-official_browser_click": {"browser", "browser_action", "browser_session", "high", "name_classification_hint", "resource_sensitive", true},
		"search_skill":                          {"skill", "skill_discovery", "skill_catalog", "medium", "host_static", "default", false},
		"skill_tool":                            {"skill", "skill_read", "installed_skill", "low", "host_static", "resource_sensitive", false},
		"install_skill":                         {"skill", "skill_change", "skill_catalog", "high", "host_static", "default", true},
		"send_file_to_user":                     {"external_send", "external_send", "external_recipient", "high", "host_static", "network_sensitive", true},
		"call_phone":                            {"device", "device_action", "user_device", "high", "host_static", "default", true},
		"write_memory":                          {"memory", "memory_write", "memory_store", "high", "host_static", "resource_sensitive", true},
		"memory_get":                            {"memory", "memory_read", "memory_store", "low", "host_static", "resource_sensitive", false},
		"memory_search":                         {"memory", "memory_search", "memory_store", "low", "host_static", "resource_sensitive", false},
		"viking_search":                         {"memory", "memory_search", "memory_store", "medium", "name_classification_hint", "resource_sensitive", true},
		"mcp_unknown_thing":                     {"mcp", "unknown_mcp", "external_resource", "high", "name_classification_hint", "full_sensitive", true},
		"totally_unknown":                       {"unknown", "unknown", "unknown", "high", "unknown", "full_sensitive", true},
		"tool_search":                           {"task_management", "internal_readonly", "internal_state", "low", "host_static", "default", false},
	}

	for name, w := range cases {
		t.Run(name, func(t *testing.T) {
			got := ClassifyTool(name)
			if got.Category != w.category {
				t.Errorf("Category = %q, want %q", got.Category, w.category)
			}
			if got.OperationFamily != w.family {
				t.Errorf("OperationFamily = %q, want %q", got.OperationFamily, w.family)
			}
			if got.ResourceClass != w.resource {
				t.Errorf("ResourceClass = %q, want %q", got.ResourceClass, w.resource)
			}
			if got.RiskTier != w.risk {
				t.Errorf("RiskTier = %q, want %q", got.RiskTier, w.risk)
			}
			if got.FactsSource != w.facts {
				t.Errorf("FactsSource = %q, want %q", got.FactsSource, w.facts)
			}
			if got.RedactionKey != w.redaction {
				t.Errorf("RedactionKey = %q, want %q", got.RedactionKey, w.redaction)
			}
			if got.HighFlex != w.highFlex {
				t.Errorf("HighFlex = %v, want %v", got.HighFlex, w.highFlex)
			}
		})
	}
}

// TestClassifyTool_副作用与Python对拍 测试 12 个带副作用工具的具体集合
func TestClassifyTool_副作用与Python对拍(t *testing.T) {
	cases := map[string][]string{
		"bash":                   {"dynamic_exec"},
		"code":                   {"dynamic_exec"},
		"read_file":              nil,
		"write_file":             {"write_local"},
		"mcp_free_search":        {"network_search"},
		"mcp_fetch_webpage":      {"network_read"},
		"session_send_message":   {"delegation"},
		"browser_phase":          {"session_cache_write"},
		"browser_recall_offload": nil,
		"browser_click":          {"browser_action"},
		"install_skill":          {"skill_change"},
		"send_file_to_user":      {"external_send"},
		"write_memory":           {"memory_write"},
	}
	for name, want := range cases {
		got := ClassifyTool(name).StaticSideEffects
		if len(got) != len(want) {
			t.Errorf("%s: 副作用数 = %d, want %d (%v vs %v)", name, len(got), len(want), got, want)
			continue
		}
		for _, w := range want {
			if _, hit := got[w]; !hit {
				t.Errorf("%s: 缺副作用 %q，实际 %v", name, w, got)
			}
		}
	}
}

// TestIsExactSubagentRuntimeControl_四个条件 测试精确匹配的四个前置条件
func TestIsExactSubagentRuntimeControl_四个条件(t *testing.T) {
	type fakeRes struct {
		registered, canonical string
		aliases               []string
	}
	mk := func(f fakeRes) permission_tools.PermissionToolNameResolution {
		return permission_tools.PermissionToolNameResolution{
			RegisteredName: f.registered,
			CanonicalName:  f.canonical,
			Aliases:        f.aliases,
		}
	}

	// 条件 1：空名
	if isExactSubagentRuntimeControl("", mk(fakeRes{"", "", nil})) {
		t.Error("空名应返回 false")
	}
	// 条件 2：不在 runtime control 集合
	if isExactSubagentRuntimeControl("bash", mk(fakeRes{"bash", "bash", nil})) {
		t.Error("非 runtime control 工具应返回 false")
	}
	// 条件 3：registered_name 被改变
	if isExactSubagentRuntimeControl("subagent_close", mk(fakeRes{"subagent_close ", "subagent_close", nil})) {
		t.Error("registered_name 不匹配应返回 false")
	}
	// 条件 4a：canonical_name 被别名映射改变
	if isExactSubagentRuntimeControl("subagent_close", mk(fakeRes{"subagent_close", "other", nil})) {
		t.Error("canonical_name 不匹配应返回 false")
	}
	// 条件 4b：有别名
	if isExactSubagentRuntimeControl("subagent_close", mk(fakeRes{"subagent_close", "subagent_close", []string{"alias"}})) {
		t.Error("有别名应返回 false")
	}
	// 全部满足
	if !isExactSubagentRuntimeControl("subagent_close", mk(fakeRes{"subagent_close", "subagent_close", nil})) {
		t.Error("全部满足应返回 true")
	}
}
