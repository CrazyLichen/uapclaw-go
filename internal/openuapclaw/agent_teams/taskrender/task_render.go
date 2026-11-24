package taskrender

import (
	"fmt"

	agentteams "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/inboundtags"
	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
)

// TaskLike 任务看板行渲染用的任务行结构视图（数据库行或测试假实现均可）。
// Python: _TaskLike (format.py)
type TaskLike interface {
	// TaskID 任务 id
	TaskID() string
	// Status 任务状态
	Status() string
	// Title 任务标题
	Title() string
	// Content 任务正文（with_content=false 时由调用方忽略）
	Content() string
	// Assignee 执行者成员名；空表示未指派
	Assignee() string
	// UpdatedAtMs 最后状态迁移的毫秒 UTC epoch；nil 表示从未迁移
	UpdatedAtMs() *int64
}

// terminalTaskStatuses 终态任务状态集合。
// Python: _TERMINAL_TASK_STATUSES (format.py:28)
var terminalTaskStatuses = map[string]bool{"completed": true, "cancelled": true}

// RenderTaskLine 渲染任务看板的一行（带最后迁移时间）。
// 看板行格式的单一真源：进程内 TaskBoardHandler 与外部任务看板渲染共用。
// Python: render_task_line (format.py:131-163)
func RenderTaskLine(task TaskLike, nowMs int64, withContent bool, blockedBy []string) (string, error) {
	assignee := task.Assignee()
	if assignee != "" {
		assignee = " → " + assignee
	} else {
		marker, err := atschema.T("dispatcher.task_unassigned_marker")
		if err != nil {
			return "", fmt.Errorf("解析 dispatcher.task_unassigned_marker 失败: %w", err)
		}
		assignee = marker
	}
	timeInfo := agentteams.FormatTimeContext(task.UpdatedAtMs(), nowMs)
	body := ""
	if withContent {
		body = ": " + task.Content()
	}
	line := fmt.Sprintf("- [%s] [%s] %s%s%s (%s)", task.TaskID(), task.Status(), task.Title(), body, assignee, timeInfo)
	if len(blockedBy) > 0 {
		refs := make([]string, 0, len(blockedBy))
		for _, id := range blockedBy {
			refs = append(refs, "#"+id)
		}
		line += " [blocked by " + join(refs, ", ") + "]"
	}
	return line, nil
}

// RenderTaskBoard 把可行动任务看板渲染成 <team-event kind="task-board">。
// 镜像 TaskBoardHandler 的 render_event(kind="task-board")：角色专属表头 +
// 每任务一行，包进单个 <team-event> 块。leader 看到全部非终态任务（不带正文）；
// teammate 只看到现在能开工的——未指派 pending 与指派给自己的 pending。
// Python: render_task_board (format.py:166-210)
func RenderTaskBoard(tasks []TaskLike, isLeader bool, nowMs int64, memberName string) (string, error) {
	var incomplete []TaskLike
	if isLeader {
		for _, task := range tasks {
			if !terminalTaskStatuses[task.Status()] {
				incomplete = append(incomplete, task)
			}
		}
	} else {
		for _, task := range tasks {
			if task.Status() == "pending" && (task.Assignee() == "" || task.Assignee() == memberName) {
				incomplete = append(incomplete, task)
			}
		}
	}
	if len(incomplete) == 0 {
		return "", nil
	}

	headerKey := "dispatcher.teammate_task_list"
	if isLeader {
		headerKey = "dispatcher.leader_task_board"
	}
	header, err := atschema.T(headerKey)
	if err != nil {
		return "", fmt.Errorf("解析 %s 失败: %w", headerKey, err)
	}
	lines := []string{header}
	for _, task := range incomplete {
		line, err := RenderTaskLine(task, nowMs, !isLeader, nil)
		if err != nil {
			return "", err
		}
		lines = append(lines, line)
	}
	return agentteams.RenderEvent("task-board", join(lines, "\n"), agentteams.EventOptions{}), nil
}

// join 局部字符串拼接（本包不引入 strings 依赖面）。
func join(parts []string, sep string) string {
	if len(parts) == 0 {
		return ""
	}
	out := parts[0]
	for _, p := range parts[1:] {
		out += sep + p
	}
	return out
}
