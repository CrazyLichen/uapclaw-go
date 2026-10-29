package retriever

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

// ──────────────────────────── 图路径测试 ────────────────────────────

// newGraphSetup 图路径脚手架：真 GraphRetriever + 双桩（chunk/triple）+ LLM。
//
// 链路可控性（spec §6.1/Q6 拍板）：GetRetrieverForMode 固定优先返回桩；
// GraphExpansion 真方法在「proximal 空 → linked 空 → fetchTriples 空 →
// 返回原 chunks」路径行为确定（beam search 深路径归 13.23 的 48 测试，
// 13.25 只测外壳编排）。
func newGraphSetup(t *testing.T, llm *agenticLLMDispatch) (*AgenticRetriever, *agenticFakeRetriever, *agenticFakeRetriever) {
	t.Helper()
	chunk := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			return nil, nil
		},
	}
	triple := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return nil, nil
		},
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			return nil, nil
		},
	}
	gr := NewGraphRetriever(
		WithGraphChunkRetriever(chunk),
		WithGraphTripleRetriever(triple),
	)
	r, err := NewAgenticRetriever(gr, llm)
	require.NoError(t, err)
	return r, chunk, triple
}

// TestRetrieve_图路径单轮 对齐 test_retrieve_with_graph_single_iteration。
func TestRetrieve_图路径单轮(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r, _, _ := newGraphSetup(t, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
}

// TestRetrieve_图路径多轮 对齐 test_retrieve_with_graph_multiple_iterations
// （chunk 桩 calls==2 断言，Python mock_chunk_retriever.retrieve.call_count）。
func TestRetrieve_图路径多轮(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": false, "next_question": "rewritten query 1"}`, nil
		},
	}
	r, chunk, _ := newGraphSetup(t, llm)
	results, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1)
	chunk.mu.Lock()
	assert.Equal(t, 2, chunk.calls)
	chunk.mu.Unlock()
}

// TestRetrieve_图路径graphExpansion开关 Go 增强 7：nil（默认 true）/
// 显式 false 两路。true 时 proximal _read 非空 → _linkTriples 走 rsr；
// false 时跳过整段 → triple 桩 rsrCalls==0。
func TestRetrieve_图路径graphExpansion开关(t *testing.T) {
	tripleOut := `[["Neville A. Stanton", "employer", "University of Southampton"]]`
	for _, tc := range []struct {
		name      string
		expansion *bool
		wantRsr   int
	}{
		{"未设默认true", nil, 1},
		{"显式false", func() *bool { b := false; return &b }(), 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			readCount := 0
			llm := &agenticLLMDispatch{
				readFn: func(string) (string, error) {
					readCount++
					if readCount == 1 {
						return tripleOut, nil // proximal read
					}
					return "[]", nil // 主 read
				},
				rewriteFn: func(string) (string, error) {
					return `{"sufficient": true, "next_question": null}`, nil
				},
			}
			r, _, triple := newGraphSetup(t, llm)
			var opts []RetrieveOption
			if tc.expansion != nil {
				opts = append(opts, WithRetrieveGraphExpansion(*tc.expansion))
			}
			_, err := r.Retrieve(context.Background(), "original query", 5, nil, "", opts...)
			require.NoError(t, err)
			triple.mu.Lock()
			assert.Equal(t, tc.wantRsr, triple.rsrCalls)
			triple.mu.Unlock()
		})
	}
}

// TestClose 对齐 test_close 三态（Go 接口编译期消解，1 例断言委托）。
func TestClose(t *testing.T) {
	fr := &agenticFakeRetriever{}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
	require.NoError(t, err)
	r.Close()
	fr.mu.Lock()
	assert.Equal(t, 1, fr.closed)
	fr.mu.Unlock()
}

// TestLinkTriples_部分失败聚合 Go 增强 5：rsr 桩混排 error/空列表/正常
// （同 ID 去重 + metadata 映射 doc_id/chunk_id + 非 string 收窄空串）。
func TestLinkTriples_部分失败聚合(t *testing.T) {
	idx := 0
	var mu sync.Mutex
	triple := &agenticFakeRetriever{
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			mu.Lock()
			defer mu.Unlock()
			idx++
			switch idx {
			case 1:
				return nil, errors.New("boom")
			case 2:
				return []common.SearchResult{}, nil
			default:
				return []common.SearchResult{
					{ID: "t1", Text: "(a b c)", Score: 0.9, Metadata: map[string]any{"doc_id": "d1", "chunk_id": "c1"}},
					{ID: "t1", Text: "(a b c) dup", Score: 0.8}, // 同 ID 去重
				}, nil
			}
		},
	}
	gr := NewGraphRetriever(WithGraphTripleRetriever(triple))
	r, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	results, err := r.linkTriples(context.Background(), []common.TripleFact{{"a", "b", "c"}, {"x", "y", "z"}, {"p", "q", "r"}}, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 1) // 3 输入：1 失败 + 1 空 + 1 成功（同 ID 去重后 1 条）
	assert.Equal(t, "d1", results[0].DocID)
	assert.Equal(t, "c1", results[0].ChunkID)
}

// TestLinkTriples_非string元数据收窄 Go 增强：doc_id 非 string → 空串
// （偏差 §8-7）。
func TestLinkTriples_非string元数据收窄(t *testing.T) {
	triple := &agenticFakeRetriever{
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			return []common.SearchResult{
				{ID: "t1", Text: "(a b c)", Score: 0.9, Metadata: map[string]any{"doc_id": 42}},
			}, nil
		},
	}
	gr := NewGraphRetriever(WithGraphTripleRetriever(triple))
	r, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	results, err := r.linkTriples(context.Background(), []common.TripleFact{{"a", "b", "c"}}, RetrievalModeHybrid)
	require.NoError(t, err)
	require.Len(t, results, 1)
	assert.Equal(t, "", results[0].DocID) // 非 string 收窄空串
}

// TestLinkPassages_失败聚合 Go 增强 6：Retrieve 桩混排 error/正常。
func TestLinkPassages_失败聚合(t *testing.T) {
	idx := 0
	var mu sync.Mutex
	chunk := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			mu.Lock()
			defer mu.Unlock()
			idx++
			if idx == 1 {
				return nil, errors.New("boom")
			}
			return []common.RetrievalResult{{Text: "passage", Score: 0.5}}, nil
		},
	}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunk))
	r, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	results, err := r.linkPassages(context.Background(), []common.TripleFact{{"a", "b", "c"}, {"x", "y", "z"}}, RetrievalModeHybrid)
	require.NoError(t, err)
	assert.Len(t, results, 1) // 1 失败 + 1 成功
	assert.Equal(t, "passage", results[0][0].Text)
}

// ──────────────────────────── Go 增强测试 ────────────────────────────

// TestRead_pyStr数字边界 增强 2：jsonrepair 产出数字一律 json.Number
// （探针实测：1862/0.5/大整数均原样字面），bool/nil 直达 pyStr 分支。
func TestRead_pyStr数字边界(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) {
			// 1862 数字、true bool、null → None、长度≠3 过滤、非 list 项过滤
			return `[["a", "b", 1862], ["x", "y", true], ["p", "q", null], ["short"], "notlist"]`, nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	results := r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}}, nil)
	require.Len(t, results, 3)
	assert.Equal(t, "1862", results[0][2]) // json.Number "1862" → Atoi 成功原样
	assert.Equal(t, "True", results[1][2]) // bool true → "True"
	assert.Equal(t, "None", results[2][2]) // nil → "None"
}

// TestRead_pyStr浮点与大整数 增强 2 附带：json.Number 浮点走 pyFloatStr、
// 超界整数字面原样透传（isPureIntegerLiteral）。
func TestRead_pyStr浮点与大整数(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) {
			return `[["a", "b", 0.5], ["x", "y", 123456789012345678901234567890]]`, nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	results := r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}}, nil)
	require.Len(t, results, 2)
	assert.Equal(t, "0.5", results[0][2])                            // json.Number "0.5" → Atoi 失败非整数字面 → Float64 → pyFloatStr("0.5")
	assert.Equal(t, "123456789012345678901234567890", results[1][2]) // 超界整数字面原样（Python int 无界）
}

// TestRead_facts串形态 增强 3：tuple repr + None 兜底（facts 进 prompt）。
func TestRead_facts串形态(t *testing.T) {
	var capturedPrompt string
	llm := &agenticLLMDispatch{
		readFn: func(prompt string) (string, error) {
			capturedPrompt = prompt
			return "[]", nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}},
		[]common.TripleFact{{"s", "p", "o"}, {"S2", "P2", "O2"}})
	assert.Contains(t, capturedPrompt, "('s', 'p', 'o'), ('S2', 'P2', 'O2')")
	// 空时兜底 None
	r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}}, nil)
	assert.Contains(t, capturedPrompt, "Facts: None")
}

// TestRead_docs截断语义 增强 3 附带：先切前 5 再滤空（Python
// `passages[:5] if p.text`——索引 [0,5) 截断后才过滤）。
func TestRead_docs截断语义(t *testing.T) {
	var capturedPrompt string
	llm := &agenticLLMDispatch{
		readFn:    func(prompt string) (string, error) { capturedPrompt = prompt; return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	passages := []common.RetrievalResult{
		{Text: "p1"}, {Text: ""}, {Text: "p2"}, {Text: "p3"}, {Text: "p4"}, {Text: "p5"}, {Text: "p6"},
	}
	r.read(context.Background(), "q", passages, nil)
	assert.Contains(t, capturedPrompt, "p1")
	assert.NotContains(t, capturedPrompt, "p6") // 索引 6 超出前 5 截断（即使非空）
	assert.NotContains(t, capturedPrompt, "p5") // 索引 5 也在截断界外——先切前 5 再滤空
	assert.Contains(t, capturedPrompt, "p2")    // 空文本 p_ 被跳过但 p2 索引 2 在界内
}

// TestRetrieve_软失败链 增强 4：LLM err→降级继续；解析根非 list/dict 降级。
func TestRetrieve_软失败链(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "", errors.New("llm down") },
		rewriteFn: func(string) (string, error) { return "", errors.New("llm down") },
	}
	r, err := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(2))
	require.NoError(t, err)
	// LLM err → _read 空 → 循环继续 → RRF 结果
	results, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// _read 解析根非 list
	llm.mu.Lock()
	llm.readFn = func(string) (string, error) { return `{"not": "a list"}`, nil }
	llm.mu.Unlock()
	results, err = r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)

	// _rewrite 解析根非 dict
	llm.mu.Lock()
	llm.readFn = func(string) (string, error) { return "[]", nil }
	llm.rewriteFn = func(string) (string, error) { return `["not", "dict"]`, nil }
	llm.mu.Unlock()
	results, err = r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1)
}

// TestRetrieve_maxIter为1 增强 8：单轮无 rewrite 调用。
func TestRetrieve_maxIter为1(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			t.Error("max_iter=1 不应调用 rewrite")
			return "", errors.New("unreachable")
		},
	}
	r := newGenericRetriever(t, llm)
	r.maxIter = 1
	_, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	llm.mu.Lock()
	assert.Equal(t, 1, llm.readCalls)
	assert.Equal(t, 0, llm.rewriteCalls)
	llm.mu.Unlock()
}

// TestRetrieve_RRF分数累加 增强 9：两轮同 text 去重保首见，分数为
// RRF 累加值（两轮 rank1：1/61+1/61）。
func TestRetrieve_RRF分数累加(t *testing.T) {
	callIdx := 0
	var mu sync.Mutex
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(_ context.Context, _ string, _ int, _ *float64, _ RetrievalMode) ([]common.RetrievalResult, error) {
			mu.Lock()
			defer mu.Unlock()
			callIdx++
			if callIdx == 1 {
				return []common.RetrievalResult{{Text: "same", Score: 0.9}}, nil
			}
			return []common.RetrievalResult{{Text: "same", Score: 0.8}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": false, "next_question": "q2"}`, nil },
	}
	r, err := NewAgenticRetriever(fr, llm, WithAgenticMaxIter(2))
	require.NoError(t, err)
	results, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	assert.Len(t, results, 1) // text 去重保首见
	assert.InDelta(t, 1.0/61+1.0/61, results[0].Score, 1e-9)
}

// TestBatchRetrieve_并发race 增强 10：-race 下多 query 并发 + LLM
// 分派桩共享（mu 保护验证）。
func TestBatchRetrieve_并发race(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return []common.RetrievalResult{{Text: "r", Score: 0.9}}, nil
		},
	}
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	resultsList, err := r.BatchRetrieve(context.Background(), []string{"q1", "q2", "q3"}, 5)
	require.NoError(t, err)
	assert.Len(t, resultsList, 3)
	assert.Len(t, resultsList[0], 1)
}

// ──────────────────────────── 覆盖率补强测试 ────────────────────────────

// TestAccessors_回读 访问器零覆盖补强：Retriever_/LLM 返回构造注入原值。
func TestAccessors_回读(t *testing.T) {
	fr := &agenticFakeRetriever{indexType: "vector"}
	llm := &stubAgenticLLM{}
	r, err := NewAgenticRetriever(fr, llm)
	require.NoError(t, err)
	assert.Same(t, fr, r.Retriever_())
	assert.Same(t, llm, r.LLM())
}

// TestRetrieveSearchResults_未覆写 Python 未覆写继承基类空方法：
// 返回 nil,nil（13.19 base.go 接口注释预留语义）。
func TestRetrieveSearchResults_未覆写(t *testing.T) {
	r, err := NewAgenticRetriever(&agenticFakeRetriever{}, &stubAgenticLLM{})
	require.NoError(t, err)
	ret, err := r.RetrieveSearchResults(context.Background(), "q", 1, "")
	require.NoError(t, err)
	assert.Nil(t, ret)
}

// TestBatchRetrieve_底层错误传播 空 queries 桩（retrieveFn 未设）→ 首错
// 返回 nil,err（裸 errgroup 聚合语义）。
func TestBatchRetrieve_底层错误传播(t *testing.T) {
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return nil, errors.New("retriever down")
		},
	}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
	require.NoError(t, err)
	resultsList, err := r.BatchRetrieve(context.Background(), []string{"q1"}, 5)
	require.Error(t, err)
	assert.Nil(t, resultsList)
	assert.Contains(t, err.Error(), "retriever down")
}

// TestRetrieve_底层检索错误传播 retrieveGeneric/retrieveWithGraph 两路径的
// chunk 检索失败传播（图路径 GetRetrieverForMode 后 Retrieve 失败）。
func TestRetrieve_底层检索错误传播(t *testing.T) {
	// 通用路径
	fr := &agenticFakeRetriever{
		indexType: "hybrid",
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return nil, errors.New("generic down")
		},
	}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = r.Retrieve(context.Background(), "q", 5, nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "generic down")

	// 图路径：chunk 桩 Retrieve 失败传播
	chunk := &agenticFakeRetriever{
		retrieveFn: func(context.Context, string, int, *float64, RetrievalMode) ([]common.RetrievalResult, error) {
			return nil, errors.New("graph chunk down")
		},
		rsrFn: func(context.Context, string, int, RetrievalMode) ([]common.SearchResult, error) {
			return nil, nil
		},
	}
	gr := NewGraphRetriever(WithGraphChunkRetriever(chunk))
	rg, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = rg.Retrieve(context.Background(), "q", 5, nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "graph chunk down")
}

// TestAssertGraphRetriever_非图防御 155211 防御分支：Go 类型系统保证
// 正常分派不可达（偏差 §8-10），同包直改 isGraph 构造病态实例触发。
func TestAssertGraphRetriever_非图防御(t *testing.T) {
	fr := &agenticFakeRetriever{indexType: "hybrid"}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
	require.NoError(t, err)
	r.isGraph = true // 同包直改模拟病态分派（类型断言外的旁路）
	_, err = r.Retrieve(context.Background(), "q", 5, nil, "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Underlying retriever must be a GraphRetriever to link passages")
}

// TestLinkTriples_非图防御与工厂失败 linkTriples 直调的防御分支 +
// GetRetrieverForMode 失败传播。
func TestLinkTriples_非图防御与工厂失败(t *testing.T) {
	// 非图防御
	fr := &agenticFakeRetriever{indexType: "hybrid"}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = r.linkTriples(context.Background(), []common.TripleFact{{"a", "b", "c"}}, RetrievalModeHybrid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Underlying retriever must be a GraphRetriever to link passages")

	// 工厂失败：非法 mode（13.23 GetRetrieverForMode 的 mode 校验）
	gr := NewGraphRetriever()
	rg, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = rg.linkTriples(context.Background(), []common.TripleFact{{"a", "b", "c"}}, RetrievalMode("bad"))
	require.Error(t, err)
}

// TestLinkPassages_非图防御与工厂失败 linkPassages 直调的防御分支 +
// 工厂失败传播。
func TestLinkPassages_非图防御与工厂失败(t *testing.T) {
	fr := &agenticFakeRetriever{indexType: "hybrid"}
	r, err := NewAgenticRetriever(fr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = r.linkPassages(context.Background(), []common.TripleFact{{"a", "b", "c"}}, RetrievalModeHybrid)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "Underlying retriever must be a GraphRetriever to link passages")

	gr := NewGraphRetriever()
	rg, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = rg.linkPassages(context.Background(), []common.TripleFact{{"a", "b", "c"}}, RetrievalMode("bad"))
	require.Error(t, err)
}

// TestRetrieve_图路径工厂失败 图路径主循环内 GetRetrieverForMode 失败传播
// （13.23 mode 校验）。
func TestRetrieve_图路径工厂失败(t *testing.T) {
	gr := NewGraphRetriever()
	r, err := NewAgenticRetriever(gr, &stubAgenticLLM{})
	require.NoError(t, err)
	_, err = r.Retrieve(context.Background(), "q", 5, nil, RetrievalMode("bad"))
	require.Error(t, err)
}

// TestRead_pyStr字符串与纯整数字面 pyStr 的 string 分支 + json.Number
// 整数字面（isPureIntegerLiteral Atoi 溢出兜底）。
func TestRead_pyStr字符串与纯整数字面(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) {
			// 注：jsonrepair 对裸 +42 会吞成粘合串（实测），符号分支
			// 改由 TestPyStr_分支表 json.Number("+42") 直测覆盖
			return `[["a", "b", "plain string"], ["x", "y", 99999999999999999999]]`, nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true, "next_question": null}`, nil },
	}
	r := newGenericRetriever(t, llm)
	results := r.read(context.Background(), "q", []common.RetrievalResult{{Text: "doc", Score: 1}}, nil)
	require.Len(t, results, 2)
	assert.Equal(t, "plain string", results[0][2])
	assert.Equal(t, "99999999999999999999", results[1][2]) // 超界整数字面原样透传（isPureIntegerLiteral）
}

// TestPyStr_分支表 同包直调 pyStr 覆盖残余分支（float64/int/int64/
// json.Number 病态非数字/nil/bool）。
func TestPyStr_分支表(t *testing.T) {
	assert.Equal(t, "None", pyStr(nil))
	assert.Equal(t, "True", pyStr(true))
	assert.Equal(t, "False", pyStr(false))
	assert.Equal(t, "abc", pyStr("abc"))
	assert.Equal(t, "42", pyStr(42))                  // int
	assert.Equal(t, "-7", pyStr(int64(-7)))           // int64
	assert.Equal(t, "+42", pyStr(json.Number("+42"))) // json.Number 符号整数 Atoi 成功原样
	assert.Equal(t, "0.5", pyStr(0.5))                // float64 → pyFloatStr
	assert.Equal(t, "2.0", pyStr(2.0))                // float64 整数值补 .0
	// json.Number 病态非数字（Repair 后仍非数字字面）
	assert.Equal(t, "abc", pyStr(json.Number("abc")))
	// json.Number 溢出整数（Atoi 失败 → isPureIntegerLiteral 命中）
	assert.Equal(t, "99999999999999999999", pyStr(json.Number("99999999999999999999")))
	// json.Number 病态：非纯整数字面也非合法 float（Float64 失败原样）
	assert.Equal(t, "1.2.3", pyStr(json.Number("1.2.3")))
}

// TestPyFloatStr_边界表 同包直调 pyFloatStr：nan/inf/科学记法/补 .0。
func TestPyFloatStr_边界表(t *testing.T) {
	assert.Equal(t, "nan", pyFloatStr(math.NaN()))
	assert.Equal(t, "inf", pyFloatStr(math.Inf(1)))
	assert.Equal(t, "-inf", pyFloatStr(math.Inf(-1)))
	assert.Equal(t, "1e+16", pyFloatStr(1e16))    // e 记法上界
	assert.Equal(t, "1e-05", pyFloatStr(1e-5))    // e 记法下界
	assert.Equal(t, "0.0", pyFloatStr(0.0))       // 零 f 记法补 .0
	assert.Equal(t, "0.0001", pyFloatStr(1e-4))   // f 记法下界
	assert.Equal(t, "1862.0", pyFloatStr(1862.0)) // f 记法整数值补 .0
}

// TestIsPureIntegerLiteral_边界表 同包直调：符号/非数字/空串边界。
func TestIsPureIntegerLiteral_边界表(t *testing.T) {
	assert.True(t, isPureIntegerLiteral("123"))
	assert.True(t, isPureIntegerLiteral("+123"))
	assert.True(t, isPureIntegerLiteral("-123"))
	assert.False(t, isPureIntegerLiteral(""))
	assert.False(t, isPureIntegerLiteral("+"))
	assert.False(t, isPureIntegerLiteral("-"))
	assert.False(t, isPureIntegerLiteral("1.5"))
	assert.False(t, isPureIntegerLiteral("1e5"))
	assert.False(t, isPureIntegerLiteral("12a"))
}

// TestPyTruthy_分支表 同包直调：nil/bool/数值/json.Number/其余 truthy。
func TestPyTruthy_分支表(t *testing.T) {
	assert.False(t, pyTruthy(nil))
	assert.True(t, pyTruthy(true))
	assert.False(t, pyTruthy(false))
	assert.True(t, pyTruthy(1.0))
	assert.False(t, pyTruthy(0.0))
	assert.True(t, pyTruthy(json.Number("1")))
	assert.False(t, pyTruthy(json.Number("0")))
	assert.True(t, pyTruthy("yes")) // string 非空 truthy
	assert.False(t, pyTruthy(""))
	assert.True(t, pyTruthy([]any{1})) // 容器非空兜底
}

// TestRewrite_历史渲染与nextQuestion形态 同包直调 rewrite：多轮历史
// 行渲染 + sufficient=false + next_question 空 → break（空串）。
func TestRewrite_历史渲染与nextQuestion形态(t *testing.T) {
	var capturedPrompt string
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(prompt string) (string, error) {
			capturedPrompt = prompt
			return `{"sufficient": false, "next_question": "next q"}`, nil
		},
	}
	r := newGenericRetriever(t, llm)
	// 双元素历史：Rewritten Question 1 渲染
	got := r.rewrite(context.Background(), "orig", "t1, t2", []string{"orig", "r1"})
	assert.Equal(t, "next q", got)
	assert.Contains(t, capturedPrompt, "Rewritten Question 1: r1")

	// sufficient=true → 空串
	llm.rewriteFn = func(string) (string, error) {
		return `{"sufficient": true, "next_question": "ignored"}`, nil
	}
	got = r.rewrite(context.Background(), "orig", "", []string{"orig"})
	assert.Empty(t, got)

	// sufficient=false + next_question 空 → 空串
	llm.rewriteFn = func(string) (string, error) {
		return `{"sufficient": false, "next_question": ""}`, nil
	}
	got = r.rewrite(context.Background(), "orig", "", []string{"orig"})
	assert.Empty(t, got)

	// next_question 非 string（数字）→ 收窄空串（偏差 §8-6）
	llm.rewriteFn = func(string) (string, error) {
		return `{"sufficient": false, "next_question": 123}`, nil
	}
	got = r.rewrite(context.Background(), "orig", "", []string{"orig"})
	assert.Empty(t, got)

	// LLM err → 软失败空串
	llm.rewriteFn = func(string) (string, error) { return "", errors.New("down") }
	got = r.rewrite(context.Background(), "orig", "", []string{"orig"})
	assert.Empty(t, got)
}

// TestRead_LLM失败与修复失败 read 软失败三分支：Invoke err / jsonrepair
// err / 根非 list。
func TestRead_LLM失败与修复失败(t *testing.T) {
	// Invoke err
	llm := &agenticLLMDispatch{
		readFn:    func(string) (string, error) { return "", errors.New("down") },
		rewriteFn: func(string) (string, error) { return `{"sufficient": true}`, nil },
	}
	r := newGenericRetriever(t, llm)
	assert.Empty(t, r.read(context.Background(), "q", []common.RetrievalResult{{Text: "d"}}, nil))

	// jsonrepair err：损坏到无法修复的串
	llm.readFn = func(string) (string, error) { return `{{{`, nil }
	assert.Empty(t, r.read(context.Background(), "q", []common.RetrievalResult{{Text: "d"}}, nil))
}

// TestRetrieve_图路径rewrite中断 图路径多轮循环中 rewrite 返回空串 →
// break（Python rewritten 变量未赋值分支同构）。
func TestRetrieve_图路径rewrite中断(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": true, "next_question": null}`, nil
		},
	}
	r, chunk, _ := newGraphSetup(t, llm)
	r.maxIter = 2
	_, err := r.Retrieve(context.Background(), "original query", 5, nil, "")
	require.NoError(t, err)
	chunk.mu.Lock()
	assert.Equal(t, 1, chunk.calls) // rewrite 返回空串 → 提前 break
	chunk.mu.Unlock()
}

// TestRetrieve_通用路径rewrite中断 通用路径同构验证。
func TestRetrieve_通用路径rewrite中断(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) { return "[]", nil },
		rewriteFn: func(string) (string, error) {
			return `{"sufficient": true, "next_question": null}`, nil
		},
	}
	r := newGenericRetriever(t, llm)
	r.maxIter = 3
	_, err := r.Retrieve(context.Background(), "q", 5, nil, "")
	require.NoError(t, err)
	fr := r.Retriever_().(*agenticFakeRetriever)
	fr.mu.Lock()
	assert.Equal(t, 1, fr.calls)
	fr.mu.Unlock()
}

// TestRead_jsonrepair空串 read 空串内容（Repair 返回非 list）。
func TestRead_jsonrepair空串(t *testing.T) {
	llm := &agenticLLMDispatch{
		readFn: func(string) (string, error) {
			return `{"still": "not a list"}`, nil
		},
		rewriteFn: func(string) (string, error) { return `{"sufficient": true}`, nil },
	}
	r := newGenericRetriever(t, llm)
	assert.Empty(t, r.read(context.Background(), "q", []common.RetrievalResult{{Text: "d"}}, nil))
}
