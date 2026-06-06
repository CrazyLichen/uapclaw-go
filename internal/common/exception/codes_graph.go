package exception

// ──────────────────────────── 全局变量 ────────────────────────────

// =============================================================================================================
// Graph State Commit 112030–112039
// =============================================================================================================

var (
	// StatusGraphStateCommitError 图状态提交错误
	StatusGraphStateCommitError = NewStatusCode(
		"GRAPH_STATE_COMMIT_ERROR", 112030,
		"graph commit state error, error='{reason}'")
)

// =============================================================================================================
// Drawable Graph 112020–112029
// =============================================================================================================

var (
	// StatusDrawableGraphStartNodeInvalid drawable_graph 起始节点无效
	StatusDrawableGraphStartNodeInvalid = NewStatusCode(
		"DRAWABLE_GRAPH_START_NODE_INVALID", 112020,
		"drawable_graph start node is invalid, node={node_id}, reason={reason}")
	// StatusDrawableGraphEndNodeInvalid drawable_graph 结束节点无效
	StatusDrawableGraphEndNodeInvalid = NewStatusCode(
		"DRAWABLE_GRAPH_END_NODE_INVALID", 112021,
		"drawable_graph end node is invalid, node={node_id}, reason={reason}")
	// StatusDrawableGraphBreakNodeInvalid drawable_graph 中断节点无效
	StatusDrawableGraphBreakNodeInvalid = NewStatusCode(
		"DRAWABLE_GRAPH_BREAK_NODE_INVALID", 112022,
		"drawable_graph break node is invalid, node={node_id}, reason={reason}")
	// StatusDrawableGraphToMermaidInvalid drawable_graph 转 Mermaid 错误
	StatusDrawableGraphToMermaidInvalid = NewStatusCode(
		"DRAWABLE_GRAPH_TO_MERMAID_INVALID", 112043,
		"drawable_graph to_mermaid error, reason={reason}")
)

// =============================================================================================================
// Stream Graph Execution 112030–112049
// =============================================================================================================

var (
	// StatusGraphStreamActorExecutionError 图流 actor 执行错误
	StatusGraphStreamActorExecutionError = NewStatusCode(
		"GRAPH_STREAM_ACTOR_EXECUTION_ERROR", 112030,
		"actor manager execute error, error='{reason}'")
)

// =============================================================================================================
// Graph Vertex Execution 112050–112069
// =============================================================================================================

var (
	// StatusGraphVertexExecutionError 图顶点执行错误
	StatusGraphVertexExecutionError = NewStatusCode(
		"GRAPH_VERTEX_EXECUTION_ERROR", 112050,
		"vertex execute error, error='{reason}', node_id={node_id}")
	// StatusGraphVertexStreamCallTimeout 图顶点流式调用超时
	StatusGraphVertexStreamCallTimeout = NewStatusCode(
		"GRAPH_VERTEX_STREAM_CALL_TIMEOUT", 112051,
		"vertex stream timeout, timeout={timeout}, node_id={node_id}")
	// StatusGraphVertexStreamCallError 图顶点流式调用错误
	StatusGraphVertexStreamCallError = NewStatusCode(
		"GRAPH_VERTEX_STREAM_CALL_ERROR", 112052,
		"vertex stream call error, error='{reason}', node_id={node_id}")
)

// =============================================================================================================
// Pregel Graph 112100–112199
// =============================================================================================================

var (
	// StatusPregelGraphNodeIDInvalid Pregel 图节点 ID 无效
	StatusPregelGraphNodeIDInvalid = NewStatusCode(
		"PREGEL_GRAPH_NODE_ID_INVALID", 112100,
		"node id is invalid, node_id={node_id}, error='{reason}'")
	// StatusPregelGraphNodeInvalid Pregel 图节点无效
	StatusPregelGraphNodeInvalid = NewStatusCode(
		"PREGEL_GRAPH_NODE_INVALID", 112101,
		"node is invalid, node_id={node_id}, error='{reason}'")
	// StatusPregelGraphEdgeInvalid Pregel 图边无效
	StatusPregelGraphEdgeInvalid = NewStatusCode(
		"PREGEL_GRAPH_EDGE_INVALID", 112102,
		"edge is invalid, source_id={source_id}, target_id={target_id}, error='{reason}'")
	// StatusPregelGraphConditionEdgeInvalid Pregel 图条件边无效
	StatusPregelGraphConditionEdgeInvalid = NewStatusCode(
		"PREGEL_GRAPH_CONDITION_EDGE_INVALID", 112103,
		"condition edge is invalid, source_id={source_id}, error='{reason}'")
)
