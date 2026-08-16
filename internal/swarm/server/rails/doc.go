// Package rails 提供 Swarm 侧的 Rail 扩展实现。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/ 下的 Rail 实现，
// 在 agentcore 的通用 Rail 基础上增加 Swarm 专属逻辑。
//
// 文件目录：
//
//	rails/
//	├── doc.go                        # 包文档
//	├── structured_ask_user_rail.go    # StructuredAskUserRail + StructuredAskUserPayload
//	└── structured_ask_user_tool.go    # StructuredAskUserTool + 扩展 schema
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/ask_user_rail.py
package rails
