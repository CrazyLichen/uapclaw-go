// Package goal 提供持续目标（Goal）相关的两个工具：
// submit_goal_report（提交结构化尝试结果）与 get_current_goal（只读查询当前目标）。
//
// 本包对应 Python openjiuwen/harness/tools/goal.py。
//
// 设计要点：
//   - GoalReportSink 只暂存报告、不写 GoalRecord，由 TaskCompletionRail 轮末消费；
//   - 两个工具仅 goal_manager 绑定后注册（由 TaskCompletionRail.Init 完成），全程可用。
//
// 文件目录：
//
//	goal/
//	├── doc.go      # 包文档
//	├── goal.go     # GoalReportSink + SubmitGoalReportTool + GetCurrentGoalTool
//	└── goal_test.go # 工具单测
//
// 对应 Python 代码：openjiuwen/harness/tools/goal.py
package goal

import "github.com/uapclaw/uapclaw-go/internal/common/logger"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// logComponent 日志组件标识
	logComponent = logger.ComponentAgentCore
)
