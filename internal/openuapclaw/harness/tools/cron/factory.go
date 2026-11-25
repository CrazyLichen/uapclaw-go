package cron

import (
	"context"
	"reflect"
	"strings"

	cschema "github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/tool"
	ptools "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts/tools"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CronListJobsInput cron_list_jobs 工具输入参数（无参数）
// Python: cron_list_jobs wrapper (cron.py L222-224)
type CronListJobsInput struct{}

// CronGetJobInput cron_get_job 工具输入参数
// Python: cron_get_job wrapper (cron.py L225-227)
type CronGetJobInput struct {
	// JobID 要查询的任务 ID
	JobID string `json:"job_id"`
}

// CronCreateJobInput cron_create_job 工具输入参数
// Python: cron_create_job wrapper (cron.py L228-230)
type CronCreateJobInput struct {
	// Name 任务名称
	Name string `json:"name"`
	// CronExpr cron 表达式（Quartz 格式）
	CronExpr string `json:"cron_expr"`
	// Timezone 时区，默认 Asia/Shanghai
	Timezone string `json:"timezone"`
	// Targets 目标频道
	Targets string `json:"targets"`
	// Enabled 是否启用，默认 true
	Enabled bool `json:"enabled"`
	// Description 任务描述内容
	Description string `json:"description"`
	// WakeOffsetSeconds 提前唤醒秒数，默认 300
	WakeOffsetSeconds int `json:"wake_offset_seconds"`
}

// CronUpdateJobInput cron_update_job 工具输入参数
// Python: cron_update_job wrapper (cron.py L231-233)
type CronUpdateJobInput struct {
	// JobID 要更新的任务 ID
	JobID string `json:"job_id"`
	// Patch 要更新的字段
	Patch map[string]any `json:"patch"`
}

// CronDeleteJobInput cron_delete_job 工具输入参数
// Python: cron_delete_job wrapper (cron.py L234-236)
type CronDeleteJobInput struct {
	// JobID 要删除的任务 ID
	JobID string `json:"job_id"`
}

// CronToggleJobInput cron_toggle_job 工具输入参数
// Python: cron_toggle_job wrapper (cron.py L237-239)
type CronToggleJobInput struct {
	// JobID 要启用/禁用的任务 ID
	JobID string `json:"job_id"`
	// Enabled 是否启用
	Enabled bool `json:"enabled"`
}

// CronPreviewJobInput cron_preview_job 工具输入参数
// Python: cron_preview_job wrapper (cron.py L240-242)
type CronPreviewJobInput struct {
	// JobID 要预览的任务 ID
	JobID string `json:"job_id"`
	// Count 预览的执行次数（1-50，默认 5）
	Count int `json:"count"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateCronTools 创建 cron 统一工具 + 可选的遗留兼容工具。
// Python: create_cron_tools (cron.py L204-308)
//
// 统一 cron 工具使用 MapFunction（保留 kwargs），遗留兼容工具使用 InvokeFunction（明确 struct）。
func CreateCronTools(
	backend CronToolBackend,
	cronCtx *CronToolContext,
	language string,
	targetChannels []string,
	defaultTargetChannel string,
	includeLegacyCompat bool,
	agentID string,
) []tool.Tool {
	scope := toolScope(cronCtx)
	finalAgentID := agentID
	if finalAgentID == "" {
		finalAgentID = scope
	}

	// 1. 统一 cron 工具（使用 MapFunction）
	// Python: L243-248: LocalFunction(card=build_tool_card("cron", ...), func=cron_tool_wrapper)
	cronCard, _ := ptools.BuildToolCard("cron", "cron_"+scope, language, nil, finalAgentID)
	// V-6 绑定身份锚点：backend/cronCtx 显式登记到卡片属性（Python 侧经
	// _closure(func, create_cron_tools, ...) 挖闭包；Go 函数闭包不可枚举，
	// 以装配期登记等价替代，行为等价——登记 spec 已知偏差）。
	cronCard.Properties = map[string]any{"cron_backend": backend, "cron_context": cronCtx}
	cronFn := func(ctx context.Context, inputs map[string]any) (map[string]any, error) {
		return dispatchCronAction(ctx, backend, inputs, cronCtx)
	}
	cronMapFn, _ := tool.NewMapFunction(cronCard, cronFn, nil)

	result := []tool.Tool{cronMapFn}
	if !includeLegacyCompat {
		return result
	}

	// 2. 遗留兼容工具（使用 InvokeFunction + struct）
	// Python: L252-306: 7 个 _make_tool 调用
	tgtSchema := targetSchema(targetChannels, defaultTargetChannel)

	// cron_list_jobs — 对齐 Python L222-224: list_jobs_wrapper()
	listFn := func(ctx context.Context, _ CronListJobsInput, opts ...tool.ToolOption) (map[string]any, error) {
		jobs, err := backend.ListJobs(ctx, true)
		if err != nil {
			return nil, err
		}
		return map[string]any{"jobs": jobs}, nil
	}
	registerCardProperties(listFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	registerCardProperties(listFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_list_jobs", scope, language, finalAgentID, listFn, nil))

	// cron_get_job — 对齐 Python L225-227: get_job_wrapper(job_id)
	getFn := func(ctx context.Context, input CronGetJobInput, opts ...tool.ToolOption) (map[string]any, error) {
		job, err := backend.GetJob(ctx, input.JobID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"job": job}, nil
	}
	registerCardProperties(getFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_get_job", scope, language, finalAgentID, getFn, nil))

	// cron_create_job — 对齐 Python L228-230: create_job_wrapper(**kwargs)
	// 需要 targetSchema 替换 InputParams 中的 targets 字段
	createFn := func(ctx context.Context, input CronCreateJobInput, opts ...tool.ToolOption) (map[string]any, error) {
		params := map[string]any{
			"name":                input.Name,
			"cron_expr":           input.CronExpr,
			"timezone":            input.Timezone,
			"targets":             input.Targets,
			"enabled":             input.Enabled,
			"description":         input.Description,
			"wake_offset_seconds": input.WakeOffsetSeconds,
		}
		created, err := backend.CreateJob(ctx, params, cronCtx)
		if err != nil {
			return nil, err
		}
		return created, nil
	}
	registerCardProperties(createFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_create_job", scope, language, finalAgentID, createFn, tgtSchema))

	// cron_update_job — 对齐 Python L231-233: update_job_wrapper(job_id, patch)
	updateFn := func(ctx context.Context, input CronUpdateJobInput, opts ...tool.ToolOption) (map[string]any, error) {
		updated, err := backend.UpdateJob(ctx, input.JobID, input.Patch, cronCtx)
		if err != nil {
			return nil, err
		}
		return updated, nil
	}
	registerCardProperties(updateFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_update_job", scope, language, finalAgentID, updateFn, nil))

	// cron_delete_job — 对齐 Python L234-236: delete_job_wrapper(job_id)
	deleteFn := func(ctx context.Context, input CronDeleteJobInput, opts ...tool.ToolOption) (map[string]any, error) {
		deleted, err := backend.DeleteJob(ctx, input.JobID)
		if err != nil {
			return nil, err
		}
		return map[string]any{"deleted": deleted}, nil
	}
	registerCardProperties(deleteFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_delete_job", scope, language, finalAgentID, deleteFn, nil))

	// cron_toggle_job — 对齐 Python L237-239: toggle_job_wrapper(job_id, enabled)
	toggleFn := func(ctx context.Context, input CronToggleJobInput, opts ...tool.ToolOption) (map[string]any, error) {
		toggled, err := backend.ToggleJob(ctx, input.JobID, input.Enabled)
		if err != nil {
			return nil, err
		}
		return toggled, nil
	}
	registerCardProperties(toggleFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_toggle_job", scope, language, finalAgentID, toggleFn, nil))

	// cron_preview_job — 对齐 Python L240-242: preview_job_wrapper(job_id, count=5)
	previewFn := func(ctx context.Context, input CronPreviewJobInput, opts ...tool.ToolOption) (map[string]any, error) {
		count := input.Count
		if count <= 0 {
			count = 5
		}
		runs, err := backend.PreviewJob(ctx, input.JobID, count)
		if err != nil {
			return nil, err
		}
		return map[string]any{"runs": runs}, nil
	}
	registerCardProperties(previewFn, map[string]any{cronBackendPropertiesKey: backend, cronContextPropertiesKey: cronCtx})
	result = append(result, makeLegacyTool("cron_preview_job", scope, language, finalAgentID, previewFn, nil))

	return result
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// makeLegacyTool 遗留兼容工具的辅助建卡函数。
// Python: _make_tool (cron.py L96-116)
//
// Python 逻辑：
// 1. build_tool_card(name, f"{name}_{scope}", language, agent_id=agent_id) 获取 ToolCard
// 2. 如果 target_schema 非空，获取 input_params 字典，替换 targets 属性，重建 ToolCard
// 3. 用 LocalFunction 包装工具卡与函数
func makeLegacyTool[I any](
	name string,
	scope string,
	language string,
	agentID string,
	fn func(context.Context, I, ...tool.ToolOption) (map[string]any, error),
	targetSchemaMap map[string]any,
) tool.Tool {
	card, _ := ptools.BuildToolCard(name, name+"_"+scope, language, nil, agentID)

	// Python: L106-115: Python: if target_schema is not None:
	// Python: input_params = get_tool_input_params(name, language)
	// Python: if "properties" in input_params and "targets" in input_params["properties"]:
	//     Python: input_params["properties"]["targets"] = target_schema
	//     Python: card = ToolCard(id=card.id, name=card.name, description=card.description, input_params=input_params)
	if targetSchemaMap != nil {
		provider, ok := ptools.GetToolProvider(name)
		if ok {
			inputParamsMap := provider.GetInputParams(language)
			if props, ok := inputParamsMap["properties"].(map[string]any); ok {
				if _, hasTargets := props["targets"]; hasTargets {
					props["targets"] = targetSchemaMap
					newParams, err := cschema.ParseJSONSchemaMap(inputParamsMap)
					if err == nil {
						card.InputParams = newParams
					}
				}
			}
		}
	}

	if properties, hasProperties := cardPropertiesOf(fn); hasProperties {
		if card.Properties == nil {
			card.Properties = map[string]any{}
		}
		for key, value := range properties {
			card.Properties[key] = value
		}
	}
	invokeFn, _ := tool.NewTool(fn, tool.WithToolCard(card), tool.WithToolInputParams(card.InputParams))
	return invokeFn
}

// cronBackendPropertiesKey 闭包 backend 身份在卡片属性中的键名。
const cronBackendPropertiesKey = "cron_backend"

// cronContextPropertiesKey 调用上下文在卡片属性中的键名。
const cronContextPropertiesKey = "cron_context"

// cardPropertiesOf 从工具函数的注册闭包中挖取身份属性。
// Go 无法枚举闭包捕获；改用包级登记表：factory 内每个闭包构造时登记自身
// backend/context，此处按函数指针取回（V-6 绑定链，行为等价于 Python 的
// closure 挖取）。
var cardPropertyRegistry = map[uintptr]map[string]any{}

func registerCardProperties(fn any, properties map[string]any) {
	cardPropertyRegistry[reflect.ValueOf(fn).Pointer()] = properties
}

func cardPropertiesOf(fn any) (map[string]any, bool) {
	properties, ok := cardPropertyRegistry[reflect.ValueOf(fn).Pointer()]
	return properties, ok
}

// targetSchema 构造 targets 字段的 JSON Schema。
// Python: _target_schema (cron.py L119-132)
//
// Python 逻辑：
// Python: schema = {"type": "string", "description": "Legacy compatibility target channel"}
// Python: enum_values = [str(item).strip() for item in list(target_channels or []) if str(item).strip()]
// Python: if enum_values: schema["enum"] = enum_values
// Python: if default_target_channel: schema["default"] = str(default_target_channel).strip()
func targetSchema(targetChannels []string, defaultTargetChannel string) map[string]any {
	schema := map[string]any{
		"type":        "string",
		"description": "Legacy compatibility target channel",
	}

	// enum 值：去除空白后的非空字符串
	enumValues := []string{}
	for _, ch := range targetChannels {
		trimmed := strings.TrimSpace(ch)
		if trimmed != "" {
			enumValues = append(enumValues, trimmed)
		}
	}
	if len(enumValues) > 0 {
		schema["enum"] = enumValues
	}

	// default 值
	defaultVal := strings.TrimSpace(defaultTargetChannel)
	if defaultVal != "" {
		schema["default"] = defaultVal
	}

	return schema
}
