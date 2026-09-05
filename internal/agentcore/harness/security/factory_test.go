package security

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestBuildPermissionInterruptRail 测试工厂函数
func TestBuildPermissionInterruptRail(t *testing.T) {
	t.Run("nil配置", func(t *testing.T) {
		engine, host := BuildPermissionInterruptRail(nil, nil, nil, "")
		assert.Nil(t, engine)
		assert.Nil(t, host)
	})

	t.Run("enabled=false", func(t *testing.T) {
		config := map[string]any{"enabled": false}
		engine, host := BuildPermissionInterruptRail(config, nil, nil, "")
		assert.Nil(t, engine)
		assert.Nil(t, host)
	})

	t.Run("enabled=true_无host", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		engine, host := BuildPermissionInterruptRail(config, nil, nil, "")
		assert.Nil(t, engine) // 传入的 engine 为 nil
		assert.NotNil(t, host)
	})

	t.Run("enabled=true_有host", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		existingEngine := NewPermissionEngine(config, nil, "", "")
		existingHost := &ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/existing" },
		}
		engine, host := BuildPermissionInterruptRail(config, existingEngine, existingHost, "")
		assert.Equal(t, existingEngine, engine)
		assert.Equal(t, existingHost, host)
	})

	t.Run("enabled=true_补充workspaceRoot", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		host := &ToolPermissionHost{} // 无 ResolveWorkspaceDir
		_, h := BuildPermissionInterruptRail(config, nil, host, "/workspace/root")
		assert.NotNil(t, h.ResolveWorkspaceDir)
		assert.Equal(t, "/workspace/root", h.ResolveWorkspaceDir())
	})

	t.Run("enabled=true_已有ResolveWorkspaceDir_不覆盖", func(t *testing.T) {
		config := map[string]any{"enabled": true}
		host := &ToolPermissionHost{
			ResolveWorkspaceDir: func() string { return "/original" },
		}
		_, h := BuildPermissionInterruptRail(config, nil, host, "/workspace/root")
		assert.Equal(t, "/original", h.ResolveWorkspaceDir())
	})
}
