package skill_call

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
)

// ──────────────────────────── 导出函数 ────────────────────────────

func TestNewTeamSkillExperienceOptimizer(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "qwen-max", "cn", "/tmp/debug", TeamSkillRecordLLMPolicy, nil)
	assert.NotNil(t, opt)
	assert.Equal(t, "qwen-max", opt.ModelName())
	assert.Equal(t, "cn", opt.Language())
	assert.Equal(t, "/tmp/debug", opt.debugDir)
	assert.Equal(t, TeamSkillRecordLLMPolicy, opt.recordLLMPolicy)
}

func TestTeamSkillExperienceOptimizer_Domain(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "qwen-max", "cn", "", TeamSkillRecordLLMPolicy, nil)
	assert.Equal(t, "skill_experience", opt.Domain())
}

func TestTeamSkillExperienceOptimizer_DefaultTargets(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "qwen-max", "cn", "", TeamSkillRecordLLMPolicy, nil)
	assert.Equal(t, []string{"experiences"}, opt.DefaultTargets())
}

func TestTeamSkillExperienceOptimizer_RequiresForwardData(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "qwen-max", "cn", "", TeamSkillRecordLLMPolicy, nil)
	assert.True(t, opt.RequiresForwardData())
}

func TestTeamSkillOptimizer_类型别名(t *testing.T) {
	// TeamSkillOptimizer 是 TeamSkillExperienceOptimizer 的类型别名
	var opt = TeamSkillExperienceOptimizer{}
	assert.IsType(t, TeamSkillExperienceOptimizer{}, opt)
}

func TestParsePatchResponse_正常(t *testing.T) {
	raw := `{"need_patch": true, "section": "Collaboration", "content": "test"}`
	parsed, errStr := parsePatchResponse(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, parsed)
	assert.Equal(t, true, parsed["need_patch"])
}

func TestParsePatchResponse_非dict(t *testing.T) {
	raw := `[{"action": "append"}]`
	parsed, errStr := parsePatchResponse(raw)
	assert.Nil(t, parsed)
	assert.NotEmpty(t, errStr)
}

func TestParsePatchResponse_无效JSON(t *testing.T) {
	raw := "not json"
	parsed, errStr := parsePatchResponse(raw)
	assert.Nil(t, parsed)
	assert.NotEmpty(t, errStr)
}

func TestSummarizeSkillContentTeam_截断(t *testing.T) {
	long := strings.Repeat("a", 7000)
	result := summarizeSkillContentTeam(long)
	assert.True(t, len(result) <= TeamSkillContentMaxChars+100)
	assert.Contains(t, result, "truncated")
}

func TestSummarizeSkillContentTeam_短内容(t *testing.T) {
	short := "short content"
	result := summarizeSkillContentTeam(short)
	assert.Equal(t, short, result)
}

func TestSummarizeSkillContentTeam_空内容(t *testing.T) {
	result := summarizeSkillContentTeam("")
	assert.Equal(t, "", result)
}

func TestShortenExistingEvolutionsSummary_截断(t *testing.T) {
	summary := "- [ev_001] [Troubleshooting] content1\n- [ev_002] [Instructions] content2\n- [ev_003] [Scripts] content3"
	result := shortenExistingEvolutionsSummary(summary, 2)
	assert.Contains(t, result, "ev_001")
	assert.Contains(t, result, "ev_002")
	assert.NotContains(t, result, "ev_003")
}

func TestShortenExistingEvolutionsSummary_空输入(t *testing.T) {
	result := shortenExistingEvolutionsSummary("", 2)
	assert.Equal(t, "", result)
}

func TestSummarizeExistingEvolutions_有记录(t *testing.T) {
	records := []checkpointing.EvolutionRecord{
		{ID: "ev_001", Change: checkpointing.EvolutionPatch{Section: "Troubleshooting", Content: "test content"}},
	}
	result := summarizeExistingEvolutions(records, "cn")
	assert.Contains(t, result, "已有演进经验：")
	assert.Contains(t, result, "ev_001")
}

func TestSummarizeExistingEvolutions_无记录(t *testing.T) {
	result := summarizeExistingEvolutions(nil, "cn")
	assert.Equal(t, "无已有演进经验", result)
}

func TestSummarizeExistingEvolutions_英文(t *testing.T) {
	result := summarizeExistingEvolutions(nil, "en")
	assert.Equal(t, "No existing evolution records", result)
}

func TestLangDefault(t *testing.T) {
	assert.Equal(t, "无", langDefault("无", "None", "cn"))
	assert.Equal(t, "None", langDefault("无", "None", "en"))
}

func TestTeamSkillOptimizer_OrElse(t *testing.T) {
	assert.Equal(t, "value", orString("value", "fallback"))
	assert.Equal(t, "fallback", orString("", "fallback"))
}

func TestGetStrFromAny(t *testing.T) {
	assert.Equal(t, "hello", getStrFromAny("hello", "default"))
	assert.Equal(t, "default", getStrFromAny(nil, "default"))
	assert.Equal(t, "123", getStrFromAny(123, "default"))
}

func TestTeamExtractJSONWithError_正常(t *testing.T) {
	raw := `{"key": "value"}`
	result, errStr := teamExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

func TestTeamExtractJSONWithError_空(t *testing.T) {
	result, errStr := teamExtractJSONWithError("")
	assert.Nil(t, result)
	assert.Equal(t, "empty response", errStr)
}

func TestTeamExtractJSONWithError_数组提取(t *testing.T) {
	raw := `some text [{"action":"append"}] more`
	result, errStr := teamExtractJSONWithError(raw)
	assert.Equal(t, "", errStr)
	assert.NotNil(t, result)
}

// ──────────────────────────── loadSkillContent 测试 ────────────────────────────

// TestLoadSkillContent_nilStore nil evolutionStore 时返回 fallback
func TestLoadSkillContent_nilStore(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, nil)
	assert.Equal(t, "无", opt.loadSkillContent(context.Background(), "skill"))
	optEn := NewTeamSkillExperienceOptimizer(nil, "test", "en", "", TeamSkillRecordLLMPolicy, nil)
	assert.Equal(t, "N/A", optEn.loadSkillContent(context.Background(), "skill"))
}

// TestLoadSkillContent_正常内容 有 store 且返回有效内容时使用内容
func TestLoadSkillContent_正常内容(t *testing.T) {
	store := &mockEvolutionStore{
		readSkillContentFn: func(_ context.Context, _ string) (string, error) {
			return "skill body content", nil
		},
	}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadSkillContent(context.Background(), "my_skill")
	assert.Equal(t, "skill body content", result)
}

// TestLoadSkillContent_错误返回 有 store 但 ReadSkillContent 返回错误时降级 fallback
func TestLoadSkillContent_错误返回(t *testing.T) {
	store := &mockEvolutionStore{
		readSkillContentFn: func(_ context.Context, _ string) (string, error) {
			return "", fmt.Errorf("skill not found")
		},
	}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadSkillContent(context.Background(), "missing_skill")
	assert.Equal(t, "无", result)
}

// TestLoadSkillContent_空内容 有 store 但返回空字符串时降级 fallback
func TestLoadSkillContent_空内容(t *testing.T) {
	store := &mockEvolutionStore{
		readSkillContentFn: func(_ context.Context, _ string) (string, error) {
			return "   ", nil // 只有空白
		},
	}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "en", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadSkillContent(context.Background(), "empty_skill")
	assert.Equal(t, "N/A", result)
}

// ──────────────────────────── loadExistingEvolutionsSummary 测试 ────────────────────────────

// TestLoadExistingEvolutionsSummary_nilStore nil evolutionStore 时返回 fallback
func TestLoadExistingEvolutionsSummary_nilStore(t *testing.T) {
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, nil)
	assert.Equal(t, "无已有演进经验", opt.loadExistingEvolutionsSummary(context.Background(), "skill"))
	optEn := NewTeamSkillExperienceOptimizer(nil, "test", "en", "", TeamSkillRecordLLMPolicy, nil)
	assert.Equal(t, "No existing evolution records", optEn.loadExistingEvolutionsSummary(context.Background(), "skill"))
}

// TestLoadExistingEvolutionsSummary_有记录 有 store 且返回有效演进日志时汇总
func TestLoadExistingEvolutionsSummary_有记录(t *testing.T) {
	store := &mockEvolutionStore{
		loadFullEvolutionLogFn: func(_ context.Context, _ string) (*checkpointing.EvolutionLog, error) {
			return &checkpointing.EvolutionLog{
				Entries: []checkpointing.EvolutionRecord{
					{ID: "ev_001", Change: checkpointing.EvolutionPatch{Section: "Troubleshooting", Content: "fix bug"}},
				},
			}, nil
		},
	}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "my_skill")
	assert.Contains(t, result, "已有演进经验：")
	assert.Contains(t, result, "ev_001")
}

// TestLoadExistingEvolutionsSummary_错误返回 有 store 但 LoadFullEvolutionLog 返回错误时降级 fallback
func TestLoadExistingEvolutionsSummary_错误返回(t *testing.T) {
	store := &mockEvolutionStore{
		loadFullEvolutionLogFn: func(_ context.Context, _ string) (*checkpointing.EvolutionLog, error) {
			return nil, fmt.Errorf("skill not found")
		},
	}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "cn", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "missing_skill")
	assert.Equal(t, "无已有演进经验", result)
}

// TestLoadExistingEvolutionsSummary_nil日志 有 store 但返回 nil EvolutionLog 时降级 fallback
func TestLoadExistingEvolutionsSummary_nil日志(t *testing.T) {
	store := &mockEvolutionStore{
		loadFullEvolutionLogFn: func(_ context.Context, _ string) (*checkpointing.EvolutionLog, error) {
			return nil, nil
		},
	}
	opt := NewTeamSkillExperienceOptimizer(nil, "test", "en", "", TeamSkillRecordLLMPolicy, store)
	result := opt.loadExistingEvolutionsSummary(context.Background(), "empty_skill")
	assert.Equal(t, "No existing evolution records", result)
}
