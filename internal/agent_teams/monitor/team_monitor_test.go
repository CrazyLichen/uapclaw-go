package monitor

import (
	"context"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// fakeTeamAgent 实现 EventListenerRegistrar 接口
type fakeTeamAgent struct {
	mu             sync.Mutex
	handlers       map[uint64]*messager.EventListenerHandle
	listenerSeq    uint64
}

func newFakeTeamAgent() *fakeTeamAgent {
	return &fakeTeamAgent{
		handlers: make(map[uint64]*messager.EventListenerHandle),
	}
}

func (f *fakeTeamAgent) AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.listenerSeq++
	h := messager.NewEventListenerHandle(handler)
	f.handlers[h.ID()] = h
	return h
}

func (f *fakeTeamAgent) RemoveEventListener(handle *messager.EventListenerHandle) {
	if handle == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.handlers, handle.ID())
}

func (f *fakeTeamAgent) fireEvent(ctx context.Context, msg *events.EventMessage) {
	f.mu.Lock()
	handlers := make([]*messager.EventListenerHandle, 0, len(f.handlers))
	for _, h := range f.handlers {
		handlers = append(handlers, h)
	}
	f.mu.Unlock()
	for _, h := range handlers {
		_ = h.Handler()(ctx, msg)
	}
}

// ──────────────────────────── 测试 ────────────────────────────

// TestCreateMonitor_NilTeamAgent 校验 teamAgent 为 nil 时返回 error
func TestCreateMonitor_NilTeamAgent(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	_, err := CreateMonitor(nil, db, "team", "session", false)
	if err == nil {
		t.Error("CreateMonitor(nil) 应返回 error")
	}
}

// TestCreateMonitor_正常 校验正常创建
func TestCreateMonitor_正常(t *testing.T) {
	agent := newFakeTeamAgent()
	db := database.NewInMemoryTeamDatabase()
	mon, err := CreateMonitor(agent, db, "team1", "sess1", false)
	if err != nil {
		t.Fatalf("CreateMonitor 返回 error: %v", err)
	}
	if mon.TeamName() != "team1" {
		t.Errorf("TeamName = %q, want %q", mon.TeamName(), "team1")
	}
	if mon.SessionID() != "sess1" {
		t.Errorf("SessionID = %q, want %q", mon.SessionID(), "sess1")
	}
}

// TestTeamMonitor_StartStop 校验 Start/Stop 生命周期
func TestTeamMonitor_StartStop(t *testing.T) {
	agent := newFakeTeamAgent()
	db := database.NewInMemoryTeamDatabase()
	mon := NewTeamMonitor("team", "sess", db, agent, false)

	ctx := context.Background()
	if err := mon.Start(ctx); err != nil {
		t.Fatalf("Start 返回 error: %v", err)
	}
	if !mon.started {
		t.Error("started 应为 true")
	}
	if len(agent.handlers) != 1 {
		t.Errorf("handlers 数量 = %d, want 1", len(agent.handlers))
	}

	if err := mon.Stop(ctx); err != nil {
		t.Fatalf("Stop 返回 error: %v", err)
	}
	if mon.started {
		t.Error("stopped 后 started 应为 false")
	}
	if len(agent.handlers) != 0 {
		t.Errorf("handlers 数量 = %d, want 0", len(agent.handlers))
	}
}

// TestTeamMonitor_Start_幂等 连续 Start 两次只注册一次
func TestTeamMonitor_Start_幂等(t *testing.T) {
	agent := newFakeTeamAgent()
	db := database.NewInMemoryTeamDatabase()
	mon := NewTeamMonitor("team", "sess", db, agent, false)

	ctx := context.Background()
	_ = mon.Start(ctx)
	_ = mon.Start(ctx)

	if len(agent.handlers) != 1 {
		t.Errorf("两次 Start 后 handlers = %d, want 1", len(agent.handlers))
	}
	_ = mon.Stop(ctx)
}

// TestTeamMonitor_Stop_幂等 未 Start 时 Stop 不 panic
func TestTeamMonitor_Stop_幂等(t *testing.T) {
	agent := newFakeTeamAgent()
	db := database.NewInMemoryTeamDatabase()
	mon := NewTeamMonitor("team", "sess", db, agent, false)

	ctx := context.Background()
	if err := mon.Stop(ctx); err != nil {
		t.Errorf("未 Start 时 Stop 不应返回 error: %v", err)
	}
}

// TestTeamMonitor_Events 校验事件流：手动触发 → Events channel 收到 MonitorEvent
func TestTeamMonitor_Events(t *testing.T) {
	agent := newFakeTeamAgent()
	db := database.NewInMemoryTeamDatabase()
	mon := NewTeamMonitor("team", "sess", db, agent, false)

	ctx := context.Background()
	_ = mon.Start(ctx)

	// 模拟事件
	msg := &events.EventMessage{
		EventType: "member_spawned",
		Payload: map[string]any{
			"team_name":   "team",
			"member_name": "m1",
		},
	}
	agent.fireEvent(ctx, msg)

	// 从 Events channel 读取
	select {
	case evt := <-mon.Events():
		if evt == nil {
			t.Error("收到 nil sentinel")
		} else if evt.EventType != MonitorEventTypeMemberSpawned {
			t.Errorf("EventType = %q, want %q", evt.EventType, MonitorEventTypeMemberSpawned)
		}
	default:
		t.Error("Events channel 中无事件")
	}

	_ = mon.Stop(ctx)
}

// TestTeamMonitor_Events_HideDM 校验 hideDM 过滤 MESSAGE 事件
func TestTeamMonitor_Events_HideDM(t *testing.T) {
	agent := newFakeTeamAgent()
	db := database.NewInMemoryTeamDatabase()
	mon := NewTeamMonitor("team", "sess", db, agent, true) // hideDM=true

	ctx := context.Background()
	_ = mon.Start(ctx)

	// 发送 MESSAGE 事件，应被丢弃
	msg := &events.EventMessage{
		EventType: "message",
		Payload: map[string]any{
			"team_name":  "team",
			"message_id": "msg-1",
		},
	}
	agent.fireEvent(ctx, msg)

	// 发送 BROADCAST 事件，应保留
	broadcastMsg := &events.EventMessage{
		EventType: "broadcast",
		Payload: map[string]any{
			"team_name":  "team",
			"message_id": "msg-2",
		},
	}
	agent.fireEvent(ctx, broadcastMsg)

	// 只应收到 BROADCAST
	select {
	case evt := <-mon.Events():
		if evt.EventType != MonitorEventTypeBroadcast {
			t.Errorf("EventType = %q, want %q", evt.EventType, MonitorEventTypeBroadcast)
		}
	default:
		t.Error("Events channel 中无事件")
	}

	// 不应有第二个事件（MESSAGE 被过滤）
	select {
	case evt := <-mon.Events():
		if evt != nil {
			t.Errorf("不应收到 MESSAGE 事件，收到 %q", evt.EventType)
		}
	default:
		// 正常：channel 为空
	}

	_ = mon.Stop(ctx)
}

// TestTeamMonitor_GetTeamInfo 校验查询团队信息
func TestTeamMonitor_GetTeamInfo(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	_ = db.Initialize(ctx)

	// 创建团队
	db.Team().CreateTeam(ctx, "team1", "测试团队", "leader", "描述", "")

	agent := newFakeTeamAgent()
	mon := NewTeamMonitor("team1", "sess", db, agent, false)

	info, err := mon.GetTeamInfo(ctx)
	if err != nil {
		t.Fatalf("GetTeamInfo 返回 error: %v", err)
	}
	if info == nil {
		t.Fatal("GetTeamInfo 返回 nil")
	}
	if info.TeamName != "team1" {
		t.Errorf("TeamName = %q, want %q", info.TeamName, "team1")
	}
	if info.DisplayName != "测试团队" {
		t.Errorf("DisplayName = %q, want %q", info.DisplayName, "测试团队")
	}
}

// TestTeamMonitor_GetMembers 校验查询成员列表
func TestTeamMonitor_GetMembers(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	_ = db.Initialize(ctx)

	// 创建成员
	db.Member().CreateMember(ctx, "m1", "team1", "成员1", "", "active", "leader", "", "idle", "build_mode", "", "")

	agent := newFakeTeamAgent()
	mon := NewTeamMonitor("team1", "sess", db, agent, false)

	members, err := mon.GetMembers(ctx, "")
	if err != nil {
		t.Fatalf("GetMembers 返回 error: %v", err)
	}
	if len(members) != 1 {
		t.Fatalf("成员数量 = %d, want 1", len(members))
	}
	if members[0].MemberName != "m1" {
		t.Errorf("MemberName = %q, want %q", members[0].MemberName, "m1")
	}
	if members[0].Status != "active" {
		t.Errorf("Status = %q, want %q", members[0].Status, "active")
	}
}

// TestTeamMonitor_GetTasks 校验查询任务列表
func TestTeamMonitor_GetTasks(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	_ = db.Initialize(ctx)
	_ = db.CreateCurSessionTables(ctx)

	assignee := "m1"
	db.Task().CreateTask(ctx, &database.TeamTaskBase{
		TaskID: "task-1", TeamName: "team1", Title: "任务1",
		Content: "内容", Status: "pending", Assignee: &assignee,
	})

	agent := newFakeTeamAgent()
	mon := NewTeamMonitor("team1", "sess", db, agent, false)

	tasks, err := mon.GetTasks(ctx, "")
	if err != nil {
		t.Fatalf("GetTasks 返回 error: %v", err)
	}
	if len(tasks) != 1 {
		t.Fatalf("任务数量 = %d, want 1", len(tasks))
	}
	if tasks[0].TaskID != "task-1" {
		t.Errorf("TaskID = %q, want %q", tasks[0].TaskID, "task-1")
	}
}

// TestTeamMonitor_GetMessages_HideDM 校验 hideDM + toMemberName → 空列表
func TestTeamMonitor_GetMessages_HideDM(t *testing.T) {
	db := database.NewInMemoryTeamDatabase()
	ctx := context.Background()
	_ = db.Initialize(ctx)

	agent := newFakeTeamAgent()
	mon := NewTeamMonitor("team1", "sess", db, agent, true) // hideDM=true

	msgs, err := mon.GetMessages(ctx, "m1", "")
	if err != nil {
		t.Fatalf("GetMessages 返回 error: %v", err)
	}
	if msgs != nil {
		t.Errorf("hideDM + toMemberName 应返回 nil, got %d 项", len(msgs))
	}
}
