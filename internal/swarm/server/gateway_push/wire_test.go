package gateway_push

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestBuildServerPushWire_有ResponseKind 测试有 response_kind 的 server_push 编码
func TestBuildServerPushWire_有ResponseKind(t *testing.T) {
	msg := map[string]any{
		"request_id":    "req-001",
		"response_kind": "cron",
		"channel_id":    "web",
		"session_id":    "sess-001",
		"body":          map[string]any{"result": "ok"},
	}

	wire := BuildServerPushWire(msg)
	require.NotNil(t, wire)

	// 验证 E2A wire 格式
	assert.Equal(t, "1.0", wire["protocol_version"])
	assert.Equal(t, "cron", wire["response_kind"])
	assert.Equal(t, "req-001", wire["request_id"])
	assert.Equal(t, true, wire["is_final"])
	assert.Equal(t, "succeeded", wire["status"])

	// 验证 metadata 含 server_push 标记
	metadata, ok := wire["metadata"].(map[string]any)
	require.True(t, ok, "metadata 应为 map[string]any")
	assert.Equal(t, true, metadata[e2a.E2AWireServerPushKey])
}

// TestBuildServerPushWire_无ResponseKind 测试无 response_kind 的 chunk 形 server_push 编码
func TestBuildServerPushWire_无ResponseKind(t *testing.T) {
	msg := map[string]any{
		"request_id": "req-002",
		"channel_id": "web",
		"session_id": "sess-002",
		"payload":    map[string]any{"event_type": "todo.updated"},
	}

	wire := BuildServerPushWire(msg)
	require.NotNil(t, wire)

	// 验证 metadata 含 server_push 标记
	metadata, ok := wire["metadata"].(map[string]any)
	require.True(t, ok, "metadata 应为 map[string]any")
	assert.Equal(t, true, metadata[e2a.E2AWireServerPushKey])

	// 验证 request_id
	assert.Equal(t, "req-002", wire["request_id"])
}

// TestBuildConnectionAckFrame 测试 connection.ack 事件帧构建
func TestBuildConnectionAckFrame(t *testing.T) {
	frame := BuildConnectionAckFrame()
	require.NotNil(t, frame)

	assert.Equal(t, "event", frame["type"])
	assert.Equal(t, "connection.ack", frame["event"])

	payload, ok := frame["payload"].(map[string]any)
	require.True(t, ok, "payload 应为 map[string]any")
	assert.Equal(t, "ready", payload["status"])
}

// TestWireRequestIDKey 测试 request_id 键规范化
func TestWireRequestIDKey(t *testing.T) {
	assert.Equal(t, "", WireRequestIDKey(nil))
	assert.Equal(t, "abc-123", WireRequestIDKey("abc-123"))
	assert.Equal(t, "", WireRequestIDKey(""))
	assert.Equal(t, "42", WireRequestIDKey(42))
	assert.Equal(t, "3.14", WireRequestIDKey(3.14))
}
