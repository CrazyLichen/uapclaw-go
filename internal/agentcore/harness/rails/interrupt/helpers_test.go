package interrupt

import (
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestConvertInteractionsToAskUserQuestion_空输入(t *testing.T) {
	result := ConvertInteractionsToAskUserQuestion(nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}

	result = ConvertInteractionsToAskUserQuestion([]any{})
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}

	result = ConvertInteractionsToAskUserQuestion("")
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

func TestConvertInteractionsToAskUserQuestion_AskUserInterrupt(t *testing.T) {
	// 对齐 Python: AskUserRail 中断，value 有 questions 字段
	stateOutputs := []any{
		map[string]any{
			"id":    "req_001",
			"value": map[string]any{
				"questions": []any{
					map[string]any{
						"question":     "你想要哪种方案？",
						"header":       "方案选择",
						"options":      []any{},
						"multi_select": false,
					},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["event_type"] != "chat.ask_user_question" {
		t.Errorf("期望 event_type=chat.ask_user_question，实际 %v", result["event_type"])
	}
	if result["request_id"] != "req_001" {
		t.Errorf("期望 request_id=req_001，实际 %v", result["request_id"])
	}
	if result["source"] != "ask_user_interrupt" {
		t.Errorf("期望 source=ask_user_interrupt，实际 %v", result["source"])
	}

	questions, ok := result["questions"].([]map[string]any)
	if !ok {
		t.Fatalf("期望 questions 为 []map[string]any，实际 %T", result["questions"])
	}
	if len(questions) != 1 {
		t.Errorf("期望 1 个 question，实际 %d", len(questions))
	}
}

func TestConvertInteractionsToAskUserQuestion_AskUserInterrupt带选项(t *testing.T) {
	stateOutputs := []any{
		map[string]any{
			"id":    "req_002",
			"value": map[string]any{
				"questions": []any{
					map[string]any{
						"question":     "选择分支",
						"header":       "分支选择",
						"options":      []any{
							map[string]any{"label": "Option A", "description": "方案A"},
							map[string]any{"label": "Option B", "description": "方案B"},
						},
						"multi_select": true,
					},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}

	questions := result["questions"].([]map[string]any)
	q := questions[0]
	options := q["options"].([]map[string]any)

	// 期望有 2 个原始选项 + 1 个 Other
	if len(options) != 3 {
		t.Errorf("期望 3 个选项（含 Other），实际 %d", len(options))
	}
	if options[2]["label"] != "Other" {
		t.Errorf("期望最后一个选项为 Other，实际 %v", options[2]["label"])
	}
	if q["multi_select"] != true {
		t.Errorf("期望 multi_select=true，实际 %v", q["multi_select"])
	}
}

func TestConvertInteractionsToAskUserQuestion_PermissionInterrupt(t *testing.T) {
	// 对齐 Python: PermissionRail 中断，value 无 questions 字段
	stateOutputs := []any{
		map[string]any{
			"id":    "perm_001",
			"value": map[string]any{
				"message":   "需要授权",
				"tool_name": "bash",
				"ui_options": []any{
					map[string]any{"label": "Allow", "description": "允许执行"},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["source"] != "permission_interrupt" {
		t.Errorf("期望 source=permission_interrupt，实际 %v", result["source"])
	}

	questions := result["questions"].([]map[string]any)
	if len(questions) != 1 {
		t.Errorf("期望 1 个 question，实际 %d", len(questions))
	}
	q := questions[0]
	if q["question"] != "需要授权" {
		t.Errorf("期望 question='需要授权'，实际 %v", q["question"])
	}
	if q["header"] != "权限审批: bash" {
		t.Errorf("期望 header='权限审批: bash'，实际 %v", q["header"])
	}
}

func TestConvertInteractionsToAskUserQuestion_PermissionInterrupt默认选项(t *testing.T) {
	// 无 ui_options → 使用默认权限审批选项
	stateOutputs := []any{
		map[string]any{
			"id":    "perm_002",
			"value": map[string]any{
				"message":   "",
				"tool_name": "write_file",
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}

	questions := result["questions"].([]map[string]any)
	q := questions[0]
	if q["question"] != "工具 `write_file` 需要授权才能执行" {
		t.Errorf("期望默认 message，实际 %v", q["question"])
	}
	options := q["options"].([]map[string]any)
	if len(options) != 3 {
		t.Errorf("期望 3 个默认选项，实际 %d", len(options))
	}
}

func TestConvertInteractionsToAskUserQuestion_toolArgs中的questions(t *testing.T) {
	// 对齐 Python: StructuredAskUserRail 路径，questions 嵌入 tool_args
	stateOutputs := []any{
		map[string]any{
			"id":    "req_003",
			"value": map[string]any{
				"tool_args": map[string]any{
					"questions": []any{
						map[string]any{
							"question":     "你的偏好？",
							"header":       "偏好选择",
							"options":      []any{},
							"multi_select": false,
						},
					},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["source"] != "ask_user_interrupt" {
		t.Errorf("期望 source=ask_user_interrupt，实际 %v", result["source"])
	}
}

func TestConvertInteractionsToAskUserQuestion_toolArgs为JSON字符串(t *testing.T) {
	// tool_args 是 JSON string 格式
	stateOutputs := []any{
		map[string]any{
			"id":    "req_004",
			"value": map[string]any{
				"tool_args": `{"questions":[{"question":"选择语言","header":"语言","options":[],"multi_select":false}]}`,
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["source"] != "ask_user_interrupt" {
		t.Errorf("期望 source=ask_user_interrupt，实际 %v", result["source"])
	}
}

func TestConvertInteractionsToAskUserQuestion_嵌套列表(t *testing.T) {
	// 对齐 Python: _iter_interactions 递归展开
	stateOutputs := []any{
		[]any{
			map[string]any{
				"id":    "nested_001",
				"value": map[string]any{
					"questions": []any{
						map[string]any{"question": "嵌套问题", "header": "H", "options": []any{}},
					},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["request_id"] != "nested_001" {
		t.Errorf("期望 request_id=nested_001，实际 %v", result["request_id"])
	}
}

func TestConvertInteractionsToAskUserQuestion_空requestID跳过(t *testing.T) {
	stateOutputs := []any{
		map[string]any{
			"id":    "",
			"value": map[string]any{
				"questions": []any{
					map[string]any{"question": "Q", "header": "H", "options": []any{}},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result != nil {
		t.Errorf("空 request_id 应跳过，期望 nil，实际 %v", result)
	}
}

func TestConvertInteractionsToAskUserQuestion_优先AskUserInterrupt(t *testing.T) {
	// 第一个有 questions（AskUser），第二个没有（Permission）
	// 应优先返回 AskUser
	stateOutputs := []any{
		map[string]any{
			"id":    "perm_only",
			"value": map[string]any{
				"message":   "需要权限",
				"tool_name": "bash",
			},
		},
		map[string]any{
			"id":    "ask_user",
			"value": map[string]any{
				"questions": []any{
					map[string]any{"question": "选择", "header": "H", "options": []any{}},
				},
			},
		},
	}

	result := ConvertInteractionsToAskUserQuestion(stateOutputs)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["source"] != "ask_user_interrupt" {
		t.Errorf("期望优先 AskUser，实际 %v", result["source"])
	}
}

func TestExtractQuestionFromInteraction_无Value(t *testing.T) {
	payload := map[string]any{
		"message": "直接消息",
	}
	result := extractQuestionFromInteraction(payload)
	if result == nil {
		t.Fatal("期望非 nil 结果")
	}
	if result["question"] != "直接消息" {
		t.Errorf("期望 question='直接消息'，实际 %v", result["question"])
	}
}

func TestExtractQuestionFromInteraction_Nil(t *testing.T) {
	result := extractQuestionFromInteraction(nil)
	if result != nil {
		t.Errorf("期望 nil，实际 %v", result)
	}
}

func TestStrVal(t *testing.T) {
	m := map[string]any{"key": "value", "empty": "", "int": 42}
	if strVal(m, "key") != "value" {
		t.Errorf("期望 'value'")
	}
	if strVal(m, "empty") != "" {
		t.Errorf("期望空字符串")
	}
	if strVal(m, "int") != "" {
		t.Errorf("非字符串类型期望空字符串")
	}
	if strVal(m, "missing") != "" {
		t.Errorf("缺失键期望空字符串")
	}
}

func TestToSlice(t *testing.T) {
	// []any
	s1, ok1 := toSlice([]any{1, 2, 3})
	if !ok1 || len(s1) != 3 {
		t.Errorf("[]any 期望 ok=true, len=3")
	}

	// []map[string]any
	s2, ok2 := toSlice([]map[string]any{{"a": "b"}})
	if !ok2 || len(s2) != 1 {
		t.Errorf("[]map 期望 ok=true, len=1")
	}

	// nil
	_, ok3 := toSlice(nil)
	if ok3 {
		t.Errorf("nil 期望 ok=false")
	}

	// 其他类型
	_, ok4 := toSlice("string")
	if ok4 {
		t.Errorf("string 期望 ok=false")
	}
}

func TestMessageOrDefault(t *testing.T) {
	if messageOrDefault("已有消息", "tool") != "已有消息" {
		t.Errorf("有消息时应返回原消息")
	}
	if messageOrDefault("", "bash") != "工具 `bash` 需要授权才能执行" {
		t.Errorf("空消息时应返回默认模板")
	}
}

func TestHeaderFromToolName(t *testing.T) {
	if headerFromToolName("bash") != "权限审批: bash" {
		t.Errorf("有 toolName 时应返回带名称的 header")
	}
	if headerFromToolName("") != "权限审批" {
		t.Errorf("空 toolName 时应返回默认 header")
	}
}
