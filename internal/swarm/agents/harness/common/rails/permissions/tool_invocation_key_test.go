package permissions

import (
	"strings"
	"testing"
)

// ──────────────────────────── NormalizeToolInvocationText 测试 ────────────────────────────

// TestNormalizeToolInvocationText_正常值 验证常规字符串被 trim 后返回
func TestNormalizeToolInvocationText_正常值(t *testing.T) {
	got, err := NormalizeToolInvocationText("invocation_id", "  abc  ", 128)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got != "abc" {
		t.Fatalf("期望 abc，实际 %q", got)
	}
}

// TestNormalizeToolInvocationText_空串报错 验证空串与纯空白均报错
func TestNormalizeToolInvocationText_空串报错(t *testing.T) {
	for _, value := range []any{"", "   ", "\t\n"} {
		if _, err := NormalizeToolInvocationText("invocation_id", value, 128); err == nil {
			t.Fatalf("value=%q 应报错", value)
		}
	}
}

// TestNormalizeToolInvocationText_非字符串视为空 验证非 string 类型按 Python isinstance 语义判空
func TestNormalizeToolInvocationText_非字符串视为空(t *testing.T) {
	for _, value := range []any{nil, 123, true, []string{"a"}, map[string]any{}} {
		_, err := NormalizeToolInvocationText("invocation_id", value, 128)
		if err == nil {
			t.Fatalf("value=%v 应报错", value)
		}
		if !strings.Contains(err.Error(), "invalid invocation_id") {
			t.Fatalf("错误信息应含字段名，实际 %v", err)
		}
	}
}

// TestNormalizeToolInvocationText_超长报错 验证超过上限报错、等于上限通过
func TestNormalizeToolInvocationText_超长报错(t *testing.T) {
	exact := strings.Repeat("a", 10)
	if _, err := NormalizeToolInvocationText("f", exact, 10); err != nil {
		t.Fatalf("等于上限应通过: %v", err)
	}
	over := strings.Repeat("a", 11)
	if _, err := NormalizeToolInvocationText("f", over, 10); err == nil {
		t.Fatalf("超过上限应报错")
	}
}

// TestNormalizeToolInvocationText_trim后判长度 验证长度按 trim 后计算
func TestNormalizeToolInvocationText_trim后判长度(t *testing.T) {
	// 原始 12 字符，trim 后 10 字符，上限 10 → 通过
	if _, err := NormalizeToolInvocationText("f", "  aaaaaaaaaa  ", 10); err != nil {
		t.Fatalf("trim 后应通过: %v", err)
	}
}

// ──────────────────────────── NewToolInvocationKeyV1 测试 ────────────────────────────

// validKeyArgs 返回一组合法的构造参数
func validKeyArgs() (string, string, string, string, string, string, int) {
	return "inv-1", "sess-1", "req-1", ExecutorKindAgent, "exec-1", "call-1", ToolInvocationKeyVersion
}

// TestNewToolInvocationKeyV1_完整字段 验证合法参数正确构造
func TestNewToolInvocationKeyV1_完整字段(t *testing.T) {
	key, err := NewToolInvocationKeyV1(validKeyArgs())
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if key.InvocationID != "inv-1" || key.RootSessionID != "sess-1" ||
		key.RequestID != "req-1" || key.ExecutorKind != ExecutorKindAgent ||
		key.ExecutionSessionID != "exec-1" || key.ToolCallID != "call-1" ||
		key.Version != ToolInvocationKeyVersion {
		t.Fatalf("字段不符: %+v", key)
	}
}

// TestNewToolInvocationKeyV1_版本非法 验证 version 必须为 1
func TestNewToolInvocationKeyV1_版本非法(t *testing.T) {
	inv, root, req, kind, exec, call, _ := validKeyArgs()
	for _, v := range []int{0, 2, -1} {
		if _, err := NewToolInvocationKeyV1(inv, root, req, kind, exec, call, v); err == nil {
			t.Fatalf("version=%d 应报错", v)
		}
	}
}

// TestNewToolInvocationKeyV1_executorKind非法 验证 executor_kind 只能是 agent
func TestNewToolInvocationKeyV1_executorKind非法(t *testing.T) {
	inv, root, req, _, exec, call, ver := validKeyArgs()
	for _, kind := range []string{"", "tool", "Agent", "subagent"} {
		if _, err := NewToolInvocationKeyV1(inv, root, req, kind, exec, call, ver); err == nil {
			t.Fatalf("executor_kind=%q 应报错", kind)
		}
	}
}

// TestNewToolInvocationKeyV1_invocationID长度上限 验证 invocation_id 用 128 而非 512
func TestNewToolInvocationKeyV1_invocationID长度上限(t *testing.T) {
	_, root, req, kind, exec, call, ver := validKeyArgs()

	ok := strings.Repeat("a", MaxToolInvocationIDLength)
	if _, err := NewToolInvocationKeyV1(ok, root, req, kind, exec, call, ver); err != nil {
		t.Fatalf("等于上限应通过: %v", err)
	}
	tooLong := strings.Repeat("a", MaxToolInvocationIDLength+1)
	if _, err := NewToolInvocationKeyV1(tooLong, root, req, kind, exec, call, ver); err == nil {
		t.Fatalf("超过上限应报错")
	}
}

// TestNewToolInvocationKeyV1_其余字段用512上限 验证非 invocation_id 字段的上限是 512
func TestNewToolInvocationKeyV1_其余字段用512上限(t *testing.T) {
	inv, _, req, kind, exec, call, ver := validKeyArgs()

	at512 := strings.Repeat("b", 512)
	if _, err := NewToolInvocationKeyV1(inv, at512, req, kind, exec, call, ver); err != nil {
		t.Fatalf("512 应通过: %v", err)
	}
	at513 := strings.Repeat("b", 513)
	if _, err := NewToolInvocationKeyV1(inv, at513, req, kind, exec, call, ver); err == nil {
		t.Fatalf("513 应报错")
	}
}

// TestNewToolInvocationKeyV1_各字段空串报错 验证五个身份字段逐个判空
func TestNewToolInvocationKeyV1_各字段空串报错(t *testing.T) {
	cases := []struct {
		name string
		args [6]string
	}{
		{"invocation_id", [6]string{"", "s", "r", ExecutorKindAgent, "e", "c"}},
		{"root_session_id", [6]string{"i", "", "r", ExecutorKindAgent, "e", "c"}},
		{"request_id", [6]string{"i", "s", "", ExecutorKindAgent, "e", "c"}},
		{"execution_session_id", [6]string{"i", "s", "r", ExecutorKindAgent, "", "c"}},
		{"tool_call_id", [6]string{"i", "s", "r", ExecutorKindAgent, "e", ""}},
	}
	for _, c := range cases {
		a := c.args
		_, err := NewToolInvocationKeyV1(a[0], a[1], a[2], a[3], a[4], a[5], ToolInvocationKeyVersion)
		if err == nil {
			t.Fatalf("%s 为空应报错", c.name)
		}
		if !strings.Contains(err.Error(), c.name) {
			t.Fatalf("%s 错误信息应含字段名，实际 %v", c.name, err)
		}
	}
}

// TestNewToolInvocationKeyV1_字段trim 验证各字段被 trim
func TestNewToolInvocationKeyV1_字段trim(t *testing.T) {
	key, err := NewToolInvocationKeyV1(" inv ", " sess ", " req ", ExecutorKindAgent, " exec ", " call ", ToolInvocationKeyVersion)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if key.InvocationID != "inv" || key.RootSessionID != "sess" || key.RequestID != "req" ||
		key.ExecutionSessionID != "exec" || key.ToolCallID != "call" {
		t.Fatalf("trim 失败: %+v", key)
	}
}

// ──────────────────────────── ToWire / FromWire 测试 ────────────────────────────

// TestToolInvocationKeyV1_ToWire 验证 wire 表示包含全部 7 个字段
func TestToolInvocationKeyV1_ToWire(t *testing.T) {
	key, err := NewToolInvocationKeyV1(validKeyArgs())
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	wire := key.ToWire()
	if len(wire) != 7 {
		t.Fatalf("wire 应有 7 个字段，实际 %d", len(wire))
	}
	expect := map[string]any{
		"version":              1,
		"invocation_id":        "inv-1",
		"root_session_id":      "sess-1",
		"request_id":           "req-1",
		"executor_kind":        "agent",
		"execution_session_id": "exec-1",
		"tool_call_id":         "call-1",
	}
	for k, want := range expect {
		if wire[k] != want {
			t.Fatalf("wire[%s] 期望 %v，实际 %v", k, want, wire[k])
		}
	}
}

// TestToolInvocationKeyV1_往返 验证 ToWire / FromWire 往返一致
func TestToolInvocationKeyV1_往返(t *testing.T) {
	key, err := NewToolInvocationKeyV1(validKeyArgs())
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}
	back, err := ToolInvocationKeyV1FromWire(key.ToWire())
	if err != nil {
		t.Fatalf("反序列化失败: %v", err)
	}
	if *back != *key {
		t.Fatalf("往返不一致: %+v vs %+v", back, key)
	}
}

// TestToolInvocationKeyV1FromWire_缺字段 验证字段缺失报错
func TestToolInvocationKeyV1FromWire_缺字段(t *testing.T) {
	key, _ := NewToolInvocationKeyV1(validKeyArgs())
	full := key.ToWire()
	for missing := range full {
		partial := make(map[string]any, len(full)-1)
		for k, v := range full {
			if k != missing {
				partial[k] = v
			}
		}
		if _, err := ToolInvocationKeyV1FromWire(partial); err == nil {
			t.Fatalf("缺 %s 应报错", missing)
		}
	}
}

// TestToolInvocationKeyV1FromWire_多字段 验证多余字段报错
func TestToolInvocationKeyV1FromWire_多字段(t *testing.T) {
	key, _ := NewToolInvocationKeyV1(validKeyArgs())
	full := key.ToWire()
	full["extra"] = "x"
	if _, err := ToolInvocationKeyV1FromWire(full); err == nil {
		t.Fatalf("多余字段应报错")
	}
}

// TestToolInvocationKeyV1FromWire_nil 验证 nil 映射报错
func TestToolInvocationKeyV1FromWire_nil(t *testing.T) {
	if _, err := ToolInvocationKeyV1FromWire(nil); err == nil {
		t.Fatalf("nil 应报错")
	}
}

// TestToolInvocationKeyV1FromWire_version非int 验证 version 类型非 int 报错
func TestToolInvocationKeyV1FromWire_version非int(t *testing.T) {
	key, _ := NewToolInvocationKeyV1(validKeyArgs())
	full := key.ToWire()
	for _, bad := range []any{"1", 1.0, nil, true} {
		m := make(map[string]any, len(full))
		for k, v := range full {
			m[k] = v
		}
		m["version"] = bad
		if _, err := ToolInvocationKeyV1FromWire(m); err == nil {
			t.Fatalf("version=%v(%T) 应报错", bad, bad)
		}
	}
}

// TestToolInvocationKeyV1FromWire_字段非字符串 验证非字符串身份字段按非法处理
func TestToolInvocationKeyV1FromWire_字段非字符串(t *testing.T) {
	key, _ := NewToolInvocationKeyV1(validKeyArgs())
	full := key.ToWire()
	m := make(map[string]any, len(full))
	for k, v := range full {
		m[k] = v
	}
	m["tool_call_id"] = 123
	if _, err := ToolInvocationKeyV1FromWire(m); err == nil {
		t.Fatalf("非字符串 tool_call_id 应报错")
	}
}
