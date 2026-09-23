// Package rails 提供 Swarm 侧的 Rail 扩展实现（对齐 Python common/rails）。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/ 下的 Rail 实现，
// 在 agentcore 的通用 Rail 基础上增加 Swarm 专属逻辑。
//
// 文件目录：
//
//	rails/
//	├── doc.go                        # 包文档
//	├── project_memory_rail.go        # ProjectMemoryRail 项目记忆护栏
//	├── response_prompt_rail.go       # ResponsePromptRail 响应提示词护栏
//	├── structured_ask_user_rail.go    # StructuredAskUserRail + StructuredAskUserPayload
//	├── structured_ask_user_tool.go    # StructuredAskUserTool + 扩展 schema
//	├── avatar_rail.go                # AvatarPromptRail 数字分身 Rail
//	├── runtime_prompt_rail.go        # RuntimePromptRail 运行时提示词护栏
//	├── project_memory/               # 项目记忆文件发现/加载/缓存/合并
//	│   ├── doc.go                    # 子包文档
//	│   ├── files.go                  # 文件发现/加载/缓存/合并
//	│   └── section.go                # PromptSection 工厂
//	└── permissions/
//	    ├── doc.go                    # 包文档
//	    └── owner_scopes.go           # OwnerScopesPermissionContext 权限上下文
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/
package rails
