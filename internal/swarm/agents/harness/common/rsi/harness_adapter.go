package rsi

// Harness Provider adapter（JiuwenSwarm RSI 服务自有）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/harness_adapter.py（217 行）。
//
// artifact RSI 已有 Provider 边界（artifact_adapter.py，ARTIFACT 轨裁剪
// D-9-1）；harness RSI 暴露的是 agent-core 编排器而非服务的可续跑 Provider
// 契约，因此本文件定义服务用于 HarnessProvider 的稳定请求形状。

import (
	"context"
	"fmt"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi"
)

// ──────────────────────────── 结构体 ────────────────────────────

// HarnessEngineRequest worker 传给 Harness Provider 的稳定请求。
// agent-core 的 IterativeSingleHarnessRequest 只聚焦编排器输入；RSI 还需要
// 任务身份、搜索参数与模型元数据，这些在此边界保留并由具体 Provider 翻译。
type HarnessEngineRequest struct {
	// TaskID 任务标识。
	TaskID string
	// DatasetFiles 数据集文件清单。
	DatasetFiles []string
	// HarnessRefsPath harness 引用文件路径。
	HarnessRefsPath string
	// OutputDir 输出目录。
	OutputDir string
	// DatasetID 数据集标识。
	DatasetID string
	// MaxIterations 最大迭代轮数。
	MaxIterations int
	// SearchWidth 搜索宽度（已废弃字段，保留兼容）。
	SearchWidth int
	// ModelRefs 模型引用。
	ModelRefs map[string]any
	// OptimizationInstruction 优化指令（可空）。
	OptimizationInstruction *string
	// Resume 是否断点续跑。
	Resume bool
	// OrchestratorConfigPath 编排器配置路径（可空）。
	OrchestratorConfigPath *string
}

// HarnessProviderContract HarnessProvider 实现的契约（对齐 Python Protocol）。
type HarnessProviderContract interface {
	// ValidateInput 引擎 load_cases 真校验。
	ValidateInput(path *string) map[string]any
	// Run 全新执行。
	Run(ctx Ctx, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error)
	// Resume 断点续跑。
	Resume(ctx Ctx, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error)
	// Pause 中途暂停（harness 暂不支持）。
	Pause(taskID string) (map[string]any, error)
	// Terminate 中途终止（harness 暂不支持；worker 走取消 runner 路径）。
	Terminate(taskID string) (map[string]any, error)
	// ReadState 读持久状态快照。
	ReadState(taskID string) map[string]any
	// ReadPublicationState 读发布状态（旧/mock provider 无则空表）。
	ReadPublicationState(taskID string) map[string]any
	// ReadReport 读报告。
	ReadReport(taskID string) map[string]any
	// GetTree 读演进树。
	GetTree(taskID string) map[string]any
	// LocateArtifact 定位产物。
	LocateArtifact(taskID string, artifactID *string) map[string]any

	// SupportsPause / SupportsResume / SupportsTerminate 能力位。
	SupportsPause() bool
	SupportsResume() bool
	SupportsTerminate() bool
}

// Ctx 引擎执行上下文别名（worker 按 task 取消）。
type Ctx = context.Context

// HarnessEngineAdapter 把一个 HarnessProvider 适配到 RSI worker 公共契约。
type HarnessEngineAdapter struct {
	provider          HarnessProviderContract
	supportsPause     bool
	supportsResume    bool
	supportsTerminate bool
}

// NewHarnessEngineAdapter 构造适配器（能力位取自 provider 缺省 true）。
func NewHarnessEngineAdapter(provider HarnessProviderContract) *HarnessEngineAdapter {
	a := &HarnessEngineAdapter{provider: provider}
	a.supportsPause = provider.SupportsPause()
	a.supportsResume = provider.SupportsResume()
	a.supportsTerminate = provider.SupportsTerminate()
	return a
}

// Provider 返回底层 provider（worker/装配根读取发布缝用）。
func (a *HarnessEngineAdapter) Provider() HarnessProviderContract { return a.provider }

// SupportsPause 能力位。
func (a *HarnessEngineAdapter) SupportsPause() bool { return a.supportsPause }

// SupportsResume 能力位。
func (a *HarnessEngineAdapter) SupportsResume() bool { return a.supportsResume }

// SupportsTerminate 能力位。
func (a *HarnessEngineAdapter) SupportsTerminate() bool { return a.supportsTerminate }

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildHarnessEngineRequest 任务视图 → 引擎请求（对齐 Python build_request）。
func BuildHarnessEngineRequest(task RsiTaskView, resume bool) (HarnessEngineRequest, error) {
	if strings.ToUpper(strings.TrimSpace(task.Scenario)) != "HARNESS" {
		return HarnessEngineRequest{}, NewScenarioNotSupported("HarnessEngineAdapter 只能处理 HARNESS 任务")
	}
	if task.InputFile == nil || *task.InputFile == "" {
		return HarnessEngineRequest{}, NewScenarioNotSupported("harness 任务缺少 input_file")
	}
	config := task.Config
	refsPath := strings.TrimSpace(valueToString(config["harness_refs_path"]))
	orchestratorPath := strings.TrimSpace(valueToString(config["orchestrator_config_path"]))
	var orchPtr *string
	if orchestratorPath != "" {
		orchPtr = &orchestratorPath
	}
	datasetID := valueToString(config["dataset_id"])
	if datasetID == "" {
		datasetID = fmt.Sprintf("%s:dataset", task.TaskID)
	}
	maxIterations := task.MaxIterations
	if maxIterations < 1 {
		maxIterations = 1
	}
	searchWidth := task.SearchWidth
	if searchWidth < 1 {
		searchWidth = 1
	}
	modelRefs := map[string]any{}
	for k, v := range task.ModelRefs {
		modelRefs[k] = v
	}
	return HarnessEngineRequest{
		TaskID:                  task.TaskID,
		DatasetFiles:            []string{*task.InputFile},
		HarnessRefsPath:         refsPath,
		OutputDir:               task.RunDir,
		DatasetID:               datasetID,
		MaxIterations:           maxIterations,
		SearchWidth:             searchWidth,
		ModelRefs:               modelRefs,
		OptimizationInstruction: task.OptimizationInstruction,
		Resume:                  resume,
		OrchestratorConfigPath:  orchPtr,
	}, nil
}

// BuildRequest 任务视图 → 引擎请求（worker 契约方法）。
func (a *HarnessEngineAdapter) BuildRequest(task RsiTaskView, resume bool) (HarnessEngineRequest, error) {
	return BuildHarnessEngineRequest(task, resume)
}

// ValidateInput 场景守卫 + provider 校验结果归一（对齐 Python validate_input）。
func (a *HarnessEngineAdapter) ValidateInput(path *string, scenario string, artifactType *string) (RsiDatasetResult, error) {
	if strings.ToUpper(strings.TrimSpace(scenario)) != "HARNESS" {
		return RsiDatasetResult{}, NewScenarioNotSupported("HarnessEngineAdapter 只能处理 HARNESS 场景")
	}
	if artifactType != nil && *artifactType != "" {
		return RsiDatasetResult{}, NewScenarioNotSupported("harness 场景不接受 artifact_type")
	}
	return datasetResult(a.provider.ValidateInput(path))
}

// Run 全新执行转发。
func (a *HarnessEngineAdapter) Run(ctx Ctx, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error) {
	return a.provider.Run(ctx, request, onEvent)
}

// Resume 续跑转发。
func (a *HarnessEngineAdapter) Resume(ctx Ctx, request HarnessEngineRequest, onEvent rsi.OnEvent) (map[string]any, error) {
	return a.provider.Resume(ctx, request, onEvent)
}

// Pause 转发（HARNESS 真身报 NotReady）。
func (a *HarnessEngineAdapter) Pause(taskID string) (map[string]any, error) {
	return a.provider.Pause(taskID)
}

// Terminate 转发。
func (a *HarnessEngineAdapter) Terminate(taskID string) (map[string]any, error) {
	return a.provider.Terminate(taskID)
}

// ReadState 状态快照转发。
func (a *HarnessEngineAdapter) ReadState(taskID string) map[string]any {
	return a.provider.ReadState(taskID)
}

// ReadPublicationState 发布状态转发：旧/mock provider 无此缝时返回空表
// （installer 回退任务 run 状态文件）。
func (a *HarnessEngineAdapter) ReadPublicationState(taskID string) map[string]any {
	value := a.provider.ReadPublicationState(taskID)
	if value == nil {
		return map[string]any{}
	}
	return value
}

// ReadReport 报告转发。
func (a *HarnessEngineAdapter) ReadReport(taskID string) map[string]any {
	return a.provider.ReadReport(taskID)
}

// GetTree 演进树转发。
func (a *HarnessEngineAdapter) GetTree(taskID string) map[string]any {
	return a.provider.GetTree(taskID)
}

// LocateArtifact 产物定位转发。
func (a *HarnessEngineAdapter) LocateArtifact(taskID string, artifactID *string) map[string]any {
	return a.provider.LocateArtifact(taskID, artifactID)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// isBoolValue bool 值判定（对齐 Python isinstance(bool) 排除）。
func isBoolValue(v any) bool {
	_, ok := v.(bool)
	return ok
}

// datasetResult 校验结果在 adapter 边界归一（对齐 Python _dataset_result）。
func datasetResult(raw map[string]any) (RsiDatasetResult, error) {
	if raw == nil {
		return RsiDatasetResult{}, &RsiError{Code: "INTERNAL_ERROR", Message: "HarnessProvider 返回了无法识别的校验结果"}
	}
	errors := []map[string]string{}
	if items, ok := raw["errors"].([]any); ok {
		for _, item := range items {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			reason := firstNonEmpty(valueToString(m["message"]), valueToString(m["reason"]), "输入校验失败")
			errors = append(errors, map[string]string{
				"reason": reason,
				"code":   firstNonEmpty(valueToString(m["code"]), "DATASET_INVALID"),
			})
		}
	}
	var sampleCount *int
	if !isBoolValue(raw["sample_count"]) {
		// 宽松 int 转换（对齐 Python try int(...) except → None）。
		sc := safeInt(raw["sample_count"])
		sampleCount = &sc
	}
	return RsiDatasetResult{Valid: boolValue(raw["valid"]), SampleCount: sampleCount, Errors: errors}, nil
}
