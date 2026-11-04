package schema

import (
	"encoding/json"
	"testing"
)

// TestAllModes_数量 验证 AllModes 返回全部 10 个枚举值（4 agent + 4 team + design + 裸 team）
func TestAllModes_数量(t *testing.T) {
	modes := AllModes()
	if len(modes) != 10 {
		t.Fatalf("AllModes() 返回 %d 个模式，want 10", len(modes))
	}
}

// TestAllModes_无重复 验证无重复枚举值
func TestAllModes_无重复(t *testing.T) {
	modes := AllModes()
	seen := make(map[Mode]bool)
	for _, m := range modes {
		if seen[m] {
			t.Errorf("重复模式: %q", m)
		}
		seen[m] = true
	}
}

// TestAllModes_包含全部 验证 6 个常量都在列表中
func TestAllModes_包含全部(t *testing.T) {
	modes := AllModes()
	seen := make(map[Mode]bool)
	for _, m := range modes {
		seen[m] = true
	}
	allConstants := []Mode{
		ModeAgentWorkPlan,
		ModeAgentWorkNormal,
		ModeCodePlan,
		ModeCodeNormal,
		ModeCodeTeam,
		ModeTeam,
	}
	for _, c := range allConstants {
		if !seen[c] {
			t.Errorf("缺少常量: %q", c)
		}
	}
}

// TestParseMode_合法值 验证全部 6 个标准值解析正确
func TestParseMode_合法值(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"agent.work.plan", ModeAgentWorkPlan},
		{"agent.work.normal", ModeAgentWorkNormal},
		{"agent.code.plan", ModeCodePlan},
		{"agent.code.normal", ModeCodeNormal},
		{"team.code.normal", ModeCodeTeam},
		{"team", ModeTeamWorkNormal},
	}
	for _, tt := range tests {
		got := ParseMode(tt.input, ModeAgentWorkPlan)
		if got != tt.want {
			t.Errorf("ParseMode(%q, ModeAgentWorkPlan) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseMode_大小写与空白 验证 strip + lower 后正确解析
func TestParseMode_大小写与空白(t *testing.T) {
	tests := []struct {
		input string
		want  Mode
	}{
		{"  AGENT.WORK.PLAN  ", ModeAgentWorkPlan},
		{"AGENT.WORK.NORMAL", ModeAgentWorkNormal},
		{" CODE.NORMAL ", ModeCodeNormal},
		{"CODE.TEAM", ModeCodeTeam},
		{"  TEAM  ", ModeTeamWorkNormal},
	}
	for _, tt := range tests {
		got := ParseMode(tt.input, ModeAgentWorkPlan)
		if got != tt.want {
			t.Errorf("ParseMode(%q, ModeAgentWorkPlan) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

// TestParseMode_非法值回退 验证非法字符串回退到 default
func TestParseMode_非法值回退(t *testing.T) {
	invalidInputs := []string{
		"invalid",
		"unknown.mode",
		"agent.",
		".plan",
		"AGENT_PLAN",
		"agent/plan",
	}
	for _, input := range invalidInputs {
		got := ParseMode(input, ModeCodeNormal)
		if got != ModeCodeNormal {
			t.Errorf("ParseMode(%q, ModeCodeNormal) = %q, want ModeCodeNormal", input, got)
		}
	}
}

// TestParseMode_空字符串回退 验证空/纯空白回退到 default
func TestParseMode_空字符串回退(t *testing.T) {
	blankInputs := []string{"", "   ", "\t", "\n", "  \t\n  "}
	for _, input := range blankInputs {
		got := ParseMode(input, ModeAgentWorkPlan)
		if got != ModeAgentWorkPlan {
			t.Errorf("ParseMode(%q, ModeAgentWorkPlan) = %q, want ModeAgentWorkPlan", input, got)
		}
	}
}

// TestParseMode_默认值参数 验证不同 default 参数生效
func TestParseMode_默认值参数(t *testing.T) {
	// 非法值 + ModeCodeNormal 默认 → ModeCodeNormal
	if got := ParseMode("invalid", ModeCodeNormal); got != ModeCodeNormal {
		t.Errorf("ParseMode(%q, ModeCodeNormal) = %q, want ModeCodeNormal", "invalid", got)
	}
	// 非法值 + ModeTeam 默认 → ModeTeam
	if got := ParseMode("invalid", ModeTeam); got != ModeTeam {
		t.Errorf("ParseMode(%q, ModeTeam) = %q, want ModeTeam", "invalid", got)
	}
	// 空字符串 + ModeAgentWorkNormal 默认 → ModeAgentWorkNormal
	if got := ParseMode("", ModeAgentWorkNormal); got != ModeAgentWorkNormal {
		t.Errorf("ParseMode(%q, ModeAgentWorkNormal) = %q, want ModeAgentWorkNormal", "", got)
	}
}

// TestIsValidMode 验证 IsValidMode 对合法/非法值的判断
func TestIsValidMode(t *testing.T) {
	// 合法值
	validModes := []string{
		"agent.work.plan",
		"agent.work.normal",
		"agent.code.plan",
		"agent.code.normal",
		"team.code.normal",
		"team",
	}
	for _, v := range validModes {
		if !IsValidMode(v) {
			t.Errorf("IsValidMode(%q) = false, want true", v)
		}
	}

	// 非法值
	invalidModes := []string{
		"",
		"invalid",
		"nonexistent.mode",
		"nonexistent",
	}
	for _, v := range invalidModes {
		if IsValidMode(v) {
			t.Errorf("IsValidMode(%q) = true, want false", v)
		}
	}
}

// TestMode_String 验证 String() 返回原始字符串值
func TestMode_String(t *testing.T) {
	if got := ModeAgentWorkPlan.String(); got != "agent.work.plan" {
		t.Errorf("ModeAgentWorkPlan.String() = %q, want %q", got, "agent.work.plan")
	}
	if got := ModeCodeNormal.String(); got != "agent.code.normal" {
		t.Errorf("ModeCodeNormal.String() = %q, want %q", got, "agent.code.normal")
	}
	if got := ModeTeam.String(); got != "team" {
		t.Errorf("ModeTeam.String() = %q, want %q", got, "team")
	}
}

// TestMode_GoString 验证 GoString() 格式
func TestMode_GoString(t *testing.T) {
	if got := ModeAgentWorkPlan.GoString(); got != `schema.Mode("agent.work.plan")` {
		t.Errorf("ModeAgentWorkPlan.GoString() = %q, want %q", got, `schema.Mode("agent.work.plan")`)
	}
	if got := ModeTeam.GoString(); got != `schema.Mode("team")` {
		t.Errorf("ModeTeam.GoString() = %q, want %q", got, `schema.Mode("team")`)
	}
}

// TestMode_ToRuntimeMode 验证 ToRuntimeMode() 返回字符串值
func TestMode_ToRuntimeMode(t *testing.T) {
	tests := []struct {
		mode Mode
		want string
	}{
		{ModeAgentWorkPlan, "agent.work.plan"},
		{ModeAgentWorkNormal, "agent.work.normal"},
		{ModeCodePlan, "agent.code.plan"},
		{ModeCodeNormal, "agent.code.normal"},
		{ModeCodeTeam, "team.code.normal"},
		{ModeTeam, "team"},
	}
	for _, tt := range tests {
		if got := tt.mode.ToRuntimeMode(); got != tt.want {
			t.Errorf("%v.ToRuntimeMode() = %q, want %q", tt.mode, got, tt.want)
		}
	}
}

// TestMode_JSON序列化往返 验证 JSON marshal/unmarshal 往返一致
func TestMode_JSON序列化往返(t *testing.T) {
	modes := []Mode{
		ModeAgentWorkPlan,
		ModeAgentWorkNormal,
		ModeCodePlan,
		ModeCodeNormal,
		ModeCodeTeam,
		ModeTeam,
	}
	for _, m := range modes {
		data, err := json.Marshal(m)
		if err != nil {
			t.Errorf("json.Marshal(%q) 错误: %v", m, err)
			continue
		}
		var got Mode
		if err := json.Unmarshal(data, &got); err != nil {
			t.Errorf("json.Unmarshal(%s) 错误: %v", data, err)
			continue
		}
		if got != m {
			t.Errorf("JSON 往返: got %q, want %q", got, m)
		}
	}
}

// TestMode常量值与Python对齐 验证全部 6 个常量字符串值与 Python Mode 完全对齐
func TestMode常量值与Python对齐(t *testing.T) {
	// Python: jiuwenswarm/common/schema/message.py (Mode)
	tests := []struct {
		got  Mode
		want string
	}{
		{ModeAgentWorkPlan, "agent.work.plan"},
		{ModeAgentWorkNormal, "agent.work.normal"},
		{ModeCodePlan, "agent.code.plan"},
		{ModeCodeNormal, "agent.code.normal"},
		{ModeCodeTeam, "team.code.normal"},
		{ModeTeam, "team"},
	}
	for _, tt := range tests {
		if string(tt.got) != tt.want {
			t.Errorf("常量值 = %q, want %q", tt.got, tt.want)
		}
	}
	// 验证测试覆盖全部 6 个常量
	if len(tests) != 6 {
		t.Errorf("Python 对齐测试覆盖 %d 个常量，want 10", len(tests))
	}
}
