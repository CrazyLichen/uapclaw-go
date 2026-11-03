package web

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestHandleMemoryCompute_字节换算为MB 锁死 bytes/(1024*1024) 公式。
func TestHandleMemoryCompute_字节换算为MB(t *testing.T) {
	orig := collectMemory
	defer func() { collectMemory = orig }()
	collectMemory = func() (uint64, uint64, uint64, float64, error) {
		return 1048576, 2097152, 524288, 25.5, nil
	}

	payload, err := handleMemoryCompute()(context.Background(), nil, "")
	require.NoError(t, err)
	require.Equal(t, 1.0, payload["rss_mb"])
	require.Equal(t, 2.0, payload["total_mb"])
	require.Equal(t, 0.5, payload["available_mb"])
	require.Equal(t, 25.5, payload["used_percent"])
}

// TestHandleMemoryCompute_真实采集四字段 断言真实 gopsutil 采集的取值范围。
func TestHandleMemoryCompute_真实采集四字段(t *testing.T) {
	payload, err := handleMemoryCompute()(context.Background(), nil, "")
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

// TestHandleMemoryCompute_采集失败返回错误 覆盖 gopsutil 错误分支。
func TestHandleMemoryCompute_采集失败返回错误(t *testing.T) {
	orig := collectMemory
	defer func() { collectMemory = orig }()
	collectMemory = func() (uint64, uint64, uint64, float64, error) {
		return 0, 0, 0, 0, errors.New("collect failed")
	}

	payload, err := handleMemoryCompute()(context.Background(), nil, "")
	require.Error(t, err)
	require.Nil(t, payload)
}
