// material_identity.go 材料身份计算：递归哈希引用配置与资产。
// Python: openjiuwen/rsi/harness_rsi/single_harness/source_evidence.py
// L23-87 子集（_IGNORED / _digest / read_mapping / _material_identity /
// _harness_identity）。自 singleharness/source_evidence.go 下沉（17.6c
// 审查拍板：解 evaluator↔singleharness 编译环，删除
// RegisterMaterialIdentity 反转缝）。

package source_evidence

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// ignoredEvidenceDirs 材料身份忽略目录（Python _IGNORED）。
var ignoredEvidenceDirs = map[string]bool{".git": true, "__pycache__": true, ".pytest_cache": true}

// envRefPattern 环境变量引用（Python ${(\w+)}）。
var envRefPattern = regexp.MustCompile(`\$\{(\w+)\}`)

// ──────────────────────────── 导出函数 ────────────────────────────

// ReadEvidenceMapping 读证据映射（Python read_mapping）。
func ReadEvidenceMapping(path string) map[string]any {
	return harnessrsi.ReadYAMLMapping(path)
}

// EvidenceDigest JSON 兼容值的 SHA-256 摘要（Python _digest）。
func EvidenceDigest(value any) string {
	encoded, err := json.Marshal(canonicalizeValue(value))
	if err != nil {
		panic(err)
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:])
}

// MaterialIdentity 递归材料身份：引用的配置/资产一并哈希（Python _material_identity）。
// 展开的凭据只进入摘要，绝不写入持久化 manifest；引用配置的环在文件哈希处终止。
func MaterialIdentity(value any, base string, seen map[string]bool) any {
	if seen == nil {
		seen = map[string]bool{}
	}
	switch v := value.(type) {
	case map[string]any:
		identity := map[string]any{}
		for key, item := range v {
			identity[key] = MaterialIdentity(item, base, seen)
		}
		return identity
	case []any:
		identity := make([]any, 0, len(v))
		for _, item := range v {
			identity = append(identity, MaterialIdentity(item, base, seen))
		}
		return identity
	}
	text, ok := value.(string)
	if !ok || text == "" {
		return value
	}
	expanded := envRefPattern.ReplaceAllStringFunc(text, func(match string) string {
		name := envRefPattern.FindStringSubmatch(match)[1]
		if env, present := os.LookupEnv(name); present {
			return env
		}
		return match
	})
	path := expandUser(expanded)
	if !filepath.IsAbs(path) {
		path = filepath.Join(base, path)
	}
	info, statErr := os.Stat(path)
	if statErr != nil || !info.Mode().IsRegular() {
		// 文本提示词与远程/容器路径不是本地文件引用
		return expanded
	}
	resolved, resErr := filepath.EvalSymlinks(path)
	if resErr != nil {
		return expanded
	}
	content, readErr := os.ReadFile(resolved)
	if readErr != nil {
		return expanded
	}
	sum := sha256.Sum256(content)
	identity := map[string]any{
		"path":   resolved,
		"sha256": hex.EncodeToString(sum[:]),
	}
	ext := strings.ToLower(filepath.Ext(resolved))
	normalized := filepath.Clean(resolved)
	if (ext == ".yaml" || ext == ".yml" || ext == ".json") && !seen[normalized] {
		marked := map[string]bool{}
		for key := range seen {
			marked[key] = true
		}
		marked[normalized] = true
		identity["references"] = MaterialIdentity(ReadEvidenceMapping(resolved), filepath.Dir(resolved), marked)
	}
	return identity
}

// HarnessIdentity harness 路径身份：目录则递归清单，否则按材料身份（Python _harness_identity）。
func HarnessIdentity(rawPath string) any {
	info, err := os.Stat(rawPath)
	if err != nil || !info.IsDir() {
		cwd, _ := os.Getwd()
		return MaterialIdentity(rawPath, cwd, nil)
	}
	// 只有不可变包递归，模型工作区/输出目录不递归
	files := map[string]any{}
	var paths []string
	walkErr := filepath.WalkDir(rawPath, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		paths = append(paths, path)
		return nil
	})
	if walkErr != nil {
		return map[string]any{"path": rawPath, "files": files}
	}
	sort.Strings(paths)
	for _, path := range paths {
		entryInfo, statErr := os.Stat(path)
		if statErr != nil || !entryInfo.Mode().IsRegular() {
			continue
		}
		relative, relErr := filepath.Rel(rawPath, path)
		if relErr != nil || relative == "." {
			continue
		}
		posix := filepath.ToSlash(relative)
		skip := false
		for _, part := range strings.Split(posix, "/") {
			if ignoredEvidenceDirs[part] {
				skip = true
				break
			}
		}
		ext := filepath.Ext(posix)
		if skip || ext == ".pyc" || ext == ".pyo" {
			continue
		}
		files[posix] = MaterialIdentity(path, rawPath, nil)
	}
	resolved, _ := filepath.EvalSymlinks(rawPath)
	if resolved == "" {
		resolved = rawPath
	}
	return map[string]any{"path": resolved, "files": files}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// canonicalizeValue 值规范化占位（Python _digest 前处理；当前恒等）。
func canonicalizeValue(value any) any {
	return value
}

// expandUser 展开 ~ 前缀（Python Path.expanduser；singleharness/promotion_policy.go
// 同款私有副本，包间不可共享私有助手，来源拆分登记）。
func expandUser(path string) string {
	if path == "~" || strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			return filepath.Join(home, path[2:])
		}
	}
	return path
}
