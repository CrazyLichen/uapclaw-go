package codebuddy

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WireFrame CLI 线帧。
// Python 无对应物（偏差 D-15.22-c）：帧语义按 CodeBuddy Code CLI 2.181.0
// headless 实测取得（`-p --output-format stream-json`），与 Claude Code 同款。
// 实测样例（2026-12-06）：
//
//	{"type":"system","subtype":"init","session_id":"...","tools":[...],"model":"..."}
//	{"type":"system","subtype":"usage_update","_meta":{"codebuddy.ai/turnUsage":{...}}}
//	{"type":"assistant","message":{"content":[{"type":"text","text":"PONG"}],"usage":{...}}}
//	{"type":"result","subtype":"success","is_error":false,"result":"PONG",
//	  "duration_ms":4069,"num_turns":2,"usage":{...}}
//
// input 方向（--input-format stream-json）：
//
//	{"type":"user","message":{"role":"user","content":[{"type":"text","text":"..."}]}}
type WireFrame struct {
	// Type 帧大类：system / assistant / result（input 方向为 user）
	Type string `json:"type"`
	// Subtype 子类：system 帧为 init/status/usage_update；result 帧为
	// success / error_max_turns / error_during_execution
	Subtype string `json:"subtype,omitempty"`
	// SessionID CLI 原生会话 id（--resume 载体）
	SessionID string `json:"session_id,omitempty"`
	// UUID 帧标识
	UUID string `json:"uuid,omitempty"`
	// Message assistant 帧的消息体（content blocks 数组原样）
	Message json.RawMessage `json:"message,omitempty"`
	// Result result 帧的最终文本输出
	Result string `json:"result,omitempty"`
	// IsError result 帧错误标记
	IsError bool `json:"is_error,omitempty"`
	// DurationMs result 帧轮耗时
	DurationMs *int64 `json:"duration_ms,omitempty"`
	// NumTurns result 帧轮内次数
	NumTurns *int `json:"num_turns,omitempty"`
	// Usage result 帧累计用量
	Usage json.RawMessage `json:"usage,omitempty"`
	// Meta _meta 元数据（usage_update 帧的 codebuddy.ai/turnUsage 在此）
	Meta map[string]any `json:"_meta,omitempty"`
}

// Transport stdio 双工传输缝（测试注入点，对齐 dsh 的 SDKFactory 手法）。
// Python 无对应物（偏差 D-15.22-a）。
type Transport interface {
	// SendFrame 向 CLI stdin 写一帧（JSONL，自动补换行）
	SendFrame(frame map[string]any) error
	// ReadFrame 从 CLI stdout 读一帧；inactivity 超时或 ctx 取消返回错误
	ReadFrame(ctx context.Context) (WireFrame, error)
	// StderrTail 进程退出后可用的 stderr 尾巴（诊断用，≤2KB）
	StderrTail() string
	// Close 关闭传输（幂等）：杀进程、关管道
	Close() error
}

// TransportOptions 默认子进程实现的启动参数。
type TransportOptions struct {
	// CliPath CLI 可执行路径
	CliPath string
	// Args 已展开的完整参数（model/resume/mcp/permission-mode 等）
	Args []string
	// Cwd 子进程工作目录
	Cwd string
	// Env 附加环境变量
	Env map[string]string
	// TurnTimeout 单轮超时；0 用 config 默认
	TurnTimeout time.Duration
	// InactivityTimeout 轮内无输出超时；0 用 config 默认
	InactivityTimeout time.Duration
}

// TransportFactory 传输缝工厂；nil 缝走默认子进程实现。
type TransportFactory func(options TransportOptions) (Transport, error)

// pipeTransport Transport 的管道测试实现（真子进程实现共用读帧循环）。
type pipeTransport struct {
	// stdin CLI stdin 写端
	stdin io.WriteCloser
	// stdout CLI stdout 读端
	stdout *bufio.Reader
	// inactivity 轮内无输出超时（0 表示不超时）
	inactivity time.Duration
	// writeMu 串行化 stdin 写入
	writeMu sync.Mutex
	// closeOnce Close 幂等
	closeOnce sync.Once
	// closed 标记已关闭
	closed bool
	// stderr 诊断尾巴（pipe 实现恒空）
	stderrTail string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProcessTransport 启动 CLI 子进程并返回 stdio 双工传输。
// stderr 尾读、双超时手法对齐 15.13 externalcli/transport（裁决 5：不 import
// externalcli——agent_teams 包反向引用成环）。
func NewProcessTransport(options TransportOptions) (Transport, error) {
	cmd := exec.Command(options.CliPath, options.Args...)
	if options.Cwd != "" {
		cmd.Dir = options.Cwd
	}
	if len(options.Env) > 0 {
		cmd.Env = envSlice(options.Env)
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderrBuffer := &syncBuffer{}
	cmd.Stderr = stderrBuffer
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start codebuddy CLI: %w", err)
	}
	transport := &processTransport{
		cmd:          cmd,
		stdin:        stdin,
		stderrBuffer: stderrBuffer,
		inactivity:   options.InactivityTimeout,
		turnTimeout:  options.TurnTimeout,
		waitOnce:     sync.Once{},
		exited:       make(chan struct{}),
		lines:        make(chan string, 16),
		readErr:      make(chan error, 1),
	}
	go transport.pump(stdout)
	go transport.wait()
	return transport, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// parseFrame 解码一行 JSONL 为线帧。
func parseFrame(line json.RawMessage) (WireFrame, error) {
	var frame WireFrame
	if err := json.Unmarshal(line, &frame); err != nil {
		return WireFrame{}, fmt.Errorf("codebuddy frame decode failed: %w", err)
	}
	return frame, nil
}

// newPipeTransport 测试用管道传输。
func newPipeTransport(stdin io.WriteCloser, stdout *bufio.Reader) *pipeTransport {
	return &pipeTransport{stdin: stdin, stdout: stdout}
}

// SendFrame 向 stdin 写一帧。
func (t *pipeTransport) SendFrame(frame map[string]any) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.closed {
		return fmt.Errorf("codebuddy transport closed")
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if _, err := t.stdin.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadFrame 读一帧：inactivity 超时或 stdin EOF/ctx 取消报错。
// 与 processTransport 共用超时语义（单轮 turnTimeout 由 harness 层承载，
// 传输层只管单帧 inactivity 与 ctx）。
func (t *pipeTransport) ReadFrame(ctx context.Context) (WireFrame, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := t.stdout.ReadString('\n')
		ch <- result{line: line, err: err}
	}()
	var timer *time.Timer
	var timeout <-chan time.Time
	if t.inactivity > 0 {
		timer = time.NewTimer(t.inactivity)
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case res := <-ch:
		if res.err != nil {
			return WireFrame{}, fmt.Errorf("codebuddy stdout closed: %w", res.err)
		}
		return parseFrame(json.RawMessage(res.line))
	case <-timeout:
		return WireFrame{}, fmt.Errorf("codebuddy inactivity timeout after %s", t.inactivity)
	case <-ctx.Done():
		return WireFrame{}, ctx.Err()
	}
}

// StderrTail 管道实现无子进程 stderr。
func (t *pipeTransport) StderrTail() string { return t.stderrTail }

// Close 关闭 stdin 写端（幂等）。
func (t *pipeTransport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		t.closed = true
		err = t.stdin.Close()
	})
	return err
}

// processTransport 默认子进程传输实现。
type processTransport struct {
	cmd          *exec.Cmd
	stdin        io.WriteCloser
	stderrBuffer *syncBuffer
	inactivity   time.Duration
	turnTimeout  time.Duration
	waitOnce     sync.Once
	exited       chan struct{}
	lines        chan string
	readErr      chan error
	writeMu      sync.Mutex
	closeOnce    sync.Once
	closed       bool
}

// pump 把 stdout 逐行泵进 lines 通道；EOF 时投递读错误。
func (t *processTransport) pump(stdout io.Reader) {
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 0, 1024*1024), 16*1024*1024)
	for scanner.Scan() {
		t.lines <- scanner.Text()
	}
	if err := scanner.Err(); err != nil {
		t.readErr <- err
	} else {
		t.readErr <- io.EOF
	}
}

// wait 等待进程退出并关闭 exited。
func (t *processTransport) wait() {
	_ = t.cmd.Wait()
	close(t.exited)
}

// SendFrame 向子进程 stdin 写一帧。
func (t *processTransport) SendFrame(frame map[string]any) error {
	t.writeMu.Lock()
	defer t.writeMu.Unlock()
	if t.closed {
		return fmt.Errorf("codebuddy transport closed")
	}
	encoded, err := json.Marshal(frame)
	if err != nil {
		return err
	}
	if _, err := t.stdin.Write(append(encoded, '\n')); err != nil {
		return err
	}
	return nil
}

// ReadFrame 从子进程 stdout 读一帧：inactivity 超时 / 单轮超时 / ctx 取消 /
// 进程退出均报错。
func (t *processTransport) ReadFrame(ctx context.Context) (WireFrame, error) {
	var timer *time.Timer
	var inactivity <-chan time.Time
	if t.inactivity > 0 {
		timer = time.NewTimer(t.inactivity)
		defer timer.Stop()
		inactivity = timer.C
	}
	var turn <-chan time.Time
	if t.turnTimeout > 0 {
		turnTimer := time.NewTimer(t.turnTimeout)
		defer turnTimer.Stop()
		turn = turnTimer.C
	}
	for {
		select {
		case line := <-t.lines:
			frame, err := parseFrame(json.RawMessage(line))
			if err != nil {
				return WireFrame{}, err
			}
			// 单帧成功后重置 inactivity 计时
			if timer != nil {
				if !timer.Stop() {
					select {
					case <-timer.C:
					default:
					}
				}
				timer.Reset(t.inactivity)
			}
			return frame, nil
		case err := <-t.readErr:
			return WireFrame{}, fmt.Errorf("codebuddy stdout closed: %w", err)
		case <-inactivity:
			return WireFrame{}, fmt.Errorf("codebuddy inactivity timeout after %s", t.inactivity)
		case <-turn:
			return WireFrame{}, fmt.Errorf("codebuddy turn timeout after %s", t.turnTimeout)
		case <-ctx.Done():
			return WireFrame{}, ctx.Err()
		case <-t.exited:
			return WireFrame{}, fmt.Errorf("codebuddy process exited: %s", tailString(t.stderrBuffer.String(), 2048))
		}
	}
}

// StderrTail 返回子进程 stderr 尾巴（≤2KB）。
func (t *processTransport) StderrTail() string {
	return tailString(t.stderrBuffer.String(), 2048)
}

// Close 杀进程并关 stdin（幂等）。
func (t *processTransport) Close() error {
	var err error
	t.closeOnce.Do(func() {
		t.closed = true
		if t.cmd.Process != nil {
			_ = t.cmd.Process.Kill()
		}
		err = t.stdin.Close()
	})
	return err
}

// envSlice 环境映射转 os.Environ 形态（附加在进程默认环境之后）。
func envSlice(env map[string]string) []string {
	environ := stdlibEnviron()
	for key, value := range env {
		environ = append(environ, key+"="+value)
	}
	return environ
}

// tailString 取字符串尾部（stderr 尾读，对齐 15.13 手法）。
func tailString(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[len(s)-limit:]
}

// stdlibEnviron 返回当前进程环境。
func stdlibEnviron() []string { return os.Environ() }

// ──────────────────────────── 辅助类型 ────────────────────────────

// syncBuffer 并发安全字节缓冲（子进程 stderr 收集）。
type syncBuffer struct {
	mu  sync.Mutex
	buf []byte
}

// Write 实现 io.Writer。
func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	return len(p), nil
}

// String 返回已收集内容。
func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
