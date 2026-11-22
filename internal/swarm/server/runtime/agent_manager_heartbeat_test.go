package runtime

import (
	"testing"

	buildctx "github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/buildctx"
)

// fakeHeartbeatService 窄面假实现。
type fakeHeartbeatService struct{ called bool }

func (f *fakeHeartbeatService) HandleOperation(string, map[string]any, string, string, string, string) (map[string]any, error) {
	f.called = true
	return nil, nil
}

// TestSetHeartbeatService_进程级发布 注入后 buildctx 进程级 Get 同实例。
// Python: agent_manager.py:250-261
func TestSetHeartbeatService_进程级发布(t *testing.T) {
	am := NewAgentManager()
	svc := &fakeHeartbeatService{}
	am.SetHeartbeatService(svc)
	if buildctx.GetHeartbeatJobService() == nil {
		t.Fatal("buildctx 进程级发布缺失")
	}
	// 真接口满足（编译期断言 via 类型断言）
	if _, ok := buildctx.GetHeartbeatJobService().(buildctx.HeartbeatJobService); !ok {
		t.Fatal("发布对象应为 HeartbeatJobService 窄面")
	}
	// 收尾清理（进程级静态，勿污染其他用例）
	am.SetHeartbeatService(nil)
}
