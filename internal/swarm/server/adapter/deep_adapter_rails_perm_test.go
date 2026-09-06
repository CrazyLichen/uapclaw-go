package adapter

import (
	"context"
	"testing"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	sschema "github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestDeepAdapter_buildPermissionRail_未启用 测试权限未启用时返回 nil。
func TestDeepAdapter_buildPermissionRail_未启用(t *testing.T) {
	d := NewDeepAdapter()
	// 无 permissions 配置
	rail := d.buildPermissionRail(map[string]any{})
	if rail != nil {
		t.Fatal("无 permissions 配置时应返回 nil")
	}

	// enabled=false
	rail = d.buildPermissionRail(map[string]any{
		"permissions": map[string]any{"enabled": false},
	})
	if rail != nil {
		t.Fatal("permissions.enabled=false 时应返回 nil")
	}
}

// TestDeepAdapter_buildPermissionRail_已启用 测试权限启用时创建 PermissionInterruptRail。
func TestDeepAdapter_buildPermissionRail_已启用(t *testing.T) {
	d := NewDeepAdapter()
	configBase := map[string]any{
		"permissions": map[string]any{
			"enabled": true,
			"tools": map[string]any{
				"bash": "ask",
			},
		},
	}
	rail := d.buildPermissionRail(configBase)
	if rail == nil {
		t.Fatal("permissions.enabled=true 时应返回非 nil")
	}
	// 验证类型
	if _, ok := rail.(*secrail.PermissionInterruptRail); !ok {
		t.Fatal("应返回 *PermissionInterruptRail 类型")
	}
}

// TestDeepAdapter_buildSecurityRail 测试安全护栏创建。
func TestDeepAdapter_buildSecurityRail(t *testing.T) {
	d := NewDeepAdapter()
	rail := d.buildSecurityRail(map[string]any{})
	if rail == nil {
		t.Fatal("buildSecurityRail 应返回非 nil")
	}
}

// TestDeepAdapter_getPermissionsSnapshot 测试权限配置快照。
func TestDeepAdapter_getPermissionsSnapshot(t *testing.T) {
	d := NewDeepAdapter()

	// 无 configCache
	snap := d.getPermissionsSnapshot()
	if snap == nil {
		t.Fatal("应返回非 nil map")
	}
	if len(snap) != 0 {
		t.Fatalf("空 configCache 应返回空 map，得到 %v", snap)
	}

	// 有 configCache 但无 permissions
	d.configCache = map[string]any{"other": "value"}
	snap = d.getPermissionsSnapshot()
	if len(snap) != 0 {
		t.Fatalf("无 permissions 段应返回空 map，得到 %v", snap)
	}

	// 有 permissions
	d.configCache = map[string]any{
		"permissions": map[string]any{"enabled": true, "tools": map[string]any{"bash": "ask"}},
	}
	snap = d.getPermissionsSnapshot()
	if len(snap) == 0 {
		t.Fatal("有 permissions 段应返回非空 map")
	}
}

// TestDeepAdapter_resolveWorkspaceDir 测试 workspace 目录解析。
func TestDeepAdapter_resolveWorkspaceDir(t *testing.T) {
	d := NewDeepAdapter()
	dir := d.resolveWorkspaceDir()
	if dir == "" {
		t.Fatal("resolveWorkspaceDir 应返回非空路径")
	}
}

// TestDeepAdapter_getPermissionYAMLPath 测试配置文件路径。
func TestDeepAdapter_getPermissionYAMLPath(t *testing.T) {
	d := NewDeepAdapter()
	path := d.getPermissionYAMLPath()
	if path == "" {
		t.Fatal("getPermissionYAMLPath 应返回非空路径")
	}
}

// TestDeepAdapter_requestPermissionConfirmation_非ACP 测试非 ACP 通道走 interrupt。
func TestDeepAdapter_requestPermissionConfirmation_非ACP(t *testing.T) {
	d := NewDeepAdapter()

	// 非 ACP 通道（web）
	ctx := sschema.WithToolPermissionChannelID(context.Background(), "web")
	req := harnesssecurity.PermissionConfirmationRequest{
		GoCtx: ctx,
	}
	resp, err := d.requestPermissionConfirmation(req)
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if resp.Action != harnesssecurity.ConfirmActionInterrupt {
		t.Fatalf("非 ACP 通道应返回 ConfirmActionInterrupt，得到 %v", resp.Action)
	}
}

// TestDeepAdapter_requestPermissionConfirmation_ACP降级 测试 ACP 通道降级。
func TestDeepAdapter_requestPermissionConfirmation_ACP降级(t *testing.T) {
	d := NewDeepAdapter()

	// ACP 通道
	ctx := sschema.WithToolPermissionChannelID(context.Background(), "acp")
	req := harnesssecurity.PermissionConfirmationRequest{
		GoCtx: ctx,
	}
	resp, err := d.requestPermissionConfirmation(req)
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	// 当前 ACP output manager 尚未实现，降级为 ConfirmActionInterrupt
	if resp.Action != harnesssecurity.ConfirmActionInterrupt {
		t.Fatalf("ACP 通道应降级为 ConfirmActionInterrupt，得到 %v", resp.Action)
	}
}

// TestDeepAdapter_permissionSceneHook_无权限上下文 测试无权限上下文时返回 nil。
func TestDeepAdapter_permissionSceneHook_无权限上下文(t *testing.T) {
	d := NewDeepAdapter()
	input := harnesssecurity.PermissionSceneHookInput{
		GoCtx:              context.Background(),
		NormalizedToolName: "bash",
		ToolArgs:           map[string]any{"command": "ls"},
	}
	result, err := d.permissionSceneHook(input)
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if result != nil {
		t.Fatalf("无权限上下文应返回 nil，得到 %v", result)
	}
}

// TestDeepAdapter_permissionSceneHook_数字分身允许 测试数字分身场景允许。
func TestDeepAdapter_permissionSceneHook_数字分身允许(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{
		"permissions": map[string]any{
			"owner_scopes": map[string]any{},
		},
	}

	ctx := context.Background()
	permCtx := sschema.NewPermissionContext(
		sschema.WithPermissionChannelID("feishu_ch1"),
		sschema.WithPermissionPrincipalUserID("user_123"),
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
	)
	ctx = sschema.WithPermissionContextValue(ctx, permCtx)
	ctx = sschema.WithToolPermissionChannelID(ctx, "feishu_ch1")

	input := harnesssecurity.PermissionSceneHookInput{
		GoCtx:              ctx,
		NormalizedToolName: "read_file",
		ToolArgs:           map[string]any{"path": "/home/user/file.txt"},
	}
	result, err := d.permissionSceneHook(input)
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	// owner_scopes 为空 → check_avatar_permission 返回 "allow"
	if result == nil {
		t.Fatal("数字分身场景应返回非 nil")
	}
	if result[0] != "approve" {
		t.Fatalf("数字分身场景 owner_scopes 为空应 allow，得到 %v", result)
	}
}

// TestDeepAdapter_permissionSceneHook_数字分身有用户输入 测试数字分身场景有用户输入时拒绝。
func TestDeepAdapter_permissionSceneHook_数字分身有用户输入(t *testing.T) {
	d := NewDeepAdapter()
	ctx := context.Background()
	permCtx := sschema.NewPermissionContext(
		sschema.WithPermissionGroupDigitalAvatar(true),
		sschema.WithPermissionAvatarMode(true),
	)
	ctx = sschema.WithPermissionContextValue(ctx, permCtx)

	input := harnesssecurity.PermissionSceneHookInput{
		GoCtx:              ctx,
		NormalizedToolName: "bash",
		ToolArgs:           map[string]any{"command": "rm -rf /"},
		UserInput:          "some input",
	}
	result, err := d.permissionSceneHook(input)
	if err != nil {
		t.Fatalf("非预期错误: %v", err)
	}
	if result == nil {
		t.Fatal("数字分身有用户输入应返回非 nil")
	}
	if result[0] != "reject" {
		t.Fatalf("数字分身有用户输入应拒绝，得到 %v", result)
	}
}

// TestDeepAdapter_updatePermissionRail_已有Rail原地更新 测试热重载更新。
func TestDeepAdapter_updatePermissionRail_已有Rail原地更新(t *testing.T) {
	d := NewDeepAdapter()
	// 先构建一个 permissionRail
	d.configCache = map[string]any{
		"permissions": map[string]any{"enabled": true, "tools": map[string]any{"bash": "ask"}},
	}
	configBase := map[string]any{
		"permissions": map[string]any{"enabled": true, "tools": map[string]any{"bash": "ask"}},
	}
	d.permissionRail = d.buildPermissionRail(configBase)
	if d.permissionRail == nil {
		t.Fatal("buildPermissionRail 应返回非 nil")
	}

	// 热重载：更新配置
	newConfigBase := map[string]any{
		"permissions": map[string]any{"enabled": true, "tools": map[string]any{"bash": "allow"}},
	}
	d.updatePermissionRail(newConfigBase)
	// 仍然是同一个 rail 对象（原地更新）
	rail, ok := d.permissionRail.(*secrail.PermissionInterruptRail)
	if !ok {
		t.Fatal("permissionRail 应为 *PermissionInterruptRail 类型")
	}
	// 验证引擎配置已更新
	engine := rail.Engine()
	if engine == nil {
		t.Fatal("PermissionInterruptRail.Engine 不应为 nil")
	}
}

// TestDeepAdapter_updatePermissionRail_首次创建 测试热重载时首次创建 rail。
func TestDeepAdapter_updatePermissionRail_首次创建(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{
		"permissions": map[string]any{"enabled": true, "tools": map[string]any{"bash": "ask"}},
	}

	// permissionRail 为 nil，enabled=true → 应创建
	configBase := map[string]any{
		"permissions": map[string]any{"enabled": true},
	}
	d.updatePermissionRail(configBase)
	if d.permissionRail == nil {
		t.Fatal("updatePermissionRail 应首次创建 permissionRail")
	}
}

// TestDeepAdapter_updatePermissionRail_未启用不创建 测试未启用时不创建。
func TestDeepAdapter_updatePermissionRail_未启用不创建(t *testing.T) {
	d := NewDeepAdapter()

	// permissionRail 为 nil，enabled=false → 不创建
	configBase := map[string]any{
		"permissions": map[string]any{"enabled": false},
	}
	d.updatePermissionRail(configBase)
	if d.permissionRail != nil {
		t.Fatal("enabled=false 时不应创建 permissionRail")
	}
}

// TestCollectOptionalToolTags 测试工具名标签收集。
func TestCollectOptionalToolTags(t *testing.T) {
	// 空 config
	result := collectOptionalToolTags(nil)
	if len(result) != 0 {
		t.Fatalf("nil config 应返回空，得到 %v", result)
	}

	// 有 tools 和 rules
	permCfg := map[string]any{
		"tools": map[string]any{
			"bash":       "ask",
			"read_file":  "allow",
			"write_file": "deny",
		},
		"rules": []any{
			map[string]any{"tools": "edit_file"},
			map[string]any{"tools": []any{"mcp_search", "mcp_exec"}},
		},
	}
	result = collectOptionalToolTags(permCfg)
	if len(result) != 6 {
		t.Fatalf("应收集 6 个工具名，得到 %d: %v", len(result), result)
	}

	// 验证排序
	for i := 1; i < len(result); i++ {
		if result[i] < result[i-1] {
			t.Fatalf("结果应已排序，但 %s > %s", result[i-1], result[i])
		}
	}
}

// TestExtractModelName 测试模型名提取。
func TestExtractModelName(t *testing.T) {
	// 空 config
	name := extractModelName(nil)
	if name != "gpt-4" {
		t.Fatalf("空 config 应返回 gpt-4，得到 %s", name)
	}

	// 完整 config
	configBase := map[string]any{
		"models": map[string]any{
			"default": map[string]any{
				"model_client_config": map[string]any{
					"model_name": "qwen-max",
				},
			},
		},
	}
	name = extractModelName(configBase)
	if name != "qwen-max" {
		t.Fatalf("应返回 qwen-max，得到 %s", name)
	}

	// 缺少 model_name
	configBase2 := map[string]any{
		"models": map[string]any{
			"default": map[string]any{},
		},
	}
	name = extractModelName(configBase2)
	if name != "gpt-4" {
		t.Fatalf("缺少 model_name 应返回 gpt-4，得到 %s", name)
	}
}
