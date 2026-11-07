package adapter

import (
	"context"
	"strings"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 非导出函数测试 ────────────────────────────

// TestOutputSchemaToDict 验证事件结构转字典的三条路径。
func TestOutputSchemaToDict(t *testing.T) {
	t.Run("nil 返回 nil", func(t *testing.T) {
		if got := outputSchemaToDict(nil); got != nil {
			t.Fatalf("应返回 nil，实际 %v", got)
		}
	})

	t.Run("Payload 为 map 时原样返回", func(t *testing.T) {
		payload := map[string]any{"kind": "approval"}
		got := outputSchemaToDict(&stream.OutputSchema{Payload: payload})
		if got["kind"] != "approval" {
			t.Fatalf("应返回原 payload，实际 %v", got)
		}
	})

	t.Run("Payload 非 map 时回退为包装结构", func(t *testing.T) {
		got := outputSchemaToDict(&stream.OutputSchema{Type: "x", Index: 3, Payload: "raw"})
		if got["type"] != "x" {
			t.Fatalf("应含 type，实际 %v", got)
		}
		if got["index"] != 3 {
			t.Fatalf("应含 index，实际 %v", got)
		}
		if got["payload"] != "raw" {
			t.Fatalf("应含原始 payload，实际 %v", got)
		}
	})
}

// TestEventFromDict 验证 map 事件被包装为 OutputSchema。
func TestEventFromDict(t *testing.T) {
	dict := map[string]any{"kind": "approval"}
	got := eventFromDict(dict)
	if got == nil {
		t.Fatal("应返回非 nil")
	}
	if got.Payload == nil {
		t.Fatal("Payload 应被赋值")
	}
	if m, ok := got.Payload.(map[string]any); !ok || m["kind"] != "approval" {
		t.Fatalf("Payload 应为原 map，实际 %v", got.Payload)
	}
}

// TestBuildRecapPrompt 验证 recap 提示词拼接（有/无 memory 前缀）。
func TestBuildRecapPrompt(t *testing.T) {
	t.Run("无 memory 时不带前缀块", func(t *testing.T) {
		got := buildRecapPrompt("")
		if strings.Contains(got, "Session memory") {
			t.Fatalf("空 memory 不应带前缀块，实际 %q", got)
		}
		if !strings.Contains(got, "1-3 short sentences") {
			t.Fatalf("应含输出约束，实际 %q", got)
		}
	})

	t.Run("有 memory 时前置 context 块", func(t *testing.T) {
		got := buildRecapPrompt("一些背景")
		if !strings.HasPrefix(got, "Session memory (broader context):\n一些背景\n\n") {
			t.Fatalf("应前置 memory 块，实际 %q", got)
		}
	})
}

// TestParseApprovalAnswers 验证审批答案解析（中英文通过词、未选中）。
func TestParseApprovalAnswers(t *testing.T) {
	tests := []struct {
		name    string
		answers []ApprovalAnswer
		want    bool
	}{
		{"空列表为拒绝", nil, false},
		{"中文接收", []ApprovalAnswer{{SelectedOptions: []string{"接收"}}}, true},
		{"英文 Accept", []ApprovalAnswer{{SelectedOptions: []string{"Accept"}}}, true},
		{"approve", []ApprovalAnswer{{SelectedOptions: []string{"approve"}}}, true},
		{"执行", []ApprovalAnswer{{SelectedOptions: []string{"执行"}}}, true},
		{"拒绝词不算通过", []ApprovalAnswer{{SelectedOptions: []string{"拒绝"}}}, false},
		{"多答案任一通过即通过", []ApprovalAnswer{
			{SelectedOptions: []string{"拒绝"}},
			{SelectedOptions: []string{"接收"}},
		}, true},
		{"无选中项为拒绝", []ApprovalAnswer{{SelectedOptions: nil}}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseApprovalAnswers(tt.answers); got != tt.want {
				t.Fatalf("parseApprovalAnswers(%v) = %v, 期望 %v", tt.answers, got, tt.want)
			}
		})
	}
}

// TestHandleEvolutionApproval_未初始化Rail 验证 rail 缺失时直接返回 false。
func TestHandleEvolutionApproval_未初始化Rail(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.handleEvolutionApproval(context.Background(), "req-1", nil); got {
		t.Fatal("rail 未初始化时应返回 false")
	}
}

// TestGetRecentMessages_无实例 验证实例缺失时返回 nil。
func TestGetRecentMessages_无实例(t *testing.T) {
	d := NewDeepAdapter()
	if got := d.getRecentMessages("session-x"); got != nil {
		t.Fatalf("无实例时应返回 nil，实际 %v", got)
	}
}

// TestPushEventToFrontend 验证推送分派的各分支不 panic 且对 nil 事件短路。
func TestPushEventToFrontend(t *testing.T) {
	d := NewDeepAdapter()
	ctx := context.Background()

	t.Run("nil 事件直接返回", func(t *testing.T) {
		// outputSchemaToDict(nil) 为 nil → 提前返回，不应 panic
		d.pushEventToFrontend(ctx, nil, "s1", "c1", "r1")
	})

	t.Run("未注入推送函数时仅告警", func(t *testing.T) {
		evt := &stream.OutputSchema{Payload: map[string]any{"kind": "outcome"}}
		d.pushEventToFrontend(ctx, evt, "s1", "c1", "r1")
	})

	t.Run("未知事件类型走默认分支", func(t *testing.T) {
		evt := &stream.OutputSchema{Payload: map[string]any{"kind": "progress"}}
		d.pushEventToFrontend(ctx, evt, "s1", "c1", "r1")
	})
}

// TestOnEvolutionWatcherDone 验证观察任务完成回调可安全调用。
func TestOnEvolutionWatcherDone(t *testing.T) {
	d := NewDeepAdapter()
	d.onEvolutionWatcherDone("session-x")
}
