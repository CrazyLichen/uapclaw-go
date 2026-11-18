// Package workflow_vertex 提供 8.11 Vertex 双链批入/流入的集成测试。
//
// 真实跨子系统链路：graph.Vertex（批入编排/双链握手）→ graph/stream_actor
// （ActorManager 队列与首帧拉起）→ session/state（WorkflowCommitState IO 暂存）
// → runner/callback（NODE_EXECUTED 事件）→ session/tracer（WorkflowHandlerValid）。
// 对齐 Python vertex.py 的会话级编排行为。
//
// 文件目录：
//
//	workflow_vertex/
//	├── doc.go                 # 包文档
//	└── vertex_stream_test.go # Vertex 双链/循环/清洗集成用例
//
// 对应 Python 代码：openjiuwen/core/graph/vertex.py
package workflow_vertex
