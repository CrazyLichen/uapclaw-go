package permissions

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"

	harnesssecurity "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/security"
)

// ──────────────────────────── 结构体 ────────────────────────────

// NativePathAccess 一次已核验的原生文件访问（Smart 场景专用）。
//
// 记录执行身份（工具名 + 参数 JSON）与一个字面路径或受限搜索根。
// 只有工具名与参数都完全一致时才认为「同一次访问」，避免跨调用串用。
//
// Python: @dataclass(frozen=True) NativePathAccess
type NativePathAccess struct {
	// ToolName 工具名
	ToolName string
	// ArgumentsJSON 参数 JSON（ASCII 转义 + 键排序）
	ArgumentsJSON string
	// Path 访问的路径
	Path string
	// Action 访问动作（read / write）
	Action string
}

// NativePathGuardProjection 只适配抽取；路径策略仍由被包装的检查器决定。
//
// 仅安装在 Smart 的 base rail 上。工具级策略与场景钩子保留真实工具名与参数。
// 不修改任何全局 SDK 注册表。
//
// Python: class NativePathGuardProjection
type NativePathGuardProjection struct {
	// checker 被包装的路径判定器
	checker harnesssecurity.FileGuardEvaluator
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// nativePathAccessMu 当前访问的读写锁
	nativePathAccessMu sync.RWMutex
	// currentNativePathAccess 当前调用栈的投影访问
	//
	// Python 用 ContextVar（协程级隔离）；Go 侧权限判定与工具执行在同一
	// goroutine 的同步调用链内完成，用进程级变量 + 清空即可。若将来出现
	// 并发嵌套调用，需改为 context.Context 传值。
	currentNativePathAccess *NativePathAccess
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GuardTool 派生用于路径检查的工具名（写→write_file，其余→read_file）。
//
// Python: NativePathAccess.guard_tool（property）
func (a *NativePathAccess) GuardTool() string {
	if a.Action == "write" {
		return "write_file"
	}
	return "read_file"
}

// GuardArgs 派生用于路径检查的参数（只含 file_path）。
//
// Python: NativePathAccess.guard_args（property）
func (a *NativePathAccess) GuardArgs() map[string]any {
	return map[string]any{"file_path": a.Path}
}

// NativeArgumentsJSON 把参数序列化为确定性 JSON。
//
// 三个约束（对齐 Python 的 json.dumps 参数）：
//   - sort_keys=True：键排序，否则 map 遍历顺序会让相同参数产生不同字符串
//   - ensure_ascii=True：非 ASCII 转成 \uXXXX。**Go 的 json.Marshal 默认输出
//     UTF-8**，不手动转义会让含中文路径的参数比较恒不相等，投影永远失效
//   - allow_nan=False：NaN/Inf 无法序列化时返回错误而非输出非法 JSON
//
// Python: native_arguments_json(args)
func NativeArgumentsJSON(args map[string]any) string {
	if args == nil {
		return "{}"
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(args); err != nil {
		return "{}"
	}
	raw := strings.TrimSuffix(buf.String(), "\n")

	// 键排序：json.Marshal 对 map 已按字典序输出，但为对齐 Python 的
	// sort_keys 语义，这里用 canonical 化再确认一次。
	canonical, err := canonicalizeJSON(raw)
	if err != nil {
		return raw
	}
	return canonical
}

// CurrentNativePathAccess 返回与本次调用匹配的投影访问，不匹配返回 nil。
//
// 两道校验（对齐 Python current_native_path_access）：
//  1. 工具名一致
//  2. 参数 JSON 完全一致（含非 ASCII 转义后）
//
// Python: current_native_path_access(tool_name, args)
func CurrentNativePathAccess(toolName string, args map[string]any) *NativePathAccess {
	nativePathAccessMu.RLock()
	access := currentNativePathAccess
	nativePathAccessMu.RUnlock()

	if access == nil || toolName != access.ToolName {
		return nil
	}
	if NativeArgumentsJSON(args) != access.ArgumentsJSON {
		return nil
	}
	return access
}

// SetNativePathAccess 设置当前投影访问；传 nil 清空。
//
// Python 无对应函数（用 ContextVar 的 token/reset），Go 侧显式设置与清空。
func SetNativePathAccess(access *NativePathAccess) {
	nativePathAccessMu.Lock()
	currentNativePathAccess = access
	nativePathAccessMu.Unlock()
}

// NewNativePathGuardProjection 包装一个路径判定器。
//
// Python: NativePathGuardProjection(checker)
func NewNativePathGuardProjection(checker harnesssecurity.FileGuardEvaluator) *NativePathGuardProjection {
	return &NativePathGuardProjection{checker: checker}
}

// Evaluate 有投影时用 guard 工具与参数调用检查器，否则透传。
//
// Python: NativePathGuardProjection.evaluate(tool_name, tool_args)
func (p *NativePathGuardProjection) Evaluate(toolName string, toolArgs map[string]any) *harnesssecurity.PermissionResult {
	if p == nil || p.checker == nil {
		return nil
	}
	if access := CurrentNativePathAccess(toolName, toolArgs); access != nil {
		return p.checker.Evaluate(access.GuardTool(), access.GuardArgs())
	}
	return p.checker.Evaluate(toolName, toolArgs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// canonicalizeJSON 把 JSON 文本重排为键有序 + 非 ASCII 全转义的形式。
//
// Python 的 json.dumps(sort_keys=True, ensure_ascii=True) 输出：
//   - 键按字典序
//   - 所有非 ASCII 字符转成 \uXXXX（小写十六进制）
//   - 分隔符为 ", " 与 ": "（默认 separators），但 Go 侧用紧凑形式，
//     因为本函数只用于**相等比较**，不用于对外输出。
func canonicalizeJSON(raw string) (string, error) {
	var decoded any
	dec := json.NewDecoder(strings.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&decoded); err != nil {
		return "", err
	}
	var sb strings.Builder
	if err := writeCanonicalJSON(&sb, decoded); err != nil {
		return "", err
	}
	return sb.String(), nil
}

// writeCanonicalJSON 递归写出 canonical JSON。
func writeCanonicalJSON(sb *strings.Builder, v any) error {
	switch x := v.(type) {
	case nil:
		sb.WriteString("null")
	case bool:
		if x {
			sb.WriteString("true")
		} else {
			sb.WriteString("false")
		}
	case json.Number:
		sb.WriteString(x.String())
	case string:
		sb.WriteString(encodeJSONStringASCII(x))
	case []any:
		sb.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				sb.WriteByte(',')
			}
			if err := writeCanonicalJSON(sb, item); err != nil {
				return err
			}
		}
		sb.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		sb.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				sb.WriteByte(',')
			}
			sb.WriteString(encodeJSONStringASCII(k))
			sb.WriteByte(':')
			if err := writeCanonicalJSON(sb, x[k]); err != nil {
				return err
			}
		}
		sb.WriteByte('}')
	default:
		return fmt.Errorf("unsupported json type %T", v)
	}
	return nil
}

// encodeJSONStringASCII 编码 JSON 字符串，所有非 ASCII 字符转 \uXXXX。
//
// 对齐 Python json.dumps 的 ensure_ascii=True 行为：
//   - BMP 内字符转 \uXXXX（小写十六进制）
//   - BMP 外字符转 UTF-16 代理对（两个 \uXXXX）
//   - 控制字符用标准短转义（\n \t 等）或 \u00XX
func encodeJSONStringASCII(s string) string {
	var sb strings.Builder
	sb.WriteByte('"')
	for _, r := range s {
		switch r {
		case '"':
			sb.WriteString(`\"`)
		case '\\':
			sb.WriteString(`\\`)
		case '\n':
			sb.WriteString(`\n`)
		case '\r':
			sb.WriteString(`\r`)
		case '\t':
			sb.WriteString(`\t`)
		case '\b':
			sb.WriteString(`\b`)
		case '\f':
			sb.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&sb, `\u%04x`, r)
				continue
			}
			if r < 0x7f {
				sb.WriteRune(r)
				continue
			}
			if r <= 0xffff {
				fmt.Fprintf(&sb, `\u%04x`, r)
				continue
			}
			// BMP 之外：拆成 UTF-16 代理对
			r -= 0x10000
			high := 0xd800 + (r >> 10)
			low := 0xdc00 + (r & 0x3ff)
			fmt.Fprintf(&sb, `\u%04x\u%04x`, high, low)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}
