package observability

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema/events"
)

func newTestMonitorHandler() (*OtelTeamMonitorHandler, *sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	tracer := tp.Tracer("openjiuwen.agent_teams.observability.monitor")
	cfg := &ObservabilityConfig{AttributeValueMaxLength: 1000}
	handler := NewOtelTeamMonitorHandler(cfg, tracer)
	return handler, tp, recorder
}

func TestOtelTeamMonitorHandler_TeamCreated_Cleaned(t *testing.T) {
	handler, tp, recorder := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// team_created
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventCreated,
		Payload:   map[string]any{"team_name": "my-team", "display_name": "My Team"},
	})
	assert.Contains(t, handler.teamSpans, "my-team")

	// team_cleaned
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventCleaned,
		Payload:   map[string]any{"team_name": "my-team"},
	})
	assert.NotContains(t, handler.teamSpans, "my-team")

	spans := recorder.Ended()
	require.Len(t, spans, 1)
	assert.Contains(t, spans[0].Name(), "team.my-team")
}

func TestOtelTeamMonitorHandler_TaskCreated_Completed(t *testing.T) {
	handler, tp, _ := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// 先创建 team span
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventCreated,
		Payload:   map[string]any{"team_name": "my-team"},
	})

	// task_created
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventTaskCreated,
		Payload:   map[string]any{"team_name": "my-team", "task_id": "task-1", "status": "pending"},
	})
	assert.Contains(t, handler.taskSpans, "task-1")

	// task_completed
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventTaskCompleted,
		Payload:   map[string]any{"task_id": "task-1"},
	})
	assert.NotContains(t, handler.taskSpans, "task-1")
}

func TestOtelTeamMonitorHandler_Task中间事件(t *testing.T) {
	handler, tp, _ := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// 先创建 team span
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventCreated,
		Payload:   map[string]any{"team_name": "my-team"},
	})

	// task_created
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventTaskCreated,
		Payload:   map[string]any{"team_name": "my-team", "task_id": "task-2", "status": "pending"},
	})

	// task_updated 中间事件（recordTaskEvent）
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventTaskUpdated,
		Payload:   map[string]any{"task_id": "task-2", "status": "running"},
	})
}

func TestOtelTeamMonitorHandler_未知事件静默忽略(t *testing.T) {
	handler, tp, _ := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// 不应 panic
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: "unknown_event",
		Payload:   map[string]any{},
	})
}

func TestOtelTeamMonitorHandler_TaskCreated_无taskID(t *testing.T) {
	handler, tp, _ := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// task_id 为空，不应创建 span
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventTaskCreated,
		Payload:   map[string]any{"team_name": "my-team"},
	})
	assert.Empty(t, handler.taskSpans)
}

func TestOtelTeamMonitorHandler_Member事件(t *testing.T) {
	handler, tp, _ := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// 先创建 team span
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventCreated,
		Payload:   map[string]any{"team_name": "my-team"},
	})

	// member 事件不应 panic
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventMemberSpawned,
		Payload:   map[string]any{
			"team_name":   "my-team",
			"member_name": "worker-1",
			"old_status":  "idle",
			"new_status":  "active",
		},
	})
}

func TestOtelTeamMonitorHandler_Message事件(t *testing.T) {
	handler, tp, _ := newTestMonitorHandler()
	defer tp.Shutdown(context.Background())

	// 先创建 team span
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventCreated,
		Payload:   map[string]any{"team_name": "my-team"},
	})

	// message 事件不应 panic
	handler.HandleEvent(context.Background(), &events.EventMessage{
		EventType: events.TeamEventMessage,
		Payload: map[string]any{
			"team_name":  "my-team",
			"message_id": "msg-1",
			"from":       "leader",
			"to":         "worker-1",
		},
	})
}

func TestToInt(t *testing.T) {
	assert.Equal(t, 42, toInt(42))
	assert.Equal(t, 42, toInt(float64(42)))
	assert.Equal(t, 42, toInt(int64(42)))
	assert.Equal(t, 0, toInt(nil))
	assert.Equal(t, 0, toInt("42")) // 字符串不支持
}

func TestToBool(t *testing.T) {
	assert.True(t, toBool(true))
	assert.False(t, toBool(false))
	assert.False(t, toBool(nil))
	assert.False(t, toBool("true"))
}

func TestMapToAttributes(t *testing.T) {
	attrs := mapToAttributes(map[string]any{
		"key1": "value1",
		"key2": 42,
		"key3": true,
	})
	assert.Len(t, attrs, 3)
}
