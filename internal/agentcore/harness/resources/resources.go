package resources

import (
	_ "embed"

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

// ──────────────────────────── 全局变量 ────────────────────────────

//go:embed builtin_rules.yaml
var builtinRulesYAML []byte

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseBuiltinRules 解析内置安全规则 YAML
func ParseBuiltinRules() (*BuiltinRules, error) {
	var rules BuiltinRules
	if err := yaml.Unmarshal(builtinRulesYAML, &rules); err != nil {
		return nil, err
	}
	return &rules, nil
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
