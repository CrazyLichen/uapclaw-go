// verification_slice.go 包级 Python 源静态安全检查（executor 依赖闭包）。
// Python: verification.py:286-327 _validate_package_python_source + 常量块；
// 全量 verification.py（1,754 行）⤴️ 17.4c3。AST 解析经 gopapy v2
// （spec 17.4c §2.4 选型；节点形状按 CPython asdl，扫描逻辑逐行对照）。

package memberoptimizer

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/tamnd/gopapy/v2/parser2"
)

// dangerousImportRoots 包级 Python 资源禁用导入根。
// Python: verification.py:123-132 _DANGEROUS_IMPORT_ROOTS。
var dangerousImportRoots = map[string]bool{
	"httpx": true, "os": true, "pathlib": true, "requests": true,
	"shutil": true, "socket": true, "subprocess": true, "sys": true, "urllib": true,
}

// dangerousCallNames 禁用调用限定名。
// Python: verification.py:105-121 _DANGEROUS_CALL_NAMES。
var dangerousCallNames = map[string]bool{
	"__import__": true, "compile": true, "eval": true, "exec": true,
	"input": true, "open": true, "os.popen": true, "os.spawn": true,
	"os.system": true, "shutil.move": true, "shutil.rmtree": true,
	"subprocess.call": true, "subprocess.check_call": true,
	"subprocess.check_output": true, "subprocess.Popen": true,
	"subprocess.run": true,
}

// ValidatePackagePythonSource 包级 Python 资源静态安全错误清单。
// Python: verification.py:286-311 _validate_package_python_source。
func ValidatePackagePythonSource(source string, path string) []string {
	// Python: try: tree = ast.parse(source, filename=path) except SyntaxError: return [str(exc)]
	// gopapy 等价：ParseString 解析失败即 SyntaxError（D-c3-1）
	tree, err := parser2.ParseString(path, source)
	if err != nil {
		return []string{err.Error()}
	}
	var errs []string
	// Python: for node in ast.walk(tree): Import / ImportFrom / Call 分派
	for _, node := range walkPyNodes(tree) {
		switch n := node.(type) {
		case *parser2.Import:
			// Python: for alias in node.names: root = alias.name.split(".", 1)[0]
			for _, alias := range n.Names {
				root := strings.SplitN(alias.Name, ".", 2)[0]
				if dangerousImportRoots[root] {
					errs = append(errs, fmt.Sprintf("dangerous import '%s' in %s", alias.Name, path))
				}
			}
		case *parser2.ImportFrom:
			// Python: root = (node.module or "").split(".", 1)[0]（gopapy Module 为空串当仅点）
			root := strings.SplitN(n.Module, ".", 2)[0]
			if dangerousImportRoots[root] {
				errs = append(errs, fmt.Sprintf("dangerous import '%s' in %s", n.Module, path))
			}
		case *parser2.Call:
			// Python: call_name = _call_name(node.func); if in _DANGEROUS_CALL_NAMES: errors.append(...)
			callName := pyCallName(n.Func)
			if dangerousCallNames[callName] {
				errs = append(errs, fmt.Sprintf("dangerous call '%s' in %s", callName, path))
			}
		}
	}
	return errs
}

// pyCallName 调用名解析（Name 直读；Attribute 仅 builtins/__builtins__ 前缀）。
// Python: verification.py:314-325 _call_name。
func pyCallName(expr parser2.Expr) string {
	switch node := expr.(type) {
	case *parser2.Name:
		return node.Id
	case *parser2.Attribute:
		// Python: Attribute 调用不等价同叶名危险内建（如 re.compile）；
		// 仅显式 builtins/__builtins__ 访问阻断，不拒绝普通库 API。
		if base, isName := node.Value.(*parser2.Name); isName {
			if base.Id == "builtins" || base.Id == "__builtins__" {
				return node.Attr
			}
		}
	}
	return ""
}

// walkPyNodes 反射式 AST 遍历（gopapy 为封闭和类型，无 ast.Walk；
// 结构体字段递归收集 Stmt/Expr 接口值——覆盖全部 77 节点形状）。
func walkPyNodes(root any) []any {
	var out []any
	seen := map[uintptr]bool{}
	var walk func(value any)
	var walkStruct func(reflect.Value)
	walk = func(value any) {
		if value == nil {
			return
		}
		v := reflect.ValueOf(value)
		switch v.Kind() {
		case reflect.Ptr:
			// 防环（异常构造下）
			if v.IsNil() {
				return
			}
			key := v.Pointer()
			if seen[key] {
				return
			}
			// 注意：reflect.NumMethod 只计导出方法，gopapy 节点方法全部非导出——
			// 不能以 NumMethod>0 作守卫，直接断言接口。
			if _, isStmt := value.(parser2.Stmt); isStmt {
				out = append(out, value)
			}
			if _, isExpr := value.(parser2.Expr); isExpr {
				out = append(out, value)
			}
			seen[key] = true
			walkStruct(v.Elem())
		case reflect.Slice:
			for i := 0; i < v.Len(); i++ {
				walk(v.Index(i).Interface())
			}
		case reflect.Struct:
			walkStruct(v)
		}
	}
	walkStruct = func(v reflect.Value) {
		for i := 0; i < v.NumField(); i++ {
			field := v.Field(i)
			switch field.Kind() {
			case reflect.Ptr, reflect.Struct, reflect.Slice, reflect.Interface:
				if field.CanInterface() {
					walk(field.Interface())
				}
			}
		}
	}
	walk(root)
	return out
}


