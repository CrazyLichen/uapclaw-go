package harness

import (
	"context"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	ceinterface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/controller/config"
	cschema "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/schema"
	ctrlmodules "github.com/uapclaw/uapclaw-go/internal/agentcore/controller/modules"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/task_loop"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/subagent"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interaction"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	sessstate "github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/ability"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/agents"
	saconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/config"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAgent 面向生产的高级 Agent 封装层。
// 包装 ReActAgent + 任务循环 + Rails + 技能 + 子 Agent，
// 实现两层架构：外层（DeepAgent）负责多轮任务编排，
// 内层（ReActAgent）负责单轮 Think-Act-Observe 推理。
// 对齐 Python: DeepAgent (openjiuwen/harness/deep_agent.py)
type DeepAgent struct {
	// card Agent 身份卡片
	card *agentschema.AgentCard
	// abilityManager 能力管理器
	abilityManager agentinterfaces.AbilityManagerInterface
	// callbackManager 回调管理器
	callbackManager *agentinterfaces.AgentCallbackManager

	// reactAgent 内层 ReActAgent
	reactAgent *agents.ReActAgent

	// deepConfig Harness 编排配置
	deepConfig *hschema.DeepAgentConfig
	// systemPromptBuilder 系统提示词构建器（与 ReActAgent 共享同一实例）
	systemPromptBuilder *hprompts.SystemPromptBuilder

	// loopCoordinator 循环协调器
	loopCoordinator *task_loop.LoopCoordinator
	// loopController 任务循环控制器
	loopController *task_loop.TaskLoopController
	// loopSession 活跃的循环会话
	loopSession *session.Session
	// boundSessionID 绑定的会话标识
	boundSessionID string
	// taskCompletionRail 任务完成 Rail
	// ⤵️ 9.11 回填：TaskCompletionRail 具体类型
	taskCompletionRail agentinterfaces.AgentRail

	// pendingRails 待注册 Rail 列表
	pendingRails []agentinterfaces.AgentRail
	// staleRails 已废弃 Rail 列表
	staleRails []agentinterfaces.AgentRail
	// registeredRails 已注册 Rail 列表
	registeredRails []agentinterfaces.AgentRail
	// railsMu Rail 列表互斥锁
	railsMu sync.Mutex

	// initialized 是否已初始化
	initialized bool
	// initMu 初始化互斥锁，保护 ensureInitialized 的并发安全
	initMu sync.Mutex
	// invokeActive 是否有活跃 invoke
	invokeActive atomic.Bool
	// autoInvokeScheduled 是否已调度自动 invoke
	autoInvokeScheduled atomic.Bool

	// streamMu 流控制互斥锁
	streamMu sync.Mutex
	// streamCancel 流取消函数
	streamCancel context.CancelFunc

	// sessionToolkit 会话工具包
	sessionToolkit *subagent.SessionToolkit

	// pendingHarnessConfigs 待加载的 Harness 配置路径
	pendingHarnessConfigs []string

	// configMu 配置读写锁
	configMu sync.RWMutex
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore

	// sessionRuntimeAttr 会话运行时属性键
	// 对齐 Python: _SESSION_RUNTIME_ATTR = "_deep_agent_runtime_state"
	sessionRuntimeAttr = "_deep_agent_runtime_state"

	// sessionStateKey 会话状态持久化键
	// 对齐 Python: _SESSION_STATE_KEY = "deep_agent_state"
	sessionStateKey = "deep_agent_state"

	// defaultAutoInvokeDelay 自动 invoke 延迟秒数
	// 对齐 Python: schedule_auto_invoke_on_spawn_done delay=0.5
	defaultAutoInvokeDelay = 0.5
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// bridgeEvents 桥接到内层 ReActAgent 的事件集合
	// 对齐 Python: _BRIDGE_EVENTS
	bridgeEvents = map[agentinterfaces.AgentCallbackEvent]bool{
		agentinterfaces.CallbackBeforeModelCall:  true,
		agentinterfaces.CallbackAfterModelCall:   true,
		agentinterfaces.CallbackOnModelException: true,
		agentinterfaces.CallbackBeforeToolCall:   true,
		agentinterfaces.CallbackAfterToolCall:    true,
		agentinterfaces.CallbackOnToolException:  true,
	}

	// outerOnlyEvents 仅注册到外层 DeepAgent 的事件集合
	// 对齐 Python: _OUTER_ONLY_EVENTS
	outerOnlyEvents = map[agentinterfaces.AgentCallbackEvent]bool{
		agentinterfaces.CallbackBeforeInvoke: true,
		agentinterfaces.CallbackAfterInvoke:  true,
	}

	// deepEvents DeepAgent 扩展事件集合
	// 对齐 Python: _DEEP_EVENTS
	deepEvents = map[agentinterfaces.AgentCallbackEvent]bool{
		agentinterfaces.CallbackBeforeTaskIteration: true,
		agentinterfaces.CallbackAfterTaskIteration:  true,
	}

	// 编译期接口检查
	_ hinterfaces.DeepAgentInterface = (*DeepAgent)(nil)
	_ agentinterfaces.BaseAgent      = (*DeepAgent)(nil)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewDeepAgent 创建 DeepAgent 实例。
// 对齐 Python: DeepAgent.__init__(card) (line 126)
func NewDeepAgent(card *agentschema.AgentCard) *DeepAgent {
	return &DeepAgent{
		card:            card,
		abilityManager:  ability.NewAbilityManager(nil),
		callbackManager: agentinterfaces.NewAgentCallbackManager(card.ID),
	}
}

// ConfigureDeepConfig 配置 DeepAgent（使用 DeepAgentConfig）。
// 首次调用执行 initialConfigure，后续调用执行 hotReconfigure。
// 对齐 Python: DeepAgent.configure(config) (line 150)
func (d *DeepAgent) ConfigureDeepConfig(ctx context.Context, deepCfg *hschema.DeepAgentConfig) error {
	d.configMu.Lock()
	defer d.configMu.Unlock()

	d.filterDisabledTools(deepCfg)

	if d.deepConfig == nil {
		d.initialConfigure(deepCfg)
	} else {
		d.hotReconfigure(ctx, deepCfg)
	}

	d.initMu.Lock()
	d.initialized = false
	d.initMu.Unlock()
	return nil
}

// Configure 配置 Agent。满足 BaseAgent 接口。
// 注意：DeepAgent 的配置必须通过 ConfigureDeepConfig() 传入 DeepAgentConfig。
// 此方法仅满足接口签名，如果传入非 DeepAgentConfig 将返回错误。
// 对齐 Python: DeepAgent.configure(config) (line 150)
func (d *DeepAgent) Configure(ctx context.Context, config agentinterfaces.AgentConfig) error {
	// DeepAgentConfig 不实现 AgentConfig 接口，
	// 调用方应使用 ConfigureDeepConfig() 代替
	return exception.BuildError(exception.StatusDeepagentConfigParamError,
		exception.WithMsg("DeepAgent 请使用 ConfigureDeepConfig() 传入 *DeepAgentConfig"))
}

// Invoke 非流式执行 Agent。
// 对齐 Python: DeepAgent.invoke(inputs, session) (line 2261)
func (d *DeepAgent) Invoke(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (map[string]any, error) {
	if err := d.ensureInitialized(ctx); err != nil {
		return nil, err
	}

	d.configMu.RLock()
	reactAgent := d.reactAgent
	deepConfig := d.deepConfig
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	invokeInputs, normErr := d.normalizeInputs(inputs)
	if normErr != nil {
		return nil, normErr
	}
	agentOpts := agentinterfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session

	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)

	d.invokeActive.Store(true)
	defer d.invokeActive.Store(false)

	var result map[string]any

	// 对齐 Python: async with ctx.lifecycle(BEFORE_INVOKE, AFTER_INVOKE):
	// 使用 FireLifecycle 包裹核心执行逻辑，保证 AFTER_INVOKE 必定触发
	if err := cbc.FireLifecycle(ctx, agentinterfaces.CallbackBeforeInvoke, agentinterfaces.CallbackAfterInvoke, func() error {
		if deepConfig != nil && deepConfig.EnableTaskLoop && !isResumeInput(invokeInputs) {
			r, err := d.runTaskLoopInvoke(ctx, cbc, sess)
			if err != nil {
				return err
			}
			result = r
		} else {
			r, err := d.runSingleRoundInvoke(ctx, cbc, sess)
			if err != nil {
				return err
			}
			result = r
		}
		invokeInputs.Result = result
		return nil
	}); err != nil {
		return nil, err
	}

	if sess != nil {
		d.saveState(sess, nil)
		d.clearState(sess, false)
	}
	return result, nil
}

// Stream 流式执行 Agent。
// 对齐 Python: DeepAgent.stream(inputs, session, stream_modes) (line 2302)
func (d *DeepAgent) Stream(ctx context.Context, inputs map[string]any, opts ...agentinterfaces.AgentOption) (<-chan stream.Schema, error) {
	if err := d.ensureInitialized(ctx); err != nil {
		return nil, err
	}

	if err := d.drainPendingHarnessConfigs(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("drainPendingHarnessConfigs 失败")
	}

	d.configMu.RLock()
	reactAgent := d.reactAgent
	deepConfig := d.deepConfig
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	invokeInputs, normErr := d.normalizeInputs(inputs)
	if normErr != nil {
		return nil, normErr
	}
	agentOpts := agentinterfaces.NewAgentOptions(opts...)
	sess := agentOpts.Session
	streamModes := agentOpts.StreamModes

	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)

	outCh := make(chan stream.Schema, 64)

	d.invokeActive.Store(true)

	go func() {
		defer d.invokeActive.Store(false)
		defer close(outCh)

		// 对齐 Python: async with ctx.lifecycle(BEFORE_INVOKE, AFTER_INVOKE):
		// 使用 FireLifecycle 包裹 chunk 循环，保证 AFTER_INVOKE 必定触发
		_ = cbc.FireLifecycle(ctx, agentinterfaces.CallbackBeforeInvoke, agentinterfaces.CallbackAfterInvoke, func() error {
			var streamResult map[string]any
			var streamOutputParts []string

			if deepConfig != nil && deepConfig.EnableTaskLoop && !isResumeInput(invokeInputs) {
				ch, err := d.runTaskLoopStream(ctx, invokeInputs, sess, streamModes)
				if err != nil {
					logger.Error(logComponent).Err(err).Msg("runTaskLoopStream 启动失败")
					return err
				}
				for chunk := range ch {
					chunkResult := resultFromStreamChunk(chunk, &streamOutputParts)
					if chunkResult != nil {
						streamResult = chunkResult
					}
					outCh <- chunk
				}
			} else {
				ch, err := d.runSingleRoundStream(ctx, invokeInputs, sess, streamModes)
				if err != nil {
					logger.Error(logComponent).Err(err).Msg("runSingleRoundStream 启动失败")
					return err
				}
				for chunk := range ch {
					chunkResult := resultFromStreamChunk(chunk, &streamOutputParts)
					if chunkResult != nil {
						streamResult = chunkResult
					}
					outCh <- chunk
				}
			}

			// 对齐 Python line 2354-2360: stream_result 合并
			if streamResult == nil && len(streamOutputParts) > 0 {
				streamResult = map[string]any{
					"output":      joinStrings(streamOutputParts),
					"result_type": "answer",
				}
			}
			if streamResult != nil {
				invokeInputs.Result = streamResult
			}
			return nil
		})

		if sess != nil {
			d.saveState(sess, nil)
			d.clearState(sess, false)
		}
	}()

	return outCh, nil
}

// Card 返回 Agent 身份卡片。
// 对齐 Python: BaseAgent.card 属性
func (d *DeepAgent) Card() *agentschema.AgentCard {
	return d.card
}

// Config 返回 nil。与 Python 对齐，DeepAgent.config 返回 None。
// 需要 DeepAgent 配置的消费者应使用 DeepConfig()，
// 需要内层 Agent 配置的消费者应使用 ReactAgent().Config()。
// 对齐 Python: DeepAgent.config = None (line 474)
func (d *DeepAgent) Config() agentinterfaces.AgentConfig {
	return nil
}

// AbilityManager 返回能力管理器。
// 对齐 Python: BaseAgent.ability_manager 属性
func (d *DeepAgent) AbilityManager() agentinterfaces.AbilityManagerInterface {
	return d.abilityManager
}

// SystemPromptBuilder 返回系统提示词构建器。
// 对齐 Python: DeepAgent.system_prompt_builder 属性
func (d *DeepAgent) SystemPromptBuilder() saprompts.SystemPromptBuilderInterface {
	if d.systemPromptBuilder != nil {
		return d.systemPromptBuilder.SystemPromptBuilder
	}
	return nil
}

// CallbackManager 返回回调管理器。
// 对齐 Python: BaseAgent.agent_callback_manager 属性
func (d *DeepAgent) CallbackManager() *agentinterfaces.AgentCallbackManager {
	return d.callbackManager
}

// RegisterCallback 注册回调。
// 对齐 Python: BaseAgent.register_callback(event, callback, priority)
func (d *DeepAgent) RegisterCallback(ctx context.Context, event agentinterfaces.AgentCallbackEvent, fn cb.PerAgentCallbackFunc, opts ...cb.CallbackOption) error {
	d.callbackManager.RegisterCallback(ctx, event, fn, opts...)
	return nil
}

// RegisterRail 注册 Rail。
// 对齐 Python: BaseAgent.register_rail(rail) (line 1187)
func (d *DeepAgent) RegisterRail(ctx context.Context, r agentinterfaces.AgentRail, opts ...cb.CallbackOption) error {
	d.railsMu.Lock()
	// 检查是否为 TaskCompletionRail
	if isTaskCompletionRail(r) {
		d.taskCompletionRail = r
	}
	d.railsMu.Unlock()

	if err := r.Init(d); err != nil {
		return err
	}
	d.registerRailSelective(ctx, r)

	d.railsMu.Lock()
	d.registeredRails = append(d.registeredRails, r)
	d.railsMu.Unlock()
	return nil
}

// UnregisterRail 注销 Rail。
// 对齐 Python: BaseAgent.unregister_rail(rail) (line 1198)
func (d *DeepAgent) UnregisterRail(ctx context.Context, r agentinterfaces.AgentRail) error {
	d.railsMu.Lock()
	// 从 pendingRails 中移除
	d.pendingRails = removeRailByRef(d.pendingRails, r)
	// 从 registeredRails 中移除
	d.registeredRails = removeRailByRef(d.registeredRails, r)

	// 检查 TaskCompletionRail
	if isTaskCompletionRail(r) && d.taskCompletionRail == r {
		d.taskCompletionRail = nil
	}
	d.railsMu.Unlock()

	// 从外层 DeepAgent 注销
	_ = d.callbackManager.UnregisterRail(ctx, r)

	// 从内层 ReActAgent 注销桥接回调
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()
	if reactAgent != nil {
		_ = reactAgent.CallbackManager().UnregisterRail(ctx, r)
	}

	_ = r.Uninit(d)
	return nil
}

// ReactAgent 返回内层 ReActAgent 实例。
// 对齐 Python: DeepAgent.react_agent 属性 (line 479)
func (d *DeepAgent) ReactAgent() *agents.ReActAgent {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.reactAgent
}

// LoopCoordinator 返回循环协调器（可能为 nil）。
// 对齐 Python: DeepAgent.loop_coordinator 属性 (line 677)
func (d *DeepAgent) LoopCoordinator() hinterfaces.LoopCoordinatorInterface {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.loopCoordinator == nil {
		return nil
	}
	return d.loopCoordinator
}

// LoopController 返回任务循环控制器。
// 对齐 Python: DeepAgent.loop_controller 属性 (line 682)
func (d *DeepAgent) LoopController() controller.ControllerInterface {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.loopController == nil {
		return nil
	}
	return d.loopController
}

// EventHandler 返回事件处理器。
// 对齐 Python: DeepAgent.event_handler 属性 (line 694)
func (d *DeepAgent) EventHandler() ctrlmodules.EventHandler {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.loopController == nil {
		return nil
	}
	return d.loopController.EventHandler()
}

// LoadState 从会话加载 DeepAgentState。
// 对齐 Python: DeepAgent.load_state(session) (line 1790)
func (d *DeepAgent) LoadState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	// 两级缓存：runtime attribute → persisted session state
	state := d.readRuntimeState(sess)
	if state != nil {
		return state
	}
	// 从持久化状态加载
	data, err := sess.GetState(sessstate.StringKey(sessionStateKey))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("LoadState: GetState 失败，使用默认状态")
	}
	var loaded hschema.DeepAgentState
	if dataMap, ok := data.(map[string]any); ok {
		loaded = hschema.DeepAgentState{}.FromSessionDict(dataMap)
	} else {
		loaded = hschema.NewDeepAgentState()
	}
	d.writeRuntimeState(sess, &loaded)
	return &loaded
}

// DeepConfig 返回 DeepAgent 配置。
// 对齐 Python: DeepAgent.deep_config 属性 (line 474)
func (d *DeepAgent) DeepConfig() *hschema.DeepAgentConfig {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.deepConfig
}

// IsInvokeActive 判断是否有活跃的 invoke。
// 对齐 Python: DeepAgent.is_invoke_active 属性 (line 460)
func (d *DeepAgent) IsInvokeActive() bool {
	return d.invokeActive.Load()
}

// IsAutoInvokeScheduled 判断是否已调度自动 invoke。
// 对齐 Python: DeepAgent.is_auto_invoke_scheduled 属性 (line 465)
func (d *DeepAgent) IsAutoInvokeScheduled() bool {
	return d.autoInvokeScheduled.Load()
}

// SetAutoInvokeScheduled 设置自动 invoke 调度标记。
// 对齐 Python: DeepAgent.set_auto_invoke_scheduled(is_scheduled) (line 469)
func (d *DeepAgent) SetAutoInvokeScheduled(scheduled bool) {
	d.autoInvokeScheduled.Store(scheduled)
}

// ScheduleAutoInvokeOnSpawnDone 延迟调度自动 invoke。
// ⤵️ 9.1 回填：实现 SessionSpawn 完成后的自动 invoke 调度
// 对齐 Python: DeepAgent.schedule_auto_invoke_on_spawn_done(query, delay) (line 1959)
func (d *DeepAgent) ScheduleAutoInvokeOnSpawnDone(steerText string) error {
	d.autoInvokeScheduled.Store(true)

	go func() {
		time.Sleep(time.Duration(defaultAutoInvokeDelay * float64(time.Second)))
		d.autoInvokeScheduled.Store(false)

		if d.invokeActive.Load() {
			return
		}

		d.configMu.RLock()
		loopSess := d.loopSession
		d.configMu.RUnlock()

		if loopSess == nil {
			logger.Warn(logComponent).Msg("[AutoInvoke] 会话在延迟期间被清理，跳过")
			return
		}

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		defer cancel()

		_, err := d.Invoke(ctx, map[string]any{"query": steerText},
			agentinterfaces.WithSession(loopSess))
		if err != nil {
			logger.Error(logComponent).Err(err).Msg("[AutoInvoke] 自动 invoke 失败")
		}
	}()

	return nil
}

// CreateSubagent 创建子 Agent 实例。
// ⤵️ 9.3 / 9.25-9.31 回填：工厂分派所有分支返回 stub 错误
// 对齐 Python: DeepAgent.create_subagent(subagent_type, subsession_id) (line 898)
func (d *DeepAgent) CreateSubagent(subagentType string, subSessionID string) (hinterfaces.DeepAgentInterface, error) {
	spec := d.findSubagentSpec(subagentType)
	if spec == nil {
		return nil, exception.BuildError(exception.StatusDeepagentCreateSubagentNotFound,
			exception.WithMsg(fmt.Sprintf("子 Agent 规格未找到: %s", subagentType)))
	}

	// 如果 spec 本身就是 *DeepAgent 实例，直接返回
	if deepAgent, ok := spec.(*DeepAgent); ok {
		logger.Info(logComponent).Str("subagent_type", subagentType).Msg("已获得 DeepAgent 实例，直接返回")
		return deepAgent, nil
	}

	// 从 spec 获取 SubAgentConfig（SubagentSpec 接口的另一个实现）
	subCfg, ok := spec.(*hschema.SubAgentConfig)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentCreateSubagentNotFound,
			exception.WithMsg(fmt.Sprintf("子 Agent 规格类型不支持: %T", spec)))
	}

	// 配置合并逻辑 — 对齐 Python L938-982
	_ = d.buildSubagentCreateKwargs(subCfg, subSessionID)

	// 工厂分派 — 所有分支返回 stub 错误
	normalizedFactory := ""
	if subCfg.FactoryName != "" {
		normalizedFactory = normalizeFactoryName(subCfg.FactoryName)
	}

	switch normalizedFactory {
	case "browser_agent", "browser_runtime":
		return nil, fmt.Errorf("browser_agent 工厂尚未实现，⤵️ 9.26 回填")
	case "code_agent":
		return nil, fmt.Errorf("code_agent 工厂尚未实现，⤵️ 9.27 回填")
	case "research_agent":
		return nil, fmt.Errorf("research_agent 工厂尚未实现，⤵️ 9.25 回填")
	case "mobile_gui_agent", "mobile_agent":
		return nil, fmt.Errorf("mobile_gui_agent 工厂尚未实现，⤵️ 9.31 回填")
	case "plan_agent":
		return nil, fmt.Errorf("plan_agent 工厂尚未实现，⤵️ 9.28 回填")
	case "verification_agent":
		return nil, fmt.Errorf("verification_agent 工厂尚未实现，⤵️ 9.29 回填")
	case "explore_agent":
		return nil, fmt.Errorf("explore_agent 工厂尚未实现，⤵️ 9.30 回填")
	default:
		// 通过 CreateDeepAgent 工厂创建子 Agent 实例
		kwargs := d.buildSubagentCreateKwargs(subCfg, subSessionID)
		createParams := buildCreateParamsFromSubagentKwargs(kwargs)
		subAgent, createErr := CreateDeepAgent(context.Background(), createParams)
		if createErr != nil {
			return nil, fmt.Errorf("创建子 Agent 失败: %w", createErr)
		}
		return subAgent, nil
	}
}

// SetSessionToolkit 设置会话工具包。
// 对齐 Python: DeepAgent.set_session_toolkit(toolkit) (line 146)
func (d *DeepAgent) SetSessionToolkit(toolkit *subagent.SessionToolkit) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.sessionToolkit = toolkit
}

// SetReactAgent 注入内层 Agent 实现（用于运行时接线/测试）。
// 对齐 Python: DeepAgent.set_react_agent(react_agent, initialized) (line 444)
func (d *DeepAgent) SetReactAgent(reactAgent *agents.ReActAgent, initd bool) {
	d.configMu.Lock()
	defer d.configMu.Unlock()
	d.reactAgent = reactAgent
	d.initMu.Lock()
	d.initialized = initd
	d.initMu.Unlock()
}

// IsInitialized 返回是否已完成懒初始化。
// 对齐 Python: DeepAgent.is_initialized 属性 (line 455)
func (d *DeepAgent) IsInitialized() bool {
	d.initMu.Lock()
	v := d.initialized
	d.initMu.Unlock()
	return v
}

// AddRail 同步排队一个 Rail 以便延迟注册。
// 对齐 Python: DeepAgent.add_rail(rail) (line 1139)
func (d *DeepAgent) AddRail(r agentinterfaces.AgentRail) *DeepAgent {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	if isTaskCompletionRail(r) {
		// 移除已有的 TaskCompletionRail
		d.pendingRails = filterRailsNotType(d.pendingRails, isTaskCompletionRail)
	}
	d.pendingRails = append(d.pendingRails, r)
	return d
}

// FindRailsByType 返回排队和已注册中匹配指定类型的 Rail。
// 对齐 Python: DeepAgent.find_rails_by_type(rail_types) (line 1155)
func (d *DeepAgent) FindRailsByType(railTypes ...reflect.Type) []agentinterfaces.AgentRail {
	if len(railTypes) == 0 {
		return nil
	}
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	var result []agentinterfaces.AgentRail
	for _, r := range d.pendingRails {
		if matchType(r, railTypes) {
			result = append(result, r)
		}
	}
	for _, r := range d.registeredRails {
		if matchType(r, railTypes) {
			result = append(result, r)
		}
	}
	return result
}

// StripRailsByType 按类型移除排队 Rail 并将已注册 Rail 标记为废弃。
// 对齐 Python: DeepAgent.strip_rails_by_type(rail_types) (line 1166)
func (d *DeepAgent) StripRailsByType(railTypes ...reflect.Type) int {
	if len(railTypes) == 0 {
		return 0
	}
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	removed := 0

	// 从 pendingRails 移除（对齐 Python 差值计算）
	before := len(d.pendingRails)
	var newPending []agentinterfaces.AgentRail
	for _, r := range d.pendingRails {
		if !matchType(r, railTypes) {
			newPending = append(newPending, r)
		}
	}
	d.pendingRails = newPending
	removed = before - len(newPending)

	// 将已注册 Rail 标记为废弃
	for _, r := range d.registeredRails {
		if matchType(r, railTypes) {
			d.staleRails = append(d.staleRails, r)
			removed++
		}
	}
	return removed
}

// SwitchMode 切换 Agent 模式，更新会话级 PlanModeState。
// 对齐 Python: DeepAgent.switch_mode(session, mode) (line 1859)
func (d *DeepAgent) SwitchMode(sess sessioninterfaces.SessionFacade, mode string) {
	state := d.LoadState(sess)
	if state.PlanMode.Mode == mode {
		state.PlanMode.PrePlanMode = state.PlanMode.Mode
		logger.Info(logComponent).Str("mode", mode).Msg("[DeepAgent] 会话中模式相同，无需切换")
		return
	}
	state.PlanMode.PrePlanMode = state.PlanMode.Mode
	state.PlanMode.Mode = mode
	d.saveState(sess, state)
}

// RestoreModeAfterPlanExit 恢复进入规划模式前的模式。
// 对齐 Python: DeepAgent.restore_mode_after_plan_exit(session) (line 1875)
func (d *DeepAgent) RestoreModeAfterPlanExit(sess sessioninterfaces.SessionFacade) {
	state := d.LoadState(sess)
	state.PlanMode.Mode = state.PlanMode.PrePlanMode
	if state.PlanMode.Mode == "" {
		state.PlanMode.Mode = "normal"
	}
	state.PlanMode.PrePlanMode = ""
	d.saveState(sess, state)
}

// GetPlanFilePath 从会话状态中的 slug 推导计划文件路径。
// 对齐 Python: DeepAgent.get_plan_file_path(session) (line 1889)
func (d *DeepAgent) GetPlanFilePath(sess sessioninterfaces.SessionFacade) string {
	state := d.LoadState(sess)
	slug := state.PlanMode.PlanSlug
	if slug == "" {
		return ""
	}
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()
	if cfg == nil || cfg.Workspace == nil {
		return ""
	}
	// ⤵️ 9.3 回填：resolve_plan_file_path 工具实现后补全
	return filepath.Join(cfg.Workspace.RootPath, slug+".md")
}

// SaveState 持久化 DeepAgent 状态到会话。
// 对齐 Python: DeepAgent.save_state(session, state) (line 1815)
func (d *DeepAgent) SaveState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState) {
	d.saveState(sess, state)
}

// ClearState 清除 DeepAgent 运行时缓存。
// 对齐 Python: DeepAgent.clear_state(session, clear_persisted) (line 1843)
func (d *DeepAgent) ClearState(sess sessioninterfaces.SessionFacade, clearPersisted bool) {
	d.clearState(sess, clearPersisted)
}

// FollowUp 发布 FollowUp 事件到任务循环。
// 对齐 Python: DeepAgent.follow_up(msg, task_id, session) (line 2368)
func (d *DeepAgent) FollowUp(ctx context.Context, msg string, taskID string, sess *session.Session) {
	d.configMu.RLock()
	ctrl := d.loopController
	loopSess := d.loopSession
	d.configMu.RUnlock()

	if ctrl == nil {
		return
	}
	sessToUse := sess
	if sessToUse == nil {
		sessToUse = loopSess
	}
	if sessToUse == nil {
		return
	}

	// 对齐 Python: FollowUpEvent.from_text(msg)
	event := cschema.FromText(msg)
	if taskID != "" {
		event.SetMetadata(map[string]any{"task_id": taskID})
	}

	// 对齐 Python: controller.event_queue.publish_event_async(card.id, sess, event)
	if err := ctrl.PublishEventAsync(ctx, sessToUse, event); err != nil {
		logger.Error(logComponent).Err(err).Str("msg", msg).Msg("FollowUp 发布事件失败")
	}
}

// Steer 发布 TaskInteraction 事件到任务循环。
// 对齐 Python: DeepAgent.steer(msg, session) (line 2400)
func (d *DeepAgent) Steer(ctx context.Context, msg string, sess *session.Session) {
	d.configMu.RLock()
	ctrl := d.loopController
	loopSess := d.loopSession
	d.configMu.RUnlock()

	if ctrl == nil {
		return
	}
	s := sess
	if s == nil {
		s = loopSess
	}
	if s == nil {
		return
	}

	// 对齐 Python: TaskInteractionEvent(interaction=[TextDataFrame(text=msg)])
	// 对齐 Python: controller.event_queue.publish_event_async(self.card.id, sess, event)
	event := &cschema.TaskInteractionEvent{
		Interaction: []cschema.DataFrame{&cschema.TextDataFrame{Text: msg}},
	}
	if err := ctrl.PublishEventAsync(ctx, s, event); err != nil {
		logger.Warn(logComponent).Err(err).Str("msg", msg).Msg("Steer 发布 TaskInteractionEvent 失败")
		return
	}
	logger.Debug(logComponent).Str("msg", msg).Msg("Steer: TaskInteractionEvent 已发布")
}

// Abort 请求立即中止任务循环。
// 对齐 Python: DeepAgent.abort(session) (line 2448)
func (d *DeepAgent) Abort(ctx context.Context) {
	d.configMu.RLock()
	coord := d.loopCoordinator
	ctrl := d.loopController
	d.configMu.RUnlock()

	if coord != nil && ctrl != nil {
		coord.RequestAbort()
		handler := ctrl.EventHandler()
		if handler != nil {
			// 对齐 Python: handler.on_abort()
			if loopHandler, ok := handler.(*task_loop.TaskLoopEventHandler); ok {
				loopHandler.OnAbort()
			}
		}
	}

	d.cancelStreamProcess()
}

// EnqueueHarnessConfig 排队一个 harness_config.yaml 在下次 Stream() 时加载。
// 对齐 Python: DeepAgent.enqueue_harness_config(config_path) (line 1601)
func (d *DeepAgent) EnqueueHarnessConfig(configPath string) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()
	d.pendingHarnessConfigs = append(d.pendingHarnessConfigs, configPath)
}

// LoadHarnessConfig 热加载 harness_config.yaml 中声明的资源。
// ⤵️ 9.3 回填：内部调 create_deep_agent，完整实现待工厂补全
// 对齐 Python: DeepAgent.load_harness_config(config_path) (line 1218)
func (d *DeepAgent) LoadHarnessConfig(ctx context.Context, configPath string) ([]string, error) {
	// ⤵️ 9.3 回填：HarnessConfigLoader.load + 资源注册逻辑
	return nil, fmt.Errorf("load_harness_config 尚未实现，⤵️ 9.3 回填")
}

// UnloadHarnessConfig 卸载 harness_config.yaml 中声明的资源。
// ⤵️ 9.3 回填：内部调 create_deep_agent，完整实现待工厂补全
// 对齐 Python: DeepAgent.unload_harness_config(config_path) (line 1359)
func (d *DeepAgent) UnloadHarnessConfig(ctx context.Context, configPath string) ([]string, error) {
	// ⤵️ 9.3 回填：资源卸载逻辑
	return nil, fmt.Errorf("unload_harness_config 尚未实现，⤵️ 9.3 回填")
}

// EnsureInitialized 执行懒初始化（仅用于测试）。
// 对齐 Python: DeepAgent.ensure_initialized() (line 865)
func (d *DeepAgent) EnsureInitialized(ctx context.Context) error {
	return d.ensureInitialized(ctx)
}

// InitWorkspace 初始化工作空间目录结构。
// 对齐 Python: DeepAgent.init_workspace() (line 869)
func (d *DeepAgent) InitWorkspace(ctx context.Context) error {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil || cfg.Workspace == nil {
		return nil
	}

	// ⤵️ 9.3 回填：DirectoryBuilder 构建逻辑
	logger.Info(logComponent).Str("root_path", cfg.Workspace.RootPath).Msg("InitWorkspace: 目录构建待补全")
	return nil
}

// GetContextUsage 获取当前上下文占用统计。
// 对齐 Python: DeepAgent.get_context_usage(session_id, context_id) (line 566)
func (d *DeepAgent) GetContextUsage(ctx context.Context, sessionID string, contextID string) (map[string]any, error) {
	// ⤵️ 9.1 回填：需要 ContextEngine 集成
	return nil, fmt.Errorf("get_context_usage 尚未实现，⤵️ 9.1 回填")
}

// GetContextOccupancy GetContextUsage 的别名。
// 对齐 Python: DeepAgent.get_context_occupancy (line 596)
func (d *DeepAgent) GetContextOccupancy(ctx context.Context, sessionID string, contextID string) (map[string]any, error) {
	return d.GetContextUsage(ctx, sessionID, contextID)
}

// GetCurrentContext 返回当前上下文消息。
// 对齐 Python: DeepAgent.get_current_context(session_id, context_id) (line 604)
func (d *DeepAgent) GetCurrentContext(ctx context.Context, sessionID string, contextID string) ([]llmschema.BaseMessage, error) {
	// ⤵️ 9.1 回填：需要 ContextEngine 集成
	return nil, fmt.Errorf("get_current_context 尚未实现，⤵️ 9.1 回填")
}

// CreateNewContextEngine 创建新的上下文引擎。
// 对齐 Python: DeepAgent.create_new_context_engine(session_id, messages) (line 643)
func (d *DeepAgent) CreateNewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error) {
	// ⤵️ 9.1 回填：需要 ContextEngine 集成
	return "", fmt.Errorf("create_new_context_engine 尚未实现，⤵️ 9.1 回填")
}

// NewContextEngine CreateNewContextEngine 的别名。
// 对齐 Python: DeepAgent.new_context_engine (line 665)
func (d *DeepAgent) NewContextEngine(ctx context.Context, sessionID string, messages []llmschema.BaseMessage) (string, error) {
	return d.CreateNewContextEngine(ctx, sessionID, messages)
}

// LoopSession 返回活跃的循环会话。
// 对齐 Python: DeepAgent.loop_session 属性 (line 888)
func (d *DeepAgent) LoopSession() *session.Session {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	return d.loopSession
}

// ReactConfig 返回内层 ReActAgent 配置（仅用于测试）。
// 对齐 Python: DeepAgent.react_config 属性 (line 894)
func (d *DeepAgent) ReactConfig() agentinterfaces.AgentConfig {
	d.configMu.RLock()
	defer d.configMu.RUnlock()
	if d.reactAgent == nil {
		return nil
	}
	return d.reactAgent.Config()
}

// AgentID 返回 Agent 唯一标识。
// 实现 agentinterfaces.BaseAgent 接口。
func (d *DeepAgent) AgentID() string {
	if d.card == nil {
		return ""
	}
	return d.card.ID
}

// SpecName 返回规格名称，用于子 Agent 匹配。
// 实现 SubagentSpec 接口。
// 对齐 Python: isinstance(spec, DeepAgent) 时通过 spec.card.name 匹配。
func (d *DeepAgent) SpecName() string {
	if d.card == nil {
		return ""
	}
	return d.card.Name
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// filterDisabledTools 过滤掉被禁用的工具。
// 对齐 Python: DeepAgent._filter_disabled_tools(config) (line 162)
func (d *DeepAgent) filterDisabledTools(config *hschema.DeepAgentConfig) {
	if config.Tools == nil {
		return
	}
	// ⤵️ 9.1 回填：is_free_search_enabled / is_paid_search_enabled 检查逻辑
	// 当前总是保留所有工具
}

// initialConfigure 首次配置：持久化配置、创建内层 ReActAgent、排队 Rails。
// 对齐 Python: DeepAgent._initial_configure(config) (line 230)
func (d *DeepAgent) initialConfigure(config *hschema.DeepAgentConfig) {
	d.deepConfig = config
	if config.Card != nil {
		d.card = config.Card
	}

	d.reactAgent = d.createReactAgent()
	d.queuePendingRails(config)
}

// hotReconfigure 热重配置：不重启 Agent，更新模型/工具/提示词等。
// 对齐 Python: DeepAgent._hot_reconfigure(config) (line 239)
func (d *DeepAgent) hotReconfigure(ctx context.Context, config *hschema.DeepAgentConfig) {
	previousConfig := d.deepConfig
	d.deepConfig = config
	if config.Card != nil {
		d.card = config.Card
	}

	d.hotReloadRails(config)

	if config.Model != nil {
		d.hotReloadModel(ctx, config)
	}

	if config.Tools != nil && d.reactAgent != nil {
		previousTools := (*[]*tool.ToolCard)(nil)
		if previousConfig != nil {
			previousTools = &previousConfig.Tools
		}
		d.hotReloadTools(config, previousTools)
	}

	if config.SystemPrompt != "" && d.reactAgent != nil {
		d.hotReloadSystemPrompt(config)
	}

	d.queuePendingRails(config)

	// 注：Python 在 hotReconfigure 后调用 _sync_builder_to_active_rails() 将新 builder
	// 同步到已注册 rail。Go 的策略不同：agent 通过 SystemPromptBuilder() 接口返回最新
	// builder，rail 调用接口获取，无需显式同步。若后续 rail 体系需要持有 builder 引用，
	// 再补充 syncBuilderToActiveRails 逻辑。
}

// hotReloadRails 热重配置时循环废弃旧 Rail。
// 对齐 Python: DeepAgent._hot_reload_rails(config) (line 263)
func (d *DeepAgent) hotReloadRails(config *hschema.DeepAgentConfig) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	if config.Rails != nil {
		// 部分更新：只替换类型相同的 Rail
		replacingTypes := make(map[reflect.Type]bool)
		for _, r := range config.Rails {
			replacingTypes[reflect.TypeOf(r)] = true
		}
		var retained []agentinterfaces.AgentRail
		for _, r := range d.registeredRails {
			if replacingTypes[reflect.TypeOf(r)] {
				d.staleRails = append(d.staleRails, r)
			} else {
				retained = append(retained, r)
			}
		}
		d.registeredRails = retained

		var newPending []agentinterfaces.AgentRail
		for _, r := range d.pendingRails {
			if !replacingTypes[reflect.TypeOf(r)] {
				newPending = append(newPending, r)
			}
		}
		d.pendingRails = newPending
	} else {
		// 全量替换：所有已注册 Rail 变为废弃
		d.staleRails = append(d.staleRails, d.registeredRails...)
		d.registeredRails = nil
		d.pendingRails = nil
	}
}

// hotReloadModel 热更新 ReActAgent 模型配置。
// 对齐 Python: DeepAgent._hot_reload_model(config) (line 291)
func (d *DeepAgent) hotReloadModel(ctx context.Context, config *hschema.DeepAgentConfig) {
	if d.reactAgent == nil {
		return
	}

	// 获取当前 ReActAgent 配置的副本
	reactCfg, ok := d.reactAgent.Config().(*saconfig.ReActAgentConfig)
	if !ok || reactCfg == nil {
		return
	}
	newReactConfig := *reactCfg // 值拷贝

	if config.Model != nil {
		// 更新模型客户端配置
		model := config.Model
		if model.ClientConfig != nil {
			newReactConfig.ModelClientConfig = model.ClientConfig
		}
		if model.ModelConfig != nil {
			newReactConfig.ModelRequestConfig = model.ModelConfig
			if model.ModelConfig.ModelName != "" {
				newReactConfig.ModelNameVal = model.ModelConfig.ModelName
			}
		}
	}

	// 设置最大迭代次数
	if config.EnableTaskLoop {
		newReactConfig.MaxIterations = math.MaxInt
	} else {
		newReactConfig.MaxIterations = config.MaxIterations
	}

	if config.ContextEngineConfig != nil {
		newReactConfig.ContextEngineConfig = *config.ContextEngineConfig
	}

	if err := d.reactAgent.Configure(ctx, &newReactConfig); err != nil {
		logger.Error(logComponent).Err(err).Msg("热重配置 ReActAgent 模型失败")
		return
	}

	logger.Info(logComponent).Msg("[DeepAgent] 模型配置热重载完成")
}

// hotReloadTools 同步 AbilityManager 工具卡片。
// 对齐 Python: DeepAgent._hot_reload_tools(config, previous_tools) (line 324)
func (d *DeepAgent) hotReloadTools(config *hschema.DeepAgentConfig, previousTools *[]*tool.ToolCard) {
	newByName := make(map[string]*tool.ToolCard)
	for _, card := range config.Tools {
		newByName[card.Name] = card
	}

	previousByName := make(map[string]*tool.ToolCard)
	if previousTools != nil {
		for _, card := range *previousTools {
			previousByName[card.Name] = card
		}
	}

	// 确定受管理的工具名称集合
	managedNames := make(map[string]bool)
	for name := range previousByName {
		managedNames[name] = true
	}
	if len(managedNames) == 0 {
		for name := range newByName {
			managedNames[name] = true
		}
	}

	// 移除不再存在的工具
	var stale []string
	for name := range managedNames {
		if _, exists := newByName[name]; !exists {
			stale = append(stale, name)
		}
	}
	if len(stale) > 0 {
		for _, name := range stale {
			if card, ok := previousByName[name]; ok {
				d.unregisterToolResource(card)
			}
		}
		d.abilityManager.RemoveMany(stale)
	}

	// 添加或替换 ID 变更的工具
	for name, card := range newByName {
		existing := d.abilityManager.Get(name)
		existingTool, _ := existing.(*tool.ToolCard)

		if existingTool != nil && existingTool.ID == card.ID {
			d.ensureBuiltinToolResource(card, config)
			continue
		}
		if existingTool != nil {
			d.unregisterToolResource(existingTool)
			d.abilityManager.Remove(name)
		}
		d.abilityManager.Add(card)
		d.ensureBuiltinToolResource(card, config)
	}

	// 重排工具顺序
	var orderedNames []string
	for name := range newByName {
		orderedNames = append(orderedNames, name)
	}
	d.abilityManager.ReorderTools(orderedNames)
}

// hotReloadSystemPrompt 重建 SystemPromptBuilder。
// 对齐 Python: DeepAgent._hot_reload_system_prompt(config) (line 370)
func (d *DeepAgent) hotReloadSystemPrompt(config *hschema.DeepAgentConfig) {
	language := hprompts.ResolveLanguage(config.Language)
	mode := hprompts.ResolveMode(config.PromptMode.String())
	promptBuilder := hprompts.NewSystemPromptBuilder(language, mode)

	if config.SystemPrompt != "" {
		promptBuilder.AddSection(saprompts.NewPromptSection(
			sections.SectionIdentity,
			map[string]string{"cn": config.SystemPrompt, "en": config.SystemPrompt},
			10,
		))
	} else {
		promptBuilder.AddSection(sections.BuildIdentitySection())
	}
	prompt := promptBuilder.Build()

	// 更新 ReActAgent 配置
	if d.reactAgent != nil {
		currentConfig := d.reactAgent.Config()
		if reactCfg, ok := currentConfig.(*saconfig.ReActAgentConfig); ok {
			newReactConfig := *reactCfg
			newReactConfig.PromptTemplate = []map[string]any{
				{"role": "system", "content": prompt},
			}
			if err := d.reactAgent.Configure(context.Background(), &newReactConfig); err != nil {
				logger.Error(logComponent).Err(err).Msg("热重配置系统提示词失败")
			}
		}
	}

	d.systemPromptBuilder = promptBuilder
	if d.reactAgent != nil {
		d.reactAgent.SetPromptBuilder(promptBuilder.SystemPromptBuilder)
	}

	logger.Info(logComponent).Msg("[DeepAgent] 系统提示词热重载完成")
}

// queuePendingRails 将配置驱动的 Rail 追加到待注册列表。
// 对齐 Python: DeepAgent._queue_pending_rails(config) (line 408)
func (d *DeepAgent) queuePendingRails(config *hschema.DeepAgentConfig) {
	d.railsMu.Lock()
	defer d.railsMu.Unlock()

	if config.Rails != nil {
		d.pendingRails = append(d.pendingRails, config.Rails...)
	}

	d.taskCompletionRail = nil

	if config.ProgressiveToolEnabled {
		// ⤴️ 9.11 回填：ProgressiveToolRail 创建
		d.pendingRails = append(d.pendingRails, rails.NewProgressiveToolRail(config))
		logger.Debug(logComponent).Msg("ProgressiveToolRail 已创建，⤴️ 9.11 回填")
	}

	if config.EnableTaskLoop {
		// ⤵️ 9.11 回填：TaskCompletionRail 创建
		// d.pendingRails = append(d.pendingRails, NewTaskCompletionRail())
		logger.Debug(logComponent).Msg("TaskCompletionRail 待创建，⤵️ 9.11 回填")
	}

	if config.Permissions != nil {
		// ⤵️ 9.11 回填：build_permission_interrupt_rail
		logger.Debug(logComponent).Msg("PermissionInterruptRail 待创建，⤵️ 9.11 回填")
	}
}

// createReactAgent 从当前 DeepAgentConfig 构建内层 ReActAgent。
// 对齐 Python: DeepAgent._create_react_agent() (line 703)
func (d *DeepAgent) createReactAgent() *agents.ReActAgent {
	cfg := d.deepConfig
	if cfg == nil {
		panic("DeepAgentConfig 为空，请先调用 Configure()")
	}

	innerCard := agentschema.NewAgentCard(
		agentschema.WithAgentName(d.card.Name+"_react"),
		agentschema.WithAgentDescription(d.card.Description),
	)

	reactConfig := saconfig.NewReActAgentConfig()

	// 设置最大迭代次数
	if cfg.EnableTaskLoop {
		reactConfig.MaxIterations = math.MaxInt
	} else {
		reactConfig.MaxIterations = cfg.MaxIterations
	}

	if cfg.ContextEngineConfig != nil {
		reactConfig.ContextEngineConfig = *cfg.ContextEngineConfig
	}

	// 设置工作空间
	if cfg.Workspace != nil {
		reactConfig.Workspace = cfg.Workspace
	}

	// 构建系统提示词
	language := hprompts.ResolveLanguage(cfg.Language)
	mode := hprompts.ResolveMode(cfg.PromptMode.String())
	promptBuilder := hprompts.NewSystemPromptBuilder(language, mode)

	if cfg.SystemPrompt != "" {
		promptBuilder.AddSection(saprompts.NewPromptSection(
			sections.SectionIdentity,
			map[string]string{"cn": cfg.SystemPrompt, "en": cfg.SystemPrompt},
			10,
		))
	} else {
		promptBuilder.AddSection(sections.BuildIdentitySection())
	}
	prompt := promptBuilder.Build()
	reactConfig.PromptTemplate = []map[string]any{
		{"role": "system", "content": prompt},
	}

	// 设置模型
	if cfg.Model != nil {
		model := cfg.Model
		if model.ClientConfig != nil {
			reactConfig.ModelClientConfig = model.ClientConfig
		}
		if model.ModelConfig != nil {
			reactConfig.ModelRequestConfig = model.ModelConfig
			if model.ModelConfig.ModelName != "" {
				reactConfig.ModelNameVal = model.ModelConfig.ModelName
			}
		}
	}

	agent := agents.NewReActAgent(innerCard, reactConfig)

	// Configure 内部会覆盖 promptBuilder
	if err := agent.Configure(context.Background(), reactConfig); err != nil {
		logger.Error(logComponent).Err(err).Msg("内层 ReActAgent Configure 失败")
	}

	// 覆盖回共享 builder（对齐 Python L755-762）
	agent.SetPromptBuilder(promptBuilder.SystemPromptBuilder)

	// 共享 AbilityManager（对齐 Python L765）
	agent.SetAbilityManager(d.abilityManager)

	// 注入预构建 LLM（对齐 Python L768-769）
	if cfg.Model != nil {
		agent.SetLLM(cfg.Model)
	}

	// 保存 builder 引用
	d.systemPromptBuilder = promptBuilder

	return agent
}

// ensureInitialized 执行懒初始化。
// 对齐 Python: DeepAgent._ensure_initialized() (line 813)
func (d *DeepAgent) ensureInitialized(ctx context.Context) error {
	d.initMu.Lock()
	if d.initialized {
		d.initMu.Unlock()
		return nil
	}
	// 整个初始化过程在锁内执行，防止并发重复初始化
	defer d.initMu.Unlock()

	d.configMu.RLock()
	_ = d.deepConfig // ⤵️ 9.1 回填：init_cwd 逻辑（cfg.Workspace 非空时待实现）
	d.configMu.RUnlock()

	// 初始化工作空间 CWD

	// 注册待处理的 MCP 服务器
	d.registerPendingMCPs(ctx)

	// 工作空间初始化
	if d.needsWorkspaceInit() {
		if err := d.InitWorkspace(ctx); err != nil {
			logger.Warn(logComponent).Err(err).Msg("工作空间初始化失败")
		}
	}

	d.railsMu.Lock()
	// 注销废弃 Rail
	var staleToUnregister []agentinterfaces.AgentRail
	for _, staleRail := range d.staleRails {
		// 跳过同时也在 pending 中的同名实例
		found := false
		for _, pendingRail := range d.pendingRails {
			if pendingRail == staleRail {
				found = true
				break
			}
		}
		if !found {
			staleToUnregister = append(staleToUnregister, staleRail)
		}
	}
	d.staleRails = nil

	// 注册待处理 Rail
	pendingToRegister := make([]agentinterfaces.AgentRail, len(d.pendingRails))
	copy(pendingToRegister, d.pendingRails)
	d.pendingRails = nil
	d.railsMu.Unlock()

	// 执行废弃 Rail 注销
	for _, staleRail := range staleToUnregister {
		if err := d.UnregisterRail(ctx, staleRail); err != nil {
			logger.Warn(logComponent).Err(err).Msg("注销废弃 Rail 失败")
		}
	}

	// 注册待处理 Rail
	for _, r := range pendingToRegister {
		if isTaskCompletionRail(r) {
			d.railsMu.Lock()
			d.taskCompletionRail = r
			d.railsMu.Unlock()
		}
		// ⤵️ 9.1 回填：DeepAgentRail.set_sys_operation / set_workspace
		if err := r.Init(d); err != nil {
			logger.Warn(logComponent).Err(err).Str("rail_type", reflect.TypeOf(r).String()).Msg("Rail 初始化失败")
			continue
		}
		d.registerRailSelective(ctx, r)
		d.railsMu.Lock()
		d.registeredRails = append(d.registeredRails, r)
		d.railsMu.Unlock()
	}

	d.initialized = true
	return nil
}

// needsWorkspaceInit 检查是否需要工作空间初始化。
// 对齐 Python: DeepAgent._needs_workspace_init() (line 854)
func (d *DeepAgent) needsWorkspaceInit() bool {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()
	if cfg == nil {
		return false
	}
	return cfg.Workspace != nil && cfg.SysOperation != nil && cfg.AutoCreateWorkspace
}

// registerPendingMCPs 注册配置的 MCP 服务器。
// 对齐 Python: DeepAgent._register_pending_mcps() (line 773)
func (d *DeepAgent) registerPendingMCPs(ctx context.Context) {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil || len(cfg.Mcps) == 0 {
		return
	}

	// ⤵️ 9.1 回填：MCP 服务器注册逻辑
	for _, mcpConfig := range cfg.Mcps {
		d.abilityManager.Add(mcpConfig)
		logger.Debug(logComponent).Str("server_id", mcpConfig.ServerID).Msg("MCP 配置已添加到 AbilityManager")
	}
}

// normalizeInputs 解析用户输入为 InvokeInputs。
// 对齐 Python: DeepAgent._normalize_inputs(inputs) (line 1056)
func (d *DeepAgent) normalizeInputs(inputs any) (*agentinterfaces.InvokeInputs, error) {
	switch v := inputs.(type) {
	case map[string]any:
		var query agentinterfaces.InvokeQuery
		// query 字段可能是 string 或 InteractiveInput
		if q, ok := v["query"]; ok {
			switch qv := q.(type) {
			case string:
				query = agentinterfaces.InvokeQueryString(qv)
			case *interaction.InteractiveInput:
				query = qv
			default:
				query = agentinterfaces.InvokeQueryString(fmt.Sprintf("%v", qv))
			}
		} else {
			query = agentinterfaces.InvokeQueryString("")
		}
		conversationID, _ := v["conversation_id"].(string)
		var runKind agentinterfaces.RunKind
		var runContext *agentinterfaces.RunContext
		if run, ok := v["run"].(map[string]any); ok {
			if kind, kOk := run["kind"].(string); kOk && kind != "" {
				runKind = agentinterfaces.RunKind(kind)
			} else {
				runKind = agentinterfaces.RunKindNormal
			}
			if contextData, cOk := run["context"].(map[string]any); cOk {
				// 对齐 Python: RunContext(**context_data) (line 1069)
				runContext = &agentinterfaces.RunContext{}
				if reason, ok := contextData["reason"].(string); ok {
					runContext.Reason = agentinterfaces.HeartbeatReason(reason)
				}
				if sessionID, ok := contextData["session_id"].(string); ok {
					runContext.SessionID = sessionID
				}
				if mode, ok := contextData["context_mode"].(string); ok {
					runContext.ContextMode = mode
				}
				if extra, ok := contextData["extra"].(map[string]any); ok {
					runContext.Extra = extra
				}
			}
		}
		return &agentinterfaces.InvokeInputs{
			Query:          query,
			ConversationID: conversationID,
			RunKind:        runKind,
			RunContext:     runContext,
		}, nil
	case string:
		return &agentinterfaces.InvokeInputs{Query: agentinterfaces.InvokeQueryString(v)}, nil
	case *interaction.InteractiveInput:
		// 对齐 Python: isinstance(inputs, InteractiveInput) → query = inputs
		return &agentinterfaces.InvokeInputs{Query: v}, nil
	default:
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg(fmt.Sprintf("不支持的输入类型: %T，必须是 string、[]Message、map 或 InvokeInputs", v)))
	}
}

// toEffectiveInputs 将 InvokeInputs 转换为 ReAct 输入字典。
// 对齐 Python: DeepAgent._to_effective_inputs(invoke_inputs) (line 1093)
func toEffectiveInputs(invokeInputs *agentinterfaces.InvokeInputs) map[string]any {
	result := map[string]any{"query": invokeInputs.Query}
	if invokeInputs.ConversationID != "" {
		result["conversation_id"] = invokeInputs.ConversationID
	}
	if invokeInputs.RunKind != "" {
		result["run_kind"] = invokeInputs.RunKind
	}
	if invokeInputs.RunContext != nil {
		result["run_context"] = invokeInputs.RunContext
	}
	return result
}

// isResumeInput 判断输入是否为中断恢复。
// 对齐 Python: DeepAgent._is_resume_input(invoke_inputs) (line 1106)
func isResumeInput(invokeInputs *agentinterfaces.InvokeInputs) bool {
	// 对齐 Python: isinstance(invoke_inputs.query, InteractiveInput)
	return invokeInputs.Query != nil && invokeInputs.Query.IsInteractiveInput()
}

// resultFromStreamChunk 从流块构建 invoke 风格的结果。
// 对齐 Python: DeepAgent._result_from_stream_chunk(chunk, output_parts) (line 1111)
func resultFromStreamChunk(chunk stream.Schema, outputParts *[]string) map[string]any {
	// 只处理 OutputSchema 类型
	outChunk, ok := chunk.(stream.OutputSchema)
	if !ok {
		return nil
	}

	chunkType := outChunk.Type
	payload, ok := outChunk.Payload.(map[string]any)
	if !ok {
		return nil
	}

	switch chunkType {
	case "llm_output":
		// 对齐 Python: llm_output → 追加 content 到 outputParts
		if content, ok := payload["content"].(string); ok {
			*outputParts = append(*outputParts, content)
		}
		return nil

	case "answer":
		// 对齐 Python: answer → 从 payload 构建结果 dict
		result := make(map[string]any, len(payload))
		for k, v := range payload {
			result[k] = v
		}
		if _, exists := result["result_type"]; !exists {
			result["result_type"] = "answer"
		}
		if _, exists := result["output"]; !exists {
			if content, ok := result["content"].(string); ok {
				*outputParts = append(*outputParts, content)
				result["output"] = content
			} else {
				return nil
			}
		}
		return result

	default:
		return nil
	}
}

// registerRailSelective 选择性路由 Rail 回调到正确的 Agent。
// 对齐 Python: DeepAgent._register_rail_selective(rail) (line 1626)
func (d *DeepAgent) registerRailSelective(ctx context.Context, r agentinterfaces.AgentRail) {
	callbacks := r.GetCallbacks()

	for event, callback := range callbacks {
		if bridgeEvents[event] {
			// 桥接事件 → 注册到内层 ReActAgent
			d.configMu.RLock()
			reactAgent := d.reactAgent
			d.configMu.RUnlock()
			if reactAgent != nil {
				reactAgent.CallbackManager().RegisterCallback(ctx, event, callback)
			}
			continue
		}

		if outerOnlyEvents[event] || deepEvents[event] {
			// 外层/Deep 事件 → 注册到外层 DeepAgent
			d.callbackManager.RegisterCallback(ctx, event, callback)
			continue
		}

		// 未知事件注册到外层
		logger.Warn(logComponent).
			Str("event", string(event)).
			Msg("未知 Rail 事件，注册到外层 DeepAgent")
		d.callbackManager.RegisterCallback(ctx, event, callback)
	}
}

// runSingleRoundInvoke 调用内层 ReActAgent 一次。
// 对齐 Python: DeepAgent._run_single_round_invoke(ctx, session) (line 1647)
func (d *DeepAgent) runSingleRoundInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	modified, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg("ctx.inputs 必须为 InvokeInputs 类型"))
	}

	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	effectiveInputs := toEffectiveInputs(modified)
	return reactAgent.Invoke(ctx, effectiveInputs, agentinterfaces.WithSession(sess))
}

// runTaskLoopInvoke 运行外层任务循环，返回最后一轮结果。
// 对齐 Python: DeepAgent._run_task_loop_invoke(ctx, session) (line 2112)
// runTaskLoopInvoke 执行外层任务循环，返回最后一轮结果。
// 对齐 Python: DeepAgent._run_task_loop_invoke(ctx, session) (line 2112-2144)
func (d *DeepAgent) runTaskLoopInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, sess sessioninterfaces.SessionFacade) (map[string]any, error) {
	sessConcrete, ok := sess.(*session.Session)
	if !ok || sessConcrete == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要 *session.Session 类型会话"))
	}

	loopCh, err := d.runTaskLoop(ctx, cbc, sessConcrete, false)
	if err != nil {
		return nil, err
	}

	// 对齐 Python: last_result: Dict[str, Any] = {}
	// async for result in _run_task_loop(): last_result = result
	var lastResult map[string]any
	for result := range loopCh {
		lastResult = result
	}

	// 对齐 Python: return last_result
	return lastResult, nil
}

// runTaskLoop 任务循环生成器，每轮完成后将 result 发送到 channel。
// 对齐 Python: DeepAgent._run_task_loop(ctx, session) (line 1991-2110)
// invoke 和 stream 共用此方法：
//   invoke: 从 channel 读取最后一轮结果
//   stream: 后台goroutine从channel读取，每轮写入session流
func (d *DeepAgent) runTaskLoop(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext, sess *session.Session, isStreaming bool) (<-chan map[string]any, error) {
	modified, ok := cbc.Inputs().(*agentinterfaces.InvokeInputs)
	if !ok {
		return nil, exception.BuildError(exception.StatusDeepagentContextParamError,
			exception.WithMsg("ctx.inputs 必须为 InvokeInputs 类型"))
	}

	coord, ctrl, err := d.setupTaskLoop(ctx, sess)
	if err != nil {
		return nil, err
	}

	// 绑定会话（对齐 Python line 2013-2017）
	d.configMu.RLock()
	boundID := d.boundSessionID
	d.configMu.RUnlock()

	sessionID := sess.GetSessionID()
	if boundID != sessionID {
		if err := ctrl.BindSession(ctx, sess); err != nil {
			logger.Warn(logComponent).Err(err).Msg("绑定会话失败")
		}
		d.configMu.Lock()
		d.boundSessionID = sessionID
		d.configMu.Unlock()
	}

	outCh := make(chan map[string]any, 1)

	go func() {
		defer close(outCh)

		// 对齐 Python: try/finally 确保清理（line 2095-2110）
		defer func() {
			state := d.LoadState(sess)
			state.StopConditionState = nil
			d.saveState(sess, state)

			if !d.hasPendingSessionSpawn() {
				_ = ctrl.UnbindSession(ctx, sess)
				_ = ctrl.Stop(ctx)
				d.configMu.Lock()
				d.loopCoordinator = nil
				d.loopController = nil
				d.loopSession = nil
				d.boundSessionID = ""
				d.configMu.Unlock()
				logLoop("all tasks completed, controller cleaned up", "", 0)
			} else {
				logLoop("pending SESSION_SPAWN tasks, controller kept alive", "", 0)
			}
		}()

		currentQuery := modified.Query
		outerRound := 0

		d.configMu.RLock()
		timeout := hschema.DefaultCompletionTimeout
		if d.deepConfig != nil && d.deepConfig.CompletionTimeout > 0 {
			timeout = d.deepConfig.CompletionTimeout
		}
		d.configMu.RUnlock()

		for coord.ShouldContinue() {
			outerRound++

				// 排空 follow-up，合并到 state 缓冲（对齐 Python line 2026-2040）
			newFollowUps := ctrl.DrainFollowUp()
			st := d.LoadState(sess)
			if len(newFollowUps) > 0 {
				st.PendingFollowUps = append(st.PendingFollowUps, newFollowUps...)
			}
			// 弹出第一个缓冲的 follow-up 作为当前查询
			isFollowUp := len(st.PendingFollowUps) > 0
			if isFollowUp {
				currentQuery = agentinterfaces.InvokeQueryString(st.PendingFollowUps[0])
				st.PendingFollowUps = st.PendingFollowUps[1:]
				d.saveState(sess, st)
			}

			queryPreview := currentQuery.PlainText()
			if len(queryPreview) > 120 {
				queryPreview = queryPreview[:120]
			}
			logLoop("round=%d started", fmt.Sprintf(", query=%s", queryPreview), outerRound)

			if err = ctrl.SubmitRound(ctx, sess, string(currentQuery.PlainText()), isFollowUp, isStreaming, modified.RunKind, modified.RunContext); err != nil {
				logger.Error(logComponent).Err(err).Int("round", outerRound).Msg("提交轮次失败")
				break
			}

			result := ctrl.WaitRoundCompletion(ctx, &timeout)
			resultType, _ := result["result_type"].(string)
			if resultType == "" {
				resultType = "N/A"
			}

			outputPreview := ""
			if output, ok := result["output"].(string); ok {
				outputPreview = output
				if len(outputPreview) > 200 {
					outputPreview = outputPreview[:200]
				}
			}
			logLoop("round=%d completed, result_type=%s", fmt.Sprintf(", output=%s", outputPreview), outerRound, resultType)

			// yield result（对齐 Python line 2063: yield result）
			select {
			case outCh <- result:
			case <-ctx.Done():
				return
			}

			coord.IncrementIteration()
			coord.SetLastResult(result)

			// 更新状态
			st = d.LoadState(sess)
			exported := coord.ExportState()
			st.StopConditionState = map[string]any{
				"iteration":        exported.Iteration,
				"token_usage":      exported.TokenUsage,
				"stop_reason":      exported.StopReason,
				"evaluator_states": exported.EvaluatorStates,
			}
			d.saveState(sess, st)

			if resultType == "interrupt" {
				logLoop("round=%d interrupted", "", outerRound)
				break
			}
			if coord.IsAborted() {
				logLoop("round=%d aborted", "", outerRound)
				break
			}

			// 对齐 Python line 2079-2086: 退出条件检查
			st = d.LoadState(sess)
			if ctrl.HasFollowUp() || len(st.PendingFollowUps) > 0 {
				continue
			}
			if !d.hasRemainingTasks(sess) {
				logLoop("no remaining tasks, loop finished", "", 0)
				break
			}

			currentQuery = modified.Query
		}

		// 对齐 Python line 2090-2094: 循环结束后记录 stop_reason
		stopReason := coord.StopReason()
		if stopReason != "" {
			logLoop("loop stopped by: %s", "", stopReason)
		}
	}()

	return outCh, nil
}

// writeRoundResultToStream 将任务循环轮次结果写入会话流。
// 对齐 Python: DeepAgent._write_round_result_to_stream(result, session) (line 2214-2232)
func (d *DeepAgent) writeRoundResultToStream(ctx context.Context, result map[string]any, sess *session.Session) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()

	if reactAgent != nil {
		reactAgent.WriteInvokeResultToStream(ctx, result, sess)
	}
}

// runTaskLoopStream 流式执行外层任务循环。
// 对齐 Python: DeepAgent._run_task_loop_stream(ctx, session, stream_modes) (line 2146-2212)
//
// 使用与 ReActAgent.Stream() 相同的"后台+前台"模式：
//   后台goroutine: 运行 _run_task_loop，每轮结果写入session流，最终 close_stream
//   前台: 从 session.StreamIterator() 读取chunk转发到 outCh
func (d *DeepAgent) runTaskLoopStream(ctx context.Context, invokeInputs *agentinterfaces.InvokeInputs, sess sessioninterfaces.SessionFacade, streamModes []stream.StreamMode) (<-chan stream.Schema, error) {
	// 对齐 Python: _ = stream_modes（显式丢弃，task-loop stream 从 session.StreamIterator() 读取）
	_ = streamModes

	if sess == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要会话"))
	}

	sessConcrete, ok := sess.(*session.Session)
	if !ok || sessConcrete == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("任务循环模式需要 *session.Session 类型会话"))
	}

	// 构建 AgentCallbackContext
	cbc := agentinterfaces.NewAgentCallbackContext(d, invokeInputs, sess)

	// 派生子 context，用于取消后台 goroutine
	// 对齐 Python: asyncio.create_task(_stream_process()) + CancelledError 处理
	streamCtx, streamCancel := context.WithCancel(ctx)
	d.streamMu.Lock()
	d.streamCancel = streamCancel
	d.streamMu.Unlock()

	outCh := make(chan stream.Schema, 64)

	// 后台 goroutine: 运行任务循环，每轮结果写入session流
	// 对齐 Python: _stream_process() (line 2182-2194)
	go func() {
		// 对齐 Python line 2190-2194: finally → session.close_stream()
		defer func() {
			d.streamMu.Lock()
			d.streamCancel = nil
			d.streamMu.Unlock()
			_ = sessConcrete.CloseStream()
		}()

		loopCh, loopErr := d.runTaskLoop(streamCtx, cbc, sessConcrete, true)
		if loopErr != nil {
			// 对齐 Python line 2187-2189: except → 写入错误结果到流
			d.writeRoundResultToStream(streamCtx, map[string]any{
				"output":      loopErr.Error(),
				"result_type": "error",
			}, sessConcrete)
			return
		}

		// 对齐 Python line 2184-2185:
		// async for result in _run_task_loop(): _write_round_result_to_stream(result, session)
		for result := range loopCh {
			d.writeRoundResultToStream(streamCtx, result, sessConcrete)
		}
	}()

	// 前台: 从 session.StreamIterator() 读取chunk转发到 outCh
	// 对齐 Python line 2199-2200: async for chunk in session.stream_iterator(): yield chunk
	go func() {
		defer close(outCh)
		defer d.cancelStreamProcess() // 前台退出时取消后台
		for chunk := range sessConcrete.StreamIterator() {
			select {
			case outCh <- chunk:
			case <-ctx.Done():
				return
			}
		}
	}()

	return outCh, nil
}

// runSingleRoundStream 流式调用内层 ReActAgent 一次。
// 对齐 Python: DeepAgent._run_single_round_stream(ctx, session, stream_modes) (line 2234)
func (d *DeepAgent) runSingleRoundStream(ctx context.Context, invokeInputs *agentinterfaces.InvokeInputs, sess sessioninterfaces.SessionFacade, streamModes []stream.StreamMode) (<-chan stream.Schema, error) {
	d.configMu.RLock()
	reactAgent := d.reactAgent
	d.configMu.RUnlock()

	if reactAgent == nil {
		return nil, exception.BuildError(exception.StatusDeepagentRuntimeError,
			exception.WithMsg("DeepAgent 未配置，请先调用 Configure()"))
	}

	effectiveInputs := toEffectiveInputs(invokeInputs)
	return reactAgent.Stream(ctx, effectiveInputs,
		agentinterfaces.WithSession(sess),
		agentinterfaces.WithStreamModes(streamModes))
}

// setupTaskLoop 创建或复用 Controller 基础设施。
// 对齐 Python: DeepAgent._setup_task_loop(session) (line 1669)
func (d *DeepAgent) setupTaskLoop(ctx context.Context, sess *session.Session) (*task_loop.LoopCoordinator, *task_loop.TaskLoopController, error) {
	sessionID := sess.GetSessionID()

	d.configMu.RLock()
	existingCtrl := d.loopController
	boundID := d.boundSessionID
	coord := d.loopCoordinator
	taskCompRail := d.taskCompletionRail
	d.configMu.RUnlock()

	// 复用已有控制器（session_id 匹配）
	if existingCtrl != nil && boundID == sessionID {
		if coord == nil {
			// 构建评估器
			var evaluators []task_loop.StopConditionEvaluator
			if taskCompRail != nil {
				// ⤵️ 9.11 回填：taskCompletionRail.buildEvaluators()
				evaluators = []task_loop.StopConditionEvaluator{}
			}
			coord = task_loop.NewLoopCoordinator(evaluators)
			d.configMu.Lock()
			d.loopCoordinator = coord
			d.configMu.Unlock()
		}
		coord.Reset()
		return coord, existingCtrl, nil
	}

	// 新控制器（首次或会话切换）
	if existingCtrl != nil {
		d.forceCleanupController(ctx)
	}

	// 构建评估器
	var evaluators []task_loop.StopConditionEvaluator
	if taskCompRail != nil {
		// ⤵️ 9.11 回填：taskCompletionRail.buildEvaluators()
		evaluators = []task_loop.StopConditionEvaluator{}
	}
	coord = task_loop.NewLoopCoordinator(evaluators)
	coord.Reset()

	queues := task_loop.NewLoopQueues(64)

	ctrlConfig := &config.ControllerConfig{
		SuppressCompletionSignal: true,
	}

	// 创建 ContextEngine
	// ⤵️ 9.1 回填：ContextEngine 实例创建
	var ce ceinterface.ContextEngine

	ctrl := task_loop.NewTaskLoopController()
	ctrl.Init(d.card, ctrlConfig, d.abilityManager, ce)

	// 注册执行器
	ctrl.AddTaskExecutor(hschema.DeepTaskType, task_loop.BuildDeepExecutor(d))
	ctrl.AddTaskExecutor(hschema.SessionSpawnTaskType, task_loop.BuildSessionSpawnExecutor(d))

	handler := task_loop.NewTaskLoopEventHandler(d)
	handler.SetInteractionQueues(queues)
	ctrl.SetEventHandler(handler)

	// 注入 SessionToolkit
	d.configMu.RLock()
	toolkit := d.sessionToolkit
	d.configMu.RUnlock()
	if toolkit != nil {
		handler.SetSessionToolkit(toolkit)
	}

	d.configMu.Lock()
	d.loopCoordinator = coord
	d.loopController = ctrl
	d.loopSession = sess
	d.configMu.Unlock()

	return coord, ctrl, nil
}

// forceCleanupController 强制清理已有控制器（会话切换时）。
// 对齐 Python: DeepAgent._force_cleanup_controller() (line 1928)
func (d *DeepAgent) forceCleanupController(ctx context.Context) {
	d.configMu.RLock()
	ctrl := d.loopController
	loopSess := d.loopSession
	d.configMu.RUnlock()

	if ctrl == nil {
		return
	}

	logLoop("forcing controller cleanup due to session switch", "", 0)

	if loopSess != nil {
		if err := ctrl.UnbindSession(ctx, loopSess); err != nil {
			logger.Warn(logComponent).Err(err).Msg("force cleanup 时 UnbindSession 失败")
		}
	}

	if err := ctrl.Stop(ctx); err != nil {
		logger.Warn(logComponent).Err(err).Msg("force cleanup 时 Stop 失败")
	}

	d.configMu.Lock()
	d.loopCoordinator = nil
	d.loopController = nil
	d.loopSession = nil
	d.boundSessionID = ""
	d.configMu.Unlock()
}

// hasRemainingTasks 检查任务计划是否还有待执行任务。
// 对齐 Python: DeepAgent._has_remaining_tasks(session) (line 1909)
func (d *DeepAgent) hasRemainingTasks(sess sessioninterfaces.SessionFacade) bool {
	state := d.LoadState(sess)
	if state.TaskPlan == nil {
		return false
	}
	return state.TaskPlan.GetNextTask() != nil
}

// hasPendingSessionSpawn 检查是否有待处理的 SESSION_SPAWN 任务。
// 对齐 Python: DeepAgent._has_pending_session_spawn() (line 1916)
func (d *DeepAgent) hasPendingSessionSpawn() bool {
	d.configMu.RLock()
	toolkit := d.sessionToolkit
	d.configMu.RUnlock()

	if toolkit == nil {
		return false
	}

	pendingTasks := toolkit.ListAll()
	for _, r := range pendingTasks {
		if r.Status == "running" {
			return true
		}
	}
	return false
}

// findSubagentSpec 查找匹配 subagentType 的子 Agent 规格。
// 返回 SubagentSpec 接口，可能是 *SubAgentConfig 或 *DeepAgent。
// 对齐 Python: DeepAgent._find_subagent_spec(subagent_type) (line 1032)
func (d *DeepAgent) findSubagentSpec(subagentType string) hschema.SubagentSpec {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil {
		return nil
	}

	for _, spec := range cfg.Subagents {
		if spec.SpecName() == subagentType {
			return spec
		}
	}
	return nil
}

// buildSubagentCreateKwargs 构建子 Agent 创建参数。
// 对齐 Python: DeepAgent.create_subagent L938-982
func (d *DeepAgent) buildSubagentCreateKwargs(subCfg *hschema.SubAgentConfig, subSessionID string) *hschema.SubagentCreateParams {
	d.configMu.RLock()
	cfg := d.deepConfig
	d.configMu.RUnlock()

	if cfg == nil {
		return nil
	}

	// 工作空间路径
	var ws *workspace.Workspace
	if cfg.Workspace != nil {
		ws = &workspace.Workspace{
			RootPath: filepath.Join(cfg.Workspace.RootPath, subSessionID),
			Language: cfg.Language,
		}
	}

	// 合并配置
	model := subCfg.Model
	if model == nil {
		model = cfg.Model
	}

	maxIterations := subCfg.MaxIterations
	if maxIterations == 0 {
		maxIterations = cfg.MaxIterations
	}

	var subWorkspace *workspace.Workspace
	if subCfg.Workspace != nil {
		subWorkspace = subCfg.Workspace
	}
	if subWorkspace == nil {
		subWorkspace = ws
	}

	backend := subCfg.Backend
	if backend == nil {
		backend = cfg.Backend
	}

	var sysOp sysop.SysOperation
	if subCfg.SysOperation != nil && subCfg.Workspace != nil {
		sysOp = subCfg.SysOperation
	}

	language := subCfg.Language
	if language == "" {
		language = cfg.Language
	}

	promptMode := subCfg.PromptMode
	if promptMode == 0 {
		promptMode = cfg.PromptMode
	}

	return &hschema.SubagentCreateParams{
		Model:                  model,
		Card:                   subCfg.AgentCard,
		SystemPrompt:           subCfg.SystemPrompt,
		Tools:                  subCfg.Tools,
		Mcps:                   subCfg.Mcps,
		Rails:                  subCfg.Rails,
		EnableTaskLoop:         subCfg.EnableTaskLoop,
		MaxIterations:          maxIterations,
		Workspace:              subWorkspace,
		Skills:                 subCfg.Skills,
		Backend:                backend,
		SysOperation:           sysOp,
		Language:               language,
		PromptMode:             promptMode,
		Subagents:              nil,
		EnableAsyncSubagent:    false,
		AddGeneralPurposeAgent: false,
		EnablePlanMode:         subCfg.EnablePlanMode,
		RestrictToWorkDir:      subCfg.RestrictToWorkDir,
	}
}

// drainPendingHarnessConfigs 在处理查询前加载排队的 harness 配置。
// 对齐 Python: DeepAgent._drain_pending_harness_configs() (line 1607)
func (d *DeepAgent) drainPendingHarnessConfigs(ctx context.Context) error {
	d.railsMu.Lock()
	configs := d.pendingHarnessConfigs
	d.pendingHarnessConfigs = nil
	d.railsMu.Unlock()

	for _, path := range configs {
		loaded, err := d.LoadHarnessConfig(ctx, path)
		if err != nil {
			logger.Error(logComponent).Err(err).Str("path", path).Msg("自动加载 harness 配置失败")
			continue
		}
		logger.Info(logComponent).Str("path", path).Any("loaded", loaded).Msg("自动加载 harness 配置成功")
	}
	return nil
}

// cancelStreamProcess 取消进行中的流处理。
// 对齐 Python: DeepAgent._cancel_stream_process_task() (line 2432)
func (d *DeepAgent) cancelStreamProcess() {
	d.streamMu.Lock()
	cancel := d.streamCancel
	d.streamCancel = nil
	d.streamMu.Unlock()

	if cancel != nil {
		cancel()
	}
}

// readRuntimeState 从会话读取运行时状态缓存。
// 对齐 Python: DeepAgent._read_runtime_state(session) (line 1758)
func (d *DeepAgent) readRuntimeState(sess sessioninterfaces.SessionFacade) *hschema.DeepAgentState {
	// 通过 GetState 读取运行时属性
	data, err := sess.GetState(sessstate.StringKey(sessionRuntimeAttr))
	if err != nil || data == nil {
		return nil
	}
	state, ok := data.(*hschema.DeepAgentState)
	if !ok {
		return nil
	}
	return state
}

// writeRuntimeState 将运行时状态写入会话缓存。
// 对齐 Python: DeepAgent._write_runtime_state(session, state) (line 1776)
func (d *DeepAgent) writeRuntimeState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState) {
	sess.UpdateState(map[string]any{sessionRuntimeAttr: state})
}

// clearRuntimeState 从会话清除运行时状态缓存。
// 对齐 Python: DeepAgent._clear_runtime_state(session) (line 1784)
func (d *DeepAgent) clearRuntimeState(sess sessioninterfaces.SessionFacade) {
	sess.UpdateState(map[string]any{sessionRuntimeAttr: nil})
}

// saveState 持久化 DeepAgent 状态到会话。
// 对齐 Python: DeepAgent.save_state(session, state) (line 1815)
func (d *DeepAgent) saveState(sess sessioninterfaces.SessionFacade, state *hschema.DeepAgentState) {
	target := state
	if target == nil {
		target = d.readRuntimeState(sess)
	}
	if target == nil {
		return
	}
	d.writeRuntimeState(sess, target)
	sess.UpdateState(map[string]any{sessionStateKey: target.ToSessionDict()})
}

// clearState 清除 DeepAgent 运行时缓存。
// 对齐 Python: DeepAgent.clear_state(session, clear_persisted) (line 1843)
func (d *DeepAgent) clearState(sess sessioninterfaces.SessionFacade, clearPersisted bool) {
	d.clearRuntimeState(sess)
	if clearPersisted {
		sess.UpdateState(map[string]any{sessionStateKey: nil})
	}
}

// unregisterToolResource 注销工具资源。
// 对齐 Python: DeepAgent._unregister_tool_resource(card) (line 178)
func (d *DeepAgent) unregisterToolResource(card *tool.ToolCard) {
	// ⤵️ 9.1 回填：Runner.resource_mgr.remove_tool / remove_resource_tag
	logger.Debug(logComponent).Str("tool_name", card.Name).Str("tool_id", card.ID).Msg("unregisterToolResource 待补全")
}

// ensureBuiltinToolResource 确保内置工具资源已注册。
// 对齐 Python: DeepAgent._ensure_builtin_tool_resource(card, config) (line 206)
func (d *DeepAgent) ensureBuiltinToolResource(card *tool.ToolCard, config *hschema.DeepAgentConfig) {
	// ⤵️ 9.1 回填：free_search / paid_search 资源注册
	logger.Debug(logComponent).Str("tool_name", card.Name).Msg("ensureBuiltinToolResource 待补全")
}

// logLoop 记录外层循环日志。
// 对齐 Python: DeepAgent._log_loop(msg, detail) (line 1951)
func logLoop(format string, detail string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	if detail != "" {
		logger.Info(logComponent).Str("event_type", "OuterLoop").Str("msg", msg+detail).Msg("")
	} else {
		logger.Info(logComponent).Str("event_type", "OuterLoop").Str("msg", msg).Msg("")
	}
}

// normalizeFactoryName 规范化工厂名称。
func normalizeFactoryName(name string) string {
	result := make([]byte, 0, len(name))
	for _, c := range name {
		if c == ' ' || c == '\t' {
			continue
		}
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		result = append(result, byte(c))
	}
	return string(result)
}

// joinStrings 拼接字符串切片。
func joinStrings(parts []string) string {
	result := make([]byte, 0, len(parts)*32)
	for _, s := range parts {
		result = append(result, s...)
	}
	return string(result)
}

// isTaskCompletionRail 判断 Rail 是否为 TaskCompletionRail 类型。
func isTaskCompletionRail(r agentinterfaces.AgentRail) bool {
	// ⤵️ 9.11 回填：TaskCompletionRail 类型检查
	return reflect.TypeOf(r).String() == "TaskCompletionRail"
}

// removeRailByRef 按引用移除 Rail。
func removeRailByRef(rails []agentinterfaces.AgentRail, target agentinterfaces.AgentRail) []agentinterfaces.AgentRail {
	var result []agentinterfaces.AgentRail
	for _, r := range rails {
		if r != target {
			result = append(result, r)
		}
	}
	return result
}

// filterRailsNotType 按类型谓词过滤 Rail。
func filterRailsNotType(rails []agentinterfaces.AgentRail, predicate func(agentinterfaces.AgentRail) bool) []agentinterfaces.AgentRail {
	var result []agentinterfaces.AgentRail
	for _, r := range rails {
		if !predicate(r) {
			result = append(result, r)
		}
	}
	return result
}

// matchType 检查 Rail 是否匹配指定类型列表。
func matchType(r agentinterfaces.AgentRail, types []reflect.Type) bool {
	rType := reflect.TypeOf(r)
	for _, t := range types {
		if rType == t {
			return true
		}
	}
	return false
}
