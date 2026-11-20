package external

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/tools/database"
)

// taskRow RenderTaskBoard/RenderTaskLine 的数据库行视图。
type taskRow struct {
	id        string
	status    string
	title     string
	content   string
	assignee  string
	updatedAt int64
}

// TaskID 任务 id。
func (t *taskRow) TaskID() string { return t.id }

// Status 状态。
func (t *taskRow) Status() string { return t.status }

// Title 标题。
func (t *taskRow) Title() string { return t.title }

// Content 正文。
func (t *taskRow) Content() string { return t.content }

// Assignee 执行者。
func (t *taskRow) Assignee() string { return t.assignee }

// UpdatedAtMs 最后迁移时间。
func (t *taskRow) UpdatedAtMs() *int64 { return &t.updatedAt }

// TestRenderTaskBoard_Leader 看板渲染对齐 Python render_task_board（leader 面）。
func TestRenderTaskBoard_Leader(t *testing.T) {
	now := int64(1000000)
	tasks := []TaskLike{
		&taskRow{id: "t1", status: "pending", title: "搭建", content: "骨架", assignee: ""},
		&taskRow{id: "t2", status: "completed", title: "完成", content: "", assignee: "dev"},
	}
	board, err := RenderTaskBoard(tasks, true, now, "leader")
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	// leader 看板：非终态任务全部出现但**不带正文**（Python
	// with_content=not is_leader——正文都是 leader 自己写的）
	if !strings.Contains(board, "[t1] [pending] 搭建") {
		t.Fatalf("leader 行应出现，实际: %s", board)
	}
	if strings.Contains(board, "搭建: 骨架") {
		t.Fatal("leader 行不应带正文")
	}
	if strings.Contains(board, "[t2]") {
		t.Fatal("终态任务应被过滤")
	}
	if !strings.Contains(board, "task-board") {
		t.Fatal("应包 <team-event kind=\"task-board\"> 块")
	}
	// 全终态 → 空串
	board, err = RenderTaskBoard([]TaskLike{&taskRow{id: "t2", status: "completed"}}, true, now, "leader")
	if err != nil || board != "" {
		t.Fatalf("全终态应空串，实际: %q err=%v", board, err)
	}
}

// TestRenderTaskBoard_Teammate teammate 只看可开工任务。
func TestRenderTaskBoard_Teammate(t *testing.T) {
	now := int64(1000000)
	tasks := []TaskLike{
		&taskRow{id: "t1", status: "pending", title: "搭建", content: "骨架", assignee: ""},
		&taskRow{id: "t3", status: "pending", title: "别人领的", content: "x", assignee: "other"},
	}
	board, err := RenderTaskBoard(tasks, false, now, "dev")
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(board, "[t1]") {
		t.Fatal("未指派 pending 应出现")
	}
	if strings.Contains(board, "[t3]") {
		t.Fatal("他人认领的 pending 不应出现")
	}
}

// TestRenderTaskLine 行格式：assignee 标记与 blocked_by 后缀。
func TestRenderTaskLine(t *testing.T) {
	now := int64(1000000)
	line, err := RenderTaskLine(&taskRow{id: "t1", status: "claimed", title: "做", content: "x", assignee: "dev"}, now, true, []string{"t0"})
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(line, "→ dev") || !strings.Contains(line, "[blocked by #t0]") {
		t.Fatalf("行格式不符: %s", line)
	}
	line, err = RenderTaskLine(&taskRow{id: "t1", status: "pending", title: "做", content: "x"}, now, true, nil)
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(line, "待领取") {
		t.Fatalf("未指派应带标记: %s", line)
	}
}

// TestRenderTaskBoard_DBRows 数据库行经适配（格式同源语义冒烟：TeamMessageBase
// 时间戳直进 RenderMessage 面由 client.go 消费）。
func TestRenderTaskBoard_DBRows(t *testing.T) {
	now := int64(1000000)
	row := &database.TeamTaskBase{TaskID: "t9", Status: "pending", Title: "r", Content: "c"}
	board, err := RenderTaskBoard([]TaskLike{&teamTaskLikeView{row: row}}, false, now, "dev")
	if err != nil {
		t.Fatalf("渲染失败: %v", err)
	}
	if !strings.Contains(board, "[t9] [pending] r: c") {
		t.Fatalf("DB 行渲染不符: %s", board)
	}
}

// teamTaskLikeView database.TeamTaskBase 的 TaskLike 适配（client.go 适配器的
// 测试侧同形复刻，避免引 runtime 包成环）。
type teamTaskLikeView struct {
	row *database.TeamTaskBase
}

// TaskID 任务 id。
func (v *teamTaskLikeView) TaskID() string { return v.row.TaskID }

// Status 状态。
func (v *teamTaskLikeView) Status() string { return v.row.Status }

// Title 标题。
func (v *teamTaskLikeView) Title() string { return v.row.Title }

// Content 正文。
func (v *teamTaskLikeView) Content() string { return v.row.Content }

// Assignee 执行者。
func (v *teamTaskLikeView) Assignee() string {
	if v.row.Assignee == nil {
		return ""
	}
	return *v.row.Assignee
}

// UpdatedAtMs 最后迁移时间。
func (v *teamTaskLikeView) UpdatedAtMs() *int64 {
	if v.row.UpdatedAt == 0 {
		return nil
	}
	return &v.row.UpdatedAt
}
