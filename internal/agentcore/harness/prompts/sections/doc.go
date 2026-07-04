// Package sections 提供 DeepAgent 系统提示词的各节构建函数。
//
// 每个节对应一个 BuildXxxSection 函数，返回单语言或双语言（cn/en）的 PromptSection。
// 模板文本严格对齐 Python 源码 openjiuwen/harness/prompts/sections/ 中的定义。
//
// 文件目录：
//
//	sections/
//	├── doc.go                   # 包文档
//	├── section_name.go          # 节名称常量
//	├── identity.go              # 身份节
//	├── safety.go                # 安全节
//	├── context.go               # 上下文/工具节
//	├── skills.go                # 技能节
//	├── memory.go                # 记忆节（主动/被动/只读）
//	├── external_memory.go       # 外部记忆节
//	├── workspace.go             # 工作空间节
//	├── progressive_tool_rail.go # 渐进式工具导航与规则节
//	├── heartbeat.go             # 心跳节
//	├── coding_memory.go         # 编码记忆节
//	├── session_tools.go         # 会话工具节
//	├── agent_mode.go            # Agent 模式节（Plan 模式）
//	├── task_tool.go             # 任务工具节
//	├── task_completion.go       # 完成信号节
//	├── todo.go                  # 待办节
//	├── reload.go                # 上下文压缩节
//	└── sections_test.go        # 测试
//
// 对应 Python 代码：openjiuwen/harness/prompts/sections/
package sections
