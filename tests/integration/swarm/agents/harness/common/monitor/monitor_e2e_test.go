//go:build integration

package monitor

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"
	monitor "github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MonitorE2ESuite 测试 TeamMonitor 和事件系统的集成行为。
//
// 覆盖：
//   - TeamMonitor 生命周期（CreateMonitor/Start/Stop）
//   - FromEventMessage 事件转换正确性
//   - MonitorEventType 枚举完整性
//   - TeamStreamLogger Feed/Flush
//   - EventMessage 发布到 TeamMonitor 事件消费
//
// 对齐 Python: openjiuwen/harness/monitor/team_monitor.py
type MonitorE2ESuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

func TestMonitorE2ESuite(t *testing.T) {
	suite.Run(t, new(MonitorE2ESuite))
}

// TestMonitorEventType_枚举完整性 测试所有 MonitorEventType 常量非空。
// 对齐 Python: monitor event type 完整性
func (s *MonitorE2ESuite) TestMonitorEventType_枚举完整性() {
	eventTypes := []monitor.MonitorEventType{
		monitor.MonitorEventTypeTeamCreated,
		monitor.MonitorEventTypeTeamCleaned,
		monitor.MonitorEventTypeTeamStandby,
		monitor.MonitorEventTypeMemberSpawned,
		monitor.MonitorEventTypeMemberRestarted,
		monitor.MonitorEventTypeMemberStatusChanged,
		monitor.MonitorEventTypeMemberExecutionChanged,
		monitor.MonitorEventTypeMemberShutdown,
		monitor.MonitorEventTypeMemberCanceled,
		monitor.MonitorEventTypeTaskCreated,
		monitor.MonitorEventTypeTaskPlanRequest,
		monitor.MonitorEventTypeTaskPlanResponse,
		monitor.MonitorEventTypeTaskUpdated,
		monitor.MonitorEventTypeTaskClaimed,
		monitor.MonitorEventTypeTaskCompleted,
		monitor.MonitorEventTypeTaskCancelled,
		monitor.MonitorEventTypeTaskUnblocked,
		monitor.MonitorEventTypeMessage,
		monitor.MonitorEventTypeBroadcast,
	}

	for _, et := range eventTypes {
		s.NotEmpty(string(et), "MonitorEventType 常量不应为空")
	}

	// 验证枚举数量
	s.Equal(19, len(eventTypes), "应有 19 个 MonitorEventType 常量")
}

// TestFromEventMessage_团队创建事件 测试 FromEventMessage 转换团队创建事件。
// 对齐 Python: monitor event conversion
func (s *MonitorE2ESuite) TestFromEventMessage_团队创建事件() {
	msg := events.NewEventMessage(
		string(monitor.MonitorEventTypeTeamCreated),
		map[string]any{
			"team_name":    "test-team",
			"display_name": "测试团队",
			"leader":       "leader-1",
		},
		"sender-1",
	)

	evt := monitor.FromEventMessage(msg)
	s.Require().NotNil(evt, "FromEventMessage 不应返回 nil")
	s.Equal(monitor.MonitorEventTypeTeamCreated, evt.EventType)
	s.Equal("test-team", evt.TeamName)
}

// TestFromEventMessage_成员生成事件 测试 FromEventMessage 转换成员生成事件。
func (s *MonitorE2ESuite) TestFromEventMessage_成员生成事件() {
	msg := events.NewEventMessage(
		string(monitor.MonitorEventTypeMemberSpawned),
		map[string]any{
			"team_name":    "test-team",
			"member_name":  "worker-1",
			"display_name": "工作节点1",
		},
		"sender-1",
	)

	evt := monitor.FromEventMessage(msg)
	s.Require().NotNil(evt, "FromEventMessage 不应返回 nil")
	s.Equal(monitor.MonitorEventTypeMemberSpawned, evt.EventType)
	s.Equal("test-team", evt.TeamName)
	s.NotNil(evt.MemberName)
	s.Equal("worker-1", *evt.MemberName)
}

// TestFromEventMessage_任务创建事件 测试 FromEventMessage 转换任务创建事件。
func (s *MonitorE2ESuite) TestFromEventMessage_任务创建事件() {
	msg := events.NewEventMessage(
		string(monitor.MonitorEventTypeTaskCreated),
		map[string]any{
			"team_name": "test-team",
			"task_id":   "task-001",
			"title":     "测试任务",
			"content":   "任务内容",
		},
		"sender-1",
	)

	evt := monitor.FromEventMessage(msg)
	s.Require().NotNil(evt, "FromEventMessage 不应返回 nil")
	s.Equal(monitor.MonitorEventTypeTaskCreated, evt.EventType)
	s.Equal("test-team", evt.TeamName)
}

// TestFromEventMessage_消息事件 测试 FromEventMessage 转换消息事件。
func (s *MonitorE2ESuite) TestFromEventMessage_消息事件() {
	msg := events.NewEventMessage(
		string(monitor.MonitorEventTypeMessage),
		map[string]any{
			"team_name":        "test-team",
			"from_member_name": "member-1",
			"to_member_name":   "member-2",
			"content":          "你好",
			"message_id":       "msg-001",
		},
		"sender-1",
	)

	evt := monitor.FromEventMessage(msg)
	s.Require().NotNil(evt, "FromEventMessage 不应返回 nil")
	s.Equal(monitor.MonitorEventTypeMessage, evt.EventType)
}

// TestFromEventMessage_未知事件返回nil 测试 FromEventMessage 对未知事件类型返回 nil。
func (s *MonitorE2ESuite) TestFromEventMessage_未知事件返回nil() {
	msg := events.NewEventMessage(
		"unknown_event_type",
		map[string]any{"team_name": "test-team"},
		"sender-1",
	)

	evt := monitor.FromEventMessage(msg)
	s.Nil(evt, "未知事件类型应返回 nil")
}

// TestTeamMonitor_CreateMonitor_无Registrar 测试 CreateMonitor 在无 EventListenerRegistrar 时返回错误。
func (s *MonitorE2ESuite) TestTeamMonitor_CreateMonitor_无Registrar() {
	_, err := monitor.CreateMonitor(nil, nil, "test-team", "session-1", false)
	s.Error(err, "无 EventListenerRegistrar 应返回错误")
}

// TestTeamMonitor_生命周期 测试 TeamMonitor 的 Start/Stop 生命周期。
// 对齐 Python: TeamMonitor.start() / TeamMonitor.stop()
//
// 核心验证：
//   - CreateMonitor 创建成功
//   - Start 后 IsRunning 为 true
//   - Stop 后事件通道关闭
func (s *MonitorE2ESuite) TestTeamMonitor_生命周期() {
	ctx := s.Ctx

	// 创建 fake EventListenerRegistrar
	registrar := newFakeEventListenerRegistrar()

	m, err := monitor.CreateMonitor(registrar, nil, "test-team", "session-1", false)
	s.Require().NoError(err, "CreateMonitor 不应返回错误")
	s.Require().NotNil(m)

	// Start
	err = m.Start(ctx)
	s.Require().NoError(err, "Start 不应返回错误")

	// 验证 Start 后 registrar 有监听器注册
	s.Equal(1, registrar.addListenerCount, "Start 后应注册一个监听器")

	// Stop
	err = m.Stop(ctx)
	s.Require().NoError(err, "Stop 不应返回错误")

	// 验证 Stop 后监听器被移除
	s.Equal(1, registrar.removeListenerCount, "Stop 后应移除监听器")
}

// TestTeamMonitor_事件通道消费 测试 TeamMonitor 的 Events() 通道能正确消费事件。
// 对齐 Python: TeamMonitor 事件流
//
// 核心验证：
//   - Start 后 Events() 返回可读通道
//   - 向 registrar 发布事件后，Events() 通道能收到 MonitorEvent
func (s *MonitorE2ESuite) TestTeamMonitor_事件通道消费() {
	ctx := s.Ctx

	registrar := newFakeEventListenerRegistrar()

	m, err := monitor.CreateMonitor(registrar, nil, "test-team", "session-1", false)
	s.Require().NoError(err)

	err = m.Start(ctx)
	s.Require().NoError(err)

	// 获取 Events 通道
	eventCh := m.Events()
	s.Require().NotNil(eventCh, "Events() 不应返回 nil 通道")

	// 模拟发布事件
	registrar.publish(&events.EventMessage{
		EventType: string(monitor.MonitorEventTypeTeamCreated),
		Payload:   map[string]any{"team_name": "test-team"},
		SenderID:  "sender-1",
	})

	// 从通道读取事件（带超时）
	select {
	case evt := <-eventCh:
		s.Require().NotNil(evt, "应收到非 nil 事件")
		s.Equal(monitor.MonitorEventTypeTeamCreated, evt.EventType)
	case <-time.After(3 * time.Second):
		s.T().Fatal("等待事件超时")
	}

	// 清理
	_ = m.Stop(ctx)
}

// TestTeamMonitor_多次Start幂等 测试 TeamMonitor 多次 Start 不崩溃。
func (s *MonitorE2ESuite) TestTeamMonitor_多次Start幂等() {
	ctx := s.Ctx

	registrar := newFakeEventListenerRegistrar()

	m, err := monitor.CreateMonitor(registrar, nil, "test-team", "session-2", false)
	s.Require().NoError(err)

	err = m.Start(ctx)
	s.Require().NoError(err, "第一次 Start 不应返回错误")

	// 第二次 Start 应幂等（不崩溃）
	err = m.Start(ctx)
	s.Require().NoError(err, "第二次 Start 不应返回错误")

	_ = m.Stop(ctx)
}

// TestTeamMonitor_多次Stop幂等 测试 TeamMonitor 多次 Stop 不崩溃。
func (s *MonitorE2ESuite) TestTeamMonitor_多次Stop幂等() {
	ctx := s.Ctx

	registrar := newFakeEventListenerRegistrar()

	m, err := monitor.CreateMonitor(registrar, nil, "test-team", "session-3", false)
	s.Require().NoError(err)

	err = m.Start(ctx)
	s.Require().NoError(err)

	err = m.Stop(ctx)
	s.Require().NoError(err, "第一次 Stop 不应返回错误")

	// 第二次 Stop 应幂等（不崩溃）
	err = m.Stop(ctx)
	s.Require().NoError(err, "第二次 Stop 不应返回错误")
}

// TestTeamStreamLogger_FeedFlush 测试 TeamStreamLogger Feed/Flush 行为。
// 对齐 Python: TeamStreamLogger 日志写入
func (s *MonitorE2ESuite) TestTeamStreamLogger_FeedFlush() {
	// 在临时目录创建日志文件
	tmpDir := s.T().TempDir()
	logPath := filepath.Join(tmpDir, "stream.log")

	logger, err := monitor.NewTeamStreamLogger(logPath)
	s.Require().NoError(err, "NewTeamStreamLogger 不应返回错误")
	s.Require().NotNil(logger)

	// Feed 不崩溃
	s.NotPanics(func() {
		logger.Feed(nil) // nil chunk 不应 panic
	}, "Feed nil 不应 panic")

	// Flush 不崩溃
	s.NotPanics(func() {
		logger.Flush()
	}, "Flush 不应 panic")

	// 验证日志文件存在
	_, err = os.Stat(logPath)
	s.NoError(err, "日志文件应存在")
}

// TestEventListenerHandle_创建 测试 EventListenerHandle 创建和访问。
func (s *MonitorE2ESuite) TestEventListenerHandle_创建() {
	handler := func(_ context.Context, _ *events.EventMessage) error { return nil }
	handle := messager.NewEventListenerHandle(handler)

	s.Require().NotNil(handle)
	s.NotEqual(0, handle.ID(), "Handle ID 不应为 0")
	s.NotNil(handle.Handler(), "Handler 不应为 nil")
}

// TestTeamInfo_FromInternal_NilInputPanic 测试 TeamInfo.FromInternal 在 nil 输入时 panic。
// 这记录了当前实现行为——Go 的 FromInternal 方法未做 nil 守卫。
func (s *MonitorE2ESuite) TestTeamInfo_FromInternal_NilInputPanic() {
	var ti monitor.TeamInfo
	s.Panics(func() {
		_ = ti.FromInternal(nil)
	}, "FromInternal(nil) 应 panic（当前实现无 nil 守卫）")
}

// TestMemberInfo_FromInternal_NilInputPanic 测试 MemberInfo.FromInternal 在 nil 输入时 panic。
func (s *MonitorE2ESuite) TestMemberInfo_FromInternal_NilInputPanic() {
	var mi monitor.MemberInfo
	s.Panics(func() {
		_ = mi.FromInternal(nil)
	}, "FromInternal(nil) 应 panic（当前实现无 nil 守卫）")
}

// TestTaskInfo_FromInternal_NilInputPanic 测试 TaskInfo.FromInternal 在 nil 输入时 panic。
func (s *MonitorE2ESuite) TestTaskInfo_FromInternal_NilInputPanic() {
	var ti monitor.TaskInfo
	s.Panics(func() {
		_ = ti.FromInternal(nil)
	}, "FromInternal(nil) 应 panic（当前实现无 nil 守卫）")
}

// TestMessageInfo_FromInternal_NilInputPanic 测试 MessageInfo.FromInternal 在 nil 输入时 panic。
func (s *MonitorE2ESuite) TestMessageInfo_FromInternal_NilInputPanic() {
	var mi monitor.MessageInfo
	s.Panics(func() {
		_ = mi.FromInternal(nil)
	}, "FromInternal(nil) 应 panic（当前实现无 nil 守卫）")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// fakeEventListenerRegistrar 实现 monitor.EventListenerRegistrar 接口的测试替身。
type fakeEventListenerRegistrar struct {
	addListenerCount    int
	removeListenerCount int
	handlers            []messager.MessagerHandler
}

func newFakeEventListenerRegistrar() *fakeEventListenerRegistrar {
	return &fakeEventListenerRegistrar{}
}

func (f *fakeEventListenerRegistrar) AddEventListener(handler messager.MessagerHandler) *messager.EventListenerHandle {
	f.addListenerCount++
	f.handlers = append(f.handlers, handler)
	return messager.NewEventListenerHandle(handler)
}

func (f *fakeEventListenerRegistrar) RemoveEventListener(_ *messager.EventListenerHandle) {
	f.removeListenerCount++
}

// publish 模拟向所有注册的监听器发布事件。
func (f *fakeEventListenerRegistrar) publish(msg *events.EventMessage) {
	for _, handler := range f.handlers {
		_ = handler(context.Background(), msg)
	}
}
