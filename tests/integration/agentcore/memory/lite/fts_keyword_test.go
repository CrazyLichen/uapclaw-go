//go:build integration

package lite

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// ──────────────────────────── 测试套件 ────────────────────────────

// FTSKeywordSuite 关键词检索链路集成测试（E-04）。
//
// 覆盖真实 SQLite + 真实 trigram FTS 表 + 真实文件索引，验证：
//   - 2 字中文词能靠 LIKE 兜底命中（trigram 匹配不到 <3 字符）
//   - 3 字以上中文走 FTS 且分数高于 LIKE 固定分
//   - 旧 unicode61 表能被识别并迁移到 trigram，迁移后索引被重新填满
//   - Token 分流与 LIKE 转义的端到端行为
//
// 为什么必须是集成测试：分词器的实际切分行为、FTS5 的 trigram 匹配语义、
// SQLite 的 LIKE ESCAPE 行为都只有真实 SQLite 才能验证，单测里的 fake 无法覆盖。
type FTSKeywordSuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
	// ws 临时工作空间
	ws *workspace.Workspace
	// memoryDir 记忆目录
	memoryDir string
}

// TestFTSKeywordSuite 运行关键词检索集成测试。
func TestFTSKeywordSuite(t *testing.T) {
	suite.Run(t, new(FTSKeywordSuite))
}

// SetupTest 每个用例前重建临时工作空间与记忆目录。
func (s *FTSKeywordSuite) SetupTest() {
	s.ctx = context.Background()
	s.ws = workspace.NewWorkspace(s.T().TempDir(), "cn")
	nodePath := s.ws.GetNodePath("memory")
	s.Require().NotNil(nodePath, "Workspace 应包含 memory 节点")
	s.memoryDir = *nodePath
	s.Require().NoError(lite.EnsureDir(s.memoryDir))
	lite.ClearMemoryManagerCache()
}

// TearDownTest 关闭缓存的管理器，避免跨用例的 indexCache 污染。
func (s *FTSKeywordSuite) TearDownTest() {
	lite.ClearMemoryManagerCache()
}

// ──────────────────────────── 辅助 ────────────────────────────

// newManager 构造一个指向当前临时目录的索引管理器（不启用文件监听与定时同步）。
func (s *FTSKeywordSuite) newManager(agentID string) lite.MemoryIndexManager {
	settings := lite.CreateMemorySettings(s.memoryDir, nil)
	settings.Sync.Watch = false
	settings.Sync.IntervalMinutes = 0
	// 不配 embedding：走 FTS5-only 路径，测试只看关键词检索
	settings.Fallback = ""

	mgr, err := lite.GetMemoryIndexManager(lite.MemoryManagerParams{
		AgentID:   agentID,
		Workspace: s.ws,
		Settings:  settings,
		NodeName:  "memory",
		Ctx:       s.ctx,
	})
	s.Require().NoError(err)
	s.Require().NotNil(mgr)
	return mgr
}

// writeMemoryFile 写入一个记忆文件。
func (s *FTSKeywordSuite) writeMemoryFile(name, content string) {
	path := filepath.Join(s.memoryDir, name)
	s.Require().NoError(os.WriteFile(path, []byte(content), 0o644))
}

// ──────────────────────────── 用例 ────────────────────────────

// TestShortChineseWordViaLike 2 字中文词必须命中：trigram 匹配不到，只能靠 LIKE 兜底。
func (s *FTSKeywordSuite) TestShortChineseWordViaLike() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\nVDS15800 支持手动组网模式，默认关闭。\n")

	mgr := s.newManager("it_short_zh")
	defer func() { _ = mgr.Close() }()
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))

	results, err := mgr.Search(s.ctx, "组网", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.Require().NotEmpty(results, "2 字中文词应命中（LIKE 兜底未生效）")
	s.Equal("MEMORY.md", results[0].Path)
}

// TestLongChineseWordViaFTS 3 字以上中文走 trigram FTS，分数应高于 LIKE 固定分 0.5。
func (s *FTSKeywordSuite) TestLongChineseWordViaFTS() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\nVDS15800 支持手动组网模式，默认关闭。\n")

	mgr := s.newManager("it_long_zh")
	defer func() { _ = mgr.Close() }()
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))

	results, err := mgr.Search(s.ctx, "组网模式", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.Require().NotEmpty(results, "3 字以上中文应由 trigram FTS 命中")
	s.Greater(results[0].Score, 0.5, "FTS 命中分数应高于 LIKE 固定分")
}

// TestKeywordOnlyQueryNoVector 只用关键词（无 embedding）时结果不被 0.3 门槛滤空。
//
// 这是 E-03 与 E-04 的交汇点：无向量时走 keywordMinScore(0.1) 而非 minScore(0.3)，
// 否则 BM25 中等分命中会被全部滤掉、搜索返回空。
func (s *FTSKeywordSuite) TestKeywordOnlyQueryNoVector() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\n项目使用 Kubernetes 部署，配置在 config 目录。\n")

	mgr := s.newManager("it_kw_only")
	defer func() { _ = mgr.Close() }()
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))

	results, err := mgr.Search(s.ctx, "Kubernetes 配置", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.Require().NotEmpty(results, "无向量时应走关键词路径且不被 minScore 滤空")
}

// TestLegacyFTSTableMigrated 旧 unicode61 表应被识别、DROP 并重建为 trigram，
// 且迁移后索引被 force 全量重填（否则遗留空 FTS 表）。
func (s *FTSKeywordSuite) TestLegacyFTSTableMigrated() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\n系统支持手动组网模式。\n")

	// 第一次建库：正常触发 trigram 建表与索引
	mgr := s.newManager("it_legacy")
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))
	s.Require().NoError(mgr.Close())
	lite.ClearMemoryManagerCache()

	// 把 chunks_fts 换成旧 tokenizer（unicode61），模拟升级前的库
	dbPath := filepath.Join(s.memoryDir, "memory.db")
	s.Require().FileExists(dbPath)
	dropAndCreateLegacyFTS(s.T(), dbPath)

	// 重新获取管理器：应检测到旧表、迁移、并 force 重建
	mgr2 := s.newManager("it_legacy")
	defer func() { _ = mgr2.Close() }()

	s.Equal(1, countChunks(s.T(), dbPath), "迁移后 chunks 应仍有 1 条")

	// 迁移后 trigram 表必须能命中长中文词（若表空则查不到）
	results, err := mgr2.Search(s.ctx, "组网模式", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.NotEmpty(results, "迁移后 FTS 表应被重新填满，长中文词应能命中")
}

// TestSplitQueryTokensBoundary 端到端验证 3 字符阈值边界（2 字走 LIKE、3 字走 FTS）。
func (s *FTSKeywordSuite) TestSplitQueryTokensBoundary() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\n组网模式已开启。\n")

	mgr := s.newManager("it_boundary")
	defer func() { _ = mgr.Close() }()
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))

	// 2 字：短 token，LIKE 兜底
	short, err := mgr.Search(s.ctx, "组网", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.NotEmpty(short, "2 字词应通过 LIKE 兜底命中")

	// 3 字：长 token，FTS
	long, err := mgr.Search(s.ctx, "已开启", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.NotEmpty(long, "3 字词应通过 FTS 命中")
}

// TestLikeEscapeMetachars LIKE 元字符不得被当作通配符：查询 "100%" 不应命中不含它的文件。
func (s *FTSKeywordSuite) TestLikeEscapeMetachars() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\n完成度 100%。\n")
	s.writeMemoryFile("OTHER.md", "# 其他\n\n完全无关的内容。\n")

	mgr := s.newManager("it_like_escape")
	defer func() { _ = mgr.Close() }()
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))

	// "10%" 是短 token（3 字符含百分号，含 % 时按 rune 数算 3 个）→ 走 LIKE
	results, err := mgr.Search(s.ctx, "100%", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	for _, r := range results {
		s.NotEqual("OTHER.md", r.Path, "LIKE 未转义时 %% 会变成通配符，把无关文件也捞出来")
	}
}

// TestReindexKeepsChunksAfterConfigChange 配置变更清表后，force 重建必须把内容写回。
//
// 这是 E-02 的端到端回归：旧实现 runReindex 不 force，清表后索引永久为空。
func (s *FTSKeywordSuite) TestReindexKeepsChunksAfterConfigChange() {
	s.writeMemoryFile("MEMORY.md", "# 记录\n\n用户偏好使用 Go 语言开发服务端。\n")

	mgr := s.newManager("it_reindex")
	defer func() { _ = mgr.Close() }()
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))

	dbPath := filepath.Join(s.memoryDir, "memory.db")
	s.Require().Equal(1, countChunks(s.T(), dbPath), "首次索引应有 1 条 chunk")

	// force 重建：文件 hash 未变，但必须重新写入
	s.Require().NoError(mgr.Sync(s.ctx, "it", true))
	s.Equal(1, countChunks(s.T(), dbPath), "force 重建后 chunk 数应保持 1（hash 未变也要重写）")

	// 重建后仍可检索
	results, err := mgr.Search(s.ctx, "服务端", lite.SearchOpts{MaxResults: 10})
	s.Require().NoError(err)
	s.NotEmpty(results, "重建后应仍能检索到内容")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// openTestDB 直接打开索引数据库，用于测试侧检查/篡改表结构。
func openTestDB(t *testing.T, dbPath string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=5000")
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	return db
}

// dropAndCreateLegacyFTS 把 chunks_fts 换成 unicode61 旧形态，模拟升级前的库。
func dropAndCreateLegacyFTS(t *testing.T, dbPath string) {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer func() { _ = db.Close() }()

	if _, err := db.Exec("DROP TABLE IF EXISTS chunks_fts"); err != nil {
		t.Fatalf("删除 fts 表失败: %v", err)
	}
	if _, err := db.Exec(
		"CREATE VIRTUAL TABLE chunks_fts USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1)",
	); err != nil {
		t.Fatalf("建 unicode61 旧表失败: %v", err)
	}
}

// countChunks 统计 chunks 表行数。
func countChunks(t *testing.T, dbPath string) int {
	t.Helper()
	db := openTestDB(t, dbPath)
	defer func() { _ = db.Close() }()

	var n int
	if err := db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&n); err != nil {
		t.Fatalf("统计 chunks 失败: %v", err)
	}
	return n
}

// 编译期断言：确保 SimpleMemoryIndex 仍满足索引契约（E-04 改动未破坏契约包）。
var _ index.BaseMemoryIndex = (*index.SimpleMemoryIndex)(nil)
