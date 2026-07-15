package context_engineer

import (
	"fmt"
	"reflect"
	"strings"
	"unicode"

	iface "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/interface"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// MergeConfigWithOverrides 将 map[string]any 的部分覆盖合并到 baseConfig 的深拷贝上。
//
// 逻辑对齐 Python: ContextProcessorRail._merge_config_with_overrides(base_config, overrides)。
// Python 使用 pydantic model_dump + dict merge + type(**merged) 方式；
// Go 使用 reflect 实现等价逻辑：
//  1. 对 baseConfig 做深拷贝
//  2. 遍历 overrides，将 snake_case 键转为 PascalCase 匹配 struct field
//  3. 设置匹配到的字段值
//  4. 特殊处理 Model/ModelClient 字段（与 Python hasattr("model") 对齐）
//
// 如果 overrides 为空，直接返回 baseConfig 本身（不做拷贝，与 Python 行为一致）。
func MergeConfigWithOverrides(baseConfig iface.ProcessorConfig, overrides map[string]any) iface.ProcessorConfig {
	if len(overrides) == 0 {
		return baseConfig
	}

	// 深拷贝 baseConfig
	copied := deepCopyConfig(baseConfig)

	// 获取 reflect.Value
	v := reflect.ValueOf(copied)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		// 非 struct 类型（如 nil interface），直接返回 baseConfig
		return baseConfig
	}

	t := v.Type()

	// 构建 field name 索引（PascalCase → field index）
	fieldIndexMap := make(map[string]int)
	for i := 0; i < t.NumField(); i++ {
		fieldIndexMap[t.Field(i).Name] = i
	}

	// 遍历 overrides，设置匹配字段
	for key, val := range overrides {
		// snake_case → PascalCase
		goName := snakeToPascal(key)

		idx, ok := fieldIndexMap[goName]
		if !ok {
			// 字段不存在，跳过（对齐 Python：model_dump 后 dict merge 不报错）
			continue
		}

		field := v.Field(idx)
		if !field.CanSet() {
			continue
		}

		setFieldValue(field, val)
	}

	return copied
}

// MergeProcessors 合并 preset 和 user processor 列表。
//
// 逻辑对齐 Python: ContextProcessorRail._merge_processors(base, overrides, model_config, model_client_config)。
// 1. 将 overrides 列表转为 map（key=processor type）
// 2. 遍历 base 列表：
//   - 如果 key 在 override_map 中，且 override 是 dict → MergeConfigWithOverrides 合并
//   - 如果 override 是完整 ProcessorConfig → 直接替换
//   - 否则保留 base
//
// 3. 遍历 overrides 列表中不在 base 的项，追加
// 4. 对合并后的 Config，如果 Model/ModelClient 为 nil，回填 modelConfig/modelClientConfig
func MergeProcessors(
	base []iface.ProcessorSpec,
	overrides []iface.ProcessorSpec,
	modelConfig any,
	modelClientConfig any,
) []iface.ProcessorSpec {
	// 构建 override map
	overrideMap := make(map[string]iface.ProcessorSpec, len(overrides))
	for _, spec := range overrides {
		overrideMap[spec.Type] = spec
	}

	// 记录 base 中被 override 命中的 key
	baseOverrideKeys := make(map[string]bool)

	// 构建合并结果
	var result []iface.ProcessorSpec

	for _, baseSpec := range base {
		if overrideSpec, ok := overrideMap[baseSpec.Type]; ok {
			baseOverrideKeys[baseSpec.Type] = true
			mergedCfg := buildMergedConfig(baseSpec.Type, baseSpec.Config, overrideSpec, modelConfig, modelClientConfig)
			result = append(result, iface.ProcessorSpec{
				Type:           baseSpec.Type,
				Config:         mergedCfg,
				ConfigOverrides: overrideSpec.ConfigOverrides,
			})
		} else {
			result = append(result, baseSpec)
		}
	}

	// 追加不在 base 中的 override 项
	for _, overrideSpec := range overrides {
		if !baseOverrideKeys[overrideSpec.Type] {
			mergedCfg := buildMergedConfig(overrideSpec.Type, nil, overrideSpec, modelConfig, modelClientConfig)
			result = append(result, iface.ProcessorSpec{
				Type:           overrideSpec.Type,
				Config:         mergedCfg,
				ConfigOverrides: overrideSpec.ConfigOverrides,
			})
		}
	}

	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildMergedConfig 构建单个 processor 的合并配置。
//
// 对齐 Python: _build_merged_cfg(key, override_cfg, base_cfg, model_config, model_client_config)
func buildMergedConfig(
	key string,
	baseCfg iface.ProcessorConfig,
	overrideSpec iface.ProcessorSpec,
	modelConfig any,
	modelClientConfig any,
) iface.ProcessorConfig {
	var mergedCfg iface.ProcessorConfig

	if baseCfg != nil {
		if len(overrideSpec.ConfigOverrides) > 0 {
			// dict 级别的部分覆盖
			mergedCfg = MergeConfigWithOverrides(baseCfg, overrideSpec.ConfigOverrides)
		} else if overrideSpec.Config != nil {
			// 完整配置替换
			mergedCfg = overrideSpec.Config
		} else {
			// 无覆盖，保留 base
			mergedCfg = baseCfg
		}
	} else {
		// 无 base config
		if len(overrideSpec.ConfigOverrides) > 0 {
			// Python 会 raise ValueError，Go 中用 panic 表达同样的约束
			panic(fmt.Sprintf(
				"Processor '%s' does not exist in preset and cannot create config from dict. "+
					"Please ensure this processor is included in the preset, "+
					"or pass a complete ProcessorConfig object.", key))
		}
		mergedCfg = overrideSpec.Config
	}

	// 回填 Model/ModelClient（对齐 Python: hasattr(merged_cfg, "model") 检查）
	if mergedCfg != nil {
		fillModelDefaults(mergedCfg, modelConfig, modelClientConfig)
	}

	return mergedCfg
}

// fillModelDefaults 对合并后的 Config 回填 Model/ModelClient 字段。
//
// 对齐 Python:
//
//	if hasattr(merged_cfg, "model") and getattr(merged_cfg, "model", None) is None:
//	    merged_cfg.model = model_config
//	if hasattr(merged_cfg, "model_client") and getattr(merged_cfg, "model_client", None) is None:
//	    merged_cfg.model_client = model_client_config
func fillModelDefaults(cfg iface.ProcessorConfig, modelConfig any, modelClientConfig any) {
	v := reflect.ValueOf(cfg)
	if v.Kind() == reflect.Pointer {
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return
	}

	// 回填 Model 字段
	if modelConfig != nil {
		if f := v.FieldByName("Model"); f.IsValid() && f.CanSet() && f.IsNil() {
			setFieldValue(f, modelConfig)
		}
	}

	// 回填 ModelClient 字段
	if modelClientConfig != nil {
		if f := v.FieldByName("ModelClient"); f.IsValid() && f.CanSet() && f.IsNil() {
			setFieldValue(f, modelClientConfig)
		}
	}
}

// deepCopyConfig 对 ProcessorConfig 做深拷贝。
//
// ProcessorConfig 是 interface，实际值可能是 *XxxConfig 指针。
// 通过 reflect 创建新的零值指针，再逐字段拷贝。
func deepCopyConfig(cfg iface.ProcessorConfig) iface.ProcessorConfig {
	if cfg == nil {
		return nil
	}

	v := reflect.ValueOf(cfg)
	if v.Kind() != reflect.Pointer {
		// 非 pointer 类型，直接返回（值类型本身就是 copy）
		return cfg
	}

	// 创建新的指针
	newPtr := reflect.New(v.Elem().Type())

	// 逐字段深拷贝
	src := v.Elem()
	dst := newPtr.Elem()
	for i := 0; i < src.NumField(); i++ {
		srcField := src.Field(i)
		dstField := dst.Field(i)
		if dstField.CanSet() {
			dstField.Set(srcField)
		}
	}

	return newPtr.Interface().(iface.ProcessorConfig)
}

// snakeToPascal 将 snake_case 字符串转换为 PascalCase。
//
// 示例: "tokens_threshold" → "TokensThreshold", "model_client" → "ModelClient"
func snakeToPascal(s string) string {
	parts := strings.Split(s, "_")
	for i, part := range parts {
		if len(part) > 0 {
			parts[i] = string(unicode.ToUpper(rune(part[0]))) + part[1:]
		}
	}
	return strings.Join(parts, "")
}

// setFieldValue 通过 reflect 设置字段值，处理类型兼容性。
func setFieldValue(field reflect.Value, val interface{}) {
	if !field.CanSet() {
		return
	}

	valReflect := reflect.ValueOf(val)

	// 直接赋值（类型完全匹配）
	if valReflect.Type().AssignableTo(field.Type()) {
		field.Set(valReflect)
		return
	}

	// 处理指针字段：如果 field 是 *T 且 val 是 T，则创建 *T
	if field.Kind() == reflect.Pointer {
		targetType := field.Type().Elem()
		if valReflect.Type().AssignableTo(targetType) {
			ptr := reflect.New(targetType)
			ptr.Elem().Set(valReflect)
			field.Set(ptr)
			return
		}
		// val 也是指针但类型不完全匹配 → 尝试直接赋值
		if valReflect.Kind() == reflect.Pointer && valReflect.Type().Elem().AssignableTo(targetType) {
			field.Set(valReflect)
			return
		}
	}

	// 处理值字段：如果 field 是 T 且 val 是 *T，则解引用
	if field.Kind() != reflect.Pointer && valReflect.Kind() == reflect.Pointer {
		if !valReflect.IsNil() {
			elem := valReflect.Elem()
			if elem.Type().AssignableTo(field.Type()) {
				field.Set(elem)
				return
			}
		}
	}

	// 数值类型兼容（如 int ↔ float64）
	if field.Kind() == reflect.Int && valReflect.Kind() == reflect.Float64 {
		field.SetInt(int64(valReflect.Float()))
		return
	}
	if field.Kind() == reflect.Float64 && valReflect.Kind() == reflect.Int {
		field.SetFloat(float64(valReflect.Int()))
		return
	}
	if field.Kind() == reflect.Bool && valReflect.Kind() == reflect.Bool {
		field.SetBool(valReflect.Bool())
		return
	}

	// slice 类型
	if field.Kind() == reflect.Slice && valReflect.Kind() == reflect.Slice {
		if valReflect.Type().Elem().AssignableTo(field.Type().Elem()) {
			field.Set(valReflect)
			return
		}
		// 尝试逐元素转换 []any → []string 等
		if valReflect.Type().Elem().Kind() == reflect.Interface {
			newSlice := reflect.MakeSlice(field.Type(), valReflect.Len(), valReflect.Len())
			for i := 0; i < valReflect.Len(); i++ {
				elem := valReflect.Index(i).Elem()
				if elem.Type().AssignableTo(field.Type().Elem()) {
					newSlice.Index(i).Set(elem)
				}
			}
			field.Set(newSlice)
			return
		}
	}
}
