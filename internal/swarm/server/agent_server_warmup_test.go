package server

import (
	"context"
	"encoding/json"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// 本文件验证图像模态预热统一防抖槽与 agent.reload_config 触发条件。
// Python: agent_ws_server.py:1857-1880 schedule_image_modality_warmup +
// :12912-12921 reload 钩子（23.7 Task 6）

// reloadRequest 构造 agent.reload_config 请求
func reloadRequest(t *testing.T, scopes []string) *schema.AgentRequest {
	params := map[string]any{}
	if scopes != nil {
		params["reload_scopes"] = scopes
	}
	raw, err := json.Marshal(params)
	require.NoError(t, err)
	return schema.NewAgentRequest("req-1", "ch-1", schema.ReqMethodAgentReloadConfig, raw)
}

// warmupRecorder 记录预热调度调用（并发安全）
type warmupRecorder struct {
	mu    sync.Mutex
	calls [][]any
}

// record 记录一次调度
func (r *warmupRecorder) record(reason string, resetCache bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls = append(r.calls, []any{reason, resetCache})
}

// len 返回已记录次数
func (r *warmupRecorder) len() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.calls)
}

// at 返回第 i 次调用
func (r *warmupRecorder) at(i int) (string, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if i >= len(r.calls) {
		return "", false
	}
	c := r.calls[i]
	return c[0].(string), c[1].(bool)
}

// newWarmupTestServer 构造记录调度调用的测试 AgentServer
func newWarmupTestServer() (*AgentServer, *warmupRecorder) {
	s := NewAgentServer(nil, nil)
	rec := &warmupRecorder{}
	s.imageModalityWarmupFn = rec.record
	return s, rec
}

// TestReloadConfig_TriggersImageModalityRefresh reload_scopes 含 model → 重探（reset 缓存）。
// Python: agent_ws_server.py:12914-12921 should_refresh_image_modality = should_warm_tokenizers
func TestReloadConfig_TriggersImageModalityRefresh(t *testing.T) {
	s, calls := newWarmupTestServer()
	_, err := s.handleAgentReloadConfig(context.Background(), reloadRequest(t, []string{"model"}))
	require.NoError(t, err)
	// 调度走后台 goroutine，等待其执行注入的记录函数
	assert.Eventually(t, func() bool { return calls.len() == 1 }, 2e9, 1e7)
	reason, reset := calls.at(0)
	assert.Equal(t, "model config change", reason)
	assert.True(t, reset)
}

// TestReloadConfig_NoScopes_TriggersRefresh 不带 reload_scopes → 全域刷新（含重探）。
// Python: should_warm_tokenizers = True（scopes 为空时）
func TestReloadConfig_NoScopes_TriggersRefresh(t *testing.T) {
	s, calls := newWarmupTestServer()
	_, err := s.handleAgentReloadConfig(context.Background(), reloadRequest(t, nil))
	require.NoError(t, err)
	assert.Eventually(t, func() bool { return calls.len() == 1 }, 2e9, 1e7)
}

// TestReloadConfig_NonModelScope_SkipsRefresh 不含 model 的 scope → 不重探。
// Python: should_warm_tokenizers = "model" in reload_scopes
func TestReloadConfig_NonModelScope_SkipsRefresh(t *testing.T) {
	s, calls := newWarmupTestServer()
	_, err := s.handleAgentReloadConfig(context.Background(), reloadRequest(t, []string{"multimodal"}))
	require.NoError(t, err)
	assert.Equal(t, 0, calls.len())
}

// TestImageModalityWarmupSlot_Debounce 统一槽位防抖：新调度取消上一轮未完成任务。
// Python: agent_ws_server.py:1857-1880 _image_modality_refresh_task 单槽
func TestImageModalityWarmupSlot_Debounce(t *testing.T) {
	s := NewAgentServer(nil, nil)

	var mu sync.Mutex
	rounds := 0
	started := make(chan struct{}, 2)
	s.imageModalityWarmupFn = func(string, bool) {
		started <- struct{}{}
		<-s.imageModalityRefreshContext().Done() // 等待被新一轮取消
		mu.Lock()
		rounds++
		mu.Unlock()
	}

	s.scheduleImageModalityWarmup("round-1", false)
	<-started
	s.scheduleImageModalityWarmup("round-2", false)
	<-started

	// 两轮都启动过；第一轮被取消后很快结束
	assert.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return rounds == 1
	}, 2e9, 1e7, "第一轮应被取消并结束")
}
