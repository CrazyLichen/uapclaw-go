package spawn

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestInProcessSpawnHandle_ForceKill超时 协程不响应取消时按超时返回错误而不是永久阻塞。
//
// M-08：成员 runCtx 是 detached（WithoutCancel），ForceKill 是唯一停机手段；
// 无超时会让 CleanupTeammate → 团队清理预算被彻底击穿。
// 超时通过 forceKillTimeout 注入，避免测试真的等 10s。
func TestInProcessSpawnHandle_ForceKill超时(t *testing.T) {
	done := make(chan struct{}) // 永不关闭：模拟不检查 ctx 的节点/LLM 调用
	cancelCalled := false
	h := NewInProcessSpawnHandle("inproc-stuck", func() { cancelCalled = true }, done, nil)
	h.forceKillTimeout = 30 * time.Millisecond

	start := time.Now()
	err := h.ForceKill()
	elapsed := time.Since(start)

	require.Error(t, err, "协程未退出时 ForceKill 必须返回超时错误")
	assert.True(t, cancelCalled, "取消函数必须被调用")
	assert.Less(t, elapsed, 2*time.Second, "超时等待不得退化为无界阻塞，实际耗时=%v", elapsed)
}

// TestInProcessSpawnHandle_ForceKill正常退出 协程响应关闭时返回 nil。
func TestInProcessSpawnHandle_ForceKill正常退出(t *testing.T) {
	done := make(chan struct{})
	h := NewInProcessSpawnHandle("inproc-ok", func() {}, done, nil)
	h.forceKillTimeout = time.Second
	close(done)

	assert.NoError(t, h.ForceKill())
}

// TestInProcessSpawnHandle_ForceKill默认超时 未注入超时时走 defaultShutdownTimeout 分支。
func TestInProcessSpawnHandle_ForceKill默认超时(t *testing.T) {
	done := make(chan struct{})
	close(done)
	h := NewInProcessSpawnHandle("inproc-default", func() {}, done, nil)

	assert.Zero(t, h.forceKillTimeout, "默认应为零值，由 ForceKill 回退到 defaultShutdownTimeout")
	assert.NoError(t, h.ForceKill())
}
