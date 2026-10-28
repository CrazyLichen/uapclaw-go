package retriever

import (
	"context"
	"errors"
	"fmt"
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

// ──────────────────────────── 通用路径测试 ────────────────────────────

// newGenericRetriever 通用路径测试脚手架：固定单结果 chunk 桩 + 可分派 LLM。
func newGenericRetriever(t *testing.T, llm *agenticLLMDispatch) *AgenticRetriever {
	t.Helper()
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	return r
}

// TestRetrieve_通用路径单轮 对齐 test_retrieve_generic_single_iteration。
func TestRetrieve_通用路径单轮(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestRetrieve_通用路径多轮 对齐 test_retrieve_generic_multiple_iterations
// （max_iter=3，chunk 桩 calls==3 断言）。
func TestRetrieve_通用路径多轮(t *testing.T) {
	rewriteCount := 0
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			rewriteCount++
			return fmt.Sprintf(`{"sufficient": false, "next_question": "rewritten query %d"}`, rewriteCount), nil
		},
	}
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	r, err := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(3))
	require.NoError(t, err)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 3, fr.calls)
}

// TestRetrieve_通用路径到顶 对齐 test_retrieve_generic_max_iterations
// （默认 max_iter=2 到顶退出）。
func TestRetrieve_通用路径到顶(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": false, "next_question": "rewritten query 1"}`, nil
		},
	}
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 2, fr.calls)
}

// TestRetrieve_融合多结果 对齐 test_retrieve_fusion_multiple_results
// （两轮不同 text → RRF 融合 2 条）。
func TestRetrieve_融合多结果(t *testing.T) {
	callIdx := 0
	var mu sync.Mutex
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(_ context.Context, _ string, _ int, _ *float64, _ RetrievalMode) ([]common.RetrievalResult, error) {
			mu.Lock()
			defer mu.Unlock()
			callIdx++
			if callIdx == 1 {
				return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
			}
			return []common.RetrievalResult{{Text: "Result 2", Score: 0.8}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": false, "next_question": "rewritten query"}`, nil
		},
	}
	r, err := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(2))
	require.NoError(t, err)
	results, err := r.Retrieve(context.Background(), "test query", 5, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	// RRF text 去重：两轮两个不同 text → 2 条
	assert.Len(t, results, 2)
}

// TestRetrieve_topK无效 对齐 test_retrieve_without_valid_top_k +
// _negative（Python None 场景 Go 编译期消解，spec §8-3）。
func TestRetrieve_topK无效(t *testing.T) {
	r := newGenericRetriever(t, &agenticLLMDispatch{})
	for _, topK := range []int{0, -1} {
		_, err := r.Retrieve(context.Background(), "test query", topK, nil, "")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "top_k is invalid, must be a positive integer")
	}
}

// TestRetrieve_模式透传 对齐 test_retrieve_with_custom_mode
// （桩 lastMode/lastTopK 断言，Python call_kwargs 对应物）。
func TestRetrieve_模式透传(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	_, err = r.Retrieve(context.Background(), "test query", 5, nil, RetrievalModeVector)
	require.NoError(t, err)
	fr.mu.Lock()
	assert.Equal(t, RetrievalModeVector, fr.lastMode)
	assert.Equal(t, 5, fr.lastTopK)
	fr.mu.Unlock()
}

// TestRetrieve_阈值透传 对齐 test_retrieve_with_score_threshold。
func TestRetrieve_阈值透传(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	threshold := 0.8
	_, err = r.Retrieve(context.Background(), "test query", 5, &threshold, "")
	require.NoError(t, err)
	fr.mu.Lock()
	require.NotNil(t, fr.lastThreshold)
	assert.Equal(t, 0.8, *fr.lastThreshold)
	fr.mu.Unlock()
}

// TestBatchRetrieve 对齐 test_batch_retrieve（双 query 各自走完循环，
// LLM 按 prompt 内容分派与调度序解耦——13.17 假失败教训）。
func TestBatchRetrieve(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	resultsList, err := r.BatchRetrieve(context.Background(), []string{"query 1", "query 2"}, 5)
	require.NoError(t, err)
	assert.Len(t, resultsList, 2)
	assert.Len(t, resultsList[0], 1)
	assert.Len(t, resultsList[1], 1)
}
