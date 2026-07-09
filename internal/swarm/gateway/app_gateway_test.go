package gateway

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/gateway_push"
)

// newTestGatewayServer 创建测试用 GatewayServer
func newTestGatewayServer(t *testing.T) *GatewayServer {
	t.Helper()
	cfg, err := config.New("")
	require.NoError(t, err)
	transport := gateway_push.NewChannelTransport()
	gs, err := NewGatewayServer(cfg, transport, transport, nil)
	require.NoError(t, err)
	return gs
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewGatewayServer(t *testing.T) {
	gs := newTestGatewayServer(t)
	assert.NotNil(t, gs)
	assert.NotNil(t, gs.router)
	assert.NotNil(t, gs.webChannel)
	assert.NotNil(t, gs.channelMgr)
	assert.NotNil(t, gs.msgHandler)
}

func TestGatewayServer_Router路由(t *testing.T) {
	gs := newTestGatewayServer(t)

	// 启动测试服务器
	server := httptest.NewServer(gs.router)
	defer server.Close()

	client := server.Client()
	client.Timeout = 2 * time.Second

	// 测试 /file-api 端点
	resp, err := client.Get(server.URL + "/file-api/ws-debug-config")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// 测试前端静态文件
	resp, err = client.Get(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestGatewayServer_Stop(t *testing.T) {
	gs := newTestGatewayServer(t)

	// 验证 Stop 不崩溃（httpServer 为 nil 的情况）
	err := gs.Stop()
	assert.NoError(t, err)
}

func TestSPAHandler(t *testing.T) {
	handler := newSPAHandler(frontendDist)
	server := httptest.NewServer(handler)
	defer server.Close()

	client := server.Client()
	client.Timeout = 2 * time.Second

	// 测试根路径
	resp, err := client.Get(server.URL + "/")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()

	// 测试 index.html
	resp, err = client.Get(server.URL + "/index.html")
	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, resp.StatusCode)
	_ = resp.Body.Close()
}

func TestDefaultHostPort(t *testing.T) {
	// 空配置使用默认值
	assert.Equal(t, "127.0.0.1", defaultHost(nil))
	assert.Equal(t, 19000, defaultPort(nil))

	// 配置中的值
	cfg, _ := config.New("")
	assert.Equal(t, "127.0.0.1", defaultHost(cfg))
	assert.Equal(t, 19000, defaultPort(cfg))
}

// ──────────────────────────── 非导出函数 ────────────────────────────
