package team

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// teamTestFakeAgent 实现 monitor.EventListenerRegistrar 接口（team 包测试用）
type teamTestFakeAgent struct {
	mu       sync.Mutex
	handlers map[uint64]*messager.EventListenerHandle
}

func newTeamTestFakeAgent() *teamTestFakeAgent {
	return &teamTestFakeAgent{
		handlers: make(map[uint64]*messager.EventListenerHandle),
	}
}

func (f *teamTestFakeAgent) AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle {
	f.mu.Lock()
	defer f.mu.Unlock()
	h := messager.NewEventListenerHandle(handler)
	f.handlers[h.ID()] = h
	return h
}

func (f *teamTestFakeAgent) RemoveEventListener(handle *messager.EventListenerHandle) {
	if handle == nil {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.handlers, handle.ID())
}

func (f *teamTestFakeAgent) fireEvent(ctx context.Context, msg *events.EventMessage) {
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

// TestTeamMonitorHandlerImpl_StartStop 校验 Start/Stop 生命周期
func TestTeamMonitorHandlerImpl_StartStop(t *testing.T) {
	agent := newTeamTestFakeAgent()
	db := database.NewInMemoryTeamDatabase()
	_ = db.Initialize(context.Background())
	mon := monitor.NewTeamMonitor("team", "sess", db, agent, false)

	handler := newTeamMonitorHandlerImpl(mon, "sess")

	ctx := context.Background()
	if err := handler.Start(ctx); err != nil {
		t.Fatalf("Start 返回 error: %v", err)
	}
	if !handler.IsRunning() {
		t.Error("Start 后 IsRunning 应为 true")
	}

	if err := handler.Stop(ctx); err != nil {
		t.Fatalf("Stop 返回 error: %v", err)
	}
	if handler.IsRunning() {
		t.Error("Stop 后 IsRunning 应为 false")
	}
}

// TestTeamMonitorHandlerImpl_Start_NilMonitor 校验 monitor 为 nil 时 Start 返回 error
func TestTeamMonitorHandlerImpl_Start_NilMonitor(t *testing.T) {
	handler := newTeamMonitorHandlerImpl(nil, "sess")
	err := handler.Start(context.Background())
	if err == nil {
		t.Error("monitor 为 nil 时 Start 应返回 error")
	}
}

// TestTeamMonitorHandlerImpl_Events 校验事件流：触发事件 → Events channel 收到转换后的 dict
func TestTeamMonitorHandlerImpl_Events(t *testing.T) {
	agent := newTeamTestFakeAgent()
	db := database.NewInMemoryTeamDatabase()
	_ = db.Initialize(context.Background())
	mon := monitor.NewTeamMonitor("team", "sess", db, agent, false)

	handler := newTeamMonitorHandlerImpl(mon, "sess")
	ctx := context.Background()
	_ = handler.Start(ctx)

	// 触发 member_spawned 事件
	msg := &events.EventMessage{
		EventType: "member_spawned",
		Payload: map[string]any{
			"team_name":   "team",
			"member_name": "m1",
		},
	}
	agent.fireEvent(ctx, msg)

	// 从 Events channel 读取（等待事件收集 goroutine 处理）
	select {
	case evtDict := <-handler.Events():
		if evtDict == nil {
			t.Error("收到 nil 事件")
		} else {
			// 验证外层包装
			if evtDict["event_type"] != "team.member" {
				t.Errorf("event_type = %v, want team.member", evtDict["event_type"])
			}
			if evtDict["session_id"] != "sess" {
				t.Errorf("session_id = %v, want sess", evtDict["session_id"])
			}
			// 验证内层 event
			inner, ok := evtDict["event"].(map[string]any)
			if !ok {
				t.Error("event 不是 map[string]any")
			} else {
				if inner["type"] != "team.member.spawned" {
					t.Errorf("type = %v, want team.member.spawned", inner["type"])
				}
				if inner["team_id"] != "team" {
					t.Errorf("team_id = %v, want team", inner["team_id"])
				}
				if inner["member_id"] != "m1" {
					t.Errorf("member_id = %v, want m1", inner["member_id"])
				}
			}
		}
	case <-time.After(2 * time.Second):
		t.Error("Events channel 中无事件（超时 2s）")
	}

	_ = handler.Stop(ctx)
}

// TestConvertEventToDict_未映射事件 校验未映射事件返回 nil
func TestConvertEventToDict_未映射事件(t *testing.T) {
	handler := newTeamMonitorHandlerImpl(nil, "sess")
	evt := &monitor.MonitorEvent{
		EventType: monitor.MonitorEventTypeTeamCreated,
		TeamName:  "team",
	}
	result := handler.convertEventToDict(context.Background(), evt)
	if result != nil {
		t.Error("未映射事件应返回 nil")
	}
}

// TestConvertEventToDict_成员状态变更 校验 old_status/new_status 字段
func TestConvertEventToDict_成员状态变更(t *testing.T) {
	handler := newTeamMonitorHandlerImpl(nil, "sess")
	oldStatus := "idle"
	newStatus := "active"
	memberName := "m1"
	evt := &monitor.MonitorEvent{
		EventType:  monitor.MonitorEventTypeMemberStatusChanged,
		TeamName:   "team",
		MemberName: &memberName,
		OldStatus:  &oldStatus,
		NewStatus:  &newStatus,
	}
	result := handler.convertEventToDict(context.Background(), evt)
	if result == nil {
		t.Fatal("convertEventToDict 返回 nil")
	}
	inner := result["event"].(map[string]any)
	if inner["old_status"] != "idle" {
		t.Errorf("old_status = %v, want idle", inner["old_status"])
	}
	if inner["new_status"] != "active" {
		t.Errorf("new_status = %v, want active", inner["new_status"])
	}
}

// TestConvertEventToDict_任务创建 校验 task_id/status 字段
func TestConvertEventToDict_任务创建(t *testing.T) {
	handler := newTeamMonitorHandlerImpl(nil, "sess")
	taskID := "task-1"
	status := "pending"
	evt := &monitor.MonitorEvent{
		EventType: monitor.MonitorEventTypeTaskCreated,
		TeamName:  "team",
		TaskID:    &taskID,
		Status:    &status,
	}
	result := handler.convertEventToDict(context.Background(), evt)
	if result == nil {
		t.Fatal("convertEventToDict 返回 nil")
	}
	if result["event_type"] != "team.task" {
		t.Errorf("event_type = %v, want team.task", result["event_type"])
	}
	inner := result["event"].(map[string]any)
	if inner["task_id"] != "task-1" {
		t.Errorf("task_id = %v, want task-1", inner["task_id"])
	}
	if inner["status"] != "pending" {
		t.Errorf("status = %v, want pending", inner["status"])
	}
}

// TestConvertEventToDict_消息事件 校验 message 事件转换
func TestConvertEventToDict_消息事件(t *testing.T) {
	handler := newTeamMonitorHandlerImpl(nil, "sess")
	msgID := "msg-1"
	fromMember := "m1"
	evt := &monitor.MonitorEvent{
		EventType:      monitor.MonitorEventTypeMessage,
		TeamName:       "team",
		MessageID:      &msgID,
		FromMemberName: &fromMember,
	}
	result := handler.convertEventToDict(context.Background(), evt)
	if result == nil {
		t.Fatal("convertEventToDict 返回 nil")
	}
	if result["event_type"] != "team.message" {
		t.Errorf("event_type = %v, want team.message", result["event_type"])
	}
	inner := result["event"].(map[string]any)
	if inner["type"] != "team.message.p2p" {
		t.Errorf("type = %v, want team.message.p2p", inner["type"])
	}
	if inner["message_id"] != "msg-1" {
		t.Errorf("message_id = %v, want msg-1", inner["message_id"])
	}
}
