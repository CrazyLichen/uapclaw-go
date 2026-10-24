package utils

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractJSONFromText 对齐 Python TestExtractJsonFromText（7 用例）
func TestExtractJSONFromText(t *testing.T) {
	t.Run("json代码块提取", func(t *testing.T) {
		if got := ExtractJSONFromText("```json\n{\"key\": \"value\"}\n```"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("裸代码块提取", func(t *testing.T) {
		if got := ExtractJSONFromText("```\n{\"key\": \"value\"}\n```"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("无代码块返回原文", func(t *testing.T) {
		if got := ExtractJSONFromText("{\"key\": \"value\"}"); got != "{\"key\": \"value\"}" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("空文本", func(t *testing.T) {
		if got := ExtractJSONFromText(""); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("None转空串等价", func(t *testing.T) {
		// Python: extract_json_from_text(None) 返回 None；Go 字符串零值即 ""
		if got := ExtractJSONFromText(""); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("json数组", func(t *testing.T) {
		if got := ExtractJSONFromText("```json\n[1, 2, 3]\n```"); got != "[1, 2, 3]" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("多行json", func(t *testing.T) {
		got := ExtractJSONFromText("```json\n{\"key1\": \"value1\",\n\"key2\": \"value2\"}\n```")
		if !strings.Contains(got, "key1") || !strings.Contains(got, "key2") {
			t.Errorf("got %q", got)
		}
	})
}

// TestFormatDialogHistory 对齐 Python TestFormatDialogHistory（5 用例）
func TestFormatDialogHistory(t *testing.T) {
	t.Run("单条消息", func(t *testing.T) {
		got := FormatDialogHistory([]map[string]string{{"role": "user", "content": "Hello"}})
		if got != "user: Hello" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("多条消息", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi! How can I help you?"},
		}
		got := FormatDialogHistory(history)
		expected := "user: Hello\nassistant: Hi! How can I help you?"
		if got != expected {
			t.Errorf("got %q, expected %q", got, expected)
		}
	})
	t.Run("空历史", func(t *testing.T) {
		if got := FormatDialogHistory(nil); got != "" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("自定义分隔符", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user", "content": "Hello"},
			{"role": "assistant", "content": "Hi"},
		}
		got := FormatDialogHistory(history, " | ")
		if got != "user: Hello | assistant: Hi" {
			t.Errorf("got %q", got)
		}
	})
	t.Run("缺失键", func(t *testing.T) {
		history := []map[string]string{
			{"role": "user"},            // 无 content
			{"content": "Missing role"}, // 无 role
		}
		got := FormatDialogHistory(history)
		if !strings.Contains(got, "user: ") || !strings.Contains(got, "unknown: Missing role") {
			t.Errorf("got %q", got)
		}
	})
}

// TestValidateSessionID 对齐 Python TestValidateSessionId（8 用例）
func TestValidateSessionID(t *testing.T) {
	t.Run("字母数字有效", func(t *testing.T) {
		if !ValidateSessionID("abc123XYZ") {
			t.Error("应为有效")
		}
	})
	t.Run("下划线有效", func(t *testing.T) {
		if !ValidateSessionID("user_001_session") {
			t.Error("应为有效")
		}
	})
	t.Run("连字符有效", func(t *testing.T) {
		if !ValidateSessionID("session-12345") {
			t.Error("应为有效")
		}
	})
	t.Run("组合有效", func(t *testing.T) {
		if !ValidateSessionID("test-session-2024") {
			t.Error("应为有效")
		}
	})
	t.Run("特殊字符无效", func(t *testing.T) {
		if ValidateSessionID("session@123") {
			t.Error("应为无效")
		}
	})
	t.Run("空格无效", func(t *testing.T) {
		if ValidateSessionID("session 123") {
			t.Error("应为无效")
		}
	})
	t.Run("空串无效", func(t *testing.T) {
		if ValidateSessionID("") {
			t.Error("应为无效")
		}
	})
	t.Run("None转空串等价", func(t *testing.T) {
		// Python: validate_session_id(None) is False；Go 字符串零值即 ""
		if ValidateSessionID("") {
			t.Error("应为无效")
		}
	})
}

// TestMergeDictLists 对齐 Python TestMergeDictLists（6 用例）
func TestMergeDictLists(t *testing.T) {
	t.Run("唯一键合并", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"id": "2", "name": "B"}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 2 || result[0]["id"] != "1" || result[1]["id"] != "2" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("重复键保留首个", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"id": "1", "name": "B"}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 || result[0]["name"] != "A" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空新增", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		result := MergeDictLists(existing, nil, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空存量", func(t *testing.T) {
		newItems := []map[string]any{{"id": "1", "name": "A"}}
		result := MergeDictLists([]map[string]any{}, newItems, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("双空", func(t *testing.T) {
		result := MergeDictLists(nil, nil, "id")
		if len(result) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("缺失唯一键跳过", func(t *testing.T) {
		existing := []map[string]any{{"id": "1", "name": "A"}}
		newItems := []map[string]any{{"name": "B"}} // 无 id 键
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空容器键值跳过_列表", func(t *testing.T) {
		// Python: bool([]) == False，unique_key 值为空列表时整条跳过
		existing := []map[string]any{{"id": "1"}}
		newItems := []map[string]any{{"id": []any{}}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 {
			t.Errorf("空列表键值应跳过，result=%v", result)
		}
	})
	t.Run("空容器键值跳过_字典", func(t *testing.T) {
		// Python: bool({}) == False，unique_key 值为空字典时整条跳过
		existing := []map[string]any{{"id": "1"}}
		newItems := []map[string]any{{"id": map[string]any{}}}
		result := MergeDictLists(existing, newItems, "id")
		if len(result) != 1 {
			t.Errorf("空字典键值应跳过，result=%v", result)
		}
	})
	// 注：非空容器键值（如 []any{1}）在两侧均为崩溃路径，非对齐修复范围——
	// Python: key_value not in existing_keys → TypeError: unhashable type: 'list'
	// Go: existingKeys[keyValue] → runtime panic: hash of unhashable type
	// 空容器跳过（isTruthy 的 []any/map[string]any case）正是防止该崩溃的唯一安全路径。
}

// TestIsTruthy 对齐 Python 隐式 bool() 全类型矩阵（Go 增强覆盖，Python 侧无直接单测）
func TestIsTruthy(t *testing.T) {
	cases := []struct {
		name     string
		value    any
		expected bool
	}{
		{"nil为falsy", nil, false},
		{"空串为falsy", "", false},
		{"非空串为truthy", "a", true},
		{"false为falsy", false, false},
		{"true为truthy", true, true},
		{"零int为falsy", 0, false},
		{"非零int为truthy", 1, true},
		{"零int64为falsy", int64(0), false},
		{"零float64为falsy", 0.0, false},
		{"非零float64为truthy", 0.1, true},
		{"空列表为falsy", []any{}, false},
		{"非空列表为truthy", []any{1}, true},
		{"空字典为falsy", map[string]any{}, false},
		{"非空字典为truthy", map[string]any{"k": 1}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTruthy(c.value); got != c.expected {
				t.Errorf("isTruthy(%#v) = %v, 期望 %v（对齐 Python bool() 语义）", c.value, got, c.expected)
			}
		})
	}
}

// TestSafeJSONLoads 对齐 Python TestSafeJsonLoads（5 用例 + 未传默认值错误分支）
func TestSafeJSONLoads(t *testing.T) {
	t.Run("有效JSON", func(t *testing.T) {
		result, err := SafeJSONLoads(`{"key": "value"}`)
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok || m["key"] != "value" {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("无效JSON返回默认值", func(t *testing.T) {
		result, err := SafeJSONLoads("invalid json", map[string]any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		m, ok := result.(map[string]any)
		if !ok || len(m) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("空串返回默认值", func(t *testing.T) {
		result, err := SafeJSONLoads("", []any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if arr, ok := result.([]any); !ok || len(arr) != 0 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("None转空串等价返回默认值", func(t *testing.T) {
		// Python: safe_json_loads(None, default=[]) → []
		result, err := SafeJSONLoads("", []any{})
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if _, ok := result.([]any); !ok {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("JSON数组", func(t *testing.T) {
		result, err := SafeJSONLoads("[1, 2, 3]")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		arr, ok := result.([]any)
		if !ok || len(arr) != 3 || arr[0] != float64(1) {
			t.Errorf("result=%v（JSON number 在 Go 反序列化为 float64）", result)
		}
	})
	t.Run("未传默认值时解析失败返回错误", func(t *testing.T) {
		// Python: default=None 时抛 COMMON_JSON_INPUT_PROCESS_ERROR(188002)
		_, err := SafeJSONLoads("invalid json")
		if err == nil {
			t.Fatal("期望返回错误")
		}
	})
	t.Run("未传默认值时空串返回nil", func(t *testing.T) {
		// Python: if not text: return default(None)
		result, err := SafeJSONLoads("")
		if err != nil {
			t.Fatalf("unexpected err: %v", err)
		}
		if result != nil {
			t.Errorf("result=%v, 期望 nil", result)
		}
	})
}

// TestLoadJSONFile Go 增强用例（Python 无直接单测，retriever 层 mock 覆盖；本包补 5 分支）
func TestLoadJSONFile(t *testing.T) {
	t.Run("文件不存在", func(t *testing.T) {
		_, err := LoadJSONFile(filepath.Join(t.TempDir(), "nonexist.json"))
		if !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("期望 fs.ErrNotExist, got %v", err)
		}
	})
	t.Run("空文件返回空map", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "empty.json")
		if err := os.WriteFile(path, []byte(""), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadJSONFile(path)
		if err != nil || len(result) != 0 {
			t.Errorf("result=%v err=%v", result, err)
		}
	})
	t.Run("空白文件返回空map", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "blank.json")
		if err := os.WriteFile(path, []byte("   \n  "), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadJSONFile(path)
		if err != nil || len(result) != 0 {
			t.Errorf("result=%v err=%v", result, err)
		}
	})
	t.Run("顶层非dict报错", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "array.json")
		if err := os.WriteFile(path, []byte("[1, 2, 3]"), 0o600); err != nil {
			t.Fatal(err)
		}
		_, err := LoadJSONFile(path)
		if err == nil {
			t.Error("期望顶层非 dict 报错")
		}
	})
	t.Run("正常dict返回内容", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "ok.json")
		if err := os.WriteFile(path, []byte(`{"key": "value"}`), 0o600); err != nil {
			t.Fatal(err)
		}
		result, err := LoadJSONFile(path)
		if err != nil || result["key"] != "value" {
			t.Errorf("result=%v err=%v", result, err)
		}
	})
}

// TestDeepMergeDict 对齐 Python TestDeepMergeDict（5 用例 + 嵌套层增强断言）
func TestDeepMergeDict(t *testing.T) {
	t.Run("简单合并", func(t *testing.T) {
		result := DeepMergeDict(map[string]any{"a": 1}, map[string]any{"b": 2})
		if result["a"] != 1 || result["b"] != 2 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("嵌套合并", func(t *testing.T) {
		base := map[string]any{"a": map[string]any{"b": 1, "c": 2}}
		update := map[string]any{"a": map[string]any{"b": 3, "d": 4}}
		result := DeepMergeDict(base, update)
		inner := result["a"].(map[string]any)
		if inner["b"] != 3 || inner["c"] != 2 || inner["d"] != 4 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("非dict值覆盖", func(t *testing.T) {
		result := DeepMergeDict(map[string]any{"a": 1}, map[string]any{"a": 2})
		if result["a"] != 2 {
			t.Errorf("result=%v", result)
		}
	})
	t.Run("不改原map", func(t *testing.T) {
		base := map[string]any{"a": 1}
		result := DeepMergeDict(base, map[string]any{"b": 2})
		if _, exists := base["b"]; exists {
			t.Error("base 不应被修改")
		}
		if _, exists := result["b"]; !exists {
			t.Error("result 应含 b")
		}
	})
	t.Run("嵌套层不改原map_增强断言", func(t *testing.T) {
		// Python 测试只断言顶层；Go 增强为嵌套层也验证（测试增强非偏差）
		base := map[string]any{"a": map[string]any{"x": 1}}
		result := DeepMergeDict(base, map[string]any{"a": map[string]any{"y": 2}})
		inner := result["a"].(map[string]any)
		inner["z"] = 3 // 修改结果嵌套层
		baseInner := base["a"].(map[string]any)
		if _, exists := baseInner["z"]; exists {
			t.Error("base 嵌套层不应被修改（递归必须新建 map）")
		}
		if _, exists := baseInner["y"]; exists {
			t.Error("base 嵌套层不应被合并写入")
		}
	})
	t.Run("双空", func(t *testing.T) {
		result := DeepMergeDict(map[string]any{}, map[string]any{})
		if len(result) != 0 {
			t.Errorf("result=%v", result)
		}
	})
}

// TestUtils集成_JSON提取解析工作流 对齐 Python system test_extract_and_parse_json_workflow
func TestUtils集成_JSON提取解析工作流(t *testing.T) {
	llmResponse := "\n        Here is the generated DSL:\n        ```json\n        {\n            \"name\": \"Test Agent\",\n            \"type\": \"llm_agent\",\n            \"config\": {\n                \"temperature\": 0.7,\n                \"max_tokens\": 1000\n            }\n        }\n        ```\n        Please review and confirm.\n        "
	jsonStr := ExtractJSONFromText(llmResponse)
	if jsonStr == "" {
		t.Fatal("提取结果为空")
	}
	parsed, err := SafeJSONLoads(jsonStr)
	if err != nil {
		t.Fatalf("解析失败: %v", err)
	}
	m := parsed.(map[string]any)
	if m["name"] != "Test Agent" {
		t.Errorf("name=%v", m["name"])
	}
	if m["config"].(map[string]any)["temperature"] != 0.7 {
		t.Errorf("temperature=%v", m["config"])
	}
}

// TestUtils集成_对话历史格式化 对齐 Python system test_dialog_history_formatting_integration
func TestUtils集成_对话历史格式化(t *testing.T) {
	history := []map[string]string{
		{"role": "user", "content": "创建一个助手"},
		{"role": "assistant", "content": "好的，请告诉我助手名称"},
		{"role": "user", "content": "叫小助手"},
	}
	formatted := FormatDialogHistory(history)
	for _, expect := range []string{"user: 创建一个助手", "assistant: 好的，请告诉我助手名称", "user: 叫小助手"} {
		if !strings.Contains(formatted, expect) {
			t.Errorf("formatted=%q 缺少 %q", formatted, expect)
		}
	}
}

// TestUtils集成_真实模式SessionID校验 对齐 Python system test_session_id_validation_with_real_patterns
func TestUtils集成_真实模式SessionID校验(t *testing.T) {
	validIDs := []string{"session-12345", "user_001_session", "abc123XYZ", "test-session-2024"}
	for _, id := range validIDs {
		if !ValidateSessionID(id) {
			t.Errorf("%q 应为有效", id)
		}
	}
}
