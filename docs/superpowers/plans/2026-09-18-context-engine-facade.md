# ContextEngine 门面实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ContextEngine 门面结构体，提供上下文池管理、处理器注册、会话状态持久化等核心能力，对齐 Python `context_engine.py`。

**Architecture:** ContextEngine 作为 Agent 级别单例，内部维护 `sessionID_contextID → ModelContext` 映射池，通过 Option 模式注入可选依赖（workspace/sysOperation），在 CreateContext 时自动透传给 SessionModelContext。接口直接修改，移除冗余的 RegisterProcessor 方法，补充 Option 参数对齐 Python 可选关键字参数。

**Tech Stack:** Go 1.22+，sync.RWMutex，context.Context，项目自有 session/schema/exception/logger 包。

**设计文档：** `docs/superpowers/specs/2026-09-18-context-engine-facade-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/agentcore/context_engine/interface/types.go` | ContextEngine 接口修改 + ModelContext 接口补充 + Option 类型定义 + ProcessorSpec |
| 修改 | `internal/agentcore/context_engine/engine.go` | contextEngine 结构体 + 全部方法实现 |
| 修改 | `internal/agentcore/context_engine/engine_test.go` | 单元测试 |
| 修改 | `internal/agentcore/context_engine/doc.go` | 更新文件目录树 |
| 修改 | `internal/agentcore/single_agent/ability_manager.go` | 适配新接口（仅影响调用处） |

---

### Task 1: 修改 ContextEngine 和 ModelContext 接口 + 定义 Option 类型

**Files:**
- 修改: `internal/agentcore/context_engine/interface/types.go`

- [ ] **Step 1: 修改 ContextEngine 接口**

在 `types.go` 中，将现有的 `ContextEngine` 接口替换为：

```go
// ContextEngine 上下文引擎门面接口。
//
// 管理上下文池、处理器注册、会话状态持久化。
// 属于 Agent 级别组件（不在 Session 中），通过 agent.contextEngine 访问。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type ContextEngine interface {
	// CreateContext 创建或获取上下文
	CreateContext(ctx context.Context, contextID string, sess *session.Session, opts ...CreateContextOption) (ModelContext, error)
	// GetContext 获取上下文（不存在返回 nil）
	GetContext(contextID string, sessionID string) ModelContext
	// CompressContext 主动压缩上下文，返回 "busy"/"compressed"/"noop"
	CompressContext(ctx context.Context, contextID string, sess *session.Session, opts ...CompressContextOption) (string, error)
	// ClearContext 清空上下文（三种粒度：全清/按session/按context+session）
	ClearContext(ctx context.Context, opts ...ClearContextOption) error
	// SaveContexts 批量持久化上下文状态，返回 contextID → state 映射
	SaveContexts(ctx context.Context, sess *session.Session, contextIDs []string) (map[string]any, error)
}
```

移除旧的 `RegisterProcessor` 方法。

- [ ] **Step 2: 补充 ModelContext 接口缺失的方法**

在 `ModelContext` 接口中，在 `ReloaderTool()` 之后添加：

```go
	// WorkspaceDir 返回工作目录路径
	//
	// 对应 Python: SessionModelContext.workspace_dir()
	WorkspaceDir() string
	// SetSessionRef 设置会话引用
	//
	// 对应 Python: SessionModelContext.set_session_ref()
	SetSessionRef(sess *session.Session)
	// OffloadMessages 将消息卸载到内存缓冲区
	//
	// 对应 Python: SessionModelContext.offload_messages()
	OffloadMessages(handle string, messages []llm_schema.BaseMessage)
	// SaveState 保存上下文状态为 map
	//
	// 对应 Python: SessionModelContext.save_state()
	SaveState() map[string]any
	// LoadState 从 map 恢复上下文状态
	//
	// 对应 Python: SessionModelContext.load_state()
	LoadState(state map[string]any)
	// CompressContext 主动压缩上下文
	//
	// 对应 Python: SessionModelContext.compress_context()
	CompressContext(ctx context.Context, opts ...CompressContextOption) (string, error)
```

- [ ] **Step 3: 在 types.go 的结构体区块前添加 Option 类型和 ProcessorSpec**

在 `// ──────────────────────────── 结构体 ────────────────────────────` 注释之前（接口区块内），添加：

```go
// ProcessorSpec 处理器规格，指定类型和配置。
//
// 对应 Python: (processor_type, processor_config) 元组
type ProcessorSpec struct {
	// Type 处理器类型标识
	Type string
	// Config 处理器配置
	Config ProcessorConfig
}

// ──────────────────────────── Option 类型 ────────────────────────────

// ContextEngineOption ContextEngine 构造器选项函数
type ContextEngineOption func(*contextEngineOptions)

// contextEngineOptions ContextEngine 构造器可选项
type contextEngineOptions struct {
	workspace    any // ⤵️ 9.32 回填：替换为 Workspace 接口类型
	sysOperation any // ⤵️ 9.32 回填：替换为 SysOperation 接口类型
}

// CreateContextOption CreateContext 方法选项函数
type CreateContextOption func(*createContextOptions)

// createContextOptions CreateContext 方法可选项
type createContextOptions struct {
	processors       []ProcessorSpec
	historyMessages  []llm_schema.BaseMessage
	tokenCounter     token.TokenCounter
}

// CompressContextOption CompressContext 方法选项函数
type CompressContextOption func(*compressContextOptions)

// compressContextOptions CompressContext 方法可选项
type compressContextOptions struct {
	processorTypes []string
}

// ClearContextOption ClearContext 方法选项函数
type ClearContextOption func(*clearContextOptions)

// clearContextOptions ClearContext 方法可选项
type clearContextOptions struct {
	sessionID string
	contextID string
}
```

- [ ] **Step 4: 在导出函数区块添加 With* 函数**

```go
// WithWorkspace 设置工作空间
func WithWorkspace(w any) ContextEngineOption {
	return func(o *contextEngineOptions) { o.workspace = w }
}

// WithSysOperation 设置系统操作接口
func WithSysOperation(op any) ContextEngineOption {
	return func(o *contextEngineOptions) { o.sysOperation = op }
}

// WithProcessors 设置处理器规格列表
func WithProcessors(specs []ProcessorSpec) CreateContextOption {
	return func(o *createContextOptions) { o.processors = specs }
}

// WithHistoryMessages 设置历史消息
func WithHistoryMessages(msgs []llm_schema.BaseMessage) CreateContextOption {
	return func(o *createContextOptions) { o.historyMessages = msgs }
}

// WithTokenCounter 设置 Token 计数器
func WithTokenCounter(tc token.TokenCounter) CreateContextOption {
	return func(o *createContextOptions) { o.tokenCounter = tc }
}

// WithProcessorTypes 设置压缩处理器类型过滤列表
func WithProcessorTypes(types []string) CompressContextOption {
	return func(o *compressContextOptions) { o.processorTypes = types }
}

// WithSessionID 设置会话 ID（用于 ClearContext）
func WithSessionID(sid string) ClearContextOption {
	return func(o *clearContextOptions) { o.sessionID = sid }
}

// WithContextID 设置上下文 ID（用于 ClearContext，需配合 WithSessionID）
func WithContextID(cid string) ClearContextOption {
	return func(o *clearContextOptions) { o.contextID = cid }
}
```

同时添加 newXxxOptions 辅助函数（非导出函数区块）：

```go
func newContextEngineOptions(opts ...ContextEngineOption) *contextEngineOptions {
	o := &contextEngineOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func newCreateContextOptions(opts ...CreateContextOption) *createContextOptions {
	o := &createContextOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func newCompressContextOptions(opts ...CompressContextOption) *compressContextOptions {
	o := &compressContextOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

func newClearContextOptions(opts ...ClearContextOption) *clearContextOptions {
	o := &clearContextOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}
```

- [ ] **Step 5: 运行编译检查**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/...`

预期：编译报错，因为 ContextEngine 接口方法签名变更导致 `ability_manager.go` 等消费者不匹配。记录报错文件列表，后续 Task 修复。

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/context_engine/interface/types.go
git commit -m "feat(context_engine): 修改 ContextEngine/ModelContext 接口，补充 Option 类型和 ProcessorSpec"
```

---

### Task 2: 实现 contextEngine 结构体和核心方法

**Files:**
- 创建: `internal/agentcore/context_engine/engine.go`
- 创建: `internal/agentcore/context_engine/engine_test.go`

- [ ] **Step 1: 创建 engine.go，实现 contextEngine 结构体和 NewContextEngine**

```go
package context_engine

import (
	"context"
	"strings"
	"sync"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// contextEngine 上下文引擎门面，管理上下文池、处理器创建和会话状态持久化。
//
// 对应 Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type contextEngine struct {
	// config 全局引擎配置
	config schema.ContextEngineConfig
	// workspace 工作空间，Option 注入
	// ⤵️ 9.32 回填：替换 any 为 Workspace 接口类型
	workspace any
	// sysOperation 系统操作接口，Option 注入
	// ⤵️ 9.32 回填：替换 any 为 SysOperation 接口类型
	sysOperation any
	// contextPool 上下文池，key 为 "sessionID_contextID"
	contextPool map[string]iface.ModelContext
	// mu 保护 contextPool 的读写锁
	mu sync.RWMutex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// compressResultBusy 压缩结果：被动压缩正在进行中
	compressResultBusy = "busy"
	// compressResultCompressed 压缩结果：主动压缩成功且改变了上下文
	compressResultCompressed = "compressed"
	// compressResultNoop 压缩结果：主动压缩未产生变化
	compressResultNoop = "noop"
	// defaultContextID 默认上下文 ID
	defaultContextID = "default_context_id"
	// defaultSessionID 默认会话 ID
	defaultSessionID = "default_session_id"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEngine 创建上下文引擎实例。
//
// 必选参数：config（全局配置）。
// 可选参数通过 ContextEngineOption 传入：
//   - WithWorkspace(w) 设置工作空间
//   - WithSysOperation(op) 设置系统操作接口
//
// 对应 Python: ContextEngine(config, workspace=, sys_operation=)
func NewContextEngine(config schema.ContextEngineConfig, opts ...iface.ContextEngineOption) iface.ContextEngine {
	opt := iface.NewContextEngineOptions(opts...)
	return &contextEngine{
		config:       config,
		workspace:    opt.Workspace,
		sysOperation: opt.SysOperation,
		contextPool:  make(map[string]iface.ModelContext),
	}
}
```

**注意**：`iface.NewContextEngineOptions` 需要在 `types.go` 中对应 `newContextEngineOptions` 改为导出。由于 Option 类型和辅助函数在 `interface` 包中定义，需要改为导出名。

- [ ] **Step 2: 实现 CreateContext 方法**

```go
// CreateContext 创建或获取上下文。
//
// 若 fullContextID 已在池中，返回已有实例并刷新会话引用和状态。
// 否则创建新实例（⤵️ 5.31 回填 SessionModelContext 构造），存入池。
//
// 对应 Python: ContextEngine.create_context()
func (ce *contextEngine) CreateContext(ctx context.Context, contextID string, sess *session.Session, opts ...iface.CreateContextOption) (iface.ModelContext, error) {
	contextID = processContextID(contextID)
	sessionID := defaultSessionID
	if sess != nil {
		sessionID = sess.GetSessionID()
	}
	fullContextID := sessionID + "_" + contextID

	ce.mu.RLock()
	if mc, ok := ce.contextPool[fullContextID]; ok {
		ce.mu.RUnlock()
		mc.SetSessionRef(sess)
		opt := iface.NewCreateContextOptions(opts...)
		loadStateFromSession(mc, sess, opt.HistoryMessages)
		return mc, nil
	}
	ce.mu.RUnlock()

	opt := iface.NewCreateContextOptions(opts...)

	// 通过工厂创建处理器实例
	processorInstances := make([]iface.ContextProcessor, 0, len(opt.Processors))
	for _, spec := range opt.Processors {
		p, err := ce.createProcessor(spec.Type, spec.Config)
		if err != nil {
			return nil, err
		}
		processorInstances = append(processorInstances, p)
	}

	// TokenCounter：Option 未提供则默认 TiktokenCounter
	tokenCounter := opt.TokenCounter
	if tokenCounter == nil {
		tokenCounter = token.NewTiktokenCounter()
	}

	// ⤵️ 5.31 回填：构造 SessionModelContext 实例
	// 当前返回占位 ModelContext（nil），5.31 实现 SessionModelContext 后回填
	_ = processorInstances
	_ = tokenCounter
	_ = historyMessages

	// TODO: 5.31 回填 — 替换为：
	// mc := NewSessionModelContext(contextID, sessionID, ce.config,
	//     historyMessages, processorInstances, tokenCounter, sess,
	//     ce.workspace, ce.sysOperation)
	// loadStateFromSession(mc, sess, opt.HistoryMessages)
	// ce.mu.Lock()
	// ce.contextPool[fullContextID] = mc
	// ce.mu.Unlock()
	// return mc, nil

	return nil, exception.NewError(
		exception.StatusContextExecutionError,
		"ContextEngine.CreateContext: SessionModelContext 尚未实现（⤵️ 5.31 回填）",
	)
}
```

- [ ] **Step 3: 实现 GetContext 方法**

```go
// GetContext 获取上下文（不存在返回 nil）。
//
// 对应 Python: ContextEngine.get_context()
func (ce *contextEngine) GetContext(contextID string, sessionID string) iface.ModelContext {
	contextID = processContextID(contextID)
	fullContextID := sessionID + "_" + contextID
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return ce.contextPool[fullContextID]
}
```

- [ ] **Step 4: 实现 CompressContext 方法**

```go
// CompressContext 主动压缩上下文。
//
// 返回值："busy"（被动压缩进行中）、"compressed"（压缩成功）、"noop"（无变化）。
//
// 对应 Python: ContextEngine.compress_context()
func (ce *contextEngine) CompressContext(ctx context.Context, contextID string, sess *session.Session, opts ...iface.CompressContextOption) (string, error) {
	opt := iface.NewCompressContextOptions(opts...)

	sessionID := defaultSessionID
	if sess != nil {
		sessionID = sess.GetSessionID()
	}

	contextID = processContextID(contextID)
	mc := ce.GetContext(contextID, sessionID)
	if mc == nil {
		return "", exception.NewError(
			exception.StatusContextExecutionError,
			"cannot find context '"+contextID+"' in session '"+sessionID+"'",
		)
	}

	return mc.CompressContext(ctx, opts...)
}
```

- [ ] **Step 5: 实现 ClearContext 方法**

```go
// ClearContext 清空上下文。
//
// 三种粒度（对齐 Python）：
//   - 不提供 Option → 清空所有上下文
//   - 仅 WithSessionID → 清除该 session 下所有上下文
//   - WithSessionID + WithContextID → 清除指定上下文
//
// 对应 Python: ContextEngine.clear_context()
func (ce *contextEngine) ClearContext(_ context.Context, opts ...iface.ClearContextOption) error {
	opt := iface.NewClearContextOptions(opts...)

	if opt.SessionID == "" {
		// 清空所有上下文
		ce.mu.Lock()
		clearedCount := len(ce.contextPool)
		ce.contextPool = make(map[string]iface.ModelContext)
		ce.mu.Unlock()
		_ = clearedCount
		// ⤵️ 6.4-6.10 回填：触发 ContextCleared 事件
		return nil
	}

	if opt.ContextID == "" {
		// 按 sessionID 清除
		ce.mu.Lock()
		var deleteContextIDs []string
		for _, mc := range ce.contextPool {
			if mc.SessionID() == opt.SessionID {
				deleteContextIDs = append(deleteContextIDs, mc.ContextID())
			}
		}
		if len(deleteContextIDs) == 0 {
			ce.mu.Unlock()
			logger.Warn(logComponent).
				Str("session_id", opt.SessionID).
				Msg("Delete context failed, session does not exist")
			return nil
		}
		for _, cid := range deleteContextIDs {
			fullContextID := opt.SessionID + "_" + processContextID(cid)
			delete(ce.contextPool, fullContextID)
		}
		clearedCount := len(deleteContextIDs)
		ce.mu.Unlock()
		_ = clearedCount
		// ⤵️ 6.4-6.10 回填：触发 ContextCleared 事件
		return nil
	}

	// 按 sessionID + contextID 精确删除
	contextID := processContextID(opt.ContextID)
	fullContextID := opt.SessionID + "_" + contextID
	ce.mu.Lock()
	if _, ok := ce.contextPool[fullContextID]; !ok {
		ce.mu.Unlock()
		logger.Warn(logComponent).
			Str("session_id", opt.SessionID).
			Msg("Delete context failed, context does not exist")
		return nil
	}
	delete(ce.contextPool, fullContextID)
	ce.mu.Unlock()
	// ⤵️ 6.4-6.10 回填：触发 ContextCleared 事件
	return nil
}
```

- [ ] **Step 6: 实现 SaveContexts 方法**

```go
// SaveContexts 批量持久化上下文状态。
//
// 遍历目标上下文，调用 mc.SaveState() 收集状态，
// 通过 saveStateToSession 写入 Session。
//
// 对应 Python: ContextEngine.save_contexts()
func (ce *contextEngine) SaveContexts(_ context.Context, sess *session.Session, contextIDs []string) (map[string]any, error) {
	if sess == nil {
		logger.Warn(logComponent).
			Msg("Save context failed, session cannot be nil")
		return nil, nil
	}

	sessionID := sess.GetSessionID()
	states := make(map[string]any)

	targetIDs := contextIDs
	if targetIDs == nil {
		ce.mu.RLock()
		for _, mc := range ce.contextPool {
			if mc.SessionID() == sessionID {
				targetIDs = append(targetIDs, mc.ContextID())
			}
		}
		ce.mu.RUnlock()
	}

	for _, cid := range targetIDs {
		cid = processContextID(cid)
		fullContextID := sessionID + "_" + cid
		ce.mu.RLock()
		mc, ok := ce.contextPool[fullContextID]
		ce.mu.RUnlock()
		if !ok {
			continue
		}
		states[cid] = mc.SaveState()
	}

	saveStateToSession(sess, states)
	// ⤵️ 6.4-6.10 回填：触发 ContextOffloaded 事件
	return states, nil
}
```

- [ ] **Step 7: 实现辅助方法**

```go
// ──────────────────────────── 非导出函数 ────────────────────────────

// createProcessor 通过工厂创建处理器实例。
//
// 对应 Python: ContextEngine._create_processor()
func (ce *contextEngine) createProcessor(processorType string, config iface.ProcessorConfig) (iface.ContextProcessor, error) {
	factory, ok := GetProcessorFactory(processorType)
	if !ok {
		return nil, exception.NewError(
			exception.StatusContextExecutionError,
			"cannot find processor type '"+processorType+"'",
		)
	}
	p, err := factory(config)
	if err != nil {
		return nil, exception.NewError(
			exception.StatusContextExecutionError,
			"init processor type '"+processorType+"' failed",
		).WithCause(err)
	}
	return p, nil
}

// processContextID 处理上下文 ID，将点号替换为下划线。
//
// 对应 Python: ContextEngine._process_context_id()
func processContextID(contextID string) string {
	return strings.ReplaceAll(contextID, ".", "_")
}

// loadStateFromSession 从 Session 中加载上下文状态到 ModelContext。
//
// 对应 Python: ContextEngine._load_state_from_session()
func loadStateFromSession(mc iface.ModelContext, sess *session.Session, historyMessages []llm_schema.BaseMessage) {
	if sess == nil {
		return
	}

	states, err := sess.GetState(state.StringKey("context"))
	if err != nil || states == nil {
		return
	}

	stateMap, ok := states.(map[string]any)
	if !ok {
		return
	}

	if historyMessages != nil {
		contextID := mc.ContextID()
		if ctxState, ok := stateMap[contextID].(map[string]any); ok {
			ctxState["messages"] = historyMessages
		}
	}

	mc.LoadState(stateMap)
}

// saveStateToSession 将上下文状态写入 Session。
//
// 对应 Python: ContextEngine._save_state_to_session()
func saveStateToSession(sess *session.Session, states map[string]any) {
	if sess == nil {
		return
	}
	// 先清空再写入，对齐 Python: session.update_state({"context": None}) 然后 session.update_state({"context": states})
	sess.UpdateState(map[string]any{"context": nil})
	sess.UpdateState(map[string]any{"context": states})
}
```

- [ ] **Step 8: 运行编译检查，修复类型引用问题**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/context_engine/...`

可能需要调整 `iface.NewContextEngineOptions` → `iface.NewContextEngineOptions` 等导出名问题（Step 1 中定义的是小写 `newContextEngineOptions`，需要改为大写导出）。

修复 `types.go` 中的辅助函数名：
- `newContextEngineOptions` → `NewContextEngineOptions`
- `newCreateContextOptions` → `NewCreateContextOptions`
- `newCompressContextOptions` → `NewCompressContextOptions`
- `newClearContextOptions` → `NewClearContextOptions`

同时 engine.go 需要引入 `state` 包：
```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
```

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/context_engine/engine.go
git commit -m "feat(context_engine): 实现 contextEngine 结构体和核心方法"
```

---

### Task 3: 适配消费者

**Files:**
- 修改: `internal/agentcore/single_agent/ability_manager.go`
- 其他编译报错的消费者文件

- [ ] **Step 1: 编译全量检查**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

找出所有因 ContextEngine/ModelContext 接口变更导致的编译错误。

- [ ] **Step 2: 修复所有编译错误**

主要影响点：
1. `ability_manager.go` 中 `contextEngine` 字段类型不变（`iface.ContextEngine`），但如果有直接调用旧接口方法的地方需要适配
2. 任何 mock 实现了旧 `ContextEngine` 或 `ModelContext` 接口的测试文件，需要补充新方法

对于 `ModelContext` 新增方法，在 mock 中添加桩实现：

```go
func (m *mockModelContext) WorkspaceDir() string                   { return "" }
func (m *mockModelContext) SetSessionRef(_ *session.Session)       {}
func (m *mockModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any              { return nil }
func (m *mockModelContext) LoadState(_ map[string]any)             {}
func (m *mockModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
    return "", nil
}
```

- [ ] **Step 3: 全量编译通过**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

预期：0 errors

- [ ] **Step 4: 提交**

```bash
git add -A
git commit -m "fix(context_engine): 适配 ContextEngine/ModelContext 接口变更的消费者"
```

---

### Task 4: 单元测试

**Files:**
- 创建: `internal/agentcore/context_engine/engine_test.go`

- [ ] **Step 1: 编写 NewContextEngine 测试**

```go
package context_engine

import (
	"testing"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
)

func TestNewContextEngine_默认配置(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	if ce == nil {
		t.Fatal("期望返回非 nil ContextEngine")
	}
}

func TestNewContextEngine_WithOptions(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config,
		iface.WithWorkspace("test_workspace"),
		iface.WithSysOperation("test_sys_op"),
	)
	if ce == nil {
		t.Fatal("期望返回非 nil ContextEngine")
	}
	// 验证内部字段（通过行为间接验证）
}
```

- [ ] **Step 2: 编写 GetContext 测试**

```go
func TestContextEngine_GetContext_不存在(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	mc := ce.GetContext("nonexistent", "session1")
	if mc != nil {
		t.Fatal("期望返回 nil")
	}
}
```

- [ ] **Step 3: 编写 processContextID 测试**

```go
func TestProcessContextID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"simple", "simple"},
		{"with.dot", "with_dot"},
		{"a.b.c", "a_b_c"},
		{"nochange", "nochange"},
		{"", ""},
	}
	for _, tt := range tests {
		result := processContextID(tt.input)
		if result != tt.expected {
			t.Errorf("processContextID(%q) = %q, 期望 %q", tt.input, result, tt.expected)
		}
	}
}
```

- [ ] **Step 4: 编写 ClearContext 测试**

```go
func TestContextEngine_ClearContext_三种粒度(t *testing.T) {
	// 测试需要先注册 mock ModelContext 到池中
	// 由于 CreateContext 依赖 5.31 SessionModelContext，
	// 此处直接操作 contextEngine 内部池来测试

	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config).(*contextEngine)

	// 手动添加 mock 上下文到池
	mc1 := &mockModelContext{sessionID: "s1", contextID: "c1"}
	mc2 := &mockModelContext{sessionID: "s1", contextID: "c2"}
	mc3 := &mockModelContext{sessionID: "s2", contextID: "c1"}
	ce.contextPool["s1_c1"] = mc1
	ce.contextPool["s1_c2"] = mc2
	ce.contextPool["s2_c1"] = mc3

	// 测试1：精确删除
	ce.ClearContext(context.Background(),
		iface.WithSessionID("s1"),
		iface.WithContextID("c1"),
	)
	if ce.GetContext("c1", "s1") != nil {
		t.Fatal("精确删除后 s1_c1 应为 nil")
	}
	if ce.GetContext("c2", "s1") == nil {
		t.Fatal("精确删除不应影响 s1_c2")
	}
	if ce.GetContext("c1", "s2") == nil {
		t.Fatal("精确删除不应影响 s2_c1")
	}

	// 测试2：按 session 删除
	ce.ClearContext(context.Background(),
		iface.WithSessionID("s1"),
	)
	if ce.GetContext("c2", "s1") != nil {
		t.Fatal("按 session 删除后 s1_c2 应为 nil")
	}
	if ce.GetContext("c1", "s2") == nil {
		t.Fatal("按 session 删除不应影响 s2_c1")
	}

	// 测试3：清空所有
	ce.contextPool["s2_c1"] = mc3
	ce.ClearContext(context.Background())
	if ce.GetContext("c1", "s2") != nil {
		t.Fatal("清空所有后 s2_c1 应为 nil")
	}
}
```

- [ ] **Step 5: 编写 CompressContext 测试**

```go
func TestContextEngine_CompressContext_上下文不存在(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	_, err := ce.CompressContext(context.Background(), "nonexistent", nil)
	if err == nil {
		t.Fatal("期望返回错误")
	}
}
```

- [ ] **Step 6: 编写 SaveContexts 测试**

```go
func TestContextEngine_SaveContexts_session为nil(t *testing.T) {
	config := schema.NewContextEngineConfig()
	ce := NewContextEngine(config)
	states, err := ce.SaveContexts(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}
	if states != nil {
		t.Fatal("session 为 nil 时期望返回 nil states")
	}
}
```

- [ ] **Step 7: 编写 mockModelContext（满足新 ModelContext 接口）**

```go
type mockModelContext struct {
	sessionID  string
	contextID  string
	messages   []llm_schema.BaseMessage
	sessionRef *session.Session
}

func (m *mockModelContext) Len() int                                         { return len(m.messages) }
func (m *mockModelContext) GetMessages(_ *int, _ bool) []llm_schema.BaseMessage { return m.messages }
func (m *mockModelContext) SetMessages(msgs []llm_schema.BaseMessage, _ bool)   { m.messages = msgs }
func (m *mockModelContext) PopMessages(_ int, _ bool) []llm_schema.BaseMessage  { return nil }
func (m *mockModelContext) ClearMessages(_ context.Context, _ bool) error       { m.messages = nil; return nil }
func (m *mockModelContext) AddMessages(_ context.Context, _ any) ([]llm_schema.BaseMessage, error) {
	return m.messages, nil
}
func (m *mockModelContext) GetContextWindow(_ context.Context, _ []llm_schema.BaseMessage, _ []*schema.ToolInfo, _ *int, _ *int) (*iface.ContextWindow, error) {
	return iface.NewContextWindow(), nil
}
func (m *mockModelContext) Statistic() *iface.ContextStats  { return &iface.ContextStats{} }
func (m *mockModelContext) SessionID() string                { return m.sessionID }
func (m *mockModelContext) ContextID() string                { return m.contextID }
func (m *mockModelContext) TokenCounter() token.TokenCounter { return nil }
func (m *mockModelContext) ReloaderTool() tool.Tool          { return nil }
func (m *mockModelContext) WorkspaceDir() string             { return "" }
func (m *mockModelContext) SetSessionRef(_ *session.Session) { m.sessionRef = nil }
func (m *mockModelContext) OffloadMessages(_ string, _ []llm_schema.BaseMessage) {}
func (m *mockModelContext) SaveState() map[string]any        { return nil }
func (m *mockModelContext) LoadState(_ map[string]any)       {}
func (m *mockModelContext) CompressContext(_ context.Context, _ ...iface.CompressContextOption) (string, error) {
	return compressResultNoop, nil
}
```

- [ ] **Step 8: 运行测试**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/context_engine/...`

预期：测试通过，覆盖率 ≥ 85%

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/context_engine/engine_test.go
git commit -m "test(context_engine): 添加 contextEngine 门面单元测试"
```

---

### Task 5: 更新 doc.go

**Files:**
- 修改: `internal/agentcore/context_engine/doc.go`
- 修改: `internal/agentcore/context_engine/interface/doc.go`

- [ ] **Step 1: 更新根 doc.go 文件目录**

在 `context_engine/doc.go` 的文件目录树中添加 `engine.go`：

``//	├── engine.go         # ContextEngine 门面实现``

- [ ] **Step 2: 更新 interface/doc.go**

在 `interface/doc.go` 中更新 `types.go` 的描述，反映新增的 Option 类型：

``//	│   ├── types.go            # ModelContext/ContextEngine 接口 + ContextWindow/ContextStats + Option 类型 + ProcessorSpec``

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/context_engine/doc.go internal/agentcore/context_engine/interface/doc.go
git commit -m "docs(context_engine): 更新 doc.go 文件目录树"
```

---

### Task 6: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- 修改: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 5.30 状态**

将 `5.30` 行的状态从 `☐` 改为 `✅`，产出列更新为具体实现内容。

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 5.30 ContextEngine 门面实现状态"
```

---

### Task 7: 最终编译和测试验证

**Files:**
- 无新增

- [ ] **Step 1: 全量编译**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

预期：0 errors

- [ ] **Step 2: 运行覆盖率检查**

运行: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/context_engine/...`

预期：整体覆盖率 ≥ 85%

- [ ] **Step 3: 修复任何遗留问题**

如果有测试失败或覆盖率不达标，补充测试用例。

- [ ] **Step 4: 最终提交**

```bash
git add -A
git commit -m "chore(context_engine): 最终编译和测试验证"
```
