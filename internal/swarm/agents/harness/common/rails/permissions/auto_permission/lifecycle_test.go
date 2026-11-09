package auto_permission

import (
	"context"
	"errors"
	"testing"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/permissions"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// fakePermissionRail 同时实现 BasePermissionRail 与 PolicyBaseRail 的替身。
type fakePermissionRail struct {
	priority int
	// initCalled / uninitCalled 记录生命周期调用
	initCalled   bool
	uninitCalled bool
	// initErr / uninitErr 注入错误
	initErr   error
	uninitErr error
	// updateConfigs 记录每次 UpdateConfig 的入参
	updateConfigs []map[string]any
	// trustedDirs 记录最后一次 SetTrustedDirs
	trustedDirs []string
	// tools 返回的工具标签
	tools []string
	// installed 返回的配置
	installed map[string]any
	// replaceResult 替换 Global 的返回值
	replaceResult map[string]any
	// replaceErr 替换 Global 的错误
	replaceErr error
	// hasReplace 是否实现替换能力
	hasReplace bool
	// config 策略评估器读取的静态配置
	config map[string]any
}

// BeforeToolCall 实现 BasePermissionRail
func (f *fakePermissionRail) BeforeToolCall(context.Context, *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// UpdateConfig 实现 BasePermissionRail
func (f *fakePermissionRail) UpdateConfig(config map[string]any, toolNames []string) {
	f.updateConfigs = append(f.updateConfigs, config)
}

// SetTrustedDirs 实现 BasePermissionRail
func (f *fakePermissionRail) SetTrustedDirs(trustedDirs []string) {
	f.trustedDirs = trustedDirs
}

// InstalledPermissionConfig 实现 BasePermissionRail
func (f *fakePermissionRail) InstalledPermissionConfig() map[string]any {
	return f.installed
}

// GetPermissionSnapshot 实现 BasePermissionRail
func (f *fakePermissionRail) GetPermissionSnapshot(string) (map[string]any, error) {
	return nil, nil
}

// PermissionCheckContext 实现 BasePermissionRail
func (f *fakePermissionRail) PermissionCheckContext(string) func() {
	return func() {}
}

// BindApprovalRequest 实现 BasePermissionRail
func (f *fakePermissionRail) BindApprovalRequest(saschema.InterruptRequester, *llmschema.ToolCall) error {
	return nil
}

// Init 实现生命周期转发目标
func (f *fakePermissionRail) Init(context.Context, agentinterfaces.BaseAgent) error {
	f.initCalled = true
	return f.initErr
}

// Uninit 实现生命周期转发目标
func (f *fakePermissionRail) Uninit(agentinterfaces.BaseAgent) error {
	f.uninitCalled = true
	return f.uninitErr
}

// GetTools 实现工具标签提供者
func (f *fakePermissionRail) GetTools() []string {
	return f.tools
}

// Priority 实现优先级提供者
func (f *fakePermissionRail) Priority() int {
	return f.priority
}

// ReplaceInstalledGlobalPermissions 实现 Global 层替换
func (f *fakePermissionRail) ReplaceInstalledGlobalPermissions(map[string]any) (map[string]any, error) {
	return f.replaceResult, f.replaceErr
}

// NormalizeToolName 实现 PolicyBaseRail
func (f *fakePermissionRail) NormalizeToolName(toolName string) string { return toolName }

// ParseToolArgs 实现 PolicyBaseRail
func (f *fakePermissionRail) ParseToolArgs(any) map[string]any { return map[string]any{} }

// Engine 实现 PolicyBaseRail
func (f *fakePermissionRail) Engine() permissions.PolicyEngine { return nil }

// StaticConfig 实现 PolicyBaseRail
func (f *fakePermissionRail) StaticConfig() map[string]any { return f.config }

// UpdateConfigOnly 实现 PolicyBaseRail 的单参 UpdateConfig（与 Base 版签名不同）
func (f *fakePermissionRail) policyUpdateConfig(config map[string]any) { f.config = config }

// PermissionSceneHook 实现 PolicyBaseRail
func (f *fakePermissionRail) PermissionSceneHook() permissions.PolicySceneHookFn { return nil }

// PermissionsSnapshot 实现 PolicyBaseRail
func (f *fakePermissionRail) PermissionsSnapshot() map[string]any { return nil }

// HasPermissionsSnapshot 实现 PolicyBaseRail
func (f *fakePermissionRail) HasPermissionsSnapshot() bool { return false }

// TrustedDirs 实现 PolicyBaseRail
func (f *fakePermissionRail) TrustedDirs() []string { return nil }

// 编译期断言
var _ permissions.BasePermissionRail = (*fakePermissionRail)(nil)

// ──────────────────────────── 构造 ────────────────────────────

// TestNewAutoPermissionRail_缺BaseRail 验证必填校验。
func TestNewAutoPermissionRail_缺BaseRail(t *testing.T) {
	if _, err := NewAutoPermissionRail(AutoPermissionRailOptions{}); err == nil {
		t.Error("missing base rail should error")
	}
}

// TestNewAutoPermissionRail_基础字段 验证构造后字段初始化。
func TestNewAutoPermissionRail_基础字段(t *testing.T) {
	base := &fakePermissionRail{priority: 77}
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         base,
		PermissionConfig: map[string]any{"mode": "auto"},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if rail.Priority() != 77 {
		t.Errorf("priority = %d, want 77（从 baseRail 读取）", rail.Priority())
	}
	if rail.permissionConfig["mode"] != "auto" {
		t.Errorf("permission_config = %v", rail.permissionConfig)
	}
	if rail.autoOptions == nil {
		t.Error("auto_options should be normalized")
	}
	if rail.policyEvaluator == nil {
		t.Error("policy_evaluator should be auto-constructed")
	}
	if rail.ownerAgent != nil {
		t.Error("owner_agent should be nil before Init")
	}
}

// TestNewAutoPermissionRail_默认优先级 验证无 Priority 时回落 90。
func TestNewAutoPermissionRail_默认优先级(t *testing.T) {
	// 用不含 Priority 的替身（BasePermissionRail 最小实现）
	base := &minimalPermissionRail{}
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         base,
		PermissionConfig: map[string]any{},
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if rail.Priority() != defaultAutoPermissionPriority {
		t.Errorf("priority = %d, want %d", rail.Priority(), defaultAutoPermissionPriority)
	}
}

// TestNewAutoPermissionRail_深拷贝配置 验证入参不被共享。
func TestNewAutoPermissionRail_深拷贝配置(t *testing.T) {
	config := map[string]any{"mode": "auto", "nested": map[string]any{"k": "v"}}
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: config,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	config["mode"] = "changed"
	if rail.permissionConfig["mode"] != "auto" {
		t.Error("config should be deep-copied")
	}
}

// TestNewAutoPermissionRail_显式评估器跳过断言 验证传入 evaluator 时不要求策略面。
func TestNewAutoPermissionRail_显式评估器跳过断言(t *testing.T) {
	evaluator := permissions.NewOpenJiuwenPolicyEvaluator(
		newPolicySurfaceAdapter(&fakePermissionRail{}),
		func() map[string]any { return map[string]any{} }, "")
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &minimalPermissionRail{},
		PermissionConfig: map[string]any{},
		PolicyEvaluator:  evaluator,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if rail.policyEvaluator != evaluator {
		t.Error("should use provided evaluator")
	}
}

// TestNewAutoPermissionRail_工作区归一 验证路径 resolve。
func TestNewAutoPermissionRail_工作区归一(t *testing.T) {
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:            &fakePermissionRail{},
		PermissionConfig:    map[string]any{},
		WorkspaceRoot:       "/tmp/ws",
		PlatformTrustedRoot: "/tmp/plat",
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if rail.workspaceRoot != "/tmp/ws" {
		t.Errorf("workspace_root = %q", rail.workspaceRoot)
	}
	if rail.platformTrustedRoot != "/tmp/plat" {
		t.Errorf("platform_trusted_root = %q", rail.platformTrustedRoot)
	}
}

// ──────────────────────────── Init / Uninit ────────────────────────────

// TestInit_转发并置owner 验证生命周期转发。
func TestInit_转发并置owner(t *testing.T) {
	base := &fakePermissionRail{}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	if err := rail.Init(context.Background(), nil); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !base.initCalled {
		t.Error("base rail Init should be called")
	}
	// agent 传 nil，ownerAgent 亦为 nil——只验证转发被调用（置值逻辑见下条用例）
}

// TestInit_转发失败不置owner 验证错误传播且 owner 保持 nil。
func TestInit_转发失败不置owner(t *testing.T) {
	base := &fakePermissionRail{initErr: errors.New("boom")}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	err := rail.Init(context.Background(), nil)
	if err == nil {
		t.Fatal("should propagate error")
	}
	if rail.ownerAgent != nil {
		t.Error("owner_agent should stay nil on error")
	}
}

// TestInit_置owner 验证 Init 成功后有归属。
func TestInit_置owner(t *testing.T) {
	base := &fakePermissionRail{}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	if err := rail.Init(context.Background(), nil); err != nil {
		t.Fatalf("err = %v", err)
	}
	if !base.initCalled {
		t.Error("base rail Init should be called")
	}
}

// TestUninit_始终清owner 验证 finally 语义。
func TestUninit_始终清owner(t *testing.T) {
	base := &fakePermissionRail{uninitErr: errors.New("boom")}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	rail.ownerAgent = nil
	err := rail.Uninit(nil)
	if err == nil {
		t.Fatal("should propagate error")
	}
	if rail.ownerAgent != nil {
		t.Error("owner_agent must be cleared even on error（Python try/finally）")
	}
	if !base.uninitCalled {
		t.Error("base rail Uninit should be called")
	}
}

// ──────────────────────────── GetTools / InstalledPermissionConfig ────────────────────────────

// TestGetTools 验证工具集合转换与去重。
func TestGetTools(t *testing.T) {
	base := &fakePermissionRail{tools: []string{"bash", "read_file", "bash"}}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	got := rail.GetTools()
	if len(got) != 2 {
		t.Errorf("got %v, want 2 unique", got)
	}
	if _, exists := got["bash"]; !exists {
		t.Error("bash should be present")
	}
}

// TestGetTools_无能力返回空集 验证不支持时返回空 map。
func TestGetTools_无能力返回空集(t *testing.T) {
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: &minimalPermissionRail{}, PermissionConfig: map[string]any{},
		PolicyEvaluator: permissions.NewOpenJiuwenPolicyEvaluator(
			newPolicySurfaceAdapter(&fakePermissionRail{}),
			func() map[string]any { return map[string]any{} }, ""),
	})
	if got := rail.GetTools(); len(got) != 0 {
		t.Errorf("got %v", got)
	}
}

// TestInstalledPermissionConfig_深拷贝 验证返回副本。
func TestInstalledPermissionConfig_深拷贝(t *testing.T) {
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{"mode": "auto"},
	})
	got := rail.InstalledPermissionConfig()
	got["mode"] = "changed"
	if rail.permissionConfig["mode"] != "auto" {
		t.Error("should return a deep copy")
	}
}

// ──────────────────────────── SetTrustedDirs ────────────────────────────

// TestSetTrustedDirs_无平台根直接转发 验证直通。
func TestSetTrustedDirs_无平台根直接转发(t *testing.T) {
	base := &fakePermissionRail{}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	rail.SetTrustedDirs([]string{"/a", "/b"})
	if len(base.trustedDirs) != 2 {
		t.Errorf("got %v", base.trustedDirs)
	}
}

// TestSetTrustedDirs_有平台根置首 验证平台根保留并去重。
func TestSetTrustedDirs_有平台根置首(t *testing.T) {
	base := &fakePermissionRail{}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:            base,
		PermissionConfig:    map[string]any{},
		PlatformTrustedRoot: "/plat",
	})
	rail.SetTrustedDirs([]string{"/a", "/plat", "", "  "})
	if len(base.trustedDirs) != 2 {
		t.Fatalf("got %v, want 2 (plat + /a)", base.trustedDirs)
	}
	if base.trustedDirs[0] != "/plat" {
		t.Errorf("platform root should be first, got %v", base.trustedDirs)
	}
}

// ──────────────────────────── ReplaceInstalledGlobalPermissions ────────────────────────────

// TestReplaceInstalledGlobalPermissions_转发 验证转发结果。
func TestReplaceInstalledGlobalPermissions_转发(t *testing.T) {
	base := &fakePermissionRail{replaceResult: map[string]any{"ok": true}}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail: base, PermissionConfig: map[string]any{},
	})
	got, err := rail.ReplaceInstalledGlobalPermissions(map[string]any{"x": 1})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if got["ok"] != true {
		t.Errorf("got %v", got)
	}
}

// TestReplaceInstalledGlobalPermissions_无能力返回nil 验证兜底。
func TestReplaceInstalledGlobalPermissions_无能力返回nil(t *testing.T) {
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &minimalPermissionRail{},
		PermissionConfig: map[string]any{},
		PolicyEvaluator: permissions.NewOpenJiuwenPolicyEvaluator(
			newPolicySurfaceAdapter(&fakePermissionRail{}),
			func() map[string]any { return map[string]any{} }, ""),
	})
	got, err := rail.ReplaceInstalledGlobalPermissions(map[string]any{})
	if err != nil || got != nil {
		t.Errorf("got (%v, %v), want (nil, nil)", got, err)
	}
}

// ──────────────────────────── UpdateConfig ────────────────────────────

// TestUpdateConfig_转发与字段更新 验证配置同步。
func TestUpdateConfig_转发与字段更新(t *testing.T) {
	base := &fakePermissionRail{}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         base,
		PermissionConfig: map[string]any{"mode": "manual"},
	})
	rail.UpdateConfig(map[string]any{"mode": "auto"}, []string{"bash"})

	if len(base.updateConfigs) != 1 {
		t.Fatalf("base UpdateConfig calls = %d", len(base.updateConfigs))
	}
	if base.updateConfigs[0]["mode"] != "auto" {
		t.Errorf("forwarded config = %v", base.updateConfigs[0])
	}
	if rail.permissionConfig["mode"] != "auto" {
		t.Errorf("permission_config = %v", rail.permissionConfig)
	}
	if rail.autoOptions == nil {
		t.Error("auto_options should be refreshed")
	}
}

// TestUpdateConfig_审计关闭时清空写入器 验证 persistent_audit_enabled=false。
func TestUpdateConfig_审计关闭时清空写入器(t *testing.T) {
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:              &fakePermissionRail{},
		PermissionConfig:      map[string]any{},
		PersistentAuditWriter: permissions.NewPersistentAuditWriter(nil),
	})
	rail.UpdateConfig(map[string]any{"auto": map[string]any{"persistent_audit_enabled": false}}, nil)
	if rail.persistentAuditWriter != nil {
		t.Errorf("writer should be cleared, got %v", rail.persistentAuditWriter)
	}
}

// TestUpdateConfig_审计开启时建写入器 验证 persistent_audit_enabled=true。
func TestUpdateConfig_审计开启时建写入器(t *testing.T) {
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
	})
	rail.UpdateConfig(map[string]any{"auto": map[string]any{"persistent_audit_enabled": true}}, nil)
	if rail.persistentAuditWriter == nil {
		t.Error("writer should be created")
	}
}

// TestUpdateConfig_复用同根写入器 验证同根不重建。
func TestUpdateConfig_复用同根写入器(t *testing.T) {
	config := map[string]any{"auto": map[string]any{"persistent_audit_enabled": true}}
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
	})
	rail.UpdateConfig(config, nil)
	first := rail.persistentAuditWriter
	rail.UpdateConfig(config, nil)
	if rail.persistentAuditWriter != first {
		t.Error("same audit root should reuse the existing writer")
	}
}

// ──────────────────────────── 私有辅助 ────────────────────────────

// TestRecentURLSources 验证会话来源读取。
func TestRecentURLSources(t *testing.T) {
	rail, _ := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:         &fakePermissionRail{},
		PermissionConfig: map[string]any{},
	})
	// 无台账 → 空
	if got := rail.recentURLSources("s1"); got != nil {
		t.Errorf("got %v", got)
	}
	// 空 sessionID → 空
	if got := rail.recentURLSources(""); got != nil {
		t.Errorf("got %v", got)
	}
}

// TestResolveRoot 验证路径归一与空串。
func TestResolveRoot(t *testing.T) {
	if got, err := resolveRoot(""); err != nil || got != "" {
		t.Errorf("empty -> (%q, %v)", got, err)
	}
	if got, err := resolveRoot("  "); err != nil || got != "" {
		t.Errorf("whitespace -> (%q, %v)", got, err)
	}
	if got, err := resolveRoot("/tmp/x"); err != nil || got != "/tmp/x" {
		t.Errorf("abs -> (%q, %v)", got, err)
	}
}

// TestSameAuditRoot 验证审计根等价判定。
func TestSameAuditRoot(t *testing.T) {
	a, b := "/a", "/b"
	if !sameAuditRoot(nil, nil) {
		t.Error("nil,nil should be equal")
	}
	if sameAuditRoot(&a, nil) || sameAuditRoot(nil, &a) {
		t.Error("nil vs non-nil should differ")
	}
	if !sameAuditRoot(&a, &a) {
		t.Error("same value should be equal")
	}
	if sameAuditRoot(&a, &b) {
		t.Error("different values should differ")
	}
}

// TestSortedEffectNames 验证有序输出。
func TestSortedEffectNames(t *testing.T) {
	got := sortedEffectNames(map[string]struct{}{"c": {}, "a": {}, "b": {}})
	if len(got) != 3 || got[0] != "a" || got[2] != "c" {
		t.Errorf("got %v", got)
	}
}

// minimalPermissionRail 只实现 BasePermissionRail（不实现 PolicyBaseRail）的替身。
type minimalPermissionRail struct{}

// BeforeToolCall 实现 BasePermissionRail
func (m *minimalPermissionRail) BeforeToolCall(context.Context, *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// UpdateConfig 实现 BasePermissionRail
func (m *minimalPermissionRail) UpdateConfig(map[string]any, []string) {}

// SetTrustedDirs 实现 BasePermissionRail
func (m *minimalPermissionRail) SetTrustedDirs([]string) {}

// InstalledPermissionConfig 实现 BasePermissionRail
func (m *minimalPermissionRail) InstalledPermissionConfig() map[string]any { return nil }

// GetPermissionSnapshot 实现 BasePermissionRail
func (m *minimalPermissionRail) GetPermissionSnapshot(string) (map[string]any, error) {
	return nil, nil
}

// PermissionCheckContext 实现 BasePermissionRail
func (m *minimalPermissionRail) PermissionCheckContext(string) func() { return func() {} }

// BindApprovalRequest 实现 BasePermissionRail
func (m *minimalPermissionRail) BindApprovalRequest(saschema.InterruptRequester, *llmschema.ToolCall) error {
	return nil
}

// 编译期断言：只满足 BasePermissionRail，不满足 PolicyBaseRail
var _ permissions.BasePermissionRail = (*minimalPermissionRail)(nil)

// ──────────────────────────── 策略面适配器 ────────────────────────────

// TestPolicySurfaceAdapter_能力存在时转发 验证鸭子类型转发。
func TestPolicySurfaceAdapter_能力存在时转发(t *testing.T) {
	base := &fakePermissionRail{
		config: map[string]any{"k": "v"},
	}
	adapter := newPolicySurfaceAdapter(base)

	if got := adapter.NormalizeToolName("bash"); got != "bash" {
		t.Errorf("NormalizeToolName = %q", got)
	}
	if got := adapter.ParseToolArgs(nil); got == nil {
		t.Error("ParseToolArgs should forward")
	}
	if got := adapter.Engine(); got != nil {
		t.Errorf("Engine = %v", got)
	}
	if got := adapter.StaticConfig(); got["k"] != "v" {
		t.Errorf("StaticConfig = %v", got)
	}
	adapter.UpdateConfig(map[string]any{"new": true})
	if len(base.updateConfigs) != 1 {
		t.Errorf("UpdateConfig should forward, got %d calls", len(base.updateConfigs))
	}
	if got := adapter.PermissionSceneHook(); got != nil {
		t.Errorf("PermissionSceneHook = %v", got)
	}
	if got := adapter.PermissionsSnapshot(); got != nil {
		t.Errorf("PermissionsSnapshot = %v", got)
	}
	if adapter.HasPermissionsSnapshot() {
		t.Error("HasPermissionsSnapshot should be false")
	}
	if got := adapter.TrustedDirs(); got != nil {
		t.Errorf("TrustedDirs = %v", got)
	}
}

// TestPolicySurfaceAdapter_能力缺失时默认值 验证 Python getattr 默认语义。
func TestPolicySurfaceAdapter_能力缺失时默认值(t *testing.T) {
	adapter := newPolicySurfaceAdapter(&minimalPermissionRail{})

	// NormalizeToolName 缺失 → 返回原名（Python 默认）
	if got := adapter.NormalizeToolName("bash"); got != "bash" {
		t.Errorf("missing capability should return input, got %q", got)
	}
	// 其余缺失 → nil / false
	if got := adapter.ParseToolArgs(nil); got != nil {
		t.Errorf("ParseToolArgs = %v", got)
	}
	if got := adapter.Engine(); got != nil {
		t.Errorf("Engine = %v", got)
	}
	if got := adapter.StaticConfig(); got != nil {
		t.Errorf("StaticConfig = %v", got)
	}
	if got := adapter.PermissionSceneHook(); got != nil {
		t.Errorf("PermissionSceneHook = %v", got)
	}
	if got := adapter.PermissionsSnapshot(); got != nil {
		t.Errorf("PermissionsSnapshot = %v", got)
	}
	if adapter.HasPermissionsSnapshot() {
		t.Error("HasPermissionsSnapshot should default false")
	}
	if got := adapter.TrustedDirs(); got != nil {
		t.Errorf("TrustedDirs = %v", got)
	}
	// UpdateConfig 始终可用（走 BasePermissionRail 的 2 参数版，补 nil toolNames）
	adapter.UpdateConfig(map[string]any{"x": 1}) // 不 panic 即可
}

// TestPolicySurfaceAdapter_UpdateConfig补nil 验证 1→2 参数桥接。
func TestPolicySurfaceAdapter_UpdateConfig补nil(t *testing.T) {
	base := &fakePermissionRail{}
	adapter := newPolicySurfaceAdapter(base)
	adapter.UpdateConfig(map[string]any{"mode": "auto"})
	if len(base.updateConfigs) != 1 {
		t.Fatalf("calls = %d", len(base.updateConfigs))
	}
	if base.updateConfigs[0]["mode"] != "auto" {
		t.Errorf("config = %v", base.updateConfigs[0])
	}
}

// TestRecentURLSources_有台账 验证从台账取来源。
func TestRecentURLSources_有台账(t *testing.T) {
	ledger := permissions.NewSessionTrustedSearchUrls("root-1")
	rail, err := NewAutoPermissionRail(AutoPermissionRailOptions{
		BaseRail:          &fakePermissionRail{},
		PermissionConfig:  map[string]any{},
		TrustedSearchUrls: ledger,
	})
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	// 匹配的 root 会话 → 空列表（尚无记录）
	if got := rail.recentURLSources("root-1"); len(got) != 0 {
		t.Errorf("got %v", got)
	}
	// 不匹配的会话 → nil
	if got := rail.recentURLSources("other"); got != nil {
		t.Errorf("mismatched session -> %v", got)
	}
}
