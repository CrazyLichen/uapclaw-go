// Package transport 外部 CLI 进程的启动与驱动抽象。
//
// 对齐 Python openjiuwen/agent_teams/external/cli_agent/transport/base.py。
// ProcessTransport 把启动 argv 变成 ProcessLike 对象，对应通用外部 CLI runtime
// 使用的 asyncio.create_subprocess_exec 子集。runtime 经下述小接口驱动返回的
// 进程。通用 CLI 后端目前只支持 LocalTransport（local.go）；SDK 后端的 agent
// 可以在本包之外提供自己的 transport 实现。
//
// 偏差 D-15.13-a：Python asyncio StreamReader/create_subprocess_exec 的异步
// 读面 → Go 的同步 io.Reader + 显式 ReadLine；Wait 经 ctx 取消。
//
// 对应 Python 代码：agent_teams/external/cli_agent/transport/base.py
package transport

import (
	"io"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamReaderLike transport stdout/stderr 流的读契约。
// D-15.13-b：Python async read/readline → Go 同步读面。
//
// Python: StreamReaderLike (transport/base.py:15)
type StreamReaderLike interface {
	// Read 从流中读取至多 len(p) 字节。
	io.Reader
	// ReadLine 从流中读取一行（含换行符；EOF 时返回 io.EOF）。
	ReadLine() ([]byte, error)
}

// StdinLike StdinPipeInjector 依赖的写契约。
// CanWriteEOF 是必需的：StdinPipeInjector.Close 用它决定是否在关闭底层流前
// 先写 EOF。
//
// Python: StdinLike (transport/base.py:27)
type StdinLike interface {
	// Write 把 data 写入底层流。
	Write(data []byte) (int, error)
	// Flush 冲刷写缓冲。
	Flush() error
	// WriteEOF 在发出 EOF 信号后关闭写半边。
	WriteEOF() error
	// CanWriteEOF 本流是否支持 WriteEOF。
	CanWriteEOF() bool
	// Close 关闭底层流。
	Close() error
}

// ProcessLike ExternalCliRuntime 驱动的进程契约，与 transport 无关。
// 对应 runtime 实际触碰的 asyncio.subprocess.Process 最小子集：stdin 写端
// （交给 StdinPipeInjector）、stdout 读端（spawn 包装成行迭代器）、退出码
// （premature-EOF 检测）、同步 Terminate 与 Wait（aclose 时释放）。
//
// Python: ProcessLike (transport/base.py:56)
type ProcessLike interface {
	// Stdin CLI 的 stdin 流写端。
	Stdin() StdinLike
	// Stdout CLI 的 stdout 流读端（spawn 把它包装成行块）。
	Stdout() StreamReaderLike
	// Stderr CLI 的 stderr 流读端（可为 nil）。
	Stderr() StreamReaderLike
	// ReturnCode 退出码；进程仍在运行时返回 -1（对应 Python None）。
	ReturnCode() int
	// Terminate 通知 CLI 停止（同步——对应 asyncio.Process）。
	Terminate()
	// Wait 等待进程退出并返回退出状态。ctx 取消时提前返回。
	Wait() (int, error)
}

// ProcessTransport 把 CLI argv 启动为 ProcessLike 进程对象。
// Aclose 释放 transport 资源；对 local transport 是 no-op。
//
// Python: ProcessTransport (transport/base.py:96)
type ProcessTransport interface {
	// Run 以 env/cwd 启动 argv；返回 ProcessLike。
	Run(argv []string, env map[string]string, cwd string) (ProcessLike, error)
	// Aclose 释放 transport（关闭连接 / local 为 no-op）。
	Aclose() error
}
