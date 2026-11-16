package duplex

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ──────────────────────────── 常量 ────────────────────────────

// QwenOmniDelegateToolName 委派工具名。
// 对齐 qwen_omni_tools.py:12 QWEN_OMNI_DELEGATE_TOOL_NAME。
const QwenOmniDelegateToolName = "jiuwen_delegate"

// QwenOmniResearchToolName 旧版搜索工具名（兼容解析）。
// 对齐 qwen_omni_tools.py:13 QWEN_OMNI_RESEARCH_TOOL_NAME。
const QwenOmniResearchToolName = "jiuwen_research"

// maxCallIDChars call_id 最大长度。
// 对齐 qwen_omni_tools.py:14 _MAX_CALL_ID_CHARS。
const maxCallIDChars = 200

// taskToolNames 任务管理工具名集合。
// 对齐 qwen_omni_tools.py:15-20 _TASK_TOOLS。
var taskToolNames = map[string]bool{
	"jiuwen_task_query":   true,
	"jiuwen_task_cancel":  true,
	"jiuwen_task_modify":  true,
	"jiuwen_task_reorder": true,
	"jiuwen_task_answer":  true,
}

// delegateArgumentNames 委派任务字段候选名。
// 对齐 qwen_omni_tools.py:21-25 _DELEGATE_ARGUMENT_NAMES。
var delegateArgumentNames = []string{"task", "query", "instruction", "request"}

// maxTaskChars 任务文本最大长度。
// 对齐 qwen_omni_tools.py:26 _MAX_TASK_CHARS。
const maxTaskChars = 16_000

// ──────────────────────────── 结构体 ────────────────────────────

// QwenOmniToolCall 验货后的工具调用。
// 对齐 qwen_omni_tools.py:28-40 QwenOmniToolCall。
type QwenOmniToolCall struct {
	// Name 工具名
	Name string
	// CallID 调用标识
	CallID string
	// Arguments 参数
	Arguments map[string]any
	// Task 任务文本（任务管理工具为空串）
	Task string
}

// Query 任务文本的兼容别名。
// 对齐 qwen_omni_tools.py:36-38 query property。
func (c QwenOmniToolCall) Query() string { return c.Task }

// ──────────────────────────── 导出函数 ────────────────────────────

// QwenOmniTools 返回每次会话新的 Qwen 兼容工具定义。
// 对齐 qwen_omni_tools.py:43-121 qwen_omni_tools（工具描述文案一比一，禁改写）。
func QwenOmniTools() []map[string]any {
	return append(TaskManagementTools(), []map[string]any{{
		"type": "function",
		"function": map[string]any{
			"name": QwenOmniDelegateToolName,
			"description": "Delegate any request that cannot be completed directly from the current " +
				"audio, video, and conversation to the full Jiuwen Core Agent. Jiuwen may " +
				"use all of its available capabilities, including research, files, document " +
				"processing, calculation, code execution, and computer or browser tools." +
				" Call this when the user explicitly requests background Agent work. If the Agent must " +
				"ask about budget or other information first, include asking and waiting in task and " +
				"delegate immediately; do not substitute your own question for task creation.",
			"parameters": map[string]any{
				"type": "object",
				"properties": map[string]any{
					"independent": map[string]any{
						"type": "boolean",
						"description": "True only for a self-contained new task that may run alongside other work. Changes to" +
							" existing work must use task_modify. Default false preserves ordering.",
					},
					"depends_on": map[string]any{
						"type":        "array",
						"items":       map[string]any{"type": "string"},
						"description": "Exact job IDs whose successful results are required. " +
							"Query to resolve IDs; never guess.",
					},
					"resources": map[string]any{
						"type":  "array",
						"items": map[string]any{"type": "string"},
						"description": "Shared resources this task may access or change; use canonical absolute file paths or" +
							" stable resource names. Same resources serialize. Declare known shared modification " +
							"targets. Omission means unspecified, not exclusive scheduling. Separate new documents" +
							" in default per-task workspaces need no resource declaration. This is scheduling " +
							"metadata, not file access authorization.",
					},
					"task": map[string]any{
						"type": "string",
						"description": "A complete, self-contained task preserving the user's requested " +
							"action, target, path or name, output format, and constraints.",
					},
				},
				"required":             []string{"task"},
				"additionalProperties": false,
			},
		},
	}}...)
}

// TaskManagementTools 任务管理五件套工具定义。
// 对齐 qwen_omni_tools.py:216-306 task_management_tools（specs 表一比一）。
func TaskManagementTools() []map[string]any {
	type spec struct {
		name, description string
		properties        map[string]any
		required          []string
	}
	specs := []spec{
		{
			"jiuwen_task_reorder",
			"Reorder waiting tasks only, without stopping any running task. Query the queue first." +
				" Clarify ambiguous targets. Priority never bypasses dependencies or resources and " +
				"does not promise immediate execution." +
				" Use returned opaque IDs, not task names. action must be exactly next or before;" +
				" for before pass the other waiting task ID in the separate before_job_id field.",
			map[string]any{
				"job_id":        map[string]any{"type": "string"},
				"queue_version": map[string]any{"type": "integer", "minimum": 0},
				"action":        map[string]any{"type": "string", "enum": []string{"next", "before"}},
				"before_job_id": map[string]any{"type": "string"},
			},
			[]string{"job_id", "queue_version", "action"},
		},
		{
			"jiuwen_task_answer",
			"Answer an observed pending Agent information question so the original task continues." +
				" Query exact job_id, interaction.id and questions first; pass interaction.id as " +
				"interaction_id. Clarify which task if ambiguous. Never use this for permission " +
				"approvals or create a new task for an answer. answers is an array of plain text " +
				"strings, one per observed question in the SAME order, e.g. [\"5000元\", \"2人\"]. " +
				"Use only explicit user answers; ask for missing answers instead of inventing them. " +
				"Accepted is not completed.",
			map[string]any{
				"job_id":         map[string]any{"type": "string"},
				"interaction_id": map[string]any{"type": "string"},
				"answers": map[string]any{
					"type": "array",
					"description": "Plain text answers in observed question order, e.g. [\"5000元\"]. " +
						"Answer every question exactly once; never copy or paraphrase the question.",
					"minItems": 1,
					"maxItems": 32,
					"items":    map[string]any{"type": "string", "maxLength": 4000},
				},
			},
			[]string{"job_id", "interaction_id", "answers"},
		},
		{
			"jiuwen_task_query",
			"Find tasks and their results in this conversation. Query before selecting an " +
				"ambiguous task; use exact returned job_id and revision for controls. " +
				"query is a literal substring of the original task text, not a question or " +
				"a list of keywords. Prefer a known job_id. If no task matches, shorten query " +
				"or list without query; no match does NOT prove completion or absence of files.",
			map[string]any{
				"job_id": map[string]any{"type": "string"},
				"query":  map[string]any{"type": "string"},
				"offset": map[string]any{"type": "integer", "minimum": 0},
				"status": map[string]any{
					"type": "string",
					"enum": []string{
						"queued", "running", "waiting_user", "cancelling", "unknown",
						"completed", "failed", "cancelled", "unfinished",
					},
				},
			},
			[]string{},
		},
		{
			"jiuwen_task_cancel",
			"Request cancellation of the exact task. Accepted does not mean stopped. Query for the final state.",
			map[string]any{"job_id": map[string]any{"type": "string"}},
			[]string{"job_id"},
		},
		{
			"jiuwen_task_modify",
			"Change the exact task's requirements. Queued input is updated; running changes wait " +
				"for a model checkpoint; finished work creates a linked revision. Receipt never proves" +
				" the requirement is satisfied.",
			map[string]any{
				"job_id":      map[string]any{"type": "string"},
				"revision":    map[string]any{"type": "integer", "minimum": 1},
				"instruction": map[string]any{"type": "string", "maxLength": 4000},
			},
			[]string{"job_id", "revision", "instruction"},
		},
	}
	tools := make([]map[string]any, 0, len(specs))
	for _, s := range specs {
		tools = append(tools, map[string]any{
			"type": "function",
			"function": map[string]any{
				"name":        s.name,
				"description": s.description,
				"parameters": map[string]any{
					"type":                 "object",
					"properties":           s.properties,
					"required":             s.required,
					"additionalProperties": false,
				},
			},
		})
	}
	return tools
}

// ParseQwenOmniToolCall 校验当前委派工具与旧版 research 调用。
// 对齐 qwen_omni_tools.py:124-214 parse_qwen_omni_tool_call。
func ParseQwenOmniToolCall(value any) (QwenOmniToolCall, error) {
	m, ok := value.(map[string]any)
	if !ok {
		return QwenOmniToolCall{}, errors.New("tool call must be an object")
	}
	name := strings.TrimSpace(toString(m["name"]))
	if !taskToolNames[name] && name != QwenOmniDelegateToolName && name != QwenOmniResearchToolName {
		return QwenOmniToolCall{}, fmt.Errorf("unsupported Qwen tool: %s", orEmptyString(name))
	}
	callID := strings.TrimSpace(toString(m["call_id"]))
	if callID == "" || len(callID) > maxCallIDChars {
		return QwenOmniToolCall{}, fmt.Errorf("call_id must contain 1-%d characters", maxCallIDChars)
	}
	rawArguments := m["arguments"]
	var arguments map[string]any
	switch v := rawArguments.(type) {
	case string:
		var parsed any
		if err := json.Unmarshal([]byte(v), &parsed); err != nil {
			return QwenOmniToolCall{}, errors.New("arguments must be valid JSON")
		}
		arguments, ok = parsed.(map[string]any)
		if !ok {
			return QwenOmniToolCall{}, errors.New("arguments must be a JSON object")
		}
	case map[string]any:
		arguments = v
	default:
		return QwenOmniToolCall{}, errors.New("arguments must be a JSON object")
	}
	if taskToolNames[name] {
		schema := taskManagementSchema(name)
		properties, _ := schema["properties"].(map[string]any)
		required, _ := schema["required"].([]string)
		for key := range arguments {
			if _, known := properties[key]; !known {
				return QwenOmniToolCall{}, errors.New("Invalid task tool arguments")
			}
		}
		for _, key := range required {
			if _, present := arguments[key]; !present {
				return QwenOmniToolCall{}, errors.New("Invalid task tool arguments")
			}
		}
		for key, item := range arguments {
			if key == "answers" {
				list, isList := item.([]any)
				if !isList || len(list) < 1 || len(list) > 32 {
					return QwenOmniToolCall{}, errors.New("Invalid answers")
				}
			} else if key == "revision" || key == "offset" || key == "queue_version" {
				num, isNum := item.(float64)
				isInt := isNum && num == float64(int64(num))
				if !isInt || num < minFor(key) {
					return QwenOmniToolCall{}, fmt.Errorf("Invalid %s", key)
				}
			} else {
				s, isStr := item.(string)
				maxLen := 256
				if key == "instruction" {
					maxLen = 4000
				}
				if !isStr || strings.TrimSpace(s) == "" || len(s) > maxLen {
					return QwenOmniToolCall{}, fmt.Errorf("Invalid %s", key)
				}
			}
		}
		if name == "jiuwen_task_query" {
			if status, present := arguments["status"]; present {
				enum := taskQueryStatusEnum()
				if !containsAnyString(enum, toString(status)) {
					return QwenOmniToolCall{}, errors.New("Invalid task status")
				}
			}
		}
		if name == "jiuwen_task_reorder" {
			action := toString(arguments["action"])
			if action != "next" && action != "before" {
				return QwenOmniToolCall{}, errors.New("Invalid queue action: use next or before; before_job_id is a separate field")
			}
			beforeJobID := toString(arguments["before_job_id"])
			if (action == "before") != (beforeJobID != "") {
				return QwenOmniToolCall{}, errors.New("Only before requires before_job_id")
			}
		}
		return QwenOmniToolCall{Name: name, CallID: callID, Arguments: arguments}, nil
	}
	taskFields := 0
	for key := range arguments {
		if key != "independent" && key != "depends_on" && key != "resources" {
			taskFields++
		}
	}
	if taskFields != 1 || (name == QwenOmniResearchToolName && len(arguments) != 1) {
		return QwenOmniToolCall{}, errors.New("arguments must contain exactly one task field")
	}
	if v, present := arguments["independent"]; present {
		if _, isBool := v.(bool); !isBool {
			return QwenOmniToolCall{}, errors.New("independent must be a boolean")
		}
	}
	for _, key := range []string{"depends_on", "resources"} {
		values, present := arguments[key]
		if !present {
			continue
		}
		list, isList := values.([]any)
		if !isList || len(list) > 32 {
			return QwenOmniToolCall{}, fmt.Errorf("Invalid %s", key)
		}
		for _, v := range list {
			s, isStr := v.(string)
			if !isStr || strings.TrimSpace(s) == "" || len(s) > 256 {
				return QwenOmniToolCall{}, fmt.Errorf("Invalid %s", key)
			}
		}
	}
	var argumentName string
	if name == QwenOmniDelegateToolName {
		for _, key := range delegateArgumentNames {
			if _, present := arguments[key]; present {
				argumentName = key
				break
			}
		}
	} else if _, present := arguments["query"]; present {
		argumentName = "query"
	}
	if argumentName == "" {
		return QwenOmniToolCall{}, errors.New("arguments must contain a supported task field")
	}
	rawTask, isStr := arguments[argumentName].(string)
	if !isStr {
		return QwenOmniToolCall{}, fmt.Errorf("%s must be a string", argumentName)
	}
	task := strings.TrimSpace(rawTask)
	if task == "" || len(task) > maxTaskChars {
		return QwenOmniToolCall{}, fmt.Errorf("%s must contain 1-%d characters", argumentName, maxTaskChars)
	}
	return QwenOmniToolCall{Name: name, CallID: callID, Arguments: arguments, Task: task}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// taskManagementSchema 按名取任务工具参数 schema。
// Python 侧无对应实现：Python 直接查 tool definitions，Go 需按名索引。
func taskManagementSchema(name string) map[string]any {
	for _, tool := range TaskManagementTools() {
		function := tool["function"].(map[string]any)
		if function["name"] == name {
			return function["parameters"].(map[string]any)
		}
	}
	return map[string]any{}
}

// taskQueryStatusEnum 任务状态枚举值。
// Python 侧无对应实现：Python 查 schema["properties"]["status"]["enum"]。
func taskQueryStatusEnum() []string {
	schema := taskManagementSchema("jiuwen_task_query")
	status, _ := schema["properties"].(map[string]any)["status"].(map[string]any)
	enum, _ := status["enum"].([]string)
	return enum
}

// minFor 整数参数下界（revision≥1，其余≥0）。
// 对齐 qwen_omni_tools.py:152 `1 if key == "revision" else 0`。
func minFor(key string) float64 {
	if key == "revision" {
		return 1
	}
	return 0
}

// containsAnyString 判定枚举包含。
func containsAnyString(values []string, target string) bool {
	for _, v := range values {
		if v == target {
			return true
		}
	}
	return false
}

// orEmptyString 空安全工具名。
// 对齐 qwen_omni_tools.py:135 name or '<empty>'。
func orEmptyString(name string) string {
	if name == "" {
		return "<empty>"
	}
	return name
}
