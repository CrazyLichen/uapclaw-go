package agent

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"

	teamharness "github.com/uapclaw/uapclaw-go/internal/agent_teams/harness"
	streambase "github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// TestStreamController_PauseAgent_资源为nil时安全 验证未装配资源时暂停/恢复是 no-op。
func TestStreamController_PauseAgent_资源为nil时安全(t *testing.T) {
	sc := &StreamController{}
	assert.NoError(t, sc.PauseAgent(context.Background()))
	assert.NoError(t, sc.ResumeAgent(context.Background(), nil))
}

// TestStreamController_PauseAgent_未启动Harness安全 验证 harness 未启动时暂停/恢复是 no-op。
func TestStreamController_PauseAgent_未启动Harness安全(t *testing.T) {
	sc := &StreamController{
		resources: &PrivateAgentResources{Harness: &teamharness.TeamHarness{}},
	}
	assert.NoError(t, sc.PauseAgent(context.Background()))
	empty := ""
	assert.NoError(t, sc.ResumeAgent(context.Background(), &empty))
}

// TestStreamController_StreamQueue快照与重置并发安全 锁定读路径必须经读锁取快照。
// 回归：ResetStreamQueue（finalize_round 协程）与 forwardOutputs/观察者转发并发时，
// 直接读 sc.streamQueue 会触发 data race（2026-11-09 集成套件实证）。
func TestStreamController_StreamQueue快照与重置并发安全(t *testing.T) {
	sc := &StreamController{streamQueue: make(chan streambase.Schema, 1)}

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			if q := sc.streamQueueSnapshot(); q != nil {
				select {
				case q <- nil:
				default:
				}
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			sc.ResetStreamQueue()
		}
	}()
	wg.Wait()
}
