package client

import (
	"context"
	"errors"
	"math"
	"math/rand"
	"os"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// reconnectState 每实例的重连状态（锁、退避计数、冷却截止、代际号）。
// 不放进客户端结构体本身：两个客户端类型都要用，且 McpClient 接口
// 不暴露这些内部字段。用 sync.Map 按客户端指针挂载，等价上游的
// setattr(client, "_reconnect_lock", ...) 动态属性。
// Python: _get_reconnect_lock / _record_reconnect_failure 里的
// “setattr(client, "_reconnect_*", ...)“（reconnect.py:214-245）
type reconnectState struct {
	// mu 重连互斥锁：同一实例的并发调用不重复重建传输
	mu sync.Mutex
	// stateMu 保护下面的计数与代际字段。
	// 与 mu 分开是必须的：reconnectTransport 持 mu 期间会调用 recordFailure/
	// recordSuccess，共用一个锁会自死锁。
	stateMu sync.Mutex
	// failureCount 连续失败次数
	failureCount int
	// cooldownUntil 冷却截止时间点（零值表示无冷却）
	cooldownUntil time.Time
	// lazyReconnect 是否因传输失败被动断开（区分用户主动断开）
	lazyReconnect bool
	// generation 传输代际；每次成功重连递增
	generation int64
}

// retryableMarker 可重试错误标记。
// skipForRuntimeError 为真时对 RuntimeError 不适用——避免把客户端自己的
// "未连接"守卫误判成传输错误。
// Python: _RETRYABLE_MARKER_DEFS (reconnect.py:38-52)
type retryableMarker struct {
	// pattern 小写子串
	pattern string
	// skipForRuntimeError 对 RuntimeError 跳过
	skipForRuntimeError bool
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// reconnectBaseDelay 退避基数（秒）
	// Python: _RECONNECT_BASE_DELAY (reconnect.py:198)
	reconnectBaseDelay = 5.0
	// reconnectMaxDelay 退避上限（秒）
	// Python: _RECONNECT_MAX_DELAY (reconnect.py:199)
	reconnectMaxDelay = 120.0
	// reconnectJitter 退避抖动比例
	// Python: _RECONNECT_JITTER (reconnect.py:200)
	reconnectJitter = 0.2
	// defaultReconnectAttempts 总尝试次数（首次 + 重试）
	// Python: _get_attempts() 默认 3 (reconnect.py:317)
	defaultReconnectAttempts = 3
	// streamableHTTPReconnectAttempts Streamable HTTP 的总尝试次数
	// Python: StreamableHttpClient._reconnect_attempts = 2
	streamableHTTPReconnectAttempts = 2
	// sessionTerminatedCode JSON-RPC「会话终止」错误码
	// Python: _SESSION_TERMINATED_CODE (reconnect.py:79)
	sessionTerminatedCode = 32600
	// connectionClosedCode JSON-RPC「连接关闭」错误码
	// Python: _CONNECTION_CLOSED_CODE (reconnect.py:80)
	connectionClosedCode = -32000
	// notConnectedMessage 客户端自身的未连接守卫文案
	notConnectedMessage = "not connected"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// sessionErrorMarkers 会话失效标记
// Python: _SESSION_ERROR_MARKERS (reconnect.py:26-31)
var sessionErrorMarkers = []string{
	"session terminated",
	"session not found",
	"invalid session",
	"no valid session",
}

// transportErrorMarkers 传输中断标记
// Python: _TRANSPORT_ERROR_MARKERS (reconnect.py:32-35)
var transportErrorMarkers = []string{
	"connection closed",
	"stream closed",
}

// retryableMarkers 全部可重试标记（已小写化）
// Python: _RETRYABLE_MARKERS (reconnect.py:55-57)
var retryableMarkers = func() []retryableMarker {
	all := make([]retryableMarker, 0, len(sessionErrorMarkers)+len(transportErrorMarkers)+7)
	for _, pattern := range sessionErrorMarkers {
		all = append(all, retryableMarker{pattern: pattern})
	}
	for _, pattern := range transportErrorMarkers {
		all = append(all, retryableMarker{pattern: pattern})
	}
	extra := []retryableMarker{
		{pattern: "closedresourceerror"},
		{pattern: "brokenresourceerror"},
		{pattern: "endofstream"},
		{pattern: "remoteprotocolerror"},
		{pattern: "readerror"},
		{pattern: "writeerror"},
		// 对 RuntimeError 跳过：客户端自己的"未连接"守卫也含此文案
		{pattern: "not connected", skipForRuntimeError: true},
		{pattern: "broken pipe"},
	}
	all = append(all, extra...)
	return all
}()

// reconnectStates 按客户端实例挂载重连状态
var reconnectStates sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// IsRetryableTransportError 判断 error 是否属于可重试的传输层故障。
// 先按 MCP 错误码判断（更稳健），再退回子串匹配。
// 匹配大小写不敏感：异常名与消息都转小写后与标记比对。
// Python: is_retryable_transport_error (reconnect.py:117-146)
func IsRetryableTransportError(err error) bool {
	if err == nil {
		return false
	}
	if code, message, ok := mcpErrorCodeAndMessage(err); ok {
		return isRetryableMCPError(code, message)
	}
	isRuntime := isRuntimeError(err)
	name := strings.ToLower(typeName(err))
	text := strings.ToLower(err.Error())
	for _, marker := range retryableMarkers {
		if marker.skipForRuntimeError && isRuntime {
			continue
		}
		if strings.Contains(name, marker.pattern) || strings.Contains(text, marker.pattern) {
			return true
		}
	}
	return false
}

// ShouldAttemptReconnect 判断一次失败的调用是否应触发一次重连。
// "not connected" 是客户端自己的守卫：只有在传输失败把连接拆掉
// （lazyReconnect 标记）时才可重试，用户主动断开后不可重试。
// Python: should_attempt_reconnect (reconnect.py:184-196)
func ShouldAttemptReconnect(client any, err error) bool {
	if err == nil {
		return false
	}
	if isNotConnectedGuard(err) {
		state := reconnectStateOf(client)
		if state == nil {
			return false
		}
		state.stateMu.Lock()
		defer state.stateMu.Unlock()
		return state.lazyReconnect
	}
	return IsRetryableTransportError(err)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// reconnectStatesKey 生成状态表的键。
// 用「接口值本身」当键：同一指针必然命中同一状态；非指针实现（值类型
// 客户端）拿不到稳定身份，返回 nil 由调用方降级为无状态处理。
func reconnectStateOf(client any) *reconnectState {
	key, ok := reconnectStateKey(client)
	if !ok {
		return nil
	}
	if state, loaded := reconnectStates.Load(key); loaded {
		return state.(*reconnectState)
	}
	state, _ := reconnectStates.LoadOrStore(key, &reconnectState{})
	return state.(*reconnectState)
}

// reconnectStateKey 取客户端实例的稳定身份键（指针地址）。
// 用接口值本身做 map 键在值是「不可比较类型」时会 panic，
// 因此统一转成指针地址；非指针实现返回 false。
func reconnectStateKey(client any) (uintptr, bool) {
	if client == nil {
		return 0, false
	}
	v := reflect.ValueOf(client)
	// 只有「可比较类型」才有稳定的身份地址；slice 等不可比较类型直接判 false。
	switch v.Kind() {
	case reflect.Pointer, reflect.Map, reflect.Chan, reflect.Func, reflect.UnsafePointer:
		return v.Pointer(), true
	default:
		return 0, false
	}
}

// errTypeOf 取错误的运行时类型名。
func errTypeOf(err error) string {
	if err == nil {
		return ""
	}
	return reflect.TypeOf(err).String()
}

// mcpErrorCodeAndMessage 尝试从 MCP 错误里取出 JSON-RPC 码与消息。
// Go 侧没有 mcp-go 的 McpError 类型可依赖（版本间命名不同），
// 按接口探测：具备 Code() int / Message() string 的错误视为 MCP 错误。
func mcpErrorCodeAndMessage(err error) (int, string, bool) {
	type codedError interface {
		Code() int
	}
	type messagedError interface {
		Message() string
	}
	coded, ok := err.(codedError)
	if !ok {
		return 0, "", false
	}
	message := err.Error()
	if messaged, ok := err.(messagedError); ok && messaged.Message() != "" {
		message = messaged.Message()
	}
	return coded.Code(), message, true
}

// isRetryableMCPError 按 JSON-RPC 码与消息判断 MCP 错误是否可重试。
// Python: _is_retryable_mcp_error (reconnect.py:85-98)
func isRetryableMCPError(code int, message string) bool {
	lower := strings.ToLower(message)
	if code == connectionClosedCode {
		return true
	}
	if code == sessionTerminatedCode && strings.Contains(lower, "session") {
		return true
	}
	for _, marker := range sessionErrorMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	for _, marker := range transportErrorMarkers {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// isNotConnectedGuard 判断是否为客户端自身的未连接守卫错误。
// Python: _is_not_connected_guard (reconnect.py:149-150)
func isNotConnectedGuard(err error) bool {
	return isRuntimeError(err) && strings.Contains(strings.ToLower(err.Error()), notConnectedMessage)
}

// isRuntimeError 判断是否属于"运行时错误"一族（含 fmt.Errorf 包装）。
// Go 没有 Python RuntimeError 的对应物：这里把非 MCP 业务错误的普通
// error 视作同类，仅用于跳过 "not connected" 这条标记。
func isRuntimeError(err error) bool {
	_, _, isMCP := mcpErrorCodeAndMessage(err)
	return !isMCP && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}

// typeName 取错误的运行时类型名（等价 Python type(e).__name__）。
func typeName(err error) string {
	if err == nil {
		return ""
	}
	name := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(errTypeOf(err)), "*"), "client.")
	return name
}

// resolveReconnectTimeout 解析重连用的超时。
// 原调用有期限就沿用，没有则返回 0（不限），与上游 -1 语义一致。
// Python: _resolve_reconnect_timeout (reconnect.py:153-163)
func resolveReconnectTimeout(ctx context.Context) float64 {
	if deadline, ok := ctx.Deadline(); ok {
		if remaining := time.Until(deadline).Seconds(); remaining > 0 {
			return remaining
		}
	}
	return 0
}

// cooldownRemaining 返回自动重连还需等待的秒数。
// Python: _cooldown_remaining (reconnect.py:209-215)
func (s *reconnectState) cooldownRemaining() float64 {
	if s == nil {
		return 0
	}
	s.stateMu.Lock()
	until := s.cooldownUntil
	s.stateMu.Unlock()
	if until.IsZero() {
		return 0
	}
	remaining := time.Until(until).Seconds()
	if remaining < 0 {
		return 0
	}
	return remaining
}

// recordFailure 记录一次重连失败并设置指数退避冷却（5s→120s，带抖动）。
// Python: _record_reconnect_failure (reconnect.py:218-232)
func (s *reconnectState) recordFailure() {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	s.failureCount++
	count := s.failureCount
	exponent := count - 1
	if exponent > 5 {
		exponent = 5
	}
	// rand 不是并发安全的，放在锁内取
	delay := math.Min(reconnectMaxDelay, reconnectBaseDelay*math.Pow(2, float64(exponent)))
	delay *= 1.0 - reconnectJitter + rand.Float64()*(2*reconnectJitter)
	s.cooldownUntil = time.Now().Add(time.Duration(delay * float64(time.Second)))
	s.stateMu.Unlock()

	logger.Warn(logComponent).
		Str("event_type", "mcp_reconnect_failed").
		Int("failure_count", count).
		Float64("next_attempt_in_s", delay).
		Msg("mcp reconnect failed, backing off")
}

// recordSuccess 连接成功后清空退避状态。
// Python: record_reconnect_success (reconnect.py:235-238)
func (s *reconnectState) recordSuccess() {
	if s == nil {
		return
	}
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	s.failureCount = 0
	s.cooldownUntil = time.Time{}
}

// resolveAttempts 解析总尝试次数（首次 + 重试）。
// 优先用客户端自报的 _reconnect_attempts；其次环境变量；最后默认 3。
// Python: _get_attempts (reconnect.py:299-318)
func resolveAttempts(override int) int {
	if override >= 1 {
		return override
	}
	if raw := os.Getenv("OPENJIUWEN_MCP_RECONNECT_ATTEMPTS"); raw != "" {
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
		logger.Debug(logComponent).
			Str("event_type", "mcp_reconnect_attempts_invalid").
			Str("value", raw).
			Msg("invalid OPENJIUWEN_MCP_RECONNECT_ATTEMPTS, using default 3")
	}
	return defaultReconnectAttempts
}

// reconnectTransport 拆掉并重建传输，按实例串行化。
// 若客户端自带 Reconnect 方法（传输相关的并发保护在内部），直接委托；
// 否则退回「断开 + 连接」并由实例锁串行化。
// Python: reconnect (reconnect.py:248-295)
func reconnectTransport(ctx context.Context, client any, state *reconnectState, timeout float64) bool {
	if native, ok := client.(interface {
		Reconnect(ctx context.Context, timeout float64) bool
	}); ok {
		if native.Reconnect(ctx, timeout) {
			return true
		}
		logger.Warn(logComponent).
			Str("event_type", "mcp_native_reconnect_failed").
			Msg("native reconnect failed, falling back to disconnect+connect")
	}

	type disconnector interface {
		Disconnect(ctx context.Context) error
	}
	type connector interface {
		Connect(ctx context.Context, opts ...types.ConnectOption) error
	}

	c, okConn := client.(connector)
	if !okConn {
		return false
	}

	if state == nil {
		// 拿不到稳定身份时退化为无锁重建（仅值类型实现会走到）
		return disconnectThenConnect(ctx, client, c)
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	return disconnectThenConnect(ctx, client, c)
}

// disconnectThenConnect 执行断开再连接。
// Python: reconnect() 的 disconnect + connect 序列
func disconnectThenConnect(ctx context.Context, client any, c interface {
	Connect(ctx context.Context, opts ...types.ConnectOption) error
}) bool {
	if disconnector, ok := client.(interface {
		Disconnect(ctx context.Context) error
	}); ok {
		if err := disconnector.Disconnect(ctx); err != nil {
			logger.Warn(logComponent).
				Str("event_type", "mcp_reconnect_disconnect_failed").
				Err(err).
				Msg("disconnect before reconnect failed")
		} else {
			logger.Info(logComponent).
				Str("event_type", "mcp_reconnect_disconnected").
				Msg("mcp client disconnected for reconnect")
		}
	}
	// 断开+连接不应被上层已取消的 ctx 直接掐断，但要保留期限
	connectCtx := ctx
	if _, hasDeadline := ctx.Deadline(); !hasDeadline {
		connectCtx = context.WithoutCancel(ctx)
	}
	if err := c.Connect(connectCtx); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "mcp_reconnect_connect_failed").
			Err(err).
			Msg("reconnect connect failed")
		return false
	}
	logger.Info(logComponent).
		Str("event_type", "mcp_reconnect_connected").
		Msg("mcp client reconnected")
	return true
}

// withReconnect 包装一次 MCP 调用：命中可重试传输错误时重连并重试。
// 语义对齐上游装饰器：非传输错误立即上抛不重连；调用方主动取消原样抛出；
// 已被其他调用方重建过传输（代际变化）时只重试不再拆连接；冷却期内的
// 自动重连直接放弃。
// Python: with_reconnect (reconnect.py:320-437)
func withReconnect[T any](ctx context.Context, client any, attemptsOverride int, method string, call func() (T, error)) (T, error) {
	var zero T
	state := reconnectStateOf(client)
	attempts := resolveAttempts(attemptsOverride)

	var lastErr error
	for attempt := 0; attempt < attempts; attempt++ {
		generationBefore := currentGeneration(state)

		result, err := call()
		if err == nil {
			return result, nil
		}

		// 调用方主动取消/超时：不是传输故障，原样上抛
		if ctx.Err() != nil {
			return zero, err
		}

		lastErr = err
		if attempt >= attempts-1 || !ShouldAttemptReconnect(client, err) {
			return zero, lastErr
		}

		// 另一个调用方已重建传输：直接重试，不再拆一次
		if generationBefore != 0 && currentGeneration(state) != generationBefore {
			logger.Info(logComponent).
				Str("event_type", "mcp_reconnect_generation_changed").
				Str("method", method).
				Int64("generation", currentGeneration(state)).
				Msg("transport already rebuilt by another call, retrying without reconnect")
			continue
		}

		if remaining := state.cooldownRemaining(); remaining > 0 {
			logger.Warn(logComponent).
				Str("event_type", "mcp_reconnect_cooldown_skip").
				Str("method", method).
				Float64("cooldown_remaining_s", remaining).
				Msg("skipping auto reconnect during cooldown")
			return zero, lastErr
		}

		if isNotConnectedGuard(err) && state != nil {
			state.stateMu.Lock()
			state.lazyReconnect = true
			state.stateMu.Unlock()
		}
		logger.Warn(logComponent).
			Str("event_type", "mcp_transport_error_reconnect").
			Str("method", method).
			Int("attempt", attempt+1).
			Int("attempts", attempts).
			Err(err).
			Msg("mcp transport error, reconnecting")

		reconnected := reconnectTransport(ctx, client, state, resolveReconnectTimeout(ctx))
		if !reconnected {
			if state.cooldownRemaining() == 0 {
				state.recordFailure()
			}
			return zero, lastErr
		}
		state.recordSuccess()
		bumpGeneration(state)
		logger.Info(logComponent).
			Str("event_type", "mcp_reconnect_retry").
			Str("method", method).
			Int("attempt", attempt+2).
			Int("attempts", attempts).
			Msg("mcp reconnect succeeded, retrying call")
	}
	return zero, lastErr
}

// currentGeneration 返回当前传输代际。
func currentGeneration(state *reconnectState) int64 {
	if state == nil {
		return 0
	}
	state.stateMu.Lock()
	defer state.stateMu.Unlock()
	return state.generation
}

// bumpGeneration 递增传输代际，让并发调用方知道传输已重建。
func bumpGeneration(state *reconnectState) {
	if state == nil {
		return
	}
	state.stateMu.Lock()
	defer state.stateMu.Unlock()
	state.generation++
}
