// Package processcli 提供 Process CLI 的两层能力：机器执行面与人类入口面。
//
// 机器执行面（20.2）：读一份 JSON 契约、跑一次 Runtime 生命周期、
// 吐一串 JSONL 事件 + 一条 result、给一个确定退出码。不触碰 Agent 内部推理循环。
//
// 人类入口面（20.3）：参数层（main）、单命令生命周期（app）、交互外壳（repl）。
// repl 每轮 fork 一个 worker 子进程，父进程只做键盘读取、stdin 转发与 stdout 中继。
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
//	├── query.go                    # 只读查询的执行层（dispatch/run/failure）
//	├── query_entry.go              # --query-json 外层入口
//	├── machine_entry.go            # 一次性入口
//	├── machine.go                  # 编排核心
//	├── duplex_protocol.go          # 控制帧解码 DuplexControl/DecodeControl（duplex_protocol.py）
//	├── duplex_input.go              # 有界行读取器 DuplexLineReader（duplex_input.py）
//	├── duplex_input_unix.go         # POSIX 描述符读源（select 探测）
//	├── duplex_input_select.go       # !windows 管道工厂占位
//	├── duplex_input_windows.go      # Windows 管道读源（PeekNamedPipe）
//	├── duplex_control.go            # 双工路由控制器 DuplexController（duplex_control.py）
//	├── main.go                     # 参数层（五分支分派）
//	├── diagnostics.go              # 诊断流捕获（跨平台主体）
//	├── diagnostics_unix.go         # POSIX dup 实现
//	├── diagnostics_windows.go      # Windows DuplicateHandle 实现
//	├── app.go                      # 单命令 Runtime 生命周期
//	├── repl.go                     # 交互外壳（每轮 fork worker）
//	├── live_layout.go              # 补充输入三态布局
//	├── live_input.go               # 运行中 steer 输入控制
//	├── ui.go                       # 宽度工具 + ProcessCliUI + HumanRunUI
//	├── render.go                   # 三态事件渲染器
//	├── display_context.go          # 配置推断模型名 + mode 展示态
//	├── commands.go                 # slash 命令注册表与解析
//	├── model_options.go            # 临时模型连接参数校验
//	├── prompt.go                   # go-prompt 读取器与补全
//	├── control_commands.go         # 只读查询与展示
//	└── session_entry.go            # 会话列表/最近会话查询
//
// 对应 Python 代码：jiuwenswarm/channels/process_cli/
package processcli
