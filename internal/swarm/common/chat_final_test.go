package common

import (
	"testing"
)

// TestAnnotateChatFinal_补齐append 对齐 Python annotate_chat_final 默认补 final_mode 并补 event_type。
func TestAnnotateChatFinal_补齐append(t *testing.T) {
	out := AnnotateChatFinal(map[string]any{"content": "done"}, FinalModeAppend)
	if out["final_mode"] != FinalModeAppend {
		t.Fatalf("final_mode = %v", out["final_mode"])
	}
	if out["event_type"] != "chat.final" {
		t.Fatalf("event_type = %v", out["event_type"])
	}
	if out["content"] != "done" {
		t.Fatalf("content = %v", out["content"])
	}
}

// TestAnnotateChatFinal_已有显式值不覆盖 对齐 :29-30 既有 final_mode 时不覆盖。
func TestAnnotateChatFinal_已有显式值不覆盖(t *testing.T) {
	out := AnnotateChatFinal(map[string]any{"final_mode": FinalModeReplaceTurn, "event_type": "chat.final"}, FinalModeAppend)
	if out["final_mode"] != FinalModeReplaceTurn {
		t.Fatalf("final_mode 被覆盖为 %v", out["final_mode"])
	}
}

// TestAnnotateChatFinal_非chatfinal原样返回 对齐 :26-27 event_type 不是 chat.final 时原样返回。
func TestAnnotateChatFinal_非chatfinal原样返回(t *testing.T) {
	in := map[string]any{"event_type": "chat.msg", "content": "x"}
	out := AnnotateChatFinal(in, FinalModeAppend)
	if _, ok := out["final_mode"]; ok {
		t.Fatalf("非 chat.final 不应补 final_mode: %v", out)
	}
	if out["event_type"] != "chat.msg" {
		t.Fatalf("event_type 被改: %v", out["event_type"])
	}
}

// TestAnnotateChatFinal_nil输入 对齐 Python dict(payload or {})。
func TestAnnotateChatFinal_nil输入(t *testing.T) {
	out := AnnotateChatFinal(nil, FinalModePatchSegment)
	if out["final_mode"] != FinalModePatchSegment || out["event_type"] != "chat.final" {
		t.Fatalf("nil 输入结果: %v", out)
	}
}

// TestAnnotateChatFinal_空白finalMode视为缺失 对齐 existing.strip() 为空则覆盖。
func TestAnnotateChatFinal_空白finalMode视为缺失(t *testing.T) {
	out := AnnotateChatFinal(map[string]any{"final_mode": "  "}, FinalModeAppend)
	if out["final_mode"] != FinalModeAppend {
		t.Fatalf("空白 final_mode 应被覆盖: %v", out["final_mode"])
	}
}

// TestEnsureFinalModeInplace_缺失时写入 对齐 :36-47。
func TestEnsureFinalModeInplace_缺失时写入(t *testing.T) {
	payload := map[string]any{"event_type": "chat.final"}
	EnsureFinalModeInplace(payload, FinalModeAppend)
	if payload["final_mode"] != FinalModeAppend {
		t.Fatalf("final_mode = %v", payload["final_mode"])
	}
}

// TestEnsureFinalModeInplace_已有显式值不动 对齐 :44-46。
func TestEnsureFinalModeInplace_已有显式值不动(t *testing.T) {
	payload := map[string]any{"event_type": "chat.final", "final_mode": FinalModeReplaceTurn}
	EnsureFinalModeInplace(payload, FinalModeAppend)
	if payload["final_mode"] != FinalModeReplaceTurn {
		t.Fatalf("final_mode 被覆盖: %v", payload["final_mode"])
	}
}

// TestEnsureFinalModeInplace_非chatfinal不动 对齐 :42 event_type 不在 ("", "chat.final") 时直接返回。
func TestEnsureFinalModeInplace_非chatfinal不动(t *testing.T) {
	payload := map[string]any{"event_type": "chat.msg"}
	EnsureFinalModeInplace(payload, FinalModeAppend)
	if _, ok := payload["final_mode"]; ok {
		t.Fatalf("非 chat.final 不应写入: %v", payload)
	}
}
