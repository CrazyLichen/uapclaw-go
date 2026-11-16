// worktree_coordinator.go member 优化动作的隔离工作区协调。
// Python: worktree_coordinator.py（全量对齐，209 行）。
// 目录方案（feat_009 design.md §4.9）：
//   wt/{role_hash}/i                 — 每波合并后的整合区
//   wt/{role_hash}/a/{wave:x}/{action_hash} — 隔离手术区
// 动作 worker 只写自己的手术区；合并 worker 只写整合区；
// 任何 worker 不得直写 current_harnesses / current_harness_refs.yaml。

package memberoptimizer

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// MemberWorktreesDirName worktrees 目录名。
// Python: worktree_coordinator.py:20 MEMBER_WORKTREES_DIR_NAME。
const MemberWorktreesDirName = "wt"

// ──────────────────────────── 导出函数 ────────────────────────────

// RoleWorktreePath 角色的短哈希 worktree 根。
// Python: worktree_coordinator.py:54-56 role_worktree_path。
func RoleWorktreePath(worktreesDir, role string) string {
	root, _ := filepath.Abs(worktreesDir)
	return filepath.Join(root, roleWorktreeName(role))
}

// LegacyRoleWorktreePath 旧版可读角色 worktree 根（legacy 产物/测试兼容）。
// Python: worktree_coordinator.py:59-61 legacy_role_worktree_path。
func LegacyRoleWorktreePath(worktreesDir, role string) string {
	root, _ := filepath.Abs(worktreesDir)
	return filepath.Join(root, role)
}

// IntegrationWorktreePath 角色的短哈希整合区路径。
// Python: worktree_coordinator.py:64-66 integration_worktree_path。
func IntegrationWorktreePath(worktreesDir, role string) string {
	return filepath.Join(RoleWorktreePath(worktreesDir, role), "i")
}

// ResolveIntegrationWorktreePath 新整合路径，legacy 路径回退。
// Python: worktree_coordinator.py:69-77 resolve_integration_worktree_path。
func ResolveIntegrationWorktreePath(worktreesDir, role string) string {
	// Python: short_path = integration_worktree_path(...); if short_path.exists(): return
	shortPath := IntegrationWorktreePath(worktreesDir, role)
	if info, err := os.Stat(shortPath); err == nil && info.IsDir() {
		return shortPath
	}
	// Python: legacy_path = legacy_role_worktree_path(...) / "integration"; if exists: return
	legacyPath := filepath.Join(LegacyRoleWorktreePath(worktreesDir, role), "integration")
	if info, err := os.Stat(legacyPath); err == nil && info.IsDir() {
		return legacyPath
	}
	// Python: return short_path
	return shortPath
}

// ValidateWorktreePath 校验路径在角色 worktree 根内。
// 返回 True：相对、不越界、不引用共享产物目录。
// Python: worktree_coordinator.py:23-42 _validate_worktree_path。
func ValidateWorktreePath(path string, roleWorktreeRoot string) bool {
	// Python: if not path: return False
	if path == "" {
		return false
	}
	// Python: abs_path = (role_worktree_root / path).resolve(); relative_to(root) else False
	absPath := filepath.Join(roleWorktreeRoot, path)
	if abs, err := filepath.Abs(absPath); err == nil {
		absPath = abs
	}
	rootAbs, _ := filepath.Abs(roleWorktreeRoot)
	if !withinDir(rootAbs, absPath) {
		return false
	}
	// Python: forbidden = {"current_harnesses", "current_harness_refs.yaml", ".publish_tmp"}
	forbidden := map[string]bool{
		"current_harnesses": true, "current_harness_refs.yaml": true, ".publish_tmp": true,
	}
	// Python: if any(segment in forbidden for segment in Path(path).parts): return False
	for _, segment := range strings.Split(strings.ReplaceAll(path, "\\", "/"), "/") {
		if forbidden[segment] {
			return false
		}
	}
	return true
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemberWorktreeCoordinator 为并行 member 优化动作分配隔离工作区。
// Python: worktree_coordinator.py:96-99 MemberWorktreeCoordinator。
type MemberWorktreeCoordinator struct {
	// config WorktreeConfig（Python 可选配置；Go 预留槽位 ⤴️消费者接线）
	config any
}

// NewMemberWorktreeCoordinator 构造协调器（Python __init__）。
func NewMemberWorktreeCoordinator(config any) *MemberWorktreeCoordinator {
	return &MemberWorktreeCoordinator{config: config}
}

// PrepareIntegrationWorktree 通过拷贝源 harness 准备角色整合区。
// Python: worktree_coordinator.py:101-143 prepare_integration_worktree（staticmethod）。
func PrepareIntegrationWorktree(role, harnessRefPath, worktreesDir string) (string, error) {
	// Python: root = Path(worktrees_dir).expanduser().resolve()
	root, _ := filepath.Abs(worktreesDir)
	// Python: role_root = role_worktree_path(root, role); integration_dir = role_root / "i"
	roleRoot := RoleWorktreePath(root, role)
	integrationDir := filepath.Join(roleRoot, "i")
	if err := os.MkdirAll(integrationDir, 0o755); err != nil {
		return "", err
	}
	// Python: _write_role_mapping(root, role, role_root.name)
	if err := writeRoleMapping(root, role, filepath.Base(roleRoot)); err != nil {
		return "", err
	}
	// Python: source = Path(harness_ref_path)... if exists: 拷贝
	if harnessRefPath != "" {
		source := filepath.Clean(harnessRefPath)
		if abs, err := filepath.Abs(source); err == nil {
			source = abs
		}
		if info, err := os.Stat(source); err == nil {
			if info.IsDir() {
				if err := copyTreeMerge(source, integrationDir); err != nil {
					return "", err
				}
			} else {
				if err := copyFile(source, filepath.Join(integrationDir, filepath.Base(source))); err != nil {
					return "", err
				}
			}
		}
	}
	// Python: prepare_plugin_registries(integration_dir)
	if err := PreparePluginRegistries(integrationDir); err != nil {
		return "", err
	}
	return integrationDir, nil
}

// PrepareActionWorktree 通过快照整合区准备隔离手术区。
// Python: worktree_coordinator.py:145-207 prepare_action_worktree（staticmethod）。
func PrepareActionWorktree(action MemberOptimizationAction, integrationWorktree, worktreesDir string, waveIndex int) (string, error) {
	// Python: role_worktree_root = role_worktree_path(worktrees_dir, action.role)
	roleWorktreeRoot := RoleWorktreePath(worktreesDir, action.Role)
	// Python: if action.target_path: if not _validate_worktree_path: raise ValueError(...)
	if action.TargetPath != "" {
		if !ValidateWorktreePath(action.TargetPath, roleWorktreeRoot) {
			return "", fmt.Errorf("target_path '%s' is outside role worktree or references forbidden paths", action.TargetPath)
		}
	}
	// Python: action_worktree = role_root / "a" / f"{wave_index:x}" / _action_worktree_name(action.action_id)
	actionWorktree := filepath.Join(roleWorktreeRoot, "a", fmt.Sprintf("%x", waveIndex), actionWorktreeName(action.ActionID))
	if err := os.MkdirAll(actionWorktree, 0o755); err != nil {
		return "", err
	}
	// Python: if integration_worktree.exists(): 快照拷贝
	if info, err := os.Stat(integrationWorktree); err == nil && info.IsDir() {
		if err := copyTreeMerge(integrationWorktree, actionWorktree); err != nil {
			return "", err
		}
	}
	return actionWorktree, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// roleWorktreeName 角色短哈希（sha1 前 8 位）。
// Python: worktree_coordinator.py:51-53 _role_worktree_name。
func roleWorktreeName(role string) string {
	sum := sha1.Sum([]byte(role))
	return hex.EncodeToString(sum[:])[:8]
}

// actionWorktreeName 动作短哈希（sha1 前 8 位）。
// Python: worktree_coordinator.py:48-50 _action_worktree_name。
func actionWorktreeName(actionID string) string {
	sum := sha1.Sum([]byte(actionID))
	return hex.EncodeToString(sum[:])[:8]
}

// writeRoleMapping 写角色映射（roles.yaml，sort_keys=True）。
// Python: worktree_coordinator.py:80-94 _write_role_mapping。
func writeRoleMapping(root, role, roleDirName string) error {
	// Python: mapping_path = root / "roles.yaml"; payload = {"roles": {}}
	mappingPath := filepath.Join(root, "roles.yaml")
	payload := map[string]any{"roles": map[string]any{}}
	// Python: if mapping_path.is_file(): try: loaded = yaml.safe_load(...) or {}
	if content, err := os.ReadFile(mappingPath); err == nil {
		var loaded map[string]any
		if yamlErr := yaml.Unmarshal(content, &loaded); yamlErr == nil && loaded != nil {
			payload = loaded
		} else {
			payload = map[string]any{"roles": map[string]any{}}
		}
	}
	// Python: roles = payload.setdefault("roles", {}); roles[role] = role_dir_name
	roles, ok := payload["roles"].(map[string]any)
	if !ok {
		roles = map[string]any{}
		payload["roles"] = roles
	}
	roles[role] = roleDirName
	// Python: mapping_path.parent.mkdir(...); write yaml sort_keys=True
	if err := os.MkdirAll(filepath.Dir(mappingPath), 0o755); err != nil {
		return err
	}
	data, err := yaml.Marshal(payload)
	if err != nil {
		return err
	}
	return os.WriteFile(mappingPath, data, 0o644)
}

// copyTreeMerge 等价 Python shutil.copytree(src, dst, dirs_exist_ok=True)（合并拷贝）。
func copyTreeMerge(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := os.MkdirAll(dstPath, 0o755); err != nil {
				return err
			}
			if err := copyTreeMerge(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

// copyFile 等价 Python shutil.copy2（内容+权限）。
func copyFile(src, dst string) error {
	info, err := os.Stat(src)
	if err != nil {
		return err
	}
	content, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, content, info.Mode().Perm())
}
