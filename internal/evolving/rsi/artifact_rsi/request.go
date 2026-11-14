package artifactrsi

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ArtifactRequestError 公开请求校验/构建失败的聚合错误。
// Python: raise ValueError([error dicts]) / raise TypeError(...)
type ArtifactRequestError struct {
	errors []map[string]string
}

// Error 返回首条错误的 code: message 形式（供日志使用）。
func (e *ArtifactRequestError) Error() string {
	if len(e.errors) == 0 {
		return "artifact request error"
	}
	return e.errors[0]["code"] + ": " + e.errors[0]["message"]
}

// Errors 完整错误列表。
func (e *ArtifactRequestError) Errors() []map[string]string { return e.errors }

// ArtifactEngineRequest 程序与论文优化器共享的提供方侧工单。
// model 是进程内已初始化的 Model 服务；公开任务请求只携带模型资源 ID，
// 由 AgentServer 在构造本工单前解析完成。Python: frozen dataclass（不可变）。
type ArtifactEngineRequest struct {
	taskID                  string
	runDir                  string
	artifactPath            *string
	model                   *llm.Model
	maxIterations           int
	optimizationInstruction *string
	webProxy                *string
}

// NewArtifactEngineRequest 构造工单。
func NewArtifactEngineRequest(
	taskID, runDir string,
	artifactPath *string,
	model *llm.Model,
	maxIterations int,
	optimizationInstruction, webProxy *string,
) *ArtifactEngineRequest {
	return &ArtifactEngineRequest{
		taskID:                  taskID,
		runDir:                  runDir,
		artifactPath:            artifactPath,
		model:                   model,
		maxIterations:           maxIterations,
		optimizationInstruction: optimizationInstruction,
		webProxy:                webProxy,
	}
}

// TaskID 任务标识。
func (r *ArtifactEngineRequest) TaskID() string { return r.taskID }

// RunDir 运行目录。
func (r *ArtifactEngineRequest) RunDir() string { return r.runDir }

// ArtifactPath artifact 路径（可为 nil）。
func (r *ArtifactEngineRequest) ArtifactPath() *string { return r.artifactPath }

// Model 已初始化的模型服务实例。
func (r *ArtifactEngineRequest) Model() *llm.Model { return r.model }

// MaxIterations 最大迭代次数。
func (r *ArtifactEngineRequest) MaxIterations() int { return r.maxIterations }

// OptimizationInstruction 优化指令（可为 nil；仅 paper 保留）。
func (r *ArtifactEngineRequest) OptimizationInstruction() *string { return r.optimizationInstruction }

// WebProxy 网络代理（可为 nil；仅 paper 保留）。
func (r *ArtifactEngineRequest) WebProxy() *string { return r.webProxy }

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateArtifactTaskRequest 校验 artifact 任务的公开请求跨字段规则。
// 只校验公开请求形状；不读 artifact_path、不检查提供方专属文件——
// 那是所选 Provider 的 ValidateInput 的职责。
// Python: validate_artifact_task_request（request.py:43-129 一比一）
func ValidateArtifactTaskRequest(request *rsi.RsiTaskCreateRequest) *rsi.ArtifactValidationResult {
	var errs []map[string]string
	add := func(code, msg string) { errs = append(errs, map[string]string{"code": code, "message": msg}) }

	// Python 逐条 if 独立追加，顺序即返回顺序
	if request.Scenario() != rsi.RsiScenarioArtifact {
		add("ARTIFACT_SCENARIO_REQUIRED", `artifact optimization requests must set scenario to "artifact"`)
	}
	at := request.ArtifactType()
	if at == nil || (*at != rsi.ArtifactTypeProgram && *at != rsi.ArtifactTypePaper) {
		add("ARTIFACT_TYPE_REQUIRED", `artifact optimization requests must set artifact_type to "program" or "paper"`)
	}
	if request.WebProxy() != nil && (at == nil || *at != rsi.ArtifactTypePaper) {
		add("WEB_PROXY_UNSUPPORTED", "web_proxy is supported only for paper optimization")
	}
	if !nonEmpty(strPtr(request.Name())) {
		add("NAME_REQUIRED", "task name must be a non-empty string")
	}
	refs := request.ModelRefs()
	if refs == nil {
		add("MODEL_REFS_INVALID", "model_refs must be a mapping")
	} else if !nonEmpty(strPtr(refs["optimizer"])) {
		add("OPTIMIZER_MODEL_REQUIRED", `model_refs must contain a non-empty "optimizer" entry`)
	}
	// Go 侧 MaxIterations 为 int，Python 的 bool/非 int 检查在类型层不可能，仅保留 <1 分支
	if request.MaxIterations() < 1 {
		add("MAX_ITERATIONS_INVALID", "max_iterations must be at least 1")
	}

	if at != nil && *at == rsi.ArtifactTypeProgram {
		if !nonEmpty(request.ArtifactPath()) {
			add("PROGRAM_ARTIFACT_PATH_REQUIRED", "program optimization requires a non-empty artifact_path")
		}
		if request.OptimizationInstruction() != nil {
			add("PROGRAM_INSTRUCTION_UNSUPPORTED", "program optimization does not accept optimization_instruction")
		}
	} else if at != nil && *at == rsi.ArtifactTypePaper {
		if !nonEmpty(request.ArtifactPath()) && !nonEmpty(request.OptimizationInstruction()) {
			add("PAPER_INPUT_REQUIRED", "paper optimization requires artifact_path or optimization_instruction")
		}
	}

	if request.DatasetFile() != nil {
		add("DATASET_FILE_UNSUPPORTED", "dataset_file is not supported by artifact optimization")
	}
	if request.SearchWidth() != nil {
		add("SEARCH_WIDTH_UNSUPPORTED", "search_width is not supported by artifact optimization")
	}

	return rsi.NewArtifactValidationResult(len(errs) == 0, errs)
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nonEmpty Python _non_empty：str 且 strip 后非空。
func nonEmpty(v *string) bool {
	return v != nil && strings.TrimSpace(*v) != ""
}

// normalizeWebProxy 复刻 str(getattr(config, "web_proxy", "") or "").strip() or None。
func normalizeWebProxy(v *string) *string {
	if v == nil {
		return nil
	}
	trimmed := strings.TrimSpace(*v)
	if trimmed == "" {
		return nil
	}
	return &trimmed
}


