package providers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/swarm/providers"
)

// writeProviderSkill 临时技能目录。
func writeProviderSkill(t *testing.T, root, name string) {
	t.Helper()
	dir := filepath.Join(root, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	content := "---\nname: " + name + "\ndescription: the " + name + " skill\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func enabledRetrievalConfig() map[string]any {
	return map[string]any{"skill_retrieval": map[string]any{"enabled": true}}
}

// TestBuildSkillRetrieval_关闭开关返回空
func TestBuildSkillRetrieval_关闭开关返回空(t *testing.T) {
	view := newTestView()
	view.Config = map[string]any{}
	tools := providers.BuildSkillRetrieval(nil, view)
	assert.Empty(t, tools)
}

// TestBuildSkillRetrieval_开启产出skill_index工具
func TestBuildSkillRetrieval_开启产出skill_index工具(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	writeProviderSkill(t, root, "alpha")
	view := newTestView()
	view.Config = enabledRetrievalConfig()
	// skillLibraryDir 唯一物理库 → 指向临时根
	view.GlobalSkillsDir = root
	tools := providers.BuildSkillRetrieval(nil, view)
	require.Len(t, tools, 1)
	card := tools[0].Card()
	assert.Equal(t, "skill_index", card.Name)
	// 三操作可达：list
	result, err := tools[0].Invoke(nil, map[string]any{"operation": "list"})
	require.NoError(t, err)
	assert.Equal(t, true, result["success"])
	content, _ := result["__mapped_content"].(string)
	assert.NotEmpty(t, content)
	// context 槽缓存复用
	assert.NotNil(t, view.SkillRetrievalToolkit)
	tools2 := providers.BuildSkillRetrieval(nil, view)
	require.Len(t, tools2, 1)
}

// TestSkillToolkitForContext_缓存复用与scope三段
func TestSkillToolkitForContext_缓存复用与scope三段(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	writeProviderSkill(t, root, "alpha")
	view := newTestView()
	view.Config = enabledRetrievalConfig()
	view.GlobalSkillsDir = root
	view.SessionID = "sess-1"
	view.TeamID = "team-a"
	view.Role = "member"
	tk1 := providers.SkillToolkitForContext(view)
	require.NotNil(t, tk1)
	assert.Equal(t, "sess-1:team-a:member", tk1.SessionScope())
	tk2 := providers.SkillToolkitForContext(view)
	assert.Same(t, tk1, tk2)
}

// TestBuildSkillRetrievalPromptRail_工厂真身
func TestBuildSkillRetrievalPromptRail_工厂真身(t *testing.T) {
	t.Setenv("UAPCLAW_HOME", t.TempDir())
	root := t.TempDir()
	writeProviderSkill(t, root, "alpha")
	view := newTestView()
	view.Config = enabledRetrievalConfig()
	view.GlobalSkillsDir = root
	rail, err := providers.BuildSkillRetrievalPromptRail(nil, view)
	require.NoError(t, err)
	require.NotNil(t, rail)
	// toolkit 绑定同一 context 槽
	assert.NotNil(t, view.SkillRetrievalToolkit)
}
