package types

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// DisallowedForSubagents 禁止传递给子 Agent 的工具名切片。
// 对齐 Python: DISALLOWED_FOR_SUBAGENTS (code_agent_rail.py L28-31)
//
// adapter 和 runtime 共享此常量，避免硬编码重复。
// adapter 层通过 init() 转为 map[string]bool 加速查找。
var DisallowedForSubagents = []string{
	"Agent", "task", "enter_plan_mode", "exit_plan_mode",
	"ask_user_question", "task_stop", "switch_mode",
}

// ToolGroups 工具分组（用于 Agent 定义 UI）。
// 对齐 Python: TOOL_GROUPS (code_agent_rail.py L34-41)
var ToolGroups = map[string][]string{
	"核心":   {"Read", "Write", "Edit", "Bash", "LS"},
	"搜索":   {"Grep", "Glob", "WebSearch", "WebFetch"},
	"代码智能": {"LSP", "TodoWrite", "TodoList"},
	"高级":   {"MemorySearch", "MemoryGet", "WriteMemory", "EditMemory", "CronCreate", "CronList", "CronDelete", "SkillTool"},
	"可视化":  {"VisionQA", "ImageOCR", "AudioTranscribe"},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
