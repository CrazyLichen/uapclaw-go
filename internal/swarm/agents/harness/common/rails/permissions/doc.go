// Package permissions 提供数字分身场景下的权限上下文、工具权限检查、权限配置 RPC，
// 以及 root 权限卡的串行化队列与意图窗口。
//
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/（38 个 .py / 11,463 行），
// 当前已完成权限上下文与配置 RPC 部分、root 五文件（9.90c）、基础类型层
// （9.90a：tool_invocation_key / protected_paths / auto_permission 子包），以及
// 决策事实层（9.90b：工具能力分类 / 单次调用事实 / 策略评估 / 三层覆盖 / 网络
// 授权域 / 执行契约 / 沙箱与原生路径投影）。剩余部分按批次推进，见
// IMPLEMENTATION_PLAN.md 的 9-H 节。
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
// ✅ 已回填（9.90d-1 Task 10）：send_file_path_guard 落地，
// policy_eval 的 send_file 分支已接上 SendFilePathGuardEvaluator。
//
// ✅ 已回填（9.90d-2 Task 19）：permission_compose 完整版落地
// （permission_compose.go），permissions_layers 的最小实现已删除。
// 现覆盖 rules 合并 + P1 severity→action、approval_overrides、
// file_guard.paths 叠加、net_guard / shell_guard 合成、
// categories / package_builtin_rules 的 Host 事实保护、fail-closed 兜底。
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
// ⤵️ auto_permission 子包中 4 个常量（ShellDisplayPathPattern /
// ShellDisplayPreSplitPathPattern / SensitiveDisplayPathPattern /
// DeterministicReadonlyPublicWebFetchSourceKinds）在 Python 全仓零消费，
// 按规则 9.2-4 照抄保留，待 9.90d 的 reviewer_redaction / deterministic 启用。
// 其余 7 个常量的消费方同样在 9.90d。
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
//	├── execution_provider_contract.go      # 执行提供方契约（9.90b：JiuwenBox / ACP IDE / 未知）
//	├── native_path_context.go             # 原生路径投影 + ASCII JSON 规范化（9.90b）
//	├── network_scope.go                   # 网络授权域原语（9.90b：host 归一 / is_global / 敏感查询键）
//	├── permissions_layers.go              # Global/User/Session 三层覆盖读写 + 跨进程文件锁（9.90b）
//	├── policy_eval.go                     # 只做策略评估、不进入人工确认流程（9.90b）
//	├── reviewer_redaction.go              # AutoReviewer 证据脱敏（689 行 / 20 函数，原属 9.90d-2，因 workspace_paths 依赖提前到 9.90d-1）
//	├── persistent_audit.go                # 追加式持久化审计（9.90d-1 Task 7）
//	├── url_safety.go                      # 只读网络/浏览器 URL 审查助手（9.90d-1 Task 8）
//	├── generated_artifact_delivery.go     # send_file 单次调用的任务级授权租约（9.90d-1 Task 9）
//	├── send_file_path_guard.go            # send_file 多路径 file_guard 读判定聚合（9.90d-1 Task 10）
//	├── trusted_search_urls.go             # 会话可信搜索 URL 台账 + Host producer 租约（9.90d-1 Task 11）
//	├── openjiuwen_contract.go             # openjiuwen 权限 rail 运行时契约 + 结果分类（9.90d-1 Task 12）
//	├── permission_interrupt_rail.go       # 精确持久化 + 中断归属标记的权限 rail（9.90d-1 Task 13）
//	├── base_rail.go                       # 自动审查层包装权限轨的协作面接口（9.90d-1 Task 15）
//	├── auto_config.go                     # auto 模式运行时配置归一化（9.90d-2 Task 18）
//	├── permission_compose.go              # Global⊕User⊕Session 权限合成（9.90d-2 Task 19）
//	├── reviewer_stream_metadata.go        # Reviewer 元数据按 tool_call_id 分桶（9.90d-2 Task 21）
//	├── auto_decision.go                   # 自动审查的确定性判定助手（9.90d-2 Task 22）
//	├── reviewer_route.go                  # 语义审查器的紧凑 Host 路由（9.90d-2 Task 23）
//	├── auto_reviewer.go                   # 隔离 LLM 审查器协议 + 严格解析（9.90d-2 Task 24）
//	├── sandbox_profile.go                 # 沙箱解析根投影（9.90b）
//	├── tool_capabilities.go               # 工具能力分类表，28 分支（9.90b）
//	├── tool_decision_facts.go             # 单次调用决策事实 + DecisionRoute（9.90b）
//	├── root_context.go                    # RootIntentTurn/RootDecisionContext 意图窗口 + ContextVar 绑定 + 意图投影
//	├── root_permission_queue.go           # RootPermissionQueue 串行化队列状态机（active/pending/resuming + quarantine）
//	├── root_permission_queue_rail.go      # RootPermissionQueueRail 身份绑定与闩锁 + RootPermissionCompletionRail
//	├── root_ask_user.go                   # 用户反问续接：冻结快照 / 还原澄清 / 追加意图窗口
//	├── root_context_rail.go               # RootContextRail 上下文绑定 rail（随 9.90c 后续提交）
//	└── auto_permission/                   # 自动权限审查层共享契约 + 身份原语（9.90a）
//	    ├── models.go                     # ToolInvocation / PermissionHandlingResult / PermissionInterruptRequest + 11 常量
//	    └── tool_binding.go               # MatchesBoundMethod / ResolveToolBinding——实现身份背书
//
// 对应 Python 代码：jiuwenswarm/agents/harness/common/rails/permissions/ + jiuwenswarm/common/config.py（CRUD 部分）
package permissions
