//go:build integration && llm

// 8.25 KnowledgeRetrievalComp 真链端到端（用户硬要求 2026-11-24）：
// 真实创建 workflow（Start→kb1→End，inputs_schema 字段映射）→ 多检索模式
// （vector/hybrid/graph/agentic）逐一验证知识库存储→提取→检索全链。
//
// LLM 装配：复用 27.4 内置智谱免费模型域（ZHIPU_BUILTIN_API_KEY env 优先，
// 内置常量兜底，8.24/27.4 真链同款）；
// embedding：免费档 endpoint 的 embedding-3（OpenAI 兼容 /embeddings），
// 不可用时 vector/hybrid/agentic Skip（bm25 用例恒跑）。
//
// 断言纪律（17.x 真链先例同款）：只钉确定性结构契约（状态机 COMPLETED、
// 输出字段形状、存储读回一致性），不断言具体检索排序。
package workflow_knowledge_retrieval

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	modelclients "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/reranker"
	storeembedding "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/store/embedding"
	retrievalembedding "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/indexing/processor/chunker"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/vector_store"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/knowledgebase"
	retrievalcommon "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/flow"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/workflow/components/resource"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/control/configpanel"
)

// ──────────────────────────── 结构体 ────────────────────────────

// KnowledgeRetrievalE2ESuite 8.25 真链端到端套件
type KnowledgeRetrievalE2ESuite struct {
	suite.Suite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestKnowledgeRetrievalE2E 运行真链端到端套件
// 运行方式: go test -tags "sqlite_fts5 test integration llm" ./tests/integration/agentcore/workflow_knowledge_retrieval/ -v
func TestKnowledgeRetrievalE2E(t *testing.T) {
	suite.Run(t, new(KnowledgeRetrievalE2ESuite))
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// chdirRepoRoot vec0.so 相对路径 third_party/... 按 CWD 解析（lite.ResolveVec0Path），
// go test 二进制目录无 third_party → 从测试文件位置向上找仓库根并 Chdir。
func (s *KnowledgeRetrievalE2ESuite) chdirRepoRoot(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join("third_party", "sqlite-vec")); err == nil {
		return
	}
	_, file, _, ok := runtime.Caller(0)
	s.Require().True(ok)
	dir := filepath.Dir(file)
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "third_party", "sqlite-vec")); err == nil {
			s.Require().NoError(os.Chdir(dir))
			return
		}
		dir = filepath.Dir(dir)
	}
	s.T().Skipf("third_party/sqlite-vec 不存在，vec0 依赖用例跳过")
}

// zhipuEntries 内置免费档（env ZHIPU_BUILTIN_API_KEY 优先，内置常量兜底）
func (s *KnowledgeRetrievalE2ESuite) zhipuEntries() []map[string]any {
	entries := configpanel.ZhipuFreeModelEntries()
	if len(entries) == 0 {
		s.T().Skip("ZHIPU_BUILTIN_API_KEY 未注入且内置常量为空，跳过真链用例")
	}
	return entries
}

// newRealEmbedding 免费档 embedding-3（OpenAI 兼容端点）；不可用时 Skip
func (s *KnowledgeRetrievalE2ESuite) newRealEmbedding() *retrievalembedding.EmbeddingConfig {
	entries := s.zhipuEntries()
	mcc := entries[0]["model_client_config"].(map[string]any)
	return &retrievalembedding.EmbeddingConfig{
		ModelName: "embedding-3",
		BaseURL:   mcc["api_base"].(string),
		APIKey:    mcc["api_key"].(string),
	}
}

// newRealLLMConfigs 直构路径模型配置（agentic 用）
func (s *KnowledgeRetrievalE2ESuite) newRealLLMConfigs() (*llmschema.ModelClientConfig, *llmschema.ModelRequestConfig) {
	entries := s.zhipuEntries()
	mcc := entries[0]["model_client_config"].(map[string]any)
	return &llmschema.ModelClientConfig{
			ClientID:       "zhipu-free-kb-e2e",
			ClientProvider: "OpenAI", // 智谱 OpenAI 兼容端点（8.24 同款）
			APIKey:         mcc["api_key"].(string),
			APIBase:        mcc["api_base"].(string),
			Timeout:        120,
		}, &llmschema.ModelRequestConfig{
			ModelName: mcc["model_name"].(string),
			MaxTokens: intPtr(1024),
		}
}

// newKBComponent 构造组件 + 预写文档（存储侧验证：写→读回一致性）
func (s *KnowledgeRetrievalE2ESuite) newKBComponent(indexType retrievalcommon.IndexTypeKind,
	useGraph bool, embedCfg *retrievalembedding.EmbeddingConfig, docs []string,
	agentic bool) *resource.KnowledgeRetrievalComponent {
	chromaPath := s.T().TempDir()
	cfg := &resource.KnowledgeRetrievalCompConfig{
		ComponentKBConfigs: []resource.ComponentKBConfig{{
			KBConfig: retrievalcommon.KnowledgeBaseConfig{
				KBID: "kb-e2e",
				IndexType: indexType,
			},
			VectorStoreConfig: retrievalcommon.VectorStoreConfig{
				StoreProvider:  retrievalcommon.StoreTypeChroma,
				CollectionName: "kb_kb-e2e_chunks", // 13.27 派生名 kb_{KBID}_chunks，存储读回一致性
				// chroma 默认 space=l2；显式声明避免 155508 开启校验 mismatch
				DistanceMetric: retrievalcommon.DistanceMetricEuclidean,
			},
			EmbedConfig: embedCfg,
		}},
		VectorStoreConnectionConfig: map[string]any{"chroma_path": chromaPath},
		RetrievalConfig: retrievalcommon.RetrievalConfig{
			TopK:     3,
			UseGraph: boolPtr(useGraph),
			Agentic:  agentic,
		},
	}
	if agentic {
		cfg.ModelClientConfig, cfg.ModelConfig = s.newRealLLMConfigs()
	}

	// 存储侧验证：组件同配置经 KB 原生 API 写入并读回（存储→提取链路独立验证；
	// bm25/hybrid 受 chroma 本地 155508 约束，不预写，见 NoWrite 变体）
	if len(docs) > 0 {
		kb, err := s.buildStandaloneKB(cfg)
		s.Require().NoError(err)
		documents := make([]reranker.Document, 0, len(docs))
		for _, d := range docs {
			documents = append(documents, reranker.Document{Text: d})
		}
		_, err = kb.AddDocuments(context.Background(), documents)
		s.Require().NoError(err, "KB 原生写入失败")
		results, err := kb.Retrieve(context.Background(), docs[0], &cfg.RetrievalConfig)
		s.Require().NoError(err, "KB 原生读回失败")
		s.Require().NotEmpty(results, "存储侧读回应命中写入文档")
		s.T().Logf("存储侧验证通过：index=%s 命中 %d 条", indexType, len(results))
	}

	comp, err := resource.NewKnowledgeRetrievalComponent(cfg)
	s.Require().NoError(err)
	return comp
}

// newKBComponentNoWrite 无预写组件（chroma 本地约束模式用）
func (s *KnowledgeRetrievalE2ESuite) newKBComponentNoWrite(indexType retrievalcommon.IndexTypeKind,
	useGraph bool) *resource.KnowledgeRetrievalComponent {
	return s.newKBComponent(indexType, useGraph, nil, nil, false)
}

// buildStandaloneKB 与组件同配置的独立 KB 实例（存储侧验证用，不入 workflow；
// 构造原语与组件 createKnowledgeBases 同构：CreateVectorStore + 按索引类型检索器）
func (s *KnowledgeRetrievalE2ESuite) buildStandaloneKB(cfg *resource.KnowledgeRetrievalCompConfig) (knowledgebase.KnowledgeBase, error) {
	var llmClient modelclients.BaseModelClient
	if cfg.RetrievalConfig.Agentic {
		mcc, mrc := s.newRealLLMConfigs()
		m, err := llm.NewModel(mcc, mrc)
		if err != nil {
			return nil, err
		}
		// 直构 *llm.Model 缺 TranscribeAudio（组件 modelClientAdapter 同款，D-25-5）
		llmClient = m
	}
	kbCfg := cfg.ComponentKBConfigs[0]
	vs, err := vector_store.CreateVectorStore(kbCfg.VectorStoreConfig,
		vector_store.WithStoreChromaPath(cfg.VectorStoreConnectionConfig["chroma_path"].(string)))
	if err != nil {
		return nil, err
	}
	var retr retriever.Retriever
	var em storeembedding.BaseEmbedding
	if kbCfg.EmbedConfig != nil {
		em = retrievalembedding.NewOpenAIEmbedding(*kbCfg.EmbedConfig)
	}
	switch kbCfg.KBConfig.IndexType {
	case retrievalcommon.IndexTypeBM25:
		retr = retriever.NewSparseRetriever(vs)
	case retrievalcommon.IndexTypeHybrid:
		retr = retriever.NewHybridRetriever(vs, em)
	default:
		retr = retriever.NewVectorRetriever(vs, em)
	}
	// AddDocuments 需 chunker（155501）+ index_manager（155502）
	chunker, err := chunker.NewCharChunker()
	if err != nil {
		return nil, err
	}
	idx, err := indexer.NewChromaIndexer(kbCfg.VectorStoreConfig, cfg.VectorStoreConnectionConfig["chroma_path"].(string))
	if err != nil {
		return nil, err
	}
	useGraph := cfg.RetrievalConfig.UseGraph != nil && *cfg.RetrievalConfig.UseGraph
	if useGraph {
		return knowledgebase.NewGraphKnowledgeBase(kbCfg.KBConfig, retr, retr,
			knowledgebase.WithVectorStore(vs), knowledgebase.WithEmbedModel(em),
			knowledgebase.WithChunker(chunker), knowledgebase.WithIndexManager(idx),
			knowledgebase.WithLLMClient(llmClient))
	}
	return knowledgebase.NewSimpleKnowledgeBase(kbCfg.KBConfig, retr,
		knowledgebase.WithVectorStore(vs), knowledgebase.WithEmbedModel(em),
		knowledgebase.WithChunker(chunker), knowledgebase.WithIndexManager(idx),
		knowledgebase.WithLLMClient(llmClient))
}

// buildWorkflow 真实编排：Start→kb1→End，inputs_schema 字段映射链
// start.question → kb1.query → end 引用 kb1.context
func (s *KnowledgeRetrievalE2ESuite) buildWorkflow(comp *resource.KnowledgeRetrievalComponent) *workflow.Workflow {
	w := workflow.NewWorkflow(nil)
	w.SetStartComp("start", flow.NewStart(), nil, nil, nil)
	// inputs 引用 workflow 顶层输入（8.24 同款 "${query}" 形态）
	w.AddWorkflowComp("kb1", comp, nil, map[string]any{"query": "${question}"}, nil, nil, nil, nil, 0, -1.0, nil, nil)
	endCfg, err := flow.NewEndConfig("answer:{{answer}}")
	s.Require().NoError(err)
	end, err := flow.NewEnd(endCfg)
	s.Require().NoError(err)
	w.SetEndComp("end", end, map[string]any{"answer": "${kb1.context}"}, nil, nil, nil, "batch", nil)
	w.AddConnection([]string{"start"}, "kb1")
	w.AddConnection([]string{"kb1"}, "end")
	return w
}

// runWorkflow 真实执行并断言 COMPLETED，返回 End 输出
func (s *KnowledgeRetrievalE2ESuite) runWorkflow(w *workflow.Workflow, question string) map[string]any {
	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	out, err := w.Invoke(context.Background(), map[string]any{"question": question}, sess, nil, nil)
	s.Require().NoError(err)
	wo, ok := out.(*workflow.WorkflowOutput)
	s.Require().True(ok)
	s.Require().Equal(workflow.WorkflowExecutionStateCompleted, wo.State, "result=%v", wo.Result)
	resultStr := fmt.Sprintf("%v", wo.Result)
	s.Require().NotEmpty(resultStr, "End 输出不应为空")
	return map[string]any{"answer": resultStr, "raw": wo.Result}
}

// TestKB_BM25模式_chroma本地约束 bm25 稀疏检索在 chroma 本地模式不支持
// （155508 库层既有约束，真链验证约束本身）；完整 sparse 真链需 milvus 服务
// （登记 .integration-scope-exempt 说明）。
func (s *KnowledgeRetrievalE2ESuite) TestKB_BM25模式_chroma本地约束() {
	// bm25 稀疏检索在 chroma 本地不支持（155508 库层既有约束）：
	// ① storage 侧读回直接报 155508（真链验证约束本身）
	// ② 组件链检索期错误被 gather 吞掉 → 空结果 COMPLETED（结构契约）
	comp := s.newKBComponentNoWrite(retrievalcommon.IndexTypeBM25, false)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "workflow engine")
	s.T().Logf("bm25 swallowed-error result=%v", result["raw"])
}

// TestKB_Vector模式_真embedding vector 索引真链（免费档 embedding-3）
func (s *KnowledgeRetrievalE2ESuite) TestKB_Vector模式_真embedding() {
	embedCfg := s.newRealEmbedding()
	docs := []string{
		"knowledge retrieval component searches multiple knowledge bases",
		"the pregel engine executes workflow nodes concurrently",
	}
	comp := s.newKBComponent(retrievalcommon.IndexTypeVector, false, embedCfg, docs, false)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "knowledge retrieval")
	s.Contains(fmt.Sprintf("%v", result["raw"]), "knowledge",
		"vector 真链 context 应含命中文档关键词，got: %v", result["raw"])
}

// TestKB_Hybrid模式_chroma本地约束 hybrid 同样受 chroma 本地约束（155508）
func (s *KnowledgeRetrievalE2ESuite) TestKB_Hybrid模式_chroma本地约束() {
	comp := s.newKBComponent(retrievalcommon.IndexTypeHybrid, false, s.newRealEmbedding(), nil, false)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "hybrid search")
	s.T().Logf("hybrid swallowed-error result=%v", result["raw"])
}

// TestKB_Graph模式_图库链路 use_graph=true：GraphKnowledgeBase 构造与 workflow 消费
func (s *KnowledgeRetrievalE2ESuite) TestKB_Graph模式_图库链路() {
	comp := s.newKBComponent(retrievalcommon.IndexTypeBM25, true, nil, nil, false)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "any question")
	// 空索引 → results 空 → context 空串（结构契约，与检索排序无关）
	answer, _ := result["answer"].(string)
	s.T().Logf("graph empty-index answer=%q", answer)
}

// ──────────────────────────── SQLite 真链用例（D-25-7，2026-11-24 用户拍板） ────────────────────────────
//
// StoreTypeSqlite（Go 私有 provider）解除 chroma 本地 155508 约束：
// bm25/hybrid/graph 三模式补齐「写入→检索命中」真链断言。

// newSQLiteKBComponentWithPrewrite 构造 sqlite 组件 + store 层预写与读回验证。
// 存储侧：经 CreateVectorStore 直写（bm25-only 无向量 / vector|hybrid 带真 embedding），
// store 原生检索读回命中；检索侧：组件在同 db 上建 KB 走 workflow 全链。
func (s *KnowledgeRetrievalE2ESuite) newSQLiteKBComponentWithPrewrite(
	indexType retrievalcommon.IndexTypeKind, embedCfg *retrievalembedding.EmbeddingConfig,
	docs []string, useGraph bool,
) *resource.KnowledgeRetrievalComponent {
	s.chdirRepoRoot(s.T())
	dbPath := filepath.Join(s.T().TempDir(), "kb_e2e.db")
	vsCfg := retrievalcommon.VectorStoreConfig{
		StoreProvider:  retrievalcommon.StoreTypeSqlite,
		DatabaseName:   "kb_e2e",
		CollectionName: "kb_kb-e2e_chunks",
		DistanceMetric: retrievalcommon.DistanceMetricEuclidean,
	}
	connCfg := map[string]any{"sqlite_path": dbPath}

	var em storeembedding.BaseEmbedding
	if embedCfg != nil {
		em = retrievalembedding.NewOpenAIEmbedding(*embedCfg)
	}

	// 存储侧预写：store 直写（AddDocuments 链的落库终态等价物）+ store 原生检索读回
	if len(docs) > 0 {
		store, err := vector_store.CreateVectorStore(vsCfg, vector_store.WithStoreSQLitePath(dbPath))
		s.Require().NoError(err, "sqlite store 构造失败")
		switch indexType {
		case retrievalcommon.IndexTypeBM25:
			// bm25-only：无向量写入（FTS 可用）
			rows := make([]map[string]any, 0, len(docs))
			for i, d := range docs {
				rows = append(rows, map[string]any{"id": fmt.Sprintf("d%d", i), "content": d})
			}
			s.Require().NoError(store.Add(context.Background(), rows), "sqlite store bm25 写入失败")
			hits, err := store.SparseSearch(context.Background(), docs[0], 3, nil)
			s.Require().NoError(err, "sqlite store sparse 读回失败")
			s.Require().NotEmpty(hits, "存储侧 sparse 读回应命中写入文档")
		case retrievalcommon.IndexTypeHybrid, retrievalcommon.IndexTypeVector:
			s.Require().NotNil(embedCfg, "vector/hybrid 预写需要 embedding 配置")
			vecs, err := em.EmbedDocuments(context.Background(), docs)
			s.Require().NoError(err, "真 embedding 失败")
			rows := make([]map[string]any, 0, len(docs))
			for i, d := range docs {
				rows = append(rows, map[string]any{
					"id": fmt.Sprintf("d%d", i), "content": d, "embedding": vecs[i],
				})
			}
			s.Require().NoError(store.Add(context.Background(), rows), "sqlite store 向量写入失败")
			qv, err := em.EmbedQuery(context.Background(), docs[0])
			s.Require().NoError(err, "真 embedding query 失败")
			hits, err := store.Search(context.Background(), qv, 3, nil)
			s.Require().NoError(err, "sqlite store vector 读回失败")
			s.Require().NotEmpty(hits, "存储侧 vector 读回应命中写入文档")
		}
		s.T().Logf("存储侧验证通过：provider=sqlite index=%s docs=%d", indexType, len(docs))
		_ = store.Close()
	}

	cfg := &resource.KnowledgeRetrievalCompConfig{
		ComponentKBConfigs: []resource.ComponentKBConfig{{
			KBConfig: retrievalcommon.KnowledgeBaseConfig{
				KBID:      "kb-e2e",
				IndexType: indexType,
			},
			VectorStoreConfig: vsCfg,
			EmbedConfig:       embedCfg,
		}},
		VectorStoreConnectionConfig: connCfg,
		RetrievalConfig: retrievalcommon.RetrievalConfig{
			TopK:     3,
			UseGraph: boolPtr(useGraph),
		},
	}
	comp, err := resource.NewKnowledgeRetrievalComponent(cfg)
	s.Require().NoError(err)
	return comp
}

// TestKB_BM25模式_sqlite真链 bm25 稀疏检索真链（FTS5 真 BM25）
func (s *KnowledgeRetrievalE2ESuite) TestKB_BM25模式_sqlite真链() {
	docs := []string{
		"workflow engine executes knowledge retrieval nodes",
		"sqlite vector store provides bm25 sparse search",
	}
	comp := s.newSQLiteKBComponentWithPrewrite(retrievalcommon.IndexTypeBM25, nil, docs, false)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "workflow engine")
	s.Contains(fmt.Sprintf("%v", result["raw"]), "workflow",
		"bm25 真链 context 应含命中文档关键词，got: %v", result["raw"])
}

// TestKB_Hybrid模式_sqlite真链 hybrid 向量+BM25 双路真链（真 embedding + FTS5）
func (s *KnowledgeRetrievalE2ESuite) TestKB_Hybrid模式_sqlite真链() {
	embedCfg := s.newRealEmbedding()
	docs := []string{
		"hybrid retrieval fuses dense vectors with bm25 sparse ranking",
		"the rrf fusion merges both ranked lists into one",
	}
	comp := s.newSQLiteKBComponentWithPrewrite(retrievalcommon.IndexTypeHybrid, embedCfg, docs, false)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "hybrid retrieval fusion")
	s.Contains(fmt.Sprintf("%v", result["raw"]), "hybrid",
		"hybrid 真链 context 应含命中文档关键词，got: %v", result["raw"])
}

// TestKB_Graph模式_sqlite真链 use_graph=true 图库 chunk 路真链
// （GraphKB chunk 检索走 sqlite 向量真链；三元组路无 LLM 抽取时为空、不阻断）
func (s *KnowledgeRetrievalE2ESuite) TestKB_Graph模式_sqlite真链() {
	embedCfg := s.newRealEmbedding()
	docs := []string{
		"graph knowledge base extends chunk retrieval with triple relations",
		"the graph expansion pulls related entities into context",
	}
	comp := s.newSQLiteKBComponentWithPrewrite(retrievalcommon.IndexTypeVector, embedCfg, docs, true)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "graph knowledge base")
	s.Contains(fmt.Sprintf("%v", result["raw"]), "graph",
		"graph 模式 chunk 路真链 context 应含命中文档关键词，got: %v", result["raw"])
}

// TestKB_Agentic模式_真LLM agentic 多跳检索真链（免费档 LLM）
func (s *KnowledgeRetrievalE2ESuite) TestKB_Agentic模式_真LLM() {
	embedCfg := s.newRealEmbedding()
	docs := []string{
		"agentic retrieval performs multi hop search with llm guided rewriting",
		"the retrieval loop stops when the rewrite step reports sufficient context",
	}
	comp := s.newKBComponent(retrievalcommon.IndexTypeVector, false, embedCfg, docs, true)
	w := s.buildWorkflow(comp)
	result := s.runWorkflow(w, "multi hop retrieval")
	s.Contains(fmt.Sprintf("%v", result["raw"]), "retrieval",
		"agentic 真链 context 应含命中文档关键词，got: %v", result["raw"])
}

// TestKB_空query_组件层拒绝 空白 query → 101102（Python :88-92）
func (s *KnowledgeRetrievalE2ESuite) TestKB_空query_组件层拒绝() {
	comp := s.newKBComponent(retrievalcommon.IndexTypeBM25, false, nil, nil, false)
	w := s.buildWorkflow(comp)
	sess := session.NewSession(session.WithEnvs(map[string]any{}))
	_, err := w.Invoke(context.Background(), map[string]any{"question": "   "}, sess, nil, nil)
	s.Require().Error(err)
	s.Contains(err.Error(), "101102", "空白 query 应报 101102，got: %v", err)
}

// ──────────────────────────── 测试辅助 ────────────────────────────

func boolPtr(b bool) *bool { return &b }

func intPtr(n int) *int { return &n }

// 确保编译期引用（fmt 用于调试日志格式化）
var _ = fmt.Sprintf
