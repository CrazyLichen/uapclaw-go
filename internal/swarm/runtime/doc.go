// Package runtime 提供 host 侧运行时编排能力（运行时事件、chat turn 编排、请求解析、plan 状态机）。
// 该包对齐 Python `jiuwenswarm/runtime/`：把"选 agent、每轮该处于什么运行模式、
// 计划状态该不该变、控制事件怎么下发"从 adapter 与 server 里抽出来，由运行时统一裁决。
// 文件目录：
//
//	runtime/
//	├── doc.go     # 包文档
//	├── events.go  # RuntimeEvent 与响应/片段包装
//	├── service.go # AgentRuntime（Invoke/Stream 编排、控制事件发射）
//	├── request.go # 请求模式解析（含 Web 组合）、工程目录解析、metadata 同步、chat turn 准备、取消请求
//	├── plan.go    # PlanModeController 计划状态机
//	├── session_manager.go      # 会话任务队列（同会话串行 LIFO、跨会话并发）
//	├── session_work_kind.go    # 请求归类判定（哪些请求归 Runtime 调度）
//	├── session_input.go        # 会话补充输入（steer / follow_up）
//	├── interrupt_resume.go     # 暂停调用应答判定
//	├── interrupt_helpers.go    # 中断恢复载荷判定
//	├── interaction.go          # 交互回答的类型化输入（InteractionAnswerInput）
//	├── session_provisioner.go  # 会话创建/删除/切换事务（RuntimeSessionProvisioner）
//	├── team_session_lifecycle.go # 团队会话生命周期适配（TeamSessionLifecycleAdapter）
//	├── agent_definition.go     # 声明式 root-Agent 定义/执行体/指纹（RuntimeAgentDefinition）
//	├── mode_catalog.go         # 单 Agent 模式目录与解析（含遗留别名折叠）
//	├── model_catalog.go        # 模型目录、选择与配置解析（含全局索引选择键）
//	├── session_catalog.go      # 会话只读查询（get/list，含分页与搜索）
//	├── permission_catalog.go   # 权限四层快照（global/user/session/effective）
//	├── tool_allowlist.go       # 在最终能力边界收窄工具集
//	├── ctx/                    # 请求级跨层上下文（RunContext / 请求级权限覆盖，见包注释）
//	└── cron/                   # 定时任务模型与持久化（文件 / etcd 双后端，10 文件）
//
// 对应 Python 代码：jiuwenswarm/runtime/（service.py / request.py / plan.py / events.py /
// interaction.py / agent_definition.py / mode_catalog.py / model_catalog.py /
// session_catalog.py / permission_catalog.py / tool_allowlist.py / ctx/ / cron/）
//
// # 请求级权限覆盖（已下沉到 ctx 子包）
//
// ctx/run_permissions.go 把一次请求携带的临时工具权限选择叠加到已安装配置上，
// 保留所有已装安全护栏；deny 不可放宽、三个桶同步、深拷贝、排序输出。
// 写入方现状：
//   - `WithRunPermissions` 已由 `permission_interrupt_rail.go` 消费（9.90d-2 回填）
//   - `WithRunContext` **尚无生产写入方**：Python 在
//     `interface_deep.py`（请求入口）与 `channels/process_cli/machine.py`（CLI）
//     两处绑定 `CURRENT_RUN_CONTEXT`，Go 侧对应入口尚未迁移（`service.go` 283 行
//     vs Python 4000+ 行）。已登记 IMPLEMENTATION_PLAN 9.90e。
package runtime
