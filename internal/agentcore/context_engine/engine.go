package context_engine

import (
	"context"
	"strings"
	"sync"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
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
//   - WithEngineSysOperation(op) 设置系统操作接口
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

// CreateContext 创建或获取上下文。
//
// 若 fullContextID 已在池中，返回已有实例并刷新会话引用和状态。
// 否则创建新实例（⤵️ 5.31 回填 SessionModelContext 构造），存入池。
//
// 对应 Python: ContextEngine.create_context()
func (ce *contextEngine) CreateContext(ctx context.Context, contextID string, sess *session.Session, opts ...iface.CreateContextOption) (iface.ModelContext, error) {
	if contextID == "" {
		contextID = defaultContextID
	}
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
		// 触发 ContextRetrieved 事件，对齐 Python: @_fw.emit_after(ContextEvents.CONTEXT_RETRIEVED, result_key="context")
		callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
			Event:     callback.ContextRetrieved,
			SessionID: sessionID,
			ContextID: contextID,
			Context:   mc,
		})
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
		tokenCounter = token.NewTiktokenCounter(ce.config.ModelName)
	}

	// 构造 SessionModelContext 实例
	mc := cecontext.NewSessionModelContext(
		contextID,
		sessionID,
		ce.config,
		opt.HistoryMessages,
		processorInstances,
		tokenCounter,
		sess,
		ce.workspace,
		ce.sysOperation,
	)

	// 存入池
	ce.mu.Lock()
	ce.contextPool[fullContextID] = mc
	ce.mu.Unlock()

	// 加载已有状态
	loadStateFromSession(mc, sess, opt.HistoryMessages)

	// 触发 ContextRetrieved 事件（与缓存命中路径一致）
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextRetrieved,
		SessionID: sessionID,
		ContextID: contextID,
		Context:   mc,
	})

	return mc, nil
}

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

// CompressContext 主动压缩上下文。
//
// 返回值："busy"（被动压缩进行中）、"compressed"（压缩成功）、"noop"（无变化）。
//
// 对应 Python: ContextEngine.compress_context()
// Python 在调用 context.compress_context() 时透传 self._sys_operation 和 **kwargs，
// Go 通过 CompressContextOption 传入 SysOperation 和 ModelName 等可选参数。
func (ce *contextEngine) CompressContext(ctx context.Context, contextID string, sess *session.Session, opts ...iface.CompressContextOption) (string, error) {
	sessionID := defaultSessionID
	if sess != nil {
		sessionID = sess.GetSessionID()
	}

	contextID = processContextID(contextID)
	mc := ce.GetContext(contextID, sessionID)
	if mc == nil {
		return "", exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("cannot find context '"+contextID+"' in session '"+sessionID+"'"),
		)
	}

	// 对齐 Python: context.compress_context(processor_types=, sys_operation=self._sys_operation, **kwargs)
	// 若调用方未通过 WithCompressSysOperation 指定，自动注入 ce.sysOperation
	compressOpts := make([]iface.CompressContextOption, 0, len(opts)+1)
	opt := iface.NewCompressContextOptions(opts...)
	if opt.SysOperation == nil && ce.sysOperation != nil {
		compressOpts = append(compressOpts, iface.WithCompressSysOperation(ce.sysOperation))
	}
	compressOpts = append(compressOpts, opts...)

	return mc.CompressContext(ctx, compressOpts...)
}

// ClearContext 清空上下文。
//
// 三种粒度（对齐 Python）：
//   - 不提供 Option → 清空所有上下文
//   - 仅 WithSessionID → 清除该 session 下所有上下文
//   - WithSessionID + WithContextID → 清除指定上下文
//
// 对应 Python: ContextEngine.clear_context()
func (ce *contextEngine) ClearContext(ctx context.Context, opts ...iface.ClearContextOption) error {
	opt := iface.NewClearContextOptions(opts...)

	if opt.SessionID == "" {
		// 清空所有上下文
		ce.mu.Lock()
		clearedCount := len(ce.contextPool)
		ce.contextPool = make(map[string]iface.ModelContext)
		ce.mu.Unlock()
		// 触发 ContextCleared 事件，对齐 Python: await trigger(ContextEvents.CONTEXT_CLEARED, context_id=, session_id=, cleared_count=)
		callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
			Event:     callback.ContextCleared,
			SessionID: "",
			ContextID: "",
			Extra:     map[string]any{"cleared_count": clearedCount},
		})
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
				Msg("删除上下文失败，会话不存在")
			return nil
		}
		for _, cid := range deleteContextIDs {
			fullContextID := opt.SessionID + "_" + processContextID(cid)
			delete(ce.contextPool, fullContextID)
		}
		clearedCount := len(deleteContextIDs)
		ce.mu.Unlock()
		// 触发 ContextCleared 事件
		callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
			Event:     callback.ContextCleared,
			SessionID: opt.SessionID,
			ContextID: "",
			Extra:     map[string]any{"cleared_count": clearedCount},
		})
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
			Msg("删除上下文失败，上下文不存在")
		return nil
	}
	delete(ce.contextPool, fullContextID)
	ce.mu.Unlock()
	// 触发 ContextCleared 事件
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextCleared,
		SessionID: opt.SessionID,
		ContextID: contextID,
	})
	return nil
}

// SaveContexts 批量持久化上下文状态。
//
// 遍历目标上下文，调用 mc.SaveState() 收集状态，
// 通过 saveStateToSession 写入 Session。
//
// 对应 Python: ContextEngine.save_contexts()
func (ce *contextEngine) SaveContexts(ctx context.Context, sess *session.Session, contextIDs []string) (map[string]any, error) {
	if sess == nil {
		logger.Warn(logComponent).
			Msg("保存上下文失败，会话不能为 nil")
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
	// 触发 ContextOffloaded 事件，对齐 Python: @_fw.emit_after(ContextEvents.CONTEXT_OFFLOADED, result_key="result")
	callback.GetCallbackFramework().TriggerContext(ctx, &callback.ContextCallEventData{
		Event:     callback.ContextOffloaded,
		SessionID: sessionID,
		Extra:     map[string]any{"result": states},
	})
	return states, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createProcessor 通过工厂创建处理器实例。
//
// 对应 Python: ContextEngine._create_processor()
func (ce *contextEngine) createProcessor(processorType string, config iface.ProcessorConfig) (iface.ContextProcessor, error) {
	factory, ok := GetProcessorFactory(processorType)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("cannot find processor type '"+processorType+"'"),
		)
	}
	p, err := factory(config)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("init processor type '"+processorType+"' failed"),
			exception.WithCause(err),
		)
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
