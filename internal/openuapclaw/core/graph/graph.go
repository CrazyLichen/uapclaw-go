package graph

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"

	pregel "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/store"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interaction"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/state"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Branch 条件分支载体。Python: graph.py:52-54 @dataclass Branch
type Branch struct {
	// Condition 选择器（Python: condition callable）
	Condition Router
}

// edge 二元组，对应 Python edges: list[Tuple[str | list[str], str]]（graph.py:61）。
// 源收窄为 []string 单形态（偏差 D-7：Python Union[str, list[str]]），
// listForm 记录 Python 原始形态（str→false=static 边、list→true=barrier 边——
// builder 按形态分派：[]string→string 是 barrier、string→string 是 static，
// StartNode/EndNode 传 [x] 列表即 barrier 形态，归一后靠此标记还原）。
type edge struct {
	source   []string
	target   string
	listForm bool
}

// PregelGraph 声明式构图实现。Python: graph.py:57-67
type PregelGraph struct {
	// preg 编译产物缓存（graph.py:60 pregel | None）
	preg *pregel.Pregel
	// edges 待编译边表（graph.py:61），只登记不建图
	edges []edge
	// waits 需要 wait_for_all 的节点集（graph.py:62）
	waits map[string]struct{}
	// nodes 节点表（graph.py:63）
	nodes map[string]*Vertex
	// branches 条件边：src → callable 名 → Branch（graph.py:64 defaultdict(dict)）
	branches map[string]map[string]Branch
	// branchTargets 分支目标集（graph.py:65 branch_targets），CNF 屏障解析用
	branchTargets map[string]map[string]struct{}
	// checkpointer 会话检查点器（graph.py:66）
	checkpointer interfaces.Checkpointer
	// sessionRef 编译时绑定的会话（graph.py:67 _session）
	sessionRef session.BaseSession
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewPregelGraph 创建构图器。Python: graph.py:59-67 __init__
func NewPregelGraph() *PregelGraph {
	return &PregelGraph{
		edges:         []edge{},
		waits:         map[string]struct{}{},
		nodes:         map[string]*Vertex{},
		branches:      map[string]map[string]Branch{},
		branchTargets: map[string]map[string]struct{}{},
	}
}

// ResolveBarrierGroups 互斥前驱 CNF 解析的导出访问器（8.14 前置件，偏差 D-f）。
// Python `_workflow.py:452` 用 getattr(self._graph, "_resolve_barrier_groups", None)
// 反射偷调非导出方法；Go 反射不可行，改加访问器——非导出本体与 8.1 API 面不动，
// 消费方为 BaseWorkflow.completeStreamSourceGroups（stream_source_groups 构造）。
func (g *PregelGraph) ResolveBarrierGroups(targetID string, sourceList []map[string]struct{}) []map[string]struct{} {
	return g.resolveBarrierGroups(targetID, sourceList)
}

// StartNode 登记起始边。Python: graph.py:69-72（add_edge([START], node_id)——列表形态=barrier）
func (g *PregelGraph) StartNode(nodeID string) Graph {
	g.validateNodeID(nodeID)
	g.addEdgeSource([]string{pregel.Start}, nodeID, true)
	return g
}

// EndNode 标记终节点并登记结束边。Python: graph.py:78-84（add_edge([node_id], END)——列表形态=barrier）
func (g *PregelGraph) EndNode(nodeID string) Graph {
	g.validateNodeID(nodeID)
	if v, ok := g.nodes[nodeID]; ok {
		v.SetEndNode(true)
	}
	g.addEdgeSource([]string{nodeID}, pregel.End, true)
	return g
}

// ──────────────────────────── 常量（AddNode option）────────────────────────────

// addNodeConfig AddNode 变参 option 的承载结构（Python add_node keyword-only 形参集）
type addNodeConfig struct {
	// waitForAll 对应 Python wait_for_all，默认 False
	waitForAll bool
}

// AddNodeOption AddNode 的变参选项（偏差 D-2：Python keyword-only 默认参数 →
// Go 变参 option，零 option ≡ 默认 False）
type AddNodeOption func(*addNodeConfig)

// WithWaitForAll 置 wait_for_all=true：该节点前驱在编译期汇聚成屏障
// （Python add_node(..., wait_for_all=True)，graph.py:96-97）
func WithWaitForAll() AddNodeOption {
	return func(c *addNodeConfig) { c.waitForAll = true }
}

// AddNode 注册节点。Python: graph.py:86-98
func (g *PregelGraph) AddNode(nodeID string, node GeneralExecutor, opts ...AddNodeOption) Graph {
	g.validateNodeID(nodeID)
	if node == nil {
		panic(exception.BuildError(exception.StatusPregelGraphNodeInvalid,
			exception.WithParam("node_id", nodeID), exception.WithParam("reason", "node is None")))
	}
	if _, exists := g.nodes[nodeID]; exists {
		panic(exception.BuildError(exception.StatusPregelGraphNodeIDInvalid,
			exception.WithParam("node_id", nodeID), exception.WithParam("reason", "already exist, can not add again")))
	}
	g.nodes[nodeID] = NewVertex(nodeID, node)
	cfg := addNodeConfig{}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.waitForAll {
		g.waits[nodeID] = struct{}{}
	}
	return g
}

// GetNodes 返回节点表浅拷贝。Python: graph.py:100-101
func (g *PregelGraph) GetNodes() map[string]*Vertex {
	out := make(map[string]*Vertex, len(g.nodes))
	for k, v := range g.nodes {
		out[k] = v
	}
	return out
}

// AddEdge 登记边。Python: graph.py:103-120（校验分支顺序照抄；源形态收窄见偏差 D-7）
func (g *PregelGraph) AddEdge(sourceNodeIDs []string, targetNodeID string) Graph {
	g.addEdgeSource(sourceNodeIDs, targetNodeID, len(sourceNodeIDs) > 1)
	return g
}

// AddConditionalEdges 登记条件边。Python: graph.py:122-132
func (g *PregelGraph) AddConditionalEdges(sourceNodeID string, router Router) Graph {
	if sourceNodeID == "" {
		panic(exception.BuildError(exception.StatusPregelGraphConditionEdgeInvalid,
			exception.WithParam("source_id", sourceNodeID), exception.WithParam("reason", "source_node_is is None or empty")))
	}
	if router == nil {
		panic(exception.BuildError(exception.StatusPregelGraphConditionEdgeInvalid,
			exception.WithParam("source_id", sourceNodeID), exception.WithParam("reason", "router is None")))
	}
	name := getCallableName(router)
	if g.branches[sourceNodeID] == nil {
		g.branches[sourceNodeID] = map[string]Branch{}
	}
	g.branches[sourceNodeID][name] = Branch{Condition: router}
	return g
}

// RegisterBranchTargets 登记分支节点的目标全集，编译期 CNF 屏障解析用。
// Python: graph.py:134-143（branch_node_id 与 targets 非空且 len>1 才登记）
func (g *PregelGraph) RegisterBranchTargets(branchNodeID string, targets []string) *PregelGraph {
	if branchNodeID != "" && len(targets) > 1 {
		set := make(map[string]struct{}, len(targets))
		for _, t := range targets {
			set[t] = struct{}{}
		}
		g.branchTargets[branchNodeID] = set
	}
	return g
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateNodeID 校验节点 ID 非空。Python: graph.py:74-76
func (g *PregelGraph) validateNodeID(nodeID string) {
	if nodeID == "" {
		panic(exception.BuildError(exception.StatusPregelGraphNodeIDInvalid,
			exception.WithParam("node_id", nodeID), exception.WithParam("reason", "is None or empty")))
	}
}

// addEdgeSource 校验并登记边（AddEdge/StartNode/EndNode 共用落点）。
// 校验分支顺序对齐 graph.py:104-119（空源/空项/空目标）；
// 源形态收窄为 []string（偏差 D-7）后，Python 的 str 分支归一进切片统一校验。
func (g *PregelGraph) addEdgeSource(sourceNodeIDs []string, targetNodeID string, listForm bool) {
	edgeInvalid := func(reason string) {
		panic(exception.BuildError(exception.StatusPregelGraphEdgeInvalid,
			exception.WithParam("source_id", sourceNodeIDs),
			exception.WithParam("target_id", targetNodeID),
			exception.WithParam("reason", reason)))
	}
	if len(sourceNodeIDs) == 0 {
		edgeInvalid("source_node_id is None or empty")
	}
	for _, item := range sourceNodeIDs {
		if item == "" {
			edgeInvalid("source_node_id list has None or empty")
		}
	}
	if targetNodeID == "" {
		edgeInvalid("target_node_id is None or empty")
	}
	g.edges = append(g.edges, edge{source: sourceNodeIDs, target: targetNodeID, listForm: listForm})
}

// getCallableName 反射取名，对应 Python graph.py:403-409 _get_callable_name 三级回退。
// 唯一消费点是 branches[src] 的 map key（同名覆盖/异名并存）。
// 偏差 D-3（spec §7）：Python 匿名 lambda 恒名 "<lambda>" 会互相覆盖；
// Go 匿名闭包经 FuncForPC 得编译器名 funcN 各不相同，不触发覆盖——已获拍板。
func getCallableName(r Router) string {
	if r == nil {
		return fmt.Sprintf("%v", r) // 三级兜底：nil selector（AddConditionalEdges 已拦，防御性留档）
	}
	v := reflect.ValueOf(r)
	switch v.Kind() {
	case reflect.Func:
		// 一级：func.__name__ → runtime.FuncForPC，去包路径取短名
		full := runtime.FuncForPC(v.Pointer()).Name()
		if idx := strings.LastIndex(full, "/"); idx >= 0 {
			full = full[idx+1:]
		}
		if idx := strings.LastIndex(full, "."); idx >= 0 {
			full = full[idx+1:]
		}
		return full
	default:
		// 二级：func.__class__.__name__ → 类型名
		t := v.Type()
		if t.Kind() == reflect.Ptr {
			t = t.Elem()
		}
		return t.Name()
	}
}

// forwardReachable BFS 正向可达集（visited 含起点自身，对齐 Python graph.py:145-157 首轮即 add）。
func (g *PregelGraph) forwardReachable(startNode string) map[string]struct{} {
	visited := map[string]struct{}{}
	queue := []string{startNode}
	for len(queue) > 0 {
		node := queue[0] // Python queue.pop(0) 头取
		queue = queue[1:]
		if _, ok := visited[node]; ok {
			continue
		}
		visited[node] = struct{}{}
		for _, e := range g.edges {
			// Python `src == node` 对 list 源恒 False（list==str 不等）——
			// 仅 str 形态（!listForm）的边参与 BFS
			if !e.listForm && e.source[0] == node {
				if _, seen := visited[e.target]; !seen {
					queue = append(queue, e.target)
				}
			}
		}
	}
	return visited
}

// buildBranchParent 嵌套分支节点 → 父分支节点映射。Python: graph.py:159-171
func (g *PregelGraph) buildBranchParent() map[string]string {
	parent := map[string]string{}
	for _, bid := range sortedKeys(g.branchTargets) { // 遍历序不影响结果（写入键互不相交），排序仅为确定性
		for _, target := range sortedKeys(g.branchTargets[bid]) {
			if _, isBranchTarget := g.branchTargets[target]; isBranchTarget {
				parent[target] = bid
				continue
			}
			if _, isBranchNode := g.branches[target]; isBranchNode {
				parent[target] = bid
			}
		}
	}
	return parent
}

// branchRoot 沿父链回溯至最外层分支祖先。Python: graph.py:173-178 @staticmethod
func branchRoot(branchNodeID string, branchParent map[string]string) string {
	root := branchNodeID
	for {
		next, ok := branchParent[root]
		if !ok {
			break
		}
		root = next
	}
	return root
}

// resolveBarrierGroups 互斥前驱 CNF 解析（五步，注释编号对齐 graph.py:180-237）。
// 前驱集合语义与 Python set 一致，组间顺序经 sortedKeys 保持确定性
// （Python set 迭代序不定，Go 不依赖组序——消费方 builder.AddEdge 组序不影响 CNF 判定）。
func (g *PregelGraph) resolveBarrierGroups(targetID string, sourceList []map[string]struct{}) []map[string]struct{} {
	// 前置：无 branch_targets 或无前驱 → 原样返回（graph.py:193-194）
	if len(g.branchTargets) == 0 || len(sourceList) == 0 {
		return sourceList
	}

	// Step 1: 收集全部直接前驱（graph.py:196-200）
	allPredecessors := map[string]struct{}{}
	for _, grp := range sourceList {
		for p := range grp {
			allPredecessors[p] = struct{}{}
		}
	}

	// Step 2: 每个 branch 出口的 BFS 正向可达集（graph.py:202-205）
	type bidTarget struct{ bid, tgt string }
	reachable := map[bidTarget]map[string]struct{}{}
	for _, bid := range sortedKeys(g.branchTargets) {
		for _, tgt := range sortedKeys(g.branchTargets[bid]) {
			reachable[bidTarget{bid, tgt}] = g.forwardReachable(tgt)
		}
	}

	// Step 3: 每个前驱的归属（branch_id, target）集合（graph.py:207-213）
	predInfo := map[string]map[bidTarget]struct{}{}
	for p := range allPredecessors {
		predInfo[p] = map[bidTarget]struct{}{}
		for bt, nodes := range reachable {
			if _, ok := nodes[p]; ok {
				predInfo[p][bt] = struct{}{}
			}
		}
	}

	// Step 4: 按 branch_id 分组；非单一归属进 standalone（graph.py:215-225）
	branchGroups := map[string]map[string]struct{}{}
	var standalone []map[string]struct{}
	for _, p := range sortedKeys(allPredecessors) {
		branches := predInfo[p]
		if len(branches) == 1 {
			var bid string
			for bt := range branches {
				bid = bt.bid
			}
			if branchGroups[bid] == nil {
				branchGroups[bid] = map[string]struct{}{}
			}
			branchGroups[bid][p] = struct{}{}
		} else {
			standalone = append(standalone, map[string]struct{}{p: {}})
		}
	}

	// Step 5: 嵌套分支组提升至根分支祖先（graph.py:227-231）
	branchParent := g.buildBranchParent()
	rootMerged := map[string]map[string]struct{}{}
	var rootOrder []string
	for _, bid := range sortedKeys(branchGroups) {
		root := branchRoot(bid, branchParent)
		if rootMerged[root] == nil {
			rootMerged[root] = map[string]struct{}{}
			rootOrder = append(rootOrder, root)
		}
		for p := range branchGroups[bid] {
			rootMerged[root][p] = struct{}{}
		}
	}

	// 结果组装（graph.py:233-237）：空则原样返回
	var result []map[string]struct{}
	for _, root := range rootOrder {
		if len(rootMerged[root]) > 0 {
			result = append(result, rootMerged[root])
		}
	}
	result = append(result, standalone...)
	if len(result) == 0 {
		return sourceList
	}
	return result
}

// sortedKeys map 键升序，保确定性遍历（值类型任意，兼容 struct{} set 与嵌套 map）
func sortedKeys[K cmp.Ordered, V any](m map[K]V) []K {
	keys := make([]K, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}

// ──────────────────────────── 导出函数（Compile/Reset）────────────────────────

// Compile 编译构图产物。Python: graph.py:239-265
// 步骤：①全节点 init（graph.py:240-241）②afterStep 回调（graph.py:243-256）
// ③建图（graph.py:258-264）：首次取 checkpointer/store 全量编译并缓存；复用仅换 session。
func (g *PregelGraph) Compile(sess session.BaseSession, kwargs map[string]any) ExecutableGraph[any, map[string]any] {
	// ① 逐节点 init（graph.py:240-241）
	for _, node := range g.nodes {
		node.Init(sess, kwargs)
	}

	// ② afterStep 回调（graph.py:243-256）：session commit + SUPER_STEP_END 日志
	afterStep := func(_ context.Context, loop *pregel.PregelLoop) {
		if g.sessionRef != nil {
			if ws, ok := g.sessionRef.State().(state.WorkflowState); ok {
				ws.Commit()
			}
		}
		logger.Debug(logComponent).
			Str("event_type", "GRAPH_SUPER_STEP_END").
			Str("graph_id", loop.Config.Ns).
			Str("session_id", loop.Config.SessionID).
			Str("ns", loop.Config.Ns).
			Int("step", loop.Step).
			Strs("active_nodes", loop.ActiveNodes).
			Msgf("Finished to run graph super-step [%d]", loop.Step)
	}

	// ③ 建图（graph.py:258-264）
	if g.preg == nil {
		g.checkpointer = sess.Checkpointer()
		graphStore := store.NewGraphStore(g.checkpointer.GraphStore())
		g.preg = g.compile(graphStore, afterStep)
		g.sessionRef = sess
	} else {
		g.sessionRef = sess
	}
	return NewCompiledGraph(g.preg, g.checkpointer)
}

// Reset 重置全部节点。Python: graph.py:305-307
func (g *PregelGraph) Reset() {
	for _, node := range g.nodes {
		node.Reset()
	}
}

// ──────────────────────────── 非导出函数（compile 三段编译）────────────────────

// compile 三段编译（graph.py:267-303，步骤编号保留）。
// Python builder.add_node(node_id, action) 直存 Vertex（鸭子类型）；
// Go 经 vertexNodeFunc 适配器桥接（偏差 D-4，见函数注释）。
func (g *PregelGraph) compile(graphStore *store.GraphStore, stepCallback pregel.AfterStepFunc) *pregel.Pregel {
	edges := []edge{}
	sources := map[string][]map[string]struct{}{}
	builder := pregel.NewPregelBuilder()
	for _, nodeID := range sortedKeys(g.nodes) {
		builder.AddNode(nodeID, vertexNodeFunc(g.nodes[nodeID]))
	}

	// Step 1: waits 前驱收集为单元素组（graph.py:274-285；str→{str}、list 逐项 {s}，
	// D-7 归一后两形态统一为逐项单元素组）
	for _, e := range g.edges {
		if _, isWait := g.waits[e.target]; isWait {
			for _, s := range e.source {
				sources[e.target] = append(sources[e.target], map[string]struct{}{s: {}})
			}
		} else {
			edges = append(edges, e)
		}
	}

	// Step 2: 互斥前驱 CNF 解析（graph.py:287-291）
	for targetID := range sources {
		sources[targetID] = g.resolveBarrierGroups(targetID, sources[targetID])
	}

	// Step 3: 传入 builder——wait 组在前、普通边在后（graph.py:293-298）
	for _, targetID := range sortedKeys(sources) {
		starts := make([]any, 0, len(sources[targetID]))
		for _, grp := range sources[targetID] {
			if len(grp) == 1 {
				for k := range grp {
					starts = append(starts, k)
				}
			} else {
				starts = append(starts, sortedKeys(grp))
			}
		}
		if err := builder.AddEdge(starts, targetID); err != nil {
			panic(err)
		}
	}
	for _, e := range edges {
		// D-7：按 Python 原始形态还原——str 源→string（static）、list 源→[]string（barrier）
		var start any
		if e.listForm {
			start = e.source
		} else {
			start = e.source[0]
		}
		if err := builder.AddEdge(start, e.target); err != nil {
			panic(err)
		}
	}

	for _, src := range sortedKeys(g.branches) {
		for _, name := range sortedKeys(g.branches[src]) {
			builder.AddBranch(src, g.branches[src][name].Condition)
		}
	}
	return builder.Build(graphStore, stepCallback)
}

// vertexNodeFunc 把 Vertex 桥接为 builder 的 NodeFunc。
// Python: builder.add_node 直存 Vertex，nodeTask 经 func.__call__ 派发（task.py:135-152）；
// Go 无鸭子类型，显式适配：Call 返回值丢弃（task.py:152 不读返回值）、错误上抛（task.py:171）。
// 偏差 D-4：适配器为 Go 侧新增的鸭子类型桥，无行为变更。
func vertexNodeFunc(v *Vertex) pregel.NodeFunc {
	return func(ctx context.Context, in pregel.NodeInput) error {
		_, err := v.Call(ctx, in.State, in.Config)
		return err
	}
}

// ──────────────────────────── 结构体（CompiledGraph）────────────────────────

// CompiledGraph 编译产物，ExecutableGraph 的图实现。
// Python: graph.py:310-400 CompiledGraph(ExecutableGraph)。
// I=any 对齐 Python Input TypeVar 无类型（executable.py:9）——信封 INPUTS_KEY
// 取出值可为 *InteractiveInput（2026-11-16 用户拍板，D-5 改写：判定回到
// Invoke_ 方法体内局部判定，Python graph.py:327 isinstance 同位同形态，
// 替代原实例字段 c.interactive 的竞态/残留落法）。
// Executable 部分由内嵌 BaseExecutable 承载（fake 先例 executable_graph_test.go:17）。
type CompiledGraph struct {
	BaseExecutable[any, map[string]any]
	preg         *pregel.Pregel
	checkpointer interfaces.Checkpointer
}

// NewCompiledGraph 构造。Python: graph.py:311-313 __init__
func NewCompiledGraph(p *pregel.Pregel, cp interfaces.Checkpointer) *CompiledGraph {
	return &CompiledGraph{preg: p, checkpointer: cp}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Invoke 拆信封默认体（Python invoke 继承 base.py:13-14）
func (c *CompiledGraph) Invoke(ctx context.Context, inputs map[string]any, sess session.BaseSession) (map[string]any, error) {
	return DefaultGraphInvoke[any, map[string]any](c, ctx, inputs, sess)
}

// Stream pass 占位（Python graph.py:396-397）
func (c *CompiledGraph) Stream(ctx context.Context, inputs map[string]any, sess session.BaseSession) (<-chan StreamChunk[any], error) {
	return DefaultGraphStream[any, map[string]any](c, ctx, inputs, sess)
}

// Collect pass 占位（Python 未覆写 collect，继承 base.py:19-20）
func (c *CompiledGraph) Collect(ctx context.Context, inputs <-chan any, sess session.BaseSession) (map[string]any, error) {
	return DefaultGraphCollect[any, map[string]any](c, ctx, inputs, sess)
}

// Transform pass 占位（Python 未覆写 transform，继承 base.py:22-23）
func (c *CompiledGraph) Transform(ctx context.Context, inputs <-chan any, sess session.BaseSession) (<-chan StreamChunk[any], error) {
	return DefaultGraphTransform[any, map[string]any](c, ctx, inputs, sess)
}

// Interrupt pass 占位（Python graph.py:399-400 interrupt 直接 return）
func (c *CompiledGraph) Interrupt(ctx context.Context, message map[string]any) error {
	return DefaultGraphInterrupt[any, map[string]any](c, ctx, message)
}

// Invoke_ 图执行生命周期（Python _invoke，graph.py:315-394，步骤编号保留）。
// ctx 经契约传入（偏差 D-6：Python asyncio 协程取消 → Go ctx 取消）；
// config 收窄 *PregelConfig（偏差 D-8），nil → isMain 默认配置。
func (c *CompiledGraph) Invoke_(ctx context.Context, inputs any, sess session.BaseSession, config *pregel.PregelConfig) (map[string]any, error) {
	isMain := false
	sessionID := sess.SessionID()
	workflowID := ""
	if wp, ok := sess.(interfaces.WorkflowIDProvider); ok { // Python session.workflow_id() hasattr 模式
		workflowID = wp.WorkflowID()
	}

	// config 为 nil → isMain + 默认配置（graph.py:320-322；D-8 收窄后无 switch 断言）
	pc := config
	if pc == nil {
		isMain = true
		pc = &pregel.PregelConfig{SessionID: sessionID, Ns: workflowID, RecursionLimit: pregel.MaxRecursiveLimit}
	}

	// 非 InteractiveInput 判定（graph.py:327）——局部判定，无实例状态
	_, isInteractive := inputs.(*interaction.InteractiveInput)

	// 正常路径（graph.py:324-348）
	if isMain {
		if err := c.checkpointer.PreWorkflowExecute(ctx, sess, inputs); err != nil {
			return nil, err
		}
	}
	// 非 InteractiveInput → commit_user_inputs（graph.py:327-328）
	// inputs 经 I=any 承载；Python 侧非交互输入即 dict，map 断言失败时传 nil
	// （对齐 Python .get 宽容取值语义，非 dict 输入由 state 层零值承接）
	if !isInteractive {
		if wcs, ok := sess.State().(*state.WorkflowCommitState); ok {
			var m map[string]any
			if inputs != nil {
				m, _ = inputs.(map[string]any)
			}
			wcs.CommitUserInputs(m)
		}
	}

	inner := pregel.CreateInnerConfig(*pc)
	result, exception, canceled := func() (map[string]any, error, bool) {
		r, err := c.preg.Run(ctx, &inner)
		if err != nil {
			if errors.Is(err, context.Canceled) {
				// graph.py:335-341：取消先记日志再 raise——raise 跳过下方正常 post
				logger.Warn(logComponent).
					Str("event_type", "GRAPH_END").
					Str("session_id", sessionID).
					Str("workflow_id", workflowID).
					Bool("cancelled", true).
					Msg("Pregel execution cancelled")
				return nil, err, true
			}
			return nil, err, false
		}
		return r, nil, false
	}()

	// 取消路径（graph.py:349-394）：外层 except CancelledError——正常 post 被跳过，
	// 仅 isMain 走取消分流，最后 re-raise 原始取消链
	if canceled {
		if isMain {
			c.handleCancellation(ctx, sess, sessionID, workflowID)
		}
		return nil, exception
	}

	if isMain {
		if err := c.checkpointer.PostWorkflowExecute(ctx, sess, result, exception); err != nil {
			return nil, err
		}
	} else if exception != nil {
		return nil, exception // graph.py:347-348
	}
	return result, nil
}

// handleCancellation 取消分流三分支（graph.py:349-394，HITL 快照语义逐条复刻）：
// ①查询失败按不可续跑——不落盘不清除，仅 warning（graph.py:367-376）；
// ②快照可续跑——带非 nil 异常走保存分支保留现场，保存分支的取消吞掉、
//
//	其他失败不顶替原始取消链（graph.py:377-393）；
//
// ③快照为 nil（全新执行中途取消）——不落盘，保留可重跑语义（graph.py:358-364）。
func (c *CompiledGraph) handleCancellation(ctx context.Context, sess session.BaseSession, sessionID, workflowID string) {
	graphState, err := c.checkpointer.GraphStore().Get(ctx, sessionID, workflowID)
	if err != nil {
		// 分支①
		logger.Warn(logComponent).
			Str("event_type", "GRAPH_END").
			Str("session_id", sessionID).
			Str("workflow_id", workflowID).
			Err(err).
			Msg("Failed to inspect graph checkpoint on workflow cancellation")
		return
	}
	if graphState != nil && graphState.IsResumable() {
		// 分支②：canceled 异常对齐 asyncio.CancelledError("workflow execution cancelled")
		saveErr := c.checkpointer.PostWorkflowExecute(ctx, sess, map[string]any{}, context.Canceled)
		if errors.Is(saveErr, context.Canceled) {
			return // 保存分支 re-raise 的取消吞掉，保原始取消链（graph.py:382-385）
		}
		if saveErr != nil {
			// 保存失败不顶替原始取消链（graph.py:386-393）
			logger.Error(logComponent).
				Str("event_type", "GRAPH_END").
				Str("session_id", sessionID).
				Str("workflow_id", workflowID).
				Err(saveErr).
				Msg("Failed to preserve checkpoint on workflow cancellation")
			return
		}
	}
	// 分支③：graphState == nil 或不可续跑 → 不落盘，保留可重跑语义
}
