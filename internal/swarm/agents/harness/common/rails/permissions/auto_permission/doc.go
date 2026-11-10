// Package auto_permission 提供自动权限审查层（Auto Permission）的共享契约与身份原语。
// 本包对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/
// 与同级 tool_binding.py。目录名去掉 Python 的下划线前缀——下划线在 Python 表示
// "内部子包"，Go 无此约定且导出包名不应以下划线开头。
// # 在权限链路中的位置
//
// 本包是 9.90a（基础类型层）的最后一块，位于 PermissionEngine 规则判定返回 ASK
// 之后、9.90b（决策事实层）之前：
//
//	PermissionEngine 规则判定
//	        ↓  ASK
//	【本包】数据契约 + 身份背书        ← 不产生决策
//	        ↓
//	9.90b 决策事实层（tool_capabilities / network_scope / policy_eval）
//	        ↓
//	9.90c root 五文件（串行队列 + 意图窗口）  ← 已完成
//	        ↓
//	9.90d 自动审查层（auto_reviewer / before_tool / adaptive 系列）
//
// # 两个文件的分工
//
//   - models.go：三个数据契约（ToolInvocation 工具调用快照、PermissionHandlingResult
//     处理结果 + 可信来源、PermissionInterruptRequest 权限中断请求）与 11 个共享常量。
//   - tool_binding.go：实现身份校验。回答"我要放行的这个工具，是不是我以为的那个
//     实现"——实现类必须精确匹配、卡片必须指针同一，任一不满足即判不合格。
//     Python 原注释：Subclasses can replace execution; this is implementation
//     identity, not assignability.
//
// # 已知缺口
//
// ⤵️ 以下 5 个符号在 Python 全仓（含 reviewer_redaction.py / deterministic.py
// 与 tests）零消费——**不是「待 9.90d 启用」，而是上游从未接入**：
//   - ShellDisplayPathPattern
//   - ShellDisplayPreSplitPathPattern
//   - SensitiveDisplayPathPattern
//   - DeterministicReadonlyPublicWebFetchSourceKinds
//   - ReviewerActionTargetMaxLength（reviewer_metadata 用字面量 160）
//
// 保留理由：规则 9.2-4 要求 Enum/常量成员集合完整映射，不得因"Go 侧用不到"裁剪。
// 注意：9.90d-1 已落地 reviewer_redaction.go，该文件并未消费上述三个正则——
// 后续若有人想「接上」这些常量，先确认上游是否真有消费方，不要凭注释臆断。
// ⤵️ 其余常量的消费方同样在 9.90d：ReviewerUITextMaxLength 与
// ReviewerUIShortTextMaxLength 用于 reviewer_metadata；ReviewerDenialGuidance
// 用于 deny_response；ProhibitedFileDeliveryReason 用于 before_tool；
// DeterministicBoundedScopeDecisionSource 与 DeterministicReadonlyPublicWebReason
// 用于 deterministic。
// 文件目录：
//
//	auto_permission/
//	├── doc.go              # 包文档
//	├── models.go           # ToolInvocation / PermissionHandlingResult / PermissionInterruptRequest + 11 常量
//	├── tool_binding.go     # MatchesBoundMethod / ResolveToolBinding——实现身份背书
//	├── lifecycle.go        # AutoPermissionRail 结构体 + 构造/Init/Uninit/配置更新（9.90d-2 Task 26）
//	├── invocation_context.go # 工具调用提取 + 执行前路径冻结（9.90d-2 Task 27）
//	├── runtime_result.go  # 判定结果的运行时还原与投影（9.90d-2 Task 28）
//	├── runtime_bridge.go # 判定结果到运行时行为的桥接（9.90d-2 Task 29）
//	├── reviewer_metadata.go # 审查器审计/UI 元数据归一（9.90d-2 Task 30）
//	├── reviewer_audit.go  # 审查器审计辅助与确认载荷分类（9.90d-2 Task 31）
//	├── domain_manual.go   # 人工/拒绝路径共用的响应辅助（9.90d-2 依赖前置）
//	├── deny_response.go   # 审查器拒绝响应构造（9.90d-2 依赖前置）
//	├── manual_authorization.go # 需人工审批时的响应构造（9.90d-2 依赖前置）
//	├── deterministic.go   # 确定性放行与路由包装（9.90d-2 依赖前置）
//	├── artifact_authorization.go # 文件投递的精确授权（9.90d-2 依赖前置）
//	├── reviewer_execution.go # 审查器执行编排：主入口 maybeRunReviewer（Task 32）
//	├── reviewer_override_gate.go # 单次放行的审查器前置闸门（Task 33）
//	├── reviewer_override_consume.go # 消费旧版 Core 会话级自动确认（Task 33）
//	├── artifact_declaration.go # send_file 参数辅助（Task 34）
//	├── artifact_tracking_capture.go # 工具执行后捕获（Task 34）
//	├── domain_grant_audit.go # 固定领域路由判定与响应（Task 35）
//	└── readonly_tool_bindings.go # 只读快速路径身份校验（V-4/D3/D5）
//
// 对应 Python 代码：
//   - jiuwenswarm/agents/harness/common/rails/permissions/_auto_permission/models.py（101 行）
//   - jiuwenswarm/agents/harness/common/rails/permissions/tool_binding.py（25 行）
package auto_permission
