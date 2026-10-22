//go:build integration && llm

package real_llm

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/db"
	kv "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/kv"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/vector"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/ltm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	commonschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ltmRealDbStore SQLite 文件数据库存储（真实持久化，对齐 Python DefaultDbStore）。
type ltmRealDbStore struct {
	gormDB *gorm.DB
}

// LTMRealSuite 真实 LLM + 真实 embedding + 真实 Chroma + SQLite 下
// LongTermMemory E2E 测试套件。
//
// 对齐 Python: tests/system_tests/memory/test_long_term_memory.py 的
// test_memory_sample（@unittest.skip("skip system test")，Python 从未运行）
// + test_memory_quality.py 的真跑变体（Go 端 mock 版见
// tests/integration/agentcore/memory/ltm/memory_quality_test.go）。
//
// 覆盖（对齐 Python test_memory_sample）：
//   - RegisterStore（Chroma 持久化 + SQLite + InMemoryKVStore + APIEmbedding）
//   - SetConfig（真实 LLM 提取模型）
//   - SetScopeConfig（默认提取规则）
//   - AddMessages 三轮对话（姓名/职业/兴趣）
//   - GetUserMemByPage 记忆已捕获
//   - SearchUserMem 三类检索（姓名/工作/兴趣）
//   - GetVariables 变量提取（姓名=Tom、职业=数据分析师）
//
// Go 与 Python 差异：
//   - Python 用 create_vector_store("chroma", persist_directory=resource_dir)；
//     Go 用 vector.NewChromaVectorStore(persistPath)（chroma-go PersistentClient）。
//   - Python DefaultDbStore(create_async_engine(sqlite+aiosqlite))；
//     Go 用 gorm sqlite（CGO，需 sqlite_fts5 build tag，与 CI 集成测试一致）。
//   - Python APIEmbedding(embed_config)；Go 的 APIEmbedding 直接满足
//     store/embedding.BaseEmbedding 接口，作为 WithEmbeddingModel 注入。
type LTMRealSuite struct {
	isuite.RealLLMSuite
	// resourceDir 资源目录（Chroma 持久化 + SQLite db）
	resourceDir string
	// scopeID 测试作用域
	scopeID string
	// userID 测试用户
	userID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestLTMRealSuite 运行真实 LTM E2E 测试套件
// 运行方式: go test -tags="sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
func TestLTMRealSuite(t *testing.T) {
	suite.Run(t, new(LTMRealSuite))
}

// SetupTest 为每个测试方法创建独立资源目录。
// 守卫放在测试方法内（skipUnlessRealLLMTests），避免连带跳过包引用验证测试。
func (s *LTMRealSuite) SetupTest() {
	if !s.realLLMTestsEnabled() {
		return
	}
	s.SkipIfNoRealLLM()
	s.SkipIfNoEmbedding()

	s.resourceDir = s.T().TempDir()
	s.scopeID = fmt.Sprintf("ltm_real_scope_%d", time.Now().UnixNano())
	s.userID = fmt.Sprintf("ltm_real_user_%d", time.Now().UnixNano())
}

// TestLTMReal_真实LLM_记忆样本 测试真实 LLM+embedding+Chroma 下完整记忆工作流。
// 对齐 Python: test_memory_sample
func (s *LTMRealSuite) TestLTMReal_真实LLM_记忆样本() {
	s.skipUnlessRealLLMTests()
	s.SkipIfNoRealLLM()
	s.SkipIfNoEmbedding()

	ctx, cancel := context.WithTimeout(s.Ctx, 10*time.Minute)
	defer cancel()

	m := s.setupLTM(ctx)

	// 对齐 Python: AgentMemoryConfig(mem_variables=[Param.string("姓名"...), Param.string("职业"...)], enable_* 全开)
	agentCfg := &config.AgentMemoryConfig{
		MemVariables: []commonschema.Param{
			*commonschema.NewStringParam("姓名", "用户姓名", false),
			*commonschema.NewStringParam("职业", "用户职业", false),
		},
		EnableLongTermMem:    true,
		EnableUserProfile:    true,
		EnableSemanticMemory: true,
		EnableEpisodicMemory: true,
		EnableSummaryMemory:  true,
	}

	// 三轮对话（对齐 Python messages1/2/3 原文）
	conversations := [][]llmschema.BaseMessage{
		{
			llmschema.NewUserMessage("你好，我是Tom"),
			llmschema.NewAssistantMessage("你好Tom，很高兴认识你"),
		},
		{
			llmschema.NewUserMessage("我是一名数据分析师"),
			llmschema.NewAssistantMessage("数据分析是个很有前景的领域"),
		},
		{
			llmschema.NewUserMessage("业余时间我喜欢阅读"),
			llmschema.NewAssistantMessage("阅读都是很好的爱好"),
		},
	}

	for i, messages := range conversations {
		result, err := m.AddMessages(
			ctx,
			messages,
			agentCfg,
			ltm.WithScopeID(s.scopeID),
			ltm.WithUserID(s.userID),
			ltm.WithSessionID(fmt.Sprintf("ltm_real_session_%d", i+1)),
			ltm.WithGenMem(true),
		)
		s.Require().NoError(err, "第 %d 轮 AddMessages 失败", i+1)
		s.Require().NotNil(result, "第 %d 轮 AddMemResult 不应为 nil", i+1)
	}

	// 对齐 Python: get_user_mem_by_page(page_size=50, page_idx=1) 验证记忆已捕获
	allMemories, err := m.GetUserMemByPage(
		ctx, 50, 1, mem_model.MemoryTypeUnknown,
		ltm.Uid(s.userID), ltm.Sid(s.scopeID),
	)
	s.Require().NoError(err, "GetUserMemByPage 失败")
	s.NotEmpty(allMemories, "应至少捕获一条记忆（真实 LLM 提取）")

	// 对齐 Python search_tests：三类检索（真实 embedding 向量相似度）
	searchTests := []struct {
		query           string
		expectedContext string
	}{
		{"用户的姓名", "Tom"},
		{"用户的工作", "数据分析师"},
		{"用户的兴趣爱好", "阅读"},
	}
	for _, tc := range searchTests {
		results, err := m.SearchUserMem(
			ctx, tc.query, 3,
			ltm.SearchWithUserID(s.userID),
			ltm.SearchWithScopeID(s.scopeID),
			ltm.SearchWithThreshold(0.3),
		)
		s.Require().NoError(err, "SearchUserMem(%s) 失败", tc.query)
		s.NotEmpty(results, "检索 %q 应返回结果", tc.query)

		// 对齐 Python: mem_found 检查期望内容出现在某条记忆中
		memFound := false
		for _, r := range results {
			if r.MemInfo != nil && strings.Contains(r.MemInfo.Content, tc.expectedContext) {
				memFound = true
				break
			}
		}
		s.True(memFound, "检索 %q 应命中包含 %q 的记忆", tc.query, tc.expectedContext)
	}

	// 对齐 Python: get_variables 验证变量提取
	variables, err := m.GetVariables(ctx, nil, ltm.Uid(s.userID), ltm.Sid(s.scopeID))
	s.Require().NoError(err, "GetVariables 失败")

	nameVal, hasName := variables["姓名"]
	s.True(hasName, "应提取出 姓名 变量（真实 LLM）")
	s.Equal("Tom", nameVal, "姓名变量值应为 Tom")

	jobVal, hasJob := variables["职业"]
	s.True(hasJob, "应提取出 职业 变量（真实 LLM）")
	s.Equal("数据分析师", jobVal, "职业变量值应为 数据分析师")
}

// TestLTMRealDoc_包引用验证 验证引用的 internal 包可正常访问
func (s *LTMRealSuite) TestLTMRealDoc_包引用验证() {
	s.NotNil(llm.NewModel)
	s.NotNil(vector.NewChromaVectorStore)
	s.NotNil(ltm.NewLongTermMemory)
	s.NotNil(apiEmbedding.NewAPIEmbedding)
	s.NotNil(config.DefaultMemoryEngineConfig)
	s.NotNil(mem_model.MemoryTypeUnknown)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// realLLMTestsEnabled 检查 RUN_REAL_LLM_TESTS 开关是否开启。
// 对齐 Python: os.getenv("RUN_REAL_LLM_TESTS", "0") != "1" → pytest.skip
func (s *LTMRealSuite) realLLMTestsEnabled() bool {
	return os.Getenv("RUN_REAL_LLM_TESTS") == "1"
}

// skipUnlessRealLLMTests 在 RUN_REAL_LLM_TESTS 未开启时跳过测试。
// 对齐 Python: pytest.skip("Real LLM test skipped. Set RUN_REAL_LLM_TESTS=1 to enable.")
func (s *LTMRealSuite) skipUnlessRealLLMTests() {
	if !s.realLLMTestsEnabled() {
		s.T().Skip("Real LLM 测试未启用。设置 RUN_REAL_LLM_TESTS=1 开启（对齐 Python 守卫）。")
	}
}

// setupLTM 创建完整 LTM：Chroma 持久化 + SQLite 文件 + InMemoryKV + APIEmbedding。
// 对齐 Python asyncSetUp 的 store 注册 + set_config + set_scope_config。
func (s *LTMRealSuite) setupLTM(ctx context.Context) *ltm.LongTermMemory {
	ltm.ResetLongTermMemory()
	m := ltm.NewLongTermMemory()

	kvStore := kv.NewInMemoryKVStore()
	vecStore := vector.NewChromaVectorStore(s.resourceDir)
	dbStore := s.newSQLiteDbStore()
	embModel := apiEmbedding.NewAPIEmbedding(*s.NewRealEmbeddingConfig())

	err := m.RegisterStore(
		ctx,
		kvStore,
		ltm.WithVectorStore(vecStore),
		ltm.WithDbStore(dbStore),
		ltm.WithEmbeddingModel(embModel),
	)
	s.Require().NoError(err, "RegisterStore 失败")

	// 对齐 Python: set_config（真实 LLM 配置）
	engineCfg := config.DefaultMemoryEngineConfig()
	engineCfg.DefaultModelCfg = s.RealModelConfig
	engineCfg.DefaultModelClientCfg = s.RealClientConfig
	err = m.SetConfig(engineCfg)
	s.Require().NoError(err, "SetConfig 失败")

	// 对齐 Python: set_scope_config（默认定义）
	err = m.SetScopeConfig(ctx, s.scopeID, config.DefaultMemoryScopeConfig())
	s.Require().NoError(err, "SetScopeConfig 失败")

	return m
}

// newSQLiteDbStore 创建 SQLite 文件数据库存储。
// 对齐 Python: DefaultDbStore(create_async_engine(f"sqlite+aiosqlite:///{db_path}"))
func (s *LTMRealSuite) newSQLiteDbStore() *ltmRealDbStore {
	dbPath := filepath.Join(s.resourceDir, "ltm_real_test.db")
	gormDB, err := gorm.Open(sqlite.Open(dbPath), &gorm.Config{})
	s.Require().NoError(err, "打开 SQLite 数据库失败")
	return &ltmRealDbStore{gormDB: gormDB}
}

// GetDB 实现 db.BaseDbStore 接口。
func (s *ltmRealDbStore) GetDB(_ context.Context) *gorm.DB {
	return s.gormDB
}

// 编译时验证 ltmRealDbStore 满足 db.BaseDbStore 接口
var _ db.BaseDbStore = (*ltmRealDbStore)(nil)
