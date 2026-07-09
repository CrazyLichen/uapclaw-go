package tools

// ──────────────────────────── 结构体 ────────────────────────────

// TodoCreateMetadataProvider todo_create 工具元数据提供者
type TodoCreateMetadataProvider struct{}

// TodoListMetadataProvider todo_list 工具元数据提供者
type TodoListMetadataProvider struct{}

// TodoModifyMetadataProvider todo_modify 工具元数据提供者
type TodoModifyMetadataProvider struct{}

// TodoGetMetadataProvider todo_get 工具元数据提供者
type TodoGetMetadataProvider struct{}

// ──────────────────────────── 全局变量 ────────────────────────────

// todoCreateDescription todo_create 工具双语描述
var todoCreateDescription = map[string]string{
	"cn": "创建当前会话的待办事项列表，用于跟踪进度、组织复杂任务，帮助用户了解整体执行情况。\n\n" +
		"## 何时使用\n\n" +
		"主动在以下场景调用：\n" +
		"- 任务需要 3 个或更多步骤\n" +
		"- 用户提供多个待完成事项（编号列表、逗号或分号分隔）\n" +
		"- 用户明确要求使用待办清单\n" +
		"- 任务具有规划性质（多步骤实现、功能开发等）\n\n" +
		"识别到规划需求后，立即调用本工具。\n\n" +
		"## 何时不使用\n\n" +
		"- 单个简单任务\n" +
		"- 纯信息查询或对话\n" +
		"- 可在 3 步以内完成的琐碎任务\n\n" +
		"## 使用方式\n\n" +
		"入参为 JSON 数组（每个任务必须包含 id 字段）：\n" +
		"    {\"tasks\": [{\"id\": \"translate_doc\", \"content\": \"翻译文档\", \"activeForm\": \"正在翻译文档\", \"description\": \"将文档翻译为目标语言\", \"selected_model_id\": \"fast\"}, {\"id\": \"analyze_arch\", \"content\": \"分析代码架构\", \"activeForm\": \"正在分析代码架构\", \"description\": \"梳理代码模块结构与依赖关系\", \"selected_model_id\": \"smart\"}]}\n\n" +
		"## 规则\n\n" +
		"- **id 字段为必填**：必须为每个任务指定简短、语义清晰的唯一字符串 ID（如 \"translate_doc\"、\"analyze_code\"），禁止使用随机字符或 UUID。同一会话内 ID 不得重复。此 ID 在后续 todo_modify 中用于精准定位任务，是跨轮次更新状态的唯一依据\n" +
		"- 第一个任务自动设为 in_progress，其余为 pending\n" +
		"- 同一时间只能有一个 in_progress 任务\n" +
		"- 任务描述必须具体、可执行、清晰明确\n" +
		"- 调用本工具会覆盖当前会话的任务列表；若需追加任务，请使用 todo_modify\n" +
		"- 当没有获取到当前可用模型信息时，不要添加 selected_model_id 字段；否则必须添加 selected_model_id 指定执行任务的模型 ID",

	"en": "Create a todo list for the current session to track progress, organize complex tasks, and help the user understand overall execution status.\n\n" +
		"## When to Use\n\n" +
		"Call proactively in these scenarios:\n" +
		"- Task requires 3 or more distinct steps\n" +
		"- User provides multiple items to complete (numbered list, comma- or semicolon-separated)\n" +
		"- User explicitly requests a todo list\n" +
		"- Task has planning nature (multi-step implementation, feature development, etc.)\n\n" +
		"Once you identify a planning need, call this tool immediately.\n\n" +
		"## When NOT to Use\n\n" +
		"- Single, straightforward task\n" +
		"- Pure informational queries or conversation\n" +
		"- Tasks completable in fewer than 3 trivial steps\n\n" +
		"## Usage\n\n" +
		"Input is a JSON array (each task must include an id field):\n" +
		"    {\"tasks\": [{\"id\": \"translate_doc\", \"content\": \"Translate document\", \"activeForm\": \"Translating document\", \"description\": \"Translate the document into the target language\", \"selected_model_id\": \"fast\"}, {\"id\": \"analyze_arch\", \"content\": \"Analyze code architecture\", \"activeForm\": \"Analyzing code architecture\", \"description\": \"Map out module structure and dependencies\", \"selected_model_id\": \"smart\"}]}\n\n" +
		"## Rules\n\n" +
		"- **id is required**: You must provide a short, semantically meaningful unique string ID for each task (e.g. \"translate_doc\", \"analyze_code\"). Do NOT use random characters or UUIDs. IDs must be unique within a session. This ID is used by todo_modify to precisely locate tasks and is the sole key for cross-turn status updates\n" +
		"- First task is automatically set to in_progress, others to pending\n" +
		"- Only one task can be in_progress at a time\n" +
		"- Task descriptions must be specific, actionable, and clear\n" +
		"- Calling this tool replaces the current session's task list; use todo_modify to append tasks\n" +
		"- When the currently available model information is not obtained, the selected_model_id field should not be added; otherwise, selected_model_id must be added to specify the model ID for executing the task.",
}

// todoListDescription todo_list 工具双语描述
var todoListDescription = map[string]string{
	"cn": "检索并显示当前会话的所有待办事项。\n\n" +
		"## 何时使用 todo_list（而非 todo_modify）\n\n" +
		"使用 todo_list 的场景：\n" +
		"- 需要查看当前任务全貌和各任务ID，再决定如何更新\n" +
		"- 不确定当前有哪些任务处于 in_progress 或 pending\n\n" +
		"使用 todo_modify 的场景（不需要先调用 todo_list）：\n" +
		"- 已知任务 ID，直接更新任务信息\n" +
		"- 任务刚完成，立即标记为 completed",

	"en": "Retrieve and display all todo items for the current session\n\n" +
		"## When to Use todo_list (vs. todo_modify)\n\n" +
		"Use todo_list when:\n" +
		"- You need an overview of all tasks and their IDs before deciding how to update\n" +
		"- You are unsure which tasks are currently in_progress or pending\n\n" +
		"Use todo_modify directly (no need to call todo_list first) when:\n" +
		"- You already know the task ID and want to update task information\n" +
		"- A task just finished and you want to mark it completed immediately",
}

// todoModifyDescription todo_modify 工具双语描述
var todoModifyDescription = map[string]string{
	"cn": "修改当前会话的待办事项。支持批量操作，尽量将多个变更合并为一次调用。\n\n" +
		"核心用途：更新（update）、删除（delete）、取消（cancel）、追加（append）、在其后插入（insert_after）、在其前插入（insert_before）\n\n" +
		"重要说明：\n" +
		"- 若需重新规划整个任务列表，请调用 todo_create\n" +
		"- 支持批量操作，尽量将多个变更合并为一次调用，避免连续多次调用\n" +
		"- **id 使用你在 todo_create 时自定义的语义 ID**（如 \"translate_doc\"），不要使用 UUID\n\n" +
		"action 支持的操作类型：\n\n" +
		"update：修改现有任务的状态或标题（id 不可修改，支持部分字段更新）：\n" +
		"    {\n" +
		"        \"action\": \"update\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"translate_doc\", \"status\": \"completed\"},\n" +
		"            {\"id\": \"analyze_code\", \"status\": \"in_progress\"}\n" +
		"        ]\n" +
		"    }\n\n" +
		"支持修改 selected_model_id：若任务 selected_model_id 不为空，且执行结果质量不佳（输出不准确、逻辑错误、未达预期），应根据模型描述更新质量更高的模型ID：\n" +
		"    {\n" +
		"        \"action\": \"update\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"translate_doc\", \"selected_model_id\": \"smart\", \"status\": \"pending\"}\n" +
		"        ]\n" +
		"    }\n\n" +
		"cancel：将指定任务标记为 cancelled（任务将被忽略，不再执行）：\n" +
		"    {\n" +
		"        \"action\": \"cancel\",\n" +
		"        \"ids\": [\"translate_doc\", \"analyze_code\"]\n" +
		"    }\n\n" +
		"delete：从列表中永久删除指定任务：\n" +
		"    {\n" +
		"        \"action\": \"delete\",\n" +
		"        \"ids\": [\"translate_doc\"]\n" +
		"    }\n\n" +
		"append：在列表末尾追加新任务（id 由你指定，须简短语义且唯一）：\n" +
		"    {\n" +
		"        \"action\": \"append\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"write_report\", \"content\": \"新任务内容\", \"activeForm\": \"执行新任务\", \"description\": \"任务的详细描述\", \"status\": \"pending\"}\n" +
		"        ]\n" +
		"    }\n\n" +
		"insert_after：在指定任务之后插入新任务（目标任务状态须为 in_progress 或 pending）：\n" +
		"    {\n" +
		"        \"action\": \"insert_after\",\n" +
		"        \"todo_data\": {\"target_id\": \"translate_doc\", \"items\": [{\"id\": \"review_translation\", \"content\": \"插入的任务\", \"activeForm\": \"执行插入的任务\", \"description\": \"任务的详细描述\", \"status\": \"pending\", \"selected_model_id\": \"fast\"}]}\n" +
		"    }\n\n" +
		"insert_before：在指定任务之前插入新任务（目标任务状态须为 pending）：\n" +
		"    {\n" +
		"        \"action\": \"insert_before\",\n" +
		"        \"todo_data\": {\"target_id\": \"analyze_code\", \"items\": [{\"id\": \"setup_env\", \"content\": \"插入的任务\", \"activeForm\": \"执行插入的任务\", \"description\": \"任务的详细描述\", \"status\": \"pending\"}]}\n" +
		"    }\n\n" +
		"核心规则：\n" +
		"- 同一时间只能有一个任务处于 in_progress 状态\n" +
		"- update 操作：id 字段不可修改，其他字段支持部分更新\n" +
		"- insert_after：目标任务状态必须为 in_progress 或 pending\n" +
		"- insert_before：目标任务状态必须为 pending\n" +
		"- 如果任务的 selected_model_id 为空时，任何操作都不要更改 selected_model_id 字段",

	"en": "Modify todo items for the current session. Supports batch operations — consolidate multiple changes into a single call whenever possible.\n\n" +
		"Core purpose: update, delete, cancel, append, insert_after, insert_before.\n\n" +
		"Important notes:\n" +
		"- To re-plan the entire task list, call todo_create instead\n" +
		"- Batch multiple changes into one call; avoid calling todo_modify repeatedly in succession\n" +
		"- **Use the semantic id you assigned in todo_create** (e.g. \"translate_doc\"); never use UUIDs\n\n" +
		"Supported action types:\n\n" +
		"update: Modify status or content of existing tasks (id cannot be changed; partial field updates supported):\n" +
		"    {\n" +
		"        \"action\": \"update\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"translate_doc\", \"status\": \"completed\"},\n" +
		"            {\"id\": \"analyze_code\", \"status\": \"in_progress\"}\n" +
		"        ]\n" +
		"    }\n\n" +
		"Support modifying selected_model_id: If the task's selected_model_id is not empty and the execution result is of poor quality (inaccurate output, logical errors, or failure to meet expectations), the model ID should be updated according to the model description to a higher-quality model:\n" +
		"    {\n" +
		"        \"action\": \"update\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"translate_doc\", \"selected_model_id\": \"smart\", \"status\": \"pending\"}\n" +
		"        ]\n" +
		"    }\n\n" +
		"cancel: Mark specified tasks as cancelled (tasks will be ignored and not executed):\n" +
		"    {\n" +
		"        \"action\": \"cancel\",\n" +
		"        \"ids\": [\"translate_doc\", \"analyze_code\"]\n" +
		"    }\n\n" +
		"delete: Permanently remove specified tasks from the list:\n" +
		"    {\n" +
		"        \"action\": \"delete\",\n" +
		"        \"ids\": [\"translate_doc\"]\n" +
		"    }\n\n" +
		"append: Add new tasks at the end of the list (id must be a short semantic string you choose, unique within the session):\n" +
		"    {\n" +
		"        \"action\": \"append\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"write_report\", \"content\": \"New task content\", \"activeForm\": \"Executing new task\", \"description\": \"Detailed description of the task\", \"status\": \"pending\"}\n" +
		"        ]\n" +
		"    }\n\n" +
		"insert_after: Insert new tasks after the specified task (target must be in_progress or pending):\n" +
		"    {\n" +
		"        \"action\": \"insert_after\",\n" +
		"        \"todo_data\": {\"target_id\": \"translate_doc\", \"items\": [{\"id\": \"review_translation\", \"content\": \"Inserted task\", \"activeForm\": \"Executing inserted task\", \"description\": \"Detailed description of the task\", \"status\": \"pending\", \"selected_model_id\": \"fast\"}]}\n" +
		"    }\n\n" +
		"insert_before: Insert new tasks before the specified task (target must be pending):\n" +
		"    {\n" +
		"        \"action\": \"insert_before\",\n" +
		"        \"todo_data\": {\"target_id\": \"analyze_code\", \"items\": [{\"id\": \"setup_env\", \"content\": \"Inserted task\", \"activeForm\": \"Executing inserted task\", \"description\": \"Detailed description of the task\", \"status\": \"pending\"}]}\n" +
		"    }\n\n" +
		"Core rules:\n" +
		"- Only one task can be in_progress at a time\n" +
		"- update action: id field cannot be modified; other fields support partial updates\n" +
		"- insert_after: target task status must be in_progress or pending\n" +
		"- insert_before: target task status must be pending\n" +
		"- If the task's selected_model_id is empty, do not modify the selected_model_id field in any operation.",
}

// todoGetDescription todo_get 工具双语描述
var todoGetDescription = map[string]string{
	"cn": "根据任务 ID 获取单个任务的完整详情。\n\n" +
		"入参：id（任务唯一标识符）\n\n" +
		"返回：完整的任务信息，包括 id、content（任务摘要）、activeForm、description（任务详细内容）、status、depends_on、result_summary、meta_data、selected_model_id。",

	"en": "Get full details of a single task by its ID.\n\n" +
		"Input: id (unique task identifier)\n\n" +
		"Returns: complete task info including id, content (task summary), activeForm, description (detailed content), status, depends_on, result_summary, meta_data, selected_model_id.",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetTodoCreateMetadataProviderInputParams 构建 todo_create 工具的参数 Schema
func GetTodoCreateMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"tasks": {
			"cn": "子任务列表，JSON 数组格式。每个元素为任务对象，必填字段：\n" +
				"- id：任务唯一标识符，由你自行指定，必须简短且语义清晰（如 \"translate_doc\"、\"analyze_code\"），" +
				"禁止使用随机字符或 UUID；同一会话内 ID 不得重复；后续 todo_modify 按此 ID 精准定位任务\n" +
				"- content：任务摘要描述\n" +
				"- activeForm：content 的进行语态（如 content 为「翻译文档」，activeForm 为「正在翻译文档」）\n" +
				"- description：任务详细内容\n" +
				"可选字段：\n" +
				"- selected_model_id：执行任务的模型 ID，见系统提示词「模型选择策略」",
			"en": "List of subtasks in JSON array format. Each element is a task object with required fields:\n" +
				"- id: unique task identifier that YOU provide; must be short and semantically meaningful " +
				"(e.g. 'translate_doc', 'analyze_code'); do NOT use random chars or UUIDs; " +
				"IDs must be unique within a session; todo_modify uses this ID to locate tasks precisely\n" +
				"- content: task summary description\n" +
				"- activeForm: present-tense form of content " +
				"(e.g., content 'Translate document' -> activeForm 'Translating document')\n" +
				"- description: detailed task content\n" +
				"Optional field:\n" +
				"- selected_model_id: model ID, see 'Model Selection Strategy' in system prompt",
		},
		"id":                {"cn": "任务唯一标识符，由你自行指定的简短语义字符串（如 \"translate_doc\"），禁止使用 UUID，同一会话内须唯一", "en": "Unique task identifier — a short semantic string you provide (e.g. 'translate_doc'); do NOT use UUIDs; must be unique within a session"},
		"content":           {"cn": "任务摘要描述", "en": "Task summary description"},
		"activeForm":        {"cn": "content 的进行语态", "en": "Present-tense form of content"},
		"description":       {"cn": "任务详细内容", "en": "Detailed task content"},
		"status":            {"cn": "任务状态", "en": "Task status"},
		"selected_model_id": {"cn": "执行此任务使用的模型 ID。见系统提示词「模型选择策略」。若任务结果不满意，可通过 todo_modify 更换更强的模型 ID 后重试。", "en": "Model ID for this task. See 'Model Selection Strategy' in system prompt. If task result is unsatisfactory, update via todo_modify and retry."},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
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
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	_ = lang
	return map[string]any{
		"type":       "object",
		"properties": map[string]any{},
		"required":   []any{},
	}
}

// GetTodoModifyMetadataProviderInputParams 构建 todo_modify 工具的参数 Schema
func GetTodoModifyMetadataProviderInputParams(language string) map[string]any {
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"action": {"cn": "要执行的操作类型", "en": "Operation type to perform"},
		"ids":    {"cn": "要操作的任务 ID 列表", "en": "List of task IDs to operate on"},
		"todos": {
			"cn": "根据 action 字段处理的待办事项数组。" +
				"支持修改 selected_model_id：若某任务执行结果质量不佳（输出不准确、逻辑错误、未达预期），" +
				"应将 selected_model_id 更新为更高等级的模型 ID，然后将任务状态重置为 pending 或 in_progress 以触发重新执行。",
			"en": "Array of todo items to process based on the action field. " +
				"Supports updating selected_model_id: if a task produces poor results (inaccurate output, " +
				"logical errors, unmet objectives), update selected_model_id to a model ID whose description " +
				"indicates stronger capability, " +
				"and reset the task status to pending or in_progress to trigger re-execution.",
		},
		"todo_data":         {"cn": "用于 insert_after/insert_before 操作的对象", "en": "Object for insert_after/insert_before actions"},
		"target_id":         {"cn": "目标任务 ID", "en": "Target task ID"},
		"items":             {"cn": "要插入的任务列表", "en": "Tasks to insert"},
		"id":                {"cn": "任务唯一标识符", "en": "Unique task identifier"},
		"content":           {"cn": "任务摘要描述", "en": "Task summary description"},
		"activeForm":        {"cn": "content 的进行语态", "en": "Present-tense form of content"},
		"description":       {"cn": "任务详细内容", "en": "Detailed task content"},
		"status":            {"cn": "任务状态", "en": "Task status"},
		"selected_model_id": {"cn": "执行此任务使用的模型 ID。见系统提示词「模型选择策略」。若任务结果不满意，可通过 todo_modify 更换更强的模型 ID 后重试。", "en": "Model ID for this task. See 'Model Selection Strategy' in system prompt. If task result is unsatisfactory, update via todo_modify and retry."},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
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
	lang := language
	if lang != "cn" && lang != "en" {
		lang = "cn"
	}
	p := map[string]map[string]string{
		"id": {"cn": "任务唯一标识符", "en": "Unique task identifier"},
	}
	d := func(key string) string {
		if v, ok := p[key][lang]; ok {
			return v
		}
		return p[key]["cn"]
	}
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
	if d, ok := todoCreateDescription[language]; ok {
		return d
	}
	return todoCreateDescription["cn"]
}
func (p *TodoCreateMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoCreateMetadataProviderInputParams(language)
}

func (p *TodoListMetadataProvider) GetName() string { return "todo_list" }
func (p *TodoListMetadataProvider) GetDescription(language string) string {
	if d, ok := todoListDescription[language]; ok {
		return d
	}
	return todoListDescription["cn"]
}
func (p *TodoListMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoListMetadataProviderInputParams(language)
}

func (p *TodoModifyMetadataProvider) GetName() string { return "todo_modify" }
func (p *TodoModifyMetadataProvider) GetDescription(language string) string {
	if d, ok := todoModifyDescription[language]; ok {
		return d
	}
	return todoModifyDescription["cn"]
}
func (p *TodoModifyMetadataProvider) GetInputParams(language string) map[string]any {
	return GetTodoModifyMetadataProviderInputParams(language)
}

func (p *TodoGetMetadataProvider) GetName() string { return "todo_get" }
func (p *TodoGetMetadataProvider) GetDescription(language string) string {
	if d, ok := todoGetDescription[language]; ok {
		return d
	}
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
