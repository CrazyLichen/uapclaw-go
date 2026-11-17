// factory.go 提供 MemberOptimizer 内部 agent 工厂。
// Python: agents/factory.py（全量对齐，533 行）。

package agents

import (
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	harnessrails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/filesystem"
	workspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	agentface "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agentface"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	harnessrsi "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	harnessschema "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi"
	"github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer"
	rails "github.com/uapclaw/uapclaw-go/internal/evolving/rsi/harness_rsi/member_optimizer/agents/rails"
)

// ──────────────────────────── 常量 ────────────────────────────

// envVarRE 环境变量占位（Python _ENV_VAR_RE）。
var envVarRE = regexp.MustCompile(`\$\{(\w+)}`)

// placeholderRE 提示词占位（Python _PLACEHOLDER_RE）。
var placeholderRE = regexp.MustCompile(`\{\{([A-Z0-9_]+)\}\}`)

// protectedPackageNames 包相对写入的受保护文件名。
// Python: factory.py _PROTECTED_PACKAGE_NAMES。
var protectedPackageNames = map[string]bool{
	"plan.yaml": true, "execution_results.json": true, "verification.json": true,
	"fix_result.json": true, "current_harness_refs.yaml": true,
	"member_optimization_ref.yaml": true,
}

// protectedTopDirs 写入禁用的顶层目录。
// Python: factory.py package_write_allowed 的 {"current_harnesses", ...}。
var protectedTopDirs = map[string]bool{
	"current_harnesses": true, "evaluation": true, "evaluations": true,
	"audit": true, "checkpoints": true,
}

// repairableTopDirs repair 模式可写的顶层目录。
var repairableTopDirs = map[string]bool{
	"prompt_sections": true, "skills": true, "tools": true,
	"rails": true, "subagents": true,
}

// repairableTopFiles repair 模式可写的顶层文件。
var repairableTopFiles = map[string]bool{
	"harness.yaml": true, "identity.md": true, "soul.md": true,
}

// repairableSuffixes repair 模式可写的文件后缀。
var repairableSuffixes = map[string]bool{
	".yaml": true, ".yml": true, ".json": true, ".py": true,
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemberAgentParams CreateMemberOptimizerAgent 参数（Python 关键字参数收拢）。
type MemberAgentParams struct {
	// Profile agent 档案
	Profile MemberOptimizerAgentProfile
	// ModelConfigRef 模型配置文件引用
	ModelConfigRef string
	// Workspace 工作空间路径
	Workspace string
	// PromptValues 提示词占位值
	PromptValues map[string]string
	// AgentSkillsDirs agent 技能目录
	AgentSkillsDirs []string
	// ExtraRails 额外护栏
	ExtraRails []agentinterfaces.AgentRail
	// EnabledSkillsOverride 启用技能覆盖
	EnabledSkillsOverride []string
	// Tools 额外工具（Python tools 参数）
	Tools []tool.Tool
	// SysOperation 系统操作（Python sys_operation 参数）
	SysOperation sys_operation.SysOperation
}

// OptimizerResourceHandle 资源释放句柄（Python optimizer_agent_resources
// asynccontextmanager finally 段等价）。
type OptimizerResourceHandle struct {
	// operationID 关联的系统操作 ID
	operationID string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// optimizerReleaseFn 取 agent 资源释放闭包（桥装配用）。
func optimizerReleaseFn(agent *harness.DeepAgent) (func() error, error) {
	handle, err := OptimizerAgentResources(agent)
	if err != nil {
		return nil, err
	}
	return handle.Release, nil
}

// OptimizerAgentResources 释放本次调用的原生资源而不停止共享 Runner。
// Python: factory.py:63-80 optimizer_agent_resources——Go 侧返回句柄，
// 调用方 defer handle.Release()。
func OptimizerAgentResources(agent *harness.DeepAgent) (*OptimizerResourceHandle, error) {
	handle := &OptimizerResourceHandle{}
	// Python: operation = agent.deep_config.sys_operation; operation_id = operation.id
	if op := agent.DeepConfig(); op != nil && op.SysOperation != nil {
		if getter, ok := op.SysOperation.(interface{ GetID() string }); ok {
			handle.operationID = getter.GetID()
		}
	}
	return handle, nil
}

// Release 释放句柄持有的资源。
func (h *OptimizerResourceHandle) Release() error {
	// Python finally：cleanup_task_resources → ability_manager.teardown_tools →
	// Runner.resource_mgr.remove_sys_operation(operation_id)。Go 侧前两步由
	// Runner 统一管理（偏差 D-3：无独立 teardown 钩子），操作注销保留。
	if h.operationID != "" {
		if rm := runner.GetResourceMgr(); rm != nil {
			_ = rm.RemoveSysOperation([]string{h.operationID})
		}
	}
	return nil
}

// CreateMemberOptimizerAgent 为一个内部 Member Optimizer Agent 构建配置化 DeepAgent。
// Python: factory.py:134-176 create_member_optimizer_agent。
func CreateMemberOptimizerAgent(ctx context.Context, params MemberAgentParams) (*harness.DeepAgent, error) {
	profile := params.Profile
	model, err := memberoptimizer.LoadMemberOptimizerModel(params.ModelConfigRef)
	if err != nil {
		return nil, err
	}
	prompt, err := RenderPrompt(profile.PromptFile, params.PromptValues)
	if err != nil {
		return nil, err
	}
	rails := BuildRails(BuildRailsParams{
		Profile:               profile,
		AgentSkillsDirs:       params.AgentSkillsDirs,
		ExtraRails:            params.ExtraRails,
		EnabledSkillsOverride: params.EnabledSkillsOverride,
	})
	if err := os.MkdirAll(params.Workspace, 0o755); err != nil {
		return nil, err
	}
	// Python: enable_skill_discovery=profile.key not in {ACTION_EXECUTION.key, VERIFICATION_REPAIR.key}
	skillDiscovery := profile.Key != ActionExecution.Key && profile.Key != VerificationRepair.Key
	workspaceAbs, _ := filepath.Abs(params.Workspace)
	invocationLogDir := filepath.Join(filepath.Dir(workspaceAbs), "agent_logs", filepath.Base(workspaceAbs))
	return harness.CreateDeepAgent(ctx, hconfig.CreateDeepAgentParams{
		Model:                unwrapModel(model),
		Card:                 agentschema.NewAgentCard(agentschema.WithAgentName(profile.AgentName), agentschema.WithAgentDescription(profile.Description)),
		SystemPrompt:         prompt,
		ToolInstances:        params.Tools,
		Rails:                rails,
		Workspace:            workspace.NewWorkspace(params.Workspace, "en"),
		EnableTaskLoop:       false,
		EnableSkillDiscovery: skillDiscovery,
		MaxIterations:        profile.MaxIterations,
		InvocationLogDir:     invocationLogDir,
		Language:             "en",
		RestrictToWorkDir:    boolPtr(true),
		AutoCreateWorkspace:  false,
	})
}

// CreateRoleAttributionAgent 创建定责 agent。
// Python: factory.py:179-188。
func CreateRoleAttributionAgent(ctx context.Context, params MemberAgentParams) (*harness.DeepAgent, error) {
	params.Profile = RoleAttribution
	return CreateMemberOptimizerAgent(ctx, params)
}

// CreateMechanismAttributionAgent 创建病根归因 agent（注入三组枚举值）。
// Python: factory.py:191-208。
func CreateMechanismAttributionAgent(ctx context.Context, params MemberAgentParams) (*harness.DeepAgent, error) {
	params.Profile = MechanismAttribution
	params.PromptValues = map[string]string{
		"MECHANISM_TYPE_VALUES":       joinBulletValues(memberoptimizer.MechanismTypeValuesExcludingRail()),
		"FAILURE_SIGNATURE_VALUES":    joinBulletValues(memberoptimizer.FailureSignatureValuesExcludingConfigOrRailMismatch()),
		"OPTIMIZATION_SURFACE_VALUES": joinBulletValues(memberoptimizer.OptimizationSurfaceValuesExcludingRail()),
	}
	return CreateMemberOptimizerAgent(ctx, params)
}

// CreateActionPlanningAgent 创建开单 agent（挂证据护栏）。
// Python: factory.py:211-228（PlannerEvidenceRail 经 agents/rails/planner_evidence.py）。
func CreateActionPlanningAgent(ctx context.Context, params MemberAgentParams, evidenceRail agentinterfaces.AgentRail) (*harness.DeepAgent, error) {
	params.Profile = ActionPlanning
	params.PromptValues = map[string]string{}
	if evidenceRail == nil {
		evidenceRail = rails.NewPlannerEvidenceRail(ActionPlanning.MaxIterations)
	}
	params.ExtraRails = append(append([]agentinterfaces.AgentRail{}, params.ExtraRails...), evidenceRail)
	return CreateMemberOptimizerAgent(ctx, params)
}

// CreateActionExecutionAgent 创建动刀 agent（文件工具 + 沙箱操作）。
// Python: factory.py:231-249。
func CreateActionExecutionAgent(ctx context.Context, params MemberAgentParams, enableSkillCreator bool, declaredWritePaths []string) (*harness.DeepAgent, error) {
	params.Profile = ActionExecution
	tools, sysOperation, err := BuildFileToolsForWorkspace(params.Workspace, ActionExecution.AgentName, nil, declaredWritePaths, false)
	if err != nil {
		return nil, err
	}
	params.Tools = tools
	params.SysOperation = sysOperation
	// Python: enabled_skills_override=(ACTION_EXECUTION.enabled_skills if enable_skill_creator else ())
	if enableSkillCreator {
		params.EnabledSkillsOverride = ActionExecution.EnabledSkills
	} else {
		params.EnabledSkillsOverride = []string{}
	}
	return CreateMemberOptimizerAgent(ctx, params)
}

// CreateVerificationRepairAgent 创建验证补救 agent（repair 模式文件工具）。
// Python: factory.py:252-265。
func CreateVerificationRepairAgent(ctx context.Context, params MemberAgentParams) (*harness.DeepAgent, error) {
	params.Profile = VerificationRepair
	tools, sysOperation, err := BuildFileToolsForWorkspace(params.Workspace, VerificationRepair.AgentName, nil, nil, true)
	if err != nil {
		return nil, err
	}
	params.Tools = tools
	params.SysOperation = sysOperation
	return CreateMemberOptimizerAgent(ctx, params)
}

// PackageWriteAllowed 独立于模型声明检查包相对写入范围。
// Python: factory.py:390-410 package_write_allowed。
func PackageWriteAllowed(path string, declaredPaths []string, repair bool) bool {
	// D-c2-5：核心实现落父包（executor/planner 父包侧消费），本包装保持签名兼容。
	return memberoptimizer.PackageWriteAllowedCore(path, declaredPaths, repair)
}

// SupportedHostReadRoots 把实现契约以只读上下文暴露给包作者。
// Python: factory.py:412-421 supported_host_read_roots。
// Python 经 __file__ 反推包根（偏差 D-4）：Go 以模块仓库根为锚，
// 四类契约目录只读暴露的语义等价。
func SupportedHostReadRoots() []string {
	repoRoot := findRepoRoot()
	return []string{
		filepath.Join(repoRoot, "internal", "agentcore", "single_agent", "rail"),
		filepath.Join(repoRoot, "internal", "agentcore", "foundation", "tool"),
		filepath.Join(repoRoot, "internal", "agentcore", "harness", "rails"),
		filepath.Join(repoRoot, "internal", "agentcore", "harness", "docs"),
	}
}

// BuildFileToolsForWorkspace 为工作空间构建作用域文件工具与沙箱操作。
// Python: factory.py:337-387 _build_file_tools_for_workspace。
func BuildFileToolsForWorkspace(workspacePath, agentName string, sandboxRoots []string, declaredWritePaths []string, repair bool) ([]tool.Tool, sys_operation.SysOperation, error) {
	wsAbs, err := filepath.Abs(expandUserPath(workspacePath))
	if err != nil {
		return nil, nil, err
	}
	readRoots := append([]string{wsAbs}, SupportedHostReadRoots()...)
	if len(sandboxRoots) == 0 {
		sandboxRoots = readRoots
	}
	resolvedSandbox := make([]string, 0, len(sandboxRoots))
	for _, root := range sandboxRoots {
		abs, _ := filepath.Abs(expandUserPath(root))
		resolvedSandbox = append(resolvedSandbox, abs)
	}
	operationRoots := append([]string{}, resolvedSandbox...)
	// Python: rg_binary = resolve_rg_binary(); if rg_binary: operation_roots.append(...)
	if rg := resolveRgBinary(); rg != "" {
		if abs, absErr := filepath.Abs(rg); absErr == nil {
			operationRoots = append(operationRoots, abs)
		}
	}
	// Python: SysOperationCard(id=f"{agent_name}_{ws.name}_{os.urandom(4).hex()}",
	//         mode=LOCAL, work_config=LocalWorkConfig(sandbox_root=..., restrict_to_sandbox=True,
	//         shell_allowlist=[]))
	sysopID := fmt.Sprintf("%s_%s_%08x", agentName, filepath.Base(wsAbs), randomUint32())
	sysopCard := sys_operation.NewSysOperationCard(sys_operation.WithSysOpMode(sys_operation.OperationModeLocal))
	sysopCard.ID = sysopID
	sysopCard.WorkConfig = &sys_operation.LocalWorkConfig{
		SandboxRoot:       operationRoots,
		RestrictToSandbox: true,
		ShellAllowlist:    []string{},
	}
	// Python: result = Runner.resource_mgr.add_sys_operation(sysop_card)
	rm := runner.GetResourceMgr()
	if rm == nil {
		return nil, nil, fmt.Errorf("add action executor sys_operation failed: resource manager unavailable")
	}
	localSysOp := sys_operation.NewLocalSysOperation(sysopCard)
	if addErr := rm.AddSysOperation(sysopID, localSysOp); addErr != nil {
		return nil, nil, fmt.Errorf("add action executor sys_operation failed: %w", addErr)
	}
	// Python: sys_operation = Runner.resource_mgr.get_sys_operation(sysop_card.id)
	sysOperation := sys_operation.SysOperation(localSysOp)
	// Python: native_tools = [ReadFileTool, GlobTool, GrepTool, ListDirTool, WriteFileTool, EditFileTool]
	language := "en"
	nativeTools := []tool.Tool{
		filesystem.NewReadFileTool(sysOperation, language, sysopID, nil),
		filesystem.NewGlobTool(sysOperation, language, sysopID),
		filesystem.NewGrepTool(sysOperation, language, sysopID),
		filesystem.NewListDirTool(sysOperation, language, sysopID),
		filesystem.NewWriteFileTool(sysOperation, language, sysopID),
		filesystem.NewEditFileTool(sysOperation, language, sysopID),
	}
	// Python: [_ScopedPackageTool(tool, workspace_path, sandbox_roots, declared_write_paths or [], repair) ...]
	scoped := make([]tool.Tool, 0, len(nativeTools))
	for _, native := range nativeTools {
		scoped = append(scoped, NewScopedPackageTool(native, wsAbs, resolvedSandbox, declaredWritePaths, repair))
	}
	return scoped, sysOperation, nil
}

// FormatActionDefinitions 把动作定义渲染为提示词片段。
// Python: factory.py:500-511 _format_action_definitions（filter_action_definitions
// 属 action_groups，⤴️ 17.4c2 接线；c1 阶段以注入点承载）。
func FormatActionDefinitions(definitions []harnessschema.ActionDefinition) string {
	filtered := filterActionDefinitionsFn(definitions)
	if len(filtered) == 0 {
		return ""
	}
	lines := []string{"## Action Definitions"}
	for _, definition := range filtered {
		lines = append(lines, fmt.Sprintf("- %s/%s: %s (function=%s)",
			definition.Group, definition.Operation, definition.Purpose, definition.Function))
	}
	return strings.Join(lines, "\n")
}

// FilterActionDefinitionsFn 动作定义过滤注入点（Python 从 action_groups 导入；
// 17.4c2 落 action_groups 时以 SetFilterActionDefinitions 接线，避免 c1 反向依赖）。
var filterActionDefinitionsFn = func(definitions []harnessschema.ActionDefinition) []harnessschema.ActionDefinition {
	return definitions
}

// SetFilterActionDefinitions 接线动作定义过滤（17.4c2 调用）。
func SetFilterActionDefinitions(fn func([]harnessschema.ActionDefinition) []harnessschema.ActionDefinition) {
	filterActionDefinitionsFn = fn
}

// RenderPrompt 渲染提示词模板（占位符缺失报错）。
// Python: factory.py:307-317 _render_prompt。
func RenderPrompt(filename string, values map[string]string) (string, error) {
	prompt, err := LoadPrompt(filename)
	if err != nil {
		return "", err
	}
	missing := ""
	result := placeholderRE.ReplaceAllStringFunc(prompt, func(match string) string {
		key := placeholderRE.FindStringSubmatch(match)[1]
		value, ok := values[key]
		if !ok {
			missing = key
			return match
		}
		return value
	})
	if missing != "" {
		return "", fmt.Errorf("missing prompt placeholder value: %s", missing)
	}
	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// unwrapModel 从预算化包装取出底层模型。
func unwrapModel(model *memberoptimizer.BudgetedRsiModel) *llm.Model {
	if model == nil {
		return nil
	}
	return model.Model
}

// boolPtr 布尔指针。
func boolPtr(v bool) *bool { return &v }

// expandUserPath 复刻 Python Path.expanduser。
func expandUserPath(path string) string {
	if strings.HasPrefix(path, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			if path == "~" {
				return home
			}
			if strings.HasPrefix(path, "~/") {
				return filepath.Join(home, path[2:])
			}
		}
	}
	return path
}

// expandEnvVarsAny 递归展开 ${VAR}（Python factory.py:288-300 _expand_env_vars）。
func expandEnvVarsAny(value any) any {
	switch v := value.(type) {
	case string:
		return envVarRE.ReplaceAllStringFunc(v, func(match string) string {
			key := envVarRE.FindStringSubmatch(match)[1]
			if env, ok := os.LookupEnv(key); ok {
				return env
			}
			return match
		})
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, item := range v {
			out[k] = expandEnvVarsAny(item)
		}
		return out
	case []any:
		out := make([]any, len(v))
		for i, item := range v {
			out[i] = expandEnvVarsAny(item)
		}
		return out
	}
	return value
}

// joinBulletValues 枚举值转 "- value" 列表（Python "\n".join(f"- {value}" ...)）。
func joinBulletValues(values []string) string {
	lines := make([]string, len(values))
	for i, v := range values {
		lines[i] = "- " + v
	}
	return strings.Join(lines, "\n")
}

// splitPathParts 复刻 Path.parts（以 / 切分的非空段）。
func splitPathParts(path string) []string {
	parts := strings.Split(strings.Trim(filepath.ToSlash(path), "/"), "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// containsDotDot 路径段含 ".."。
func containsDotDot(parts []string) bool {
	for _, part := range parts {
		if part == ".." {
			return true
		}
	}
	return false
}

// isUnderPath 判断 candidate 是否在 declared 之下。
func isUnderPath(candidate, declared string) bool {
	rel, err := filepath.Rel(declared, candidate)
	if err != nil {
		return false
	}
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// findRepoRoot 向上查找 go.mod 定位仓库根（偏差 D-4 锚点）。
func findRepoRoot() string {
	dir, _ := os.Getwd()
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "."
		}
		dir = parent
	}
}

// resolveRgBinary 定位 ripgrep 可执行文件（Python rg_binary.resolve_rg_binary 等价）。
func resolveRgBinary() string {
	if path, err := exec.LookPath("rg"); err == nil {
		return path
	}
	return ""
}

// randomUint32 随机 32 位（Python os.urandom(4).hex() 等价）。
func randomUint32() uint32 {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// 编译期锚定 harnessrails / harnessrsi / model_clients 依赖。
var (
	_ = harnessrails.NewSysOperationRail
	_ = harnessrsi.DefaultOutputRepairConfig
	_ = model_clients.NewInvokeParams
)

// CreatePlannerAgentAdapter 创建开单 agent 并返回证据护栏持有者句柄。
// Python: factory.py:211-228 create_action_planning_agent + planner agent 内部
// evidence_rail 创建（action_planner.py:765）；Go 经桥接后以 handle 承载 rail
// 能力面（D-c2-4 桥接时序等价）。
func CreatePlannerAgentAdapter(ctx context.Context, params memberoptimizer.PlannerAgentParams) (*agentface.Agent, error) {
	// Python: HarnessStructureRail 构造/update_harness_summaries 等价——构造时注入
	structureRail := rails.NewHarnessStructureRail(params.HarnessSummaries)
	holder := rails.NewPlannerEvidenceRail(ActionPlanning.MaxIterations)
	evidenceRail := holder
	agent, err := CreateActionPlanningAgent(ctx, MemberAgentParams{
		ModelConfigRef:  params.ModelConfigRef,
		Workspace:       params.Workspace,
		AgentSkillsDirs: params.AgentSkillsDirs,
		ExtraRails:      []agentinterfaces.AgentRail{structureRail},
	}, evidenceRail)
	if err != nil {
		return nil, err
	}
	release, releaseErr := optimizerReleaseFn(agent)
	if releaseErr != nil {
		return nil, releaseErr
	}
	return &agentface.Agent{Deep: agent, Finalizer: holder, Release: release}, nil
}
