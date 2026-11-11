// Package permissions 的权限交互判据实现。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/permission_interaction.py（54 行）。
//
// # 用途
//
// 流事件层收到一条嵌套消息时，用它判断「这条消息里是否携带宿主权限定位信息」，
// 决定走「权限中断快照撤销」还是「普通中断投影」。
//
// # 为什么要扫描键名而不是判断类型
//
// 消息从权限层产生到流事件层，中途经过序列化、事件总线、可能的进程边界。
// 到这里类型信息已经丢失（只剩 dict），无法用类型断言识别，只能扫字段。
package permissions

import (
	"reflect"
	"strings"

	interruptrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// PermissionRuntimeQuarantinedKey 会话隔离标记在 session state 中的键。
	//
	// Python: PERMISSION_RUNTIME_QUARANTINED_KEY (permission_interaction.py L10-12)
	//
	// 定义已下沉到 interrupt 包（rails 包读写该键时不能让
	// rails → rails/permissions 成环），这里转发以保持既有调用点不变。
	PermissionRuntimeQuarantinedKey = interruptrail.PermissionRuntimeQuarantinedKey

	// permissionInteractionScanLimit 扫描循环硬上限。
	// Python: permission_interaction.py L20 `for _ in range(128)`
	permissionInteractionScanLimit = 128

	// toolInvocationKeyField 权限请求身份指纹字段名
	toolInvocationKeyField = "tool_invocation_key"

	// manualApprovalSupportedField 人工审批标志字段名
	manualApprovalSupportedField = "manual_approval_supported"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// permissionInteractionNestedFields 对象载体上待下钻的字段名。
	//
	// Python: permission_interaction.py L47
	// `for name in ("payload", "value", "state")`
	permissionInteractionNestedFields = []string{"payload", "value", "state"}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ContainsPermissionInteraction 判断一个嵌套值是否携带宿主权限定位信息。
//
// Python: contains_permission_interaction (permission_interaction.py L15-51)
//
// 三条判据（短路顺序不可调换）：
//  1. metadata 含 tool_invocation_key
//  2. metadata.manual_approval_supported 严格等于 true（1 与 "true" 都不算）
//  3. 对象顶层含 tool_invocation_key
//
// 扫满 128 层仍未完成时返回 true（fail-safe）：无法确认干净就按有权限交互处理。
func ContainsPermissionInteraction(value any) bool {
	pending := []any{value}
	seen := make(map[uintptr]struct{}, 16)

	for i := 0; i < permissionInteractionScanLimit; i++ {
		if len(pending) == 0 {
			return false
		}
		// Python 用 pending.pop()（从尾部取），保证与 Python 的遍历顺序一致
		item := pending[len(pending)-1]
		pending = pending[:len(pending)-1]

		if identity, ok := valueIdentity(item); ok {
			if _, dup := seen[identity]; dup {
				continue
			}
			seen[identity] = struct{}{}
		}

		switch typed := item.(type) {
		case map[string]any:
			if mappingHasPermissionKey(typed["metadata"]) {
				return true
			}
			if _, exists := typed[toolInvocationKeyField]; exists {
				return true
			}
			for _, nested := range typed {
				pending = append(pending, nested)
			}
		case []any:
			for _, nested := range typed {
				pending = append(pending, nested)
			}
		default:
			if mappingHasPermissionKey(readObjectField(item, "metadata")) {
				return true
			}
			for _, field := range permissionInteractionNestedFields {
				if nested := readObjectField(item, field); nested != nil {
					pending = append(pending, nested)
				}
			}
		}
	}
	return len(pending) > 0
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// mappingHasPermissionKey 判断一个值是否为携带权限定位信息的映射。
//
// Python: permission_interaction.py L29-34 / L41-46
func mappingHasPermissionKey(value any) bool {
	mapping, ok := value.(map[string]any)
	if !ok {
		return false
	}
	if _, exists := mapping[toolInvocationKeyField]; exists {
		return true
	}
	flag, ok := mapping[manualApprovalSupportedField].(bool)
	return ok && flag
}

// readObjectField 通过反射读取对象字段。
//
// Python: getattr(item, name, None)
//
// 只读字段，不调方法——Python 那边 getattr 也是取属性。
func readObjectField(item any, name string) any {
	if item == nil {
		return nil
	}
	value := reflect.ValueOf(item)
	if value.Kind() == reflect.Ptr {
		if value.IsNil() {
			return nil
		}
		value = value.Elem()
	}
	if value.Kind() != reflect.Struct {
		return nil
	}
	field := value.FieldByName(exportedName(name))
	if !field.IsValid() || !field.CanInterface() {
		return nil
	}
	return field.Interface()
}

// exportedName 把 snake_case 字段名转成 Go 导出名。
//
// 例：tool_invocation_key → ToolInvocationKey
func exportedName(name string) string {
	parts := strings.Split(name, "_")
	var builder strings.Builder
	for _, part := range parts {
		if part == "" {
			continue
		}
		builder.WriteString(strings.ToUpper(part[:1]))
		builder.WriteString(part[1:])
	}
	return builder.String()
}

// valueIdentity 取值的身份标识用于去重。
//
// Python: id(item)
//
// 偏差 D-1：Go 的 any 装箱后取不到原地址，仅对 map / slice / ptr 可取。
// 取不到时返回 ok=false，该次不做去重——128 层上限已能兜住绝大多数循环。
func valueIdentity(value any) (uintptr, bool) {
	if value == nil {
		return 0, false
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Map, reflect.Slice, reflect.Ptr:
		if reflected.IsNil() {
			return 0, false
		}
		return reflected.Pointer(), true
	default:
		return 0, false
	}
}
