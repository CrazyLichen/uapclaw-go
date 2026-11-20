package sessionctx

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestInitSessionState 测试创建 SessionState
func TestInitSessionState(t *testing.T) {
	s := InitSessionState()
	assert.NotNil(t, s)
	assert.Equal(t, "", s.GetSessionID())
}

// TestWithSessionState 测试注入和获取 SessionState
func TestWithSessionState(t *testing.T) {
	s := InitSessionState()
	s.SetSessionID("sess-123")
	ctx := WithSessionState(context.Background(), s)
	got := SessionStateFromCtx(ctx)
	assert.Equal(t, s, got)
}

// TestSessionStateFromCtx_无状态 测试未绑定 SessionState 的 context
func TestSessionStateFromCtx_无状态(t *testing.T) {
	got := SessionStateFromCtx(context.Background())
	assert.Nil(t, got)
}

// TestGetSessionID_从Context 测试从 context 获取 session ID
func TestGetSessionID_从Context(t *testing.T) {
	s := InitSessionState()
	s.SetSessionID("abc")
	ctx := WithSessionState(context.Background(), s)
	assert.Equal(t, "abc", GetSessionID(ctx))
}

// TestGetSessionID_空Context 测试未绑定 context 返回空字符串
func TestGetSessionID_空Context(t *testing.T) {
	assert.Equal(t, "", GetSessionID(context.Background()))
}

// TestSetSessionID_并发安全 测试并发读写不 panic
func TestSetSessionID_并发安全(t *testing.T) {
	s := InitSessionState()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = s.GetSessionID()
		}()
		go func() {
			defer wg.Done()
			s.SetSessionID("concurrent-id")
		}()
	}
	wg.Wait()
}

// TestSetSessionID_清空 测试清空 sessionID
func TestSetSessionID_清空(t *testing.T) {
	s := InitSessionState()
	s.SetSessionID("sess-123")
	s.SetSessionID("")
	assert.Equal(t, "", s.GetSessionID())
}

// Test子Agent隔离 测试子 Agent 创建新 SessionState 不影响父
func Test子Agent隔离(t *testing.T) {
	parentState := InitSessionState()
	parentState.SetSessionID("parent-sess")
	parentCtx := WithSessionState(context.Background(), parentState)

	subState := InitSessionState()
	subState.SetSessionID("sub-sess")
	subCtx := WithSessionState(parentCtx, subState)

	assert.Equal(t, "parent-sess", GetSessionID(parentCtx))
	assert.Equal(t, "sub-sess", GetSessionID(subCtx))
}

// Test父改子可见 测试同一指针修改后同 ctx 可见
func Test父改子可见(t *testing.T) {
	state := InitSessionState()
	ctx := WithSessionState(context.Background(), state)

	state.SetSessionID("first")
	assert.Equal(t, "first", GetSessionID(ctx))

	state.SetSessionID("second")
	assert.Equal(t, "second", GetSessionID(ctx))
}

// ──────────────────────────── WithMemberID / MemberIDFromCtx 测试 ────────────────────────────

// TestWithMemberID 测试注入成员 ID 到 context
func TestWithMemberID(t *testing.T) {
	ctx := WithMemberID(context.Background(), "worker1")
	got := MemberIDFromCtx(ctx)
	assert.Equal(t, "worker1", got)
}

// TestMemberIDFromCtx_无成员ID 测试未绑定成员 ID 的 context
func TestMemberIDFromCtx_无成员ID(t *testing.T) {
	got := MemberIDFromCtx(context.Background())
	assert.Equal(t, "", got)
}

// TestWithMemberID_覆盖 测试多次注入成员 ID
func TestWithMemberID_覆盖(t *testing.T) {
	ctx := WithMemberID(context.Background(), "member1")
	ctx = WithMemberID(ctx, "member2")
	got := MemberIDFromCtx(ctx)
	assert.Equal(t, "member2", got)
}

// TestSetSessionIDIfAbsent_临时注入并还原 验证 pause/stop 阶段提取记忆时的
// session 上下文注入：临时生效、结束还原，不污染调用方状态。
func TestSetSessionIDIfAbsent_临时注入并还原(t *testing.T) {
	state := InitSessionState()
	ctx := WithSessionState(context.Background(), state)

	restore := SetSessionIDIfAbsent(ctx, "sess-temp")
	if got := GetSessionID(ctx); got != "sess-temp" {
		t.Fatalf("注入后应读到 sess-temp，实际 %q", got)
	}
	restore()
	if got := GetSessionID(ctx); got != "" {
		t.Errorf("还原后应为空，实际 %q", got)
	}
}

// TestSetSessionIDIfAbsent_已有值时不覆盖 上游语义是"仅在缺失时注入"，
// ctx 已有 session_id 时必须原样保留。
func TestSetSessionIDIfAbsent_已有值时不覆盖(t *testing.T) {
	state := InitSessionState()
	state.SetSessionID("sess-origin")
	ctx := WithSessionState(context.Background(), state)

	restore := SetSessionIDIfAbsent(ctx, "sess-new")
	if got := GetSessionID(ctx); got != "sess-origin" {
		t.Errorf("已有 session 不应被覆盖，实际 %q", got)
	}
	restore()
	if got := GetSessionID(ctx); got != "sess-origin" {
		t.Errorf("还原后应保持原值 sess-origin，实际 %q", got)
	}
}

// TestSetSessionIDIfAbsent_空输入或无状态时是空操作 两者都不应 panic。
func TestSetSessionIDIfAbsent_空输入或无状态时是空操作(t *testing.T) {
	restore := SetSessionIDIfAbsent(context.Background(), "sess-x")
	restore() // 无 SessionState，不应 panic

	state := InitSessionState()
	ctx := WithSessionState(context.Background(), state)
	restore = SetSessionIDIfAbsent(ctx, "")
	restore()
	if got := GetSessionID(ctx); got != "" {
		t.Errorf("空 sessionID 不应写入，实际 %q", got)
	}
}
