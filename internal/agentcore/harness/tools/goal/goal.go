package goal

// 本文件对应 Python openjiuwen/harness/tools/goal.py，
// 提供 GoalReportSink（报告暂存）与 submit_goal_report / get_current_goal 两个工具。

import (
	"context"
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/goal"
	hprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// GoalReportSink 会话内当前 goal 尝试的报告暂存（last-write-wins）。
// 只写 sink 不写 GoalRecord，由 TaskCompletionRail 轮末 consume。
// Python: GoalReportSink (tools/goal.py L25)
type GoalReportSink struct {
	// report 当前尝试报告（可空）
	report *goal.GoalAssessment
	// sessionID / goalID / revision / attemptIndex 记录本轮尝试身份
	sessionID    string
	goalID       string
	revision     int
	attemptIndex int
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGoalReportSink 构造空 sink。
// Python: GoalReportSink.__init__ (tools/goal.py L32)
func NewGoalReportSink() *GoalReportSink {
	return &GoalReportSink{}
}

// BeginAttempt 重置 sink 以承接新尝试。
// Python: GoalReportSink.begin_attempt (tools/goal.py L55)
func (s *GoalReportSink) BeginAttempt(sessionID, goalID string, revision, attemptIndex int) {
	s.report = nil
	s.sessionID = sessionID
	s.goalID = goalID
	s.revision = revision
	s.attemptIndex = attemptIndex
}

// Submit 存报告（单次尝试内 last-write-wins）。
// Python: GoalReportSink.submit (tools/goal.py L69)
func (s *GoalReportSink) Submit(assessment goal.GoalAssessment) {
	s.report = &assessment
}

// Consume 取走并清空报告。
// Python: GoalReportSink.consume (tools/goal.py L79)
func (s *GoalReportSink) Consume() *goal.GoalAssessment {
	r := s.report
	s.report = nil
	return r
}

// GoalID 返回当前尝试的目标标识。
func (s *GoalReportSink) GoalID() string { return s.goalID }

// Revision 返回当前尝试的代际号。
func (s *GoalReportSink) Revision() int { return s.revision }

// NewSubmitGoalReportTool 创建 submit_goal_report 工具。
// Python: SubmitGoalReportTool.__init__ + invoke (tools/goal.py L86-136)
func NewSubmitGoalReportTool(sink *GoalReportSink, language, agentID string) (tool.Tool, error) {
	card, err := hprompts.BuildToolCard("submit_goal_report", "submit_goal_report", language, nil, agentID)
	if err != nil {
		return nil, fmt.Errorf("构建 submit_goal_report ToolCard 失败: %w", err)
	}
	return tool.NewMapFunction(card, func(_ context.Context, inputs map[string]any) (map[string]any, error) {
		statusStr, _ := inputs["status"].(string)
		if statusStr == "" {
			statusStr = "continue"
		}
		status := goal.GoalAssessmentStatus(statusStr)
		switch status {
		case goal.GoalAssessmentStatusContinue,
			goal.GoalAssessmentStatusComplete,
			goal.GoalAssessmentStatusBlocked:
		default:
			// 非法 status 归一化 continue（Python L113-119）
			status = goal.GoalAssessmentStatusContinue
		}
		sink.Submit(goal.GoalAssessment{
			Status:          status,
			Evidence:        asString(inputs["evidence"]),
			RemainingWork:   optionalString(inputs["remaining_work"]),
			NextInstruction: optionalString(inputs["next_instruction"]),
		})
		return map[string]any{"result": "report_accepted", "status": string(status)}, nil
	}, nil)
}

// NewGetCurrentGoalTool 创建 get_current_goal 工具（只读）。
// Python: GetCurrentGoalTool.__init__ + invoke (tools/goal.py L143-195)
func NewGetCurrentGoalTool(goalManager *goal.GoalManager, language, agentID string) (tool.Tool, error) {
	card, err := hprompts.BuildToolCard("get_current_goal", "get_current_goal", language, nil, agentID)
	if err != nil {
		return nil, fmt.Errorf("构建 get_current_goal ToolCard 失败: %w", err)
	}
	return tool.NewMapFunction(card, func(_ context.Context, _ map[string]any) (map[string]any, error) {
		record := goalManager.Get()
		if record == nil {
			msg := "当前会话没有设置持续目标。"
			if language == "en" {
				msg = "No persistent goal is set for this session."
			}
			return map[string]any{"has_goal": false, "message": msg}, nil
		}
		out := map[string]any{
			"has_goal":      true,
			"goal_id":       record.GoalID,
			"objective":     record.Objective,
			"status":        string(record.Status),
			"attempt_count": record.AttemptCount,
		}
		if record.LastAssessment != nil {
			out["last_assessment"] = record.LastAssessment.ToDict()
		}
		return out, nil
	}, nil)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// asString 把 any 转为 string，nil 或非 string 返回空串。
func asString(v any) string {
	s, _ := v.(string)
	return s
}

// optionalString 把 any 转为 *string，nil 或空串返回 nil。
func optionalString(v any) *string {
	s, ok := v.(string)
	if !ok || s == "" {
		return nil
	}
	return &s
}
