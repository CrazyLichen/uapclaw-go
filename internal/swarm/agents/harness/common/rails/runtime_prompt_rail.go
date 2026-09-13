package rails

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
)

// ──────────────────────────── 结构体 ────────────────────────────

// RuntimePromptRail 运行时提示词护栏 — 在 before_model_call 中注入时间及运行时状态。
//
// 在每次 LLM 调用前动态注入 7 个 PromptSection：
// time/runtime/language_output/env/git_status/browser_tool_policy/trusted_dirs_policy，
// 确保 LLM 无需工具调用即可感知当前运行时环境。
//
// Python: RuntimePromptRail(DeepAgentRail) — runtime_prompt_rail.py (385 行)
type RuntimePromptRail struct {
	rails.DeepAgentRail
	// language per-request 语言
	language string
	// channel per-request 频道
	channel string
	// trustedDirs per-request 可信目录
	trustedDirs []string
	// cwd per-request 当前工作目录
	cwd string
	// projectDir per-request 项目目录
	projectDir string
	// modelName per-request 模型名称（YAML 读取失败时的 fallback）
	modelName string
	// mode per-request 运行模式（YAML 读取失败时的 fallback）
	mode string
	// forceEnglish 强制英文 section（code 模式）
	forceEnglish bool
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// runtimePromptRailPriority RuntimePromptRail 优先级
	// Python: RuntimePromptRail.priority = 5
	runtimePromptRailPriority = 5

	// sectionTimePriority time section 优先级
	sectionTimePriority = 92
	// sectionRuntimePriority runtime section 优先级
	sectionRuntimePriority = 95
	// sectionLanguageOutputPriority language_output section 优先级
	sectionLanguageOutputPriority = 93
	// sectionEnvPriority env section 优先级
	sectionEnvPriority = 89
	// sectionGitStatusPriority git_status section 优先级
	sectionGitStatusPriority = 87
	// sectionBrowserToolPolicyPriority browser_tool_policy section 优先级
	sectionBrowserToolPolicyPriority = 98
	// sectionTrustedDirsPolicyPriority trusted_dirs_policy section 优先级
	sectionTrustedDirsPolicyPriority = 90

	// gitCommandTimeout git 命令超时时间
	// Python: _run_git → subprocess.run(..., timeout=5)
	gitCommandTimeout = 5 * time.Second
)

// ──────────────────────────── 全局变量 ────────────────────────────

// languageNames 语言代码到名称的映射。
// Python: _LANGUAGE_NAMES = {"cn": "Chinese", "zh": "Chinese", "en": "English"}
var languageNames = map[string]string{"cn": "Chinese", "zh": "Chinese", "en": "English"}

// modeDisplayMap 模式显示名称映射。
// Python: _MODE_DISPLAY_MAP (interface_deep.py L427-431)
var modeDisplayMap = map[string]map[string]string{
	"agent.plan": {"cn": "规划模式", "en": "Planning Mode"},
	"agent.fast": {"cn": "性能模式", "en": "Performance Mode"},
	"team":       {"cn": "集群模式", "en": "Cluster Mode"},
}

// runtimeLogComponent 日志组件标识
var runtimeLogComponent = logger.ComponentAgentServer

// 编译时验证 RuntimePromptRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*RuntimePromptRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewRuntimePromptRail 创建 RuntimePromptRail 实例。
// Python: RuntimePromptRail(language="cn", channel="web", timezone_offset=8)
func NewRuntimePromptRail(language, channel string) *RuntimePromptRail {
	r := &RuntimePromptRail{
		language: language,
		channel:  channel,
	}
	r.WithPriority(runtimePromptRailPriority)
	return r
}

// SetLanguage per-request 更新语言。
// Python: RuntimePromptRail.set_language(language)
func (r *RuntimePromptRail) SetLanguage(language string) {
	r.language = language
}

// SetChannel per-request 更新频道。
// Python: RuntimePromptRail.set_channel(channel)
func (r *RuntimePromptRail) SetChannel(channel string) {
	r.channel = channel
}

// SetTrustedDirs per-request 更新可信目录。
// Python: RuntimePromptRail.set_trusted_dirs(trusted_dirs)
func (r *RuntimePromptRail) SetTrustedDirs(dirs []string) {
	r.trustedDirs = dirs
}

// SetRuntimePaths per-request 更新当前工作目录和项目目录。
// Python: RuntimePromptRail.set_runtime_paths(cwd, project_dir)
func (r *RuntimePromptRail) SetRuntimePaths(cwd, projectDir string) {
	r.cwd = strings.TrimSpace(cwd)
	r.projectDir = strings.TrimSpace(projectDir)
}

// SetModelName per-request 更新模型名称，作为文件读取失败时的兜底。
// Python: RuntimePromptRail.set_model_name(model_name)
func (r *RuntimePromptRail) SetModelName(modelName string) {
	r.modelName = modelName
}

// SetMode per-request 更新运行模式，作为文件读取失败时的兜底。
// Python: RuntimePromptRail.set_mode(mode)
func (r *RuntimePromptRail) SetMode(mode string) {
	r.mode = mode
}

// SetForceEnglish 强制英文 section，无视 language 字段（code 模式）。
// Python: RuntimePromptRail.set_force_english(force)
func (r *RuntimePromptRail) SetForceEnglish(force bool) {
	r.forceEnglish = force
}

// BeforeModelCall 模型调用前动态注入运行时状态 PromptSection。
// Python: RuntimePromptRail.before_model_call(ctx)
func (r *RuntimePromptRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	builder := cbc.Agent().SystemPromptBuilder()
	if builder == nil {
		return nil
	}

	// ── time section ──
	r.injectTimeSection(builder)

	// ── runtime section（读 YAML fallback setter）──
	r.injectRuntimeSection(builder)

	// ── language_output section ──
	r.injectLanguageOutputSection(builder)

	// ── env section ──
	r.injectEnvSection(builder)

	// ── git_status section（条件）──
	r.injectGitStatusSection(builder)

	// ── browser_tool_policy section（条件）──
	r.injectBrowserToolPolicySection(builder)

	// ── trusted_dirs_policy section（条件）──
	r.injectTrustedDirsPolicySection(builder)

	return nil
}

// GetCallbacks 覆写基类回调映射，注册 BeforeModelCall。
func (r *RuntimePromptRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// WriteRuntimeStateYAML 将运行时状态写入 config 目录下的 runtime_state.yaml。
// Python: _write_runtime_state() (interface_deep.py L756-821)
func WriteRuntimeStateYAML(modelName, mode, language, channel, agentName, projectDir string) {
	gitBranch := "N/A"
	gitMainBranch := ""
	gitStatus := ""
	gitRecentCommits := ""
	gitUser := ""

	gitBin, err := exec.LookPath("git")
	if err == nil && gitBin != "" && projectDir != "" {
		info, statErr := os.Stat(projectDir)
		if statErr == nil && info.IsDir() {
			gitBranch = runGit(projectDir, "rev-parse", "--abbrev-ref", "HEAD")
			if gitBranch == "" {
				gitBranch = "N/A"
			}
			if gitBranch != "N/A" {
				insideWorkTree := runGit(projectDir, "rev-parse", "--is-inside-work-tree")
				if insideWorkTree == "true" {
					gitStatus = runGit(projectDir, "status", "--short")
					if len(gitStatus) > 0 {
						lines := strings.Split(gitStatus, "\n")
						if len(lines) > 50 {
							lines = lines[:50]
						}
						gitStatus = strings.Join(lines, "\n")
					}
					gitRecentCommits = runGit(projectDir, "log", "--oneline", "-5")
					gitUser = runGit(projectDir, "config", "user.name")
					for _, candidate := range []string{"origin/main", "origin/master", "main", "master"} {
						if runGit(projectDir, "rev-parse", "--verify", "--quiet", candidate) != "" {
							gitMainBranch = candidate
							break
						}
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
		"platform":           fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH),
		"go_version":         runtime.Version(),
		"git_branch":         gitBranch,
		"git_main_branch":    gitMainBranch,
		"git_status":         gitStatus,
		"git_recent_commits": gitRecentCommits,
		"git_user":           gitUser,
	}

	yamlData, err := yaml.Marshal(state)
	if err != nil {
		logger.Debug(runtimeLogComponent).Err(err).Msg("序列化 runtime_state.yaml 失败")
		return
	}

	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	if err := os.WriteFile(yamlPath, yamlData, 0644); err != nil {
		logger.Debug(runtimeLogComponent).Err(err).Msg("写入 runtime_state.yaml 失败")
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

// injectTimeSection 注入 time PromptSection。
func (r *RuntimePromptRail) injectTimeSection(builder saprompt.SystemPromptBuilderInterface) {
	if !r.forceEnglish && r.language == "cn" {
		// Python L131-134: 一比一复制
		timeContentCN := "# 时间说明\n\n" +
			"- 当用户询问\u201c最新、当前、今年、本年、实时、近期\u201d等信息并需要搜索时，" +
			"搜索 query 必须优先使用当前年份或日期"
		timeContentEN := "# Time Description\n\n" +
			"- When the user asks for latest/current/this-year/recent information and search is needed, " +
			"search queries must prefer the current year or date."
		builder.AddSection(saprompt.PromptSection{
			Name:     "time",
			Content:  map[string]string{"cn": timeContentCN, "en": timeContentEN},
			Priority: sectionTimePriority,
		})
	} else {
		// Python L136-140: 一比一复制
		timeContentEN := "# Time Description\n\n" +
			"- When the user asks for latest/current/this-year/recent information and search is needed, " +
			"search queries must prefer the current year or date."
		builder.AddSection(saprompt.PromptSection{
			Name:     "time",
			Content:  map[string]string{"cn": timeContentEN, "en": timeContentEN},
			Priority: sectionTimePriority,
		})
	}
}

// injectRuntimeSection 注入 runtime PromptSection。
func (r *RuntimePromptRail) injectRuntimeSection(builder saprompt.SystemPromptBuilderInterface) {
	// 读取 runtime_state.yaml
	// Python L148-155: 一比一复制逻辑
	runtimeState := readRuntimeStateYAML()

	model := FirstNonEmpty(runtimeState["model"], r.modelName, "unknown")
	mde := FirstNonEmpty(runtimeState["mode"], r.mode, "unknown")
	languageVal := FirstNonEmpty(runtimeState["language"], r.language, "unknown")
	channel := FirstNonEmpty(runtimeState["channel"], r.channel, "unknown")

	if !r.forceEnglish && r.language == "cn" {
		// Python L163-171: 一比一复制
		runtimeContent := "# 运行时状态\n\n" +
			fmt.Sprintf("- 当前模型：%s\n", model) +
			fmt.Sprintf("- 当前模式：%s\n", mde) +
			fmt.Sprintf("- 当前语言：%s\n", languageVal) +
			fmt.Sprintf("- 当前渠道：%s\n", channel) +
			"- 当用户询问「你是什么模型」「当前用的是哪个模型」等问题时，" +
			"直接用上方「当前模型」的值回答，只说模型名称，不要介绍身份或列出能力"
		builder.AddSection(saprompt.PromptSection{
			Name:     "runtime",
			Content:  map[string]string{"cn": runtimeContent, "en": runtimeContent},
			Priority: sectionRuntimePriority,
		})
	} else {
		// Python L173-182: 一比一复制
		runtimeContent := "# Runtime State\n\n" +
			fmt.Sprintf("- Current model: %s\n", model) +
			fmt.Sprintf("- Current mode: %s\n", mde) +
			fmt.Sprintf("- Current language: %s\n", languageVal) +
			fmt.Sprintf("- Current channel: %s\n", channel) +
			"- When the user asks \"what model are you\" or similar questions, " +
			"answer with only the model name above in one sentence — " +
			"do NOT introduce yourself or list capabilities."
		builder.AddSection(saprompt.PromptSection{
			Name:     "runtime",
			Content:  map[string]string{"cn": runtimeContent, "en": runtimeContent},
			Priority: sectionRuntimePriority,
		})
	}
}

// injectLanguageOutputSection 注入 language_output PromptSection。
func (r *RuntimePromptRail) injectLanguageOutputSection(builder saprompt.SystemPromptBuilderInterface) {
	// Python L191-205: 一比一复制逻辑
	builder.RemoveSection("language_output")
	runtimeState := readRuntimeStateYAML()
	languageVal := FirstNonEmpty(runtimeState["language"], r.language, "unknown")
	languageName, ok := languageNames[languageVal]
	if !ok {
		languageName = languageVal
	}
	// Python L193-199: 一比一复制
	languageOutputContent := "# Language\n\n" +
		fmt.Sprintf("Always respond in %s. ", languageName) +
		fmt.Sprintf("Use %s for all explanations, comments, ", languageName) +
		"and communications with the user. " +
		"Technical terms and code identifiers should remain " +
		"in their original form."
	builder.AddSection(saprompt.PromptSection{
		Name:     "language_output",
		Content:  map[string]string{"cn": languageOutputContent, "en": languageOutputContent},
		Priority: sectionLanguageOutputPriority,
	})
}

// injectEnvSection 注入 env PromptSection。
func (r *RuntimePromptRail) injectEnvSection(builder saprompt.SystemPromptBuilderInterface) {
	// Python L208-212: 对齐平台检测
	osType := runtime.GOOS
	shellPath := os.Getenv("SHELL")
	shellName := "unknown"
	if shellPath != "" {
		shellName = filepath.Base(shellPath)
	}
	osVersion := fmt.Sprintf("%s %s", runtime.GOOS, runtime.GOARCH)

	if !r.forceEnglish && r.language == "cn" {
		// Python L214-237: 一比一复制
		envContent := "# 运行环境\n\n" +
			fmt.Sprintf("- 当前运行平台：`%s`\n", osType) +
			fmt.Sprintf("- Shell：%s\n", shellName) +
			fmt.Sprintf("- OS 版本：%s\n\n", osVersion) +
			"## 平台命令差异（仅在必须使用 shell 时参考）\n\n" +
			"以下命令差异仅适用于测试、构建、git、包管理、运行脚本等必须调用 shell 的场景。" +
			"文件读取、编辑、搜索仍应优先使用专用工具。\n\n" +
			"| 操作 | Windows (`win32`/`win64`) | Linux/macOS (`linux`/`darwin`) |\n" +
			"|------|---------------------------|-------------------------------|\n" +
			"| 创建目录 | `mkdir folder` 或 PowerShell " +
			"`New-Item -ItemType Directory -Path folder` " +
			"| `mkdir -p folder` |\n" +
			"| 删除文件 | `del file.txt` 或 PowerShell `Remove-Item file.txt` | `rm file.txt` |\n" +
			"| 删除目录 | `rmdir folder` 或 PowerShell `Remove-Item -Recurse folder` | `rm -rf folder` |\n" +
			"| 查找文件 | `dir /s pattern` 或 PowerShell " +
			"`Get-ChildItem -Recurse -Filter pattern` " +
			"| `find . -name pattern` |\n\n" +
			"**特别注意**：Windows 的 `mkdir` 不支持 `-p` 参数！" +
			"在 Windows 上使用 `mkdir -p folder` 会错误创建名为 `-p` 的目录。" +
			"如需创建嵌套目录，请使用 PowerShell `New-Item -ItemType Directory -Path \"parent/child\" -Force`，" +
			"或使用 cmd 分步创建 `mkdir parent && mkdir parent\\child`。"
		builder.AddSection(saprompt.PromptSection{
			Name:     "env",
			Content:  map[string]string{"cn": envContent, "en": envContent},
			Priority: sectionEnvPriority,
		})
	} else {
		// Python L239-263: 一比一复制
		envContent := "# Environment\n\n" +
			fmt.Sprintf("- Current platform: `%s`\n", osType) +
			fmt.Sprintf("- Shell: %s\n", shellName) +
			fmt.Sprintf("- OS Version: %s\n\n", osVersion) +
			"## Platform Command Differences (only when shell is required)\n\n" +
			"The following command differences apply only to scenarios where shell execution is required " +
			"(testing, builds, git, package management, running scripts). " +
			"File reading, editing, and searching should still prefer dedicated tools.\n\n" +
			"| Operation | Windows (`win32`/`win64`) | Linux/macOS (`linux`/`darwin`) |\n" +
			"|-----------|---------------------------|-------------------------------|\n" +
			"| Create directory | `mkdir folder` or PowerShell " +
			"`New-Item -ItemType Directory -Path folder` " +
			"| `mkdir -p folder` |\n" +
			"| Delete file | `del file.txt` or PowerShell `Remove-Item file.txt` | `rm file.txt` |\n" +
			"| Delete directory | `rmdir folder` or PowerShell `Remove-Item -Recurse folder` | `rm -rf folder` |\n" +
			"| Find file | `dir /s pattern` or PowerShell " +
			"`Get-ChildItem -Recurse -Filter pattern` " +
			"| `find . -name pattern` |\n\n" +
			"**WARNING**: Windows `mkdir` does NOT support the `-p` flag! " +
			"Using `mkdir -p folder` on Windows will incorrectly create a directory named `-p`. " +
			"To create nested directories on Windows, use either PowerShell " +
			"`New-Item -ItemType Directory -Path \"parent/child\" -Force` " +
			"or cmd with step-by-step creation `mkdir parent && mkdir parent\\\\child`."
		builder.AddSection(saprompt.PromptSection{
			Name:     "env",
			Content:  map[string]string{"cn": envContent, "en": envContent},
			Priority: sectionEnvPriority,
		})
	}
}

// injectGitStatusSection 注入 git_status PromptSection（条件）。
func (r *RuntimePromptRail) injectGitStatusSection(builder saprompt.SystemPromptBuilderInterface) {
	// Python L271-300: 一比一复制逻辑
	builder.RemoveSection("git_status")
	runtimeState := readRuntimeStateYAML()
	gitBranch := strings.TrimSpace(runtimeState["git_branch"])
	if gitBranch == "" || gitBranch == "N/A" {
		return
	}
	gitMainBranch := strings.TrimSpace(runtimeState["git_main_branch"])
	gitStatusText := strings.TrimSpace(runtimeState["git_status"])
	gitRecentCommits := strings.TrimSpace(runtimeState["git_recent_commits"])
	gitUser := strings.TrimSpace(runtimeState["git_user"])

	// Python L280-294: 一比一复制
	gitLines := []string{
		"This is the git status at the start of the conversation. " +
			"Note that this status is a snapshot in time, and will not update during the conversation.",
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

// injectBrowserToolPolicySection 注入 browser_tool_policy PromptSection（条件）。
func (r *RuntimePromptRail) injectBrowserToolPolicySection(builder saprompt.SystemPromptBuilderInterface) {
	// Python L302-319: 一比一复制逻辑
	builder.RemoveSection("browser_tool_policy")
	if r.channel != "web" {
		return
	}
	// Python L304-313: 一比一复制
	browserToolPolicy := "# Browser Tool Policy\n\n" +
		"- For browser tasks such as opening pages, navigation, clicking, typing, login, screenshots, " +
		"page inspection, or extracting data from a live website, use `spawn_sub_agent` with " +
		"`subagent_type` set to `\"browser_agent\"` and put the full browser objective in " +
		"`task_description`.\n" +
		"- Do not use bash, execute_code, subprocess, shell commands, or direct Chrome/Edge launches " +
		"for browser automation.\n" +
		"- If `spawn_sub_agent` or `browser_agent` is unavailable, say that the browser " +
		"subagent is unavailable before trying to start a browser through commands."
	builder.AddSection(saprompt.PromptSection{
		Name:     "browser_tool_policy",
		Content:  map[string]string{"cn": browserToolPolicy, "en": browserToolPolicy},
		Priority: sectionBrowserToolPolicyPriority,
	})
}

// injectTrustedDirsPolicySection 注入 trusted_dirs_policy PromptSection（条件）。
func (r *RuntimePromptRail) injectTrustedDirsPolicySection(builder saprompt.SystemPromptBuilderInterface) {
	// Python L321-385: 一比一复制逻辑
	builder.RemoveSection("trusted_dirs_policy")
	if r.channel != "tui" {
		return
	}
	dirs := existingDirs(r.trustedDirs)
	currentDir := existingDir(r.cwd)
	if currentDir == "" {
		currentDir = existingDir(r.projectDir)
	}
	if currentDir == "" && len(dirs) > 0 {
		currentDir = dirs[0]
	}
	if currentDir == "" {
		return
	}
	workspaceDir := workspace.AgentRootDir()
	projectDir := currentDir
	var otherDirs []string
	for _, p := range dirs {
		if !samePath(p, projectDir) {
			otherDirs = append(otherDirs, p)
		}
	}
	cnDirsDisplay := "无"
	enDirsDisplay := "none"
	if len(otherDirs) > 0 {
		cnDirsDisplay = strings.Join(otherDirs, ", ")
		enDirsDisplay = cnDirsDisplay
	}

	if !r.forceEnglish && r.language == "cn" {
		// Python L340-354: 一比一复制
		trustedDirsContent := "# 工作目录策略\n\n" +
			fmt.Sprintf("- 系统目录（不要在其中查找或运行项目文件）：%s\n", workspaceDir) +
			fmt.Sprintf("- 当前项目目录（你正在工作的项目，查询文件、运行测试、执行命令等均应在此目录下进行）：%s\n", projectDir) +
			fmt.Sprintf("- 其他可访问目录（可读写其中的资源，但不是当前项目目录）：%s\n\n", cnDirsDisplay) +
			"重要规则：\n" +
			"- 命令执行工具（mcp_exec_command）默认的工作目录是系统目录，" +
			"如果你要在项目目录下执行命令，必须将工具的 workdir 参数设置为当前项目目录，" +
			fmt.Sprintf("即 workdir=\"%s\"，不要使用默认值或 cd 方式切换，", projectDir) +
			"因为 cd 只在子shell中生效，不会改变工具本身的工作目录\n" +
			"- 查找项目文件、读取项目代码时，应在当前项目目录下搜索，不要在系统目录下查找\n" +
			"- 不要在系统目录下运行项目测试或构建，系统目录仅用于存放配置和状态文件\n" +
			"- 若用户请求的操作涉及超出上述目录范围的路径，必须先向用户确认是否允许此次操作\n" +
			"- 确认时需明确告知：操作的完整路径、操作类型（读取/编辑/执行）、潜在风险\n"
		builder.AddSection(saprompt.PromptSection{
			Name:     "trusted_dirs_policy",
			Content:  map[string]string{"cn": trustedDirsContent, "en": trustedDirsContent},
			Priority: sectionTrustedDirsPolicyPriority,
		})
	} else {
		// Python L356-378: 一比一复制
		trustedDirsContent := "# Working Directory Policy\n\n" +
			fmt.Sprintf("- System directory (never search or run project files here): %s\n", workspaceDir) +
			fmt.Sprintf("- Current project directory (the project you are working on; "+
				"all file queries, test runs, command execution should happen here): %s\n", projectDir) +
			fmt.Sprintf("- Other accessible directories (read/write allowed, but not the current project): "+
				"%s\n\n", enDirsDisplay) +
			"Important rules:\n" +
			"- The command execution tool (mcp_exec_command) defaults its working directory " +
			"to the system directory. When you need to execute commands in the project directory, " +
			"you MUST set the tool's workdir parameter to the current project directory, " +
			fmt.Sprintf("i.e. workdir=\"%s\". Do NOT rely on cd to switch directories, ", projectDir) +
			"because cd only takes effect inside a subshell and does not change the tool's " +
			"actual working directory\n" +
			"- When searching for project files or reading project code, search within the " +
			"current project directory, not the system directory\n" +
			"- Never run project tests or builds in the system directory; " +
			"the system directory is only for config and state files\n" +
			"- If the user requests an operation involving paths outside the above directories, " +
			"you must first ask the user to confirm whether to allow this operation\n" +
			"- When confirming, clearly state: the full path, operation type (read/edit/execute), " +
			"potential risks\n"
		builder.AddSection(saprompt.PromptSection{
			Name:     "trusted_dirs_policy",
			Content:  map[string]string{"cn": trustedDirsContent, "en": trustedDirsContent},
			Priority: sectionTrustedDirsPolicyPriority,
		})
	}
}

// readRuntimeStateYAML 读取 runtime_state.yaml 文件，返回 map。
// 文件不存在返回空 map（不中断），其他错误 warn 日志后返回空 map。
// Python L148-155: 一比一复制逻辑
func readRuntimeStateYAML() map[string]string {
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		if !os.IsNotExist(err) {
			logger.Warn(runtimeLogComponent).Err(err).Msg("读取 runtime_state.yaml 失败")
		}
		return make(map[string]string)
	}
	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		logger.Warn(runtimeLogComponent).Err(err).Msg("解析 runtime_state.yaml 失败")
		return make(map[string]string)
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		result[k] = strings.TrimSpace(fmt.Sprint(v))
	}
	return result
}

// existingDirs 规范化并过滤存在的目录，保留顺序，去重。
// Python: RuntimePromptRail._existing_dirs(paths)
func existingDirs(paths []string) []string {
	var result []string
	seen := make(map[string]struct{})
	for _, item := range paths {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		path := filepath.Clean(item)
		key := filepath.Clean(strings.ToLower(path))
		if _, ok := seen[key]; ok {
			continue
		}
		info, err := os.Stat(path)
		if err != nil || !info.IsDir() {
			continue
		}
		seen[key] = struct{}{}
		result = append(result, path)
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
	path = filepath.Clean(path)
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
func runGit(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), gitCommandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
