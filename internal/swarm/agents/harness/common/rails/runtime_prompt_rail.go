package rails

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	pathutil "github.com/uapclaw/uapclaw-go/internal/common/utils/path"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	agentteamsmodels "github.com/uapclaw/uapclaw-go/internal/openuapclaw/agent_teams/models"
	llm "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm"
	intellirouter "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/model_clients/intellirouter"
	llmschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/foundation/llm/schema"
	cb "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/runner/callback"
	sessioninterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/session/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/agents"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/prompts"
	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/rails"
	hschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/harness/schema"
	promptpkg "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
	swarmcommon "github.com/uapclaw/uapclaw-go/internal/swarm/common"
)

// RuntimePromptRail — 组装稳定与动态运行时提示词状态。
// 稳定环境规则与会话起始 git 快照留在系统提示词；
// 动态运行时状态以 prompt attachment 管理。
// 请求日期时间留在真实用户消息的 JSON 信封中。
//
// Python: RuntimePromptRail — runtime_prompt_rail.py（1180 行，2026-12 对齐）。
// 稳定前缀节：env / directory_boundaries / git_status；
// 动态 attachment 节：runtime.setting / a4p.intent_authorization / a4p.cron_authorization_scope。
// 旧版 time/language_output/browser_tool_policy/trusted_dirs_policy/model_answer_policy
// 注入已随上游废弃（新版仅在 remove 列表中清除残留）。

// ──────────────────────────── 常量 ────────────────────────────

// routedModelPlaceholder 模型组逻辑模型的占位文本。
// 安装的 openjiuwen 不填充时为空，prompt 绝不向模型展示未填充的占位符。
// Python: _routed_model_placeholder()（文件头 :50-59）
func routedModelPlaceholder() string {
	return intellirouter.RoutedModelPlaceholder
}

const (
	// runtimePromptRailPriority RuntimePromptRail 优先级
	// Python: RuntimePromptRail.priority = 5
	runtimePromptRailPriority = 5

	// sectionEnvPriority env section 优先级
	// Python: SystemPromptPriority.ENV = 86
	sectionEnvPriority = 86
	// sectionDirectoryBoundariesPriority directory_boundaries section 优先级
	// Python: SystemPromptPriority.DIRECTORY_BOUNDARIES = 87
	sectionDirectoryBoundariesPriority = 87
	// sectionGitStatusPriority git_status section 优先级
	// Python: SystemPromptPriority.GIT_STATUS = 91
	sectionGitStatusPriority = 91

	// a4pPromptVersion A4P 提示词版本
	// Python: A4P_PROMPT_VERSION = "2026-10-19.1"
	a4pPromptVersion = "2026-10-19.1"

	// modelGroupLogicalModel Team 模型组池给每个成员的模型名；
	// 路由器在每次调用内挑选真实模型（openjiuwen INTELLI_ROUTER_UNIFIED_MODEL）。
	// Python: _MODEL_GROUP_LOGICAL_MODEL = "*"
	modelGroupLogicalModel = "*"

	// runtimeSettingAttachmentPriority runtime.setting attachment 优先级
	// Python: priority=95（_refresh_dynamic_attachments）
	runtimeSettingAttachmentPriority = 95

	// a4pAttachmentPriority a4p attachment 优先级
	// Python: priority=94
	a4pAttachmentPriority = 94

	// gitCommandTimeout git 命令超时时间
	// Python: _run_git → subprocess.run(..., timeout=5)
	gitCommandTimeout = 5 * time.Second

	// runtimePromptSource attachment source 标识
	// Python: "jiuwenswarm.runtime_prompt_rail"
	runtimePromptSource = "jiuwenswarm.runtime_prompt_rail"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// modeDisplayMap 模式显示名称映射。
// Python: _MODE_DISPLAY_MAP (interface_deep.py L427-431)
var modeDisplayMap = map[string]map[string]string{
	"agent.work.plan":   {"cn": "规划模式", "en": "Planning Mode"},
	"agent.work.normal": {"cn": "性能模式", "en": "Performance Mode"},
	"team":              {"cn": "集群模式", "en": "Cluster Mode"},
}

// runtimeStateSessionSanitizer session id 清洗规则（非字母数字下划线点横线 → "_"）。
// Python: re.sub(r"[^A-Za-z0-9_.-]", "_", ...)（utils.py get_runtime_state_path）
var runtimeStateSessionSanitizer = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// _ 编译时验证 RuntimePromptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*RuntimePromptRail)(nil)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimePromptRail 运行时提示词护栏。
type RuntimePromptRail struct {
	rails.DeepAgentRail
	// mu 保护下列可变字段。rail 实例跨会话共享，Set* 与 BeforeModelCall 会并发访问。
	mu sync.RWMutex
	// agent init 时捕获的 agent 引用（Python: self._agent）
	agent agentinterfaces.BaseAgent
	// builder 系统提示词构建器引用（Python: self.system_prompt_builder）
	builder saprompt.SystemPromptBuilderInterface
	// attachmentManager 动态提示词管理器（Python: self.attachment_manager）
	attachmentManager *prompts.Manager
	// language 每次请求的语言
	language string
	// channel 每次请求的频道
	channel string
	// a4pAuthorizerAvailable 本请求是否有交互式 Web 授权路由
	a4pAuthorizerAvailable bool
	// trustedDirs 每次请求的可信目录
	trustedDirs []string
	// cwd 每次请求的当前工作目录
	cwd string
	// projectDir 每次请求的项目目录
	projectDir string
	// workspaceDir 本 agent 自身工作区（产物/记忆/技能视图；团队成员各自拥有）
	workspaceDir string
	// taskWorkspaceRoot 自动分配的 projectless 任务工作区根
	taskWorkspaceRoot string
	// taskWorkDir 临时/中间文件目录
	taskWorkDir string
	// taskOutputsDir 最终产物目录
	taskOutputsDir string
	// executionCwd 执行路径：任务内 cwd（Python: self._execution_cwd）
	executionCwd string
	// executionProjectRoot 执行路径：项目根（Python: self._execution_project_root）
	executionProjectRoot string
	// executionWorkspace 执行路径：工作区（Python: self._execution_workspace）
	executionWorkspace string
	// executionPathsRevision 执行路径变更版本号（Python: self._execution_paths_revision）
	executionPathsRevision int
	// boundExecutionPathsRevision 已绑定到执行上下文的版本号
	// Python: ContextVar _bound_execution_paths_revision（default -1）。
	// 偏差：Go 无 ContextVar，以 rail 字段承载；rebind 产生 cwd.InitCwd 结果，
	// 经 ExecutionCwdState 暴露供 runtime 轮次 ctx 写入点消费（⤴️runtime 轮次链路）。
	boundExecutionPathsRevision int
	// executionCwdState 最近一次 rebind 构造的 CwdState（偏差缝载体）
	executionCwdState *cwd.CwdState
	// modelName 每次请求的模型名称（YAML 读取失败时的 fallback）
	modelName string
	// mode 每次请求的运行模式（YAML 读取失败时的 fallback）
	mode string
	// forceEnglish 强制英文 section（code 模式）
	forceEnglish bool
	// sessionID per-request session id（读取按 session 隔离的 runtime_state 文件）
	sessionID string
	// requestMetadata 每次请求的元数据（cron 标识等；Python: self._request_metadata）
	requestMetadata map[string]any
	// modelGroupID 记住的模型组 id（Python: self._model_group_id）
	modelGroupID string
	// modelGroupNames 模型组 id → 显示名缓存（Python: self._model_group_names）
	modelGroupNames map[string]string
}

// runtimePromptSnapshot BeforeModelCall 入口读取的字段快照。
type runtimePromptSnapshot struct {
	language           string
	channel            string
	trustedDirs        []string
	cwd                string
	projectDir         string
	workspaceDir       string
	taskWorkspaceRoot  string
	taskWorkDir        string
	taskOutputsDir     string
	modelName          string
	mode               string
	forceEnglish       bool
	sessionID          string
	requestMetadata    map[string]any
	a4pAuthorizerAvail bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimePromptRail 创建 RuntimePromptRail 实例。
// Python: RuntimePromptRail.__init__(language="cn", channel="web", timezone_offset=8)
func NewRuntimePromptRail(language, channel string) *RuntimePromptRail {
	r := &RuntimePromptRail{
		language:                    language,
		channel:                     channel,
		boundExecutionPathsRevision: -1,
		modelGroupNames:             map[string]string{},
	}
	r.WithPriority(runtimePromptRailPriority)
	return r
}

// SetLanguage 每次请求更新语言。
// Python: RuntimePromptRail.set_language(language)
func (r *RuntimePromptRail) SetLanguage(language string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.language = language
}

// SetChannel 每次请求更新频道。
// Python: RuntimePromptRail.set_channel(channel)
func (r *RuntimePromptRail) SetChannel(channel string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.channel = channel
}

// SetA4pAuthorizerAvailable 记录本请求是否有交互式 Web 授权路由。
// Python: RuntimePromptRail.set_a4p_authorizer_available(available)
func (r *RuntimePromptRail) SetA4pAuthorizerAvailable(available bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.a4pAuthorizerAvailable = available
}

// SetTrustedDirs 每次请求更新可信目录。
// Python: RuntimePromptRail.set_trusted_dirs(trusted_dirs)
func (r *RuntimePromptRail) SetTrustedDirs(dirs []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.trustedDirs = dirs
}

// SetRuntimePaths 每次请求更新项目身份、任务路径、cwd 与自身工作区。
//
// Python: RuntimePromptRail.set_runtime_paths (runtime_prompt_rail.py:153-201)
func (r *RuntimePromptRail) SetRuntimePaths(cwdArg, projectDir, workspaceDir, taskWorkspaceRoot, taskWorkDir, taskOutputsDir string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cwd = trimOrEmpty(cwdArg)
	r.projectDir = trimOrEmpty(projectDir)
	r.workspaceDir = trimOrEmpty(workspaceDir)
	r.taskWorkspaceRoot = trimOrEmpty(taskWorkspaceRoot)
	r.taskWorkDir = trimOrEmpty(taskWorkDir)
	r.taskOutputsDir = trimOrEmpty(taskOutputsDir)
}

// SetModelName 每次请求更新模型名称，作为文件读取失败时的兜底。
// Python: RuntimePromptRail.set_model_name(model_name)
func (r *RuntimePromptRail) SetModelName(modelName string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.modelName = modelName
}

// SetExecutionPaths 在执行当前轮次的任务内绑定 Agent/Code 路径。
//
// Python: RuntimePromptRail.set_execution_paths (runtime_prompt_rail.py:207-229)
func (r *RuntimePromptRail) SetExecutionPaths(cwdArg, projectRoot, workspaceArg string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if cwdArg == r.executionCwd && projectRoot == r.executionProjectRoot && workspaceArg == r.executionWorkspace {
		return
	}
	r.executionCwd = cwdArg
	r.executionProjectRoot = projectRoot
	r.executionWorkspace = workspaceArg
	// 会话通常保持一个固定的操作目录。仅在这些路径真实变化时递增，
	// 使同一会话内的重复请求不会覆盖运行时工具（如 Code worktree）选定的 cwd。
	r.executionPathsRevision++
}

// SetMode 每次请求更新运行模式，作为文件读取失败时的兜底。
// Python: RuntimePromptRail.set_mode(mode)
func (r *RuntimePromptRail) SetMode(mode string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.mode = mode
}

// SetSessionID 每次请求更新 session id，用于读取按 session 隔离的 runtime_state 文件。
// Python: RuntimePromptRail.set_session_id (runtime_prompt_rail.py:235-241)
func (r *RuntimePromptRail) SetSessionID(sessionID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sessionID = trimOrEmpty(sessionID)
}

// SetRequestMetadata 每次请求更新请求元数据。
// Python: RuntimePromptRail.set_request_metadata (runtime_prompt_rail.py:243-245)
func (r *RuntimePromptRail) SetRequestMetadata(metadata map[string]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.requestMetadata = metadata
}

// SetForceEnglish 强制英文 section。
// Python: RuntimePromptRail.set_force_english(force)
func (r *RuntimePromptRail) SetForceEnglish(force bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.forceEnglish = force
}

// Init 从 Agent 获取 system_prompt_builder 与 prompt_attachment_manager 引用。
// Python: RuntimePromptRail.init(agent) (runtime_prompt_rail.py:110-114)
func (r *RuntimePromptRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	if agent == nil {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agent = agent
	r.builder = agent.SystemPromptBuilder()
	if deepAgent, ok := agent.(interface {
		PromptAttachments() *prompts.Manager
	}); ok {
		r.attachmentManager = deepAgent.PromptAttachments()
	}
	return nil
}

// Uninit 清理注入的 section 并释放引用与任务路径字段。
// Python: RuntimePromptRail.uninit(agent) (runtime_prompt_rail.py:116-136)
func (r *RuntimePromptRail) Uninit(_ agentinterfaces.BaseAgent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.builder != nil {
		r.builder.RemoveSection("time")
		r.builder.RemoveSection("runtime.model_answer_policy")
		r.builder.RemoveSection("language_output")
		r.builder.RemoveSection("env")
		r.builder.RemoveSection("directory_boundaries")
		r.builder.RemoveSection("tui_current_project_policy")
		r.builder.RemoveSection("trusted_dirs_policy")
		r.builder.RemoveSection("git_status")
	}
	r.agent = nil
	r.builder = nil
	r.attachmentManager = nil
	r.taskWorkspaceRoot = ""
	r.taskWorkDir = ""
	r.taskOutputsDir = ""
	r.executionCwd = ""
	r.executionProjectRoot = ""
	r.executionWorkspace = ""
	return nil
}

// BeforeInvoke 首次模型调用前准备会话起始上下文。
// Python: RuntimePromptRail.before_invoke (runtime_prompt_rail.py:460-464)
func (r *RuntimePromptRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	r.bindExecutionPaths()
	runtimeState := r.refreshDynamicAttachments(ctx, cbc)
	r.syncGitSystemContext(cbc, runtimeState)
	return nil
}

// BeforeModelCall 模型调用前刷新动态状态并注入稳定前缀节。
// Python: RuntimePromptRail.before_model_call (runtime_prompt_rail.py:466-770)
func (r *RuntimePromptRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 长寿命 Agent/Code 交互在请求到达前创建的监督任务中执行各轮。
	// 在该任务内重新绑定，使后续派生的 Bash/文件工具任务继承请求 cwd，
	// 而不是启动时捕获的 Agent 内部数据工作区。
	if r.usesRoundExecutionPaths() {
		r.bindExecutionPaths()
	}
	runtimeState := r.refreshDynamicAttachments(ctx, cbc)
	builder := r.builderOf(cbc)
	if builder == nil {
		return nil
	}

	// Python: remove_section 列表（:481-489）
	for _, name := range []string{
		"time",
		"runtime.model_answer_policy",
		"language_output",
		"env",
		"tui_current_project_policy",
		"trusted_dirs_policy"} {
		builder.RemoveSection(name)
	}

	// ── 平台、Shell、编码、时间查询与渠道规则 ──
	snap := r.snapshot()
	channel := FirstNonEmpty(runtimeStateString(runtimeState, "channel"), snap.channel, "unknown")

	osType := runtime.GOOS
	shellCapabilities := promptpkg.GetShellCapabilities(ctx, osType)
	shellPath := os.Getenv("SHELL")
	if strings.HasPrefix(osType, "windows") {
		// Windows 通常没有 SHELL 变量。优先使用实际 PowerShell 可执行文件，
		// 使 prompt 不上报 unknown。
		shellPath = shellCapabilities.PowerShell
	}
	shellName := "unknown"
	if shellPath != "" {
		shellName = filepath.Base(shellPath)
	}
	osVersion := getOSVersion()
	envLanguage := "en"
	if !snap.forceEnglish && snap.language == "cn" {
		envLanguage = "cn"
	}
	shellEnvPrompt := promptpkg.BuildShellEnvironmentPrompt(envLanguage, osType, shellCapabilities)

	var envContent string
	if !snap.forceEnglish && snap.language == "cn" {
		envContent = "# 运行环境\n\n" +
			"## 平台与 Shell\n\n" +
			fmt.Sprintf("- 当前运行平台：`%s`\n", osType) +
			fmt.Sprintf("- OS 版本：%s\n", osVersion) +
			fmt.Sprintf("- Shell：%s\n\n", shellName) +
			shellEnvPrompt + "\n\n" +
			"## 编码兼容性\n\n" +
			"- 代码将在 GBK 控制台或仅支持 GBK 的工具中运行时，避免直接使用 GBK 无法编码的 Emoji 和特殊字符。\n" +
			"- 必须使用这些字符时，选择明确支持 UTF-8 的执行工具或显式配置 UTF-8 编码。\n\n" +
			"## 时间相关查询\n\n" +
			"- 用户询问“最新、当前、今年、实时、近期”等信息并需要搜索时，搜索 query 应优先包含当前年份或日期。\n\n" +
			"## 当前渠道\n\n" +
			fmt.Sprintf("- 当前渠道：`%s`", channel)
	} else {
		envContent = "# Runtime Environment\n\n" +
			"## Platform and Shell\n\n" +
			fmt.Sprintf("- Current platform: `%s`\n", osType) +
			fmt.Sprintf("- OS version: %s\n", osVersion) +
			fmt.Sprintf("- Shell: %s\n\n", shellName) +
			shellEnvPrompt + "\n\n" +
			"## Encoding Compatibility\n\n" +
			"- When code will run in a GBK console or a tool that supports only GBK, avoid Emoji and " +
			"special characters that GBK cannot encode.\n" +
			"- If those characters are required, use a tool that explicitly supports UTF-8 or " +
			"configure UTF-8 encoding.\n\n" +
			"## Time-sensitive Queries\n\n" +
			"- When the user asks for the latest, current, this year's, real-time, or recent information " +
			"and search is needed, prefer including the current year or date in the query.\n\n" +
			"## Current Channel\n\n" +
			fmt.Sprintf("- Current channel: `%s`", channel)
	}

	builder.AddSection(saprompt.PromptSection{
		Name:     "env",
		Content:  map[string]string{"cn": envContent, "en": envContent},
		Priority: sectionEnvPriority,
	})

	// ── 渠道：目录与文件操作边界 ──
	// 先移除合并节与遗留节，使切换到无本地目录指引的渠道时能清除。
	builder.RemoveSection("directory_boundaries")
	builder.RemoveSection("tui_current_project_policy")
	builder.RemoveSection("trusted_dirs_policy")
	if snap.channel == "tui" || snap.channel == "web" || snap.channel == "ws_client" || snap.channel == "process_cli" {
		content := r.buildDirectoryBoundaries(snap)
		builder.AddSection(saprompt.PromptSection{
			Name:     "directory_boundaries",
			Content:  map[string]string{"cn": content, "en": content},
			Priority: sectionDirectoryBoundariesPriority,
		})
	}

	// ── A4P 授权 attachment（基座 ⤴️21.7，桩降级为清除语义） ──
	r.syncA4pIntentAuthorizationAttachment(ctx, cbc)
	r.syncA4pCronAuthorizationScopeAttachment(ctx, cbc)

	return nil
}

// GetCallbacks 覆写基类回调映射，注册 BeforeInvoke 与 BeforeModelCall。
func (r *RuntimePromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ExecutionCwdState 返回最近一次执行路径绑定产生的 CwdState（偏差缝，见字段注释）。
func (r *RuntimePromptRail) ExecutionCwdState() *cwd.CwdState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.executionCwdState
}

// RuntimeStatePath 返回按 session 隔离的 runtime_state 文件路径。
// Python: get_runtime_state_path(session_id)（utils.py:1758-1766）——
// 每个 session 独占一份文件，避免心跳/定时/并发 session 共用单文件互相覆盖；
// session_id 为空回退 "default"。
func RuntimeStatePath(sessionID string) string {
	sid := runtimeStateSessionSanitizer.ReplaceAllString(strings.TrimSpace(sessionID), "_")
	if len(sid) > 128 {
		sid = sid[:128]
	}
	if sid == "" {
		sid = "default"
	}
	return filepath.Join(workspace.ConfigDir(), "runtime_state", sid+".yaml")
}

// WriteRuntimeStateYAML 将运行时状态写入按 session 隔离的 runtime_state 文件。
// Python: _write_runtime_state() (interface_deep.py L756-821)
func WriteRuntimeStateYAML(ctx context.Context, sessionID, modelName, mode, language, channel, agentName, projectDir string) {
	gitBranch := "N/A"
	gitMainBranch := ""
	gitStatus := ""
	gitRecentCommits := ""
	gitUser := ""

	// 对齐 Python: git_bin = which("git")
	gitBin, err := exec.LookPath("git")
	if err == nil && gitBin != "" && projectDir != "" {
		info, statErr := os.Stat(projectDir)
		if statErr == nil && info.IsDir() {
			// 对齐 Python: 先检查 --is-inside-work-tree，再获取 branch
			insideWorkTree := runGit(ctx, projectDir, "rev-parse", "--is-inside-work-tree")
			if insideWorkTree == "true" {
				gitBranch = runGit(ctx, projectDir, "rev-parse", "--abbrev-ref", "HEAD")
				if gitBranch == "" {
					gitBranch = "HEAD"
				}
				gitStatus = runGit(ctx, projectDir, "status", "--short")
				if len(gitStatus) > 0 {
					lines := strings.Split(gitStatus, "\n")
					if len(lines) > 50 {
						lines = lines[:50]
					}
					gitStatus = strings.Join(lines, "\n")
				}
				gitRecentCommits = runGit(ctx, projectDir, "log", "--oneline", "-5")
				gitUser = runGit(ctx, projectDir, "config", "user.name")
				for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
					if runGit(ctx, projectDir, "rev-parse", "--verify", "--quiet", candidate) != "" {
						gitMainBranch = candidate
						break
					}
				}
			}
		}
	}

	// 模式显示名映射
	// Python: mode_display = _MODE_DISPLAY_MAP.get(mode, {}).get(language, mode)
	modeDisplay := mode
	if langMap, ok := modeDisplayMap[mode]; ok {
		if display, ok2 := langMap[language]; ok2 {
			modeDisplay = display
		}
	}

	state := map[string]any{
		"model":              modelName,
		"mode":               modeDisplay,
		"language":           language,
		"channel":            channel,
		"agent":              agentName,
		"platform":           fmt.Sprintf("%s %s", capitalizeOS(runtime.GOOS), runtime.GOARCH),
		"go_version":         runtime.Version(),
		"git_branch":         gitBranch,
		"git_main_branch":    gitMainBranch,
		"git_status":         gitStatus,
		"git_recent_commits": gitRecentCommits,
		"git_user":           gitUser,
	}

	yamlData, err := yaml.Marshal(state)
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("Failed to serialize runtime_state.yaml")
		return
	}

	yamlPath := RuntimeStatePath(sessionID)
	if err := os.MkdirAll(filepath.Dir(yamlPath), 0o755); err != nil {
		logger.Debug(logComponent).Err(err).Msg("Failed to create runtime_state dir")
		return
	}
	if err := os.WriteFile(yamlPath, yamlData, 0o644); err != nil {
		logger.Debug(logComponent).Err(err).Msg("Failed to write runtime_state.yaml")
	}
}

// FirstNonEmpty 返回第一个非空字符串，都为空则返回最后的 fallback。
func FirstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// bindExecutionPaths 在执行当前轮次的任务内绑定请求路径。
//
// Python: RuntimePromptRail._bind_execution_paths (runtime_prompt_rail.py:251-269)
// 偏差：Python init_cwd 设置任务 ContextVar；Go 以 cwd.InitCwd 构造状态存入
// executionCwdState（ExecutionCwdState 缝），跨任务 ctx 写入点 ⤴️runtime 轮次链路。
func (r *RuntimePromptRail) bindExecutionPaths() {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.executionCwd == "" || r.executionProjectRoot == "" || r.executionWorkspace == "" {
		return
	}
	if r.boundExecutionPathsRevision == r.executionPathsRevision {
		return
	}
	r.executionCwdState = cwd.InitCwd(r.executionCwd,
		cwd.WithProjectRoot(r.executionProjectRoot),
		cwd.WithWorkspace(r.executionWorkspace))
	r.boundExecutionPathsRevision = r.executionPathsRevision
}

// usesRoundExecutionPaths 返回 Agent/Code 轮次是否需要请求路径重绑。
//
// Python: RuntimePromptRail._uses_round_execution_paths (runtime_prompt_rail.py:271-275)
func (r *RuntimePromptRail) usesRoundExecutionPaths() bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	modeRoot := strings.ToLower(strings.TrimSpace(r.mode))
	if idx := strings.Index(modeRoot, "."); idx >= 0 {
		modeRoot = modeRoot[:idx]
	}
	return modeRoot == "agent" || modeRoot == "code"
}

// refreshDynamicAttachments 刷新 runtime 与 git 节，不触碰系统前缀。
//
// Python: RuntimePromptRail._refresh_dynamic_attachments (runtime_prompt_rail.py:772-874)
func (r *RuntimePromptRail) refreshDynamicAttachments(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) map[string]any {
	runtimeState := readRuntimeStateYAML(r.sessionID)

	var configuredModels []string
	rawAvailableModels, _ := runtimeState["available_models"].([]any)
	var availableModels []string
	if rawAvailableModels != nil {
		for _, item := range rawAvailableModels {
			if s, ok := item.(string); ok && strings.TrimSpace(s) != "" {
				availableModels = append(availableModels, strings.TrimSpace(s))
			}
		}
	}
	modelField, hasModel := runtimeState["model"].(string)
	if (len(availableModels) == 0) || (!hasModel || strings.TrimSpace(modelField) == "") {
		configuredModels = configuredModelNames()
	}
	if len(availableModels) == 0 {
		availableModels = configuredModels
	}
	// 会话级列表列出所有已配置模型，但模型组 agent 只能路由到其组内模型。
	groupModels := r.agentGroupModelNames(cbc)
	if len(groupModels) > 0 {
		availableModels = groupModels
	}
	fallbackModel := ""
	if len(configuredModels) > 0 {
		fallbackModel = configuredModels[0]
	}
	agentModel := r.agentModelName(cbc)
	groupLabel := r.modelGroupModelLabel(cbc)
	if agentModel == modelGroupLogicalModel || (agentModel == "" && groupLabel != "") {
		// 模型组 agent 跑在逻辑 "*" 模型上，路由器在每次调用内挑选真实模型。
		// 保留路由器填充的占位符，使每次调用都点名实际作答的模型。
		// 没有路由器支持时，命名为组。绝不离席回退到 session 模型——该 agent 不跑在其上。
		placeholder := routedModelPlaceholder()
		agentModel = FirstNonEmpty(placeholder, groupLabel, agentModel)
	}
	model := FirstNonEmpty(agentModel, modelField, r.snapshot().modelName, fallbackModel, "unknown")
	availableModelsStr := model
	if len(availableModels) > 0 {
		availableModelsStr = strings.Join(availableModels, ", ")
	}
	snap := r.snapshot()
	configuredMode := FirstNonEmpty(snap.mode, runtimeStateString(runtimeState, "mode"), "unknown")
	mode := r.resolveCurrentMode(cbc, configuredMode)
	languageVal := FirstNonEmpty(snap.language, runtimeStateString(runtimeState, "language"), "unknown")
	channel := FirstNonEmpty(runtimeStateString(runtimeState, "channel"), snap.channel, "unknown")

	var runtimeContent string
	if !snap.forceEnglish && snap.language == "cn" {
		runtimeContent = "# 运行时状态\n\n" +
			fmt.Sprintf("- 当前模型：%s\n", model) +
			fmt.Sprintf("- 可用模型：%s\n", availableModelsStr) +
			fmt.Sprintf("- 当前模式：%s\n", mode) +
			fmt.Sprintf("- 当前语言：%s\n", languageVal) +
			fmt.Sprintf("- 当前渠道：%s", channel)
	} else {
		runtimeContent = "# Runtime State\n\n" +
			fmt.Sprintf("- Current model: %s\n", model) +
			fmt.Sprintf("- Available models: %s\n", availableModelsStr) +
			fmt.Sprintf("- Current mode: %s\n", mode) +
			fmt.Sprintf("- Current language: %s\n", languageVal) +
			fmt.Sprintf("- Current channel: %s", channel)
	}
	r.upsertPromptAttachment(ctx, cbc, "runtime.setting", runtimeContent, prompts.AttachmentRuntime, runtimeSettingAttachmentPriority)
	r.syncA4pIntentAuthorizationAttachment(ctx, cbc)
	r.syncA4pCronAuthorizationScopeAttachment(ctx, cbc)

	return runtimeState
}

// syncA4pIntentAuthorizationAttachment 同步 A4P 意图授权 attachment。
//
// Python: _sync_a4p_intent_authorization_attachment (runtime_prompt_rail.py:905-1031)。
// 基座 is_a4p_preauthorization_active / get_a4p_supported_action_names /
// AUTHORIZATION_EXECUTION_CONTEXTS 属 A4P 预授权（21.7 ☐），Go 未实施 ⤴️21.7——
// 本批保留清除语义（非 web 渠道/未启用时清 section），注入分支待 21.7 回填。
func (r *RuntimePromptRail) syncA4pIntentAuthorizationAttachment(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	const section = "a4p.intent_authorization"
	r.clearPromptAttachment(cbc, section)
	// Python: 非 web 渠道或 authorizer 不可用 → 清 + 注入日志；启用时构造内容注入。
	// ⤴️21.7：注入分支依赖 a4p runtime，回填时按 Python :936-1031 一比一实现。
	_ = a4pPromptVersion
}

// syncA4pCronAuthorizationScopeAttachment 同步 A4P 定时任务授权范围 attachment。
//
// Python: _sync_a4p_cron_authorization_scope_attachment (runtime_prompt_rail.py:1033-1113)。
// ⤴️21.7：job_id 提取段保持 1:1（request_metadata.cron.job_id），注入段依赖
// a4p runtime，回填时实现。
func (r *RuntimePromptRail) syncA4pCronAuthorizationScopeAttachment(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) {
	const section = "a4p.cron_authorization_scope"
	r.mu.RLock()
	metadata := r.requestMetadata
	r.mu.RUnlock()
	cron, _ := metadata["cron"].(map[string]any)
	jobID := ""
	if cron != nil {
		if v, ok := cron["job_id"].(string); ok {
			jobID = strings.TrimSpace(v)
		}
		if jobID == "" {
			if v, ok := cron["jobId"].(string); ok {
				jobID = strings.TrimSpace(v)
			}
		}
	}
	if jobID == "" {
		r.clearPromptAttachment(cbc, section)
		return
	}
	// ⤴️21.7：注入段依赖 get_a4p_runtime().cron_intent_authorization_summary，
	// 回填时按 Python :1056-1113 实现。
	r.clearPromptAttachment(cbc, section)
}

// syncGitSystemContext 把会话起始 git 快照装入可缓存的系统前缀。
//
// Python: _sync_git_system_context (runtime_prompt_rail.py:1115-1150)
func (r *RuntimePromptRail) syncGitSystemContext(cbc *agentinterfaces.AgentCallbackContext, runtimeState map[string]any) {
	// 升级活 agent 时清除旧版逐模型调用 attachment。
	r.clearPromptAttachment(cbc, "git_status")
	builder := r.builderOf(cbc)
	if builder == nil {
		return
	}

	builder.RemoveSection("git_status")
	gitBranch := strings.TrimSpace(runtimeStateString(runtimeState, "git_branch"))
	if gitBranch == "" || gitBranch == "N/A" {
		return
	}
	gitMainBranch := strings.TrimSpace(runtimeStateString(runtimeState, "git_main_branch"))
	gitStatusText := strings.TrimSpace(runtimeStateString(runtimeState, "git_status"))
	gitRecentCommits := strings.TrimSpace(runtimeStateString(runtimeState, "git_recent_commits"))
	gitUser := strings.TrimSpace(runtimeStateString(runtimeState, "git_user"))
	gitLines := []string{
		"This is the git status at the start of the conversation. " +
			"Note that this status is a snapshot in time, and will not update during the conversation. " +
			"Run git yourself when you need the current state — for example before staging or " +
			"committing, or after anything may have changed the working tree.",
		fmt.Sprintf("Current branch: %s", gitBranch),
	}
	if gitMainBranch != "" {
		gitLines = append(gitLines,
			fmt.Sprintf("Main branch (you will usually use this for PRs): %s", gitMainBranch))
	}
	if gitUser != "" {
		gitLines = append(gitLines, fmt.Sprintf("Git user: %s", gitUser))
	}
	statusText := gitStatusText
	if statusText == "" {
		statusText = "(clean)"
	}
	gitLines = append(gitLines, fmt.Sprintf("Status:\n%s", statusText))
	commitsText := gitRecentCommits
	if commitsText == "" {
		commitsText = "(none)"
	}
	gitLines = append(gitLines, fmt.Sprintf("Recent commits:\n%s", commitsText))
	gitContent := strings.Join(gitLines, "\n\n")
	builder.AddSection(saprompt.PromptSection{
		Name:     "git_status",
		Content:  map[string]string{"cn": gitContent, "en": gitContent},
		Priority: sectionGitStatusPriority,
	})
}

// buildDirectoryBoundaries 构造「目录与文件操作边界」节内容。
//
// Python: before_model_call 的 directory_boundaries 段 (runtime_prompt_rail.py:548-770)
func (r *RuntimePromptRail) buildDirectoryBoundaries(snap runtimePromptSnapshot) string {
	installedWorkspaceDir := workspace.AgentWorkspaceDir()
	userWorkspaceDir := workspace.WorkspaceDir()
	// 本 agent 自身工作区。团队成员各有一个；无它时（单 agent 运行）
	// 进程级 agent 工作区本来就是同一目录。
	agentWorkspaceDir := existingDir(snap.workspaceDir)
	if agentWorkspaceDir == "" {
		agentWorkspaceDir = installedWorkspaceDir
	}
	installedSkillsDir := filepath.Join(installedWorkspaceDir, "skills", "{skill_name}")
	installedAgentTemplatesDir := filepath.Join(installedWorkspaceDir, "plugins", "agent_templates", "local")
	installedAgentGroupsDir := filepath.Join(userWorkspaceDir, ".agent_teams", "agent_groups", "local")
	installedPluginPackagesDir := filepath.Join(installedWorkspaceDir, "plugins", "plugin_packages", "local")
	installedMcpHubDir := filepath.Join(installedWorkspaceDir, "mcp", "mcp_hub")
	configDir := workspace.ConfigDir()
	projectDir := existingDir(snap.projectDir)
	taskWorkspaceRoot := existingDir(snap.taskWorkspaceRoot)
	taskWorkDir := existingDir(snap.taskWorkDir)
	taskOutputsDir := existingDir(snap.taskOutputsDir)
	runtimeCwd := existingDir(snap.cwd)
	if runtimeCwd == "" {
		runtimeCwd = FirstNonEmpty(taskWorkDir, projectDir, agentWorkspaceDir)
	}
	hasProjectlessTask := projectDir == "" &&
		taskWorkspaceRoot != "" && taskWorkDir != "" && taskOutputsDir != ""
	hasProject := projectDir != ""
	promptProjectDir := FirstNonEmpty(projectDir, runtimeCwd)
	hasDistinctCwd := hasProject && !samePath(projectDir, runtimeCwd)

	cn := !snap.forceEnglish && snap.language == "cn"
	var directoryContent string
	switch {
	case hasProjectlessTask:
		if cn {
			directoryContent = "# 目录与文件操作边界\n\n" +
				"## 当前任务目录\n\n" +
				fmt.Sprintf("- 当前任务根目录：`%s`\n", taskWorkspaceRoot) +
				fmt.Sprintf("- 临时工作目录：`%s`\n", taskWorkDir) +
				fmt.Sprintf("- 最终产物目录：`%s`\n\n", taskOutputsDir) +
				"- 相对路径以临时工作目录为基准。\n" +
				"- Bash 未显式传入 `workdir` 时，默认在临时工作目录执行。\n" +
				"- 中间文件、缓存和临时脚本放在临时工作目录。\n" +
				"- 报告、导出文件、图片、数据等最终产物放在最终产物目录。\n\n" +
				"## 通用目录规则\n\n"
		} else {
			directoryContent = "# Directory and File-Operation Boundaries\n\n" +
				"## Current Task Directories\n\n" +
				fmt.Sprintf("- Current task root: `%s`\n", taskWorkspaceRoot) +
				fmt.Sprintf("- Temporary working directory: `%s`\n", taskWorkDir) +
				fmt.Sprintf("- Final deliverables directory: `%s`\n\n", taskOutputsDir) +
				"- Resolve relative paths against the temporary working directory.\n" +
				"- When Bash is called without an explicit `workdir`, run it in the temporary working directory.\n" +
				"- Put intermediate files, caches, and temporary scripts in the temporary working directory.\n" +
				"- Put reports, exports, images, data, and other final deliverables in the final deliverables directory.\n\n" +
				"## General Directory Rules\n\n"
		}
	case !hasProject:
		if cn {
			directoryContent = "# 目录与文件操作边界\n\n" +
				"## 当前项目目录\n\n" +
				fmt.Sprintf("- 项目目录是当前运行时工作空间：`%s`\n\n", promptProjectDir) +
				"- 相对路径以项目目录为基准。\n" +
				"- Bash 未显式传入 `workdir` 时，默认在项目目录执行。\n" +
				"- 未指定保存位置时，任务产物放在项目目录内的合理位置。\n\n" +
				"## 通用目录规则\n\n"
		} else {
			directoryContent = "# Directory and File-Operation Boundaries\n\n" +
				"## Current Project Directory\n\n" +
				fmt.Sprintf("- The project directory is the current runtime workspace: `%s`\n\n", promptProjectDir) +
				"- Resolve relative paths against the project directory.\n" +
				"- When Bash is called without an explicit `workdir`, run it in the project directory.\n" +
				"- When no save location is specified, put task artifacts in an appropriate location under the project directory.\n\n" +
				"## General Directory Rules\n\n"
		}
	default:
		if cn {
			projectDescription := "- 项目目录是你当前的工作空间，"
			cwdDescription := "\n"
			separationRule := ""
			operationDirectory := "当前项目目录"
			if hasDistinctCwd {
				projectDescription = "- 项目目录是当前项目的根目录与项目上下文边界，"
				cwdDescription = fmt.Sprintf("- 当前工作目录（cwd、相对路径基准及 Bash 默认目录）是：`%s`\n\n", runtimeCwd)
				separationRule = "- 项目目录与当前工作目录是两个独立概念，不得互相替换。\n"
				operationDirectory = "当前工作目录"
			}
			directoryContent = "# 目录与文件操作边界\n\n" +
				"## 项目目录\n\n" +
				"### 项目目录说明\n\n" +
				projectDescription +
				fmt.Sprintf("当前项目目录是：`%s`\n", promptProjectDir) +
				cwdDescription +
				"### 项目目录规则\n\n" +
				separationRule +
				fmt.Sprintf("- 用户任务中的相对路径必须相对于%s路径去解析。\n", operationDirectory) +
				fmt.Sprintf("- Bash 未显式传入 `workdir` 时，默认在%s执行。\n", operationDirectory)
		} else {
			projectDescription := "- The project directory is your current workspace; "
			cwdDescription := "\n"
			separationRule := ""
			operationDirectory := "current project directory"
			if hasDistinctCwd {
				projectDescription = "- The project directory is the project root and project-context boundary; "
				cwdDescription = fmt.Sprintf("- The current working directory (cwd, relative-path base, and Bash default) is: `%s`\n\n", runtimeCwd)
				separationRule = "- The project directory and current working directory are independent concepts; do not substitute one for the other.\n"
				operationDirectory = "current working directory"
			}
			directoryContent = "# Directory and File-Operation Boundaries\n\n" +
				"## Project Directory\n\n" +
				"### Project Directory Description\n\n" +
				projectDescription +
				fmt.Sprintf("the current project directory is: `%s`\n", promptProjectDir) +
				cwdDescription +
				"### Project Directory Rules\n\n" +
				separationRule +
				fmt.Sprintf("- Resolve relative paths in user tasks against the %s.\n", operationDirectory) +
				fmt.Sprintf("- When Bash is called without an explicit `workdir`, run it in the %s.\n", operationDirectory)
		}
	}

	if cn {
		directoryContent += "- 用户已经提供明确路径时直接使用，不要重复询问。\n" +
			"- 只有任务确实需要操作某个项目、且现有上下文无法确定项目位置时，才询问项目路径。\n\n" +
			"## JiuwenSwarm 内部目录\n\n" +
			"### 核心内部数据\n\n" +
			fmt.Sprintf("- 智能体内部数据目录：`%s`\n", agentWorkspaceDir) +
			fmt.Sprintf("- JiuwenSwarm 启动配置目录：`%s`\n", configDir) +
			"- `IDENTITY.md`、`memory/`、`todo/` 和运行状态属于智能体内部数据。\n\n" +
			"### 已安装扩展资产\n\n" +
			fmt.Sprintf("- 已安装的 Skill 根目录：`%s`\n", installedSkillsDir) +
			fmt.Sprintf("- 已安装的专家模板存放在：`%s`\n", installedAgentTemplatesDir) +
			fmt.Sprintf("- 已安装的专家团存放在：`%s`\n", installedAgentGroupsDir) +
			fmt.Sprintf("- 已安装的插件包存放在：`%s`\n", installedPluginPackagesDir) +
			fmt.Sprintf("- 已安装的 MCP 存放在：`%s`\n\n", installedMcpHubDir) +
			"### 目录使用规则\n\n" +
			"- 不要把普通任务产物写入智能体内部目录或启动配置目录。\n" +
			"- 仅当用户明确要求创建、安装、更新或管理 JiuwenSwarm 内部资产时，才写入对应目录。\n" +
			"- 修改或删除已有内部资产前，确认目标及操作范围，避免影响其他智能体或会话。\n" +
			"- 用户任务中的 `config/`、`memory/`、`skills/`、`todo/`、`plugins/`、`mcp/` " +
			"或 `workspace/` 不自动映射到 JiuwenSwarm 内部目录。\n" +
			"- 目录中存在某项资产，不代表当前会话可以直接调用；实际可用性以当前技能、工具和 MCP 注册状态为准。"
	} else {
		directoryContent += "- When the user has provided an explicit path, use it directly without asking again.\n" +
			"- Ask for a project path only when the task truly requires a project and its location cannot be determined from the existing context.\n\n" +
			"## JiuwenSwarm Internal Directories\n\n" +
			"### Core Internal Data\n\n" +
			fmt.Sprintf("- Agent internal data directory: `%s`\n", agentWorkspaceDir) +
			fmt.Sprintf("- JiuwenSwarm startup configuration directory: `%s`\n", configDir) +
			"- `IDENTITY.md`, `memory/`, `todo/`, and runtime state are Agent internal data.\n\n" +
			"### Installed Extension Assets\n\n" +
			fmt.Sprintf("- Installed Skill root: `%s`\n", installedSkillsDir) +
			fmt.Sprintf("- Installed Agent templates: `%s`\n", installedAgentTemplatesDir) +
			fmt.Sprintf("- Installed Agent groups: `%s`\n", installedAgentGroupsDir) +
			fmt.Sprintf("- Installed plugin packages: `%s`\n", installedPluginPackagesDir) +
			fmt.Sprintf("- Installed MCPs: `%s`\n\n", installedMcpHubDir) +
			"### Directory Usage Rules\n\n" +
			"- Do not write ordinary task artifacts to the Agent internal data directory or startup configuration directory.\n" +
			"- Write to these directories only when the user explicitly asks to create, install, update, or manage JiuwenSwarm internal assets.\n" +
			"- Before modifying or deleting an existing internal asset, confirm the target and operation scope to avoid affecting other Agents or sessions.\n" +
			"- `config/`, `memory/`, `skills/`, `todo/`, `plugins/`, `mcp/`, or `workspace/` in a user task do not automatically refer to JiuwenSwarm internal directories.\n" +
			"- An asset's presence in one of these directories does not make it directly callable in the current session; actual availability follows the current Skill, tool, and MCP registrations."
	}
	return directoryContent
}

// upsertPromptAttachment 写入或覆盖动态提示词 attachment。
//
// Python: _upsert_prompt_attachment (runtime_prompt_rail.py:1152-1171)
func (r *RuntimePromptRail) upsertPromptAttachment(_ context.Context, cbc *agentinterfaces.AgentCallbackContext, section, content string, kind prompts.AttachmentKind, priority int) bool {
	if r.attachmentManager == nil {
		logger.Warn(logComponent).
			Str("section", section).
			Msg("[RuntimePromptRail] prompt attachment manager unavailable; skip dynamic section")
		return false
	}
	writer := r.attachmentManager.BindContext(cbc)
	_, err := writer.AddSection(section, content, kind, runtimePromptSource, prompts.WithAttachmentPriority(priority), prompts.WithAttachmentContentKind("text/markdown"))
	if err != nil {
		logger.Warn(logComponent).
			Str("section", section).
			Err(err).
			Msg("[RuntimePromptRail] skip prompt attachment")
		return false
	}
	return true
}

// clearPromptAttachment 清除动态提示词 attachment。
//
// Python: _clear_prompt_attachment (runtime_prompt_rail.py:1173-1180)
func (r *RuntimePromptRail) clearPromptAttachment(cbc *agentinterfaces.AgentCallbackContext, section string) {
	if r.attachmentManager == nil {
		return
	}
	if _, err := r.attachmentManager.BindContext(cbc).ClearSection(section); err != nil {
		logger.Warn(logComponent).
			Str("section", section).
			Err(err).
			Msg("[RuntimePromptRail] skip clearing prompt attachment")
	}
}

// resolveCurrentMode 用 DeepAgent session state 覆盖 code 模式的请求初始快照。
//
// Python: _resolve_current_mode (runtime_prompt_rail.py:417-458)
func (r *RuntimePromptRail) resolveCurrentMode(cbc *agentinterfaces.AgentCallbackContext, configuredMode string) string {
	switch configuredMode {
	case "code", "code.normal", "code.plan", "agent.code.normal", "agent.code.plan":
	default:
		return configuredMode
	}

	agent := r.agentOf(cbc)
	loader, canLoad := agent.(interface {
		LoadState(sessioninterfaces.SessionFacade) *hschema.DeepAgentState
	})
	if !canLoad || cbc == nil || cbc.Session() == nil {
		return configuredMode
	}

	state := loader.LoadState(cbc.Session())
	planMode := ""
	if state != nil && state.PlanMode.Mode != "" {
		planMode = state.PlanMode.Mode
	}

	normalized := strings.ToLower(strings.TrimSpace(planMode))
	if normalized == "plan" {
		return "code.plan"
	}
	if normalized == "normal" || normalized == "auto" {
		return "code.normal"
	}
	return configuredMode
}

// configuredModelNames 从 config.yaml 读取已配置模型名作为运行时兜底。
//
// Python: _configured_model_names (runtime_prompt_rail.py:304-317) +
// get_model_names（config.py:3646，优先读 models.defaults 列表）。
func configuredModelNames() []string {
	data, err := loadConfigRawForModelNames()
	if err != nil {
		logger.Debug(logComponent).Err(err).Msg("Failed to read configured model names")
		return []string{}
	}
	models, _ := data["models"].(map[string]any)
	defaultsList, _ := models["defaults"].([]any)
	names := []string{}
	for _, item := range defaultsList {
		entry, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := entry["model_name"].(string)
		name = strings.TrimSpace(name)
		if name != "" {
			names = append(names, name)
		}
	}
	return names
}

// agentModelOf 返回回调的活模型与承载它的 agent（偏差缝简化形）。
//
// Python: _agent_model (runtime_prompt_rail.py:319-344)。
// 偏差：Go ReActAgent/DeepAgent 经 SetLLM 注入模型，GetLLM 读取；
// model-group 内层 agent 探测段以 ReactAgent() 访问器承接。
func (r *RuntimePromptRail) agentModelOf(cbc *agentinterfaces.AgentCallbackContext) (llmModel, config any) {
	agent := r.agentOf(cbc)

	if getter, ok := agent.(interface {
		GetLLM() (*llm.Model, error)
	}); ok {
		if m, err := getter.GetLLM(); err == nil && m != nil {
			llmModel = m
		}
	}
	if llmModel == nil {
		if deepAgent, ok := agent.(interface {
			ReactAgent() *agents.ReActAgent
		}); ok {
			if inner := deepAgent.ReactAgent(); inner != nil {
				if m, err := inner.GetLLM(); err == nil && m != nil {
					llmModel = m
				}
			}
		}
	}
	return llmModel, nil
}

// agentModelName 读取回调的活模型名，而非共享 session 模型。
//
// Python: _agent_model_name (runtime_prompt_rail.py:346-360)
func (r *RuntimePromptRail) agentModelName(cbc *agentinterfaces.AgentCallbackContext) string {
	model, _ := r.agentModelOf(cbc)
	if mc, ok := model.(*llm.Model); ok && mc != nil && mc.ModelConfig != nil && mc.ModelConfig.ModelName != "" {
		return strings.TrimSpace(mc.ModelConfig.ModelName)
	}
	return ""
}

// agentGroupModelNames 返回本 agent 模型组可路由到的模型；组外返回空。
//
// Python: _agent_group_model_names (runtime_prompt_rail.py:362-382)。
// 委托 Team allocator 的 _selectable_route_tags（导出入口 SelectableRouteTags），
// 使列表始终与 spawn_teammate 接受的 selectable_models 一致。
func (r *RuntimePromptRail) agentGroupModelNames(cbc *agentinterfaces.AgentCallbackContext) []string {
	model, _ := r.agentModelOf(cbc)
	mc, ok := model.(*llm.Model)
	if !ok || mc == nil || mc.ClientConfig == nil || mc.ClientConfig.IntelliRouter == nil {
		return []string{}
	}
	if len(mc.ClientConfig.IntelliRouter.Deployments) == 0 {
		return []string{}
	}
	routerMap := intelliRouterToMap(mc.ClientConfig.IntelliRouter)
	return agentteamsmodels.SelectableRouteTags(routerMap)
}

// modelGroupModelLabel 返回本 agent 的模型组名；从未跑在组上时返回空。
//
// Python: _model_group_model_label (runtime_prompt_rail.py:384-405)。
// 组 id 只记一次：无活模型的回调（如外层 agent）仍归属同一组。
func (r *RuntimePromptRail) modelGroupModelLabel(cbc *agentinterfaces.AgentCallbackContext) string {
	model, _ := r.agentModelOf(cbc)
	if mc, ok := model.(*llm.Model); ok && mc != nil && mc.ClientConfig != nil && mc.ClientConfig.IntelliRouter != nil && mc.ClientConfig.IntelliRouter.ModelGroupID != nil {
		r.mu.Lock()
		r.modelGroupID = *mc.ClientConfig.IntelliRouter.ModelGroupID
		r.mu.Unlock()
	}
	r.mu.RLock()
	groupID := r.modelGroupID
	r.mu.RUnlock()
	if groupID == "" {
		return ""
	}
	r.mu.Lock()
	if _, cached := r.modelGroupNames[groupID]; !cached {
		r.modelGroupNames[groupID] = lookupModelGroupName(groupID)
	}
	name := r.modelGroupNames[groupID]
	r.mu.Unlock()

	snap := r.snapshot()
	if !snap.forceEnglish && snap.language == "cn" {
		return "模型组 " + name
	}
	return "model group " + name
}

// lookupModelGroupName 返回模型组显示名，回退为 id。
//
// Python: _lookup_model_group_name (runtime_prompt_rail.py:407-415)
func lookupModelGroupName(groupID string) string {
	// Python: ModelCatalog().group_display_name(group_id)
	var catalog swarmcommon.ModelCatalog
	return catalog.GroupDisplayName(groupID)
}

// ──────────────────────────── 非导出函数（路径与状态读取） ────────────────────────────

// capitalizeOS 将 runtime.GOOS 首字母大写，对齐 Python platform.system()。
func capitalizeOS(osName string) string {
	if len(osName) > 0 {
		return strings.ToUpper(osName[:1]) + osName[1:]
	}
	return osName
}

// getOSVersion 获取操作系统版本字符串，对齐 Python: platform.system() + " " + platform.release()。
func getOSVersion() string {
	sysName := capitalizeOS(runtime.GOOS)
	release := getOSRelease()
	if release != "" {
		return sysName + " " + release
	}
	return sysName
}

// getOSRelease 获取操作系统 release 信息。对齐 Python: platform.release()
func getOSRelease() string {
	switch runtime.GOOS {
	case "linux", "darwin":
		release, err := getUnameRelease()
		if err == nil && release != "" {
			return release
		}
		return ""
	default:
		// Windows: 暂无 syscall.Uname，使用环境变量
		return os.Getenv("OS")
	}
}

// getUnameRelease 通过 uname 系统调用获取 release 信息。
func getUnameRelease() (string, error) {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

// readRuntimeStateYAML 读取按 session 隔离的 runtime_state 文件，返回 map。
// 文件不存在返回空 map（不中断），其他错误 warn 日志后返回空 map。
// Python: _refresh_dynamic_attachments 的 open(state_path) 段（:776-786）
func readRuntimeStateYAML(sessionID string) map[string]any {
	yamlPath := RuntimeStatePath(sessionID)
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(logComponent).Err(err).Msg("Failed to read runtime state file")
		}
		return map[string]any{}
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		logger.Warn(logComponent).Err(err).Msg("Failed to read runtime state file")
		return map[string]any{}
	}
	if raw == nil {
		return map[string]any{}
	}
	return raw
}

// runtimeStateString 从 runtime_state 读取字符串字段（Python str(...) 语义）。
func runtimeStateString(state map[string]any, key string) string {
	v, ok := state[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

// existingDirs 规范化并过滤存在的目录，保留顺序，去重。
// Python: RuntimePromptRail._existing_dirs(paths)
func existingDirs(paths []string) []string {
	var result []string
	seen := make(map[string]struct{})
	for _, item := range paths {
		p := strings.TrimSpace(item)
		if p == "" {
			continue
		}
		p = pathutil.ExpandHome(p)
		if absPath, err := filepath.Abs(p); err == nil {
			p = absPath
		}
		key := filepath.Clean(strings.ToLower(p))
		if _, ok := seen[key]; ok {
			continue
		}
		info, err := os.Stat(p)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, p)
	}
	return result
}

// existingDir 返回规范化后的存在目录路径，不存在返回空字符串。
// Python: RuntimePromptRail._existing_dir(path)
func existingDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	path = pathutil.ExpandHome(path)
	if absPath, err := filepath.Abs(path); err == nil {
		path = absPath
	}
	info, err := os.Stat(path)
	if err != nil || !info.IsDir() {
		return ""
	}
	return path
}

// samePath 大小写不敏感的路径相等比较。
// Python: RuntimePromptRail._same_path(left, right)
func samePath(left, right string) bool {
	return filepath.Clean(strings.ToLower(left)) == filepath.Clean(strings.ToLower(right))
}

// runGit 在指定目录执行 git 命令，返回 stdout（5 秒超时）。
// Python: _run_git(args) (interface_deep.py L775-783)
func runGit(ctx context.Context, dir string, args ...string) string {
	childCtx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(childCtx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// trimOrEmpty 去空白；全空白返回空串（Python str.strip() 空判语义）。
func trimOrEmpty(s string) string {
	return strings.TrimSpace(s)
}

// snapshot 在读锁内一次性拷贝可变字段。
func (r *RuntimePromptRail) snapshot() runtimePromptSnapshot {
	r.mu.RLock()
	defer r.mu.RUnlock()
	dirs := make([]string, len(r.trustedDirs))
	copy(dirs, r.trustedDirs)
	metadata := map[string]any{}
	for k, v := range r.requestMetadata {
		metadata[k] = v
	}
	return runtimePromptSnapshot{
		language:           r.language,
		channel:            r.channel,
		trustedDirs:        dirs,
		cwd:                r.cwd,
		projectDir:         r.projectDir,
		workspaceDir:       r.workspaceDir,
		taskWorkspaceRoot:  r.taskWorkspaceRoot,
		taskWorkDir:        r.taskWorkDir,
		taskOutputsDir:     r.taskOutputsDir,
		modelName:          r.modelName,
		mode:               r.mode,
		forceEnglish:       r.forceEnglish,
		sessionID:          r.sessionID,
		requestMetadata:    metadata,
		a4pAuthorizerAvail: r.a4pAuthorizerAvailable,
	}
}

// builderOf 返回注入用的系统提示词构建器；rail 上未设置时回退到回调上下文的 Agent。
func (r *RuntimePromptRail) builderOf(cbc *agentinterfaces.AgentCallbackContext) saprompt.SystemPromptBuilderInterface {
	r.mu.RLock()
	builder := r.builder
	r.mu.RUnlock()
	if builder == nil && cbc != nil && cbc.Agent() != nil {
		builder = cbc.Agent().SystemPromptBuilder()
	}
	return builder
}

// agentOf 返回 init 捕获的 agent，缺省回退回调上下文的 Agent。
// Python: self._agent or ctx.agent
func (r *RuntimePromptRail) agentOf(cbc *agentinterfaces.AgentCallbackContext) agentinterfaces.BaseAgent {
	r.mu.RLock()
	agent := r.agent
	r.mu.RUnlock()
	if agent == nil && cbc != nil {
		agent = cbc.Agent()
	}
	return agent
}

// loadConfigRawForModelNames 读取原始配置 map（对齐 Python get_config_raw 的 models.defaults 消费段）。
func loadConfigRawForModelNames() (map[string]any, error) {
	data, err := os.ReadFile(workspace.ConfigFile())
	if err != nil {
		return nil, err
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return raw, nil
}

// intelliRouterToMap 将 IntelliRouterConfig 转为 map（allocator selectableRouteTags 的 map 入参形态，
// 对齐 Python router.model_dump(mode="json")）。
func intelliRouterToMap(router *llmschema.IntelliRouterConfig) map[string]any {
	data, err := json.Marshal(router)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}
