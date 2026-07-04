package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// powershellDescription powershell 工具双语描述
var powershellDescription = map[string]string{
	"cn": `执行给定的 PowerShell 命令并返回输出。工作目录会在命令之间保持不变；但 shell 状态，例如变量、函数和别名，不会保留。

重要：本工具用于通过 PowerShell 执行终端操作，例如 git、npm、docker、python 和 PowerShell cmdlet。不要用它做文件搜索、内容搜索、读文件、写文件、编辑文件，除非用户明确要求，或你已经确认专用工具无法完成任务。优先使用 glob、grep、read_file、edit_file、write_file。`,
	"en": `Execute a given PowerShell command and return its output. The working directory persists between commands; shell state, such as variables, functions, and aliases, does not.

IMPORTANT: This tool is for terminal operations via PowerShell: git, npm, docker, python, and PowerShell cmdlets. Do not use it for file search, content search, reading files, writing files, or editing files unless the user explicitly asks for it or you have verified that the dedicated tools cannot complete the task. Prefer glob, grep, read_file, edit_file, and write_file.`,
}

// ──────────────────────────── 结构体 ────────────────────────────

// PowerShellMetadataProvider powershell 工具元数据提供者
type PowerShellMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetPowerShellMetadataProviderInputParams 构建 powershell 工具的参数 Schema
func GetPowerShellMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"command":          {"cn": "要执行的 PowerShell 命令", "en": "PowerShell command to execute"},
		"timeout":          {"cn": "可选超时时间（秒），默认 300，上限 3600。对于长时间运行的任务，建议适当增大该值以避免任务被提前中断", "en": "Optional timeout in seconds, default 300, max 3600. For long-running tasks, it is recommended to increase this value to avoid premature termination"},
		"workdir":          {"cn": "执行目录（相对或绝对路径），默认工作区根目录；不能越出沙箱", "en": "Working directory (relative or absolute path), defaults to workspace root; cannot escape sandbox"},
		"background":       {"cn": "是否后台运行，默认 false；设为 true 时立即返回 PID", "en": "Run in background (default false); returns PID immediately when true"},
		"max_output_chars": {"cn": "最大输出字符数，0 表示不限制（默认）；非零时上限 20000", "en": "Max output characters; 0 (default) means no limit; non-zero values are capped at 20000"},
		"description":      {"cn": "命令描述（可选），用于日志和审计", "en": "Optional command description for logging and audit trail"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"command":          map[string]any{"type": "string", "description": d("command")},
			"timeout":          map[string]any{"type": "integer", "description": d("timeout")},
			"workdir":          map[string]any{"type": "string", "description": d("workdir")},
			"background":       map[string]any{"type": "boolean", "description": d("background")},
			"max_output_chars": map[string]any{"type": "integer", "description": d("max_output_chars")},
			"description":      map[string]any{"type": "string", "description": d("description")},
		},
		"required": []any{"command"},
	}
}

func (p *PowerShellMetadataProvider) GetName() string { return "powershell" }
func (p *PowerShellMetadataProvider) GetDescription(language string) string {
	if d, ok := powershellDescription[language]; ok { return d }
	return powershellDescription["cn"]
}
func (p *PowerShellMetadataProvider) GetInputParams(language string) map[string]any {
	return GetPowerShellMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() { RegisterToolProvider(&PowerShellMetadataProvider{}) }
