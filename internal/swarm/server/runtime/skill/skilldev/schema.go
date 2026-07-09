package skilldev

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"sort"
	"time"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillDevEvent Pipeline 内部事件，最终被序列化为 AgentResponseChunk 推送给前端。
type SkillDevEvent struct {
	// EventType 事件类型
	EventType SkillDevEventType `json:"event_type"`
	// Payload 事件负载
	Payload map[string]any `json:"payload"`
	// TaskID 任务标识
	TaskID string `json:"task_id"`
}

// SkillDevState Pipeline 运行时状态，在请求执行期间驻内存，在阶段边界通过 StateStore checkpoint。
type SkillDevState struct {
	// TaskID 任务标识
	TaskID string `json:"task_id"`
	// Stage 当前阶段
	Stage SkillDevStage `json:"stage"`
	// Mode 任务入口模式
	Mode SkillDevTaskMode `json:"mode"`
	// Iteration 当前改进轮次（从 0 开始）
	Iteration int `json:"iteration"`

	// Input 输入参数
	Input map[string]any `json:"input"`

	// ReferenceTexts 资源文件解析后的文本
	ReferenceTexts []string `json:"reference_texts"`
	// ExistingSkillMD 已有 SKILL.md 内容（可空）
	ExistingSkillMD *string `json:"existing_skill_md"`
	// Plan PLAN 阶段产出
	Plan map[string]any `json:"plan"`
	// PlanConfirmedAt Plan 确认时间
	PlanConfirmedAt *string `json:"plan_confirmed_at"`
	// Evals TEST_DESIGN 阶段产出
	Evals map[string]any `json:"evals"`
	// EvalResults EVALUATE 阶段产出
	EvalResults map[string]any `json:"eval_results"`
	// FeedbackHistory 每轮改进的用户反馈
	FeedbackHistory []map[string]any `json:"feedback_history"`

	// DescOptimizeResult run_loop 输出（best_description, history）
	DescOptimizeResult map[string]any `json:"desc_optimize_result"`

	// ZipPath 打包产物路径
	ZipPath *string `json:"zip_path"`
	// ZipSize 打包产物大小
	ZipSize int `json:"zip_size"`

	// CreatedAt 创建时间
	CreatedAt string `json:"created_at"`
	// UpdatedAt 更新时间
	UpdatedAt string `json:"updated_at"`
	// Error 错误信息
	Error *string `json:"error"`
}

// SuspensionAction 挂起点按钮动作
type SuspensionAction struct {
	// ID 动作标识
	ID string `json:"id"`
	// Label 按钮文本
	Label string `json:"label"`
	// Style 按钮样式
	Style string `json:"style"`
}

// SuspensionConfig 挂起点的声明式配置。
//
// Pipeline 到达挂起点时：
// 1. 推送 CONFIRM_REQUEST 事件（前端据此弹出确认框）
// 2. Checkpoint 当前状态并暂停
//
// 恢复时（前端通过 skilldev.respond 统一入口）：
// 1. 调用 on_resume 更新状态
// 2. 跳转到 next_stage
type SuspensionConfig struct {
	// ConfirmType 标识确认类型（前端用于区分弹框样式）
	ConfirmType string `json:"confirm_type"`
	// Title 弹框标题
	Title string `json:"title"`
	// Message 弹框描述文字
	Message string `json:"message"`
	// Actions 按钮列表
	Actions []SuspensionAction `json:"actions"`
	// ExtractData 从 state 提取展示给前端的数据
	ExtractData func(state *SkillDevState) map[string]any
	// OnResume 根据用户响应更新 state
	OnResume func(state *SkillDevState, data map[string]any)
	// NextStage 下一阶段（固定值），与 NextStageFunc 互斥
	NextStage SkillDevStage
	// NextStageFunc 下一阶段（动态函数，根据 data 决定），与 NextStage 互斥
	NextStageFunc func(data map[string]any) SkillDevStage
}

// StageGroup 一组后端阶段的展示配置。
type StageGroup struct {
	// ID 分组标识
	ID string `json:"id"`
	// Label 分组标签
	Label string `json:"label"`
	// Stages 包含的阶段集合
	Stages map[SkillDevStage]bool
	// Modes 适用的任务模式（nil 表示所有模式都展示）
	Modes map[SkillDevTaskMode]bool
}

// EvalCase 单个测试用例。
type EvalCase struct {
	// ID 用例标识
	ID int `json:"id"`
	// Prompt 模拟真实用户输入
	Prompt string `json:"prompt"`
	// ExpectedOutput 预期结果的人可读描述
	ExpectedOutput string `json:"expected_output"`
	// Files 输入文件路径
	Files []string `json:"files"`
	// Expectations 可客观验证的声明
	Expectations []string `json:"expectations"`
}

// EvalSet 完整的测试集。
type EvalSet struct {
	// SkillName 技能名称
	SkillName string `json:"skill_name"`
	// Evals 测试用例列表
	Evals []EvalCase `json:"evals"`
}

// GradingExpectation 单条 assertion 的评分结果。
type GradingExpectation struct {
	// Text 断言原文
	Text string `json:"text"`
	// Passed 是否通过
	Passed bool `json:"passed"`
	// Evidence 具体证据引用
	Evidence string `json:"evidence"`
}

// GradingResult 单次运行的评分结果（grading.json）。
type GradingResult struct {
	// Expectations 评分详情列表
	Expectations []GradingExpectation `json:"expectations"`
	// PassRate 通过率
	PassRate float64 `json:"pass_rate"`
	// PassedCount 通过数
	PassedCount int `json:"passed_count"`
	// FailedCount 失败数
	FailedCount int `json:"failed_count"`
}

// RunTiming 单次运行的耗时数据（timing.json）。
type RunTiming struct {
	// TotalTokens 总 token 数
	TotalTokens int `json:"total_tokens"`
	// DurationMs 耗时毫秒
	DurationMs int `json:"duration_ms"`
	// TotalDurationSeconds 总耗时秒
	TotalDurationSeconds float64 `json:"total_duration_seconds"`
}

// MetricStats 某指标的统计摘要。
type MetricStats struct {
	// Mean 均值
	Mean float64 `json:"mean"`
	// Stddev 标准差
	Stddev float64 `json:"stddev"`
	// Min 最小值
	Min float64 `json:"min"`
	// Max 最大值
	Max float64 `json:"max"`
}

// BenchmarkRun benchmark.json 中的一条 run 记录。
type BenchmarkRun struct {
	// EvalID 用例标识
	EvalID int `json:"eval_id"`
	// EvalName 用例名称
	EvalName string `json:"eval_name"`
	// Configuration 配置标识（"with_skill" | "baseline"）
	Configuration string `json:"configuration"`
	// RunNumber 运行序号
	RunNumber int `json:"run_number"`
	// PassRate 通过率
	PassRate float64 `json:"pass_rate"`
	// TimeSeconds 耗时秒
	TimeSeconds float64 `json:"time_seconds"`
	// Tokens token 数量
	Tokens int `json:"tokens"`
	// Expectations 期望详情列表
	Expectations []map[string]any `json:"expectations"`
}

// Benchmark 完整的 benchmark 结果。
type Benchmark struct {
	// SkillName 技能名称
	SkillName string `json:"skill_name"`
	// Runs 运行记录列表
	Runs []BenchmarkRun `json:"runs"`
	// RunSummary 运行摘要
	RunSummary map[string]any `json:"run_summary"`
	// Notes 备注列表
	Notes []string `json:"notes"`
	// Timestamp 时间戳
	Timestamp string `json:"timestamp"`
}

// TriggerEvalQuery 描述优化阶段的单个触发测试查询。
type TriggerEvalQuery struct {
	// Query 查询文本
	Query string `json:"query"`
	// ShouldTrigger 是否应触发
	ShouldTrigger bool `json:"should_trigger"`
}

// DescOptimizeIteration 描述优化的单轮迭代结果。
type DescOptimizeIteration struct {
	// Iteration 迭代号
	Iteration int `json:"iteration"`
	// Description 描述文本
	Description string `json:"description"`
	// TrainPassed 训练通过数
	TrainPassed int `json:"train_passed"`
	// TrainTotal 训练总数
	TrainTotal int `json:"train_total"`
	// TestPassed 测试通过数（可空）
	TestPassed *int `json:"test_passed"`
	// TestTotal 测试总数（可空）
	TestTotal *int `json:"test_total"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// SkillDevStage SkillDev Pipeline 的所有阶段。
//
// 流程：INIT → PLAN → PLAN_CONFIRM(挂起) → GENERATE → VALIDATE
//
//	→ TEST_DESIGN → TEST_RUN → EVALUATE → REVIEW(挂起)
//	→ IMPROVE → (回到 TEST_RUN 迭代)
//	→ PACKAGE → DESC_OPTIMIZE_CONFIRM(挂起) → DESC_OPTIMIZE → COMPLETED
type SkillDevStage string

const (
	// SkillDevStageInit 初始化
	SkillDevStageInit SkillDevStage = "init"
	// SkillDevStagePlan 规划
	SkillDevStagePlan SkillDevStage = "plan"
	// SkillDevStagePlanConfirm 挂起点：等待用户确认 plan
	SkillDevStagePlanConfirm SkillDevStage = "plan_confirm"
	// SkillDevStageGenerate 生成
	SkillDevStageGenerate SkillDevStage = "generate"
	// SkillDevStageValidate 校验生成的 SKILL.md 格式（YAML frontmatter + 命名规范）
	SkillDevStageValidate SkillDevStage = "validate"
	// SkillDevStageTestDesign 测试设计
	SkillDevStageTestDesign SkillDevStage = "test_design"
	// SkillDevStageTestRun 测试运行
	SkillDevStageTestRun SkillDevStage = "test_run"
	// SkillDevStageEvaluate grader 评分 + aggregate_benchmark 聚合 + analyst 分析
	SkillDevStageEvaluate SkillDevStage = "evaluate"
	// SkillDevStageReview 挂起点：等待用户审阅评测结果
	SkillDevStageReview SkillDevStage = "review"
	// SkillDevStageImprove 改进
	SkillDevStageImprove SkillDevStage = "improve"
	// SkillDevStagePackage 打包
	SkillDevStagePackage SkillDevStage = "package"
	// SkillDevStageDescOptimizeConfirm 挂起点：询问用户是否需要描述优化
	SkillDevStageDescOptimizeConfirm SkillDevStage = "desc_optimize_confirm"
	// SkillDevStageDescOptimize 触发描述优化循环
	SkillDevStageDescOptimize SkillDevStage = "desc_optimize"
	// SkillDevStageCompleted 已完成
	SkillDevStageCompleted SkillDevStage = "completed"
	// SkillDevStageError 错误
	SkillDevStageError SkillDevStage = "error"
)

// SkillDevTaskMode 任务入口模式（由请求参数自动判断）。
type SkillDevTaskMode string

const (
	// SkillDevTaskModeCreate 纯 query 创建
	SkillDevTaskModeCreate SkillDevTaskMode = "create"
	// SkillDevTaskModeCreateWithResources 携带资源包创建
	SkillDevTaskModeCreateWithResources SkillDevTaskMode = "create_with_resources"
	// SkillDevTaskModeModify 修改/升级已有 skill
	SkillDevTaskModeModify SkillDevTaskMode = "modify"
)

// SkillDevEventType Pipeline 向前端推送的事件类型。
//
// 设计原则：后端推的每个事件，前端都应能直接映射到一个 UI 动作，
// 而非让前端猜测语义。
type SkillDevEventType string

const (
	// SkillDevEventTypeStageChanged 阶段切换（内部标识）
	SkillDevEventTypeStageChanged SkillDevEventType = "skilldev.stage_changed"
	// SkillDevEventTypeProgress 阶段内进度文本（对话流展示）
	SkillDevEventTypeProgress SkillDevEventType = "skilldev.progress"
	// SkillDevEventTypeError 不可恢复错误
	SkillDevEventTypeError SkillDevEventType = "skilldev.error"
	// SkillDevEventTypeAgentThinking Agent 推理流（delta + model_name + elapsed_ms + status）
	SkillDevEventTypeAgentThinking SkillDevEventType = "skilldev.agent_thinking"
	// SkillDevEventTypeTestProgress 测试执行进度
	SkillDevEventTypeTestProgress SkillDevEventType = "skilldev.test_progress"
	// SkillDevEventTypeConfirmRequest 挂起点：驱动前端弹出确认框
	SkillDevEventTypeConfirmRequest SkillDevEventType = "skilldev.confirm_request"
	// SkillDevEventTypeTodosUpdate 驱动右侧 Todo 列表
	SkillDevEventTypeTodosUpdate SkillDevEventType = "skilldev.todos_update"
	// SkillDevEventTypeArtifactReady 驱动右侧产物/附件列表
	SkillDevEventTypeArtifactReady SkillDevEventType = "skilldev.artifact_ready"
	// SkillDevEventTypeEvalReady 评测结果（benchmark JSON）
	SkillDevEventTypeEvalReady SkillDevEventType = "skilldev.eval_ready"
	// SkillDevEventTypeValidateResult SKILL.md 校验结果
	SkillDevEventTypeValidateResult SkillDevEventType = "skilldev.validate_result"
	// SkillDevEventTypeDescOptReady 描述优化 before/after
	SkillDevEventTypeDescOptReady SkillDevEventType = "skilldev.desc_opt_ready"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SkillNameMaxLen SKILL 名称最大长度
	SkillNameMaxLen = 64
	// SkillDescMaxLen SKILL 描述最大长度
	SkillDescMaxLen = 1024
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// AllowedFrontmatterKeys SKILL.md 允许的 frontmatter 键集合
	AllowedFrontmatterKeys = map[string]bool{
		"name":          true,
		"description":   true,
		"license":       true,
		"allowed-tools": true,
		"metadata":      true,
		"compatibility": true,
	}

	// SuspensionPoints 各挂起点的配置，key 为阶段枚举
	SuspensionPoints = map[SkillDevStage]SuspensionConfig{
		SkillDevStagePlanConfirm: {
			ConfirmType: "plan_confirm",
			Title:       "请审阅开发计划",
			Message:     "以下是生成的开发计划，请确认或修改",
			Actions: []SuspensionAction{
				{ID: "confirm", Label: "确认", Style: "primary"},
				{ID: "modify", Label: "修改", Style: "secondary"},
			},
			ExtractData:   planExtractData,
			OnResume:      planConfirmOnResume,
			NextStage:     SkillDevStageGenerate,
			NextStageFunc: nil,
		},
		SkillDevStageReview: {
			ConfirmType: "review",
			Title:       "评测结果审阅",
			Message:     "请审阅评测结果并决定下一步",
			Actions: []SuspensionAction{
				{ID: "accept", Label: "通过，进入打包", Style: "primary"},
				{ID: "improve", Label: "继续改进", Style: "secondary"},
			},
			ExtractData:   reviewExtractData,
			OnResume:      reviewOnResume,
			NextStageFunc: reviewNextStage,
		},
		SkillDevStageDescOptimizeConfirm: {
			ConfirmType: "desc_optimize_confirm",
			Title:       "描述优化",
			Message:     "Skill 已打包完成。是否需要优化触发描述以提高触发准确率？",
			Actions: []SuspensionAction{
				{ID: "optimize", Label: "优化", Style: "primary"},
				{ID: "skip", Label: "跳过", Style: "secondary"},
			},
			ExtractData:   descOptExtractData,
			OnResume:      descOptimizeConfirmOnResume,
			NextStageFunc: descOptimizeConfirmNextStage,
		},
	}

	// stageGroups 后端定义的阶段分组。前端只负责渲染，不决定内容。
	stageGroups = []StageGroup{
		{
			ID:    "plan",
			Label: "需求分析与规划",
			Stages: map[SkillDevStage]bool{
				SkillDevStageInit:        true,
				SkillDevStagePlan:        true,
				SkillDevStagePlanConfirm: true,
			},
			Modes: nil,
		},
		{
			ID:    "generate",
			Label: "技能生成与校验",
			Stages: map[SkillDevStage]bool{
				SkillDevStageGenerate: true,
				SkillDevStageValidate: true,
			},
			Modes: nil,
		},
		{
			ID:    "test",
			Label: "测试与评测",
			Stages: map[SkillDevStage]bool{
				SkillDevStageTestDesign: true,
				SkillDevStageTestRun:    true,
				SkillDevStageEvaluate:   true,
				SkillDevStageReview:     true,
			},
			Modes: nil,
		},
		{
			ID:    "improve",
			Label: "优化改进",
			Stages: map[SkillDevStage]bool{
				SkillDevStageImprove: true,
			},
			Modes: nil,
		},
		{
			ID:    "package",
			Label: "打包",
			Stages: map[SkillDevStage]bool{
				SkillDevStagePackage: true,
			},
			Modes: nil,
		},
		{
			ID:    "desc_optimize",
			Label: "描述优化",
			Stages: map[SkillDevStage]bool{
				SkillDevStageDescOptimizeConfirm: true,
				SkillDevStageDescOptimize:        true,
			},
			Modes: nil,
		},
	}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillDevState 创建新的 SkillDevState 实例，初始化默认值。
func NewSkillDevState(taskID string) *SkillDevState {
	now := nowISO()
	return &SkillDevState{
		TaskID:             taskID,
		Stage:              SkillDevStageInit,
		Mode:               SkillDevTaskModeCreate,
		Iteration:          0,
		Input:              make(map[string]any),
		ReferenceTexts:     []string{},
		ExistingSkillMD:    nil,
		Plan:               nil,
		PlanConfirmedAt:    nil,
		Evals:              nil,
		EvalResults:        nil,
		FeedbackHistory:    []map[string]any{},
		DescOptimizeResult: nil,
		ZipPath:            nil,
		ZipSize:            0,
		CreatedAt:          now,
		UpdatedAt:          now,
		Error:              nil,
	}
}

// Touch 更新 updated_at 时间戳。
func (s *SkillDevState) Touch() {
	s.UpdatedAt = nowISO()
}

// ToCheckpointDict 序列化为可持久化的字典（用于 StateStore）。
func (s *SkillDevState) ToCheckpointDict() map[string]any {
	return map[string]any{
		"task_id":              s.TaskID,
		"stage":                string(s.Stage),
		"mode":                 string(s.Mode),
		"iteration":            s.Iteration,
		"input":                s.Input,
		"reference_texts":      s.ReferenceTexts,
		"existing_skill_md":    s.ExistingSkillMD,
		"plan":                 s.Plan,
		"plan_confirmed_at":    s.PlanConfirmedAt,
		"evals":                s.Evals,
		"eval_results":         s.EvalResults,
		"feedback_history":     s.FeedbackHistory,
		"desc_optimize_result": s.DescOptimizeResult,
		"zip_path":             s.ZipPath,
		"zip_size":             s.ZipSize,
		"created_at":           s.CreatedAt,
		"updated_at":           s.UpdatedAt,
		"error":                s.Error,
	}
}

// FromCheckpointDict 从持久化字典恢复状态。
func FromCheckpointDict(data map[string]any) *SkillDevState {
	state := &SkillDevState{
		TaskID:    getStr(data, "task_id"),
		Stage:     SkillDevStage(getStr(data, "stage")),
		Mode:      SkillDevTaskMode(getStrDefault(data, "mode", "create")),
		Iteration: getIntDefault(data, "iteration", 0),
	}

	state.Input = getMapDefault(data, "input")
	state.ReferenceTexts = getStrSliceDefault(data, "reference_texts")
	state.ExistingSkillMD = getNilStr(data, "existing_skill_md")
	state.Plan = getNilMap(data, "plan")
	state.PlanConfirmedAt = getNilStr(data, "plan_confirmed_at")
	state.Evals = getNilMap(data, "evals")
	state.EvalResults = getNilMap(data, "eval_results")
	state.FeedbackHistory = getMapSliceDefault(data, "feedback_history")
	state.DescOptimizeResult = getNilMap(data, "desc_optimize_result")
	state.ZipPath = getNilStr(data, "zip_path")
	state.ZipSize = getIntDefault(data, "zip_size", 0)
	state.CreatedAt = getStrDefault(data, "created_at", nowISO())
	state.UpdatedAt = getStrDefault(data, "updated_at", nowISO())
	state.Error = getNilStr(data, "error")

	return state
}

// ToStatusDict 序列化为前端可展示的状态摘要。
func (s *SkillDevState) ToStatusDict() map[string]any {
	return map[string]any{
		"task_id":      s.TaskID,
		"stage":        string(s.Stage),
		"mode":         string(s.Mode),
		"iteration":    s.Iteration,
		"plan":         s.Plan,
		"eval_results": s.EvalResults,
		"created_at":   s.CreatedAt,
		"updated_at":   s.UpdatedAt,
		"error":        s.Error,
	}
}

// ToDict 序列化 EvalCase 为字典。
func (e *EvalCase) ToDict() map[string]any {
	return map[string]any{
		"id":              e.ID,
		"prompt":          e.Prompt,
		"expected_output": e.ExpectedOutput,
		"files":           e.Files,
		"expectations":    e.Expectations,
	}
}

// ToDict 序列化 EvalSet 为字典。
func (es *EvalSet) ToDict() map[string]any {
	evals := make([]map[string]any, len(es.Evals))
	for i, e := range es.Evals {
		evals[i] = e.ToDict()
	}
	return map[string]any{
		"skill_name": es.SkillName,
		"evals":      evals,
	}
}

// NewEvalSetFromDict 从字典恢复 EvalSet。
func NewEvalSetFromDict(data map[string]any) *EvalSet {
	skillName, _ := data["skill_name"].(string)
	evalsData, _ := data["evals"].([]any)
	evals := make([]EvalCase, 0, len(evalsData))
	for _, item := range evalsData {
		if m, ok := item.(map[string]any); ok {
			ec := EvalCase{}
			if v, ok := m["id"]; ok {
				ec.ID = toInt(v)
			}
			if v, ok := m["prompt"].(string); ok {
				ec.Prompt = v
			}
			if v, ok := m["expected_output"].(string); ok {
				ec.ExpectedOutput = v
			}
			if v, ok := m["files"].([]any); ok {
				ec.Files = toStringSlice(v)
			}
			if v, ok := m["expectations"].([]any); ok {
				ec.Expectations = toStringSlice(v)
			}
			evals = append(evals, ec)
		}
	}
	return &EvalSet{SkillName: skillName, Evals: evals}
}

// ToDict 序列化 GradingResult 为字典。
func (g *GradingResult) ToDict() map[string]any {
	expectations := make([]map[string]any, len(g.Expectations))
	for i, e := range g.Expectations {
		expectations[i] = map[string]any{
			"text":     e.Text,
			"passed":   e.Passed,
			"evidence": e.Evidence,
		}
	}
	return map[string]any{
		"expectations": expectations,
		"summary": map[string]any{
			"passed":    g.PassedCount,
			"failed":    g.FailedCount,
			"total":     g.PassedCount + g.FailedCount,
			"pass_rate": g.PassRate,
		},
	}
}

// ToDict 序列化 RunTiming 为字典。
func (rt *RunTiming) ToDict() map[string]any {
	return map[string]any{
		"total_tokens":           rt.TotalTokens,
		"duration_ms":            rt.DurationMs,
		"total_duration_seconds": rt.TotalDurationSeconds,
	}
}

// ToDict 序列化 MetricStats 为字典。
func (ms *MetricStats) ToDict() map[string]any {
	return map[string]any{
		"mean":   ms.Mean,
		"stddev": ms.Stddev,
		"min":    ms.Min,
		"max":    ms.Max,
	}
}

// ToDict 序列化 BenchmarkRun 为字典。
func (br *BenchmarkRun) ToDict() map[string]any {
	return map[string]any{
		"eval_id":       br.EvalID,
		"eval_name":     br.EvalName,
		"configuration": br.Configuration,
		"run_number":    br.RunNumber,
		"result": map[string]any{
			"pass_rate":    br.PassRate,
			"time_seconds": br.TimeSeconds,
			"tokens":       br.Tokens,
		},
		"expectations": br.Expectations,
	}
}

// ToDict 序列化 Benchmark 为字典。
func (b *Benchmark) ToDict() map[string]any {
	runs := make([]map[string]any, len(b.Runs))
	for i, r := range b.Runs {
		runs[i] = r.ToDict()
	}
	return map[string]any{
		"metadata": map[string]any{
			"skill_name": b.SkillName,
			"timestamp":  b.Timestamp,
		},
		"runs":        runs,
		"run_summary": b.RunSummary,
		"notes":       b.Notes,
	}
}

// ToDict 序列化 TriggerEvalQuery 为字典。
func (t *TriggerEvalQuery) ToDict() map[string]any {
	return map[string]any{
		"query":          t.Query,
		"should_trigger": t.ShouldTrigger,
	}
}

// ToDict 序列化 DescOptimizeIteration 为字典。
func (d *DescOptimizeIteration) ToDict() map[string]any {
	result := map[string]any{
		"iteration":    d.Iteration,
		"description":  d.Description,
		"train_passed": d.TrainPassed,
		"train_total":  d.TrainTotal,
	}
	if d.TestPassed != nil {
		result["test_passed"] = *d.TestPassed
		result["test_total"] = *d.TestTotal
	}
	return result
}

// ComputeTodos 根据当前阶段和任务模式，计算面向用户的 Todo 列表。
//
// 后端是步骤定义的唯一权威来源。前端只做渲染。
func ComputeTodos(currentStage SkillDevStage, mode *SkillDevTaskMode) []map[string]string {
	groups := stageGroups
	if mode != nil {
		filtered := make([]StageGroup, 0, len(groups))
		for _, g := range groups {
			if g.Modes == nil || g.Modes[*mode] {
				filtered = append(filtered, g)
			}
		}
		groups = filtered
	}

	if currentStage == SkillDevStageCompleted {
		result := make([]map[string]string, len(groups))
		for i, g := range groups {
			result[i] = map[string]string{"id": g.ID, "label": g.Label, "status": "completed"}
		}
		return result
	}
	if currentStage == SkillDevStageError {
		result := make([]map[string]string, len(groups))
		for i, g := range groups {
			result[i] = map[string]string{"id": g.ID, "label": g.Label, "status": "cancelled"}
		}
		return result
	}

	foundCurrent := false
	result := make([]map[string]string, 0, len(groups))
	for _, g := range groups {
		var status string
		if g.Stages[currentStage] {
			status = "in_progress"
			foundCurrent = true
		} else if foundCurrent {
			status = "pending"
		} else {
			status = "completed"
		}
		result = append(result, map[string]string{"id": g.ID, "label": g.Label, "status": status})
	}
	return result
}

// GenerateTaskID 生成唯一 task_id，格式：sd_{timestamp}_{random}。
func GenerateTaskID() string {
	ts := time.Now().Unix()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	randStr := hex.EncodeToString(b)
	return fmt.Sprintf("sd_%d_%s", ts, randStr)
}

// DetermineTaskMode 根据请求参数自动判断任务模式。
func DetermineTaskMode(params map[string]any) SkillDevTaskMode {
	if _, ok := params["existing_skill"]; ok {
		return SkillDevTaskModeModify
	}
	if _, ok := params["resources"]; ok {
		return SkillDevTaskModeCreateWithResources
	}
	return SkillDevTaskModeCreate
}

// CalcStats 计算一组 float64 的统计摘要（均值、标准差、最小值、最大值）。
func CalcStats(values []float64) MetricStats {
	n := len(values)
	if n == 0 {
		return MetricStats{}
	}

	sorted := make([]float64, n)
	copy(sorted, values)
	sort.Float64s(sorted)

	sum := 0.0
	for _, v := range sorted {
		sum += v
	}
	mean := sum / float64(n)

	variance := 0.0
	for _, v := range sorted {
		d := v - mean
		variance += d * d
	}
	variance /= float64(n)
	stddev := math.Sqrt(variance)

	return MetricStats{
		Mean:   mean,
		Stddev: stddev,
		Min:    sorted[0],
		Max:    sorted[n-1],
	}
}

// GetNextStage 获取 SuspensionConfig 的下一阶段（支持固定值和动态函数）。
func (sc *SuspensionConfig) GetNextStage(data map[string]any) SkillDevStage {
	if sc.NextStageFunc != nil {
		return sc.NextStageFunc(data)
	}
	return sc.NextStage
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// nowISO 返回当前 UTC 时间的 ISO 8601 字符串。
func nowISO() string {
	return time.Now().UTC().Format("2006-01-02T15:04:05Z")
}

// planExtractData PLAN_CONFIRM 挂起点的 extract_data 实现。
func planExtractData(state *SkillDevState) map[string]any {
	return map[string]any{"plan": state.Plan}
}

// planConfirmOnResume PLAN_CONFIRM 挂起点的 on_resume 实现。
func planConfirmOnResume(state *SkillDevState, data map[string]any) {
	if _, ok := data["plan"]; ok {
		state.Plan = getNilMap(data, "plan")
	}
	now := nowISO()
	state.PlanConfirmedAt = &now
}

// reviewExtractData REVIEW 挂起点的 extract_data 实现。
func reviewExtractData(state *SkillDevState) map[string]any {
	evalResults := state.EvalResults
	if evalResults == nil {
		evalResults = make(map[string]any)
	}
	return map[string]any{
		"benchmark": evalResults["benchmark"],
		"report":    evalResults["report"],
		"iteration": state.Iteration,
	}
}

// reviewOnResume REVIEW 挂起点的 on_resume 实现。
func reviewOnResume(state *SkillDevState, data map[string]any) {
	if feedback, ok := data["feedback"]; ok {
		state.FeedbackHistory = append(state.FeedbackHistory, map[string]any{
			"iteration": state.Iteration,
			"feedback":  feedback,
		})
	}
}

// reviewNextStage REVIEW 挂起点的 next_stage 动态函数。
func reviewNextStage(data map[string]any) SkillDevStage {
	action, _ := data["action"].(string)
	if action == "" {
		action = "improve"
	}
	if action == "improve" {
		return SkillDevStageImprove
	}
	return SkillDevStagePackage
}

// descOptExtractData DESC_OPTIMIZE_CONFIRM 挂起点的 extract_data 实现。
func descOptExtractData(state *SkillDevState) map[string]any {
	plan := state.Plan
	if plan == nil {
		plan = make(map[string]any)
	}
	desc, _ := plan["description"].(string)
	return map[string]any{"current_description": desc}
}

// descOptimizeConfirmOnResume DESC_OPTIMIZE_CONFIRM 挂起点的 on_resume 实现。
func descOptimizeConfirmOnResume(_ *SkillDevState, _ map[string]any) {
	// 无操作，对齐 Python pass
}

// descOptimizeConfirmNextStage DESC_OPTIMIZE_CONFIRM 挂起点的 next_stage 动态函数。
func descOptimizeConfirmNextStage(data map[string]any) SkillDevStage {
	action, _ := data["action"].(string)
	if action == "" {
		action = "skip"
	}
	if action == "optimize" {
		return SkillDevStageDescOptimize
	}
	return SkillDevStageCompleted
}

// getStr 从 map 中获取字符串值。
func getStr(m map[string]any, key string) string {
	v, _ := m[key].(string)
	return v
}

// getStrDefault 从 map 中获取字符串值，不存在则返回默认值。
func getStrDefault(m map[string]any, key, defaultVal string) string {
	v, ok := m[key].(string)
	if !ok {
		return defaultVal
	}
	return v
}

// getIntDefault 从 map 中获取整数值，不存在则返回默认值。
func getIntDefault(m map[string]any, key string, defaultVal int) int {
	v, ok := m[key]
	if !ok {
		return defaultVal
	}
	return toInt(v)
}

// toInt 将 any 转为 int（支持 float64/json.Number 等常见反序列化类型）。
func toInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0
		}
		return int(i)
	default:
		return 0
	}
}

// getMapDefault 从 map 中获取 map[string]any，不存在则返回空 map。
func getMapDefault(m map[string]any, key string) map[string]any {
	v, ok := m[key].(map[string]any)
	if !ok {
		return make(map[string]any)
	}
	return v
}

// getNilMap 从 map 中获取 map[string]any，不存在则返回 nil。
func getNilMap(m map[string]any, key string) map[string]any {
	v, ok := m[key].(map[string]any)
	if !ok {
		return nil
	}
	return v
}

// getStrSliceDefault 从 map 中获取 []string，不存在则返回空切片。
func getStrSliceDefault(m map[string]any, key string) []string {
	v, ok := m[key].([]any)
	if !ok {
		return []string{}
	}
	return toStringSlice(v)
}

// toStringSlice 将 []any 转为 []string。
func toStringSlice(v []any) []string {
	result := make([]string, 0, len(v))
	for _, item := range v {
		if s, ok := item.(string); ok {
			result = append(result, s)
		}
	}
	return result
}

// getMapSliceDefault 从 map 中获取 []map[string]any，不存在则返回空切片。
func getMapSliceDefault(m map[string]any, key string) []map[string]any {
	v, ok := m[key].([]any)
	if !ok {
		return []map[string]any{}
	}
	result := make([]map[string]any, 0, len(v))
	for _, item := range v {
		if mm, ok := item.(map[string]any); ok {
			result = append(result, mm)
		}
	}
	return result
}

// getNilStr 从 map 中获取 *string（可空字符串），不存在或为 nil 则返回 nil。
func getNilStr(m map[string]any, key string) *string {
	v, ok := m[key]
	if !ok || v == nil {
		return nil
	}
	s, ok := v.(string)
	if !ok {
		return nil
	}
	return &s
}
