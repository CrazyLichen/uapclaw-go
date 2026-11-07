//go:build integration

package attachment

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	model_clients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CommitCallbackSuite 覆盖 spec C1/C2：附件提交回调在真实 invoke 链路中的时机。
//
// 链路：DeepAgent.Invoke → 内层 ReActAgent admission → fire(on_user_message)
// → rail 登记 section + 挂提交回调 → syncPromptAttachments → runCommitCallbacks。
//
// Python: ReActAgent._sync_prompt_attachments（react_agent.py L1148-1182）。
type CommitCallbackSuite struct {
	isuite.AgentSuite
}

// commitRail 在 OnUserMessage 里登记 section 并把提交回调挂进 ctx.extra。
//
// 对齐 team_policy_rail._queue_team_context_attachment 的做法。
type commitRail struct {
	agentinterfaces.BaseRail
	// manager 与 DeepAgent 共享的附件管理器
	manager *prompts.Manager
	// commits 回调执行次数
	commits int
	// panics 首条回调是否 panic
	panics bool
	// content section 内容
	content string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCommitCallbackSuite 运行 CommitCallbackSuite。
func TestCommitCallbackSuite(t *testing.T) { suite.Run(t, new(CommitCallbackSuite)) }

// GetCallbacks 声明 OnUserMessage。
func (r *commitRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	return r.BuildCallbacks(
		r.CallbackFrom(agentinterfaces.CallbackOnUserMessage, r.onUserMessage),
	)
}

// TestIT_提交回调_真Sync后才推进基线 验证回调在附件真正写进历史之后才执行。
//
// spec C1：一轮 invoke → 回调恰好执行一次，且模型收到的消息里确有附件内容。
func (s *CommitCallbackSuite) TestIT_提交回调_真Sync后才推进基线() {
	ctx := context.Background()
	rail := &commitRail{content: "# 团队状态 v1"}
	agent, sess := s.createAttachmentAgent(ctx, rail, "it-commit-1")

	_, err := agent.Invoke(ctx, map[string]any{"query": "hi"}, agentinterfaces.WithSession(sess))
	s.Require().NoError(err)

	s.Equal(1, rail.commits, "Sync 成功后 commit 回调必须执行一次")
	s.Contains(s.modelSaw(0), "团队状态",
		"附件内容必须真的进了历史（模型看得见），回调才算数")
}

// TestIT_提交回调_单条炸不拖垮整轮 验证回调 panic 不影响本轮完成。
//
// spec C2。
func (s *CommitCallbackSuite) TestIT_提交回调_单条炸不拖垮整轮() {
	ctx := context.Background()
	rail := &commitRail{content: "# 团队状态", panics: true}
	agent, sess := s.createAttachmentAgent(ctx, rail, "it-commit-3")

	s.NotPanics(func() {
		_, err := agent.Invoke(ctx, map[string]any{"query": "hi"}, agentinterfaces.WithSession(sess))
		s.Require().NoError(err)
	})
	s.Equal(1, rail.commits, "panic 的回调也计入一次")
	s.Contains(s.modelSaw(0), "团队状态", "附件仍须正常投递")
}

// TestIT_提交回调_失败不推进基线 验证历史不可写时 Sync 报错、无历史可推进。
//
// spec C1 后半：失败即无历史；回调不执行由单元用例覆盖
// （syncPromptAttachments 返回 false 时调用方不进 runCommitCallbacks）。
func (s *CommitCallbackSuite) TestIT_提交回调_历史不可写则不推进基线() {
	ctx := context.Background()
	m := prompts.NewManager()
	_, err := m.AddSection("it-commit-2", "team.context", "# 团队状态 v1",
		prompts.AttachmentRuntime, "test.rail")
	s.Require().NoError(err)
	s.Error(m.Sync(ctx, testhelpers.NewFailingHistory("it-commit-2", errors.New("db down"))),
		"历史不可写时 Sync 必须报错")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// onUserMessage 登记 section 并挂提交回调。
func (r *commitRail) onUserMessage(_ context.Context, railCtx any) error {
	cbc, ok := railCtx.(*agentinterfaces.AgentCallbackContext)
	if !ok || cbc.Session() == nil || r.manager == nil {
		return nil
	}
	sessionID := cbc.Session().GetSessionID()
	if _, err := r.manager.AddSection(sessionID, "team.context", r.content,
		prompts.AttachmentRuntime, "test.rail"); err != nil {
		return err
	}

	fn := func() error { r.commits++; return nil }
	if r.panics && r.commits == 0 {
		fn = func() error { r.commits++; panic("commit panic") }
	}
	// Python: ctx.extra.setdefault(KEY, []).append(callback)
	existing, _ := cbc.GetExtra(prompts.CommitCallbacksExtraKey)
	list, _ := existing.([]func() error)
	cbc.SetExtra(prompts.CommitCallbacksExtraKey, append(list, fn))
	return nil
}

// createAttachmentAgent 构造 DeepAgent 并注册共用同一附件管理器的 rail。
func (s *CommitCallbackSuite) createAttachmentAgent(
	ctx context.Context,
	rail *commitRail,
	sessionName string,
) (*harness.DeepAgent, *session.Session) {
	dummyTool := testhelpers.NewBlockingTool("dummy_tool")
	dummyTool.Release()

	agent, err := s.NewDeepAgentForTest(ctx, hconfig.CreateDeepAgentParams{
		MaxIterations:     3,
		CompletionTimeout: 30.0,
		ToolInstances:     []tool.Tool{dummyTool.Tool()},
	})
	s.Require().NoError(err, "创建 DeepAgent 失败")

	// 关键：rail 必须用 DeepAgent 内部那个 manager，否则登记落不到 admission 的车道
	rail.manager = agent.PromptAttachments()
	s.Require().NotNil(rail.manager, "DeepAgent 必须自带附件管理器")

	// 必须先初始化：on_user_message 是桥接事件，registerRailSelective 在
	// 内层 ReActAgent 尚未创建时会静默跳过注册（deep_agent.go:2849-2851），
	// rail 的钩子就永远收不到。
	ctx, err = agent.EnsureInitialized(ctx)
	s.Require().NoError(err, "初始化 DeepAgent 失败")
	s.Require().NoError(agent.RegisterRail(ctx, rail))

	sess := s.NewTestSession(sessionName)
	return agent, sess
}

// modelSaw 把第 i 次模型调用收到的消息拼成文本。
//
// MockLLM 记录的是真实发给模型的 messages，是"附件是否进了历史"的可靠观测点。
func (s *CommitCallbackSuite) modelSaw(i int) string {
	call := s.MockLLM.GetInvokeCall(i)
	if call == nil {
		return ""
	}
	var sb strings.Builder
	appendMsgs(&sb, call.Messages)
	return sb.String()
}

// appendMsgs 把消息列表的文本拼进 builder。
func appendMsgs(sb *strings.Builder, msgs model_clients.MessagesParam) {
	for _, m := range msgs.Messages() {
		if m == nil {
			continue
		}
		sb.WriteString(m.GetContent().Text())
		sb.WriteString("\n")
	}
}
