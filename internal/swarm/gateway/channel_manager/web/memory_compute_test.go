package web

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHandleMemoryCompute_字节换算为MB 锁死 bytes/(1024*1024) 公式。
func TestHandleMemoryCompute_字节换算为MB(t *testing.T) {
	orig := collectMemory
	defer func() { collectMemory = orig }()
	collectMemory = func() (uint64, uint64, uint64, float64, error) {
		return 1048576, 2097152, 524288, 25.5, nil
	}

	payload, err := callHandler(memoryCompute(newTestChannel()), nil, "")
	require.NoError(t, err)
	require.Equal(t, 1.0, payload["rss_mb"])
	require.Equal(t, 2.0, payload["total_mb"])
	require.Equal(t, 0.5, payload["available_mb"])
	require.Equal(t, 25.5, payload["used_percent"])
}

// TestHandleMemoryCompute_真实采集四字段 断言真实 gopsutil 采集的取值范围。
func TestHandleMemoryCompute_真实采集四字段(t *testing.T) {
	payload, err := callHandler(memoryCompute(newTestChannel()), nil, "")
	require.NoError(t, err)

	rssMB, ok := payload["rss_mb"].(float64)
	require.True(t, ok, "rss_mb 应为 float64，实际 %T", payload["rss_mb"])
	require.Greater(t, rssMB, 0.0, "本进程 RSS 应大于 0")

	totalMB, ok := payload["total_mb"].(float64)
	require.True(t, ok, "total_mb 应为 float64，实际 %T", payload["total_mb"])
	require.Greater(t, totalMB, 0.0, "系统总内存应大于 0")

	availMB, ok := payload["available_mb"].(float64)
	require.True(t, ok, "available_mb 应为 float64，实际 %T", payload["available_mb"])
	require.Greater(t, availMB, 0.0, "系统可用内存应大于 0")

	usedPercent, ok := payload["used_percent"].(float64)
	require.True(t, ok, "used_percent 应为 float64，实际 %T", payload["used_percent"])
	require.GreaterOrEqual(t, usedPercent, 0.0)
	require.LessOrEqual(t, usedPercent, 100.0)

	require.LessOrEqual(t, rssMB, totalMB, "进程 RSS 不应超过系统总内存")
}

// TestHandleMemoryCompute_进程RSS采集失败 覆盖 defaultCollectMemory 第一段错误分支。
func TestHandleMemoryCompute_进程RSS采集失败(t *testing.T) {
	orig := processRSSBytes
	defer func() { processRSSBytes = orig }()
	processRSSBytes = func() (uint64, error) {
		return 0, errors.New("rss failed")
	}

	_, err := callHandler(memoryCompute(newTestChannel()), nil, "")
	require.ErrorContains(t, err, "rss failed")
}

// TestHandleMemoryCompute_系统内存采集失败 覆盖 defaultCollectMemory 第二段错误分支。
func TestHandleMemoryCompute_系统内存采集失败(t *testing.T) {
	orig := systemVirtualMemory
	defer func() { systemVirtualMemory = orig }()
	systemVirtualMemory = func() (*mem.VirtualMemoryStat, error) {
		return nil, errors.New("vm failed")
	}

	_, err := callHandler(memoryCompute(newTestChannel()), nil, "")
	require.ErrorContains(t, err, "vm failed")
}

// TestHandleMemoryCompute_进程不存在 覆盖 defaultProcessRSSBytes 的 NewProcess 错误分支。
func TestHandleMemoryCompute_进程不存在(t *testing.T) {
	orig := currentPID
	defer func() { currentPID = orig }()
	currentPID = func() int { return -1 }

	_, err := callHandler(memoryCompute(newTestChannel()), nil, "")
	require.Error(t, err)
}

// TestHandleMemoryCompute_采集失败返回错误 覆盖 gopsutil 错误分支。
func TestHandleMemoryCompute_采集失败返回错误(t *testing.T) {
	orig := collectMemory
	defer func() { collectMemory = orig }()
	collectMemory = func() (uint64, uint64, uint64, float64, error) {
		return 0, 0, 0, 0, errors.New("collect failed")
	}

	payload, err := callHandler(memoryCompute(newTestChannel()), nil, "")
	require.Error(t, err)
	assert.Empty(t, payload)
}

// TestMemoryCompute_真实WS返回四字段 真 WebChannel + 真 dispatcher + 真采集。
func TestMemoryCompute_真实WS返回四字段(t *testing.T) {
	wc := NewWebChannel(WebChannelConfig{Enabled: true}, nil, nil)
	RegisterWebHandlers(&WebHandlersBindParams{Channel: wc})
	require.NoError(t, wc.Start(context.Background()))
	defer func() { _ = wc.Stop(context.Background()) }()

	mux := http.NewServeMux()
	mux.HandleFunc("/ws", wc.HandleWebSocket)
	server := httptest.NewServer(mux)
	defer server.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	require.NoError(t, err)
	defer func() { _ = conn.Close() }()

	reqFrame := map[string]any{"type": "req", "id": "mem-1", "method": "memory.compute"}
	reqData, err := json.Marshal(reqFrame)
	require.NoError(t, err)
	require.NoError(t, conn.WriteMessage(websocket.TextMessage, reqData))

	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	_, respData, err := conn.ReadMessage()
	require.NoError(t, err)

	var resp map[string]any
	require.NoError(t, json.Unmarshal(respData, &resp))
	require.Equal(t, "res", resp["type"])
	require.Equal(t, true, resp["ok"])

	payload, ok := resp["payload"].(map[string]any)
	require.True(t, ok, "payload 应为 map，实际 %T", resp["payload"])

	for _, key := range []string{"rss_mb", "total_mb", "available_mb"} {
		value, isNum := payload[key].(float64)
		require.True(t, isNum, "%s 应为 number，实际 %T", key, payload[key])
		require.Greater(t, value, 0.0, "%s 应大于 0", key)
	}
	usedPercent, isNum := payload["used_percent"].(float64)
	require.True(t, isNum, "used_percent 应为 number，实际 %T", payload["used_percent"])
	require.GreaterOrEqual(t, usedPercent, 0.0)
	require.LessOrEqual(t, usedPercent, 100.0)
}
