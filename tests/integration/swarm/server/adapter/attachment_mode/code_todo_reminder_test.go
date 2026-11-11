//go:build integration

package attachment_mode

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/suite"

	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/adapter"
	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeTodoReminderSuite 覆盖 code 模式 todo 提醒节奏（IT-S2-11）。
type CodeTodoReminderSuite struct {
	isuite.BaseIntegrationSuite
	// env code 族测试环境（直连 CodeAdapter：本用例验证 code rail 的提醒节奏，
	// 与 plan/runtime 层无关；走 AgentServer 需要先解决 modes.code.tools 配置口径）
	env *codeEnv
}

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskReminderSection 提醒附件片段名
	taskReminderSection = "task_reminder"
	// codeSession code 模式会话 ID
	codeSession = "itest-code-session"
	// reminderTurns 触发提醒所需轮次（上游 10/10 节奏）
	reminderTurns = 10
	// codeEntrySession 入口级用例的会话 ID
	codeEntrySession = "itest-code-entry-session"
	// codeEntryMode 入口级用例的 mode 入参。
	//
	// 必须用裸 "agent"：code 族由 work_mode=code 组合出来
	// （runtime/request.go resolveRequestMode → ComposeWebMode）。
	// 直接传三段 "agent.work.normal" 时 work_mode 已无位置可组合，
	// 请求就会落到 work 族的 agent 上。
	codeEntryMode = "agent"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCodeTodoReminderSuite 运行 CodeTodoReminderSuite。
func TestCodeTodoReminderSuite(t *testing.T) { suite.Run(t, new(CodeTodoReminderSuite)) }

// TestIT_S2_11_code提醒节奏 验证连续 10 轮未管理 todo 后出现提醒附件，
// 内容不变不重复投递；todo 工具调用后计数重置，再满 10 轮出现第二次提醒。
//
// 跨子系统：CodeAdapter + code rails + context_engine。
func (s *CodeTodoReminderSuite) TestIT_S2_11_code提醒节奏() {
	// 第 1~9 轮：未达阈值，不应有提醒
	for i := 0; i < reminderTurns-1; i++ {
		s.env.Send(normalMode, fmt.Sprintf("第%d轮", i+1))
	}
	s.Nil(s.env.AttachmentOf(taskReminderSection), "未达 10 轮不应有提醒附件")

	// 第 10 轮：触发第一次提醒
	s.env.Send(normalMode, "第10轮")
	first := s.env.AttachmentOf(taskReminderSection)
	s.Require().NotNil(first, "满 10 轮必须出现 task_reminder 附件")
	s.Equal("todo_reminder", string(first.Kind))
	s.Equal("jiuwenswarm.code_task_planning.task_reminder", first.Source)
	s.Contains(first.Content, "<!-- task-reminder-delivery:1 -->")

	// 再两轮：内容未变，不产生新的投递序号
	s.env.Send(normalMode, "第11轮")
	s.env.Send(normalMode, "第12轮")
	stable := s.env.AttachmentOf(taskReminderSection)
	s.Require().NotNil(stable)
	s.Equal(first.Content, stable.Content, "内容未变不得重复追加")
	s.Equal(1, s.env.CountAttachmentsContaining("<!-- task-reminder-delivery:1 -->"),
		"模型窗口里同一提醒只应出现一次")

	// 模拟一次 todo 工具调用 → 计数重置
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create",
			`{"todos":[{"id":"phase-1","content":"实现后端","activeForm":"实现后端","description":"接口与存储"}]}`),
		mockllm.CreateTextResponse("已创建任务列表"),
	)
	s.env.Send(normalMode, "创建任务")

	// 重置后再满 10 轮 → 第二次提醒
	for i := 0; i < reminderTurns; i++ {
		s.env.Send(normalMode, fmt.Sprintf("重置后第%d轮", i+1))
	}
	second := s.env.AttachmentOf(taskReminderSection)
	s.Require().NotNil(second)
	s.Contains(second.Content, "<!-- task-reminder-delivery:2 -->", "重置后应再次提醒且序号递增")
}

// ──────────────────────────── 入口级（真 AgentManager） ────────────────────────────

// TestIT_S2_11b_code提醒节奏_入口级 与 S2_11 同断言序列，但从 chat.send
// 入口进入：真 AgentServer → AgentManager（按 code 模式选 agent）→
// adapter 根路由 → 会话 child → code rails。
//
// 存在理由：adapter 已是"根 router + 会话 child"双形态，入口级链路才能
// 证明"从请求进来，code 专属 rail 真的装上了并按时投递提醒"。直连
// CodeAdapter 的 S2_11 只覆盖装配后的行为，覆盖不到 AgentManager 选型
// 与根分流这两段。
func (s *CodeTodoReminderSuite) TestIT_S2_11b_code提醒节奏_入口级() {
	env := newEntryCodeEnv(s.T(), s.MockLLM, "mock_s2_code_entry")

	// 第 1~9 轮：未达阈值，不应有提醒
	for i := 0; i < reminderTurns-1; i++ {
		env.Send(fmt.Sprintf("第%d轮", i+1))
	}
	s.Nil(env.AttachmentOf(taskReminderSection), "未达 10 轮不应有提醒附件")

	// 第 10 轮：触发第一次提醒
	env.Send("第10轮")
	first := env.AttachmentOf(taskReminderSection)
	s.Require().NotNil(first, "满 10 轮必须出现 task_reminder 附件（入口级）")
	s.Equal("todo_reminder", string(first.Kind))
	s.Equal("jiuwenswarm.code_task_planning.task_reminder", first.Source)
	s.Contains(first.Content, "<!-- task-reminder-delivery:1 -->")

	// 再两轮：内容未变，不产生新的投递序号
	env.Send("第11轮")
	env.Send("第12轮")
	stable := env.AttachmentOf(taskReminderSection)
	s.Require().NotNil(stable)
	s.Equal(first.Content, stable.Content, "内容未变不得重复追加")
	s.Equal(1, env.CountAttachmentsContaining("<!-- task-reminder-delivery:1 -->"),
		"模型窗口里同一提醒只应出现一次")

	// 模拟一次 todo 工具调用 → 计数重置
	s.MockLLM.SetResponses(
		mockllm.CreateToolCallResponse("todo_create",
			`{"todos":[{"id":"phase-1","content":"实现后端","activeForm":"实现后端","description":"接口与存储"}]}`),
		mockllm.CreateTextResponse("已创建任务列表"),
	)
	env.Send("创建任务")

	// 重置后再满 10 轮 → 第二次提醒
	for i := 0; i < reminderTurns; i++ {
		env.Send(fmt.Sprintf("重置后第%d轮", i+1))
	}
	second := env.AttachmentOf(taskReminderSection)
	s.Require().NotNil(second)
	s.Contains(second.Content, "<!-- task-reminder-delivery:2 -->", "重置后应再次提醒且序号递增")
}

// entryCodeEnv 入口级 code 环境：真 AgentServer + AgentManager，
// 工厂按 manager 解析出的 code 模式创建 agent。
//
// 与 codeEnv（直连 CodeAdapter）的区别：请求从 chat.send 入口进入，
// 经 AgentManager 选 agent、经 adapter 根路由到会话 child，覆盖完整链路。
type entryCodeEnv struct {
	// plan 复用 PlanEnv 的 Server/Mock/附件读取能力
	plan *testhelpers.PlanEnv
	// mock 模型桩
	mock *mockllm.MockModelClient
	// t 测试句柄
	t *testing.T
}

// newEntryCodeEnv 构建入口级 code 环境。
//
// work_mode=code 让 AgentManager 按 code 族解析 mode，
// 从而 create_instance 拿到的就是 code 模式（与生产一致）。
func newEntryCodeEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *entryCodeEnv {
	t.Helper()
	// NewCodePlanEnv 已带 WorkMode="code"：每轮请求都会带 work_mode=code，
	// 让 AgentManager 按 code 族选 agent（与生产同一路径）。
	// 不要再另建 AdapterTestEnv 覆盖 plan.Env —— 两套数据目录会让 agent
	// 与 mock 各读一份 config。
	plan := testhelpers.NewCodePlanEnv(t, mock, prefix)
	return &entryCodeEnv{plan: plan, mock: mock, t: t}
}

// Send 从 chat.send 入口发一轮（流式）。
func (e *entryCodeEnv) Send(query string) {
	e.t.Helper()
	e.plan.SendStream(codeEntrySession, codeEntryMode, query, nil)
}

// AttachmentOf 返回指定 section 的附件。
//
// 附件挂在"正在跑该会话的 DeepAgent"上（会话分身后在 child），
// PlanEnv.AttachmentOf 已按 sessionID 走 GetLiveSessionInstance。
func (e *entryCodeEnv) AttachmentOf(section string) *hprompts.Attachment {
	e.t.Helper()
	return e.plan.AttachmentOf(codeEntrySession, section)
}

// CountAttachmentsContaining 统计最近一次模型窗口里包含指定文本的消息数。
func (e *entryCodeEnv) CountAttachmentsContaining(substr string) int {
	e.t.Helper()
	var messages []llmschema.BaseMessage
	if rec := e.mock.LastStreamCall(); rec != nil {
		messages = rec.Messages.Messages()
	} else if rec := e.mock.LastInvokeCall(); rec != nil {
		messages = rec.Messages.Messages()
	}
	count := 0
	for _, msg := range messages {
		meta := msg.GetMetadata()
		if meta == nil {
			continue
		}
		// 只统计"历史附件"消息（与 codeEnv 同口径），避免把普通
		// 对话/系统提示词里出现的同串也算进去。
		if marked, _ := meta["_openjiuwen_prompt_attachment_history"].(bool); !marked {
			continue
		}
		if strings.Contains(msg.GetContent().Text(), substr) {
			count++
		}
	}
	return count
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每例隔离 code 环境。
func (s *CodeTodoReminderSuite) SetupTest() {
	s.env = newCodeEnv(s.T(), s.MockLLM, "mock_s2_code")
}

// ──────────────────────────── code 环境 ────────────────────────────

// codePlanAdapter code 族测试所需的最小适配器能力（CodeAdapter 满足）。
type codePlanAdapter interface {
	// ProcessMessageStreamImpl 走完整流式请求链路
	ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error)
	// Instance 返回装配后的 DeepAgent
	Instance() *harness.DeepAgent
}

// codeEnv code 族直连环境：CodeAdapter + MockLLM + 隔离数据目录。
type codeEnv struct {
	// t 测试句柄
	t *testing.T
	// adapter 受测 CodeAdapter
	adapter codePlanAdapter
	// mock 模型桩
	mock *mockllm.MockModelClient
}

// newCodeEnv 构建 code 族直连环境。
func newCodeEnv(t *testing.T, mock *mockllm.MockModelClient, prefix string) *codeEnv {
	t.Helper()
	env := testhelpers.NewAdapterTestEnv(t, mock, prefix)
	env.WriteConfigYAML(t, "")
	d := adapter.NewCodeAdapter()
	// 10.3.28f：本用例直连 adapter 跑多轮，验证 code 模式的 todo 提醒节奏
	// （10/10 rail 行为）。根 adapter 现在是纯 router，chat 入口会分流到
	// 会话 child；而 child 的 CreateInstance 用的是根的 mode（"agent"），
	// 与这里要测的 code 运行态不符。
	// 标记为 session-scoped 与 child 同构（生产里 code 模式由 AgentManager
	// 按 mode 单独建 agent，其 CreateInstance 的 mode 就是 code 族），
	// 让本案在"已装配好的 code adapter"上直接验证行为。
	d.MarkAsSessionScoped(codeSession)
	if err := d.CreateInstance(context.Background(), map[string]any{
		"project_dir": filepath.Join(env.DataDir, "myproj"),
	}, "agent", "", nil); err != nil {
		t.Fatalf("创建 CodeAdapter 失败: %v", err)
	}
	t.Cleanup(func() { _ = d.Cleanup() })
	return &codeEnv{t: t, adapter: d, mock: mock}
}

// Send 跑一轮流式对话（直连 adapter，收集事件即可）。
func (e *codeEnv) Send(mode, query string) {
	e.t.Helper()
	params, err := json.Marshal(map[string]any{"mode": mode, "query": query})
	if err != nil {
		e.t.Fatalf("序列化 params 失败: %v", err)
	}
	sid := codeSession
	req := &schema.AgentRequest{
		RequestID: "req-code", ChannelID: "web", SessionID: &sid,
		Params: params, IsStream: true,
	}
	ch, streamErr := e.adapter.ProcessMessageStreamImpl(context.Background(), req, map[string]any{
		"query": query, "mode": mode, "conversation_id": codeSession,
	})
	if streamErr != nil {
		e.t.Fatalf("流式启动失败: %v", streamErr)
	}
	for range ch {
	}
}

// AttachmentOf 返回指定 section 的附件（不存在返回 nil）。
func (e *codeEnv) AttachmentOf(section string) *hprompts.Attachment {
	e.t.Helper()
	inst := e.adapter.Instance()
	if inst == nil || inst.PromptAttachments() == nil {
		return nil
	}
	items, err := inst.PromptAttachments().ListByFilter(hprompts.AttachmentFilter{
		SessionID: codeSession, Section: section,
	})
	if err != nil {
		e.t.Fatalf("读取附件失败: %v", err)
	}
	if len(items) == 0 {
		return nil
	}
	return items[0]
}

// CountAttachmentsContaining 统计最近一次模型窗口里包含指定文本的附件消息数。
func (e *codeEnv) CountAttachmentsContaining(substr string) int {
	e.t.Helper()
	var messages []llmschema.BaseMessage
	if rec := e.mock.LastStreamCall(); rec != nil {
		messages = rec.Messages.Messages()
	} else if rec := e.mock.LastInvokeCall(); rec != nil {
		messages = rec.Messages.Messages()
	}
	count := 0
	for _, msg := range messages {
		meta := msg.GetMetadata()
		if meta == nil {
			continue
		}
		if marked, _ := meta["_openjiuwen_prompt_attachment_history"].(bool); !marked {
			continue
		}
		if strings.Contains(msg.GetContent().Text(), substr) {
			count++
		}
	}
	return count
}
