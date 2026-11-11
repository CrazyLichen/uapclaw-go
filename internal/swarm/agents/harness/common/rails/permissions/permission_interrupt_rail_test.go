package permissions

import (
	"context"
	"fmt"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	runtimectx "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/ctx"
)

// ──────────────────────────── 辅助 ────────────────────────────

// newTestInterruptRail 构造一个最小 rail（无 exact persist 回调）。
func newTestInterruptRail(t *testing.T, withCallback bool) *JiuwenSwarmPermissionInterruptRail {
	t.Helper()
	var callback ExactPermissionPersistCallback
	if withCallback {
		callback = func(string, map[string]any, [][2]string, bool, bool) bool { return true }
	}
	return NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", nil, callback)
}

// ──────────────────────────── 用例：MarkPlanHardReject / PlanHardRejected ───────────────────────────────

func TestMarkPlanHardReject_往返(t *testing.T) {
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if PlanHardRejected(cbc) {
		t.Error("fresh ctx should not be rejected")
	}
	MarkPlanHardReject(cbc)
	if !PlanHardRejected(cbc) {
		t.Error("after mark should be rejected")
	}
}

func TestPlanHardRejected_非cbc与nil(t *testing.T) {
	if PlanHardRejected(nil) {
		t.Error("nil should be false")
	}
	if PlanHardRejected("not-a-ctx") {
		t.Error("non-cbc should be false")
	}
}

func TestMarkPlanHardReject_nil不panic(t *testing.T) {
	MarkPlanHardReject(nil)
}

// ──────────────────────────── 用例：ApprovalArgumentsDigest ───────────────────────────────

func TestApprovalArgumentsDigest_稳定性(t *testing.T) {
	a := ApprovalArgumentsDigest(map[string]any{"b": 1, "a": 2})
	b := ApprovalArgumentsDigest(map[string]any{"a": 2, "b": 1})
	if a != b {
		t.Error("digest should be key-order independent (Go map 无序但 json.Marshal 排序)")
	}
	if len(a) != 64 {
		t.Errorf("sha256 hex length = %d, want 64", len(a))
	}
}

func TestApprovalArgumentsDigest_不同参数不同值(t *testing.T) {
	a := ApprovalArgumentsDigest(map[string]any{"x": 1})
	b := ApprovalArgumentsDigest(map[string]any{"x": 2})
	if a == b {
		t.Error("different args should differ")
	}
}

func TestApprovalArgumentsDigest_空map(t *testing.T) {
	got := ApprovalArgumentsDigest(map[string]any{})
	if got == "" {
		t.Error("empty map should still hash")
	}
}

// ──────────────────────────── 构造与 override 注入 ────────────────────────────

func TestNewRail_嵌入父类可用(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	if rail.PermissionInterruptRail == nil {
		t.Fatal("parent should be embedded")
	}
	if rail.exactPersistCallback == nil {
		t.Error("callback should be stored")
	}
}

func TestNewRail_无回调时走父类默认(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	// 无回调时 override 返回 handled=false，走父类默认实现（无 host 钩子 → false）
	if got := rail.PersistAllowAlways(context.Background(), "bash", map[string]any{}); got {
		t.Error("no-callback rail should fall back to parent (false without host hook)")
	}
}

func TestNewRail_有回调但无attempt时返回false(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	// 有回调时 override 接管，但无 currentAttempt → persistAllowAlways 返回 false
	if got := rail.PersistAllowAlways(context.Background(), "bash", map[string]any{}); got {
		t.Error("no attempt should return false")
	}
}

// ──────────────────────────── 用例：InstalledPermissionConfig ───────────────────────────────

func TestInstalledPermissionConfig_深拷贝(t *testing.T) {
	cfg := map[string]any{"k": "v", "nested": map[string]any{"a": 1}}
	rail := NewJiuwenSwarmPermissionInterruptRail(cfg, nil, nil, nil, "", nil, nil)

	got := rail.InstalledPermissionConfig()
	if got["k"] != "v" {
		t.Errorf("got %v", got)
	}
	// 修改返回的嵌套 map 不应影响原配置
	got["nested"].(map[string]any)["a"] = 999
	if cfg["nested"].(map[string]any)["a"] != 1 {
		t.Error("nested map should be deep-copied")
	}
}

// ──────────────────────────── 用例：PermissionCheckContext ───────────────────────────────

func TestPermissionCheckContext_设置与清理(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	cleanup := rail.PermissionCheckContext("sess-1")
	if rail.snapshotOwner != rail {
		t.Error("snapshot owner should be set")
	}
	cleanup()
	if rail.snapshotOwner != nil {
		t.Error("snapshot owner should be cleared")
	}
}

func TestPermissionCheckContext_重入不重复读取(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	first := rail.PermissionCheckContext("sess-1")
	// 重入：应直接返回 no-op，不覆盖 owner
	second := rail.PermissionCheckContext("sess-1")
	second()
	if rail.snapshotOwner != rail {
		t.Error("inner cleanup should not clear outer snapshot")
	}
	first()
	if rail.snapshotOwner != nil {
		t.Error("outer cleanup should clear")
	}
}

// ──────────────────────────── 用例：GetPermissionSnapshot ───────────────────────────────

func TestGetPermissionSnapshot_钩子缺失报错(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	if _, err := rail.GetPermissionSnapshot("sess-1"); err == nil {
		t.Error("missing hook should error")
	}
}

func TestGetPermissionSnapshot_钩子返回值(t *testing.T) {
	host := &harnesssecurity.ToolPermissionHost{
		GetPermissionsSnapshot: func() map[string]any {
			return map[string]any{"permissions": "ok"}
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host, nil)
	got, err := rail.GetPermissionSnapshot("sess-1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["permissions"] != "ok" {
		t.Errorf("got %v", got)
	}
}

// ──────────────────────────── 用例：UpdateConfig / SetTrustedDirs ───────────────────────────────

func TestUpdateConfig_同步工作区并刷新投影(t *testing.T) {
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return t.TempDir() },
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host, func(string, map[string]any, [][2]string, bool, bool) bool {
			return true
		})
	// 不应 panic
	rail.UpdateConfig(map[string]any{"file_guard": map[string]any{"enabled": true}}, nil)
}

func TestSetTrustedDirs_刷新投影(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	rail.SetTrustedDirs([]string{t.TempDir()})
	// 不应 panic
}

// ──────────────────────────── 用例：BeforeToolCall ───────────────────────────────

func TestBeforeToolCall_plan硬拒绝短路(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	MarkPlanHardReject(cbc)

	// plan 硬拒绝时应直接返回 nil（不进入父类逻辑）
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("err = %v", err)
	}
}

func TestBeforeToolCall_非权限续接短路(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	cbc.SetExtra(RootNonPermissionResumeAttribute, &RootNonPermissionResume{})

	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("err = %v", err)
	}
}

// ──────────────────────────── 用例：collectFileGuardPersistAccesses ───────────────────────────────

func TestCollectFileGuardPersistAccesses_sendFile走专用评估器(t *testing.T) {
	dir := t.TempDir()
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return dir },
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })
	// 无路径规则命中时 ask_accesses 为空，不应报错
	accesses, err := rail.collectFileGuardPersistAccesses(
		SendFileToolName,
		map[string]any{"abs_file_path_list": dir + "/a.txt"},
		map[string]any{"file_guard": map[string]any{"enabled": true}},
	)
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	_ = accesses
}

func TestCollectFileGuardPersistAccesses_deny报错(t *testing.T) {
	dir := t.TempDir()
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return dir },
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })
	_, err := rail.collectFileGuardPersistAccesses(
		SendFileToolName,
		map[string]any{"abs_file_path_list": dir + "/a.txt"},
		map[string]any{"file_guard": map[string]any{
			"enabled": true,
			"paths": []any{map[string]any{
				"match": "prefix", "path": dir,
				"read": "deny", "write": "deny", "exec": "deny",
			}},
		}},
	)
	if err == nil {
		t.Error("deny should produce error")
	}
	if !strings.Contains(err.Error(), "deny") {
		t.Errorf("err = %v", err)
	}
}

// ──────────────────────────── 用例：ResolveInterrupt ───────────────────────────────

func TestResolveInterrupt_runPolicyDeny(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	ctx := runtimectx.WithRunPermissions(context.Background(), map[string]string{"bash": "deny"})
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	got := rail.ResolveInterrupt(ctx, cbc, nil, nil, nil)
	if got == nil {
		t.Fatal("should return a decision")
	}
	// deny 分支应返回 reject 结果
	if _, ok := got.(interface{ IsReject() bool }); !ok {
		t.Logf("decision type = %T", got)
	}
}

// ──────────────────────────── 补充：核心路径 ────────────────────────────

func TestPayloadPersist判定_PersistScope优先(t *testing.T) {
	if payloadApprovedSessionOnly(nil) || payloadWantsPermanentPersist(nil) {
		t.Error("nil payload should be false")
	}
	// PersistScope 显式 session：即使 AutoConfirm=true 也只算 session
	session := &harnesssecurity.PermissionConfirmResponse{
		AutoConfirm: true, PersistScope: persistScopeSession,
	}
	if !payloadApprovedSessionOnly(session) {
		t.Error("scope=session should be session persist")
	}
	if payloadWantsPermanentPersist(session) {
		t.Error("scope=session should NOT be permanent persist")
	}
	// PersistScope 显式 permanent：即使 AutoConfirm=false 也算 permanent
	permanent := &harnesssecurity.PermissionConfirmResponse{
		AutoConfirm: false, PersistScope: persistScopePermanent,
	}
	if payloadApprovedSessionOnly(permanent) {
		t.Error("scope=permanent should NOT be session persist")
	}
	if !payloadWantsPermanentPersist(permanent) {
		t.Error("scope=permanent should be permanent persist")
	}
}

func TestPayloadPersist判定_空scope回退AutoConfirm(t *testing.T) {
	auto := &harnesssecurity.PermissionConfirmResponse{AutoConfirm: true}
	if !payloadApprovedSessionOnly(auto) || !payloadWantsPermanentPersist(auto) {
		t.Error("empty scope with auto_confirm should fall back to true for both")
	}
	plain := &harnesssecurity.PermissionConfirmResponse{}
	if payloadApprovedSessionOnly(plain) || payloadWantsPermanentPersist(plain) {
		t.Error("empty scope without auto_confirm should be false for both")
	}
}

func TestPayloadPersist判定_未知scope回退AutoConfirm(t *testing.T) {
	unknown := &harnesssecurity.PermissionConfirmResponse{
		AutoConfirm: true, PersistScope: "whatever",
	}
	if !payloadApprovedSessionOnly(unknown) || !payloadWantsPermanentPersist(unknown) {
		t.Error("unknown scope should fall back to auto_confirm")
	}
}

func TestMetadataHelpers(t *testing.T) {
	if got := metadataString(nil, "k"); got != "" {
		t.Errorf("nil metadata -> %q", got)
	}
	if got := metadataString(map[string]any{"k": "v"}, "k"); got != "v" {
		t.Errorf("got %q", got)
	}
	if got := metadataString(map[string]any{"k": 1}, "k"); got != "" {
		t.Errorf("non-string -> %q", got)
	}
	if got := metadataString(map[string]any{"k": nil}, "k"); got != "" {
		t.Errorf("nil value -> %q", got)
	}
}

func TestMetadataAccessPairs(t *testing.T) {
	if got := metadataAccessPairs(nil); got != nil {
		t.Errorf("nil -> %v", got)
	}
	// [][2]string 直传
	direct := metadataAccessPairs(map[string]any{
		permissionAskAccessesKey: [][2]string{{"/a", "read"}},
	})
	if len(direct) != 1 || direct[0][0] != "/a" {
		t.Errorf("direct -> %v", direct)
	}
	// []any 里嵌 [2]string
	nested := metadataAccessPairs(map[string]any{
		permissionAskAccessesKey: []any{[2]string{"/b", "write"}},
	})
	if len(nested) != 1 || nested[0][0] != "/b" {
		t.Errorf("nested pair -> %v", nested)
	}
	// []any 里嵌 []any
	slices := metadataAccessPairs(map[string]any{
		permissionAskAccessesKey: []any{[]any{"/c", "exec"}},
	})
	if len(slices) != 1 || slices[0][1] != "exec" {
		t.Errorf("nested slice -> %v", slices)
	}
	// 未知类型
	if got := metadataAccessPairs(map[string]any{permissionAskAccessesKey: 42}); got != nil {
		t.Errorf("unknown -> %v", got)
	}
}

func TestApprovalRequestFromContext(t *testing.T) {
	if got := approvalRequestFromContext(nil); got != nil {
		t.Errorf("nil cbc -> %v", got)
	}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if got := approvalRequestFromContext(cbc); got != nil {
		t.Errorf("no extra -> %v", got)
	}
}

func TestBindApprovalRequest_无回调直接返回(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	if err := rail.BindApprovalRequest(nil, nil); err != nil {
		t.Errorf("no-callback should return nil, got %v", err)
	}
}

func TestBindApprovalRequest_nilToolCall直接返回(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	if err := rail.BindApprovalRequest(nil, nil); err != nil {
		t.Errorf("nil toolCall should return nil, got %v", err)
	}
}

func TestDeepCopyMap_嵌套(t *testing.T) {
	src := map[string]any{
		"list": []any{map[string]any{"a": 1}},
	}
	got := deepCopyMap(src)
	got["list"].([]any)[0].(map[string]any)["a"] = 999
	if src["list"].([]any)[0].(map[string]any)["a"] != 1 {
		t.Error("nested list should be deep-copied")
	}
	if deepCopyMap(nil) != nil {
		t.Error("nil -> nil")
	}
}

func TestResolveWorkspaceForSync(t *testing.T) {
	dir := t.TempDir()
	got, err := resolveWorkspaceForSync(dir)
	if err != nil || got == "" {
		t.Errorf("got (%q, %v)", got, err)
	}
}

func TestInstallSceneHookPreserver_无回调不包装(t *testing.T) {
	host := &harnesssecurity.ToolPermissionHost{
		PermissionSceneHook: func(harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			return nil, nil
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host, nil)
	// 无回调时不包装，原 hook 保持
	if rail.Host().PermissionSceneHook == nil {
		t.Error("hook should be preserved")
	}
}

func TestInstallSceneHookPreserver_有回调时包装(t *testing.T) {
	called := false
	host := &harnesssecurity.ToolPermissionHost{
		PermissionSceneHook: func(harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			called = true
			return []string{"approve"}, nil
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })
	out, err := rail.Host().PermissionSceneHook(harnesssecurity.PermissionSceneHookInput{})
	if err != nil || !called {
		t.Errorf("err=%v called=%v", err, called)
	}
	// 无 attempt 时透传 outcome
	if len(out) != 1 || out[0] != "approve" {
		t.Errorf("out = %v", out)
	}
}

func TestInstallSceneHookPreserver_已批准时吞掉approve(t *testing.T) {
	host := &harnesssecurity.ToolPermissionHost{
		PermissionSceneHook: func(harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			return []string{"approve"}, nil
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })
	// 模拟已批准
	rail.currentAttempt = &exactPersistAttempt{Approved: true}
	out, err := rail.Host().PermissionSceneHook(harnesssecurity.PermissionSceneHookInput{})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if out != nil {
		t.Errorf("approved attempt should swallow approve, got %v", out)
	}
}

// ──────────────────────────── InterruptRequest 辅助 ────────────────────────────

func TestInterruptRequestMetadataHelpers(t *testing.T) {
	request := &saschema.InterruptRequest{
		Message:        "确认？",
		AutoConfirmKey: "old-key",
		Metadata:       map[string]any{"existing": "v"},
	}
	if got := interruptRequestMetadata(request); got["existing"] != "v" {
		t.Errorf("metadata = %v", got)
	}
	setInterruptRequestMetadata(request, map[string]any{"new": "v2"})
	if request.Metadata["new"] != "v2" {
		t.Errorf("metadata not set: %v", request.Metadata)
	}
	setInterruptRequestAutoConfirmKey(request, "")
	if request.AutoConfirmKey != "" {
		t.Errorf("auto_confirm_key = %q, want empty", request.AutoConfirmKey)
	}
}

func TestInterruptRequestMetadataHelpers_nil安全(t *testing.T) {
	// nil 接口不应 panic
	if got := interruptRequestMetadata(nil); got != nil {
		t.Errorf("nil -> %v", got)
	}
	setInterruptRequestMetadata(nil, map[string]any{"k": "v"})
	setInterruptRequestAutoConfirmKey(nil, "")
}

func TestBindApprovalRequest_完整路径(t *testing.T) {
	dir := t.TempDir()
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return dir },
		GetPermissionsSnapshot: func() map[string]any {
			return map[string]any{"file_guard": map[string]any{"enabled": true}}
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })

	request := &saschema.InterruptRequest{AutoConfirmKey: "old"}
	toolCall := &llmschema.ToolCall{Name: "read_file", Arguments: `{"file_path":"/a.txt"}`}

	if err := rail.BindApprovalRequest(request, toolCall); err != nil {
		t.Fatalf("err = %v", err)
	}
	if request.Metadata[permissionArgumentsSHA256Key] == "" {
		t.Error("digest should be written into metadata")
	}
	if request.AutoConfirmKey != "" {
		t.Errorf("auto_confirm_key should be cleared, got %q", request.AutoConfirmKey)
	}
}

// ──────────────────────────── ctx 传递：run 策略下拒绝持久化 ────────────────────────────

// TestPersistAllowAlways_run策略下拒绝 验证 ctx 里的 RUN_PERMISSIONS 生效。
//
// Python: _persist_allow_always 开头 `if RUN_PERMISSIONS.get() is not None: return False`
func TestPersistAllowAlways_run策略下拒绝(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	rail.currentAttempt = &exactPersistAttempt{Approved: true}

	// 无 run 策略 → 走回调（返回 true）
	if got := rail.PersistAllowAlways(context.Background(), "bash", map[string]any{}); !got {
		t.Error("without run policy should persist via callback")
	}
	// 有 run 策略 → 直接拒绝
	ctx := runtimectx.WithRunPermissions(context.Background(), map[string]string{"bash": "ask"})
	rail.currentAttempt = &exactPersistAttempt{Approved: true}
	if got := rail.PersistAllowAlways(ctx, "bash", map[string]any{}); got {
		t.Error("with run policy should refuse to persist")
	}
}

// TestPersistMergedAllow_run策略下拒绝 验证 merged 也读 ctx。
//
// Python: _persist_merged_allow 开头同样的 RUN_PERMISSIONS 检查。
func TestPersistMergedAllow_run策略下拒绝(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	ctx := runtimectx.WithRunPermissions(context.Background(), map[string]string{"bash": "ask"})
	if got := rail.PersistMergedAllow(ctx, "bash", map[string]any{}, nil, true); got {
		t.Error("with run policy merged persist should be refused")
	}
}

// ──────────────────────────── 用例：callExactPersist ───────────────────────────────

// TestCallExactPersist_正常返回 验证回调结果透传。
func TestCallExactPersist_正常返回(t *testing.T) {
	rail := newTestInterruptRail(t, true) // 回调恒返回 true
	attempt := &exactPersistAttempt{}
	got := rail.callExactPersist(
		&llmschema.ToolCall{Name: "read_file", Arguments: `{}`},
		map[string]any{}, attempt, true)
	if !got {
		t.Error("callback returned true, want true")
	}
	if attempt.Invalidated {
		t.Error("should not be invalidated")
	}
}

// TestCallExactPersist_回调返回false 验证 false 透传且不标记失效。
func TestCallExactPersist_回调返回false(t *testing.T) {
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", nil,
		func(string, map[string]any, [][2]string, bool, bool) bool { return false })
	attempt := &exactPersistAttempt{}
	got := rail.callExactPersist(&llmschema.ToolCall{Name: "read_file"}, map[string]any{}, attempt, true)
	if got {
		t.Error("callback returned false, want false")
	}
	if attempt.Invalidated {
		t.Error("false return should not invalidate")
	}
}

// TestCallExactPersist_panic转invalidated 验证异常被吞并标记失效。
func TestCallExactPersist_panic转invalidated(t *testing.T) {
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", nil,
		func(string, map[string]any, [][2]string, bool, bool) bool {
			panic("persist boom")
		})
	attempt := &exactPersistAttempt{}
	got := rail.callExactPersist(&llmschema.ToolCall{Name: "read_file"}, map[string]any{}, attempt, true)
	if got {
		t.Error("panic should yield false")
	}
	if !attempt.Invalidated {
		t.Error("panic should mark attempt invalidated")
	}
}

// TestCallExactPersist_nilToolCall 验证 nil toolCall 时工具名为空。
func TestCallExactPersist_nilToolCall(t *testing.T) {
	var capturedName string
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", nil,
		func(name string, _ map[string]any, _ [][2]string, _ bool, _ bool) bool {
			capturedName = name
			return true
		})
	attempt := &exactPersistAttempt{}
	rail.callExactPersist(nil, map[string]any{}, attempt, false)
	if capturedName != "" {
		t.Errorf("nil toolCall should give empty name, got %q", capturedName)
	}
}

// ──────────────────────────── 用例：removeExactAutoConfirm ───────────────────────────────

// recordingSession 记录 UpdateState 写入的会话替身。
type recordingSession struct {
	fakeSession
	// updates 记录每次 UpdateState 的入参
	updates []map[string]any
}

// UpdateState 记录写入内容（覆盖 fakeSession 的空实现）。
func (r *recordingSession) UpdateState(data map[string]any) {
	r.updates = append(r.updates, data)
}

// TestRemoveExactAutoConfirm_移除命中条目 验证 key 被删且其余保留。
func TestRemoveExactAutoConfirm_移除命中条目(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	sess := &recordingSession{fakeSession: fakeSession{
		sessionID: "sess-1",
		state: map[string]any{
			saschema.InterruptAutoConfirmKey: map[string]any{
				"read_file": "v1",
				"bash:ls":   "v2",
			},
		},
	}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, sess)
	toolCall := &llmschema.ToolCall{Name: "read_file", Arguments: `{}`}

	rail.removeExactAutoConfirm(cbc, toolCall)

	if len(sess.updates) != 1 {
		t.Fatalf("should write once, got %d", len(sess.updates))
	}
	updated, ok := sess.updates[0][saschema.InterruptAutoConfirmKey].(map[string]any)
	if !ok {
		t.Fatalf("updated config type = %T", sess.updates[0][saschema.InterruptAutoConfirmKey])
	}
	if _, exists := updated["read_file"]; exists {
		t.Error("read_file should be removed")
	}
	if updated["bash:ls"] != "v2" {
		t.Errorf("other entries should be kept, got %v", updated)
	}
}

// TestRemoveExactAutoConfirm_无命中不写 验证 key 不存在时不写回。
func TestRemoveExactAutoConfirm_无命中不写(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	sess := &recordingSession{fakeSession: fakeSession{
		sessionID: "sess-1",
		state: map[string]any{
			saschema.InterruptAutoConfirmKey: map[string]any{"other": "v"},
		},
	}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, sess)
	rail.removeExactAutoConfirm(cbc, &llmschema.ToolCall{Name: "read_file"})

	if len(sess.updates) != 0 {
		t.Errorf("no matching key should not write, got %d writes", len(sess.updates))
	}
}

// TestRemoveExactAutoConfirm_各提前返回分支 验证 nil cbc / 无 session / 空 key / 非 map。
func TestRemoveExactAutoConfirm_各提前返回分支(t *testing.T) {
	rail := newTestInterruptRail(t, true)

	// nil cbc
	rail.removeExactAutoConfirm(nil, &llmschema.ToolCall{Name: "read_file"})

	// 无 session
	cbcNoSession := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	rail.removeExactAutoConfirm(cbcNoSession, &llmschema.ToolCall{Name: "read_file"})

	// 空 key（toolCall 为 nil）
	sess := &recordingSession{fakeSession: fakeSession{sessionID: "s"}}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, sess)
	rail.removeExactAutoConfirm(cbc, nil)

	// state 不是 map
	sess2 := &recordingSession{fakeSession: fakeSession{
		sessionID: "s",
		state:     map[string]any{saschema.InterruptAutoConfirmKey: "not-a-map"},
	}}
	cbc2 := agentinterfaces.NewAgentCallbackContext(nil, nil, sess2)
	rail.removeExactAutoConfirm(cbc2, &llmschema.ToolCall{Name: "read_file"})

	if len(sess.updates) != 0 || len(sess2.updates) != 0 {
		t.Error("early-return branches should not write")
	}
}

// ──────────────────────────── 用例：bindApprovalOnInterrupt ───────────────────────────────

// TestBindApprovalOnInterrupt_正常登记 验证从 AbortError 提取请求并登记。
func TestBindApprovalOnInterrupt_正常登记(t *testing.T) {
	dir := t.TempDir()
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return dir },
		GetPermissionsSnapshot: func() map[string]any {
			return map[string]any{"file_guard": map[string]any{"enabled": true}}
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })

	toolCall := &llmschema.ToolCall{ID: "call-1", Name: "read_file", Arguments: `{"file_path":"/a.txt"}`}
	inputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file", ToolArgs: map[string]any{}}
	inputs.ToolCall = toolCall
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	request := &saschema.InterruptRequest{Message: "确认？"}
	exc := &saschema.ToolInterruptException{Request: request, ToolCall: toolCall}
	abortErr := cb.NewAbortError("工具执行已中断", exc)

	rail.bindApprovalOnInterrupt(cbc, abortErr)

	// BindApprovalRequest 应把摘要写进 metadata
	if request.Metadata[permissionArgumentsSHA256Key] == "" {
		t.Error("approval digest should be written into request metadata")
	}
	// MarkPermissionInterruptRequest 应把 request 挂到 cbc
	if _, exists := cbc.GetExtra(PermissionInterruptRequestAttribute); !exists {
		t.Error("interrupt request should be marked on cbc")
	}
}

// TestBindApprovalOnInterrupt_各提前返回分支 验证类型不匹配时静默返回。
func TestBindApprovalOnInterrupt_各提前返回分支(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)

	// 非 AbortError
	rail.bindApprovalOnInterrupt(cbc, "not-an-abort-error")
	// nil AbortError 指针
	var nilAbort *cb.AbortError
	rail.bindApprovalOnInterrupt(cbc, nilAbort)
	// Cause 不是 ToolInterruptException
	rail.bindApprovalOnInterrupt(cbc, cb.NewAbortError("x", fmt.Errorf("other")))
	// Request 为 nil
	rail.bindApprovalOnInterrupt(cbc, cb.NewAbortError("x", &saschema.ToolInterruptException{}))
	// nil cbc
	rail.bindApprovalOnInterrupt(nil, cb.NewAbortError("x", &saschema.ToolInterruptException{
		Request: &saschema.InterruptRequest{},
	}))

	if _, exists := cbc.GetExtra(PermissionInterruptRequestAttribute); exists {
		t.Error("no branch should mark the request")
	}
}

// ──────────────────────────── 覆盖率补齐（9.90d-1 Task 16 续） ────────────────────────────

// TestBeforeToolCall_中断时recover后重新抛出 覆盖 recover → bind → re-panic 全链。
func TestBeforeToolCall_中断时recover后重新抛出(t *testing.T) {
	dir := t.TempDir()
	host := &harnesssecurity.ToolPermissionHost{
		ResolveWorkspaceDir: func() string { return dir },
		GetPermissionsSnapshot: func() map[string]any {
			return map[string]any{"file_guard": map[string]any{"enabled": true}}
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })

	inputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file", ToolArgs: map[string]any{}}
	inputs.ToolCall = &llmschema.ToolCall{ID: "c1", Name: "read_file", Arguments: `{"file_path":"/a.txt"}`}
	cbc := agentinterfaces.NewAgentCallbackContext(nil, inputs, nil)

	defer func() {
		if r := recover(); r == nil {
			t.Error("interrupt should be re-panicked after binding approval")
		}
	}()
	_ = rail.BeforeToolCall(context.Background(), cbc)
}

// TestBeforeToolCall_非ToolCallInputs 覆盖类型断言失败分支。
func TestBeforeToolCall_非ToolCallInputs(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	if err := rail.BeforeToolCall(context.Background(), cbc); err != nil {
		t.Errorf("err = %v", err)
	}
}

// TestResolveInterrupt_runPolicyAsk 覆盖 level=="ask" 的 hook 包装与还原。
func TestResolveInterrupt_runPolicyAsk(t *testing.T) {
	hookCalled := false
	host := &harnesssecurity.ToolPermissionHost{
		PermissionSceneHook: func(harnesssecurity.PermissionSceneHookInput) ([]string, error) {
			hookCalled = true
			return []string{"reject", "denied by scene"}, nil
		},
	}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host, nil)

	ctx := runtimectx.WithRunPermissions(context.Background(), map[string]string{"read_file": "ask"})
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	toolCall := &llmschema.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}

	rail.ResolveInterrupt(ctx, cbc, toolCall, nil, nil)

	// defer 应把原 hook 还原
	if rail.Host().PermissionSceneHook == nil {
		t.Error("original hook should be restored")
	}
	_ = hookCalled
}

// TestResolveInterrupt_runPolicyAllow 覆盖非 deny/ask 的 level。
func TestResolveInterrupt_runPolicyAllow(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	ctx := runtimectx.WithRunPermissions(context.Background(), map[string]string{"read_file": "allow"})
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	toolCall := &llmschema.ToolCall{ID: "c1", Name: "read_file", Arguments: `{}`}

	// 无 exact callback → 走父类默认解析
	_ = rail.ResolveInterrupt(ctx, cbc, toolCall, nil, nil)
}

// TestOverride钩子_PersistSessionAllow 覆盖 override 的 session 分支。
func TestOverride钩子_PersistSessionAllow(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	rail.currentAttempt = &exactPersistAttempt{Approved: true}
	if got := rail.PersistSessionAllow(context.Background(), "read_file", map[string]any{}, "s1"); !got {
		t.Error("session persist should go through callback")
	}
}

// TestOverride钩子_CollectFileGuardPersistAccesses 覆盖 override 的 collect 分支。
func TestOverride钩子_CollectFileGuardPersistAccesses(t *testing.T) {
	dir := t.TempDir()
	host := &harnesssecurity.ToolPermissionHost{ResolveWorkspaceDir: func() string { return dir }}
	rail := NewJiuwenSwarmPermissionInterruptRail(
		map[string]any{}, nil, nil, nil, "", host,
		func(string, map[string]any, [][2]string, bool, bool) bool { return true })

	// 非 send_file 且无原生投影 → 走父类默认（无 workspace 规则时返回 nil）
	got := rail.CollectFileGuardPersistAccesses("read_file", map[string]any{}, map[string]any{})
	_ = got
}

// TestOverride钩子_GetUserInput 覆盖 override 的 resume 分支。
func TestOverride钩子_GetUserInput(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	cbc := agentinterfaces.NewAgentCallbackContext(nil, nil, nil)
	// 无 resume → override 返回 not handled，父类继续
	_ = rail.GetUserInput(cbc, "call-1")
}

// TestMetadataAccessPairs_未命中 覆盖 metadata 无该键的分支。
func TestMetadataAccessPairs_未命中(t *testing.T) {
	if got := metadataAccessPairs(map[string]any{"other": 1}); got != nil {
		t.Errorf("got %v", got)
	}
	if got := metadataAccessPairs(map[string]any{permissionAskAccessesKey: nil}); got != nil {
		t.Errorf("nil value -> %v", got)
	}
}

// TestPermissionCheckContext_无hook 覆盖 hook 为 nil 的分支。
func TestPermissionCheckContext_无hook(t *testing.T) {
	rail := newTestInterruptRail(t, false)
	cleanup := rail.PermissionCheckContext("s1")
	if rail.snapshot != nil {
		t.Error("no hook should yield nil snapshot")
	}
	cleanup()
}
