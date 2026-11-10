package permissions

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
)

// ──────────────────────────── 替身 ────────────────────────────

// fakePolicyEngine 策略引擎替身。
type fakePolicyEngine struct {
	result      *PolicyPermissionResult
	updateCalls []map[string]any
}

// CheckPermission 返回预置结果
func (f *fakePolicyEngine) CheckPermission(string, map[string]any) *PolicyPermissionResult {
	return f.result
}

// UpdateConfig 记录调用
func (f *fakePolicyEngine) UpdateConfig(cfg map[string]any) {
	f.updateCalls = append(f.updateCalls, cfg)
}

// fakePolicyRail base rail 替身。
type fakePolicyRail struct {
	engine         PolicyEngine
	staticConfig   map[string]any
	snapshot       map[string]any
	updateCalls    []map[string]any
	sceneHook      PolicySceneHookFn
	trustedDirs    []string
	normalizedMap  map[string]string
	parsedArgs     map[string]any
	normalizeCalls int
	hasSnapshot    bool
}

// NormalizeToolName 按预置映射归一
func (f *fakePolicyRail) NormalizeToolName(name string) string {
	f.normalizeCalls++
	if f.normalizedMap != nil {
		return f.normalizedMap[name]
	}
	return name
}

// ParseToolArgs 返回预置参数
func (f *fakePolicyRail) ParseToolArgs(any) map[string]any { return f.parsedArgs }

// Engine 返回引擎
func (f *fakePolicyRail) Engine() PolicyEngine { return f.engine }

// StaticConfig 返回静态配置
func (f *fakePolicyRail) StaticConfig() map[string]any { return f.staticConfig }

// UpdateConfig 记录调用
func (f *fakePolicyRail) UpdatePolicyConfig(cfg map[string]any) {
	f.updateCalls = append(f.updateCalls, cfg)
}

// PermissionSceneHook 返回场景钩子
func (f *fakePolicyRail) PermissionSceneHook() PolicySceneHookFn { return f.sceneHook }

// PermissionsSnapshot 返回配置快照
func (f *fakePolicyRail) PermissionsSnapshot() map[string]any { return f.snapshot }

// HasPermissionsSnapshot 由 hasSnapshot 字段控制
func (f *fakePolicyRail) HasPermissionsSnapshot() bool { return f.hasSnapshot }

// TrustedDirs 返回受信目录
func (f *fakePolicyRail) TrustedDirs() []string { return f.trustedDirs }

// ──────────────────────────── PolicyEvaluation / strictest 策略求值与取严 ────────────────────────────

// TestStrictestEvaluation_取最严 测试取 level 最严的那条
func TestStrictestEvaluation_取最严(t *testing.T) {
	allow := &PolicyEvaluation{Level: "allow", Reason: "r_allow", Source: "permission_engine"}
	ask := &PolicyEvaluation{Level: "ask", Reason: "r_ask", Source: "permission_engine"}
	deny := &PolicyEvaluation{Level: "deny", Reason: "r_deny", Source: "scene_hook"}

	got := strictestEvaluation([]*PolicyEvaluation{allow, ask, deny}, nil)
	if got.Level != "deny" {
		t.Errorf("Level = %q, want deny", got.Level)
	}
	if got.Reason != "r_deny" {
		t.Errorf("Reason = %q, want r_deny（取最严那条的 reason）", got.Reason)
	}
	if got.Source != "scene_hook" {
		t.Errorf("Source = %q, want scene_hook", got.Source)
	}
}

// TestStrictestEvaluation_跳过nil 测试 nil 候选被忽略
func TestStrictestEvaluation_跳过nil(t *testing.T) {
	ask := &PolicyEvaluation{Level: "ask", Reason: "r", Source: "x"}
	got := strictestEvaluation([]*PolicyEvaluation{nil, ask, nil}, nil)
	if got.Level != "ask" {
		t.Errorf("Level = %q, want ask", got.Level)
	}
}

// TestStrictestEvaluation_全nil fail-closed 到 ASK
func TestStrictestEvaluation_全nil(t *testing.T) {
	got := strictestEvaluation([]*PolicyEvaluation{nil, nil}, nil)
	if got.Level != "ask" {
		t.Errorf("Level = %q, want ask（fail-closed）", got.Level)
	}
	if got.Source != "fail_closed" {
		t.Errorf("Source = %q, want fail_closed", got.Source)
	}
}

// TestStrictestEvaluation_合并matchedRule 测试 matched_rule 按 | 拆开去重
func TestStrictestEvaluation_合并matchedRule(t *testing.T) {
	a := &PolicyEvaluation{Level: "ask", MatchedRule: "r1|r2", Source: "x"}
	b := &PolicyEvaluation{Level: "deny", MatchedRule: "r2|r3", Source: "y"}
	got := strictestEvaluation([]*PolicyEvaluation{a, b}, nil)
	if got.MatchedRule != "r1|r2|r3" {
		t.Errorf("MatchedRule = %q, want r1|r2|r3（去重保序）", got.MatchedRule)
	}
}

// TestStrictestEvaluation_合并externalPaths 测试 external_paths 去重保序
func TestStrictestEvaluation_合并externalPaths(t *testing.T) {
	a := &PolicyEvaluation{Level: "ask", ExternalPaths: []string{"/a", "/b"}, Source: "x"}
	b := &PolicyEvaluation{Level: "deny", ExternalPaths: []string{"/b", "/c"}, Source: "y"}
	got := strictestEvaluation([]*PolicyEvaluation{a, b}, nil)
	if len(got.ExternalPaths) != 3 {
		t.Fatalf("ExternalPaths = %v, want 3 项", got.ExternalPaths)
	}
	if got.ExternalPaths[0] != "/a" || got.ExternalPaths[1] != "/b" || got.ExternalPaths[2] != "/c" {
		t.Errorf("ExternalPaths = %v, 顺序错", got.ExternalPaths)
	}
}

// TestStrictestEvaluation_manual任一为真 测试 manual_approval_required 是「任一」
func TestStrictestEvaluation_manual任一为真(t *testing.T) {
	a := &PolicyEvaluation{Level: "ask", Source: "x", ManualApprovalRequired: false}
	b := &PolicyEvaluation{Level: "allow", Source: "y", ManualApprovalRequired: true}
	got := strictestEvaluation([]*PolicyEvaluation{a, b}, nil)
	if !got.ManualApprovalRequired {
		t.Error("ManualApprovalRequired = false, want true（任一为真即真）")
	}
}

// TestStrictestEvaluation_未知level按ask 测试未知级别按 ask 排名
func TestStrictestEvaluation_未知level按ask(t *testing.T) {
	allow := &PolicyEvaluation{Level: "allow", Source: "x"}
	weird := &PolicyEvaluation{Level: "bogus", Reason: "r_weird", Source: "y"}
	got := strictestEvaluation([]*PolicyEvaluation{allow, weird}, nil)
	// bogus 按 ask(1) > allow(0)，应胜出
	if got.Level != "bogus" {
		t.Errorf("Level = %q, want bogus（未知按 ask 排名）", got.Level)
	}
}

// ──────────────────────────── validatedExternalPaths 外部路径校验 ────────────────────────────

// TestValidatedExternalPaths_合法 测试合法输入
func TestValidatedExternalPaths_合法(t *testing.T) {
	if got, ok := validatedExternalPaths(nil); !ok || len(got) != 0 {
		t.Errorf("nil → (%v, %v), want (空, true)", got, ok)
	}
	paths, ok := validatedExternalPaths([]string{"/a", "/b"})
	if !ok || len(paths) != 2 {
		t.Errorf("got (%v, %v), want ([a b], true)", paths, ok)
	}
}

// TestValidatedExternalPaths_非法 测试四类非法输入
func TestValidatedExternalPaths_非法(t *testing.T) {
	// 含空白项
	if _, ok := validatedExternalPaths([]string{"/a", "   "}); ok {
		t.Error("含空白项应返回 ok=false")
	}
	// 超单条长度
	long := strings.Repeat("x", maxExternalPathLength+1)
	if _, ok := validatedExternalPaths([]string{long}); ok {
		t.Error("超单条长度应返回 ok=false")
	}
	// 超总长度
	many := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		many = append(many, strings.Repeat("y", maxExternalPathLength))
	}
	if _, ok := validatedExternalPaths(many); ok {
		t.Error("超总长度应返回 ok=false")
	}
	// 超条数
	tooMany := make([]string, maxExternalPaths+1)
	for i := range tooMany {
		tooMany[i] = "/a"
	}
	if _, ok := validatedExternalPaths(tooMany); ok {
		t.Error("超条数应返回 ok=false")
	}
}

// ──────────────────────────── evaluationFromEngine 从引擎构造求值结果 ────────────────────────────

// TestEvaluationFromEngine_三级别 测试 allow/ask/deny 直接透传
func TestEvaluationFromEngine_三级别(t *testing.T) {
	for _, level := range []string{"allow", "ask", "deny"} {
		got := evaluationFromEngine(&PolicyPermissionResult{Permission: level, Reason: "r"})
		if got.Level != level {
			t.Errorf("Level = %q, want %q", got.Level, level)
		}
		if got.Source != "permission_engine" {
			t.Errorf("Source = %q, want permission_engine", got.Source)
		}
	}
}

// TestEvaluationFromEngine_未知级别failClosed 测试未知级别 fail-closed 到 ASK
func TestEvaluationFromEngine_未知级别failClosed(t *testing.T) {
	got := evaluationFromEngine(&PolicyPermissionResult{Permission: "bogus"})
	if got.Level != "ask" {
		t.Errorf("Level = %q, want ask", got.Level)
	}
	if got.Reason != "unknown_policy_result" {
		t.Errorf("Reason = %q, want unknown_policy_result", got.Reason)
	}
	if got.Source != "fail_closed" {
		t.Errorf("Source = %q, want fail_closed", got.Source)
	}
}

// TestEvaluationFromEngine_非法externalPaths fail-closed（DENY 除外）
func TestEvaluationFromEngine_非法externalPaths(t *testing.T) {
	// 非 DENY 且 external_paths 非法 → ASK
	got := evaluationFromEngine(&PolicyPermissionResult{
		Permission:    "allow",
		ExternalPaths: []string{"  "},
	})
	if got.Level != "ask" {
		t.Errorf("Level = %q, want ask（external_paths 非法）", got.Level)
	}
	if got.Reason != "malformed_policy_external_paths" {
		t.Errorf("Reason = %q", got.Reason)
	}
	// DENY 时保留 DENY（不放宽）
	denyResult := evaluationFromEngine(&PolicyPermissionResult{
		Permission:    "deny",
		ExternalPaths: []string{"  "},
	})
	if denyResult.Level != "deny" {
		t.Errorf("Level = %q, want deny（DENY 不因 external_paths 非法而降级）", denyResult.Level)
	}
}

// TestEvaluationFromEngine_空reason回退level 测试 reason 为空时用 level
func TestEvaluationFromEngine_空reason回退level(t *testing.T) {
	got := evaluationFromEngine(&PolicyPermissionResult{Permission: "deny"})
	if got.Reason != "deny" {
		t.Errorf("Reason = %q, want deny", got.Reason)
	}
}

// TestEvaluationFromEngine_nil 空引擎时失败关闭
func TestEvaluationFromEngine_nil(t *testing.T) {
	got := evaluationFromEngine(nil)
	if got.Level != "ask" || got.Source != "fail_closed" {
		t.Errorf("got (%q, %q), want (ask, fail_closed)", got.Level, got.Source)
	}
}

// ──────────────────────────── evaluationFromPathResult 从路径结果构造求值结果 ────────────────────────────

// TestEvaluationFromPathResult_nil与neutral 测试 nil 与 neutral 返回 nil
func TestEvaluationFromPathResult_nil与neutral(t *testing.T) {
	if got := evaluationFromPathResult(nil); got != nil {
		t.Errorf("nil → %+v, want nil", got)
	}
	if got := evaluationFromPathResult(&SendFilePathGuardResult{Level: "neutral"}); got != nil {
		t.Errorf("neutral → %+v, want nil", got)
	}
}

// TestEvaluationFromPathResult_有意见 测试非 neutral 结果转换
func TestEvaluationFromPathResult_有意见(t *testing.T) {
	got := evaluationFromPathResult(&SendFilePathGuardResult{
		Level:        "deny",
		Reason:       "r",
		MatchedRules: []string{"a", "b"},
	})
	if got == nil {
		t.Fatal("应返回结果")
	}
	if got.Level != "deny" || got.Source != "file_guard_adapter" {
		t.Errorf("got (%q, %q), want (deny, file_guard_adapter)", got.Level, got.Source)
	}
	if got.MatchedRule != "a|b" {
		t.Errorf("MatchedRule = %q, want a|b", got.MatchedRule)
	}
}

// ──────────────────────────── OpenJiuwenPolicyEvaluator 上游策略求值器 ────────────────────────────

// TestEvaluate_引擎不可用failClosed 测试无引擎时 ASK
func TestEvaluate_引擎不可用failClosed(t *testing.T) {
	rail := &fakePolicyRail{staticConfig: map[string]any{}}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "ask" {
		t.Errorf("Level = %q, want ask", got.Level)
	}
	if got.Reason != "policy_engine_unavailable" {
		t.Errorf("Reason = %q, want policy_engine_unavailable", got.Reason)
	}
	if got.Source != "fail_closed" {
		t.Errorf("Source = %q, want fail_closed", got.Source)
	}
}

// TestEvaluate_引擎结果透传 测试引擎判定被透传
func TestEvaluate_引擎结果透传(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{
		Permission:  "deny",
		Reason:      "blocked",
		MatchedRule: "r1",
	}}
	rail := &fakePolicyRail{engine: engine, staticConfig: map[string]any{"a": 1}}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "deny" {
		t.Errorf("Level = %q, want deny", got.Level)
	}
	if got.MatchedRule != "r1" {
		t.Errorf("MatchedRule = %q, want r1", got.MatchedRule)
	}
}

// TestEvaluate_apply两次config 测试配置各在钩子前后各 apply 一次
func TestEvaluate_apply两次config(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{engine: engine, staticConfig: map[string]any{"a": 1}}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	_ = evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)

	// rail.UpdateConfig 被调用两次（钩子前 + 引擎前）
	if len(rail.updateCalls) != 2 {
		t.Errorf("rail.UpdateConfig 调用 %d 次, want 2", len(rail.updateCalls))
	}
}

// TestEvaluate_场景钩子approve 测试 approve 时非 final 也返回 ALLOW
func TestEvaluate_场景钩子approve(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "deny"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return []string{"approve"}, nil
		},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	// approve 不是 final → 继续走引擎，引擎 deny 更严 → 最终 deny
	if got.Level != "deny" {
		t.Errorf("Level = %q, want deny（引擎更严胜出）", got.Level)
	}
}

// TestEvaluate_场景钩子approve且final 测试 final 时 approve 直接返回
func TestEvaluate_场景钩子approve且final(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "deny"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return []string{"approve"}, nil
		},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, true)
	if got.Level != "allow" {
		t.Errorf("Level = %q, want allow（scene_approval_is_final）", got.Level)
	}
	if got.Source != "scene_hook" {
		t.Errorf("Source = %q, want scene_hook", got.Source)
	}
}

// TestEvaluate_场景钩子reject 测试 reject 直接返回 DENY（即使非 final）
func TestEvaluate_场景钩子reject(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return []string{"reject", "自定义原因"}, nil
		},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "deny" {
		t.Errorf("Level = %q, want deny", got.Level)
	}
	if got.Reason != "自定义原因" {
		t.Errorf("Reason = %q, want 自定义原因", got.Reason)
	}
}

// TestEvaluate_场景钩子异常不短路 测试钩子异常时继续走引擎
func TestEvaluate_场景钩子异常不短路(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return nil, errTest
		},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "allow" {
		t.Errorf("Level = %q, want allow（钩子异常不短路）", got.Level)
	}
}

// TestEvaluate_配置刷新失败failClosed 测试有快照钩子但取不到配置时 ASK
//
// 宿主声明了 get_permissions_snapshot（HasPermissionsSnapshot=true）但快照
// 返回 nil，且无 getter / staticConfig —— 说明本应能取到却取不到，
// 必须 fail-closed 到 ASK，不能静默走引擎放行。
func TestEvaluate_配置刷新失败failClosed(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{engine: engine, hasSnapshot: true, snapshot: nil}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "ask" {
		t.Errorf("Level = %q, want ask（配置刷新失败 fail-closed）", got.Level)
	}
	if got.Reason != "policy_config_refresh_failed" {
		t.Errorf("Reason = %q, want policy_config_refresh_failed", got.Reason)
	}
	if got.Source != "fail_closed" {
		t.Errorf("Source = %q, want fail_closed", got.Source)
	}
}

// TestEvaluate_无快照钩子不算刷新失败 测试未声明钩子时不 fail-closed
func TestEvaluate_无快照钩子不算刷新失败(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	// hasSnapshot=false，无 staticConfig → effectiveConfig 为 nil，但不算刷新失败
	rail := &fakePolicyRail{engine: engine}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "allow" {
		t.Errorf("Level = %q, want allow（未声明快照钩子，不 fail-closed）", got.Level)
	}
}

// TestParseToolArgs_三种回退 测试 map / JSON 字符串 / 非法字符串
func TestParseToolArgs_三种回退(t *testing.T) {
	evaluator := NewOpenJiuwenPolicyEvaluator(&fakePolicyRail{}, nil, "")

	// 1. rail 解析优先
	rail := &fakePolicyRail{parsedArgs: map[string]any{"from": "rail"}}
	ev1 := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	if got := ev1.parseToolArgs(nil, map[string]any{"from": "fallback"}); got["from"] != "rail" {
		t.Errorf("应优先 rail 解析，got %v", got)
	}

	// 2. map 直取
	if got := evaluator.parseToolArgs(nil, map[string]any{"k": "v"}); got["k"] != "v" {
		t.Errorf("map 回退失败，got %v", got)
	}

	// 3. JSON 字符串
	if got := evaluator.parseToolArgs(nil, `{"k":"v"}`); got["k"] != "v" {
		t.Errorf("JSON 回退失败，got %v", got)
	}

	// 4. 非法 JSON → 空 map
	if got := evaluator.parseToolArgs(nil, `not json`); len(got) != 0 {
		t.Errorf("非法 JSON 应返回空 map，got %v", got)
	}

	// 5. 其他类型 → 空 map
	if got := evaluator.parseToolArgs(nil, 123); len(got) != 0 {
		t.Errorf("非 map/string 应返回空 map，got %v", got)
	}
}

// TestNormalizeToolName_优先rail 测试 rail 归一化优先，再经 permission_tools
func TestNormalizeToolName_优先rail(t *testing.T) {
	rail := &fakePolicyRail{normalizedMap: map[string]string{"my_tool": "exec_command"}}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := evaluator.normalizeToolName("my_tool")
	if got != "mcp_exec_command" {
		t.Errorf("got %q, want mcp_exec_command（rail 归一后再经别名表）", got)
	}
	if rail.normalizeCalls != 1 {
		t.Errorf("rail.NormalizeToolName 调用 %d 次, want 1", rail.normalizeCalls)
	}
}

// TestNormalizeToolName_rail返回空回退 测试 rail 返回空时直接归一
func TestNormalizeToolName_rail返回空回退(t *testing.T) {
	rail := &fakePolicyRail{normalizedMap: map[string]string{"raw": ""}}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	if got := evaluator.normalizeToolName("raw"); got != "raw" {
		t.Errorf("got %q, want raw", got)
	}
}

// TestEffectivePermissionConfig_三层 测试 getter → snapshot → static 的优先级
func TestEffectivePermissionConfig_三层(t *testing.T) {
	// getter 优先
	rail := &fakePolicyRail{snapshot: map[string]any{"from": "snapshot"}, staticConfig: map[string]any{"from": "static"}}
	ev := NewOpenJiuwenPolicyEvaluator(rail, func() map[string]any { return map[string]any{"from": "getter"} }, "")
	if got, err := ev.effectivePermissionConfig(context.Background()); err != nil || got["from"] != "getter" {
		t.Errorf("应优先 getter，got %v, err %v", got, err)
	}

	// getter 返回 nil → snapshot
	ev2 := NewOpenJiuwenPolicyEvaluator(rail, func() map[string]any { return nil }, "")
	if got, err := ev2.effectivePermissionConfig(context.Background()); err != nil || got["from"] != "snapshot" {
		t.Errorf("应回退 snapshot，got %v, err %v", got, err)
	}

	// snapshot 也 nil → static
	rail3 := &fakePolicyRail{staticConfig: map[string]any{"from": "static"}}
	ev3 := NewOpenJiuwenPolicyEvaluator(rail3, nil, "")
	if got, err := ev3.effectivePermissionConfig(context.Background()); err != nil || got["from"] != "static" {
		t.Errorf("应回退 static，got %v, err %v", got, err)
	}
}

// TestLevelRank 测试级别排名
func TestLevelRank(t *testing.T) {
	if levelRank("allow") != 0 {
		t.Error("allow 应为 0")
	}
	if levelRank("ask") != 1 {
		t.Error("ask 应为 1")
	}
	if levelRank("deny") != 2 {
		t.Error("deny 应为 2")
	}
	if levelRank("bogus") != 1 {
		t.Error("未知级别应按 ask=1")
	}
}

// errTest 测试用错误
var errTest = &testError{}

// testError 测试用错误类型
type testError struct{}

// Error 实现 error
func (e *testError) Error() string { return "test error" }

// TestApplyConfig_回退engine 测试 rail 为 nil 时回退到 engine.UpdateConfig
func TestApplyConfig_回退engine(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	ev := NewOpenJiuwenPolicyEvaluator(nil, nil, "")
	if err := ev.applyConfig(engine, map[string]any{"a": 1}); err != nil {
		t.Fatalf("err = %v", err)
	}
	if len(engine.updateCalls) != 1 {
		t.Errorf("engine.UpdateConfig 调用 %d 次, want 1", len(engine.updateCalls))
	}
}

// TestEngine_nilRail 测试 base rail 为 nil 时 engine() 返回 nil
func TestEngine_nilRail(t *testing.T) {
	ev := NewOpenJiuwenPolicyEvaluator(nil, nil, "")
	if got := ev.engine(); got != nil {
		t.Errorf("engine() = %v, want nil", got)
	}
}

// TestEvaluate_nilRail fail-closed 测试无 rail 时引擎不可用
func TestEvaluate_nilRail(t *testing.T) {
	ev := NewOpenJiuwenPolicyEvaluator(nil, nil, "")
	got := ev.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Level != "ask" || got.Source != "fail_closed" {
		t.Errorf("got (%q, %q), want (ask, fail_closed)", got.Level, got.Source)
	}
}

// TestEvaluateSceneHook_返回空与未知决策 测试钩子返回空或未知决策时不短路
func TestEvaluateSceneHook_返回空与未知决策(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}

	// 返回空切片
	rail := &fakePolicyRail{
		engine: engine, staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return []string{}, nil
		},
	}
	ev := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	if got := ev.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false); got.Level != "allow" {
		t.Errorf("空返回应不短路，got %q", got.Level)
	}

	// 返回未知决策
	rail2 := &fakePolicyRail{
		engine: engine, staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return []string{"maybe"}, nil
		},
	}
	ev2 := NewOpenJiuwenPolicyEvaluator(rail2, nil, "")
	if got := ev2.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false); got.Level != "allow" {
		t.Errorf("未知决策应不短路，got %q", got.Level)
	}
}

// TestEvaluateSceneHook_reject无原因 测试 reject 无第二项时用默认原因
func TestEvaluateSceneHook_reject无原因(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine: engine, staticConfig: map[string]any{},
		sceneHook: func(PolicyInvocation, string, map[string]any, PolicyEngine) ([]string, error) {
			return []string{"reject"}, nil
		},
	}
	ev := NewOpenJiuwenPolicyEvaluator(rail, nil, "")
	got := ev.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.Reason != "scene_hook_rejected" {
		t.Errorf("Reason = %q, want scene_hook_rejected", got.Reason)
	}
}

// ──────────────────────────── 请求级权限叠加（9.90d-1 Task 4b） ────────────────────────────

// TestEvaluate_叠加请求级权限_静态配置路径 验证第三条 return 路径过 OverlayRunPermissions。
func TestEvaluate_叠加请求级权限_静态配置路径(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{"tools": map[string]any{"bash": "ask"}},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	ctx := runtime.WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	got := evaluator.Evaluate(ctx, PolicyInvocation{ToolName: "bash"}, false)

	if got.Level != "allow" {
		t.Errorf("Level = %q, want allow", got.Level)
	}
	if len(rail.updateCalls) == 0 {
		t.Fatal("UpdateConfig should be called with overlaid config")
	}
	applied := rail.updateCalls[0]["tools"].(map[string]any)
	if applied["bash"] != "allow" {
		t.Errorf("overlaid tools.bash = %v, want allow", applied["bash"])
	}
	// 三桶同步
	allowTools, _ := rail.updateCalls[0]["allow_tools"].([]string)
	if len(allowTools) != 1 || allowTools[0] != "bash" {
		t.Errorf("allow_tools = %v", allowTools)
	}
}

// TestEvaluate_叠加请求级权限_运行时getter路径 验证第一条 return 路径。
func TestEvaluate_叠加请求级权限_运行时getter路径(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{engine: engine}
	getter := func() map[string]any {
		return map[string]any{"tools": map[string]any{"bash": "ask"}}
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, getter, "")

	ctx := runtime.WithRunPermissions(context.Background(), map[string]string{"bash": "deny"})
	_ = evaluator.Evaluate(ctx, PolicyInvocation{ToolName: "bash"}, false)

	if len(rail.updateCalls) == 0 {
		t.Fatal("UpdateConfig should be called")
	}
	if rail.updateCalls[0]["tools"].(map[string]any)["bash"] != "deny" {
		t.Errorf("overlaid tools.bash = %v, want deny",
			rail.updateCalls[0]["tools"].(map[string]any)["bash"])
	}
}

// TestEvaluate_叠加请求级权限_快照路径 验证第二条 return 路径。
func TestEvaluate_叠加请求级权限_快照路径(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		hasSnapshot:  true,
		snapshot:     map[string]any{"tools": map[string]any{"bash": "ask"}},
		staticConfig: map[string]any{"tools": map[string]any{"bash": "deny"}},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	ctx := runtime.WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	_ = evaluator.Evaluate(ctx, PolicyInvocation{ToolName: "bash"}, false)

	if len(rail.updateCalls) == 0 {
		t.Fatal("UpdateConfig should be called")
	}
	if rail.updateCalls[0]["tools"].(map[string]any)["bash"] != "allow" {
		t.Errorf("snapshot path should be overlaid, got %v",
			rail.updateCalls[0]["tools"].(map[string]any)["bash"])
	}
}

// TestEvaluate_无请求级绑定不叠加 验证未绑定时配置原样透传
func TestEvaluate_无请求级绑定不叠加(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{"tools": map[string]any{"bash": "ask"}},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	_ = evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)

	if len(rail.updateCalls) == 0 {
		t.Fatal("UpdateConfig should be called")
	}
	if rail.updateCalls[0]["tools"].(map[string]any)["bash"] != "ask" {
		t.Errorf("no overlay should keep ask, got %v",
			rail.updateCalls[0]["tools"].(map[string]any)["bash"])
	}
	// 不叠加时不应凭空产生三桶
	if _, exists := rail.updateCalls[0]["allow_tools"]; exists {
		t.Error("no overlay should not synthesize allow_tools")
	}
}

// TestEvaluate_Ctx字段与GoContext互不干扰 验证两个 ctx 语义分离
func TestEvaluate_Ctx字段与GoContext互不干扰(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{"tools": map[string]any{"bash": "ask"}},
		sceneHook: func(inv PolicyInvocation, _ string, _ map[string]any, _ PolicyEngine) ([]string, error) {
			// invocation.Ctx 装的是宿主回调上下文对象，不是 context.Context
			if _, ok := inv.Ctx.(context.Context); ok {
				return nil, fmt.Errorf("invocation.Ctx must NOT be context.Context")
			}
			return nil, nil
		},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	goCtx := runtime.WithRunPermissions(context.Background(), map[string]string{"bash": "allow"})
	inv := PolicyInvocation{ToolName: "bash", Ctx: "host-callback-context"}
	got := evaluator.Evaluate(goCtx, inv, false)
	if got.Level != "allow" {
		t.Errorf("Level = %q, want allow", got.Level)
	}
}

// TestEvaluate_叠加歧义failClosed 验证 OverlayRunPermissions 返回 error 时
// effectivePermissionConfig 返回 nil，Evaluate 走 fail-closed。
func TestEvaluate_叠加歧义failClosed(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{"tools": map[string]any{"bash": "ask"}},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, "")

	// 通过 CURRENT_RUN_CONTEXT 兜底路径构造歧义（别名 + 本名归一撞车）
	rc := &runtime.RunContext{
		ToolPermissions: []runtime.ToolPermissionLevel{
			{Name: "exec_command", Level: "allow"},
			{Name: "mcp_exec_command", Level: "deny"},
		},
	}
	ctx := runtime.WithRunContext(context.Background(), rc)

	got := evaluator.Evaluate(ctx, PolicyInvocation{ToolName: "bash"}, false)
	if got.Source != "fail_closed" {
		t.Errorf("Source = %q, want fail_closed（叠加失败应降级）", got.Source)
	}
}

// TestEffectivePermissionConfig_三条路径叠加失败 覆盖三个 error 分支
func TestEffectivePermissionConfig_三条路径叠加失败(t *testing.T) {
	rc := &runtime.RunContext{
		ToolPermissions: []runtime.ToolPermissionLevel{
			{Name: "exec_command", Level: "allow"},
			{Name: "mcp_exec_command", Level: "deny"},
		},
	}
	ctx := runtime.WithRunContext(context.Background(), rc)

	cases := []struct {
		name   string
		rail   *fakePolicyRail
		getter func() map[string]any
	}{
		{"getter", &fakePolicyRail{}, func() map[string]any { return map[string]any{"tools": map[string]any{}} }},
		{"snapshot", &fakePolicyRail{snapshot: map[string]any{"tools": map[string]any{}}}, nil},
		{"static", &fakePolicyRail{staticConfig: map[string]any{"tools": map[string]any{}}}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev := NewOpenJiuwenPolicyEvaluator(c.rail, c.getter, "")
			got, err := ev.effectivePermissionConfig(ctx)
			if err == nil {
				t.Errorf("overlay error should be propagated, got %v", got)
			}
		})
	}
}

// ──────────────────────────── send_file 路径守卫接线（9.90d-1 Task 10） ────────────────────────────

// TestEvaluate_sendFile触发路径守卫 验证 send_file_to_user 走守卫并填充 PathResult。
func TestEvaluate_sendFile触发路径守卫(t *testing.T) {
	dir := t.TempDir()
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine: engine,
		staticConfig: map[string]any{
			"file_guard": map[string]any{"enabled": true},
		},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, dir)

	inv := PolicyInvocation{
		ToolName: SendFileToolName,
		ToolArgs: map[string]any{"abs_file_path_list": filepath.Join(dir, "a.txt")},
	}
	got := evaluator.Evaluate(context.Background(), inv, false)

	// 守卫评估为 allow 时 evaluationFromPathResult 返回非 nil（Level=allow）
	if got.PathResult == nil {
		t.Fatalf("PathResult should be populated for send_file, got %+v", got)
	}
	if got.PathResult.Status != SendFileGuardStatusEvaluated {
		t.Errorf("status = %q", got.PathResult.Status)
	}
	if len(got.PathResult.CanonicalPaths) != 1 {
		t.Errorf("canonical_paths = %v", got.PathResult.CanonicalPaths)
	}
}

// TestEvaluate_sendFile守卫deny最严 验证路径守卫 deny 会压过引擎 allow。
func TestEvaluate_sendFile守卫deny最严(t *testing.T) {
	dir := t.TempDir()
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine: engine,
		staticConfig: map[string]any{"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{map[string]any{
				"match": "prefix", "path": dir,
				"read": "deny", "write": "deny", "exec": "deny",
			}},
		}},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, dir)

	inv := PolicyInvocation{
		ToolName: SendFileToolName,
		ToolArgs: map[string]any{"abs_file_path_list": filepath.Join(dir, "a.txt")},
	}
	got := evaluator.Evaluate(context.Background(), inv, false)

	if got.Level != "deny" {
		t.Errorf("level = %q, want deny（路径守卫应压过引擎 allow）", got.Level)
	}
	if got.Source != "file_guard_adapter" {
		t.Errorf("source = %q", got.Source)
	}
}

// TestEvaluate_非sendFile不触发守卫 验证其他工具 PathResult 恒 nil。
func TestEvaluate_非sendFile不触发守卫(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{engine: engine, staticConfig: map[string]any{}}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, t.TempDir())

	got := evaluator.Evaluate(context.Background(), PolicyInvocation{ToolName: "bash"}, false)
	if got.PathResult != nil {
		t.Errorf("non-send_file should have nil PathResult, got %+v", got.PathResult)
	}
}

// TestSetPathGuardEvaluator_注入生效 验证宿主可替换评估器。
func TestSetPathGuardEvaluator_注入生效(t *testing.T) {
	engine := &fakePolicyEngine{result: &PolicyPermissionResult{Permission: "allow"}}
	rail := &fakePolicyRail{
		engine:       engine,
		staticConfig: map[string]any{"file_guard": map[string]any{"enabled": true}},
	}
	evaluator := NewOpenJiuwenPolicyEvaluator(rail, nil, t.TempDir())
	evaluator.SetPathGuardEvaluator(NewSendFilePathGuardEvaluator())

	inv := PolicyInvocation{
		ToolName: SendFileToolName,
		ToolArgs: map[string]any{"abs_file_path_list": "/tmp/x.txt"},
	}
	got := evaluator.Evaluate(context.Background(), inv, false)
	if got.PathResult == nil {
		t.Error("injected evaluator should still produce PathResult")
	}
}

// TestTrustedDirsFromEngine_各分支 验证可选接口断言。
func TestTrustedDirsFromEngine_各分支(t *testing.T) {
	if got := trustedDirsFromEngine(nil); got != nil {
		t.Errorf("nil engine -> %v", got)
	}
	// fakePolicyEngine 未实现 TrustedDirs → nil
	if got := trustedDirsFromEngine(&fakePolicyEngine{}); got != nil {
		t.Errorf("engine without TrustedDirs -> %v", got)
	}
	// 实现 TrustedDirs 的替身
	if got := trustedDirsFromEngine(&fakeTrustedDirsEngine{}); len(got) != 1 {
		t.Errorf("engine with TrustedDirs -> %v", got)
	}
}

// fakeTrustedDirsEngine 实现 PolicyEngine + TrustedDirs。
type fakeTrustedDirsEngine struct {
	fakePolicyEngine
}

// TrustedDirs 返回预置受信目录
func (f *fakeTrustedDirsEngine) TrustedDirs() []string { return []string{"/trusted"} }
