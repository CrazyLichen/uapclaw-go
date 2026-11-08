package adapter

import (
	"context"
	"testing"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestParseApprovalAnswersFromAny 验证各类输入到 ApprovalAnswer 的解析。
func TestParseApprovalAnswersFromAny(t *testing.T) {
	t.Run("nil 返回 nil", func(t *testing.T) {
		if got := parseApprovalAnswersFromAny(nil); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("非切片类型返回 nil", func(t *testing.T) {
		if got := parseApprovalAnswersFromAny("string-input"); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("解析 selected_options 字符串数组", func(t *testing.T) {
		got := parseApprovalAnswersFromAny([]any{
			map[string]any{"selected_options": []any{"accept", 42, "接收"}},
		})
		if len(got) != 1 {
			t.Fatalf("应解析出 1 条，实际 %v", got)
		}
		// 非字符串选项被跳过
		if len(got[0].SelectedOptions) != 2 {
			t.Fatalf("应保留 2 个字符串选项，实际 %v", got[0].SelectedOptions)
		}
	})

	t.Run("缺少 selected_options 时为空选项", func(t *testing.T) {
		got := parseApprovalAnswersFromAny([]any{map[string]any{}})
		if len(got) != 1 || len(got[0].SelectedOptions) != 0 {
			t.Fatalf("应为 1 条空选项，实际 %v", got)
		}
	})

	t.Run("非 map 元素被跳过", func(t *testing.T) {
		got := parseApprovalAnswersFromAny([]any{"not-a-map", 42})
		if len(got) != 0 {
			t.Fatalf("应跳过所有非 map 元素，实际 %v", got)
		}
	})

	t.Run("selected_options 非数组时为空选项", func(t *testing.T) {
		got := parseApprovalAnswersFromAny([]any{
			map[string]any{"selected_options": "accept"},
		})
		if len(got) != 1 || len(got[0].SelectedOptions) != 0 {
			t.Fatalf("非数组应得空选项，实际 %v", got)
		}
	})
}

// TestHandleTeamSkillEvolveApproval_无rail 验证未注册 rail 时返回 false。
func TestHandleTeamSkillEvolveApproval_无rail(t *testing.T) {
	d := NewDeepAdapter()
	got := d.handleTeamSkillEvolveApproval(
		context.Background(),
		"req-not-registered-xyz",
		[]any{map[string]any{"selected_options": []any{"accept"}}},
		"sess-1", "chan-unknown-xyz",
	)
	if got {
		t.Fatal("rail 未注册时应返回 false")
	}
}

// TestPushTeamSkillEvolveResolutionStatus_空session 验证 sessionID 为空时直接返回。
func TestPushTeamSkillEvolveResolutionStatus_空session(t *testing.T) {
	d := NewDeepAdapter()
	for _, accepted := range []bool{true, false} {
		if err := d.pushTeamSkillEvolveResolutionStatus(context.Background(), "req-1", "", "chan-1", accepted); err != nil {
			t.Fatalf("sessionID 为空应返回 nil，实际 %v", err)
		}
	}
}
