package database

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/fsm"
)

// TestStartTaskCAS_仅归属且PENDING胜出 对齐 Python start_task CAS：
// WHERE assignee=member AND status=pending；resume_pending 被消费（F_128）。
// Python: task_dao.py L855-905
func TestStartTaskCAS_仅归属且PENDING胜出(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: StringPtr("bob"), ResumePending: true})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPending, Assignee: StringPtr("carol")})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t3", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: StringPtr("dave")})

	// 非 owner 启动失败
	if ok, _ := db.StartTaskCAS(ctx, "t1", "mallory", fsm.TaskStatusInProgress); ok {
		t.Error("非 owner 不得启动")
	}
	// 非 PENDING 失败
	if ok, _ := db.StartTaskCAS(ctx, "t3", "dave", fsm.TaskStatusInProgress); ok {
		t.Error("IN_PROGRESS 不是合法前驱")
	}
	// owner 成功且 resume_pending 被消费
	ok, err := db.StartTaskCAS(ctx, "t1", "bob", fsm.TaskStatusInProgress)
	if err != nil || !ok {
		t.Fatalf("owner 启动应成功: ok=%v err=%v", ok, err)
	}
	task, _ := db.GetTask(ctx, "t1")
	if task.Status != fsm.TaskStatusInProgress || task.ResumePending {
		t.Errorf("状态应推进且 resume 消费: %+v", task)
	}
	// t2 属于 carol：第二次 CAS（并发语义在 SQL 层由单语句保证）
	if ok, _ := db.StartTaskCAS(ctx, "t2", "carol", fsm.TaskStatusInProgress); !ok {
		t.Error("carol 应能启动自己的任务")
	}
}

// TestGetUnseenRevisions_过滤条件 对齐 get_unseen_revisions：
// 已指派 + 未终态 + revision > observed_revision；不读正文。
// Python: task_dao.py L723-752
func TestGetUnseenRevisions_过滤条件(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "a", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: StringPtr("bob"), Revision: 2, ObservedRevision: 1})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "b", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: StringPtr("bob"), Revision: 1, ObservedRevision: 1})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "c", TeamName: "alpha", Status: fsm.TaskStatusCompleted, Assignee: StringPtr("bob"), Revision: 2, ObservedRevision: 0})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "d", TeamName: "alpha", Status: fsm.TaskStatusPending, Revision: 2, ObservedRevision: 0})

	rows, err := db.GetUnseenRevisions(ctx, "alpha")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 || rows[0].TaskID != "a" || rows[0].Assignee != "bob" || rows[0].Revision != 2 {
		t.Errorf("只有 a 满足全部条件: %+v", rows)
	}
}

// TestGetOtherActiveTaskID_三活跃态 对齐 get_other_active_task_id：三活跃态
// LIMIT 1 存在式探测，排除 exclude_task_id。
// Python: task_dao.py L822-852
func TestGetOtherActiveTaskID_三活跃态(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusInProgress, Assignee: StringPtr("bob")})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusPlanning, Assignee: StringPtr("bob")})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t3", TeamName: "alpha", Status: fsm.TaskStatusCompleted, Assignee: StringPtr("bob")})

	id, err := db.GetOtherActiveTaskID(ctx, "alpha", "bob", "t1")
	if err != nil || id == nil || *id != "t2" {
		t.Errorf("排除 t1 后应命中 t2: %v err=%v", id, err)
	}
	id, _ = db.GetOtherActiveTaskID(ctx, "alpha", "bob", "t2")
	if id == nil || *id != "t1" {
		t.Errorf("排除 t2 后应命中 t1: %v", id)
	}
	id, _ = db.GetOtherActiveTaskID(ctx, "alpha", "bob", "zzz")
	if id == nil {
		t.Error("排除未知 id 应命中任一活跃任务")
	}
}

// TestCountTasksTerminality_B09 对齐 count_tasks_terminality 聚合。
func TestCountTasksTerminality_B09(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t1", TeamName: "alpha", Status: fsm.TaskStatusInProgress})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t2", TeamName: "alpha", Status: fsm.TaskStatusCompleted})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "t3", TeamName: "alpha", Status: fsm.TaskStatusPending})
	total, nonTerminal, err := db.CountTasksTerminality(ctx, "alpha")
	if err != nil || total != 3 || nonTerminal != 2 {
		t.Errorf("(3, 2) 期望: got (%d, %d) err=%v", total, nonTerminal, err)
	}
}

// TestMembersWithUnread_直发与广播 对齐 members_with_unread 两读取器。
// Python: message_dao.py L483-497
func TestMembersWithUnread_直发与广播(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	db.CreateMember(ctx, "bob", "alpha", "Bob", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "{}")
	db.CreateMember(ctx, "carol", "alpha", "Carol", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "{}")

	db.CreateMessage(ctx, &TeamMessageBase{MessageID: "m1", TeamName: "alpha", FromMemberName: "leader1", ToMemberName: StringPtr("bob"), IsRead: BoolPtr(false), Timestamp: 100})
	db.CreateMessage(ctx, &TeamMessageBase{MessageID: "m2", TeamName: "alpha", FromMemberName: "leader1", ToMemberName: StringPtr("bob"), IsRead: BoolPtr(true), Timestamp: 110})
	db.CreateMessage(ctx, &TeamMessageBase{MessageID: "m3", TeamName: "alpha", FromMemberName: "leader1", Broadcast: true, Timestamp: 200})

	got, err := db.MembersWithUnread(ctx, "alpha", []string{"bob", "carol"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if !got["bob"] {
		t.Error("bob 有未读直发")
	}
	if !got["carol"] {
		t.Error("carol 未读水位落后于他人广播")
	}
	if err := db.MarkMessageRead(ctx, "m1", "bob"); !err {
		t.Fatal("标记 m1 已读失败")
	}
	db.MarkMessageRead(ctx, "m3", "carol")
	got, _ = db.MembersWithUnread(ctx, "alpha", []string{"bob", "carol"})
	if got["carol"] {
		t.Error("carol 追平广播水位后不应再有未读")
	}
}

// TestListRetirableMembers_Ready与lifetime 对齐 list_retirable_members：
// 只取 READY 且 options 带 lifetime。
// Python: member_dao.py L338-367
func TestListRetirableMembers_Ready与lifetime(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	optsShutdown := `{"lifetime":"shutdown"}`
	db.CreateMember(ctx, "inline1", "alpha", "I1", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", optsShutdown)
	db.CreateMember(ctx, "worker", "alpha", "W", "{}", fsm.MemberStatusReady, "teammate", "", "", "build_mode", "", "{}")
	db.CreateMember(ctx, "busy", "alpha", "B", "{}", fsm.MemberStatusBusy, "teammate", "", "", "build_mode", "", optsShutdown)

	rows, err := db.ListRetirableMembers(ctx, "alpha")
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(rows) != 1 || rows[0].MemberName != "inline1" || rows[0].Lifetime != "shutdown" || rows[0].Status != fsm.MemberStatusReady {
		t.Errorf("只有 READY+lifetime 的 inline1 满足: %+v", rows)
	}

	gone, err := db.ListShutdownMembers(ctx, "alpha")
	if err != nil || len(gone) != 0 {
		t.Errorf("无 SHUTDOWN 成员: %v err=%v", gone, err)
	}
}

// TestGetSubtasks_GetDependentTaskIDs_GetTasksByIDs 新增查询方法的内存实现验证。
func TestGetSubtasks_GetDependentTaskIDs_GetTasksByIDs(t *testing.T) {
	db := NewInMemoryTeamDatabase()
	ctx := context.Background()
	db.CreateTeam(ctx, "alpha", "Alpha", "leader1", "", "", "scheduled", false)
	parent := "p1"
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "p1", TeamName: "alpha", Status: fsm.TaskStatusInProgress})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "c1", TeamName: "alpha", Status: fsm.TaskStatusPending, ParentTaskID: &parent})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "c2", TeamName: "alpha", Status: fsm.TaskStatusPending, ParentTaskID: &parent})
	db.CreateTask(ctx, &TeamTaskBase{TaskID: "other", TeamName: "alpha", Status: fsm.TaskStatusPending})

	subs, err := db.GetSubtasks(ctx, "alpha", "p1")
	if err != nil || len(subs) != 2 {
		t.Errorf("p1 应有两个子任务: %d err=%v", len(subs), err)
	}
	db.MutateDependencyGraph(ctx, "alpha", nil, []EdgeSpec{{TaskID: "c1", DependsOnID: "p1"}})
	ids, err := db.GetDependentTaskIDs(ctx, "p1")
	if err != nil || len(ids) != 1 || ids[0] != "c1" {
		t.Errorf("p1 的下游应为 [c1]: %v err=%v", ids, err)
	}
	rows, err := db.GetTasksByIDs(ctx, []string{"p1", "other", "missing"})
	if err != nil || len(rows) != 2 {
		t.Errorf("IN 查询应命中 2 行（缺失跳过）: %d err=%v", len(rows), err)
	}
}
