package artifactrsi

import (
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
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
