package agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tracker"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/session/interfaces"
)

// stubTaskTracker 任务通知器替身，用于断言 Set/Get 往返拿到同一实例。
//
// 嵌入 tracker.UnimplementedTaskEventTracker 补齐 Announce/Wakes/Release
// （规则 10：接口扩张时改替身，不改接口）。
type stubTaskTracker struct {
	tracker.UnimplementedTaskEventTracker
}

// Claim 见 tracker.TaskEventTracker。
func (stubTaskTracker) Claim(_ sessioninterfaces.SessionFacade) *tracker.ClaimedNotice {
	return nil
}

// Persist 见 tracker.TaskEventTracker。
func (stubTaskTracker) Persist(_ sessioninterfaces.SessionFacade) {}
