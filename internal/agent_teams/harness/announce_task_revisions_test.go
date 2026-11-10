package harness

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// stubAnnounceTracker 记录 announce 走到的位置，用于覆盖 harness 侧分支。
type stubAnnounceTracker struct {
	tracker.UnimplementedTaskEventTracker
	// releaseCalls Release 被调用次数
	releaseCalls int
}

// Wakes 覆写为真，让 announce 继续往下走。
func (s *stubAnnounceTracker) Wakes() bool { return true }

// Claim 返回一条固定通知。
func (s *stubAnnounceTracker) Claim(sessioninterfaces.SessionFacade) *tracker.ClaimedNotice {
	return &tracker.ClaimedNotice{Text: "notice", Entries: []string{"t1@r1"}}
}

// Release 记录调用。
func (s *stubAnnounceTracker) Release(*tracker.ClaimedNotice) { s.releaseCalls++ }

// Announce 复刻基类逻辑（Claim → send → 失败归还），以便验证 release 调用。
func (s *stubAnnounceTracker) Announce(_ sessioninterfaces.SessionFacade, send func(string) error) (bool, error) {
	if !s.Wakes() {
		return false, nil
	}
	notice := s.Claim(nil)
	if notice == nil {
		return false, nil
	}
	if err := send(notice.Text); err != nil {
		s.Release(notice)
		return false, err
	}
	return true, nil
}

// TestAnnounceTaskRevisions_无tracker直接返回 没注入通知器时静默跳过。
func TestAnnounceTaskRevisions_无tracker直接返回(t *testing.T) {
	h := &TeamHarness{}
	assert.NoError(t, h.AnnounceTaskRevisions(context.Background()))
}

// TestAnnounceTaskRevisions_无会话直接返回 轮次之外没有可认领的会话。
func TestAnnounceTaskRevisions_无会话直接返回(t *testing.T) {
	h := &TeamHarness{taskEventTracker: &stubAnnounceTracker{}}
	assert.NoError(t, h.AnnounceTaskRevisions(context.Background()))
}

// TestAnnounceTaskRevisions_投递失败归还 未 start 时 Send 报错，认领被归还且错误上抛。
func TestAnnounceTaskRevisions_投递失败归还(t *testing.T) {
	tr := &stubAnnounceTracker{}
	h := &TeamHarness{taskEventTracker: tr}
	h.setActiveSession(&session.Session{})

	err := h.AnnounceTaskRevisions(context.Background())
	require.Error(t, err, "native 为 nil 时 Send 应报错")
	assert.Equal(t, 1, tr.releaseCalls, "投递失败必须归还认领")
}

// TestCurrentSession_未启动返回nil 未设置活跃会话时返回 nil。
func TestCurrentSession_未启动返回nil(t *testing.T) {
	h := &TeamHarness{}
	assert.Nil(t, h.CurrentSession())
}

// TestCurrentSession_返回活跃会话 已设置活跃会话时原样返回。
func TestCurrentSession_返回活跃会话(t *testing.T) {
	h := &TeamHarness{}
	sess := &session.Session{}
	h.setActiveSession(sess)
	assert.Equal(t, sess, h.CurrentSession())
}

// TestFindRailsByType_原型为nil返回nil nil 原型安全返回。
func TestFindRailsByType_原型为nil返回nil(t *testing.T) {
	h := &TeamHarness{}
	assert.Nil(t, h.FindRailsByType(nil))
}

// TestFindRailsByType_无deepAgent返回nil 未装配底层 agent 时返回 nil。
func TestFindRailsByType_无deepAgent返回nil(t *testing.T) {
	h := &TeamHarness{}
	assert.Nil(t, h.FindRailsByType(&struct{}{}))
}
