package config

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/graph"
)

// TestEpisodeType_String 测试 EpisodeType 枚举的字符串表示
func TestEpisodeType_String(t *testing.T) {
	if EpisodeTypeConversation.String() != "CONVERSATION" {
		t.Errorf("期望 CONVERSATION，实际 %s", EpisodeTypeConversation.String())
	}
	if EpisodeTypeDocument.String() != "DOCUMENT" {
		t.Errorf("期望 DOCUMENT，实际 %s", EpisodeTypeDocument.String())
	}
	if EpisodeTypeJSON.String() != "JSON" {
		t.Errorf("期望 JSON，实际 %s", EpisodeTypeJSON.String())
	}
}

// TestEpisodeType_Value 测试 EpisodeType 枚举的整数值
func TestEpisodeType_Value(t *testing.T) {
	if EpisodeTypeConversation.Value() != 0 {
		t.Errorf("CONVERSATION 值期望 0，实际 %d", EpisodeTypeConversation.Value())
	}
	if EpisodeTypeDocument.Value() != 1 {
		t.Errorf("DOCUMENT 值期望 1，实际 %d", EpisodeTypeDocument.Value())
	}
	if EpisodeTypeJSON.Value() != 2 {
		t.Errorf("JSON 值期望 2，实际 %d", EpisodeTypeJSON.Value())
	}
}

// TestAddMemStrategy_默认值 测试 AddMemStrategy 默认值
func TestAddMemStrategy_默认值(t *testing.T) {
	s := DefaultAddMemStrategy
	if !s.ChineseEntity {
		t.Error("ChineseEntity 默认应为 true")
	}
	if s.ChineseEntityDedupe {
		t.Error("ChineseEntityDedupe 默认应为 false")
	}
	if s.ChineseRelation {
		t.Error("ChineseRelation 默认应为 false")
	}
	if s.SkipUUIDDedupe {
		t.Error("SkipUUIDDedupe 默认应为 false")
	}
	if s.SummaryTarget != 250 {
		t.Errorf("SummaryTarget 默认应为 250，实际 %d", s.SummaryTarget)
	}
	if !s.MergeEntities {
		t.Error("MergeEntities 默认应为 true")
	}
	if !s.MergeRelations {
		t.Error("MergeRelations 默认应为 true")
	}
	if !s.MergeFilter {
		t.Error("MergeFilter 默认应为 true")
	}
}

// TestAddMemStrategy_RecallEpisode 测试 AddMemStrategy 中 RecallEpisode 默认值
func TestAddMemStrategy_RecallEpisode(t *testing.T) {
	s := DefaultAddMemStrategy
	if !s.RecallEpisode.ExcludeFutureResults {
		t.Error("RecallEpisode.ExcludeFutureResults 默认应为 true")
	}
	if s.RecallEpisode.MinScore != 0.025 {
		t.Errorf("RecallEpisode.MinScore 默认应为 0.025，实际 %f", s.RecallEpisode.MinScore)
	}
	if s.RecallEpisode.TopK != 3 {
		t.Errorf("RecallEpisode.TopK 默认应为 3，实际 %d", s.RecallEpisode.TopK)
	}
}

// TestAddMemStrategy_RecallEntity 测试 AddMemStrategy 中 RecallEntity 默认值
func TestAddMemStrategy_RecallEntity(t *testing.T) {
	s := DefaultAddMemStrategy
	if s.RecallEntity.MinScore != 0.1 {
		t.Errorf("RecallEntity.MinScore 默认应为 0.1，实际 %f", s.RecallEntity.MinScore)
	}
	if s.RecallEntity.TopK != 3 {
		t.Errorf("RecallEntity.TopK 默认应为 3，实际 %d", s.RecallEntity.TopK)
	}
	// RecallEntity 的 RankConfig 应为 WeightedRankConfig
	if _, ok := s.RecallEntity.RankConfig.(*graph.WeightedRankConfig); !ok {
		t.Errorf("RecallEntity.RankConfig 应为 *WeightedRankConfig，实际 %T", s.RecallEntity.RankConfig)
	}
}

// TestAddMemStrategy_RecallRelation 测试 AddMemStrategy 中 RecallRelation 默认值
func TestAddMemStrategy_RecallRelation(t *testing.T) {
	s := DefaultAddMemStrategy
	if s.RecallRelation.MinScore != 0.02 {
		t.Errorf("RecallRelation.MinScore 默认应为 0.02，实际 %f", s.RecallRelation.MinScore)
	}
	if s.RecallRelation.TopK != 3 {
		t.Errorf("RecallRelation.TopK 默认应为 3，实际 %d", s.RecallRelation.TopK)
	}
	// RecallRelation 的 RankConfig 应为 RRFRankConfig
	if _, ok := s.RecallRelation.RankConfig.(*graph.RRFRankConfig); !ok {
		t.Errorf("RecallRelation.RankConfig 应为 *RRFRankConfig，实际 %T", s.RecallRelation.RankConfig)
	}
}

// TestSearchConfig_默认值 测试 SearchConfig 默认值
func TestSearchConfig_默认值(t *testing.T) {
	sc := DefaultSearchConfig
	if sc.TopK != 3 {
		t.Errorf("TopK 默认应为 3，实际 %d", sc.TopK)
	}
	if sc.MinScore != 0.3 {
		t.Errorf("MinScore 默认应为 0.3，实际 %f", sc.MinScore)
	}
	if sc.BFSK != 3 {
		t.Errorf("BFSK 默认应为 3，实际 %d", sc.BFSK)
	}
	if sc.BFSDepth != 0 {
		t.Errorf("BFSDepth 默认应为 0，实际 %d", sc.BFSDepth)
	}
	if sc.Rerank {
		t.Error("Rerank 默认应为 false")
	}
	if sc.Language != "en" {
		t.Errorf("Language 默认应为 en，实际 %s", sc.Language)
	}
	if sc.FilterExpr != nil {
		t.Error("FilterExpr 默认应为 nil")
	}
	if sc.OutputFields != nil {
		t.Error("OutputFields 默认应为 nil")
	}
}

// TestRetrievalStrategy_默认值 测试 RetrievalStrategy 默认值
func TestRetrievalStrategy_默认值(t *testing.T) {
	rs := DefaultRetrievalStrategy
	if rs.TopK != 3 {
		t.Errorf("TopK 默认应为 3，实际 %d", rs.TopK)
	}
	if rs.MinScore != 0.3 {
		t.Errorf("MinScore 默认应为 0.3，实际 %f", rs.MinScore)
	}
	if rs.SameKind {
		t.Error("SameKind 默认应为 false")
	}
}

// TestEpisodeRetrievalStrategy_默认值 测试 EpisodeRetrievalStrategy 默认值
func TestEpisodeRetrievalStrategy_默认值(t *testing.T) {
	ers := DefaultEpisodeRetrievalStrategy
	if !ers.ExcludeFutureResults {
		t.Error("ExcludeFutureResults 默认应为 true")
	}
	if ers.MinScore != 0.025 {
		t.Errorf("MinScore 默认应为 0.025，实际 %f", ers.MinScore)
	}
	if ers.TopK != 3 {
		t.Errorf("TopK 默认应为 3，实际 %d", ers.TopK)
	}
	if ers.SameKind {
		t.Error("SameKind 默认应为 false")
	}
}

// TestBaseStrategy_默认值 测试 BaseStrategy 默认值
func TestBaseStrategy_默认值(t *testing.T) {
	bs := DefaultBaseStrategy
	if bs.TopK != 3 {
		t.Errorf("TopK 默认应为 3，实际 %d", bs.TopK)
	}
	if bs.MinScore != 0.3 {
		t.Errorf("MinScore 默认应为 0.3，实际 %f", bs.MinScore)
	}
	if bs.RankConfig == nil {
		t.Error("RankConfig 默认不应为 nil")
	}
}

// TestEpisodeType_无效值 测试无效 EpisodeType
func TestEpisodeType_无效值(t *testing.T) {
	et := EpisodeType(99)
	if et.String() != "UNKNOWN(99)" {
		t.Errorf("无效值应返回 UNKNOWN(99)，实际 %s", et.String())
	}
}

// TestNewAddMemStrategy 测试 NewAddMemStrategy 构造函数
func TestNewAddMemStrategy(t *testing.T) {
	s := NewAddMemStrategy()
	if !s.ChineseEntity {
		t.Error("NewAddMemStrategy ChineseEntity 应为 true")
	}
}

// TestNewSearchConfig 测试 NewSearchConfig 构造函数
func TestNewSearchConfig(t *testing.T) {
	sc := NewSearchConfig()
	if sc.Language != "en" {
		t.Errorf("NewSearchConfig Language 应为 en，实际 %s", sc.Language)
	}
}
