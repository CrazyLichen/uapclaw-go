package harnessrsi

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

// RuntimePolicyFile 候选包运行时策略文件名（对齐 RUNTIME_POLICY_FILE）
const RuntimePolicyFile = "runtime.yaml"

// runtimePolicyKeys 支持的配置键白名单（对齐 RUNTIME_POLICY_KEYS）
var runtimePolicyKeys = map[string]bool{"max_iterations": true, "on_max_iterations": true}

// iterationLimitStrategies 迭代上限策略（对齐 ITERATION_LIMIT_STRATEGIES）
var iterationLimitStrategies = []string{"error", "return_observations"}

// runtimePolicyMaxBytes 配置文件大小上限（16 KiB）
const runtimePolicyMaxBytes = 16384

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessRuntimePolicy 生效配置；宿主上限不可被候选更改（对齐 HarnessRuntimePolicy）
type HarnessRuntimePolicy struct {
	// MaxIterations 最大迭代次数
	MaxIterations int `json:"max_iterations" yaml:"max_iterations"`
	// OnMaxIterations 到达上限的策略（error / return_observations）
	OnMaxIterations string `json:"on_max_iterations" yaml:"on_max_iterations"`
	// MaxIterationsHardLimit 宿主硬上限
	MaxIterationsHardLimit int `json:"max_iterations_hard_limit" yaml:"max_iterations_hard_limit"`
	// Source 配置来源（evaluator_default / runtime.yaml）
	Source string `json:"source" yaml:"source"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateRuntimePolicy 只校验两个受支持的包配置字段（对齐 validate_runtime_policy）
func ValidateRuntimePolicy(data any, hardLimit int) (map[string]any, error) {
	mapping, ok := data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("runtime.yaml must contain a mapping")
	}
	var unknown []string
	for key := range mapping {
		if !runtimePolicyKeys[key] {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		return nil, fmt.Errorf("runtime.yaml contains unsupported keys: %v", unknown)
	}
	if hardLimit < 1 {
		return nil, fmt.Errorf("solver_max_iterations_hard_limit must be a positive integer")
	}
	if raw, exists := mapping["max_iterations"]; exists {
		value, isInt := toIntStrict(raw)
		if !isInt || value < 1 || value > hardLimit {
			return nil, fmt.Errorf("runtime.yaml.max_iterations must be an integer between 1 and %d", hardLimit)
		}
	}
	if raw, exists := mapping["on_max_iterations"]; exists {
		value, _ := toStr(raw)
		valid := false
		for _, strategy := range iterationLimitStrategies {
			if value == strategy {
				valid = true
				break
			}
		}
		if !valid {
			return nil, fmt.Errorf("runtime.yaml.on_max_iterations must be one of %v", iterationLimitStrategies)
		}
	}
	return copyMapping(mapping), nil
}

// ReadRuntimePolicy 读取包策略，不加载插件、代码或凭据（对齐 read_runtime_policy）
func ReadRuntimePolicy(harnessPath string, defaultMaxIterations int, defaultOnMaxIterations string, hardLimit int) (HarnessRuntimePolicy, error) {
	root, err := filepath.Abs(expandUser(harnessPath))
	if err != nil {
		return HarnessRuntimePolicy{}, err
	}
	if info, err := os.Stat(root); err == nil && !info.IsDir() {
		root = filepath.Dir(root)
	}
	values := map[string]any{
		"max_iterations":     defaultMaxIterations,
		"on_max_iterations":  defaultOnMaxIterations,
	}
	source := "evaluator_default"
	policyPath := filepath.Join(root, RuntimePolicyFile)
	if lstatInfo, statErr := os.Lstat(policyPath); statErr == nil {
		// symlink 或越界文件拒绝（对齐 policy_path.is_symlink() / is_relative_to(root)）
		if lstatInfo.Mode()&os.ModeSymlink != 0 || !isWithinDir(policyPath, root) {
			return HarnessRuntimePolicy{}, fmt.Errorf("runtime.yaml must be a package-local regular file")
		}
		if lstatInfo.Size() > runtimePolicyMaxBytes {
			return HarnessRuntimePolicy{}, fmt.Errorf("runtime.yaml exceeds the 16 KiB configuration limit")
		}
		raw, err := os.ReadFile(policyPath)
		if err != nil {
			return HarnessRuntimePolicy{}, err
		}
		var loaded any
		if err := yaml.Unmarshal(raw, &loaded); err != nil {
			return HarnessRuntimePolicy{}, err
		}
		overrides, err := ValidateRuntimePolicy(loaded, hardLimit)
		if err != nil {
			return HarnessRuntimePolicy{}, err
		}
		for key, value := range overrides {
			values[key] = value
		}
		source = RuntimePolicyFile
	}
	if _, err := ValidateRuntimePolicy(values, hardLimit); err != nil {
		return HarnessRuntimePolicy{}, err
	}
	maxIterations, _ := toIntStrict(values["max_iterations"])
	onMaxIterations, _ := toStr(values["on_max_iterations"])
	return HarnessRuntimePolicy{
		MaxIterations:          maxIterations,
		OnMaxIterations:        onMaxIterations,
		MaxIterationsHardLimit: hardLimit,
		Source:                 source,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// toIntStrict 仅当值是整数（非 bool）时返回真（对齐 Python type(value) is not int 判定）
func toIntStrict(raw any) (int, bool) {
	if _, isBool := raw.(bool); isBool {
		return 0, false
	}
	switch typed := raw.(type) {
	case int:
		return typed, true
	case int64:
		return int(typed), true
	case float64:
		return int(typed), true
	default:
		return 0, false
	}
}

// toStr 字符串归一
func toStr(raw any) (string, bool) {
	switch typed := raw.(type) {
	case string:
		return typed, true
	default:
		return fmt.Sprintf("%v", raw), false
	}
}

// copyMapping 浅拷贝映射
func copyMapping(src map[string]any) map[string]any {
	out := make(map[string]any, len(src))
	for key, value := range src {
		out[key] = value
	}
	return out
}

// isWithinDir 判断路径是否位于 root 目录内（对齐 Path.is_relative_to(root)）
func isWithinDir(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
