//go:build integration

package lite

import (
	"testing"

	"github.com/stretchr/testify/suite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/coding_memory"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemorySuite 测试 CodingMemory（lite 包）核心功能。
//
// 覆盖：
//   - MockEmbeddingProvider 确定性向量
//   - CodingMemoryToolContext 工具创建
//   - MemorySettings 默认值
//
// 对齐 Python: tests/system_tests/harness/test_coding_memory_rail_e2e.py
type CodingMemorySuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestCodingMemorySuite(t *testing.T) {
	suite.Run(t, new(CodingMemorySuite))
}

// TestMockEmbedding_确定性向量 测试 MockEmbeddingProvider 对同一输入返回相同向量。
// 对齐 Python: MockEmbeddingProvider.embed_query 使用 md5 hash 做 seed。
func (s *CodingMemorySuite) TestMockEmbedding_确定性向量() {
	ctx := s.Ctx
	emb := lite.NewMockEmbeddingProvider()

	vec1, err := emb.EmbedQuery(ctx, "hello world")
	s.Require().NoError(err)
	s.Len(vec1, 128, "向量维度应为 128")

	vec2, err := emb.EmbedQuery(ctx, "hello world")
	s.Require().NoError(err)

	// 同一输入应返回完全相同的向量
	for i := range vec1 {
		s.Equal(vec1[i], vec2[i], "同一输入的向量元素应相同（索引 %d）", i)
	}

	// 不同输入应返回不同向量
	vec3, err := emb.EmbedQuery(ctx, "different text")
	s.Require().NoError(err)
	different := false
	for i := range vec1 {
		if vec1[i] != vec3[i] {
			different = true
			break
		}
	}
	s.True(different, "不同输入应返回不同向量")
}

// TestMockEmbedding_批量嵌入 测试 MockEmbeddingProvider 批量嵌入。
func (s *CodingMemorySuite) TestMockEmbedding_批量嵌入() {
	ctx := s.Ctx
	emb := lite.NewMockEmbeddingProvider()

	texts := []string{"hello", "world", "test"}
	vecs, err := emb.EmbedDocuments(ctx, texts)
	s.Require().NoError(err)
	s.Len(vecs, 3, "应返回 3 个向量")
	for _, v := range vecs {
		s.Len(v, 128, "每个向量维度应为 128")
	}
}

// TestMockEmbedding_标识信息 测试 MockEmbeddingProvider 的 ID/Model/Dims。
func (s *CodingMemorySuite) TestMockEmbedding_标识信息() {
	emb := lite.NewMockEmbeddingProvider()
	s.Equal("mock", emb.ID())
	s.Equal("mock", emb.Model())
	s.Equal(128, emb.Dims())
}

// TestCodingMemoryToolContext_工具创建 测试 CodingMemoryToolContext 创建工具列表。
// 对齐 Python: test_coding_memory_rail_e2e.test_scenario_switching
func (s *CodingMemorySuite) TestCodingMemoryToolContext_工具创建() {
	tempDir := s.T().TempDir()
	toolCtx := lite.NewCodingMemoryToolContext().
		WithCodingMemoryDir(tempDir).
		WithAgentID("itest_cm_tools")

	tools := coding_memory.CreateCodingMemoryTools(toolCtx, "cn", "itest_cm_tools")
	s.NotEmpty(tools, "CreateCodingMemoryTools 应返回非空工具列表")

	// 验证工具都有名称
	for _, t := range tools {
		card := t.Card()
		s.NotNil(card, "工具应有 Card")
		s.NotEmpty(card.Name, "工具名称不应为空")
	}
}

// TestMemorySettings_默认值 测试 CreateMemorySettings 默认值。
// 对齐 Python: test_coding_memory_rail_e2e.test_scenario_switching
func (s *CodingMemorySuite) TestMemorySettings_默认值() {
	settings := lite.CreateMemorySettings("", nil)
	s.Require().NotNil(settings)
	s.Equal("memory.db", settings.Store.Path, "默认 Store.Path 应为 memory.db")
	s.Equal("mock", settings.Fallback, "默认 Fallback 应为 mock")
	s.Equal("openai_compatible", settings.Provider, "默认 Provider 应为 openai_compatible")
	s.True(settings.Sync.Watch, "默认 Sync.Watch 应为 true")
	s.True(settings.Store.Vector.Enabled, "默认 Vector.Enabled 应为 true")
	s.True(settings.Store.Fts.Enabled, "默认 Fts.Enabled 应为 true")
}
