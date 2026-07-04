package tools

// ──────────────────────────── 全局变量 ────────────────────────────

// todoCreateDescription todo_create 工具双语描述
var todoCreateDescription = map[string]string{
	"cn": `创建当前会话的待办事项列表，用于跟踪进度、组织复杂任务，帮助用户了解整体执行情况。

## 何时使用

主动在以下场景调用：
- 任务需要 3 个或更多步骤
- 用户提供多个待完成事项（编号列表、逗号或分号分隔）
- 用户明确要求使用待办清单
- 任务具有规划性质（多步骤实现、功能开发等）

识别到规划需求后，立即调用本工具。`,
	"en": `Create a todo list for the current session to track progress, organize complex tasks, and help the user understand overall execution status.

## When to Use

Call proactively in these scenarios:
- Task requires 3 or more distinct steps
- User provides multiple items to complete (numbered list, comma- or semicolon-separated)
- User explicitly requests a todo list
- Task has planning nature (multi-step implementation, feature development, etc.)

Once you identify a planning need, call this tool immediately.`,
}

// todoListDescription todo_list 工具双语描述
var todoListDescription = map[string]string{
	"cn": "检索并显示当前会话的所有待办事项。",
	"en": "Retrieve and display all todo items for the current session",
}

// todoModifyDescription todo_modify 工具双语描述
var todoModifyDescription = map[string]string{
	"cn": `修改当前会话的待办事项。支持批量操作，尽量将多个变更合并为一次调用。

核心用途：更新（update）、删除（delete）、取消（cancel）、追加（append）、在其后插入（insert_after）、在其前插入（insert_before）`,
	"en": `Modify todo items for the current session. Supports batch operations — consolidate multiple changes into a single call whenever possible.

Core purpose: update, delete, cancel, append, insert_after, insert_before.`,
}

// todoGetDescription todo_get 工具双语描述
var todoGetDescription = map[string]string{
	"cn": "根据任务 ID 获取单个任务的完整详情。",
	"en": "Get full details of a single task by its ID.",
}

// ──────────────────────────── 结构体 ────────────────────────────

// TodoCreateMetadataProvider todo_create 工具元数据提供者
type TodoCreateMetadataProvider struct{}

// TodoListMetadataProvider todo_list 工具元数据提供者
type TodoListMetadataProvider struct{}

// TodoModifyMetadataProvider todo_modify 工具元数据提供者
type TodoModifyMetadataProvider struct{}

// TodoGetMetadataProvider todo_get 工具元数据提供者
type TodoGetMetadataProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTodoCreateMetadataProviderInputParams 构建 todo_create 工具的参数 Schema
func GetTodoCreateMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"tasks": {
			"cn": `子任务列表，JSON 数组格式。每个元素为任务对象，必填字段：
- id：任务唯一标识符，由你自行指定，必须简短且语义清晰（如 "translate_doc"、"analyze_code"），禁止使用随机字符或 UUID；同一会话内 ID 不得重复；后续 todo_modify 按此 ID 精准定位任务
- content：任务摘要描述
- activeForm：content 的进行语态（如 content 为「翻译文档」，activeForm 为「正在翻译文档」）
- description：任务详细内容
可选字段：
- selected_model_id：执行任务的模型 ID，见系统提示词「模型选择策略」`,
			"en": `List of subtasks in JSON array format. Each element is a task object with required fields:
- id: unique task identifier that YOU provide; must be short and semantically meaningful (e.g. 'translate_doc', 'analyze_code'); do NOT use random chars or UUIDs; IDs must be unique within a session; todo_modify uses this ID to locate tasks precisely
- content: task summary description
- activeForm: present-tense form of content (e.g., content 'Translate document' -> activeForm 'Translating document')
- description: detailed task content
Optional field:
- selected_model_id: model ID, see 'Model Selection Strategy' in system prompt`,
		},
		"id":                {"cn": `任务唯一标识符，由你自行指定的简短语义字符串（如 "translate_doc"），禁止使用 UUID，同一会话内须唯一`, "en": `Unique task identifier — a short semantic string you provide (e.g. 'translate_doc'); do NOT use UUIDs; must be unique within a session`},
		"content":           {"cn": "任务摘要描述", "en": "Task summary description"},
		"activeForm":        {"cn": "content 的进行语态", "en": "Present-tense form of content"},
		"description":       {"cn": "任务详细内容", "en": "Detailed task content"},
		"status":            {"cn": "任务状态", "en": "Task status"},
		"selected_model_id": {"cn": "执行此任务使用的模型 ID", "en": "Model ID for this task"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"tasks": map[string]any{
				"type":        "array",
				"description": d("tasks"),
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"id":                map[string]any{"type": "string", "description": d("id")},
						"content":           map[string]any{"type": "string", "description": d("content")},
						"activeForm":        map[string]any{"type": "string", "description": d("activeForm")},
						"description":       map[string]any{"type": "string", "description": d("description")},
						"selected_model_id": map[string]any{"type": "string", "description": d("selected_model_id")},
					},
					"required": []any{"id", "content", "activeForm", "description"},
				},
			},
		},
		"required": []any{"tasks"},
	}
}

// GetTodoListMetadataProviderInputParams 构建 todo_list 工具的参数 Schema
func GetTodoListMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	_ = lang
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetTodoModifyMetadataProviderInputParams 构建 todo_modify 工具的参数 Schema
func GetTodoModifyMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"action":           {"cn": "要执行的操作类型", "en": "Operation type to perform"},
		"ids":              {"cn": "要操作的任务 ID 列表", "en": "List of task IDs to operate on"},
		"todos":            {"cn": "根据 action 字段处理的待办事项数组", "en": "Array of todo items to process based on the action field"},
		"todo_data":        {"cn": "用于 insert_after/insert_before 操作的对象", "en": "Object for insert_after/insert_before actions"},
		"target_id":        {"cn": "目标任务 ID", "en": "Target task ID"},
		"items":            {"cn": "要插入的任务列表", "en": "Tasks to insert"},
		"id":               {"cn": `任务唯一标识符`, "en": "Unique task identifier"},
		"content":          {"cn": "任务摘要描述", "en": "Task summary description"},
		"activeForm":       {"cn": "content 的进行语态", "en": "Present-tense form of content"},
		"description":      {"cn": "任务详细内容", "en": "Detailed task content"},
		"status":           {"cn": "任务状态", "en": "Task status"},
		"selected_model_id": {"cn": "执行此任务使用的模型 ID", "en": "Model ID for this task"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	todoItemSchema := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":                map[string]any{"type": "string", "description": d("id")},
			"content":           map[string]any{"type": "string", "description": d("content")},
			"activeForm":        map[string]any{"type": "string", "description": d("activeForm")},
			"description":       map[string]any{"type": "string", "description": d("description")},
			"status":            map[string]any{"type": "string", "description": d("status"), "enum": []any{"pending", "in_progress", "completed", "cancelled"}},
			"selected_model_id": map[string]any{"type": "string", "description": d("selected_model_id")},
		},
		"required": []any{"id"},
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"action": map[string]any{"type": "string", "description": d("action"), "enum": []any{"update", "delete", "cancel", "append", "insert_after", "insert_before"}},
			"ids":    map[string]any{"type": "array", "description": d("ids"), "items": map[string]any{"type": "string"}},
			"todos":  map[string]any{"type": "array", "description": d("todos"), "items": todoItemSchema},
			"todo_data": map[string]any{
				"type":        "object",
				"description": d("todo_data"),
				"properties": map[string]any{
					"target_id": map[string]any{"type": "string", "description": d("target_id")},
					"items":     map[string]any{"type": "array", "description": d("items"), "items": todoItemSchema},
				},
				"required": []any{"target_id", "items"},
			},
		},
		"required": []any{"action"},
	}
}

// GetTodoGetMetadataProviderInputParams 构建 todo_get 工具的参数 Schema
func GetTodoGetMetadataProviderInputParams(language string) map[string]any {
	lang := language; if lang != "cn" && lang != "en" { lang = "cn" }
	p := map[string]map[string]string{
		"id": {"cn": "任务唯一标识符", "en": "Unique task identifier"},
	}
	d := func(key string) string { if v, ok := p[key][lang]; ok { return v }; return p[key]["cn"] }
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id": map[string]any{"type": "string", "description": d("id")},
		},
		"required": []any{"id"},
	}
}

func (p *TodoCreateMetadataProvider) GetName() string { return "todo_create" }
func (p *TodoCreateMetadataProvider) GetDescription(language string) string {
	if d, ok := todoCreateDescription[language]; ok { return d }
	return todoCreateDescription["cn"]
}
func (p *TodoCreateMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoCreateMetadataProviderInputParams(language)
}

func (p *TodoListMetadataProvider) GetName() string { return "todo_list" }
func (p *TodoListMetadataProvider) GetDescription(language string) string {
	if d, ok := todoListDescription[language]; ok { return d }
	return todoListDescription["cn"]
}
func (p *TodoListMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoListMetadataProviderInputParams(language)
}

func (p *TodoModifyMetadataProvider) GetName() string { return "todo_modify" }
func (p *TodoModifyMetadataProvider) GetDescription(language string) string {
	if d, ok := todoModifyDescription[language]; ok { return d }
	return todoModifyDescription["cn"]
}
func (p *TodoModifyMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoModifyMetadataProviderInputParams(language)
}

func (p *TodoGetMetadataProvider) GetName() string { return "todo_get" }
func (p *TodoGetMetadataProvider) GetDescription(language string) string {
	if d, ok := todoGetDescription[language]; ok { return d }
	return todoGetDescription["cn"]
}
func (p *TodoGetMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoGetMetadataProviderInputParams(language)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func init() {
	RegisterToolProvider(&TodoCreateMetadataProvider{})
	RegisterToolProvider(&TodoListMetadataProvider{})
	RegisterToolProvider(&TodoModifyMetadataProvider{})
	RegisterToolProvider(&TodoGetMetadataProvider{})
}
