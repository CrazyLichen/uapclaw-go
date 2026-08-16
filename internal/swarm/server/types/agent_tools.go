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

// ToolDescriptions 工具描述映射（显示名→描述）。
// 对齐 Python: _TOOL_DESCRIPTIONS (agent_config_service.py L28-52)
// ListAvailableTools() 动态构建工具列表时使用此映射。
var ToolDescriptions = map[string]string{
	"Read":            "读取文件内容",
	"Write":           "写入文件",
	"Edit":            "编辑文件（精准替换）",
	"Bash":            "执行 shell 命令",
	"LS":              "列出目录内容",
	"Grep":            "搜索文件内容",
	"Glob":            "按模式搜索文件名",
	"WebSearch":       "网络搜索",
	"WebFetch":        "获取网页内容",
	"LSP":             "代码智能（定义跳转、引用查找）",
	"TodoWrite":       "创建/更新任务列表",
	"TodoList":        "查看任务列表",
	"MemorySearch":    "搜索记忆",
	"MemoryGet":       "获取记忆条目",
	"WriteMemory":     "写入记忆",
	"EditMemory":      "编辑记忆",
	"CronCreate":      "创建定时任务",
	"CronList":        "列出定时任务",
	"CronDelete":      "删除定时任务",
	"SkillTool":       "调用 Skill",
	"VisionQA":        "视觉问答",
	"ImageOCR":        "图片文字识别",
	"AudioTranscribe": "音频转录",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
