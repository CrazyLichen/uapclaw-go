package prompt

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 接口 ────────────────────────────

// Variable 模板变量接口，定义占位符变量的求值协议。
//
// 对应 Python: openjiuwen/core/foundation/prompt/assemble/variables/variable.py (Variable)
//
// 实现类型：
//   - TextableVariable — 字符串模板变量
//   - DictableVariable — 字典/列表模板变量
type Variable interface {
	// Name 返回变量名。
	Name() string

	// InputKeys 返回该变量依赖的外部输入键列表。
	InputKeys() []string

	// Value 返回最近一次 Eval/Update 的求值结果。
	Value() any

	// Eval 求值：按 InputKeys 过滤 kwargs → 调用 Update → 返回 Value。
	// 对应 Python: Variable.eval()
	Eval(kwargs map[string]any) any

	// Update 根据传入的键值对更新变量值。
	// 对应 Python: Variable.update()
	Update(kwargs map[string]any)
}

// baseVariable 提供 Variable 接口的公共字段和 Eval 模板方法实现。
// 具体变量类型嵌入此结构体，只需实现 Update 方法即可满足 Variable 接口。
type baseVariable struct {
	name      string
	inputKeys []string
	value     any // 初始值 nil；TextableVariable 存 string，DictableVariable 存 map/list
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Name 实现 Variable.Name。
func (b *baseVariable) Name() string {
	return b.name
}

// InputKeys 实现 Variable.InputKeys。
func (b *baseVariable) InputKeys() []string {
	return b.inputKeys
}

// Value 实现 Variable.Value。
func (b *baseVariable) Value() any {
	return b.value
}

// Eval 实现 Variable.Eval：模板方法，先过滤无关参数，再调 Update，最后返回 Value。
//
// 对应 Python: Variable.eval()
//
// 注意：由于 Go 嵌入结构体不支持多态调用子类方法，
// Eval 的实现需要放在各子类型中（调用自身的 Update），
// 此处提供 evalBase 辅助函数统一实现模板方法逻辑。
func (b *baseVariable) Eval(kwargs map[string]any) any {
	// baseVariable 本身不应直接被使用，此处仅提供兼容实现
	// 实际的 Eval 逻辑在各子类型中通过 evalBase 调用
	filtered := prepareInputs(b.inputKeys, kwargs)
	_ = filtered // 子类应调用 evalBase
	return b.Value()
}

// PrepareInputs 按 inputKeys 过滤无关参数，仅保留键名匹配的键值对。
// 对应 Python: Variable.prepare_inputs()（公开方法，测试可用）
func PrepareInputs(inputKeys []string, kwargs map[string]any) map[string]any {
	return prepareInputs(inputKeys, kwargs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// evalBase 提供 Eval 的模板方法逻辑，由子类型的 Eval 方法调用。
// 参数 v 是 Variable 接口，确保调用的是子类型的 Update 方法。
func evalBase(b *baseVariable, v Variable, kwargs map[string]any) any {
	filtered := prepareInputs(b.inputKeys, kwargs)
	v.Update(filtered)
	return b.Value()
}

// prepareInputs 按 inputKeys 过滤无关参数。
// 对应 Python: Variable._prepare_inputs()
func prepareInputs(inputKeys []string, kwargs map[string]any) map[string]any {
	if len(inputKeys) == 0 || len(kwargs) == 0 {
		return map[string]any{}
	}
	keySet := make(map[string]struct{}, len(inputKeys))
	for _, k := range inputKeys {
		keySet[k] = struct{}{}
	}
	result := make(map[string]any, len(keySet))
	for k, v := range kwargs {
		if _, ok := keySet[k]; ok {
			result[k] = v
		}
	}
	return result
}

// extractInputKeys 从占位符列表中提取 inputKeys（点号前第一段，去重保序）。
// 对应 Python: TextableVariable / DictableVariable 构造函数中的 input_keys 提取逻辑。
func extractInputKeys(placeholders []string) []string {
	seen := make(map[string]struct{}, len(placeholders))
	keys := make([]string, 0, len(placeholders))
	for _, p := range placeholders {
		key := p
		if idx := findDotIndex(p); idx >= 0 {
			key = p[:idx]
		}
		if _, ok := seen[key]; !ok {
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
	}
	return keys
}

// findDotIndex 返回字符串中第一个 '.' 的位置，不存在则返回 -1。
func findDotIndex(s string) int {
	for i := 0; i < len(s); i++ {
		if s[i] == '.' {
			return i
		}
	}
	return -1
}

// resolveNestedValue 解析 "user.profile.name" 形式的嵌套路径。
// 第一段从 root map 取值，后续段逐层 map 查找或 reflect struct field 访问。
//
// 对应 Python: TextableVariable.update() / DictableVariable._recursive_format() 中的嵌套属性解析逻辑。
func resolveNestedValue(path string, root map[string]any) (any, error) {
	nodes := splitDotPath(path)
	if len(nodes) == 0 {
		return nil, exception.NewBaseError(
			exception.StatusPromptAssemblerVariableInitFailed,
			exception.WithMsg(fmt.Sprintf("error parsing the placeholder `%s`: empty path", path)),
		)
	}

	// 第一段从 root map 取
	current, ok := root[nodes[0]]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusPromptAssemblerVariableInitFailed,
			exception.WithMsg(fmt.Sprintf("error parsing the placeholder `%s`: key %q not found", path, nodes[0])),
		)
	}

	// 后续段逐层解析
	for _, node := range nodes[1:] {
		val, err := accessField(current, node)
		if err != nil {
			return nil, exception.NewBaseError(
				exception.StatusPromptAssemblerVariableInitFailed,
				exception.WithMsg(fmt.Sprintf("error parsing the placeholder `%s`: %s", path, err.Error())),
			)
		}
		current = val
	}
	return current, nil
}

// splitDotPath 将 "user.profile.name" 拆分为 ["user", "profile", "name"]。
func splitDotPath(path string) []string {
	if path == "" {
		return nil
	}
	result := make([]string, 0)
	start := 0
	for i := 0; i < len(path); i++ {
		if path[i] == '.' {
			if i > start {
				result = append(result, path[start:i])
			}
			start = i + 1
		}
	}
	if start < len(path) {
		result = append(result, path[start:])
	}
	return result
}

// accessField 从一个值中按名称访问字段，优先 map 查找，fallback reflect struct field。
// 对应 Python: isinstance(value, dict) → value.get(node) else → getattr(value, node)
func accessField(current any, field string) (any, error) {
	// 优先 map 查找
	if m, ok := current.(map[string]any); ok {
		val, exists := m[field]
		if !exists {
			return nil, fmt.Errorf("key %q not found in map", field)
		}
		return val, nil
	}

	// 降级：反射结构体字段
	return accessStructField(current, field)
}
