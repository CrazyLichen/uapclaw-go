// Python 源：openjiuwen/harness/manifest/builtin_elements.py 常量段（:64-81，19 值 1:1）
//
// 内置元素的 provider 注册名常量。RailSpec.Type / BuiltinToolSpec.Type 引用的
// 就是这些注册名。本文件只落常量段；构造注册段（builtin_elements.py 其余，
// 各默认 rail/tool 构造器与 ensure_builtin_elements_registered）⤴️ rail
// 注册表章节（Go 侧注册缝见 schema/ensureBuiltinElementsRegisteredHook）。

package manifest

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TaskPlanning 任务规划元素。Python: builtin_elements.py:64
	TaskPlanning = "core.task_planning"
	// SkillUse 技能使用元素。Python: builtin_elements.py:65
	SkillUse = "core.skill_use"
	// Subagent 子代理元素。Python: builtin_elements.py:66
	Subagent = "core.subagent"
	// SysOperation 系统操作元素。Python: builtin_elements.py:67
	SysOperation = "core.sys_operation"
	// Security 安全元素。Python: builtin_elements.py:68
	Security = "core.security"
	// Heartbeat 心跳元素。Python: builtin_elements.py:69
	Heartbeat = "core.heartbeat"
	// Worktree 工作树元素。Python: builtin_elements.py:70
	Worktree = "core.worktree"
	// LSP 语言服务器元素。Python: builtin_elements.py:71
	LSP = "core.lsp"
	// TokenTracking token 追踪元素。Python: builtin_elements.py:72
	TokenTracking = "core.token_tracking"
	// ToolTracking 工具追踪元素。Python: builtin_elements.py:73
	ToolTracking = "core.tool_tracking"
	// AskUser 询问用户元素。Python: builtin_elements.py:74
	AskUser = "core.ask_user"
	// ConfirmInterrupt 确认中断元素。Python: builtin_elements.py:75
	ConfirmInterrupt = "core.confirm_interrupt"
	// WebSearch 网页搜索元素。Python: builtin_elements.py:76
	WebSearch = "core.web_search"
	// WebFetch 网页抓取元素。Python: builtin_elements.py:77
	WebFetch = "core.web_fetch"
	// WebPaidSearch 付费网页搜索元素。Python: builtin_elements.py:78
	WebPaidSearch = "core.web_paid_search"
	// Vision 视觉元素。Python: builtin_elements.py:79
	Vision = "core.vision"
	// Audio 音频元素。Python: builtin_elements.py:80
	Audio = "core.audio"
	// Observability 可观测元素。Python: builtin_elements.py:81
	Observability = "core.observability"
)
