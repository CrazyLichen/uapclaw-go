package context_engine

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/token"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// contextEngine 上下文引擎门面，管理上下文池、处理器创建和会话状态持久化。
// Python: openjiuwen/core/context_engine/context_engine.py (ContextEngine)
type contextEngine struct {
	// config 全局引擎配置
	config schema.ContextEngineConfig
	// workspace 工作空间，Option 注入
	workspace *hworkspace.Workspace
	// sysOperation 系统操作接口，Option 注入
	sysOperation sysop.SysOperation
	// contextPool 上下文池，key 为 "sessionID_contextID"
	contextPool map[string]iface.ModelContext
	// mu 保护 contextPool 的读写锁
	mu sync.RWMutex
	// pendingConfig 待落地的引擎配置（由 rail 热重载推送）
	pendingConfig schema.ContextEngineConfig
	// pendingSpecs 待落地的处理器规格
	pendingSpecs []iface.ProcessorSpec
	// pendingFingerprint 待落地配置的内容指纹
	pendingFingerprint string
	// appliedFingerprint 已应用到上下文池的配置指纹
	appliedFingerprint string
}

// runtimeConfigApplier 可选能力：接收热重载后的引擎配置与处理器实例。
// 只有 SessionModelContext 实现；测试用 mock 可不实现（此时跳过刷新）。
type runtimeConfigApplier interface {
	// ApplyRuntimeConfig 刷新上下文级配置并替换处理器实例
	ApplyRuntimeConfig(config schema.ContextEngineConfig, processors []iface.ContextProcessor)
}

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore
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

// ──────────────────────────── 导出函数 ────────────────────────────

// NewContextEngine 创建上下文引擎实例。
// 必选参数：config（全局配置）。
// 可选参数通过 ContextEngineOption 传入：
//   - WithWorkspace(w) 设置工作空间
//   - WithEngineSysOperation(op) 设置系统操作接口
//
// Python: ContextEngine(config, workspace=, sys_operation=)
func NewContextEngine(config schema.ContextEngineConfig, opts ...iface.ContextEngineOption) iface.ContextEngine {
	opt := iface.NewContextEngineOptions(opts...)
	return &contextEngine{
		config:       config,
		workspace:    opt.Workspace,
		sysOperation: opt.SysOperation,
		contextPool:  make(map[string]iface.ModelContext),
	}
}

// ApplyRuntimeConfig 推送热重载后的引擎配置与处理器规格。
// 只暂存期望值，真正落地发生在下一次 CreateContext（用户轮次入口），
// 因此对正在执行的轮次零影响。同一份配置重复推送按内容指纹去重，不会重建。
// 与 Python 的差异：Python 命中上下文池时忽略 processors，老会话不跟随；
// 本实现会把待生效配置在下次建上下文时应用到池内所有上下文（Go 侧增强）。
func (ce *contextEngine) ApplyRuntimeConfig(config schema.ContextEngineConfig, specs []iface.ProcessorSpec) {
	fingerprint := runtimeConfigFingerprint(config, specs)

	ce.mu.Lock()
	ce.pendingConfig = config
	ce.pendingSpecs = specs
	ce.pendingFingerprint = fingerprint
	ce.mu.Unlock()
}

// PendingFingerprint 返回已推送但尚未落地的配置指纹（诊断与测试用）。
func (ce *contextEngine) PendingFingerprint() string {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return ce.pendingFingerprint
}

// AppliedFingerprint 返回已应用到上下文池的配置指纹（诊断与测试用）。
func (ce *contextEngine) AppliedFingerprint() string {
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return ce.appliedFingerprint
}

// CreateContext 创建或获取上下文。
// 若 fullContextID 已在池中，返回已有实例并刷新会话引用和状态。
// 否则创建新实例（⤵️ 5.31 回填 SessionModelContext 构造），存入池。
// Python: ContextEngine.create_context()
func (ce *contextEngine) CreateContext(ctx context.Context, contextID string, sess sessioninterfaces.SessionFacade, opts ...iface.CreateContextOption) (iface.ModelContext, error) {
	if contextID == "" {
		contextID = defaultContextID
	}
	contextID = processContextID(contextID)
	sessionID := defaultSessionID
	if sess != nil {
		sessionID = sess.GetSessionID()
	}
	fullContextID := sessionID + "_" + contextID

	// 全程使用写锁，消除 check-then-act 竞态条件
	// Python 因 asyncio 单线程不存在此问题，Go 多 goroutine 需要全程写锁保证原子性
	ce.mu.Lock()
	// 热重载的配置在此落地（轮次入口），保证池内老上下文与新上下文口径一致
	ce.applyPendingLocked()
	if mc, ok := ce.contextPool[fullContextID]; ok {
		ce.mu.Unlock()
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

	opt := iface.NewCreateContextOptions(opts...)

	// 通过工厂创建处理器实例
	processorInstances := make([]iface.ContextProcessor, 0, len(opt.Processors))
	for _, spec := range opt.Processors {
		p, err := ce.createProcessor(spec.Type, spec.Config)
		if err != nil {
			ce.mu.Unlock()
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

	// 加载已有状态（对齐 Python：先 _load_state_from_session，再存入 pool）
	loadStateFromSession(mc, sess, opt.HistoryMessages)

	// 存入池
	ce.contextPool[fullContextID] = mc
	ce.mu.Unlock()

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
// Python: ContextEngine.get_context()
func (ce *contextEngine) GetContext(contextID string, sessionID string) iface.ModelContext {
	contextID = processContextID(contextID)
	fullContextID := sessionID + "_" + contextID
	ce.mu.RLock()
	defer ce.mu.RUnlock()
	return ce.contextPool[fullContextID]
}

// CompressContext 主动压缩上下文。
// 返回值："busy"（被动压缩进行中）、"compressed"（压缩成功）、"noop"（无变化）。
// Python: ContextEngine.compress_context()
// Python 在调用 context.compress_context() 时透传 self._sys_operation 和 **kwargs，
// Go 通过 CompressContextOption 传入 SysOperation 和 ModelName 等可选参数。
func (ce *contextEngine) CompressContext(ctx context.Context, contextID string, sess sessioninterfaces.SessionFacade, opts ...iface.CompressContextOption) (*iface.CompressContextResult, error) {
	// 三层 fallback：sess → SessionID 选项 → defaultSessionID，对齐 Python
	sessionID := defaultSessionID
	opt := iface.NewCompressContextOptions(opts...)
	if sess != nil {
		sessionID = sess.GetSessionID()
	} else if opt.SessionID != "" {
		sessionID = opt.SessionID
	}

	contextID = processContextID(contextID)
	mc := ce.GetContext(contextID, sessionID)
	if mc == nil {
		return nil, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("在会话 '"+sessionID+"' 中找不到上下文 '"+contextID+"'"),
		)
	}

	// Python: context.compress_context(processor_types=, sys_operation=self._sys_operation, **kwargs)
	// 若调用方未通过 WithCompressSysOperation 指定，自动注入 ce.sysOperation
	compressOpts := make([]iface.CompressContextOption, 0, len(opts)+1)
	if opt.SysOperation == nil && ce.sysOperation != nil {
		compressOpts = append(compressOpts, iface.WithCompressSysOperation(ce.sysOperation))
	}
	compressOpts = append(compressOpts, opts...)

	return mc.CompressContext(ctx, compressOpts...)
}

// ClearContext 清空上下文。
// 三种粒度（对齐 Python）：
//   - 不提供 Option → 清空所有上下文
//   - 仅 WithSessionID → 清除该 session 下所有上下文
//   - WithSessionID + WithContextID → 清除指定上下文
//
// Python: ContextEngine.clear_context()
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
				Msg("failed to delete context, session not found")
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
			Msg("failed to delete context, context not found")
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
// 遍历目标上下文，调用 mc.SaveState() 收集状态，
// 通过 saveStateToSession 写入 Session。
// Python: ContextEngine.save_contexts()
func (ce *contextEngine) SaveContexts(ctx context.Context, sess sessioninterfaces.SessionFacade, contextIDs []string) (map[string]any, error) {
	if sess == nil {
		logger.Warn(logComponent).
			Msg("failed to save context, session cannot be nil")
		return nil, nil
	}

	sessionID := sess.GetSessionID()
	states := make(map[string]any)

	targetIDs := contextIDs
	if targetIDs == nil {
		// 注意：收集 ID 与后续逐个保存之间释放了锁，contextPool 可能被并发修改，
		// 导致部分 contextID 在保存时已不存在。这是已知的弱一致性语义，静默跳过即可。
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

// UpdateModelContext 把选中模型元数据推给引擎配置与池内全部上下文（不重建上下文）。
// 对齐 Python ContextEngine.update_model_context()：全局 context_window_tokens 不动
// （保持最高优先），只刷新 model_name 与模型级窗口 override；已建上下文同步生效，
// 避免换模型后仍沿用旧窗口。
func (ce *contextEngine) UpdateModelContext(modelName string, contextWindowTokens int) {
	ce.mu.Lock()
	ce.config.ModelName = modelName
	ce.config.ModelContextWindowTokensOverride = 0
	if contextWindowTokens > 0 {
		ce.config.ModelContextWindowTokensOverride = contextWindowTokens
	}
	targets := make([]iface.ModelContext, 0, len(ce.contextPool))
	for _, mc := range ce.contextPool {
		targets = append(targets, mc)
	}
	ce.mu.Unlock()

	for _, mc := range targets {
		if updater, ok := mc.(interface {
			UpdateModelContext(string, int)
		}); ok {
			updater.UpdateModelContext(modelName, contextWindowTokens)
		}
	}
}

// RecoverFromModelContextOverflow 模型报上下文溢出后压缩一次，返回是否值得重试。
// 只有压缩真的发生（结果标识为 compressed）才允许重试，否则重试必然再次溢出。
// 流式场景已向用户吐字时直接拒绝：重试会把已发送前缀重复输出。
// Python: ContextEngine.recover_from_model_context_overflow()
func (ce *contextEngine) RecoverFromModelContextOverflow(
	ctx context.Context,
	sess sessioninterfaces.SessionFacade,
	contextID string,
	streaming bool,
	streamChunksEmitted int,
) bool {
	if streaming && streamChunksEmitted > 0 {
		logger.Warn(logComponent).
			Str("event_type", "context_recovery_skipped").
			Int("stream_chunks_emitted", streamChunksEmitted).
			Msg("skip model context recovery: streaming output already emitted")
		return false
	}
	result, err := ce.CompressContext(ctx, contextID, sess)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("event_type", "context_recovery_failed").
			Str("context_id", contextID).
			Msg("compress context for overflow recovery failed")
		return false
	}
	if result == nil {
		return false
	}
	logger.Info(logComponent).
		Str("event_type", "context_recovery_compressed").
		Str("context_id", contextID).
		Str("result", result.Result).
		Msg("compressed context after model overflow")
	return result.Result == "compressed"
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// applyPendingLocked 把暂存的热重载配置落地到引擎与池内全部上下文。
// 调用方必须持有 ce.mu 写锁。任一处理器创建失败时整批放弃：不替换任何上下文、
// 不推进 appliedFingerprint，下一次 CreateContext 会自动重试（指纹仍不一致），
// 期间老上下文继续使用旧实例，不打断对话。
func (ce *contextEngine) applyPendingLocked() {
	if ce.pendingFingerprint == "" || ce.pendingFingerprint == ce.appliedFingerprint {
		return
	}

	instances := make([]iface.ContextProcessor, 0, len(ce.pendingSpecs))
	for _, spec := range ce.pendingSpecs {
		p, err := ce.createProcessor(spec.Type, spec.Config)
		if err != nil {
			logger.Warn(logComponent).
				Str("event_type", "context_runtime_config_apply_failed").
				Str("processor_type", spec.Type).
				Err(err).
				Msg("failed to rebuild context processors, keep previous instances")
			return
		}
		instances = append(instances, p)
	}

	contextMaxBefore := cecontext.ResolveContextMax(
		ce.config.ModelName, ce.config.ContextWindowTokens, ce.config.ModelContextWindowTokens)

	refreshed := 0
	for _, mc := range ce.contextPool {
		applier, ok := mc.(runtimeConfigApplier)
		if !ok {
			continue
		}
		applier.ApplyRuntimeConfig(ce.pendingConfig, instances)
		refreshed++
	}

	ce.config = ce.pendingConfig
	ce.appliedFingerprint = ce.pendingFingerprint
	contextMaxAfter := cecontext.ResolveContextMax(
		ce.config.ModelName, ce.config.ContextWindowTokens, ce.config.ModelContextWindowTokens)

	logger.Info(logComponent).
		Str("event_type", "context_runtime_config_applied").
		Str("fingerprint", shortFingerprint(ce.appliedFingerprint)).
		Int("processors", len(instances)).
		Int("contexts", refreshed).
		Int("context_max_before", contextMaxBefore).
		Int("context_max_after", contextMaxAfter).
		Str("model_name", ce.config.ModelName).
		Msg("context runtime config applied to existing contexts")
}

// runtimeConfigFingerprint 计算引擎配置与处理器规格的内容指纹。
// encoding/json 对结构体按字段序、对 map 按 key 排序，结果稳定；因此同一份配置
// 每次算出的指纹一致，可以用于"配置没变就不重建"的去重判断。
func runtimeConfigFingerprint(config schema.ContextEngineConfig, specs []iface.ProcessorSpec) string {
	payload := struct {
		Config schema.ContextEngineConfig `json:"config"`
		Specs  []iface.ProcessorSpec      `json:"specs"`
	}{Config: config, Specs: specs}

	data, err := json.Marshal(payload)
	if err != nil {
		// 序列化失败时退化为唯一值：宁可多重建一次，也不能静默不生效
		logger.Warn(logComponent).
			Err(err).
			Str("event_type", "context_runtime_config_fingerprint_failed").
			Msg("failed to compute runtime config fingerprint, force rebuild")
		return fmt.Sprintf("unserializable-%d", time.Now().UnixNano())
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// shortFingerprint 截断指纹用于日志展示。
func shortFingerprint(fingerprint string) string {
	if len(fingerprint) <= 8 {
		return fingerprint
	}
	return fingerprint[:8]
}

// createProcessor 通过工厂创建处理器实例。
// Python: ContextEngine._create_processor()
func (ce *contextEngine) createProcessor(processorType string, config iface.ProcessorConfig) (iface.ContextProcessor, error) {
	factory, ok := GetProcessorFactory(processorType)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("找不到处理器类型 '"+processorType+"'"),
		)
	}
	p, err := factory(config)
	if err != nil {
		return nil, exception.NewBaseError(
			exception.StatusContextExecutionError,
			exception.WithMsg("初始化处理器类型 '"+processorType+"' 失败"),
			exception.WithCause(err),
		)
	}
	return p, nil
}

// processContextID 处理上下文 ID，将点号替换为下划线。
// Python: ContextEngine._process_context_id()
func processContextID(contextID string) string {
	return strings.ReplaceAll(contextID, ".", "_")
}

// loadStateFromSession 从 Session 中加载上下文状态到 ModelContext。
// Python: ContextEngine._load_state_from_session()
func loadStateFromSession(mc iface.ModelContext, sess sessioninterfaces.SessionFacade, historyMessages []llm_schema.BaseMessage) {
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
// Python: ContextEngine._save_state_to_session()
func saveStateToSession(sess sessioninterfaces.SessionFacade, states map[string]any) {
	if sess == nil {
		return
	}
	// 先清空再写入，对齐 Python: session.update_state({"context": None}) 然后 session.update_state({"context": states})
	sess.UpdateState(map[string]any{"context": nil})
	sess.UpdateState(map[string]any{"context": states})
}
