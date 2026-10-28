// ACP 输出工具（10.6.24d）：AgentServer 向 IDE 发送请求。
// 对齐 Python jiuwenswarm/agents/harness/common/tools/acp_output_tools.py（585 行）：
// AcpOutputManager 单例管理 pending JSON-RPC 请求，经 E2A server_push 推给 IDE，
// acp.tool_response 回流完成（handleACPToolResponse，11.25 接线）。

package tools

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AcpOutputResult 单个输出请求的完成结果（替代 asyncio.Future 的单 chan 双态）。
// 导出原因：AcpOutputRequest.Done 为导出字段，跨包测试（server 包回填测试）
// 需构造该 chan。
type AcpOutputResult struct {
	// Response 完成响应（Err 为 nil 时有效）
	Response map[string]any
	// Err 失败原因
	Err error
}

// AcpOutputRequest 待完成的 ACP JSON-RPC 输出请求。
// Python: AcpOutputRequest dataclass (acp_output_tools.py L45-51)
type AcpOutputRequest struct {
	// JsonrpcID JSON-RPC 标识
	JsonrpcID string
	// Method 请求方法
	Method string
	// Params 请求参数
	Params map[string]any
	// Done 完成通道（缓冲 1，替代 asyncio.Future）
	Done chan AcpOutputResult
	// RequestID 请求标识
	RequestID string
}

// AcpOutputManager ACP 输出请求管理器（单例）。
// Python: AcpOutputManager (acp_output_tools.py L54-241)
type AcpOutputManager struct {
	// mu 保护 pending/counter/callback
	mu sync.Mutex
	// pending 待完成请求（jsonrpc_id → 请求）
	pending map[string]*AcpOutputRequest
	// jsonrpcCounter 自增计数器
	jsonrpcCounter int64
	// sendPushFunc E2A 推送函数（agent_server 启动注入）
	sendPushFunc func(ctx context.Context, pushMsg map[string]any) error
}

// acpRequestConfig SendJsonrpcRequest 可选项聚合。
type acpRequestConfig struct {
	ChannelID string
	SessionID string
	Timeout   time.Duration
}

// AcpOutputError ACP 输出请求的业务错误。
// Python: AcpOutputError (acp_output_tools.py L244-250)
type AcpOutputError struct {
	// Method 请求方法
	Method string
	// Code 错误码
	Code int
	// Message 错误消息
	Message string
	// Data 附加数据
	Data any
}

// AcpOutputTimeoutError ACP 输出请求超时（sentinel，可判别）。
type AcpOutputTimeoutError struct {
	// Method 请求方法
	Method string
	// Timeout 超时时长
	Timeout time.Duration
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// acpRequestTimeoutSeconds 默认输出请求超时。
	// Python: _ACP_REQUEST_TIMEOUT_SECONDS (L36)
	acpRequestTimeoutSeconds = 30 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// acpOutputManagerOnce 单例构造 once。
	acpOutputManagerOnce sync.Once
	// acpOutputManagerInstance 单例实例。
	acpOutputManagerInstance *AcpOutputManager

	// acpWaitForExitTimeout wait_for_terminal_exit 超时
	// （env ACP_WAIT_FOR_EXIT_TIMEOUT_SECONDS，默认 30s，解析失败回退）。
	// Python: _ACP_WAIT_FOR_EXIT_TIMEOUT_SECONDS (L37-42)
	acpWaitForExitTimeout = acpEnvDuration("ACP_WAIT_FOR_EXIT_TIMEOUT_SECONDS", 30*time.Second)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetAcpOutputManager 返回 ACP 输出管理器单例。
// Python: get_acp_output_manager (L240-241)
func GetAcpOutputManager() *AcpOutputManager {
	acpOutputManagerOnce.Do(func() {
		acpOutputManagerInstance = &AcpOutputManager{
			pending: make(map[string]*AcpOutputRequest),
		}
	})
	return acpOutputManagerInstance
}

// SetSendPushFunc 注入 E2A 推送函数（agent_server 启动调用，24b 先例）。
// Python: set_send_push_callback (L71-72)
func (m *AcpOutputManager) SetSendPushFunc(fn func(ctx context.Context, msg map[string]any) error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.sendPushFunc = fn
}

// ResetState 重置运行态（测试与受控生命周期清理）。
// Python: reset_state (L74-80)
func (m *AcpOutputManager) ResetState() {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending = make(map[string]*AcpOutputRequest)
	m.jsonrpcCounter = 0
}

// AddPendingRequest 显式登记待完成请求（测试钩子）。
// Python: add_pending_request (L82-87)
func (m *AcpOutputManager) AddPendingRequest(request *AcpOutputRequest) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pending[request.JsonrpcID] = request
}

// CompleteJsonrpcResponse 完成一个待定的 ACP JSON-RPC 请求。
// Python: complete_jsonrpc_response (L89-117)
func (m *AcpOutputManager) CompleteJsonrpcResponse(jsonrpcID any, response map[string]any) bool {
	key := normalizeJsonrpcID(jsonrpcID)
	if key == "" {
		return false
	}
	m.mu.Lock()
	pending := m.pending[key]
	delete(m.pending, key)
	pendingKeys := m.pendingKeysLocked()
	m.mu.Unlock()
	if pending == nil {
		logger.Warn(logComponent).Str("jsonrpc_id", key).
			Strs("pending_keys", pendingKeys).
			Msg("[AcpOutput] completion dropped")
		return false
	}
	select {
	case pending.Done <- AcpOutputResult{Response: copyAcpMap(response)}:
		logger.Info(logComponent).Str("jsonrpc_id", key).Str("method", pending.Method).
			Msg("[AcpOutput] request completed")
		return true
	default:
		// Python: future.done() → False（Go 缓冲 1 chan 满即等价已投递）
		return false
	}
}

// FailJsonrpcResponse 使一个待定请求以错误完成。
// Python: fail_jsonrpc_response (L119-134)
func (m *AcpOutputManager) FailJsonrpcResponse(jsonrpcID any, exc error) bool {
	key := normalizeJsonrpcID(jsonrpcID)
	if key == "" {
		return false
	}
	m.mu.Lock()
	pending := m.pending[key]
	delete(m.pending, key)
	m.mu.Unlock()
	if pending == nil {
		return false
	}
	select {
	case pending.Done <- AcpOutputResult{Err: exc}:
		return true
	default:
		return false
	}
}

// SendJsonrpcRequest 发送 ACP 输出请求（E2A server_push → IDE → 回流）。
// Python: send_jsonrpc_request (L136-237)
func (m *AcpOutputManager) SendJsonrpcRequest(ctx context.Context, method string,
	params map[string]any, opts ...AcpRequestOption) (map[string]any, error) {
	cfg := acpRequestConfig{
		ChannelID: "acp",
		Timeout:   acpRequestTimeoutSeconds,
	}
	for _, o := range opts {
		o(&cfg)
	}
	logger.Info(logComponent).Str("method", method).Strs("params_keys", mapAcpKeys(params)).
		Msg("[AcpOutput] send_jsonrpc_request called")

	m.mu.Lock()
	if m.sendPushFunc == nil {
		m.mu.Unlock()
		logger.Error(logComponent).Msg("[AcpOutput] send_push callback is None!")
		return nil, errors.New("ACP output send_push callback not set")
	}
	pushFn := m.sendPushFunc
	m.jsonrpcCounter++
	jsonrpcID := fmt.Sprintf("%d", m.jsonrpcCounter)
	requestID := fmt.Sprintf("acp_out_%s", randomAcpHex12())
	done := make(chan AcpOutputResult, 1)
	acpReq := &AcpOutputRequest{
		JsonrpcID: jsonrpcID,
		Method:    method,
		Params:    params,
		Done:      done,
		RequestID: requestID,
	}
	m.pending[jsonrpcID] = acpReq
	pendingKeys := m.pendingKeysLocked()
	m.mu.Unlock()
	logger.Info(logComponent).Str("jsonrpc_id", jsonrpcID).
		Strs("pending_keys", pendingKeys).
		Msg("[AcpOutput] added to pending")

	// E2AResponse 构造（Python L175-210）
	ts := e2a.UTCNowISO()
	resp := e2a.NewE2AResponse()
	resp.ResponseID = fmt.Sprintf("acp_out_resp_%s", randomAcpHex12())
	resp.RequestID = requestID
	resp.Sequence = 0
	resp.IsFinal = false
	resp.ResponseKind = e2a.E2AResponseKindACPOutputRequest
	resp.Timestamp = ts
	resp.Provenance = e2a.E2AProvenance{
		SourceProtocol: e2a.E2ASourceProtocolE2A,
		Converter:      "jiuwenswarm.agents.harness.common.tools.acp_output_tools:send_jsonrpc_request",
		ConvertedAt:    ts,
		Details:        map[string]any{"kind": "acp_output_request", "acp_method": method},
	}
	bodyParams := params
	if cfg.SessionID != "" {
		merged := make(map[string]any, len(params)+1)
		for k, v := range params {
			merged[k] = v
		}
		merged["sessionId"] = cfg.SessionID
		bodyParams = merged
	}
	resp.Body = map[string]any{
		"jsonrpc": "2.0",
		"id":      jsonrpcID,
		"method":  method,
		"params":  bodyParams,
	}
	resp.JSONRPCID = jsonrpcID
	resp.SessionID = cfg.SessionID
	resp.Channel = cfg.ChannelID
	resp.Metadata = map[string]any{e2a.E2AWireServerPushKey: true}

	pushMsg := resp.ToMap()
	// Python L207-210: to_dict 后处理双 key 兼容
	if _, exists := pushMsg["channel_id"]; !exists {
		if ch, ok := pushMsg["channel"]; ok {
			pushMsg["channel_id"] = ch
		}
	}
	if _, exists := pushMsg["payload"]; !exists {
		if body, ok := pushMsg["body"]; ok {
			pushMsg["payload"] = body
		}
	}

	if err := pushFn(ctx, pushMsg); err != nil {
		m.mu.Lock()
		delete(m.pending, jsonrpcID)
		m.mu.Unlock()
		return nil, fmt.Errorf("Failed to send ACP output request: %w", err)
	}
	logger.Info(logComponent).Str("jsonrpc_id", jsonrpcID).Str("method", method).
		Str("request_id", requestID).Msg("[AcpOutput] sent E2A request")

	select {
	case res := <-done:
		if res.Err != nil {
			return nil, res.Err
		}
		return res.Response, nil
	case <-ctx.Done():
		m.mu.Lock()
		delete(m.pending, jsonrpcID)
		m.mu.Unlock()
		return nil, ctx.Err()
	case <-time.After(cfg.Timeout):
		m.mu.Lock()
		delete(m.pending, jsonrpcID)
		m.mu.Unlock()
		logger.Warn(logComponent).Str("jsonrpc_id", jsonrpcID).Str("method", method).
			Float64("timeout", cfg.Timeout.Seconds()).
			Msg("[AcpOutput] request timed out")
		return nil, &AcpOutputTimeoutError{Method: method, Timeout: cfg.Timeout}
	}
}

// WithAcpChannelID 设置渠道。
func WithAcpChannelID(id string) AcpRequestOption {
	return func(c *acpRequestConfig) { c.ChannelID = id }
}

// WithAcpSessionID 设置会话。
func WithAcpSessionID(id string) AcpRequestOption {
	return func(c *acpRequestConfig) { c.SessionID = id }
}

// WithAcpTimeout 设置超时。
func WithAcpTimeout(d time.Duration) AcpRequestOption {
	return func(c *acpRequestConfig) { c.Timeout = d }
}

// GetAcpOutputTools 返回 ACP 输出工具列表（六闭包绑定 sessionID）。
// Python: get_tools (L449-584)——卡名/描述/参数逐字复制。
// requestID 签名携带但六闭包仅绑定 session_id（Python L467-487）。
func GetAcpOutputTools(sessionID, requestID string) []tool.Tool {
	_ = requestID
	mgr := GetAcpOutputManager()

	// read_text_file（Python L490-510）
	readCard := tool.NewToolCard(
		"read_text_file",
		"[ACP] 通过 IDE 读取用户本地文件内容。这是唯一可用的文件读取工具。必须调用此工具才能读取文件。",
		[]*schema.Param{
			{Name: "path", Description: "要读取的文件路径", Type: schema.ParamTypeString, Required: true},
			schema.NewIntegerParam("offset", "起始行号（可选，从 1 开始）", false),
			schema.NewIntegerParam("limit", "读取行数（可选）", false),
		},
		nil,
	)
	readFn, _ := tool.NewMapFunction(readCard, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		path, _ := inputs["path"].(string)
		result, err := readTextFile(ctx, mgr, path, inputs["offset"], inputs["limit"], sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}, nil)

	// write_text_file（Python L511-527）
	writeCard := tool.NewToolCard(
		"write_text_file",
		"[ACP] 通过 IDE 写入文件到用户本地。这是唯一可用的文件写入工具。必须调用此工具才能写入文件。",
		[]*schema.Param{
			{Name: "path", Description: "要写入的文件路径", Type: schema.ParamTypeString, Required: true},
			{Name: "content", Description: "要写入的文件内容", Type: schema.ParamTypeString, Required: true},
		},
		nil,
	)
	writeFn, _ := tool.NewMapFunction(writeCard, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		path, _ := inputs["path"].(string)
		content, _ := inputs["content"].(string)
		result, err := writeTextFile(ctx, mgr, path, content, sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}, nil)

	// create_terminal（Python L528-544）
	createCard := tool.NewToolCard(
		"create_terminal",
		"[ACP] 通过 IDE 创建终端并执行命令。这是唯一可用的命令执行工具。必须调用此工具才能执行命令。",
		[]*schema.Param{
			{Name: "cmd", Description: "要执行的命令", Type: schema.ParamTypeString, Required: true},
			{Name: "cwd", Description: "工作目录（可选）", Type: schema.ParamTypeString},
		},
		nil,
	)
	createFn, _ := tool.NewMapFunction(createCard, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		cmd, _ := inputs["cmd"].(string)
		var cwd any
		if c, ok := inputs["cwd"]; ok {
			cwd = c
		}
		result, err := createTerminal(ctx, mgr, cmd, cwd, sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}, nil)

	// read_terminal_output（Python L545-556）
	readTermCard := tool.NewToolCard(
		"read_terminal_output",
		"读取终端输出内容。",
		[]*schema.Param{
			{Name: "terminal_id", Description: "终端 ID", Type: schema.ParamTypeString, Required: true},
		},
		nil,
	)
	readTermFn, _ := tool.NewMapFunction(readTermCard, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		terminalID, _ := inputs["terminal_id"].(string)
		result, err := readTerminalOutput(ctx, mgr, terminalID, sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}, nil)

	// wait_for_terminal_exit（Python L557-571）
	waitCard := tool.NewToolCard(
		"wait_for_terminal_exit",
		"等待终端命令执行完成。若返回 timedOut=true / running=true / shouldRetry=true，"+
			"表示任务仍在执行中，应继续使用同一个 terminal_id 再次调用本工具轮询。",
		[]*schema.Param{
			{Name: "terminal_id", Description: "终端 ID", Type: schema.ParamTypeString, Required: true},
		},
		nil,
	)
	waitFn, _ := tool.NewMapFunction(waitCard, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		terminalID, _ := inputs["terminal_id"].(string)
		result, err := waitForTerminalExit(ctx, mgr, terminalID, sessionID, acpWaitForExitTimeout)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}, nil)

	// release_terminal（Python L572-583）
	releaseCard := tool.NewToolCard(
		"release_terminal",
		"释放终端资源（命令完成后必须调用）。",
		[]*schema.Param{
			{Name: "terminal_id", Description: "终端 ID", Type: schema.ParamTypeString, Required: true},
		},
		nil,
	)
	releaseFn, _ := tool.NewMapFunction(releaseCard, func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		terminalID, _ := inputs["terminal_id"].(string)
		result, err := releaseTerminal(ctx, mgr, terminalID, sessionID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"result": result}, nil
	}, nil)

	return []tool.Tool{readFn, writeFn, createFn, readTermFn, waitFn, releaseFn}
}

// Error 实现 error 接口。
// Python: super().__init__(f"[{method}] error {code}: {message}")
func (e *AcpOutputError) Error() string {
	return fmt.Sprintf("[%s] error %d: %s", e.Method, e.Code, e.Message)
}

// Error 实现 error 接口（超时 sentinel）。
func (e *AcpOutputTimeoutError) Error() string {
	return fmt.Sprintf("acp output request timed out after %.1fs: %s", e.Timeout.Seconds(), e.Method)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// AcpRequestOption SendJsonrpcRequest 选项。
type AcpRequestOption func(*acpRequestConfig)

// readTextFile 经 IDE 读取文件。
// Python: read_text_file (L258-286)
func readTextFile(ctx context.Context, mgr *AcpOutputManager,
	path string, offset, limit any, sessionID string) (map[string]any, error) {
	params := map[string]any{"path": path}
	if offset != nil {
		params["offset"] = offset
	}
	if limit != nil {
		params["limit"] = limit
	}
	return sendAcpToolRequest(ctx, mgr, "fs/read_text_file", params, sessionID, 0)
}

// writeTextFile 经 IDE 写文件。
// Python: write_text_file (L289-312)
func writeTextFile(ctx context.Context, mgr *AcpOutputManager,
	path, content, sessionID string) (map[string]any, error) {
	params := map[string]any{"path": path, "content": content}
	return sendAcpToolRequest(ctx, mgr, "fs/write_text_file", params, sessionID, 0)
}

// createTerminal 经 IDE 创建终端并执行命令。
// Python: create_terminal (L315-340)
func createTerminal(ctx context.Context, mgr *AcpOutputManager,
	cmd string, cwd any, sessionID string) (map[string]any, error) {
	params := map[string]any{"command": cmd}
	if cwd != nil {
		if s, ok := cwd.(string); ok {
			params["cwd"] = s
		}
	}
	return sendAcpToolRequest(ctx, mgr, "terminal/create", params, sessionID, 0)
}

// readTerminalOutput 读取终端输出。
// Python: read_terminal_output (L343-365)
func readTerminalOutput(ctx context.Context, mgr *AcpOutputManager,
	terminalID, sessionID string) (map[string]any, error) {
	params := map[string]any{"terminalId": terminalID}
	return sendAcpToolRequest(ctx, mgr, "terminal/output", params, sessionID, 0)
}

// waitForTerminalExit 等待终端退出（软超时归一化）。
// Python: wait_for_terminal_exit (L368-416)
func waitForTerminalExit(ctx context.Context, mgr *AcpOutputManager,
	terminalID, sessionID string, timeout time.Duration) (map[string]any, error) {
	params := map[string]any{"terminalId": terminalID}
	resp, err := sendAcpToolRequest(ctx, mgr, "terminal/wait_for_exit",
		params, sessionID, timeout)
	if err != nil {
		var timeErr *AcpOutputTimeoutError
		if errors.As(err, &timeErr) {
			logger.Info(logComponent).Str("terminal_id", terminalID).
				Float64("timeout", timeout.Seconds()).
				Msg("[AcpOutput] wait_for_terminal_exit soft-timeout")
			return map[string]any{
				"exitCode": nil, "signal": nil,
				"timedOut": true, "running": true, "shouldRetry": true,
			}, nil
		}
		return nil, err
	}
	// 归一化使调用方可确定性决定是否继续轮询（Python L407-416）
	result := copyAcpMap(resp)
	if _, exists := result["timedOut"]; !exists {
		result["timedOut"] = false
	}
	if _, exists := result["running"]; !exists {
		if result["exitCode"] != nil || result["signal"] != nil {
			result["running"] = false
		}
	}
	result["shouldRetry"] = truthyAcp(result["timedOut"]) || truthyAcp(result["running"])
	return result, nil
}

// releaseTerminal 释放终端资源。
// Python: release_terminal (L419-441)
func releaseTerminal(ctx context.Context, mgr *AcpOutputManager,
	terminalID, sessionID string) (map[string]any, error) {
	params := map[string]any{"terminalId": terminalID}
	return sendAcpToolRequest(ctx, mgr, "terminal/release", params, sessionID, 0)
}

// sendAcpToolRequest 统一发送 + error 检查（六工具共用尾部逻辑）。
// timeout=0 表示默认（acpRequestTimeoutSeconds）。
func sendAcpToolRequest(ctx context.Context, mgr *AcpOutputManager,
	method string, params map[string]any, sessionID string, timeout time.Duration) (map[string]any, error) {
	opts := []AcpRequestOption{}
	if sessionID != "" {
		opts = append(opts, WithAcpSessionID(sessionID))
	}
	if timeout > 0 && timeout != acpRequestTimeoutSeconds {
		opts = append(opts, WithAcpTimeout(timeout))
	}
	response, err := mgr.SendJsonrpcRequest(ctx, method, params, opts...)
	if err != nil {
		return nil, err
	}
	if errObj, hasErr := response["error"]; hasErr && errObj != nil {
		em, _ := errObj.(map[string]any)
		code := -32000
		if c, ok := em["code"].(float64); ok {
			code = int(c)
		}
		message := "Unknown error"
		if ms, ok := em["message"].(string); ok && ms != "" {
			message = ms
		}
		return nil, &AcpOutputError{Method: method, Code: code, Message: message, Data: em["data"]}
	}
	if result, ok := response["result"].(map[string]any); ok {
		return result, nil
	}
	return map[string]any{}, nil
}

// normalizeJsonrpcID jsonrpc ID 归一化（string/number → trim 字符串）。
// Python: str(jsonrpc_id or "").strip() (L94-95)
func normalizeJsonrpcID(id any) string {
	switch v := id.(type) {
	case string:
		return strings.TrimSpace(v)
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64)
	case int:
		return strconv.Itoa(v)
	case int64:
		return strconv.FormatInt(v, 10)
	case json.Number:
		return strings.TrimSpace(v.String())
	default:
		return ""
	}
}

// randomAcpHex12 12 位随机 hex（uuid4().hex[:12] 等价）。
func randomAcpHex12() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// pendingKeysLocked 待定键快照（调用方持锁）。
func (m *AcpOutputManager) pendingKeysLocked() []string {
	keys := make([]string, 0, len(m.pending))
	for k := range m.pending {
		keys = append(keys, k)
	}
	return keys
}

// pendingSnapshot 测试用 pending 快照。
func (m *AcpOutputManager) pendingSnapshot() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pendingKeysLocked()
}

// mapAcpKeys map 键快照。
func mapAcpKeys(m map[string]any) []string {
	if m == nil {
		return nil
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// copyAcpMap 浅拷贝（nil → 空 map；对齐 Python dict(x or {})）。
func copyAcpMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// truthyAcp Python bool() 真值语义。
func truthyAcp(v any) bool {
	switch x := v.(type) {
	case nil:
		return false
	case bool:
		return x
	case string:
		return x != ""
	case float64:
		return x != 0
	case int:
		return x != 0
	default:
		return true
	}
}

// acpEnvDuration env 秒数读取（解析失败回退默认）。
func acpEnvDuration(name string, def time.Duration) time.Duration {
	if raw := os.Getenv(name); raw != "" {
		if v, err := strconv.ParseFloat(raw, 64); err == nil && v > 0 {
			return time.Duration(v * float64(time.Second))
		}
	}
	return def
}
