// Package workspace_content 提供工作空间文件的双语（中文/英文）模板常量。
//
// 本包存储了智能体工作空间中各类文件（IDENTITY.md、AGENT.md、SOUL.md、
// HEARTBEAT.md、session_memory.md、MEMORY.md）的中文和英文模板内容，
// 以及工作空间头部信息、上下文文件标题、目录描述等常量定义。
//
// 文件目录：
//
//	workspace_content/
//	├── doc.go                    # 包文档
//	├── workspace_header.go       # 工作空间头部常量（标题、重要文件表、上下文等）
//	├── template_identity.go      # IDENTITY.md 中英文模板
//	├── template_agent.go         # AGENT.md 中英文模板
//	├── template_soul.go          # SOUL.md 中英文模板
//	├── template_heartbeat.go     # HEARTBEAT.md 中英文模板
//	├── template_session_memory.go # session_memory.md 中英文模板
//	├── template_memory.go        # MEMORY.md 中英文模板
//	└── workspace_content_test.go # 单元测试
//
// 对应 Python 代码：openjiuwen/harness/prompts/workspace_content/
package workspace_content
