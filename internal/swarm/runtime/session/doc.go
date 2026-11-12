// Package session 提供会话执行的临时状态账本（Runtime 层）。
// 本包对齐上游 jiuwenswarm/runtime/session/ 目录，负责回答"某个会话
// 现在有哪些执行、各自处于什么状态、第几代"，并向上提供按代次取消与
// 关闭的能力。它不持有任何持久化会话元数据与历史——那些在
// internal/swarm/server/session 包。
// 核心概念（两个 chan 不是两个状态）：
//   - Terminal 由 registry.MarkTerminal 关闭，表示业务结果已定；
//   - Done 由执行方 defer 的最后一行关闭，表示收尾完成、协程即将退出。
//
// "清场"等 Done，"收结果"等 Terminal，两者用途正交。
// 文件目录：
//
//	session/
//	├── doc.go                    # 包文档
//	├── model.go                  # 枚举、快照、执行句柄（handle）
//	├── execution_registry.go     # 执行索引与终态回收（含双信号语义）
//	├── session_lifecycle.go      # 生命周期事件、参与者接口与登记表
//	├── coordinator_port.go       # CoordinatorPort 协调器能力接口 + ErrCoordinatorUnavailable
//	├── session_delete.go         # 删除结果、删除锁与团队删除围栏
//	└── session_input_errors.go   # 会话补充输入的三个拒收异常与意图枚举
//	├── work_scheduler.go         # 每会话串行 lane 与两级优先级
//	└── coordinator.go            # 账本门面：登记、运行、取消、关闭
//
// 对应 Python 代码：jiuwenswarm/runtime/session/
package session
