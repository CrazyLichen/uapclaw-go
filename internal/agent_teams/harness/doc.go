// Package harness 提供团队成员的 NativeHarness（L2）与 HarnessProtocol。
// 它是团队成员"像被调度那样运行"的交互层：把 DeepAgent 的自驱任务循环
// 换成"单协程 supervisor + 命令队列"，从而实现并发安全的 send/abort，
// 并向团队侧发出 state/round 生命周期事件。
// 文件目录：
//
//	harness/
//	├── doc.go            # 包文档
//	├── protocol.go       # HarnessProtocol 接口与事件载荷
//	├── state.go          # 生命周期相位、活动轮次、内部状态
//	├── control.go        # 控制命令（带应答通道）
//	├── outputs.go        # 输出迭代器（单消费者 + END 哨兵）
//	├── native_harness.go # NativeHarness 本体
//	└── team_harness.go   # TeamHarness（团队对外窗口）
//
// 对应 Python 代码：openjiuwen/agent_teams/harness/
package harness
