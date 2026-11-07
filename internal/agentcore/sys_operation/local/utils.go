package local

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	textencoding "golang.org/x/text/encoding/ianaindex"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/encoding/traditionalchinese"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// StreamEvent 命令执行过程中产生的一条流式事件（stdout/stderr/exit/error）。
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

// InvokeData 一次同步执行的结果汇总（stdout/stderr/退出码/异常）。
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

// AsyncProcessHandler 异步子进程句柄：负责流式读取输出、超时控制与进程状态保护。
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

// OperationUtils 平台操作工具函数的命名空间（无状态，仅提供方法分组）。
type OperationUtils struct{}

// lockedBuffer 并发安全的输出缓冲：os/exec 的 copy 协程写入，主协程在超时早退后读取。
type lockedBuffer struct {
	// mu 保护 buf
	mu sync.Mutex
	// buf 已写入的原始字节
	buf bytes.Buffer
}

// chunkDecoder 流式分块解码器：跨块的半个多字节字符留到下一块再解码。
// Python: AsyncProcessHandler._reader 用 codecs.getincrementaldecoder 达到同样效果。
type chunkDecoder struct {
	// encoding 目标编码
	encoding string
	// pending 上一块末尾不完整的多字节字节
	pending []byte
}

// ──────────────────────────── 枚举 ────────────────────────────

// StreamEventType 流式事件类型枚举。
// Python: StreamEventType：STDOUT, STDERR, EXIT, ERROR。
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
	// backgroundStderrRetainLimit 后台进程保留的 stderr 字节上限。
	// Python: _drain_background_stderr(limit=8192) 默认值
	backgroundStderrRetainLimit = 8192
	// backgroundStderrWait 早退时等待 stderr 排空的最长时间。
	// 后代进程可能继承 stderr 写端并长期持有，管道 EOF 迟迟不到；
	// 只等一小段就把已有内容当诊断，任务本身继续留在后台排空。
	// Python: background() 里 remaining = deadline - loop.time() 的等待窗
	backgroundStderrWait = 500 * time.Millisecond
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// safePathPattern 文件名安全字符集合
	safePathPattern = regexp.MustCompile(`[^\w.-]`)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// String 返回流式事件类型的字符串表示。
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

// NewAsyncProcessHandler 创建异步进程处理器实例。
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

// Write 追加一块输出（os/exec 的 copy 协程调用）。
func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

// Bytes 返回当前已写入的全部字节副本。
func (b *lockedBuffer) Bytes() []byte {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]byte(nil), b.buf.Bytes()...)
}

// decode 解码一块原始字节；末尾不完整的半个多字节字符留到下一块。
func (d *chunkDecoder) decode(raw []byte) string {
	if len(raw) == 0 {
		return ""
	}
	data := raw
	if len(d.pending) > 0 {
		data = append(append([]byte(nil), d.pending...), raw...)
		d.pending = nil
	}
	out, rest := decodeComplete(data, d.encoding)
	d.pending = rest
	return out
}

// flush 冲出残留的不完整字节（流结束时按原样输出，不丢字节）。
func (d *chunkDecoder) flush() string {
	if len(d.pending) == 0 {
		return ""
	}
	raw := d.pending
	d.pending = nil
	return decodeOutput(raw, d.encoding)
}

// Invoke 同步执行子进程，等待完成后返回输出数据。
func (h *AsyncProcessHandler) Invoke(ctx context.Context) (*InvokeData, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd == nil {
		return nil, fmt.Errorf("进程命令为 nil")
	}

	// 设置进程组隔离
	h.setProcessGroup()

	// 输出交给 os/exec 托管：Wait 会等内部 io.Copy 协程把管道读到 EOF 再返回，
	// 因此不会出现「读协程尚未调度就被 Wait 关闭管道」的空输出/丢尾。
	// Python: invoke 里独立 _drain 任务读满 EOF 后再取 buffer。
	var stdoutBuf, stderrBuf lockedBuffer
	h.cmd.Stdout = &stdoutBuf
	h.cmd.Stderr = &stderrBuf

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

	waitCh := make(chan error, 1)
	go func() { waitCh <- h.cmd.Wait() }()

	// 等待进程退出或超时；超时/取消时先 kill 进程树，让管道 EOF 后 Wait 自然返回
	timedOut := false
	var waitErr error
	select {
	case waitErr = <-waitCh:
	case <-ctx.Done():
		timedOut = true
		h.killProcessTree()
		select {
		case waitErr = <-waitCh:
		case <-time.After(30 * time.Second):
			logger.Warn(logComponent).
				Str("event_type", "INVOKE_COLLECT_TIMEOUT").
				Msg("timed out waiting 30s for process exit after kill")
		}
	}

	data := &InvokeData{
		Stdout: decodeOutput(stdoutBuf.Bytes(), h.encoding),
		Stderr: decodeOutput(stderrBuf.Bytes(), h.encoding),
	}
	if h.cmd.ProcessState != nil {
		data.ExitCode = h.cmd.ProcessState.ExitCode()
	}

	if timedOut {
		data.ExitCode = -1
		if ctx.Err() == context.DeadlineExceeded {
			data.Exception = fmt.Errorf("执行超时 %d 秒", h.overallTimeout)
		} else {
			data.Exception = waitErr
		}
	} else if waitErr != nil {
		data.Exception = waitErr
	}

	return data, nil
}

// Stream 流式执行，通过 channel 逐块返回。
// Python: AsyncProcessHandler.stream：reader 协程 + queue 逻辑，支持超时控制。
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
	// done 消费者退出信号：reader/exit 协程据此提前收工，避免 channel 堵塞泄漏
	done := make(chan struct{})
	var doneOnce sync.Once
	stop := func() { doneOnce.Do(func() { close(done) }) }

	send := func(ev StreamEvent) bool {
		select {
		case queue <- ev:
			return true
		case <-done:
			return false
		}
	}

	// reader goroutine: 读 pipe → 解码 → 写 queue（按 chunkSize 字节分块，不按行）
	var wg sync.WaitGroup
	wg.Add(2)

	readStream := func(r io.Reader, streamType StreamEventType) {
		defer wg.Done()
		dec := &chunkDecoder{encoding: h.encoding}
		buf := make([]byte, h.chunkSize)
		for {
			n, readErr := r.Read(buf)
			if n > 0 {
				if text := dec.decode(buf[:n]); text != "" {
					if !send(StreamEvent{Type: streamType, Data: text, Timestamp: time.Now()}) {
						return
					}
				}
			}
			if readErr != nil {
				if readErr != io.EOF {
					send(StreamEvent{
						Type:      StreamEventTypeError,
						Data:      fmt.Sprintf("%s 读取错误: %v", streamType, readErr),
						Timestamp: time.Now(),
					})
					return
				}
				// 流结束：冲出解码器里剩余的半个字符
				if tail := dec.flush(); tail != "" {
					send(StreamEvent{Type: streamType, Data: tail, Timestamp: time.Now()})
				}
				return
			}
		}
	}

	go readStream(stdoutPipe, StreamEventTypeStdout)
	go readStream(stderrPipe, StreamEventTypeStderr)

	// reader 完成后等进程退出并发送 EXIT 事件
	go func() {
		wg.Wait()
		select {
		case <-done:
			// 消费者已放弃：终止进程，避免 Wait 永久阻塞
			h.killProcessTree()
		default:
		}
		exitCode := 0
		if waitErr := h.cmd.Wait(); waitErr != nil {
			if exitErr, ok := waitErr.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
			} else {
				exitCode = -1
			}
		}
		if send(StreamEvent{
			Type:      StreamEventTypeExit,
			Data:      fmt.Sprintf("%d", exitCode),
			ExitCode:  exitCode,
			Timestamp: time.Now(),
		}) {
			close(queue)
		}
	}()

	// 主协程：select 循环消费 queue，支持超时和 context 取消
	go func() {
		defer close(ch)
		defer stop()

		// 超时 ticker（1 秒间隔检查）；下游阻塞在发送时也要能检查超时，
		// 故发送与收事件放在同一个 select 里（pending 暂存待发事件）。
		var ticker *time.Ticker
		if h.overallTimeout > 0 {
			ticker = time.NewTicker(time.Second)
			defer ticker.Stop()
		}
		startTime := time.Now()

		var pending *StreamEvent
		for {
			var sendCh chan StreamEvent
			var sendEvent StreamEvent
			if pending != nil {
				sendCh, sendEvent = ch, *pending
			}
			// 有待发事件时不再从 queue 取新事件，避免覆盖 pending 丢块
			var queueCh chan StreamEvent
			if pending == nil {
				queueCh = queue
			}
			var tickCh <-chan time.Time
			if ticker != nil {
				tickCh = ticker.C
			}

			select {
			case sendCh <- sendEvent:
				finished := sendEvent.Type == StreamEventTypeExit
				pending = nil
				if finished {
					return
				}

			case event, ok := <-queueCh:
				if !ok {
					// queue 关闭，所有事件已处理完毕
					return
				}
				pending = &event

			case <-tickCh:
				if h.overallTimeout > 0 && time.Since(startTime) > time.Duration(h.overallTimeout)*time.Second {
					logger.Warn(logComponent).
						Str("event_type", "STREAM_TIMEOUT").
						Int("overall_timeout", h.overallTimeout).
						Msg("streaming execution timed out")
					h.killProcessTree()
					select {
					case ch <- StreamEvent{
						Type:      StreamEventTypeError,
						Data:      fmt.Sprintf("执行超时 %d 秒", h.overallTimeout),
						Timestamp: time.Now(),
					}:
					default:
					}
					return
				}

			case <-ctx.Done():
				logger.Warn(logComponent).
					Str("event_type", "STREAM_CANCELLED").
					Msg("streaming execution cancelled")
				h.killProcessTree()
				select {
				case ch <- StreamEvent{
					Type:      StreamEventTypeError,
					Data:      "执行被取消",
					Timestamp: time.Now(),
				}:
				default:
				}
				return
			}
		}
	}()

	return ch, nil
}

// Background 启动后台进程并等待 grace 秒检测是否提前退出。
func (h *AsyncProcessHandler) Background(grace float64) (pid int, err error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.cmd == nil {
		return 0, fmt.Errorf("进程命令为 nil")
	}

	// 后台模式：stdout 丢弃，stderr 保留管道。
	// 保留 stderr 有两个目的：早退时能拿到诊断；持续排空避免子进程写满管道后
	// 阻塞（历史实现设为 nil，进程初期的报错信息完全丢失）。
	// Python: shell_operation.py:393-398 stderr = asyncio.subprocess.PIPE
	h.cmd.Stdin = nil
	h.cmd.Stdout = nil
	stderrPipe, pipeErr := h.cmd.StderrPipe()
	if pipeErr != nil {
		return 0, fmt.Errorf("创建 stderr 管道失败: %w", pipeErr)
	}

	h.setProcessGroup()

	if err := h.cmd.Start(); err != nil {
		return 0, fmt.Errorf("启动后台进程失败: %w", err)
	}

	pid = h.cmd.Process.Pid

	// 排空协程：无论后续走哪条分支都持续读，避免管道写满阻塞子进程。
	// Python: background() 里 create_task(_drain_background_stderr())
	stderrCh := make(chan string, 1)
	stderrDone := make(chan struct{})
	go func() {
		msg := drainStderr(stderrPipe, backgroundStderrRetainLimit)
		stderrCh <- msg
		close(stderrDone)
	}()

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
						Msg("background process exited with non-zero code within grace")
					// 早退时把已捕获的 stderr 拼进错误：只给退出码时，
					// 调用方无从判断是依赖缺失、命令写错还是权限问题。
					msg := fmt.Sprintf("进程提前退出，退出码 %d", exitErr.ExitCode())
					if stderrText := collectBackgroundStderr(stderrCh, stderrDone, backgroundStderrWait); stderrText != "" {
						msg += ": " + stderrText
					}
					return pid, fmt.Errorf("%s", msg)
				}
				// 退出码 0 也视为成功
				logger.Info(logComponent).
					Str("event_type", "BACKGROUND_EARLY_EXIT_SUCCESS").
					Int("pid", pid).
					Msg("background process exited normally within grace")
				return pid, nil
			}
			return pid, nil
		case <-timer.C:
			// grace 超时，进程还在运行 → 成功
			logger.Info(logComponent).
				Str("event_type", "BACKGROUND_GRACE_TIMEOUT").
				Int("pid", pid).
				Float64("grace", grace).
				Msg("background process grace check timed out, process still running")
			return pid, nil
		}
	}

	return pid, nil
}

// KillProcessTree 终止进程树（导出版本）。
func (h *AsyncProcessHandler) KillProcessTree() {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.killProcessTree()
}

// PrepareEnvironment 合并系统环境变量和自定义环境变量。
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

// CreateTmpFile 创建临时文件并写入内容。
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

// DeleteTmpFile 删除临时文件。
func (OperationUtils) DeleteTmpFile(path string) error {
	return os.Remove(path)
}

// ResolveCwd 解析当前工作目录路径（无 ctx 版本，保留兼容）。
// 优先使用 resolveCwd(ctx, ...)：agent 的 CwdState（cwd/project_root/
// workspace）经 ctx 注入，用 context.Background() 读取恒为空，
// 会让沙箱根为空、相对路径基准退化为进程 CWD。
func ResolveCwd(cwdPath string) string {
	return resolveCwd(context.Background(), cwdPath)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// drainStderr 排空 stderr 管道并保留前 limit 字节，超限追加截断标记。
// 必须读到 EOF：提前 return 会让管道写满，子进程在 stderr 上阻塞。
// Python: _drain_background_stderr (utils.py:322-343)
func drainStderr(r io.Reader, limit int) string {
	var retained bytes.Buffer
	truncated := false
	buf := make([]byte, defaultChunkSize)
	for {
		n, readErr := r.Read(buf)
		if n > 0 {
			remaining := limit - retained.Len()
			if remaining > 0 {
				if n <= remaining {
					retained.Write(buf[:n])
				} else {
					retained.Write(buf[:remaining])
					truncated = true
				}
			} else {
				truncated = true
			}
		}
		if readErr != nil {
			break
		}
	}
	message := strings.TrimSpace(retained.String())
	if truncated {
		if message != "" {
			return message + "\n[stderr truncated]"
		}
		return "[stderr truncated]"
	}
	return message
}

// collectBackgroundStderr 在限定时间内取排空协程的结果。
// 后代进程可能继承 stderr 写端，管道 EOF 迟迟不到；超时返回空串，
// 协程继续排空（不阻塞调用方，也不让管道写满）。
// Python: background() 里 asyncio.wait({stderr_task}, timeout=remaining)
func collectBackgroundStderr(ch <-chan string, done <-chan struct{}, wait time.Duration) string {
	select {
	case msg := <-ch:
		return msg
	case <-done:
		// 正常 EOF 后 close(done)，此时 ch 里已有值，再取一次
		select {
		case msg := <-ch:
			return msg
		default:
			return ""
		}
	case <-time.After(wait):
		return ""
	}
}

// resolveCwd 基于调用方 ctx 中的 CwdState 解析工作目录。
// 对齐 Python get_cwd()：contextvar 随 ctx 传播，agent 隔离靠不同 CwdState 实例。
func resolveCwd(ctx context.Context, cwdPath string) string {
	if ctx == nil {
		ctx = context.Background()
	}
	if cwdPath != "" {
		if !filepath.IsAbs(cwdPath) {
			return filepath.Join(cwd.GetCwd(ctx), cwdPath)
		}
		return cwdPath
	}
	return cwd.GetCwd(ctx)
}

// setProcessGroup 让子进程独立成组（平台实现在 proc_unix.go / proc_windows.go）。
func (h *AsyncProcessHandler) setProcessGroup() {
	applyProcessGroup(h.cmd)
}

// killProcessTree 终止子进程树（平台实现在 proc_unix.go / proc_windows.go）。
func (h *AsyncProcessHandler) killProcessTree() {
	killProcessTreePlatform(h)
}

// decodeComplete 解码整块字节，末尾不完整的多字节序列作为 rest 返回。
// Python 用 codecs 的增量解码器保留半个字符；Go 侧显式算「末尾不完整字节数」：
// 单字节系（UTF-8）看起始字节，双字节系（GBK/Big5 等）按字节对扫描。
func decodeComplete(raw []byte, encName string) (string, []byte) {
	hold := incompleteTailLen(raw, encName)
	data := raw[:len(raw)-hold]
	out, err := decodeStrict(data, encName)
	if err != nil {
		return decodeOutput(raw, encName), nil
	}
	return out, append([]byte(nil), raw[len(raw)-hold:]...)
}

// incompleteTailLen 返回末尾不完整多字节序列的字节数（0 表示完整）。
func incompleteTailLen(raw []byte, encName string) int {
	switch strings.ToLower(strings.TrimSpace(encName)) {
	case "", "utf-8", "utf8":
		return incompleteUTF8Tail(raw)
	case "gbk", "cp936", "gb2312", "ms936", "gb18030", "cp54936", "big5", "cp950":
		return incompleteDoubleByteTail(raw)
	default:
		return 0
	}
}

// incompleteUTF8Tail 返回末尾半个 UTF-8 字符的字节数。
func incompleteUTF8Tail(raw []byte) int {
	for i := 0; i < utf8.UTFMax && i < len(raw); i++ {
		b := raw[len(raw)-1-i]
		if !utf8.RuneStart(b) {
			continue
		}
		want := 1
		switch {
		case b&0xF8 == 0xF0:
			want = 4
		case b&0xF0 == 0xE0:
			want = 3
		case b&0xE0 == 0xC0:
			want = 2
		}
		if want > i+1 {
			return i + 1
		}
		return 0
	}
	return 0
}

// incompleteDoubleByteTail 返回末尾半个双字节字符（GBK/Big5 系）的字节数。
func incompleteDoubleByteTail(raw []byte) int {
	i := 0
	for i < len(raw) {
		if raw[i] < 0x80 {
			i++
			continue
		}
		if i+1 >= len(raw) {
			return 1
		}
		i += 2
	}
	return 0
}

// decodeStrict 严格解码：字节序列非法时返回 error；编码名无法识别时按原文直通。
func decodeStrict(raw []byte, encName string) (string, error) {
	if len(raw) == 0 {
		return "", nil
	}
	switch strings.ToLower(strings.TrimSpace(encName)) {
	case "", "utf-8", "utf8":
		return string(raw), nil
	case "gbk", "cp936", "gb2312", "ms936":
		out, err := simplifiedchinese.GBK.NewDecoder().Bytes(raw)
		return string(out), err
	case "gb18030", "cp54936":
		out, err := simplifiedchinese.GB18030.NewDecoder().Bytes(raw)
		return string(out), err
	case "big5", "cp950":
		out, err := traditionalchinese.Big5.NewDecoder().Bytes(raw)
		return string(out), err
	default:
		e, err := textencoding.MIME.Encoding(strings.ToLower(strings.TrimSpace(encName)))
		if err != nil || e == nil {
			return string(raw), nil
		}
		out, decErr := e.NewDecoder().Bytes(raw)
		return string(out), decErr
	}
}

// decodeOutput 把子进程的原始输出字节按指定编码解码为字符串。
// Python: asyncio 子进程输出 bytes.decode(encoding)；encoding 缺省为
// _detect_shell_encoding()（中文 Windows 返回 cp936）。
// UTF-8 系直通；GBK 系用 x/text 解码（GBK 解码器即 cp936，gb2312 是其子集）；
// 其余编码名经 ianaindex 解析，解析或解码失败时原样返回（与旧行为一致）。
func decodeOutput(raw []byte, encName string) string {
	if len(raw) == 0 {
		return ""
	}
	if out, err := decodeStrict(raw, encName); err == nil {
		return out
	}
	return string(raw)
}
