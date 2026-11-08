package tool_display

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestInjectCallGoalSchema 验证 call_goal 注入且结构正确
func TestInjectCallGoalSchema(t *testing.T) {
	params := map[string]any{
		"type":       "object",
		"properties": map[string]any{},
	}
	InjectCallGoalSchema(params)

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatalf("properties should be a map")
	}
	goal, ok := props["call_goal"].(map[string]any)
	if !ok {
		t.Fatalf("call_goal not injected")
	}
	if goal["type"] != "string" {
		t.Errorf("call_goal type = %v, want string", goal["type"])
	}
	if desc, _ := goal["description"].(string); desc == "" {
		t.Error("call_goal description should not be empty")
	}
}

// TestInjectCallGoalSchema_无properties 验证自动创建 properties 段
func TestInjectCallGoalSchema_无properties(t *testing.T) {
	params := map[string]any{"type": "object"}
	InjectCallGoalSchema(params)

	props, ok := params["properties"].(map[string]any)
	if !ok {
		t.Fatal("properties should be created")
	}
	if _, exists := props["call_goal"]; !exists {
		t.Error("call_goal should be injected")
	}
}

// TestInjectCallGoalSchema_已有定义不覆盖 验证不覆盖工具自带的 call_goal
func TestInjectCallGoalSchema_已有定义不覆盖(t *testing.T) {
	existing := map[string]any{"type": "string", "description": "custom"}
	params := map[string]any{
		"properties": map[string]any{"call_goal": existing},
	}
	InjectCallGoalSchema(params)

	got := params["properties"].(map[string]any)["call_goal"].(map[string]any)
	if got["description"] != "custom" {
		t.Errorf("existing call_goal overwritten: %v", got)
	}
}

// TestInjectCallGoalSchema_清理误注入的display_name 验证 UI 文案型 display_name 被移除
func TestInjectCallGoalSchema_清理误注入的display_name(t *testing.T) {
	cases := []string{"仅界面展示用", "UI only field", "不影响工具实际执行"}
	for _, marker := range cases {
		params := map[string]any{
			"properties": map[string]any{
				"display_name": map[string]any{"description": marker},
			},
		}
		InjectCallGoalSchema(params)

		props := params["properties"].(map[string]any)
		if _, exists := props["display_name"]; exists {
			t.Errorf("marker %q: UI-style display_name should be removed", marker)
		}
	}
}

// TestInjectCallGoalSchema_保留业务display_name 验证 team 的 display_name 不被误删
func TestInjectCallGoalSchema_保留业务display_name(t *testing.T) {
	params := map[string]any{
		"properties": map[string]any{
			"display_name": map[string]any{"description": "成员展示名，必填"},
		},
	}
	InjectCallGoalSchema(params)

	props := params["properties"].(map[string]any)
	if _, exists := props["display_name"]; !exists {
		t.Error("business display_name must NOT be removed")
	}
}

// TestInjectCallGoalSchema_非map输入不panic 验证非法输入安全返回
func TestInjectCallGoalSchema_非map输入不panic(t *testing.T) {
	// 不应 panic
	InjectCallGoalSchema(nil)
	InjectCallGoalSchema("not a map")
	InjectCallGoalSchema(42)
	InjectCallGoalSchema([]any{1, 2})
}

// TestExtractCallGoal_从map提取 验证提取并剥离 call_goal
func TestExtractCallGoal_从map提取(t *testing.T) {
	args := map[string]any{"call_goal": "调研官网", "path": "/tmp/x"}
	name, cleaned := ExtractCallGoal(args)

	if name != "调研官网" {
		t.Errorf("name = %q, want 调研官网", name)
	}
	m, ok := cleaned.(map[string]any)
	if !ok {
		t.Fatalf("cleaned should be map, got %T", cleaned)
	}
	if _, exists := m["call_goal"]; exists {
		t.Error("call_goal should be stripped")
	}
	if m["path"] != "/tmp/x" {
		t.Errorf("path = %v, want /tmp/x", m["path"])
	}
}

// TestExtractCallGoal_兼容callGoal驼峰 验证两个键名都识别
func TestExtractCallGoal_兼容callGoal驼峰(t *testing.T) {
	name, cleaned := ExtractCallGoal(map[string]any{"callGoal": "驼峰写法", "x": 1})
	if name != "驼峰写法" {
		t.Errorf("name = %q, want 驼峰写法", name)
	}
	if _, exists := cleaned.(map[string]any)["callGoal"]; exists {
		t.Error("callGoal should be stripped")
	}
}

// TestExtractCallGoal_两个键同时存在取第一个 验证按 _CALL_GOAL_KEYS 顺序
func TestExtractCallGoal_两个键同时存在取第一个(t *testing.T) {
	// Python 按 ("call_goal", "callGoal") 顺序，首个非空者胜
	name, cleaned := ExtractCallGoal(map[string]any{
		"call_goal": "下划线优先",
		"callGoal":  "驼峰被忽略",
	})
	if name != "下划线优先" {
		t.Errorf("name = %q, want 下划线优先", name)
	}
	m := cleaned.(map[string]any)
	if _, exists := m["call_goal"]; exists {
		t.Error("call_goal should be stripped")
	}
	if _, exists := m["callGoal"]; exists {
		t.Error("callGoal should also be stripped（两个 key 都会 pop）")
	}
}

// TestExtractCallGoal_从JSON字符串提取 验证字符串输入被解析后返回 JSON
func TestExtractCallGoal_从JSON字符串提取(t *testing.T) {
	raw := `{"call_goal":"字符串输入","x":1}`
	name, cleaned := ExtractCallGoal(raw)

	if name != "字符串输入" {
		t.Errorf("name = %q", name)
	}
	s, ok := cleaned.(string)
	if !ok {
		t.Fatalf("cleaned should be string, got %T", cleaned)
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("cleaned is not valid JSON: %v", err)
	}
	if _, exists := m["call_goal"]; exists {
		t.Error("call_goal should be stripped from JSON string")
	}
	if m["x"] != float64(1) {
		t.Errorf("x = %v, want 1", m["x"])
	}
}

// TestExtractCallGoal_截断到200字符 验证超长被截断并加省略号
func TestExtractCallGoal_截断到200字符(t *testing.T) {
	long := strings.Repeat("a", 300)
	name, _ := ExtractCallGoal(map[string]any{"call_goal": long})

	runes := []rune(name)
	if len(runes) != 201 { // 200 字符 + 省略号
		t.Errorf("len = %d, want 201", len(runes))
	}
	if runes[200] != '…' {
		t.Errorf("should end with ellipsis, got %q", string(runes[200]))
	}
}

// TestExtractCallGoal_压缩空白 验证 " ".join(split()) 语义
func TestExtractCallGoal_压缩空白(t *testing.T) {
	name, _ := ExtractCallGoal(map[string]any{"call_goal": "  多个   空白\n\t换行  "})
	if name != "多个 空白 换行" {
		t.Errorf("name = %q, want %q", name, "多个 空白 换行")
	}
}

// TestExtractCallGoal_不触碰display_name 验证业务字段 display_name 保留
func TestExtractCallGoal_不触碰display_name(t *testing.T) {
	name, cleaned := ExtractCallGoal(map[string]any{
		"display_name": "player-x",
		"call_goal":    "通知落子",
	})

	if name != "通知落子" {
		t.Errorf("name = %q", name)
	}
	m := cleaned.(map[string]any)
	if m["display_name"] != "player-x" {
		t.Errorf("display_name = %v, want player-x（业务字段不可剥离）", m["display_name"])
	}
}

// TestExtractCallGoal_空白与非法输入 验证返回空名与原始值
func TestExtractCallGoal_空白与非法输入(t *testing.T) {
	if name, raw := ExtractCallGoal("  "); name != "" || raw != "  " {
		t.Errorf("whitespace: name=%q raw=%v", name, raw)
	}
	if name, raw := ExtractCallGoal("{invalid json"); name != "" || raw != "{invalid json" {
		t.Errorf("invalid json: name=%q raw=%v", name, raw)
	}
	if name, raw := ExtractCallGoal(nil); name != "" || raw != nil {
		t.Errorf("nil: name=%q raw=%v", name, raw)
	}
}

// TestExtractCallGoal_JSON非对象返回原值 验证 parsed 非 Mapping 时不动
func TestExtractCallGoal_JSON非对象返回原值(t *testing.T) {
	raw := `["a","b"]`
	name, cleaned := ExtractCallGoal(raw)
	if name != "" {
		t.Errorf("name = %q, want empty", name)
	}
	if cleaned != raw {
		t.Errorf("cleaned = %v, want original", cleaned)
	}
}

// TestExtractCallGoal_空call_goal不取 验证空串跳过
func TestExtractCallGoal_空call_goal不取(t *testing.T) {
	name, cleaned := ExtractCallGoal(map[string]any{"call_goal": "   ", "callGoal": "有效值"})
	if name != "有效值" {
		t.Errorf("name = %q, want 有效值（空串应跳过取下一个）", name)
	}
	m := cleaned.(map[string]any)
	if len(m) != 0 {
		t.Errorf("cleaned should be empty, got %v", m)
	}
}
