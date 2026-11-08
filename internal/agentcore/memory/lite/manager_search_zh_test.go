package lite

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestSearch_中文关键词命中 端到端验证「短中文词走 LIKE 兜底 → 命中」。
//
// 背景：chunks_fts 用 trigram 分词器，2 个汉字的查询（"组网"）无法与 trigram 匹配，
// 必须靠 searchKeyword 的 LIKE 兜底才能命中。向量侧权重置 0，只看关键词路。
func TestSearch_中文关键词命中(t *testing.T) {
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

	// 纯 2 字中文查询：trigram 匹配不到，全靠 LIKE 兜底。
	results, err := m.Search(context.Background(), "组网", SearchOpts{MinScore: 0.01})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("2 字中文查询应命中最少一条（LIKE 兜底未生效？）")
	}
	if results[0].Path != "MEMORY.md" {
		t.Errorf("命中文件 = %q, want MEMORY.md", results[0].Path)
	}
}

// TestSearch_长词走FTS 3 字以上中文由 trigram FTS 命中，且分数应高于 LIKE 固定分。
func TestSearch_长词走FTS(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	m.settings.Query.Hybrid.Enabled = true
	m.settings.Query.Hybrid.VectorWeight = 0
	m.settings.Query.Hybrid.TextWeight = 1
	m.settings.Query.MinScore = 0.01

	dir := filepath.Dir(m.dbPath)
	content := "# 记录\n\nVDS15800 支持手动组网模式，默认关闭。\n"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写 MEMORY.md 失败: %v", err)
	}
	if err := m.Sync(context.Background(), "unit", true); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	// 4 字词 → 长 token，走 FTS trigram
	results, err := m.Search(context.Background(), "组网模式", SearchOpts{MinScore: 0.01})
	if err != nil {
		t.Fatalf("检索失败: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("3 字以上中文查询应命中（trigram FTS 未生效？）")
	}
	if results[0].Score <= likeFallbackScore {
		t.Errorf("FTS 命中分数 %.3f 应高于 LIKE 固定分 %.1f", results[0].Score, likeFallbackScore)
	}
}

// TestSearchKeyword_长短token合并且FTS优先 同一 chunk 被两路命中时只出现一次，且保留 FTS 分数。
func TestSearchKeyword_长短token合并且FTS优先(t *testing.T) {
	m, cleanup := newReindexTestManager(t)
	defer cleanup()

	dir := filepath.Dir(m.dbPath)
	content := "# 记录\n\nVDS15800 支持手动组网模式，默认关闭。\n"
	if err := os.WriteFile(filepath.Join(dir, "MEMORY.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写 MEMORY.md 失败: %v", err)
	}
	if err := m.Sync(context.Background(), "unit", true); err != nil {
		t.Fatalf("同步失败: %v", err)
	}

	// "组网模式" 是长 token（走 FTS），"组网" 是短 token（走 LIKE），同一 chunk 两路都命中
	results, err := m.searchKeyword(context.Background(), "组网模式 组网", 10)
	if err != nil {
		t.Fatalf("searchKeyword 失败: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("同一 chunk 两路命中应去重为 1 条，实际 %d 条", len(results))
	}
	if results[0].Score <= likeFallbackScore {
		t.Errorf("去重后应保留 FTS 分数（> %.1f），实际 %.3f", likeFallbackScore, results[0].Score)
	}
}
