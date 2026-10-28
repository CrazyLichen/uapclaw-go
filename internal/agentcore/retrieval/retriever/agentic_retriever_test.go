package retriever

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// stubAgenticLLM 零行为桩（stubTripleClient 先例）——嵌入补全 BaseModelClient 全接口。
type stubAgenticLLM struct{}

func (s *stubAgenticLLM) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubAgenticLLM) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubAgenticLLM) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubAgenticLLM) SupportsKVCacheRelease() bool { return false }

// agenticLLMDispatch 按 prompt 内容分派的可编程 LLM 桩。
//
// 分派规则（13.17 dispatchTripleClient 先例——与调度序解耦）：
//   - prompt 含 "knowledge triples, which are structured"（readPrompt 特征）
//     → readFn
//   - prompt 含 "\"sufficient\" to true"（rewritePrompt 特征）→ rewriteFn
//   - 都不命中 → otherCalls 计数（测试断言零意外调用）
//
// mu 保护：BatchRetrieve 双 query 并发 + _link gather 并发下 Invoke 竞争。
type agenticLLMDispatch struct {
	stubAgenticLLM
	mu           sync.Mutex
	readFn       func(prompt string) (string, error)
	rewriteFn    func(prompt string) (string, error)
	readCalls    int
	rewriteCalls int
	otherCalls   int
}

func (d *agenticLLMDispatch) Invoke(_ context.Context, messages model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	prompt := ""
	for _, m := range messages.Messages() {
		if u, ok := m.(*llmschema.UserMessage); ok {
			prompt = u.Content.Text()
		}
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	var content string
	var err error
	switch {
	case strings.Contains(prompt, "knowledge triples, which are structured"):
		d.readCalls++
		content, err = d.readFn(prompt)
	case strings.Contains(prompt, `"sufficient" to true`):
		d.rewriteCalls++
		content, err = d.rewriteFn(prompt)
	default:
		d.otherCalls++
		content, err = "", nil
	}
	if err != nil {
		return nil, err
	}
	return llmschema.NewAssistantMessage(content), nil
}

// agenticFakeRetriever 可编程检索器桩（不动 13.23 现有桩）。
//
// retrieveFn/rsrFn 闭包注入；调用记录 mu 保护（_link gather 并发）；
// indexType 实现 IndexTypeCarrier（defaultMode 三态测试）。
type agenticFakeRetriever struct {
	mu            sync.Mutex
	retrieveFn    func(ctx context.Context, query string, topK int, threshold *float64, mode RetrievalMode) ([]common.RetrievalResult, error)
	rsrFn         func(ctx context.Context, query string, topK int, mode RetrievalMode) ([]common.SearchResult, error)
	lastQuery     string
	lastTopK      int
	lastMode      RetrievalMode
	lastThreshold *float64
	calls         int
	rsrCalls      int
	closed        int
	indexType     string
}

func (f *agenticFakeRetriever) Retrieve(ctx context.Context, query string, topK int, threshold *float64, mode RetrievalMode, _ ...RetrieveOption) ([]common.RetrievalResult, error) {
	f.mu.Lock()
	f.lastQuery, f.lastTopK, f.lastMode, f.lastThreshold, f.calls = query, topK, mode, threshold, f.calls+1
	f.mu.Unlock()
	return f.retrieveFn(ctx, query, topK, threshold, mode)
}

func (f *agenticFakeRetriever) BatchRetrieve(context.Context, []string, int, ...RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}

func (f *agenticFakeRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, mode RetrievalMode, _ ...RetrieveOption) ([]common.SearchResult, error) {
	f.mu.Lock()
	f.rsrCalls++
	f.mu.Unlock()
	return f.rsrFn(ctx, query, topK, mode)
}

func (f *agenticFakeRetriever) Close() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.closed++
}

// IndexType 实现 IndexTypeCarrier（defaultMode 推导测试用；接口含
// SetIndexType 双方法——capability.go 13.23 定义）。
func (f *agenticFakeRetriever) IndexType() string     { return f.indexType }
func (f *agenticFakeRetriever) SetIndexType(v string) { f.indexType = v }

// ──────────────────────────── 构造测试 ────────────────────────────

// TestNewAgenticRetriever_图检索器构造 对齐 test_init_success_with_graph_retriever。
func TestNewAgenticRetriever_图检索器构造(t *testing.T) {
	gr := NewGraphRetriever()
	r, err := NewAgenticRetriever(gr, &stubAgenticLLM{}, WithAgenticMaxIter(3))
	require.NoError(t, err)
	assert.True(t, r.IsGraphRetriever())
	assert.Equal(t, 3, r.MaxIter())
	assert.Equal(t, RetrievalModeHybrid, r.DefaultMode()) // 零值 indexType → hybrid
}

// TestNewAgenticRetriever_通用检索器构造 对齐 test_init_success_with_base_retriever。
func TestNewAgenticRetriever_通用检索器构造(t *testing.T) {
	fr := &agenticFakeRetriever{indexType: "hybrid"}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{}, WithAgenticMaxIter(2))
	require.NoError(t, err)
	assert.False(t, r.IsGraphRetriever())
	assert.Equal(t, 2, r.MaxIter())
}

// TestNewAgenticRetriever_默认值 对齐 test_init_with_defaults。
func TestNewAgenticRetriever_默认值(t *testing.T) {
	r, err := NewAgenticRetriever(NewGraphRetriever(), &stubAgenticLLM{})
	require.NoError(t, err)
	assert.Equal(t, 2, r.MaxIter())
}

// TestNewAgenticRetriever_非法maxIter回落 对齐 test_init_with_invalid_max_iter。
func TestNewAgenticRetriever_非法maxIter回落(t *testing.T) {
	r, err := NewAgenticRetriever(NewGraphRetriever(), &stubAgenticLLM{}, WithAgenticMaxIter(-1))
	require.NoError(t, err)
	assert.Equal(t, 2, r.MaxIter())
}

// TestNewAgenticRetriever_缺retriever 对齐 test_init_without_retriever（155208）。
func TestNewAgenticRetriever_缺retriever(t *testing.T) {
	_, err := NewAgenticRetriever(nil, &stubAgenticLLM{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "retriever is required for AgenticRetriever")
}

// TestNewAgenticRetriever_缺llm客户端 对齐 test_init_without_llm_client（155209）。
func TestNewAgenticRetriever_缺llm客户端(t *testing.T) {
	_, err := NewAgenticRetriever(NewGraphRetriever(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "llm_client is required for AgenticRetriever")
}

// TestNewAgenticRetriever_默认模式三态 对齐 test_default_mode_vector/_bm25/_hybrid。
func TestNewAgenticRetriever_默认模式三态(t *testing.T) {
	for _, tc := range []struct {
		indexType string
		want      RetrievalMode
	}{
		{"vector", RetrievalModeVector},
		{"bm25", RetrievalModeSparse},
		{"hybrid", RetrievalModeHybrid},
		{"", RetrievalModeHybrid},             // getattr None → or "hybrid"
		{"unknown-type", RetrievalModeHybrid}, // 其余分支
	} {
		fr := &agenticFakeRetriever{indexType: tc.indexType}
		r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
		require.NoError(t, err)
		assert.Equal(t, tc.want, r.DefaultMode(), "indexType=%q", tc.indexType)
	}
}
