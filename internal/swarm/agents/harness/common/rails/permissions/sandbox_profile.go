package permissions

import (
	"reflect"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SandboxDescriptor 当前沙箱操作暴露的解析根目录。
//
// 只描述「解析根在哪」，不解释沙箱能力（能不能写、能不能联网由别处判定）。
//
// Python: @dataclass(frozen=True, slots=True) SandboxDescriptor
type SandboxDescriptor struct {
	// ExecutionWorkspaceRoot 执行工作空间根
	ExecutionWorkspaceRoot string
	// PythonPackageVenvRoot Python 包虚拟环境根
	PythonPackageVenvRoot string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildSandboxProfile 从 sys_operation 提取解析根，不解释沙箱能力。
//
// sys_operation 支持三种形态：nil、map[string]any（按蛇形键取）、任意 struct
// （按字段名或 json tag 取）。
//
// Python: build_sandbox_profile(sys_operation=None)
func BuildSandboxProfile(sysOperation any) SandboxDescriptor {
	if sysOperation == nil {
		return SandboxDescriptor{}
	}
	return SandboxDescriptor{
		ExecutionWorkspaceRoot: rawStringAttr(sysOperation, "sandbox_execution_workspace_root"),
		PythonPackageVenvRoot:  rawStringAttr(sysOperation, "sandbox_python_package_venv_root"),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// rawStringAttr 取字符串属性并裁剪；非字符串或空白返回空串。
//
// Python: _raw_string_attr(source, name)
func rawStringAttr(source any, name string) string {
	value := valueGet(source, name)
	s, ok := value.(string)
	if !ok {
		return ""
	}
	return strings.TrimSpace(s)
}

// valueGet 从 map 或 struct 取字段；source 为 nil 或类型不支持时返回 nil。
//
// Python: _value_get(source, name)
func valueGet(source any, name string) any {
	if source == nil {
		return nil
	}
	// map 形态：直接按蛇形键取（Python 的 Mapping.get）
	if m, ok := source.(map[string]any); ok {
		return m[name]
	}
	// struct 形态：反射按字段名或 json tag 匹配（Python 的 getattr）
	v := reflect.ValueOf(source)
	if v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return nil
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return nil
	}
	t := v.Type()
	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		if field.PkgPath != "" {
			continue // 非导出字段跳过
		}
		if field.Name == name {
			return v.Field(i).Interface()
		}
		tag := field.Tag.Get("json")
		if tag == "" {
			continue
		}
		if idx := strings.Index(tag, ","); idx >= 0 {
			tag = tag[:idx]
		}
		if tag == name {
			return v.Field(i).Interface()
		}
	}
	return nil
}
