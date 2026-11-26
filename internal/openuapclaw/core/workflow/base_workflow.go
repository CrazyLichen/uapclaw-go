package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/stream_actor"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/config"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/constants"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components"
)

// logComponent 日志组件标识（工作流属 agentcore 面）
const logComponent = logger.ComponentAgentCore

// compIDPattern 组件 ID 合法形态（Python _workflow.py:503 ^[A-Za-z0-9_-]+$）
var compIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// ConnectionType 连接类型（Python _workflow.py:142-146 ConnectionType 枚举；
// 仅 collectProblematicEdges 明细拼接消费）。
type ConnectionType string

const (
	// ConnectionTypeConnection 常规连接
	ConnectionTypeConnection ConnectionType = "connection"
	// ConnectionTypeStreamConnection 流式连接
	ConnectionTypeStreamConnection = "stream_connection"
)

// ──────────────────────────── 结构体 ────────────────────────────

// branchRouterLike 条件边登记所需的 BranchRouter 结构面（偏差 D-19-3：
// workflow→flow 禁止反向导入——flow 经 SubWorkflowComponent 依赖 workflow 成环，
// Python isinstance(BranchRouter)（_workflow.py:281）以结构等价接口承载，
// SetSession/AllTargets/Select 三方法唯 flow.BranchRouter 满足）。
type branchRouterLike interface {
	SetSession(session.BaseSession) error
	AllTargets() map[string]struct{}
	Select(ctx context.Context, state any) ([]string, error)
}

// BaseWorkflow 工作流构图与编译基座（Python _workflow.py BaseWorkflow 一比一）。
type BaseWorkflow struct {
	graph          *graph.PregelGraph
	workflowConfig *WorkflowConfig
	workflowSpec   *WorkflowSpec
	streamActor    *stream_actor.StreamGraph
	session        *session.ProxySession
	// drawable 可视化件（Python _workflow.py:157-160；8.13 D-d 已回填具体类型，
	// env WORKFLOW_DRAWABLE=true 时构造期创建）
	drawable *visualization.Drawable
}

// EdgeTopology 边拓扑上下文（Python _workflow.py:38-53 EdgeTopology dataclass 一比一）。
type EdgeTopology struct {
	// SourceMap 常规边 source -> targets
	SourceMap map[string][]string
	// TargetMap 常规边 target -> sources（反向索引）
	TargetMap map[string][]string
	// SourceStreamMap 流边 source -> targets
	SourceStreamMap map[string][]string
	// TargetStreamMap 流边 target -> sources（反向索引）
	TargetStreamMap map[string][]string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewBaseWorkflow 构造（Python _workflow.py:149-160；graph/config nil 兜底一比一）。
func NewBaseWorkflow(cfg *WorkflowConfig, g *graph.PregelGraph) *BaseWorkflow {
	if g == nil {
		g = graph.NewPregelGraph()
	}
	if cfg == nil {
		// Python: WorkflowConfig(card=WorkflowCard(id=uuid4().hex))
		card := NewWorkflowCard()
		cfg, _ = NewWorkflowConfig(card)
	}
	// Python _workflow.py:157-160：WORKFLOW_DRAWABLE=true 时创建 Drawable
	var drawable *visualization.Drawable
	if strings.EqualFold(os.Getenv("WORKFLOW_DRAWABLE"), "true") {
		drawable = visualization.NewDrawable()
	}
	return &BaseWorkflow{
		graph:          g,
		workflowConfig: cfg,
		workflowSpec:   cfg.Spec,
		streamActor:    stream_actor.NewStreamGraph(),
		session:        session.NewProxySession(),
		drawable:       drawable,
	}
}

// Drawable 返回可视化件（Python `_drawable` property 访问，_workflow.py:326-327；
// 8.13 D-d 回填件，LoopGroup/SubWorkflow 子图取图消费）
func (w *BaseWorkflow) Drawable() *visualization.Drawable { return w.drawable }

// Config 返回工作流配置（Python config()）。
func (w *BaseWorkflow) Config() *WorkflowConfig {
	return w.workflowConfig
}

// workflowIDSetter 可设 WorkflowID 的会话（≙ Python isinstance(session, WorkflowSession)
// 后 set_workflow_id；Go 以能力断言承载，实现在 session/internal.WorkflowSession）。
type workflowIDSetter interface {
	SetWorkflowID(id string)
}

// nestingDepthChecker 子工作流嵌套深度面（≙ Python isinstance(session, SubWorkflowSession)）。
type nestingDepthChecker interface {
	Config() config.SessionConfig
	MainWorkflowID() string
	WorkflowNestingDepth() int
}

// AllEdgeNodes 所有边引用节点（Python _workflow.py:46-53）。
func (e *EdgeTopology) AllEdgeNodes() map[string]struct{} {
	out := map[string]struct{}{}
	for _, m := range []map[string][]string{e.SourceMap, e.TargetMap, e.SourceStreamMap, e.TargetStreamMap} {
		for k, vs := range m {
			out[k] = struct{}{}
			for _, v := range vs {
				out[v] = struct{}{}
			}
		}
	}
	return out
}

// AddWorkflowComp 组件注册（Python _workflow.py:168-202 步骤一比一）。
// 形参集对齐 Python keyword-only：wait_for_all（nil=未声明）、四 schema、
// comp_ability、max_retries、timeout、exception_config、name。
func (w *BaseWorkflow) AddWorkflowComp(
	compID string,
	workflowComp components.ComponentComposable,
	waitForAll *bool,
	inputsSchema, outputsSchema any,
	streamInputsSchema, streamOutputsSchema any,
	compAbility []components.ComponentAbility,
	maxRetries int,
	timeout float64,
	exceptionConfig *ExceptionConfig,
	name *string,
) error {
	if err := w.validateCompID(compID); err != nil {
		return err
	}
	if err := w.validateSchemas(compID, inputsSchema, outputsSchema, streamInputsSchema, streamOutputsSchema); err != nil {
		return err
	}
	if err := w.validateCompAbility(compID, compAbility, waitForAll); err != nil {
		return err
	}
	nodeSpec, err := NewNodeSpec()
	if err != nil {
		return err
	}
	nodeSpec.IOConfigs = &CompIOConfig{InputsSchema: inputsSchema, OutputsSchema: outputsSchema}
	nodeSpec.StreamIOConfigs = &CompIOConfig{InputsSchema: streamInputsSchema, OutputsSchema: streamOutputsSchema}
	if compAbility != nil {
		nodeSpec.Abilities = compAbility
	}
	nodeSpec.MaxRetries = maxRetries
	nodeSpec.Timeout = timeout
	nodeSpec.ExceptionConfig = exceptionConfig
	nodeSpec.Name = name
	w.workflowSpec.CompConfigs[compID] = nodeSpec
	// Python _workflow.py:196-197：wait_for_all is None → False
	wf := false
	if waitForAll == nil {
		waitForAll = &wf
	}
	// Python workflow_comp.add_component(graph=self._graph, node_id=comp_id, wait_for_all=wait_for_all)
	if err := workflowComp.AddComponent(w.graph, compID, *waitForAll); err != nil {
		return err
	}
	// Python _workflow.py:200-202：drawable.add_node(comp_id, workflow_comp)
	if w.drawable != nil {
		w.drawable.AddNode(compID, workflowComp)
	}
	return nil
}

// StartComp 登记起始组件（Python _workflow.py:204-213）。
func (w *BaseWorkflow) StartComp(startCompID string) error {
	if err := w.validateCompID(startCompID); err != nil {
		return err
	}
	w.graph.StartNode(startCompID)
	// Python _workflow.py:209-210：drawable.set_start_node
	if w.drawable != nil {
		// 112020 错误即 Python build_error 上抛
		if err := w.drawable.SetStartNode(startCompID); err != nil {
			return err
		}
	}
	w.workflowSpec.StartNodes = append(w.workflowSpec.StartNodes, startCompID)
	return nil
}

// EndComp 标记终节点（Python _workflow.py:215-223；Python 不写 spec 字段）。
func (w *BaseWorkflow) EndComp(endCompID string) error {
	if err := w.validateCompID(endCompID); err != nil {
		return err
	}
	w.graph.EndNode(endCompID)
	// Python _workflow.py:220-221：drawable.set_end_node
	if w.drawable != nil {
		if err := w.drawable.SetEndNode(endCompID); err != nil {
			return err
		}
	}
	return nil
}

// AddConnection 常规边（Python _workflow.py:225-243 add_connection）。
// 偏差 D-14-6：Python src_comp_id: str|list[str] 运行时分发 → Go 收窄 []string
// （单源由调用方包一元素列表），外层 Workflow.AddConnection 同形。
func (w *BaseWorkflow) AddConnection(srcCompIDs []string, targetCompID string) error {
	if err := w.validateEdge(srcCompIDs, targetCompID, exception.StatusWorkflowEdgeInvalid); err != nil {
		return err
	}
	w.graph.AddEdge(srcCompIDs, targetCompID)
	for _, src := range srcCompIDs {
		w.appendEdge(src, targetCompID)
		// Python _workflow.py:235-236（list 分支）/242（单源分支）：drawable.add_edge
		if w.drawable != nil {
			w.drawable.AddEdge(src, targetCompID, false, false, nil)
		}
	}
	return nil
}

// AddStreamConnection 流式边（Python _workflow.py:245-257；目标可执行体注册流消费者）。
func (w *BaseWorkflow) AddStreamConnection(srcCompID string, targetCompID string) error {
	if err := w.validateEdge([]string{srcCompID}, targetCompID, exception.StatusWorkflowStreamEdgeInvalid); err != nil {
		return err
	}
	w.graph.AddEdge([]string{srcCompID}, targetCompID)
	if tgt, ok := w.graph.GetNodes()[targetCompID]; ok {
		// Python stream_executables[target_comp_id] → add_stream_consumer
		w.streamActor.AddStreamConsumer(tgt, targetCompID)
	}
	w.appendStreamEdge(srcCompID, targetCompID)
	// Python _workflow.py:255-256：drawable.add_edge(src, dst, False, True) 流式边
	if w.drawable != nil {
		w.drawable.AddEdge(srcCompID, targetCompID, false, true, nil)
	}
	return nil
}

// AddConditionalConnection 条件边（Python _workflow.py:259-295 四重校验 + 双分支）。
// BranchRouter 分支已接入（8.19 回填 8.14 ⤴️ 钩子）；普通 router 反射嗅探
// session 形参在调用方包装（workflow 层显式嗅探必须复刻，与 8.5 D2 引擎层不冲突）。
func (w *BaseWorkflow) AddConditionalConnection(srcCompID any, router any) error {
	toStr := w.workflowToStr()
	src, ok := srcCompID.(string)
	if !ok || src == "" {
		return exception.BuildError(exception.StatusWorkflowConditionEdgeInvalid,
			exception.WithParam("src_comp_id", src),
			exception.WithParam("reason", "src_comp_id cannot be empty or None"),
			exception.WithParam("workflow", toStr))
	}
	if router == nil {
		return exception.BuildError(exception.StatusWorkflowConditionEdgeInvalid,
			exception.WithParam("src_comp_id", src),
			exception.WithParam("reason", "router function is required for conditional edges"),
			exception.WithParam("workflow", toStr))
	}
	switch r := router.(type) {
	case branchRouterLike:
		// Python isinstance(BranchRouter) 分支（_workflow.py:281-283）：
		// 构图期注入工作流会话 + 条件边 + 目标登记。
		// 偏差 D-19-3：workflow→flow 禁止反向导入（flow 经 SubWorkflowComponent
		// 依赖 workflow，成环），isinstance 判定以结构等价接口承载
		//（SetSession/AllTargets/Select 三方法唯 BranchRouter 满足）。
		if err := r.SetSession(w.session); err != nil {
			return err
		}
		w.graph.AddConditionalEdges(src, r)
		// Register branch target mapping for CNF barrier resolution at compile time
		//（_workflow.py:284-285 逐字复刻：len>1 才登记）
		allTargets := r.AllTargets()
		if len(allTargets) > 1 {
			ids := make([]string, 0, len(allTargets))
			for id := range allTargets {
				ids = append(ids, id)
			}
			w.graph.RegisterBranchTargets(src, ids)
		}
	case graph.Router:
		// Python 非 BranchRouter 分支（_workflow.py:284-291）：签名嗅探在调用方
		// 包装 selector 时完成，此处直接登记
		w.graph.AddConditionalEdges(src, r)
	default:
		return exception.BuildError(exception.StatusWorkflowConditionEdgeInvalid,
			exception.WithParam("src_comp_id", src),
			exception.WithParam("reason", fmt.Sprintf("router must be a callable function, got %T", router)),
			exception.WithParam("workflow", toStr))
	}
	// Python _workflow.py:293-294：drawable.add_edge(source=..., conditional=True, data=router)
	if w.drawable != nil {
		w.drawable.AddEdge(src, "", true, false, router)
	}
	return nil
}

// Compile 编译（Python _workflow.py:297-315 三段一比一）。
// graph.Compile 为 panic 语义（8.1），此处 recover 包装为 COMPILE_ERROR。
func (w *BaseWorkflow) Compile(sess session.BaseSession, context any) (ec graph.ExecutableGraph[any, map[string]any], err error) {
	defer func() {
		if r := recover(); r != nil {
			// Python except Exception → build_error(COMPILE_ERROR, reason=str(e))
			ec, err = nil, exception.BuildError(exception.StatusWorkflowCompileError,
				exception.WithParam("reason", fmt.Sprintf("%v", r)),
				exception.WithParam("workflow", w.workflowToStr()))
		}
	}()
	// Python _workflow.py:298-300：isinstance(WorkflowSession) → set_workflow_id
	//（Go：能力断言，实现在 session/internal.WorkflowSession）
	if ws, ok := sess.(workflowIDSetter); ok {
		ws.SetWorkflowID(w.workflowConfig.Card.ID)
	}
	// Python _workflow.py:300-309：isinstance(SubWorkflowSession) 嵌套深度校验
	if sws, ok := sess.(nestingDepthChecker); ok {
		mainCfg := sws.Config().GetWorkflowConfig(sws.MainWorkflowID())
		if mainCfg != nil && sws.WorkflowNestingDepth() > mainCfg.WorkflowMaxNestingDepth {
			return nil, exception.BuildError(exception.StatusWorkflowCompileError,
				exception.WithParam("reason", fmt.Sprintf(
					"workflow nesting hierarchy is too big, must <= %d", mainCfg.WorkflowMaxNestingDepth)),
				exception.WithParam("workflow", mainCfg.Card.ID))
		}
	}
	// Python _workflow.py:310：self._session.set_session(session)
	w.session.SetSession(sess)
	// Python _workflow.py:312：self._graph.compile(session, context=context)
	//——D-18-7 修正：context（ModelContext）经 kwargs["context"] 下发至各 Vertex.Init，
	// 原实现传 nil 致 ModelContext 全链丢失
	return w.graph.Compile(sess, map[string]any{"context": context}), nil
}

// Reset 复位（Python _workflow.py:337-339）。
func (w *BaseWorkflow) Reset(ctx context.Context) error {
	w.graph.Reset()
	return nil
}

// AutoCompleteAbilities 按边拓扑补全组件能力（Python _workflow.py:340-350 五步调用序一比一）。
func (w *BaseWorkflow) AutoCompleteAbilities() {
	topo := w.buildEdgeTopology()
	w.validateEdgeNodes(topo)
	userProvided := w.getUserProvidedAbilities()
	w.completeLoopNodeAbilities(topo, userProvided)
	w.completeStreamNodeAbilities(topo, userProvided)
	w.completeInvokeAbilities(topo, userProvided)
	w.completeStreamSourceGroups(topo)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// workflowToStr card 调试串（Python card.to_str()；BaseCard 无 to_str，以 ID/Name 结构化承载）。
func (w *BaseWorkflow) workflowToStr() string {
	return fmt.Sprintf("WorkflowCard(id=%s, name=%s, version=%s)",
		w.workflowConfig.Card.ID, w.workflowConfig.Card.Name, w.workflowConfig.Card.Version)
}

// validateCompID 组件 ID 校验（Python _workflow.py:492-507 四分支一比一）。
func (w *BaseWorkflow) validateCompID(compID string) error {
	if compID == "" {
		return exception.BuildError(exception.StatusWorkflowComponentIDInvalid,
			exception.WithParam("comp_id", compID), exception.WithParam("reason", "is None or empty"),
			exception.WithParam("workflow", w.workflowToStr()))
	}
	if len(compID) > 100 {
		return exception.BuildError(exception.StatusWorkflowComponentIDInvalid,
			exception.WithParam("comp_id", compID),
			exception.WithParam("reason", "length must not between [1, 100]"),
			exception.WithParam("workflow", w.workflowToStr()))
	}
	if !compIDPattern.MatchString(compID) {
		return exception.BuildError(exception.StatusWorkflowComponentIDInvalid,
			exception.WithParam("comp_id", compID),
			exception.WithParam("reason", "only support letters (a–z, A–Z), digits (0–9), underscores (_) or hyphens (-)"),
			exception.WithParam("workflow", w.workflowToStr()))
	}
	return nil
}

// validateSchemas 批/流 schema 键冲突检测（Python _workflow.py:545-569）。
func (w *BaseWorkflow) validateSchemas(compID string, inputsSchema, outputsSchema,
	streamInputsSchema, streamOutputsSchema any) error {
	inMap, inOK := inputsSchema.(map[string]any)
	sInMap, sInOK := streamInputsSchema.(map[string]any)
	if inOK && sInOK {
		fi := flattenDict(inMap)
		fs := flattenDict(sInMap)
		for k := range fi {
			if _, dup := fs[k]; dup {
				return exception.BuildError(exception.StatusWorkflowComponentSchemaInvalid,
					exception.WithParam("comp_id", compID),
					exception.WithParam("reason", "duplicate key both exist in inputs_schema with stream_inputs_schema, key="+k),
					exception.WithParam("workflow", w.workflowToStr()))
			}
		}
	}
	outMap, outOK := outputsSchema.(map[string]any)
	sOutMap, sOutOK := streamOutputsSchema.(map[string]any)
	if outOK && sOutOK {
		fo := flattenDict(outMap)
		fs := flattenDict(sOutMap)
		for k := range fo {
			if _, dup := fs[k]; dup {
				return exception.BuildError(exception.StatusWorkflowComponentSchemaInvalid,
					exception.WithParam("comp_id", compID),
					exception.WithParam("reason", "duplicate key both exist in outputs_schema with stream_outputs_schema, key="+k),
					exception.WithParam("workflow", w.workflowToStr()))
			}
		}
	}
	return nil
}

// validateCompAbility 流能力强制 wait_for_all（Python _workflow.py:509-519）。
func (w *BaseWorkflow) validateCompAbility(compID string, abilities []components.ComponentAbility, waitForAll *bool) error {
	if abilities == nil {
		return nil
	}
	for _, ab := range abilities {
		if ab == components.ComponentAbilityTransform || ab == components.ComponentAbilityCollect {
			wf := false
			if waitForAll != nil {
				wf = *waitForAll
			}
			if !wf {
				return exception.BuildError(exception.StatusWorkflowComponentAbilityInvalid,
					exception.WithParam("comp_id", compID),
					exception.WithParam("reason", "stream components (TRANSFORM/COLLECT) must set 'wait_for_all' to True"),
					exception.WithParam("workflow", w.workflowToStr()))
			}
		}
	}
	return nil
}

// validateEdge 源端校验（Python _workflow.py:521-543 list 分支承载；str 形态由
// 调用方包一元素列表归一，偏差 D-14-6）。
func (w *BaseWorkflow) validateEdge(srcCompIDs []string, targetCompID string, code exception.StatusCode) error {
	for _, id := range srcCompIDs {
		if id == "" {
			return exception.BuildError(code,
				exception.WithParam("src_comp_id", fmt.Sprintf("%v", srcCompIDs)), exception.WithParam("target_comp_id", targetCompID),
				exception.WithParam("reason", "src_comp_id list contains empty or None value"),
				exception.WithParam("workflow", w.workflowToStr()))
		}
	}
	return nil
}

// appendEdge 维护 spec.Edges（Python dict append：无则建、有则追加）。
func (w *BaseWorkflow) appendEdge(src, tgt string) {
	w.workflowSpec.Edges[src] = append(w.workflowSpec.Edges[src], tgt)
}

// appendStreamEdge 维护 spec.StreamEdges。
func (w *BaseWorkflow) appendStreamEdge(src, tgt string) {
	w.workflowSpec.StreamEdges[src] = append(w.workflowSpec.StreamEdges[src], tgt)
}

// buildEdgeTopology 步骤1（Python _workflow.py:352-361）。
func (w *BaseWorkflow) buildEdgeTopology() *EdgeTopology {
	return &EdgeTopology{
		SourceMap:       w.workflowSpec.Edges,
		TargetMap:       sourceToTargetMap(w.workflowSpec.Edges),
		SourceStreamMap: w.workflowSpec.StreamEdges,
		TargetStreamMap: sourceToTargetMap(w.workflowSpec.StreamEdges),
	}
}

// validateEdgeNodes 步骤2（Python _workflow.py:363-376；raise → Go panic 构造期纪律）。
func (w *BaseWorkflow) validateEdgeNodes(topo *EdgeTopology) {
	registered := map[string]struct{}{}
	for id := range w.workflowSpec.CompConfigs {
		registered[id] = struct{}{}
	}
	var missing []string
	for id := range topo.AllEdgeNodes() {
		if _, ok := registered[id]; !ok {
			missing = append(missing, id)
		}
	}
	if len(missing) == 0 {
		return
	}
	sort.Strings(missing)
	panic(exception.BuildError(exception.StatusWorkflowCompileError,
		exception.WithParam("reason", fmt.Sprintf(
			"Component ID mismatch: nodes %v are referenced in edges but not registered via "+
				"add_workflow_comp/set_start_comp/set_end_comp.\nRegistered components: %v\nProblematic edges:\n%s",
			missing, sortedKeys(registered), w.collectProblematicEdges(topo, missing)))))
}

// collectProblematicEdges 明细拼接（Python _workflow.py:378-388；ConnectionType 仅此处消费）。
func (w *BaseWorkflow) collectProblematicEdges(topo *EdgeTopology, missing []string) string {
	miss := map[string]struct{}{}
	for _, m := range missing {
		miss[m] = struct{}{}
	}
	var lines []string
	for _, pair := range []struct {
		label string
		m     map[string][]string
	}{
		{string(ConnectionTypeConnection), topo.SourceMap},
		{string(ConnectionTypeStreamConnection), topo.SourceStreamMap},
	} {
		for src, ts := range pair.m {
			for _, tgt := range ts {
				if _, bad := miss[src]; bad {
					lines = append(lines, fmt.Sprintf("  - %s: '%s' -> '%s'", pair.label, src, tgt))
				}
				if _, bad := miss[tgt]; bad {
					lines = append(lines, fmt.Sprintf("  - %s: '%s' -> '%s'", pair.label, src, tgt))
				}
			}
		}
	}
	return strings.Join(lines, "\n")
}

// getUserProvidedAbilities 步骤3（Python _workflow.py:390-395）。
func (w *BaseWorkflow) getUserProvidedAbilities() map[string]bool {
	out := map[string]bool{}
	for id, cfg := range w.workflowSpec.CompConfigs {
		out[id] = len(cfg.Abilities) > 0
	}
	return out
}

// completeLoopNodeAbilities 步骤4a（Python _workflow.py:397-411）——偏差 D-e：
// Python getattr(self,'_start_nodes',None) 恒 None，两循环体永不执行；照复刻空数组走法。
func (w *BaseWorkflow) completeLoopNodeAbilities(topo *EdgeTopology, userProvided map[string]bool) {
	var loopStartNodes, loopEndNodes []string // Python 恒 []
	for _, node := range loopStartNodes {
		if !userProvided[node] {
			if _, ok := topo.SourceStreamMap[node]; ok {
				w.addAbilityToNode(node, components.ComponentAbilityStream)
			}
			if _, ok := topo.SourceMap[node]; ok {
				w.addAbilityToNode(node, components.ComponentAbilityInvoke)
			}
		}
	}
	for _, node := range loopEndNodes {
		if !userProvided[node] {
			if _, ok := topo.TargetStreamMap[node]; ok {
				w.addAbilityToNode(node, components.ComponentAbilityCollect)
			}
		}
	}
}

// completeStreamNodeAbilities 步骤4b（Python _workflow.py:413-435）。
func (w *BaseWorkflow) completeStreamNodeAbilities(topo *EdgeTopology, userProvided map[string]bool) {
	// 流源节点（Python for node in edge_topology.source_stream_map）
	for node := range topo.SourceStreamMap {
		if userProvided[node] {
			continue
		}
		// 有常规输入 + 流式输出 → STREAM（Python:420-421）
		if _, ok := topo.TargetMap[node]; ok {
			w.addAbilityToNode(node, components.ComponentAbilityStream)
		}
		// 有流式输入 + 流式输出 → TRANSFORM（Python:423-424）
		if _, ok := topo.TargetStreamMap[node]; ok {
			w.addAbilityToNode(node, components.ComponentAbilityTransform)
		} else if !w.isStartNode(node) {
			// when branch target node with stream edge（Python:425-427）
			w.addAbilityToNode(node, components.ComponentAbilityStream)
		}
	}
	// 流目标节点（Python:429-435）：有流式输入 + 常规输出 → COLLECT
	for node := range topo.TargetStreamMap {
		if userProvided[node] {
			continue
		}
		if _, ok := topo.SourceMap[node]; ok {
			w.addAbilityToNode(node, components.ComponentAbilityCollect)
		}
	}
}

// isStartNode 判 start（Python node not in self._workflow_spec.start_nodes）。
func (w *BaseWorkflow) isStartNode(node string) bool {
	for _, s := range w.workflowSpec.StartNodes {
		if s == node {
			return true
		}
	}
	return false
}

// completeInvokeAbilities 步骤4c（Python _workflow.py:437-441）。
func (w *BaseWorkflow) completeInvokeAbilities(topo *EdgeTopology, userProvided map[string]bool) {
	for node := range topo.TargetMap {
		if !userProvided[node] {
			if _, ok := topo.SourceMap[node]; ok {
				w.addAbilityToNode(node, components.ComponentAbilityInvoke)
			}
		}
	}
}

// addAbilityToNode 去重追加（Python _workflow.py:443-447）。
func (w *BaseWorkflow) addAbilityToNode(compID string, ab components.ComponentAbility) {
	cfg := w.workflowSpec.CompConfigs[compID]
	for _, a := range cfg.Abilities {
		if a == ab {
			return
		}
	}
	cfg.Abilities = append(cfg.Abilities, ab)
}

// completeStreamSourceGroups 步骤5（Python _workflow.py:449-479；CNF 解析走 D-f 访问器）。
func (w *BaseWorkflow) completeStreamSourceGroups(topo *EdgeTopology) {
	sourceGroups := map[string][][]string{}
	for consumerID, producerIDs := range topo.TargetStreamMap {
		// Python:454-456 groups = [{pid} for pid in producer_ids] + resolver
		groups := make([]map[string]struct{}, 0, len(producerIDs))
		for _, pid := range producerIDs {
			groups = append(groups, map[string]struct{}{pid: {}})
		}
		groups = w.graph.ResolveBarrierGroups(consumerID, groups)

		var streamGroups [][]string
		for _, group := range groups {
			if len(group) == 1 {
				// Python:460-466 单源组：按能力展开 producer-ABILITY
				producerID := mapSingleKey(group)
				var g []string
				for _, ab := range w.workflowSpec.CompConfigs[producerID].Abilities {
					if ab == components.ComponentAbilityStream || ab == components.ComponentAbilityTransform {
						g = append(g, producerID+"-"+abilityName(ab))
					}
				}
				if len(g) > 0 {
					streamGroups = append(streamGroups, g)
				}
				continue
			}
			// Python:468-475 多源组：拼键后组内排序
			var g []string
			for pid := range group {
				for _, ab := range w.workflowSpec.CompConfigs[pid].Abilities {
					if ab == components.ComponentAbilityStream || ab == components.ComponentAbilityTransform {
						g = append(g, pid+"-"+abilityName(ab))
					}
				}
			}
			if len(g) > 0 {
				sort.Strings(g)
				streamGroups = append(streamGroups, g)
			}
		}
		if len(streamGroups) > 0 {
			sourceGroups[consumerID] = streamGroups
		}
	}
	w.workflowSpec.StreamSourceGroups = sourceGroups
}

// abilityName 枚举名（Python ability.name："STREAM"/"TRANSFORM" 等）。
func abilityName(ab components.ComponentAbility) string {
	switch ab {
	case components.ComponentAbilityInvoke:
		return "INVOKE"
	case components.ComponentAbilityStream:
		return "STREAM"
	case components.ComponentAbilityCollect:
		return "COLLECT"
	case components.ComponentAbilityTransform:
		return "TRANSFORM"
	default:
		return fmt.Sprintf("%d", int(ab))
	}
}

// sortedKeys 集合键排序（错误明细用）。
func sortedKeys(set map[string]struct{}) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// mapSingleKey 单元素 map 取键。
func mapSingleKey(m map[string]struct{}) string {
	for k := range m {
		return k
	}
	return ""
}

// sourceToTargetMap 源表转目标表（Python _workflow.py:481-490）。
func sourceToTargetMap(src map[string][]string) map[string][]string {
	out := map[string][]string{}
	for source, targets := range src {
		for _, t := range targets {
			out[t] = append(out[t], source)
		}
	}
	return out
}

// flattenDict 递归展平嵌套 dict，键以 "." 连接（Python dict_utils.flatten_dict）。
func flattenDict(d map[string]any) map[string]any {
	out := map[string]any{}
	var walk func(prefix string, m map[string]any)
	walk = func(prefix string, m map[string]any) {
		for k, v := range m {
			key := k
			if prefix != "" {
				key = prefix + "." + k
			}
			if sub, ok := v.(map[string]any); ok {
				walk(key, sub)
			} else {
				out[key] = v
			}
		}
	}
	walk("", d)
	return out
}

// ExecuteSingleComponent 单组件直调（Python _workflow.py:56-139 模块级函数一比一；
// 不经过图引擎，中断只存 io 侧——SINGLE_COMPONENT_EXECUTION 标记供 checkpointer
// 恢复校验跳过图侧快照检查）。
func ExecuteSingleComponent(ctx context.Context, componentID string, sess session.Session,
	executor components.ComponentComposable, inputs any,
	inputsSchema, outputsSchema any, modelContext any) (any, error) {
	// 1. Create WorkflowSession（:67-71）
	workflowSession := session.NewWorkflowSessionFacade(componentID, sess.GetSessionID(), nil)
	// 单组件直调：中断状态只存 io 侧、无图侧快照（Python :72-74）
	workflowSession.Config().SetEnvs(map[string]any{constants.SingleComponentExecutionEnvKey: true})
	if err := workflowSession.Checkpointer().PreWorkflowExecute(ctx, workflowSession, inputs); err != nil {
		return nil, err
	}
	// 2. Create NodeSession（:77-79）：Python type(executor).__name__ 作类型名
	_, innerNode := session.NewWorkflowNodeSessionPair(workflowSession.Inner(), componentID, executorTypeName(executor), false)
	wcs, _ := innerNode.State().(state.WorkflowState)
	if _, isII := inputs.(*interaction.InteractiveInput); !isII {
		// Python :78-79 非 InteractiveInput 才 commit_user_inputs
		//（WorkflowCommitState 具体方法，接口未收口——8.15 NodeSession.NodeConfig 同款接缝）
		if cu, ok := innerNode.State().(interface {
			CommitUserInputs(inputs map[string]any)
		}); ok {
			if m, ok := inputs.(map[string]any); ok {
				cu.CommitUserInputs(m)
			}
		}
	}
	// 3. Create Vertex（:81-82）
	exec := executor.ToExecutable()
	vertex := graph.NewVertex(componentID, exec)
	// 4. Initialize Vertex（:86；context 为 nil 时仍可正常初始化）
	// Python :86 vertex.init(node_session, context=context)——D-18-7 修正：
	// modelContext（ModelContext）经 kwargs 下发，原实现传 nil 丢失
	vertex.Init(innerNode, map[string]any{"context": modelContext})
	// 5. 直设 node config（:90-106 SimpleNodeConfig）——Go 以 config 注册面承载：
	// 注册单条 WorkflowConfig，NodeConfig() 即从此取（偏差注记：Python 直设属性）
	spec := NewWorkflowSpec()
	simpleSpec, _ := NewNodeSpec()
	simpleSpec.IOConfigs = &CompIOConfig{InputsSchema: inputsSchema, OutputsSchema: outputsSchema}
	simpleSpec.Abilities = []components.ComponentAbility{components.ComponentAbilityInvoke}
	simpleSpec.MaxRetries = 0
	spec.CompConfigs[componentID] = simpleSpec
	wfc, _ := NewWorkflowConfig(NewWorkflowCard())
	wfc.Spec = spec
	workflowSession.Config().AddWorkflowConfig(componentID, wfc)

	// 7. Create PregelConfig（:109-120：graph_invoker 有无 → Inner/Plain 双形态）
	var cfg any
	if isGraphInvoker(exec) {
		inner := pregel.CreateInnerConfig(pregel.PregelConfig{
			SessionID: workflowSession.SessionID(), Ns: componentID,
			RecursionLimit: 100,
		})
		inner.ParentNs = componentID
		cfg = inner
	} else {
		cfg = pregel.PregelConfig{
			SessionID: workflowSession.SessionID(), Ns: componentID,
			RecursionLimit: 100,
		}
	}

	var interruptErr *pregel.GraphInterrupt
	// 8. Execute component（:125）
	_, err := vertex.Call(ctx, nil, cfg)
	if err != nil {
		if errors.As(err, &interruptErr) {
			// Python :126-128 GraphInterrupt → return e.value[0].value
			if len(interruptErr.Interrupts) > 0 {
				return interruptErr.Interrupts[0].Value, nil
			}
			return nil, nil
		}
	}
	// 9-10. Commit + outputs（:130-139；Python finally 块，Go 顺序等价——
	// 非 interrupt 时才走常规收口）
	// ≙ node_session.state().commit() + get_outputs(component_id)
	wcs.Commit()
	result := wcs.GetOutputs(componentID)
	if result == nil {
		result = map[string]any{}
	}
	// post_workflow_execute 异常吞掉（Python :135-138 except Exception: pass）
	if resultMap, ok := result.(map[string]any); ok {
		_ = workflowSession.Checkpointer().PostWorkflowExecute(ctx, workflowSession, resultMap, err)
	}
	if m, ok := result.(map[string]any); ok {
		return m[componentID], nil
	}
	return result, nil
}

// isGraphInvoker 判定可执行体是否为子图调用面（Python :109 hasattr(executable,"graph_invoker")）。
func isGraphInvoker(exec graph.GeneralExecutor) bool {
	gi, ok := any(exec).(interface{ GraphInvoker() bool })
	return ok && gi.GraphInvoker()
}

// executorTypeName 组件类型名（Python type(executor).__name__）。
func executorTypeName(executor components.ComponentComposable) string {
	return reflect.TypeOf(executor).Elem().Name()
}

// Graph 返回构图图（Python _graph 属性访问；8.20 LoopGroup checkValidate/构图消费）
func (w *BaseWorkflow) Graph() *graph.PregelGraph { return w.graph }

// Spec 返回工作流规格（Python _workflow_spec 属性访问；8.20 LoopGroup on_invoke 消费）
func (w *BaseWorkflow) Spec() *WorkflowSpec { return w.workflowSpec }

// StreamGraph 访问器（Python _stream_actor 属性访问；8.20 LoopGroup on_invoke 消费）
func (w *BaseWorkflow) StreamGraph() *stream_actor.StreamGraph { return w.streamActor }
