//go:build integration

package progressive

import (
	"context"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	hsections "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProgressiveToolRailSuite 测试延迟工具目录的曝光、检索与执行闭环。
//
// 覆盖：
//   - 曝光标签映射到系统提示词（目录摘要）与顶层工具过滤
//   - deferred 工具未经 tool_search 不可直接调用
//   - tool_search + tool_call 的真实检索与嵌套执行
//   - 目录变更推送 delta / 同会话不重复推送
//   - schema 变更后授权失效，必须重新搜索
//
// Python: openjiuwen/harness/rails/progressive_tool_rail.py
type ProgressiveToolRailSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestProgressiveToolRailSuite(t *testing.T) {
	suite.Run(t, new(ProgressiveToolRailSuite))
}

// TestProgressive_ExposureMapsToPrompt 验证曝光标签真的决定目录摘要与不可直调。
func (s *ProgressiveToolRailSuite) TestProgressive_ExposureMapsToPrompt() {
	var xlsxCalls atomic.Int32
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("完成"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances: []tool.Tool{
			s.newCountingTool("xlsx_read", "读取 Excel 表格", &xlsxCalls),
			s.newCountingTool("read_file", "读取文件", nil),
		},
		ProgressiveToolEnabled: true,
		MaxIterations:          3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "你好"})
	s.Require().NoError(err)

	abilityManager := agent.AbilityManager()
	s.Require().NotNil(abilityManager)

	deferredCard, ok := abilityManager.Get("xlsx_read").(*tool.ToolCard)
	s.Require().True(ok)
	s.Equal(tool.ToolExposureDeferred, deferredCard.EffectiveExposure(), "非名单工具应 deferred")

	directCard, ok := abilityManager.Get("read_file").(*tool.ToolCard)
	s.Require().True(ok)
	s.Equal(tool.ToolExposureDirect, directCard.EffectiveExposure(), "名单内工具应 direct")

	builder := agent.SystemPromptBuilder()
	s.Require().NotNil(builder)
	rulesSection := builder.GetSection(hsections.SectionProgressiveToolRules)
	s.Require().NotNil(rulesSection, "应注入渐进式工具规则节")
	s.Contains(rulesSection.Content["cn"], "- **xlsx_read**: 读取 Excel 表格", "规则节应含初始 deferred 目录")
	s.False(builder.HasSection(hsections.SectionToolNavigation), "导航节已随旧语义删除")
}

// TestProgressive_DirectCallRejected 验证 deferred 工具未搜索直接调用被拒。
func (s *ProgressiveToolRailSuite) TestProgressive_DirectCallRejected() {
	var xlsxCalls atomic.Int32
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("xlsx_read", `{"sheet":"S1"}`),
		mockllm.CreateTextResponse("结束"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:          []tool.Tool{s.newCountingTool("xlsx_read", "读取 Excel 表格", &xlsxCalls)},
		ProgressiveToolEnabled: true,
		MaxIterations:          3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "读 Sheet1"})
	s.Require().NoError(err)
	s.Equal(int32(0), xlsxCalls.Load(), "deferred 工具不得被直接执行")
}

// TestProgressive_SearchThenCallClosesLoop 验证 tool_search 检索 + tool_call 执行闭环。
func (s *ProgressiveToolRailSuite) TestProgressive_SearchThenCallClosesLoop() {
	var xlsxCalls atomic.Int32
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("tool_search", `{"query":"excel 表格"}`),
		mockllm.CreateToolCallResponse("tool_call", `{"name":"xlsx_read","args":{"sheet":"S1"}}`),
		mockllm.CreateTextResponse("已读取 Sheet1"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:          []tool.Tool{s.newCountingTool("xlsx_read", "读取 Excel 表格", &xlsxCalls)},
		ProgressiveToolEnabled: true,
		MaxIterations:          5,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "帮我读 Sheet1"})
	s.Require().NoError(err)
	s.Equal(int32(1), xlsxCalls.Load(), "tool_call 应嵌套执行目标工具一次")
}

// TestProgressive_ToolCallWithoutSearchRejected 验证未经搜索的 tool_call 被拒。
func (s *ProgressiveToolRailSuite) TestProgressive_ToolCallWithoutSearchRejected() {
	var xlsxCalls atomic.Int32
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("tool_call", `{"name":"xlsx_read","args":{"sheet":"S1"}}`),
		mockllm.CreateTextResponse("结束"),
	)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:          []tool.Tool{s.newCountingTool("xlsx_read", "读取 Excel 表格", &xlsxCalls)},
		ProgressiveToolEnabled: true,
		MaxIterations:          3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "读 Sheet1"})
	s.Require().NoError(err)
	s.Equal(int32(0), xlsxCalls.Load(), "未搜索的 tool_call 不得执行目标工具")
}

// TestProgressive_CatalogDeltaPushed 验证运行中新增 deferred 工具触发目录 delta。
func (s *ProgressiveToolRailSuite) TestProgressive_CatalogDeltaPushed() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("完成"))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:          []tool.Tool{s.newCountingTool("xlsx_read", "读取 Excel 表格", nil)},
		ProgressiveToolEnabled: true,
		MaxIterations:          3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第一轮"})
	s.Require().NoError(err)

	// 运行中新增一个 deferred 工具（走注册期策略）
	newCard := tool.NewToolCardWithID("pdf_read", "pdf_read", "提取 PDF 文本", nil, nil)
	agent.AbilityManager().Add(newCard)

	s.MockLLM.SetResponses(mockllm.CreateTextResponse("完成"))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第二轮"})
	s.Require().NoError(err)

	manager := agent.PromptAttachments()
	s.Require().NotNil(manager)
	items, err := manager.ListByFilter(prompts.AttachmentFilter{Section: "progressive_deferred_tools"})
	s.Require().NoError(err)
	s.NotEmpty(items, "目录变化应写入附件")
	s.NotEmpty(items[0].Content, "附件内容不应为空")
}

// TestProgressive_SessionReuseNoDuplicate 验证同会话复用时目录未变不重复推送。
//
// 时序说明（对齐 Python test_deferred_catalog_does_not_repeat_after_session_state_reset）：
// 首轮目录无变化时 rail 不写附件，Sync 也不会为空的附件集合建历史快照；
// 因此第二轮 has_history_snapshot 仍为 false，按上游逻辑会补发一次全量 snapshot。
// 要验证「不重复」必须先有快照基线——这里在第一轮之后手动注册一个无关 section
// 并触发一次同步（等同 Python 用例里 add_section + sync_to_context 那两步）。
//
// 必须显式传同一个 session：Invoke 不传 session 时 deep_agent 会为每轮
// prepareSingleRoundSession 造新会话（owns_session），测的就不是「复用」了。
func (s *ProgressiveToolRailSuite) TestProgressive_SessionReuseNoDuplicate() {
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("完成"))

	sess := s.NewTestSession("progressive-reuse")
	s.Require().NoError(sess.PreRun(s.Ctx, nil))

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:          []tool.Tool{s.newCountingTool("xlsx_read", "读取 Excel 表格", nil)},
		ProgressiveToolEnabled: true,
		MaxIterations:          3,
	})
	s.Require().NoError(err)

	// 第一轮：目录无变化，不写 catalog 附件
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第一轮"}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)
	manager := agent.PromptAttachments()
	s.Require().NotNil(manager)
	first, err := manager.ListByFilter(prompts.AttachmentFilter{Section: "progressive_deferred_tools"})
	s.Require().NoError(err)
	s.Empty(first, "首轮目录无变化不应写附件")

	// 建立快照基线：注册一个无关 section，让下一次同步把历史快照写出来
	// Python: await manager.add_section(section="runtime", ...) + sync_to_context
	created, err := manager.AddSection(sess.GetSessionID(), "runtime", "runtime state",
		prompts.AttachmentRuntime, "test.runtime")
	s.Require().NoError(err)
	s.Require().NotNil(created)

	// 第二轮：此时已有快照基线，目录仍未变化 → 不重复推送
	s.MockLLM.SetResponses(mockllm.CreateTextResponse("完成"))
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "第二轮"}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	items, err := manager.ListByFilter(prompts.AttachmentFilter{Section: "progressive_deferred_tools"})
	s.Require().NoError(err)
	s.Empty(items, "目录未变化且已有快照基线时不应重复推送")
}

// TestProgressive_SchemaChangedRequiresResearch 验证 schema 变更后旧授权失效。
func (s *ProgressiveToolRailSuite) TestProgressive_SchemaChangedRequiresResearch() {
	var xlsxCalls atomic.Int32
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("tool_search", `{"query":"excel 表格"}`),
		mockllm.CreateTextResponse("检索完成"),
	)

	card := tool.NewToolCardWithID("xlsx_read", "xlsx_read", "读取 Excel 表格", nil, nil)
	instance, err := tool.NewMapFunction(card, func(_ context.Context, inputs map[string]any) (map[string]any, error) {
		xlsxCalls.Add(1)
		sheet, _ := inputs["sheet"].(string)
		return map[string]any{"content": "sheet=" + sheet}, nil
	}, nil)
	s.Require().NoError(err)

	agent, err := s.NewDeepAgentForTest(s.Ctx, hconfig.CreateDeepAgentParams{
		ToolInstances:          []tool.Tool{instance},
		ProgressiveToolEnabled: true,
		MaxIterations:          3,
	})
	s.Require().NoError(err)

	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "先搜一次"})
	s.Require().NoError(err)

	// 搜索后原地改 schema：指纹变化应使授权失效
	card.Description = "读取 Excel 工作表内容（新 schema）"

	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("tool_call", `{"name":"xlsx_read","args":{"sheet":"S1"}}`),
		mockllm.CreateTextResponse("结束"),
	)
	_, err = agent.Invoke(s.Ctx, map[string]any{"query": "再直接调"})
	s.Require().NoError(err)
	s.Equal(int32(0), xlsxCalls.Load(), "schema 变更后旧授权必须失效，不得执行目标工具")
}

// newCountingTool 创建可统计调用次数的测试工具。
func (s *ProgressiveToolRailSuite) newCountingTool(name, description string, counter *atomic.Int32) tool.Tool {
	card := tool.NewToolCardWithID(name, name, description, nil, nil)
	instance, err := tool.NewMapFunction(card, func(_ context.Context, inputs map[string]any) (map[string]any, error) {
		if counter != nil {
			counter.Add(1)
		}
		sheet, _ := inputs["sheet"].(string)
		return map[string]any{"content": "sheet=" + sheet}, nil
	}, nil)
	s.Require().NoError(err)
	return instance
}
