package team

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/monitor"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// intp 返回整型指针。
func intp(v int) *int { return &v }

// boolPtr 返回布尔指针。
func boolPtr(v bool) *bool { return &v }

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestPtrStrVal 验证指针取值的 nil 安全。
func TestPtrStrVal(t *testing.T) {
	if got := ptrStrVal(nil); got != "" {
		t.Fatalf("nil 应返回空串，实际 %q", got)
	}
	if got := ptrStrVal(strPtr("x")); got != "x" {
		t.Fatalf("应返回 x，实际 %q", got)
	}
}

// TestCustomHeadersToStringMap 验证自定义头解析的各输入形态。
func TestCustomHeadersToStringMap(t *testing.T) {
	t.Run("nil 返回 nil", func(t *testing.T) {
		if got := customHeadersToStringMap(nil); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("map 提取字符串值", func(t *testing.T) {
		got := customHeadersToStringMap(map[string]any{"a": "1", "b": 2})
		if len(got) != 1 || got["a"] != "1" {
			t.Fatalf("应只保留字符串值，实际 %v", got)
		}
	})

	t.Run("map 全为非字符串时返回 nil", func(t *testing.T) {
		if got := customHeadersToStringMap(map[string]any{"b": 2}); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("空串返回 nil", func(t *testing.T) {
		if got := customHeadersToStringMap(""); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("非 map 非字符串返回 nil", func(t *testing.T) {
		if got := customHeadersToStringMap(42); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})
}

// TestMonitorHandler_MemberEvents 验证成员类事件填充到 base。
func TestMonitorHandler_MemberEvents(t *testing.T) {
	h := &teamMonitorHandlerImpl{}

	t.Run("执行状态变更", func(t *testing.T) {
		base := map[string]any{}
		h.handleMemberExecutionChanged(base, &monitor.MonitorEvent{
			MemberName: strPtr("dev-1"),
			OldStatus:  strPtr("idle"),
			NewStatus:  strPtr("running"),
		})
		if base["member_id"] != "dev-1" || base["old_status"] != "idle" || base["new_status"] != "running" {
			t.Fatalf("base = %v", base)
		}
	})

	t.Run("成员重启", func(t *testing.T) {
		base := map[string]any{}
		h.handleMemberRestarted(base, &monitor.MonitorEvent{
			MemberName:   strPtr("dev-1"),
			Reason:       strPtr("crash"),
			RestartCount: intp(2),
		})
		if base["reason"] != "crash" || base["restart_count"] != 2 {
			t.Fatalf("base = %v", base)
		}
	})

	t.Run("成员关闭", func(t *testing.T) {
		base := map[string]any{}
		h.handleMemberShutdown(base, &monitor.MonitorEvent{
			MemberName: strPtr("dev-1"),
			Force:      boolPtr(true),
		})
		if base["force"] != true {
			t.Fatalf("base = %v", base)
		}
	})

	t.Run("成员名为 nil 时不写 member_id", func(t *testing.T) {
		base := map[string]any{}
		h.handleMemberShutdown(base, &monitor.MonitorEvent{Force: boolPtr(false)})
		if _, ok := base["member_id"]; ok {
			t.Fatalf("不应写 member_id，实际 %v", base)
		}
	})
}

// TestMonitorHandler_TaskEvents 验证任务类事件填充到 base。
func TestMonitorHandler_TaskEvents(t *testing.T) {
	h := &teamMonitorHandlerImpl{}

	t.Run("任务创建带状态", func(t *testing.T) {
		base := map[string]any{}
		h.handleTaskCreated(base, &monitor.MonitorEvent{TaskID: strPtr("t1"), Status: strPtr("pending")})
		if base["task_id"] != "t1" || base["status"] != "pending" {
			t.Fatalf("base = %v", base)
		}
	})

	for name, fn := range map[string]func(map[string]any, *monitor.MonitorEvent){
		"认领":  h.handleTaskClaimed,
		"完成":  h.handleTaskCompleted,
		"取消":  h.handleTaskCancelled,
		"解阻塞": h.handleTaskUnblocked,
	} {
		t.Run("任务"+name+"只写 task_id", func(t *testing.T) {
			base := map[string]any{}
			fn(base, &monitor.MonitorEvent{TaskID: strPtr("t2")})
			if base["task_id"] != "t2" {
				t.Fatalf("base = %v", base)
			}
			if _, ok := base["status"]; ok {
				t.Fatalf("不应写 status，实际 %v", base)
			}
		})
	}
}
