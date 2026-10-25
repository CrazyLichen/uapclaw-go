// Package routing 提供 Gateway 与 AgentServer 之间的路由和客户端连接。
//
// AgentClient 是 Gateway 侧与 AgentServer 通信的核心组件：
//   - Connect: 启动接收循环，等待 connection.ack 就绪通知
//   - SendRequest: 非流式请求，等单个完整响应
//   - SendRequestStream: 流式请求，持续接收 chunk 直到 is_complete
//   - receiverLoop: 统一消费 transport.Recv()，区分事件帧/server_push/正常响应
//
// RouteBinding 描述 WS 路径的消息处理策略，预留多路径框架（/ws、/acp、/tui）。
//
// SessionMap 将企业 IM 稳定身份映射为轮转 agent session_id（checkpoint 持久化，
// Python session_map.py；scope 从 config gateway.session_map_scope 读一次）。
//
// PendingInteraction 统一追问上下文（群聊/DM 两模式），JSON 文件持久化 + 24h TTL
// （Python interaction_context.py；消费方 im_pipeline 11.12 ✅ / feishu 11.16 / wecom 11.21 ⤵️）。
//
// Python: jiuwenswarm/gateway/routing/agent_client.py (WebSocketAgentServerClient)
//
// 文件目录：
//
//	routing/
//	├── doc.go                 # 包文档
//	├── agent_client.go        # AgentClient 完整实现
//	├── route_binding.go       # RouteBinding 多路径 WS 路由绑定
//	├── session_map.go         # SessionMap 企业通道会话映射（11.7）
//	└── interaction_context.go # PendingInteraction 追问上下文（11.8）
//
// 对应 Python 代码：jiuwenswarm/gateway/routing/
package routing
