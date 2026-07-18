package trajectory

import "testing"

// TestJSONSafe_nil 验证 nil 返回 nil
func TestJSONSafe_nil(t *testing.T) {
	if JSONSafe(nil) != nil {
		t.Error("JSONSafe(nil) should return nil")
	}
}

// TestJSONSafe_基础类型 验证基础类型原值返回
func TestJSONSafe_基础类型(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  any
	}{
		{"string", "hello", "hello"},
		{"int", 42, 42},
		{"float64", 3.14, 3.14},
		{"bool", true, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if JSONSafe(tt.input) != tt.want {
				t.Errorf("JSONSafe(%v) = %v, want %v", tt.input, JSONSafe(tt.input), tt.want)
			}
		})
	}
}

// TestJSONSafe_切片 验证切片递归
func TestJSONSafe_切片(t *testing.T) {
	input := []any{"a", 1, true}
	result := JSONSafe(input).([]any)
	if len(result) != 3 {
		t.Fatalf("len = %d, want 3", len(result))
	}
	if result[0] != "a" || result[1] != 1 || result[2] != true {
		t.Errorf("JSONSafe slice result = %v, want [a 1 true]", result)
	}
}

// TestJSONSafe_映射 验证映射递归
func TestJSONSafe_映射(t *testing.T) {
	input := map[string]any{"key": "value", "num": 42}
	result := JSONSafe(input).(map[string]any)
	if result["key"] != "value" || result["num"] != 42 {
		t.Errorf("JSONSafe map result = %v", result)
	}
}

// TestJSONSafe_嵌套 验证嵌套结构递归
func TestJSONSafe_嵌套(t *testing.T) {
	input := map[string]any{
		"list": []any{1, "two"},
		"nested": map[string]any{
			"deep": true,
		},
	}
	result := JSONSafe(input).(map[string]any)
	list := result["list"].([]any)
	if list[0] != 1 || list[1] != "two" {
		t.Errorf("nested list = %v", list)
	}
	nested := result["nested"].(map[string]any)
	if nested["deep"] != true {
		t.Errorf("nested.deep = %v, want true", nested["deep"])
	}
}

// TestJSONSafe_自定义对象兜底 验证自定义对象走 json.Marshal 路径
func TestJSONSafe_自定义对象兜底(t *testing.T) {
	type testObj struct {
		Name string `json:"name"`
		Age  int    `json:"age"`
	}
	obj := testObj{Name: "test", Age: 25}
	result := JSONSafe(obj)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("JSONSafe custom object returned %T, want map[string]any", result)
	}
	if m["name"] != "test" {
		t.Errorf("name = %v, want test", m["name"])
	}
	// JSON number 解析为 float64（设计决策中已记录此差异）
	if age, _ := m["age"].(float64); age != 25 {
		t.Errorf("age = %v, want 25", m["age"])
	}
}

// TestJSONSafe_不可序列化对象 验证 Marshal 失败时走 fmt.Sprint 兜底
func TestJSONSafe_不可序列化对象(t *testing.T) {
	// channel 不可 json.Marshal
	ch := make(chan int)
	result := JSONSafe(ch)
	str, ok := result.(string)
	if !ok {
		t.Fatalf("JSONSafe unserializable returned %T, want string", result)
	}
	if str == "" {
		t.Error("fallback string should not be empty")
	}
}

// TestMessageToDict_map输入 验证已经是 map 的情况
func TestMessageToDict_map输入(t *testing.T) {
	input := map[string]any{"role": "user", "content": "hello"}
	result := MessageToDict(input)
	if result["role"] != "user" {
		t.Errorf("role = %v, want user", result["role"])
	}
	if result["content"] != "hello" {
		t.Errorf("content = %v, want hello", result["content"])
	}
}

// TestMessageToDict_结构体输入 验证结构体走 JSON 序列化路径
func TestMessageToDict_结构体输入(t *testing.T) {
	type testMsg struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	}
	msg := testMsg{Role: "assistant", Content: "response"}
	result := MessageToDict(msg)
	if result["role"] != "assistant" {
		t.Errorf("role = %v, want assistant", result["role"])
	}
}

// TestMessageToDict_nil 验证 nil 输入
func TestMessageToDict_nil(t *testing.T) {
	result := MessageToDict(nil)
	if result["role"] != "unknown" {
		t.Errorf("role = %v, want unknown", result["role"])
	}
}

// TestMessageToDict_不可序列化 验证兜底
func TestMessageToDict_不可序列化(t *testing.T) {
	ch := make(chan int)
	result := MessageToDict(ch)
	if result["role"] != "unknown" {
		t.Errorf("role = %v, want unknown", result["role"])
	}
	if result["content"] == "" {
		t.Error("content should not be empty in fallback")
	}
}

// TestResponseToText_map_content 验证 map 取 content 键
func TestResponseToText_map_content(t *testing.T) {
	result := responseToText(map[string]any{"content": "hello world"})
	if result != "hello world" {
		t.Errorf("responseToText = %q, want %q", result, "hello world")
	}
}

// TestResponseToText_map_text 验证 map 取 text 键（无 content 时）
func TestResponseToText_map_text(t *testing.T) {
	result := responseToText(map[string]any{"text": "hello"})
	if result != "hello" {
		t.Errorf("responseToText = %q, want %q", result, "hello")
	}
}

// TestResponseToText_map_content为空时取text 验证 content 为空时回退到 text
func TestResponseToText_map_content为空时取text(t *testing.T) {
	result := responseToText(map[string]any{"content": "", "text": "from_text"})
	if result != "from_text" {
		t.Errorf("responseToText = %q, want %q", result, "from_text")
	}
}

// TestResponseToText_nil 验证 nil 返回空字符串
func TestResponseToText_nil(t *testing.T) {
	result := responseToText(nil)
	if result != "" {
		t.Errorf("responseToText(nil) = %q, want empty", result)
	}
}

// TestResponseToText_兜底 验证其他类型走 fmt.Sprint
func TestResponseToText_兜底(t *testing.T) {
	result := responseToText(42)
	if result != "42" {
		t.Errorf("responseToText(42) = %q, want %q", result, "42")
	}
}
