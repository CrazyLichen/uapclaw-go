package runtime

import (
	"context"
	"errors"
	"testing"

	sessionpkg "github.com/uapclaw/uapclaw-go/internal/swarm/runtime/session"
)

// recordingTeamController 记录团队控制面调用顺序。
type recordingTeamController struct {
	// order 调用记录
	order []string
}

// QuiesceForDelete 记录静默调用。
func (c *recordingTeamController) QuiesceForDelete(
	_ context.Context, target sessionpkg.SessionLifecycleTarget, _ string,
) error {
	c.order = append(c.order, "quiesce:"+target.TeamName)
	return nil
}

// DisposeAfterResourceRelease 记录释放后销毁。
func (c *recordingTeamController) DisposeAfterResourceRelease(
	_ context.Context, target sessionpkg.SessionLifecycleTarget, _ string,
) error {
	c.order = append(c.order, "dispose:"+target.TeamName)
	return nil
}

// DeleteAborted 记录删除中止。
func (c *recordingTeamController) DeleteAborted(target sessionpkg.SessionLifecycleTarget) {
	c.order = append(c.order, "aborted:"+target.TeamName)
}

// DeleteCommitted 记录删除提交。
func (c *recordingTeamController) DeleteCommitted(target sessionpkg.SessionLifecycleTarget) {
	c.order = append(c.order, "committed:"+target.TeamName)
}

// hasCall 判断某调用是否发生。
func (c *recordingTeamController) hasCall(call string) bool {
	for _, item := range c.order {
		if item == call {
			return true
		}
	}
	return false
}

// newTeamTestProvisioner 构造团队删除测试用 provisioner。
func newTeamTestProvisioner(controller *recordingTeamController, sessionIDs []string) *RuntimeSessionProvisioner {
	provisioner := NewRuntimeSessionProvisioner(ProvisionerDeps{
		TeamExecutionController: controller,
	})
	provisioner.TeamDeletionGate = sessionpkg.NewTeamDeletionGate()
	provisioner.DeleteAgentTeam = func(context.Context, string, []string) (bool, error) {
		return true, nil
	}
	provisioner.extra.ListTeamSessions = func(string) []string { return sessionIDs }
	return provisioner
}

// TestDeleteTeam_整体quiesce先于逐会话删除 顺序断言。
//
// 上游语义：先整体静默整队，再逐会话删，最后 Runner 只删一次。
// 若顺序颠倒（先删会话再静默），静默会打到已删的执行上。
func TestDeleteTeam_整体quiesce先于逐会话删除(t *testing.T) {
	controller := &recordingTeamController{}
	provisioner := newTeamTestProvisioner(controller, nil)

	result, err := provisioner.DeleteTeam(context.Background(), "team-a", "ch-1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Deleted {
		t.Error("应报告已删除")
	}
	if len(controller.order) == 0 {
		t.Fatal("应调用团队控制面")
	}
	if controller.order[0] != "quiesce:team-a" {
		t.Errorf("首个动作应为整体 quiesce，实际 %v", controller.order)
	}
	if !controller.hasCall("committed:team-a") {
		t.Errorf("成功删除应调 DeleteCommitted，实际 %v", controller.order)
	}
}

// TestDeleteTeam_空团队名 拒绝空团队名。
func TestDeleteTeam_空团队名(t *testing.T) {
	controller := &recordingTeamController{}
	provisioner := newTeamTestProvisioner(controller, nil)

	result, err := provisioner.DeleteTeam(context.Background(), "  ", "ch-1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if result.OK {
		t.Error("空团队名应失败")
	}
	if result.ErrorCode != "BAD_REQUEST" {
		t.Errorf("错误码应为 BAD_REQUEST，实际 %q", result.ErrorCode)
	}
}

// TestDeleteTeam_Runner删一次 Runner 只调一次（无论多少个会话）。
func TestDeleteTeam_Runner删一次(t *testing.T) {
	controller := &recordingTeamController{}
	runnerCalls := 0
	provisioner := NewRuntimeSessionProvisioner(ProvisionerDeps{
		TeamExecutionController: controller,
	})
	provisioner.TeamDeletionGate = sessionpkg.NewTeamDeletionGate()
	provisioner.DeleteAgentTeam = func(_ context.Context, _ string, _ []string) (bool, error) {
		runnerCalls++
		return true, nil
	}
	provisioner.extra.ListTeamSessions = func(string) []string { return []string{"s1", "s2", "s3"} }

	if _, err := provisioner.DeleteTeam(context.Background(), "team-a", "ch-1"); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if runnerCalls != 1 {
		t.Errorf("Runner 应只删一次，实际 %d", runnerCalls)
	}
}

// TestDeleteTeam_Runner失败时中止 Runner 失败时调 DeleteAborted。
func TestDeleteTeam_Runner失败时中止(t *testing.T) {
	controller := &recordingTeamController{}
	provisioner := NewRuntimeSessionProvisioner(ProvisionerDeps{
		TeamExecutionController: controller,
	})
	provisioner.TeamDeletionGate = sessionpkg.NewTeamDeletionGate()
	provisioner.DeleteAgentTeam = func(context.Context, string, []string) (bool, error) {
		return false, errors.New("runner failed")
	}
	provisioner.extra.ListTeamSessions = func(string) []string { return nil }

	if _, err := provisioner.DeleteTeam(context.Background(), "team-a", "ch-1"); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !controller.hasCall("aborted:team-a") {
		t.Errorf("Runner 失败应调 DeleteAborted，实际 %v", controller.order)
	}
	if controller.hasCall("committed:team-a") {
		t.Error("Runner 失败不应调 DeleteCommitted")
	}
}

// TestDeleteTeam_重复调用幂等 同团队重复删除返回缓存结果。
func TestDeleteTeam_重复调用幂等(t *testing.T) {
	controller := &recordingTeamController{}
	provisioner := newTeamTestProvisioner(controller, nil)

	first, err := provisioner.DeleteTeam(context.Background(), "team-a", "ch-1")
	if err != nil {
		t.Fatalf("首次不应报错: %v", err)
	}
	second, err := provisioner.DeleteTeam(context.Background(), "team-a", "ch-1")
	if err != nil {
		t.Fatalf("重复不应报错: %v", err)
	}
	if first.Deleted != second.Deleted {
		t.Error("重复删除应返回一致结果")
	}
}

// TestDeleteTeam_无控制器也完成 控制器未装配时仍能删（降级）。
func TestDeleteTeam_无控制器也完成(t *testing.T) {
	provisioner := NewRuntimeSessionProvisioner(ProvisionerDeps{})
	provisioner.TeamDeletionGate = sessionpkg.NewTeamDeletionGate()
	provisioner.DeleteAgentTeam = func(context.Context, string, []string) (bool, error) {
		return true, nil
	}
	provisioner.extra.ListTeamSessions = func(string) []string { return nil }

	result, err := provisioner.DeleteTeam(context.Background(), "team-a", "ch-1")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !result.Deleted {
		t.Error("无控制器时也应完成删除")
	}
}
