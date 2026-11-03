package evolution

import (
	"context"
	"strings"
	"testing"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// TestSkillCreateRail_阈值不足不提议 工具数与种类未达阈值时不入队。
func TestSkillCreateRail_阈值不足不提议(t *testing.T) {
	rail, cbc, ctl := newSkillCreateRailForTest(t, []string{"read_file", "write_file"})
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.followUps) != 0 {
		t.Fatalf("阈值不足不应入队，实际 %d 条", len(ctl.followUps))
	}
}

// TestSkillCreateRail_满足阈值只提议一次 达阈值入队且每轮仅一次。
func TestSkillCreateRail_满足阈值只提议一次(t *testing.T) {
	rail, cbc, ctl := newSkillCreateRailForTest(t, thresholdToolNames())

	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.followUps) != 1 {
		t.Fatalf("达阈值应入队 1 条，实际 %d", len(ctl.followUps))
	}
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.followUps) != 1 {
		t.Fatalf("同一轮不应重复入队，实际 %d", len(ctl.followUps))
	}
}

// TestSkillCreateRail_BeforeInvoke重置提议标记 新一轮开始后可再次提议。
func TestSkillCreateRail_BeforeInvoke重置提议标记(t *testing.T) {
	rail, cbc, ctl := newSkillCreateRailForTest(t, thresholdToolNames())
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if err := rail.OnBeforeInvoke(context.Background(), cbc); err != nil {
		t.Fatalf("OnBeforeInvoke error: %v", err)
	}
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if len(ctl.followUps) != 2 {
		t.Fatalf("before_invoke 后应可再次提议，实际 %d", len(ctl.followUps))
	}
}

// TestSkillCreateRail_无Controller时不入队 拿不到 LoopController 时丢弃提议且不置标记。
func TestSkillCreateRail_无Controller时不入队(t *testing.T) {
	rail, _, _ := newSkillCreateRailForTest(t, thresholdToolNames())
	cbc := agentinterfaces.NewAgentCallbackContext(&fakeLoopAgent{}, nil, nil)
	if err := rail.OnAfterTaskIteration(context.Background(), cbc); err != nil {
		t.Fatalf("OnAfterTaskIteration error: %v", err)
	}
	if rail.proposalSent {
		t.Fatal("无 controller 时不应置提议标记")
	}
}

// TestSkillCreateRail_Priority 优先级对齐 Python 的 85。
func TestSkillCreateRail_Priority(t *testing.T) {
	if got := NewSkillCreateRail(t.TempDir()).Priority(); got != 85 {
		t.Fatalf("Priority = %d, want 85", got)
	}
}

// TestSkillCreateRail_CN与EN文案含skillsDir 文案按语言选择并替换目录。
func TestSkillCreateRail_CN与EN文案含skillsDir(t *testing.T) {
	railCN := NewSkillCreateRail("/tmp/skills")
	if p := railCN.buildPrompt(); !strings.Contains(p, "/tmp/skills") || !strings.Contains(p, "skill-creator") {
		t.Fatalf("CN 文案应包含目录与 skill-creator: %s", p)
	}
	railEN := NewSkillCreateRail("/tmp/skills", WithSkillCreateLanguage("en"))
	if p := railEN.buildPrompt(); !strings.Contains(p, "/tmp/skills") || !strings.Contains(p, "skill-creator") {
		t.Fatalf("EN 文案应包含目录与 skill-creator: %s", p)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// thresholdToolNames 返回满足阈值（10 次调用、5 种工具）的工具名序列。
func thresholdToolNames() []string {
	names := make([]string, 0, 10)
	for _, name := range []string{"read_file", "write_file", "bash", "code", "edit_file"} {
		names = append(names, name, name)
	}
	return names
}

// newSkillCreateRailForTest 构造带 step 的 rail 与回调上下文。
func newSkillCreateRailForTest(t *testing.T, toolNames []string) (*SkillCreateRail, *agentinterfaces.AgentCallbackContext, *fakeController) {
	t.Helper()
	rail := NewSkillCreateRail(t.TempDir())
	builder := trajectory.NewTrajectoryBuilder("sess-1", "test")
	for _, name := range toolNames {
		builder.RecordStep(&trajectory.TrajectoryStep{
			Kind:   trajectory.StepKindTool,
			Detail: &trajectory.ToolCallDetail{ToolName: name},
		})
	}
	rail.builder = builder

	ctl := &fakeController{}
	agent := &fakeLoopAgent{ctl: ctl}
	cbc := agentinterfaces.NewAgentCallbackContext(agent, nil, nil)
	return rail, cbc, ctl
}
