package runtime

import (
	"context"
	"errors"
	"sync"
	"testing"
)

// recordingPromptForks 记录 SessionDeleted 调用。
type recordingPromptForks struct {
	mu    sync.Mutex
	calls []string
}

// SessionDeleted 记录会话标识。
func (p *recordingPromptForks) SessionDeleted(sessionID string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.calls = append(p.calls, sessionID)
}

// count 取调用次数。
func (p *recordingPromptForks) count() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.calls)
}

// recordingSubagentStopper 记录 StopSubagentRuntimes 调用。
type recordingSubagentStopper struct {
	mu      sync.Mutex
	calls   []string
	reasons []string
	err     error
}

// StopSubagentRuntimes 记录调用并返回预设错误。
func (s *recordingSubagentStopper) StopSubagentRuntimes(
	_ context.Context, sessionID, reason string,
) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, sessionID)
	s.reasons = append(s.reasons, reason)
	return s.err
}

// count 取调用次数。
func (s *recordingSubagentStopper) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

// recordingSessionCleaner 记录 CleanupSession 调用。
type recordingSessionCleaner struct {
	mu      sync.Mutex
	calls   []string
	channel []string
	err     error
}

// CleanupSession 记录调用并返回预设错误。
func (c *recordingSessionCleaner) CleanupSession(
	_ context.Context, channelID, sessionID string,
) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls = append(c.calls, sessionID)
	c.channel = append(c.channel, channelID)
	return c.err
}

// count 取调用次数。
func (c *recordingSessionCleaner) count() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.calls)
}

// TestDisposeSessionAfterResourceRelease_三步都调用 顺序与参数正确。
func TestDisposeSessionAfterResourceRelease_三步都调用(t *testing.T) {
	forks := &recordingPromptForks{}
	stopper := &recordingSubagentStopper{}
	cleaner := &recordingSessionCleaner{}
	prov := &RuntimeSessionProvisioner{
		PromptForks:     forks,
		SubagentStopper: stopper,
		SessionCleaner:  cleaner,
	}
	prov.disposeSessionAfterResourceRelease(context.Background(), "ch-1", "s1")

	if forks.count() != 1 || forks.calls[0] != "s1" {
		t.Errorf("应调用 PromptForks.SessionDeleted(s1)，实际 %v", forks.calls)
	}
	if stopper.count() != 1 || stopper.calls[0] != "s1" {
		t.Errorf("应调用 SubagentStopper.StopSubagentRuntimes(s1)，实际 %v", stopper.calls)
	}
	// 上游固定传 reason="session_deleted"
	if len(stopper.reasons) != 1 || stopper.reasons[0] != "session_deleted" {
		t.Errorf("reason 应为 session_deleted，实际 %v", stopper.reasons)
	}
	if cleaner.count() != 1 || cleaner.calls[0] != "s1" {
		t.Errorf("应调用 SessionCleaner.CleanupSession(s1)，实际 %v", cleaner.calls)
	}
	if len(cleaner.channel) != 1 || cleaner.channel[0] != "ch-1" {
		t.Errorf("渠道标识应透传，实际 %v", cleaner.channel)
	}
}

// TestDisposeSessionAfterResourceRelease_前一步失败不中断 只告警。
func TestDisposeSessionAfterResourceRelease_前一步失败不中断(t *testing.T) {
	forks := &recordingPromptForks{}
	stopper := &recordingSubagentStopper{err: errors.New("boom")}
	cleaner := &recordingSessionCleaner{}
	prov := &RuntimeSessionProvisioner{
		PromptForks:     forks,
		SubagentStopper: stopper,
		SessionCleaner:  cleaner,
	}
	prov.disposeSessionAfterResourceRelease(context.Background(), "ch-1", "s1")

	if cleaner.count() != 1 {
		t.Error("子代理停止失败不应阻止后续清理")
	}
}

// TestDisposeSessionAfterResourceRelease_末步失败不返回错误 上游 try/except 语义。
func TestDisposeSessionAfterResourceRelease_末步失败不返回错误(t *testing.T) {
	cleaner := &recordingSessionCleaner{err: errors.New("boom")}
	prov := &RuntimeSessionProvisioner{
		PromptForks:     &recordingPromptForks{},
		SubagentStopper: &recordingSubagentStopper{},
		SessionCleaner:  cleaner,
	}
	if err := prov.disposeSessionAfterResourceRelease(context.Background(), "ch-1", "s1"); err != nil {
		t.Errorf("单条失败应只告警，不应返回错误: %v", err)
	}
}

// TestDisposeSessionAfterResourceRelease_全nil不panic 依赖未注入时安全。
func TestDisposeSessionAfterResourceRelease_全nil不panic(t *testing.T) {
	prov := &RuntimeSessionProvisioner{}
	if err := prov.disposeSessionAfterResourceRelease(context.Background(), "ch-1", "s1"); err != nil {
		t.Errorf("全 nil 不应报错: %v", err)
	}
}

// TestDisposeSessionAfterResourceRelease_导出包装 导出方法委托同一实现。
func TestDisposeSessionAfterResourceRelease_导出包装(t *testing.T) {
	forks := &recordingPromptForks{}
	prov := &RuntimeSessionProvisioner{PromptForks: forks}
	if err := prov.DisposeSessionAfterResourceRelease(context.Background(), "ch-1", "s9"); err != nil {
		t.Fatalf("导出方法不应报错: %v", err)
	}
	if forks.count() != 1 || forks.calls[0] != "s9" {
		t.Errorf("导出方法应委托实现，实际 %v", forks.calls)
	}
}

// disposeTestNoopForks 测试用建议生成桩。
type disposeTestNoopForks struct{}

// SessionDeleted 无操作。
func (disposeTestNoopForks) SessionDeleted(string) {}

// disposeTestNoopStopper 测试用子代理停止桩。
type disposeTestNoopStopper struct{}

// StopSubagentRuntimes 返回 nil。
func (disposeTestNoopStopper) StopSubagentRuntimes(context.Context, string, string) error {
	return nil
}

// TestDisposeStubs_无操作 桩可安全调用。
func TestDisposeStubs_无操作(t *testing.T) {
	disposeTestNoopForks{}.SessionDeleted("s1")
	if err := (disposeTestNoopStopper{}).StopSubagentRuntimes(
		context.Background(), "s1", "session_deleted"); err != nil {
		t.Errorf("桩应返回 nil，实际 %v", err)
	}
}
