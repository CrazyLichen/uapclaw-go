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

// ──────────────────────────── 结构体 ────────────────────────────

// StreamEvent 流式事件。
// 对齐 Python StreamEvent：type, data, exit_code, timestamp。
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

// InvokeData 一次性执行结果。
// 对齐 Python InvokeData：stdout, stderr, exit_code, exception。
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

// AsyncProcessHandler 异步进程处理器。
// 对齐 Python AsyncProcessHandler：process, chunk_size, encoding, overall_timeout, is_executed。
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

// OperationUtils 操作工具类。
// 对齐 Python OperationUtils：prepare_environment, create_handler, create_tmp_file, delete_tmp_file。
type OperationUtils struct{}

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

// safePathPattern 安全路径模式
var safePathPattern = regexp.MustCompile(`[^\w.-]`)

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回 StreamEventType 的字符串表示
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

// NewAsyncProcessHandler 创建异步进程处理器。
// 对齐 Python OperationUtils.create_handler。
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

// Invoke 一次性执行，收集完整输出。
// 对齐 Python AsyncProcessHandler.invoke：drain stdout+stderr，超时 kill 进程树。
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

	// 收集输出
	stdoutCh := readPipe(stdoutPipe, h.encoding)
	stderrCh := readPipe(stderrPipe, h.encoding)

	var stdoutBuf, stderrBuf strings.Builder
	collectDone := make(chan struct{})
	go func() {
		for s := range stdoutCh {
			stdoutBuf.WriteString(s)
		}
		for s := range stderrCh {
			stderrBuf.WriteString(s)
		}
		close(collectDone)
	}()

	// 等待进程完成或超时
	waitErr := h.waitProcess(ctx)

	// 等待收集完成
	<-collectDone

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
// 对齐 Python AsyncProcessHandler.stream：reader 协程 + queue 逻辑。
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

	// 流式读取
	go func() {
		defer close(ch)

		var wg sync.WaitGroup
		wg.Add(2)

		// stdout 读取
		go func() {
			defer wg.Done()
			scanner := bufio.NewScanner(stdoutPipe)
			scanner.Buffer(make([]byte, h.chunkSize), h.chunkSize)
			for scanner.Scan() {
				ch <- StreamEvent{
					Type:      StreamEventTypeStdout,
					Data:      scanner.Text() + "\n",
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
				ch <- StreamEvent{
					Type:      StreamEventTypeStderr,
					Data:      scanner.Text() + "\n",
					Timestamp: time.Now(),
				}
			}
		}()

		wg.Wait()

		// 等待进程退出
		exitCode := 0
		if err := h.cmd.Wait(); err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		ch <- StreamEvent{
			Type:      StreamEventTypeExit,
			Data:      fmt.Sprintf("%d", exitCode),
			ExitCode:  exitCode,
			Timestamp: time.Now(),
		}
	}()

	return ch, nil
}

// Background 后台执行，等待 grace 秒检测早期失败。
// 对齐 Python AsyncProcessHandler.background：启动 + grace 检测。
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

	// grace 检测：等待一段时间检测早期失败
	if grace > 0 {
		time.Sleep(time.Duration(grace * float64(time.Second)))
		// 检查进程是否已退出
		if h.cmd.ProcessState != nil {
			return pid, fmt.Errorf("后台进程早期退出")
		}
	}

	return pid, nil
}

// KillProcessTree 终止进程树。
// 对齐 Python _kill_process_tree。
func (h *AsyncProcessHandler) KillProcessTree() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killProcessTree()
}

// PrepareEnvironment 合并环境变量。
// 对齐 Python OperationUtils.prepare_environment：合并 os.Environ + custom。
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

// CreateTmpFile 创建临时文件。
// 对齐 Python OperationUtils.create_tmp_file。
func (OperationUtils) CreateTmpFile(content string, suffix string) (string, error) {
	tmpFile, err := os.CreateTemp("", "sys_op_*"+suffix)
	if err != nil {
		return "", fmt.Errorf("创建临时文件失败: %w", err)
	}
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(content); err != nil {
		os.Remove(tmpFile.Name())
		return "", fmt.Errorf("写入临时文件失败: %w", err)
	}
	return tmpFile.Name(), nil
}

// DeleteTmpFile 删除临时文件。
// 对齐 Python OperationUtils.delete_tmp_file。
func (OperationUtils) DeleteTmpFile(path string) error {
	return os.Remove(path)
}

// ResolveCwd 解析 CWD：显式参数 → context CWD → os.Getenv("PWD")。
// 对齐 Python _resolve_cwd。
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

// setProcessGroup 设置进程组隔离（POSIX：start_new_session）。
// 对齐 Python _create_subprocess 中 start_new_session=True。
func (h *AsyncProcessHandler) setProcessGroup() {
	if h.cmd.SysProcAttr == nil {
		h.cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	h.cmd.SysProcAttr.Setpgid = true
}

// killProcessTree 终止进程组
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

// waitProcess 等待进程完成，支持 context 取消
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

// readPipe 从管道读取全部内容
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
