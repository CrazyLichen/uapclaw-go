//go:build integration

package attachment_mode

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/tests/integration/mockllm"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
	"github.com/uapclaw/uapclaw-go/tests/integration/testhelpers"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodeTodoReminderSuite 覆盖 code 模式 todo 提醒节奏（IT-S2-11）。
type CodeTodoReminderSuite struct {
	isuite.BaseIntegrationSuite
	// env code 族测试环境
	env *testhelpers.PlanEnv
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
		s.env.Send(codeSession, normalMode, fmt.Sprintf("第%d轮", i+1), nil)
	}
	s.Nil(s.env.AttachmentOf(codeSession, taskReminderSection), "未达 10 轮不应有提醒附件")

	// 第 10 轮：触发第一次提醒
	s.env.Send(codeSession, normalMode, "第10轮", nil)
	first := s.env.AttachmentOf(codeSession, taskReminderSection)
	s.Require().NotNil(first, "满 10 轮必须出现 task_reminder 附件")
	s.Equal("todo_reminder", string(first.Kind))
	s.Equal("jiuwenswarm.code_task_planning.task_reminder", first.Source)
	s.Contains(first.Content, "<!-- task-reminder-delivery:1 -->")

	// 再两轮：内容未变，不产生新的投递序号
	s.env.Send(codeSession, normalMode, "第11轮", nil)
	s.env.Send(codeSession, normalMode, "第12轮", nil)
	stable := s.env.AttachmentOf(codeSession, taskReminderSection)
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
	s.env.Send(codeSession, normalMode, "创建任务", nil)

	// 重置后再满 10 轮 → 第二次提醒
	for i := 0; i < reminderTurns; i++ {
		s.env.Send(codeSession, normalMode, fmt.Sprintf("重置后第%d轮", i+1), nil)
	}
	second := s.env.AttachmentOf(codeSession, taskReminderSection)
	s.Require().NotNil(second)
	s.Contains(second.Content, "<!-- task-reminder-delivery:2 -->", "重置后应再次提醒且序号递增")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// SetupTest 每例隔离 code 环境。
func (s *CodeTodoReminderSuite) SetupTest() {
	s.env = testhelpers.NewCodePlanEnv(s.T(), s.MockLLM, "mock_s2_code")
}
