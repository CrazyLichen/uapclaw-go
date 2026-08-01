package session

import (
	"testing"
	"time"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAutoTitle_基本截取(t *testing.T) {
	longContent := "这是一段很长的用户消息内容，超过了五十个字符的限制所以需要被截断加上省略号标记"
	result := AutoTitle(longContent)
	if len(result) != titleMaxLen+3 { // 50 + "..."
		t.Errorf("AutoTitle 长度 = %d, 期望 %d", len(result), titleMaxLen+3)
	}
	if !stringsHasSuffix(result, "...") {
		t.Errorf("AutoTitle 应以 ... 结尾, 实际: %q", result)
	}
}

func TestAutoTitle_换行替换(t *testing.T) {
	content := "第一行\n第二行\n第三行"
	result := AutoTitle(content)
	if stringsContains(result, "\n") {
		t.Errorf("AutoTitle 应替换换行, 实际: %q", result)
	}
}

func TestAutoTitle_短内容不截断(t *testing.T) {
	content := "短消息"
	result := AutoTitle(content)
	if result != "短消息" {
		t.Errorf("AutoTitle 短内容不应截断, 期望 %q, 实际 %q", "短消息", result)
	}
}

func TestSerializeValue_timeTime(t *testing.T) {
	now := time.Date(2026, 1, 15, 10, 30, 0, 0, time.UTC)
	result := SerializeValue(now)
	str, ok := result.(string)
	if !ok {
		t.Fatalf("SerializeValue(time.Time) 应返回 string, 实际: %T", result)
	}
	if str != "2026-01-15T10:30:00Z" {
		t.Errorf("时间格式不匹配, 实际: %q", str)
	}
}

func TestSerializeValue_map递归(t *testing.T) {
	input := map[string]any{
		"nested": map[string]any{
			"time_field": time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}
	result := SerializeValue(input)
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("应返回 map[string]any, 实际: %T", result)
	}
	nested, ok := m["nested"].(map[string]any)
	if !ok {
		t.Fatalf("nested 应为 map[string]any, 实际: %T", m["nested"])
	}
	timeVal, ok := nested["time_field"].(string)
	if !ok {
		t.Fatalf("time_field 应为 string, 实际: %T", nested["time_field"])
	}
	if timeVal != "2026-01-01T00:00:00Z" {
		t.Errorf("time_field 值不匹配, 实际: %q", timeVal)
	}
}

func TestSerializeValue_slice递归(t *testing.T) {
	input := []any{time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC), "hello"}
	result := SerializeValue(input)
	slice, ok := result.([]any)
	if !ok {
		t.Fatalf("应返回 []any, 实际: %T", result)
	}
	if len(slice) != 2 {
		t.Fatalf("长度 = %d, 期望 2", len(slice))
	}
	strVal, ok := slice[0].(string)
	if !ok {
		t.Fatalf("slice[0] 应为 string, 实际: %T", slice[0])
	}
	_ = strVal
	if slice[1] != "hello" {
		t.Errorf("slice[1] = %q, 期望 %q", slice[1], "hello")
	}
}

func TestSerializeValue_普通值不变(t *testing.T) {
	result := SerializeValue("plain string")
	if result != "plain string" {
		t.Errorf("普通字符串应不变, 实际: %q", result)
	}
	result2 := SerializeValue(42)
	if result2 != 42 {
		t.Errorf("普通数字应不变, 实际: %v", result2)
	}
}

func TestDeepCopyMap(t *testing.T) {
	src := map[string]any{
		"key":  "value",
		"nested": map[string]any{
			"inner": "data",
		},
	}
	dst := DeepCopyMap(src)

	// 修改 dst 不影响 src
	dst["key"] = "modified"
	dst["nested"].(map[string]any)["inner"] = "changed"

	if src["key"] != "value" {
		t.Errorf("深拷贝应不共享引用, src[key] = %q", src["key"])
	}
	if src["nested"].(map[string]any)["inner"] != "data" {
		t.Errorf("深拷贝应不共享嵌套引用, src[nested][inner] = %q", src["nested"].(map[string]any)["inner"])
	}
}

func TestDerefStr(t *testing.T) {
	s := "hello"
	if DerefStr(&s, "fallback") != "hello" {
		t.Errorf("DerefStr(&s) = %q, 期望 %q", DerefStr(&s, "fallback"), "hello")
	}
	if DerefStr(nil, "fallback") != "fallback" {
		t.Errorf("DerefStr(nil) = %q, 期望 %q", DerefStr(nil, "fallback"), "fallback")
	}
}

func TestMakeSessionID(t *testing.T) {
	id := MakeSessionID()
	if len(id) < 10 {
		t.Errorf("session ID 太短: %q", id)
	}
	prefix := "sess_"
	if len(id) < len(prefix) || id[:len(prefix)] != prefix {
		t.Errorf("session ID 前缀不匹配: %q, 期望以 %q 开头", id, prefix)
	}
}

func TestNormalizeSessionID(t *testing.T) {
	if NormalizeSessionID("") != "default" {
		t.Errorf("空串应归一化为 default, 实际: %q", NormalizeSessionID(""))
	}
	if NormalizeSessionID("sess_1") != "sess_1" {
		t.Errorf("非空串应保持不变, 实际: %q", NormalizeSessionID("sess_1"))
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// stringsHasSuffix 辅助判断后缀（避免 import strings 仅用于测试）
func stringsHasSuffix(s, suffix string) bool {
	return len(s) >= len(suffix) && s[len(s)-len(suffix):] == suffix
}

func stringsContains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
