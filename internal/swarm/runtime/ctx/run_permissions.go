// 请求级权限覆盖层，叠加在已安装的 Host 策略之上。
//
// 对应 Python 代码：jiuwenswarm/runtime/run_permissions.py（54 行）
//
// 作用：一次请求（一次用户消息）可以携带临时的工具权限选择，例如
// {"bash": "allow"}，只影响本次请求，不改全局安装配置。本包把这层选择
// 叠加到已装配置上，同时保留所有已安装的安全护栏
//（file_guard / net_guard / shell_guard 等其余键原样深拷贝）。
//
// 流程位置：
//
//	请求入口 → WithRunPermissions(ctx, levels)
//	  → JiuwenSwarmPermissionInterruptRail.resolve_interrupt
//	    → OverlayRunPermissions(ctx, installedPermissionConfig())
//	      → update_config(叠加结果) → engine.CheckPermission
//	  → 请求结束清理 ctx
//
// 语言差异：Python 用两个 ContextVar（RUN_PERMISSIONS / CURRENT_RUN_CONTEXT）
// 隐式传递，Go 用 context.Context 派生值显式传递（同 cwd 包的设计）。
// Python 的歧义检查抛 ValueError，Go 返回 error 由调用方传播
//（与 9.90d-1 的 cwd.GetCwd 同一口径，见偏差 V-10）。

package ctx

import (
	"context"
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/permission_tools"
)

// ──────────────────────────── 常量 ────────────────────────────

// runPermissionsKey 请求级权限覆盖的 context key 类型。
//
// Python: RUN_PERMISSIONS = ContextVar("jiuwenswarm_run_permissions", default=None)
type runPermissionsKey struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// WithRunPermissions 把请求级工具权限选择绑定到 ctx。
//
// Python: RUN_PERMISSIONS.set(levels)
func WithRunPermissions(ctx context.Context, levels map[string]string) context.Context {
	if levels == nil {
		return context.WithValue(ctx, runPermissionsKey{}, map[string]string(nil))
	}
	copied := make(map[string]string, len(levels))
	for k, v := range levels {
		copied[k] = v
	}
	return context.WithValue(ctx, runPermissionsKey{}, copied)
}

// RunPermissionsFromCtx 读取当前绑定的请求级权限选择，未绑定返回 nil。
//
// Python: RUN_PERMISSIONS.get()
func RunPermissionsFromCtx(ctx context.Context) map[string]string {
	levels, ok := ctx.Value(runPermissionsKey{}).(map[string]string)
	if !ok {
		return nil
	}
	return levels
}

// OverlayRunPermissions 应用逐工具选择，同时保留所有已安装的安全护栏。
//
// Python: overlay_run_permissions(config) -> dict
//
// 步骤（与 Python 逐条对应）：
//  1. levels = RUN_PERMISSIONS.get()
//  2. levels is None 时，从 CURRENT_RUN_CONTEXT.tool_permissions 兜底读取，
//     逐项归一工具名；归一为空或归一后撞车 → ValueError
//  3. if not levels: return config（**不深拷贝**，直接返回原对象）
//  4. result = deepcopy(config)
//  5. 取 tools 字典（非 dict 时重建空 dict）、三个桶集合
//  6. 逐项：整工具 deny 跳过；否则改 tools[name] 并同步三个桶
//  7. 三个桶排序输出（Python sorted()）
//
// 返回 error 用于表达第 2 步的歧义校验（Python 抛 ValueError）。
func OverlayRunPermissions(ctx context.Context, config map[string]any) (map[string]any, error) {
	levels := RunPermissionsFromCtx(ctx)
	if levels == nil {
		// Runtime workers can predate the request's RUN_PERMISSIONS binding.
		// ⚠️ 写入方（请求入口）暂留 ⤵️：见 9.90d-2 装配层 / 10.3.x 章节。
		if rc := CurrentRunContext(ctx); rc != nil && rc.ToolPermissions != nil {
			levels = make(map[string]string)
			for _, item := range rc.ToolPermissions {
				canonical := permission_tools.NormalizePermissionToolName(item.Name)
				if canonical == "" {
					return nil, fmt.Errorf("Run permission tool names are ambiguous.")
				}
				if _, exists := levels[canonical]; exists {
					return nil, fmt.Errorf("Run permission tool names are ambiguous.")
				}
				levels[canonical] = item.Level
			}
		}
	}
	if len(levels) == 0 {
		return config, nil
	}

	result := deepCopyMap(config)

	toolsRaw, _ := result["tools"].(map[string]any)
	tools := make(map[string]any, len(toolsRaw))
	for k, v := range toolsRaw {
		tools[k] = v
	}

	denied := toStringSet(result["deny_tools"])
	asked := toStringSet(result["ask_tools"])
	allowed := toStringSet(result["allow_tools"])

	for name, level := range levels {
		// 整工具 deny 不可被放宽
		if denied[name] {
			continue
		}
		if tv, ok := tools[name]; ok && tv == "deny" {
			continue
		}
		tools[name] = level
		delete(denied, name)
		delete(asked, name)
		delete(allowed, name)
		switch level {
		case "deny":
			denied[name] = true
		case "ask":
			asked[name] = true
		case "allow":
			allowed[name] = true
		}
	}

	result["tools"] = tools
	result["deny_tools"] = sortedKeys(denied)
	result["ask_tools"] = sortedKeys(asked)
	result["allow_tools"] = sortedKeys(allowed)
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// deepCopyMap 深拷贝 map[string]any（嵌套 map 递归拷贝）。
//
// Python: deepcopy(config)
func deepCopyMap(src map[string]any) map[string]any {
	if src == nil {
		return map[string]any{}
	}
	dst := make(map[string]any, len(src))
	for k, v := range src {
		switch nested := v.(type) {
		case map[string]any:
			dst[k] = deepCopyMap(nested)
		case []string:
			cp := make([]string, len(nested))
			copy(cp, nested)
			dst[k] = cp
		case []any:
			cp := make([]any, len(nested))
			copy(cp, nested)
			dst[k] = cp
		default:
			dst[k] = v
		}
	}
	return dst
}

// toStringSet 把 []any / []string / nil 归一为 set。
//
// Python: set(result.get("deny_tools") or ())
func toStringSet(value any) map[string]bool {
	out := make(map[string]bool)
	switch v := value.(type) {
	case []string:
		for _, s := range v {
			out[s] = true
		}
	case []any:
		for _, item := range v {
			if s, ok := item.(string); ok {
				out[s] = true
			}
		}
	}
	return out
}

// sortedKeys 返回排序后的键切片。
//
// Python: sorted(denied)
func sortedKeys(set map[string]bool) []string {
	keys := make([]string, 0, len(set))
	for k := range set {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
