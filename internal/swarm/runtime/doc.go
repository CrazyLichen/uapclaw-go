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
//	├── run_context.go          # RunContext 一次调用选项 + CURRENT_RUN_CONTEXT ctx 绑定
//	└── run_permissions.go      # 请求级权限覆盖层（overlay_run_permissions）
//
// 对应 Python 代码：jiuwenswarm/runtime/（service.py / request.py / plan.py / events.py /
// run_context.py / run_permissions.py）
//
// # 请求级权限覆盖
//
// run_permissions.go 把一次请求携带的临时工具权限选择叠加到已安装配置上，
// 保留所有已装安全护栏；deny 不可放宽、三个桶同步、深拷贝、排序输出。
// 请求入口的写入方（WithRunPermissions / WithRunContext 绑定）暂留
// ⤵️ 9.90d-2 装配层。
package runtime
