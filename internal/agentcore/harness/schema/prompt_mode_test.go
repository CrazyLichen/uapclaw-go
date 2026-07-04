package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestPromptMode_默认值为Full(t *testing.T) {
	var m PromptMode
	if m != PromptModeFull {
		t.Errorf("PromptMode 零值应为 PromptModeFull(0)，实际为 %d", m)
	}
}

func TestPromptMode_String(t *testing.T) {
	tests := []struct {
		mode PromptMode
		want string
	}{
		{PromptModeFull, "full"},
		{PromptModeMinimal, "minimal"},
		{PromptModeNone, "none"},
		{PromptMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("PromptMode(%d).String() = %q，期望 %q", tt.mode, got, tt.want)
		}
	}
}

func TestPromptMode_MarshalJSON(t *testing.T) {
	tests := []struct {
		mode PromptMode
		want string
	}{
		{PromptModeFull, `"full"`},
		{PromptModeMinimal, `"minimal"`},
		{PromptModeNone, `"none"`},
	}
	for _, tt := range tests {
		data, err := json.Marshal(tt.mode)
		if err != nil {
			t.Errorf("MarshalJSON(%d) 出错: %v", tt.mode, err)
			continue
		}
		if string(data) != tt.want {
			t.Errorf("MarshalJSON(%d) = %s，期望 %s", tt.mode, data, tt.want)
		}
	}
}

func TestPromptMode_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  PromptMode
	}{
		{`"full"`, PromptModeFull},
		{`"minimal"`, PromptModeMinimal},
		{`"none"`, PromptModeNone},
		{`"FULL"`, PromptModeFull},
		{`"Minimal"`, PromptModeMinimal},
	}
	for _, tt := range tests {
		var m PromptMode
		if err := json.Unmarshal([]byte(tt.input), &m); err != nil {
			t.Errorf("UnmarshalJSON(%s) 出错: %v", tt.input, err)
			continue
		}
		if m != tt.want {
			t.Errorf("UnmarshalJSON(%s) = %d，期望 %d", tt.input, m, tt.want)
		}
	}
}

func TestPromptMode_UnmarshalJSON_无效值(t *testing.T) {
	var m PromptMode
	if err := json.Unmarshal([]byte(`"invalid"`), &m); err == nil {
		t.Error("UnmarshalJSON 无效值应返回错误")
	}
}

func TestPromptMode_UnmarshalJSON_非字符串(t *testing.T) {
	var m PromptMode
	if err := json.Unmarshal([]byte(`123`), &m); err == nil {
		t.Error("UnmarshalJSON 非字符串输入应返回错误")
	}
}

func TestPromptMode_JSON往返(t *testing.T) {
	modes := []PromptMode{PromptModeFull, PromptModeMinimal, PromptModeNone}
	for _, original := range modes {
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("Marshal 出错: %v", err)
			continue
		}
		var decoded PromptMode
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("Unmarshal 出错: %v", err)
			continue
		}
		if decoded != original {
			t.Errorf("JSON 往返: 原值 %d，解码后 %d", original, decoded)
		}
	}
}

func TestParsePromptMode(t *testing.T) {
	tests := []struct {
		input string
		want  PromptMode
		err   bool
	}{
		{"full", PromptModeFull, false},
		{"minimal", PromptModeMinimal, false},
		{"none", PromptModeNone, false},
		{"FULL", PromptModeFull, false},
		{"Minimal", PromptModeMinimal, false},
		{"invalid", PromptModeFull, true},
		{"", PromptModeFull, true},
	}
	for _, tt := range tests {
		got, err := ParsePromptMode(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParsePromptMode(%q) 应返回错误", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParsePromptMode(%q) 出错: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParsePromptMode(%q) = %d，期望 %d", tt.input, got, tt.want)
			}
		}
	}
}

func TestPromptMode_在结构体中序列化(t *testing.T) {
	type config struct {
		Mode PromptMode `json:"mode"`
	}
	original := config{Mode: PromptModeMinimal}
	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("Marshal 出错: %v", err)
	}
	want := `{"mode":"minimal"}`
	if string(data) != want {
		t.Errorf("结构体 Marshal = %s，期望 %s", data, want)
	}
	var decoded config
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("Unmarshal 出错: %v", err)
	}
	if decoded.Mode != original.Mode {
		t.Errorf("结构体往返: 原值 %d，解码后 %d", original.Mode, decoded.Mode)
	}
}
