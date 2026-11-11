package schema

import "testing"

// TestDeprecateMode_空串原样返回 上游铁律 3：空/空白原样返回，不回落 agent。
// Python: gateway/common/mode_matrix.py:344-350
func TestDeprecateMode_空串原样返回(t *testing.T) {
	for _, c := range []string{"", "   ", "\t", "\n"} {
		if got := DeprecateMode(c); got != c {
			t.Errorf("DeprecateMode(%q) = %q, 期望原样返回", c, got)
		}
	}
}

// TestDeprecateMode_十条弃用映射 逐条对齐 DEPRECATION_MAP。
func TestDeprecateMode_十条弃用映射(t *testing.T) {
	cases := map[string]string{
		"agent":            "agent.work.normal",
		"agent.plan":       "agent.work.plan",
		"agent.fast":       "agent.work.normal",
		"code":             "agent.code.normal",
		"code.normal":      "agent.code.normal",
		"code.plan":        "agent.code.plan",
		"code.team":        "team.code.normal",
		"team":             "team.work.normal",
		"team.plan.normal": "team.work.plan",
		"team.plan.code":   "team.code.plan",
	}
	for in, want := range cases {
		if got := DeprecateMode(in); got != want {
			t.Errorf("DeprecateMode(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestDeprecateMode_别名先归一 "team.work" 是别名，经 canonicalize 后映射。
func TestDeprecateMode_别名先归一(t *testing.T) {
	if got := DeprecateMode("team.work"); got != "team.work.normal" {
		t.Errorf("DeprecateMode(team.work) = %q, 期望 team.work.normal", got)
	}
}

// TestIsSingleAgentMode_决策表 对齐上游 SINGLE_AGENT_CANONICAL_MODES 四成员。
// Python: mode_matrix.py:125-132 + :271-273
func TestIsSingleAgentMode_决策表(t *testing.T) {
	cases := map[string]bool{
		"agent.work.normal": true,
		"agent.work.plan":   true,
		"agent.code.normal": true,
		"agent.code.plan":   true,
		"code.normal":       false,
		"code.plan":         false,
		"code.team":         false,
		"team":              false,
		"team.code.normal":  false,
		"auto_harness":      false,
		"":                  false,
		"agent":             false,
	}
	for in, want := range cases {
		if got := IsSingleAgentMode(in); got != want {
			t.Errorf("IsSingleAgentMode(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

// TestDeprecateMode_组合IsSingleAgentMode 复刻上游 service.py:3822 写法。
func TestDeprecateMode_组合IsSingleAgentMode(t *testing.T) {
	if !IsSingleAgentMode(DeprecateMode("code.normal")) {
		t.Error("IsSingleAgentMode(DeprecateMode(code.normal)) 应为 true")
	}
	if !IsSingleAgentMode(DeprecateMode("agent")) {
		t.Error("IsSingleAgentMode(DeprecateMode(agent)) 应为 true")
	}
}

// TestNormalizeWorkMode 非法值返回空串（不是 "work"）。
// Python: mode_matrix.py:237-242
func TestNormalizeWorkMode(t *testing.T) {
	cases := map[string]string{
		"code": "code", "CODE": "code", " code ": "code",
		"work": "work", "Work": "work",
		"": "", "foo": "", "normal": "",
	}
	for in, want := range cases {
		if got := NormalizeWorkMode(in); got != want {
			t.Errorf("NormalizeWorkMode(%q) = %q, 期望 %q", in, got, want)
		}
	}
	if got := NormalizeWorkMode(nil); got != "" {
		t.Errorf("NormalizeWorkMode(nil) = %q, 期望空串", got)
	}
}

// TestBaseModeWithoutPlan 退出 plan 后回到 normal。
func TestBaseModeWithoutPlan(t *testing.T) {
	cases := map[string]string{
		"agent.work.plan":   "agent.work.normal",
		"agent.code.plan":   "agent.code.normal",
		"agent.work.normal": "agent.work.normal",
		"team":              "team",
	}
	for in, want := range cases {
		if got := BaseModeWithoutPlan(in); got != want {
			t.Errorf("BaseModeWithoutPlan(%q) = %q, 期望 %q", in, got, want)
		}
	}
}

// TestIsPlanMode_旧串 上游 PLAN_CANONICAL_MODES 含 agent.plan 与 code.plan。
func TestIsPlanMode_旧串(t *testing.T) {
	cases := map[string]bool{
		"agent.plan":        true,
		"code.plan":         true,
		"team.plan.normal":  true,
		"team.plan.code":    true,
		"agent.work.plan":   true,
		"agent.code.plan":   true,
		"agent.work.normal": false,
		"team":              false,
	}
	for in, want := range cases {
		if got := IsPlanMode(in); got != want {
			t.Errorf("IsPlanMode(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

// TestIsTeamMode_旧串 上游 TEAM_CANONICAL_MODES 含 team 与 code.team。
func TestIsTeamMode_旧串(t *testing.T) {
	cases := map[string]bool{
		"team":              true,
		"code.team":         true,
		"team.work.normal":  true,
		"team.code.normal":  true,
		"agent":             false,
		"agent.work.normal": false,
	}
	for in, want := range cases {
		if got := IsTeamMode(in); got != want {
			t.Errorf("IsTeamMode(%q) = %v, 期望 %v", in, got, want)
		}
	}
}

// TestResolveRequestMode_legacy分支四字段 对齐上游 :470-489。
//
// 用 code.plan 作入参：它不在 WEB_COMPOSABLE_MODES 里，必定走 legacy 分支。
func TestResolveRequestMode_legacy分支四字段(t *testing.T) {
	resolver := func(mode, workMode string) (string, string, string) {
		if workMode == "code" {
			return "code", "plan", "code.plan"
		}
		return "agent", "plan", "agent.plan"
	}
	got := ResolveRequestMode(map[string]any{"mode": "code.plan"}, resolver, "code")
	if got.ManagerMode != "code" || got.SubMode != "plan" || got.CanonicalMode != "code.plan" {
		t.Errorf("manager/sub/canonical = %q/%q/%q", got.ManagerMode, got.SubMode, got.CanonicalMode)
	}
	if got.WorkMode != "code" {
		t.Errorf("WorkMode = %q, 期望 code", got.WorkMode)
	}
	if !got.IsCodeProfile || got.Profile != "code" {
		t.Errorf("IsCodeProfile=%v Profile=%q, 期望 true/code", got.IsCodeProfile, got.Profile)
	}
	if got.FromWebComposition {
		t.Error("legacy 分支 FromWebComposition 应为 false")
	}
	if got.NormalMode != "code.normal" {
		t.Errorf("NormalMode = %q, 期望 code.normal", got.NormalMode)
	}
	if !got.IsPlan {
		t.Error("code.plan 应判为 plan")
	}
}

// TestResolveRequestMode_web组合分支 显式 work_mode + web composable mode 走组合表。
func TestResolveRequestMode_web组合分支(t *testing.T) {
	resolver := func(mode, workMode string) (string, string, string) {
		panic("web 组合分支不应调 legacy resolver")
	}
	got := ResolveRequestMode(map[string]any{"mode": "agent", "work_mode": "code"}, resolver, "")
	if got.CanonicalMode != "code.normal" || got.ManagerMode != "code" {
		t.Errorf("canonical=%q manager=%q", got.CanonicalMode, got.ManagerMode)
	}
	if !got.FromWebComposition {
		t.Error("web 组合分支 FromWebComposition 应为 true")
	}
}

// TestReadRequestWorkMode 缺失/非法返回空串。
func TestReadRequestWorkMode(t *testing.T) {
	if got := ReadRequestWorkMode(nil); got != "" {
		t.Errorf("nil params = %q", got)
	}
	if got := ReadRequestWorkMode(map[string]any{}); got != "" {
		t.Errorf("空 params = %q", got)
	}
	if got := ReadRequestWorkMode(map[string]any{"work_mode": "code"}); got != "code" {
		t.Errorf("work_mode=code 得 %q", got)
	}
	if got := ReadRequestWorkMode(map[string]any{"work_mode": "bogus"}); got != "" {
		t.Errorf("非法值应返回空串，得 %q", got)
	}
}
