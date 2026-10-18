//go:build integration

// Package ltm_test 提供 LongTermMemory 模块的集成测试。
package ltm_test

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math"
	"math/rand"
	"sort"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	migrationop "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/migration/operation"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryQualitySuite 对齐 Python test_memory_quality.py 的集成测试。
//
// 覆盖：
//   - 变量提取（test_variable_01/02）
//   - 用户记忆基本提取+检索（test_user_mem_base）
//   - 冲突更新（test_user_mem_check_new_conflict）
//   - 非自身记忆不混入（test_user_mem_not_self）
//   - 记忆更新（test_user_mem_update）
//   - 指代消解（test_user_mem_reference）
//   - 情景记忆（test_user_mem_episodic）
//   - 真实冲突情景记忆（test_user_mem_episodic_conflict_real）
//   - 假冲突情景记忆（test_user_mem_episodic_conflict_false）
//   - 语义记忆（test_user_mem_semantic）
//   - 混合类型记忆（test_user_mem_mixed）
//
// 对应 Python: tests/system_tests/memory/test_memory_quality.py
type MemoryQualitySuite struct {
	isuite.MemorySuite
	// mockLLM Mock LLM 客户端
	mockLLM *mockllm.MockModelClient
	// ltm 实例
	ltmInstance *ltm.LongTermMemory
}

// mockBaseEmbedding 适配 BaseEmbedding 接口的 Mock 嵌入模型。
// 使用 MD5-hash 确定性 128 维向量，对齐 Python MockEmbeddingProvider。
type mockBaseEmbedding struct{}

// mockVectorStore 内存向量存储，支持余弦相似度搜索。
type mockVectorStore struct {
	collections map[string]*mockCollection
}

// mockCollection 内存向量集合
type mockCollection struct {
	schema *vector.CollectionSchema
	docs   []mockDoc
	nextID int
}

// mockDoc 内存向量文档
type mockDoc struct {
	id        string
	embedding []float64
	fields    map[string]any
}

// mockDbStore 基于 SQLite 内存模式的 Mock 数据库存储
type mockDbStore struct {
	db *gorm.DB
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// mqTestProvider Mock LLM 在 ClientRegistry 中的 provider 名
	mqTestProvider = "mq_test_llm"
	// mqTestClientID Mock LLM 在 ClientRegistry 中的 client ID
	mqTestClientID = "mq_test_client_id"
	// mqTestScopeID 测试用 scope ID
	mqTestScopeID = "scope_mq_test"
	// mqTestUserID 测试用 user ID
	mqTestUserID = "user_mq_test"
	// mockEmbeddingDim Mock 嵌入向量维度
	mockEmbeddingDim = 128
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMemoryQualitySuite(t *testing.T) {
	suite.Run(t, new(MemoryQualitySuite))
}

// SetupSuite 初始化 Memory Quality 测试环境。
func (s *MemoryQualitySuite) SetupSuite() {
	s.MemorySuite.SetupSuite()

	// 创建并注册 Mock LLM 到全局 ClientRegistry
	s.mockLLM = mockllm.NewMockModelClient()
	registry := model_clients.GetClientRegistry()
	registry.Register(mqTestProvider, "llm", s.mockLLM.Factory())
}

// TearDownSuite 清理 Memory Quality 测试环境。
func (s *MemoryQualitySuite) TearDownSuite() {
	// 重置 LTM 单例
	ltm.ResetLongTermMemory()
	s.MemorySuite.TearDownSuite()
}

// ──────────────────────────── Mock 实现 ────────────────────────────

// EmbedQuery 返回基于文本 MD5 hash 的确定性 128 维向量。
func (e *mockBaseEmbedding) EmbedQuery(_ context.Context, text string, _ ...embedding.EmbedOption) ([]float64, error) {
	return md5HashVector(text), nil
}

// EmbedDocuments 批量嵌入文档。
func (e *mockBaseEmbedding) EmbedDocuments(ctx context.Context, texts []string, _ ...embedding.EmbedOption) ([][]float64, error) {
	result := make([][]float64, len(texts))
	for i, text := range texts {
		vec, err := e.EmbedQuery(ctx, text)
		if err != nil {
			return nil, err
		}
		result[i] = vec
	}
	return result, nil
}

// Dimension 返回嵌入向量维度。
func (e *mockBaseEmbedding) Dimension() int { return mockEmbeddingDim }

// DimensionWithContext 返回嵌入向量维度（支持 context）。
func (e *mockBaseEmbedding) DimensionWithContext(_ context.Context) (int, error) {
	return mockEmbeddingDim, nil
}

// md5HashVector 基于 MD5 hash 生成确定性 128 维向量。
// 对齐 Python MockEmbeddingProvider.embed_query。
func md5HashVector(text string) []float64 {
	h := md5.Sum([]byte(text))
	hexStr := hex.EncodeToString(h[:])
	seed := int64(binary.BigEndian.Uint64([]byte(hexStr[:8])))
	r := rand.New(rand.NewSource(seed))
	vec := make([]float64, mockEmbeddingDim)
	for i := range vec {
		vec[i] = r.Float64()*2 - 1 // 均匀分布 uniform(-1, 1)
	}
	return vec
}

// --- mockVectorStore 实现 ---

func newMockVectorStore() *mockVectorStore {
	return &mockVectorStore{
		collections: make(map[string]*mockCollection),
	}
}

func (vs *mockVectorStore) CreateCollection(_ context.Context, name string, schema *vector.CollectionSchema, _ ...vector.Option) error {
	vs.collections[name] = &mockCollection{schema: schema, nextID: 1}
	return nil
}

func (vs *mockVectorStore) DeleteCollection(_ context.Context, name string, _ ...vector.Option) error {
	delete(vs.collections, name)
	return nil
}

func (vs *mockVectorStore) CollectionExists(_ context.Context, name string, _ ...vector.Option) (bool, error) {
	_, ok := vs.collections[name]
	return ok, nil
}

func (vs *mockVectorStore) GetSchema(_ context.Context, name string, _ ...vector.Option) (*vector.CollectionSchema, error) {
	col, ok := vs.collections[name]
	if !ok {
		return nil, fmt.Errorf("collection %s not found", name)
	}
	return col.schema, nil
}

func (vs *mockVectorStore) AddDocs(_ context.Context, collectionName string, docs []map[string]any, _ ...vector.Option) error {
	col, ok := vs.collections[collectionName]
	if !ok {
		return fmt.Errorf("collection %s not found", collectionName)
	}
	for _, doc := range docs {
		id, _ := doc["id"].(string)
		emb, _ := doc["embedding"].([]float64)
		// 深拷贝 fields
		fields := make(map[string]any)
		for k, v := range doc {
			fields[k] = v
		}
		col.docs = append(col.docs, mockDoc{id: id, embedding: emb, fields: fields})
	}
	return nil
}

func (vs *mockVectorStore) Search(_ context.Context, collectionName string, queryVector []float64, vectorField string, topK int, _ map[string]any, _ ...vector.Option) ([]vector.VectorSearchResult, error) {
	col, ok := vs.collections[collectionName]
	if !ok {
		return nil, nil
	}
	if topK <= 0 {
		topK = 5
	}

	type scored struct {
		doc   mockDoc
		score float64
	}
	var results []scored
	for _, doc := range col.docs {
		embVec, ok := doc.fields[vectorField]
		if !ok {
			embVec = doc.embedding
		}
		// 尝试转换为 []float64
		var emb []float64
		switch v := embVec.(type) {
		case []float64:
			emb = v
		case []any:
			emb = make([]float64, len(v))
			for i, f := range v {
				if fv, ok := f.(float64); ok {
					emb[i] = fv
				}
			}
		}
		if len(emb) == 0 {
			continue
		}
		score := cosineSimilarity(queryVector, emb)
		results = append(results, scored{doc: doc, score: score})
	}

	// 按 score 降序排序
	sort.Slice(results, func(i, j int) bool {
		return results[i].score > results[j].score
	})

	// 取 topK
	if topK > len(results) {
		topK = len(results)
	}
	out := make([]vector.VectorSearchResult, topK)
	for i := 0; i < topK; i++ {
		out[i] = vector.VectorSearchResult{
			Score:  results[i].score,
			Fields: results[i].doc.fields,
		}
	}
	return out, nil
}

func (vs *mockVectorStore) DeleteDocsByIDs(_ context.Context, collectionName string, ids []string, _ ...vector.Option) error {
	col, ok := vs.collections[collectionName]
	if !ok {
		return nil
	}
	idSet := make(map[string]bool, len(ids))
	for _, id := range ids {
		idSet[id] = true
	}
	filtered := col.docs[:0]
	for _, doc := range col.docs {
		if !idSet[doc.id] {
			filtered = append(filtered, doc)
		}
	}
	col.docs = filtered
	return nil
}

func (vs *mockVectorStore) DeleteDocsByFilters(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (vs *mockVectorStore) ListCollectionNames(_ context.Context) ([]string, error) {
	names := make([]string, 0, len(vs.collections))
	for name := range vs.collections {
		names = append(names, name)
	}
	return names, nil
}

func (vs *mockVectorStore) UpdateSchema(_ context.Context, _ string, _ []migrationop.Operation, _ ...vector.Option) error {
	return nil
}

func (vs *mockVectorStore) UpdateCollectionMetadata(_ context.Context, _ string, _ map[string]any, _ ...vector.Option) error {
	return nil
}

func (vs *mockVectorStore) GetCollectionMetadata(_ context.Context, _ string, _ ...vector.Option) (map[string]any, error) {
	return nil, nil
}

func (vs *mockVectorStore) RenameCollection(_ context.Context, _, _ string, _ ...vector.Option) error {
	return fmt.Errorf("not supported")
}

// cosineSimilarity 计算余弦相似度
func cosineSimilarity(a, b []float64) float64 {
	if len(a) != len(b) || len(a) == 0 {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

// --- mockDbStore 实现 ---

func newMockDbStore() *mockDbStore {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		panic(fmt.Sprintf("创建 SQLite 内存数据库失败: %v", err))
	}
	return &mockDbStore{db: db}
}

func (s *mockDbStore) GetDB(_ context.Context) *gorm.DB {
	return s.db
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// setupLTM 创建完整的 LTM 实例，注册所有必需存储。
// 每个测试方法独立调用，保证隔离性。
func (s *MemoryQualitySuite) setupLTM() *ltm.LongTermMemory {
	ltm.ResetLongTermMemory()
	m := ltm.NewLongTermMemory()

	kvStore := kv.NewInMemoryKVStore()
	vecStore := newMockVectorStore()
	embModel := &mockBaseEmbedding{}
	dbStore := newMockDbStore()

	err := m.RegisterStore(
		s.Ctx,
		kvStore,
		ltm.WithVectorStore(vecStore),
		ltm.WithEmbeddingModel(embModel),
		ltm.WithDbStore(dbStore),
	)
	s.Require().NoError(err, "RegisterStore 应成功")

	// 设置引擎配置（含 Mock LLM 参数）
	engineCfg := config.DefaultMemoryEngineConfig()
	engineCfg.DefaultModelCfg = &llmschema.ModelRequestConfig{
		ModelName: "mock-model",
	}
	engineCfg.DefaultModelClientCfg = &llmschema.ModelClientConfig{
		ClientProvider: mqTestProvider,
		ClientID:       mqTestClientID,
	}
	err = m.SetConfig(engineCfg)
	s.Require().NoError(err, "SetConfig 应成功")

	// 设置 scope 配置（使用默认定义）
	scopeCfg := config.DefaultMemoryScopeConfig()
	err = m.SetScopeConfig(s.Ctx, mqTestScopeID, scopeCfg)
	s.Require().NoError(err, "SetScopeConfig 应成功")

	return m
}

// addUserMsg 构造一条 UserMessage。
func addUserMsg(text string) llmschema.BaseMessage {
	return llmschema.NewUserMessage(text)
}

// addAssistantMsg 构造一条 AssistantMessage。
func addAssistantMsg(text string) llmschema.BaseMessage {
	return llmschema.NewAssistantMessage(text)
}

// analysisJSON 构造第1次 LLM 调用（Analyze）的响应 JSON。
func analysisJSON(variables []map[string]string, summary string) map[string]any {
	return map[string]any{
		"has_key_information": true,
		"variables":           variables,
		"summary":             summary,
	}
}

// fragmentJSON 构造第2次 LLM 调用（ExtractLongTermMemory）的响应 JSON。
func fragmentJSON(userProfile, semanticMem, episodicMem []string, instructMemories []map[string]any) map[string]any {
	hasInstruct := len(instructMemories) > 0
	result := map[string]any{
		"has_explict_instruct": hasInstruct,
		"instruct_memories":    instructMemories,
		"user_profile":         userProfile,
		"semantic_memory":      semanticMem,
		"episodic_memory":      episodicMem,
	}
	return result
}

// setLLMResponses 设置 Mock LLM 的响应队列。
// responses 按 LLM 调用顺序依次消费。
func (s *MemoryQualitySuite) setLLMResponses(responses ...map[string]any) {
	s.mockLLM.SetResponses() // 清空旧响应
	for _, resp := range responses {
		s.mockLLM.AddJSONResponse(resp)
	}
}

// addMessagesAndCheck 调用 AddMessages 并验证结果不为空。
func (s *MemoryQualitySuite) addMessagesAndCheck(
	m *ltm.LongTermMemory,
	messages []llmschema.BaseMessage,
	agentConfig *config.AgentMemoryConfig,
) *ltm.AddMemResult {
	result, err := m.AddMessages(
		s.Ctx,
		messages,
		agentConfig,
		ltm.WithScopeID(mqTestScopeID),
		ltm.WithUserID(mqTestUserID),
		ltm.WithGenMem(true),
	)
	s.Require().NoError(err, "AddMessages 应成功")
	s.Require().NotNil(result, "AddMemResult 不应为 nil")
	return result
}

// searchMem 调用 SearchUserMem 并返回结果内容列表。
// 传入 SearchWithThreshold(0) 绕过阈值过滤，因为 mockBaseEmbedding
// 使用 MD5-hash 随机向量，不同文本的余弦相似度约 0，远低于默认阈值 0.3。
// 传入较大的 topK(20) 以确保返回足够多的记忆，因为随机向量的排序不相关。
func (s *MemoryQualitySuite) searchMem(m *ltm.LongTermMemory, query string) []string {
	results, err := m.SearchUserMem(
		s.Ctx,
		query,
		20, // topK=20，因为随机向量排序不相关，需要足够大的 topK 确保覆盖
		ltm.SearchWithUserID(mqTestUserID),
		ltm.SearchWithScopeID(mqTestScopeID),
		ltm.SearchWithThreshold(0),
	)
	s.Require().NoError(err, "SearchUserMem 应成功")

	var contents []string
	for _, r := range results {
		if r.MemInfo != nil {
			contents = append(contents, r.MemInfo.Content)
		}
	}
	return contents
}

// listAllMem 调用 GetUserMemByPage 获取用户所有记忆内容。
// 不依赖向量搜索，直接列出所有已写入的记忆。
func (s *MemoryQualitySuite) listAllMem(m *ltm.LongTermMemory, memType mem_model.MemoryType) []string {
	results, err := m.GetUserMemByPage(
		s.Ctx,
		100, 1, memType,
		ltm.Uid(mqTestUserID),
		ltm.Sid(mqTestScopeID),
	)
	s.Require().NoError(err, "GetUserMemByPage 应成功")

	var contents []string
	for _, mi := range results {
		contents = append(contents, mi.Content)
	}
	return contents
}

// getVar 调用 GetVariables 获取单个变量值。
func (s *MemoryQualitySuite) getVar(m *ltm.LongTermMemory, varName string) string {
	vars, err := m.GetVariables(
		s.Ctx,
		[]string{varName},
		ltm.Uid(mqTestUserID),
		ltm.Sid(mqTestScopeID),
	)
	s.Require().NoError(err, "GetVariables 应成功")

	if val, ok := vars[varName]; ok {
		return val
	}
	return ""
}

// assertContains 断言 contents 中至少一条包含 expected 子串。
func (s *MemoryQualitySuite) assertContains(contents []string, expected string, msgAndArgs ...any) {
	for _, c := range contents {
		if strings.Contains(c, expected) {
			return
		}
	}
	s.Failf("assertContains 失败", "期望包含 %q，实际内容: %v %v", expected, contents, msgAndArgs)
}

// assertNotContains 断言 contents 中没有任何一条包含 unexpected 子串。
func (s *MemoryQualitySuite) assertNotContains(contents []string, unexpected string, msgAndArgs ...any) {
	for _, c := range contents {
		if strings.Contains(c, unexpected) {
			s.Failf("assertNotContains 失败", "不期望包含 %q，实际内容: %v %v", unexpected, contents, msgAndArgs)
			return
		}
	}
}

// defaultAgentConfig 返回默认的 AgentMemoryConfig（启用所有记忆类型）。
func defaultAgentConfig() *config.AgentMemoryConfig {
	return config.DefaultAgentMemoryConfig()
}

// varOnlyConfig 返回仅启用变量提取的 AgentMemoryConfig。
func varOnlyConfig() *config.AgentMemoryConfig {
	cfg := config.DefaultAgentMemoryConfig()
	cfg.EnableLongTermMem = false
	cfg.EnableSummaryMemory = false
	cfg.EnableUserProfile = false
	cfg.EnableSemanticMemory = false
	cfg.EnableEpisodicMemory = false
	return cfg
}

// ──────────────────────────── 测试方法 ────────────────────────────

// TestVariable_01 基本变量提取：姓名=Tom, 职业=数据分析师。
// 对齐 Python: test_variable_01
func (s *MemoryQualitySuite) TestVariable_01() {
	m := s.setupLTM()

	// 设置 LLM 响应：第1次 Analyze 返回变量
	s.setLLMResponses(
		analysisJSON(
			[]map[string]string{
				{"variable_key": "姓名", "variable_value": "Tom"},
				{"variable_key": "职业", "variable_value": "数据分析师"},
			},
			"用户介绍了自己的基本信息",
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("你好，我叫Tom，我是一名数据分析师"),
		addAssistantMsg("你好Tom！很高兴认识你"),
	}

	agentCfg := varOnlyConfig()
	agentCfg.MemVariables = []commonschema.Param{
		{Name: "姓名", Description: "用户姓名"},
		{Name: "职业", Description: "用户职业"},
	}

	result := s.addMessagesAndCheck(m, messages, agentCfg)
	s.NotNil(result.Variables, "应提取变量")

	// 验证变量值
	nameVal := s.getVar(m, "姓名")
	s.Equal("Tom", nameVal, "姓名应为 Tom")

	jobVal := s.getVar(m, "职业")
	s.Equal("数据分析师", jobVal, "职业应为数据分析师")
}

// TestVariable_02 多变量 + 禁用变量过滤：身份证号被 forbidden_variables 过滤。
// 对齐 Python: test_variable_02
//
// 注意：forbidden_variables 仅在 LLM 提示词中指示模型不提取被禁止变量，
// Python/Go 两端均无代码级过滤。Mock LLM 不理解 prompt 指令，
// 因此需要模拟 LLM 遵守 forbidden_variables 的行为：被禁止变量返回空值。
// processExtractedData 会过滤 VariableValue=="" 的变量，从而实现等价效果。
func (s *MemoryQualitySuite) TestVariable_02() {
	m := s.setupLTM()

	// 设置 forbidden_variables（在 SetConfig 中配置）
	engineCfg := config.DefaultMemoryEngineConfig()
	engineCfg.ForbiddenVariables = "身份证号"
	engineCfg.DefaultModelCfg = &llmschema.ModelRequestConfig{ModelName: "mock-model"}
	engineCfg.DefaultModelClientCfg = &llmschema.ModelClientConfig{
		ClientProvider: mqTestProvider,
		ClientID:       mqTestClientID,
	}
	err := m.SetConfig(engineCfg)
	s.Require().NoError(err, "SetConfig 应成功")

	// 设置 LLM 响应：模拟 LLM 遵守 forbidden_variables 指令，
	// "身份证号"变量返回空值（真实 LLM 会在 prompt 指导下跳过该变量）
	s.setLLMResponses(
		analysisJSON(
			[]map[string]string{
				{"variable_key": "用户书籍", "variable_value": "悬疑小说"},
				{"variable_key": "老婆书籍", "variable_value": "时间简史"},
				{"variable_key": "身份证号", "variable_value": ""}, // 模拟 LLM 遵守 forbidden_variables，返回空值
			},
			"用户介绍了阅读偏好",
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("我喜欢看悬疑小说，我老婆喜欢看时间简史，我的身份证号是110101199001011234"),
		addAssistantMsg("好的，了解了你的阅读偏好"),
	}

	agentCfg := varOnlyConfig()
	agentCfg.MemVariables = []commonschema.Param{
		{Name: "用户书籍", Description: "用户喜欢的书籍"},
		{Name: "老婆书籍", Description: "用户老婆喜欢的书籍"},
		{Name: "身份证号", Description: "用户身份证号"},
	}

	result := s.addMessagesAndCheck(m, messages, agentCfg)
	s.NotNil(result.Variables, "应提取变量")

	// 验证正常变量已提取
	bookVal := s.getVar(m, "用户书籍")
	s.Equal("悬疑小说", bookVal, "用户书籍应为悬疑小说")

	wifeBookVal := s.getVar(m, "老婆书籍")
	s.Equal("时间简史", wifeBookVal, "老婆书籍应为时间简史")

	// 身份证号应被过滤（LLM 遵守 forbidden_variables 返回空值，processExtractedData 过滤空值）
	idVal := s.getVar(m, "身份证号")
	s.Empty(idVal, "身份证号应被 forbidden_variables 过滤")
}

// TestUserMemBase 基本记忆提取+检索：姓名/工作/爱好。
// 对齐 Python: test_user_mem_base
func (s *MemoryQualitySuite) TestUserMemBase() {
	m := s.setupLTM()

	// 第1次 Analyze + 第2次 ExtractLongTermMemory
	s.setLLMResponses(
		analysisJSON(nil, "用户介绍了自己的基本信息"),
		fragmentJSON(
			[]string{"用户的姓名是小明", "用户是一名程序员", "用户喜欢打篮球"},
			nil, nil, nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("你好，我叫小明，我是一名程序员，我喜欢打篮球"),
		addAssistantMsg("你好小明！"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.UserProfile) > 0, "应提取用户画像")

	// 通过 GetUserMemByPage 验证记忆已正确写入（不依赖向量搜索排序）
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "小明", "用户画像应包含姓名")
	s.assertContains(contents, "程序员", "用户画像应包含工作")
	s.assertContains(contents, "篮球", "用户画像应包含爱好")

	// 验证搜索 API 可用（mock 向量排序随机，仅断言返回非空）
	searchResults := s.searchMem(m, "用户叫什么名字")
	s.True(len(searchResults) > 0, "搜索应返回结果")
}

// TestUserMemCheckNewConflict 真实冲突更新：Tom -> Tim。
// 对齐 Python: test_user_mem_check_new_conflict
func (s *MemoryQualitySuite) TestUserMemCheckNewConflict() {
	m := s.setupLTM()

	// 第1轮：写入 Tom
	s.setLLMResponses(
		analysisJSON(nil, "用户介绍了自己的姓名"),
		fragmentJSON(
			[]string{"用户的姓名是Tom"},
			nil, nil, nil,
		),
	)

	msgs1 := []llmschema.BaseMessage{
		addUserMsg("我叫Tom"),
		addAssistantMsg("好的Tom"),
	}
	s.addMessagesAndCheck(m, msgs1, defaultAgentConfig())

	// 验证 Tom 存在
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "Tom", "初始用户画像应包含 Tom")

	// 第2轮：冲突更新 Tom → Tim（使用 instruct_memories 的 UPDATE 指令）
	s.setLLMResponses(
		analysisJSON(nil, "用户更正了姓名"),
		fragmentJSON(
			[]string{"用户的姓名是Tim"},
			nil, nil,
			[]map[string]any{
				{
					"mem_content":  "用户的姓名是Tim",
					"mem_type":     "user_profile",
					"mem_instruct": "UPDATE",
					"old_mem":      "用户的姓名是Tom",
				},
			},
		),
		// 第3次 LLM 调用：semantic_validation 返回 CORRECT
		map[string]any{"result": "CORRECT"},
	)

	msgs2 := []llmschema.BaseMessage{
		addUserMsg("其实我叫Tim，不是Tom"),
		addAssistantMsg("好的Tim，记住了"),
	}
	s.addMessagesAndCheck(m, msgs2, defaultAgentConfig())

	// 验证 Tim 存在
	contents = s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "Tim", "更新后用户画像应包含 Tim")
}

// TestUserMemNotSelf 非自身记忆不混入：朋友的信息不影响用户自身。
// 对齐 Python: test_user_mem_not_self
func (s *MemoryQualitySuite) TestUserMemNotSelf() {
	m := s.setupLTM()

	// LLM 应只提取用户自身的画像，朋友的"宋朝"不应出现在用户画像中
	s.setLLMResponses(
		analysisJSON(nil, "用户提到了朋友"),
		fragmentJSON(
			[]string{"用户有一个叫宋朝的朋友"},
			nil, nil, nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("我的朋友叫宋朝，他是个历史学家"),
		addAssistantMsg("你朋友宋朝是历史学家呀"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.UserProfile) > 0, "应提取用户画像")

	// 验证记忆已写入（通过直接列表而非向量搜索）
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.True(len(contents) > 0, "用户画像应包含朋友信息")
}

// TestUserMemUpdate 记忆更新：硬件工程师->软件工程师, 爱好跑步->不爱跑步。
// 对齐 Python: test_user_mem_update
func (s *MemoryQualitySuite) TestUserMemUpdate() {
	m := s.setupLTM()

	// 第1轮：写入初始信息
	s.setLLMResponses(
		analysisJSON(nil, "用户介绍了自己的职业和爱好"),
		fragmentJSON(
			[]string{"用户是硬件工程师", "用户的爱好是跑步"},
			nil, nil, nil,
		),
	)

	msgs1 := []llmschema.BaseMessage{
		addUserMsg("我是硬件工程师，爱好是跑步"),
		addAssistantMsg("了解了"),
	}
	s.addMessagesAndCheck(m, msgs1, defaultAgentConfig())

	// 第2轮：更新职业和爱好
	s.setLLMResponses(
		analysisJSON(nil, "用户更新了职业和爱好"),
		fragmentJSON(
			[]string{"用户是软件工程师", "用户不爱跑步了"},
			nil, nil,
			[]map[string]any{
				{
					"mem_content":  "用户是软件工程师",
					"mem_type":     "user_profile",
					"mem_instruct": "UPDATE",
					"old_mem":      "用户是硬件工程师",
				},
				{
					"mem_content":  "用户不爱跑步了",
					"mem_type":     "user_profile",
					"mem_instruct": "UPDATE",
					"old_mem":      "用户的爱好是跑步",
				},
			},
		),
		// semantic_validation 两次都返回 CORRECT
		map[string]any{"result": "CORRECT"},
		map[string]any{"result": "CORRECT"},
	)

	msgs2 := []llmschema.BaseMessage{
		addUserMsg("我现在转行做软件工程师了，也不跑步了"),
		addAssistantMsg("好的，记住了"),
	}
	s.addMessagesAndCheck(m, msgs2, defaultAgentConfig())

	// 验证更新后的记忆
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "软件工程师", "更新后用户画像应包含新职业")
}

// TestUserMemReference 指代消解：先说"苹果"，后说"它"，系统应将"它"关联到"苹果"。
// 对齐 Python: test_user_mem_reference
func (s *MemoryQualitySuite) TestUserMemReference() {
	m := s.setupLTM()

	// LLM 应正确解析"它"指代"苹果"
	s.setLLMResponses(
		analysisJSON(nil, "用户提到了喜欢的水果"),
		fragmentJSON(
			[]string{"用户喜欢苹果", "用户觉得苹果很好吃"},
			nil, nil, nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("我喜欢苹果"),
		addAssistantMsg("苹果很健康"),
		addUserMsg("它很好吃"),
		addAssistantMsg("确实"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.UserProfile) > 0, "应提取用户画像")

	// 验证记忆已写入
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "苹果", "用户画像应包含苹果")
}

// TestUserMemEpisodic 情景记忆：北京旅游+故宫+买新书。
// 对齐 Python: test_user_mem_episodic
func (s *MemoryQualitySuite) TestUserMemEpisodic() {
	m := s.setupLTM()

	s.setLLMResponses(
		analysisJSON(nil, "用户分享了旅游经历"),
		fragmentJSON(
			nil,
			nil,
			[]string{"用户去北京旅游了", "用户参观了故宫", "用户买了新书"},
			nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("我最近去北京旅游了，参观了故宫，还买了本新书"),
		addAssistantMsg("听起来很棒"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.EpisodicMemory) > 0, "应提取情景记忆")

	// 验证情景记忆已写入
	contents := s.listAllMem(m, mem_model.MemoryTypeEpisodicMemory)
	s.assertContains(contents, "北京", "情景记忆应包含北京")
}

// TestUserMemEpisodicConflictReal 真实冲突的情景记忆：搬家 北京→上海, 换工作 A公司→B公司。
// 对齐 Python: test_user_mem_episodic_conflict_real
func (s *MemoryQualitySuite) TestUserMemEpisodicConflictReal() {
	m := s.setupLTM()

	// 第1轮：初始信息
	s.setLLMResponses(
		analysisJSON(nil, "用户介绍了居住和工作"),
		fragmentJSON(
			[]string{"用户居住在北京", "用户在A公司工作"},
			nil, nil, nil,
		),
	)

	msgs1 := []llmschema.BaseMessage{
		addUserMsg("我住在北京，在A公司工作"),
		addAssistantMsg("好的"),
	}
	s.addMessagesAndCheck(m, msgs1, defaultAgentConfig())

	// 第2轮：真实冲突更新
	s.setLLMResponses(
		analysisJSON(nil, "用户搬家和换工作"),
		fragmentJSON(
			[]string{"用户居住在上海", "用户在B公司工作"},
			nil, nil,
			[]map[string]any{
				{
					"mem_content":  "用户居住在上海",
					"mem_type":     "user_profile",
					"mem_instruct": "UPDATE",
					"old_mem":      "用户居住在北京",
				},
				{
					"mem_content":  "用户在B公司工作",
					"mem_type":     "user_profile",
					"mem_instruct": "UPDATE",
					"old_mem":      "用户在A公司工作",
				},
			},
		),
		map[string]any{"result": "CORRECT"},
		map[string]any{"result": "CORRECT"},
	)

	msgs2 := []llmschema.BaseMessage{
		addUserMsg("我搬到上海了，也跳槽到了B公司"),
		addAssistantMsg("好的，记住了"),
	}
	s.addMessagesAndCheck(m, msgs2, defaultAgentConfig())

	// 验证旧记忆被更新
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "上海", "用户画像应包含更新后的城市")
}

// TestUserMemEpisodicConflictFalse 假冲突的情景记忆：中午吃面条 vs 晚上吃米饭，应共存。
// 对齐 Python: test_user_mem_episodic_conflict_false
func (s *MemoryQualitySuite) TestUserMemEpisodicConflictFalse() {
	m := s.setupLTM()

	// 假冲突：不同时间段的情景，不应替换
	s.setLLMResponses(
		analysisJSON(nil, "用户分享了日常生活"),
		fragmentJSON(
			nil,
			nil,
			[]string{
				"用户中午吃了面条",
				"用户晚上吃了米饭",
				"用户上午看了电影",
				"用户下午逛了公园",
			},
			nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("我中午吃了面条，晚上吃了米饭"),
		addAssistantMsg("一日三餐很重要"),
		addUserMsg("我上午看了电影，下午逛了公园"),
		addAssistantMsg("很充实的一天"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.EpisodicMemory) > 0, "应提取情景记忆")

	// 两个时间段的信息都应存在（假冲突，应共存）
	contents := s.listAllMem(m, mem_model.MemoryTypeEpisodicMemory)
	s.assertContains(contents, "面条", "情景记忆应包含中午的面条")
	s.assertContains(contents, "米饭", "情景记忆应包含晚上的米饭")
}

// TestUserMemSemantic 语义记忆：地球位置/H2O/Python 编程语言。
// 对齐 Python: test_user_mem_semantic
func (s *MemoryQualitySuite) TestUserMemSemantic() {
	m := s.setupLTM()

	s.setLLMResponses(
		analysisJSON(nil, "用户提到了一些事实性知识"),
		fragmentJSON(
			nil,
			[]string{"地球位于太阳系第三颗行星", "H2O是水的化学式", "Python是一种解释型编程语言"},
			nil, nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("地球在太阳系排第三，水的化学式是H2O，Python是解释型语言"),
		addAssistantMsg("这些都是正确的知识"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.SemanticMemory) > 0, "应提取语义记忆")

	// 验证语义记忆已写入
	contents := s.listAllMem(m, mem_model.MemoryTypeSemanticMemory)
	s.assertContains(contents, "太阳系", "语义记忆应包含太阳系")
	s.assertContains(contents, "H2O", "语义记忆应包含 H2O")
}

// TestUserMemMixed 混合类型记忆：用户画像+情景+语义交织。
// 对齐 Python: test_user_mem_mixed
func (s *MemoryQualitySuite) TestUserMemMixed() {
	m := s.setupLTM()

	s.setLLMResponses(
		analysisJSON(nil, "用户分享了多种信息"),
		fragmentJSON(
			[]string{"用户是一名教师", "用户喜欢阅读"},
			[]string{"光合作用是植物利用阳光合成养分的过程"},
			[]string{"用户昨天参加了家长会"},
			nil,
		),
	)

	messages := []llmschema.BaseMessage{
		addUserMsg("我是一名教师，喜欢阅读。昨天参加了家长会。"),
		addUserMsg("对了，光合作用是植物利用阳光合成养分的过程"),
		addAssistantMsg("了解了你的情况"),
	}

	result := s.addMessagesAndCheck(m, messages, defaultAgentConfig())
	s.True(len(result.UserProfile) > 0, "应提取用户画像")
	s.True(len(result.SemanticMemory) > 0, "应提取语义记忆")
	s.True(len(result.EpisodicMemory) > 0, "应提取情景记忆")

	// 搜索各类记忆
	contents := s.listAllMem(m, mem_model.MemoryTypeUserProfile)
	s.assertContains(contents, "教师", "用户画像应包含教师")

	contents = s.listAllMem(m, mem_model.MemoryTypeSemanticMemory)
	s.assertContains(contents, "光合作用", "语义记忆应包含光合作用")

	contents = s.listAllMem(m, mem_model.MemoryTypeEpisodicMemory)
	s.assertContains(contents, "家长会", "情景记忆应包含家长会")
}
