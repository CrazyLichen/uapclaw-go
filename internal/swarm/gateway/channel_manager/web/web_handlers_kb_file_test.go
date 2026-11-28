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
