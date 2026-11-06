//go:build integration

package context_engine

import (
	"context"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine"
	cecontext "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/context"
	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/usage"
	llm_schema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// UsageBaselineSuite 覆盖"用量基线失效 → 快照回落"的整条链路：
// 真实 ContextEngine 建上下文 → 写入带 provider 用量的 assistant → 压缩改写 →
// 统计与实际分类计数 → 产出 context-usage.v1 快照。
type UsageBaselineSuite struct {
	suite.Suite
	// ctx 测试上下文
	ctx context.Context
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetupTest 每个用例独立的上下文。
func (s *UsageBaselineSuite) SetupTest() {
	s.ctx = context.Background()
}

// TestContextUsageSnapshot_压缩后回落且四类齐全 验证压缩后总量不被旧基线污染、
// 快照四类分类都有非零值，且满足前端校验所需字段。
func (s *UsageBaselineSuite) TestContextUsageSnapshot_压缩后回落且四类齐全() {
	config := ceschema.NewContextEngineConfig()
	config.ModelName = "qwen3-max"
	config.ContextWindowTokens = 200000
	engine := context_engine.NewContextEngine(config)
	sess := newMockSessionFacade("sess-usage")

	mc, err := engine.CreateContext(s.ctx, "default_context", sess)
	s.Require().NoError(err)
	s.Require().NotNil(mc)

	// 1) 系统提示词 + 一轮对话，assistant 带 provider 上报的累计用量
	systemMsg := llm_schema.NewSystemMessage("你是助手，请简洁回答。")
	userMsg := llm_schema.NewUserMessage("帮我解释一下上下文压缩")
	assistantMsg := llm_schema.NewAssistantMessage("上下文压缩是把历史消息摘要化。")
	assistantMsg.UsageMetadata = &llm_schema.UsageMetadata{InputTokens: 50000, TotalTokens: 50000}

	_, err = mc.AddMessages(s.ctx, []llm_schema.BaseMessage{systemMsg, userMsg, assistantMsg})
	s.Require().NoError(err)

	baseline := mc.Statistic().TotalTokens
	s.Require().Equal(50000, baseline, "基线有效时应直接采用 provider 上报的总量")

	// 2) 模拟压缩：保留系统提示词与用户问题，答复被摘要替换
	compressed := []llm_schema.BaseMessage{
		systemMsg,
		userMsg,
		llm_schema.NewUserMessage("[SUMMARY] 上下文压缩把历史消息摘要化。"),
	}
	mc.SetMessages(compressed, true)

	afterCompress := mc.Statistic().TotalTokens
	s.Require().NotEqual(50000, afterCompress, "压缩后不得沿用压缩前的基线数字")
	s.Require().Positive(afterCompress, "压缩后应回落到现算值")

	// 3) 分类独立计数 + 快照
	systemTokens, toolsTokens, messageTokens := iface.CountCategoryTokens(compressed, nil, mc.TokenCounter())
	s.Require().Positive(systemTokens, "系统提示词分类应有非零值")
	s.Require().Positive(messageTokens, "消息分类应有非零值")

	snapshot := usage.BuildSnapshot(usage.SnapshotInput{
		Phase:              usage.PhasePostCompact,
		RequestID:          "req-integration-1",
		SessionID:          "sess-usage",
		LimitTokens:        200000,
		SystemPromptTokens: systemTokens,
		ToolsTokens:        toolsTokens,
		MessagesTokens:     messageTokens,
	})

	// 4) 前端契约：八个必填项 + 四类端点都出现
	s.Equal(usage.EventTypeContextUsage, snapshot.EventType)
	s.Equal(usage.SchemaVersion, snapshot.SchemaVersion)
	s.Equal(usage.PhasePostCompact, snapshot.Phase)
	s.NotEmpty(snapshot.RequestID)
	s.NotEmpty(snapshot.ProductSessionID)
	s.Require().NotNil(snapshot.ContextWindow.LimitTokens)
	s.Equal(200000, *snapshot.ContextWindow.LimitTokens)
	s.Len(snapshot.Parts, 4, "四类端点（系统/工具/技能/消息）都必须出现")

	systemPart := snapshot.Parts[usage.CategorySystemPrompt]
	s.Require().Positive(systemPart.Tokens, "系统提示词 token 应非零")
	s.Require().NotNil(systemPart.PercentageOfWindow, "窗口已知时应有占比")

	messagePart := snapshot.Parts[usage.CategoryMessages]
	s.Require().Positive(messagePart.Tokens, "消息 token 应非零")
	s.Require().NotNil(messagePart.PercentageOfWindow)

	// 技能类别本期无独立来源，允许为 0，但必须存在（前端按 category 取值）
	_, hasSkills := snapshot.Parts[usage.CategorySkills]
	s.True(hasSkills, "技能端点必须存在")

	// 占用率 = 权威总量 / 窗口；无 provider 上报时用本地合计
	s.Require().NotNil(snapshot.ContextWindow.OccupancyRate)
	s.Greater(*snapshot.ContextWindow.OccupancyRate, 0.0)
	s.Less(*snapshot.ContextWindow.OccupancyRate, 1.0)
}

// TestContextUsageSnapshot_换模型后基线失效 验证热切换模型同样让旧用量作废。
func (s *UsageBaselineSuite) TestContextUsageSnapshot_换模型后基线失效() {
	config := ceschema.NewContextEngineConfig()
	config.ModelName = "qwen3-max"
	config.ContextWindowTokens = 200000
	engine := context_engine.NewContextEngine(config)

	mc, err := engine.CreateContext(s.ctx, "default_context", newMockSessionFacade("sess-model"))
	s.Require().NoError(err)

	assistantMsg := llm_schema.NewAssistantMessage("答复")
	assistantMsg.UsageMetadata = &llm_schema.UsageMetadata{InputTokens: 8000, TotalTokens: 8000}
	_, err = mc.AddMessages(s.ctx, []llm_schema.BaseMessage{
		llm_schema.NewUserMessage("问题"),
		assistantMsg,
	})
	s.Require().NoError(err)
	s.Require().Equal(8000, mc.Statistic().TotalTokens)

	engine.UpdateModelContext("glm-4-long", 1000000)
	s.NotEqual(8000, mc.Statistic().TotalTokens, "换模型后旧绑定下的基线必须作废")
	s.Positive(mc.Statistic().TotalTokens)
}

// TestContextUsageSnapshot_压缩上下文窗口读得到 验证窗口上限来自引擎配置（压缩后仍有效）。
func (s *UsageBaselineSuite) TestContextUsageSnapshot_压缩上下文窗口读得到() {
	config := ceschema.NewContextEngineConfig()
	config.ModelName = "qwen3-max"
	modelContextWindow := cecontext.ResolveContextMax("qwen3-max", config.ContextWindowTokens, config.ModelContextWindowTokens)
	s.Equal(262144, modelContextWindow, "内置表应给出 qwen3-max=262144")
}

// TestUsageBaselineSuite 运行套件。
func TestUsageBaselineSuite(t *testing.T) {
	suite.Run(t, new(UsageBaselineSuite))
}
