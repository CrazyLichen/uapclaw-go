package resource

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/knowledgebase"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner"
)

// ──────────────────────────── 测试夹具 ────────────────────────────

// newTestExecutable 最小构造（无会话依赖）
func newTestExecutable(cfg *KnowledgeRetrievalCompConfig) *KnowledgeRetrievalExecutable {
	e, _ := NewKnowledgeRetrievalExecutable(cfg)
	return e
}

// newBM25Config bm25 索引 + 本地 Chroma TempDir（真实链，无网络依赖）
func newBM25Config(chromaPath string) *KnowledgeRetrievalCompConfig {
	return &KnowledgeRetrievalCompConfig{
		ComponentKBConfigs: []ComponentKBConfig{{
			KBConfig: common.KnowledgeBaseConfig{
				KBID:      "kb-1",
				IndexType: common.IndexTypeBM25,
			},
			VectorStoreConfig: common.VectorStoreConfig{
				StoreProvider:  common.StoreTypeChroma,
				CollectionName: "col_bm25",
			},
		}},
		VectorStoreConnectionConfig: map[string]any{"chroma_path": chromaPath},
	}
}

// newVectorNoEmbedConfig vector 索引但无 embed 配置（101101 路径）
func newVectorNoEmbedConfig(chromaPath string) *KnowledgeRetrievalCompConfig {
	cfg := newBM25Config(chromaPath)
	cfg.ComponentKBConfigs[0].KBConfig.IndexType = common.IndexTypeVector
	return cfg
}

// stubLLMClient 网络边界替身（真实链纪律：仅协议边界允许；agentic 检索器装配验证用）
type stubLLMClient struct{}

func (s *stubLLMClient) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return nil, nil
}
func (s *stubLLMClient) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, nil
}
func (s *stubLLMClient) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, nil
}
func (s *stubLLMClient) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, nil
}
func (s *stubLLMClient) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, nil
}
func (s *stubLLMClient) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, nil
}
func (s *stubLLMClient) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubLLMClient) SupportsKVCacheRelease() bool     { return false }
func (s *stubLLMClient) SupportsKVCacheAffinity() bool    { return false }
func (s *stubLLMClient) BuildKVCacheAffinityInvokeKwargs(_ any, _, _ string, _ int) map[string]any {
	return nil
}

// ──────────────────────────── createLLMInstance ────────────────────────────

// Python _create_llm_instance :235-246

func TestCreateLLMInstance_agentic无模型配置返回101103(t *testing.T) {
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		RetrievalConfig: common.RetrievalConfig{Agentic: true},
	})
	_, err := e.createLLMInstance(context.Background())
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalLLMModelInitError)
}

func TestCreateLLMInstance_缺modelConfig返回101103(t *testing.T) {
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		RetrievalConfig:   common.RetrievalConfig{Agentic: true},
		ModelClientConfig: &llmschema.ModelClientConfig{},
	})
	_, err := e.createLLMInstance(context.Background())
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalLLMModelInitError)
}

func TestCreateLLMInstance_直构Model走适配器(t *testing.T) {
	// Python :242 Model(client_config, model_config)；D-25-5 适配能力面
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		RetrievalConfig:   common.RetrievalConfig{Agentic: true},
		ModelClientConfig: &llmschema.ModelClientConfig{ClientID: "test-client", ClientProvider: "llm_OpenAI", APIKey: "test-key", APIBase: "https://open.bigmodel.cn/api/paas/v4"},
		ModelConfig:       &llmschema.ModelRequestConfig{},
	})
	m, err := e.createLLMInstance(context.Background())
	require.NoError(t, err)
	assert.NotNil(t, m)
}

func TestCreateLLMInstance_modelID走ResourceMgr(t *testing.T) {
	// Python :244-246 Runner.resource_mgr.get_model(id=...)；D-25-1 单元素收窄
	mgr := runner.GetResourceMgr()
	require.NotNil(t, mgr)
	require.NoError(t, mgr.AddModel("glm-4.5-flash", func(_ context.Context, _ string) (model_clients.BaseModelClient, error) {
		return &stubLLMClient{}, nil
	}))

	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		RetrievalConfig: common.RetrievalConfig{Agentic: true},
		ModelID:         "glm-4.5-flash",
	})
	m, err := e.createLLMInstance(context.Background())
	require.NoError(t, err)
	var _ model_clients.BaseModelClient = m
}

func TestCreateLLMInstance_modelID未注册报错(t *testing.T) {
	e := newTestExecutable(&KnowledgeRetrievalCompConfig{
		RetrievalConfig: common.RetrievalConfig{Agentic: true},
		ModelID:         "not-exist-model",
	})
	_, err := e.createLLMInstance(context.Background())
	require.Error(t, err)
}

// ──────────────────────────── createKnowledgeBases ────────────────────────────

// Python _create_knowledge_bases :185-233

func TestCreateKnowledgeBases_vector无embedConfig返回101101(t *testing.T) {
	e := newTestExecutable(newVectorNoEmbedConfig(t.TempDir()))
	_, err := e.createKnowledgeBases(context.Background())
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalEmbedModelInitError)
}

func TestCreateKnowledgeBases_hybrid同样要求embed(t *testing.T) {
	cfg := newVectorNoEmbedConfig(t.TempDir())
	cfg.ComponentKBConfigs[0].KBConfig.IndexType = common.IndexTypeHybrid
	e := newTestExecutable(cfg)
	_, err := e.createKnowledgeBases(context.Background())
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalEmbedModelInitError)
}

func TestCreateKnowledgeBases_bm25无需embed构造SimpleKB(t *testing.T) {
	e := newTestExecutable(newBM25Config(t.TempDir()))
	kbs, err := e.createKnowledgeBases(context.Background())
	require.NoError(t, err)
	require.Len(t, kbs, 1)
	_, ok := kbs[0].(*knowledgebase.SimpleKnowledgeBase)
	assert.True(t, ok, "bm25 + use_graph=false 应构造 SimpleKnowledgeBase")
}

func TestCreateKnowledgeBases_useGraph构造GraphKB(t *testing.T) {
	cfg := newBM25Config(t.TempDir())
	useGraph := true
	cfg.RetrievalConfig.UseGraph = &useGraph
	e := newTestExecutable(cfg)
	kbs, err := e.createKnowledgeBases(context.Background())
	require.NoError(t, err)
	require.Len(t, kbs, 1)
	_, ok := kbs[0].(*knowledgebase.GraphKnowledgeBase)
	assert.True(t, ok, "use_graph=true 应构造 GraphKnowledgeBase")
}

func TestCreateKnowledgeBases_agentic只建基础检索器包装由KB层完成(t *testing.T) {
	// Python :175 先建 llm 再建 KB；agentic 包装发生在 KB Retrieve 时
	// （simple_kb.go:142 / graph_kb.go:192），组件只传 WithLLMClient（D-25-2 修正）
	cfg := newBM25Config(t.TempDir())
	cfg.RetrievalConfig.Agentic = true
	e := newTestExecutable(cfg)
	e.llm = &stubLLMClient{}
	kbs, err := e.createKnowledgeBases(context.Background())
	require.NoError(t, err)
	require.Len(t, kbs, 1)
	skb := kbs[0].(*knowledgebase.SimpleKnowledgeBase)
	// 组件 buildRetriever 产物为基础检索器（bm25 → SparseRetriever）
	r, err := e.buildRetriever(&cfg.ComponentKBConfigs[0], skb.VectorStore(), nil)
	require.NoError(t, err)
	_, ok := r.(*retriever.SparseRetriever)
	assert.True(t, ok, "组件侧应为基础检索器，agentic 包装由 KB 层完成")
}

func TestCreateKnowledgeBases_未知provider透传错误(t *testing.T) {
	cfg := newBM25Config(t.TempDir())
	cfg.ComponentKBConfigs[0].VectorStoreConfig.StoreProvider = "unknown"
	e := newTestExecutable(cfg)
	_, err := e.createKnowledgeBases(context.Background())
	require.Error(t, err)
}

// ──────────────────────────── initializeIfNeeded ────────────────────────────

// Python _initialize_if_needed :167-183

func TestInitializeIfNeeded_并发只初始化一次(t *testing.T) {
	e := newTestExecutable(newBM25Config(t.TempDir()))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = e.initializeIfNeeded(context.Background())
		}()
	}
	wg.Wait()
	assert.True(t, e.initialized.Load())
	assert.Len(t, e.kbs, 1)
}

func TestInitializeIfNeeded_已初始化直接返回(t *testing.T) {
	e := newTestExecutable(newBM25Config(t.TempDir()))
	require.NoError(t, e.initializeIfNeeded(context.Background()))
	// 二次调用短路（Python :169-170）
	e.kbs = nil
	require.NoError(t, e.initializeIfNeeded(context.Background()))
	assert.Nil(t, e.kbs, "initialized=true 应短路不重建")
}

func TestInitializeIfNeeded_失败保持未初始化可重试(t *testing.T) {
	// Python :178-183 失败 wrap 101100；initialized 保持 false
	e := newTestExecutable(newVectorNoEmbedConfig(t.TempDir()))
	err := e.initializeIfNeeded(context.Background())
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInvokeCallFailed)
	assert.False(t, e.initialized.Load())

	// 修复配置后重试成功（可重试语义）
	e.cfg = newBM25Config(t.TempDir())
	require.NoError(t, e.initializeIfNeeded(context.Background()))
	assert.True(t, e.initialized.Load())
}

func TestInitializeIfNeeded_agentic模型失败传播(t *testing.T) {
	cfg := newBM25Config(t.TempDir())
	cfg.RetrievalConfig.Agentic = true // 无模型配置 → 101103 → wrap 101100
	e := newTestExecutable(cfg)
	err := e.initializeIfNeeded(context.Background())
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInvokeCallFailed)
}

// ──────────────────────────── invokeEntry / traceMetadata ────────────────────────────

// Python invoke :80-143 / trace_metadata :148-165

func TestInvokeEntry_空query返回101102(t *testing.T) {
	// Python :88-92 query.strip() 为空 → 101102
	e := newTestExecutable(newBM25Config(t.TempDir()))
	e.sess = nil
	_, err := e.invokeEntry(context.Background(), map[string]any{"query": "   "}, nil, nil)
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInputParamError)
}

func TestInvokeEntry_初始化失败直接传播101100(t *testing.T) {
	e := newTestExecutable(newVectorNoEmbedConfig(t.TempDir()))
	_, err := e.invokeEntry(context.Background(), map[string]any{"query": "q"}, nil, nil)
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInvokeCallFailed)
}

func TestTraceMetadata_非agentic无requestModel(t *testing.T) {
	e := newTestExecutable(newBM25Config(t.TempDir()))
	md := e.traceMetadata()
	assert.Equal(t, 0, md["top_k"]) // RetrievalConfig 零值 TopK=0（组件不补默认，对齐 Python 直接读 config）
	assert.Equal(t, []string{"kb-1"}, md["data_source_ids"])
	_, ok := md["request_model"]
	assert.False(t, ok)
}

func TestTraceMetadata_agentic且有modelId带requestModel(t *testing.T) {
	cfg := newBM25Config(t.TempDir())
	cfg.RetrievalConfig.Agentic = true
	cfg.ModelID = "glm-4.5-flash"
	e := newTestExecutable(cfg)
	md := e.traceMetadata()
	assert.Equal(t, "glm-4.5-flash", md["request_model"])
}

// ──────────────────────────── storeOptionsFromConnection ────────────────────────────

func TestStoreOptionsFromConnection_键映射(t *testing.T) {
	opts := storeOptionsFromConnection(map[string]any{
		"chroma_path": "/tmp/c",
		"milvus_uri":  "http://localhost:19530",
		"text_field":  "txt",
		"unknown_key": 1, // 未知键忽略（Python **kwargs 会透传，Go 侧仅映射已支持键——D-25-6）
	})
	assert.Len(t, opts, 3)
}

// ──────────────────────────── invoke 成功链（边界替身 + 真实 KB 管线） ────────────────────────────

// newBM25KBWithStubStore 真实 SimpleKnowledgeBase + SparseRetriever 管线，仅存储边界替身
func newBM25KBWithStubStore(store *stubVectorStore) knowledgebase.KnowledgeBase {
	kb, err := knowledgebase.NewSimpleKnowledgeBase(
		common.KnowledgeBaseConfig{KBID: "kb-1", IndexType: common.IndexTypeBM25},
		retriever.NewSparseRetriever(store),
		knowledgebase.WithVectorStore(store),
	)
	if err != nil {
		panic(err)
	}
	return kb
}

func TestInvokeEntry_检索成功返回results与context(t *testing.T) {
	store := &stubVectorStore{results: []common.SearchResult{
		{ID: "d1", Text: "alpha doc", Score: 0.9},
		{ID: "d2", Text: "beta doc", Score: 0.8},
	}}
	e := newTestExecutable(newBM25Config(t.TempDir()))
	e.kbs = []knowledgebase.KnowledgeBase{newBM25KBWithStubStore(store)}
	e.initialized.Store(true)
	sess := newTestNodeSession(t)

	out, err := e.invokeEntry(context.Background(), map[string]any{"query": "alpha"}, sess, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{"alpha doc", "beta doc"}, out["results"])
	assert.Equal(t, "alpha doc\n\nbeta doc", out["context"])
}

func TestInvokeEntry_单KB失败吞错留空(t *testing.T) {
	// Python retrieve_multi_kb_with_source 单库失败只留空（multi_kb.go gather 语义）；
	// invoke 的 except 101100 仅拦截管线级错误（Python :114-127 防御分支，保留）
	store := &stubVectorStore{err: assert.AnError}
	e := newTestExecutable(newBM25Config(t.TempDir()))
	e.kbs = []knowledgebase.KnowledgeBase{newBM25KBWithStubStore(store)}
	e.initialized.Store(true)
	sess := newTestNodeSession(t)

	out, err := e.invokeEntry(context.Background(), map[string]any{"query": "q"}, sess, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, out["results"])
	assert.Equal(t, "", out["context"])
}

func TestInvokeEntry_无KB返回空results(t *testing.T) {
	// RetrieveMultiKBWithSource: len(kbs)==0 → 空结果（Python retrieve_multi_kb_with_source 同语义）
	e := newTestExecutable(newBM25Config(t.TempDir()))
	e.kbs = []knowledgebase.KnowledgeBase{}
	e.initialized.Store(true)
	sess := newTestNodeSession(t)

	out, err := e.invokeEntry(context.Background(), map[string]any{"query": "q"}, sess, nil)
	require.NoError(t, err)
	assert.Equal(t, []string{}, out["results"])
	assert.Equal(t, "", out["context"])
}

func TestSetSession_记录会话(t *testing.T) {
	// Python _set_session :145-146
	e := newTestExecutable(newBM25Config(t.TempDir()))
	sess := newTestNodeSession(t)
	e.setSession(sess)
	assert.Same(t, sess, e.sess)
}

// ──────────────────────────── 补分支覆盖 ────────────────────────────

func TestNewKnowledgeRetrievalComponent_槽注入与nil配置(t *testing.T) {
	_, err := NewKnowledgeRetrievalComponent(nil)
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInputParamError)

	comp, err := NewKnowledgeRetrievalComponent(newBM25Config(t.TempDir()))
	require.NoError(t, err)
	assert.NotNil(t, comp.ToExecutable(), "ToExecutableFn 槽应注入执行体")
	assert.NotNil(t, comp.TraceMetadataFn)
}

func TestBuildRetriever_三分支(t *testing.T) {
	e := newTestExecutable(newBM25Config(t.TempDir()))
	vs := &stubVectorStore{}
	kbCfg := &ComponentKBConfig{}

	kbCfg.KBConfig.IndexType = common.IndexTypeBM25
	r, err := e.buildRetriever(kbCfg, vs, nil)
	require.NoError(t, err)
	assert.IsType(t, &retriever.SparseRetriever{}, r)

	kbCfg.KBConfig.IndexType = common.IndexTypeHybrid
	r, err = e.buildRetriever(kbCfg, vs, nil)
	require.NoError(t, err)
	assert.IsType(t, &retriever.HybridRetriever{}, r)

	kbCfg.KBConfig.IndexType = common.IndexTypeVector
	r, err = e.buildRetriever(kbCfg, vs, nil)
	require.NoError(t, err)
	assert.IsType(t, &retriever.VectorRetriever{}, r)
}

func TestInvokeEntry_validate失败路径(t *testing.T) {
	// Python :86 validate_inputs 失败 → 101102（经 invoke 传播）
	e := newTestExecutable(newBM25Config(t.TempDir()))
	e.initialized.Store(true)
	sess := newTestNodeSession(t)
	_, err := e.invokeEntry(context.Background(), map[string]any{"query": 123}, sess, nil)
	assertKBCode(t, err, exception.StatusComponentKnowledgeRetrievalInputParamError)
}

func TestStoreOptionsFromConnection_全部键与类型不符忽略(t *testing.T) {
	opts := storeOptionsFromConnection(map[string]any{
		"chroma_path":        "/tmp/c",
		"milvus_uri":         "http://m",
		"milvus_token":       "tok",
		"vector_field_name":  "vec",
		"sparse_vector_field": "sp",
		"metadata_field":     "md",
		"doc_id_field":       "did",
		"chroma_path_bad":    1, // 类型不符忽略
	})
	assert.Len(t, opts, 7)
}
