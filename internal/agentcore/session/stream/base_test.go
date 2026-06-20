package stream

import "testing"

// TestStreamMode_Mode 测试 StreamMode 的 Mode() 方法
func TestStreamMode_Mode(t *testing.T) {
	tests := []struct {
		mode     StreamMode
		expected string
	}{
		{StreamModeOutput, "output"},
		{StreamModeTrace, "trace"},
		{StreamModeCustom, "custom"},
	}
	for _, tt := range tests {
		if got := tt.mode.Mode(); got != tt.expected {
			t.Errorf("StreamMode.Mode() = %q, want %q", got, tt.expected)
		}
	}
}

// TestStreamMode_Desc 测试 StreamMode 的 Desc() 方法
func TestStreamMode_Desc(t *testing.T) {
	if StreamModeOutput.Desc() == "" {
		t.Error("StreamModeOutput.Desc() 不应为空")
	}
	if StreamModeTrace.Desc() == "" {
		t.Error("StreamModeTrace.Desc() 不应为空")
	}
	if StreamModeCustom.Desc() == "" {
		t.Error("StreamModeCustom.Desc() 不应为空")
	}
}

// TestStreamMode_Options 测试 StreamMode 的 Options() 方法
func TestStreamMode_Options(t *testing.T) {
	// 内置模式的 options 应为空 map
	if opts := StreamModeOutput.Options(); len(opts) != 0 {
		t.Errorf("StreamModeOutput.Options() = %v, want empty map", opts)
	}

	// 自定义模式可携带 options
	custom := NewStreamMode("custom_mode", "自定义模式", map[string]any{"key": "value"})
	if opts := custom.Options(); opts["key"] != "value" {
		t.Errorf("custom.Options()[\"key\"] = %v, want \"value\"", opts["key"])
	}
}

// TestStreamMode_String 测试 StreamMode 的 String() 方法
func TestStreamMode_String(t *testing.T) {
	// 内置模式应包含 mode 标识
	s := StreamModeOutput.String()
	if s == "" {
		t.Error("StreamModeOutput.String() 不应为空")
	}
	// String 应包含 mode 值 "output"
	if len(s) < len("StreamMode(") {
		t.Errorf("StreamModeOutput.String() = %q, 格式不正确", s)
	}
}

// TestNewStreamMode 测试创建自定义流模式
func TestNewStreamMode(t *testing.T) {
	// 无 options
	m1 := NewStreamMode("my_mode", "我的模式")
	if m1.Mode() != "my_mode" {
		t.Errorf("Mode() = %q, want %q", m1.Mode(), "my_mode")
	}
	if m1.Desc() != "我的模式" {
		t.Errorf("Desc() = %q, want %q", m1.Desc(), "我的模式")
	}
	if len(m1.Options()) != 0 {
		t.Errorf("Options() = %v, want empty map", m1.Options())
	}

	// 有 options
	m2 := NewStreamMode("my_mode2", "模式2", map[string]any{"k1": "v1"})
	if m2.Options()["k1"] != "v1" {
		t.Errorf("Options()[\"k1\"] = %v, want \"v1\"", m2.Options()["k1"])
	}
}

// TestStreamMode_MapKey 测试 StreamMode 作为 map key（通过 Mode() 字符串）
func TestStreamMode_MapKey(t *testing.T) {
	m := make(map[string]string)
	m[StreamModeOutput.Mode()] = "output_value"
	m[StreamModeTrace.Mode()] = "trace_value"

	if m[StreamModeOutput.Mode()] != "output_value" {
		t.Errorf("map[StreamModeOutput.Mode()] = %q, want %q", m[StreamModeOutput.Mode()], "output_value")
	}
}

// TestOutputSchema_SchemaType 测试 OutputSchema 实现 Schema 接口
func TestOutputSchema_SchemaType(t *testing.T) {
	s := OutputSchema{Type: "message", Index: 0, Payload: "hello"}
	if s.SchemaType() != "message" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "message")
	}
}

// TestTraceSchema_SchemaType 测试 TraceSchema 实现 Schema 接口
func TestTraceSchema_SchemaType(t *testing.T) {
	s := TraceSchema{Type: "step", Payload: "data"}
	if s.SchemaType() != "step" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "step")
	}
}

// TestCustomSchema_SchemaType 测试 CustomSchema 实现 Schema 接口
func TestCustomSchema_SchemaType(t *testing.T) {
	s := CustomSchema{Type: "my_event", Data: map[string]any{"key": "val"}}
	if s.SchemaType() != "my_event" {
		t.Errorf("SchemaType() = %q, want %q", s.SchemaType(), "my_event")
	}
}

// TestSchema 接口多态测试
func TestSchema(t *testing.T) {
	var schemas = []Schema{
		OutputSchema{Type: "message", Index: 0, Payload: "hello"},
		TraceSchema{Type: "step", Payload: "data"},
		CustomSchema{Type: "event", Data: map[string]any{"k": "v"}},
	}
	expected := []string{"message", "step", "event"}
	for i, s := range schemas {
		if s.SchemaType() != expected[i] {
			t.Errorf("schemas[%d].SchemaType() = %q, want %q", i, s.SchemaType(), expected[i])
		}
	}
}

// ──────────────────────────── Validate 校验测试 ────────────────────────────

// TestOutputSchema_Validate 测试 OutputSchema 字段校验
func TestOutputSchema_Validate(t *testing.T) {
	tests := []struct {
		name    string
		schema  OutputSchema
		wantErr bool
	}{
		{"合法数据", OutputSchema{Type: "message", Index: 0, Payload: "hello"}, false},
		{"Type为空", OutputSchema{Type: "", Index: 0, Payload: "hello"}, true},
		{"Type为空格", OutputSchema{Type: "  ", Index: 0, Payload: "hello"}, true},
		{"Index为负数", OutputSchema{Type: "message", Index: -1, Payload: "hello"}, true},
		{"Index为零合法", OutputSchema{Type: "message", Index: 0, Payload: nil}, false},
		{"Payload为nil合法", OutputSchema{Type: "message", Index: 1, Payload: nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("OutputSchema.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestTraceSchema_Validate 测试 TraceSchema 字段校验
func TestTraceSchema_Validate(t *testing.T) {
	tests := []struct {
		name    string
		schema  TraceSchema
		wantErr bool
	}{
		{"合法数据", TraceSchema{Type: "step", Payload: "data"}, false},
		{"Type为空", TraceSchema{Type: "", Payload: "data"}, true},
		{"Type为空格", TraceSchema{Type: "  ", Payload: "data"}, true},
		{"Payload为nil合法", TraceSchema{Type: "step", Payload: nil}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("TraceSchema.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

// TestCustomSchema_Validate 测试 CustomSchema 字段校验（对齐 Python extra="allow"，几乎不校验）
func TestCustomSchema_Validate(t *testing.T) {
	tests := []struct {
		name    string
		schema  CustomSchema
		wantErr bool
	}{
		{"合法数据", CustomSchema{Type: "event", Data: map[string]any{"key": "val"}}, false},
		{"Type为空不报错", CustomSchema{Type: "", Data: map[string]any{"key": "val"}}, false},
		{"Data为nil不报错", CustomSchema{Type: "event", Data: nil}, false},
		{"全空不报错", CustomSchema{}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.schema.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("CustomSchema.Validate() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
