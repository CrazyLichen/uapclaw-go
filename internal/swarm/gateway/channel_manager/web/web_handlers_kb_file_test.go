package web

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/gateway/routing"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// TestKBFileProxies_注册齐全 30.5 kb 分帧方法族五个方法已注册
func TestKBFileProxies_注册齐全(t *testing.T) {
	d := RegisterWebHandlers(nil)
	for _, method := range []string{
		"kb.doc.upload", "kb.file.upload_chunk", "kb.file.upload_complete",
		"kb.doc.download", "kb.doc.cancel_upload",
	} {
		_, ok := d.methodHandlers[method]
		assert.True(t, ok, "方法未注册: %s", method)
	}
}

// TestKBFileProxies_无客户端回SERVICE_UNAVAILABLE 缺 AgentServer 客户端时回上游同款文案
func TestKBFileProxies_无客户端回SERVICE_UNAVAILABLE(t *testing.T) {
	wc := RegisterWebHandlers(nil) // agentClient=nil，handler 已注册但转发必失败
	for _, method := range []string{
		"kb.doc.upload", "kb.file.upload_chunk", "kb.file.upload_complete",
		"kb.doc.download", "kb.doc.cancel_upload",
	} {
		_, err := dispatch(wc, method, map[string]any{}, "sess-1")
		require.Error(t, err, "%s 无客户端应失败", method)
		assert.Equal(t, WsErrServiceUnavailable, errorCode(err))
	}
}

// TestKBFileUploadChunk_真链映射落盘 gateway 层入口驱动真链：
// kb.file.upload_chunk 经薄映射 → E2A file.upload_chunk → 真实 AgentServer
// workspace_file_adapter 落盘 → 响应回传（30.5 B1 数据面）。
func TestKBFileUploadChunk_真链映射落盘(t *testing.T) {
	cfg, err := config.New("")
	require.NoError(t, err)
	tr := transport.NewChannelTransport()
	srv := server.NewAgentServer(cfg, tr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, srv.Start(ctx))

	ac := routing.NewAgentClient(tr)
	require.NoError(t, ac.Connect(ctx))
	require.True(t, ac.WaitServerReady(ctx))
	defer ac.Disconnect()

	wc := newTestChannel()
	handler := kbUploadChunkForward(wc, ac)

	rel := filepath.Join("kb_frame_it", "hello.txt")
	payload, err := callHandler(handler, map[string]any{
		"upload_id":       "u-it-1",
		"target_rel_path": rel,
		"data":            "aGVsbG8ga2I=", // base64("hello kb")
		"final":           true,
	}, "sess-kbframe")
	require.NoError(t, err)
	require.Equal(t, "u-it-1", payload["upload_id"])
	target, _ := payload["path"].(string)
	require.NotEmpty(t, target)

	// 落盘在用户 workspace 内且内容正确（既有协议语义不丢）
	data, readErr := os.ReadFile(target)
	require.NoError(t, readErr)
	require.Equal(t, "hello kb", string(data))
	require.True(t, strings.HasPrefix(target, path.WorkspaceDir()), "落盘应位于用户 workspace 边界内")

	// 收尾清理（不污染 workspace）
	require.NoError(t, os.RemoveAll(filepath.Join(path.WorkspaceDir(), "kb_frame_it")))
}

// TestKBUploadForward_成功登记upload_id ②+①：kb.doc.upload 成功后 upload_id
// 应登记进 session 注册表，断连清理后摘除（真实 AgentServer 链）。
func TestKBUploadForward_成功登记upload_id(t *testing.T) {
	cfg, err := config.New("")
	require.NoError(t, err)
	tr := transport.NewChannelTransport()
	srv := server.NewAgentServer(cfg, tr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, srv.Start(ctx))

	ac := routing.NewAgentClient(tr)
	require.NoError(t, ac.Connect(ctx))
	require.True(t, ac.WaitServerReady(ctx))
	defer ac.Disconnect()

	wc := newTestChannel()
	wc.kbUploadAC = ac

	// 建宿主 KB
	env := e2a.E2AFromAgentFields("req-kb-create",
		e2a.WithFieldChannelID(wc.ChannelID()),
		e2a.WithFieldReqMethod("kb.kb.create"),
		e2a.WithFieldParams(map[string]any{
			"id": "it-track-kb", "name": "断连登记库", "type": "document",
		}),
	)
	resp, err := ac.SendRequest(ctx, env)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.OK, "kb.kb.create 失败: %v", resp.Payload)

	// 发起上传（经带追踪的 handler）
	payload, herr := callHandler(kbDocUploadForward(wc, ac), map[string]any{
		"kb_id": "it-track-kb", "file_name": "track.txt",
		"size": 3, "file_hash": "",
	}, "sess-track")
	require.NoError(t, herr)
	require.True(t, payload["upload"] != nil, "成功响应缺 upload: %v", payload)

	wc.kbUploadMu.Lock()
	_, tracked := wc.kbUploadSessions["sess-track"]
	wc.kbUploadMu.Unlock()
	require.True(t, tracked, "upload_id 未登记进 session 注册表")

	// 断连清理：注册表摘除（真实 cancel fire-and-forget；TTL/janitor 兜底）
	wc.trackConnSession(nil, "sess-track")
	wc.cleanupKBUploadsOnDisconnect(nil)
	wc.kbUploadMu.Lock()
	_, still := wc.kbUploadSessions["sess-track"]
	wc.kbUploadMu.Unlock()
	assert.False(t, still, "断连后在途登记未清理")
}

// TestExtractUploadID payload 形状解析
func TestExtractUploadID(t *testing.T) {
	assert.Equal(t, "u-1", extractUploadID(map[string]any{
		"upload": map[string]any{"upload_id": "u-1"},
	}))
	assert.Empty(t, extractUploadID(map[string]any{}))
	assert.Empty(t, extractUploadID(nil))
}
// TestKBUploadForward_成功登记upload_id ②+①：kb.doc.upload 成功后 upload_id
// 应登记进 session 注册表（真实 AgentServer 链）。
func TestKBUploadForward_成功登记upload_id(t *testing.T) {
	cfg, err := config.New("")
	require.NoError(t, err)
	tr := transport.NewChannelTransport()
	srv := server.NewAgentServer(cfg, tr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, srv.Start(ctx))

	ac := routing.NewAgentClient(tr)
	require.NoError(t, ac.Connect(ctx))
	require.True(t, ac.WaitServerReady(ctx))
	defer ac.Disconnect()

	wc := newTestChannel()
	wc.kbUploadAC = ac

	// 建宿主 KB
	env := e2a.E2AFromAgentFields("req-kb-create",
		e2a.WithFieldChannelID(wc.ChannelID()),
		e2a.WithFieldReqMethod("kb.kb.create"),
		e2a.WithFieldParams(map[string]any{
			"id": "it-track-kb", "name": "断连登记库", "type": "document",
		}),
	)
	resp, err := ac.SendRequest(ctx, env)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.OK, "kb.kb.create 失败: %v", resp.Payload)

	// 发起上传（经带追踪的 handler）
	payload, herr := callHandler(kbDocUploadForward(wc, ac), map[string]any{
		"kb_id": "it-track-kb", "file_name": "track.txt",
		"size": 3, "file_hash": "",
	}, "sess-track")
	require.NoError(t, herr)
	require.True(t, payload["upload"] != nil, "成功响应缺 upload: %v", payload)

	wc.kbUploadMu.Lock()
	_, tracked := wc.kbUploadSessions["sess-track"]
	wc.kbUploadMu.Unlock()
	require.True(t, tracked, "upload_id 未登记进 session 注册表")

	// 断连清理：注册表摘除（真实 cancel fire-and-forget；TTL/janitor 兜底）
	wc.trackConnSession(nil, "sess-track")
	wc.cleanupKBUploadsOnDisconnect(nil)
	wc.kbUploadMu.Lock()
	_, still := wc.kbUploadSessions["sess-track"]
	wc.kbUploadMu.Unlock()
	assert.False(t, still, "断连后在途登记未清理")
}

// TestExtractUploadID payload 形状解析
func TestExtractUploadID(t *testing.T) {
	assert.Equal(t, "u-1", extractUploadID(map[string]any{
		"upload": map[string]any{"upload_id": "u-1"},
	}))
	assert.Empty(t, extractUploadID(map[string]any{}))
	assert.Empty(t, extractUploadID(nil))
}

// TestKBUploadForward_成功登记upload_id ②+①：kb.doc.upload 成功后 upload_id
// 应登记进 session 注册表，断连清理后摘除（真实 AgentServer 链）。
func TestKBUploadForward_成功登记upload_id(t *testing.T) {
	cfg, err := config.New("")
	require.NoError(t, err)
	tr := transport.NewChannelTransport()
	srv := server.NewAgentServer(cfg, tr)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	require.NoError(t, srv.Start(ctx))

	ac := routing.NewAgentClient(tr)
	require.NoError(t, ac.Connect(ctx))
	require.True(t, ac.WaitServerReady(ctx))
	defer ac.Disconnect()

	wc := newTestChannel()
	wc.kbUploadAC = ac

	env := e2a.E2AFromAgentFields("req-kb-create",
		e2a.WithFieldChannelID(wc.ChannelID()),
		e2a.WithFieldReqMethod("kb.kb.create"),
		e2a.WithFieldParams(map[string]any{
			"id": "it-track-kb", "name": "断连登记库", "type": "document",
		}),
	)
	resp, err := ac.SendRequest(ctx, env)
	require.NoError(t, err)
	require.NotNil(t, resp)
	require.True(t, resp.OK, "kb.kb.create 失败: %v", resp.Payload)

	payload, herr := callHandler(kbDocUploadForward(wc, ac), map[string]any{
		"kb_id": "it-track-kb", "file_name": "track.txt",
		"size": 3, "file_hash": "",
	}, "sess-track")
	require.NoError(t, herr)
	require.True(t, payload["upload"] != nil, "成功响应缺 upload: %v", payload)

	wc.kbUploadMu.Lock()
	_, tracked := wc.kbUploadSessions["sess-track"]
	wc.kbUploadMu.Unlock()
	require.True(t, tracked, "upload_id 未登记进 session 注册表")

	wc.trackConnSession(nil, "sess-track")
	wc.cleanupKBUploadsOnDisconnect(nil)
	wc.kbUploadMu.Lock()
	_, still := wc.kbUploadSessions["sess-track"]
	wc.kbUploadMu.Unlock()
	assert.False(t, still, "断连后在途登记未清理")
}

// TestExtractUploadID payload 形状解析
func TestExtractUploadID(t *testing.T) {
	assert.Equal(t, "u-1", extractUploadID(map[string]any{
		"upload": map[string]any{"upload_id": "u-1"},
	}))
	assert.Empty(t, extractUploadID(map[string]any{}))
	assert.Empty(t, extractUploadID(nil))
}
