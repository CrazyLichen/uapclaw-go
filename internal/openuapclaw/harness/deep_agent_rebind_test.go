package harness

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/cwd"
)

// TestDeepAgent_RebindCwdState 执行路径重绑缝：覆盖 agent 级 cwdState，
// attachRunContexts 在调用方 ctx 未注入时挂载 rebind 后的状态。
//
// Python: init_cwd(execution_cwd, ...) 设 ContextVar 全局生效
// （runtime_prompt_rail.py:251-269；interface_deep _seed_runtime_cwd 同机制）。
func TestDeepAgent_RebindCwdState(t *testing.T) {
	d := newTestDeepAgent()
	// 初始 cwdState 模拟 ensureInitialized 产物（workspace root）
	root := t.TempDir()
	d.RebindCwdState(cwd.InitCwd(root, cwd.WithWorkspace(root)))

	// 初始：attachRunContexts 注入 workspace root
	ctx := d.attachRunContexts(context.Background())
	base := cwd.CwdStateFromCtx(ctx)
	require.NotNil(t, base)
	assert.Equal(t, root, base.GetWorkspace())

	// rebind 为执行路径
	dir := t.TempDir()
	rebound := cwd.InitCwd(dir, cwd.WithProjectRoot(dir), cwd.WithWorkspace(dir))
	d.RebindCwdState(rebound)

	// 新调用（无 CwdState 的 ctx）→ 注入 rebind 后状态
	ctx2 := d.attachRunContexts(context.Background())
	got := cwd.CwdStateFromCtx(ctx2)
	require.NotNil(t, got)
	assert.Equal(t, dir, got.GetWorkspace())
	assert.NotSame(t, base, got)

	// 调用方 ctx 已带 CwdState 时不覆盖（守卫语义不变，子 agent 自己 init_cwd 优先）
	own := cwd.InitCwd(t.TempDir(), cwd.WithWorkspace(t.TempDir()))
	ctx3 := cwd.WithCwdState(context.Background(), own)
	got3 := cwd.CwdStateFromCtx(d.attachRunContexts(ctx3))
	assert.Same(t, own, got3)
}
