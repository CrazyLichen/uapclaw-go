package tools

import (
	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// codeTodoCreateDescription code 模式 todo_create 工具描述（逐字复刻上游 code_todo_tool_prompts.py）
	codeTodoCreateDescription = "Create a todo list for the current session. Scale the list to how complex the work actually is.\n" +
		"\n" +
		"## When to skip (do the work directly)\n" +
		"\n" +
		"- Single focused change: one bug, one function, one config tweak, or a short answer\n" +
		"- A few related edits with no real phase boundaries (rename, small refactor)\n" +
		"- You can finish in one continuous pass without losing track\n" +
		"\n" +
		"## How many items\n" +
		"\n" +
		"| Complexity | Items | Examples |\n" +
		"|------------|-------|----------|\n" +
		"| Medium | 2–3 | Greenfield app: backend, frontend/UI, verify end-to-end |\n" +
		"| Complex | 4–6 max | Multi-service feature, large refactor, many deliverables, unclear order |\n" +
		"| User asks for a plan | Match their structure | Still prefer outcomes over files |\n" +
		"\n" +
		"Do NOT mirror the user's spec headings (Section 1, Section 2…) or project file list as separate todos.\n" +
		"Do NOT create one todo per file unless the user explicitly asks for file-level tracking.\n" +
		"\n" +
		"## Granularity\n" +
		"\n" +
		"- Each item = one outcome or phase (e.g. \"Implement Flask API and SQLite\", \"Build Canvas game UI\", \"Verify server and score flow\").\n" +
		"- Each task needs id, content, activeForm, and description; keep them brief.\n" +
		"- Include verification in the last milestone — do not add separate todos per curl, test, or check.\n" +
		"\n" +
		"## Usage\n" +
		"\n" +
		"Call once before substantive work. Prefer todo_create in parallel with the first write/bash when possible — avoid a todo-only first round.\n" +
		"\n" +
		"{\"todos\": [{\"id\": \"backend\", \"content\": \"Implement Flask backend\", \"activeForm\": \"Implementing Flask backend\", \"description\": \"app.py, SQLite scores API, static routes\"}, {\"id\": \"verify\", \"content\": \"Verify end-to-end\", \"activeForm\": \"Verifying end-to-end\", \"description\": \"Run app, exercise APIs, confirm game loads\"}]}"

	// codeTodoListDescription code 模式 todo_list 工具描述（逐字复刻上游）
	codeTodoListDescription = "List all todos for the current session.\n" +
		"\n" +
		"Use rarely — only when the plan is unclear, after a long interruption, or the user asks.\n" +
		"Do not call routinely between steps; use todo_modify to update progress instead."

	// codeTodoGetDescription code 模式 todo_get 工具描述（逐字复刻上游）
	codeTodoGetDescription = "Get full details for one todo by id. Use only when you need fields not visible in recent todo_modify results."

	// codeTodoModifyDescription code 模式 todo_modify 工具描述（逐字复刻上游）
	codeTodoModifyDescription = "Update todo items for the current session.\n" +
		"\n" +
		"## Prefer efficiency\n" +
		"\n" +
		"- Mark a milestone completed and start the next in the same response as the next write/bash/edit — parallel tool calls when independent.\n" +
		"- Avoid todo-only rounds: do not call todo_modify alone just to flip status unless wrapping up.\n" +
		"- Batch multiple updates in one call (e.g. complete backend + set frontend in_progress).\n" +
		"\n" +
		"## Actions\n" +
		"\n" +
		"update: change status or fields of an existing milestone. Mark it completed as soon as the phase is done. Send `todos`; each item needs `id` plus only the fields that changed (usually `status`).\n" +
		"    {\n" +
		"        \"action\": \"update\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"backend\", \"status\": \"completed\"},\n" +
		"            {\"id\": \"verify\", \"status\": \"in_progress\"}\n" +
		"        ]\n" +
		"    }\n" +
		"\n" +
		"append: add a follow-up milestone when scope genuinely expands (use sparingly; prefer 4–6 total items). Send `todos`; each new item needs id, content, activeForm, description, and status.\n" +
		"    {\n" +
		"        \"action\": \"append\",\n" +
		"        \"todos\": [\n" +
		"            {\"id\": \"verify\", \"content\": \"Verify end-to-end\", \"activeForm\": \"Verifying end-to-end\", \"description\": \"Run the app and confirm the phase outcome\", \"status\": \"pending\"}\n" +
		"        ]\n" +
		"    }\n" +
		"\n" +
		"cancel: mark milestones as cancelled. Send `ids`.\n" +
		"    {\n" +
		"        \"action\": \"cancel\",\n" +
		"        \"ids\": [\"verify\"]\n" +
		"    }\n" +
		"\n" +
		"delete: remove milestones that are no longer needed. Send `ids`.\n" +
		"    {\n" +
		"        \"action\": \"delete\",\n" +
		"        \"ids\": [\"verify\"]\n" +
		"    }\n" +
		"\n" +
		"insert_after: insert after a milestone that is in_progress or pending. Only for a real mid-plan scope change, not file-by-file progress. Send `todo_data`.\n" +
		"    {\n" +
		"        \"action\": \"insert_after\",\n" +
		"        \"todo_data\": {\"target_id\": \"backend\", \"items\": [{\"id\": \"verify\", \"content\": \"Verify end-to-end\", \"activeForm\": \"Verifying end-to-end\", \"description\": \"Run the app and confirm the phase outcome\", \"status\": \"pending\"}]}\n" +
		"    }\n" +
		"\n" +
		"insert_before: insert before a milestone that is pending. Same scope rule as insert_after. Send `todo_data`.\n" +
		"    {\n" +
		"        \"action\": \"insert_before\",\n" +
		"        \"todo_data\": {\"target_id\": \"verify\", \"items\": [{\"id\": \"backend\", \"content\": \"Implement the API\", \"activeForm\": \"Implementing the API\", \"description\": \"Phase outcome, not one file\", \"status\": \"pending\"}]}\n" +
		"    }\n" +
		"\n" +
		"To replace the entire plan, call todo_create instead of many inserts.\n" +
		"\n" +
		"Do not create a new todo for each file written or each verification command."
)

// ──────────────────────────── 导出函数 ────────────────────────────

// CodeTodoCardBuilder 构建 code 模式的 todo 工具卡片。
//
// name 取 todo_create/todo_list/todo_get/todo_modify；未知 name 返回 nil，
// 调用方回落注册表默认文案。
//
// Python: _build_code_todo_card(name, tool_id_prefix, agent_id)
func CodeTodoCardBuilder(name, toolID, language, agentID string) *tool.ToolCard {
	_ = language // code 模式只用英文文案（上游 CODE_TODO_TOOL_PROMPTS 仅 EN）
	description, schemaMap := codeTodoPrompt(name)
	if description == "" {
		return nil
	}
	params, err := cschema.ParseJSONSchemaMap(schemaMap)
	if err != nil {
		return nil
	}
	cardID := toolID + "_" + agentID
	if agentID == "" {
		cardID = toolID + "_" + uuid.New().String()[:8]
	}
	return tool.NewToolCardWithID(cardID, name, description, params, nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// codeTodoPrompt 返回指定工具的 code 模式描述与 JSON schema。
//
// Python: CODE_TODO_TOOL_PROMPTS[name]
func codeTodoPrompt(name string) (string, map[string]any) {
	// 本函数就是一张查表：按工具名返回它的中文描述和对应的参数结构。
	// 参数结构都是 JSON Schema 的形式，告诉模型调用该工具时能填哪些字段、哪些必填。
	switch name {
	// 新建待办：一次性给出一组里程碑，每个都要有 id、内容、进行时描述和说明。
	case "todo_create":
		return codeTodoCreateDescription, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"todos": map[string]any{
					"type": "array",
					"description": "Outcome-based milestones scaled to complexity: skip todos for trivial work; " +
						"2–3 for medium multi-phase work; 4–6 max for complex work. " +
						"Each task requires id, content, activeForm, and description.",
					"items": map[string]any{
						"type": "object",
						"properties": map[string]any{
							"id":          codeTodoItemProperties["id"],
							"content":     codeTodoItemProperties["content"],
							"activeForm":  codeTodoItemProperties["activeForm"],
							"description": codeTodoItemProperties["description"],
						},
						"required": []any{"id", "content", "activeForm", "description"},
					},
				},
			},
			"required": []any{"todos"},
		}
	// 列出全部待办：不需要任何入参。
	case "todo_list":
		return codeTodoListDescription, map[string]any{
			"type":       "object",
			"properties": map[string]any{},
			"required":   []any{},
		}
	// 查看单条待办：只传待办 id。
	case "todo_get":
		return codeTodoGetDescription, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"id": codeTodoItemProperties["id"],
			},
			"required": []any{"id"},
		}
	// 修改待办：action 决定是改、删、取消还是插入，
	// 批量更新用 todos，按位置插入还要额外给 todo_data（目标 id 加待插入条目）。
	case "todo_modify":
		return codeTodoModifyDescription, map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action": map[string]any{
					"type":        "string",
					"description": "One of: update, delete, cancel, append, insert_after, insert_before.",
				},
				"ids": map[string]any{
					"type":        "array",
					"items":       map[string]any{"type": "string"},
					"description": "Task ids for delete or cancel.",
				},
				"todos": map[string]any{
					"type":        "array",
					"description": "Tasks to update or append. Prefer batching several status changes in one call.",
					"items": map[string]any{
						"type":       "object",
						"properties": codeTodoItemProperties,
						"required":   []any{"id"},
					},
				},
				"todo_data": map[string]any{
					"type":        "object",
					"description": "Object for insert_after or insert_before.",
					"properties": map[string]any{
						"target_id": map[string]any{
							"type":        "string",
							"description": "Id of the existing milestone to insert beside.",
						},
						"items": map[string]any{
							"type":        "array",
							"description": "Milestones to insert.",
							"items": map[string]any{
								"type":       "object",
								"properties": codeTodoItemProperties,
								"required":   []any{"id"},
							},
						},
					},
					"required": []any{"target_id", "items"},
				},
			},
			"required": []any{"action"},
		}
	// 名字不在上面几个之内，属于未知工具，返回空值让调用方自行处理。
	default:
		return "", nil
	}
}

// ──────────────────────────── 全局变量 ────────────────────────────

// codeTodoItemProperties todo 条目字段定义（逐字复刻上游 _CODE_TODO_ITEM_PROPERTIES_EN）。
var codeTodoItemProperties = map[string]any{
	"id": map[string]any{
		"type":        "string",
		"description": "Short semantic task id (e.g. 'backend', 'frontend', 'verify'). Unique within the session.",
	},
	"content": map[string]any{
		"type":        "string",
		"description": "Brief outcome-based title (phase or deliverable, not a filename).",
	},
	"activeForm": map[string]any{
		"type":        "string",
		"description": "Present-tense spinner label; keep short.",
	},
	"description": map[string]any{
		"type":        "string",
		"description": "What this phase achieves; one or two sentences. May mention multiple files.",
	},
	"status": map[string]any{
		"type":        "string",
		"description": "Task status.",
		"enum":        []any{"pending", "in_progress", "completed", "cancelled"},
	},
}
