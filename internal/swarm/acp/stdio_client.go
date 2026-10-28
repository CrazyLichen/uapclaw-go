package acp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// rpcResult 单次 RPC 调用的结果投递。
type rpcResult struct {
	Msg map[string]any
	Err error
}

// chatConfig Chat 可选项聚合。
type chatConfig struct {
	timeout time.Duration
}

// AcpStdioClient 管理一个外部 ACP 子进程的 stdio JSON-RPC 会话。
// 并发模型：读写分离 goroutine + sync 原语（对齐 Python asyncio 单事件循环
// 的串行读写语义：reader goroutine 常驻解析分发，写侧 writeMu 串行化）。
// Python: AcpStdioClient (stdio_client.py L219-670)
type AcpStdioClient struct {
	// command 启动命令（trim 后）
	command string
	// args 命令参数
	args []string
	// cwd 子进程工作目录（空 = 继承）
	cwd string
	// env 子进程环境（键值对）
	env []string

	// mu 保护连接状态字段
	mu sync.Mutex
	// cmd 子进程句柄
	cmd *exec.Cmd
	// stdin 子进程 stdin 写端
	stdin io.WriteCloser
	// connected 连接标志
	connected bool
	// sessionID ACP 会话 ID
	sessionID string
	// nextID RPC 自增标识
	nextID int64
	// stderrDone stderr drain 完成信号
	stderrDone chan struct{}
	// exitCh 进程退出信号（Connect 时由后台 Wait goroutine 关闭；免锁）
	exitCh chan struct{}
	// closed 已关闭标志（Close 置位）
	closed bool

	// writeMu 串行化 stdin 写
	writeMu sync.Mutex
	// pendingMu 保护 pending
	pendingMu sync.Mutex
	// pending 请求 ID → 等待者
	pending map[int64]chan *rpcResult

	// updatesCh session/update 文本通道（缓冲 256；构造后不可变，免锁访问）。
	// Python _stdout_buf 由事件循环独占；Go 侧残缓冲为 readerLoop 局部变量
	// （Close 不读它，EOF Warn 在 readerLoop 的错误分支内完成）。
	updatesCh chan string

	// closeOnce Close 幂等
	closeOnce sync.Once
	// closeErr Close 结果
	closeErr error
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────// NewAcpStdioClient 构造客户端。
// env 为 profile env map（Python dict），内部经 BuildAcpSubprocessEnv 合成。
// Python: __init__ (stdio_client.py L222-238)
func NewAcpStdioClient(command string, args []string, cwd string, env map[string]any) *AcpStdioClient {
	c := &AcpStdioClient{
		command:   strings.TrimSpace(command),
		args:      append([]string{}, args...),
		cwd:       cwd,
		pending:   make(map[int64]chan *rpcResult),
		updatesCh: make(chan string, 256),
	}
	envMap := BuildAcpSubprocessEnv(env)
	for k, v := range envMap {
		c.env = append(c.env, k+"="+v)
	}
	return c
}

// SessionID 返回当前会话 ID。
// Python: session_id property (L380-382)
func (c *AcpStdioClient) SessionID() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessionID
}

// IsConnected 是否已连接且子进程存活（returncode is None）。
// Python: is_connected property (L384-389)
func (c *AcpStdioClient) IsConnected() bool {
	c.mu.Lock()
	closed := c.closed
	cmd := c.cmd
	exitCh := c.exitCh
	c.mu.Unlock()
	if closed || cmd == nil {
		return false
	}
	if exitCh == nil {
		return false
	}
	// 进程退出即 exitCh 关闭（returncode is not None 的免锁判定）
	select {
	case <-exitCh:
		return false
	default:
		return true
	}
}

// Connect 启动子进程并完成 initialize + session/new 握手。
// Python: connect() (L488-543)
func (c *AcpStdioClient) Connect(ctx context.Context) error {
	c.mu.Lock()
	if c.connected && c.cmd != nil {
		c.mu.Unlock()
		return nil
	}
	if c.closed {
		c.mu.Unlock()
		return errors.New("AcpStdioClient is closed")
	}
	if c.command == "" {
		c.mu.Unlock()
		return errors.New("ACP agent command is empty")
	}
	// Python L494: self._stdout_buf = ""——Go 侧残缓冲为 readerLoop 局部变量，
	// 每次连接新建 goroutine 自带空缓冲，等价复位。
	cmd := exec.Command(resolveSpawnCommand(c.command), c.args...)
	cmd.Env = c.env
	if c.cwd != "" {
		cmd.Dir = c.cwd
	}
	if killProcessGroup() {
		cmd.SysProcAttr = newPgroupAttr()
	}
	stdin, err := cmd.StdinPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		c.mu.Unlock()
		return err
	}
	if err := cmd.Start(); err != nil {
		c.mu.Unlock()
		return err
	}
	c.cmd = cmd
	c.stdin = stdin
	c.connected = true
	c.sessionID = ""
	c.exitCh = make(chan struct{})
	stderrDone := make(chan struct{})
	c.stderrDone = stderrDone
	c.mu.Unlock()

	// 后台 Wait goroutine：进程退出即关闭 exitCh（ProcessState 仅此 goroutine 写）。
	// Python asyncio.Process.returncode 由事件循环统一维护，等价此单写者模型。
	exitCh := c.exitCh
	go func() {
		_ = cmd.Wait()
		close(exitCh)
	}()

	// stderr drain goroutine（Python _drain_stderr L396-409）
	go func() {
		defer close(stderrDone)
		scanner := bufio.NewScanner(stderr)
		scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
		for scanner.Scan() {
			if txt := strings.TrimRight(scanner.Text(), "\r\n"); txt != "" {
				if debugChat() {
					logger.Info(logComponent).Str("line", txt).Msg("[acp-agent stderr]")
				} else {
					logger.Debug(logComponent).Str("line", txt).Msg("[acp-agent stderr]")
				}
			}
		}
	}()

	// reader goroutine：解析 stdout 并分发（Python _read_one_message 缓冲循环）
	go c.readerLoop(stdout)

	// initialize（120s）
	_, err = c.rpcCall(ctx, "initialize", map[string]any{
		"protocolVersion": 1,
		// 广告 FS 能力（Codex ACP 等以完成工具回合）；terminal 不支持故省略
		"clientCapabilities": map[string]any{
			"fs":       map[string]any{"readTextFile": true, "writeTextFile": true},
			"terminal": false,
		},
		"clientInfo": map[string]any{"name": "jiuwenswarm", "version": "0.1"},
	}, 120*time.Second)
	if err != nil {
		_ = c.Close()
		return err
	}

	// session/new（60s）；Codex ACP 期待 cwd + mcpServers
	sessionCwd := c.cwd
	if sessionCwd == "" {
		sessionCwd, _ = os.Getwd()
	}
	result, err := c.rpcCall(ctx, "session/new", map[string]any{
		"cwd": sessionCwd, "mcpServers": []any{},
	}, 60*time.Second)
	if err != nil {
		_ = c.Close()
		return err
	}
	resultMap, ok := result.(map[string]any)
	if !ok {
		_ = c.Close()
		return fmt.Errorf("session/new: unexpected result: %v", result)
	}
	sid := strings.TrimSpace(strOf(firstNonNil(resultMap["sessionId"], resultMap["session_id"])))
	if sid == "" || sid == "<nil>" {
		_ = c.Close()
		return fmt.Errorf("session/new: missing sessionId in %v", result)
	}
	c.mu.Lock()
	c.sessionID = sid
	c.mu.Unlock()
	logger.Info(logComponent).Str("sessionId", sid).Msg("[AcpStdioClient] connected")
	return nil
}

// Chat 发送 prompt 并聚合文本回复。
// Python: chat() (L545-590)
func (c *AcpStdioClient) Chat(ctx context.Context, message string, opts ...ChatOption) (string, error) {
	cfg := chatConfig{timeout: defaultChatTimeout()}
	for _, o := range opts {
		o(&cfg)
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", errors.New("AcpStdioClient is closed")
	}
	if !c.connected || c.cmd == nil || c.sessionID == "" {
		c.mu.Unlock()
		return "", errors.New("not connected; call connect() first")
	}
	sid := c.sessionID
	c.mu.Unlock()

	rid := c.allocID()
	params := map[string]any{
		"sessionId": sid,
		"prompt":    []any{map[string]any{"type": "text", "text": message}},
	}
	waitCh, err := c.registerPending(rid)
	if err != nil {
		return "", err
	}
	defer c.removePending(rid)
	if err := c.writeRequest(rid, "session/prompt", params); err != nil {
		return "", err
	}

	var parts []string
	deadline := time.After(cfg.timeout)
	updates := c.updates()
	for {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-deadline:
			return "", errors.New("timed out or EOF waiting for session/prompt completion")
		case text := <-updates:
			if text != "" {
				parts = append(parts, text)
			}
		case res := <-waitCh:
			if res.Err != nil {
				return "", res.Err
			}
			// Python 单循环顺序语义：响应行之前到达的 update 必已聚合。
			// Go 双通道 select 可能先取 result——非阻塞排空 updatesCh 补齐
			// 缓冲中已投递的 update（readerLoop 顺序保证 result 投递前
			// 先前 update 已入缓冲）。
		drainUpdates:
			for {
				select {
				case text := <-updates:
					if text != "" {
						parts = append(parts, text)
					}
				default:
					break drainUpdates
				}
			}
			// Python L590: "".join(parts).strip() or _result_text_fallback(...)
			joined := strings.TrimSpace(strings.Join(parts, ""))
			if joined != "" {
				return joined, nil
			}
			return resultTextFallback(res.Msg["result"]), nil
		}
	}
}

// Close 幂等三段优雅关闭。
// Python: close() (L597-660)
func (c *AcpStdioClient) Close() error {
	c.closeOnce.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.connected = false
		c.sessionID = ""
		cmd := c.cmd
		stdin := c.stdin
		stderrDone := c.stderrDone
		exitCh := c.exitCh
		c.cmd = nil
		c.stdin = nil
		c.stderrDone = nil
		c.exitCh = nil
		c.mu.Unlock()

		if cmd == nil {
			if stderrDone != nil {
				<-stderrDone
			}
			c.failAllPending(errors.New("AcpStdioClient is closed"))
			return
		}

		// 1. stdin 关闭（等 ACP_CLOSE_STDIN_WAIT_S）
		if stdin != nil {
			_ = stdin.Close()
			waitChan(exitCh, closeStdinWaitS())
		}

		// 2. TERM（进程组，等 ACP_CLOSE_TERM_WAIT_S）
		if !chanClosed(exitCh) {
			signalProcessLeader(cmd, false)
			waitChan(exitCh, closeTermWaitS())
		}

		// 3. KILL（等 ACP_CLOSE_KILL_WAIT_S）
		if !chanClosed(exitCh) {
			signalProcessLeader(cmd, true)
			waitChan(exitCh, closeKillWaitS())
		}

		// 4. stderr join（≤ ACP_CLOSE_STDERR_JOIN_S）
		if stderrDone != nil {
			select {
			case <-stderrDone:
			case <-time.After(closeStderrJoinS()):
			}
		}

		// 5. 失败 pending 投递错误
		c.failAllPending(errors.New("AcpStdioClient is closed"))
	})
	return c.closeErr
}

// ChatOption Chat 可选项。
type ChatOption func(*chatConfig)

// WithChatTimeout 设置 Chat 超时。
func WithChatTimeout(d time.Duration) ChatOption {
	return func(c *chatConfig) { c.timeout = d }
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rpcCall 出站 JSON-RPC 调用：写请求 → 等 pending 响应。
// Python: _rpc_call (L454-486)——readerLoop 常驻分发等价于 Python 循环内
// session/update continue（投递 updates chan）与 _is_peer_jsonrpc_request
// 分支（dispatchMessage 异步处理）。
func (c *AcpStdioClient) rpcCall(ctx context.Context, method string, params map[string]any, timeout time.Duration) (any, error) {
	rid := c.allocID()
	waitCh, err := c.registerPending(rid)
	if err != nil {
		return nil, err
	}
	defer c.removePending(rid)
	if err := c.writeRequest(rid, method, params); err != nil {
		return nil, err
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(timeout):
		return nil, fmt.Errorf("ACP agent closed stream or timed out waiting for %s response", method)
	case res := <-waitCh:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Msg["result"], nil
	}
}

// readerLoop 读 stdout、解析 JSON-RPC 消息并分发。
// Python _read_one_message (L411-452) 的常驻化：内层解析循环产出消息，
// EOF/读错误时按 Python 语义残缓冲 Warn + failAllPending。
// 残缓冲为局部变量（Python _stdout_buf 的串行访问在 Go 侧由
// readerLoop 单 goroutine 独占保证）。
func (c *AcpStdioClient) readerLoop(stdout io.ReadCloser) {
	buf := make([]byte, stdoutReadChunk())
	stdoutBuf := make([]byte, 0, stdoutReadChunk())
	for {
		n, err := stdout.Read(buf)
		if n > 0 {
			stdoutBuf = append(stdoutBuf, buf[:n]...)
			// 解析循环
			for {
				obj, rest, perr := consumeOneJSONErr(stdoutBuf)
				if perr != nil {
					logger.Error(logComponent).Err(perr).Msg("ACP stdout parse buffer exceeds limit")
					c.failAllPending(perr)
					return
				}
				if obj == nil {
					break
				}
				stdoutBuf = rest
				msg, ok := obj.(map[string]any)
				if !ok {
					logger.Warn(logComponent).
						Msg("[AcpStdioClient] dropping non-object JSON-RPC message")
					continue
				}
				if debugChat() {
					logger.Info(logComponent).Str("method", strOf(msg["method"])).
						Str("id", strOf(msg["id"])).Msg("[AcpStdioClient] rx")
				}
				c.dispatchMessage(msg)
			}
		}
		if err != nil {
			if err != io.EOF && !errors.Is(err, os.ErrClosed) {
				logger.Debug(logComponent).Err(err).Msg("stdout reader stopped")
			}
			if len(bytes.TrimSpace(stdoutBuf)) > 0 {
				logger.Warn(logComponent).Int("bytes", len(stdoutBuf)).
					Msg("[AcpStdioClient] EOF while partial JSON buffered")
			}
			c.failAllPending(errors.New("ACP agent closed stream"))
			return
		}
	}
}

// dispatchMessage 按消息形态分发：响应 → pending；对端请求 → 异步处理；
// session/update 通知 → updates chan；其余忽略。
func (c *AcpStdioClient) dispatchMessage(msg map[string]any) {
	// 响应：有 id 且有 result 或 error
	if idAny := msg["id"]; idAny != nil {
		if _, hasResult := msg["result"]; hasResult {
			c.deliverPending(idAny, &rpcResult{Msg: msg})
			return
		}
		if _, hasError := msg["error"]; hasError {
			errMsg := jsonrpcErrorMessage(msg["error"])
			c.deliverPending(idAny, &rpcResult{Msg: msg, Err: errors.New(errMsg)})
			return
		}
	}
	// 对端请求（Python L480-482/L583-585: handle 后 continue）
	if isPeerJSONRPCRequest(msg) {
		go c.handlePeerRequest(msg)
		return
	}
	// 通知：session/update 文本投递（Python L577-581: chunk 非空才 append）
	if text := extractSessionUpdateText(msg); text != "" {
		select {
		case c.updatesCh <- text:
		default:
			// 缓冲满丢弃新项防阻塞 reader（Python 单循环消费无此问题）
			logger.Debug(logComponent).Str("text", text).
				Msg("[AcpStdioClient] updates channel full, dropping session/update")
		}
	}
}

// deliverPending 向等待者非阻塞投递结果。
func (c *AcpStdioClient) deliverPending(idAny any, res *rpcResult) {
	rid, ok := toInt(idAny)
	if !ok {
		return
	}
	c.pendingMu.Lock()
	ch := c.pending[int64(rid)]
	delete(c.pending, int64(rid))
	c.pendingMu.Unlock()
	if ch != nil {
		select {
		case ch <- res:
		default:
		}
	}
}

// handlePeerRequest 处理对端反向请求（fs 读写/权限），回写响应使其不阻塞。
// Python: _handle_peer_request (L256-378)
func (c *AcpStdioClient) handlePeerRequest(msg map[string]any) {
	reqID := msg["id"]
	method, _ := msg["method"].(string)
	method = strings.TrimSpace(method)

	switch method {
	case "session/request_permission":
		// 对齐 Gateway 权限 outcome 解析（interrupt_helpers permiss flow）
		if autoApprovePermissions() {
			_ = c.writeResponse(reqID, map[string]any{
				"outcome": map[string]any{"outcome": "selected", "optionId": "allow-once"},
			}, nil)
			logger.Info(logComponent).Str("id", strOf(reqID)).
				Msg("[AcpStdioClient] replied to session/request_permission (allow-once)")
		} else {
			_ = c.writeResponse(reqID, map[string]any{
				"outcome": map[string]any{"outcome": "cancelled"},
			}, nil)
			logger.Info(logComponent).Str("id", strOf(reqID)).
				Msg("[AcpStdioClient] replied to session/request_permission (cancelled)")
		}
		return
	case "fs/read_text_file":
		params, _ := msg["params"].(map[string]any)
		if params == nil {
			_ = c.writeResponse(reqID, nil, map[string]any{"code": -32602, "message": "invalid fs/read_text_file params"})
			return
		}
		root := resolvedSessionRoot(c.cwd)
		absPath := resolvedPathInsideRoot(root, params["path"])
		if absPath == "" {
			_ = c.writeResponse(reqID, nil, map[string]any{"code": -32001, "message": "path denied or invalid"})
			return
		}
		data, err := os.ReadFile(absPath)
		if err != nil {
			if os.IsNotExist(err) {
				_ = c.writeResponse(reqID, nil, map[string]any{"code": -32002, "message": "ENOENT"})
			} else {
				_ = c.writeResponse(reqID, nil, map[string]any{"code": -32000, "message": err.Error()})
			}
			return
		}
		chunk := sliceFileLines(string(data), params["line"], params["limit"])
		_ = c.writeResponse(reqID, map[string]any{"content": chunk}, nil)
		if debugChat() {
			logger.Info(logComponent).Str("path", absPath).Int("bytes", len(chunk)).
				Msg("[AcpStdioClient] fs/read_text_file ok")
		}
		return
	case "fs/write_text_file":
		params, _ := msg["params"].(map[string]any)
		if params == nil {
			_ = c.writeResponse(reqID, nil, map[string]any{"code": -32602, "message": "invalid fs/write_text_file params"})
			return
		}
		root := resolvedSessionRoot(c.cwd)
		absPath := resolvedPathInsideRoot(root, params["path"])
		if absPath == "" {
			_ = c.writeResponse(reqID, nil, map[string]any{"code": -32001, "message": "path denied or invalid"})
			return
		}
		wbody := ""
		if wc := params["content"]; wc != nil {
			wbody = strOf(wc)
		}
		if err := os.MkdirAll(filepath.Dir(absPath), 0o755); err != nil {
			_ = c.writeResponse(reqID, nil, map[string]any{"code": -32000, "message": err.Error()})
			return
		}
		if err := os.WriteFile(absPath, []byte(wbody), 0o644); err != nil {
			_ = c.writeResponse(reqID, nil, map[string]any{"code": -32000, "message": err.Error()})
			return
		}
		_ = c.writeResponse(reqID, nil, nil)
		if debugChat() {
			logger.Info(logComponent).Str("path", absPath).
				Msg("[AcpStdioClient] fs/write_text_file ok")
		}
		return
	default:
		// 最小桩使 agent 能继续；调用方可按 Agent 收紧
		logger.Warn(logComponent).Str("method", method).Str("id", strOf(reqID)).
			Msg("[AcpStdioClient] unhandled peer request — error reply")
		_ = c.writeResponse(reqID, nil, map[string]any{
			"code": -32601, "message": "stdio ACP client: unsupported method " + method,
		})
	}
}

// writeRequest 写 JSON-RPC 请求行。
func (c *AcpStdioClient) writeRequest(rid int64, method string, params map[string]any) error {
	payload := map[string]any{
		"jsonrpc": "2.0",
		"id":      rid,
		"method":  method,
		"params":  params,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return c.writeLine(body)
}

// writeResponse 写 JSON-RPC 响应行（result/error 二选一）。
// Python: _send_json_rpc_response (L240-254)
func (c *AcpStdioClient) writeResponse(reqID any, result any, rpcErr map[string]any) error {
	body := map[string]any{"jsonrpc": "2.0", "id": reqID}
	if rpcErr != nil {
		body["error"] = rpcErr
	} else {
		body["result"] = result
	}
	line, err := json.Marshal(body)
	if err != nil {
		return err
	}
	return c.writeLine(line)
}

// writeLine 串行写一行 + 换行符。
func (c *AcpStdioClient) writeLine(line []byte) error {
	c.mu.Lock()
	stdin := c.stdin
	c.mu.Unlock()
	if stdin == nil {
		return errors.New("AcpStdioClient stdin closed")
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	if _, err := stdin.Write(append(line, '\n')); err != nil {
		return err
	}
	return nil
}

// allocID 分配自增 RPC ID。
// Python: _next_id (L391-394)
func (c *AcpStdioClient) allocID() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	rid := c.nextID
	c.nextID++
	return rid + 1
}

// registerPending 登记等待者（缓冲 1）。重复 ID 视为逻辑错误返回错误。
func (c *AcpStdioClient) registerPending(rid int64) (chan *rpcResult, error) {
	ch := make(chan *rpcResult, 1)
	c.pendingMu.Lock()
	defer c.pendingMu.Unlock()
	if _, dup := c.pending[rid]; dup {
		return nil, fmt.Errorf("duplicate rpc id %d", rid)
	}
	c.pending[rid] = ch
	return ch, nil
}

// removePending 移除等待者。
func (c *AcpStdioClient) removePending(rid int64) {
	c.pendingMu.Lock()
	delete(c.pending, rid)
	c.pendingMu.Unlock()
}

// failAllPending 向全部等待者投递错误。
func (c *AcpStdioClient) failAllPending(err error) {
	c.pendingMu.Lock()
	pending := c.pending
	c.pending = make(map[int64]chan *rpcResult)
	c.pendingMu.Unlock()
	for _, ch := range pending {
		select {
		case ch <- &rpcResult{Err: err}:
		default:
		}
	}
}

// updates 返回 session/update 通知通道（构造后不可变）。
func (c *AcpStdioClient) updates() chan string {
	return c.updatesCh
}

// resolvedSessionRoot 会话根目录（realpath(abspath(expanduser(cwd)))，空则 getcwd）。
// Python: _resolved_session_root (L135-138)
func resolvedSessionRoot(clientCwd string) string {
	dir := strings.TrimSpace(clientCwd)
	if dir == "" {
		dir, _ = os.Getwd()
	}
	if expanded, err := expandUser(dir); err == nil {
		dir = expanded
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved
	}
	return abs
}

// resolvedPathInsideRoot 判定路径在根内（越界返回空串）。
// Python: _resolved_path_inside_root (L141-152)
func resolvedPathInsideRoot(sessionRoot string, pathIn any) string {
	s, ok := pathIn.(string)
	if !ok || strings.TrimSpace(s) == "" {
		return ""
	}
	if expanded, err := expandUser(strings.TrimSpace(s)); err == nil {
		s = expanded
	}
	abs, err := filepath.Abs(s)
	if err != nil {
		return ""
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// Python realpath 对不存在路径也返回归一结果；Go EvalSymlinks 失败时用 Abs 近似
		resolved = abs
	}
	if resolved == sessionRoot {
		return resolved
	}
	if strings.HasPrefix(resolved, sessionRoot+string(os.PathSeparator)) {
		return resolved
	}
	return ""
}

// sliceFileLines 行切片（line<1 钳 1；limit>0 生效否则到尾）。
// Python: _slice_file_lines (L155-168)——splitlines(True) 保留换行符。
func sliceFileLines(text string, lineRaw, limitRaw any) string {
	// splitlines(True) 语义：保留行界符（含 \n 与 \r\n）
	lines := splitLinesKeepEnds(text)
	lineNo := 1
	if v, ok := toInt(lineRaw); ok && v > 1 {
		lineNo = v
	}
	start := lineNo - 1
	if start < 0 {
		start = 0
	}
	if start > len(lines) {
		start = len(lines)
	}
	if lim, ok := toInt(limitRaw); ok && lim > 0 {
		end := start + lim
		if end > len(lines) {
			end = len(lines)
		}
		return strings.Join(lines[start:end], "")
	}
	return strings.Join(lines[start:], "")
}

// splitLinesKeepEnds Python splitlines(True) 等价（保留行界符）。
func splitLinesKeepEnds(text string) []string {
	if text == "" {
		return nil
	}
	var lines []string
	start := 0
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			lines = append(lines, text[start:i+1])
			start = i + 1
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// expandUser 展开 ~ 前缀。
func expandUser(p string) (string, error) {
	if !strings.HasPrefix(p, "~") {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p, err
	}
	return filepath.Join(home, strings.TrimPrefix(p, "~")), nil
}

// toInt 宽松整数转换（float64/字符串）。
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case int64:
		return int(n), true
	case string:
		if i, err := strconv.Atoi(strings.TrimSpace(n)); err == nil {
			return i, true
		}
	}
	return 0, false
}

// firstNonNil 返回首个非 nil 值。
func firstNonNil(vals ...any) any {
	for _, v := range vals {
		if v != nil {
			return v
		}
	}
	return nil
}

// resultTextFallback 会话结果文本回退。
// Python: _result_text_fallback (L663-670)
func resultTextFallback(result any) string {
	if result == nil {
		return ""
	}
	if m, ok := result.(map[string]any); ok {
		// 部分代理仅返回 stopReason
		sr := firstNonNil(m["stopReason"], m["stop_reason"])
		if sr != nil {
			return strOf(sr)
		}
		b, _ := json.Marshal(m)
		return string(b)
	}
	return strOf(result)
}

// defaultChatTimeout 默认聊天超时（env ACP_CHAT_TIMEOUT，默认 600s）。
// Python: _DEFAULT_CHAT_TIMEOUT (L24)
func defaultChatTimeout() time.Duration {
	if raw := os.Getenv("ACP_CHAT_TIMEOUT"); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	}
	return 600 * time.Second
}

// debugChat 是否输出聊天调试日志（env ACP_CHAT_DEBUG）。
// Python: _DEBUG_CHAT (L25)
func debugChat() bool {
	raw := strings.TrimSpace(os.Getenv("ACP_CHAT_DEBUG"))
	return strings.ToLower(raw) == "1" || strings.ToLower(raw) == "true" || strings.ToLower(raw) == "yes"
}

// autoApprovePermissions 是否自动应答权限请求（env ACP_AUTO_APPROVE_PERMISSIONS，默认 true）。
// 启用（默认）使 headless Codex 不停滞。
// Python: _AUTO_APPROVE (L41-45)
func autoApprovePermissions() bool {
	raw := strings.TrimSpace(os.Getenv("ACP_AUTO_APPROVE_PERMISSIONS"))
	if raw == "" {
		return true
	}
	lo := strings.ToLower(raw)
	return lo == "1" || lo == "true" || lo == "yes"
}

// killProcessGroup 是否按进程组发信号（env ACP_KILL_PROCESS_GROUP，默认 true，仅非 Windows）。
// Python: _KILL_PG (L34-38)
func killProcessGroup() bool {
	raw := strings.TrimSpace(os.Getenv("ACP_KILL_PROCESS_GROUP"))
	enabled := raw == "" || strings.EqualFold(raw, "1") || strings.EqualFold(raw, "true") || strings.EqualFold(raw, "yes")
	return enabled && runtime.GOOS != "windows"
}

// chanClosed 通道是否已关闭（非阻塞探测）。
func chanClosed(ch chan struct{}) bool {
	if ch == nil {
		return false
	}
	select {
	case <-ch:
		return true
	default:
		return false
	}
}

// waitChan 有界等待通道关闭。
// Python: await asyncio.wait_for(proc.wait(), timeout=...)——
// Go 侧 Wait 由 Connect 的后台 goroutine 独占，此处只等 exitCh。
func waitChan(ch chan struct{}, d time.Duration) {
	if ch == nil {
		return
	}
	select {
	case <-ch:
	case <-time.After(d):
	}
}

// closeStdinWaitS stdin 关闭等待上限（env ACP_CLOSE_STDIN_WAIT_S，默认 4s）。
// Python: _CLOSE_STDIN_WAIT_S (L29)
func closeStdinWaitS() time.Duration { return envSeconds("ACP_CLOSE_STDIN_WAIT_S", 4) }

// closeTermWaitS TERM 段等待上限（env ACP_CLOSE_TERM_WAIT_S，默认 5s）。
// Python: _CLOSE_TERM_WAIT_S (L30)
func closeTermWaitS() time.Duration { return envSeconds("ACP_CLOSE_TERM_WAIT_S", 5) }

// closeKillWaitS KILL 段等待上限（env ACP_CLOSE_KILL_WAIT_S，默认 8s）。
// Python: _CLOSE_KILL_WAIT_S (L31)
func closeKillWaitS() time.Duration { return envSeconds("ACP_CLOSE_KILL_WAIT_S", 8) }

// closeStderrJoinS stderr join 上限（env ACP_CLOSE_STDERR_JOIN_S，默认 3s）。
// Python: _CLOSE_STDERR_JOIN_S (L32)
func closeStderrJoinS() time.Duration { return envSeconds("ACP_CLOSE_STDERR_JOIN_S", 3) }

// envSeconds env 秒数读取（解析失败回退默认）。
func envSeconds(name string, def float64) time.Duration {
	if raw := os.Getenv(name); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	}
	return time.Duration(def * float64(time.Second))
}
