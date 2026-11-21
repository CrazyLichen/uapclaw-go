// 覆盖档案内容寻址缓存与改动目录判定：
// ① sourceFingerprint 对 internal/ 与 tests/ 下全部 .go 源码做内容哈希，
// 作为缓存键的失效指纹——任一相关源码变动即整批失效（保守但正确）；
// ② changedIntegrationDirs 用 git diff/status 判定本次改动过的集成测试目录，
// 供 -only-changed 本地降级（未改动灰区跳过运行，CI 全量兜底）。

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProfileCache 覆盖档案内容寻址缓存。
// 键 = sha256(源码指纹 | tags | coverpkg | runFilter | 测试目录)，
// 命中时复用档案免重跑 go test；键含全量源码指纹，任何相关源码
// 变动都会使缓存失效，防伪语义不变。
type ProfileCache struct {
	// dir 缓存目录（不存在则惰性创建）
	dir string
}

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// sourceFingerprintFn 源码指纹函数（测试可注入替身）。
var sourceFingerprintFn = sourceFingerprint

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProfileCache 构造缓存（dir 为空返回 nil 表示禁用）。
func NewProfileCache(dir string) *ProfileCache {
	if dir == "" {
		return nil
	}
	return &ProfileCache{dir: dir}
}

// Load 读取缓存档案；未命中返回 nil。
func (c *ProfileCache) Load(key string) []byte {
	if c == nil {
		return nil
	}
	data, err := os.ReadFile(c.path(key))
	if err != nil {
		return nil
	}
	return data
}

// Store 写入缓存档案（写失败只降级为下次重跑，不报错）。
func (c *ProfileCache) Store(key string, data []byte) {
	if c == nil {
		return
	}
	if err := os.MkdirAll(c.dir, 0o755); err != nil {
		return
	}
	tmp := c.path(key) + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return
	}
	_ = os.Rename(tmp, c.path(key))
}

// CacheKey 计算内容寻址键：源码指纹 + 运行参数。
func CacheKey(fingerprint, tags, coverPkgs, runFilter, testDir string) string {
	h := sha256.Sum256([]byte(strings.Join([]string{
		fingerprint, tags, coverPkgs, runFilter, testDir,
	}, "\x00")))
	return hex.EncodeToString(h[:])
}

// sourceFingerprint 对 root 下 internal/ 与 tests/ 的全部 .go 文件做内容指纹。
// 按相对路径排序逐个哈希（路径 + 内容），任一文件增删改都会改变指纹。
func sourceFingerprint(root string) (string, error) {
	type entry struct {
		rel  string
		data []byte
	}
	var entries []entry
	dirs := []string{filepath.Join(root, "internal"), filepath.Join(root, "tests")}
	for _, dir := range dirs {
		// 目录不存在（如最小测试环境）直接跳过，不算错误
		if _, statErr := os.Stat(dir); os.IsNotExist(statErr) {
			continue
		}
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			data, readErr := os.ReadFile(path)
			if readErr != nil {
				// 并发会话可能正在改写该文件：重试一次，仍失败则跳过
				// （指纹只影响缓存失效，跳过文件下轮自然复算）
				time.Sleep(100 * time.Millisecond)
				data, readErr = os.ReadFile(path)
				if readErr != nil {
					return nil
				}
			}
			rel, relErr := filepath.Rel(root, path)
			if relErr != nil {
				return relErr
			}
			entries = append(entries, entry{rel: rel, data: data})
			return nil
		})
		if err != nil {
			return "", fmt.Errorf("扫描 %s 失败: %w", dir, err)
		}
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].rel < entries[j].rel })
	h := sha256.New()
	for _, e := range entries {
		io.WriteString(h, e.rel)
		h.Write(e.data)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// ChangedIntegrationDirs 判定本次改动过的集成测试目录。
// 来源：`git diff --name-only <base>...HEAD`（已提交改动）+
// `git status --porcelain`（工作区改动与新增文件），路径收敛到
// tests/integration 下的目录级集合（键为去掉 tests/integration/ 前缀的
// 测试根相对路径，与 DirAudit.Dir 同一命名空间）。git 不可用时返回 nil。
func ChangedIntegrationDirs(root, base string) map[string]bool {
	paths, err := gitChangedPaths(root, base)
	if err != nil {
		return nil
	}
	const prefix = "tests/integration/"
	dirs := make(map[string]bool)
	for _, p := range paths {
		clean := filepath.ToSlash(p)
		if !strings.HasPrefix(clean, prefix) {
			continue
		}
		dirs[strings.TrimPrefix(filepath.Dir(clean), prefix)] = true
	}
	return dirs
}

// DirChanged 判断灰区目录是否在改动集合内（目录本身或其子路径命中）。
// 双方统一归一化为去掉 tests/integration/ 前缀的测试根相对路径。
func DirChanged(changed map[string]bool, dir string) bool {
	norm := strings.TrimPrefix(filepath.ToSlash(dir), "tests/integration/")
	if changed[norm] {
		return true
	}
	prefix := norm + "/"
	for d := range changed {
		if strings.HasPrefix(d, prefix) {
			return true
		}
	}
	return false
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// path 缓存档案路径。
func (c *ProfileCache) path(key string) string {
	return filepath.Join(c.dir, key+".out")
}

// gitChangedPaths 收集 base...HEAD 的已提交改动路径与工作区改动路径。
func gitChangedPaths(root, base string) ([]string, error) {
	var paths []string
	if base != "" {
		out, err := exec.Command("git", "-C", root, "diff", "--name-only", base+"...HEAD").Output()
		if err != nil {
			return nil, fmt.Errorf("git diff 失败: %w", err)
		}
		paths = append(paths, strings.Fields(string(out))...)
	}
	out, err := exec.Command("git", "-C", root, "status", "--porcelain", "--untracked-files=all").Output()
	if err != nil {
		return nil, fmt.Errorf("git status 失败: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		// porcelain 格式：XY 路径（重命名行 "R  old -> new" 取 -> 后段）
		p := strings.TrimSpace(line[3:])
		if i := strings.LastIndex(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		if p != "" {
			paths = append(paths, p)
		}
	}
	return paths, nil
}
