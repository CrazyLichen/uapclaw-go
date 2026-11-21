package worktree

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	atschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/schema"
)

// TestValidateMemberWorktreeIsolation_非git仓库报错 project_dir 非 git 仓库时显式报错。
// Python: agent_configurator.py :83 文案一比一
func TestValidateMemberWorktreeIsolation_非git仓库报错(t *testing.T) {
	dir := t.TempDir()
	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": dir}}
	err := ValidateMemberWorktreeIsolation(withSessionID(t, "s1"), spec, "team-a", "alice")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "is not in a git repository")
}

// TestValidateMemberWorktreeIsolation_git仓库通过 git 仓库校验通过。
func TestValidateMemberWorktreeIsolation_git仓库通过(t *testing.T) {
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t.com",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t.com")
		out, err := cmd.CombinedOutput()
		require.NoError(t, err, "git %v: %s", args, out)
	}
	run("init", "-b", "main")
	require.NoError(t, os.WriteFile(filepath.Join(dir, "f"), []byte("x"), 0o644))
	run("add", ".")
	run("commit", "-m", "init")

	spec := &atschema.TeamAgentSpec{BuildContextSeed: map[string]any{"project_dir": dir}}
	assert.NoError(t, ValidateMemberWorktreeIsolation(withSessionID(t, "s1"), spec, "team-a", "alice"))
}
