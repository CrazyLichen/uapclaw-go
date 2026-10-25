// 4 个枚举类型与 26 个常量定义（包文档见 doc.go）。
// 对齐 Python: openjiuwen/dev_tools/agent_builder/utils/enums.py。
package utils

// ──────────────────────────── 枚举 ────────────────────────────

// AgentType 构建的 Agent 类型，对齐 Python str-Enum AgentType。
//
// 消费方：14.5 executor（ParseAgentType 入参校验关口→140032）、
// 14.6 factory（map[AgentType] 注册键）。
type AgentType string

// BuildState 构建器状态机三态，对齐 Python str-Enum BuildState。
//
// 消费方：14.6 BaseAgentBuilder._state（INITIAL→PROCESSING→COMPLETED）。
type BuildState string

// ProgressStage 构建进度阶段（15 值，分 Common/LLM Agent/Workflow Agent 三组），
// 对齐 Python str-Enum ProgressStage。
//
// 消费方：14.3 progress（BuildProgress.current_stage / ProgressStep.stage）。
type ProgressStage string

// ProgressStatus 构建进度状态，对齐 Python str-Enum ProgressStatus。
//
// 消费方：14.3 progress（BuildProgress.current_status / ProgressStep.status）。
type ProgressStatus string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// AgentTypeLLMAgent LLM Agent 类型
	AgentTypeLLMAgent AgentType = "llm_agent"
	// AgentTypeWorkflow Workflow Agent 类型
	AgentTypeWorkflow AgentType = "workflow"
)

const (
	// BuildStateInitial 初始态
	BuildStateInitial BuildState = "initial"
	// BuildStateProcessing 处理中
	BuildStateProcessing BuildState = "processing"
	// BuildStateCompleted 已完成
	BuildStateCompleted BuildState = "completed"
)

const (
	// ── Common 通用阶段 ──

	// ProgressStageInitializing 初始化
	ProgressStageInitializing ProgressStage = "initializing"
	// ProgressStageClarifying 需求澄清
	ProgressStageClarifying ProgressStage = "clarifying"
	// ProgressStageResourceRetrieving 资源检索
	ProgressStageResourceRetrieving ProgressStage = "resource_retrieving"
	// ProgressStageCompleted 已完成
	ProgressStageCompleted ProgressStage = "completed"
	// ProgressStageError 错误
	ProgressStageError ProgressStage = "error"
	// ProgressStageOptimizing 优化中
	ProgressStageOptimizing ProgressStage = "optimizing"

	// ── LLM Agent 专属阶段 ──

	// ProgressStageGeneratingConfig 生成配置
	ProgressStageGeneratingConfig ProgressStage = "generating_config"
	// ProgressStageTransformingDSL 转换 DSL
	ProgressStageTransformingDSL ProgressStage = "transforming_dsl"

	// ── Workflow Agent 专属阶段 ──

	// ProgressStageDetectingIntention 意图检测
	ProgressStageDetectingIntention ProgressStage = "detecting_intention"
	// ProgressStageGeneratingWorkflowDesign 生成工作流设计
	ProgressStageGeneratingWorkflowDesign ProgressStage = "generating_workflow_design"
	// ProgressStageGeneratingDL 生成 DL
	ProgressStageGeneratingDL ProgressStage = "generating_dl"
	// ProgressStageValidatingDL 校验 DL
	ProgressStageValidatingDL ProgressStage = "validating_dl"
	// ProgressStageRefiningDL 修正 DL
	ProgressStageRefiningDL ProgressStage = "refining_dl"
	// ProgressStageTransformingMermaid 转换 Mermaid 图
	ProgressStageTransformingMermaid ProgressStage = "transforming_mermaid"
	// ProgressStageTransformingWorkflowDSL 转换工作流 DSL
	ProgressStageTransformingWorkflowDSL ProgressStage = "transforming_workflow_dsl"
)

const (
	// ProgressStatusPending 待执行
	ProgressStatusPending ProgressStatus = "pending"
	// ProgressStatusRunning 执行中
	ProgressStatusRunning ProgressStatus = "running"
	// ProgressStatusSuccess 已成功
	ProgressStatusSuccess ProgressStatus = "success"
	// ProgressStatusFailed 已失败
	ProgressStatusFailed ProgressStatus = "failed"
	// ProgressStatusWarning 警告
	ProgressStatusWarning ProgressStatus = "warning"
)
