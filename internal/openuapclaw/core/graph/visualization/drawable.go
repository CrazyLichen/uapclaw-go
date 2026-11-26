package visualization

import (
	"encoding/base64"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"

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
	// Python dict 覆盖语义：重复 compID 时覆盖节点且保持原插入序（_workflow.py
	// 无重复检查，comp_configs/add_node 均为 dict 覆盖；R-2 修正：不重复 append 序）
	_, exists := d.graph.Nodes[nodeID]
	if !exists {
		d.nodeOrder = append(d.nodeOrder, nodeID)
		d.graph.SubNodeOrder = append(d.graph.SubNodeOrder, nodeID)
	}
	// drawable.py:58-73：LoopComponent/AdvancedLoopComponent 分支
	if src, ok := component.(LoopSubgraphSource); ok {
		subgraph := src.LoopDrawableSubgraph()
		// drawable.py:62-70：end 未设时遍历子图发现出度=0 的 end 节点
		// （R-1 修正：Python out_degrees dict 按 subgraph.nodes 插入序迭代 →
		// EndNodes append 序确定；Go 按 SubNodeOrder 序迭代对齐）
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
			for _, id := range d.subNodeOrder(subgraph) {
				if outDegrees[id] == 0 {
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

// ──────────────────────────── mermaid 节点/连线（_MermaidDiagram 内部件） ────────────────────────────

// snakeCaseRe Node id_ 归一（mermaid-py utils.py:34-50 text_to_snake_case 一比一）
var snakeCaseRe = regexp.MustCompile(`[^a-zA-Z0-9_\.-]`)

// snakeCase 归一实现（utils.py:44-50：非字母数字下划线点横线 → _，再转小写）
func snakeCase(text string) string {
	return strings.ToLower(snakeCaseRe.ReplaceAllString(text, "_"))
}

// mermaidNode mermaid 节点（mermaid-py node.py Node 消费面复刻：
// href/styles 在 drawable.py 零使用不落；shape 消费面 normal/round-edge +
// 子图形态；D-13 复刻面=drawable.py 消费面）
type mermaidNode struct {
	id        string // 已 snakeCase（Node.id_）
	content   string // Node.content
	normal    bool   // true=normal [ ]，false=round-edge ( )
	subNodes  []*mermaidNode
	isSub     bool
	direction string // 子图方向，恒 "TB"（drawable.py:310 TOP_TO_BOTTOM）
}

// stringNormal 单节点形态（Node.__str__ else 分支：id_+start+"content"+end）
func (m *mermaidNode) stringNormal() string {
	start, end := "[", "]"
	if !m.normal {
		start, end = "(", ")"
	}
	return m.id + start + "\"" + m.content + "\"" + end
}

// stringSubgraph 子图形态（Node.__str__ subgraph 分支逐字复刻；
// 仅子节点串首行加 \t，嵌套子图后续行不加深——Python str(f"\t{node}") 语义）
func (m *mermaidNode) stringSubgraph() string {
	lines := []string{
		"subgraph " + m.id + " [\"" + m.content + "\"]",
		"\tdirection " + m.direction,
	}
	for _, sn := range m.subNodes {
		lines = append(lines, "\t"+sn.String())
	}
	return strings.Join(lines, "\n") + "\nend"
}

// String 总入口（Node.__str__）
func (m *mermaidNode) String() string {
	if m.isSub {
		return m.stringSubgraph()
	}
	return m.stringNormal()
}

// mermaidLink 连线（mermaid-py link.py Link + drawable.py _ExtendLink 合体；
// headLeft 恒 ""（none）、headRight 恒 ">"（arrow）——drawable.py 全部消费走默认值）
type mermaidLink struct {
	origin     *mermaidNode
	end        *mermaidNode
	shape      string // "--" / "-.-" / "=="
	message    string
	idTag      string   // animate 时 "link_N"，空=无（_ExtendLink id_）
	properties []string // animate 时 ["animate: true"]
}

// String _ExtendLink.__str__ 一比一（drawable.py:210-228：
// origin.id_ + " " + [id_@] + head_left + shape + head_right + [|"msg"|] + " " +
// end.id_ + [\n tag {k: v, ...}]）
func (l *mermaidLink) String() string {
	tag := ""
	if l.idTag != "" {
		tag = l.idTag + "@"
	}
	propertiesStr := ""
	if len(l.properties) > 0 {
		propertiesStr = "\n" + tag + "{" + strings.Join(l.properties, ", ") + "}"
	}
	msg := ""
	if l.message != "" {
		msg = "|\"" + l.message + "\"|"
	}
	return l.origin.id + " " + tag + l.shape + ">" + msg + " " + l.end.id + propertiesStr
}

// subgraphMermaidNode 子图 mermaid 投影（drawable.py:184-190 _SubGraphNode 一比一）
type subgraphMermaidNode struct {
	node       *mermaidNode   // 子图头（drawable.py:309-310）
	links      []*mermaidLink // 子图内部 links（drawable.py:311）
	startNodes []*mermaidNode // drawable.py:304
	endNodes   []*mermaidNode // drawable.py:305
	breakNodes []*mermaidNode // drawable.py:306
}

// idGenerator 自增 ID（drawable.py:164-182 _NodeIdGenerator/_LinkIdGenerator 合体）
type idGenerator struct {
	prefix string
	n      int
}

// next 产出 "prefix_N"
func (g *idGenerator) next() string {
	g.n++
	return g.prefix + "_" + strconv.Itoa(g.n)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// truthyExpand Python truth(expand_subgraph) 等价：bool 取值，int 非零为真
func truthyExpand(v any) bool {
	switch x := v.(type) {
	case bool:
		return x
	case int:
		return x != 0
	}
	return false
}

// nextExpand drawable.py:251-253 层数递减：bool 恒传 / int<=0 恒传 / int 递减 1
func nextExpand(v any) any {
	if n, ok := v.(int); ok {
		if n > 0 {
			return n - 1
		}
		return n
	}
	return v
}

// containsNodeLike 节点身份判断（Python `node in list`：dataclass 值等 →
// Go 指针或 ID 等同；登记件即 Nodes 表同指针，ID 兜底测试注入场景）
func containsNodeLike(list []NodeLike, n NodeLike) bool {
	for _, x := range list {
		if x == n {
			return true
		}
		xn, ok1 := x.(*DrawableNode)
		nn, ok2 := n.(*DrawableNode)
		if ok1 && ok2 && xn.ID == nn.ID {
			return true
		}
		xs, ok1 := x.(*DrawableSubgraphNode)
		ns, ok2 := n.(*DrawableSubgraphNode)
		if ok1 && ok2 && xs.ID == ns.ID {
			return true
		}
	}
	return false
}

// nodeMermaidShape 形状判定（drawable.py:255-258/288-292：start/end → round-edge）
func nodeMermaidShape(list []NodeLike, n NodeLike) bool {
	return !containsNodeLike(list, n)
}

// ToMermaid 转 Mermaid 文本（Python drawable.py:234-263 to_mermaid 一比一；
// expandSubgraph any 承载 bool|int（D-13-3）；enableAnimation bool 收窄——
// Python isinstance(enable_animation, bool) 校验分支在 Go 静态类型下不可达不落
// （D-13-3 修订：全仓消费方恒传 bool，回收条件=出现非 bool 消费方）
// Python isinstance(title, str) 分支在 Go 串型形参下不可达不落）
func (d *Drawable) ToMermaid(title string, expandSubgraph any, enableAnimation bool) (string, error) {
	// drawable.py:239-241：expand 校验（112043）
	if _, ok := expandSubgraph.(bool); !ok {
		if n, isInt := expandSubgraph.(int); !isInt || n < 0 {
			return "", exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
				exception.WithParam("reason", "'expand_subgraph' type is not bool"))
		}
	}

	nodeIDs := &idGenerator{prefix: "node"}
	linkIDs := &idGenerator{prefix: "link"}
	mermaidNodes := map[string]*mermaidNode{}
	subMermaidNodes := map[string]*subgraphMermaidNode{}
	// drawable.py:248-258：节点分派（遍历序=插入序 nodeOrder）
	for _, id := range d.nodeOrder {
		node := d.graph.Nodes[id]
		if sg, isSG := node.(*DrawableSubgraphNode); isSG && truthyExpand(expandSubgraph) {
			subMermaidNodes[id] = d.genMermaidNode(nextExpand(expandSubgraph), sg, enableAnimation, nodeIDs, linkIDs)
		} else {
			normal := nodeMermaidShape(append(d.graph.StartNodes, d.graph.EndNodes...), node)
			mermaidNodes[id] = &mermaidNode{id: snakeCase(nodeIDs.next()), content: node.NodeID(), normal: normal}
		}
	}
	links := d.genMermaidLinks(mermaidNodes, subMermaidNodes, enableAnimation, linkIDs)
	// FlowChart._build_script 逐字复刻（graph.py:55-58 + flowchart/__init__.py:63-73）
	var sb strings.Builder
	sb.WriteString("---\ntitle: " + title + "\n---")
	sb.WriteString("\nflowchart TB")
	// drawable.py:261：普通节点在前、子图头节点在后（两个 dict 插入序）
	for _, id := range d.nodeOrder {
		if n, ok := mermaidNodes[id]; ok {
			sb.WriteString("\n\t" + n.String())
		}
	}
	for _, id := range d.nodeOrder {
		if n, ok := subMermaidNodes[id]; ok {
			sb.WriteString("\n\t" + n.node.String())
		}
	}
	for _, l := range links {
		sb.WriteString("\n\t" + l.String())
	}
	sb.WriteString("\n")
	return sb.String(), nil
}

// genMermaidNode 子图递归展开（Python drawable.py:276-315 _gen_mermaid_node 一比一）
func (d *Drawable) genMermaidNode(expandSubgraph any, node *DrawableSubgraphNode, enableAnimation bool,
	nodeIDs, linkIDs *idGenerator) *subgraphMermaidNode {
	mermaidNodes := map[string]*mermaidNode{}
	subMermaidNodes := map[string]*subgraphMermaidNode{}
	graph := node.Subgraph
	for _, subID := range d.subNodeOrder(graph) {
		subNode := graph.Nodes[subID]
		if sg, isSG := subNode.(*DrawableSubgraphNode); isSG && truthyExpand(expandSubgraph) {
			subMermaidNodes[subID] = d.genMermaidNode(nextExpand(expandSubgraph), sg, enableAnimation, nodeIDs, linkIDs)
		} else {
			normal := nodeMermaidShape(append(graph.StartNodes, graph.EndNodes...), subNode)
			mermaidNodes[subID] = &mermaidNode{id: snakeCase(nodeIDs.next()), content: subNode.NodeID(), normal: normal}
		}
	}
	links := d.genSubgraphLinks(graph, mermaidNodes, subMermaidNodes, enableAnimation, linkIDs)

	// drawable.py:296-302 get_node_by_id：mermaid_nodes → subgraph_mermaid_nodes.node
	// → "invalid node id"（Python 裸字符串属潜在 AttributeError 死分支，Go 以
	// 占位节点承载，实现注记不另立偏差）
	getNodeByID := func(nodeID string) *mermaidNode {
		if n, ok := mermaidNodes[nodeID]; ok {
			return n
		}
		if sn, ok := subMermaidNodes[nodeID]; ok {
			return sn.node
		}
		return &mermaidNode{id: "invalid node id"}
	}
	subgraphStartNodes := make([]*mermaidNode, 0, len(graph.StartNodes))
	for _, sn := range graph.StartNodes {
		subgraphStartNodes = append(subgraphStartNodes, getNodeByID(sn.NodeID()))
	}
	subgraphEndNodes := make([]*mermaidNode, 0, len(graph.EndNodes))
	for _, sn := range graph.EndNodes {
		subgraphEndNodes = append(subgraphEndNodes, getNodeByID(sn.NodeID()))
	}
	subgraphBreakNodes := make([]*mermaidNode, 0, len(graph.BreakNodes))
	for _, sn := range graph.BreakNodes {
		subgraphBreakNodes = append(subgraphBreakNodes, getNodeByID(sn.NodeID()))
	}
	subNodes := make([]*mermaidNode, 0, len(mermaidNodes)+len(subMermaidNodes))
	for _, id := range d.subNodeOrder(graph) {
		if n, ok := mermaidNodes[id]; ok {
			subNodes = append(subNodes, n)
		} else if sn, ok := subMermaidNodes[id]; ok {
			subNodes = append(subNodes, sn.node)
		}
	}
	// drawable.py:309-310：子图头节点（direction=TOP_TO_BOTTOM → "TB"）
	return &subgraphMermaidNode{
		node: &mermaidNode{id: snakeCase(nodeIDs.next()), content: node.ID,
			subNodes: subNodes, isSub: true, direction: "TB"},
		links:      links,
		startNodes: subgraphStartNodes,
		endNodes:   subgraphEndNodes,
		breakNodes: subgraphBreakNodes,
	}
}

// genMermaidLinks 顶层连线（Python drawable.py:317-356 _gen_mermaid_links 一比一 +
// drawable.py:354-355 子图 links 追加）
func (d *Drawable) genMermaidLinks(mermaidNodes map[string]*mermaidNode,
	subMermaidNodes map[string]*subgraphMermaidNode, enableAnimation bool, linkIDs *idGenerator) []*mermaidLink {
	links := d.genSubgraphLinks(d.graph, mermaidNodes, subMermaidNodes, enableAnimation, linkIDs)
	for _, id := range d.nodeOrder {
		if sn, ok := subMermaidNodes[id]; ok {
			links = append(links, sn.links...) // drawable.py:354-355
		}
	}
	return links
}

// genSubgraphLinks 连线四象限展开（Python drawable.py:321-352 一比一；
// graph 入参区分顶层/子图递归复用，start/end/break 取自该层 graph）
func (d *Drawable) genSubgraphLinks(graph *DrawableGraph, mermaidNodes map[string]*mermaidNode,
	subMermaidNodes map[string]*subgraphMermaidNode, enableAnimation bool, linkIDs *idGenerator) []*mermaidLink {
	links := []*mermaidLink{}
	for _, edge := range graph.Edges {
		// drawable.py:322-331：形状选择
		shape := "--"
		if edge.Conditional {
			shape = "-.-"
		}
		if edge.Streaming {
			shape = "=="
		}
		linkExtendID := ""
		linkExtendProps := []string(nil)
		if edge.Streaming && enableAnimation {
			linkExtendID = linkIDs.next()
			linkExtendProps = []string{"animate: true"}
		}
		// drawable.py:332-333：条件边标签
		message := ""
		if edge.Conditional && edge.Data != "" {
			message = edge.Data
		}
		mk := func(origin, end *mermaidNode) *mermaidLink {
			return &mermaidLink{origin: origin, end: end, shape: shape,
				message: message, idTag: linkExtendID, properties: linkExtendProps}
		}
		// drawable.py:334-352 四象限
		srcNormal, srcIsNormal := mermaidNodes[edge.Source]
		tgtNormal, tgtIsNormal := mermaidNodes[edge.Target]
		srcSub, srcIsSub := subMermaidNodes[edge.Source]
		tgtSub, tgtIsSub := subMermaidNodes[edge.Target]
		switch {
		case srcIsNormal && tgtIsNormal:
			links = append(links, mk(srcNormal, tgtNormal))
		case srcIsNormal && tgtIsSub:
			for _, n := range tgtSub.startNodes {
				links = append(links, mk(srcNormal, n))
			}
		case srcIsSub && tgtIsNormal:
			for _, n := range srcSub.endNodes {
				links = append(links, mk(n, tgtNormal))
			}
			for _, n := range srcSub.breakNodes {
				links = append(links, mk(n, tgtNormal))
			}
		case srcIsSub && tgtIsSub:
			for _, ns := range srcSub.endNodes {
				for _, nt := range tgtSub.startNodes {
					links = append(links, mk(ns, nt))
				}
			}
		}
	}
	return links
}

// subNodeOrder 子图节点插入序（Python dict 插入序承载；DrawableGraph 本身不记序，
// 以注册序辅助切片 lazy 构建不可行——子图 nodes 由外部构造，Go map 无序，
// 此处按 drawable.SubgraphNodes 辅助序或 ID 排序兜底：为对齐 Python 插入序，
// DrawableGraph 增加 order 记录——由 Drawable.addNode 的子图路径与测试构造方填充）
func (d *Drawable) subNodeOrder(graph *DrawableGraph) []string {
	if len(graph.SubNodeOrder) > 0 {
		return graph.SubNodeOrder
	}
	ids := make([]string, 0, len(graph.Nodes))
	for id := range graph.Nodes {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ──────────────────────────── 常量 ────────────────────────────

// DefaultMermaidInkServer mermaid.ink 默认服务（mermaid-py __main__.py:194-196）
const DefaultMermaidInkServer = "https://mermaid.ink"

// MermaidInkServerEnv 服务地址 env 键（Python MERMAID_INK_SERVER 同名）
const MermaidInkServerEnv = "MERMAID_INK_SERVER"

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMermaidPNG 转 Mermaid 并渲染为 png（Python drawable.py:130-132 to_mermaid_png；
// expandSubgraph any 承载 bool|int，D-13-3）
func (d *Drawable) ToMermaidPNG(title string, expandSubgraph any) ([]byte, error) {
	script, err := d.ToMermaid(title, expandSubgraph, false)
	if err != nil {
		return nil, err
	}
	return mermaidRender(script, "img", "format=png")
}

// ToMermaidSVG 转 Mermaid 并渲染为 svg（Python drawable.py:134-136 to_mermaid_svg；
// enable_animation 恒 True，drawable.py:274）
func (d *Drawable) ToMermaidSVG(title string, expandSubgraph any) ([]byte, error) {
	script, err := d.ToMermaid(title, expandSubgraph, true)
	if err != nil {
		return nil, err
	}
	return mermaidRender(script, "svg", "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mermaidRender mermaid.ink 渲染（mermaid-py __main__.py:165-225 一比一：
// urlsafe base64 → GET {server}/{endpoint}/{b64}?{query}；svg query 为空仍带尾部 ?）
func mermaidRender(script, endpoint, query string) ([]byte, error) {
	server := os.Getenv(MermaidInkServerEnv)
	if server == "" {
		server = DefaultMermaidInkServer
	}
	b64 := base64.URLEncoding.EncodeToString([]byte(script))
	url := server + "/" + endpoint + "/" + b64 + "?" + query
	resp, err := http.Get(url)
	if err != nil {
		return nil, exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason", err.Error()))
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason", err.Error()))
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, exception.BuildError(exception.StatusDrawableGraphToMermaidInvalid,
			exception.WithParam("reason",
				fmt.Sprintf("HTTP %d Error: %s", resp.StatusCode, truncateDetails(body))))
	}
	return body, nil
}

// truncateDetails MermaidError Details 段（前 200 字符，mermaid-py :119-123）
func truncateDetails(body []byte) string {
	s := string(body)
	if len(s) > 200 {
		return s[:200] + "..."
	}
	return s
}
