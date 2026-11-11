// Package processcli 提供 Process CLI 的一次性机器执行面。
//
// 它是「把自己打扮成一个 CLI」的那一层：读一份 JSON 契约、跑一次 Runtime 生命周期、
// 吐一串 JSONL 事件 + 一条 result、给一个确定退出码。不触碰 Agent 内部推理循环。
//
// 「非交互」= 协议无 TTY 依赖（可长连接、可双向），非「只跑一次就退」，
// 同时是 20.8 SDK 的传输层。
//
// 文件目录：
//
//	processcli/
//	├── doc.go                      # 包文档
//	├── protocol/                   # 机器协议契约（对齐 protocol/*.py）
//	├── protocol_stdout.go          # stdout 隔离（跨平台主体）
//	├── protocol_stdout_unix.go     # POSIX dup2 实现
//	├── protocol_stdout_windows.go  # Windows SetStdHandle 实现
//	├── machine_io.go               # 输入读取 + 输出写入
//	├── machine_result.go           # 事件流归约 RunSummary
//	├── machine_policy.go           # 输出 schema / 用量限额
//	├── machine_signals.go          # 信号归属
//	├── persistence.go              # 退出前落盘
//	├── client.go                   # InProcessRuntimeClient
//	├── query.go                    # 10 个只读查询的执行层（dispatch/run/failure）
//	├── machine_entry.go            # 一次性入口
//	└── machine.go                  # 编排核心
//
// 对应 Python 代码：jiuwenswarm/channels/process_cli/
package processcli
