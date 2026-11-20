package demand

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/trajectory"
)

// TestGetTrajectorySpanProcessor_懒构造单例 对齐 demand.py:52-66——首次调用
// 构造 TrajectorySpanProcessor，后续幂等返回同实例。
func TestGetTrajectorySpanProcessor_懒构造单例(t *testing.T) {
	p1 := GetTrajectorySpanProcessor()
	if p1 == nil {
		t.Fatal("首次调用应懒构造非 nil processor")
	}
	if _, ok := p1.(*trajectory.TrajectorySpanProcessor); !ok {
		t.Fatalf("应返回 TrajectorySpanProcessor，got %T", p1)
	}
	if p2 := GetTrajectorySpanProcessor(); p1 != p2 {
		t.Fatal("应返回同一实例（进程级单例）")
	}
}
