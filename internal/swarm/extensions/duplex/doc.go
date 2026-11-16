// Package duplex 提供全双工音视频旁路通道（领域 23.9）。
//
// 一比一移植 Python jiuwenswarm/extensions/video_duplex/：实时音视频问答走
// WebSocket 代理直连 vLLM Omni / JoyAI 实时模型（不进 Agent）；需要真干活的
// 请求经工具调用转持久任务单，由标准 CHAT_SEND 走普通 AgentServer 管线
// （channel_id="video_tool"）。装配层、配置面板、ASR/TTS、搜索供给、遥测
// 落盘与任务账本均在本包内。
//
// 文件目录：
//
//	duplex/
//	├── doc.go                  # 包文档
//	├── settings.go            # 配置持久化与密钥打码（settings.py）
//	├── video_files.go         # 文件资源归一（video_files.py）
//	├── connection_check.go    # 连通性探测（connection_check.py）
//	├── extension.go           # video-duplex 插件装配（extension.py）
//	├── video_voice.go         # ASR/TTS 适配（video_voice.py）
//	├── video_search.go        # 核心 Agent 执行与搜索任务（video_search.py）
//	├── qwen_omni_tools.go     # vLLM-Omni 工具协议（qwen_omni_tools.py）
//	├── qwen_omni_gateway.go  # vLLM-Omni WS 代理（qwen_omni_gateway.py）
//	├── joyai_provider.go      # JoyAI 实时 provider（joyai_provider.py）
//	├── video_live.go          # 实时 RPC 编排与遥测（video_live.py）
//	├── task_adapter.go        # Agent 执行器适配（task_adapter.py）
//	├── conversation_context.go # 会话上下文同步（conversation_context.py）
//	└── tasks/                # 持久任务管理（backend/tasks/ + bridge.py）
//
// 对应 Python 代码：jiuwenswarm/extensions/video_duplex/
// （tasks/bridge.go 对应 gateway/extensions/video_duplex/backend/tasks/bridge.py，
// 归属偏差 O-1 见 spec）

package duplex
