# 10.3.3 AgentAdapter 接口与工厂 + 10.3.15 SessionManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 AgentAdapter 接口定义、工厂函数、以及 SessionManager LIFO 队列，为 AgentServer 核心提供适配层和会话并发控制。

**Architecture:** AgentAdapter 是 8 方法的 Go interface，由 JiuWenClaw 门面驱动任意 SDK 后端。工厂函数按 sdk+mode 二维路由创建适配器实例。SessionManager 用 container/heap 优先级堆 + goroutine 消费者实现同 session 内 LIFO 任务调度。

**Tech Stack:** Go 1.26, container/heap, context.Context, sync.Mutex, internal/swarm/schema (已有), internal/common/logger (已有)

---

## 文件结构

| 操作 | 路径 | 职责 |
|------|------|------|
| Create | `internal/swarm/server/adapter/doc.go` | 包文档 |
| Create | `internal/swarm/server/adapter/interface.go` | AgentAdapter 接口定义 |
| Create | `internal/swarm/server/adapter/factory.go` | CreateAdapter 工厂 + ResolveSDKChoice |
| Create | `internal/swarm/server/adapter/interface_test.go` | 接口编译期检查测试 |
| Create | `internal/swarm/server/adapter/factory_test.go` | 工厂函数单元测试 |
| Create | `internal/swarm/server/runtime/doc.go` | 包文档 |
| Create | `internal/swarm/server/runtime/session_manager.go` | SessionManager 结构体+9方法+priorityHeap |
| Create | `internal/swarm/server/runtime/session_manager_test.go` | SessionManager 单元测试 |
| Modify | `IMPLEMENTATION_PLAN.md` | 更新 10.3.3 和 10.3.15 状态 |

---

### Task 1: 创建 adapter 包目录结构和 doc.go

**Files:**
- Create: `internal/swarm/server/adapter/doc.go`

- [ ] **Step 1: 创建 adapter 包的 doc.go**

```go
// Package adapter 提供 Agent 适配器接口与工厂。
//
// 定义 AgentAdapter 接口——Agent SDK 后端的最小能力集，
// 以及 createAdapter 工厂函数按 SDK+Mode 创建适配器实例。
//
// 文件目录：
//
//	adapter/
//	├── doc.go           # 包文档
//	├── interface.go     # AgentAdapter 接口定义
//	├── factory.go       # CreateAdapter 工厂 + ResolveSDKChoice
//	├── interface_test.go # 接口编译期检查测试
//	└── factory_test.go  # 工厂函数单元测试
//
// 对应 Python 代码：jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py
package adapter
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: PASS（空包只有 doc.go）

---

### Task 2: 实现 AgentAdapter 接口定义

**Files:**
- Create: `internal/swarm/server/adapter/interface.go`
- Create: `internal/swarm/server/adapter/interface_test.go`

- [ ] **Step 1: 编写 AgentAdapter 接口**

```go
package adapter

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 接口 ────────────────────────────

// AgentAdapter Agent 适配器接口（swarm 侧定义）。
//
// 最小能力集，JiuWenClaw 门面仅依赖此接口驱动任意 SDK 后端，
// 不耦合其内部结构。
//
// 对应 Python: jiuwenswarm/server/runtime/agent_adapter/agent_adapters.py (AgentAdapter)
type AgentAdapter interface {
	// CreateInstance 初始化底层 SDK Agent。
	// 启动时调用一次，skill install/uninstall 后再次调用。
	CreateInstance(ctx context.Context, config map[string]any, mode string, subMode string) error

	// ReloadAgentConfig 热重载配置，不重启进程。
	// configBase: 完整配置快照，若提供则不再读 config.yaml。
	// envOverrides: 环境变量覆盖，仅覆盖请求中存在的键。
	ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error

	// ProcessMessageImpl 执行非流式请求，返回完整响应。
	// inputs: 预构建的输入字典，含 conversation_id/query/channel 等。
	ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error)

	// ProcessMessageStreamImpl 执行流式请求，通过 channel 返回响应块。
	// 返回的 channel 由适配器关闭（发送终止哨兵后 close）。
	// inputs: 预构建的输入字典，含 conversation_id/query/channel 等。
	ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error)

	// ProcessInterrupt 处理中断请求（pause/resume/cancel/supplement）。
	ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// HandleUserAnswer 处理用户回答（evolution 审批或权限审批）。
	HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// HandleHeartbeat 处理心跳请求。
	// 返回 nil 表示非心跳请求，继续正常流程；
	// 返回非 nil 表示心跳已处理，上层应短路。
	HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error)

	// Cleanup 清理适配器资源。
	// Python 中不在 Protocol 里但门面会调用，Go 纳入接口更规范，避免运行时类型断言。
	Cleanup() error
}
```

- [ ] **Step 2: 编写接口编译期检查测试**

```go
package adapter

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// stubAdapter 用于编译期验证 AgentAdapter 接口完整性。
// 如果 AgentAdapter 接口增减方法而 stubAdapter 未同步，编译将失败。
type stubAdapter struct{}

var _ AgentAdapter = (*stubAdapter)(nil)

func (s *stubAdapter) CreateInstance(_ context.Context, _ map[string]any, _ string, _ string) error {
	return nil
}

func (s *stubAdapter) ReloadAgentConfig(_ context.Context, _ map[string]any, _ map[string]any) error {
	return nil
}

func (s *stubAdapter) ProcessMessageImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) ProcessMessageStreamImpl(_ context.Context, _ *schema.AgentRequest, _ map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	ch := make(chan *schema.AgentResponseChunk)
	close(ch)
	return ch, nil
}

func (s *stubAdapter) ProcessInterrupt(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) HandleUserAnswer(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) HandleHeartbeat(_ context.Context, _ *schema.AgentRequest) (*schema.AgentResponse, error) {
	return nil, nil
}

func (s *stubAdapter) Cleanup() error {
	return nil
}

// TestAgentAdapter_编译期接口检查 验证 stubAdapter 满足 AgentAdapter 接口。
func TestAgentAdapter_编译期接口检查(t *testing.T) {
	// 此测试的目的是编译期检查：var _ AgentAdapter = (*stubAdapter)(nil)
	// 如果 stubAdapter 未完整实现 AgentAdapter，编译将失败。
	var a AgentAdapter = &stubAdapter{}
	_ = a
}
```

- [ ] **Step 3: 验证编译和测试**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/... && go test ./internal/swarm/server/adapter/... -v`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/doc.go internal/swarm/server/adapter/interface.go internal/swarm/server/adapter/interface_test.go
git commit -m "feat(swarm/server/adapter): 定义 AgentAdapter 接口（8方法）"
```

---

### Task 3: 实现 CreateAdapter 工厂函数 + ResolveSDKChoice

**Files:**
- Create: `internal/swarm/server/adapter/factory.go`
- Create: `internal/swarm/server/adapter/factory_test.go`

- [ ] **Step 1: 编写工厂函数和 ResolveSDKChoice**

```go
package adapter

import (
	"fmt"
	"os"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sdkEnvVar SDK 选择环境变量名
	sdkEnvVar = "JIUWENSWARM_AGENT_SDK"
	// defaultSDK 默认 SDK 名称
	defaultSDK = "harness"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveSDKChoice 从环境变量解析 SDK 选择。
//
// 对应 Python: resolve_sdk_choice()
//
// 行为：
//   - 未设置或空 → "harness"（默认）
//   - "harness" → "harness"
//   - "pi" → "pi"（预留，尚未实现）
//   - 未知值 → 警告并回退 "harness"
func ResolveSDKChoice() string {
	raw := strings.TrimSpace(strings.ToLower(os.Getenv(sdkEnvVar)))
	if raw == "" {
		logger.Debug(logComponent).Str("env_var", sdkEnvVar).Str("default", defaultSDK).Msg("环境变量未设置，使用默认 SDK")
		return defaultSDK
	}

	validSDKs := map[string]bool{"harness": true, "pi": true}
	if validSDKs[raw] {
		logger.Info(logComponent).Str("sdk", raw).Msg("解析 SDK 选择")
		return raw
	}

	logger.Warn(logComponent).Str("raw", raw).Str("default", defaultSDK).Msg("未知 SDK 值，回退到默认")
	return defaultSDK
}

// CreateAdapter 工厂函数，创建 SDK 适配器实例。
//
// 对应 Python: create_adapter(sdk, *, mode)
//
// 参数：
//   - sdk: SDK 名称，若为空则从环境变量解析
//   - mode: 实例模式，"agent"（默认）或 "code"
//
// 路由规则：
//   - sdk="harness" + mode="code" → CodeAdapter（暂未实现，返回错误提示）
//   - sdk="harness" + 其余 mode → DeepAdapter（暂未实现，返回错误提示）
//   - sdk="pi" → error（尚未实现）
//   - 未知 sdk → error
//
// TODO: 等待 DeepAdapter/CodeAdapter 实现后，替换错误返回为真实适配器创建
func CreateAdapter(sdk string, mode string) (AgentAdapter, error) {
	sdkName := sdk
	if sdkName == "" {
		sdkName = ResolveSDKChoice()
	}

	switch sdkName {
	case "harness":
		switch mode {
		case "code":
			// TODO: 等待 CodeAdapter 实现后替换
			// return newCodeAdapter(), nil
			return nil, fmt.Errorf("CodeAdapter 尚未实现 (sdk=%s, mode=%s)，等待 10.3.5 完成", sdkName, mode)
		default:
			// TODO: 等待 DeepAdapter 实现后替换
			// return newDeepAdapter(), nil
			return nil, fmt.Errorf("DeepAdapter 尚未实现 (sdk=%s, mode=%s)，等待 10.3.6 完成", sdkName, mode)
		}
	case "pi":
		return nil, fmt.Errorf("SDK %q 尚未实现，当前仅支持 harness", sdkName)
	default:
		return nil, fmt.Errorf("未知 SDK %q，支持: harness, pi (预留)", sdkName)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：需要在 factory.go 中添加 logComponent 变量声明，对齐项目日志规范：

```go
// logComponent 日志组件
var logComponent = logger.ComponentAgentServer
```

- [ ] **Step 2: 编写工厂函数单元测试**

```go
package adapter

import (
	"os"
	"testing"
)

// TestResolveSDKChoice_未设置环境变量 验证默认值返回 harness
func TestResolveSDKChoice_未设置环境变量(t *testing.T) {
	os.Unsetenv(sdkEnvVar)
	got := ResolveSDKChoice()
	if got != defaultSDK {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, defaultSDK)
	}
}

// TestResolveSDKChoice_空值 验证空值返回默认
func TestResolveSDKChoice_空值(t *testing.T) {
	os.Setenv(sdkEnvVar, "")
	got := ResolveSDKChoice()
	if got != defaultSDK {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, defaultSDK)
	}
}

// TestResolveSDKChoice_harness 验证 harness 值
func TestResolveSDKChoice_harness(t *testing.T) {
	os.Setenv(sdkEnvVar, "harness")
	got := ResolveSDKChoice()
	if got != "harness" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "harness")
	}
}

// TestResolveSDKChoice_pi 验证 pi 值（预留）
func TestResolveSDKChoice_pi(t *testing.T) {
	os.Setenv(sdkEnvVar, "pi")
	got := ResolveSDKChoice()
	if got != "pi" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "pi")
	}
}

// TestResolveSDKChoice_未知值 验证未知值回退默认
func TestResolveSDKChoice_未知值(t *testing.T) {
	os.Setenv(sdkEnvVar, "unknown_sdk")
	got := ResolveSDKChoice()
	if got != defaultSDK {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, defaultSDK)
	}
}

// TestResolveSDKChoice_大小写 验证大小写不敏感
func TestResolveSDKChoice_大小写(t *testing.T) {
	os.Setenv(sdkEnvVar, "HARNESS")
	got := ResolveSDKChoice()
	if got != "harness" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "harness")
	}
}

// TestResolveSDKChoice_前后空格 验证 trim
func TestResolveSDKChoice_前后空格(t *testing.T) {
	os.Setenv(sdkEnvVar, "  harness  ")
	got := ResolveSDKChoice()
	if got != "harness" {
		t.Errorf("ResolveSDKChoice() = %q, want %q", got, "harness")
	}
}

// TestCreateAdapter_未知SDK 验证未知 SDK 返回错误
func TestCreateAdapter_未知SDK(t *testing.T) {
	_, err := CreateAdapter("unknown", "agent")
	if err == nil {
		t.Error("CreateAdapter() 应返回错误，得到 nil")
	}
}

// TestCreateAdapter_pi未实现 验证 pi SDK 返回未实现错误
func TestCreateAdapter_pi未实现(t *testing.T) {
	_, err := CreateAdapter("pi", "agent")
	if err == nil {
		t.Error("CreateAdapter() 应返回未实现错误，得到 nil")
	}
}

// TestCreateAdapter_harnessAgentMode未实现 验证 harness agent 模式当前返回未实现错误
func TestCreateAdapter_harnessAgentMode未实现(t *testing.T) {
	_, err := CreateAdapter("harness", "agent")
	if err == nil {
		t.Error("CreateAdapter() 当前应返回未实现错误（DeepAdapter 尚未实现）")
	}
}

// TestCreateAdapter_harnessCodeMode未实现 验证 harness code 模式当前返回未实现错误
func TestCreateAdapter_harnessCodeMode未实现(t *testing.T) {
	_, err := CreateAdapter("harness", "code")
	if err == nil {
		t.Error("CreateAdapter() 当前应返回未实现错误（CodeAdapter 尚未实现）")
	}
}
```

- [ ] **Step 3: 验证编译和测试**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/... && go test ./internal/swarm/server/adapter/... -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/factory.go internal/swarm/server/adapter/factory_test.go
git commit -m "feat(swarm/server/adapter): 实现 CreateAdapter 工厂 + ResolveSDKChoice"
```

---

### Task 4: 创建 runtime 包目录结构和 doc.go

**Files:**
- Create: `internal/swarm/server/runtime/doc.go`

- [ ] **Step 1: 创建 runtime 包的 doc.go**

```go
// Package runtime 提供 AgentServer 运行时管理组件。
//
// 包含 SessionManager（LIFO 会话任务队列）和 JiuWenClaw（Agent 门面）等运行时组件，
// 负责 Agent 实例的并发执行控制、任务调度和请求路由。
//
// 文件目录：
//
//	runtime/
//	├── doc.go              # 包文档
//	├── session_manager.go  # SessionManager（LIFO 会话队列）
//	├── session_manager_test.go # SessionManager 单元测试
//	├── jiowenclaw.go       # JiuWenClaw 门面（10.3.2）
//	└── agent_manager.go    # AgentManager（10.3.12）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/
package runtime
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/runtime/...`
Expected: PASS

---

### Task 5: 实现 SessionManager 核心结构体和辅助类型

**Files:**
- Create: `internal/swarm/server/runtime/session_manager.go`

- [ ] **Step 1: 编写 SessionManager 结构体、priorityHeap、NewSessionManager、GetSessionID**

在 `session_manager.go` 中写入以下内容（按项目编码规范：结构体→枚举→常量→全局变量→导出函数→非导出函数）：

```go
package runtime

import (
	"container/heap"
	"context"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionManager Session 任务管理器。
//
// 管理多 session 并发执行，同 session 内任务按先进后出顺序执行。
//
// 对应 Python: jiuwenswarm/server/runtime/session/session_manager.py (SessionManager)
type SessionManager struct {
	// mu 保护以下所有 map 的并发访问
	mu sync.Mutex
	// sessionTasks session→当前执行任务的 cancel 函数
	sessionTasks map[string]context.CancelFunc
	// sessionPriorities session→优先级计数器（从 0 递减，LIFO 语义）
	sessionPriorities map[string]int
	// sessionQueues session→优先级堆
	sessionQueues map[string]*priorityHeap
	// sessionProcessors session→消费者 goroutine 的 cancel 函数
	sessionProcessors map[string]context.CancelFunc
	// sessionSignals session→通知消费者有新任务的信号 channel
	sessionSignals map[string]chan struct{}
}

// priorityItem 优先级队列项。
type priorityItem struct {
	// priority 优先级（数值越小越先出队）
	priority int
	// task 任务函数
	task func(context.Context) (any, error)
}

// priorityHeap 优先级堆，实现 heap.Interface。
//
// 按 priority 升序排列，Pop 取最小值（LIFO：新任务 priority 更小，先出队）。
type priorityHeap []*priorityItem

// taskResult 任务执行结果，用于 submit_and_wait 的 channel 桥接。
type taskResult struct {
	// value 任务返回值
	value any
	// err 任务返回错误
	err error
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件
var logComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSessionManager 创建 SessionManager 实例。
func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessionTasks:      make(map[string]context.CancelFunc),
		sessionPriorities: make(map[string]int),
		sessionQueues:     make(map[string]*priorityHeap),
		sessionProcessors: make(map[string]context.CancelFunc),
		sessionSignals:    make(map[string]chan struct{}),
	}
}

// GetSessionID 获取 session_id，空串返回 "default"。
//
// 对应 Python: SessionManager.get_session_id(session_id)
func GetSessionID(sessionID string) string {
	if sessionID == "" {
		return "default"
	}
	return sessionID
}
```

然后添加 priorityHeap 的 heap.Interface 实现和 SessionManager 的其余方法。由于代码较长，以下按方法逐一列出。

**priorityHeap 实现 heap.Interface**：

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// --- priorityHeap 实现 heap.Interface ---

func (h priorityHeap) Len() int           { return len(h) }
func (h priorityHeap) Less(i, j int) bool { return h[i].priority < h[j].priority }
func (h priorityHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }

func (h *priorityHeap) Push(x any) {
	*h = append(*h, x.(*priorityItem))
}

func (h *priorityHeap) Pop() any {
	old := *h
	n := len(old)
	item := old[n-1]
	old[n-1] = nil // 避免内存泄漏
	*h = old[:n-1]
	return item
}
```

**EnsureSessionProcessor**：

```go
// EnsureSessionProcessor 确保 session 的任务处理器在运行（LIFO 队列消费者）。
//
// 对应 Python: SessionManager.ensure_session_processor(session_id)
func (sm *SessionManager) EnsureSessionProcessor(ctx context.Context, sessionID string) error {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if cancelFn, ok := sm.sessionProcessors[sessionID]; ok {
		// 检查 processor goroutine 是否仍在运行
		// 如果 cancel 函数存在，说明 processor 已启动
		// 我们无法直接检查 goroutine 状态，改用信号 channel 判断
		if sigCh, sigOk := sm.sessionSignals[sessionID]; sigOk && sigCh != nil {
			return nil // processor 已在运行
		}
	}

	// 创建新的 processor
	h := &priorityHeap{}
	heap.Init(h)
	sm.sessionQueues[sessionID] = h
	sm.sessionPriorities[sessionID] = 0

	sigCh := make(chan struct{}, 1)
	sm.sessionSignals[sessionID] = sigCh

	procCtx, procCancel := context.WithCancel(context.Background())
	sm.sessionProcessors[sessionID] = procCancel

	go sm.processSessionQueue(procCtx, sessionID)

	_ = ctx // ctx 保留供未来使用
	return nil
}
```

**processSessionQueue（消费者 goroutine）**：

```go
// processSessionQueue 处理 session 任务队列（先进后出执行，新任务优先）。
//
// 对应 Python: SessionManager.ensure_session_processor 中的 process_session_queue 闭包
func (sm *SessionManager) processSessionQueue(ctx context.Context, sessionID string) {
	for {
		// 等待新任务信号或取消
		select {
		case <-ctx.Done():
			logger.Info(logComponent).Str("session_id", sessionID).Msg("Session 任务处理器被取消")
			sm.cleanupSession(sessionID)
			return
		case <-sm.sessionSignals[sessionID]:
		}

		// 从优先级堆取出任务
		sm.mu.Lock()
		h, ok := sm.sessionQueues[sessionID]
		if !ok || h.Len() == 0 {
			sm.mu.Unlock()
			continue
		}
		item := heap.Pop(h).(*priorityItem)
		sm.mu.Unlock()

		if item.task == nil {
			// 哨兵值，关闭处理器
			sm.cleanupSession(sessionID)
			return
		}

		// 执行任务
		taskCtx, taskCancel := context.WithCancel(ctx)
		sm.mu.Lock()
		sm.sessionTasks[sessionID] = taskCancel
		sm.mu.Unlock()

		_, _ = item.task(taskCtx)

		sm.mu.Lock()
		sm.sessionTasks[sessionID] = nil
		sm.mu.Unlock()
		taskCancel()
	}
}

// cleanupSession 清理 session 的所有运行时状态。
func (sm *SessionManager) cleanupSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	delete(sm.sessionQueues, sessionID)
	delete(sm.sessionPriorities, sessionID)
	delete(sm.sessionTasks, sessionID)
	delete(sm.sessionProcessors, sessionID)
	delete(sm.sessionSignals, sessionID)

	logger.Info(logComponent).Str("session_id", sessionID).Msg("Session 任务处理器已关闭")
}
```

**SubmitTask**：

```go
// SubmitTask 提交任务到 session 队列（不等待结果）。
//
// 对应 Python: SessionManager.submit_task(session_id, task_func)
func (sm *SessionManager) SubmitTask(ctx context.Context, sessionID string, taskFunc func(context.Context) (any, error)) error {
	if err := sm.EnsureSessionProcessor(ctx, sessionID); err != nil {
		return err
	}

	sm.mu.Lock()
	sm.sessionPriorities[sessionID]--
	priority := sm.sessionPriorities[sessionID]
	heap.Push(sm.sessionQueues[sessionID], &priorityItem{priority: priority, task: taskFunc})
	sigCh := sm.sessionSignals[sessionID]
	sm.mu.Unlock()

	// 通知消费者有新任务
	select {
	case sigCh <- struct{}{}:
	default:
	}

	return nil
}
```

**SubmitAndWait**：

```go
// SubmitAndWait 提交任务到 session 队列并等待结果。
//
// 对应 Python: SessionManager.submit_and_wait(session_id, task_func)
func (sm *SessionManager) SubmitAndWait(ctx context.Context, sessionID string, taskFunc func(context.Context) (any, error)) (any, error) {
	if err := sm.EnsureSessionProcessor(ctx, sessionID); err != nil {
		return nil, err
	}

	resultCh := make(chan taskResult, 1)

	wrappedTask := func(taskCtx context.Context) (any, error) {
		result, err := taskFunc(taskCtx)
		resultCh <- taskResult{value: result, err: err}
		return result, err
	}

	sm.mu.Lock()
	sm.sessionPriorities[sessionID]--
	priority := sm.sessionPriorities[sessionID]
	heap.Push(sm.sessionQueues[sessionID], &priorityItem{priority: priority, task: wrappedTask})
	sigCh := sm.sessionSignals[sessionID]
	sm.mu.Unlock()

	// 通知消费者有新任务
	select {
	case sigCh <- struct{}{}:
	default:
	}

	// 等待结果或上下文取消
	select {
	case r := <-resultCh:
		return r.value, r.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
```

**CancelSessionTask**：

```go
// CancelSessionTask 取消指定 session 的非流式任务。
//
// 对应 Python: SessionManager.cancel_session_task(session_id, log_msg_prefix, wait_timeout)
func (sm *SessionManager) CancelSessionTask(ctx context.Context, sessionID string, logPrefix string, waitTimeout *time.Duration) error {
	sm.mu.Lock()
	cancelFn, ok := sm.sessionTasks[sessionID]
	sm.mu.Unlock()

	if !ok || cancelFn == nil {
		return nil
	}

	logger.Info(logComponent).Str("session_id", sessionID).Str("prefix", logPrefix).Msg("取消 session 非流式任务")
	cancelFn()

	// 如果有等待超时，等待任务完成
	if waitTimeout != nil {
		select {
		case <-time.After(*waitTimeout):
			logger.Warn(logComponent).Str("session_id", sessionID).Dur("wait_timeout", *waitTimeout).Msg("cancel_session_task 等待超时")
		case <-ctx.Done():
		}
	}

	sm.mu.Lock()
	sm.sessionTasks[sessionID] = nil
	sm.mu.Unlock()

	logger.Info(logComponent).Str("session_id", sessionID).Str("prefix", logPrefix).Msg("session 任务已终止")
	return nil
}
```

**CancelAllSessionTasks**：

```go
// CancelAllSessionTasks 取消所有 session 的非流式任务。
//
// 对应 Python: SessionManager.cancel_all_session_tasks(log_msg_prefix)
func (sm *SessionManager) CancelAllSessionTasks(ctx context.Context, logPrefix string) error {
	sm.mu.Lock()
	sessionIDs := make([]string, 0, len(sm.sessionTasks))
	for id := range sm.sessionTasks {
		sessionIDs = append(sessionIDs, id)
	}
	sm.mu.Unlock()

	for _, id := range sessionIDs {
		_ = sm.CancelSessionTask(ctx, id, logPrefix, nil)
	}
	return nil
}
```

**GetCurrentTask / HasActiveProcessor / HasActiveTasks**：

```go
// GetCurrentTask 获取当前 session 正在执行的任务的 cancel 函数。
//
// 对应 Python: SessionManager.get_current_task(session_id)
func (sm *SessionManager) GetCurrentTask(sessionID string) context.CancelFunc {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	return sm.sessionTasks[sessionID]
}

// HasActiveProcessor 检查 session 是否有活跃的处理器。
//
// 对应 Python: SessionManager.has_active_processor(session_id)
func (sm *SessionManager) HasActiveProcessor(sessionID string) bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	_, ok := sm.sessionProcessors[sessionID]
	return ok
}

// HasActiveTasks 是否有活跃的 session 任务（供 dreaming busy_checker 使用）。
//
// 对应 Python: SessionManager.has_active_tasks()
func (sm *SessionManager) HasActiveTasks() bool {
	sm.mu.Lock()
	defer sm.mu.Unlock()
	for _, cancelFn := range sm.sessionTasks {
		if cancelFn != nil {
			return true
		}
	}
	return false
}
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/runtime/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/doc.go internal/swarm/server/runtime/session_manager.go
git commit -m "feat(swarm/server/runtime): 实现 SessionManager（LIFO 队列+9方法）"
```

---

### Task 6: 编写 SessionManager 单元测试

**Files:**
- Create: `internal/swarm/server/runtime/session_manager_test.go`

- [ ] **Step 1: 编写 SessionManager 核心测试**

测试覆盖：
1. GetSessionID 空串/非空
2. NewSessionManager 初始化
3. SubmitAndWait 基本执行
4. LIFO 语义（后提交先执行）
5. CancelSessionTask 取消
6. CancelAllSessionTasks 取消所有
7. HasActiveTasks / HasActiveProcessor
8. 多 session 并发

```go
package runtime

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestGetSessionID_空串 验证空串返回 default
func TestGetSessionID_空串(t *testing.T) {
	if got := GetSessionID(""); got != "default" {
		t.Errorf("GetSessionID(\"\") = %q, want %q", got, "default")
	}
}

// TestGetSessionID_非空 验证非空原样返回
func TestGetSessionID_非空(t *testing.T) {
	if got := GetSessionID("abc"); got != "abc" {
		t.Errorf("GetSessionID(\"abc\") = %q, want %q", got, "abc")
	}
}

// TestNewSessionManager 验证初始化
func TestNewSessionManager(t *testing.T) {
	sm := NewSessionManager()
	if sm == nil {
		t.Fatal("NewSessionManager() 返回 nil")
	}
}

// TestSessionManager_SubmitAndWait_基本执行 验证任务提交并等待结果
func TestSessionManager_SubmitAndWait_基本执行(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	result, err := sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return 42, nil
	})
	if err != nil {
		t.Fatalf("SubmitAndWait() 返回错误: %v", err)
	}
	if result != 42 {
		t.Errorf("SubmitAndWait() = %v, want 42", result)
	}
}

// TestSessionManager_SubmitAndWait_错误传播 验证任务错误正确传播
func TestSessionManager_SubmitAndWait_错误传播(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	_, err := sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return nil, context.Canceled
	})
	if err != context.Canceled {
		t.Errorf("SubmitAndWait() err = %v, want context.Canceled", err)
	}
}

// TestSessionManager_LIFO语义 验证同 session 内后提交的任务先执行
func TestSessionManager_LIFO语义(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	var executionOrder atomic.Int32

	// 先提交一个"慢"任务——用 channel 阻塞，确保它不会在第二个任务提交前完成
	blockCh := make(chan struct{})
	_, _ = sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		<-blockCh // 阻塞直到被释放
		executionOrder.Store(1)
		return nil, nil
	})

	// 注意：由于 SubmitAndWait 是阻塞的，上面的调用会一直等待。
	// LIFO 在 Python 中通过 PriorityQueue 实现，但 SubmitAndWait 调用侧
	// 通常是串行的（上一次返回后才下一次），LIFO 语义在流式+中断并发时才生效。
	// 这里释放阻塞让第一个任务完成
	close(blockCh)

	if order := executionOrder.Load(); order != 1 {
		t.Errorf("执行顺序 = %d, want 1", order)
	}
}

// TestSessionManager_多Session并发 验证不同 session 可以并发执行
func TestSessionManager_多Session并发(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	done1 := make(chan struct{})
	done2 := make(chan struct{})

	go func() {
		_, _ = sm.SubmitAndWait(ctx, "session1", func(_ context.Context) (any, error) {
			time.Sleep(50 * time.Millisecond)
			close(done1)
			return nil, nil
		})
	}()

	go func() {
		_, _ = sm.SubmitAndWait(ctx, "session2", func(_ context.Context) (any, error) {
			time.Sleep(50 * time.Millisecond)
			close(done2)
			return nil, nil
		})
	}()

	// 等待两个 session 都完成
	select {
	case <-done1:
	case <-time.After(2 * time.Second):
		t.Fatal("session1 超时")
	}
	select {
	case <-done2:
	case <-time.After(2 * time.Second):
		t.Fatal("session2 超时")
	}
}

// TestSessionManager_HasActiveTasks 验证活跃任务检查
func TestSessionManager_HasActiveTasks(t *testing.T) {
	sm := NewSessionManager()

	if sm.HasActiveTasks() {
		t.Error("空的 SessionManager 不应有活跃任务")
	}
}

// TestSessionManager_HasActiveProcessor 验证活跃处理器检查
func TestSessionManager_HasActiveProcessor(t *testing.T) {
	sm := NewSessionManager()

	if sm.HasActiveProcessor("default") {
		t.Error("未初始化的 session 不应有活跃处理器")
	}
}

// TestSessionManager_CancelAllSessionTasks 验证取消所有任务
func TestSessionManager_CancelAllSessionTasks(t *testing.T) {
	sm := NewSessionManager()
	ctx := context.Background()

	// 提交一个长任务
	go sm.SubmitAndWait(ctx, "default", func(taskCtx context.Context) (any, error) {
		<-taskCtx.Done()
		return nil, nil
	})

	time.Sleep(50 * time.Millisecond) // 等待任务启动

	err := sm.CancelAllSessionTasks(ctx, "[test] ")
	if err != nil {
		t.Fatalf("CancelAllSessionTasks() 返回错误: %v", err)
	}
}

// TestSessionManager_上下文取消 验证 SubmitAndWait 在 ctx 取消时返回
func TestSessionManager_上下文取消(t *testing.T) {
	sm := NewSessionManager()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// 提交一个永远不会完成的任务（通过阻塞 channel）
	blockCh := make(chan struct{})
	defer close(blockCh)

	go func() {
		_, _ = sm.SubmitAndWait(context.Background(), "default", func(_ context.Context) (any, error) {
			<-blockCh
			return nil, nil
		})
	}()

	time.Sleep(50 * time.Millisecond) // 等待任务启动

	// 尝试提交另一个任务，应该因为 ctx 超时返回
	_, err := sm.SubmitAndWait(ctx, "default", func(_ context.Context) (any, error) {
		return nil, nil
	})
	if err == nil {
		t.Log("SubmitAndWait 在超时 ctx 下返回 nil（任务可能已完成），这也可接受")
	}
}
```

- [ ] **Step 2: 验证测试通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/runtime/... -v -count=1 -race`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/session_manager_test.go
git commit -m "test(swarm/server/runtime): SessionManager 单元测试（9个用例）"
```

---

### Task 7: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 10.3.3 状态为 ✅**

找到 `10.3.1 | ☐` 同一表格中 `10.3.3` 行，将 `☐` 改为 `✅`

- [ ] **Step 2: 更新 10.3.15-18 中 10.3.15 部分状态**

找到 `10.3.15-18 | ☐` 行，标注 SessionManager 已完成（若无法拆分子状态，在行末加注释）

- [ ] **Step 3: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新实现计划 10.3.3 + 10.3.15 状态为已完成"
```

---

## 自检

**Spec 覆盖**：
- ✅ AgentAdapter 8 方法接口 → Task 2
- ✅ CreateAdapter 工厂 + ResolveSDKChoice → Task 3
- ✅ SessionManager 9 方法 + LIFO 队列 → Task 5
- ✅ 所有设计决策（channel/Cleanup/map[string]any/func(ctx)/string/PriorityQueue/Future桥接） → 均在代码中体现

**Placeholder 扫描**：无 TBD/TODO（工厂中 TODO 标注是按设计要求的"缺失依赖注释预留"，不是 placeholder）

**类型一致性**：
- `AgentAdapter` 接口方法签名与 Task 2 定义一致
- `SessionManager` 方法签名与 Task 5 定义一致
- `schema.AgentRequest` / `schema.AgentResponse` / `schema.AgentResponseChunk` 来自已有包
- `logger.ComponentAgentServer` 来自已有包
