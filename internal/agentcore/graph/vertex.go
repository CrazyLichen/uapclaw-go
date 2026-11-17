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
	"reflect"
	"strings"
	"sync/atomic"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Vertex 工作流图节点执行器。
// Python: Vertex(AsyncAtomicNode, StreamConsumer)（vertex.py:65），单结构体承载
// 双基类（AsyncAtomicNode 由 8.10 AtomicNode 承载，于批入编排任务接入；
// StreamConsumer 三方法由本类型实现）。
type Vertex struct {
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
	componentAbility []components.ComponentAbility
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

// applyNodeConfig 派生 nodeConfig 与能力面（Python init 的 vertex.py:92-97 段）。
// 偏差 D-3：Python 内联于 init，Go 抽为独立方法以在 8.15 NodeConfig 管路回填前
// 支持同包测试驱动；步骤顺序与判定不变。
func (v *Vertex) applyNodeConfig(nc any) {
	// ④ node_config（vertex.py:92）：any 断言 NodeSpec，nil/类型不符 → nil
	v.nodeConfig, _ = nc.(*workflow.NodeSpec)
	// ⑤ 能力面（vertex.py:93-95）：空则默认 [INVOKE]
	if v.nodeConfig != nil && len(v.nodeConfig.Abilities) > 0 {
		v.componentAbility = v.nodeConfig.Abilities
	} else {
		v.componentAbility = []components.ComponentAbility{components.ComponentAbilityInvoke}
	}
	// ⑥⑦ hasStreamCall/hasCall（vertex.py:96-97）
	v.hasStreamCall = len(v.streamAbilities()) > 0
	v.hasCall = len(v.componentAbility) > len(v.streamAbilities())
}

// streamAbilities 流入能力过滤（Python _stream_abilities，vertex.py:800-803：
// 仅 COLLECT/TRANSFORM）
func (v *Vertex) streamAbilities() []components.ComponentAbility {
	var out []components.ComponentAbility
	for _, a := range v.componentAbility {
		if a == components.ComponentAbilityCollect || a == components.ComponentAbilityTransform {
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
}

// Init 初始化节点会话与能力面（Python init，vertex.py:87-112；步骤一比一，返回 bool）
func (v *Vertex) Init(sess session.BaseSession, kwargs map[string]any) bool {
	// ① NodeSession 构造（vertex.py:88-89）：nodeType=可执行体类型名、skipTrace 透传
	v.nodeSession = session.NewWorkflowVertexSession(sess, v.nodeID, executableTypeName(v.executable), v.executable.SkipTrace())
	// ② context 注入（vertex.py:90）
	v.ctx, _ = kwargs["context"].(context.Context)
	// ③ 流调用超时（vertex.py:91）
	v.streamCalledTimeout = anyToFloat64(sess.Config().GetEnv(constants.CompStreamCallTimeoutKey))
	// ④⑤⑥⑦ node_config 读取与能力派生（vertex.py:92-97，抽 applyNodeConfig 驱动，偏差 D-3）
	v.applyNodeConfig(v.nodeSession.NodeConfig())
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

// Reset 重置计数与流容器（Python reset，vertex.py:882-890）
func (v *Vertex) Reset() {
	v.callCount.Store(0)
	v.streamCallCount.Store(0)
	v.resetStreamDone()
}
