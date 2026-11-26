package visualization

import (
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopSubgraphSource 循环体子图源能力（偏差 D-13-5，≙ Python
// isinstance(LoopComponent/AdvancedLoopComponent) 分支；实现方 flow/loop 包）
type LoopSubgraphSource interface {
	// LoopDrawableSubgraph 返回循环体子图（Python loop_group/body drawable.get_graph()）
	LoopDrawableSubgraph() *DrawableGraph
}

// SubgraphSource 子工作流子图源能力（≙ Python isinstance(SubWorkflowComponent) 分支；
// 实现方 flow.SubWorkflowComponent）
type SubgraphSource interface {
	// SubWorkflowDrawableSubgraph 返回子工作流子图
	SubWorkflowDrawableSubgraph() *DrawableGraph
}

// BranchRouterSource 分支路由源能力（≙ Python isinstance(BranchComponent/
// IntentDetectionComponent) 分支；实现方 flow/llm，返回 component.router()）
type BranchRouterSource interface {
	// DrawableRouter 返回分支路由器（*flow.BranchRouter）
	DrawableRouter() any
}

// DrawableBranchTargetsSource 分支目标投影能力（≙ Python BranchRouter 的
// get_drawable_branch_router；实现方 flow.BranchRouter，D-19-1 回填件）
type DrawableBranchTargetsSource interface {
	// GetDrawableBranchTargets 返回分支目标与标签（branch_router.py:71）
	GetDrawableBranchTargets() (targets []string, datas []string)
}

// Drawable 可视化构建器（Python drawable.py:46-140 一比一）
type Drawable struct {
	// graph 图镜像（Python self._graph）
	graph *DrawableGraph
	// loopNodes 循环节点集合（Python self._loop_nodes）
	loopNodes map[string]struct{}
	// nodeOrder 节点插入序（Python dict 插入序承载，供 Mermaid 输出排序）
	nodeOrder []string
}

// NewDrawable 构造（Python drawable.py:47-49 __init__：DrawableGraph(nodes={},
// edges=[], start_nodes=[], end_nodes=[], break_nodes=[])）
func NewDrawable() *Drawable {
	return &Drawable{
		graph: &DrawableGraph{
			Nodes:      map[string]NodeLike{},
			Edges:      []*DrawableEdge{},
			StartNodes: []NodeLike{},
			EndNodes:   []NodeLike{},
		},
		loopNodes: map[string]struct{}{},
	}
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetStartNode 登记起始节点（Python drawable.py:83-88 set_start_node；
// 112020 复刻 reason 文案）
func (d *Drawable) SetStartNode(nodeID string) error {
	node, ok := d.graph.Nodes[nodeID]
	if !ok {
		return exception.BuildError(exception.StatusDrawableGraphStartNodeInvalid,
			exception.WithParam("node_id", nodeID),
			exception.WithParam("reason", "node '"+nodeID+"' does not exist in the graph"))
	}
	d.graph.StartNodes = append(d.graph.StartNodes, node)
	return nil
}

// SetEndNode 登记结束节点（Python drawable.py:90-95 set_end_node，112021）
func (d *Drawable) SetEndNode(nodeID string) error {
	node, ok := d.graph.Nodes[nodeID]
	if !ok {
		return exception.BuildError(exception.StatusDrawableGraphEndNodeInvalid,
			exception.WithParam("node_id", nodeID),
			exception.WithParam("reason", "node '"+nodeID+"' does not exist in the graph"))
	}
	d.graph.EndNodes = append(d.graph.EndNodes, node)
	return nil
}

// SetBreakNode 登记中断节点（Python drawable.py:97-102 set_break_node，112022）
func (d *Drawable) SetBreakNode(nodeID string) error {
	node, ok := d.graph.Nodes[nodeID]
	if !ok {
		return exception.BuildError(exception.StatusDrawableGraphBreakNodeInvalid,
			exception.WithParam("node_id", nodeID),
			exception.WithParam("reason", "node '"+nodeID+"' does not exist in the graph"))
	}
	d.graph.BreakNodes = append(d.graph.BreakNodes, node)
	return nil
}

// AddNode 组件登记（Python drawable.py:51-81 add_node 形参序一比一）
func (d *Drawable) AddNode(nodeID string, component any) {
	d.addNode(nodeID, component)
}

// AddEdge 加边（Python drawable.py:104-124 add_edge 形参序一比一）
func (d *Drawable) AddEdge(source, target string, conditional, streaming bool, data any) {
	d.addEdge(source, target, conditional, streaming, data)
}

// GetGraph 返回图镜像（Python drawable.py:138-140 get_graph）
func (d *Drawable) GetGraph() *DrawableGraph { return d.graph }

// ──────────────────────────── 非导出函数 ────────────────────────────

// addNode 组件登记内实现（Python drawable.py:51-81 add_node 步骤一比一；
// D-13-5：isinstance 链 → 能力接口断言，断言序=Python isinstance 分支序）
func (d *Drawable) addNode(nodeID string, component any) {
	d.nodeOrder = append(d.nodeOrder, nodeID)
	// drawable.py:58-73：LoopComponent/AdvancedLoopComponent 分支
	if src, ok := component.(LoopSubgraphSource); ok {
		subgraph := src.LoopDrawableSubgraph()
		// drawable.py:62-70：end 未设时遍历子图发现出度=0 的 end 节点
		if len(subgraph.EndNodes) == 0 {
			outDegrees := map[string]int{}
			for id := range subgraph.Nodes {
				outDegrees[id] = 0
			}
			for _, edge := range subgraph.Edges {
				if _, ok := outDegrees[edge.Source]; !ok {
					outDegrees[edge.Source] = 0
				}
				outDegrees[edge.Source]++
			}
			for id, deg := range outDegrees {
				if deg == 0 {
					subgraph.EndNodes = append(subgraph.EndNodes, subgraph.Nodes[id])
				}
			}
		}
		d.graph.Nodes[nodeID] = &DrawableSubgraphNode{
			DrawableNode: DrawableNode{ID: nodeID}, Subgraph: subgraph}
		d.loopNodes[nodeID] = struct{}{}
		// drawable.py:73：自环边
		d.addEdge(nodeID, nodeID, false, false, nil)
		return
	}
	// drawable.py:74-76：SubWorkflowComponent 分支
	if src, ok := component.(SubgraphSource); ok {
		d.graph.Nodes[nodeID] = &DrawableSubgraphNode{
			DrawableNode: DrawableNode{ID: nodeID},
			Subgraph:     src.SubWorkflowDrawableSubgraph()}
		return
	}
	// drawable.py:77-79：BranchComponent/IntentDetectionComponent 分支
	if src, ok := component.(BranchRouterSource); ok {
		d.graph.Nodes[nodeID] = &DrawableNode{ID: nodeID}
		d.addEdge(nodeID, "", true, false, src.DrawableRouter())
		return
	}
	// drawable.py:80-81：默认普通节点
	d.graph.Nodes[nodeID] = &DrawableNode{ID: nodeID}
}

// addEdge 加边内实现（Python drawable.py:104-124 add_edge 步骤一比一）
func (d *Drawable) addEdge(source, target string, conditional, streaming bool, data any) {
	// drawable.py:107-109：循环源强制条件边
	if _, ok := d.loopNodes[source]; ok {
		d.graph.Edges = append(d.graph.Edges,
			&DrawableEdge{Source: source, Target: target, Conditional: true})
		return
	}
	// drawable.py:110-113：非条件边直落
	if !conditional {
		d.graph.Edges = append(d.graph.Edges, &DrawableEdge{Source: source, Target: target,
			Conditional: conditional, Streaming: streaming})
		return
	}
	// drawable.py:114-124：条件边展开（D-13-5：isinstance(BranchRouter) →
	// DrawableBranchTargetsSource 断言；否则 getTargets 函数壳）
	var targets []string
	var datas []string
	if src, ok := data.(DrawableBranchTargetsSource); ok {
		targets, datas = src.GetDrawableBranchTargets()
	} else {
		targets = getTargets(data)
	}
	for i, tgt := range targets {
		// drawable.py:123：datas[i] if branch_datas is not None else None → 空串
		dataStr := ""
		if datas != nil {
			dataStr = datas[i]
		}
		d.graph.Edges = append(d.graph.Edges, &DrawableEdge{Source: source, Target: tgt,
			Data: dataStr, Conditional: conditional, Streaming: streaming})
	}
}

// getTargets 返回类型注解反射的目标提取（Python drawable.py:29-43 _get_targets；
// 偏差 D-13-1：Go 无 Literal 类型注解反射且调用形态不可达（两条 Router() 恒
// *flow.BranchRouter），函数壳保留——非 callable 一律 nil）
func getTargets(_ any) []string {
	return nil
}
