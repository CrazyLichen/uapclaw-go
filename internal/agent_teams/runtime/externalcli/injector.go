package externalcli

// 运行中外部 CLI agent 的旁路输入通道：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/injector.py。
//
// 团队把入站文本投递给 spawn 的 CLI 成员的方式是写 CLI 的输入通道。
// StdinPipeInjector 是第一个（Unix 优先）后端：把按换行框架化的文本写入
// 子进程 stdin 管道。PTY / Windows 后端可以实现同一契约而不触碰 runtime。

import (
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/runtime/externalcli/transport"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponent = logger.ComponentTeam

// ──────────────────────────── 结构体 ────────────────────────────

// StdinPipeInjector 经向子进程 stdin 管道写入换行框架化的行注入文本。
//
// 只对持续读取 stdin 的 CLI agent 有效（交互/流式输入模式）。只读一次 stdin
// 的 one-shot CLI 观察不到后续写入——那些退化为轮次边界投递。
//
// Python: StdinPipeInjector (injector.py:29)
type StdinPipeInjector struct {
	// stdin 绑定的打开的子进程 stdin 流
	stdin transport.StdinLike
	// closed 关闭标记（aclose 幂等）
	mu     sync.Mutex
	closed bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewStdinPipeInjector 绑定一个打开的子进程 stdin 流。
// Python: StdinPipeInjector.__init__ (injector.py:37)
func NewStdinPipeInjector(stdin transport.StdinLike) *StdinPipeInjector {
	return &StdinPipeInjector{stdin: stdin}
}

// Write 写入 text 加一个尾换行并冲刷管道。
// 已关闭时丢弃写入并记 Debug 日志（对齐 Python write-after-close 分支）。
//
// Python: StdinPipeInjector.write (injector.py:43)
func (i *StdinPipeInjector) Write(text string) error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		logger.Debug(logComponent).Msg("StdinPipeInjector.write after close; dropping")
		return nil
	}
	if _, err := i.stdin.Write([]byte(text + "\n")); err != nil {
		return err
	}
	return i.stdin.Flush()
}

// Close 关闭 stdin 管道。幂等。
// CanWriteEOF 为 true 时先写 EOF 再关闭底层流；RuntimeError/OSError 等价
// 的关闭失败只记 Debug 日志（对齐 Python except 分支）。
//
// Python: StdinPipeInjector.aclose (injector.py:53)
func (i *StdinPipeInjector) Close() error {
	i.mu.Lock()
	defer i.mu.Unlock()
	if i.closed {
		return nil
	}
	i.closed = true
	if i.stdin.CanWriteEOF() {
		if err := i.stdin.WriteEOF(); err != nil {
			logger.Debug(logComponent).Err(err).Msg("StdinPipeInjector close failed")
		}
	}
	if err := i.stdin.Close(); err != nil {
		logger.Debug(logComponent).Err(err).Msg("StdinPipeInjector close failed")
	}
	return nil
}
