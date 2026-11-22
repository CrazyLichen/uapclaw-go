package runtime

import (
	"context"
	"strings"
	"testing"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// fakeLifecyclePort 生命周期端假实现（记录调用序与参数）。
type fakeLifecyclePort struct {
	events []string
	paused bool
}

func (f *fakeLifecyclePort) Release(context.Context) error { return nil }

func (f *fakeLifecyclePort) RemoveResponseWaiter(_ context.Context, _ sessionpkg.ExecutionLifecycleContext, reason string) error {
	f.events = append(f.events, "remove_waiter:"+reason)
	return nil
}

func (f *fakeLifecyclePort) PauseSession(_ context.Context, sess sessionpkg.SessionLifecycleContext, reason, channelID string) (bool, error) {
	f.events = append(f.events, "pause:"+reason+":"+channelID+":"+sess.SessionID)
	return f.paused, nil
}

func (f *fakeLifecyclePort) StopSession(_ context.Context, sess sessionpkg.SessionLifecycleContext, reason string) error {
	f.events = append(f.events, "stop:"+reason+":"+sess.SessionID)
	return nil
}

func (f *fakeLifecyclePort) PublishPauseCompleted(_ context.Context, sessionID string, generation *int, channelID string) error {
	f.events = append(f.events, "publish_pause_completed:"+channelID)
	return nil
}

func (f *fakeLifecyclePort) IsRunning(sessionpkg.SessionLifecycleContext) bool { return false }

// teamRequest 构造 team 请求（mode=team，触发 requestTargetsTeam）。
func teamRequest(sessionID, intent string) *schema.AgentRequest {
	sid := sessionID
	mode := "team"
	req := schema.NewAgentRequest("req-1", "web", "chat.send",
		[]byte(`{"intent":"`+intent+`","mode":"`+mode+`"}`))
	req.SessionID = &sid
	return req
}

// TestCancelRequestWithIntent_PauseTeamBranch pause intent → adapter pause（USER_PAUSE + channel）。
// Python: service.py:1415-1431
func TestCancelRequestWithIntent_PauseTeamBranch(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{})
	fake := &fakeLifecyclePort{paused: true}
	r.sessionRuntimeLifecycle = map[string]SessionRuntimeLifecyclePort{"web": fake}
	// 注册会话使 snapshot 非空（Python :1416 snapshot is not None 前提）
	r.coordinator.RegisterSession("ci-sess-1", "web", sessionpkg.SessionPersistencePolicyPersistent)
	req := teamRequest("ci-sess-1", "pause")
	resp, err := r.CancelRequestWithIntent(context.Background(), req)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if resp == nil {
		t.Fatal("resp nil")
	}
	joined := strings.Join(fake.events, ";")
	if !strings.Contains(joined, "pause:user_pause:web:ci-sess-1") {
		t.Fatalf("pause 分支未命中: %v", fake.events)
	}
	// 暂停成功且取消排空 → publish（步骤 ⑩c；无活跃执行时 drained=true）
	if !strings.Contains(joined, "publish_pause_completed:web") {
		t.Fatalf("publish 未触发: %v", fake.events)
	}
}

// TestCancelRequestWithIntent_StopTeamBranch stop intent → adapter stop（USER_CANCEL）。
func TestCancelRequestWithIntent_StopTeamBranch(t *testing.T) {
	r := NewAgentRuntime(AgentRuntimeDeps{})
	fake := &fakeLifecyclePort{}
	r.sessionRuntimeLifecycle = map[string]SessionRuntimeLifecyclePort{"web": fake}
	r.coordinator.RegisterSession("ci-sess-2", "web", sessionpkg.SessionPersistencePolicyPersistent)
	req := teamRequest("ci-sess-2", "stop")
	if _, err := r.CancelRequestWithIntent(context.Background(), req); err != nil {
		t.Fatalf("err: %v", err)
	}
	joined := strings.Join(fake.events, ";")
	if !strings.Contains(joined, "stop:user_cancel:ci-sess-2") {
		t.Fatalf("stop 分支未命中: %v", fake.events)
	}
	if strings.Contains(joined, "publish_pause_completed") {
		t.Fatal("stop 路径不应发布暂停完成")
	}
}

// TestCancelIntentParam intent 解析（缺省 cancel / 大小写归一）。
// Python: service.py:1313-1315
func TestCancelIntentParam(t *testing.T) {
	if got := cancelIntentParam(map[string]any{}); got != "cancel" {
		t.Fatalf("default = %s", got)
	}
	if got := cancelIntentParam(map[string]any{"intent": " PAUSE "}); got != "pause" {
		t.Fatalf("normalized = %s", got)
	}
}
