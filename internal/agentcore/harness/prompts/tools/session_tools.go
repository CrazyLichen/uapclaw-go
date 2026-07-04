package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// sessionsListDescription sessions_list 工具双语描述
var sessionsListDescription = map[string]string{
	"cn": "查看当前所有后台异步子任务(包括运行中、已完成、失败、已取消)及其元数据",
	"en": "List all background async tasks (running, completed, failed, canceled) and its metadata",
}

// sessionsSpawnDescription sessions_spawn 工具双语描述
var sessionsSpawnDescription = map[string]string{
	"cn": `创建异步后台子代理任务，立即返回 pending 状态，任务在后台执行，不阻塞当前对话。

可用代理类型及对应工具：
{available_agents}

重要：使用 sessions_spawn 时，必须指定 subagent_type, task_description 参数选择代理类型和描述任务。请勿指定你无权访问的其他代理！！！`,
	"en": `Create async background subagent task that returns pending status immediately while the task executes in the background without blocking the current conversation.

Available agent types and the tools they have access to:
{available_agents}

Important: When using sessions_spawn, you must specify the subagent_type and task_description parameters to select the agent type and describe the task. Do not specify agents you do not have access to!!!`,
}

// sessionsCancelDescription sessions_cancel 工具双语描述
var sessionsCancelDescription = map[string]string{
	"cn": "取消后台异步子任务。此操作会同步阻塞直到任务取消完成。",
	"en": "Cancel background async task. This operation blocks synchronously until cancellation completes.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// SessionsListMetadataProvider sessions_list 工具元数据提供者
type SessionsListMetadataProvider struct{}

// SessionsSpawnMetadataProvider sessions_spawn 工具元数据提供者
type SessionsSpawnMetadataProvider struct{}

// SessionsCancelMetadataProvider sessions_cancel 工具元数据提供者
type SessionsCancelMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSessionsListMetadataProviderInputParams 构建 sessions_list 工具的参数 Schema
func GetSessionsListMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	_ = lang // 无参数工具
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetSessionsSpawnMetadataProviderInputParams 构建 sessions_spawn 工具的参数 Schema
func GetSessionsSpawnMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"subagent_type":    {"cn": "子 agent 类型(如 'general-purpose')", "en": "Subagent type (e.g., 'general-purpose')"},
		"task_description": {"cn": "任务描述", "en": "Task description"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"subagent_type":    map[string]any{"type": "string", "description": d("subagent_type")},
			"task_description": map[string]any{"type": "string", "description": d("task_description")},
		},
		"required": []any{"subagent_type", "task_description"},
	}
}

// GetSessionsCancelMetadataProviderInputParams 构建 sessions_cancel 工具的参数 Schema
func GetSessionsCancelMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"task_id": {"cn": "要取消的任务 ID（从 sessions_list 获取）", "en": "Task ID to cancel (obtained from sessions_list)"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"task_id": map[string]any{"type": "string", "description": d("task_id")},
		},
		"required": []any{"task_id"},
	}
}

func (p *SessionsListMetadataProvider) GetName() string { return "sessions_list" }
func (p *SessionsListMetadataProvider) GetDescription(language string) string {
	if d, ok := sessionsListDescription[language]; ok { return d }
	return sessionsListDescription["cn"]
}
func (p *SessionsListMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSessionsListMetadataProviderInputParams(language)
}

func (p *SessionsSpawnMetadataProvider) GetName() string { return "sessions_spawn" }
func (p *SessionsSpawnMetadataProvider) GetDescription(language string) string {
	if d, ok := sessionsSpawnDescription[language]; ok { return d }
	return sessionsSpawnDescription["cn"]
}
func (p *SessionsSpawnMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSessionsSpawnMetadataProviderInputParams(language)
}

func (p *SessionsCancelMetadataProvider) GetName() string { return "sessions_cancel" }
func (p *SessionsCancelMetadataProvider) GetDescription(language string) string {
	if d, ok := sessionsCancelDescription[language]; ok { return d }
	return sessionsCancelDescription["cn"]
}
func (p *SessionsCancelMetadataProvider) GetInputParams(language string) map[string]any {
	return GetSessionsCancelMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&SessionsListMetadataProvider{})
	RegisterToolProvider(&SessionsSpawnMetadataProvider{})
	RegisterToolProvider(&SessionsCancelMetadataProvider{})
}
