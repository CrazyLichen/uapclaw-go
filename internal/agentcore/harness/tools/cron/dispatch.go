package cron

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// dispatchCronAction cron 统一工具的 action 路由分发器。
// 对齐 Python: _dispatch_cron_action (cron.py L135-201)
//
// 关键对齐点：
// - status/add/update/wake 直接返回 backend 结果（不加包装层）
// - list 包装为 {"jobs": ...}
// - remove 包装为 {"deleted": ...}
// - run 包装为 {"run_id": ...}
// - runs 包装为 {"runs": ...}
// - kwargs 中的 "id" 兼容为 jobId
// - excluded_keys 集合过滤 flat_kwargs
func dispatchCronAction(
	ctx context.Context,
	backend CronToolBackend,
	inputs map[string]any,
	cronCtx *CronToolContext,
) (map[string]any, error) {
	// 1. 提取 action
	// 对齐 Python: action_name = str(action or "").strip().lower()
	actionName := strings.TrimSpace(strings.ToLower(strVal(inputs, "action")))

	// 2. 提取 jobId（兼容 Python kwargs.pop("id", None)）
	// 对齐 Python: legacy_job_id = kwargs.pop("id", None)
	// Python: target_job_id = str(jobId or legacy_job_id or "").strip()
	targetJobID := strings.TrimSpace(strVal(inputs, "jobId"))
	if targetJobID == "" {
		targetJobID = strings.TrimSpace(strVal(inputs, "id"))
	}

	// 3. 防御性日志：记录 action 和 jobId
	logger.Debug(logComponent).
		Str("action", actionName).
		Str("job_id", targetJobID).
		Msg("dispatchCronAction 入口")

	// 4. 构建 excluded_keys 集合
	// 对齐 Python: excluded_keys (cron.py L152-165)
	excludedKeys := map[string]bool{
		"action": true, "job": true, "jobId": true, "patch": true,
		"includeDisabled": true, "text": true, "mode": true,
		"contextMessages": true, "gatewayUrl": true, "gatewayToken": true,
		"timeoutMs": true, "runMode": true, "id": true,
	}

	// 5. 收集 flat_kwargs（排除 excluded_keys 后的剩余字段）
	// 对齐 Python: flat_kwargs (cron.py L166-170)
	flatKwargs := map[string]any{}
	for key, value := range inputs {
		if !excludedKeys[key] {
			flatKwargs[key] = value
		}
	}

	// 6. 路由分发
	// 对齐 Python: if action_name == "xxx" 分支 (cron.py L171-201)
	switch actionName {
	case "status":
		// 对齐 Python L172: return await backend.status()
		result, err := backend.Status(ctx)
		if err != nil {
			return nil, err
		}
		return result, nil

	case "list":
		// 对齐 Python L174: return {"jobs": await backend.list_jobs(include_disabled=bool(includeDisabled))}
		// 对齐 Python: CronToolBackend.list_jobs 默认 include_disabled=False
		includeDisabled := false
		if v, ok := inputs["includeDisabled"]; ok {
			if b, ok := v.(bool); ok {
				includeDisabled = b
			}
		}
		jobs, err := backend.ListJobs(ctx, includeDisabled)
		if err != nil {
			return nil, err
		}
		return map[string]any{"jobs": jobs}, nil

	case "add":
		// 对齐 Python L176-179:
		// Python: create_input = dict(job or {})
		// if not create_input: create_input = flat_kwargs
		// return await backend.create_job(create_input, context=context)
		createInput := mapVal(inputs, "job")
		if len(createInput) == 0 {
			createInput = flatKwargs
		}
		result, err := backend.CreateJob(ctx, createInput, cronCtx)
		if err != nil {
			return nil, err
		}
		return result, nil

	case "update":
		// 对齐 Python L180-186:
		// if not target_job_id: raise ValueError("jobId is required")
		// Python: patch_input = dict(patch or {})
		// if not patch_input: patch_input = flat_kwargs
		// return await backend.update_job(target_job_id, patch_input, context=context)
		if targetJobID == "" {
			return nil, fmt.Errorf("jobId is required")
		}
		patchInput := mapVal(inputs, "patch")
		if len(patchInput) == 0 {
			patchInput = flatKwargs
		}
		result, err := backend.UpdateJob(ctx, targetJobID, patchInput, cronCtx)
		if err != nil {
			return nil, err
		}
		return result, nil

	case "remove":
		// 对齐 Python L187-190:
		// if not target_job_id: raise ValueError("jobId is required")
		// return {"deleted": await backend.delete_job(target_job_id)}
		if targetJobID == "" {
			return nil, fmt.Errorf("jobId is required")
		}
		deleted, err := backend.DeleteJob(ctx, targetJobID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": deleted}, nil

	case "run":
		// 对齐 Python L191-194:
		// if not target_job_id: raise ValueError("jobId is required")
		// return {"run_id": await backend.run_now(target_job_id)}
		if targetJobID == "" {
			return nil, fmt.Errorf("jobId is required")
		}
		runID, err := backend.RunNow(ctx, targetJobID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"run_id": runID}, nil

	case "runs":
		// 对齐 Python L195-198:
		// if not target_job_id: raise ValueError("jobId is required")
		// return {"runs": await backend.get_runs(target_job_id)}
		// Python 调用 get_runs 时未传 limit，使用接口默认值 20
		if targetJobID == "" {
			return nil, fmt.Errorf("jobId is required")
		}
		runs, err := backend.GetRuns(ctx, targetJobID, 20)
		if err != nil {
			return nil, err
		}
		return map[string]any{"runs": runs}, nil

	case "wake":
		// 对齐 Python L199-200:
		// return await backend.wake(text or "", context=context, mode=mode)
		text := strVal(inputs, "text")
		mode := strVal(inputs, "mode")
		result, err := backend.Wake(ctx, text, cronCtx, mode)
		if err != nil {
			return nil, err
		}
		return result, nil

	default:
		// 对齐 Python L201: raise ValueError("unsupported cron action")
		logger.Warn(logComponent).
			Str("action", actionName).
			Msg("不支持的 cron action")
		return nil, fmt.Errorf("不支持的 cron action: %s", actionName)
	}
}

// strVal 从 map[string]any 中提取字符串值。
func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	s, ok := v.(string)
	if !ok {
		return fmt.Sprintf("%v", v)
	}
	return s
}

// boolVal 从 map[string]any 中提取布尔值。
func boolVal(m map[string]any, key string) bool {
	v, ok := m[key]
	if !ok {
		return false
	}
	b, ok := v.(bool)
	if !ok {
		return false
	}
	return b
}

// mapVal 从 map[string]any 中提取嵌套 map[string]any。
func mapVal(m map[string]any, key string) map[string]any {
	v, ok := m[key]
	if !ok {
		return map[string]any{}
	}
	nested, ok := v.(map[string]any)
	if !ok {
		return map[string]any{}
	}
	return nested
}
