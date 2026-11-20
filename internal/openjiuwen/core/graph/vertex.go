// Vertex 工作流图节点执行器：双链模型——批入侧（INVOKE/STREAM，Pregel 调度同步链）
// 与流入侧（COLLECT/TRANSFORM，StreamActor 首帧异步链），两半经当轮 streamDone
// 握手对齐后 call() 才 mark_producer_done 交屏障。
//
// 并发映射零锁依据（spec docs/superpowers/specs/2026-11-17-vertex-8.11-design.md §1
// 不变式）：计数器/streamDone 槽位的跨 goroutine 访问对全部被引擎屏障定序——
// 批侧判定窗口内无并发写方，新一轮 spawn（含 streamDone 槽位重写）只会发生在
// 旧任务结束且批侧已消费当轮结果之后。-race 并发用例锁定该不变式。
package graph

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"

	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/stream"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/tracer"
	"github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/utils"
	sutils "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/session/utils"
	compability "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/workflow/components/ability"
	workflow "github.com/uapclaw/uapclaw-go/internal/openjiuwen/core/workflow/workflowconfig"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Vertex 工作流图节点执行器。
// Python: Vertex(AsyncAtomicNode, StreamConsumer)（vertex.py:65），单结构体承载
// 双基类（AsyncAtomicNode 由 8.10 AtomicNode 承载，于批入编排任务接入；
// StreamConsumer 三方法由本类型实现）。
type Vertex struct {
	// AtomicNode 承载 Python AsyncAtomicNode 基类（8.10，构造期注入 _atomic_invoke 回调）
	AtomicNode
	// executable 可执行体（Python: self._executable: Executable；GeneralExecutor
	// 对齐 Python GeneralExecutor = Executable[dict, dict] 的默认实例化形状）
	executable GeneralExecutor
	// nodeID 节点标识（Python: self._node_id）
	nodeID string
	// ctx 执行上下文（Python: self._context，init 时从 kwargs 注入）
	ctx context.Context
	// streamCalledTimeout 流调用等待超时秒数（Python: self._stream_called_timeout = 10）
	streamCalledTimeout float64
	// nodeSession 节点级会话（Python: self._session: NodeSession；Go 侧经
	// session.NewWorkflowVertexSession 构造代理，偏差 D-2 见 spec §4.2）
	nodeSession session.WorkflowVertexSession
	// streamDone 当轮流侧结果容器（Python: self._stream_done asyncio.Future，
	// vertex.py:74；每轮 StreamCall 新建，缓冲 1 对齐 set_result 不阻塞语义）
	streamDone chan error
	// streamDoneSet 结果已送达标记（Python Future.done()，vertex.py:817/879 探测用；
	// 不消费 channel 的非破坏性 done 判定）
	streamDoneSet atomic.Bool
	// callCount 批入轮次计数（Python: self._call_count，单写方=批侧）
	callCount atomic.Int64
	// streamCallCount 流入轮次计数（Python: self._stream_call_count，单写方=流侧）
	streamCallCount atomic.Int64
	// isEndNode 是否 End 节点（Python: self.is_end_node）
	isEndNode bool
	// isStarted trace 去重：一轮只发一次 component_begin（Python: asyncio.Event）
	isStarted atomic.Bool
	// isCallStarted 批入侧已记输入标记（Python: asyncio.Event；trace 合并发送协议）
	isCallStarted atomic.Bool
	// nodeConfig 节点规格（Python: self._node_config；NodeSession.NodeConfig() any 断言）
	nodeConfig *workflow.NodeSpec
	// componentAbility 组件能力面（Python: self._component_ability）
	componentAbility []compability.ComponentAbility
	// hasStreamCall 是否含流能力（Python: self._has_stream_call）
	hasStreamCall bool
	// hasCall 是否含批能力（Python: self._has_call）
	hasCall bool
	// sourceID 源标识（Python: self._source_id）
	sourceID []string
	// isFirstInit 是否首次初始化（Python: self._is_first_init；首初始化才记 debug 日志）
	isFirstInit atomic.Bool
}

// ──────────────────────────── 常量 ────────────────────────────

// subWorkflowComponent 子工作流组件类型标签（Python vertex.py:30 模块级常量）
const subWorkflowComponent = "sub_workflow"

// logComponent 日志组件（规则 3：agentcore 域）
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewVertex 创建 Vertex（Python __init__，vertex.py:67-85 字段一比一）
func NewVertex(nodeID string, executable GeneralExecutor) *Vertex {
	v := &Vertex{
		nodeID:              nodeID,
		executable:          executable,
		streamCalledTimeout: 10,
		streamDone:          make(chan error, 1),
	}
	// Python AsyncAtomicNode 基类继承 → Go 构造期注入 _atomic_invoke 回调（8.10 D1 承载）
	v.AtomicNode = NewAtomicNode(v.atomicInvoke)
	v.isFirstInit.Store(true)
	return v
}

// IsDone 消费者是否完成（Python is_done，vertex.py:711-713：call==stream 或
// call==stream+1，即两半轮次对齐或批入多一轮的纯批入节点）。只读对账，跨 goroutine
// 一致性由引擎屏障定序保证（spec §1 不变式）。
func (v *Vertex) IsDone() bool {
	c := v.callCount.Load()
	s := v.streamCallCount.Load()
	return c == s || c == s+1
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// streamCalled 本轮流侧是否已先于批侧启动（Python _stream_called，vertex.py:715-716）
func (v *Vertex) streamCalled() bool {
	return v.streamCallCount.Load() == v.callCount.Load()+1
}

// collectRefSourceIDs 递归收集 schema 中 ${comp_id...} 引用的组件 ID 集合。
// Python: _collect_ref_source_ids（vertex.py:33-62，模块级函数 → 包级函数）
func collectRefSourceIDs(schema map[string]any) map[string]bool {
	ids := map[string]bool{}
	var walk func(obj any)
	walk = func(obj any) {
		switch val := obj.(type) {
		case map[string]any:
			for _, item := range val {
				walk(item)
			}
		case []any:
			for _, item := range val {
				walk(item)
			}
		case string:
			if utils.IsRefPath(val) {
				origin := utils.ExtractOriginKey(val)
				if origin != "" {
					ids[strings.SplitN(origin, ".", 2)[0]] = true
				}
			}
		}
	}
	walk(schema)
	return ids
}

// ShouldHandleMessage 是否可处理流式消息（Python should_handle_message，vertex.py:805-807）
func (v *Vertex) ShouldHandleMessage() bool {
	return len(v.streamAbilities()) > 0
}

// streamAbilities 流入能力过滤（Python _stream_abilities，vertex.py:800-803：
// 仅 COLLECT/TRANSFORM）
func (v *Vertex) streamAbilities() []compability.ComponentAbility {
	var out []compability.ComponentAbility
	for _, a := range v.componentAbility {
		if a == compability.ComponentAbilityCollect || a == compability.ComponentAbilityTransform {
			out = append(out, a)
		}
	}
	return out
}

// executableTypeName 可执行体类型名（Python type(self._executable).__name__，
// vertex.py:88；Go 经 reflect 取类型名）
func executableTypeName(exe GeneralExecutor) string {
	if exe == nil {
		return ""
	}
	t := reflect.TypeOf(exe)
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	return t.Name()
}

// anyToFloat64 env 值转浮点（Python get_env 返回数值；非数值回退 0，由调用方
// 的 >0 判定承担不超时语义）
func anyToFloat64(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	default:
		return 0
	}
}

// resetStreamDone 重建当轮流结果容器（Python reset 的 cancel→新建 Future 语义，
// vertex.py:885-889；Go 无等待方时取消无副作用，直接新建）
func (v *Vertex) resetStreamDone() {
	v.streamDone = make(chan error, 1)
	v.streamDoneSet.Store(false)
}

// Init 初始化节点会话与能力面（Python init，vertex.py:87-112；步骤一比一，返回 bool）
func (v *Vertex) Init(sess session.BaseSession, kwargs map[string]any) bool {
	// ① NodeSession 构造（vertex.py:88-89）：nodeType=可执行体类型名、skipTrace 透传
	v.nodeSession = session.NewWorkflowVertexSession(sess, v.nodeID, executableTypeName(v.executable), v.executable.SkipTrace())
	// ② context 注入（vertex.py:90）
	v.ctx, _ = kwargs["context"].(context.Context)
	// ③ 流调用超时（vertex.py:91）
	v.streamCalledTimeout = anyToFloat64(sess.Config().GetEnv(constants.CompStreamCallTimeoutKey))
	// ④⑤⑥⑦ node_config 读取与能力派生（vertex.py:92-97，✅ 8.15 D-3 内联回收）
	v.nodeConfig = v.nodeSession.NodeConfig()
	// ⑤ 能力面（vertex.py:93-95）：空则默认 [INVOKE]
	if v.nodeConfig != nil && len(v.nodeConfig.Abilities) > 0 {
		v.componentAbility = v.nodeConfig.Abilities
	} else {
		v.componentAbility = []compability.ComponentAbility{compability.ComponentAbilityInvoke}
	}
	// ⑥⑦ hasStreamCall/hasCall（vertex.py:96-97）
	v.hasStreamCall = len(v.streamAbilities()) > 0
	v.hasCall = len(v.componentAbility) > len(v.streamAbilities())
	// ⑧ 首初始化 debug 日志（vertex.py:99-105）：GRAPH_VERTEX_INIT + node_abilities
	if v.isFirstInit.CompareAndSwap(true, false) {
		abilities := make([]string, 0, len(v.componentAbility))
		for _, a := range v.componentAbility {
			abilities = append(abilities, a.Name())
		}
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_INIT").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Strs("node_abilities", abilities).
			Msgf("Initialized node [%s], abilities is %v", v.nodeID, abilities)
	}
	// ⑨ has_stream_inputs 且可执行体支持 set_mix → 调用（vertex.py:106-110，
	// Python hasattr 鸭子探测 → Go 接口断言）
	hasStreamInputs := v.hasStreamCall && v.nodeConfig != nil &&
		v.nodeConfig.StreamIOConfigs != nil && v.nodeConfig.StreamIOConfigs.InputsSchema != nil
	if hasStreamInputs {
		if sm, ok := v.executable.(interface{ SetMix() }); ok {
			sm.SetMix()
		}
	}
	// ⑩ clear is_started（vertex.py:111）并返回 true（vertex.py:112）
	v.isStarted.Store(false)
	return true
}

// ──────────────────────────── trace 埋点方法（Python __trace_*__ 族，vertex.py:809-880） ────────────────────────────

// traceEnable 埋点是否可用（Python _trace_enable，vertex.py:809-811）
func (v *Vertex) traceEnable() bool {
	return v.nodeSession.Tracer() != nil && v.nodeSession.Tracer().WorkflowHandlerValid() &&
		!v.executable.SkipTrace()
}

// traceComponentInputs 组件输入埋点（Python __trace_component_inputs__，vertex.py:813-820）：
// 置位 isCallStarted；混合节点流未完成时 send=false（合并发送协议 spec §4.3-3）
func (v *Vertex) traceComponentInputs(ctx context.Context, inputs any) {
	if !v.traceEnable() {
		return
	}
	v.isCallStarted.Store(true)
	needSend := !v.hasStreamCall || v.streamDoneSet.Load()
	tracer.TracerWorkflowUtils{}.TraceComponentInputs(ctx, v.nodeSession, inputs, needSend)
	if v.executable.ComponentType() == subWorkflowComponent {
		v.nodeSession.Tracer().RegisterWorkflowSpanManager(v.nodeSession.ExecutableID())
	}
}

// traceComponentOutputs 组件输出埋点（Python __trace_component_outputs__，vertex.py:822-825）
func (v *Vertex) traceComponentOutputs(ctx context.Context, outputs any) {
	if !v.traceEnable() {
		return
	}
	tracer.TracerWorkflowUtils{}.TraceComponentOutputs(ctx, v.nodeSession, outputs)
}

// traceComponentBegin 组件开始埋点（Python __trace_component_begin__，vertex.py:827-846）：
// isStarted 去重一轮一次；trace_metadata 钩子经接口断言复刻 hasattr 探测（偏差 D-4：
// 钩子返回值无法经现有 TraceComponentBegin 签名透传，仅保留执行与失败告警语义）
func (v *Vertex) traceComponentBegin(ctx context.Context) {
	if !v.traceEnable() {
		return
	}
	if !v.isStarted.CompareAndSwap(false, true) {
		return
	}
	// 组件可导出静态事实（top_k、数据源 id 等）到 trace span；基类钩子默认 None
	var extraMetadata map[string]any
	if hook, ok := v.executable.(interface{ TraceMetadata() map[string]any }); ok {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).
						Str("event_type", "GRAPH_VERTEX_ABILITY_ERROR").
						Str("graph_id", v.nodeSession.WorkflowID()).
						Str("node_id", v.nodeID).
						Any("exception", r).
						Msgf("trace_metadata() failed for node [%s]", v.nodeID)
				}
			}()
			extraMetadata = hook.TraceMetadata()
		}()
	}
	// Python: extra_metadata 透传给 trace_component_begin（workflow_tracer.py:61），
	// D-4 已回收：TracerWorkflowUtils.TraceComponentBegin 已增加 extraMetadata 形参
	tracer.TracerWorkflowUtils{}.TraceComponentBegin(ctx, v.nodeSession, nil, extraMetadata)
}

// traceComponentDone 组件完成埋点（Python __trace_component_done__，vertex.py:848-851）
func (v *Vertex) traceComponentDone(ctx context.Context) {
	if !v.traceEnable() {
		return
	}
	tracer.TracerWorkflowUtils{}.TraceComponentDone(ctx, v.nodeSession)
}

// traceComponentStreamOutput 组件流式输出埋点（Python __trace_component_stream_output__，vertex.py:853-856）
func (v *Vertex) traceComponentStreamOutput(ctx context.Context, chunk any) {
	if !v.traceEnable() {
		return
	}
	tracer.TracerWorkflowUtils{}.TraceComponentStreamOutput(ctx, v.nodeSession, chunk)
}

// traceError 错误埋点（Python __trace_error__，vertex.py:858-861）
func (v *Vertex) traceError(ctx context.Context, err error) {
	if !v.traceEnable() {
		return
	}
	tracer.TracerWorkflowUtils{}.TraceError(ctx, v.nodeSession, err)
}

// traceInnerError 重试内层错误埋点（Python __trace_inner_error__，vertex.py:863-874）：
// BaseError 取 code/message，否则回退组件执行错误码 + str(err)
func (v *Vertex) traceInnerError(ctx context.Context, err error) {
	if !v.traceEnable() {
		return
	}
	innerErrorInfo := map[string]any{"error_code": exception.StatusWorkflowComponentExecutionError.Code(), "message": err.Error()}
	var be *exception.BaseError
	if errors.As(err, &be) {
		innerErrorInfo = map[string]any{"error_code": be.Code(), "message": be.Message()}
	}
	tracer.TracerWorkflowUtils{}.Trace(ctx, v.nodeSession, map[string]any{
		"inner_error":  innerErrorInfo,
		"current_time": time.Now().UTC().Format(time.RFC3339Nano),
	})
}

// traceComponentStreamInputSend 流输入埋点补发（Python __trace_component_stream_input_send__，
// vertex.py:876-880）：无批侧或批侧已置位时补发 send=true 帧
func (v *Vertex) traceComponentStreamInputSend(ctx context.Context) {
	if !v.traceEnable() {
		return
	}
	if !v.hasCall || v.isCallStarted.Load() {
		tracer.TracerWorkflowUtils{}.TraceComponentStreamInput(ctx, v.nodeSession, map[string]any{}, true)
	}
}

// ──────────────────────────── 能力执行与重试（Python _run_executable(+with_retry)/_build_timeout_error/_run_error_recovery，vertex.py:114-362） ────────────────────────────

// setEvent 置位一次性信号（Python asyncio.Event.set 的幂等语义 → close channel）
func setEvent(ev chan struct{}) {
	if ev == nil {
		return
	}
	defer func() { _ = recover() }() // 已关闭时幂等返回
	select {
	case <-ev:
	default:
		close(ev)
	}
}

// triggerUntil 触发自定义事件并返回首个非 nil 结果
// （Python CallbackFramework.trigger_until(event, predicate=r is not None, **kwargs)
// 的语义组合：Go TriggerCustom 收集全部结果，此处取首个非 nil）
func triggerUntil(ctx context.Context, event string, data map[string]any) (result any) {
	defer func() {
		if r := recover(); r != nil {
			result = nil // handler 异常 → 视为无命中（Python except 分支）
		}
	}()
	for _, r := range callback.Trigger(ctx, event, data) {
		if r != nil {
			return r
		}
	}
	return nil
}

// runExecutable 执行单个能力的策略分派（Python _run_executable，vertex.py:114-236 步骤一比一）
func (v *Vertex) runExecutable(ctx context.Context, ability compability.ComponentAbility,
	isSubgraph bool, config any, event chan struct{}) (bool, error) {
	// 策略体共用错误通道（Python async 函数的异常沿 await 传播 → Go 以 error 返回值承载，
	// 分类处理在 strategyError 分类段）
	type strategyResult struct{ err error }
	invokeStrategy := func() error {
		// Python invoke_strategy（vertex.py:129-146）
		batchInputs := v.preInvoke(ctx)
		v.debugAbilityRunning(ability, "inputs", batchInputs, isSubgraph)
		if isSubgraph {
			// 子图封装（vertex.py:136-137）
			batchInputs = map[string]any{InputsKey: batchInputs, ConfigKey: config}
		}
		results, err := v.executable.OnInvoke(ctx, toMapAny(batchInputs), v.nodeSession,
			map[string]any{"context": v.ctx})
		if err != nil {
			return err
		}
		_, err = v.postInvoke(ctx, results)
		if err != nil {
			return err
		}
		v.debugAbilityRunning(ability, "outputs", results, isSubgraph)
		return nil
	}
	streamStrategy := func() error {
		// Python stream_strategy（vertex.py:148-158）
		batchInputs := v.preInvoke(ctx)
		v.debugAbilityRunning(ability, "inputs", batchInputs, isSubgraph)
		if isSubgraph {
			batchInputs = map[string]any{InputsKey: batchInputs, ConfigKey: config}
		}
		resultsIter, err := v.executable.OnStream(ctx, toMapAny(batchInputs), v.nodeSession,
			map[string]any{"context": v.ctx})
		if err != nil {
			return err
		}
		v.postStream(ctx, resultsIter, ability)
		return nil
	}
	collectStrategy := func() error {
		// Python collect_strategy（vertex.py:160-175）：preStream → set_event →
		// End 节点未执行分支清洗 → on_collect → post_invoke
		collectIter, err := v.preStream(ctx, ability)
		if err != nil {
			return err
		}
		setEvent(event)
		if v.isEndNode {
			var streamSchema map[string]any
			if v.nodeConfig != nil && v.nodeConfig.StreamIOConfigs != nil {
				streamSchema, _ = v.nodeConfig.StreamIOConfigs.InputsSchema.(map[string]any)
			}
			if len(streamSchema) > 0 {
				collectIter = v.sanitizeUnexecutedBranchInputs(ctx, collectIter, streamSchema)
			}
		}
		// 单元素信封：channel 仅作传输壳，dict-of-StreamReader 形状对齐 Python collect_iter
		envelope := make(chan map[string]any, 1)
		envelope <- collectIter
		close(envelope)
		batchOutput, err := v.executable.OnCollect(ctx, envelope, v.nodeSession,
			map[string]any{"context": v.ctx})
		if err != nil {
			return err
		}
		_, err = v.postInvoke(ctx, batchOutput)
		if err != nil {
			return err
		}
		v.debugAbilityRunning(ability, "outputs", batchOutput, isSubgraph)
		return nil
	}
	transformStrategy := func() error {
		// Python transform_strategy（vertex.py:177-188）
		transformIter, err := v.preStream(ctx, ability)
		if err != nil {
			return err
		}
		setEvent(event)
		if v.isEndNode {
			var streamSchema map[string]any
			if v.nodeConfig != nil && v.nodeConfig.StreamIOConfigs != nil {
				streamSchema, _ = v.nodeConfig.StreamIOConfigs.InputsSchema.(map[string]any)
			}
			if len(streamSchema) > 0 {
				transformIter = v.sanitizeUnexecutedBranchInputs(ctx, transformIter, streamSchema)
			}
		}
		envelope := make(chan map[string]any, 1)
		envelope <- transformIter
		close(envelope)
		outputIter, err := v.executable.OnTransform(ctx, envelope, v.nodeSession,
			map[string]any{"context": v.ctx})
		if err != nil {
			return err
		}
		v.postStream(ctx, outputIter, ability)
		return nil
	}
	// 策略表（vertex.py:190-195）
	strategies := map[compability.ComponentAbility]func() error{
		compability.ComponentAbilityInvoke:    invokeStrategy,
		compability.ComponentAbilityStream:    streamStrategy,
		compability.ComponentAbilityCollect:   collectStrategy,
		compability.ComponentAbilityTransform: transformStrategy,
	}
	// 执行策略（vertex.py:198-199）
	strategy, found := strategies[ability]
	if !found {
		return false, nil // Python strategy() 为 None 会 TypeError；Go 侧能力面由 init 保证，防御分支
	}
	// mark_node_executed + ABILITY_START 日志（vertex.py:117-122）
	v.markNodeExecuted()
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_VERTEX_ABILITY_START").
		Str("graph_id", v.nodeSession.WorkflowID()).
		Str("node_id", v.nodeID).
		Str("ability", ability.Name()).
		Msgf("Begin to call node [%s] ability [%s]", v.nodeID, ability.Name())
	strategyErr := strategy()
	// finally：event 兜底置位（vertex.py:234-236）
	defer setEvent(event)
	if strategyErr == nil {
		// 成功（vertex.py:200-205）
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_ABILITY_END").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Str("ability", ability.Name()).
			Msgf("Succeed to call node [%s] ability [%s]", v.nodeID, ability.Name())
		return true, nil
	}
	// 异常分类（vertex.py:206-233）
	var gi *pregel.GraphInterrupt
	if errors.As(strategyErr, &gi) {
		logger.Info(logComponent).
			Str("event_type", "GRAPH_VERTEX_ABILITY_END").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Str("ability", ability.Name()).
			Msgf("Interrupt to call node [%s] ability [%s]", v.nodeID, ability.Name())
		return false, strategyErr
	}
	var be *exception.BaseError
	if errors.As(strategyErr, &be) {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_VERTEX_ABILITY_ERROR").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Str("ability", ability.Name()).
			Int("error_code", be.Code()).
			Str("error_msg", be.Message()).
			Err(strategyErr).
			Msgf("Failed to call node [%s] ability [%s]", v.nodeID, ability.Name())
		return false, strategyErr
	}
	logger.Error(logComponent).
		Str("event_type", "GRAPH_VERTEX_ABILITY_ERROR").
		Str("graph_id", v.nodeSession.WorkflowID()).
		Str("node_id", v.nodeID).
		Err(strategyErr).
		Msgf("Failed to call node [%s]'s '%s'", v.nodeID, ability.Name())
	wrapped := exception.BuildError(exception.StatusWorkflowComponentExecutionError,
		exception.WithCause(strategyErr),
		exception.WithParam("ability", ability.Name()),
		exception.WithParam("comp", v.nodeID),
		exception.WithParam("reason", strategyErr),
		exception.WithParam("workflow", v.nodeSession.WorkflowID()))
	return false, wrapped
}

// debugAbilityRunning ABILITY_RUNNING 调试日志（vertex.py:131-135/142-146/171-175 公共字段）
func (v *Vertex) debugAbilityRunning(ability compability.ComponentAbility, phase string, payload any, isSubgraph bool) {
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_VERTEX_ABILITY_RUNNING").
		Str("graph_id", v.nodeSession.WorkflowID()).
		Str("node_id", v.nodeID).
		Str("ability", ability.Name()).
		Any(phase, payload).
		Bool("is_subgraph", isSubgraph).
		Msgf("%s for [%s] ability [%s]", map[string]string{
			"inputs": "Prepare inputs", "outputs": "Post-process results",
		}[phase], v.nodeID, ability.Name())
}

// secondsToDuration 秒转时长（<=0 返回 0 表示不限时）
func secondsToDuration(sec float64) time.Duration {
	if sec <= 0 {
		return 0
	}
	return time.Duration(sec * float64(time.Second))
}

// toMapAny 输入形状收窄（GeneralExecutor I=map[string]any；非 map 形状以空 map 承载）
func toMapAny(v any) map[string]any {
	if m, ok := v.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

// runExecutableWithRetry 带重试与超时的能力执行（Python _run_executable_with_retry，
// vertex.py:238-287 步骤一比一）
func (v *Vertex) runExecutableWithRetry(ctx context.Context, ability compability.ComponentAbility,
	isSubgraph bool, config any, event chan struct{}) (bool, error) {
	// ① 重试与超时配置（vertex.py:240-241；Python getattr(timeout, -1.0) 默认无超时，
	// Go NodeSpec.Timeout<=0 同义）
	maxRetries := 0
	timeout := -1.0
	if v.nodeConfig != nil {
		maxRetries = v.nodeConfig.MaxRetries
		if v.nodeConfig.Timeout > 0 {
			timeout = v.nodeConfig.Timeout
		}
	}
	// ② 负重试数 → 直接执行（vertex.py:243-244）
	if maxRetries < 0 {
		return v.runExecutable(ctx, ability, isSubgraph, config, event)
	}
	// ③ 重试循环（vertex.py:246-285）
	for attempt := 0; attempt <= maxRetries; attempt++ {
		var runErr error
		if timeout < 0 {
			_, runErr = v.runExecutable(ctx, ability, isSubgraph, config, event)
		} else {
			// Python asyncio.wait_for 超时会取消被包裹协程（CancelledError 传播）
			// → Go context.WithTimeout 真取消（非偏差，见 spec §4.2 修正记录）
			runCtx, cancel := context.WithTimeout(ctx, secondsToDuration(timeout))
			_, runErr = v.runExecutable(runCtx, ability, isSubgraph, config, event)
			cancel()
			// 超时分支（vertex.py:256-272）：asyncio.TimeoutError → 包 BaseError
			if errors.Is(runErr, context.DeadlineExceeded) {
				wrapped := v.buildTimeoutError(ctx, runErr, timeout)
				if attempt < maxRetries {
					logger.Warn(logComponent).
						Str("event_type", "GRAPH_VERTEX_ABILITY_ERROR").
						Str("graph_id", v.nodeSession.WorkflowID()).
						Str("node_id", v.nodeID).
						Str("ability", ability.Name()).
						Int("attempt", attempt+1).
						Int("max_retries", maxRetries+1).
						Err(runErr).
						Msgf("Node [%s] ability [%s] timed out on attempt %d/%d, will retry",
							v.nodeID, ability.Name(), attempt+1, maxRetries+1)
					v.traceInnerError(ctx, runErr)
					continue
				}
				// 重试耗尽 → 错误恢复（vertex.py:271-272）
				return v.runErrorRecovery(ctx, ability, wrapped)
			}
		}
		if runErr == nil {
			return true, nil
		}
		// 中断直接上抛（vertex.py:254-255）
		var gi *pregel.GraphInterrupt
		if errors.As(runErr, &gi) {
			return false, runErr
		}
		// 外部取消（非超时）→ 对齐 Python CancelledError 不被捕获直接传播
		if errors.Is(runErr, context.Canceled) && ctx.Err() != nil {
			return false, runErr
		}
		// 通用错误重试（vertex.py:273-285）
		if attempt < maxRetries {
			logger.Warn(logComponent).
				Str("event_type", "GRAPH_VERTEX_ABILITY_ERROR").
				Str("graph_id", v.nodeSession.WorkflowID()).
				Str("node_id", v.nodeID).
				Str("ability", ability.Name()).
				Int("attempt", attempt+1).
				Int("max_retries", maxRetries+1).
				Err(runErr).
				Msgf("Node [%s] ability [%s] failed on attempt %d/%d, will retry",
					v.nodeID, ability.Name(), attempt+1, maxRetries+1)
			v.traceInnerError(ctx, runErr)
			continue
		}
		// 重试耗尽 → 错误恢复（vertex.py:284-285）
		return v.runErrorRecovery(ctx, ability, runErr)
	}
	return false, nil // Python:287 兜底返回 False
}

// buildTimeoutError 超时错误构建（Python _build_timeout_error，vertex.py:289-330）：
// 经 CallbackFramework component_timeout_error 事件，命中 handler 用其返回的
// BaseError；未命中/框架异常回落默认超时错误
func (v *Vertex) buildTimeoutError(ctx context.Context, cause error, timeout float64) error {
	defer func() {
		if r := recover(); r != nil {
			logger.Error(logComponent).
				Str("event_type", "GRAPH_VERTEX_ABILITY_ERROR").
				Str("graph_id", v.nodeSession.WorkflowID()).
				Str("node_id", v.nodeID).
				Any("exception", r).
				Msgf("Failed to use CallbackFramework to build timeout error for node [%s]", v.nodeID)
		}
	}()
	var exceptionConfig any
	if v.nodeConfig != nil && v.nodeConfig.ExceptionConfig != nil {
		exceptionConfig = v.nodeConfig.ExceptionConfig
	}
	if result := triggerUntil(ctx, "component_timeout_error", map[string]any{
		"cause":            cause,
		"timeout":          timeout,
		"node_id":          v.nodeID,
		"exception_config": exceptionConfig,
		"session":          v.nodeSession,
	}); result != nil {
		if err, ok := result.(error); ok {
			return err
		}
	}
	return exception.BuildError(exception.StatusWorkflowExecutionTimeout,
		exception.WithCause(cause),
		exception.WithParam("timeout", timeout),
		exception.WithParam("node_id", v.nodeID))
}

// runErrorRecovery 重试耗尽后的错误恢复（Python _run_error_recovery，vertex.py:332-362）：
// 流式能力不可恢复；INVOKE/COLLECT 经 component_error_recovery 事件，
// 命中则 postInvoke 恢复数据并返回成功
func (v *Vertex) runErrorRecovery(ctx context.Context, ability compability.ComponentAbility, err error) (bool, error) {
	// ① 流式输出不可恢复（vertex.py:335-337）
	if ability != compability.ComponentAbilityInvoke && ability != compability.ComponentAbilityCollect {
		return false, err
	}
	// ② 节点异常配置（vertex.py:339-340）
	var exceptionConfig any
	if v.nodeConfig != nil && v.nodeConfig.ExceptionConfig != nil {
		exceptionConfig = v.nodeConfig.ExceptionConfig
	}
	// ③ 事件触发（vertex.py:343-358）；handler 异常 → 视为中断（triggerUntil 内 recover）
	if result := triggerUntil(ctx, "component_error_recovery", map[string]any{
		"error":            err,
		"session":          v.nodeSession,
		"node_id":          v.nodeID,
		"ability":          ability,
		"exception_config": exceptionConfig,
	}); result != nil {
		if recoveryData, ok := result.(map[string]any); ok {
			if _, perr := v.postInvoke(ctx, recoveryData); perr == nil {
				return true, nil
			}
		}
	}
	// ④ 无命中/handler None → 中断上抛（vertex.py:362）
	return false, err
}

// ──────────────────────────── 流入侧执行（Python stream_call，vertex.py:718-798） ────────────────────────────

// 编译期断言：Vertex 实现 StreamConsumer（Python 双基类之一的 StreamConsumer ABC）
var _ stream_actor.StreamConsumer = (*Vertex)(nil)

// SetEndNode 装配期 End 节点标记（Python graph.py 构图期设置 is_end_node 的
// 对应物；调用方为 8.1 PregelGraph.Compile）
func (v *Vertex) SetEndNode(isEnd bool) {
	v.isEndNode = isEnd
}

// RegisterStreamConsumer 注册到流式节点注册表（Python graph.py 构图期
// add_stream_consumer(vertex, node_id) 的薄封装；实际调用方为 8.1 PregelGraph）
func (v *Vertex) RegisterStreamConsumer(g *stream_actor.StreamGraph) {
	g.AddStreamConsumer(v, v.nodeID)
}

// graphID 安全取工作流 ID（nodeSession 未初始化时返回空串，日志用）
func (v *Vertex) graphID() string {
	if v.nodeSession == nil {
		return ""
	}
	return v.nodeSession.WorkflowID()
}

// deliverStreamDone 送达当轮流结果（Python _stream_done.set_result，缓冲 1 不阻塞）
func (v *Vertex) deliverStreamDone(err error) {
	select {
	case v.streamDone <- err:
	default:
	}
	v.streamDoneSet.Store(true)
}

// StreamCall 流入侧执行入口（Python stream_call，vertex.py:718-798 步骤一比一；
// 实现 StreamConsumer 接口）
func (v *Vertex) StreamCall(ctx context.Context, event chan struct{}, doneCallback func(error)) {
	// 开始日志（vertex.py:719-721）
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_VERTEX_STREAM_CALL_START").
		Str("graph_id", v.graphID()).
		Str("node_id", v.nodeID).
		Msgf("Begin to call stream-in node [%s]", v.nodeID)
	// ① 计数与当轮容器（vertex.py:722-723）
	v.streamCallCount.Add(1)
	v.resetStreamDone()
	// ② 未初始化分支（vertex.py:725-735）
	if v.nodeSession == nil || v.nodeSession.ActorManager() == nil {
		err := exception.BuildError(exception.StatusGraphVertexStreamCallError,
			exception.WithParam("reason", "queue manager is not initialized"),
			exception.WithParam("node_id", v.nodeID))
		v.deliverStreamDone(err)
		logger.Warn(logComponent).
			Str("event_type", "GRAPH_VERTEX_STREAM_CALL_ERROR").
			Str("graph_id", v.graphID()).
			Str("node_id", v.nodeID).
			Err(err).
			Msgf("Failed to call stream-in node [%s, actor_manager is missing", v.nodeID)
		if doneCallback != nil {
			doneCallback(err)
		}
		return
	}
	var errorResult error
	// ③ 逐能力启动：spawn → 能力级 event 就位 → 下一能力（vertex.py:738-745）
	abilities := v.streamAbilities()
	wg := &sync.WaitGroup{}
	errCh := make(chan error, len(abilities)+1)
spawnLoop:
	for _, ability := range abilities {
		e := make(chan struct{})
		wg.Add(1)
		go func(ab compability.ComponentAbility) {
			defer wg.Done()
			// Python create_task(self._run_executable_with_retry(ability, event=e))：
			// is_subgraph=False、config=None（vertex.py:742）
			if _, err := v.runExecutableWithRetry(ctx, ab, false, nil, e); err != nil {
				errCh <- err
			}
		}(ability)
		select {
		case <-e: // await e.wait()：策略在 _pre_stream 注册后置位
		case <-ctx.Done():
			break spawnLoop
		}
	}
	// ④ 全部就位放行（vertex.py:745）
	setEvent(event)
	// ⑤ 收割（vertex.py:746-749 gather return_exceptions=True → 首个异常上抛）
	finished := make(chan struct{})
	go func() { wg.Wait(); close(finished) }()
	select {
	case <-finished:
		close(errCh)
		for err := range errCh {
			if errorResult == nil {
				errorResult = err
			}
		}
		if errorResult == nil {
			// 成功（vertex.py:750-752）
			logger.Debug(logComponent).
				Str("event_type", "GRAPH_VERTEX_STREAM_CALL_END").
				Str("graph_id", v.graphID()).
				Str("node_id", v.nodeID).
				Msgf("Succeed to call stream-in node [%s]", v.nodeID)
		} else {
			// 失败（vertex.py:787-795）：error 日志 + error_callback
			logger.Error(logComponent).
				Str("event_type", "GRAPH_VERTEX_STREAM_CALL_ERROR").
				Str("graph_id", v.graphID()).
				Str("node_id", v.nodeID).
				Err(errorResult).
				Msgf("Failed to call stream-in node [%s]", v.nodeID)
			if doneCallback != nil {
				doneCallback(errorResult)
			}
		}
	case <-ctx.Done():
		// ⑥ 取消分类收割（vertex.py:753-786 CancelledError 分支）：
		// ctx 级联取消在途能力（对齐 task.cancel()），不调 error_callback、
		// error 保持 nil（对齐 Python CancelledError 不进入 error 通道）
		wg.Wait()
		close(errCh)
		errCount := 0
		for range errCh {
			errCount++
		}
		logger.Warn(logComponent).
			Str("event_type", "GRAPH_VERTEX_STREAM_CALL_ERROR").
			Str("graph_id", v.graphID()).
			Str("node_id", v.nodeID).
			Int("cancelled", len(abilities)).
			Int("finished", len(abilities)-errCount).
			Int("error", errCount).
			Msgf("Cancel to call stream-in node [%s]", v.nodeID)
	}
	// ⑦ finally（vertex.py:796-798）：送达结果 + 流输入埋点补发
	v.deliverStreamDone(errorResult)
	v.traceComponentStreamInputSend(ctx)
}

// ──────────────────────────── 批入编排（Python __call__/call/_atomic_invoke，vertex.py:364-419,660-716） ────────────────────────────

// callInternal 执行批入能力与流等待（Python call，vertex.py:660-709 步骤①-⑥一比一）
func (v *Vertex) callInternal(ctx context.Context, config any) error {
	// ① 未初始化检查（vertex.py:661-664）
	if v.nodeSession == nil || v.executable == nil {
		return exception.BuildError(exception.StatusGraphVertexExecutionError,
			exception.WithParam("reason", "node is not initialized"),
			exception.WithParam("node_id", v.nodeID))
	}
	// ② 批入能力顺序执行（vertex.py:665-685）
	isSubgraph := v.executable.GraphInvoker()
	currentAbility := compability.ComponentAbility(-1)
	var callAbility []compability.ComponentAbility
	for _, ability := range v.componentAbility {
		if ability == compability.ComponentAbilityInvoke || ability == compability.ComponentAbilityStream {
			callAbility = append(callAbility, ability)
		}
	}
	for _, ability := range callAbility {
		currentAbility = ability
		if _, err := v.runExecutableWithRetry(ctx, ability, isSubgraph, config, nil); err != nil {
			// ExecutionError 分支（vertex.py:677-685）
			var be *exception.BaseError
			if errors.As(err, &be) && be.Category() == exception.ErrorCategoryExecution {
				var abilityName any
				if currentAbility >= 0 {
					abilityName = currentAbility.Name()
				}
				logger.Error(logComponent).
					Str("event_type", "GRAPH_VERTEX_CALL_ERROR").
					Str("node_id", v.nodeID).
					Str("error_message", err.Error()).
					Any("ability", abilityName).
					Msg("Node ability call failed")
			}
			return err
		}
	}
	if len(callAbility) == 0 {
		v.traceComponentBegin(ctx)
	}
	// ③ 流等待（vertex.py:687-704）
	if v.streamCalled() {
		streamTimeout := v.streamCalledTimeout
		if !(streamTimeout > 0) {
			streamTimeout = -1 // Python None → 无超时
		}
		var resultErr error
		if streamTimeout > 0 {
			select {
			case resultErr = <-v.streamDone:
			case <-time.After(secondsToDuration(streamTimeout)):
				// D-1：仅放弃等待，流侧任务继续（对齐 Python 等待 Future 结构）
				return exception.BuildError(exception.StatusGraphVertexStreamCallTimeout,
					exception.WithParam("timeout", streamTimeout),
					exception.WithParam("node_id", v.nodeID))
			}
		} else {
			resultErr = <-v.streamDone
		}
		if resultErr != nil {
			return resultErr
		}
	} else if v.hasStreamCall && !v.isEndNode {
		// 有流能力但整轮无流输入 → 报错（vertex.py:701-704）
		return exception.BuildError(exception.StatusGraphVertexStreamCallError,
			exception.WithParam("reason", "no stream data in"),
			exception.WithParam("node_id", v.nodeID))
	}
	// ④ 生产者收口登记（vertex.py:705-706）
	if am := v.nodeSession.ActorManager(); am != nil {
		am.MarkProducerDone(v.nodeID)
	}
	// ⑤ 流式模式收尾埋点（vertex.py:708-709）
	v.traceComponentDone(ctx)
	return nil
}

// Call 批入编排入口（Python __call__，vertex.py:364-416 步骤一比一）：
// 返回 {"source_node_id": [nodeID]} 喂给 GraphState.SourceNodeID（8.8 首个真实消费点）
func (v *Vertex) Call(ctx context.Context, stateVal any, config any) (map[string]any, error) {
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_VERTEX_CALL_START").
		Str("graph_id", v.nodeSession.WorkflowID()).
		Str("node_id", v.nodeID).
		Msgf("Begin to call batch-in node [%s]", v.nodeID)
	// finally（vertex.py:413-416）：计数与标志复位（成功/失败都执行）
	defer func() {
		v.callCount.Add(1)
		v.isStarted.Store(false)
		v.isCallStarted.Store(false)
	}()
	nodeOutput, callErr := func() (any, error) {
		// post_commit 分流（vertex.py:368-371）
		if v.executable.PostCommit() {
			if _, err := v.AtomicInvoke(ctx, map[string]any{
				"config": config, "session": v.nodeSession}); err != nil {
				return nil, err
			}
			return nil, nil
		}
		if err := v.callInternal(ctx, config); err != nil {
			return nil, err
		}
		return nil, nil
	}()
	if callErr == nil {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_CALL_END").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Msgf("Succeed to call batch-in node [%s]", v.nodeID)
		nodeOutput = v.nodeSession.State().Get(state.StringKey(v.nodeID))
		// NODE_EXECUTED 事件（vertex.py:377-383）
		abilityNames := make([]string, 0, len(v.componentAbility))
		for _, a := range v.componentAbility {
			abilityNames = append(abilityNames, a.Name())
		}
		callback.Trigger(ctx, string(callback.NodeExecuted), map[string]any{
			"node_id":  v.nodeID,
			"ability":  abilityNames,
			"graph_id": v.nodeSession.WorkflowID(),
			"inputs":   stateVal,
			"outputs":  nodeOutput,
		})
		return map[string]any{"source_node_id": []string{v.nodeID}}, nil
	}
	// 异常路径（vertex.py:386-412）
	if v.nodeSession.Tracer() != nil {
		v.traceError(ctx, callErr)
	}
	var be *exception.BaseError
	var gi *pregel.GraphInterrupt
	if errors.As(callErr, &be) {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_VERTEX_CALL_END").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Int("error_code", be.Code()).
			Str("error_msg", be.Message()).
			Err(callErr).
			Msgf("Failed to call batch-in node [%s]", v.nodeID)
	} else if errors.As(callErr, &gi) {
		logger.Info(logComponent).
			Str("event_type", "GRAPH_VERTEX_CALL_END").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Msgf("Interrupt to call batch-in node [%s]", v.nodeID)
	} else {
		logger.Error(logComponent).
			Str("event_type", "GRAPH_VERTEX_CALL_END").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Err(callErr).
			Msgf("Failed to call batch-in node [%s]", v.nodeID)
	}
	if !errors.As(callErr, &gi) {
		// 非中断 → NODE_ERROR 事件（vertex.py:405-411）
		callback.Trigger(ctx, string(callback.NodeError), map[string]any{
			"node_id":  v.nodeID,
			"graph_id": v.nodeSession.WorkflowID(),
			"error":    callErr,
		})
	}
	return nil, callErr
}

// atomicInvoke 原子回调（Python _atomic_invoke，vertex.py:418-419：转发 call(config)）
func (v *Vertex) atomicInvoke(ctx context.Context, kwargs map[string]any) (any, error) {
	return nil, v.callInternal(ctx, kwargs["config"])
}

// ──────────────────────────── 批入前/后处理（Python _pre_invoke/_post_invoke，vertex.py:421-567） ────────────────────────────

// preInvoke 批入取数（Python _pre_invoke，vertex.py:421-432 步骤一比一）
func (v *Vertex) preInvoke(ctx context.Context) map[string]any {
	// ① 组件开始埋点（vertex.py:422）
	v.traceComponentBegin(ctx)
	// ② 输入 schema（vertex.py:423）
	var inputsSchema any
	if v.nodeConfig != nil && v.nodeConfig.IOConfigs != nil {
		inputsSchema = v.nodeConfig.IOConfigs.InputsSchema
	}
	// ③ 双轨取数（vertex.py:424-428）：dict → GetInputs 轨；非 dict → transformer 轨。
	//    Transformer 契约已按 Python workflow_state.py:127（-> dict）收窄为 map[string]any
	//    （用户 2026-11-18 拍板；state.Transformer 类型注释载明回收条件）
	var inputs map[string]any
	if _, isDict := inputsSchema.(map[string]any); !isDict && inputsSchema != nil {
		if tr, ok := inputsSchema.(state.Transformer); ok {
			inputs = v.stateView().GetInputsByTransformer(tr)
		}
	} else if inputsSchema != nil {
		if d, ok := v.stateView().GetInputs(state.SchemaKey(inputsSchema.(map[string]any))).(map[string]any); ok {
			inputs = d
		}
	}
	// ④ End 节点批量输入清洗（vertex.py:429-430）
	if inputs != nil {
		if sd, ok := inputsSchema.(map[string]any); ok && v.isEndNode {
			inputs = v.sanitizeEndNodeInputs(ctx, inputs, sd)
		}
	}
	// ⑤ 输入埋点（vertex.py:431）
	v.traceComponentInputs(ctx, inputs)
	return inputs
}

// sanitizeEndNodeInputs End 节点批量输入清洗与混合模式等待调整
// （Python _sanitize_end_node_inputs，vertex.py:434-465 步骤一比一）
func (v *Vertex) sanitizeEndNodeInputs(ctx context.Context, inputs map[string]any, inputsSchema map[string]any) map[string]any {
	// ① template 鸭子探测（vertex.py:447-449，Python getattr(_template) → Go 接口断言）
	tpl := v.executableTemplate()
	if tpl == nil {
		return inputs
	}
	// ② 未执行分支清洗（vertex.py:450）
	inputs = v.sanitizeUnexecutedBranchInputs(ctx, inputs, inputsSchema)
	// ③ 非混合模式直接返回（vertex.py:451-452）
	if !(v.hasCall && v.hasStreamCall) {
		return inputs
	}
	// ④ 流式输入 schema（vertex.py:453-457）
	var streamSchema map[string]any
	if v.nodeConfig != nil && v.nodeConfig.StreamIOConfigs != nil {
		streamSchema, _ = v.nodeConfig.StreamIOConfigs.InputsSchema.(map[string]any)
	}
	if len(streamSchema) == 0 {
		return inputs
	}
	// ⑤ 流侧源组件全部未执行 → 数据源计数清零，模板跳过等待（vertex.py:460-464）
	streamSrcIDs := collectRefSourceIDs(streamSchema)
	allUnexecuted := len(streamSrcIDs) > 0
	for sid := range streamSrcIDs {
		if v.isComponentExecuted(sid) {
			allUnexecuted = false
			break
		}
	}
	if allUnexecuted {
		tpl.SetDataSourceCount(0)
	}
	return inputs
}

// templateLike 模板鸭子探测契约（Python getattr(exe, '_template') 的接口化）
type templateLike interface {
	// SetDataSourceCount 设置模板数据源计数（Python template.set_data_source_count）
	SetDataSourceCount(int)
}

// executableTemplate 取可执行体模板（nil 表示无模板，清洗短路）
func (v *Vertex) executableTemplate() templateLike {
	prov, ok := v.executable.(interface{ Template() templateLike })
	if !ok {
		return nil
	}
	return prov.Template()
}

// sanitizeUnexecutedBranchInputs 未执行分支输入清洗入口
// （Python _sanitize_unexecuted_branch_inputs，vertex.py:467-480）
func (v *Vertex) sanitizeUnexecutedBranchInputs(ctx context.Context, inputs map[string]any, inputsSchema map[string]any) map[string]any {
	v.sanitizeNode(ctx, inputs, inputsSchema)
	return inputs
}

// sanitizeNode 递归清洗 dict/list 嵌套结构（Python _sanitize_node，vertex.py:482-501）
func (v *Vertex) sanitizeNode(ctx context.Context, inputs, inputsSchema any) {
	switch in := inputs.(type) {
	case map[string]any:
		schema, ok := inputsSchema.(map[string]any)
		if !ok {
			return
		}
		for key, value := range in {
			schemaValue := schema[key]
			switch schemaValue.(type) {
			case map[string]any, []any:
				if _, isContainer := value.(map[string]any); isContainer {
					v.sanitizeNode(ctx, value, schemaValue)
					continue
				}
				if _, isContainer := value.([]any); isContainer {
					v.sanitizeNode(ctx, value, schemaValue)
					continue
				}
			}
			v.sanitizeLeaf(ctx, in, key, value, schemaValue)
		}
	case []any:
		schema, ok := inputsSchema.([]any)
		if !ok {
			return
		}
		n := len(in)
		if len(schema) < n {
			n = len(schema)
		}
		for i := 0; i < n; i++ {
			value := in[i]
			switch value.(type) {
			case map[string]any, []any:
				v.sanitizeNode(ctx, value, schema[i])
				continue
			}
			v.sanitizeLeaf(ctx, in, i, value, schema[i])
		}
	}
}

// sanitizeLeaf 叶子清洗：来自未执行分支的值替换为空串
// （Python _sanitize_leaf，vertex.py:503-519 步骤一比一）
func (v *Vertex) sanitizeLeaf(ctx context.Context, container any, key, value, refPath any) {
	// ① 非 None 且非流类型 → 原样（vertex.py:505-506）
	if value != nil && !isStreamReader(value) {
		return
	}
	// ② 引用路径判定（vertex.py:507-508）
	refStr, ok := refPath.(string)
	if !ok || !utils.IsRefPath(refStr) {
		return
	}
	// ③ origin 键（vertex.py:509-511）
	originKey := utils.ExtractOriginKey(refStr)
	if originKey == "" {
		return
	}
	compID := strings.SplitN(originKey, ".", 2)[0]
	// ④ 组件已执行 → 原样（vertex.py:512-514）
	if v.isComponentExecuted(compID) {
		return
	}
	// ⑤ 流类型且流源仍 pending → 原样（vertex.py:515-516）
	if isStreamReader(value) && v.isStreamSourceStillPending(compID) {
		return
	}
	// ⑥ 关闭僵尸流 + 置空串（vertex.py:517-519；Go StreamReader 无消费即终止，
	// 放弃 Next 等价 aclose）
	setLeaf(container, key, "")
}

// isStreamReader 值是否为流式叶子（Python isinstance(value, AsyncGenerator)）
func isStreamReader(value any) bool {
	_, ok := value.(*stream_actor.StreamReader)
	return ok
}

// setLeaf 容器叶子赋值（dict/list 双轨）
func setLeaf(container any, key, val any) {
	switch c := container.(type) {
	case map[string]any:
		c[key.(string)] = val
	case []any:
		c[key.(int)] = val
	}
}

// isStreamSourceStillPending 流源是否仍在等待（Python _is_stream_source_still_pending，
// vertex.py:521-528）
func (v *Vertex) isStreamSourceStillPending(compID string) bool {
	am := v.nodeSession.ActorManager()
	if am == nil {
		return false
	}
	// Python getattr 探测 should_sanitize_stream_source —— Go 静态方法必在，仅 nil 守卫
	return !am.ShouldSanitizeStreamSource(v.nodeID, compID)
}

// isComponentExecuted 组件本轮是否已执行（Python _is_component_executed，vertex.py:530-533）
func (v *Vertex) isComponentExecuted(compID string) bool {
	return containsString(workflowStateList(v.stateView(), "executed_nodes"), compID)
}

// markNodeExecuted 标记节点开始执行（Python _mark_node_executed，vertex.py:535-540）
func (v *Vertex) markNodeExecuted() {
	executed := workflowStateList(v.stateView(), "executed_nodes")
	if containsString(executed, v.nodeID) {
		return
	}
	executed = append(executed, v.nodeID)
	v.stateView().UpdateAndCommitWorkflowState(map[string]any{"executed_nodes": executed})
}

// stateView 节点状态全量消费面（SessionState → WorkflowState 断言；NodeSession
// 构造期已保证节点状态视图实现 WorkflowState，失败时返回 nil 由调用方 panic 传播）
func (v *Vertex) stateView() state.WorkflowState {
	ws, _ := v.nodeSession.State().(state.WorkflowState)
	return ws
}

// workflowStateList 读工作流状态列表键（Python get_workflow_state(key) or []）
func workflowStateList(st state.WorkflowState, key string) []string {
	raw := st.GetWorkflowState(state.StringKey(key))
	if raw == nil {
		return nil
	}
	if l, ok := raw.([]string); ok {
		return l
	}
	if l, ok := raw.([]any); ok {
		out := make([]string, 0, len(l))
		for _, item := range l {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func containsString(list []string, target string) bool {
	for _, s := range list {
		if s == target {
			return true
		}
	}
	return false
}

// postInvoke 输出后处理（Python _post_invoke，vertex.py:542-567 步骤一比一）
func (v *Vertex) postInvoke(ctx context.Context, results map[string]any) (map[string]any, error) {
	// ① 输出 schema（vertex.py:543）
	var outputsSchema any
	if v.nodeConfig != nil && v.nodeConfig.IOConfigs != nil {
		outputsSchema = v.nodeConfig.IOConfigs.OutputsSchema
	}
	// ② 双轨（vertex.py:544-551）：dict → get_by_schema；非 dict → transformer
	if _, isDict := outputsSchema.(map[string]any); !isDict && outputsSchema != nil {
		// Python 单一 Callable → Go 双断言（utils.MessageTransformer / state.Transformer）
		if tr, ok := outputsSchema.(sutils.MessageTransformer); ok {
			results = tr(results)
		} else if tr, ok := outputsSchema.(state.Transformer); ok {
			// Transformer 已按 workflow_state.py:127 契约收窄为 map[string]any，直接赋值
			results = tr(nil)
		}
	} else if outputsSchema != nil && len(results) > 0 {
		results = state.GetBySchemaMap(outputsSchema.(map[string]any), results)
		if !v.isEndNode && len(results) > 0 {
			// 非 End 节点过滤 None 值（vertex.py:548-549）
			filtered := make(map[string]any, len(results))
			for k, val := range results {
				if val != nil {
					filtered[k] = val
				}
			}
			results = filtered
		}
	}
	// ③ End 混合模式 output 列表合并（vertex.py:552-561）
	isEndMixMode := v.isEndNode && v.hasCall && v.hasStreamCall
	if len(results) > 0 && isEndMixMode {
		if outputs, ok := results["output"]; ok && outputs != nil {
			if _, isList := outputs.([]any); !isList {
				results["output"] = []any{outputs}
			}
		}
		oldOutputs := v.stateView().GetOutputs()
		if oldMap, ok := oldOutputs.(map[string]any); ok {
			// 存储形如 {nodeID: {…}}（UpdateByID 嵌套写入）时解包一层，
			// 对齐 Python get_outputs(node_id) 直接返回节点输出 dict 的语义
			if inner, ok2 := oldMap[v.nodeID].(map[string]any); ok2 {
				oldMap = inner
			}
			oldList, ok1 := oldMap["output"].([]any)
			newList, ok2 := results["output"].([]any)
			if ok1 && ok2 {
				results["output"] = append(newList, oldList...)
			}
		}
	}
	// ④ 写回输出（vertex.py:563-564）
	if results != nil {
		v.stateView().SetOutputs(results)
	}
	// ⑤ 输出埋点 + 交互清理（vertex.py:565-566）
	v.traceComponentOutputs(ctx, results)
	v.clearInteractive()
	return results, nil
}

// clearInteractive 清理交互输入（Python _clear_interactive，vertex.py:656-658）
func (v *Vertex) clearInteractive() {
	if v.nodeSession.State().Get(state.StringKey(constants.InteractiveInputKey)) != nil {
		_ = v.nodeSession.State().Update(map[string]any{constants.InteractiveInputKey: nil})
	}
}

// ──────────────────────────── 流式半边（Python _pre_stream/_post_stream/_process_chunk，vertex.py:569-654） ────────────────────────────

// preStream 流入取数（Python _pre_stream，vertex.py:569-588 步骤一比一）：
// 返回 schema 形状的输入树（$ref 叶子为 *StreamReader）
func (v *Vertex) preStream(ctx context.Context, ability compability.ComponentAbility) (map[string]any, error) {
	// ① 组件开始埋点（vertex.py:571）
	v.traceComponentBegin(ctx)
	// ② 流式输入 schema（vertex.py:573-575）：非 dict → nil
	var inputsSchema map[string]any
	if v.nodeConfig != nil && v.nodeConfig.StreamIOConfigs != nil {
		inputsSchema, _ = v.nodeConfig.StreamIOConfigs.InputsSchema.(map[string]any)
	}
	// ③ 逐帧回调（vertex.py:578-584）：debug 日志 + trace stream input（send=false）
	enableTrace := v.nodeSession.Tracer() != nil && !v.executable.SkipTrace()
	streamCallable := func(chunk map[string]any) {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_ABILITY_RUNNING").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Str("ability", ability.Name()).
			Any("chunk", chunk).
			Msgf("Consume chunk of %s[%s]", v.nodeID, ability.Name())
		if enableTrace {
			tracer.TracerWorkflowUtils{}.TraceComponentStreamInput(ctx, v.nodeSession, chunk, false)
		}
	}
	// ④ 消费注册（vertex.py:586）
	am := v.nodeSession.ActorManager()
	return am.Consume(ctx, v.nodeID, ability, inputsSchema, streamCallable)
}

// postStream 流入产出收尾（Python _post_stream，vertex.py:590-628 步骤一比一）
func (v *Vertex) postStream(ctx context.Context, resultsIter <-chan StreamChunk[map[string]any], ability compability.ComponentAbility) {
	// ① 形态标记（vertex.py:591-593）
	isSubGraph := v.nodeSession.ParentID() != ""
	am := v.nodeSession.ActorManager()
	// ② 输出 schema 双轨（vertex.py:594-598）
	var outputSchema map[string]any
	var outputTransformer sutils.MessageTransformer
	if v.nodeConfig != nil && v.nodeConfig.StreamIOConfigs != nil {
		os, _ := v.nodeConfig.StreamIOConfigs.OutputsSchema.(map[string]any)
		outputSchema = os
	}
	if v.nodeConfig != nil && v.nodeConfig.StreamIOConfigs != nil {
		if ot, ok := v.nodeConfig.StreamIOConfigs.OutputsSchema.(sutils.MessageTransformer); ok {
			outputTransformer = ot
		}
	}
	// ③ 逐 chunk 转换与下发（vertex.py:600-612）
	endStreamIndex := 0
	for chunk := range resultsIter {
		if chunk.Err != nil {
			// 流中错误为末元素（8.2 StreamChunk 契约）：转 panic 由 runExecutable
			// 的 recover 兜底？Python 侧迭代器抛异常直接沿 await 传播——Go 以 error
			// 上抛通道等价物处理：此处返回不再可行（无 error 返回值，对齐 Python
			// _post_stream 无 error 返回），以 panic 传播等价异常语义
			panic(chunk.Err)
		}
		var message any
		if outputTransformer == nil {
			if len(outputSchema) > 0 {
				message = am.StreamTransform().GetByDefaultTransformer(chunk.Val, outputSchema)
			} else {
				message = chunk.Val
			}
		} else {
			message = am.StreamTransform().GetByDefinedTransformer(chunk.Val, outputTransformer)
		}
		_ = outputSchema
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_VERTEX_ABILITY_RUNNING").
			Str("graph_id", v.nodeSession.WorkflowID()).
			Str("node_id", v.nodeID).
			Str("ability", ability.Name()).
			Int("index", endStreamIndex).
			Any("chunk", message).
			Bool("is_end_node", v.isEndNode).
			Bool("is_sub_graph", isSubGraph).
			Msgf("Produce chunk[%d] from %s[%s]", endStreamIndex, v.nodeID, ability.Name())
		v.processChunk(ctx, message, v.isEndNode, endStreamIndex, isSubGraph, ability)
		endStreamIndex++
	}
	// ④ 收口分流（vertex.py:613-616）：End+子图 → 子图流发 END_FRAME；否则 end_message
	if v.isEndNode && isSubGraph {
		if q, err := am.SubWorkflowStream(); err == nil {
			q.Close(ctx, 0) // Go close 信号等价 END_FRAME（session/stream D-哨兵决策）
		}
	} else {
		_ = am.EndMessage(ctx, v.nodeID, ability)
	}
	logger.Debug(logComponent).
		Str("event_type", "GRAPH_VERTEX_ABILITY_RUNNING").
		Str("graph_id", v.nodeSession.WorkflowID()).
		Str("node_id", v.nodeID).
		Str("ability", ability.Name()).
		Bool("is_end_node", v.isEndNode).
		Bool("is_sub_graph", isSubGraph).
		Msgf("Produce 'END_FRAME' chunk of [%s] ability [%s]", v.nodeID, ability.Name())
	// ⑤ 交互清理（vertex.py:622）
	v.clearInteractive()
	// ⑥ get_stream_output 鸭子探测（vertex.py:625-628，hasattr → 接口断言）
	if gso, ok := v.executable.(interface{ GetStreamOutput() map[string]any }); ok {
		if result := gso.GetStreamOutput(); result != nil {
			v.stateView().SetOutputs(result)
		}
	}
}

// processChunk 单帧下发三分支（Python _process_chunk，vertex.py:630-654 步骤一比一）
func (v *Vertex) processChunk(ctx context.Context, message any, isEndNode bool, endStreamIndex int, isSubGraph bool, ability compability.ComponentAbility) {
	switch {
	case isEndNode && !isSubGraph:
		// End 非子图（vertex.py:635-646）：StreamSchemas 直用；否则 end-node-stream 信封
		var messageStreamData stream.Schema
		switch msg := message.(type) {
		case stream.OutputSchema:
			messageStreamData = msg
		case *stream.OutputSchema:
			messageStreamData = msg
		case stream.CustomSchema:
			messageStreamData = msg
		case *stream.CustomSchema:
			messageStreamData = msg
		case stream.TraceSchema:
			messageStreamData = msg
		case *stream.TraceSchema:
			messageStreamData = msg
		default:
			// Python dict {type, index, payload} 信封 → Go OutputSchema 同构承载
			messageStreamData = &stream.OutputSchema{
				Type:    constants.EndNodeStream,
				Index:   endStreamIndex,
				Payload: message,
			}
		}
		v.traceComponentStreamOutput(ctx, messageStreamData)
		if writer := v.nodeSession.StreamWriterManager().GetOutputWriter(); writer != nil {
			_ = writer.Write(ctx, messageStreamData)
		}
	case isEndNode && isSubGraph:
		// End 子图（vertex.py:647-650）：OutputSchema 取 payload 直发子图流
		messageStreamData := message
		if os, ok := message.(*stream.OutputSchema); ok {
			messageStreamData = os.Payload
		}
		v.traceComponentStreamOutput(ctx, messageStreamData)
		if q, err := v.nodeSession.ActorManager().SubWorkflowStream(); err == nil {
			_ = q.Send(ctx, messageStreamData, 0, 1)
		}
	default:
		// 普通节点（vertex.py:651-654）：首帧标记 + produce
		firstFrame := endStreamIndex == 0
		v.traceComponentStreamOutput(ctx, message)
		_ = v.nodeSession.ActorManager().Produce(ctx, v.nodeID, message, ability, firstFrame)
	}
}

// Reset 重置计数与流容器（Python reset，vertex.py:882-890）
func (v *Vertex) Reset() {
	v.callCount.Store(0)
	v.streamCallCount.Store(0)
	v.resetStreamDone()
}
