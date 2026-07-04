package schema

import (
	"encoding/json"
	"testing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestAgentMode_默认值为Normal(t *testing.T) {
	var m AgentMode
	if m != AgentModeNormal {
		t.Errorf("AgentMode 零值应为 AgentModeNormal(0)，实际为 %d", m)
	}
}

func TestAgentMode_String(t *testing.T) {
	tests := []struct {
		mode AgentMode
		want string
	}{
		{AgentModeNormal, "normal"},
		{AgentModePlan, "plan"},
		{AgentMode(99), "unknown(99)"},
	}
	for _, tt := range tests {
		if got := tt.mode.String(); got != tt.want {
			t.Errorf("AgentMode(%d).String() = %q，期望 %q", tt.mode, got, tt.want)
		}
	}
}

func TestAgentMode_MarshalJSON(t *testing.T) {
	tests := []struct {
		mode AgentMode
		want string
	}{
		{AgentModeNormal, `"normal"`},
		{AgentModePlan, `"plan"`},
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

func TestAgentMode_UnmarshalJSON(t *testing.T) {
	tests := []struct {
		input string
		want  AgentMode
	}{
		{`"normal"`, AgentModeNormal},
		{`"plan"`, AgentModePlan},
		{`"NORMAL"`, AgentModeNormal},
		{`"Plan"`, AgentModePlan},
	}
	for _, tt := range tests {
		var m AgentMode
		if err := json.Unmarshal([]byte(tt.input), &m); err != nil {
			t.Errorf("UnmarshalJSON(%s) 出错: %v", tt.input, err)
			continue
		}
		if m != tt.want {
			t.Errorf("UnmarshalJSON(%s) = %d，期望 %d", tt.input, m, tt.want)
		}
	}
}

func TestAgentMode_UnmarshalJSON_无效值(t *testing.T) {
	var m AgentMode
	if err := json.Unmarshal([]byte(`"invalid"`), &m); err == nil {
		t.Error("UnmarshalJSON 无效值应返回错误")
	}
}

func TestAgentMode_UnmarshalJSON_非字符串(t *testing.T) {
	var m AgentMode
	if err := json.Unmarshal([]byte(`123`), &m); err == nil {
		t.Error("UnmarshalJSON 非字符串输入应返回错误")
	}
}

func TestAgentMode_JSON往返(t *testing.T) {
	modes := []AgentMode{AgentModeNormal, AgentModePlan}
	for _, original := range modes {
		data, err := json.Marshal(original)
		if err != nil {
			t.Errorf("Marshal 出错: %v", err)
			continue
		}
		var decoded AgentMode
		if err := json.Unmarshal(data, &decoded); err != nil {
			t.Errorf("Unmarshal 出错: %v", err)
			continue
		}
		if decoded != original {
			t.Errorf("JSON 往返: 原值 %d，解码后 %d", original, decoded)
		}
	}
}

func TestParseAgentMode(t *testing.T) {
	tests := []struct {
		input string
		want  AgentMode
		err   bool
	}{
		{"normal", AgentModeNormal, false},
		{"plan", AgentModePlan, false},
		{"NORMAL", AgentModeNormal, false},
		{"Plan", AgentModePlan, false},
		{"invalid", AgentModeNormal, true},
		{"", AgentModeNormal, true},
	}
	for _, tt := range tests {
		got, err := ParseAgentMode(tt.input)
		if tt.err {
			if err == nil {
				t.Errorf("ParseAgentMode(%q) 应返回错误", tt.input)
			}
		} else {
			if err != nil {
				t.Errorf("ParseAgentMode(%q) 出错: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("ParseAgentMode(%q) = %d，期望 %d", tt.input, got, tt.want)
			}
		}
	}
}
