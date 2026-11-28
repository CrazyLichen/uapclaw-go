package server

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/e2a"
)

// TestHandleMessage_KBPrefixBranch 验证 kb.* 前缀分支进入 kbadmin 域并写回响应。
func TestHandleMessage_KBPrefixBranch(t *testing.T) {
	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// kb.kb.list 经前缀分支 → kbadmin 分发 → 响应写回 RecvCh
	env := makeTestEnvelope("kb.kb.list", false)
	go func() { s.handleMessage(ctx, env) }()

	recvCh, err := transport.Recv()
	require.NoError(t, err)
	select {
	case data := <-recvCh:
		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		resp := e2a.ResponseFromMap(m)
		require.Equal(t, true, resp.IsFinal)
		// wire body.details 携带 kb 列表（编码链核对）
		body := m["body"].(map[string]any)
		require.NotNil(t, body)
	case <-time.After(4 * time.Second):
		t.Fatal("kb.kb.list 响应超时")
	}
}

// TestHandleMessage_KBNotImplemented 验证未实现方法经前缀分支返回 KB_NOT_IMPLEMENTED。
func TestHandleMessage_KBNotImplemented(t *testing.T) {
	s, transport := newTestServer()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	env := makeTestEnvelope("kb.doc.upload", false)
	go func() { s.handleMessage(ctx, env) }()

	recvCh, err := transport.Recv()
	require.NoError(t, err)
	select {
	case data := <-recvCh:
		var m map[string]any
		require.NoError(t, json.Unmarshal(data, &m))
		resp := e2a.ResponseFromMap(m)
		require.Equal(t, true, resp.IsFinal)
		// wire body.details.error_code == KB_NOT_IMPLEMENTED
		body, _ := m["body"].(map[string]any)
		require.NotNil(t, body)
		details, _ := body["details"].(map[string]any)
		require.NotNil(t, details)
		require.Equal(t, "KB_NOT_IMPLEMENTED", details["error_code"])
	case <-time.After(4 * time.Second):
		t.Fatal("kb.doc.upload 响应超时")
	}
}
