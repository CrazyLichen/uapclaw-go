package checkpointer

import (
	"testing"
)

// ──────────────────────────── JSONSerializer 测试 ────────────────────────────

// TestNewJSONSerializer 测试创建实例
func TestNewJSONSerializer(t *testing.T) {
	s := NewJSONSerializer()
	if s == nil {
		t.Fatal("NewJSONSerializer 返回 nil")
	}
}

// TestJSONSerializer_DumpsTyped_基本 测试基本序列化
func TestJSONSerializer_DumpsTyped_基本(t *testing.T) {
	s := NewJSONSerializer()
	tag, data, err := s.DumpsTyped(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("DumpsTyped 返回错误：%v", err)
	}
	if tag != "json" {
		t.Errorf("格式标签期望 'json'，实际=%s", tag)
	}
	if len(data) == 0 {
		t.Error("序列化数据不应为空")
	}
}

// TestJSONSerializer_DumpsTyped_嵌套结构 测试嵌套 map 序列化
func TestJSONSerializer_DumpsTyped_嵌套结构(t *testing.T) {
	s := NewJSONSerializer()
	obj := map[string]any{
		"global_state": map[string]any{"k1": "v1"},
		"agent_state":  map[string]any{"k2": 42},
	}
	tag, _, err := s.DumpsTyped(obj)
	if err != nil {
		t.Fatalf("DumpsTyped 返回错误：%v", err)
	}
	if tag != "json" {
		t.Errorf("格式标签期望 'json'，实际=%s", tag)
	}
}

// TestJSONSerializer_LoadsTyped_json标签 测试 json 格式标签反序列化
func TestJSONSerializer_LoadsTyped_json标签(t *testing.T) {
	s := NewJSONSerializer()
	// 先序列化
	_, data, err := s.DumpsTyped(map[string]any{"key": "value"})
	if err != nil {
		t.Fatalf("DumpsTyped 返回错误：%v", err)
	}
	// 再反序列化
	result, err := s.LoadsTyped("json", data)
	if err != nil {
		t.Fatalf("LoadsTyped 返回错误：%v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际=%T", result)
	}
	if m["key"] != "value" {
		t.Errorf("key 期望 'value'，实际=%v", m["key"])
	}
}

// TestJSONSerializer_LoadsTyped_非json标签 测试非 json 格式标签返回 nil
func TestJSONSerializer_LoadsTyped_非json标签(t *testing.T) {
	s := NewJSONSerializer()
	result, err := s.LoadsTyped("pickle", []byte("data"))
	if err != nil {
		t.Fatalf("非 json 标签不应返回错误：%v", err)
	}
	if result != nil {
		t.Errorf("非 json 标签应返回 nil，实际=%v", result)
	}
}

// TestJSONSerializer_往返测试 测试序列化-反序列化往返一致性
func TestJSONSerializer_往返测试(t *testing.T) {
	s := NewJSONSerializer()
	original := map[string]any{
		"global_state": map[string]any{"name": "test"},
		"agent_state":  map[string]any{"count": float64(10)},
	}
	tag, data, err := s.DumpsTyped(original)
	if err != nil {
		t.Fatalf("DumpsTyped 错误：%v", err)
	}
	result, err := s.LoadsTyped(tag, data)
	if err != nil {
		t.Fatalf("LoadsTyped 错误：%v", err)
	}
	m, ok := result.(map[string]any)
	if !ok {
		t.Fatalf("期望 map[string]any，实际=%T", result)
	}
	if m["global_state"] == nil || m["agent_state"] == nil {
		t.Error("往返后丢失数据")
	}
}

// TestJSONSerializer_LoadsTyped_nil输入 测试 nil 输入返回 nil
// 对应 Python: JsonSerializer.loads_typed() 中 if data is None: return None
func TestJSONSerializer_LoadsTyped_nil输入(t *testing.T) {
	s := NewJSONSerializer()
	result, err := s.LoadsTyped("json", nil)
	if err != nil {
		t.Fatalf("nil 输入不应返回错误：%v", err)
	}
	if result != nil {
		t.Errorf("nil 输入应返回 nil，实际=%v", result)
	}
}
