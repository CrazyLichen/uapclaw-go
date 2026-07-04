package tools

import (
	"fmt"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ToolMetadataProvider 工具元数据提供者接口
//
// 所有 deepagent 内置工具必须实现此接口，
// 确保提供完整的双语描述和参数 schema。
type ToolMetadataProvider interface {
	// GetName 返回工具注册名称
	GetName() string
	// GetDescription 返回指定语言的工具描述
	GetDescription(language string) string
	// GetInputParams 返回指定语言的参数 JSON Schema
	GetInputParams(language string) map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateToolMetadata 校验工具元数据的双语完整性
//
// 检查项：
//   - cn/en description 都非空
//   - cn/en schema 都是合法 object schema（有 type, properties, required）
//   - 每个参数都有 description
//   - cn/en schema 结构一致（properties key 集合相同）
//   - 递归检查嵌套 properties/items
func ValidateToolMetadata(provider ToolMetadataProvider) error {
	name := provider.GetName()
	for _, lang := range []string{"cn", "en"} {
		desc := provider.GetDescription(lang)
		if strings.TrimSpace(desc) == "" {
			return fmt.Errorf("[%s] %s description is empty", name, lang)
		}
	}

	schemas := map[string]map[string]any{
		"cn": provider.GetInputParams("cn"),
		"en": provider.GetInputParams("en"),
	}

	return validateSchemaPair(name, schemas["cn"], schemas["en"], "cn", "en", "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// validateSchemaPair 递归校验两种语言 schema 的结构一致性
func validateSchemaPair(name string, refSchema, otherSchema map[string]any, refLang, otherLang, path string) error {
	prefix := fmt.Sprintf("[%s]%s", name, path)

	if refSchema["type"] != "object" {
		return fmt.Errorf("%s %s schema type != 'object'", prefix, refLang)
	}
	if otherSchema["type"] != "object" {
		return fmt.Errorf("%s %s schema type != 'object'", prefix, otherLang)
	}
	if _, ok := refSchema["properties"]; !ok {
		return fmt.Errorf("%s %s schema missing 'properties'", prefix, refLang)
	}
	if _, ok := refSchema["required"]; !ok {
		return fmt.Errorf("%s %s schema missing 'required'", prefix, refLang)
	}

	refProps, _ := refSchema["properties"].(map[string]any)
	otherProps, _ := otherSchema["properties"].(map[string]any)

	if refProps == nil {
		refProps = map[string]any{}
	}
	if otherProps == nil {
		otherProps = map[string]any{}
	}

	// 检查 properties key 集合一致
	refKeys := keySet(refProps)
	otherKeys := keySet(otherProps)
	if !keysEqual(refKeys, otherKeys) {
		return fmt.Errorf("%s property keys differ: %s=%v, %s=%v",
			prefix, refLang, sortedKeys(refKeys), otherLang, sortedKeys(otherKeys))
	}

	for key := range refKeys {
		for _, langProps := range []struct {
			lang string
			props map[string]any
		}{
			{refLang, refProps},
			{otherLang, otherProps},
		} {
			prop, _ := langProps.props[key].(map[string]any)
			if prop == nil {
				continue
			}
			if _, ok := prop["description"]; !ok {
				return fmt.Errorf("%s.%s %s missing description", prefix, key, langProps.lang)
			}
			// 递归检查嵌套 object
			if prop["type"] == "object" {
				if nested, ok := prop["properties"]; ok {
					nestedMap, _ := nested.(map[string]any)
					if nestedMap != nil {
						// 获取另一语言对应属性
						otherProp, _ := getOtherProp(refProps, otherProps, langProps.lang, key)
						otherNested, _ := otherProp["properties"].(map[string]any)
						if otherNested == nil {
							otherNested = map[string]any{}
						}
						if err := validateSchemaPair(name, prop, otherProp, refLang, otherLang, fmt.Sprintf("%s.%s", path, key)); err != nil {
							return err
						}
					}
				}
			}
			// 递归检查 array items 中的嵌套 object
			if prop["type"] == "array" {
				if items, ok := prop["items"].(map[string]any); ok {
					if items["type"] == "object" {
						if _, hasProps := items["properties"]; hasProps {
							otherProp, _ := getOtherProp(refProps, otherProps, langProps.lang, key)
							otherItems, _ := otherProp["items"].(map[string]any)
							if otherItems == nil {
								otherItems = map[string]any{}
							}
							if err := validateSchemaPair(name, items, otherItems, refLang, otherLang, fmt.Sprintf("%s.%s[]", path, key)); err != nil {
								return err
							}
						}
					}
				}
			}
		}
	}
	return nil
}

// getOtherProp 获取另一语言对应属性的 map
func getOtherProp(refProps, otherProps map[string]any, currentLang, key string) (map[string]any, error) {
	var src map[string]any
	if currentLang == "cn" {
		src = otherProps
	} else {
		src = refProps
	}
	prop, _ := src[key].(map[string]any)
	if prop == nil {
		return map[string]any{}, nil
	}
	return prop, nil
}

// keySet 返回 map 的 key 集合
func keySet(m map[string]any) map[string]struct{} {
	result := make(map[string]struct{}, len(m))
	for k := range m {
		result[k] = struct{}{}
	}
	return result
}

// keysEqual 检查两个 key 集合是否相等
func keysEqual(a, b map[string]struct{}) bool {
	if len(a) != len(b) {
		return false
	}
	for k := range a {
		if _, ok := b[k]; !ok {
			return false
		}
	}
	return true
}

// sortedKeys 返回排序后的 key 列表
func sortedKeys(m map[string]struct{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// 简单排序
	for i := 0; i < len(keys); i++ {
		for j := i + 1; j < len(keys); j++ {
			if keys[i] > keys[j] {
				keys[i], keys[j] = keys[j], keys[i]
			}
		}
	}
	return keys
}
