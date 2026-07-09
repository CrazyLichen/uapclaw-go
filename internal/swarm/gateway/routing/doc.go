// Package routing 提供 Gateway 与 AgentServer 之间的路由和客户端连接。
//
// AgentClient 是 Gateway 侧与 AgentServer 通信的核心组件：
//   - Connect: 启动接收循环，等待 connection.ack 就绪通知
//   - SendRequest: 非流式请求，等单个完整响应
//   - SendRequestStream: 流式请求，持续接收 chunk 直到 is_complete
//   - receiverLoop: 统一消费 transport.Recv()，区分事件帧/server_push/正常响应
//
// 对齐 Python: jiuwenswarm/gateway/routing/agent_client.py (WebSocketAgentServerClient)
//
// 文件目录：
//
//	routing/
//	├── doc.go              # 包文档
//	└── agent_client.go     # AgentClient 完整实现
//
// 对应 Python 代码：jiuwenswarm/gateway/routing/
package routing
