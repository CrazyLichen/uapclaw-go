// Package server 提供 Agent 核心服务，对齐 Python AgentWebSocketServer，适配单进程 ChannelTransport 模式。
//
// 从 ChannelTransport.SendCh() 消费 E2AEnvelope，每个请求独立 goroutine 处理。
// 流式任务通过 sessionStreamTasks 追踪，支持按会话取消。
// 主分发逻辑在 handleEnvelope 中，按 ReqMethod switch 分发到具体 handler。
//
// 文件目录：
//
//	server/
//	├── doc.go                 # 包文档
//	├── agent_server.go        # AgentServer 结构体、生命周期、消费循环、流式任务管理
//	├── handle_envelope.go     # 主分发逻辑：handleEnvelope/handleUnary/handleStream/handleCancel 及辅助函数
//	├── handle_session.go      # Session 管理 handler（list/create/delete/rename/switch/rewind/fork）
//	├── handle_command.go      # Command 系列 handler（add_dir/chrome/compact/context/recap/diff/model/mcp/sandbox/resume/session/status）
//	├── handle_team.go         # Team handler（delete/snapshot/history_get）
//	├── handle_history.go      # History handler（history_get 含分页）
//	├── handle_permissions.go  # Permissions handler（tools/rules/approval_overrides 二次分发）
//	├── handle_agents.go       # Agents handler（list/get/create/update/delete/enable/disable/tools_list）
//	├── handle_extensions.go   # Extensions + Hooks handler（list/import/delete/toggle/hooks_list）
//	├── handle_harness.go      # Harness + Schedule handler（packages/schedule 系列）
//	├── handle_browser.go      # Browser handler（start/runtime_restart）
//	├── handle_config.go       # Config handler（cache_clear/reload_config）
//	├── handle_initialize.go   # Initialize + ACP handler（initialize/acp_tool_response）
//	├── adapter/               # Agent 适配器（Code/Deep/Factory）
//	├── gateway_push/          # 进程内传输（ChannelTransport）
//	└── runtime/               # Agent 运行时（AgentManager/JiuWenClaw/SessionManager）
//
// 对应 Python 代码：jiuwenswarm/server/agent_server.py
package server
