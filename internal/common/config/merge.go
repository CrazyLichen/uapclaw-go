package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxMergeDepth DeepMerge 最大递归深度。
	maxMergeDepth = 4
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DeepMerge 递归合并模板配置与用户配置。
//
// 规则：
//   - 模板有用户无 → 添加（新配置项）
//   - 双方都有 → 保留用户值
//   - 用户有模板无 → 保留（不删除用户自定义项，比 Python 版更安全）
//   - 当双方值都是 map 时，递归合并
//   - maxDepth <= 0 时使用默认值 4
func DeepMerge(tmpl, user map[string]any, maxDepth int) map[string]any {
	if maxDepth <= 0 {
		maxDepth = maxMergeDepth
	}
	return deepMerge(tmpl, user, maxDepth)
}

// MigrateFromTemplate 同步用户配置与模板结构。
//
// 读取模板文件和用户文件，执行 DeepMerge，将合并结果写回用户文件。
// 返回 (是否发生了变更, error)。
// 如果用户文件不存在，则从模板创建。
func MigrateFromTemplate(tmplPath, userPath string) (bool, error) {
	// 读取模板
	tmplData, err := readYAMLFile(tmplPath)
	if err != nil {
		return false, fmt.Errorf("读取模板配置失败: %w", err)
	}

	// 读取用户配置（不存在则为空）
	userData, err := readYAMLFile(userPath)
	if err != nil {
		if os.IsNotExist(err) {
			// 用户配置不存在，从模板创建
			merged := deepCopyMap(tmplData)
			if err := writeYAMLFile(userPath, merged); err != nil {
				return true, fmt.Errorf("创建用户配置失败: %w", err)
			}
			return true, nil
		}
		return false, fmt.Errorf("读取用户配置失败: %w", err)
	}

	// 合并
	merged := DeepMerge(tmplData, userData, maxMergeDepth)

	// 检查是否发生了变更
	changed := !mapsEqual(userData, merged)

	if changed {
		if err := writeYAMLFile(userPath, merged); err != nil {
			return false, fmt.Errorf("写入用户配置失败: %w", err)
		}
	}

	return changed, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepMerge 递归合并实现。
func deepMerge(tmpl, user map[string]any, depth int) map[string]any {
	if depth <= 0 {
		return deepCopyMap(user)
	}

	result := make(map[string]any)

	// 添加模板中有的所有键
	for key, tmplVal := range tmpl {
		userVal, userHas := user[key]
		if !userHas {
			// 模板有用户无 → 添加
			result[key] = deepCopyValue(tmplVal)
			continue
		}

		// 双方都有
		tmplMap, tmplIsMap := tmplVal.(map[string]any)
		userMap, userIsMap := userVal.(map[string]any)

		if tmplIsMap && userIsMap {
			// 双方都是 map → 递归合并
			result[key] = deepMerge(tmplMap, userMap, depth-1)
		} else {
			// 保留用户值
			result[key] = deepCopyValue(userVal)
		}
	}

	// 保留用户有但模板没有的键（不删除用户自定义项）
	for key, userVal := range user {
		if _, tmplHas := tmpl[key]; !tmplHas {
			result[key] = deepCopyValue(userVal)
		}
	}

	return result
}

// deepCopyValue 深拷贝任意值。
func deepCopyValue(v any) any {
	switch val := v.(type) {
	case map[string]any:
		return deepCopyMap(val)
	case []any:
		return deepCopySlice(val)
	default:
		return v
	}
}

// mapsEqual 判断两个 map 是否相等。
func mapsEqual(a, b map[string]any) bool {
	if len(a) != len(b) {
		return false
	}
	for key, aVal := range a {
		bVal, ok := b[key]
		if !ok {
			return false
		}
		// 递归比较
		aMap, aIsMap := aVal.(map[string]any)
		bMap, bIsMap := bVal.(map[string]any)
		if aIsMap && bIsMap {
			if !mapsEqual(aMap, bMap) {
				return false
			}
			continue
		}
		// 简单值比较
		if aVal != bVal {
			// 处理 float64 和 int 的比较（YAML 解析后的差异）
			aFloat, aIsFloat := toFloat64(aVal)
			bFloat, bIsFloat := toFloat64(bVal)
			if aIsFloat && bIsFloat && aFloat == bFloat {
				continue
			}
			return false
		}
	}
	return true
}

// toFloat64 尝试将值转换为 float64。
func toFloat64(v any) (float64, bool) {
	switch val := v.(type) {
	case int:
		return float64(val), true
	case int64:
		return float64(val), true
	case float64:
		return val, true
	default:
		return 0, false
	}
}

// readYAMLFile 读取 YAML 文件并反序列化为 map[string]any。
func readYAMLFile(path string) (map[string]any, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var data map[string]any
	if err := yaml.Unmarshal(content, &data); err != nil {
		return nil, err
	}

	if data == nil {
		data = make(map[string]any)
	}

	return data, nil
}

// writeYAMLFile 将 map[string]any 序列化并写入 YAML 文件。
func writeYAMLFile(path string, data map[string]any) error {
	content, err := yaml.Marshal(data)
	if err != nil {
		return err
	}
	return os.WriteFile(path, content, 0o644)
}
