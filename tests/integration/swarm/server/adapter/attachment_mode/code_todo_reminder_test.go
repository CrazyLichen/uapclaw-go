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
	if err := d.CreateInstance(context.Background(), map[string]any{
		"project_dir": filepath.Join(env.DataDir, "myproj"),
	}, "agent", ""); err != nil {
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
