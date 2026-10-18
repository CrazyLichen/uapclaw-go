package adapter

import (
	"context"
	"testing"

	memrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestHandleExternalMemoryRailByConfig_provider清空后不注册
// 回归偏差 #5：IsExternalMemoryEnabled 补 provider 非空判断后，
// provider 清空（engine 仍 both）应走禁用分支，不误注册
func TestHandleExternalMemoryRailByConfig_provider清空后不注册(t *testing.T) {
	d := NewDeepAdapter()
	d.configCache = map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{"provider": ""},
	}}
	// 外接记忆未注册时调用禁用分支：不应 panic、不应误注册
	d.handleExternalMemoryRailByConfig(context.Background())
	if d.externalMemoryRailRegistered {
		t.Error("provider 为空时不应处于已注册状态")
	}
	if d.externalMemoryRail != nil {
		t.Error("provider 为空时不应构建 rail")
	}
}

// TestHandleExternalMemoryRailByConfig_启用但构建失败不注册
// provider 配置残缺（mem0 无 key）→ buildExternalMemoryRail 返回 nil → 不注册
func TestHandleExternalMemoryRailByConfig_启用但构建失败不注册(t *testing.T) {
	d := NewDeepAdapter()
	t.Setenv("MEM0_API_KEY", "") // 防 CI 环境泄漏
	d.configCache = map[string]any{"memory": map[string]any{
		"engine": "both",
		"external": map[string]any{
			"provider": "mem0",
			"mem0":     map[string]any{}, // 无 api_key
		},
	}}
	d.handleExternalMemoryRailByConfig(context.Background())
	if d.externalMemoryRailRegistered {
		t.Error("mem0 无 api_key 构建失败时不应注册")
	}
	if d.externalMemoryRail != nil {
		t.Error("构建失败时 rail 字段应保持 nil")
	}
}

// TestHandleExternalMemoryRailByConfig_构建成功存字段
// instance 为 nil 时（RegisterRail 跳过）构建成功的 rail 仍应存储到字段
func TestHandleExternalMemoryRailByConfig_构建成功存字段(t *testing.T) {
	d := NewDeepAdapter()
	t.Setenv("MEM0_API_KEY", "test-key") // t.Setenv 测试结束自动恢复原值
	d.configCache = map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{"provider": "mem0"},
	}}
	d.handleExternalMemoryRailByConfig(context.Background())
	if d.externalMemoryRail == nil {
		t.Fatal("构建成功时 rail 应存储到字段")
	}
	if _, ok := d.externalMemoryRail.(*memrail.ExternalMemoryRail); !ok {
		t.Errorf("rail 类型 = %T, want *memrail.ExternalMemoryRail", d.externalMemoryRail)
	}
	// instance 为 nil：registered 不置位
	if d.externalMemoryRailRegistered {
		t.Error("instance 为 nil 时 registered 不应置位")
	}
	// 幂等：已构建（!registered）再次调用不重复构建破坏字段
	d.handleExternalMemoryRailByConfig(context.Background())
	if d.externalMemoryRail == nil {
		t.Error("二次调用后 rail 字段应保持非 nil")
	}
}

// TestHandleExternalMemoryRailByConfig_热重载注销
// 偏差 #5 端到端：已注册状态下 provider 清空（engine 仍 both）→
// 走注销分支：OnSessionEnd(nil) 不 panic、字段清空、registered 复位
func TestHandleExternalMemoryRailByConfig_热重载注销(t *testing.T) {
	d := NewDeepAdapter()
	t.Setenv("MEM0_API_KEY", "test-key")

	// 第一阶段：构建 rail 存字段
	d.configCache = map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{"provider": "mem0"},
	}}
	d.handleExternalMemoryRailByConfig(context.Background())
	if d.externalMemoryRail == nil {
		t.Fatal("前置条件失败：应构建 rail")
	}
	// 模拟 instance 注册成功（instance 为 nil 无法真注册）
	d.externalMemoryRailRegistered = true

	// 第二阶段：热重载清空 provider（engine 仍 both）
	d.configCache = map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{"provider": ""},
	}}
	d.handleExternalMemoryRailByConfig(context.Background())

	// 注销分支断言：字段清空 + registered 复位（OnSessionEnd nil 不 panic）
	if d.externalMemoryRail != nil {
		t.Error("热重载清空 provider 后 rail 字段应清空")
	}
	if d.externalMemoryRailRegistered {
		t.Error("热重载清空 provider 后 registered 应复位 false")
	}
}

// TestHandleExternalMemoryRailByConfig_engine切换builtin注销
// engine 从 both 改为 builtin（provider 保留）→ 同样走注销分支
func TestHandleExternalMemoryRailByConfig_engine切换builtin注销(t *testing.T) {
	d := NewDeepAdapter()
	t.Setenv("MEM0_API_KEY", "test-key")

	d.configCache = map[string]any{"memory": map[string]any{
		"engine":   "both",
		"external": map[string]any{"provider": "mem0"},
	}}
	d.handleExternalMemoryRailByConfig(context.Background())
	d.externalMemoryRailRegistered = true

	// engine 切换为 builtin：IsExternalMemoryAllowed false → 注销
	d.configCache = map[string]any{"memory": map[string]any{
		"engine":   "builtin",
		"external": map[string]any{"provider": "mem0"},
	}}
	d.handleExternalMemoryRailByConfig(context.Background())
	if d.externalMemoryRail != nil || d.externalMemoryRailRegistered {
		t.Error("engine 切换 builtin 后应注销外接记忆 rail")
	}
}
