package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// ConstructionInput harness 元素的"双源构造输入"契约。
//
// 一个 rail / tool 的构造参数来自两个命名空间：
//   - params：spec 里写的可序列化配置（能被 checkpoint 持久化）
//   - context：运行时活句柄与解析后的事实（不可序列化）
//
// 上游用 pydantic 的 model_fields + json_schema_extra 反射式取值：
//
//	values = {}
//	for name, field in cls.model_fields.items():
//	    source = field.json_schema_extra.get("source")
//	    if source == "params":
//	        if params and name in params: values[name] = params[name]
//	    elif "context_attr" in extra:
//	        values[name] = getattr(context, extra["context_attr"], None)
//	return cls(**{k: v for k, v in values.items() if v is not None})
//
// Go 无 pydantic 反射式构造，改为 tag 声明来源 + 手写 Resolve
// （偏差 D-3）。手写而非 reflect：本项目对 reflect 持警惕（历史事故），
// 且收益只是省掉每个 input 约 20 行。
//
// 关键语义（每个实现都必须遵守）：**取不到就让字段默认值生效**，
// 而不是留零值——上游靠"丢弃 None 结果"做到这一点。
//
// Python: ConstructionInput (openjiuwen/harness/manifest/inputs.py:98-128)
type ConstructionInput interface {
	// Resolve 从 params + ctx 取齐所有字段并做类型校验。
	Resolve(params map[string]any, ctx BuildContextInterface) error
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParamString 从 params 取字符串；缺失、为 nil 或类型不符时返回 def。
//
// 对齐上游 `if params and name in params`：键不存在就不设值（回落默认）。
func ParamString(params map[string]any, key, def string) string {
	if params == nil {
		return def
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return def
	}
	s, ok := raw.(string)
	if !ok {
		return def
	}
	return s
}

// ParamInt 从 params 取整数；缺失或为 nil 时返回 def。
//
// 接受 int / int64 / float64 三种形态（JSON 反序列化产出 float64）。
// 类型不符或非整值时报错——静默回落默认会让配置错误无声消失。
func ParamInt(params map[string]any, key string, def int) (int, error) {
	if params == nil {
		return def, nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return def, nil
	}
	switch v := raw.(type) {
	case int:
		return v, nil
	case int64:
		return int(v), nil
	case float64:
		if v != float64(int(v)) {
			return def, fmt.Errorf("param %q: expected integer, got float %v", key, v)
		}
		return int(v), nil
	default:
		return def, fmt.Errorf("param %q: expected integer, got %T", key, raw)
	}
}

// ParamBool 从 params 取布尔；缺失、为 nil 或类型不符时返回 def。
func ParamBool(params map[string]any, key string, def bool) bool {
	if params == nil {
		return def
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return def
	}
	b, ok := raw.(bool)
	if !ok {
		return def
	}
	return b
}

// ParamStringSlice 从 params 取字符串切片；缺失或为 nil 时返回 nil。
//
// 接受 []string 与 []any 两种形态（JSON 反序列化产出 []any）；
// []any 里的非字符串元素被跳过。
func ParamStringSlice(params map[string]any, key string) []string {
	if params == nil {
		return nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return nil
	}
	switch v := raw.(type) {
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

// ParamStringPtr 从 params 取字符串指针；缺失、为 nil 或类型不符时返回 nil。
//
// 与 ParamString 的区别：nil 表示"调用方未提供"，空串表示"显式给了空串"。
// 对应上游 Optional[str] 字段。
func ParamStringPtr(params map[string]any, key string) *string {
	if params == nil {
		return nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return nil
	}
	s, ok := raw.(string)
	if !ok {
		return nil
	}
	return &s
}

// ParamMap 从 params 取子映射；缺失、为 nil 或类型不符时返回 nil。
func ParamMap(params map[string]any, key string) map[string]any {
	if params == nil {
		return nil
	}
	raw, ok := params[key]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

// ContextString 从 context 取字符串字段；取不到或为空时返回 def。
//
// attr 是 BuildContextInterface 上 getter 去掉 Get 前缀后的名字
// （"Role" 对应 GetRole()）。ctx 为 nil 或 attr 未知时直接返回 def，
// 不 panic——对齐上游 `getattr(context, attr, None)` 的容忍语义。
func ContextString(ctx BuildContextInterface, attr, def string) string {
	if ctx == nil {
		return def
	}
	var v string
	switch attr {
	case "Language":
		v = ctx.GetLanguage()
	case "MemberName":
		v = ctx.GetMemberName()
	case "Role":
		v = ctx.GetRole()
	case "MemberCardID":
		v = ctx.GetMemberCardID()
	case "ProjectDir":
		v = ctx.GetProjectDir()
	case "TeamOutputsDir":
		v = ctx.GetTeamOutputsDir()
	default:
		// 未知属性视同取不到：不 panic，回落默认
		return def
	}
	if v == "" {
		return def
	}
	return v
}

// ContextExtras 返回 context 的 Extras（ctx 或 Extras 为 nil 时返回空 map）。
//
// 供 provider 无条件读取，避免每处都判两次 nil。
func ContextExtras(ctx BuildContextInterface) map[string]any {
	if ctx == nil {
		return map[string]any{}
	}
	extras := ctx.GetExtras()
	if extras == nil {
		return map[string]any{}
	}
	return extras
}
