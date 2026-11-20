package transport

// 本地子进程 transport：对齐 Python
// openjiuwen/agent_teams/external/cli_agent/transport/local.py。
//
// LocalTransport 包装 asyncio.create_subprocess_exec——transport 抽象出现之前
// spawn 模块内联使用的同一条本地 fork 路径。偏差 D-15.13-a：Python 返回原生
// asyncio.subprocess.Process（零适配满足 ProcessLike），Go 侧用 os/exec +
// os.Pipe 组装出满足 ProcessLike 的 localProcess。

import (
	"bufio"
	"os"
	"os/exec"
	"sync"
	"syscall"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LocalTransport 把 CLI argv 作为本地子进程启动；返回原生进程。
//
// Python: LocalTransport (transport/local.py:14)
type LocalTransport struct{}

// localProcess exec.Cmd 组装出的 ProcessLike 实现。
type localProcess struct {
	cmd      *exec.Cmd
	stdin    *stdinPipe
	stdout   *readerPipe
	stderr   *readerPipe
	waitOnce sync.Once
	waitErr  error
	exitCode int
}

// stdinPipe 基于管道写端的 StdinLike 实现。
type stdinPipe struct {
	w      *os.File
	writer *bufio.Writer
}

// readerPipe 基于管道读端的 StreamReaderLike 实现。
type readerPipe struct {
	r      *os.File
	reader *bufio.Reader
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Run 以管道 fork argv；返回 localProcess。
// env 为 nil 时继承进程环境（exec.Cmd 语义与 Python env=None 一致）。
// 子进程未暴露 stdin/stdout 管道时报 AGENT_TEAM_EXECUTION_ERROR。
//
// Python: LocalTransport.run (transport/local.py:23)
func (t *LocalTransport) Run(argv []string, env map[string]string, cwd string) (ProcessLike, error) {
	if len(argv) == 0 {
		return nil, exception.RaiseError(exception.StatusAgentTeamExecutionError,
			exception.WithMsg("local CLI '' did not expose stdin/stdout pipes"))
	}
	cmd := exec.Command(argv[0], argv[1:]...) //nolint:gosec // argv 由团队配置/adapter 声明
	if env != nil {
		cmd.Env = make([]string, 0, len(env))
		for k, v := range env {
			cmd.Env = append(cmd.Env, k+"="+v)
		}
	}
	if cwd != "" {
		cmd.Dir = cwd
	}
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		return nil, err
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		_ = stdinR.Close()
		_ = stdinW.Close()
		_ = stdoutR.Close()
		_ = stdoutW.Close()
		return nil, err
	}
	cmd.Stdin = stdinR
	cmd.Stdout = stdoutW
	cmd.Stderr = stderrW
	if err := cmd.Start(); err != nil {
		for _, f := range []*os.File{stdinR, stdinW, stdoutR, stdoutW, stderrR, stderrW} {
			_ = f.Close()
		}
		return nil, err
	}
	// 父进程侧关闭子进程持有的端（Python asyncio 的 fork 行为等价物）
	_ = stdinR.Close()
	_ = stdoutW.Close()
	_ = stderrW.Close()
	return &localProcess{
		cmd:    cmd,
		stdin:  &stdinPipe{w: stdinW, writer: bufio.NewWriter(stdinW)},
		stdout: &readerPipe{r: stdoutR, reader: bufio.NewReader(stdoutR)},
		stderr: &readerPipe{r: stderrR, reader: bufio.NewReader(stderrR)},
	}, nil
}

// Aclose no-op：本地子进程的生命周期归 runtime 所有。
// Python: LocalTransport.aclose (transport/local.py:63)
func (t *LocalTransport) Aclose() error {
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Stdin CLI 的 stdin 写端。
func (p *localProcess) Stdin() StdinLike { return p.stdin }

// Stdout CLI 的 stdout 读端。
func (p *localProcess) Stdout() StreamReaderLike { return p.stdout }

// Stderr CLI 的 stderr 读端。
func (p *localProcess) Stderr() StreamReaderLike { return p.stderr }

// ReturnCode 退出码；仍在运行时返回 -1（对应 Python None）。
func (p *localProcess) ReturnCode() int {
	p.waitOnce.Do(func() {})
	if p.cmd.ProcessState != nil {
		return p.cmd.ProcessState.ExitCode()
	}
	return -1
}

// Terminate 通知 CLI 停止（SIGTERM，对应 Python asyncio.Process.terminate）。
func (p *localProcess) Terminate() {
	if p.cmd.Process != nil {
		_ = p.cmd.Process.Signal(syscall.SIGTERM)
	}
}

// Wait 等待进程退出并返回退出状态。
// 信号终止（terminate/kill）不算错误——对应 Python asyncio.Process.wait 只
// 返回 returncode 不抛异常（负值/非零码表达信号退出）。
func (p *localProcess) Wait() (int, error) {
	p.waitOnce.Do(func() {
		err := p.cmd.Wait()
		p.waitErr = err
		p.exitCode = p.cmd.ProcessState.ExitCode()
		if exitErr, ok := err.(*exec.ExitError); ok {
			if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && (ws.Signaled() || ws.Exited()) {
				p.waitErr = nil
			}
		}
	})
	return p.exitCode, p.waitErr
}

// Write 把 data 写入 stdin 管道。
func (s *stdinPipe) Write(data []byte) (int, error) { return s.writer.Write(data) }

// Flush 冲刷写缓冲。
func (s *stdinPipe) Flush() error { return s.writer.Flush() }

// WriteEOF 关闭写半边发出 EOF。
func (s *stdinPipe) WriteEOF() error { return s.w.Close() }

// CanWriteEOF 管道写端支持 EOF。
func (s *stdinPipe) CanWriteEOF() bool { return true }

// Close 关闭底层流。
func (s *stdinPipe) Close() error { return s.w.Close() }

// Read 从 stdout 读取。
func (r *readerPipe) Read(p []byte) (int, error) { return r.r.Read(p) }

// ReadLine 读取一行（含换行符；流结束时返回 io.EOF）。
func (r *readerPipe) ReadLine() ([]byte, error) {
	return r.reader.ReadBytes('\n')
}
