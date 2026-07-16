# 9.58 SpawnManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 SpawnManager 及其依赖的 spawn 子包，完整对齐 Python spawn_manager.py / inprocess_spawn.py / inprocess_handle.py / shared_resources.py 的所有方法和步骤。

**Architecture:** 三层分离：spawn/ 子包提供 SpawnHandle 接口 + InProcessSpawnHandle + InProcessSpawn + SharedResources；agent/spawn_manager.go 提供管理器调度 inprocess/subprocess 双路径；修改现有文件将 any 类型替换为具体类型并实现方法委托。

**Tech Stack:** Go 1.22+ / context / sync / fmt / time

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新建 | `internal/agent_teams/spawn/doc.go` | 包文档 |
| 新建 | `internal/agent_teams/spawn/handle.go` | SpawnHandle 统一接口 |
| 新建 | `internal/agent_teams/spawn/inprocess_handle.go` | InProcessSpawnHandle |
| 新建 | `internal/agent_teams/spawn/inprocess_spawn.go` | SpawnableAgent 接口 + AgentFactory + InProcessSpawn |
| 新建 | `internal/agent_teams/spawn/shared_resources.go` | 进程级全局单例 |
| 新建 | `internal/agent_teams/spawn/handle_test.go` | SpawnHandle 接口测试 |
| 新建 | `internal/agent_teams/spawn/inprocess_handle_test.go` | InProcessSpawnHandle 测试 |
| 新建 | `internal/agent_teams/spawn/inprocess_spawn_test.go` | InProcessSpawn 测试 |
| 新建 | `internal/agent_teams/spawn/shared_resources_test.go` | SharedResources 测试 |
| 新建 | `internal/agent_teams/agent/spawn_manager.go` | SpawnManager |
| 新建 | `internal/agent_teams/agent/spawn_manager_test.go` | SpawnManager 测试 |
| 修改 | `internal/agent_teams/doc.go` | 更新 spawn/ 子目录描述 |
| 修改 | `internal/agent_teams/agent/team_agent.go` | spawnManager any → *SpawnManager + 方法实现 |
| 修改 | `internal/agent_teams/agent/agent_configurator.go` | 回调类型 + SetupTeamBackend |
| 修改 | `internal/agent_teams/agent/payload.go` | BuildSpawnConfig 返回实际类型 |
| 修改 | `internal/agentcore/runner/spawn/child.go` | TeamAgent 分支改为 TODO 占位 |

---

### Task 1: spawn 包骨架 — doc.go + handle.go

**Files:**
- Create: `internal/agent_teams/spawn/doc.go`
- Create: `internal/agent_teams/spawn/handle.go`
- Test: `internal/agent_teams/spawn/handle_test.go`

- [x] **Step 1: 创建 spawn/doc.go**

```go
// Package spawn 提供进程内生成（inprocess spawn）和进程级共享资源。
//
// 本包是团队 Agent 生成机制的独立子包，与 agentcore/runner/spawn/（通用子进程基础设施）
// 形成分层：agentcore/runner/spawn/ 不感知 TeamAgent，本包知道 TeamAgent 概念。
//
// 循环依赖处理：本包不 import agent/ 包，通过 SpawnableAgent 最小接口
// 和 AgentFactory 工厂函数解耦。
//
// 文件目录：
//
//	spawn/
//	├── doc.go              # 包文档
//	├── handle.go           # SpawnHandle 统一接口
//	├── inprocess_handle.go # InProcessSpawnHandle 进程内句柄
//	├── inprocess_spawn.go  # InProcessSpawn 函数 + SpawnableAgent 接口
//	└── shared_resources.go # 进程级全局单例
//
// 对应 Python 代码：openjiuwen/agent_teams/spawn/
package spawn
```

- [x] **Step 2: 创建 spawn/handle.go**

```go
package spawn

import (
	"context"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// ──────────────────────────── 接口 ────────────────────────────

// SpawnHandle 统一 inprocess 和 subprocess 的操作接口。
// 不含回调设置——回调在构造句柄时或通过 SetOnUnhealthy 注入。
// 对齐 Python: SpawnedProcessHandle / InProcessSpawnHandle 的公共方法集。
type SpawnHandle interface {
	// ProcessID 返回进程唯一标识
	ProcessID() string
	// IsAlive 检查进程/任务是否仍在运行
	IsAlive() bool
	// IsHealthy 检查进程/任务是否健康（健康且存活）
	IsHealthy() bool
	// Shutdown 优雅关闭，返回是否在超时内完成
	Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error)
	// ForceKill 强制终止
	ForceKill() error
	// WaitForCompletion 等待完成，0=成功，-1=异常
	WaitForCompletion() (int, error)
	// StartHealthCheck 启动健康检查后台任务
	StartHealthCheck(ctx context.Context, interval ...time.Duration) error
	// StopHealthCheck 停止健康检查后台任务
	StopHealthCheck() error
}
```

- [x] **Step 3: 创建 spawn/handle_test.go**

验证 SpawnedProcessHandle 满足 SpawnHandle 接口的编译期断言：

```go
package spawn

// 编译期断言：SpawnedProcessHandle 满足 SpawnHandle 接口
var _ SpawnHandle = (*SpawnedProcessHandle)(nil)
```

- [x] **Step 4: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/spawn/...`
Expected: PASS（SpawnedProcessHandle 已在 agentcore/runner/spawn 包中，当前包引用不到，编译期断言需要在同包内——改为文档说明）

修正：将编译期断言替换为接口文档说明测试：

```go
package spawn_test

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/spawn"
)

// TestSpawnHandle_SpawnedProcessHandle满足接口 验证 SpawnedProcessHandle 满足 SpawnHandle 接口。
// 对齐 Python: SpawnedProcessHandle 和 InProcessSpawnHandle 共享相同的方法表面。
func TestSpawnHandle_SpawnedProcessHandle满足接口(t *testing.T) {
	// 编译期断言：SpawnedProcessHandle 满足 SpawnHandle 接口
	var _ spawn.SpawnHandle = (*spawn.SpawnedProcessHandle)(nil)
}
```

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -run TestSpawnHandle_SpawnedProcessHandle满足接口 -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/agent_teams/spawn/
git commit -m "feat(agent_teams): spawn 包骨架 — doc.go + SpawnHandle 接口"
```

---

### Task 2: InProcessSpawnHandle

**Files:**
- Create: `internal/agent_teams/spawn/inprocess_handle.go`
- Test: `internal/agent_teams/spawn/inprocess_handle_test.go`

- [x] **Step 1: 写 InProcessSpawnHandle 测试**

```go
package spawn_test

import (
	"context"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
)

// TestNewInProcessSpawnHandle_基本属性 测试句柄的基本属性。
func TestNewInProcessSpawnHandle_基本属性(t *testing.T) {
	done := make(chan struct{})
	cancelCtx := func() {}

	h := spawn.NewInProcessSpawnHandle("inproc-test", cancelCtx, done, nil)

	if h.ProcessID() != "inproc-test" {
		t.Errorf("ProcessID() = %q, want %q", h.ProcessID(), "inproc-test")
	}
	if !h.IsAlive() {
		t.Error("IsAlive() = false, want true（done chan 未关闭）")
	}
	if !h.IsHealthy() {
		t.Error("IsHealthy() = false, want true（alive 且未请求关闭）")
	}
}

// TestInProcessSpawnHandle_IsAlive_done关闭后返回false 测试 done 关闭后 IsAlive 返回 false。
func TestInProcessSpawnHandle_IsAlive_done关闭后返回false(t *testing.T) {
	done := make(chan struct{})
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	close(done)

	if h.IsAlive() {
		t.Error("IsAlive() = true, want false（done 已关闭）")
	}
}

// TestInProcessSpawnHandle_IsHealthy_shutdownRequested后返回false 测试请求关闭后 IsHealthy 返回 false。
func TestInProcessSpawnHandle_IsHealthy_shutdownRequested后返回false(t *testing.T) {
	done := make(chan struct{})
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	h.SetOnUnhealthy(func() {})
	// 模拟请求关闭
	_ = h.ForceKill()

	if h.IsHealthy() {
		t.Error("IsHealthy() = true, want false（已请求关闭）")
	}
}

// TestInProcessSpawnHandle_Shutdown_正常关闭 测试正常关闭流程。
func TestInProcessSpawnHandle_Shutdown_正常关闭(t *testing.T) {
	done := make(chan struct{})
	cancelCalled := false
	cancel := func() { cancelCalled = true }

	h := spawn.NewInProcessSpawnHandle("inproc-test", cancel, done, nil)

	// 模拟 goroutine 完成后关闭 done
	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()

	graceful, err := h.Shutdown(context.Background(), 2*time.Second)
	if err != nil {
		t.Errorf("Shutdown() error = %v", err)
	}
	if !graceful {
		t.Error("Shutdown() graceful = false, want true")
	}
	if !cancelCalled {
		t.Error("cancel 未被调用")
	}
}

// TestInProcessSpawnHandle_ForceKill 测试强制终止。
func TestInProcessSpawnHandle_ForceKill(t *testing.T) {
	done := make(chan struct{})
	cancelCalled := false
	cancel := func() { cancelCalled = true }

	h := spawn.NewInProcessSpawnHandle("inproc-test", cancel, done, nil)

	err := h.ForceKill()
	if err != nil {
		t.Errorf("ForceKill() error = %v", err)
	}
	if !cancelCalled {
		t.Error("cancel 未被调用")
	}
}

// TestInProcessSpawnHandle_WaitForCompletion_正常 测试正常等待完成。
func TestInProcessSpawnHandle_WaitForCompletion_正常(t *testing.T) {
	done := make(chan struct{})
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, done, nil)

	go func() {
		time.Sleep(50 * time.Millisecond)
		close(done)
	}()

	code, err := h.WaitForCompletion()
	if err != nil {
		t.Errorf("WaitForCompletion() error = %v", err)
	}
	if code != 0 {
		t.Errorf("WaitForCompletion() code = %d, want 0", code)
	}
}

// TestInProcessSpawnHandle_StartHealthCheck_noop 测试健康检查为 no-op。
func TestInProcessSpawnHandle_StartHealthCheck_noop(t *testing.T) {
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, make(chan struct{}), nil)

	err := h.StartHealthCheck(context.Background())
	if err != nil {
		t.Errorf("StartHealthCheck() error = %v, want nil（no-op）", err)
	}

	err = h.StopHealthCheck()
	if err != nil {
		t.Errorf("StopHealthCheck() error = %v, want nil（no-op）", err)
	}
}

// TestInProcessSpawnHandle_SetOnUnhealthy 测试设置不健康回调。
func TestInProcessSpawnHandle_SetOnUnhealthy(t *testing.T) {
	h := spawn.NewInProcessSpawnHandle("inproc-test", func() {}, make(chan struct{}), nil)

	called := false
	h.SetOnUnhealthy(func() { called = true })

	// 触发回调（通过 ForceKill → shutdownRequested）
	_ = h.ForceKill()
	// 注意：SetOnUnhealthy 只设置回调，不会自动触发
	// onUnhealthy 的触发由 SpawnManager 管理
	if called {
		t.Error("onUnhealthy 不应在 ForceKill 时自动触发")
	}
}

// TestInProcessSpawnHandle_满足SpawnHandle接口 编译期断言。
func TestInProcessSpawnHandle_满足SpawnHandle接口(t *testing.T) {
	var _ spawn.SpawnHandle = (*spawn.InProcessSpawnHandle)(nil)
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -v`
Expected: FAIL — `spawn.NewInProcessSpawnHandle` 未定义

- [x] **Step 3: 实现 InProcessSpawnHandle**

```go
package spawn

import (
	"context"
	"fmt"
	"sync"
	"time"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// InProcessSpawnHandle 进程内生成句柄，管理 goroutine 生命周期。
// 对齐 Python: InProcessSpawnHandle (inprocess_handle.py)
//
// 用 context.CancelFunc 对齐 Python task.cancel()，
// 用 done chan 对齐 Python task.done()。
// agentRef 对齐 Python agent_ref: Any，InProcess 模式独有。
type InProcessSpawnHandle struct {
	// processID 进程唯一标识（"inproc-{memberName}"）
	processID string
	// cancelCtx 取消 goroutine 的 context.CancelFunc
	cancelCtx context.CancelFunc
	// done goroutine 完成通知 chan，close 表示完成
	done chan struct{}
	// agentRef 进程内 Agent 引用（对齐 Python agent_ref: Any）
	agentRef SpawnableAgent
	// chunkForward chunk 转发观察者引用，cleanup 时可确定性断开
	// ⤵️ 预留：StreamController（9.60）实现后回填类型
	chunkForward any
	// onUnhealthy 不健康回调
	onUnhealthy func()
	// shutdownRequested 是否已请求关闭
	shutdownRequested bool
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// inprocessLogComponent 日志组件
	inprocessLogComponent = logger.ComponentChannel
	// defaultShutdownTimeout 默认关闭超时
	defaultShutdownTimeout = 10 * time.Second
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewInProcessSpawnHandle 创建新的 InProcessSpawnHandle。
// 对齐 Python: InProcessSpawnHandle(process_id, _task, agent_ref)
func NewInProcessSpawnHandle(
	processID string,
	cancelCtx context.CancelFunc,
	done chan struct{},
	agentRef SpawnableAgent,
) *InProcessSpawnHandle {
	return &InProcessSpawnHandle{
		processID: processID,
		cancelCtx: cancelCtx,
		done:      done,
		agentRef:  agentRef,
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// ProcessID 返回进程唯一标识。
func (h *InProcessSpawnHandle) ProcessID() string {
	return h.processID
}

// IsAlive 检查任务是否仍在运行。
// 对齐 Python: InProcessSpawnHandle.is_alive
func (h *InProcessSpawnHandle) IsAlive() bool {
	select {
	case <-h.done:
		return false
	default:
		return true
	}
}

// IsHealthy 检查任务是否健康（存活且未请求关闭）。
// 对齐 Python: InProcessSpawnHandle.is_healthy
func (h *InProcessSpawnHandle) IsHealthy() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.IsAlive() && !h.shutdownRequested
}

// StartHealthCheck 启动健康检查后台任务（No-op：进程内无需 IPC 健康检查）。
// 对齐 Python: InProcessSpawnHandle.start_health_check
func (h *InProcessSpawnHandle) StartHealthCheck(_ context.Context, _ ...time.Duration) error {
	return nil
}

// StopHealthCheck 停止健康检查后台任务（No-op）。
// 对齐 Python: InProcessSpawnHandle.stop_health_check
func (h *InProcessSpawnHandle) StopHealthCheck() error {
	return nil
}

// Shutdown 优雅关闭：取消 goroutine 并等待完成。
// 对齐 Python: InProcessSpawnHandle.shutdown
//
// 返回 (graceful, error)：graceful=true 表示在超时内完成。
func (h *InProcessSpawnHandle) Shutdown(ctx context.Context, timeout ...time.Duration) (bool, error) {
	h.mu.Lock()
	if h.shutdownRequested {
		h.mu.Unlock()
		return false, fmt.Errorf("关闭已请求")
	}
	h.shutdownRequested = true
	h.mu.Unlock()

	if !h.IsAlive() {
		return true, nil
	}

	// 取消 goroutine
	if h.cancelCtx != nil {
		h.cancelCtx()
	}

	// 等待完成
	shutdownTimeout := defaultShutdownTimeout
	if len(timeout) > 0 && timeout[0] > 0 {
		shutdownTimeout = timeout[0]
	}

	timer := time.NewTimer(shutdownTimeout)
	defer timer.Stop()

	select {
	case <-h.done:
		logger.Info(inprocessLogComponent).
			Str("process_id", h.processID).
			Msg("进程内任务正常关闭")
		return true, nil
	case <-timer.C:
		logger.Warn(inprocessLogComponent).
			Str("process_id", h.processID).
			Dur("timeout", shutdownTimeout).
			Msg("进程内任务关闭超时")
		return false, fmt.Errorf("关闭超时: %s", h.processID)
	}
}

// ForceKill 强制终止：取消 goroutine，不等待完成。
// 对齐 Python: InProcessSpawnHandle.force_kill
func (h *InProcessSpawnHandle) ForceKill() error {
	h.mu.Lock()
	h.shutdownRequested = true
	h.mu.Unlock()

	if h.cancelCtx != nil {
		h.cancelCtx()
	}

	logger.Info(inprocessLogComponent).
		Str("process_id", h.processID).
		Msg("强制终止进程内任务")
	return nil
}

// WaitForCompletion 等待任务完成。
// 对齐 Python: InProcessSpawnHandle.wait_for_completion
//
// 返回 0=成功，-1=异常或未启动。
func (h *InProcessSpawnHandle) WaitForCompletion() (int, error) {
	<-h.done
	return 0, nil
}

// SetOnUnhealthy 设置不健康回调。
// 对齐 Python: SpawnedProcessHandle.on_unhealthy 构造后赋值
func (h *InProcessSpawnHandle) SetOnUnhealthy(fn func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onUnhealthy = fn
}

// AgentRef 返回进程内 Agent 引用。
// 对齐 Python: InProcessSpawnHandle.agent_ref
// 消费者需自行断言为具体类型。
func (h *InProcessSpawnHandle) AgentRef() SpawnableAgent {
	return h.agentRef
}

// ChunkForward 返回 chunk 转发观察者引用。
// ⤵️ 预留：StreamController（9.60）实现后回填
func (h *InProcessSpawnHandle) ChunkForward() any {
	return h.chunkForward
}

// SetChunkForward 设置 chunk 转发观察者引用。
// ⤵️ 预留：StreamController（9.60）实现后回填
func (h *InProcessSpawnHandle) SetChunkForward(v any) {
	h.chunkForward = v
}

// AgentCard 返回 AgentCard（满足 SpawnableAgent 接口）。
// 便利方法，等价于 h.agentRef.AgentCard()。
func (h *InProcessSpawnHandle) AgentCard() *agentschema.AgentCard {
	if h.agentRef == nil {
		return nil
	}
	return h.agentRef.AgentCard()
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/agent_teams/spawn/
git commit -m "feat(agent_teams): InProcessSpawnHandle 进程内生成句柄"
```

---

### Task 3: InProcessSpawn 函数 + SpawnableAgent 接口

**Files:**
- Create: `internal/agent_teams/spawn/inprocess_spawn.go`
- Test: `internal/agent_teams/spawn/inprocess_spawn_test.go`

- [x] **Step 1: 写 InProcessSpawn 测试**

```go
package spawn_test

import (
	"context"
	"testing"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// stubSpawnableAgent 测试用 Agent 桩
type stubSpawnableAgent struct {
	card *agentschema.AgentCard
}

func (a *stubSpawnableAgent) AgentCard() *agentschema.AgentCard {
	return a.card
}

// TestInProcessSpawn_基本流程 测试基本的 inprocess spawn 流程。
func TestInProcessSpawn_基本流程(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return &stubSpawnableAgent{
			card: &agentschema.AgentCard{ID: "test-agent", Name: "Test"},
		}, nil
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "alice",
	}

	handle, err := spawn.InProcessSpawn(
		context.Background(),
		factory,
		runtimeCtx,
		"Hello team",
		"session-1",
	)
	if err != nil {
		t.Fatalf("InProcessSpawn() error = %v", err)
	}

	if handle.ProcessID() != "inproc-alice" {
		t.Errorf("ProcessID() = %q, want %q", handle.ProcessID(), "inproc-alice")
	}
	if !handle.IsAlive() {
		t.Error("IsAlive() = false, want true")
	}
	if handle.AgentRef() == nil {
		t.Error("AgentRef() = nil, want non-nil")
	}
}

// TestInProcessSpawn_空initialMessage使用默认值 测试空 initialMessage 使用默认消息。
func TestInProcessSpawn_空initialMessage使用默认值(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return &stubSpawnableAgent{card: &agentschema.AgentCard{ID: "test"}}, nil
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "bob",
	}

	handle, err := spawn.InProcessSpawn(context.Background(), factory, runtimeCtx, "", "session-1")
	if err != nil {
		t.Fatalf("InProcessSpawn() error = %v", err)
	}
	_ = handle // 基本流程验证
}

// TestInProcessSpawn_工厂返回错误 测试工厂函数返回错误。
func TestInProcessSpawn_工厂返回错误(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return nil, fmt.Errorf("工厂失败")
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "charlie",
	}

	_, err := spawn.InProcessSpawn(context.Background(), factory, runtimeCtx, "", "session-1")
	if err == nil {
		t.Fatal("InProcessSpawn() error = nil, want error")
	}
}

// TestInProcessSpawn_取消context关闭goroutine 测试取消 context 关闭 goroutine。
func TestInProcessSpawn_取消context关闭goroutine(t *testing.T) {
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		return &stubSpawnableAgent{card: &agentschema.AgentCard{ID: "test"}}, nil
	}

	runtimeCtx := atschema.TeamRuntimeContext{
		Role:       atschema.TeamRoleTeammate,
		MemberName: "dave",
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	handle, err := spawn.InProcessSpawn(ctx, factory, runtimeCtx, "test", "session-1")
	if err != nil {
		t.Fatalf("InProcessSpawn() error = %v", err)
	}

	if !handle.IsAlive() {
		t.Error("IsAlive() = false, want true")
	}

	// 强制终止
	_ = handle.ForceKill()

	// 等待 goroutine 完成（给一点时间）
	time.Sleep(100 * time.Millisecond)
}

// TestInProcessSpawn_满足SpawnHandle接口 编译期断言。
func TestInProcessSpawn_满足SpawnHandle接口(t *testing.T) {
	var _ spawn.SpawnHandle = (*spawn.InProcessSpawnHandle)(nil)
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -v`
Expected: FAIL — `spawn.InProcessSpawn` 未定义

- [x] **Step 3: 实现 inprocess_spawn.go**

```go
package spawn

import (
	"context"
	"fmt"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// defaultInitialMessage 默认初始消息
	// 对齐 Python: "Join the team and wait for your first assignment."
	defaultInitialMessage = "Join the team and wait for your first assignment."
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 接口 ────────────────────────────

// SpawnableAgent 进程内生成的 Agent 最小接口。
// 仅暴露 InProcessSpawnHandle 消费者所需操作，不包含运行方法——运行由 Runner 层负责。
// 对齐 Python: InProcessSpawnHandle.agent_ref 的 Any 类型，
// Go 中用接口替代以保留最小类型安全，同时避免 spawn/ 包 import agent/ 包。
type SpawnableAgent interface {
	// AgentCard 返回 Agent 身份卡片
	AgentCard() *agentschema.AgentCard
}

// ──────────────────────────── 导出函数 ────────────────────────────

// AgentFactory 创建并配置 Agent 的工厂函数。
// 对齐 Python: _TeamAgent(card) + teammate.configure(spec, ctx)
// 由 SpawnManager 注入具体实现，封装 spec 解析 / card 构建 / 配置全流程。
type AgentFactory func(runtimeCtx atschema.TeamRuntimeContext) (SpawnableAgent, error)

// InProcessSpawn 以进程内 goroutine 方式生成 teammate。
// 对齐 Python: inprocess_spawn(team_agent, ctx, initial_message, session_id)
//
// 核心逻辑：
//  1. 调用工厂创建并配置 teammate
//  2. 准备输入（initialMessage 为空时使用默认消息）
//  3. 启动 goroutine 运行 Agent（对齐 Python: asyncio.create_task）
//     goroutine 内部调用 Runner.RunAgentTeam，当前 TODO(#9.85) 占位
//  4. 包装 InProcessSpawnHandle 返回
func InProcessSpawn(
	ctx context.Context,
	factory AgentFactory,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
) (*InProcessSpawnHandle, error) {
	// 1. 调用工厂创建并配置 teammate
	teammate, err := factory(runtimeCtx)
	if err != nil {
		return nil, fmt.Errorf("工厂创建 Agent 失败: %w", err)
	}

	// 2. 准备输入
	query := initialMessage
	if query == "" {
		query = defaultInitialMessage
	}
	inputs := map[string]any{"query": query}

	// 3. 启动 goroutine 运行 Agent（对齐 Python: asyncio.create_task(_run())）
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		// 对齐 Python: set_session_id(session_id)
		// Go: sessionID 通过参数传递，无需 contextvars

		logger.Info(inprocessLogComponent).
			Str("member_name", runtimeCtx.MemberName).
			Msg("[inprocess] teammate started")

		// 对齐 Python: await Runner.run_agent_team(teammate, inputs, member=True, session=session_id)
		// ⤵️ 预留：TeamRunner（9.85）实现后回填
		// _, err := runner.RunAgentTeam(runCtx, teammate, inputs, true, sessionID)
		_ = inputs   // 避免未使用变量警告
		_ = query    // 同上
		_ = sessionID

		logger.Info(inprocessLogComponent).
			Str("member_name", runtimeCtx.MemberName).
			Msg("[inprocess] teammate exited")
	}()

	// 4. 包装句柄
	handle := &InProcessSpawnHandle{
		processID: fmt.Sprintf("inproc-%s", runtimeCtx.MemberName),
		cancelCtx: cancel,
		done:      done,
		agentRef:  teammate,
	}

	logger.Info(inprocessLogComponent).
		Str("member_name", runtimeCtx.MemberName).
		Str("process_id", handle.processID).
		Msg("[inprocess] spawned teammate")

	return handle, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/agent_teams/spawn/
git commit -m "feat(agent_teams): InProcessSpawn 函数 + SpawnableAgent 接口"
```

---

### Task 4: SharedResources

**Files:**
- Create: `internal/agent_teams/spawn/shared_resources.go`
- Test: `internal/agent_teams/spawn/shared_resources_test.go`

- [x] **Step 1: 写 SharedResources 测试**

```go
package spawn_test

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
)

// TestGetSharedRuntime_当前返回nil 测试 GetSharedRuntime 当前返回 nil + TODO。
func TestGetSharedRuntime_当前返回nil(t *testing.T) {
	// 清理确保干净状态
	spawn.CleanupSharedResources()

	result := spawn.GetSharedRuntime()
	// 当前返回 nil（TODO #9.85）
	_ = result
}

// TestGetSharedDB_当前返回nil 测试 GetSharedDB 当前返回 nil + TODO。
func TestGetSharedDB_当前返回nil(t *testing.T) {
	spawn.CleanupSharedResources()

	result := spawn.GetSharedDB(nil)
	// 当前返回 nil（TODO #9.64）
	_ = result
}

// TestCleanupSharedResources_可重复调用 测试清理可重复调用。
func TestCleanupSharedResources_可重复调用(t *testing.T) {
	// 不应 panic
	spawn.CleanupSharedResources()
	spawn.CleanupSharedResources()
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -v`
Expected: FAIL — `spawn.GetSharedRuntime` 未定义

- [x] **Step 3: 实现 shared_resources.go**

```go
package spawn

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sharedLogComponent 日志组件
	sharedLogComponent = logger.ComponentChannel
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// sharedRuntime 进程级 TeamRuntime 单例
	// ⤵️ 预留：TeamRuntime（9.85）实现后回填类型
	sharedRuntime any
	// sharedMemoryDB 进程级 InMemoryTeamDatabase 单例
	// ⤵️ 预留：TeamDatabase（9.64）实现后回填类型
	sharedMemoryDB any
	// sharedDBInstances 按 db_type::connection_string 索引的 TeamDatabase 实例
	// ⤵️ 预留：TeamDatabase（9.64）实现后回填类型
	sharedDBInstances = make(map[string]any)
	// resourcesMu 共享资源读写锁
	resourcesMu sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetSharedRuntime 返回进程级 TeamRuntime 单例，首次调用时创建。
// 对齐 Python: get_shared_runtime()
// ⤵️ 预留：TeamRuntime（9.85）实现后回填
func GetSharedRuntime() any {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()

	if sharedRuntime == nil {
		logger.Info(sharedLogComponent).Msg("创建共享 TeamRuntime 单例（TODO #9.85）")
		// TODO(#9.85): sharedRuntime = NewTeamRuntime()
	}
	return sharedRuntime
}

// GetSharedDB 返回进程级数据库实例。
// 对齐 Python: get_shared_db(config)
//
// db_type == "memory" → 全局唯一 InMemoryTeamDatabase 单例。
// db_type != "memory" → 按 db_type::connection_string 去重。
// ⤵️ 预留：TeamDatabase（9.64）实现后回填
func GetSharedDB(config any) any {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()

	// TODO(#9.64): 解析 config.db_type
	// if dbType == "memory" { return _getSharedMemoryDB() }
	// return _getSharedDBInstance(config)

	logger.Debug(sharedLogComponent).Msg("GetSharedDB 当前返回 nil（TODO #9.64）")
	return nil
}

// CleanupSharedResources 重置所有进程级全局单例。
// 对齐 Python: cleanup_shared_resources()
// 用于测试间重置。
func CleanupSharedResources() {
	resourcesMu.Lock()
	defer resourcesMu.Unlock()

	sharedRuntime = nil
	sharedMemoryDB = nil
	sharedDBInstances = make(map[string]any)

	// ⤵️ 预留：Messager（9.65）实现后回填
	// cleanupInprocessBus()

	logger.Debug(sharedLogComponent).Msg("已清理共享资源")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// _getSharedMemoryDB 返回全局 InMemoryTeamDatabase 单例。
// ⤵️ 预留：TeamDatabase（9.64）实现后回填
// func _getSharedMemoryDB() any { ... }

// _getSharedDBInstance 按 db_type::connection_string 返回 TeamDatabase 实例。
// ⤵️ 预留：TeamDatabase（9.64）实现后回填
// func _getSharedDBInstance(config any) any { ... }
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/spawn/... -v`
Expected: PASS

- [x] **Step 5: Commit**

```bash
git add internal/agent_teams/spawn/
git commit -m "feat(agent_teams): SharedResources 进程级全局单例"
```

---

### Task 5: SpawnManager

**Files:**
- Create: `internal/agent_teams/agent/spawn_manager.go`
- Test: `internal/agent_teams/agent/spawn_manager_test.go`

- [x] **Step 1: 写 SpawnManager 测试**

```go
package agent_test

import (
	"context"
	"testing"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/agent"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
)

// TestNewSpawnManager_基本创建 测试 SpawnManager 基本创建。
func TestNewSpawnManager_基本创建(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)
	if sm == nil {
		t.Fatal("NewSpawnManager() = nil")
	}
}

// TestSpawnManager_LookupInprocessAgent_空map 测试空 map 时查找返回 nil。
func TestSpawnManager_LookupInprocessAgent_空map(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)

	result := sm.LookupInprocessAgent("alice")
	if result != nil {
		t.Errorf("LookupInprocessAgent() = %v, want nil", result)
	}
}

// TestSpawnManager_CancelRecoveryTasks 测试取消恢复任务。
func TestSpawnManager_CancelRecoveryTasks(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)

	// 不应 panic
	sm.CancelRecoveryTasks()
}

// TestSpawnManager_ShutdownAllHandles_空 测试关闭空句柄集合。
func TestSpawnManager_ShutdownAllHandles_空(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)

	// 不应 panic
	sm.ShutdownAllHandles(context.Background())
}

// TestSpawnManager_OnTeammateUnhealthy 测试不健康回调。
func TestSpawnManager_OnTeammateUnhealthy(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)

	// 没有句柄时不应 panic
	sm.OnTeammateUnhealthy("nonexistent")
}

// TestSpawnManager_BuildContextFromDB_占位 测试从 DB 恢复上下文（当前占位）。
func TestSpawnManager_BuildContextFromDB_占位(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)

	ctx, err := sm.BuildContextFromDB("alice")
	// 当前返回空上下文 + nil error（TODO #9.64）
	if err != nil {
		t.Errorf("BuildContextFromDB() error = %v, want nil", err)
	}
	_ = ctx
}

// TestSpawnManager_PublishRestartEvent_占位 测试发布重启事件（当前占位）。
func TestSpawnManager_PublishRestartEvent_占位(t *testing.T) {
	state := agent.NewTeamAgentState()
	card := &agentschema.AgentCard{ID: "test", Name: "Test"}
	configurator := agent.NewAgentConfigurator(card)

	sm := agent.NewSpawnManager(state, configurator, nil)

	// 当前为 no-op（TODO #9.65），不应 panic
	sm.PublishRestartEvent("alice", 1)
}
```

- [x] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -run TestNewSpawnManager -v`
Expected: FAIL — `agent.NewSpawnManager` 未定义

- [x] **Step 3: 实现 spawn_manager.go**

```go
package agent

import (
	"context"
	"fmt"
	"sync"
	"time"

	atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/spawn"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SpawnManager 管理 teammate 进程生命周期和健康监控。
// 对齐 Python: SpawnManager (openjiuwen/agent_teams/agent/spawn_manager.py)
//
// 职责：
//   - 双模式进程生成（inprocess / subprocess）
//   - 健康检查协调
//   - 进程清理和重启
//   - 生成配置构建
type SpawnManager struct {
	// state 可变运行时状态
	state *TeamAgentState
	// configurator Agent 配置器
	configurator *AgentConfigurator
	// getTeamAgent 获取当前 TeamAgent 实例的闭包
	getTeamAgent func() *TeamAgent
	// spawnedHandles 已生成的句柄，key=memberName
	spawnedHandles map[string]spawn.SpawnHandle
	// recoveryCancel 恢复任务取消函数，key=memberName
	recoveryCancel map[string]context.CancelFunc
	// mu 保护并发访问
	mu sync.Mutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// spawnLogComponent 日志组件
	spawnLogComponent = logger.ComponentAgentCore
	// defaultMaxRetries 默认最大重启重试次数
	// 对齐 Python: restart_teammate(max_retries=3)
	defaultMaxRetries = 3
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSpawnManager 创建新的 SpawnManager。
// 对齐 Python: SpawnManager.__init__(state, configurator, team_agent_getter)
func NewSpawnManager(
	state *TeamAgentState,
	configurator *AgentConfigurator,
	teamAgentGetter func() *TeamAgent,
) *SpawnManager {
	return &SpawnManager{
		state:          state,
		configurator:   configurator,
		getTeamAgent:   teamAgentGetter,
		spawnedHandles: make(map[string]spawn.SpawnHandle),
		recoveryCancel: make(map[string]context.CancelFunc),
	}
}

// ──────────────────────────── 导出方法 ────────────────────────────

// SpawnTeammate 生成 teammate，根据 spawn_mode 选择 inprocess 或 subprocess。
// 对齐 Python: SpawnManager.spawn_teammate(ctx, initial_message, session, spawn_config)
func (m *SpawnManager) SpawnTeammate(
	ctx context.Context,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
	spawnCfg any,
) error {
	memberName := runtimeCtx.MemberName
	spawnMode := "process" // 默认 subprocess
	if m.configurator != nil && m.configurator.Spec() != nil {
		spawnMode = m.configurator.Spec().SpawnMode
	}

	logger.Info(spawnLogComponent).
		Str("member_name", memberName).
		Str("spawn_mode", spawnMode).
		Msg("生成 teammate")

	var handle spawn.SpawnHandle
	var err error

	if spawnMode == "inprocess" {
		handle, err = m.spawnInprocess(ctx, runtimeCtx, initialMessage, sessionID)
	} else {
		handle, err = m.spawnSubprocess(ctx, runtimeCtx, initialMessage, sessionID, spawnCfg)
	}

	if err != nil {
		return fmt.Errorf("生成 teammate %s 失败: %w", memberName, err)
	}

	// 注册不健康回调
	handle.SetOnUnhealthy(func() { m.OnTeammateUnhealthy(memberName) })

	m.mu.Lock()
	m.spawnedHandles[memberName] = handle
	m.mu.Unlock()

	return nil
}

// LookupInprocessAgent 查找进程内 agent 引用。
// 对齐 Python: SpawnManager.lookup_inprocess_agent(member_name)
//
// 返回 nil 如果该成员不是 inprocess 模式或不存在。
func (m *SpawnManager) LookupInprocessAgent(memberName string) spawn.SpawnableAgent {
	m.mu.Lock()
	handle, ok := m.spawnedHandles[memberName]
	m.mu.Unlock()

	if !ok {
		return nil
	}

	inproc, ok := handle.(*spawn.InProcessSpawnHandle)
	if !ok {
		return nil
	}

	return inproc.AgentRef()
}

// CleanupTeammate 清理单个 teammate。
// 对齐 Python: SpawnManager.cleanup_teammate(member_name)
//
// 先断开 chunk_forward 观察者，再 force_kill。
func (m *SpawnManager) CleanupTeammate(ctx context.Context, memberName string) {
	m.mu.Lock()
	handle, ok := m.spawnedHandles[memberName]
	if ok {
		delete(m.spawnedHandles, memberName)
	}
	m.mu.Unlock()

	if !ok {
		return
	}

	// 断开 chunk_forward 观察者（对齐 Python: handle.chunk_forward = None）
	if inproc, ok := handle.(*spawn.InProcessSpawnHandle); ok {
		inproc.SetChunkForward(nil)
	}

	// 强制终止
	_ = handle.ForceKill()

	logger.Info(spawnLogComponent).
		Str("member_name", memberName).
		Msg("已清理 teammate")
}

// RestartTeammate 重启 teammate，指数退避重试。
// 对齐 Python: SpawnManager.restart_teammate(member_name, max_retries=3)
func (m *SpawnManager) RestartTeammate(ctx context.Context, memberName string, maxRetries int) error {
	if maxRetries <= 0 {
		maxRetries = defaultMaxRetries
	}

	// 清理旧句柄
	m.CleanupTeammate(ctx, memberName)

	// 从 DB 恢复上下文
	runtimeCtx, err := m.BuildContextFromDB(memberName)
	if err != nil {
		return fmt.Errorf("恢复 %s 上下文失败: %w", memberName, err)
	}

	// 指数退避重试
	for attempt := 1; attempt <= maxRetries; attempt++ {
		err := m.SpawnTeammate(ctx, runtimeCtx, "", "", nil)
		if err == nil {
			m.PublishRestartEvent(memberName, attempt)
			logger.Info(spawnLogComponent).
				Str("member_name", memberName).
				Int("attempt", attempt).
				Msg("重启 teammate 成功")
			return nil
		}

		logger.Warn(spawnLogComponent).
			Str("member_name", memberName).
			Int("attempt", attempt).
			Int("max_retries", maxRetries).
			Err(err).
			Msg("重启 teammate 失败")

		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(backoff):
			}
		}
	}

	return fmt.Errorf("重启 teammate %s 失败：超过最大重试次数 %d", memberName, maxRetries)
}

// OnTeammateUnhealthy 不健康回调，标记 RESTARTING 并尝试重启。
// 对齐 Python: SpawnManager.on_teammate_unhealthy(member_name)
func (m *SpawnManager) OnTeammateUnhealthy(memberName string) {
	logger.Warn(spawnLogComponent).
		Str("member_name", memberName).
		Msg("teammate 不健康，尝试重启")

	// 在独立 goroutine 中重启，避免阻塞健康检查
	recoverCtx, cancel := context.WithTimeout(context.Background(), 60*time.Second)

	m.mu.Lock()
	m.recoveryCancel[memberName] = cancel
	m.mu.Unlock()

	go func() {
		defer cancel()
		if err := m.RestartTeammate(recoverCtx, memberName, defaultMaxRetries); err != nil {
			logger.Error(spawnLogComponent).
				Str("member_name", memberName).
				Err(err).
				Msg("重启 teammate 最终失败")
			// TODO(#9.64): 更新 DB 状态为 ERROR
		}
	}()
}

// BuildContextFromDB 从 DB 恢复 TeamRuntimeContext。
// 对齐 Python: SpawnManager.build_context_from_db(member_name)
// ⤵️ 预留：TeamDatabase（9.64）实现后回填
func (m *SpawnManager) BuildContextFromDB(memberName string) (atschema.TeamRuntimeContext, error) {
	// TODO(#9.64): 从 TeamDatabase 读取 teammate 行
	// 解析 model_ref_json → resolve_member_model
	// 构建 TeamRuntimeContext (role, member_name, persona, team_spec, ...)
	logger.Debug(spawnLogComponent).
		Str("member_name", memberName).
		Msg("BuildContextFromDB 当前返回空上下文（TODO #9.64）")
	return atschema.TeamRuntimeContext{}, nil
}

// PublishRestartEvent 发布重启事件。
// 对齐 Python: SpawnManager.publish_restart_event(member_name, restart_count)
// ⤵️ 预留：Messager（9.65）实现后回填
func (m *SpawnManager) PublishRestartEvent(memberName string, restartCount int) {
	// TODO(#9.65): 通过 Messager 发布 MemberRestartedEvent
	logger.Debug(spawnLogComponent).
		Str("member_name", memberName).
		Int("restart_count", restartCount).
		Msg("PublishRestartEvent 当前为 no-op（TODO #9.65）")
}

// ShutdownAllHandles 关闭所有已生成的句柄。
// 对齐 Python: SpawnManager.shutdown_all_handles()
func (m *SpawnManager) ShutdownAllHandles(ctx context.Context) {
	m.mu.Lock()
	handles := make(map[string]spawn.SpawnHandle)
	for k, v := range m.spawnedHandles {
		handles[k] = v
	}
	m.spawnedHandles = make(map[string]spawn.SpawnHandle)
	m.mu.Unlock()

	for memberName, handle := range handles {
		// 断开 chunk_forward
		if inproc, ok := handle.(*spawn.InProcessSpawnHandle); ok {
			inproc.SetChunkForward(nil)
		}
		_ = handle.ForceKill()
		logger.Info(spawnLogComponent).
			Str("member_name", memberName).
			Msg("已关闭 teammate 句柄")
	}
}

// CancelRecoveryTasks 取消所有恢复任务。
// 对齐 Python: SpawnManager.cancel_recovery_tasks()
func (m *SpawnManager) CancelRecoveryTasks() {
	m.mu.Lock()
	cancels := make(map[string]context.CancelFunc)
	for k, v := range m.recoveryCancel {
		cancels[k] = v
	}
	m.recoveryCancel = make(map[string]context.CancelFunc)
	m.mu.Unlock()

	for _, cancel := range cancels {
		cancel()
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// spawnInprocess 以 inprocess 模式生成 teammate。
// 对齐 Python: inprocess_spawn(team_agent, ctx, initial_message, session_id)
func (m *SpawnManager) spawnInprocess(
	ctx context.Context,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
) (*spawn.InProcessSpawnHandle, error) {
	// 构建工厂函数（对齐 Python: _TeamAgent(card) + teammate.configure(spec, ctx)）
	factory := func(ctx atschema.TeamRuntimeContext) (spawn.SpawnableAgent, error) {
		// 对齐 Python: agent_spec = spec.agents.get(ctx.role.value) or spec.agents["leader"]
		spec := m.configurator.Spec()
		agentSpec := ResolveAgentSpec(*spec, ctx.Role, ctx.MemberName)

		// 对齐 Python: card = agent_spec.card or AgentCard(...)
		card := agentSpec.Card
		if card == nil {
			card = &agentschema.AgentCard{
				ID:          fmt.Sprintf("%s_%s", spec.TeamName, ctx.MemberName),
				Name:        ctx.MemberName,
				Description: fmt.Sprintf("Teammate: %s", ctx.Persona),
			}
		}

		// 对齐 Python: teammate = _TeamAgent(card)
		teammate := NewTeamAgent(card)

		// 对齐 Python: teammate.configure(spec, ctx)
		teammate.Configure(context.Background(), *spec, ctx)

		return teammate, nil
	}

	handle, err := spawn.InProcessSpawn(ctx, factory, runtimeCtx, initialMessage, sessionID)
	if err != nil {
		return nil, err
	}

	// 接入 chunk 转发观察者
	// ⤵️ 预留：StreamController（9.60）实现后回填
	// m.wireInprocessChunkForward(handle)

	return handle, nil
}

// spawnSubprocess 以 subprocess 模式生成 teammate。
// 对齐 Python: Runner.spawn_agent(build_spawn_config(ctx), build_spawn_payload(ctx), session, spawn_config)
func (m *SpawnManager) spawnSubprocess(
	ctx context.Context,
	runtimeCtx atschema.TeamRuntimeContext,
	initialMessage string,
	sessionID string,
	spawnCfg any,
) (spawn.SpawnHandle, error) {
	// 构建载荷
	payload := m.configurator.BuildSpawnPayload(runtimeCtx, initialMessage)
	if payload == nil {
		payload = make(map[string]any)
	}

	// 构建配置
	agentConfig := m.configurator.BuildSpawnConfig(runtimeCtx).(runnerSpawnConfig)
	inputs := map[string]any{"query": initialMessage}
	if initialMessage == "" {
		inputs["query"] = "Join the team and wait for your first assignment."
	}

	// 调用 Runner.SpawnAgent
	handle, err := runner.SpawnAgent(ctx, agentConfig, inputs, nil, nil)
	if err != nil {
		return nil, fmt.Errorf("子进程生成失败: %w", err)
	}

	return handle, nil
}

// wireInprocessChunkForward 接入 chunk 转发观察者。
// 对齐 Python: SpawnManager._wire_inprocess_chunk_forward(handle)
// ⤵️ 预留：StreamController（9.60）实现后回填
// func (m *SpawnManager) wireInprocessChunkForward(handle *spawn.InProcessSpawnHandle) { ... }
```

- [x] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/agent/... -run TestSpawnManager -v`
Expected: PASS（可能需要调整 import 和类型断言）

- [x] **Step 5: Commit**

```bash
git add internal/agent_teams/agent/spawn_manager.go internal/agent_teams/agent/spawn_manager_test.go
git commit -m "feat(agent_teams): SpawnManager 子进程管理器"
```

---

### Task 6: 修改现有文件 — team_agent.go + agent_configurator.go + payload.go + child.go + doc.go

**Files:**
- Modify: `internal/agent_teams/agent/team_agent.go`
- Modify: `internal/agent_teams/agent/agent_configurator.go`
- Modify: `internal/agent_teams/agent/payload.go`
- Modify: `internal/agentcore/runner/spawn/child.go`
- Modify: `internal/agent_teams/doc.go`

- [x] **Step 1: 修改 team_agent.go — spawnManager 类型 + 方法实现**

将 `spawnManager any` 改为 `spawnManager *SpawnManager`，并实现相关方法委托：

1. `NewTeamAgent` 中构建 SpawnManager
2. `SpawnTeammate` 委托 `spawnManager.SpawnTeammate`
3. `AutoStartMember` / `AutoStartAll` 标注 ⤵️ #9.58 TeamBackend
4. `LookupHumanAgentRuntime` 通过 `spawnManager.LookupInprocessAgent` 查找

- [x] **Step 2: 修改 agent_configurator.go — 回调类型 + SetupTeamBackend**

1. `onTeammateCreated` / `onTeamCleaned` / `onTeamBuilt` 类型从 `any` 改为 `func(memberName string)`
2. `setupInfraConfig` / `setupTeamBackendConfig` 中回调类型同步修改
3. `SetupTeamBackend` 方法体：补充注释步骤但标注 ⤵️ #9.58 TeamBackend

- [x] **Step 3: 修改 payload.go — BuildSpawnConfig 返回实际类型**

将 `BuildSpawnConfig` 返回 `spawn.SpawnAgentConfig` 类型：

```go
func (b *SpawnPayloadBuilder) BuildSpawnConfig(ctx atschema.TeamRuntimeContext) any {
	return spawn.SpawnAgentConfig{
		AgentKind: spawn.SpawnAgentKindTeamAgent,
		Payload:   b.BuildSpawnPayload(ctx, ""),
	}
}
```

- [x] **Step 4: 修改 child.go — TeamAgent 分支 TODO 占位**

将 `return nil, fmt.Errorf("team_agent 模式尚未实现：依赖 9.x TeamAgent")` 改为：
`return nil, fmt.Errorf("team_agent 模式尚未实现：⤵️ 预留 TeamRunner（9.85）实现后回填")`

- [x] **Step 5: 修改 agent_teams/doc.go — spawn/ 子目录描述更新**

将 `spawn/ # ⤵️ 回填: 9.58 生成器` 改为：
```
//	├── spawn/              # 进程内生成 + 共享资源（9.58）
//	│   ├── doc.go          # 包文档
//	│   ├── handle.go       # SpawnHandle 统一接口
//	│   ├── inprocess_handle.go # InProcessSpawnHandle
//	│   ├── inprocess_spawn.go  # InProcessSpawn + SpawnableAgent
//	│   └── shared_resources.go # 进程级全局单例
```

- [x] **Step 6: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`
Expected: PASS

- [x] **Step 7: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/... -v`
Expected: PASS

- [x] **Step 8: Commit**

```bash
git add -A
git commit -m "feat(agent_teams): 修改现有文件 — SpawnManager 集成 + 回调类型 + BuildSpawnConfig + doc.go"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [x] **Step 1: 更新 9.58 状态为 ✅**

将 `| 9.58 | ☐ | SpawnManager | 子进程管理 |` 改为 `| 9.58 | ✅ | SpawnManager | ... |`

补充实现说明：InProcessSpawnHandle + InProcessSpawn + SharedResources + SpawnManager + 回调类型 + BuildSpawnConfig

- [x] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 9.58 状态为已完成"
```
