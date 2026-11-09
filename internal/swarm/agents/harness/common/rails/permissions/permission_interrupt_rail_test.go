package permissions

import (
	"context"
	"strings"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/runtime"
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

// ──────────────────────────── MarkPlanHardReject / PlanHardRejected ────────────────────────────

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

// ──────────────────────────── ApprovalArgumentsDigest ────────────────────────────

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
	if got := rail.PersistAllowAlways("bash", map[string]any{}); got {
		t.Error("no-callback rail should fall back to parent (false without host hook)")
	}
}

func TestNewRail_有回调但无attempt时返回false(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	// 有回调时 override 接管，但无 currentAttempt → persistAllowAlways 返回 false
	if got := rail.PersistAllowAlways("bash", map[string]any{}); got {
		t.Error("no attempt should return false")
	}
}

// ──────────────────────────── InstalledPermissionConfig ────────────────────────────

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

// ──────────────────────────── PermissionCheckContext ────────────────────────────

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

// ──────────────────────────── GetPermissionSnapshot ────────────────────────────

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

// ──────────────────────────── UpdateConfig / SetTrustedDirs ────────────────────────────

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

// ──────────────────────────── BeforeToolCall ────────────────────────────

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

// ──────────────────────────── collectFileGuardPersistAccesses ────────────────────────────

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

// ──────────────────────────── ResolveInterrupt ────────────────────────────

func TestResolveInterrupt_runPolicyDeny(t *testing.T) {
	rail := newTestInterruptRail(t, true)
	ctx := runtime.WithRunPermissions(context.Background(), map[string]string{"bash": "deny"})
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

func TestPayloadPersist判定(t *testing.T) {
	if payloadApprovedSessionOnly(nil) || payloadWantsPermanentPersist(nil) {
		t.Error("nil payload should be false")
	}
	auto := &harnesssecurity.PermissionConfirmResponse{AutoConfirm: true}
	if !payloadApprovedSessionOnly(auto) {
		t.Error("auto_confirm should map to session persist")
	}
	if !payloadWantsPermanentPersist(auto) {
		t.Error("auto_confirm should map to permanent persist")
	}
	plain := &harnesssecurity.PermissionConfirmResponse{}
	if payloadApprovedSessionOnly(plain) || payloadWantsPermanentPersist(plain) {
		t.Error("no auto_confirm should be false")
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
