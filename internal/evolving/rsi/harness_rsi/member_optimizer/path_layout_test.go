package memberoptimizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPathLayout_键归一(t *testing.T) {
	require.Equal(t, "m007", OptimizationKey("member_optimization_7"))
	require.Equal(t, "m007", OptimizationKey("member_optimization_007"))
	// 非编号 → sha1 前 8
	require.Regexp(t, `^m[0-9a-f]{8}$`, OptimizationKey("weird-id"))
	require.Regexp(t, `^r[0-9a-f]{8}$`, RoleKey("solver"))
	// 空值 default
	require.Equal(t, hashToken("default"), hashToken(""))
}

func TestPathLayout_路径分配(t *testing.T) {
	// workspace/<team>/member_optimizations 三层结构才命中短根推导（Python 同语义）
	output := filepath.Join(t.TempDir(), "workspace", "team1", "member_optimizations")
	l := NewMemberOptimizerPathLayout(output)
	require.Equal(t, filepath.Join(filepath.Dir(filepath.Dir(output)), "mh"), l.RuntimeRoot)
	require.Contains(t, l.RunRuntimeDir("member_optimization_1"), filepath.Join("mh", "runs", "m001"))
	require.Contains(t, l.WorktreesDir("member_optimization_1"), filepath.Join("m001", "wt"))
	require.Contains(t, l.PublishTmpDir("member_optimization_1", "solver"), filepath.Join("m001", "tmp", RoleKey("solver")))
	require.Contains(t, l.CurrentHarnessDir("solver"), filepath.Join("mh", "current", RoleKey("solver")))
	require.Contains(t, l.CandidateHarnessDir("member_optimization_1", "solver"), filepath.Join("m001", "candidate", RoleKey("solver")))
	require.Contains(t, l.InitialHarnessDir("solver"), filepath.Join("mh", "initial", RoleKey("solver")))
}

func TestPathLayout_无编排器根回退(t *testing.T) {
	output := filepath.Join(t.TempDir(), "plain", "output")
	l := NewMemberOptimizerPathLayout(output)
	require.Equal(t, filepath.Join(output, "mh"), l.RuntimeRoot)
}

func TestPathLayout_WriteRoleMapping(t *testing.T) {
	output := filepath.Join(t.TempDir(), "workspace", "team1", "member_optimizations")
	l := NewMemberOptimizerPathLayout(output)
	require.NoError(t, l.WriteRoleMapping("solver"))
	// 二次写入合并
	require.NoError(t, l.WriteRoleMapping("reviewer"))
	data, err := os.ReadFile(l.RolesMapPath())
	require.NoError(t, err)
	require.Contains(t, string(data), "solver")
	require.Contains(t, string(data), "reviewer")
	require.Contains(t, string(data), "current_harness_dir")
}
