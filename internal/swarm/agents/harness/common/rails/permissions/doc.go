// Package permissions 提供数字分身场景下的权限上下文、工具权限检查、权限配置 RPC，
// 以及 root 权限卡的串行化队列与意图窗口。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/（38 个 .py / 11,463 行），
// 当前已完成权限上下文与配置 RPC 部分，以及 root 五文件（9.90c）。剩余约 10,000 行
// 按批次推进，见 IMPLEMENTATION_PLAN.md 的 9-H 节。
//
// # root 权限链路
//
// root 系列解决的是：LLM 一批并行工具调用产生多个权限中断时，如何收敛成
// 一条串行队列，做到一次只问用户一张卡、答案精确不串台、对不齐就冻结会话。
//
// 数据流（详见 docs/superpowers/specs/2026-11-07-root-permission-5-files-design.md）：
//
//	root_context_rail       组装 RootDecisionContext（意图窗口）并绑定
//	        ↓
//	root_permission_queue_rail  发号码牌 ToolInvocationKeyV1，抛中断
//	        ↓
//	root_permission_queue   排队串行化，冻结卡与上下文，校验答案归属
//	        ↓
//	root_ask_user           用户反问时把问答对并入意图窗口
//
// # 已知缺口
//
// ⤵️ 意图窗口的消费方待 9.90d（_auto_permission）批次回填：
// RootDecisionContext 目前只被两个 root rail 读写，尚无判定逻辑消费。
// Python 侧的三处消费方是 auto_reviewer._intent_view、
// _auto_permission/before_tool.resolve_original_user_intent、
// url_safety.has_valid_ask_user_clarification。
// 在此之前本包 RootDecisionContext / OriginalUserIntent / FileDeliveryConstraintText /
// HasValidAskUserClarification 均无实际调用方，属"管道就位、未接水源"。
//
// ⤵️ 非权限续接的包装器校验（RootPermissionWrapperResume 的完整校验链，
// 涉及 ProgressiveToolRail._call_discovered_tool / ToolCallTool / Runner.resource_mgr）
// 依赖 Go 侧 progressive_tool_rail 与 tool discovery 的等价内部结构，
// 待对应章节落地后回填。
//
// 文件目录：
//
//	permissions/
//	├── doc.go                            # 包文档
//	├── owner_scopes.go                    # OwnerScopesPermissionContext + CheckAvatarPermission/ResolveOwnerScopeLevel/MatchArgs/PersistToOwnerScope
//	├── permissions_config_rpc.go          # 10 个权限配置 RPC 分发 + config CRUD 辅助函数
//	├── permissions_persist.go             # 权限配置落盘 helper（PersistPermissionAllowRule/PersistExternalDirectoryAllow/PersistCliTrustedDirectoryWithOverrides）
//	├── protected_paths.go                 # 默认保护写入路径 + MergeProtectedWritePaths
//	├── tool_invocation_key.go             # ToolInvocationKeyV1 号码牌 + NormalizeToolInvocationText
//	├── root_context.go                    # RootIntentTurn/RootDecisionContext 意图窗口 + ContextVar 绑定 + 意图投影
//	├── root_permission_queue.go           # RootPermissionQueue 串行化队列状态机（active/pending/resuming + quarantine）
//	├── root_permission_queue_rail.go      # RootPermissionQueueRail 身份绑定与闩锁 + RootPermissionCompletionRail
//	├── root_ask_user.go                   # 用户反问续接：冻结快照 / 还原澄清 / 追加意图窗口
//	└── root_context_rail.go               # RootContextRail 上下文绑定 rail（随 9.90c 后续提交）
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/ + jiuwenswarm/common/config.py（CRUD 部分）
package permissions
