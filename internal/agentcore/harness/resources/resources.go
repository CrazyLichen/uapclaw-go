package resources

import (
	_ "embed"
	"os"
	"path/filepath"
	"runtime"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 结构体 ────────────────────────────

// BuiltinRules 内置安全规则集合
type BuiltinRules struct {
	// Rules 规则列表
	Rules []SecurityRule `yaml:"rules"`
}

// SecurityRule 单条安全规则
type SecurityRule struct {
	// ID 规则唯一标识
	ID string `yaml:"id"`
	// Description 规则描述
	Description string `yaml:"description"`
	// Severity 严重等级
	Severity string `yaml:"severity"`
	// MatchType 匹配类型
	MatchType string `yaml:"match_type"`
	// Pattern 匹配模式
	Pattern string `yaml:"pattern"`
	// TargetTools 目标工具列表
	TargetTools []string `yaml:"tools"`
	// Action 动作（可选，如 deny）
	Action string `yaml:"action,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed builtin_rules.yaml
var builtinRulesYAML []byte

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseBuiltinRules 解析内置安全规则 YAML（从嵌入资源）。
// 用于文件系统不可用时的回退场景。
func ParseBuiltinRules() (*BuiltinRules, error) {
	var rules BuiltinRules
	if err := yaml.Unmarshal(builtinRulesYAML, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

// ParseBuiltinRulesFromFile 从文件系统解析内置安全规则 YAML。
// Python: get_builtin_security_rules() 中 with path.open(...) 读取
func ParseBuiltinRulesFromFile(filePath string) (*BuiltinRules, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, err
	}
	var rules BuiltinRules
	if err := yaml.Unmarshal(data, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
}

// ResolveBuiltinRulesYAMLPath 返回包内 builtin_rules.yaml 的文件系统路径。
// Python: _package_builtin_rules_path() — Path(__file__).resolve().parent.parent / "resources" / "builtin_rules.yaml"
// 使用 runtime.Caller 获取当前源文件位置，推导 resources 目录下的 YAML 路径。
func ResolveBuiltinRulesYAMLPath() string {
	// runtime.Caller(0) 返回当前函数所在源文件的路径
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		return ""
	}
	// filename = .../resources/resources.go → yaml 在同目录
	dir := filepath.Dir(filename)
	candidate := filepath.Join(dir, "builtin_rules.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// GetFileModTime 返回文件的修改时间（Unix 时间戳秒），文件不存在返回 -1。
// Python: path.stat().st_mtime
func GetFileModTime(filePath string) float64 {
	info, err := os.Stat(filePath)
	if err != nil {
		return -1
	}
	return float64(info.ModTime().UnixNano()) / 1e9
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsString 检查字符串切片是否包含指定字符串
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
