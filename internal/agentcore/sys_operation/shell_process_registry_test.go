package sys_operation

import (
	"context"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ──────────────────────────── NewShellProcessRegistry ────────────────────────────

// TestNewShellProcessRegistry 测试创建空注册表
func TestNewShellProcessRegistry(t *testing.T) {
	r := NewShellProcessRegistry()
	assert.NotNil(t, r)
	assert.Empty(t, r.processes)
	assert.Empty(t, r.cancelledSessions)
}

// ──────────────────────────── Register / Unregister ────────────────────────────

// TestShellProcessRegistry_Register 测试注册进程
func TestShellProcessRegistry_Register(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("session1", proc)

	r.mu.Lock()
	bucket, ok := r.processes["session1"]
	r.mu.Unlock()
	assert.True(t, ok)
	_, exists := bucket[proc]
	assert.True(t, exists)
}

// TestShellProcessRegistry_Register_空SessionID 测试空 sessionID 被忽略
func TestShellProcessRegistry_Register_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("", proc)
	r.Register("   ", proc)

	r.mu.Lock()
	assert.Empty(t, r.processes)
	r.mu.Unlock()
}

// TestShellProcessRegistry_Register_nil进程 测试 nil 进程被忽略
func TestShellProcessRegistry_Register_nil进程(t *testing.T) {
	r := NewShellProcessRegistry()
	r.Register("session1", nil)

	r.mu.Lock()
	assert.Empty(t, r.processes)
	r.mu.Unlock()
}

// TestShellProcessRegistry_Register_同一会话多进程 测试同一会话注册多个进程
func TestShellProcessRegistry_Register_同一会话多进程(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	defer proc1.Kill()
	proc2 := startSleepProcess(t)
	defer proc2.Kill()

	r.Register("session1", proc1)
	r.Register("session1", proc2)

	r.mu.Lock()
	bucket := r.processes["session1"]
	r.mu.Unlock()
	assert.Len(t, bucket, 2)
}

// TestShellProcessRegistry_Register_幂等 测试重复注册同一进程为幂等操作
func TestShellProcessRegistry_Register_幂等(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("session1", proc)
	r.Register("session1", proc)

	r.mu.Lock()
	bucket := r.processes["session1"]
	r.mu.Unlock()
	assert.Len(t, bucket, 1)
}

// TestShellProcessRegistry_Unregister 测试注销进程
func TestShellProcessRegistry_Unregister(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	r.Register("session1", proc)
	r.Unregister("session1", proc)

	r.mu.Lock()
	_, ok := r.processes["session1"]
	r.mu.Unlock()
	assert.False(t, ok, "桶空后应自动清理 key")
}

// TestShellProcessRegistry_Unregister_部分注销 测试部分注销后桶仍存在
func TestShellProcessRegistry_Unregister_部分注销(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	defer proc1.Kill()
	proc2 := startSleepProcess(t)
	defer proc2.Kill()

	r.Register("session1", proc1)
	r.Register("session1", proc2)
	r.Unregister("session1", proc1)

	r.mu.Lock()
	bucket := r.processes["session1"]
	r.mu.Unlock()
	assert.Len(t, bucket, 1)
	_, exists := bucket[proc2]
	assert.True(t, exists)
}

// TestShellProcessRegistry_Unregister_不存在的会话 测试注销不存在的会话不 panic
func TestShellProcessRegistry_Unregister_不存在的会话(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	// 不应 panic
	r.Unregister("nonexistent", proc)
}

// ──────────────────────────── KillSession ────────────────────────────

// TestShellProcessRegistry_KillSession 测试终止会话所有进程
func TestShellProcessRegistry_KillSession(t *testing.T) {
	r := NewShellProcessRegistry()
	proc := startSleepProcess(t)

	r.Register("session1", proc)
	killed := r.KillSession("session1")
	assert.Equal(t, 1, killed)

	// 验证会话已被标记为已取消
	assert.True(t, r.ConsumeCancelled("session1"))
}

// TestShellProcessRegistry_KillSession_空SessionID 测试空 sessionID 返回 0
func TestShellProcessRegistry_KillSession_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	killed := r.KillSession("")
	assert.Equal(t, 0, killed)
}

// TestShellProcessRegistry_KillSession_不存在的会话 测试终止不存在的会话返回 0
func TestShellProcessRegistry_KillSession_不存在的会话(t *testing.T) {
	r := NewShellProcessRegistry()
	killed := r.KillSession("nonexistent")
	assert.Equal(t, 0, killed)
}

// TestShellProcessRegistry_KillSession_已退出进程 测试终止已退出的进程返回 0
func TestShellProcessRegistry_KillSession_已退出进程(t *testing.T) {
	r := NewShellProcessRegistry()

	// 启动一个会立即退出的进程
	cmd := exec.Command("true")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", "0")
	}
	require.NoError(t, cmd.Start())
	cmd.Wait() // 等待进程退出

	// 进程已退出，KillSession 不会真正终止它
	r.Register("session1", cmd.Process)
	killed := r.KillSession("session1")
	assert.Equal(t, 0, killed)
}

// ──────────────────────────── KillSessionTree ────────────────────────────

// TestShellProcessRegistry_KillSessionTree 测试终止会话树（前缀匹配）
func TestShellProcessRegistry_KillSessionTree(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	proc2 := startSleepProcess(t)
	proc3 := startSleepProcess(t)

	// 注册主会话和两个子会话
	r.Register("parent", proc1)
	r.Register("parent_child1", proc2)
	r.Register("parent_child2", proc3)

	killed := r.KillSessionTree("parent")
	assert.Equal(t, 3, killed)

	// 验证所有会话已被标记为已取消
	assert.True(t, r.ConsumeCancelled("parent"))
	assert.True(t, r.ConsumeCancelled("parent_child1"))
	assert.True(t, r.ConsumeCancelled("parent_child2"))
}

// TestShellProcessRegistry_KillSessionTree_不匹配前缀 测试不匹配的会话不受影响
func TestShellProcessRegistry_KillSessionTree_不匹配前缀(t *testing.T) {
	r := NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	proc2 := startSleepProcess(t)
	defer proc2.Kill()

	r.Register("abc", proc1)
	r.Register("abcd", proc2) // "abcd" 不以 "abc_" 开头，不应被匹配

	killed := r.KillSessionTree("abc")
	assert.Equal(t, 1, killed)

	// "abcd" 不应被取消
	assert.False(t, r.ConsumeCancelled("abcd"))
}

// TestShellProcessRegistry_KillSessionTree_空SessionID 测试空 sessionID 返回 0
func TestShellProcessRegistry_KillSessionTree_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	killed := r.KillSessionTree("")
	assert.Equal(t, 0, killed)
}

// ──────────────────────────── ConsumeCancelled ────────────────────────────

// TestShellProcessRegistry_ConsumeCancelled 测试消费取消标记
func TestShellProcessRegistry_ConsumeCancelled(t *testing.T) {
	r := NewShellProcessRegistry()

	// 未取消的会话
	assert.False(t, r.ConsumeCancelled("session1"))

	// 标记为已取消
	r.KillSession("session1")
	assert.True(t, r.ConsumeCancelled("session1"))

	// 消费后再次检查返回 false
	assert.False(t, r.ConsumeCancelled("session1"))
}

// TestShellProcessRegistry_ConsumeCancelled_空SessionID 测试空 sessionID 返回 false
func TestShellProcessRegistry_ConsumeCancelled_空SessionID(t *testing.T) {
	r := NewShellProcessRegistry()
	assert.False(t, r.ConsumeCancelled(""))
	assert.False(t, r.ConsumeCancelled("   "))
}

// ──────────────────────────── 全局便捷函数 ────────────────────────────

// TestRegisterShellProcess 测试全局便捷函数 RegisterShellProcess
func TestRegisterShellProcess(t *testing.T) {
	// 使用临时 registry 避免影响全局
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc := startSleepProcess(t)
	defer proc.Kill()

	RegisterShellProcess("global_session", proc)

	DefaultRegistry.mu.Lock()
	_, ok := DefaultRegistry.processes["global_session"]
	DefaultRegistry.mu.Unlock()
	assert.True(t, ok)
}

// TestKillShellProcessesForSession 测试全局便捷函数 KillShellProcessesForSession
func TestKillShellProcessesForSession(t *testing.T) {
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc := startSleepProcess(t)

	RegisterShellProcess("global_session", proc)
	killed := KillShellProcessesForSession("global_session")
	assert.Equal(t, 1, killed)
}

// TestKillShellProcessesForSessionTree 测试全局便捷函数 KillShellProcessesForSessionTree
func TestKillShellProcessesForSessionTree(t *testing.T) {
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc1 := startSleepProcess(t)
	proc2 := startSleepProcess(t)

	RegisterShellProcess("parent", proc1)
	RegisterShellProcess("parent_sub", proc2)
	killed := KillShellProcessesForSessionTree("parent")
	assert.Equal(t, 2, killed)
}

// TestConsumeShellSessionCancelled 测试全局便捷函数 ConsumeShellSessionCancelled
func TestConsumeShellSessionCancelled(t *testing.T) {
	orig := DefaultRegistry
	defer func() { DefaultRegistry = orig }()

	DefaultRegistry = NewShellProcessRegistry()
	proc := startSleepProcess(t)

	RegisterShellProcess("session", proc)
	KillShellProcessesForSession("session")
	assert.True(t, ConsumeShellSessionCancelled("session"))
	assert.False(t, ConsumeShellSessionCancelled("session"))
}

// ──────────────────────────── TerminateShellProcess ────────────────────────────

// TestTerminateShellProcess_成功终止 测试两阶段终止正在运行的进程
func TestTerminateShellProcess_成功终止(t *testing.T) {
	proc := startSleepProcess(t)
	result := TerminateShellProcess(proc)
	assert.True(t, result)
}

// TestTerminateShellProcess_nil进程 测试 nil 进程返回 false
func TestTerminateShellProcess_nil进程(t *testing.T) {
	result := TerminateShellProcess(nil)
	assert.False(t, result)
}

// TestTerminateShellProcess_已退出进程 测试已退出进程返回 false
func TestTerminateShellProcess_已退出进程(t *testing.T) {
	cmd := exec.Command("true")
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "exit", "0")
	}
	require.NoError(t, cmd.Start())
	cmd.Wait()

	result := TerminateShellProcess(cmd.Process)
	assert.False(t, result)
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// startSleepProcess 启动一个长时间休眠的子进程（用于测试终止），调用方负责清理。
func startSleepProcess(t *testing.T) *os.Process {
	t.Helper()
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("cmd", "/c", "timeout", "/t", "300", "/nobreak")
	} else {
		cmd = exec.Command("sleep", "300")
	}
	// 设置进程组隔离（POSIX: 新 session；Windows: 新进程组）
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	require.NoError(t, cmd.Start())
	return cmd.Process
}

// ──────────────────────────── Session ID Context 传递 ────────────────────────────

// TestSetShellSessionID 测试设置 session ID 到 context
func TestSetShellSessionID(t *testing.T) {
	ctx := context.Background()
	ctx = SetShellSessionID(ctx, "test-session-123")
	assert.Equal(t, "test-session-123", GetShellSessionID(ctx))
}

// TestGetShellSessionID_未设置 测试未设置时返回空字符串
func TestGetShellSessionID_未设置(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", GetShellSessionID(ctx))
}

// TestResetShellSessionID 测试重置 session ID
func TestResetShellSessionID(t *testing.T) {
	ctx := context.Background()
	ctx = SetShellSessionID(ctx, "test-session-123")
	assert.Equal(t, "test-session-123", GetShellSessionID(ctx))

	ctx = ResetShellSessionID(ctx)
	assert.Equal(t, "", GetShellSessionID(ctx))
}

// TestResolveShellSessionID_从Context 测试从 context 解析 session ID
func TestResolveShellSessionID_从Context(t *testing.T) {
	ctx := context.Background()
	ctx = SetShellSessionID(ctx, "resolved-session")
	assert.Equal(t, "resolved-session", ResolveShellSessionID(ctx))
}

// TestResolveShellSessionID_未设置 测试未设置时返回空字符串
func TestResolveShellSessionID_未设置(t *testing.T) {
	ctx := context.Background()
	assert.Equal(t, "", ResolveShellSessionID(ctx))
}

// TestResolveShellSessionID_空白 测试空白字符串被 trim 后返回空
func TestResolveShellSessionID_空白(t *testing.T) {
	ctx := context.Background()
	ctx = SetShellSessionID(ctx, "   ")
	assert.Equal(t, "", ResolveShellSessionID(ctx))
}
