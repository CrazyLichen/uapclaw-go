//go:build integration

// 9.11b 检索工具 Prompt Rails 的端到端集成测试。
//
// 覆盖场景（计划 Task 9）：
//  1. tool_search 闭环：真 AbilityManager 注册 deferred 工具 → rail 冻结目录
//     → BeforeModelCall 注入披露节 → toolkit get_schema / list 全链
//  2. tool_call 信封注册：rail.Init 经真 AbilityManager 注册 DIRECT 信封，
//     uninit 精确摘除（单一所有者契约的装配面）
//  3. R-5 开关翻转判据配置面：ToolRetrievalEnabled 双开关与 Python config.py
//     :469-479 逐分支一致
//
// 真实跨子系统（规则 7）：harness（DeepAgent/AbilityManager/rails 注册）+
// swarm/agents/harness/common/rails（目录披露 rail）+
// swarm/agents/harness/common/tools（toolkits/检索投影）+
// openuapclaw/harness/tools/tool_discovery（BM25）+ common/config（开关）。
package toolretrieval_test

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	swarmrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolRetrievalSuite 9.11b 端到端套件。
type ToolRetrievalSuite struct {
	isuite.AgentSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestToolRetrievalSuite 运行套件。
func TestToolRetrievalSuite(t *testing.T) {
	suite.Run(t, new(ToolRetrievalSuite))
}

// ──────────────────────────── 基建 ────────────────────────────

// integrationEchoTool 回显工具（真 tool.Tool 实现）。
type integrationEchoTool struct{ card *tool.ToolCard }

func (e *integrationEchoTool) Card() *tool.ToolCard { return e.card }
func (e *integrationEchoTool) Invoke(_ context.Context, inputs map[string]any, _ ...tool.ToolOption) (map[string]any, error) {
	return map[string]any{"success": true, "echo": inputs}, nil
}
func (e *integrationEchoTool) Stream(context.Context, map[string]any, ...tool.ToolOption) (<-chan tool.StreamChunk, error) {
	return nil, tool.ErrStreamNotSupported
}
func (e *integrationEchoTool) GetCallTimeout(_ map[string]any, defaultTimeout float64) float64 {
	return defaultTimeout
}

// ──────────────────────────── 场景 1：tool_search 全链 ────────────

// TestToolSearch全链 真实 DeepAgent + 真 AbilityManager + 真目录披露 rail：
// deferred 注册 → 冻结目录 → BeforeModelCall 注入 → get_schema/list。
func (s *ToolRetrievalSuite) TestToolSearch全链() {
	ctx := context.Background()
	sess := session.NewSession()
	owner := harness.NewDeepAgent(&agentschema.AgentCard{})
	am := owner.AbilityManager()
	s.Require().NotNil(am)

	// 真实 deferred 工具（cron 类两件 → 静态表 cron 分类 ≥2 保留）+ direct
	deferredCard := tool.NewToolCardWithID("cron_list_jobs", "cron_list_jobs", "列出定时任务", nil, nil)
	deferredCard.Exposure = tool.ToolExposureDeferred
	deferredCard.SetExposureDeclared(true)
	s.Require().True(am.AddAbility(deferredCard, &integrationEchoTool{card: deferredCard}).Added)
	deferredCard2 := tool.NewToolCardWithID("cron_create_job", "cron_create_job", "创建定时任务", nil, nil)
	deferredCard2.Exposure = tool.ToolExposureDeferred
	deferredCard2.SetExposureDeclared(true)
	s.Require().True(am.AddAbility(deferredCard2, &integrationEchoTool{card: deferredCard2}).Added)
	// 单成员分类（documents/read_pdf）→ 折叠进未归类 [tool] 块
	deferredCard3 := tool.NewToolCardWithID("read_pdf", "read_pdf", "读取 PDF 文本", nil, nil)
	deferredCard3.Exposure = tool.ToolExposureDeferred
	deferredCard3.SetExposureDeclared(true)
	s.Require().True(am.AddAbility(deferredCard3, &integrationEchoTool{card: deferredCard3}).Added)
	directCard := tool.NewToolCardWithID("bash", "bash", "执行命令", nil, nil)
	directCard.Exposure = tool.ToolExposureDirect
	directCard.SetExposureDeclared(true)
	s.Require().True(am.AddAbility(directCard, &integrationEchoTool{card: directCard}).Added)

	provider := func() []schema.ToolInfoInterface {
		infos := []schema.ToolInfoInterface{}
		for _, abilityCard := range am.List() {
			if card, ok := abilityCard.(*tool.ToolCard); ok && card.EffectiveExposure() == tool.ToolExposureDeferred {
				infos = append(infos, card.ToolInfo())
			}
		}
		return infos
	}
	toolkit := swarmtools.NewToolRetrievalToolkit(provider, sess.GetSessionID(), nil, nil, "cn")
	rail := swarmrails.NewToolRetrievalPromptRail(toolkit, map[string]any{
		"progressive_tool_enabled": true, "progressive_tool_optimize": true,
	})
	s.Require().True(rail.IsTrusted())
	// 裸 NewDeepAgent 无 systemPromptBuilder（生产路径 CreateDeepAgent 装配）；
	// 以包装 agent 注入真 builder（嵌入复用 DeepAgent 其余能力面）。
	wrapped := &agentWithBuilder{DeepAgent: owner, sb: saprompt.NewSystemPromptBuilder()}
	s.Require().NoError(rail.Init(ctx, wrapped))
	s.Require().NoError(owner.RegisterRail(ctx, rail))
	_ = rail.Init(ctx, wrapped) // 重建 builder 引用（RegisterRail 以 owner 再 Init 一次）

	// rail 注册到真实 DeepAgent：FindRailsByType 消费成立
	s.Require().Len(owner.FindRailsByType(
		reflect.TypeOf((*swarmrails.ToolRetrievalPromptRail)(nil))), 1)

	// BeforeModelCall 真实回调上下文：注入披露节到真 builder
	inputs := &agentinterfaces.ModelCallInputs{
		Tools: []schema.ToolInfoInterface{
			&schema.ToolInfo{Name: "bash"},
			&schema.ToolInfo{Name: "cron_list_jobs"},
		},
	}
	cbc := agentinterfaces.NewAgentCallbackContext(wrapped, inputs, sess)
	s.Require().NoError(rail.BeforeModelCall(ctx, cbc))

	// 披露节渲染断言（真 builder.Build）
	s.Require().NotNil(wrapped.sb)
	rendered := wrapped.sb.(*saprompt.SystemPromptBuilder).Build()
	for _, want := range []string{"## Deferred 工具（渐进式披露）", "[category] `cron`", "[tool] `read_pdf`", "直接 get_schema 工具"} {
		s.Require().Contains(rendered, want, "披露节缺 %q", want)
	}
	// tools 过滤：deferred 隐藏、direct 保留
	names := []string{}
	for _, info := range inputs.Tools {
		names = append(names, info.GetName())
	}
	s.Require().Contains(strings.Join(names, ","), "bash")
	s.Require().NotContains(strings.Join(names, ","), "cron_list_jobs")

	// tool_search get_schema / list 全链（toolkits + BM25 + 检索投影）
	out, err := toolkit.ToolSearch(map[string]any{"operation": "get_schema", "tool": "cron_list_jobs"})
	s.Require().NoError(err)
	s.Require().Contains(out, "cron_list_jobs")
	out, err = toolkit.ToolSearch(map[string]any{"operation": "list", "category": "cron"})
	s.Require().NoError(err)
	s.Require().Contains(out, "## Tools (category: cron)")
	// 退役字段降级
	_, err = toolkit.ToolSearch(map[string]any{"operation": "get_schema", "tools": []any{"cron_create_job"}})
	s.Require().NoError(err)
	// 裸 list 教学错误
	_, err = toolkit.ToolSearch(map[string]any{"operation": "list"})
	s.Require().Error(err)
	s.Require().Contains(err.Error(), "list needs")
}

// ──────────────────────────── 场景 2：tool_call 信封装配面 ────────────

// TestToolCall信封装配 rail.Init 经真 AbilityManager 注册 DIRECT 信封；
// Uninit 精确摘除（他人重注册的名字不动）。
func (s *ToolRetrievalSuite) TestToolCall信封装配() {
	ctx := context.Background()
	sess := session.NewSession()
	owner := harness.NewDeepAgent(&agentschema.AgentCard{})
	provider := func() []schema.ToolInfoInterface { return nil }
	toolkit := swarmtools.NewToolRetrievalToolkit(provider, sess.GetSessionID(), nil, nil, "cn")
	rail := swarmrails.NewToolRetrievalPromptRail(toolkit, map[string]any{
		"progressive_tool_enabled": true, "progressive_tool_optimize": true,
	})
	s.Require().NoError(owner.RegisterRail(ctx, rail))

	// 信封卡已注册且 DIRECT 曝光
	envelopeAbility := owner.AbilityManager().Get("tool_call")
	s.Require().NotNil(envelopeAbility)
	envelopeCard, ok := envelopeAbility.(*tool.ToolCard)
	s.Require().True(ok)
	s.Require().Equal(tool.ToolExposureDirect, envelopeCard.EffectiveExposure())

	// Uninit 精确摘除
	s.Require().NoError(rail.Uninit(owner))
	s.Require().Nil(owner.AbilityManager().Get("tool_call"))
}

// ──────────────────────────── 场景 3：R-5 开关配置面 ────────────

// TestR5开关配置面 ToolRetrievalEnabled 双开关逐分支与 Python 一致。
func (s *ToolRetrievalSuite) TestR5开关配置面() {
	s.Require().False(config.ToolRetrievalEnabled(nil))
	s.Require().False(config.ToolRetrievalEnabled(map[string]any{}))
	s.Require().False(config.ToolRetrievalEnabled(map[string]any{"progressive_tool_enabled": true}))
	s.Require().False(config.ToolRetrievalEnabled(map[string]any{"progressive_tool_optimize": true}))
	s.Require().True(config.ToolRetrievalEnabled(map[string]any{
		"progressive_tool_enabled": true, "progressive_tool_optimize": true,
	}))
	s.Require().True(config.ToolRetrievalEnabled(map[string]any{
		"progressive_tool_enabled": "on", "progressive_tool_optimize": "1",
	}))
}

// agentWithBuilder 注入真实 builder 的 DeepAgent 包装（嵌入复用其余能力面）。
type agentWithBuilder struct {
	*harness.DeepAgent
	sb saprompt.SystemPromptBuilderInterface
}

func (a *agentWithBuilder) SystemPromptBuilder() saprompt.SystemPromptBuilderInterface { return a.sb }
