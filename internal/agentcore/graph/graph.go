package graph

import (
	"context"
	"fmt"

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

// getCallableName 临时最小实现，Task 3 补齐三级反射与测试。
func getCallableName(r Router) string { return fmt.Sprintf("%T", r) }

// ctxRef 占位引用，Task 4/5 移除（避免 import 阶段未使用报错）
var _ = context.Background

// sessRef 占位引用，Task 4/5 移除
var _ session.BaseSession
