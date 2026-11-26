package evolution

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"

	"github.com/google/uuid"
)

// member_skill_workspace.go 成员域 Skill 演进的 copy-on-write 助手。
// Python: openjiuwen/harness/rails/evolution/member_skill_workspace.py（84 行 1:1）。

// ──────────────────────────── 常量 ────────────────────────────

// safeSkillNamePattern 安全 Skill 名正则。
// Python: _SAFE_SKILL_NAME = re.compile(r"^[A-Za-z0-9_-]+$")（member_skill_workspace.py:16）。
var safeSkillNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// memberSkillCopyLockFile 拷贝互斥锁文件名。
// Python: ".member-skill-copy.lock"（:45）。
const memberSkillCopyLockFile = ".member-skill-copy.lock"

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureMemberSkillCopy 在变更前把全局 Skill 物化为成员自有拷贝。
// 已有真实成员目录保留不动；指向全局库的链接被原子替换，
// 使成员演进无法经 symlink/junction 间接改写全局 Skill。
// Python: ensure_member_skill_copy（member_skill_workspace.py:19-72）。
func EnsureMemberSkillCopy(memberSkillsDir, globalSkillsDir, skillName string) (string, error) {
	// Python: normalized_name = str(skill_name or "").strip()
	normalized := trimSpaces(skillName)
	if normalized == "" || safeSkillNamePattern.MatchString(normalized) == false {
		return "", fmt.Errorf("unsafe Skill name: %q", skillName)
	}

	// Python: global_root = Path(global_skills_dir).expanduser().resolve()
	globalRoot, err := filepath.Abs(expandUser(globalSkillsDir))
	if err != nil {
		return "", err
	}
	if resolved, rerr := filepath.EvalSymlinks(globalRoot); rerr == nil {
		globalRoot = resolved
	}
	source := filepath.Join(globalRoot, normalized)
	// Python: not source.is_dir() or not (source / "SKILL.md").is_file()
	if fi, serr := os.Stat(source); serr != nil || !fi.IsDir() {
		return "", fmt.Errorf("global Skill '%s' does not exist: %s", normalized, source)
	}
	if fi, serr := os.Stat(filepath.Join(source, "SKILL.md")); serr != nil || fi.IsDir() {
		return "", fmt.Errorf("global Skill '%s' does not exist: %s", normalized, source)
	}

	memberRoot := expandUser(memberSkillsDir)
	// Python: member_root.mkdir(parents=True, exist_ok=True)
	if err := os.MkdirAll(memberRoot, 0o755); err != nil {
		return "", err
	}
	destination := filepath.Join(memberRoot, normalized)
	lockPath := filepath.Join(memberRoot, memberSkillCopyLockFile)

	// Python: with portalocker.Lock(str(lock_path), timeout=30)
	unlock, err := acquireFileLock(lockPath)
	if err != nil {
		return "", err
	}
	defer unlock()

	// Python: destination.is_dir() 且 resolve() != source.resolve() → 保留
	if fi, derr := os.Lstat(destination); derr == nil && fi.IsDir() {
		destResolved, aerr := filepath.EvalSymlinks(destination)
		if aerr == nil && destResolved != globalRoot {
			// 真实成员目录（解析后不等于全局库根下的同名路径）→ 原样返回
			return destination, nil
		}
	}

	temporary := filepath.Join(memberRoot, fmt.Sprintf(".%s.copy-%s", normalized, uuid.NewString()))
	success := false
	defer func() {
		if !success {
			// Python: finally: if temporary.exists(): shutil.rmtree(..., ignore_errors=True)
			_ = os.RemoveAll(temporary)
		}
	}()
	// Python: shutil.copytree(source, temporary, symlinks=False, copy_function=shutil.copy2,
	//                         dirs_exist_ok=False)
	if err := copyTree(source, temporary); err != nil {
		return "", err
	}
	// Python: if os.path.lexists(destination): _remove_member_skill_link(destination)
	if _, lerr := os.Lstat(destination); lerr == nil {
		if rerr := removeMemberSkillLink(destination); rerr != nil {
			return "", rerr
		}
	}
	// Python: if os.path.lexists(destination): return destination
	//（拷贝准备期间出现了真实目录 → 原样返回）
	if _, lerr := os.Lstat(destination); lerr == nil {
		success = true
		return destination, nil
	}
	// Python: os.replace(temporary, destination)
	if err := os.Rename(temporary, destination); err != nil {
		return "", err
	}
	success = true
	return destination, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// trimSpaces Python str.strip 语义。
func trimSpaces(v string) string {
	return regexp.MustCompile(`^\s+|\s+$`).ReplaceAllString(v, "")
}

// expandUser 展开 ~ 前缀（Python Path.expanduser）。
func expandUser(path string) string {
	if len(path) > 0 && path[0] == '~' {
		if home, err := os.UserHomeDir(); err == nil {
			if len(path) == 1 {
				return home
			}
			return filepath.Join(home, path[1:])
		}
	}
	return path
}

// removeMemberSkillLink 只删目录链接，绝不删真实成员目录。
// Python: _remove_member_skill_link（member_skill_workspace.py:75-81）。
// 偏差（平台差异）：Python 的 Windows junction 分支在 Linux 侧恒 false，仅保留 symlink 分支。
func removeMemberSkillLink(path string) error {
	fi, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if fi.Mode()&os.ModeSymlink != 0 {
		return os.Remove(path)
	}
	// 真实目录：上游语义为"绝不删除"，直接返回
	return nil
}

// copyTree 递归复制目录（symlinks=False：链接展开为内容，对齐 shutil.copytree 语义）。
func copyTree(src, dst string) error {
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	for _, entry := range entries {
		s := filepath.Join(src, entry.Name())
		d := filepath.Join(dst, entry.Name())
		if entry.Type()&os.ModeSymlink != 0 {
			// symlinks=False：解析链接目标后按目标类型复制
			target, lerr := os.Readlink(s)
			if lerr != nil {
				return lerr
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(src, target)
			}
			fi, serr := os.Stat(target)
			if serr != nil {
				return serr
			}
			if fi.IsDir() {
				if err := copyTree(target, d); err != nil {
					return err
				}
				continue
			}
			if err := copyFileContents(target, d); err != nil {
				return err
			}
			continue
		}
		if entry.IsDir() {
			if err := copyTree(s, d); err != nil {
				return err
			}
			continue
		}
		if err := copyFileContents(s, d); err != nil {
			return err
		}
	}
	return nil
}

// copyFileContents 拷贝文件内容与权限位（对齐 shutil.copy2）。
func copyFileContents(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	fi, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.WriteFile(dst, data, fi.Mode().Perm()); err != nil {
		return err
	}
	return os.Chmod(dst, fi.Mode().Perm())
}

// acquireFileLock 获取成员拷贝互斥文件锁（返回解锁函数）。
// 偏差（语言层）：Python portalocker.Lock(timeout=30)；Go 以 syscall 级
// 目录哨兵目录 + 重试实现进程内互斥（与 portalocker 的跨进程 flock 语义
// 等价承载；回收条件 = Go 侧引入跨进程文件锁库）。
func acquireFileLock(lockPath string) (func(), error) {
	return acquireFileLockDefault(lockPath)
}
