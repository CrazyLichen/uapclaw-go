// Package pregel 提供 Pregel 图引擎的配置、常量、超步调度信号、中断值与中断控制流。
//
// 本包是领域八的编译期最底层：构图（8.1）、节点与通道抽象（8.3）、
// 调度引擎（8.3b/8.3c）、通道管理（8.4）、顶点（8.11）、工作流（8.14）
// 均依赖本包提供的保留节点名、终止态键、递归预算、配置归一化能力，
// 以及超步之间传递调度消息的 Message、承载中断值的 Interrupt、承载中断跳转的
// GraphInterrupt、产出调度消息的 Router，与落地这些消息的 Channel/ChannelManager
// （就绪判定 + CNF 汇聚 + 检查点快照）。
//
// 接入状态：Router 与 Channel 已交付（8.4/8.5），但**尚未接入**超步循环——
// PregelLoop 要等 8.3b，届时由它在循环 goroutine 内串行驱动这些类型并决定下一
// 超步激活哪些节点。当前它们在生产代码中零调用方。
//
// 配置以值类型结构体表达：字段零值等价于 Python TypedDict(total=False)
// 的「键缺失/None」，因此不存在指针字段，调用方也无须判空。
//
// 分层纪律（审查口径）：
//
//   - 本包禁止 import internal/agentcore/session/ 下任何包。Python 侧 graph/pregel
//     同样不依赖 session，二者靠 Interrupt.Value 声明为 Any 维持单向。
//   - Interrupt.Value 只承载、不解读：本包不读取其内容，也不对其做类型断言；
//     断言只允许发生在 session 层（那里的具体类型是 stream.OutputSchema，
//     其 Payload 装 interaction.InteractionOutput）。
//     消费链路只做存在性判断（checkpointer 的 isWorkflowInterrupted 判 key 存在且非 nil）。
//   - 用户可见的中断内容不依赖该字段，而是在 raise 之前经 writeInteractionOutput 写入流。
//   - 本包含 panic 类 GraphInterrupt（Python 侧是 Exception），但仍是叶子包：
//     不 recover、不打日志、不读取 Interrupts 的内容。
//   - recover 点必须用 `case *pregel.GraphInterrupt` 判别；禁止 `r.(error)` 兜底——
//     GraphInterrupt 实现了 Error()（仅供日志），按 error 兜底会把中断当普通错误吞掉。
//   - ChannelManager 无锁（对齐 Python 的 channels.py）：只允许超步循环所在
//     goroutine 串行调用；节点任务在其他 goroutine 执行，但不得触碰 ChannelManager。
//
// 文件目录：
//
//	pregel/
//	├── doc.go         # 包文档
//	├── base.go        # Message/MessageKind/构造函数/Kind 与 Interrupt 值对象、GraphInterrupt 中断跳转
//	├── channels.go    # Channel 接口 + ChannelSnapshot + Trigger/Barrier 通道 + ChannelManager
//	├── router.go      # Router 接口 + Selector/SelectorFunc + Static/Barrier/Conditional 三个路由实现
//	├── constants.go   # 保留节点名、终止态键、命名空间键名与分隔符
//	└── config.go      # PregelConfig/InnerPregelConfig/CreateInnerConfig/DefaultPregelConfig
//
// 对应 Python 代码：openjiuwen/core/graph/pregel/base.py · channels.py · config.py · constants.py · router.py
package pregel
