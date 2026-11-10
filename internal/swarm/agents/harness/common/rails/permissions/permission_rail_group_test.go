package permissions

import (
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// stubPermissionRail 权限轨替身（实现标识接口）。
type stubPermissionRail struct {
	agentinterfaces.BaseRail
	// auto 是否为自动权限轨
	auto bool
}

// PermissionRailMarker 实现标识
func (s *stubPermissionRail) PermissionRailMarker() {}

// stubAutoPermissionRail 自动权限轨替身。
type stubAutoPermissionRail struct {
	agentinterfaces.BaseRail
}

// PermissionRailMarker 实现标识
func (s *stubAutoPermissionRail) PermissionRailMarker() {}

// AutoPermissionRailMarker 实现自动标识
func (s *stubAutoPermissionRail) AutoPermissionRailMarker() {}

// stubStreamRail 流事件轨替身。
type stubStreamRail struct {
	agentinterfaces.BaseRail
}

// stubAskUserRail 结构化询问轨替身（与 stream rail 不同类型，对齐 Python）。
type stubAskUserRail struct {
	agentinterfaces.BaseRail
}

// newGroupFixture 构造装配自检用的齐备组。
func newGroupFixture(t *testing.T) (*PermissionRailGroup, *RootPermissionQueue, *stubStreamRail) {
	t.Helper()
	queue := NewRootPermissionQueue(nil)
	ctxRail, err := NewRootContextRail(queue, nil, nil, nil)
	if err != nil {
		t.Fatalf("RootContextRail: %v", err)
	}
	stream := &stubStreamRail{}
	ask := &stubAskUserRail{}
	group := NewPermissionRailGroup(PermissionRailGroupOptions{
		PermissionRail:  &stubAutoPermissionRail{},
		Queue:           queue,
		RootContextRail: ctxRail,
		StreamEventRail: &JiuClawStreamEventRailBridge{
			Rail:                stream,
			RootPermissionQueue: queue,
		},
		AskUserRail: &StructuredAskUserRailBridge{
			Rail:                       ask,
			StrictContinuationContract: true,
		},
	})
	return group, queue, stream
}

// TestNewPermissionRailGroup_共享同一队列 验证三个 rail 用的是同一个队列实例。
func TestNewPermissionRailGroup_共享同一队列(t *testing.T) {
	group, queue, _ := newGroupFixture(t)
	if group.RootPermissionQueueRail == nil || group.RootPermissionCompletionRail == nil {
		t.Fatal("both rails should be created")
	}
	if group.RootPermissionQueueRail.queue != queue {
		t.Error("queue rail should share the queue")
	}
	if group.RootPermissionCompletionRail.queue != queue {
		t.Error("completion rail should share the queue")
	}
	if group.RootContextRail.rootPermissionQueue != queue {
		t.Error("context rail should share the queue")
	}
}

// TestRails_过滤nil 验证只返回非 nil 项。
func TestRails_过滤nil(t *testing.T) {
	group, _, _ := newGroupFixture(t)
	rails := group.Rails()
	if len(rails) != 6 {
		t.Errorf("got %d rails, want 6", len(rails))
	}
	// 空组
	empty := &PermissionRailGroup{}
	if got := empty.Rails(); len(got) != 0 {
		t.Errorf("empty group -> %d rails", len(got))
	}
}

// TestValidateComposition_smart齐备 验证 smart 模式下校验通过。
func TestValidateComposition_smart齐备(t *testing.T) {
	group, _, _ := newGroupFixture(t)
	if err := group.ValidateComposition(group.Rails(), true, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateComposition_smart缺rail 验证缺 root rail 时报错。
func TestValidateComposition_smart缺rail(t *testing.T) {
	group, _, _ := newGroupFixture(t)
	rails := group.Rails()
	// 去掉 RootContextRail
	trimmed := make([]agentinterfaces.AgentRail, 0, len(rails))
	for _, rail := range rails {
		if rail == group.RootContextRail {
			continue
		}
		trimmed = append(trimmed, rail)
	}
	if err := group.ValidateComposition(trimmed, true, nil); err == nil {
		t.Error("missing root_context_rail should fail")
	}
}

// TestValidateComposition_smart非自动权限轨 验证类型不符时报错。
func TestValidateComposition_smart非自动权限轨(t *testing.T) {
	group, _, _ := newGroupFixture(t)
	group.PermissionRail = &stubPermissionRail{} // 非自动轨
	if err := group.ValidateComposition(group.Rails(), true, nil); err == nil {
		t.Error("non-auto permission rail in smart mode should fail")
	}
}

// TestValidateComposition_非smart无权限轨 验证非 smart 模式允许无权限轨。
func TestValidateComposition_非smart无权限轨(t *testing.T) {
	group := &PermissionRailGroup{}
	if err := group.ValidateComposition(nil, false, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidateComposition_非smart有权限轨 验证非 smart 模式允许普通权限轨。
func TestValidateComposition_非smart有权限轨(t *testing.T) {
	rail := &stubPermissionRail{}
	group := &PermissionRailGroup{PermissionRail: rail}
	if err := group.ValidateComposition([]agentinterfaces.AgentRail{rail}, false, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestVerify_齐备通过 验证 verify 全条件通过。
func TestVerify_齐备通过(t *testing.T) {
	group, queue, _ := newGroupFixture(t)
	if err := group.Verify(group.Rails(), true, queue, nil); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestVerify_图身份不符 验证注册的不是本组实例时失败。
func TestVerify_图身份不符(t *testing.T) {
	group, queue, _ := newGroupFixture(t)
	// 换一个同类型但不同实例的 rail
	other, _ := NewRootContextRail(queue, nil, nil, nil)
	rails := group.Rails()
	for index, rail := range rails {
		if rail == group.RootContextRail {
			rails[index] = other
		}
	}
	if err := group.Verify(rails, true, queue, nil); err == nil {
		t.Error("graph identity mismatch should fail")
	}
}

// TestVerify_队列不一致 验证 stream rail 队列不匹配时失败。
func TestVerify_队列不一致(t *testing.T) {
	group, queue, _ := newGroupFixture(t)
	group.StreamEventRail.RootPermissionQueue = NewRootPermissionQueue(nil)
	if err := group.Verify(group.Rails(), true, queue, nil); err == nil {
		t.Error("stream rail queue mismatch should fail")
	}
}

// TestVerify_ask契约不匹配 验证 ask rail 契约标记不符时失败。
func TestVerify_ask契约不匹配(t *testing.T) {
	group, queue, _ := newGroupFixture(t)
	group.AskUserRail.StrictContinuationContract = false
	if err := group.Verify(group.Rails(), true, queue, nil); err == nil {
		t.Error("ask rail contract mismatch should fail")
	}
}

// TestVerify_数量不符 验证 rail 数量不同时失败。
func TestVerify_数量不符(t *testing.T) {
	group, queue, _ := newGroupFixture(t)
	if err := group.Verify(group.Rails()[:3], true, queue, nil); err == nil {
		t.Error("rail count mismatch should fail")
	}
}

// TestRootPermissionQueue_标记接口 验证队列实现窄接口。
func TestRootPermissionQueue_标记接口(t *testing.T) {
	queue := NewRootPermissionQueue(nil)
	var accessor interface{ IsRootPermissionQueue() } = queue
	if accessor == nil {
		t.Error("queue should implement the marker")
	}
}
