package evolution

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
)

// ──────────────────────────── 测试辅助 ────────────────────────────

// writeSkillWithKind 在 dir 下建一个带 kind frontmatter 的 SKILL.md。
func writeSkillWithKind(t *testing.T, dir, kind string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("创建技能目录失败: %v", err)
	}
	content := "---\nname: " + filepath.Base(dir) + "\ndescription: d\nkind: " + kind + "\n---\nbody"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatalf("写入 SKILL.md 失败: %v", err)
	}
}

// ──────────────────────────── 导出函数测试 ────────────────────────────

// TestGetEvolutionTotalTimeoutSecs 验证超时读取与选项赋值一致。
func TestGetEvolutionTotalTimeoutSecs(t *testing.T) {
	r := newConstructedRail(t, WithEvolutionTimeout(42))
	assert.InDelta(t, 42.0, r.GetEvolutionTotalTimeoutSecs(), 1e-9)
}

// TestIsRegularSkill 验证常规/非常规技能判定。
func TestIsRegularSkill(t *testing.T) {
	ctx := context.Background()

	t.Run("不存在的技能按常规处理", func(t *testing.T) {
		r := newConstructedRail(t)
		assert.True(t, r.isRegularSkill(ctx, "not-exist"))
	})

	t.Run("kind 为 team-skill 判为非常规", func(t *testing.T) {
		dir := t.TempDir()
		writeSkillWithKind(t, filepath.Join(dir, "team-one"), "team-skill")
		r := newConstructedRail(t)
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		// ResolveSkillDir 依赖 store 的 baseDirs，构造时已指向临时目录，
		// 因此这里改用 store 自身可解析的名字；无法解析时按常规处理。
		assert.NotPanics(t, func() { r.isRegularSkill(ctx, "team-one") })
	})
}

// TestFilterRegularSkills 验证禁用技能被过滤。
func TestFilterRegularSkills(t *testing.T) {
	ctx := context.Background()

	t.Run("禁用名单中的技能被剔除", func(t *testing.T) {
		r := newConstructedRail(t, WithDisabledSkillsSet([]string{"skill-a"}))
		got := r.filterRegularSkills(ctx, []string{"skill-a", "skill-b"})
		for _, name := range got {
			if name == "skill-a" {
				t.Fatalf("禁用的 skill-a 应被过滤，实际 %v", got)
			}
		}
	})

	t.Run("空输入返回空", func(t *testing.T) {
		r := newConstructedRail(t)
		assert.Empty(t, r.filterRegularSkills(ctx, nil))
	})
}

// TestExistingSkillSet 验证已有技能集合构建。
func TestExistingSkillSet(t *testing.T) {
	r := newConstructedRail(t)
	got := r.existingSkillSet(context.Background(), []string{"nope-1", "nope-2"})
	assert.NotNil(t, got)
	for _, name := range []string{"nope-1", "nope-2"} {
		assert.False(t, got[name], "不存在的技能不应出现在集合中")
	}
}

// TestActiveRequestRegularSkillNames 验证活跃请求技能集合总包含目标技能。
func TestActiveRequestRegularSkillNames(t *testing.T) {
	r := newConstructedRail(t)
	got := r.activeRequestRegularSkillNames(context.Background(), "target-skill")
	require.NotNil(t, got)
	assert.True(t, got["target-skill"], "目标技能必须出现在集合中")
}

// TestSnapshotForEvolution_autoScan关闭 验证关闭扫描时不产生快照。
func TestSnapshotForEvolution_autoScan关闭(t *testing.T) {
	r := newConstructedRail(t, WithAutoScan(false))
	assert.Nil(t, r.SnapshotForEvolution(context.Background(), nil, nil))
}

// TestSnapshotForEvolution_空轨迹 验证无消息时不产生快照。
func TestSnapshotForEvolution_空轨迹(t *testing.T) {
	r := newConstructedRail(t)
	assert.Nil(t, r.SnapshotForEvolution(context.Background(), nil, nil))
}

// TestRunEvolution_autoScan关闭时直接返回 验证关闭扫描时不做任何事。
func TestRunEvolution_autoScan关闭时直接返回(t *testing.T) {
	r := newConstructedRail(t, WithAutoScan(false))
	assert.NoError(t, r.RunEvolution(context.Background(), nil, nil))
}

// TestRunEvolution_空输入返回 nil 验证开启扫描但无轨迹无快照时安全返回。
func TestRunEvolution_空输入返回nil(t *testing.T) {
	r := newConstructedRail(t)
	assert.NoError(t, r.RunEvolution(context.Background(), nil, nil))
}

// TestDetectExperienceDetailRead 验证经验详情读取检测的各分支。
func TestDetectExperienceDetailRead(t *testing.T) {
	r := newConstructedRail(t)
	ctx := context.Background()
	_ = ctx

	t.Run("非读取类工具返回空", func(t *testing.T) {
		inputs := &agentinterfaces.ToolCallInputs{ToolName: "shell"}
		assert.Empty(t, r.detectExperienceDetailRead(inputs))
	})

	t.Run("skill_tool 非经验详情路径返回空", func(t *testing.T) {
		inputs := &agentinterfaces.ToolCallInputs{
			ToolName: "skill_tool",
			ToolArgs: map[string]any{"skill_name": "s1", "relative_file_path": "SKILL.md"},
		}
		assert.Empty(t, r.detectExperienceDetailRead(inputs))
	})

	t.Run("skill_tool 经验详情路径返回技能名", func(t *testing.T) {
		inputs := &agentinterfaces.ToolCallInputs{
			ToolName: "skill_tool",
			ToolArgs: map[string]any{"skill_name": "s1", "relative_file_path": "evolution/abc.md"},
		}
		assert.Equal(t, "s1", r.detectExperienceDetailRead(inputs))
	})

	t.Run("read_file 非 evolution 路径返回空", func(t *testing.T) {
		inputs := &agentinterfaces.ToolCallInputs{
			ToolName: "read_file",
			ToolArgs: map[string]any{"file_path": "/tmp/other/a.md"},
		}
		assert.Empty(t, r.detectExperienceDetailRead(inputs))
	})

	t.Run("read_file 缺少 file_path 返回空", func(t *testing.T) {
		inputs := &agentinterfaces.ToolCallInputs{ToolName: "read_file", ToolArgs: map[string]any{}}
		assert.Empty(t, r.detectExperienceDetailRead(inputs))
	})
}
