// Package rails 提供 Swarm 侧的 Rail 扩展实现（对齐 Python common/rails）。
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/ 下的 Rail 实现，
// 在 agentcore 的通用 Rail 基础上增加 Swarm 专属逻辑。
// 文件目录：
//
//	rails/
//	├── doc.go                        # 包文档
//	├── avatar_rail.go                # AvatarPromptRail 数字分身 Rail
//	├── project_memory_rail.go        # ProjectMemoryRail 项目记忆护栏
//	├── response_prompt_rail.go       # ResponsePromptRail 响应提示词护栏
//	├── runtime_prompt_rail.go        # RuntimePromptRail 运行时提示词护栏
//	├── stream_event_helpers.go       # StreamEvent 辅助函数（boolish/infer/extract/parse/truncate/常量）
//	├── stream_event_context.go       # StreamEvent 上下文修复（ensureJSONArguments/fixMissingQuotes/fixIncompleteToolContext）
//	├── stream_event_emit.go          # StreamEvent 流式事件发射（emitToolCall/Result/Update/ContextUsage/AskUser/Todo）
//	├── stream_event_rail.go          # JiuClawStreamEventRail 流事件护栏（钩子 + 暂停/中止/收集 API）
//	├── structured_ask_user_rail.go    # StructuredAskUserRail + StructuredAskUserPayload
//	├── structured_ask_user_tool.go    # StructuredAskUserTool + 扩展 schema
//	├── ask_user_contract.go           # 结构化 ask_user 的共享有界契约（字段上限/校验）
//	├── structured_ask_user_validation.go # 结构化 ask_user 的 questions 严格校验（对齐 resolve_interrupt 校验段）
//	├── project_memory/               # 项目记忆文件发现/加载/缓存/合并
//	│   ├── doc.go                    # 子包文档
//	│   ├── files.go                  # 文件发现/加载/缓存/合并
//	│   └── section.go                # PromptSection 工厂
//	└── permissions/
//	    ├── doc.go                    # 包文档
//	    └── owner_scopes.go           # owner_scopes 权限判定（CheckAvatarPermission/ResolveOwnerScopeLevel/MatchArgs/PersistToOwnerScope）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/
package rails
