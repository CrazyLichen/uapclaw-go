package stream_actor

import (
	"context"
	"fmt"
	workflow "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/workflowconfig"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/state"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sutils "github.com/uapclaw/uapclaw-go/internal/agentcore/session/utils"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ActorSession ActorManager 对 session 的依赖面（窄接口，规则 10 使用方方向）。
// Python: manager.py __init__ 的 session 参数消费面（config().get_env / state() 读写 finished_stream_nodes）。
type ActorSession interface {
	// GetEnvFloat 读 session env 浮点值（STREAM_INPUT_GEN_TIMEOUT_KEY）
	GetEnvFloat(key string) (float64, bool)
	// GetWorkflowStateValue 读工作流状态
	GetWorkflowStateValue(key string) any
	// UpdateAndCommitWorkflowState 更新并提交工作流状态
	UpdateAndCommitWorkflowState(data map[string]any)
}

// StreamTransform 流式变换器集合（Python manager.py StreamTransform，两 classmethod → 方法）。
type StreamTransform struct{}

// ActorManager 流式 actor 管理器（Python manager.py ActorManager 一比一）。
type ActorManager struct {
	// streamEdges 流式边表：producer → [consumers]
	streamEdges map[string][]string
	// streams consumer_id → StreamActor
	streams map[string]*StreamActor
	// streamsTransform 变换器集合
	streamsTransform *StreamTransform
	// activeProducerIDs 本轮已活跃的 producer 能力
	activeProducerIDs map[string]map[ability.ComponentAbility]bool
	// consumerDict 反向图：consumer → [producers]
	consumerDict map[string][]string
	// producerAbilities producer 能力集（∩ {STREAM, TRANSFORM}）
	producerAbilities map[string]map[ability.ComponentAbility]bool
	// streamSourceGroups consumer 源组
	streamSourceGroups map[string][][]string
	// workflowSession 会话依赖面
	workflowSession ActorSession
	// subGraph 是否子图
	subGraph bool
	// subWorkflowStreamQ 子图流队列（AsyncStreamQueue maxsize=10*1024）
	subWorkflowStreamQ *stream.AsyncStreamQueue
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetByDefinedTransformer 自定义变换函数直调（Python:19-21）
func (t *StreamTransform) GetByDefinedTransformer(originMessage map[string]any,
	tr sutils.MessageTransformer) map[string]any {
	return tr(originMessage)
}

// GetByDefaultTransformer 按 schema 默认取值（Python:23-25）
func (t *StreamTransform) GetByDefaultTransformer(originMessage map[string]any,
	schema map[string]any) map[string]any {
	return state.GetBySchemaMap(schema, originMessage)
}

// NewActorManager 构造（Python __init__ 28-58 步骤一比一）。
func NewActorManager(ctx context.Context, spec *workflow.WorkflowSpec, graph *StreamGraph,
	subGraph bool, session ActorSession) (*ActorManager, error) {
	// stream_generator_timeout 来自 session env（Python:55-56）
	timeout := -1.0
	if session != nil {
		if v, ok := session.GetEnvFloat(constants.StreamInputGenTimeoutKey); ok {
			if fv, isNum := toFloat(v); isNum {
				timeout = fv
			}
		}
	}
	m := &ActorManager{
		streamEdges:        spec.StreamEdges,
		streams:            map[string]*StreamActor{},
		streamsTransform:   &StreamTransform{},
		activeProducerIDs:  map[string]map[ability.ComponentAbility]bool{},
		consumerDict:       buildReverseGraph(spec.StreamEdges),
		producerAbilities:  map[string]map[ability.ComponentAbility]bool{},
		streamSourceGroups: map[string][][]string{},
		workflowSession:    session,
		subGraph:           subGraph,
	}
	if subGraph {
		// Python:58 AsyncStreamQueue(maxsize=10*1024)
		q, err := stream.NewAsyncStreamQueue(10 * 1024)
		if err != nil {
			return nil, err
		}
		m.subWorkflowStreamQ = q
	}
	// Python:37-56 逐消费者建 actor
	for consumerID, producerIDs := range m.consumerDict {
		consumerSpec := spec.CompConfigs[consumerID]
		var consumerAbilities []ability.ComponentAbility
		if consumerSpec != nil {
			for _, ab := range consumerSpec.Abilities {
				if ab == ability.ComponentAbilityCollect || ab == ability.ComponentAbilityTransform {
					consumerAbilities = append(consumerAbilities, ab)
				}
			}
		}
		sources := map[string]bool{}
		for _, producerID := range producerIDs {
			abilities := m.producerAbilities[producerID]
			if abilities == nil {
				abilities = map[ability.ComponentAbility]bool{}
			}
			producerSpec := spec.CompConfigs[producerID]
			if producerSpec != nil {
				for _, ab := range producerSpec.Abilities {
					if ab == ability.ComponentAbilityStream || ab == ability.ComponentAbilityTransform {
						abilities[ab] = true
						sources[producerID+"-"+ab.Name()] = true
					}
				}
			}
			m.producerAbilities[producerID] = abilities
		}
		declaredGroups := spec.StreamSourceGroups[consumerID]
		var sourceGroups [][]string
		if len(declaredGroups) == 0 {
			// Python:51 未声明时默认每源单拆，按源排序
			keys := make([]string, 0, len(sources))
			for k := range sources {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				sourceGroups = append(sourceGroups, []string{k})
			}
		} else {
			sourceGroups = declaredGroups
		}
		groups := make([][]string, 0, len(sourceGroups))
		for _, g := range sourceGroups {
			if len(g) > 0 {
				groups = append(groups, g)
			}
		}
		// Python:52 self._stream_source_groups[consumer_id]
		m.streamSourceGroups[consumerID] = groups
		m.streams[consumerID] = NewStreamActor(consumerID, graph.GetNode(consumerID),
			consumerAbilities, groups, secondsToDuration(timeout))
	}
	return m, nil
}

// SubWorkflowStream 子图流队列；非子图报错（Python:60-64 build_error，错误码对齐
// StatusCode.GRAPH_STREAM_ACTOR_EXECUTION_ERROR，Go 复刻 Python 字面量 112030（上游与 GRAPH_STATE_COMMIT_ERROR 撞号笔误一并复刻））。
func (m *ActorManager) SubWorkflowStream() (*stream.AsyncStreamQueue, error) {
	if !m.subGraph {
		return nil, exception.BuildError(exception.StatusGraphStreamActorExecutionError,
			exception.WithParam("reason", "only sub graph has sub_workflow_stream"))
	}
	return m.subWorkflowStreamQ, nil
}

// ActiveProduceAbility 登记活跃能力（Python:66-69）
func (m *ActorManager) ActiveProduceAbility(producerID string, ab ability.ComponentAbility) {
	if m.activeProducerIDs[producerID] == nil {
		m.activeProducerIDs[producerID] = map[ability.ComponentAbility]bool{}
	}
	m.activeProducerIDs[producerID][ab] = true
}

// MarkProducerDone 写回 finished_stream_nodes（Python:71-76，幂等 append）
func (m *ActorManager) MarkProducerDone(producerID string) {
	if m.workflowSession == nil {
		// 审查变化 4：nil 守卫必须留痕，防止装配遗漏时静默不写状态
		logger.Warn(logComponent).
			Str("node_id", producerID).
			Msg("workflow session is nil, skip marking producer done")
		return
	}
	nodes := toStringSlice(m.workflowSession.GetWorkflowStateValue("finished_stream_nodes"))
	found := false
	for _, n := range nodes {
		if n == producerID {
			found = true
			break
		}
	}
	if !found {
		nodes = append(nodes, producerID)
	}
	m.workflowSession.UpdateAndCommitWorkflowState(map[string]any{"finished_stream_nodes": nodes})
}

// ShouldSanitizeStreamSource 流源是否已知本轮不活跃（Python:78-97 步骤一比一）。
func (m *ActorManager) ShouldSanitizeStreamSource(consumerID, producerID string) bool {
	// 匹配含该 producer 的组
	var matchedGroups []map[string]bool
	for _, group := range m.groupSets(consumerID) {
		for sourceKey := range group {
			if sourceKeyMatchesProducer(sourceKey, producerID) {
				matchedGroups = append(matchedGroups, group)
				break
			}
		}
	}
	if len(matchedGroups) == 0 {
		return true
	}
	for _, group := range matchedGroups {
		// 单源组是 AND 依赖，可能只是晚到，必须等其 end frame
		if len(group) == 1 {
			return false
		}
		if m.groupHasActiveAlternative(group, producerID) {
			return true
		}
	}
	return false
}

// StreamTransform 访问器（Python property stream_transform）
func (m *ActorManager) StreamTransform() *StreamTransform { return m.streamsTransform }

// StreamActor 取消费者 actor（观测/测试辅助）
func (m *ActorManager) StreamActor(consumerID string) *StreamActor {
	return m.streams[consumerID]
}

// ActiveAbilities 取 producer 已活跃能力（观测/测试辅助）
func (m *ActorManager) ActiveAbilities(producerID string) map[ability.ComponentAbility]bool {
	return m.activeProducerIDs[producerID]
}

// Produce 路由帧到 stream_edges 指定的消费者（Python:106-120 步骤一比一）。
func (m *ActorManager) Produce(ctx context.Context, producerID string, messageContent any,
	ability ability.ComponentAbility, firstFrame bool) error {
	m.ActiveProduceAbility(producerID, ability)
	consumerIDs := m.streamEdges[producerID]
	if len(consumerIDs) > 0 {
		for _, consumerID := range consumerIDs {
			actor := m.getActor(consumerID)
			if actor == nil {
				continue
			}
			if err := actor.Send(ctx, map[string]any{producerID: messageContent}, ability,
				firstFrame, producerID); err != nil {
				return err
			}
		}
	} else {
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_SEND_STREAM_CHUNK").
			Str("producer_id", producerID).
			Str("chunk", fmt.Sprintf("%v", messageContent)).
			Msgf("Discard chunk send from [%s] to none consumer", producerID)
	}
	return nil
}

// EndMessage 发 END 哨兵（Python:122-124）
func (m *ActorManager) EndMessage(ctx context.Context, producerID string, ability ability.ComponentAbility) error {
	return m.Produce(ctx, producerID, fmt.Sprintf("END_%s", producerID), ability, false)
}

// Consume 注册 gen 树并启动 processor.run（Python:126-142 步骤一比一 + 偏差 D1 启动点）。
// 尾部补发：finished_stream_nodes 中已完成且无活跃能力的 producer → EndMessage 补发。
func (m *ActorManager) Consume(ctx context.Context, consumerID string, ability ability.ComponentAbility,
	schema map[string]any, streamCallback func(map[string]any)) (map[string]any, error) {
	actor := m.getActor(consumerID)
	if actor == nil {
		return nil, fmt.Errorf("actor not found for consumer %s", consumerID)
	}
	consumeIter := actor.Generator(ability, schema, streamCallback)
	if m.workflowSession == nil {
		// 审查变化 4：nil 守卫必须留痕，防止装配遗漏时静默跳过补发
		logger.Warn(logComponent).
			Str("consumer_id", consumerID).
			Msg("workflow session is nil, skip resending end messages for finished producers")
	} else {
		finished := toStringSlice(m.workflowSession.GetWorkflowStateValue("finished_stream_nodes"))
		for _, producerID := range m.consumerDict[consumerID] {
			fin := false
			for _, n := range finished {
				if n == producerID {
					fin = true
					break
				}
			}
			if !fin {
				continue
			}
			allAbilities := m.producerAbilities[producerID]
			active := m.activeProducerIDs[producerID]
			if len(active) == 0 {
				for ab := range allAbilities {
					if err := m.EndMessage(ctx, producerID, ab); err != nil {
						return nil, err
					}
					m.ActiveProduceAbility(producerID, ab)
				}
			}
		}
	}
	// 8.11 前置还原：D1 启动段删除——processor.run 启动点回归 Python send:98-100（Send 侧）
	return consumeIter, nil
}

// Shutdown 全 actor 级联关闭（Python:165-167）
func (m *ActorManager) Shutdown() error {
	for _, actor := range m.streams {
		if err := actor.Shutdown(); err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getActor 取 actor（Python _get_actor）
func (m *ActorManager) getActor(consumerID string) *StreamActor {
	return m.streams[consumerID]
}

// groupSets consumer 源组的集合形态
func (m *ActorManager) groupSets(consumerID string) []map[string]bool {
	groups := m.streamSourceGroups[consumerID]
	sets := make([]map[string]bool, 0, len(groups))
	for _, g := range groups {
		set := map[string]bool{}
		for _, k := range g {
			set[k] = true
		}
		sets = append(sets, set)
	}
	return sets
}

// groupHasActiveAlternative 组内是否有该 producer 之外的活跃替代源（Python:144-151）
func (m *ActorManager) groupHasActiveAlternative(group map[string]bool, producerID string) bool {
	for sourceKey := range group {
		groupProducerID, ability, err := splitSourceKey(sourceKey)
		if err != nil {
			continue
		}
		if groupProducerID == producerID {
			continue
		}
		if m.activeProducerIDs[groupProducerID][ability] {
			return true
		}
	}
	return false
}

// sourceKeyMatchesProducer 源键是否属于该 producer（Python:153-155）
func sourceKeyMatchesProducer(sourceKey, producerID string) bool {
	id, _, err := splitSourceKey(sourceKey)
	if err != nil {
		return false
	}
	return id == producerID
}

// splitSourceKey 源键反解 (producerID, ability)；能力名未知时报错（Python:157-163 ValueError）
func splitSourceKey(sourceKey string) (string, ability.ComponentAbility, error) {
	for i := len(sourceKey) - 1; i >= 0; i-- {
		if sourceKey[i] == '-' {
			producerID, abilityName := sourceKey[:i], sourceKey[i+1:]
			for _, ab := range ability.AbilityValues() {
				if ab.Name() == abilityName {
					return producerID, ab, nil
				}
			}
			return "", 0, fmt.Errorf("Unknown component ability: %s", abilityName)
		}
	}
	return "", 0, fmt.Errorf("invalid source key: %s", sourceKey)
}

// buildReverseGraph 反向图（Python manager.py:170-178 一比一）
func buildReverseGraph(graph map[string][]string) map[string][]string {
	rev := map[string][]string{}
	for source, targets := range graph {
		for _, target := range targets {
			rev[target] = append(rev[target], source)
		}
	}
	return rev
}

// toStringSlice any → []string（finished_stream_nodes 为 JSON 兼容值）
func toStringSlice(v any) []string {
	switch t := v.(type) {
	case []string:
		return t
	case []any:
		out := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// toFloat 宽松数值转换（env 值可能为 float64/int）
func toFloat(v any) (float64, bool) {
	switch t := v.(type) {
	case float64:
		return t, true
	case float32:
		return float64(t), true
	case int:
		return float64(t), true
	case int64:
		return float64(t), true
	default:
		return 0, false
	}
}
