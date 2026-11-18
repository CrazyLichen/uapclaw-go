package graph

import (
	"cmp"
	"fmt"
	"reflect"
	"runtime"
	"slices"
	"strings"

	pregel "github.com/uapclaw/uapclaw-go/internal/agentcore/graph/pregel"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// Branch 条件分支载体。Python: graph.py:52-54 @dataclass Branch
type Branch struct {
	// Condition 选择器（Python: condition callable）
	Condition Router
}

// edge 二元组，对应 Python edges: list[Tuple[str | list[str], str]]（graph.py:61）
type edge struct {
	source any // string 或 []string
	target string
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

// StartNode 登记起始边。Python: graph.py:69-72
func (g *PregelGraph) StartNode(nodeID string) *PregelGraph {
	g.validateNodeID(nodeID)
	g.addEdgeSource([]string{pregel.Start}, nodeID)
	return g
}

// EndNode 标记终节点并登记结束边。Python: graph.py:78-84
func (g *PregelGraph) EndNode(nodeID string) *PregelGraph {
	g.validateNodeID(nodeID)
	if v, ok := g.nodes[nodeID]; ok {
		v.SetEndNode(true)
	}
	g.addEdgeSource([]string{nodeID}, pregel.End)
	return g
}

// AddNode 注册节点。Python: graph.py:86-98
func (g *PregelGraph) AddNode(nodeID string, node GeneralExecutor, waitForAll bool) *PregelGraph {
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
	if waitForAll {
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

// AddEdge 登记边。Python: graph.py:103-120（校验四分支顺序照抄）
func (g *PregelGraph) AddEdge(sourceNodeID any, targetNodeID string) *PregelGraph {
	g.addEdgeSource(sourceNodeID, targetNodeID)
	return g
}

// AddConditionalEdges 登记条件边。Python: graph.py:122-132
func (g *PregelGraph) AddConditionalEdges(sourceNodeID string, router Router) *PregelGraph {
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
// 校验四分支顺序对齐 graph.py:104-119。
func (g *PregelGraph) addEdgeSource(sourceNodeID any, targetNodeID string) {
	edgeInvalid := func(reason string) {
		panic(exception.BuildError(exception.StatusPregelGraphEdgeInvalid,
			exception.WithParam("source_id", sourceNodeID),
			exception.WithParam("target_id", targetNodeID),
			exception.WithParam("reason", reason)))
	}
	switch src := sourceNodeID.(type) {
	case string:
		if src == "" {
			edgeInvalid("source_node_id is None or empty")
		}
	case []string:
		if len(src) == 0 {
			edgeInvalid("source_node_id is None or empty")
		}
		for _, item := range src {
			if item == "" {
				edgeInvalid("source_node_id list has None or empty")
			}
		}
	default:
		if sourceNodeID == nil {
			edgeInvalid("source_node_id is None or empty")
		}
	}
	if targetNodeID == "" {
		edgeInvalid("target_node_id is None or empty")
	}
	g.edges = append(g.edges, edge{source: sourceNodeID, target: targetNodeID})
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
			if src, ok := e.source.(string); ok && src == node {
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
