//go:build integration

package task_loop

import (
	"encoding/json"
	"sort"
	"testing"

	"github.com/stretchr/testify/suite"

	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	isuite "github.com/uapclaw/uapclaw-go/tests/integration/suite"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LoopEventSuite 深度循环事件 schema 集成测试。
// 对照 Python: tests/unit_tests/harness/test_loop_event_schema.py
type LoopEventSuite struct {
	isuite.BaseIntegrationSuite
}

// ──────────────────────────── 导出函数 ────────────────────────────

// TestCreateLoopEvent_基本创建 验证 CreateLoopEvent 返回的事件基本字段非空
func (s *LoopEventSuite) TestCreateLoopEvent_基本创建() {
	evt := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeFollowup, "hello")

	s.Equal(1, evt.Seq)
	s.Equal(hschema.DeepLoopEventTypeFollowup, evt.EventType)
	s.Equal("hello", evt.Content)
	s.NotZero(evt.Priority)
	s.NotEmpty(evt.EventID)
	s.NotZero(evt.CreatedAt)
}

// TestCreateLoopEvent_Followup 验证 Followup 事件默认优先级为 10
func (s *LoopEventSuite) TestCreateLoopEvent_Followup() {
	evt := hschema.CreateLoopEvent(0, hschema.DeepLoopEventTypeFollowup, "")
	s.Equal(10, evt.Priority)
}

// TestCreateLoopEvent_Steer 验证 Steer 事件默认优先级为 1
func (s *LoopEventSuite) TestCreateLoopEvent_Steer() {
	evt := hschema.CreateLoopEvent(0, hschema.DeepLoopEventTypeSteer, "")
	s.Equal(1, evt.Priority)
}

// TestCreateLoopEvent_Abort 验证 Abort 事件默认优先级为 0
func (s *LoopEventSuite) TestCreateLoopEvent_Abort() {
	evt := hschema.CreateLoopEvent(0, hschema.DeepLoopEventTypeAbort, "")
	s.Equal(0, evt.Priority)
}

// TestCreateLoopEvent_WithTaskID 验证 WithTaskID 选项
func (s *LoopEventSuite) TestCreateLoopEvent_WithTaskID() {
	evt := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeFollowup, "msg",
		hschema.WithTaskID("task-123"),
	)
	s.Equal("task-123", evt.TaskID)
}

// TestCreateLoopEvent_WithMetadata 验证 WithMetadata 选项
func (s *LoopEventSuite) TestCreateLoopEvent_WithMetadata() {
	meta := map[string]any{"key": "value"}
	evt := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeFollowup, "msg",
		hschema.WithMetadata(meta),
	)
	s.Equal(meta, evt.Metadata)
}

// TestCreateLoopEvent_WithPriority 验证 WithPriority 选项覆盖默认优先级
func (s *LoopEventSuite) TestCreateLoopEvent_WithPriority() {
	evt := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeFollowup, "msg",
		hschema.WithPriority(42),
	)
	s.Equal(42, evt.Priority)
}

// TestDeepLoopEventType_String 验证 String() 方法返回正确的字符串
func (s *LoopEventSuite) TestDeepLoopEventType_String() {
	s.Equal("followup", hschema.DeepLoopEventTypeFollowup.String())
	s.Equal("steer", hschema.DeepLoopEventTypeSteer.String())
	s.Equal("abort", hschema.DeepLoopEventTypeAbort.String())
}

// TestParseDeepLoopEventType 验证从字符串解析 DeepLoopEventType
func (s *LoopEventSuite) TestParseDeepLoopEventType() {
	t, err := hschema.ParseDeepLoopEventType("followup")
	s.NoError(err)
	s.Equal(hschema.DeepLoopEventTypeFollowup, t)

	t, err = hschema.ParseDeepLoopEventType("steer")
	s.NoError(err)
	s.Equal(hschema.DeepLoopEventTypeSteer, t)

	t, err = hschema.ParseDeepLoopEventType("abort")
	s.NoError(err)
	s.Equal(hschema.DeepLoopEventTypeAbort, t)

	// 大小写不敏感
	t, err = hschema.ParseDeepLoopEventType("FOLLOWUP")
	s.NoError(err)
	s.Equal(hschema.DeepLoopEventTypeFollowup, t)

	// 未知类型
	_, err = hschema.ParseDeepLoopEventType("unknown")
	s.Error(err)
}

// TestDeepLoopEvent_Less 验证优先级排序：abort(0) < steer(1) < followup(10)，同优先级按 Seq
func (s *LoopEventSuite) TestDeepLoopEvent_Less() {
	abort := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeAbort, "")
	steer := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeSteer, "")
	followup := hschema.CreateLoopEvent(1, hschema.DeepLoopEventTypeFollowup, "")

	// abort 优先于 steer，steer 优先于 followup
	s.True(abort.Less(steer))
	s.True(steer.Less(followup))
	s.True(abort.Less(followup))
	s.False(followup.Less(abort))

	// 同优先级按 Seq
	early := hschema.DeepLoopEvent{Priority: 1, Seq: 1}
	late := hschema.DeepLoopEvent{Priority: 1, Seq: 5}
	s.True(early.Less(late))
	s.False(late.Less(early))
}

// TestDeepLoopEventType_MarshalJSON 验证 JSON 序列化
func (s *LoopEventSuite) TestDeepLoopEventType_MarshalJSON() {
	data, err := json.Marshal(hschema.DeepLoopEventTypeSteer)
	s.NoError(err)
	s.Equal(`"steer"`, string(data))
}

// TestDeepLoopEventType_UnmarshalJSON 验证 JSON 反序列化
func (s *LoopEventSuite) TestDeepLoopEventType_UnmarshalJSON() {
	var t hschema.DeepLoopEventType
	err := json.Unmarshal([]byte(`"abort"`), &t)
	s.NoError(err)
	s.Equal(hschema.DeepLoopEventTypeAbort, t)

	// 无效值
	err = json.Unmarshal([]byte(`"invalid"`), &t)
	s.Error(err)
}

// TestDefaultEventPriority 验证默认优先级映射
func (s *LoopEventSuite) TestDefaultEventPriority() {
	s.Equal(0, hschema.DefaultEventPriority(hschema.DeepLoopEventTypeAbort))
	s.Equal(1, hschema.DefaultEventPriority(hschema.DeepLoopEventTypeSteer))
	s.Equal(10, hschema.DefaultEventPriority(hschema.DeepLoopEventTypeFollowup))
	// 未知类型默认返回 10
	s.Equal(10, hschema.DefaultEventPriority(hschema.DeepLoopEventType(99)))
}

// TestDeepLoopEvent_回调事件类型间接验证
// 验证 DeepLoopEvent 对应的回调事件存在于 AgentCallbackEvent 枚举中
func (s *LoopEventSuite) TestDeepLoopEvent_回调事件类型间接验证() {
	// abort 类事件在 after_invoke 回调中处理
	s.NotEmpty(agentinterfaces.CallbackAfterInvoke)
	s.NotEmpty(agentinterfaces.CallbackBeforeToolCall)
	s.NotEmpty(agentinterfaces.CallbackAfterToolCall)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestLoopEventSuite 运行 LoopEventSuite
func TestLoopEventSuite(t *testing.T) {
	suite.Run(t, new(LoopEventSuite))
}

// sortByPriority 按 Less 规则排序事件切片
func sortByPriority(events []hschema.DeepLoopEvent) {
	sort.Slice(events, func(i, j int) bool {
		return events[i].Less(events[j])
	})
}
