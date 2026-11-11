package processcli

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/channels/processcli/protocol"
)

// TestValidateQueryResult_不匹配操作报错。
// Python: control_commands.py:58-60
func TestValidateQueryResult_不匹配操作报错(t *testing.T) {
	err := validateQueryResult(map[string]any{
		"operation": "model.list",
		"status":    string(protocol.StatusCompleted),
		"data":      map[string]any{},
	}, "session.list")
	if err == nil {
		t.Error("operation 不匹配应报错")
	}
}

// TestValidateQueryResult_非completed报错。
func TestValidateQueryResult_非completed报错(t *testing.T) {
	err := validateQueryResult(map[string]any{
		"operation": "session.list",
		"status":    "failed",
		"error":     map[string]any{"code": "QUERY_FAILED"},
	}, "session.list")
	if err == nil || !strings.Contains(err.Error(), "QUERY_FAILED") {
		t.Errorf("非 completed 应报错：%v", err)
	}
}

// TestValidateQueryResult_缺data报错。
func TestValidateQueryResult_缺data报错(t *testing.T) {
	if err := validateQueryResult(map[string]any{
		"operation": "session.list",
		"status":    string(protocol.StatusCompleted),
	}, "session.list"); err == nil {
		t.Error("缺 data 应报错")
	}
}

// TestValidateQueryResult_合法通过。
func TestValidateQueryResult_合法通过(t *testing.T) {
	if err := validateQueryResult(map[string]any{
		"operation": "session.list",
		"status":    string(protocol.StatusCompleted),
		"data":      map[string]any{"sessions": []any{}},
	}, "session.list"); err != nil {
		t.Errorf("合法结果报错：%v", err)
	}
}

// TestShowSessions_空列表提示 无搜索/有搜索两种文案。
// Python: control_commands.py:115-119
func TestShowSessions_空列表提示(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 80)
	_, err := ShowSessions(ui, map[string]any{"sessions": []any{}, "total": 0, "offset": 0}, "", "")
	if err != nil {
		t.Fatalf("ShowSessions: %v", err)
	}
	if !strings.Contains(out.String(), "暂无进程式 CLI 会话") {
		t.Errorf("无搜索文案缺失：%q", out.String())
	}

	out.Reset()
	ui = NewProcessCliUI(&out, 80)
	if _, err := ShowSessions(ui, map[string]any{"sessions": []any{}, "total": 0, "offset": 0}, "", "kw"); err != nil {
		t.Fatalf("ShowSessions: %v", err)
	}
	if !strings.Contains(out.String(), "没有匹配的进程式 CLI 会话") {
		t.Errorf("有搜索文案缺失：%q", out.String())
	}
}

// TestShowSessions_返回ID列表 当前会话带 * 标记。
func TestShowSessions_返回ID列表(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 120)
	ids, err := ShowSessions(ui, map[string]any{
		"sessions": []any{
			map[string]any{"session_id": "s1", "title": "T1", "mode": "agent.code.normal"},
			map[string]any{"session_id": "s2", "title": "T2", "mode": "agent.work.normal"},
		},
		"total": 2, "offset": 0,
	}, "s2", "")
	if err != nil {
		t.Fatalf("ShowSessions: %v", err)
	}
	if len(ids) != 2 || ids[0] != "s1" || ids[1] != "s2" {
		t.Errorf("ID 列表 = %v", ids)
	}
	if !strings.Contains(out.String(), "* s2") {
		t.Errorf("当前会话未标记：%q", out.String())
	}
}

// TestShowSessions_格式无效 sessions 不是数组时报错。
func TestShowSessions_格式无效(t *testing.T) {
	ui := NewProcessCliUI(&strings.Builder{}, 80)
	if _, err := ShowSessions(ui, map[string]any{"sessions": "bad"}, "", ""); err == nil {
		t.Error("非法格式应报错")
	}
}

// TestShowModels_当前标记 is_current 与显式 selected 两种标记。
// Python: control_commands.py:131-136
func TestShowModels_当前标记(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 120)
	err := ShowModels(ui, map[string]any{
		"models": []any{
			map[string]any{"selection_key": "k1", "model_name": "m1", "is_current": true},
			map[string]any{"selection_key": "k2", "model_name": "m2", "is_current": false},
		},
	}, "")
	if err != nil {
		t.Fatalf("ShowModels: %v", err)
	}
	if !strings.Contains(out.String(), "* m1 [k1]") {
		t.Errorf("is_current 未标记：%q", out.String())
	}

	out.Reset()
	ui = NewProcessCliUI(&out, 120)
	if err := ShowModels(ui, map[string]any{
		"models": []any{
			map[string]any{"selection_key": "k1", "model_name": "m1", "is_current": true},
			map[string]any{"selection_key": "k2", "model_name": "m2"},
		},
	}, "k2"); err != nil {
		t.Fatalf("ShowModels: %v", err)
	}
	if !strings.Contains(out.String(), "* m2 [k2]") {
		t.Errorf("显式 selected 未标记：%q", out.String())
	}
}

// TestShowStatus_空会话 无会话时只显示下一轮信息。
// Python: control_commands.py:149-176
func TestShowStatus_空会话(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 120)
	if err := ShowStatus(ui, "", nil, "agent.code", "gpt-4", "/tmp"); err != nil {
		t.Fatalf("ShowStatus: %v", err)
	}
	if !strings.Contains(out.String(), "尚未创建") {
		t.Errorf("空会话文案缺失：%q", out.String())
	}
	if !strings.Contains(out.String(), "下一轮模型：gpt-4") {
		t.Errorf("下一轮模型缺失：%q", out.String())
	}
}

// TestShowPermissions_计数与规则 工具等级计数与规则列表。
// Python: control_commands.py:179-215
func TestShowPermissions_计数与规则(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 120)
	err := ShowPermissions(ui, map[string]any{
		"scope": "session",
		"effective": map[string]any{
			"enabled": true,
			"tools": []any{
				map[string]any{"name": "read_file", "level": "allow"},
				map[string]any{"name": "bash", "level": "ask"},
				map[string]any{"name": "rm", "level": "deny"},
			},
			"rules": []any{
				map[string]any{"id": "r1", "action": "deny", "tools": []any{"rm"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("ShowPermissions: %v", err)
	}
	text := out.String()
	for _, want := range []string{"当前会话", "权限开关：启用", "allow 1、ask 1、deny 1", "bash: ask", "rm: deny", "规则配置：1 条"} {
		if !strings.Contains(text, want) {
			t.Errorf("缺少 %q：%q", want, text)
		}
	}
}

// TestShowPermissions_关闭提示 enabled=false 时附快照说明。
func TestShowPermissions_关闭提示(t *testing.T) {
	var out strings.Builder
	ui := NewProcessCliUI(&out, 120)
	err := ShowPermissions(ui, map[string]any{
		"effective": map[string]any{"enabled": false},
	})
	if err != nil {
		t.Fatalf("ShowPermissions: %v", err)
	}
	if !strings.Contains(out.String(), "当前权限检查已关闭") {
		t.Errorf("关闭提示缺失：%q", out.String())
	}
}
