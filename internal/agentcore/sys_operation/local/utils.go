package local

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

type StreamEvent struct {
	// Type 事件类型
	Type StreamEventType
	// Data 事件数据（stdout/stderr 文本或 error 消息）
	Data string
	// ExitCode 退出码（仅 Exit 事件有效）
	ExitCode int
	// Timestamp 事件时间戳
	Timestamp time.Time
}

type InvokeData struct {
	// Stdout 标准输出
	Stdout string
	// Stderr 标准错误
	Stderr string
	// ExitCode 退出码
	ExitCode int
	// Exception 执行异常
	Exception error
}

type AsyncProcessHandler struct {
	// cmd 子进程命令
	cmd *exec.Cmd
	// chunkSize 流式块大小
	chunkSize int
	// encoding 编码
	encoding string
	// overallTimeout 总超时（秒）
	overallTimeout int
	// mu 保护进程状态
	mu sync.Mutex
}

type OperationUtils struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// StreamEventType 流式事件类型枚举。
// 对齐 Python StreamEventType：STDOUT, STDERR, EXIT, ERROR。
type StreamEventType int

const (
	// StreamEventTypeStdout 标准输出事件
	StreamEventTypeStdout StreamEventType = iota
	// StreamEventTypeStderr 标准错误事件
	StreamEventTypeStderr
	// StreamEventTypeExit 进程退出事件
	StreamEventTypeExit
	// StreamEventTypeError 执行错误事件
	StreamEventTypeError
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件
	logComponent = logger.ComponentAgentCore
	// defaultChunkSize 默认流式块大小
	defaultChunkSize = 1024
	// defaultEncoding 默认编码
	defaultEncoding = "utf-8"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var safePathPattern = regexp.MustCompile(`[^\w.-]`)

// ──────────────────────────── 导出函数 ────────────────────────────

func (t StreamEventType) String() string {
	switch t {
	case StreamEventTypeStdout:
		return "stdout"
	case StreamEventTypeStderr:
		return "stderr"
	case StreamEventTypeExit:
		return "exit"
	case StreamEventTypeError:
		return "error"
	default:
		return fmt.Sprintf("unknown(%d)", int(t))
	}
}

func NewAsyncProcessHandler(cmd *exec.Cmd, chunkSize int, encoding string, timeout int) *AsyncProcessHandler {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}
	if encoding == "" {
		encoding = defaultEncoding
	}
	return &AsyncProcessHandler{
		cmd:            cmd,
		chunkSize:      chunkSize,
		encoding:       encoding,
		overallTimeout: timeout,
	}
}

func (h *AsyncProcessHandler) Invoke(ctx context.Context) (*InvokeData, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd == nil {
		return nil, fmt.Errorf("进程命令为 nil")
	}

	// 设置进程组隔离
	h.setProcessGroup()

	// 创建管道
	stdoutPipe, err := h.cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderrPipe, err := h.cmd.StderrPipe()
	if err != nil {
		return nil, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	// 启动进程
	if err := h.cmd.Start(); err != nil {
		return nil, fmt.Errorf("启动进程失败: %w", err)
	}

	// 带超时等待
	var cancel context.CancelFunc
	if h.overallTimeout > 0 {
		ctx, cancel = context.WithTimeout(ctx, time.Duration(h.overallTimeout)*time.Second)
		defer cancel()
	}

	// 并行收集 stdout/stderr
	var stdoutBuf, stderrBuf strings.Builder
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		for scanner.Scan() {
			stdoutBuf.WriteString(scanner.Text())
			stdoutBuf.WriteByte('\n')
		}
	}()

	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		for scanner.Scan() {
			stderrBuf.WriteString(scanner.Text())
			stderrBuf.WriteByte('\n')
		}
	}()

	// collectDone 在两个收集协程都完成后关闭
	collectDone := make(chan struct{})
	go func() {
		wg.Wait()
		close(collectDone)
	}()

	// 等待进程完成或超时
	waitErr := h.waitProcess(ctx)

	// 等待收集完成，加 30 秒 grace period 超时保护
	select {
	case <-collectDone:
	case <-time.After(30 * time.Second):
		logger.Warn(logComponent).
			Str("event_type", "INVOKE_COLLECT_TIMEOUT").
			Msg("等待输出收集超时 30 秒")
	}

	data := &InvokeData{
		Stdout: stdoutBuf.String(),
		Stderr: stderrBuf.String(),
	}

	if waitErr != nil {
		if ctx.Err() == context.DeadlineExceeded {
			data.ExitCode = -1
			data.Exception = fmt.Errorf("执行超时 %d 秒", h.overallTimeout)
			// kill 进程树
			h.killProcessTree()
		} else {
			data.Exception = waitErr
		}
	} else {
		data.ExitCode = h.cmd.ProcessState.ExitCode()
	}

	return data, nil
}

// Stream 流式执行，通过 channel 逐块返回。
// 对齐 Python AsyncProcessHandler.stream：reader 协程 + queue 逻辑，支持超时控制。
func (h *AsyncProcessHandler) Stream(ctx context.Context) (<-chan StreamEvent, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	ch := make(chan StreamEvent, 64)

	if h.cmd == nil {
		close(ch)
		return ch, fmt.Errorf("进程命令为 nil")
	}

	// 设置进程组隔离
	h.setProcessGroup()

	stdoutPipe, err := h.cmd.StdoutPipe()
	if err != nil {
		close(ch)
		return ch, fmt.Errorf("创建 stdout 管道失败: %w", err)
	}
	stderrPipe, err := h.cmd.StderrPipe()
	if err != nil {
		close(ch)
		return ch, fmt.Errorf("创建 stderr 管道失败: %w", err)
	}

	if err := h.cmd.Start(); err != nil {
		close(ch)
		return ch, fmt.Errorf("启动进程失败: %w", err)
	}

	// 内部 queue channel，reader goroutine 写入，主协程消费
	queue := make(chan StreamEvent, 128)

	// reader goroutine: 读 pipe → 写 queue
	var wg sync.WaitGroup
	wg.Add(2)

	// stdout 读取
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stdoutPipe)
		scanner.Buffer(make([]byte, h.chunkSize), h.chunkSize)
		for scanner.Scan() {
			queue <- StreamEvent{
				Type:      StreamEventTypeStdout,
				Data:      scanner.Text() + "\n",
				Timestamp: time.Now(),
			}
		}
		if err := scanner.Err(); err != nil {
			queue <- StreamEvent{
				Type:      StreamEventTypeError,
				Data:      fmt.Sprintf("stdout 读取错误: %v", err),
				Timestamp: time.Now(),
			}
		}
	}()

	// stderr 读取
	go func() {
		defer wg.Done()
		scanner := bufio.NewScanner(stderrPipe)
		scanner.Buffer(make([]byte, h.chunkSize), h.chunkSize)
		for scanner.Scan() {
			queue <- StreamEvent{
				Type:      StreamEventTypeStderr,
				Data:      scanner.Text() + "\n",
				Timestamp: time.Now(),
			}
		}
		if err := scanner.Err(); err != nil {
			queue <- StreamEvent{
				Type:      StreamEventTypeError,
				Data:      fmt.Sprintf("stderr 读取错误: %v", err),
				Timestamp: time.Now(),
			}
		}
	}()

	// reader 完成后关闭 queue，并等 cmd.Wait() 发送 EXIT 事件
	go func() {
		wg.Wait()
		exitCode := 0
		if waitErr := h.cmd.Wait(); waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		queue <- StreamEvent{
			Type:      StreamEventTypeExit,
			Data:      fmt.Sprintf("%d", exitCode),
			ExitCode:  exitCode,
			Timestamp: time.Now(),
		}
		close(queue)
	}()

	// 主协程：select 循环消费 queue，支持超时和 context 取消
	go func() {
		defer close(ch)

		// 超时 ticker（1 秒间隔检查）
		var ticker *time.Ticker
		if h.overallTimeout > 0 {
			ticker = time.NewTicker(time.Second)
			defer ticker.Stop()
		}
		startTime := time.Now()

		for {
			select {
			case event, ok := <-queue:
				if !ok {
					// queue 关闭，所有事件已处理完毕
					return
				}
				ch <- event
				// EXIT 事件表示流结束
				if event.Type == StreamEventTypeExit {
					return
				}

			case <-ticker.C:
				// 检查 overallTimeout
				if h.overallTimeout > 0 && time.Since(startTime) > time.Duration(h.overallTimeout)*time.Second {
					logger.Warn(logComponent).
						Str("event_type", "STREAM_TIMEOUT").
						Int("overall_timeout", h.overallTimeout).
						Msg("流式执行超时")
					h.killProcessTree()
					ch <- StreamEvent{
						Type:      StreamEventTypeError,
						Data:      fmt.Sprintf("执行超时 %d 秒", h.overallTimeout),
						Timestamp: time.Now(),
					}
					return
				}

			case <-ctx.Done():
				logger.Warn(logComponent).
					Str("event_type", "STREAM_CANCELLED").
					Msg("流式执行被取消")
				h.killProcessTree()
				ch <- StreamEvent{
					Type:      StreamEventTypeError,
					Data:      "执行被取消",
					Timestamp: time.Now(),
				}
				return
			}
		}
	}()

	return ch, nil
}

func (h *AsyncProcessHandler) Background(grace float64) (pid int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd == nil {
		return 0, fmt.Errorf("进程命令为 nil")
	}

	// 后台模式：重定向 IO
	h.cmd.Stdin = nil
	h.cmd.Stdout = nil
	h.cmd.Stderr = nil

	h.setProcessGroup()

	if err := h.cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动后台进程失败: %w", err)
	}

	pid = h.cmd.Process.Pid

	// grace 检测：用 goroutine+Wait+select/timer 等待进程在 grace 内是否退出
	if grace > 0 {
		waitCh := make(chan error, 1)
		go func() { waitCh <- h.cmd.Wait() }()

		timer := time.NewTimer(time.Duration(grace * float64(time.Second)))
		defer timer.Stop()

		select {
		case waitErr := <-waitCh:
			// 进程在 grace 内退出
			if waitErr != nil {
				if exitErr, ok := waitErr.(*exec.ExitError); ok && exitErr.ExitCode() != 0 {
					logger.Warn(logComponent).
						Str("event_type", "BACKGROUND_EARLY_EXIT").
						Int("pid", pid).
						Int("exit_code", exitErr.ExitCode()).
						Msg("后台进程在 grace 内以非零退出码退出")
					return pid, fmt.Errorf("process exited early with code %d", exitErr.ExitCode())
				}
				// 退出码 0 也视为成功
				logger.Info(logComponent).
					Str("event_type", "BACKGROUND_EARLY_EXIT_SUCCESS").
					Int("pid", pid).
					Msg("后台进程在 grace 内正常退出")
				return pid, nil
			}
			return pid, nil
		case <-timer.C:
			// grace 超时，进程还在运行 → 成功
			logger.Info(logComponent).
				Str("event_type", "BACKGROUND_GRACE_TIMEOUT").
				Int("pid", pid).
				Float64("grace", grace).
				Msg("后台进程 grace 检测超时，进程仍在运行")
			return pid, nil
		}
	}

	return pid, nil
}

func (h *AsyncProcessHandler) KillProcessTree() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killProcessTree()
}

func (OperationUtils) PrepareEnvironment(customEnv map[string]string) map[string]string {
	env := make(map[string]string)
	for _, e := range os.Environ() {
		if idx := strings.Index(e, "="); idx > 0 {
			env[e[:idx]] = e[idx+1:]
		}
	}
	for k, v := range customEnv {
		env[k] = v
	}
	return env
}

func (OperationUtils) CreateTmpFile(content string, suffix string) (string, error) {
	tmpFile, err := os.CreateTemp("", "sys_op_*"+suffix)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer func() { _ = tmpFile.Close() }()

	if _, err := tmpFile.WriteString(content); err != nil {
		_ = os.Remove(tmpFile.Name())
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}
	return tmpFile.Name(), nil
}

func (OperationUtils) DeleteTmpFile(path string) error {
	return os.Remove(path)
}

func ResolveCwd(cwdPath string) string {
	if cwdPath != "" {
		if !filepath.IsAbs(cwdPath) {
			base := cwd.GetCwd(context.Background())
			return filepath.Join(base, cwdPath)
		}
		return cwdPath
	}
	return cwd.GetCwd(context.Background())
}

// ──────────────────────────── 非导出函数 ────────────────────────────

func (h *AsyncProcessHandler) setProcessGroup() {
	if h.cmd.SysProcAttr == nil {
		h.cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	h.cmd.SysProcAttr.Setpgid = true
}

func (h *AsyncProcessHandler) killProcessTree() {
	if h.cmd.Process == nil {
		return
	}
	pid := h.cmd.Process.Pid
	// 尝试终止整个进程组
	_ = syscall.Kill(-pid, syscall.SIGTERM)
	time.Sleep(100 * time.Millisecond)
	_ = syscall.Kill(-pid, syscall.SIGKILL)
}

func (h *AsyncProcessHandler) waitProcess(ctx context.Context) error {
	done := make(chan error, 1)
	go func() {
		done <- h.cmd.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		h.killProcessTree()
		return ctx.Err()
	}
}

func readPipe(r io.Reader, encoding string) <-chan string {
	ch := make(chan string, 64)
	go func() {
		defer close(ch)
		scanner := bufio.NewScanner(r)
		for scanner.Scan() {
			ch <- scanner.Text() + "\n"
		}
	}()
	return ch
}
