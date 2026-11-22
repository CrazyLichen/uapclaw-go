package rsi

// RSI 服务域公共模型（与 web 契约 v0.3 / 内部接口 v3 对齐）。
// 对应 Python：jiuwenswarm/agents/harness/common/rsi/models.py（331 行）。
//
// - 枚举严格采用 web §3.3 全大写：status / scenario / artifact_type / kind。
// - usage/token 统一结构（§3.4）。
// - RsiTask 为 adapter 只读视图 + 持久化对象二合一（内部 v3 §4.1 出参）。

import (
	cryptorand "crypto/rand"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// ──────────────────────────── 常量 ────────────────────────────

// TaskIDPrefix 任务 ID 前缀（Python TASK_ID_PREFIX）。
const TaskIDPrefix = "rsi-"

// ──────────────────────────── 结构体 ────────────────────────────

// Tokens §3.4 token 明细。
type Tokens struct {
	// Input 输入 token 数。
	Input int `json:"input"`
	// Output 输出 token 数。
	Output int `json:"output"`
	// CacheHit 缓存命中 token 数。
	CacheHit int `json:"cache_hit"`
}

// ToDict 对齐 Python Tokens.to_dict。
func (t Tokens) ToDict() map[string]int {
	return map[string]int{"input": t.Input, "output": t.Output, "cache_hit": t.CacheHit}
}

// Usage §3.4 usage 统一结构。
type Usage struct {
	// Tokens token 明细。
	Tokens Tokens `json:"tokens"`
	// CostEstimate 成本估算。
	CostEstimate float64 `json:"cost_estimate"`
	// CallCount 调用次数。
	CallCount int `json:"call_count"`
}

// Merge 聚合另一份用量（服务侧聚合，批合并属后续优化项）。
func (u *Usage) Merge(other Usage) {
	u.Tokens.Input += other.Tokens.Input
	u.Tokens.Output += other.Tokens.Output
	u.Tokens.CacheHit += other.Tokens.CacheHit
	u.CostEstimate += other.CostEstimate
	u.CallCount += other.CallCount
}

// ToDict 对齐 Python Usage.to_dict。
func (u Usage) ToDict() map[string]any {
	return map[string]any{
		"tokens":        u.Tokens.ToDict(),
		"cost_estimate": u.CostEstimate,
		"call_count":    u.CallCount,
	}
}

// RsiModelCall progress.usage 事件载荷（内部 v3 §3.3）。
type RsiModelCall struct {
	// Model 模型名。
	Model string `json:"model"`
	// CallCount 调用次数（默认 1）。
	CallCount int `json:"call_count"`
	// Tokens token 明细。
	Tokens Tokens `json:"tokens"`
}

// RsiTask 任务存储对象（内部 v3 §4.1 出参 + 持久化字段）。
type RsiTask struct {
	// TaskID 任务标识（rsi-<uuid8>）。
	TaskID string `json:"task_id"`
	// Name 任务名。
	Name string `json:"name"`
	// Scenario 场景（HARNESS / ARTIFACT）。
	Scenario string `json:"scenario"`
	// Status 任务状态（TaskStatus 值）。
	Status string `json:"status"`
	// CreatedAt 创建时间（UTC RFC3339 秒级）。
	CreatedAt string `json:"created_at"`
	// ArtifactType 产物类型（PAPER / PROGRAM，HARNESS 场景可为 nil）。
	ArtifactType *string `json:"artifact_type"`
	// InputFile 输入文件路径。
	InputFile *string `json:"input_file"`
	// ModelRefs 模型引用（optimizer/tester）。
	ModelRefs map[string]any `json:"model_refs"`
	// MaxIterations 最大迭代轮数。
	MaxIterations int `json:"max_iterations"`
	// SearchWidth 已废弃：新 task.json 序列化不再落盘（Python to_dict 显式 pop），
	// 旧文件仍可读入。见 MarshalJSON。
	SearchWidth int `json:"-"`
	// OptimizationInstruction 优化指令。
	OptimizationInstruction *string `json:"optimization_instruction"`
	// ArtifactPath 产物路径。
	ArtifactPath *string `json:"artifact_path"`
	// Config 任务配置（含 results/web_proxy/rsi_session_id 等）。
	Config map[string]any `json:"config"`
	// RunDir 运行目录。
	RunDir string `json:"run_dir"`
	// UpdatedAt 最近更新时间。
	UpdatedAt *string `json:"updated_at"`
	// StatusHistory 状态迁移历史（[{from,to,cause,at}]）。
	StatusHistory []map[string]any `json:"status_history"`
}

// MarshalJSON 对齐 Python to_dict：序列化时剔除已废弃的 search_width。
func (t RsiTask) MarshalJSON() ([]byte, error) {
	type alias RsiTask
	payload, err := json.Marshal(alias(t))
	if err != nil {
		return nil, err
	}
	// alias 上 SearchWidth 无 json 标签会以字段名落盘，需显式剔除。
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	delete(m, "SearchWidth")
	return json.Marshal(m)
}

// UnmarshalJSON 对齐 Python from_dict：已知键才收入（未知键丢弃），
// search_width 兼容旧文件读入。
func (t *RsiTask) UnmarshalJSON(data []byte) error {
	type alias RsiTask
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*t = RsiTask(a)
	// Python from_dict 的 known 集合含 search_width：旧文件读入恢复字段值。
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if v, ok := raw["search_width"]; ok {
		_ = json.Unmarshal(v, &t.SearchWidth)
	}
	return nil
}

// ListProjection ``rsi.task.list`` 六字段投影（web §6.2）。
func (t *RsiTask) ListProjection() map[string]any {
	return map[string]any{
		"task_id":       t.TaskID,
		"name":          t.Name,
		"scenario":      t.Scenario,
		"artifact_type": nullableAny(t.ArtifactType),
		"status":        t.Status,
		"created_at":    t.CreatedAt,
	}
}

// GetProjection ``rsi.task.get`` 出参（web §6.3）。
// progress/usage/bestArtifact 由调用方注入（对齐 Python 可空参数）。
func (t *RsiTask) GetProjection(progress, usage, bestArtifact map[string]any) map[string]any {
	config := map[string]any{
		"model": map[string]any{
			"optimizer": t.ModelRefs["optimizer"],
			"tester":    t.ModelRefs["tester"],
		},
		"input_file":              nullableAny(t.InputFile),
		"max_iterations":          t.MaxIterations,
		"optimization_instruction": nullableAny(t.OptimizationInstruction),
		"artifact_path":           nullableAny(t.ArtifactPath),
		// URL 可能含代理凭据：只返回是否配置了任务级代理。
		"web_proxy_configured": t.Config["web_proxy"] != nil,
	}
	payload := map[string]any{
		"task_id":       t.TaskID,
		"name":          t.Name,
		"scenario":      t.Scenario,
		"artifact_type": nullableAny(t.ArtifactType),
		"status":        t.Status,
		"config":        config,
		"progress":      anyMap(progress),
		"best_artifact": anyMap(bestArtifact),
	}
	if usage != nil {
		payload["usage"] = usage
	}
	if t.Status == string(TaskStatusFailed) {
		if reason := t.FailureReasonText(); reason != "" {
			payload["failure_reason"] = reason
		}
	}
	return payload
}

// FailureReasonText 返回已失败任务最可行动的持久化原因。
// 对齐 Python RsiTask.failure_reason_text：状态历史中最后一次迁入 FAILED 的
// cause 与 config.results 结构化原因二选一（结果原因优先于泛化历史原因）。
func (t *RsiTask) FailureReasonText() string {
	if t.Status != string(TaskStatusFailed) {
		return ""
	}
	historyReason := ""
	for i := len(t.StatusHistory) - 1; i >= 0; i-- {
		item := t.StatusHistory[i]
		if valueToString(item["to"]) != string(TaskStatusFailed) {
			continue
		}
		historyReason = strings_TrimSpace(valueToString(item["cause"]))
		break
	}
	results, _ := t.Config["results"].(map[string]any)
	resultReason := FailureReason(results)
	if resultReason != "" && (historyReason == "" || isGenericFailureReason(historyReason)) {
		return resultReason
	}
	if historyReason != "" {
		return historyReason
	}
	return resultReason
}

// ToTaskView 生成 adapter 只读视图（对齐 Python to_taskview）。
func (t *RsiTask) ToTaskView() RsiTaskView {
	return RsiTaskView{
		TaskID:                  t.TaskID,
		Scenario:                t.Scenario,
		ArtifactType:            t.ArtifactType,
		InputFile:               t.InputFile,
		ModelRefs:               t.ModelRefs,
		MaxIterations:           t.MaxIterations,
		SearchWidth:             t.SearchWidth,
		Config:                  t.Config,
		RunDir:                  t.RunDir,
		OptimizationInstruction: t.OptimizationInstruction,
		ArtifactPath:            t.ArtifactPath,
	}
}

// RsiTaskView adapter 只读任务视图（内部 v3 §5.1 RsiTask Protocol 字段）。
type RsiTaskView struct {
	// TaskID 任务标识。
	TaskID string
	// Scenario 场景。
	Scenario string
	// ArtifactType 产物类型。
	ArtifactType *string
	// InputFile 输入文件。
	InputFile *string
	// ModelRefs 模型引用。
	ModelRefs map[string]any
	// MaxIterations 最大迭代轮数。
	MaxIterations int
	// SearchWidth 已废弃字段，保留兼容旧构造位。
	SearchWidth int
	// Config 任务配置。
	Config map[string]any
	// RunDir 运行目录。
	RunDir string
	// OptimizationInstruction 优化指令。
	OptimizationInstruction *string
	// ArtifactPath 产物路径。
	ArtifactPath *string
}

// RsiDatasetResult ``rsi.dataset.validate`` 出参（adapter validate_input）。
type RsiDatasetResult struct {
	// Valid 是否有效。
	Valid bool `json:"valid"`
	// SampleCount 样本数（可空）。
	SampleCount *int `json:"sample_count"`
	// Errors 错误明细列表。
	Errors []map[string]string `json:"errors"`
}

// ToDict 对齐 Python RsiDatasetResult.to_dict。
func (r RsiDatasetResult) ToDict() map[string]any {
	if r.Errors == nil {
		r.Errors = []map[string]string{}
	}
	return map[string]any{"valid": r.Valid, "sample_count": nullableAny(r.SampleCount), "errors": r.Errors}
}

// RsiArtifactPath ``node.created.artifacts[]`` 元素（内部 v3 §4.5）。
type RsiArtifactPath struct {
	// Role 角色名。
	Role string `json:"role"`
	// Path 产物路径。
	Path string `json:"path"`
	// Format 产物格式。
	Format string `json:"format"`
}

// RsiTreeNode 演进树节点（web §9.1 字段）。
type RsiTreeNode struct {
	// NodeID 节点标识。
	NodeID string `json:"node_id"`
	// Iteration 迭代轮次。
	Iteration int `json:"iteration"`
	// ParentID 父节点标识。
	ParentID *string `json:"parent_id"`
	// Type 节点类型。
	Type string `json:"type"`
	// Adopted 是否被采纳。
	Adopted bool `json:"adopted"`
	// Score 评分（可空）。
	Score *float64 `json:"score"`
	// Description 描述。
	Description *string `json:"description"`
	// SnapshotArtifactID 快照产物标识。
	SnapshotArtifactID *string `json:"snapshot_artifact_id"`
	// FailureReason 失败原因。
	FailureReason *string `json:"failure_reason"`
	// FailureClass 失败分类。
	FailureClass *string `json:"failure_class"`
	// Changes 变更明细。
	Changes []map[string]string `json:"changes"`
	// Extra 扩展字段。
	Extra map[string]any `json:"extra"`
}

// ToDict 对齐 Python RsiTreeNode.to_dict。
func (n RsiTreeNode) ToDict() map[string]any {
	payload, err := structToMap(n)
	if err != nil {
		return map[string]any{}
	}
	return payload
}

// FromTreeNodeDict 对齐 Python RsiTreeNode.from_dict：已知键才收入。
func FromTreeNodeDict(data map[string]any) RsiTreeNode {
	node := RsiTreeNode{}
	payload, err := json.Marshal(data)
	if err != nil {
		return node
	}
	_ = json.Unmarshal(payload, &node)
	return node
}

// RsiArtifactFile RsiArtifactService.locate 出参（内部 v3 §4.5）。
type RsiArtifactFile struct {
	// Path 文件路径。
	Path string `json:"path"`
	// Kind 文件类别。
	Kind string `json:"kind"`
	// IsBest 是否最优产物。
	IsBest bool `json:"is_best"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskStatus 任务状态全集（web §3.3）。
type TaskStatus string

const (
	// TaskStatusCreated 已创建。
	TaskStatusCreated TaskStatus = "CREATED"
	// TaskStatusQueued 已排队。
	TaskStatusQueued TaskStatus = "QUEUED"
	// TaskStatusRunning 运行中。
	TaskStatusRunning TaskStatus = "RUNNING"
	// TaskStatusCompleted 已完成。
	TaskStatusCompleted TaskStatus = "COMPLETED"
	// TaskStatusFailed 已失败。
	TaskStatusFailed TaskStatus = "FAILED"
	// TaskStatusPaused 已暂停。
	TaskStatusPaused TaskStatus = "PAUSED"
	// TaskStatusTerminated 已终止。
	TaskStatusTerminated TaskStatus = "TERMINATED"
)

// TerminalSet 终态集合（对齐 Python TaskStatus.terminal()）。
func TerminalSet() map[TaskStatus]bool {
	return map[TaskStatus]bool{TaskStatusCompleted: true, TaskStatusFailed: true, TaskStatusTerminated: true}
}

// Scenario 场景枚举（成员完整保留——规则 9.2.4；本批只消费 HARNESS 轨）。
type Scenario string

const (
	// ScenarioHarness HARNESS 场景。
	ScenarioHarness Scenario = "HARNESS"
	// ScenarioArtifact ARTIFACT 场景（真身随 17.3/17.5）。
	ScenarioArtifact Scenario = "ARTIFACT"
)

// ArtifactType 产物类型枚举（成员完整保留）。
type ArtifactType string

const (
	// ArtifactTypePaper PAPER（真身随 17.5）。
	ArtifactTypePaper ArtifactType = "PAPER"
	// ArtifactTypeProgram PROGRAM（真身随 17.3）。
	ArtifactTypeProgram ArtifactType = "PROGRAM"
)

// ArtifactKind 产物种类枚举。
type ArtifactKind string

const (
	// ArtifactKindHarnessPlugin harness 插件包。
	ArtifactKindHarnessPlugin ArtifactKind = "harness_plugin"
	// ArtifactKindArtifactPackage 通用产物包。
	ArtifactKindArtifactPackage ArtifactKind = "artifact_package"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GenerateTaskID 生成 ``rsi-<uuid8>`` 任务 ID（内部 v3 §4.1）。
func GenerateTaskID() string {
	return fmt.Sprintf("%s%s", TaskIDPrefix, newUUID8())
}

// UtcnowISO UTC RFC3339 秒级时间戳（对齐 Python utcnow_iso）。
func UtcnowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05+00:00")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// newUUID8 生成 8 位 hex 随机段（对齐 uuid.uuid4().hex[:8]）。
func newUUID8() string {
	var b [4]byte
	for i := range b {
		b[i] = uuidByte(i)
	}
	return fmt.Sprintf("%02x%02x%02x%02x", b[0], b[1], b[2], b[3])
}

// isGenericFailureReason 对齐 Python _is_generic_failure_reason：
// 泛化历史原因要让位给结构化结果原因。
func isGenericFailureReason(reason string) bool {
	normalized := strings.ToLower(strings.TrimSpace(reason))
	switch normalized {
	case "provider.failed", "provider_snapshot.failed", "provider 输入校验失败", "provider returned failure":
		return true
	}
	return strings.HasPrefix(normalized, "provider.failed") || strings.HasPrefix(normalized, "provider_snapshot.failed")
}

// strings_TrimSpace 兼容别名（保持调用点语义显式）。
func strings_TrimSpace(s string) string { return strings.TrimSpace(s) }

// asciiLower ASCII 小写归一。
func asciiLower(s string) string { return strings.ToLower(s) }

// uuidByte 取一个加密随机字节（uuid.uuid4 来源）。
func uuidByte(_ int) byte {
	var buf [1]byte
	if _, err := cryptorand.Read(buf[:]); err != nil {
		return 0
	}
	return buf[0]
}

// nullableAny 指针转 JSON 可空值。
func nullableAny[T any](p *T) any {
	if p == nil {
		return nil
	}
	return *p
}

// anyMap nil map 归一为 nil any（对齐 Python None 缺省）。
func anyMap(m map[string]any) any {
	if m == nil {
		return nil
	}
	return m
}

// structToMap struct 经 JSON 往返转 map（对齐 Python asdict）。
func structToMap(v any) (map[string]any, error) {
	payload, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(payload, &m); err != nil {
		return nil, err
	}
	return m, nil
}
