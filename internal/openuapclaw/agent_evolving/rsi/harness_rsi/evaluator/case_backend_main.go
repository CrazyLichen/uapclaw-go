// case_backend.go 用例执行后端——考场搭建与答卷收集（主链）。
// Python: openjiuwen/rsi/harness_rsi/evaluator/case_backend.py（836 行全量对齐；
// CaseExecutionResult 前置切片见本文件头部段）。
//
// 已知偏差登记：
// ①run span：Python open_agent_run_span/close_agent_run_span（run root 录制）
// 在 Go 侧无对应机制——rootRecording 恒 false（RoleExecutionEvidence 将带
// run_root_not_recording 原因）；回收条件=Go 落地 run root span。
// ②on_max_iterations：Go DeepAgent 工厂无 on_max_iterations 钩子——
// runtime_policy.OnMaxIterations 记入 runtime_config 但不生效。
// ③清理链：Python agent.cleanup_task_resources/Runner.resource_mgr
// .remove_sys_operation；Go 走 DeepAgent 内置生命周期（TeardownTools 保留）。
// ④swebench 容器分支经 SwebenchContainerOps 缝（真身 Task 11）。

package evaluator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/config"
	memberoptimizer "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_evolving/rsi/harness_rsi/member_optimizer"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness"
	harnessconfig "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/harness_config"
)

const logComponentCB = logger.ComponentAgentCore

// ──────────────────────────── 结构体 ────────────────────────────

// CaseExecutionBackend 用例执行策略接口。
// Python: case_backend.py:75 CaseExecutionBackend(Protocol)。
type CaseExecutionBackend interface {
	// Execute 跑一个用例并只返回执行输出（判分留给 CaseRunner）。
	Execute(ctx context.Context, params ExecuteParams) (*CaseExecutionResult, error)
	// Cleanup 释放用例级运行时资源（归档写完后调用）。
	Cleanup(teamName, sessionID string)
}

// ExecuteParams Execute 入参包（Python 关键字参数）。
type ExecuteParams struct {
	// Case 考题映射
	Case map[string]any
	// OutputDir 用例输出目录
	OutputDir string
	// SessionID 会话标识
	SessionID string
	// TeamSkillRefPath 团队技能引用路径（可空）
	TeamSkillRefPath string
	// HarnessRefs harness 引用映射（role→path）
	HarnessRefs map[string]string
}

// SingleHarnessExecutionBackend 经绑定 ExpertHarness 的单个 DeepAgent 跑评估用例。
// Python: case_backend.py:95 SingleHarnessExecutionBackend。
type SingleHarnessExecutionBackend struct {
	// cfg 评估器配置（Python config）
	cfg *config.EvaluatorConfig
	// opsMu 复用锁
	opsMu sync.Mutex
	// containerOps swebench 容器操作缝（nil 时 swebench 用例报基建错误；Task 11 注入）
	containerOps SwebenchContainerOps
}

// SwebenchContainerOps swebench 容器操作缝（Python: prepare_swebench_workspace /
// start_terminal_bench_solver_container / sync_container_git_patch_to_workspace /
// remove_terminal_bench_container——真身 Task 11；本缝由 17.6d 注册）。
type SwebenchContainerOps interface {
	PrepareWorkspace(caseMapping map[string]any, workspaceDir string, timeoutSec int) (string, error)
	StartSolverContainer(dockerImage, caseID, workspaceDir string, timeoutSec int) (string, error)
	SyncGitPatch(containerName, workspaceDir string) (string, error)
	RemoveContainer(containerName string)
}

// swebenchContainerOpsFn 注册的容器操作实现。
var swebenchContainerOpsFn SwebenchContainerOps

// RegisterSwebenchContainerOps 由 17.6d swebench_runtime.go 在 init 时注册。
func RegisterSwebenchContainerOps(ops SwebenchContainerOps) {
	swebenchContainerOpsFn = ops
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSingleHarnessExecutionBackend 构造单 harness 后端。
// Python: case_backend.py:96-100（config 注入；processor 走进程级 demand 单例）。
func NewSingleHarnessExecutionBackend(cfg *config.EvaluatorConfig) *SingleHarnessExecutionBackend {
	return &SingleHarnessExecutionBackend{cfg: cfg}
}

// ReuseTrajectoryProcessor 共享上下文隔离追踪而不重复注册 processor。
// Python: case_backend.py:108-110 reuse_trajectory_processor——Go processor
// 为 demand 进程级单例，共享即 no-op（语义等价，偏差登记）。
func (b *SingleHarnessExecutionBackend) ReuseTrajectoryProcessor(_ *SingleHarnessExecutionBackend) {}

// TrajectorySpanProcessor 返回用例共享的 processor（Python property 等价）。
func (b *SingleHarnessExecutionBackend) TrajectorySpanProcessor() any {
	return demandProcessor()
}

// Execute 跑一个独立 harness，判分留给 CaseRunner。
// Python: case_backend.py:112-330 execute——步骤顺序逐条对齐。
func (b *SingleHarnessExecutionBackend) Execute(ctx context.Context, params ExecuteParams) (*CaseExecutionResult, error) {
	logger.Info(logComponentCB).
		Str("case_id", stringOrTP(params.Case["case_id"], "")).
		Msg("[SingleHarnessExecutionBackend] begin to execute case")
	status := "passed"
	var response any
	errText := ""
	var agent *harness.DeepAgent
	workspaceDir := filepath.Join(absPath(params.OutputDir), "workspace")
	roleName := ""
	workspaceBefore := map[string]map[string]any{}
	workspaceAfter := map[string]map[string]any{}
	var controlledSkillTreatment *ControlledSkillTreatmentRail
	var skillUseRails []*RSISkillUseRail
	solverContainerName := ""
	swebenchModelPatchPath := ""
	var trajectoryRail *RSITrajectoryRail
	rootRecording := false
	var sysOperation sysop.SysOperation
	caseRoot := absPath(params.OutputDir)
	runtimeConfig := map[string]any{}

	swebenchDict, isSwebench := params.Case["swebench"].(map[string]any)

	defer func() {
		// Python finally 段：容器 patch sync → 工作区后快照 → 资源清理。
		if solverContainerName != "" {
			patch, err := b.containerOpsSync(solverContainerName, workspaceDir)
			if err != nil {
				status = "failed"
				errText = fmt.Sprintf("failed to sync SWE-bench solver workspace: %v", err)
			} else {
				patchPath := filepath.Join(absPath(params.OutputDir), "verifier", "container_model.patch")
				if mkErr := os.MkdirAll(filepath.Dir(patchPath), 0o755); mkErr == nil {
					if wErr := os.WriteFile(patchPath, []byte(patch), 0o644); wErr == nil {
						swebenchModelPatchPath = patchPath
					}
				}
			}
			b.containerOpsRemove(solverContainerName)
		}
		workspaceAfter = snapshotWorkspace(workspaceDir)
		if agent != nil {
			// Python: cleanup_task_resources → ability_manager.teardown_tools →
			// Runner.resource_mgr.remove_sys_operation（偏差③：Go 走内置生命周期，
			// teardown tools 保留）。
			if am := agent.AbilityManager(); am != nil {
				am.TeardownTools()
			}
		}
		logger.Info(logComponentCB).
			Str("case_id", stringOrTP(params.Case["case_id"], "")).
			Msg("[SingleHarnessExecutionBackend] end to execute case")
	}()

	var runtimePolicy harnessrsi.HarnessRuntimePolicy
	harnessPath := ""
	// Python try 段（L131-317）：
	roleName, harnessPath, refErr := resolveSingleHarnessRef(params.HarnessRefs)
	if refErr != nil {
		return nil, refErr
	}
	runtimePolicy, policyErr := harnessrsi.ReadRuntimePolicy(harnessPath,
		b.cfg.SolverMaxIterations, b.cfg.SolverOnMaxIterations,
		b.cfg.SolverMaxIterationsHardLimit)
	if policyErr != nil {
		return nil, NewEvaluationInfrastructureError(policyErr.Error())
	}
	runtimeConfig = runtimePolicy.ToDict()
	if mkErr := os.MkdirAll(caseRoot, 0o755); mkErr != nil {
		return nil, NewEvaluationInfrastructureError(mkErr.Error())
	}
	workspaceDir = singleHarnessWorkspaceDir(params.Case, absPath(params.OutputDir), params.SessionID)

	if isSwebench {
		// Python L160-186 容器分支（swebench case）。
		timeoutSec := 1800
		if raw, ok := swebenchDict["timeout_sec"]; ok {
			if f, ok := toFloatAny(raw); ok {
				timeoutSec = int(f)
			}
		}
		image, prepErr := b.containerOpsPrepare(params.Case, workspaceDir, timeoutSec)
		if prepErr != nil {
			return nil, NewEvaluationInfrastructureError(prepErr.Error())
		}
		name, startErr := b.containerOpsStart(image, stringOrTP(params.Case["case_id"], params.SessionID),
			workspaceDir, timeoutSec)
		if startErr != nil {
			return nil, NewEvaluationInfrastructureError(startErr.Error())
		}
		solverContainerName = name
		if stageErr := stageCaseAssets(params.Case, workspaceDir, solverContainerName); stageErr != nil {
			return nil, stageErr
		}
	} else {
		if prepErr := prepareSingleHarnessWorkspace(params.Case, workspaceDir); prepErr != nil {
			return nil, NewEvaluationInfrastructureError(prepErr.Error())
		}
		if stageErr := stageCaseAssets(params.Case, workspaceDir, ""); stageErr != nil {
			return nil, stageErr
		}
	}
	workspaceBefore = snapshotWorkspace(workspaceDir)

	// Python: model = load_member_optimizer_model(self.config.model_config_ref)
	model, modelErr := memberoptimizer.LoadMemberOptimizerModel(b.cfg.ModelConfigRef)
	if modelErr != nil {
		return nil, NewEvaluationInfrastructureError(modelErr.Error())
	}

	// Python: agent_rails = _single_harness_rails(...)
	agentRails, railsErr := singleHarnessRails(singleHarnessRailsParams{
		teamSkillRefPath:    params.TeamSkillRefPath,
		harnessPath:         harnessPath,
		shellOnly:           solverContainerName != "",
		controlledSkillName: controlledSkillName(params.Case),
		workspace:           ifThen(solverContainerName != "", "/testbed", workspaceDir),
	})
	if railsErr != nil {
		return nil, railsErr
	}
	for _, rail := range agentRails {
		if cr, ok := rail.(*ControlledSkillTreatmentRail); ok {
			controlledSkillTreatment = cr
		}
	}

	// Python: agent = create_deep_agent(...)——20 参数逐项对齐。
	card := agentschema.NewAgentCard(agentschema.WithAgentName(roleName))
	var railsList []agentinterfaces.AgentRail
	for _, rail := range agentRails {
		if _, isSkill := rail.(*RSISkillUseRail); !isSkill {
			railsList = append(railsList, rail)
		}
	}
	built, buildErr := harness.CreateDeepAgent(ctx, harnessconfig.CreateDeepAgentParams{
		Model:               model.Model,
		Card:                card,
		SystemPrompt:        singleHarnessSystemPrompt(roleName, ifThen(solverContainerName != "", "/testbed", workspaceDir)),
		Workspace:           workspaceOf(workspaceDir),
		Rails:               railsList,
		EnableTaskLoop:      false,
		MaxIterations:       runtimePolicy.MaxIterations,
		InvocationLogDir:    filepath.Join(caseRoot, "logs"),
		Language:            "en",
		AutoCreateWorkspace: true,
		SysOperation:        sysOperation,
	})
	if buildErr != nil {
		return nil, NewEvaluationInfrastructureError(buildErr.Error())
	}
	agent = built

	// Python: RSISkillUseRail 经原生 API 注册（plugin 发现前）。
	for _, rail := range agentRails {
		if sr, isSkill := rail.(*RSISkillUseRail); isSkill {
			if regErr := agent.RegisterRail(ctx, sr); regErr != nil {
				return nil, NewEvaluationInfrastructureError(regErr.Error())
			}
			skillUseRails = append(skillUseRails, sr)
		}
	}
	// Python: await agent.load_plugin(harness_path)
	if _, loadErr := agent.LoadPlugin(ctx, harnessPath, nil); loadErr != nil {
		return nil, NewEvaluationInfrastructureError(loadErr.Error())
	}
	for _, skillRail := range skillUseRails {
		// Python: 单 harness 执行在源/候选/重放/发布评估中统一任务启动触发。
		skillRail.SetListSkillModel(model.Model)
		skillRail.SetTriggerAtTaskStart(true)
	}
	if solverContainerName != "" {
		// Python: _enforce_container_sys_operation_rail(agent)
		if enforceErr := enforceContainerSysOperationRail(ctx, agent); enforceErr != nil {
			return nil, NewEvaluationInfrastructureError(enforceErr.Error())
		}
	}
	// Python: trajectory_rail = _attach_single_harness_trajectory_rail(...)
	trajectoryRail = attachSingleHarnessTrajectoryRail(ctx, agent, attachTrajectoryRailParams{
		outputDir: absPath(params.OutputDir),
		roleName:  roleName,
	})
	rootRecording = false // 偏差①：Go 无 run root span 机制。

	// Python: response = await run_agent_with_empty_response_recovery(
	//     agent, {"query": _case_inputs(case)}, session=session_id)
	caseInput := normalizeCaseInputForBackend(params.Case, taskInputOf(params.Case))
	var invokeErr error
	response, invokeErr = runAgentForCase(ctx, agent, caseInput, params.SessionID)
	if invokeErr != nil {
		return nil, NewEvaluationInfrastructureError(invokeErr.Error())
	}

	// ─── Python L331-391：termination 构造 + 证据归档 + 结果组装 ───
	resultData, _ := response.(map[string]any)
	if resultData == nil {
		resultData = map[string]any{}
	}
	modelCallCount := trajectoryRail.ModelCallCount()
	termination := map[string]any{
		"termination_reason":          stringOrTP(resultData["termination_reason"], ifThen(errText != "", "exception", "completed")),
		"iterations_used":             resultData["iterations_used"],
		"model_call_count":            modelCallCount,
		"max_iterations":              resultData["max_iterations"],
		"result_type":                 stringOrTP(resultData["result_type"], ifThen(errText != "", "error", "answer")),
		"truncated_by_max_iterations": resultData["truncated_by_max_iterations"],
		"finalization_attempted":      resultData["finalization_attempted"],
		"finalization_succeeded":      resultData["finalization_succeeded"],
		"finalization_error":          stringOrTP(resultData["finalization_error"], ""),
	}
	_ = termination
	if _, exists := resultData["termination_reason"]; !exists {
		termination["termination_reason"] = ifThen(errText != "", "exception", "completed")
	}
	if _, exists := resultData["max_iterations"]; !exists {
		termination["max_iterations"] = runtimeConfig["max_iterations"]
	}
	if _, exists := resultData["result_type"]; !exists {
		termination["result_type"] = ifThen(errText != "", "error", "answer")
	}

	runtimeLogs := []string{}
	executionEvidence := map[string]any(nil)
	evidenceError := ""
	if mkErr := os.MkdirAll(caseRoot, 0o755); mkErr == nil {
		if raw, err := marshalIndentSafe(termination); err == nil {
			_ = os.WriteFile(filepath.Join(caseRoot, "termination.json"), raw, 0o644)
		}
		logRoot := filepath.Join(caseRoot, "logs")
		if entries, err := collectJSONLLogs(logRoot); err == nil {
			runtimeLogs = entries
		}
		executionEvidence = RoleExecutionEvidence(
			filepath.Join(absPath(params.OutputDir), RoleTrajectoryDirName),
			ifThen(roleName != "", roleName, "solver"),
			rootRecording,
			trajectoryRail.CaptureIssues(),
			modelCallCount,
			trajectoryRail.ToolCallCount(),
		)
	} else {
		evidenceError = "failed to create case root"
	}

	metadata := singleHarnessMetadata(singleHarnessMetadataParams{
		roleName:                 roleName,
		workspaceBefore:          workspaceBefore,
		workspaceAfter:           workspaceAfter,
		teamSkillRefPath:         params.TeamSkillRefPath,
		controlledSkillTreatment: controlledEvidenceOf(controlledSkillTreatment),
		skillTriggers:            collectSkillTriggers(skillUseRails),
		commandRecorder:          nil,
		swebenchModelPatchPath:   swebenchModelPatchPath,
		solverMaxIterations:      runtimePolicy.MaxIterations,
		runtimeConfig:            runtimeConfig,
		executionTermination:     termination,
		runtimeLogs:              runtimeLogs,
		executionEvidence:        executionEvidence,
		evidenceError:            evidenceError,
	})
	logger.Info(logComponentCB).Str("case_id", stringOrTP(params.Case["case_id"], "")).
		Msg("[SingleHarnessExecutionBackend] execute completed")
	return &CaseExecutionResult{
		Response:        response,
		ExecutionStatus: status,
		Error:           errText,
		JudgeResult:     nil,
		WorkspaceDir:    workspaceDir,
		Metadata:        metadata,
	}, nil
}

// Cleanup 无操作；execute 释放用例资源，宿主持有的 Runner 不在此释放。
// Python: case_backend.py:325-328 cleanup。
func (b *SingleHarnessExecutionBackend) Cleanup(_, _ string) {}

// BuildBackend 按配置实例化执行后端（RSI 仅暴露独立 Harness 后端）。
// Python: case_backend.py:815-821 build_backend。
func BuildBackend(cfg *config.EvaluatorConfig) (CaseExecutionBackend, error) {
	if cfg == nil {
		return nil, fmt.Errorf("unknown backend type: %v; supported values: [single_harness]", cfg)
	}
	if cfg.Backend != "single_harness" && cfg.Backend != "" {
		return nil, fmt.Errorf("unknown backend type: %q; supported values: [single_harness]", cfg.Backend)
	}
	return NewSingleHarnessExecutionBackend(cfg), nil
}

// ──────────────────────────── 缝位转发 ────────────────────────────

func (b *SingleHarnessExecutionBackend) containerOpsPrepare(caseMapping map[string]any, workspaceDir string, timeoutSec int) (string, error) {
	if swebenchContainerOpsFn == nil {
		return "", NewEvaluationInfrastructureError("swebench container ops not registered (Task 11)")
	}
	return swebenchContainerOpsFn.PrepareWorkspace(caseMapping, workspaceDir, timeoutSec)
}

func (b *SingleHarnessExecutionBackend) containerOpsStart(image, caseID, workspaceDir string, timeoutSec int) (string, error) {
	if swebenchContainerOpsFn == nil {
		return "", NewEvaluationInfrastructureError("swebench container ops not registered (Task 11)")
	}
	return swebenchContainerOpsFn.StartSolverContainer(image, caseID, workspaceDir, timeoutSec)
}

func (b *SingleHarnessExecutionBackend) containerOpsSync(containerName, workspaceDir string) (string, error) {
	if swebenchContainerOpsFn == nil {
		return "", NewEvaluationInfrastructureError("swebench container ops not registered (Task 11)")
	}
	return swebenchContainerOpsFn.SyncGitPatch(containerName, workspaceDir)
}

func (b *SingleHarnessExecutionBackend) containerOpsRemove(containerName string) {
	if swebenchContainerOpsFn != nil {
		swebenchContainerOpsFn.RemoveContainer(containerName)
	}
}
