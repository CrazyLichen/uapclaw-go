package runtime

import "testing"

// TestAnswersPausedCalls 覆盖 5 个 resume source 与 request_id 的组合。
func TestAnswersPausedCalls(t *testing.T) {
	cases := []struct {
		name   string
		params map[string]any
		want   bool
	}{
		{
			name:   "nil 参数",
			params: nil,
			want:   false,
		},
		{
			name:   "非 resume source",
			params: map[string]any{"source": "other", "answers": []any{"a"}, "request_id": "r1"},
			want:   false,
		},
		{
			name:   "resume source 但 answers 为空",
			params: map[string]any{"source": "ask_user_interrupt", "answers": []any{}, "request_id": "r1"},
			want:   false,
		},
		{
			name:   "resume source 但缺 request_id",
			params: map[string]any{"source": "ask_user_interrupt", "answers": []any{"a"}},
			want:   false,
		},
		{
			name:   "ask_user_interrupt 完整",
			params: map[string]any{"source": "ask_user_interrupt", "answers": []any{"a"}, "request_id": "r1"},
			want:   true,
		},
		{
			name:   "permission_interrupt 完整",
			params: map[string]any{"source": "permission_interrupt", "answers": []any{"a"}, "request_id": "r1"},
			want:   true,
		},
		{
			name:   "confirm_interrupt 完整",
			params: map[string]any{"source": "confirm_interrupt", "answers": []any{"a"}, "request_id": "r1"},
			want:   true,
		},
		{
			name:   "evolution_interrupt 完整",
			params: map[string]any{"source": "evolution_interrupt", "answers": []any{"a"}, "request_id": "r1"},
			want:   true,
		},
		{
			name:   "client_tool_interrupt 无 request_id 也通过",
			params: map[string]any{"source": "client_tool_interrupt", "answers": []any{"a"}},
			want:   true,
		},
		{
			name:   "client_tool_interrupt 无 answers",
			params: map[string]any{"source": "client_tool_interrupt"},
			want:   false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := AnswersPausedCalls(tc.params); got != tc.want {
				t.Errorf("AnswersPausedCalls() = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestIsClientToolResume 只认 source 取值。
func TestIsClientToolResume(t *testing.T) {
	if !IsClientToolResume(map[string]any{"source": "client_tool_interrupt"}) {
		t.Error("应识别 client_tool_interrupt")
	}
	if IsClientToolResume(map[string]any{"source": "ask_user_interrupt"}) {
		t.Error("其他 source 不应识别为 client tool resume")
	}
	if IsClientToolResume(nil) {
		t.Error("nil 不应识别")
	}
}

// TestInterruptResumeSources_五个来源 确认常量集合与上游一致。
func TestInterruptResumeSources_五个来源(t *testing.T) {
	want := []string{
		"permission_interrupt",
		"confirm_interrupt",
		"ask_user_interrupt",
		"evolution_interrupt",
		"client_tool_interrupt",
	}
	if len(InterruptResumeSources) != len(want) {
		t.Fatalf("来源数量应为 %d，实际 %d", len(want), len(InterruptResumeSources))
	}
	for _, source := range want {
		if _, ok := InterruptResumeSources[source]; !ok {
			t.Errorf("缺少来源 %q", source)
		}
	}
}
