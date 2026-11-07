// Package external 提供外部成员（第三方 CLI 驱动）的入站上下文渲染。
// 这些辅助函数把原始团队模型（消息、任务）转成与进程内成员经协调处理器收到的
// 完全相同的 <team-inbound> / <team-event> XML，复用共享的 inbound_render 结构与
// i18n 提示文案，让外部 agent 读到一致的形状。无 IO、无 LLM 调用，完全确定性、可单测。
// 文件目录：
//
//	external/
//	├── doc.go            # 包文档
//	└── format.go         # RenderMessage / RenderMessages（消息装配）
//
// 待 25.4 回填：RenderTaskLine / RenderTaskBoard（依赖任务板状态机语义）
// 对应 Python 代码：openjiuwen/agent_teams/external/format.py
package external
