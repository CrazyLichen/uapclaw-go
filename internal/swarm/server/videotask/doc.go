// Package videotask 提供语音委派任务的 AgentServer 侧绑定与检查点协议。
//
// 对齐 Python jiuwenswarm/server/runtime/video_task/（bridge/execution/rail/server_adapter）：
// 网关侧 duplex 包发出 managed_task_binding 的 CHAT_SEND 后，本包在 AgentServer 执行侧
// 识别 video_tool 渠道请求，给会话专属 DeepAgent 安装 VoiceAgentTaskRail（priority 81），
// 在模型调用回路前后通过 RemoteTaskEndpoint（E2A push/ACK 环）回网关账本执行
// bind/claim/context_written/observed/close/settle 命令——实现「任务跑一半用户改需求，
// 跑着的 Agent 感知」。
//
// 文件目录：
//
//	videotask/
//	├── doc.go            # 包文档
//	├── bridge.go         # RemoteTaskEndpoint + ACK 登记（bridge.py）
//	├── execution.go      # TaskExecutionBinding + BindTaskExecution（execution.py）
//	├── rail.go           # VoiceAgentTaskRail + 分派（rail.py）
//	└── server_adapter.go # VOICE_TASK_CHECKPOINT_ACK / VOICE_TASK_FILES RPC（server_adapter.py）
//
// 对应 Python 代码：jiuwenswarm/server/runtime/video_task/
package videotask
