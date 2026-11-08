package handlers

import (
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestBuildInteractiveInput 验证从 payload 构建交互输入的各分支。
func TestBuildInteractiveInput(t *testing.T) {
	t.Run("缺少 tool_call_id 返回 nil", func(t *testing.T) {
		if got := buildInteractiveInput(map[string]any{}); got != nil {
			t.Fatalf("应返回 nil，实际 %#v", got)
		}
	})

	t.Run("tool_call_id 非字符串返回 nil", func(t *testing.T) {
		if got := buildInteractiveInput(map[string]any{"tool_call_id": 42}); got != nil {
			t.Fatalf("应返回 nil，实际 %#v", got)
		}
	})

	t.Run("完整 payload 构建成功", func(t *testing.T) {
		got := buildInteractiveInput(map[string]any{
			"tool_call_id": "tc-1",
			"approved":     true,
			"feedback":     "ok",
			"auto_confirm": true,
		})
		if got == nil {
			t.Fatal("应返回非 nil 交互输入")
		}
		if got.UserInputs == nil {
			t.Fatal("UserInputs 不应为 nil")
		}
		value, ok := got.UserInputs["tc-1"]
		if !ok {
			t.Fatalf("应含 tc-1 键，实际 %v", got.UserInputs)
		}
		m, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("值应为 map，实际 %T", value)
		}
		if m["approved"] != true || m["feedback"] != "ok" || m["auto_confirm"] != true {
			t.Fatalf("字段不符: %v", m)
		}
	})

	t.Run("仅 tool_call_id 时其余字段取零值", func(t *testing.T) {
		got := buildInteractiveInput(map[string]any{"tool_call_id": "tc-2"})
		if got == nil {
			t.Fatal("应返回非 nil")
		}
		m, _ := got.UserInputs["tc-2"].(map[string]any)
		if m["approved"] != false || m["feedback"] != "" || m["auto_confirm"] != false {
			t.Fatalf("零值不符: %v", m)
		}
	})
}

// TestFormatTeamMessage 验证团队消息格式化的直发/广播与 human-agent 分支。
func TestFormatTeamMessage(t *testing.T) {
	msg := &database.TeamMessageBase{
		MessageID:      "m1",
		FromMemberName: "dev-1",
		Content:        "hello",
	}

	t.Run("直发消息给普通成员", func(t *testing.T) {
		got := formatTeamMessage(msg, false)
		if got == "" {
			t.Fatal("输出不应为空")
		}
		if !strings.Contains(got, "hello") {
			t.Fatalf("应含消息内容，实际 %q", got)
		}
	})

	t.Run("广播消息", func(t *testing.T) {
		broadcast := *msg
		broadcast.Broadcast = true
		got := formatTeamMessage(&broadcast, false)
		if !strings.Contains(got, "hello") {
			t.Fatalf("应含消息内容，实际 %q", got)
		}
	})

	t.Run("human-agent 使用专用模板", func(t *testing.T) {
		got := formatTeamMessage(msg, true)
		if got == "" {
			t.Fatal("输出不应为空")
		}
		// human-agent 分支走不同模板键，输出文本应与普通成员分支不同
		if got == formatTeamMessage(msg, false) {
			t.Fatal("human-agent 应与普通成员使用不同模板")
		}
	})
}
