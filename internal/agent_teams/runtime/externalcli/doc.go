// Package externalcli 提供外部 CLI 团队成员的运行时支撑。
//
// 本包对应 Python openjiuwen/agent_teams/spawn/external_cli_spawn.py（795 行）
// 与 openjiuwen/agent_teams/external/、external/cli_agent/ 两层（15.13 一文件
// 对一文件移植；偏差 D-15.12-e：Python 单文件由 15.12/15.13 两批填充同一 Go
// 包）。外部 CLI（adapter 路的 gemini/hermes/openclaw；provider 路的 dsh 等）
// 作为一等团队成员拉起：入团描述符注入、MCP 工具注入、auth fallback、失败
// 诊断与会话清理。claude/codex SDK 路留桩 ⤵️15.7/15.8（D-15.13-x）。
//
// 文件目录：
//
//	externalcli/
//	├── doc.go               # 包文档
//	├── paths.go             # cwd 三分离与 workspace meta 路径段（15.12）
//	├── descriptor.go        # TeamJoinDescriptor 入团描述符与 to_env
//	├── adapters.go          # CliAgentAdapter 与 gemini/codex-exec/openclaw/hermes/generic 内置条目
//	├── backends.go          # ExternalCliBackend 后端查找（sdk/adapter/provider）
//	├── injector.go          # StdinPipeInjector stdin 注入器
//	├── model_compat.go      # provider/协议/模型配置映射共享原语
//	├── session_cleanup.go   # 后端原生会话的尽力清理
//	├── runtime.go           # CliRuntimeBase/ExternalCliRuntime/ReinvokeCliRuntime
//	├── spawn.go             # BuildCliRuntime spawn 主体（provider/adapter 路与守卫）
//	├── member_runtime.go    # ExternalHarnessMemberRuntime 骨架（provider 路）
//	├── activity_emitter.go  # 结构化 AGENT_ACTIVITY 事件构造器（归包偏差 D-15.16-a4）
//	└── transport/           # 进程启动与驱动抽象子包（base/local）
//
// 对应 Python 代码：openjiuwen/agent_teams/spawn/external_cli_spawn.py、
// openjiuwen/agent_teams/external/{descriptor,runtime,member_runtime}.py、
// openjiuwen/agent_teams/external/cli_agent/*
package externalcli
