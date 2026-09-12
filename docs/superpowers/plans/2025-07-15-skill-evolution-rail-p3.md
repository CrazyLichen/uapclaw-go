# 9.24 P3 SkillEvolutionRail 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 完整实现 9.24 P3 — SkillEvolutionRail 结构体 + EvolutionExtension 实现 + Slash 命令 API + DeepAdapter 占位回填 + Sharing 集成，使 `/evolve*` 系列命令和被动演化流程端到端可用。

**Architecture:** SkillEvolutionRail 嵌入 `*EvolutionRail` 基类获得自动轨迹收集和异步触发能力，实现 `EvolutionExtension` 9 个方法完成子类多态分派。持有 9 个已实现的底层组件（EvolutionStore / SkillExperienceOptimizer / ExperienceScorer / ExperienceManager / EvolutionApprovalRuntime / SingleDimUpdater / OnlineEvolutionOrchestrator / ExperienceTracker / SignalDetector）和可选的 2 个 sharing 组件（ShareStager / ExperienceSharer）。DeepAdapter 层通过具体类型 `*evolution.SkillEvolutionRail` 持有实例，暴露 slash 命令和 approval 路由。

**Tech Stack:** Go 1.22+, functional options 构造模式, 组合替代 Python mixin 继承, goroutine 替代 Python asyncio

---

## 文件清单

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 新建 | `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` | SkillEvolutionRail 结构体 + EvolutionExtension + 演化 API |
| 新建 | `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go` | 单元测试 |
| 修改 | `internal/agentcore/harness/rails/evolution/doc.go` | 添加 skill_evolution_rail.go 条目 |
| 修改 | `internal/swarm/server/adapter/deep_adapter.go` | skillEvolutionRail 字段类型 + HandleUserAnswer 路由 + slash 集成 |
| 修改 | `internal/swarm/server/adapter/deep_adapter_rails.go` | buildSkillEvolutionRail + updateRailsForMode |
| 修改 | `internal/swarm/server/adapter/deep_adapter_slash.go` | 6 个处理器实现 |
| 修改 | `internal/swarm/server/adapter/deep_adapter_evolution.go` | watchEvolutionAndPush + handleEvolutionApproval |
| 修改 | `IMPLEMENTATION_PLAN.md` | 9.24 P3/P5 状态更新 |

---

### Task 1: SkillEvolutionRail 结构体 + 构造函数 + Functional Options

**Files:**
- Create: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

- [ ] **Step 1: 创建 skill_evolution_rail.go，定义结构体和构造函数**

创建文件，写入以下内容（按 Go 编码规范排列顺序：结构体→枚举→常量→全局变量→导出函数→非导出函数）：

```go
package evolution

import (
	"context"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/evolving/checkpointing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/experience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/llm_resilience"
	"github.com/uapclaw/uapclaw-go/internal/evolving/optimizer/skill_call"
	"github.com/uapclaw/uapclaw-go/internal/evolving/sharing"
	"github.com/uapclaw/uapclaw-go/internal/evolving/signal"
	"github.com/uapclaw/uapclaw-go/internal/evolving/trajectory"
	"github.com/uapclaw/uapclaw-go/internal/evolving/updater/single_dim"
	"github.com/uapclaw/uapclaw-go/internal/evolving/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SkillEvolutionRail 单 Agent 技能演进护栏，在 after_invoke 后自动触发演进。
//
// 嵌入 EvolutionRail 基类获得自动轨迹收集和异步触发能力，
// 通过实现 EvolutionExtension 接口的 9 个方法完成子类多态分派。
//
// 核心流程：信号检测 → 技能归属 → 经验生成 → 评分 → 暂存审批 → 应用更新
// Slash 命令入口：/evolve /evolve_list /evolve_simplify /evolve_rebuild /evolve_rollback
//
// 对齐 Python: openjiuwen/harness/rails/evolution/skill_evolution_rail.py SkillEvolutionRail
type SkillEvolutionRail struct {
	*EvolutionRail

	// ─── 核心组件 ───

	// evolutionStore 技能演进数据存储
	evolutionStore *checkpointing.EvolutionStore
	// evolver 技能经验优化器
	evolver *skill_call.SkillExperienceOptimizer
	// scorer 经验评分器
	scorer *experience.ExperienceScorer
	// manager 经验生命周期管理器
	manager *experience.ExperienceManager
	// approvalRuntime 审批运行时
	approvalRuntime *EvolutionApprovalRuntime
	// onlineUpdater 单维更新器
	onlineUpdater *single_dim.SingleDimUpdater
	// orchestrator 在线演进编排器
	orchestrator *experience.OnlineEvolutionOrchestrator
	// experienceTracker 经验展示追踪器
	experienceTracker *experience.ExperienceTracker

	// ─── 配置 ───

	// autoScan 是否自动检测演进信号
	autoScan bool
	// autoSave 是否自动保存（跳过审批）
	autoSave bool
	// language 语言（"cn" 或 "en"）
	language string
	// evalInterval 评估间隔
	evalInterval int
	// evolutionTimeoutSec 后台演化超时秒数
	evolutionTimeoutSec float64

	// ─── 信号去重 ───

	// processedSignalKeys 已处理的信号指纹集合
	processedSignalKeys map[[4]string]bool

	// ─── LLM 策略 ───

	// generateRecordsLLMPolicy 记录生成 LLM 调用策略
	generateRecordsLLMPolicy llm_resilience.LLMInvokePolicy
	// evaluateLLMPolicy 评估 LLM 调用策略
	evaluateLLMPolicy llm_resilience.LLMInvokePolicy
	// simplifyLLMPolicy 精简 LLM 调用策略
	simplifyLLMPolicy llm_resilience.LLMInvokePolicy

	// ─── Sharing（9.80a 组合，可选）───

	// shareStager 经验共享暂存器（可选）
	shareStager *sharing.ShareStager
	// experienceSharer 经验共享器（可选）
	experienceSharer *sharing.ExperienceSharer
	// sharingEnabled 是否启用共享
	sharingEnabled bool

	// ─── 共享状态 ───

	// skillOps 技能经验操作器映射
	skillOps map[string]*skill_call.SkillExperienceOperator
	// pendingGovernance 暂存治理操作映射
	pendingGovernance map[string]*experience.PendingGovernance
	// skillsDir 技能目录
	skillsDir string
}

// SkillEvolutionRailOption SkillEvolutionRail 构造选项函数。
type SkillEvolutionRailOption func(*SkillEvolutionRail)

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// maxProcessedSignalKeys 已处理信号指纹最大数量
	maxProcessedSignalKeys = 500
	// defaultEvolutionTimeoutSecs 默认后台演化超时秒数
	defaultEvolutionTimeoutSecs = 600.0
	// defaultEvalInterval 默认评估间隔
	defaultEvalInterval = 5
	// nonRegularSkillKinds 非常规技能类型集合
	nonRegularSkillKinds = "team-skill,swarm-skill"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// experienceRecordHeadingRE 匹配经验记录标题行中的 record ID。
var experienceRecordHeadingRE = regexp.MustCompile(`#+\s*\[([A-Za-z0-9_-]+)\]`)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSkillEvolutionRail 创建 SkillEvolutionRail 实例。
//
// 必选参数：skillsDir（技能目录）、llmModel（LLM 客户端）、model（模型名称）、language（语言）。
// 可选参数通过 Functional Options 覆盖默认配置。
//
// 对齐 Python: SkillEvolutionRail.__init__(
//
//	skills_dir, llm, model, auto_scan, auto_save, language,
//	trajectory_store, eval_interval, evolution_total_timeout_secs,
//	generate_records_llm_policy, evaluate_llm_policy, simplify_llm_policy,
//	sharing_config, disabled_skills
//
// )
func NewSkillEvolutionRail(
	skillsDir string,
	llmModel *llm.Model,
	model string,
	language string,
	opts ...SkillEvolutionRailOption,
) *SkillEvolutionRail {
	r := &SkillEvolutionRail{
		skillsDir:               skillsDir,
		language:                language,
		autoScan:                true,
		autoSave:                true,
		evalInterval:            defaultEvalInterval,
		evolutionTimeoutSec:     defaultEvolutionTimeoutSecs,
		processedSignalKeys:     make(map[[4]string]bool),
		skillOps:                make(map[string]*skill_call.SkillExperienceOperator),
		pendingGovernance:       make(map[string]*experience.PendingGovernance),
		generateRecordsLLMPolicy: skill_call.GenerateRecordsDefaultPolicy(),
		evaluateLLMPolicy:       experience.EvaluateDefaultPolicy(),
		simplifyLLMPolicy:       experience.SimplifyDefaultPolicy(),
	}

	// 应用可选参数
	for _, opt := range opts {
		opt(r)
	}

	// ─── 初始化核心组件 ───

	r.evolutionStore = checkpointing.NewEvolutionStore(r.skillsDir)

	r.evolver = skill_call.NewSkillExperienceOptimizer(
		llmModel, model, language,
		r.generateRecordsLLMPolicy,
	)

	r.scorer = experience.NewExperienceScorer(
		llmModel, model, language,
		experience.WithEvaluateLLMPolicy(r.evaluateLLMPolicy),
		experience.WithSimplifyLLMPolicy(r.simplifyLLMPolicy),
	)

	pendingApprovalSnapshots := make(map[string]*experience.PendingChange)

	r.manager, _ = experience.NewExperienceManager(
		r.evolutionStore,
		r.scorer,
		"skill",
		language,
		r.skillOps,
		pendingApprovalSnapshots,
		r.pendingGovernance,
	)

	r.approvalRuntime = NewEvolutionApprovalRuntime(
		r.manager,
		pendingApprovalSnapshots,
	)

	r.onlineUpdater = single_dim.NewSingleDimUpdater(r.evolver)

	r.orchestrator = experience.NewOnlineEvolutionOrchestrator(
		r.evolutionStore,
		r.onlineUpdater,
		r.manager,
		r.skillOps,
		"skill_evolve_",
		"experience_updater",
	)

	r.experienceTracker = experience.NewExperienceTracker(
		r.evolutionStore,
		r.scorer,
		r.evalInterval,
	)

	// ─── 初始化 Sharing ───
	if r.sharingEnabled && r.shareStager == nil && r.experienceSharer != nil {
		r.shareStager = sharing.NewShareStager(
			sharing.NewKeywordExtractor(llmModel, model, language),
			r.experienceSharer,
		)
	}

	// ─── 构造 EvolutionRail 基类 ───
	railOpts := []EvolutionRailOption{
		WithDisabledSkills(r._normalizeNameSet(r.skillsDir).Keys()),
	}
	if r.evolutionTimeoutSec > 0 {
		railOpts = append(railOpts, WithMaxConcurrentEvolution(1))
	}
	r.EvolutionRail = NewEvolutionRail(r, railOpts...)

	return r
}

// WithAutoScan 设置是否自动检测演进信号。
func WithAutoScan(autoScan bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.autoScan = autoScan }
}

// WithAutoSave 设置是否自动保存（跳过审批）。
func WithAutoSave(autoSave bool) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.autoSave = autoSave }
}

// WithEvalInterval 设置评估间隔。
func WithEvalInterval(interval int) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.evalInterval = interval }
}

// WithEvolutionTimeout 设置后台演化超时秒数。
func WithEvolutionTimeout(secs float64) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.evolutionTimeoutSec = secs }
}

// WithGenerateRecordsLLMPolicy 设置记录生成 LLM 调用策略。
func WithGenerateRecordsLLMPolicy(policy llm_resilience.LLMInvokePolicy) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.generateRecordsLLMPolicy = policy }
}

// WithEvaluateLLMPolicy 设置评估 LLM 调用策略。
func WithEvaluateLLMPolicy(policy llm_resilience.LLMInvokePolicy) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.evaluateLLMPolicy = policy }
}

// WithSimplifyLLMPolicy 设置精简 LLM 调用策略。
func WithSimplifyLLMPolicy(policy llm_resilience.LLMInvokePolicy) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.simplifyLLMPolicy = policy }
}

// WithSharingConfig 设置跨用户共享配置。
//
// config 非空时创建 ExperienceSharer + ShareStager，对齐 Python: _init_sharing()。
func WithSharingConfig(config map[string]any) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		if config == nil {
			r.sharingEnabled = false
			return
		}
		enabled, _ := config["enabled"].(bool)
		if !enabled {
			r.sharingEnabled = false
			return
		}
		r.sharingEnabled = true
		hubPath, _ := config["hub_path"].(string)
		if hubPath == "" {
			return
		}
		backend := sharing.NewLocalFileBackend(hubPath)
		cacheDir, _ := config["local_cache_dir"].(string)
		r.experienceSharer = sharing.NewExperienceSharer(
			backend,
			sharing.WithSharerLocalCacheDir(cacheDir),
		)
	}
}

// WithDisabledSkills 设置禁用的技能名称列表。
func WithDisabledSkillsSet(names []string) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) {
		// 委托给基类的 WithDisabledSkills
	}
}

// Priority 返回优先级 80。
// 对齐 Python: SkillEvolutionRail.priority = 80
func (r *SkillEvolutionRail) Priority() int { return 80 }

// ─── 属性访问器 ───

// EvolutionStore 返回技能演进数据存储。
func (r *SkillEvolutionRail) EvolutionStore() *checkpointing.EvolutionStore {
	return r.evolutionStore
}

// Scorer 返回经验评分器。
func (r *SkillEvolutionRail) Scorer() *experience.ExperienceScorer {
	return r.scorer
}

// Evolver 返回经验优化器。
func (r *SkillEvolutionRail) Evolver() *skill_call.SkillExperienceOptimizer {
	return r.evolver
}

// AutoScan 返回是否自动检测演进信号。
func (r *SkillEvolutionRail) AutoScan() bool { return r.autoScan }

// SetAutoScan 设置是否自动检测演进信号。
func (r *SkillEvolutionRail) SetAutoScan(v bool) { r.autoScan = v }

// AutoSave 返回是否自动保存。
func (r *SkillEvolutionRail) AutoSave() bool { return r.autoSave }

// SetAutoSave 设置是否自动保存。
func (r *SkillEvolutionRail) SetAutoSave(v bool) { r.autoSave = v }

// ApprovalRuntime 返回审批运行时。
func (r *SkillEvolutionRail) ApprovalRuntime() *EvolutionApprovalRuntime {
	return r.approvalRuntime
}

// ProcessedSignalKeys 返回已处理的信号指纹集合。
func (r *SkillEvolutionRail) ProcessedSignalKeys() map[[4]string]bool {
	return r.processedSignalKeys
}

// ClearProcessedSignals 清空已处理信号指纹集合。
func (r *SkillEvolutionRail) ClearProcessedSignals() {
	r.processedSignalKeys = make(map[[4]string]bool)
}

// IsSharingEnabled 返回是否启用共享。
func (r *SkillEvolutionRail) IsSharingEnabled() bool { return r.sharingEnabled }
```

**注意**：`experience.NewExperienceScorer`、`skill_call.GenerateRecordsDefaultPolicy`、`experience.EvaluateDefaultPolicy`、`experience.SimplifyDefaultPolicy` 等函数需要确认是否已存在，如不存在需要在对应包中添加。`sharing.NewShareStager` 和 `sharing.NewKeywordExtractor` 的签名需与实际代码对齐。

- [ ] **Step 2: 确认依赖函数签名**

检查以下函数/方法是否已存在于对应包中：
- `experience.NewExperienceScorer` 签名
- `skill_call.GenerateRecordsDefaultPolicy()` 是否存在
- `experience.EvaluateDefaultPolicy()` / `experience.SimplifyDefaultPolicy()` 是否存在
- `sharing.NewShareStager` / `sharing.NewKeywordExtractor` 签名
- `checkpointing.NewEvolutionStore` 签名
- `single_dim.NewSingleDimUpdater` 签名

如不存在，需要在对应包中添加默认策略常量或构造函数。

Run: `cd /home/opensource/uap-claw-go && grep -rn 'func NewExperienceScorer\|func GenerateRecordsDefaultPolicy\|func EvaluateDefaultPolicy\|func SimplifyDefaultPolicy\|func NewShareStager\|func NewKeywordExtractor\|func NewEvolutionStore\|func NewSingleDimUpdater' internal/`

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`

Expected: 编译通过（结构体定义和构造函数不依赖未实现方法）

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go
git commit -m "feat(evolution): 添加 SkillEvolutionRail 结构体和构造函数 (9.24 P3)"
```

---

### Task 2: EvolutionExtension 9 方法实现

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

- [ ] **Step 1: 实现 AllowEvolutionTrigger 和 SnapshotForEvolution**

在 skill_evolution_rail.go 的导出函数区追加：

```go
// AllowEvolutionTrigger 返回 autoScan 控制是否允许触发演化。
//
// 对齐 Python: SkillEvolutionRail._allow_evolution_trigger(trigger_point, ctx)
func (r *SkillEvolutionRail) AllowEvolutionTrigger(trigger EvolutionTriggerPoint, cbc *agentinterfaces.AgentCallbackContext) bool {
	return r.autoScan
}

// SnapshotForEvolution 捕获消息和评估状态用于异步演化。
//
// 对齐 Python: SkillEvolutionRail._snapshot_for_evolution(trajectory, ctx)
func (r *SkillEvolutionRail) SnapshotForEvolution(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) *EvolutionSnapshot {
	if !r.autoScan {
		return nil
	}

	// 基类快照：收集 messages
	snapshot := r.EvolutionRail._collectMessagesFromTrajectory(traj)
	if len(snapshot) == 0 {
		return nil
	}

	// 收集 session_id
	sessionID := ""
	if cbc != nil {
		sessionID = cbc.Session().GetSessionID()
	}

	// 消费评估状态
	presentedEntries := r.experienceTracker.ConsumeEvalState(sessionID)

	skillName := "skill-evolution"
	return &EvolutionSnapshot{
		Trajectory: traj,
		Messages:   snapshot,
		SkillName:  &skillName,
		// presented_entries 和 session_id 通过扩展字段传递
		// 需要在 EvolutionSnapshot 中扩展或在 RunEvolution 中从 ctx 获取
	}
}
```

**注意**：EvolutionSnapshot 当前只有 Trajectory / Messages / SkillName 三个字段。需要在 contracts.go 中扩展以支持 `presented_entries` 和 `session_id`。

- [ ] **Step 2: 扩展 EvolutionSnapshot 增加 PresentedEntries 和 SessionID**

修改 `internal/agentcore/harness/rails/evolution/contracts.go`：

在 `EvolutionSnapshot` 结构体中添加：
```go
// PresentedEntries 评估状态条目（可选）
PresentedEntries []experience.PresentedRecordEntry
// SessionID 会话标识（可选）
SessionID string
// IncrementalMessages 增量消息（可选，用于 sharing download）
IncrementalMessages []map[string]any
```

- [ ] **Step 3: 实现其余 7 个 EvolutionExtension 方法**

在 skill_evolution_rail.go 追加：

```go
// OnBeforeInvoke 空操作，基类已处理轨迹初始化。
func (r *SkillEvolutionRail) OnBeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterModelCall 空操作，基类已记录 LLM 步骤。
func (r *SkillEvolutionRail) OnAfterModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterToolCall 检测经验详情读取，追踪 presented records。
//
// 对齐 Python: SkillEvolutionRail._on_after_tool_call(ctx)
func (r *SkillEvolutionRail) OnAfterToolCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	inputs, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs)
	if !ok {
		return nil
	}

	skillName := r.detectExperienceDetailRead(inputs)
	if skillName == "" {
		return nil
	}

	content := r.extractToolContent(inputs)
	recordIDs := r.extractPresentedRecordIDs(content)
	if len(recordIDs) == 0 {
		return nil
	}

	sessionID := ""
	if cbc.Session() != nil {
		sessionID = cbc.Session().GetSessionID()
	}

	r.experienceTracker.RecordPresentedRecords(ctx, sessionID, skillName, content, recordIDs)
	return nil
}

// OnAfterInvoke 空操作，基类已保存轨迹+触发演化。
func (r *SkillEvolutionRail) OnAfterInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterTaskIteration 空操作。
func (r *SkillEvolutionRail) OnAfterTaskIteration(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}

// OnAfterEvolutionTriggered 空操作，被动演化在 RunEvolution 处理。
func (r *SkillEvolutionRail) OnAfterEvolutionTriggered(ctx context.Context, traj *trajectory.Trajectory, cbc *agentinterfaces.AgentCallbackContext) error {
	return nil
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/contracts.go
git commit -m "feat(evolution): 实现 SkillEvolutionRail EvolutionExtension 9 方法 (9.24 P3)"
```

---

### Task 3: RunEvolution 核心流程

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

- [ ] **Step 1: 实现 RunEvolution 方法**

在 skill_evolution_rail.go 导出函数区追加核心方法：

```go
// RunEvolution 执行技能演进，基于收集到的轨迹。
//
// 异步模式：ctx=nil，snapshot 包含捕获的数据；同步模式：ctx 活跃，snapshot=nil。
//
// 对齐 Python: SkillEvolutionRail.run_evolution(trajectory, ctx, snapshot)
func (r *SkillEvolutionRail) RunEvolution(ctx context.Context, traj *trajectory.Trajectory, snapshot *EvolutionSnapshot) error {
	logger.Info(logComponent).Bool("auto_scan", r.autoScan).Msg("[SkillEvolutionRail] run_evolution called")
	if !r.autoScan {
		logger.Info(logComponent).Msg("[SkillEvolutionRail] auto_scan disabled, skipping")
		return nil
	}

	var messages []map[string]any
	var presentedEntries []experience.PresentedRecordEntry

	// 异步路径：从 snapshot 读取
	if snapshot != nil {
		if snapshot.Trajectory != nil {
			traj = snapshot.Trajectory
		}
		messages = snapshot.Messages
		presentedEntries = snapshot.PresentedEntries
	} else if traj != nil {
		// 同步路径：从轨迹收集消息
		messages = r._collectMessagesFromTrajectory(traj)
	} else {
		return nil
	}

	logger.Info(logComponent).Int("messages", len(messages)).Msg("[SkillEvolutionRail] collected messages")

	r.emitProgress("started", "starting regular skill evolution analysis for completed conversation")

	if len(messages) == 0 {
		logger.Info(logComponent).Msg("[SkillEvolutionRail] no messages, skipping")
		r.emitProgress("cancelled", "no conversation messages available; cancelling regular skill evolution analysis")
		r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
		return nil
	}

	// 列出 regular skills
	allSkillNames := r.evolutionStore.ListSkillNames(ctx)
	skillNames := r.filterRegularSkills(ctx, allSkillNames)

	r.emitProgress("detecting_signals", fmt.Sprintf("checking %d regular skill(s) for evolution signals (filtered from %d local skill(s))", len(skillNames), len(allSkillNames)))

	// 信号检测
	detector := signal.NewConversationSignalDetector(
		signal.WithExistingSkills(r.existingSkillSet(ctx, skillNames)),
	).BindLLM(r.evolver.LLM(), r.evolver.Model(), r.language)

	detected := detector.DetectTrajectorySignals(traj, messages)

	var signals []*signal.EvolutionSignal
	existingFingerprints := make(map[[4]string]bool)

	for _, sig := range detected {
		fp := signal.MakeSignalFingerprint(sig)
		existingFingerprints[fp] = true
		if !r.processedSignalKeys[fp] {
			r.processedSignalKeys[fp] = true
			signals = append(signals, sig)
		}
	}

	// 用户意图信号（LLM 辅助检测）
	userIntentSignals, _ := detector.DetectUserIntent(ctx, messages)
	for _, sig := range userIntentSignals {
		fp := signal.MakeSignalFingerprint(sig)
		if existingFingerprints[fp] {
			continue
		}
		if !r.processedSignalKeys[fp] {
			r.processedSignalKeys[fp] = true
			existingFingerprints[fp] = true
			signals = append(signals, sig)
		}
	}

	// 限制去重集合大小
	if len(r.processedSignalKeys) > maxProcessedSignalKeys {
		r.processedSignalKeys = make(map[[4]string]bool)
	}

	logger.Info(logComponent).Int("signals", len(signals)).Msg("[SkillEvolutionRail] detected signals")

	// 无信号时推断 primary skill
	if len(signals) == 0 {
		primarySkill := r.inferPrimarySkill(messages, skillNames)
		if primarySkill != "" {
			signals = []*signal.EvolutionSignal{
				signal.MakeEvolutionSignal(
					schema.ConversationReviewSignal, "", "[Auto] No rule-based signals. Analyze conversation for implicit experiences.",
					signal.WithSkillName(primarySkill),
					signal.WithSource("passive_conversation"),
				),
			}
		}
	}

	// 技能归属
	skillGroups := r.attributeSignalsToSkills(signals)

	if len(skillGroups) == 0 {
		msg := "no skill usage of a regular skill or actionable evolution signal detected; cancelling regular skill evolution analysis"
		if len(signals) > 0 {
			msg = "detected evolution signals but no regular skill could be attributed; cancelling regular skill evolution analysis"
		}
		r.emitProgress("cancelled", msg)
		r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
		return nil
	}

	// Sharing 下载（演化前从 hub 获取共享经验）
	downloadedPerSkill := map[string][]checkpointing.EvolutionRecord{}
	if r.sharingEnabled && r.experienceSharer != nil && len(skillGroups) > 0 {
		// 对齐 Python: _download_shared_experiences
		for skillName := range skillGroups {
			shared, err := r.experienceSharer.Download(ctx, skillName)
			if err != nil {
				logger.Warn(logComponent).Str("skill", skillName).Err(err).Msg("[SkillEvolutionRail] sharing download failed")
				continue
			}
			if len(shared) > 0 {
				var records []checkpointing.EvolutionRecord
				for _, exp := range shared {
					records = append(records, exp.Records...)
				}
				downloadedPerSkill[skillName] = records
			}
		}
	}

	// 逐技能演化
	for skillName, skillSignals := range skillGroups {
		sharedRecords := downloadedPerSkill[skillName]
		r.evolveSkill(ctx, skillName, skillSignals, messages, sharedRecords)
	}

	r.experienceTracker.EvaluatePresented(ctx, presentedEntries)
	return nil
}
```

- [ ] **Step 2: 实现辅助方法**

在非导出函数区追加：

```go
// emitProgress 发送进度事件。
// 对齐 Python: SkillEvolutionRail._emit_progress()
func (r *SkillEvolutionRail) emitProgress(stage, message string, opts ...ProgressEventOption) {
	logger.Info(logComponent).Str("stage", stage).Msg(message)
	r.EmitHostEvent(BuildEvolutionProgressEvent("regular", stage, message, append(opts, WithPrefix("[Skill Evolution]"))...))
}

// filterRegularSkills 过滤非常规技能（team-skill/swarm-skill）和禁用技能。
// 对齐 Python: SkillEvolutionRail._is_regular_skill()
func (r *SkillEvolutionRail) filterRegularSkills(ctx context.Context, allNames []string) []string {
	var result []string
	for _, name := range allNames {
		if r._isSkillDisabled(name) {
			continue
		}
		if !r.isRegularSkill(ctx, name) {
			continue
		}
		result = append(result, name)
	}
	return result
}

// isRegularSkill 判断技能是否为常规类型（排除 team-skill/swarm-skill）。
// 对齐 Python: SkillEvolutionRail._is_regular_skill()
func (r *SkillEvolutionRail) isRegularSkill(ctx context.Context, name string) bool {
	skillDir := r.evolutionStore.ResolveSkillDir(name)
	if skillDir == "" {
		return true
	}
	skillMD := filepath.Join(skillDir, "SKILL.md")
	content, err := r.evolutionStore.ReadSkillContent(ctx, name)
	if err != nil || content == "" {
		return true
	}
	frontmatter := utils.ParseTopLevelFrontmatter(content)
	kind, _ := frontmatter["kind"].(string)
	return !isNonRegularSkillKind(kind)
}

// isNonRegularSkillKind 判断是否为非常规技能类型。
func isNonRegularSkillKind(kind string) bool {
	return kind == "team-skill" || kind == "swarm-skill"
}

// existingSkillSet 构建已有技能集合。
func (r *SkillEvolutionRail) existingSkillSet(ctx context.Context, names []string) map[string]bool {
	result := make(map[string]bool, len(names))
	for _, name := range names {
		if r.evolutionStore.SkillExists(ctx, name) {
			result[name] = true
		}
	}
	return result
}

// inferPrimarySkill 从 SKILL.md 读取痕迹推断最可能的活跃技能。
// 对齐 Python: SkillEvolutionRail._infer_primary_skill()
func (r *SkillEvolutionRail) inferPrimarySkill(messages []map[string]any, skillNames []string) string {
	var texts []string
	var skillToolPayloads []string

	for _, msg := range messages {
		role, _ := msg["role"].(string)
		if role == "tool" || role == "function" {
			content, _ := msg["content"].(string)
			texts = append(texts, content)
		} else if role == "assistant" {
			if tcSlice, ok := msg["tool_calls"].([]any); ok {
				for _, tc := range tcSlice {
					if tcDict, ok := tc.(map[string]any); ok {
						args, _ := tcDict["arguments"].(string)
						texts = append(texts, args)
						name, _ := tcDict["name"].(string)
						if name == "skill_tool" {
							skillToolPayloads = append(skillToolPayloads, args)
						}
					}
				}
			}
		}
	}

	return utils.InferSkillFromTexts(skillNames, skillToolPayloads, texts)
}

// attributeSignalsToSkills 将信号归属到技能，处理无归属信号。
// 对齐 Python: run_evolution 中的技能归属逻辑
func (r *SkillEvolutionRail) attributeSignalsToSkills(signals []*signal.EvolutionSignal) map[string][]*signal.EvolutionSignal {
	// 单技能 fallback：将无归属信号分配给唯一归属技能
	attributedSkills := make(map[string]bool)
	var unattributed []*signal.EvolutionSignal
	for _, sig := range signals {
		if sig.SkillName != nil && *sig.SkillName != "" {
			attributedSkills[*sig.SkillName] = true
		} else {
			unattributed = append(unattributed, sig)
		}
	}

	if len(attributedSkills) == 1 && len(unattributed) > 0 {
		fallbackSkill := ""
		for k := range attributedSkills {
			fallbackSkill = k
			break
		}
		for _, sig := range unattributed {
			sig.SkillName = &fallbackSkill
		}
	}

	groups := make(map[string][]*signal.EvolutionSignal)
	for _, sig := range signals {
		if sig.SkillName == nil || *sig.SkillName == "" {
			continue
		}
		groups[*sig.SkillName] = append(groups[*sig.SkillName], sig)
	}
	return groups
}

// evolveSkill 对单个技能执行演进。
// 对齐 Python: SkillEvolutionRail._evolve_skill_with_sharing()
func (r *SkillEvolutionRail) evolveSkill(ctx context.Context, skillName string, skillSignals []*signal.EvolutionSignal, messages []map[string]any, sharedRecords []checkpointing.EvolutionRecord) {
	r.emitProgress("generating_updates", fmt.Sprintf("generating evolution records for '%s'", skillName), WithSkillName(skillName))

	// 将 shared records 作为额外信号注入
	if len(sharedRecords) > 0 {
		// 对齐 Python: _evolve_skill_with_sharing 中 shared_records 参数
		// 通过 orchestrator 的 metadata 传递
	}

	// 走 orchestrator 统一演进流程
	result, err := r.orchestrator.Evolve(
		ctx,
		skillName,
		convertSignalsToValues(skillSignals),
		messages,
		"", // userQuery
		nil, // trajectory
		!r.autoSave, // requiresApproval
		map[string]any{},
		nil, // source
	)

	if err != nil {
		logger.Warn(logComponent).Str("skill", skillName).Err(err).Msg("[SkillEvolutionRail] evolve_skill failed")
		r.emitProgress("failed", fmt.Sprintf("evolution failed for '%s': %s", skillName, err.Error()), WithSkillName(skillName))
		return
	}

	if result.Request == nil {
		if result.Status == experience.OnlineEvolutionStatusNoEvolutionNoRecords {
			r.emitBackgroundOutcomeEvent(map[string]string{
				"status":    string(result.Status),
				"message":   result.Message,
				"rail_kind": "regular",
				"skill_name": result.SkillName,
				"stage":     "completed",
				"source":    "experience_updater",
			})
		}
		r.emitProgress("completed", fmt.Sprintf("no evolution records generated for '%s'", skillName), WithSkillName(skillName))
		return
	}

	// 路由审批/自动保存
	if !r.autoSave {
		// 需要审批：发送审批事件
		r.emitGeneratedRecords(nil, skillName, result.Request)
	} else {
		// 自动审批：Sharing 上传
		if r.sharingEnabled && r.shareStager != nil {
			pending := r.manager.PendingApprovalSnapshots()[result.Request.RequestID]
			if pending != nil {
				r.shareStager.ScreenAndStage(ctx, skillName, pending.Payload)
				r.experienceSharer.FlushUploads(ctx, skillName)
			}
		}
	}
}
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go
git commit -m "feat(evolution): 实现 SkillEvolutionRail.RunEvolution 核心流程 (9.24 P3)"
```

---

### Task 4: Slash 命令 API 方法

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go`

- [ ] **Step 1: 实现 RequestUserEvolution**

```go
// RequestUserEvolution 用户主动触发的演进入口。
//
// 对齐 Python: SkillEvolutionRail.request_user_evolution(skill_name, user_intent, auto_approve)
func (r *SkillEvolutionRail) RequestUserEvolution(ctx context.Context, skillName string, userIntent string, autoApprove bool) (*EvolutionRequestResult, error) {
	traj := r.buildTrajectory()
	var messages []map[string]any
	var signals []*signal.EvolutionSignal

	if traj != nil && len(traj.Steps) > 0 {
		messages = r._collectMessagesFromTrajectory(traj)
		detected := r.detectActiveRequestSignals(ctx, skillName, traj, messages)
		signals = append(signals, detected...)
	}

	// 追加 USER_INTENT_SIGNAL
	if userIntent != "" {
		sig := signal.MakeEvolutionSignal(
			schema.UserIntentSignal,
			"Instructions",
			userIntent,
			signal.WithSkillName(skillName),
			signal.WithSource("explicit_request"),
		)
		signals = appendUniqueSignal(signals, sig)
	}

	if len(signals) == 0 {
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	if len(messages) == 0 && userIntent != "" {
		messages = []map[string]any{{"role": "user", "content": userIntent}}
	}

	requiresApproval := !autoApprove
	request, err := r.handleEvolutionFromSignals(ctx, skillName, signals, messages, nil, userIntent, requiresApproval, false)
	if err != nil {
		return nil, err
	}

	if request == nil {
		return &EvolutionRequestResult{SkillName: skillName}, nil
	}

	records := request.Proposal.Records
	var approvalEvent *stream.OutputSchema
	if !autoApprove {
		approvalEvent = r.buildGeneratedRecordsEvent(skillName, request)
	}

	return &EvolutionRequestResult{
		SkillName:     skillName,
		RequestID:     &request.RequestID,
		ApprovalEvent: approvalEvent,
		Records:       records,
		AutoApproved:  autoApprove,
	}, nil
}
```

- [ ] **Step 2: 实现 RequestSimplify / RequestRebuild / RollbackSkill / ApproveRecord / RejectRecord**

每个方法对应 Python 中的一个 API，实现逻辑直接对照 Python。完整代码包含在 skill_evolution_rail.go 的导出函数区。

关键方法签名：
```go
func (r *SkillEvolutionRail) RequestSimplify(ctx context.Context, skillName string, userIntent *string) (*SimplifyRequestResult, error)
func (r *SkillEvolutionRail) OnApproveSimplify(ctx context.Context, requestID string) (map[string]int, error)
func (r *SkillEvolutionRail) OnRejectSimplify(requestID string)
func (r *SkillEvolutionRail) RequestRebuild(ctx context.Context, skillName string, userIntent *string, minScore float64) (string, error)
func (r *SkillEvolutionRail) RollbackSkill(ctx context.Context, skillName string, version *string) (bool, error)
func (r *SkillEvolutionRail) ApproveRecord(ctx context.Context, requestID string) error
func (r *SkillEvolutionRail) RejectRecord(ctx context.Context, requestID string) error
func (r *SkillEvolutionRail) ShouldHintSimplifyOrRebuild(skillName string) bool
func (r *SkillEvolutionRail) UpdateLLM(llmModel *llm.Model, model string)
func (r *SkillEvolutionRail) SetSysOperation(sysOp sys_operation.SysOperation)
```

- [ ] **Step 3: 实现辅助方法**

非导出函数区追加：
```go
func (r *SkillEvolutionRail) detectActiveRequestSignals(ctx context.Context, skillName string, traj *trajectory.Trajectory, messages []map[string]any) []*signal.EvolutionSignal
func (r *SkillEvolutionRail) handleEvolutionFromSignals(ctx context.Context, skillName string, signals []*signal.EvolutionSignal, messages []map[string]any, cbc *agentinterfaces.AgentCallbackContext, userQuery string, requiresApproval bool, emitHostEvents bool) (*experience.ExperienceApprovalRequest, error)
func (r *SkillEvolutionRail) buildGeneratedRecordsEvent(skillName string, request *experience.ExperienceApprovalRequest) *stream.OutputSchema
func (r *SkillEvolutionRail) emitGeneratedRecords(cbc *agentinterfaces.AgentCallbackContext, skillName string, request *experience.ExperienceApprovalRequest)
func (r *SkillEvolutionRail) detectExperienceDetailRead(inputs *agentinterfaces.ToolCallInputs) string
func (r *SkillEvolutionRail) extractToolContent(inputs *agentinterfaces.ToolCallInputs) string
func (r *SkillEvolutionRail) extractPresentedRecordIDs(content string) []string
func appendUniqueSignal(signals []*signal.EvolutionSignal, sig *signal.EvolutionSignal) []*signal.EvolutionSignal
func convertSignalsToValues(ptrs []*signal.EvolutionSignal) []signal.EvolutionSignal
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/harness/rails/evolution/...`

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go
git commit -m "feat(evolution): 实现 SkillEvolutionRail Slash 命令 API 方法 (9.24 P3)"
```

---

### Task 5: DeepAdapter 字段类型 + buildSkillEvolutionRail

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`

- [ ] **Step 1: 修改 skillEvolutionRail 字段类型**

在 `deep_adapter.go` 中将：
```go
// skillEvolutionRail 技能演进护栏
// ⤵️ 10.6.3-10: SkillEvolutionRail
skillEvolutionRail sainterfaces.AgentRail
```

改为：
```go
// skillEvolutionRail 技能演进护栏
// ✅ 已回填：SkillEvolutionRail（对齐 Python: _skill_evolution_rail: SkillEvolutionRail | None）
skillEvolutionRail *evolution.SkillEvolutionRail
```

需要添加 import：`"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"`

- [ ] **Step 2: 实现 buildSkillEvolutionRail**

在 `deep_adapter_rails.go` 中将占位替换为实际实现：

```go
// buildSkillEvolutionRail 构建技能演进护栏。
// ✅ 已回填：SkillEvolutionRail（对齐 Python: _build_skill_evolution_rail()）
func (d *DeepAdapter) buildSkillEvolutionRail() sainterfaces.AgentRail {
	if d.model == nil {
		logger.Warn(logComponent).Msg("buildSkillEvolutionRail: model 为空，跳过")
		return nil
	}

	// 读取 evolution 配置
	evolutionConfig, _ := d.configCache["evolution"].(map[string]any)
	enabled, _ := evolutionConfig["enabled"].(bool)
	if !enabled {
		return nil
	}

	// 读取 auto_scan
	autoScan := true
	if as, ok := evolutionConfig["auto_scan"].(bool); ok {
		autoScan = as
	}
	// 环境变量覆盖
	if envAS := os.Getenv("EVOLUTION_AUTO_SCAN"); envAS != "" {
		autoScan = strings.ToLower(envAS) == "true"
	}

	// auto_save 默认 false（需要审批）
	autoSave := false
	if as, ok := evolutionConfig["auto_save"].(bool); ok {
		autoSave = as
	}

	// 禁用技能列表
	var disabledSkills []string
	if d.skillManager != nil {
		disabledSkills = d.skillManager.ListExecutionDisabledSkills()
	}

	// skills 目录
	skillsDir := d.resolveSkillsDir()

	modelName := d.defaultModelName
	if modelName == "" {
		modelName = "qwen-max"
	}

	opts := []evolution.SkillEvolutionRailOption{
		evolution.WithAutoScan(autoScan),
		evolution.WithAutoSave(autoSave),
		evolution.WithDisabledSkillsSet(disabledSkills),
	}

	// sharing 配置
	sharingConfig, _ := evolutionConfig["sharing"].(map[string]any)
	if sharingConfig != nil {
		opts = append(opts, evolution.WithSharingConfig(sharingConfig))
	}

	rail := evolution.NewSkillEvolutionRail(
		skillsDir,
		d.model,
		modelName,
		"cn",
		opts...,
	)

	logger.Info(logComponent).Msg("SkillEvolutionRail 创建成功")
	return rail
}
```

- [ ] **Step 3: 实现 updateRailsForMode 中 evolution 分支**

在 `deep_adapter_rails.go` 的 `updateRailsForMode` 中替换占位：

```go
// evolution_enabled 分支
evolutionConfig, _ := d.configCache["evolution"].(map[string]any)
evolutionEnabled, _ := evolutionConfig["enabled"].(bool)
if evolutionEnabled {
	if d.skillEvolutionRail == nil {
		rail := d.buildSkillEvolutionRail()
		if rail != nil {
			d.skillEvolutionRail = rail.(*evolution.SkillEvolutionRail)
			if d.instance != nil {
				d.instance.RegisterRail(d.skillEvolutionRail)
			}
		}
	}
} else {
	if d.skillEvolutionRail != nil {
		if d.instance != nil {
			d.instance.UnregisterRail(d.skillEvolutionRail)
		}
		d.skillEvolutionRail = nil
	}
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_rails.go
git commit -m "feat(adapter): 回填 DeepAdapter buildSkillEvolutionRail + updateRailsForMode (9.24 P3)"
```

---

### Task 6: Slash 命令处理器回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_slash.go`

- [ ] **Step 1: 实现 handleEvolveCommand**

```go
// handleEvolveCommand 处理 /evolve 命令。
// ✅ 已回填（对齐 Python: _handle_evolve_command()）
func (d *DeepAdapter) handleEvolveCommand(ctx context.Context, query string, sessionID string) (map[string]any, error) {
	if d.skillEvolutionRail == nil {
		logger.Warn(logComponent).Str("session_id", sessionID).Msg("handleEvolveCommand: SkillEvolutionRail 未初始化")
		return nil, nil
	}

	// 解析命令：/evolve [skill_name] [query]
	parts := strings.Fields(strings.TrimPrefix(query, "/evolve"))
	skillName := ""
	userIntent := ""
	if len(parts) >= 1 {
		skillName = parts[0]
	}
	if len(parts) >= 2 {
		userIntent = strings.Join(parts[1:], " ")
	}

	if skillName == "" {
		// /evolve 无参数 → 列出可用技能
		return d.handleEvolveListCommand(ctx, sessionID)
	}

	result, err := d.skillEvolutionRail.RequestUserEvolution(ctx, skillName, userIntent, false)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("skill", skillName).Msg("handleEvolveCommand: RequestUserEvolution 失败")
		return map[string]any{"status": "failed", "error": err.Error()}, nil
	}

	if result.ApprovalEvent != nil {
		return map[string]any{
			"action":           "approval_required",
			"approval_chunks":  []map[string]any{result.ApprovalEvent.Payload.(map[string]any)},
			"request_id":       ptrToStr(result.RequestID),
			"skill_name":       result.SkillName,
		}, nil
	}

	return map[string]any{
		"status":     "ok",
		"skill_name": result.SkillName,
		"auto_approved": result.AutoApproved,
	}, nil
}
```

- [ ] **Step 2: 实现 handleEvolveListCommand**

读取 EvolutionStore 中的技能列表和经验摘要。

- [ ] **Step 3: 实现 handleEvolveSimplifyCommand**

调用 `d.skillEvolutionRail.RequestSimplify(ctx, skillName, &userIntent)`。

- [ ] **Step 4: 实现 handleEvolveRebuildCommand**

调用 `d.skillEvolutionRail.RequestRebuild(ctx, skillName, &userIntent, 0.5)`，返回 `{action: "run_rebuild_followup", followup_prompt: prompt}`。

- [ ] **Step 5: 实现 handleEvolveRollbackCommand**

调用 `d.skillEvolutionRail.RollbackSkill(ctx, skillName, &version)`。

- [ ] **Step 6: 实现 handleGovernanceApproval**

根据 request_id 前缀路由到 `OnApproveSimplify` / `OnRejectSimplify`。

- [ ] **Step 7: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_slash.go
git commit -m "feat(adapter): 回填 /evolve* slash 命令处理器 (9.24 P3)"
```

---

### Task 7: Evolution watcher + approval 路由回填

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_evolution.go`
- Modify: `internal/swarm/server/adapter/deep_adapter.go`

- [ ] **Step 1: 实现 watchEvolutionAndPush**

goroutine 轮询 `d.skillEvolutionRail.DrainPendingApprovalEvents()` → 通过 evolution helpers 推送到前端。

- [ ] **Step 2: 实现 handleEvolutionApproval**

根据 request_id 前缀路由到 `d.skillEvolutionRail.ApproveRecord` / `RejectRecord`。

- [ ] **Step 3: 实现 HandleUserAnswer 3 个路由分支**

在 `deep_adapter.go` 的 `HandleUserAnswer` 中回填：
- `skill_evolve_` → `handleEvolutionApproval`
- `evolve_simplify_` → `handleGovernanceApproval`
- `team_skill_evolve_` → 保留占位（依赖 P4）

- [ ] **Step 4: 实现 processMessage / processMessageStream 中 slash 命令集成**

在消息处理入口添加 `handleSlashCommand` 调用，处理 `/evolve_rebuild` 返回的 followup_prompt。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/swarm/server/adapter/...`

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_evolution.go internal/swarm/server/adapter/deep_adapter.go
git commit -m "feat(adapter): 回填 evolution watcher + approval 路由 (9.24 P3)"
```

---

### Task 8: doc.go 更新 + 单元测试

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/doc.go`
- Create: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 更新 doc.go**

在 `evolution/doc.go` 的文件目录中添加：
```
//	├── skill_evolution_rail.go  # SkillEvolutionRail 单 Agent 技能演进护栏
```

- [ ] **Step 2: 编写单元测试**

覆盖以下测试用例：
- `TestNewSkillEvolutionRail` — 构造函数 + Options
- `TestSkillEvolutionRail_Priority` — 优先级为 80
- `TestSkillEvolutionRail_AllowEvolutionTrigger` — autoScan 控制
- `TestSkillEvolutionRail_ClearProcessedSignals` — 清空去重集合
- `TestSkillEvolutionRail_ShouldHintSimplifyOrRebuild` — 经验≥10 条提示
- `TestSkillEvolutionRail_AppendUniqueSignal` — 去重逻辑
- `TestSkillEvolutionRail_FilterRegularSkills` — 过滤 team-skill
- `TestSkillEvolutionRail_AttributeSignalsToSkills` — 信号归属
- `TestSkillEvolutionRail_DetectExperienceDetailRead` — 经验详情读取检测
- `TestSkillEvolutionRail_ExtractPresentedRecordIDs` — record ID 提取

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/harness/rails/evolution/...`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/harness/rails/evolution/doc.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "test(evolution): 添加 SkillEvolutionRail 单元测试 (9.24 P3)"
```

---

### Task 9: IMPLEMENTATION_PLAN.md 状态更新

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 9.24 行状态**

将 9.24 行的 P3 和 P5 状态更新：

```
| 9.24 | 🔄 | EvolutionRail | P1(✅) / P2(✅) / P3(✅ 单Agent演化: SkillEvolutionRail+slash命令回填+DeepAdapter占位回填+Sharing集成) / P4(☐) / P5(✅ 跨用户共享组合到SkillEvolutionRail) / P6(☐) |
```

- [ ] **Step 2: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.24 P3/P5 实现状态 (9.24 P3)"
```

---

### Task 10: 全量编译 + 测试验证

**Files:**
- 无新增修改

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`

Expected: 编译通过

- [ ] **Step 2: 运行 evolution 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/harness/rails/evolution/...`

Expected: 覆盖率 ≥ 85%

- [ ] **Step 3: 运行 adapter 包测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/swarm/server/adapter/...`

Expected: 测试通过

- [ ] **Step 4: 最终 Commit**

如需修复任何编译/测试问题，修复后提交。
