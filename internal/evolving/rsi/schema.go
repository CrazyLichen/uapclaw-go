// Package rsi 提供跨 RSI 场景（harness 演进 / artifact 优化）共享的
// 数据契约、引擎事件与模型用量记账。
package rsi

import "encoding/json"

// schema.go —— RSI harness 与 artifact 优化共享的数据契约。
// Python: openjiuwen/rsi/schema.py（frozen dataclass → Go 私有字段 + 访问器，
// 序列化键与 Python asdict 一致；不可变性语义见 spec D2）。

// ──────────────────────────── 枚举 ────────────────────────────

// ArtifactType artifact 种类。Python: Literal["program", "paper"]
type ArtifactType string

const (
	// ArtifactTypeProgram 程序类 artifact
	ArtifactTypeProgram ArtifactType = "program"
	// ArtifactTypePaper 论文类 artifact
	ArtifactTypePaper ArtifactType = "paper"
)

// RsiScenario RSI 场景。Python: Literal["harness", "artifact"]
type RsiScenario string

const (
	// RsiScenarioHarness harness 演进场景
	RsiScenarioHarness RsiScenario = "harness"
	// RsiScenarioArtifact artifact 优化场景
	RsiScenarioArtifact RsiScenario = "artifact"
)

// RsiStatus RSI 任务状态。Python: Literal[七态]
type RsiStatus string

const (
	// RsiStatusCreated 已创建
	RsiStatusCreated RsiStatus = "created"
	// RsiStatusQueued 已排队
	RsiStatusQueued RsiStatus = "queued"
	// RsiStatusRunning 运行中
	RsiStatusRunning RsiStatus = "running"
	// RsiStatusCompleted 已完成
	RsiStatusCompleted RsiStatus = "completed"
	// RsiStatusFailed 已失败
	RsiStatusFailed RsiStatus = "failed"
	// RsiStatusPaused 已暂停
	RsiStatusPaused RsiStatus = "paused"
	// RsiStatusTerminated 已终止
	RsiStatusTerminated RsiStatus = "terminated"
)

// ModelCallStatus 一次模型调用的状态。Python: Literal["succeeded", "failed", "incomplete"]
type ModelCallStatus string

const (
	// ModelCallStatusSucceeded 成功
	ModelCallStatusSucceeded ModelCallStatus = "succeeded"
	// ModelCallStatusFailed 失败
	ModelCallStatusFailed ModelCallStatus = "failed"
	// ModelCallStatusIncomplete 未完成（中断/未上报，不编造成功）
	ModelCallStatusIncomplete ModelCallStatus = "incomplete"
)

// ──────────────────────────── 常量 ────────────────────────────

// validRsiStatuses 状态合法性表（枚举成员集合对齐 Python Literal）
var validRsiStatuses = map[RsiStatus]bool{
	RsiStatusCreated:    true,
	RsiStatusQueued:     true,
	RsiStatusRunning:    true,
	RsiStatusCompleted:  true,
	RsiStatusFailed:     true,
	RsiStatusPaused:     true,
	RsiStatusTerminated: true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// RsiTaskCreateRequest harness 与 artifact 场景共享的公开任务创建请求。
// Python: RsiTaskCreateRequest
type RsiTaskCreateRequest struct {
	scenario                RsiScenario
	artifactType            *ArtifactType
	name                    string
	artifactPath            *string
	optimizationInstruction *string
	datasetFile             *string
	searchWidth             *int
	modelRefs               map[string]string
	maxIterations           int
	webProxy                *string
}

// NewRsiTaskCreateRequest 构造任务创建请求。
func NewRsiTaskCreateRequest(
	scenario RsiScenario,
	artifactType *ArtifactType,
	name string,
	artifactPath *string,
	optimizationInstruction *string,
	datasetFile *string,
	searchWidth *int,
	modelRefs map[string]string,
	maxIterations int,
	webProxy *string,
) *RsiTaskCreateRequest {
	return &RsiTaskCreateRequest{
		scenario:                scenario,
		artifactType:            artifactType,
		name:                    name,
		artifactPath:            artifactPath,
		optimizationInstruction: optimizationInstruction,
		datasetFile:             datasetFile,
		searchWidth:             searchWidth,
		modelRefs:               modelRefs,
		maxIterations:           maxIterations,
		webProxy:                webProxy,
	}
}

// Scenario 场景。
func (r *RsiTaskCreateRequest) Scenario() RsiScenario { return r.scenario }

// ArtifactType artifact 种类（可为 nil）。
func (r *RsiTaskCreateRequest) ArtifactType() *ArtifactType { return r.artifactType }

// Name 任务名。
func (r *RsiTaskCreateRequest) Name() string { return r.name }

// ArtifactPath artifact 路径（可为 nil）。
func (r *RsiTaskCreateRequest) ArtifactPath() *string { return r.artifactPath }

// OptimizationInstruction 优化指令（可为 nil）。
func (r *RsiTaskCreateRequest) OptimizationInstruction() *string { return r.optimizationInstruction }

// DatasetFile 数据集文件（可为 nil）。
func (r *RsiTaskCreateRequest) DatasetFile() *string { return r.datasetFile }

// SearchWidth 搜索宽度（可为 nil）。
func (r *RsiTaskCreateRequest) SearchWidth() *int { return r.searchWidth }

// ModelRefs 模型引用映射。
func (r *RsiTaskCreateRequest) ModelRefs() map[string]string { return r.modelRefs }

// MaxIterations 最大迭代次数。
func (r *RsiTaskCreateRequest) MaxIterations() int { return r.maxIterations }

// WebProxy 网络代理（可为 nil）。
func (r *RsiTaskCreateRequest) WebProxy() *string { return r.webProxy }

// RsiTaskEnvelope AgentServer 为单个 RSI 任务持有的身份与配置。
// Python: RsiTaskEnvelope
type RsiTaskEnvelope struct {
	taskID       string
	runDir       string
	artifactType *ArtifactType
	config       *RsiTaskCreateRequest
}

// NewRsiTaskEnvelope 构造任务信封。
func NewRsiTaskEnvelope(taskID, runDir string, artifactType *ArtifactType, config *RsiTaskCreateRequest) *RsiTaskEnvelope {
	return &RsiTaskEnvelope{taskID: taskID, runDir: runDir, artifactType: artifactType, config: config}
}

// TaskID 任务标识。
func (e *RsiTaskEnvelope) TaskID() string { return e.taskID }

// RunDir 运行目录。
func (e *RsiTaskEnvelope) RunDir() string { return e.runDir }

// ArtifactType artifact 种类（可为 nil）。
func (e *RsiTaskEnvelope) ArtifactType() *ArtifactType { return e.artifactType }

// Config 创建请求配置。
func (e *RsiTaskEnvelope) Config() *RsiTaskCreateRequest { return e.config }

// ArtifactValidationResult 提供方专属的输入校验结果。
// Python: ArtifactValidationResult
type ArtifactValidationResult struct {
	valid  bool
	errors []map[string]string
}

// NewArtifactValidationResult 构造校验结果。
func NewArtifactValidationResult(valid bool, errors []map[string]string) *ArtifactValidationResult {
	return &ArtifactValidationResult{valid: valid, errors: errors}
}

// Valid 是否通过校验。
func (v *ArtifactValidationResult) Valid() bool { return v.valid }

// Errors 错误列表。
func (v *ArtifactValidationResult) Errors() []map[string]string { return v.errors }

// RsiUsageTokens 提供方 token 计数器；nil 表示该计数器未被上报（不得估算）。
// Python: RsiUsageTokens
type RsiUsageTokens struct {
	input    *int
	output   *int
	cacheHit *int
}

// NewRsiUsageTokens 构造 token 计数器。
func NewRsiUsageTokens(input, output, cacheHit *int) *RsiUsageTokens {
	return &RsiUsageTokens{input: input, output: output, cacheHit: cacheHit}
}

// Input 输入 token 数（含缓存命中；可为 nil）。
func (t *RsiUsageTokens) Input() *int { return t.input }

// Output 输出 token 数（推理 token 已含其中；可为 nil）。
func (t *RsiUsageTokens) Output() *int { return t.output }

// CacheHit 缓存命中 token 数（输入的子集；可为 nil）。
func (t *RsiUsageTokens) CacheHit() *int { return t.cacheHit }

// MarshalJSON 以 Python asdict 键名序列化（私有字段需显式导出）。
func (t *RsiUsageTokens) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Input    *int `json:"input"`
		Output   *int `json:"output"`
		CacheHit *int `json:"cache_hit"`
	}{t.input, t.output, t.cacheHit})
}

// RsiUsage 模型与优化引擎的累计用量。
// Python: RsiUsage
type RsiUsage struct {
	tokens       *RsiUsageTokens
	costEstimate *float64
	callCount    int
}

// NewRsiUsage 构造累计用量。
func NewRsiUsage(tokens *RsiUsageTokens, costEstimate *float64, callCount int) *RsiUsage {
	return &RsiUsage{tokens: tokens, costEstimate: costEstimate, callCount: callCount}
}

// Tokens token 计数器累计。
func (u *RsiUsage) Tokens() *RsiUsageTokens { return u.tokens }

// CostEstimate 成本估算（恒为 nil，占位字段对齐 Python）。
func (u *RsiUsage) CostEstimate() *float64 { return u.costEstimate }

// CallCount 调用次数累计。
func (u *RsiUsage) CallCount() int { return u.callCount }

// MarshalJSON 以 Python asdict 键名序列化（私有字段需显式导出）。
func (u *RsiUsage) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Tokens       *RsiUsageTokens `json:"tokens"`
		CostEstimate *float64        `json:"cost_estimate"`
		CallCount    int             `json:"call_count"`
	}{u.tokens, u.costEstimate, u.callCount})
}

// RsiModelCall 一次已完成的模型请求（不含提示词与凭据）。
// Python: RsiModelCall；status 默认 "succeeded"（空串入参时取默认）。
type RsiModelCall struct {
	model      string
	callCount  int
	tokens     *RsiUsageTokens
	status     ModelCallStatus
	durationMs *int
}

// NewRsiModelCall 构造模型调用记录；status 为空串时取 Python 默认值 succeeded。
func NewRsiModelCall(model string, callCount int, tokens *RsiUsageTokens, status ModelCallStatus, durationMs *int) *RsiModelCall {
	if status == "" {
		status = ModelCallStatusSucceeded
	}
	return &RsiModelCall{model: model, callCount: callCount, tokens: tokens, status: status, durationMs: durationMs}
}

// Model 模型名。
func (m *RsiModelCall) Model() string { return m.model }

// CallCount 调用次数（单笔恒为 1）。
func (m *RsiModelCall) CallCount() int { return m.callCount }

// Tokens 本笔 token 计数。
func (m *RsiModelCall) Tokens() *RsiUsageTokens { return m.tokens }

// Status 调用状态。
func (m *RsiModelCall) Status() ModelCallStatus { return m.status }

// DurationMs 耗时毫秒（可为 nil）。
func (m *RsiModelCall) DurationMs() *int { return m.durationMs }

// MarshalJSON 以 Python asdict 键名序列化（账本行内嵌 model_call）。
func (m *RsiModelCall) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct {
		Model      string          `json:"model"`
		CallCount  int             `json:"call_count"`
		Tokens     *RsiUsageTokens `json:"tokens"`
		Status     ModelCallStatus `json:"status"`
		DurationMs *int            `json:"duration_ms"`
	}{m.model, m.callCount, m.tokens, m.status, m.durationMs})
}

// RsiChange 单个树节点所代表变更的意图级摘要。
// Python: RsiChange
type RsiChange struct {
	group     string
	operation string
	function  *string
	target    *string
	summary   string
}

// NewRsiChange 构造变更摘要。
func NewRsiChange(group, operation string, function, target *string, summary string) *RsiChange {
	return &RsiChange{group: group, operation: operation, function: function, target: target, summary: summary}
}

// Group 变更分组。
func (c *RsiChange) Group() string { return c.group }

// Operation 变更操作。
func (c *RsiChange) Operation() string { return c.operation }

// Function 变更函数（可为 nil）。
func (c *RsiChange) Function() *string { return c.function }

// Target 变更目标（可为 nil）。
func (c *RsiChange) Target() *string { return c.target }

// Summary 摘要。
func (c *RsiChange) Summary() string { return c.summary }

// RsiTreeNode 树查询与节点事件共用的节点投影。
// Python: RsiTreeNode
type RsiTreeNode struct {
	nodeID             string
	iteration          int
	parentID           *string
	nodeType           string
	adopted            bool
	score              *float64
	summary            *string
	snapshotArtifactID *string
	reason             *string
	failureClass       *string
	changes            []RsiChange
	extra              map[string]any
}

// NewRsiTreeNode 构造树节点投影。
func NewRsiTreeNode(
	nodeID string,
	iteration int,
	parentID *string,
	nodeType string,
	adopted bool,
	score *float64,
	summary *string,
	snapshotArtifactID *string,
	reason *string,
	failureClass *string,
	changes []RsiChange,
	extra map[string]any,
) *RsiTreeNode {
	return &RsiTreeNode{
		nodeID:             nodeID,
		iteration:          iteration,
		parentID:           parentID,
		nodeType:           nodeType,
		adopted:            adopted,
		score:              score,
		summary:            summary,
		snapshotArtifactID: snapshotArtifactID,
		reason:             reason,
		failureClass:       failureClass,
		changes:            changes,
		extra:              extra,
	}
}

// NodeID 节点标识。
func (n *RsiTreeNode) NodeID() string { return n.nodeID }

// Iteration 迭代号。
func (n *RsiTreeNode) Iteration() int { return n.iteration }

// ParentID 父节点标识（根为 nil）。
func (n *RsiTreeNode) ParentID() *string { return n.parentID }

// Type 节点类型。
func (n *RsiTreeNode) Type() string { return n.nodeType }

// Adopted 是否被采纳。
func (n *RsiTreeNode) Adopted() bool { return n.adopted }

// Score 分数（可为 nil）。
func (n *RsiTreeNode) Score() *float64 { return n.score }

// Summary 摘要（可为 nil）。
func (n *RsiTreeNode) Summary() *string { return n.summary }

// SnapshotArtifactID 快照 artifact 标识（可为 nil）。
func (n *RsiTreeNode) SnapshotArtifactID() *string { return n.snapshotArtifactID }

// Reason 原因（可为 nil）。
func (n *RsiTreeNode) Reason() *string { return n.reason }

// FailureClass 失败分类（可为 nil）。
func (n *RsiTreeNode) FailureClass() *string { return n.failureClass }

// Changes 变更摘要列表。
func (n *RsiTreeNode) Changes() []RsiChange { return n.changes }

// Extra 附加信息。
func (n *RsiTreeNode) Extra() map[string]any { return n.extra }

// TreeResponse 完整持久化 RSI 树（含根与全部分支）。
// Python: TreeResponse
type TreeResponse struct {
	nodes     []RsiTreeNode
	depth     int
	iteration int
}

// NewTreeResponse 构造树响应。
func NewTreeResponse(nodes []RsiTreeNode, depth, iteration int) *TreeResponse {
	return &TreeResponse{nodes: nodes, depth: depth, iteration: iteration}
}

// Nodes 节点列表。
func (t *TreeResponse) Nodes() []RsiTreeNode { return t.nodes }

// Depth 树深度。
func (t *TreeResponse) Depth() int { return t.depth }

// Iteration 当前迭代代号。
func (t *TreeResponse) Iteration() int { return t.iteration }

// EngineResult 执行控制操作的结果。
// Python: EngineResult
type EngineResult struct {
	taskID       string
	status       RsiStatus
	finalNodeID  *string
	errorCode    *string
	errorMessage *string
}

// NewEngineResult 构造执行控制结果。
func NewEngineResult(taskID string, status RsiStatus, finalNodeID, errorCode, errorMessage *string) *EngineResult {
	return &EngineResult{taskID: taskID, status: status, finalNodeID: finalNodeID, errorCode: errorCode, errorMessage: errorMessage}
}

// TaskID 任务标识。
func (r *EngineResult) TaskID() string { return r.taskID }

// Status 结果状态。
func (r *EngineResult) Status() RsiStatus { return r.status }

// FinalNodeID 最终节点标识（可为 nil）。
func (r *EngineResult) FinalNodeID() *string { return r.finalNodeID }

// ErrorCode 错误码（可为 nil）。
func (r *EngineResult) ErrorCode() *string { return r.errorCode }

// ErrorMessage 错误消息（可为 nil）。
func (r *EngineResult) ErrorMessage() *string { return r.errorMessage }

// EngineState 单个 RSI 引擎任务的持久化当前状态。
// Python: EngineState
type EngineState struct {
	taskID          string
	status          RsiStatus
	iteration       int
	totalIterations int
	bestNodeID      *string
	score           *float64
	baseline        *float64
	usage           *RsiUsage
	updatedAt       string
	errorCode       *string
	errorMessage    *string
}

// NewEngineState 构造引擎状态。
func NewEngineState(
	taskID string,
	status RsiStatus,
	iteration, totalIterations int,
	bestNodeID *string,
	score, baseline *float64,
	usage *RsiUsage,
	updatedAt string,
	errorCode, errorMessage *string,
) *EngineState {
	return &EngineState{
		taskID:          taskID,
		status:          status,
		iteration:       iteration,
		totalIterations: totalIterations,
		bestNodeID:      bestNodeID,
		score:           score,
		baseline:        baseline,
		usage:           usage,
		updatedAt:       updatedAt,
		errorCode:       errorCode,
		errorMessage:    errorMessage,
	}
}

// TaskID 任务标识。
func (s *EngineState) TaskID() string { return s.taskID }

// Status 当前状态。
func (s *EngineState) Status() RsiStatus { return s.status }

// Iteration 当前迭代代号。
func (s *EngineState) Iteration() int { return s.iteration }

// TotalIterations 总迭代次数。
func (s *EngineState) TotalIterations() int { return s.totalIterations }

// BestNodeID 最优节点标识（可为 nil）。
func (s *EngineState) BestNodeID() *string { return s.bestNodeID }

// Score 当前分数（可为 nil）。
func (s *EngineState) Score() *float64 { return s.score }

// Baseline 基线分数（可为 nil）。
func (s *EngineState) Baseline() *float64 { return s.baseline }

// Usage 累计用量（可为 nil）。
func (s *EngineState) Usage() *RsiUsage { return s.usage }

// UpdatedAt 更新时间。
func (s *EngineState) UpdatedAt() string { return s.updatedAt }

// ErrorCode 错误码（可为 nil）。
func (s *EngineState) ErrorCode() *string { return s.errorCode }

// ErrorMessage 错误消息（可为 nil）。
func (s *EngineState) ErrorMessage() *string { return s.errorMessage }

// ArtifactRef 提供方侧可下载的任务或节点 artifact 引用。
// Python: ArtifactRef
type ArtifactRef struct {
	artifactID  string
	nodeID      *string
	name        string
	kind        string
	path        string
	sha256      *string
	downloadURL *string
}

// NewArtifactRef 构造 artifact 引用。
func NewArtifactRef(artifactID string, nodeID *string, name, kind, path string, sha256, downloadURL *string) *ArtifactRef {
	return &ArtifactRef{artifactID: artifactID, nodeID: nodeID, name: name, kind: kind, path: path, sha256: sha256, downloadURL: downloadURL}
}

// ArtifactID artifact 标识。
func (a *ArtifactRef) ArtifactID() string { return a.artifactID }

// NodeID 节点标识（可为 nil）。
func (a *ArtifactRef) NodeID() *string { return a.nodeID }

// Name 名称。
func (a *ArtifactRef) Name() string { return a.name }

// Kind 种类。
func (a *ArtifactRef) Kind() string { return a.kind }

// Path 路径。
func (a *ArtifactRef) Path() string { return a.path }

// Sha256 内容哈希（可为 nil）。
func (a *ArtifactRef) Sha256() *string { return a.sha256 }

// DownloadURL 下载地址（可为 nil）。
func (a *ArtifactRef) DownloadURL() *string { return a.downloadURL }

// EngineReport 当前或最终报告与任务的完整 artifact 索引。
// Python: EngineReport。best_score/baseline/score_details 为 paper 提供方
// 兼容字段：把当前最优与初始基线暴露给 AgentServer 以便在任务/报告响应中
// 渲染相同分数；默认值保持旧的 program 提供方与持久化报告兼容。
type EngineReport struct {
	taskID        string
	status        RsiStatus
	bestNodeID    *string
	usage         *RsiUsage
	artifactIndex []ArtifactRef
	summary       *string
	bestScore     *float64
	baseline      *float64
	scoreDetails  map[string]any
}

// NewEngineReport 构造引擎报告。
func NewEngineReport(
	taskID string,
	status RsiStatus,
	bestNodeID *string,
	usage *RsiUsage,
	artifactIndex []ArtifactRef,
	summary *string,
	bestScore, baseline *float64,
	scoreDetails map[string]any,
) *EngineReport {
	return &EngineReport{
		taskID:        taskID,
		status:        status,
		bestNodeID:    bestNodeID,
		usage:         usage,
		artifactIndex: artifactIndex,
		summary:       summary,
		bestScore:     bestScore,
		baseline:      baseline,
		scoreDetails:  scoreDetails,
	}
}

// TaskID 任务标识。
func (r *EngineReport) TaskID() string { return r.taskID }

// Status 报告状态。
func (r *EngineReport) Status() RsiStatus { return r.status }

// BestNodeID 最优节点标识（可为 nil）。
func (r *EngineReport) BestNodeID() *string { return r.bestNodeID }

// Usage 累计用量（可为 nil）。
func (r *EngineReport) Usage() *RsiUsage { return r.usage }

// ArtifactIndex artifact 索引。
func (r *EngineReport) ArtifactIndex() []ArtifactRef { return r.artifactIndex }

// Summary 摘要（可为 nil）。
func (r *EngineReport) Summary() *string { return r.summary }

// BestScore 当前最优分数（paper 兼容字段；可为 nil）。
func (r *EngineReport) BestScore() *float64 { return r.bestScore }

// Baseline 初始基线分数（paper 兼容字段；可为 nil）。
func (r *EngineReport) Baseline() *float64 { return r.baseline }

// ScoreDetails 分数明细（paper 兼容字段；可为 nil）。
func (r *EngineReport) ScoreDetails() map[string]any { return r.scoreDetails }
