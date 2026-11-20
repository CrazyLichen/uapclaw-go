// Package transport 外部 CLI 进程的启动与驱动抽象。
//
// 文件目录：
//
//	transport/
//	├── doc.go    # 包文档
//	├── base.go  # ProcessLike/StdinLike/StreamReaderLike/ProcessTransport 契约
//	└── local.go # LocalTransport 本地子进程启动（exec + os.Pipe）
//
// 对应 Python 代码：openjiuwen/agent_teams/external/cli_agent/transport/
package transport
