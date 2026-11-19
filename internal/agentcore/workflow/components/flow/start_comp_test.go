package flow

import (
	"context"
	"reflect"
	"testing"

	ability "github.com/uapclaw/uapclaw-go/internal/agentcore/workflow/components/ability"
)

// TestStart_Invoke_透传 验证 Start 原样返回 inputs（start_comp.py:14-15）。
func TestStart_Invoke_透传(t *testing.T) {
	s := NewStart()
	inner := newFakeVertexSession(t, "n1", "Start")
	in := map[string]any{"q": "hi"}
	out, err := s.OnInvoke(context.Background(), in, inner, nil)
	if err != nil || !reflect.DeepEqual(out, in) {
		t.Fatalf("透传失败: out=%v err=%v", out, err)
	}
}

// TestStart_其余钩子默认未实现 验证 stream/collect/transform 走基类默认错误。
func TestStart_其余钩子默认未实现(t *testing.T) {
	s := NewStart()
	inner := newFakeVertexSession(t, "n1", "Start")
	if _, err := s.OnStream(context.Background(), nil, inner, nil); err == nil {
		t.Fatal("OnStream 期望默认未实现错误")
	}
	in := make(chan map[string]any)
	defer close(in)
	if _, err := s.OnCollect(context.Background(), in, inner, nil); err == nil {
		t.Fatal("OnCollect 期望默认未实现错误")
	}
	if _, err := s.OnTransform(context.Background(), in, inner, nil); err == nil {
		t.Fatal("OnTransform 期望默认未实现错误")
	}
}

// 编译期确认 ability 别名锚点（components 包桥接面可用性）
var _ = ability.ComponentAbilityInvoke
