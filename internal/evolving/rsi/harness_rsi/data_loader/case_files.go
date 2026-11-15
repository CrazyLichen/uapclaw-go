package dataloader

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// inputFields 显式输入别名（对齐 INPUT_FIELDS；绝不暴露整个 case）
var inputFields = []string{"input", "inputs", "task_input", "query", "prompt", "question"}

// ──────────────────────────── 导出函数 ────────────────────────────

// TaskInput 读取用例任务输入，保留 legacy 显式别名（对齐 task_input）
func TaskInput(caseMapping map[string]any) (any, error) {
	caseID, _ := caseMapping["case_id"].(string)
	for _, key := range inputFields {
		raw, exists := caseMapping[key]
		if !exists {
			continue
		}
		value := raw
		// legacy：input 为 {"user_message": ...} 对象时展开
		if key == "input" {
			if mapping, ok := value.(map[string]any); ok && len(mapping) == 1 {
				if message, hasKey := mapping["user_message"]; hasKey {
					value = message
				}
			}
		}
		if value == nil {
			return nil, fmt.Errorf("case %s: %s must not be empty", caseID, key)
		}
		if str, isStr := value.(string); isStr && strings.TrimSpace(str) == "" {
			return nil, fmt.Errorf("case %s: %s must not be empty", caseID, key)
		}
		return value, nil
	}
	return nil, fmt.Errorf("case %s: input is required", caseID)
}

// ValidateCaseFields 校验公共字段并保留既有基准元数据（对齐 validate_case_fields）
func ValidateCaseFields(caseMapping map[string]any, requireInput bool) error {
	caseID, isStr := caseMapping["case_id"].(string)
	if requireInput && (!isStr || strings.TrimSpace(caseID) == "") {
		return fmt.Errorf("case_id must be a non-empty string")
	}
	hasAnyInput := false
	for _, key := range inputFields {
		if _, exists := caseMapping[key]; exists {
			hasAnyInput = true
			break
		}
	}
	if requireInput || hasAnyInput {
		value, err := TaskInput(caseMapping)
		if err != nil {
			return err
		}
		valid := false
		switch typed := value.(type) {
		case string:
			valid = typed != ""
		case map[string]any:
			valid = len(typed) > 0
		}
		if !valid {
			return fmt.Errorf("input must be a non-empty string or a legacy input object")
		}
	}
	if _, exists := caseMapping["assets"]; exists {
		if _, err := stringListField(caseMapping["assets"], "assets"); err != nil {
			return err
		}
	}
	reference, hasReference := caseMapping["reference"]
	if hasReference && reference != nil {
		if _, isMap := reference.(map[string]any); !isMap {
			return fmt.Errorf("reference must be an object")
		}
	}
	referenceMap := mappingField(caseMapping, "reference")
	if raw, exists := referenceMap["rubric"]; exists {
		if _, err := stringListField(raw, "reference.rubric"); err != nil {
			return err
		}
	}
	if raw, exists := referenceMap["files"]; exists {
		if _, err := stringListField(raw, "reference.files"); err != nil {
			return err
		}
	}
	return nil
}

// ResolveDatasetFile 拒绝绝对路径、穿越、ADS 与逃逸链接（对齐 resolve_dataset_file）
func ResolveDatasetFile(base, value string) (string, error) {
	normalized := strings.ReplaceAll(value, `\`, "/")
	if normalized == "" || filepath.IsAbs(normalized) || hasWindowsDrive(value) {
		return "", fmt.Errorf("dataset file must be a relative path within the dataset: %s", value)
	}
	if strings.Contains(normalized, ":") {
		return "", fmt.Errorf("dataset file must be a relative path within the dataset: %s", value)
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return "", fmt.Errorf("dataset file must be a relative path within the dataset: %s", value)
		}
	}
	root, err := filepath.Abs(base)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.Abs(filepath.Join(root, normalized))
	if err != nil {
		return "", err
	}
	if !pathWithinDir(resolved, root) {
		return "", fmt.Errorf("dataset file escapes dataset directory: %s", value)
	}
	if _, err := os.Stat(resolved); os.IsNotExist(err) {
		return "", fmt.Errorf("dataset file not found: %s", value)
	}
	return resolved, nil
}

// ReferencedFiles 枚举用例声明的资产与引用文件（对齐 referenced_files）
func ReferencedFiles(caseMapping map[string]any, base string) ([][2]string, []string, error) {
	reference := mappingField(caseMapping, "reference")
	paths := []string{}
	pairs := [][2]string{}
	entries := []struct {
		kind  string
		value any
	}{
		{"assets", caseMapping["assets"]},
		{"reference", reference["files"]},
	}
	for _, entry := range entries {
		if entry.value == nil {
			continue
		}
		values, err := stringListField(entry.value, entry.kind)
		if err != nil {
			return nil, nil, err
		}
		for _, value := range values {
			resolved, err := ResolveDatasetFile(base, value)
			if err != nil {
				return nil, nil, err
			}
			pairs = append(pairs, [2]string{entry.kind, strings.ReplaceAll(value, `\`, "/")})
			paths = append(paths, resolved)
		}
	}
	return pairs, paths, nil
}

// ValidateDatasetFiles 校验用例清单与声明文件（对齐 validate_dataset_files）
func ValidateDatasetFiles(cases []map[string]any, base string, requireInput bool) error {
	publicPaths := map[string]bool{}
	privatePaths := map[string]bool{}
	for _, caseMapping := range cases {
		if err := ValidateCaseFields(caseMapping, requireInput); err != nil {
			return err
		}
		pairs, paths, err := ReferencedFiles(caseMapping, base)
		if err != nil {
			return err
		}
		for index, pair := range pairs {
			if pair[0] == "assets" {
				publicPaths[paths[index]] = true
			} else {
				privatePaths[paths[index]] = true
			}
		}
	}
	for path := range publicPaths {
		if privatePaths[path] {
			return fmt.Errorf("a private reference file must not also appear in assets")
		}
	}
	return nil
}

// FileFingerprint 用例声明文件的 sha256 指纹（对齐 file_fingerprint）
func FileFingerprint(caseMapping map[string]any) (map[string]string, error) {
	_, hasAssets := caseMapping["assets"]
	reference := mappingField(caseMapping, "reference")
	_, hasRefFiles := reference["files"]
	if !hasAssets && !hasRefFiles {
		return map[string]string{}, nil
	}
	casePath, _ := caseMapping["case_path"].(string)
	if casePath == "" {
		return nil, fmt.Errorf("case_path is required to resolve dataset files")
	}
	base := filepath.Dir(casePath)
	pairs, paths, err := ReferencedFiles(caseMapping, base)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	for index, pair := range pairs {
		digest, err := fileSHA256(paths[index])
		if err != nil {
			return nil, err
		}
		out[pair[0]+"/"+pair[1]] = digest
	}
	return out, nil
}

// CopyDatasetFiles 快照仅声明文件，不复制整个数据集目录（对齐 copy_dataset_files）
func CopyDatasetFiles(cases []map[string]any, source, target string) (map[string]string, error) {
	if err := ValidateDatasetFiles(cases, filepath.Dir(source), true); err != nil {
		return nil, err
	}
	copied := map[string]string{}
	targetRoot, err := filepath.Abs(filepath.Dir(target))
	if err != nil {
		return nil, err
	}
	for _, caseMapping := range cases {
		pairs, paths, err := ReferencedFiles(caseMapping, filepath.Dir(source))
		if err != nil {
			return nil, err
		}
		for index, pair := range pairs {
			relative := pair[1]
			origin := paths[index]
			destination, err := filepath.Abs(filepath.Join(targetRoot, relative))
			if err != nil {
				return nil, err
			}
			if !pathWithinDir(destination, targetRoot) || destination == target {
				return nil, fmt.Errorf("dataset file collides with snapshot or escapes its directory: %s", relative)
			}
			if _, exists := copied[relative]; exists {
				continue
			}
			originDigest, err := fileSHA256(origin)
			if err != nil {
				return nil, err
			}
			if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
				return nil, err
			}
			if err := copyFile(origin, destination); err != nil {
				return nil, err
			}
			copiedDigest, err := fileSHA256(destination)
			if err != nil {
				return nil, err
			}
			copied[relative] = copiedDigest
			if copied[relative] != originDigest {
				return nil, fmt.Errorf("dataset file changed during snapshot: %s", relative)
			}
		}
	}
	return copied, nil
}

// CopyPublicAssets 复制用例公开资产到工作区（对齐 copy_public_assets）
func CopyPublicAssets(caseMapping map[string]any, workspace string) ([][2]string, error) {
	if _, hasAssets := caseMapping["assets"]; !hasAssets {
		return nil, nil
	}
	casePath, _ := caseMapping["case_path"].(string)
	if casePath == "" {
		return nil, fmt.Errorf("case_path is required to resolve assets")
	}
	base := filepath.Dir(casePath)
	pairs, paths, err := ReferencedFiles(caseMapping, base)
	if err != nil {
		return nil, err
	}
	workspaceRoot, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	copied := [][2]string{}
	for index, pair := range pairs {
		if pair[0] != "assets" {
			continue
		}
		destination, err := filepath.Abs(filepath.Join(workspaceRoot, pair[1]))
		if err != nil {
			return nil, err
		}
		if !pathWithinDir(destination, workspaceRoot) {
			return nil, fmt.Errorf("asset destination escapes workspace: %s", pair[1])
		}
		if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
			return nil, err
		}
		if err := copyFile(paths[index], destination); err != nil {
			return nil, err
		}
		copied = append(copied, [2]string{pair[1], destination})
	}
	return copied, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mappingField 取映射字段；缺失或非映射返回 nil（调用方判 reference must be an object）
func mappingField(caseMapping map[string]any, key string) map[string]any {
	if raw, exists := caseMapping[key]; exists {
		if mapping, ok := raw.(map[string]any); ok {
			return mapping
		}
	}
	return nil
}

// stringListField 非空字符串列表校验（对齐 _string_list）
func stringListField(value any, field string) ([]string, error) {
	list, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("%s must be a list of non-empty strings", field)
	}
	out := make([]string, 0, len(list))
	for _, item := range list {
		str, isStr := item.(string)
		if !isStr || strings.TrimSpace(str) == "" {
			return nil, fmt.Errorf("%s must be a list of non-empty strings", field)
		}
		out = append(out, str)
	}
	return out, nil
}

// hasWindowsDrive 判断 Windows 盘符前缀（对齐 PureWindowsPath(value).drive：
// 平台无关，Linux 上同样识别盘符/UNC）
func hasWindowsDrive(value string) bool {
	if len(value) >= 2 && value[1] == ':' && isDriveLetter(value[0]) {
		return true
	}
	// UNC 前缀也算 drive
	return strings.HasPrefix(value, `\\`)
}

// isDriveLetter 判断是否盘符字母
func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// pathWithinDir 判断路径是否位于 root 内（对齐 is_relative_to）
func pathWithinDir(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// fileSHA256 文件内容 sha256（对齐 _file_sha256）
func fileSHA256(path string) (string, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(raw)), nil
}

// copyFile 复制文件（shutil.copy2 等价：内容+权限）
func copyFile(origin, destination string) error {
	raw, err := os.ReadFile(origin)
	if err != nil {
		return err
	}
	info, err := os.Stat(origin)
	mode := fs.FileMode(0o644)
	if err == nil {
		mode = info.Mode()
	}
	return os.WriteFile(destination, raw, mode)
}
