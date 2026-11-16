// path_layout.go member 优化短运行时路径布局。
// Python: member_optimizer/path_layout.py（全量对齐，123 行）。
//
// 审计产物留在配置的 member_optimizations 目录下；递归拷贝 ExpertHarness
// 包的运行时目录放在工作区级短根下，规避 Windows 路径长度失败。

package memberoptimizer

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

// optimizationIDPattern member_optimization_N 编号模式。
// Python: path_layout.py:19 _OPTIMIZATION_ID_RE。
var optimizationIDPattern = regexp.MustCompile(`member_optimization_(\d+)$`)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberOptimizerPathLayout 分配短运行时路径同时保留可读审计路径。
// Python: path_layout.py:22-24 MemberOptimizerPathLayout。
type MemberOptimizerPathLayout struct {
	// OutputRoot 审计输出根
	OutputRoot string
	// RuntimeRoot 短运行时根
	RuntimeRoot string
}

// NewMemberOptimizerPathLayout 从审计输出根推导布局。
// Python: path_layout.py:26-31 from_output_root。
func NewMemberOptimizerPathLayout(outputRoot string) *MemberOptimizerPathLayout {
	resolved, _ := filepath.Abs(expandUserTilde(outputRoot))
	return &MemberOptimizerPathLayout{
		OutputRoot: resolved,
		RuntimeRoot: inferRuntimeRoot(resolved),
	}
}

// RolesMapPath roles.yaml 映射路径。
// Python: path_layout.py:33-34 roles_map_path。
func (l *MemberOptimizerPathLayout) RolesMapPath() string {
	return filepath.Join(l.RuntimeRoot, "roles.yaml")
}

// OptimizationKey 优化 id → 短键（编号归一或 sha1 前 8）。
// Python: path_layout.py:36-40 optimization_key。
func OptimizationKey(optimizationID string) string {
	if match := optimizationIDPattern.FindStringSubmatch(optimizationID); match != nil {
		n, _ := strconv.Atoi(match[1])
		return fmt.Sprintf("m%03d", n)
	}
	return "m" + hashToken(optimizationID)
}

// RoleKey 角色 → 短键。
// Python: path_layout.py:42-44 role_key。
func RoleKey(role string) string {
	return "r" + hashToken(role)
}

// RunRuntimeDir 运行时 run 目录。
// Python: path_layout.py:46-47 run_runtime_dir。
func (l *MemberOptimizerPathLayout) RunRuntimeDir(optimizationID string) string {
	return filepath.Join(l.RuntimeRoot, "runs", OptimizationKey(optimizationID))
}

// WorktreesDir run 的 worktrees 目录。
// Python: path_layout.py:49-50 worktrees_dir。
func (l *MemberOptimizerPathLayout) WorktreesDir(optimizationID string) string {
	return filepath.Join(l.RunRuntimeDir(optimizationID), "wt")
}

// PublishTmpDir 发布临时目录。
// Python: path_layout.py:52-54 publish_tmp_dir。
func (l *MemberOptimizerPathLayout) PublishTmpDir(optimizationID, role string) string {
	return filepath.Join(l.RunRuntimeDir(optimizationID), "tmp", RoleKey(role))
}

// CurrentHarnessDir 当前 harness 目录。
// Python: path_layout.py:56-57 current_harness_dir。
func (l *MemberOptimizerPathLayout) CurrentHarnessDir(role string) string {
	return filepath.Join(l.RuntimeRoot, "current", RoleKey(role))
}

// CandidateHarnessDir 不可变 run 级候选目录。
// Python: path_layout.py:59-62 candidate_harness_dir。
func (l *MemberOptimizerPathLayout) CandidateHarnessDir(optimizationID, role string) string {
	return filepath.Join(l.RunRuntimeDir(optimizationID), "candidate", RoleKey(role))
}

// InitialHarnessDir 初始 harness 目录。
// Python: path_layout.py:63-64 initial_harness_dir。
func (l *MemberOptimizerPathLayout) InitialHarnessDir(role string) string {
	return filepath.Join(l.RuntimeRoot, "initial", RoleKey(role))
}

// WriteRoleMapping 持久化可读 role → 短键映射（审计用）。
// Python: path_layout.py:66-83 write_role_mapping。
func (l *MemberOptimizerPathLayout) WriteRoleMapping(role string) error {
	roleKey := RoleKey(role)
	payload := map[string]any{"roles": map[string]any{}}
	if data, err := os.ReadFile(l.RolesMapPath()); err == nil {
		var loaded map[string]any
		if yaml.Unmarshal(data, &loaded) == nil && loaded != nil {
			payload = loaded
		}
	}
	roles, _ := payload["roles"].(map[string]any)
	if roles == nil {
		roles = map[string]any{}
		payload["roles"] = roles
	}
	roles[role] = map[string]any{
		"role_key":            roleKey,
		"current_harness_dir": l.CurrentHarnessDir(role),
	}
	if err := os.MkdirAll(filepath.Dir(l.RolesMapPath()), 0o755); err != nil {
		return err
	}
	out, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(l.RolesMapPath(), out, 0o644)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// inferRuntimeRoot 从 member_optimizations 推导工作区级短运行时根。
// Python: path_layout.py:85-99 _infer_runtime_root。
func inferRuntimeRoot(outputRoot string) string {
	memberRoot := outputRoot
	for {
		if filepath.Base(memberRoot) == "member_optimizations" {
			break
		}
		parent := filepath.Dir(memberRoot)
		if parent == memberRoot {
			memberRoot = ""
			break
		}
		memberRoot = parent
	}
	if memberRoot != "" {
		teamRoot := filepath.Dir(memberRoot)
		workspaceRoot := filepath.Dir(teamRoot)
		if filepath.Base(workspaceRoot) == "workspace" || looksLikeOrchestratorTeamRoot(teamRoot) {
			return filepath.Join(workspaceRoot, "mh")
		}
	}
	return filepath.Join(outputRoot, "mh")
}

// looksLikeOrchestratorTeamRoot 目录含编排器团队根预期子目录之一。
// Python: path_layout.py:101-112 _looks_like_orchestrator_team_root。
func looksLikeOrchestratorTeamRoot(teamRoot string) bool {
	expected := map[string]bool{"evaluations": true, "team_skills": true, "datasets": true, "checkpoints": true}
	entries, err := os.ReadDir(teamRoot)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if expected[entry.Name()] {
			return true
		}
	}
	return false
}

// hashToken sha1 前 8（空值 "default"）。
// Python: path_layout.py:114-117 _hash_token。
func hashToken(value string) string {
	if value == "" {
		value = "default"
	}
	sum := sha1.Sum([]byte(value))
	return hex.EncodeToString(sum[:])[:8]
}

// expandUserTilde ~ 展开（展开失败原样返回）。
func expandUserTilde(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, strings.TrimPrefix(path, "~"))
		}
	}
	return path
}
