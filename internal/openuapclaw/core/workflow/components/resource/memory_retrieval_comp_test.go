package resource

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	ltm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/memory/ltm"
)

// newTestLTM 真实链：真 ltm.LongTermMemory（未装配存储——错误传播路径真链可验；
// 成功路径需完整向量库装配，见集成测试登记）
func newTestLTM() *ltm.LongTermMemory {
	return ltm.NewLongTermMemory()
}

func TestMemoryRetrieval_validateInputs_query缺失报错(t *testing.T) {
	_, err := validateMemoryRetrievalInputs(map[string]any{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "101200")
}

func TestMemoryRetrieval_validateInputs_query非字符串报错(t *testing.T) {
	_, err := validateMemoryRetrievalInputs(map[string]any{"query": 123})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Input should be a valid string")
}

func TestMemoryRetrieval_validateInputs_topK默认5与透传(t *testing.T) {
	in, err := validateMemoryRetrievalInputs(map[string]any{"query": "q"})
	require.NoError(t, err)
	assert.Equal(t, 5, in.TopK)

	in2, err := validateMemoryRetrievalInputs(map[string]any{"query": "q", "top_k": 3})
	require.NoError(t, err)
	assert.Equal(t, 3, in2.TopK)

	_, err = validateMemoryRetrievalInputs(map[string]any{"query": "q", "top_k": 1.5})
	require.Error(t, err, "pydantic int strict 不接受 float")
}

func TestMemoryRetrieval_默认值三元组落__default__(t *testing.T) {
	cfg := newMemoryRetrievalConfigDefaults(nil)
	assert.Equal(t, "__default__", cfg.ScopeID)
	assert.Equal(t, "__default__", cfg.UserID)
	assert.Equal(t, 0.3, cfg.Threshold)
}

func TestMemoryRetrieval_invoke_空query返回101200(t *testing.T) {
	e, err := NewMemoryRetrievalExecutable(&MemoryRetrievalCompConfig{Memory: newTestLTM()})
	require.NoError(t, err)
	sess := newTestNodeSession(t)
	_, err = e.invokeEntry(context.Background(), map[string]any{"query": "  "}, sess, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "101200")
}

func TestMemoryRetrieval_invoke_真实链错误包装101201(t *testing.T) {
	// 真实链：裸 LTM 未装配 search manager → SearchUserMem 返回错误 → 组件 wrap 101201
	e, err := NewMemoryRetrievalExecutable(&MemoryRetrievalCompConfig{Memory: newTestLTM()})
	require.NoError(t, err)
	sess := newTestNodeSession(t)
	_, err = e.invokeEntry(context.Background(), map[string]any{"query": "test"}, sess, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "101201")
}

func TestMemoryRetrieval_构造_memory缺失报错(t *testing.T) {
	_, err := NewMemoryRetrievalExecutable(&MemoryRetrievalCompConfig{})
	require.Error(t, err)
}

func TestMemoryRetrieval_格式化输出键名与空列表(t *testing.T) {
	out := formatMemoryRetrievalOutput(nil, nil)
	results, ok := out["fragment_memory_results"]
	require.True(t, ok)
	assert.Equal(t, []*ltm.MemResult{}, results)
	_, ok = out["summary_results"]
	assert.True(t, ok)
}
