package embedding

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/store/embedding"
)

func TestVLLMEmbedding_EmbedQuery(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"data": [{"embedding": [0.1, 0.2, 0.3], "index": 0}],
			"model": "Qwen3-VL-Embedding",
			"object": "list",
			"usage": {"prompt_tokens": 5, "total_tokens": 5}
		}`)
	}))
	defer server.Close()

	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "Qwen3-VL-Embedding",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})
	vllm := NewVLLMEmbedding(openAI)

	vec, err := vllm.EmbedQuery(context.Background(), "hello")
	require.NoError(t, err)
	assert.Len(t, vec, 3)
}

func TestVLLMEmbedding_EmbedDocuments(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"data": [{"embedding": [0.1, 0.2], "index": 0}, {"embedding": [0.3, 0.4], "index": 1}],
			"model": "Qwen3-VL-Embedding",
			"object": "list",
			"usage": {"prompt_tokens": 10, "total_tokens": 10}
		}`)
	}))
	defer server.Close()

	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "Qwen3-VL-Embedding",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})
	vllm := NewVLLMEmbedding(openAI)

	vecs, err := vllm.EmbedDocuments(context.Background(), []string{"hello", "world"})
	require.NoError(t, err)
	assert.Len(t, vecs, 2)
}

func TestVLLMEmbedding_Dimension(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"data": [{"embedding": [0.1, 0.2, 0.3, 0.4], "index": 0}],
			"model": "Qwen3-VL-Embedding",
			"object": "list",
			"usage": {"prompt_tokens": 5, "total_tokens": 5}
		}`)
	}))
	defer server.Close()

	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "Qwen3-VL-Embedding",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})
	vllm := NewVLLMEmbedding(openAI)

	dim := vllm.Dimension()
	assert.Equal(t, 4, dim)
}

func TestVLLMEmbedding_EmbedMultimodal(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"data": [{"embedding": [0.5, 0.6, 0.7], "index": 0}],
			"model": "Qwen3-VL-Embedding",
			"object": "list",
			"usage": {"prompt_tokens": 10, "total_tokens": 10}
		}`)
	}))
	defer server.Close()

	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "Qwen3-VL-Embedding",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})
	vllm := NewVLLMEmbedding(openAI)

	doc := common.NewMultimodalDocument().
		AddField(common.ModalityText, "描述").
		AddField(common.ModalityImage, "https://example.com/img.png")
	vec, err := vllm.EmbedMultimodal(context.Background(), doc)
	require.NoError(t, err)
	assert.Len(t, vec, 3)
	assert.InDelta(t, 0.5, vec[0], 0.001)
}

func TestVLLMEmbedding_EmbedMultimodal_nil文档(t *testing.T) {
	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})
	vllm := NewVLLMEmbedding(openAI)

	_, err := vllm.EmbedMultimodal(context.Background(), nil)
	assert.Error(t, err)
}

func TestVLLMEmbedding_EmbedMultimodal_自定义指令(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{
			"data": [{"embedding": [0.5, 0.6], "index": 0}],
			"model": "Qwen3-VL-Embedding",
			"object": "list",
			"usage": {"prompt_tokens": 10, "total_tokens": 10}
		}`)
	}))
	defer server.Close()

	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "Qwen3-VL-Embedding",
		BaseURL:   server.URL,
		APIKey:    "sk-test",
	})
	vllm := NewVLLMEmbedding(openAI)

	doc := common.NewMultimodalDocument().AddField(common.ModalityText, "测试")
	vec, err := vllm.EmbedMultimodal(context.Background(), doc, MultimodalOption{Instruction: "自定义指令"})
	require.NoError(t, err)
	assert.Len(t, vec, 2)
}

func TestVLLMEmbedding_接口约束(t *testing.T) {
	openAI := NewOpenAIEmbedding(EmbeddingConfig{
		ModelName: "test-model",
		BaseURL:   "http://localhost",
	})
	vllm := NewVLLMEmbedding(openAI)

	// 验证 VLLMEmbedding 满足 BaseEmbedding 接口
	var _ embedding.BaseEmbedding = vllm
	// 验证 VLLMEmbedding 满足 MultimodalEmbedder 接口
	var _ MultimodalEmbedder = vllm
}

func TestParseMultimodalInput(t *testing.T) {
	doc := common.NewMultimodalDocument().AddField(common.ModalityText, "描述").AddField(common.ModalityImage, "https://example.com/img.png")
	messages := parseMultimodalInput(doc, "测试指令")
	assert.Len(t, messages, 2)
	assert.Equal(t, "system", messages[0]["role"])
	assert.Equal(t, "user", messages[1]["role"])
}

func TestParseMultimodalInput_nil文档(t *testing.T) {
	messages := parseMultimodalInput(nil, "测试指令")
	assert.Nil(t, messages)
}
