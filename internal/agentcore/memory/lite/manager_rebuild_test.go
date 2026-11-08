package lite

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	apiEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNormalizeEmbeddingBaseURL 验证端点归一化的四种等价写法。
// 对齐 Python normalize_base_url（embeddings.py:51）。
func TestNormalizeEmbeddingBaseURL(t *testing.T) {
	cases := []struct {
		// name 用例名
		name string
		// in 输入
		in string
		// want 期望输出
		want string
	}{
		{"普通端点", "https://x.com/v1", "https://x.com/v1"},
		{"带尾斜杠", "https://x.com/v1/", "https://x.com/v1"},
		{"带 /embeddings 后缀", "https://x.com/v1/embeddings", "https://x.com/v1"},
		{"带 /embeddings 且尾斜杠", "https://x.com/v1/embeddings/", "https://x.com/v1"},
		{"多个尾斜杠", "https://x.com/v1///", "https://x.com/v1"},
		{"前后空白", "  https://x.com/v1  ", "https://x.com/v1"},
		{"空串", "", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := NormalizeEmbeddingBaseURL(tc.in); got != tc.want {
				t.Errorf("NormalizeEmbeddingBaseURL(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestCreateEmbeddingProvider_ConfigFingerprint 验证配置指纹含端点与 key 哈希。
//
// 三个断言对应三件事：① 指纹含归一化后的 base_url；② 指纹含 api_key 的 sha256 前 16 位
// 而不是原文；③ 只换端点或只换 key 都会让指纹变化（这是"配置变更需重建"的判定基础）。
// 对齐 Python OpenAICompatibleEmbeddingProvider.config_fingerprint（embeddings.py:78）。
func TestCreateEmbeddingProvider_ConfigFingerprint(t *testing.T) {
	keyA := "sk-aaa"
	keyB := "sk-bbb"
	cfgA := &apiEmbedding.EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   "https://api.a.com/v1/embeddings",
		APIKey:    keyA,
	}
	pA, err := CreateEmbeddingProvider("openai_compatible", "text-embedding-v3", "", cfgA)
	if err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	fpA := pA.ConfigFingerprint()

	// ① 归一化后的 base_url 参与指纹（原始带 /embeddings，归一化后不带）
	if got := cfgA.BaseURL; got != "https://api.a.com/v1" {
		t.Errorf("base_url 应被归一化为 https://api.a.com/v1，实际 %q", got)
	}
	if want := "openai_compatible:text-embedding-v3:https://api.a.com/v1:" +
		sha256Hex16(keyA); fpA != want {
		t.Errorf("fingerprint = %q, want %q", fpA, want)
	}
	// ② 不落 api_key 原文
	if strings.Contains(fpA, keyA) {
		t.Errorf("fingerprint 不应包含 api_key 原文，实际 %q", fpA)
	}

	// ③ 只换 key → 指纹变
	cfgB := &apiEmbedding.EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   "https://api.a.com/v1",
		APIKey:    keyB,
	}
	pB, err := CreateEmbeddingProvider("openai_compatible", "text-embedding-v3", "", cfgB)
	if err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	if pB.ConfigFingerprint() == fpA {
		t.Error("只换 api_key 时 fingerprint 应变化")
	}

	// ③ 只换端点 → 指纹变
	cfgC := &apiEmbedding.EmbeddingConfig{
		ModelName: "text-embedding-v3",
		BaseURL:   "https://api.b.com/v1",
		APIKey:    keyA,
	}
	pC, err := CreateEmbeddingProvider("openai_compatible", "text-embedding-v3", "", cfgC)
	if err != nil {
		t.Fatalf("创建 provider 失败: %v", err)
	}
	if pC.ConfigFingerprint() == fpA {
		t.Error("只换 base_url 时 fingerprint 应变化")
	}
}

// TestNeedsRebuildOnConfigChange 覆盖初始化期重建判定的四条分支。
//
// 关键分支是 provider 为 nil：端点不可用属运行期健康状态，不能清表。
// 对齐 Python _needs_rebuild_on_config_change（manager.py:376）。
func TestNeedsRebuildOnConfigChange(t *testing.T) {
	t.Run("provider 为 nil 时不清表", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()
		m.provider = nil
		m.providerKey = "none:no-embedding"

		need, _ := m.needsRebuildOnConfigChange()
		if need {
			t.Error("provider 为 nil 属运行期健康问题，不应触发清表重建")
		}
	})

	t.Run("无 meta 时不算配置变更", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()
		m.provider = &fakeEmbedProvider{id: "mock", model: "mock", dims: 1024}
		m.providerKey = "mock:mock"
		if _, err := m.db.Exec("DELETE FROM meta"); err != nil {
			t.Fatalf("删除 meta 失败: %v", err)
		}

		need, _ := m.needsRebuildOnConfigChange()
		if need {
			t.Error("无历史索引时应交给正常 sync 建，不算配置变更")
		}
	})

	t.Run("providerKey 变化时需重建", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()
		m.provider = &fakeEmbedProvider{id: "mock", model: "mock", dims: 1024}
		m.providerKey = "mock:mock:https://new:bbb"
		if err := m.writeMeta(map[string]any{
			"provider":     "mock",
			"model":        "mock",
			"providerKey":  "mock:mock:https://old:aaa",
			"chunkTokens":  float64(256),
			"chunkOverlap": float64(32),
		}); err != nil {
			t.Fatalf("写 meta 失败: %v", err)
		}

		need, reason := m.needsRebuildOnConfigChange()
		if !need || reason != "provider_key_changed" {
			t.Errorf("needsRebuildOnConfigChange() = (%v, %q), want (true, provider_key_changed)", need, reason)
		}
	})

	t.Run("全一致时无需重建", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()
		m.provider = &fakeEmbedProvider{id: "mock", model: "mock", dims: 1024}
		m.providerKey = "mock:mock"
		if err := m.writeMeta(map[string]any{
			"provider":     "mock",
			"model":        "mock",
			"providerKey":  "mock:mock",
			"chunkTokens":  float64(256),
			"chunkOverlap": float64(32),
		}); err != nil {
			t.Fatalf("写 meta 失败: %v", err)
		}

		need, _ := m.needsRebuildOnConfigChange()
		if need {
			t.Error("配置完全一致时不应重建")
		}
	})
}

// TestClearIndexTables 验证清表后 chunks/files 为空，且随后 force 重索引能重新填满。
//
// 这是 E-02 的核心回归点：配置变更 → 清表 → 重建，重建必须真的把内容写回来。
// 旧实现 runReindex 不 force，hash 未变的文件被跳过，清表后索引永远为空。
func TestClearIndexTables(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	nodePath := m.workspace.GetNodePath("memory")
	if nodePath == nil {
		t.Fatal("Workspace 应包含 memory 节点")
	}
	memFile := filepath.Join(*nodePath, "MEMORY.md")
	if err := os.WriteFile(memFile, []byte("# 记忆\n\n用户喜欢用 Go 写服务端程序。\n"), 0o644); err != nil {
		t.Fatalf("写记忆文件失败: %v", err)
	}

	// 先正常同步，让 chunks 有内容
	if err := m.Sync(context.Background(), "test", true); err != nil {
		t.Fatalf("首次同步失败: %v", err)
	}
	before := countRows(t, m, "chunks")
	if before == 0 {
		t.Fatal("首次同步后 chunks 应有内容")
	}

	// 清表
	if err := m.clearIndexTables(); err != nil {
		t.Fatalf("清表失败: %v", err)
	}
	if got := countRows(t, m, "chunks"); got != 0 {
		t.Errorf("清表后 chunks 应为 0，实际 %d", got)
	}
	if got := countRows(t, m, "files"); got != 0 {
		t.Errorf("清表后 files 应为 0，实际 %d", got)
	}

	// force 重建：文件 hash 未变，但必须重新写入（这正是旧实现漏掉的一环）
	if err := m.Sync(context.Background(), "test", true); err != nil {
		t.Fatalf("重建失败: %v", err)
	}
	if got := countRows(t, m, "chunks"); got == 0 {
		t.Error("force 重建后 chunks 不应为空——hash 未变的文件也必须重写")
	}
}

// TestFtsTableIsLegacy 验证旧 tokenizer 表能被识别、trigram 表不被误判、无表不算旧表。
// 对齐 Python _fts_table_is_legacy（manager.py:609）。
func TestFtsTableIsLegacy(t *testing.T) {
	t.Run("当前 trigram 表不算旧表", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()
		if m.ftsTableIsLegacy() {
			t.Error("新建的 trigram 表不应被判为旧表")
		}
	})

	t.Run("unicode61 旧表判为旧表", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()

		// 手工换成 unicode61 的旧形态
		if _, err := m.db.Exec("DROP TABLE IF EXISTS " + ftsTable); err != nil {
			t.Fatalf("删除 fts 表失败: %v", err)
		}
		if _, err := m.db.Exec(
			"CREATE VIRTUAL TABLE " + ftsTable +
				" USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1)",
		); err != nil {
			t.Fatalf("建旧表失败: %v", err)
		}

		if !m.ftsTableIsLegacy() {
			t.Error("无 tokenize=trigram 的表应被判为旧表")
		}
	})

	t.Run("表不存在不算旧表", func(t *testing.T) {
		m, cleanup := newReindexTestManager(t)
		defer cleanup()
		if _, err := m.db.Exec("DROP TABLE IF EXISTS " + ftsTable); err != nil {
			t.Fatalf("删除 fts 表失败: %v", err)
		}
		if m.ftsTableIsLegacy() {
			t.Error("表不存在时不应判为旧表（首次建库）")
		}
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// sha256Hex16 复算 api_key 的 sha256 前 16 位，与生产实现保持一致。
func sha256Hex16(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])[:16]
}

// countRows 统计表行数。
func countRows(t *testing.T, m *memoryIndexManager, table string) int {
	t.Helper()
	var n int
	if err := m.db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
		t.Fatalf("统计 %s 行数失败: %v", table, err)
	}
	return n
}

// TestCloseMemoryManagersFor_只关目标agent 定向关闭：同进程多个 agent 的 manager
// 必须互不影响，否则成员退役会把别人的记忆索引一起关掉。
// 对齐 Python aclose_memory_managers_for（manager.py:1727）。
func TestCloseMemoryManagersFor_只关目标agent(t *testing.T) {
	ws := workspace.NewWorkspace(t.TempDir(), "cn")
	nodePath := ws.GetNodePath("memory")
	if nodePath == nil {
		t.Fatal("Workspace 应包含 memory 节点")
	}
	settings := CreateMemorySettings(*nodePath, nil)
	settings.Sync.Watch = false
	settings.Sync.IntervalMinutes = 0

	open := func(agentID string) *memoryIndexManager {
		mgr, err := GetMemoryIndexManager(MemoryManagerParams{
			AgentID:   agentID,
			Workspace: ws,
			Settings:  settings,
			NodeName:  "memory",
		})
		if err != nil {
			t.Fatalf("初始化 manager 失败: %v", err)
		}
		return mgr.(*memoryIndexManager)
	}
	defer ClearMemoryManagerCache()

	target := open("agent-target")
	other := open("agent-other")

	closed := CloseMemoryManagersFor("agent-target", ws)
	if closed != 1 {
		t.Errorf("应关闭 1 个 manager，实际 %d", closed)
	}
	if !target.IsClosed() {
		t.Error("目标 agent 的 manager 应已关闭")
	}
	if other.IsClosed() {
		t.Error("其他 agent 的 manager 不应被关闭")
	}
}

// TestCloseMemoryManagersFor_不同工作空间不误关 同一 agentID 在不同 workspace 上是
// 不同实例，按根路径匹配才不会误关。
func TestCloseMemoryManagersFor_不同工作空间不误关(t *testing.T) {
	mkWS := func() (*workspace.Workspace, string) {
		ws := workspace.NewWorkspace(t.TempDir(), "cn")
		p := ws.GetNodePath("memory")
		if p == nil {
			t.Fatal("Workspace 应包含 memory 节点")
		}
		return ws, *p
	}
	wsA, dirA := mkWS()
	wsB, dirB := mkWS()

	mkSettings := func(dir string) *MemorySettings {
		s := CreateMemorySettings(dir, nil)
		s.Sync.Watch = false
		s.Sync.IntervalMinutes = 0
		return s
	}

	get := func(ws *workspace.Workspace, dir string) *memoryIndexManager {
		mgr, err := GetMemoryIndexManager(MemoryManagerParams{
			AgentID: "same-agent", Workspace: ws, Settings: mkSettings(dir), NodeName: "memory",
		})
		if err != nil {
			t.Fatalf("初始化失败: %v", err)
		}
		return mgr.(*memoryIndexManager)
	}
	defer ClearMemoryManagerCache()

	onA := get(wsA, dirA)
	onB := get(wsB, dirB)

	if closed := CloseMemoryManagersFor("same-agent", wsA); closed != 1 {
		t.Errorf("应只关闭 wsA 上的 1 个，实际 %d", closed)
	}
	if !onA.IsClosed() {
		t.Error("wsA 的 manager 应已关闭")
	}
	if onB.IsClosed() {
		t.Error("wsB 的 manager 不应被关闭")
	}
}
