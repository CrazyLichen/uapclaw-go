package llm

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
)

// ──────────────────────────── 测试替身 ────────────────────────────

// kvcActionClientMock 支持可选 KVC 动作接口的客户端替身，记录最近一次调用。
type kvcActionClientMock struct {
	mockModelClient
	// lastAction 记录最近一次动作名（evict/offload/prefetch）
	lastAction string
	// lastParams 记录最近一次入参
	lastParams *model_clients.KVCacheActionParams
	// result 返回值
	result bool
	// err 注入错误
	err error
}

func (m *kvcActionClientMock) EvictKvc(_ context.Context, params *model_clients.KVCacheActionParams) (bool, error) {
	m.lastAction = "evict"
	m.lastParams = params
	return m.result, m.err
}

func (m *kvcActionClientMock) OffloadKvc(_ context.Context, params *model_clients.KVCacheActionParams) (bool, error) {
	m.lastAction = "offload"
	m.lastParams = params
	return m.result, m.err
}

func (m *kvcActionClientMock) PrefetchKvc(_ context.Context, params *model_clients.KVCacheActionParams) (bool, error) {
	m.lastAction = "prefetch"
	m.lastParams = params
	return m.result, m.err
}

// newKVCActionTestModel 构造挂接替身客户端的 Model。
func newKVCActionTestModel(c model_clients.BaseModelClient) *Model {
	return &Model{client: c}
}

// ──────────────────────────── 用例 ────────────────────────────

// TestModel_EvictKvc_普通客户端静默False 测试底层客户端不支持 KVC 动作时静默返回 false。
// Python: model.py:400 _run_kv_cache_action getattr 探测失败返回 False。
func TestModel_EvictKvc_普通客户端静默False(t *testing.T) {
	m := newKVCActionTestModel(&mockModelClient{})
	got := m.EvictKvc(context.Background(), &model_clients.KVCacheActionParams{SessionID: "s1"})
	assert.False(t, got)
}

// TestModel_EvictKvc_支持客户端转发参数 测试支持客户端的参数转发与默认值。
// Python: model.py:416 evict_kvc 逐关键字参数透传。
func TestModel_EvictKvc_支持客户端转发参数(t *testing.T) {
	mock := &kvcActionClientMock{result: true}
	m := newKVCActionTestModel(mock)

	got := m.EvictKvc(context.Background(), &model_clients.KVCacheActionParams{
		SessionID:       "cache-1",
		ParentSessionID: "parent-1",
		MsgStart:        intPtr(0),
		MsgEnd:          intPtr(5),
	})
	require.True(t, got)
	assert.Equal(t, "evict", mock.lastAction)
	require.NotNil(t, mock.lastParams)
	assert.Equal(t, "cache-1", mock.lastParams.SessionID)
	assert.Equal(t, "parent-1", mock.lastParams.ParentSessionID)
	// Python 默认 target="session"（model.py:421）
	assert.Equal(t, "session", mock.lastParams.Target)
}

// TestModel_EvictKvc_底层错误静默False 测试底层动作报错时不上抛、返回 false。
// Python: model.py:408-415 except 分支 warning + False。
func TestModel_EvictKvc_底层错误静默False(t *testing.T) {
	mock := &kvcActionClientMock{err: errors.New("affinity endpoint down")}
	m := newKVCActionTestModel(mock)
	got := m.EvictKvc(context.Background(), &model_clients.KVCacheActionParams{SessionID: "s1"})
	assert.False(t, got)
}

// TestModel_OffloadKvc_同形转发 测试 offload 动作同形转发。
// Python: model.py:464 offload_kvc。
func TestModel_OffloadKvc_同形转发(t *testing.T) {
	mock := &kvcActionClientMock{result: true}
	m := newKVCActionTestModel(mock)
	got := m.OffloadKvc(context.Background(), &model_clients.KVCacheActionParams{
		SessionID:    "s2",
		IncludeTools: true,
	})
	require.True(t, got)
	assert.Equal(t, "offload", mock.lastAction)
	assert.True(t, mock.lastParams.IncludeTools)
}

// TestModel_PrefetchKvc_同形转发 测试 prefetch 动作同形转发。
// Python: model.py prefetch_kvc（openai_model_client.py:855 同族）。
func TestModel_PrefetchKvc_同形转发(t *testing.T) {
	mock := &kvcActionClientMock{result: true}
	m := newKVCActionTestModel(mock)
	got := m.PrefetchKvc(context.Background(), &model_clients.KVCacheActionParams{
		SessionID: "s3",
		TurnNum:   intPtr(3),
	})
	require.True(t, got)
	assert.Equal(t, "prefetch", mock.lastAction)
	assert.Equal(t, 3, *mock.lastParams.TurnNum)
}

// TestModel_EvictKvc_普通客户端零参数静默False 无 params 也不 panic，静默 false。
func TestModel_EvictKvc_普通客户端零参数静默False(t *testing.T) {
	m := newKVCActionTestModel(&mockModelClient{})
	assert.False(t, m.EvictKvc(context.Background(), nil))
}

// intPtr 测试辅助
func intPtr(i int) *int { return &i }
