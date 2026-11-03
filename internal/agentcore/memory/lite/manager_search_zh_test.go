package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSearch_中文关键词命中 端到端验证「索引写分词文本 → 查询分词 → 命中」。
//
// 向量侧权重置 0，排除 embedding 干扰，只看 FTS 文本路。
func TestSearch_中文关键词命中(t *testing.T) {
	if id := segmenterID(); id == "none" {
		t.Skip("分词器不可用，跳过中文检索用例")
	}

	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.settings.Query.Hybrid.Enabled = true
	m.settings.Query.Hybrid.VectorWeight = 0
	m.settings.Query.Hybrid.TextWeight = 1
	// minScore 必须 > 0：向量侧权重为 0，纯向量命中会得到 0 分，
	// 用 0 做阈值会把它们放进来（mock 向量是随机值，可能碰巧相似）。
	m.settings.Query.MinScore = 0.01

	dir := filepath.Dir(m.dbPath)
	content := "# 记录\n\nVDS15800 支持手动组网模式，默认关闭。\n"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写 MEMORY.md 失败: %v", err)
	}

	if err := m.Sync(context.Background(), "unit", true); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	// 纯中文查询：未分词时 unicode61 会把「支持手动组网模式」当成单个 token，
	// 「组网」匹配不到；分词后 token 对齐才能命中。
	results, err := m.Search(context.Background(), "组网", SearchOpts{MinScore: 0.01})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("中文查询应命中至少一条（索引侧未写分词文本？）")
	}
	if results[0].Path != "MEMORY.md" {
		t.Errorf("命中文件 = %q, want MEMORY.md", results[0].Path)
	}
}
