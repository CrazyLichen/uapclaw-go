// Package permissions 的审计旁路实现。
//
// 本文件对齐 Python jiuwenswarm/agents/harness/common/rails/permissions/audit.py（65 行）。
//
// # observe-only 边界
//
// 审计是纯旁路：日志段与持久化段各自有独立的异常边界，任一段失败都只吞掉，
// 绝不改变被观察的权限判定结果。这与 Python 的两段 try/except 一一对应。
package permissions

import (
	"encoding/json"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AuditOptions 一次审计事件的决策事实。
//
// Python: emit_permission_audit 的关键字参数（audit.py L21-31）
type AuditOptions struct {
	// Decision 决策结果（allow / ask / deny）
	Decision string
	// Reason 决策原因码
	Reason string
	// Degraded 是否处于降级状态
	Degraded bool
	// GrantID 授权标识，空串不写入记录
	GrantID string
	// GrantReason 授权原因，空串不写入记录
	GrantReason string
	// Extra 白名单附加字段，非白名单键被丢弃
	Extra map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EmitPermissionAudit 发出自动权限审计事件，不含原始工具参数。
//
// Python: emit_permission_audit (audit.py L21-65)
//
// 两段独立的异常边界：
//  1. 构造 + 记日志：异常吞掉（Python 的 except: pass，刻意不留噪声）
//  2. 持久化：异常吞掉，返回 nil
//
// persistentWriter 为 nil 时直接返回 nil，不进第二段。
func EmitPermissionAudit(
	facts ToolDecisionFacts,
	opts AuditOptions,
	persistentWriter *PersistentAuditWriter,
) *PersistentAuditWriteResult {
	emitAuditLog(facts, opts)
	if persistentWriter == nil {
		return nil
	}
	return writePersistentAudit(persistentWriter, facts, opts)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// emitAuditLog 记审计日志。
//
// Python: audit.py L33-50 的第一段 try/except
func emitAuditLog(facts ToolDecisionFacts, opts AuditOptions) {
	defer func() {
		// Python: except Exception: pass
		_ = recover()
	}()

	event := BuildSanitizedAuditFields(
		facts,
		opts.Decision,
		opts.Reason,
		opts.Degraded,
		opts.GrantID,
		opts.GrantReason,
		opts.Extra,
	)
	encoded, err := marshalSortedCompact(event)
	if err != nil {
		return
	}
	logger.Info(logComponent).
		Msg("permission_audit " + encoded)
}

// writePersistentAudit 落盘审计记录。
//
// Python: audit.py L51-65 的第二段 try/except
func writePersistentAudit(
	writer *PersistentAuditWriter,
	facts ToolDecisionFacts,
	opts AuditOptions,
) *PersistentAuditWriteResult {
	var result *PersistentAuditWriteResult
	func() {
		defer func() {
			// Python: except Exception: return None
			if recover() != nil {
				result = nil
			}
		}()
		written := writer.Write(
			facts,
			opts.Decision,
			opts.Reason,
			opts.Degraded,
			opts.GrantID,
			opts.GrantReason,
			opts.Extra,
		)
		result = &written
	}()
	return result
}

// marshalSortedCompact 序列化为键有序、无空格的 JSON。
//
// Python: json.dumps(event, sort_keys=True, separators=(",", ":"))
//
// Go 的 encoding/json 对 map[string]any 天然按键名字典序输出，
// 与 sort_keys=True 一致；这里再显式归一一次，把 []string 转 []any
// 避免序列化差异。
func marshalSortedCompact(value any) (string, error) {
	normalized := normalizeForSortedJSON(value)
	encoded, err := json.Marshal(normalized)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// normalizeForSortedJSON 递归归一化为可稳定序列化的结构。
func normalizeForSortedJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		result := make(map[string]any, len(typed))
		for _, key := range keys {
			result[key] = normalizeForSortedJSON(typed[key])
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = normalizeForSortedJSON(item)
		}
		return result
	case []string:
		result := make([]any, len(typed))
		for index, item := range typed {
			result[index] = item
		}
		return result
	default:
		return value
	}
}
