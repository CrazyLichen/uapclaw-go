package harness_providers

import (
	"fmt"
	"math"
	"reflect"
	"strings"
)

// 本文件对应 Python openjiuwen/harness_providers/jsonsafe.py（55 行）：
// 把厂商 SDK 对象转换成协议安全的 JSON 值。
//
// 这是**序列化前的类型归一化**，与 harness_protocol.FreezeJsonValue（严格准入校验）
// 串联两道工序：
//
//	provider SDK 对象
//	  → ToJsonSafe       归一化（NaN→nil、枚举→字符串值、对象→字符串）
//	  → FreezeJsonValue  递归校验 + 冻结（拒绝残留 NaN / 非 string key）
//	  → 进 HarnessEvent / HarnessCheckpoint
//
// 两道不可合并：FreezeJsonValue 是严格拒绝的（NaN 抛错），而 provider 数据里
// NaN 常见（SDK 的 usage、耗时字段）。没有本文件的清洗，provider 吐一个 NaN
// 整条事件构造就失败。
//
// 这也解释了目录归属：本文件是 provider 侧的脏数据清洗（harness_providers/），
// FreezeJsonValue 是协议侧的准入校验（harness_protocol/）。

// ──────────────────────────── 导出函数 ────────────────────────────

// ToJsonSafe 返回 value 的 JSON 兼容副本，可被 FreezeJsonValue 接受。
//
// 分支顺序对齐上游（jsonsafe.py:24-43），其中两项顺序不可调：
//   - []byte 必须在 slice 之前判断（Kind() 都是 Slice，否则会展开成数字数组）
//   - 非有限 float 必须在普通 float 之前清洗成 nil
//
// 与上游的三处偏差（spec D1/D2/D3）：
//   - os.PathLike：Go 无对应抽象，仓内路径一律 string，走兜底
//   - dataclass：Go 无对应物，用「有字段的结构体」启发式会误伤 time.Time
//     等（Python 对它们走兜底），故不实现
//   - model_dump：用可选接口 ModelDump 探测，本批无实现者
//
// Python: jsonsafe.py:15-43 to_json_safe
func ToJsonSafe(value any) any {
	// 步骤 1：None（Go 的 nil）原样
	// Python: jsonsafe.py:24
	if value == nil {
		return nil
	}

	// 步骤 2：基础类型原样；非有限 float 洗成 nil
	// Python: jsonsafe.py:24-27
	switch v := value.(type) {
	case string, bool,
		int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64:
		return v
	case float64:
		if math.IsNaN(v) || math.IsInf(v, 0) {
			return nil
		}
		return v
	case float32:
		f := float64(v)
		if math.IsNaN(f) || math.IsInf(f, 0) {
			return nil
		}
		return v
	}

	// 步骤 3：bytes 必须先于 slice（Kind() 都是 Slice）
	// Python: jsonsafe.py:32-33（decode("utf-8", errors="replace")）
	// 用 strings.ToValidUTF8 复刻 errors="replace"（仓内先例：
	// internal/agentcore/harness/tools/shell/output.go:108-110）
	if b, ok := value.([]byte); ok {
		return strings.ToValidUTF8(string(b), "\ufffd")
	}

	// 步骤 4：model_dump 可选接口（spec D3 留缝，本批无实现者）
	// Python: jsonsafe.py:36-38
	if dumper, ok := value.(interface {
		ModelDump(mode string, byAlias bool) (any, error)
	}); ok {
		if dumped, err := dumper.ModelDump("json", true); err == nil {
			return ToJsonSafe(dumped)
		}
	}

	// 步骤 5：容器递归
	// Python: jsonsafe.py:39-42
	rv := reflect.ValueOf(value)
	switch rv.Kind() {
	case reflect.Map:
		// Python: jsonsafe.py:39-40（key 转 str）
		out := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			out[fmt.Sprint(iter.Key().Interface())] = ToJsonSafe(iter.Value().Interface())
		}
		return out
	case reflect.Slice, reflect.Array:
		// Python: jsonsafe.py:41-42（list/tuple/set/frozenset → list）
		out := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = ToJsonSafe(rv.Index(i).Interface())
		}
		return out
	case reflect.String:
		// spec Q3：具名 string 类型（本仓全部枚举都是 type X string）
		// 取字符串值，等价于 Python str,Enum 取 .value。
		// 注：不能靠 Type().Name() 判枚举——实测 reflect 对预声明 string
		// 也返回 "string" 而非空串，该判据不成立。三者 Kind() 都是 String，
		// 返回原字符串本身，语义与 Python 完全一致。
		return rv.String()
	case reflect.Pointer, reflect.Interface:
		// Python 无指针概念；Go 侧解引用后递归，nil 保持 nil。
		if rv.IsNil() {
			return nil
		}
		return ToJsonSafe(rv.Elem().Interface())
	}

	// 步骤 6：兜底转字符串
	// 这是本函数的灵魂：provider payload 绝不把 live SDK 对象泄漏进事件流。
	// Python: jsonsafe.py:43
	return fmt.Sprint(value)
}

// ToJsonObject 把 value 转成 JSON 对象，非 mapping 值包成 {"value": converted}。
// Python: jsonsafe.py:46-52 to_json_object
func ToJsonObject(value any) map[string]any {
	// 步骤 1：归一化
	converted := ToJsonSafe(value)
	// 步骤 2：是对象则直接返回，否则包一层
	if obj, ok := converted.(map[string]any); ok {
		return obj
	}
	return map[string]any{"value": converted}
}
