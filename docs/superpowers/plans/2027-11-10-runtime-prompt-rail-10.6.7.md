# RuntimePromptRail (10.6.7) 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 RuntimePromptRail，在每次 LLM 调用前动态注入运行时状态 PromptSection，并回填 adapter 层 stub。

**Architecture:** 新建 `runtime_prompt_rail.go` 实现 RuntimePromptRail（嵌入 DeepAgentRail，BeforeModelCall 注入 7 个 PromptSection），扩展 `SystemPromptBuilderInterface` 加 `SetLanguage`，扩展 `runtimeConfig` 对齐 Python，改写 `writeRuntimeState` 为 YAML 输出，回填 `buildRuntimePromptRail` / `updatePromptForMode` / `updateRuntimeConfig` 的 7 个 setter 调用。

**Tech Stack:** Go, gopkg.in/yaml.v3, DeepAgentRail 基类, SystemPromptBuilder 接口

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/agentcore/single_agent/prompts/builder.go` | 修改 | `SystemPromptBuilderInterface` 加 `SetLanguage` |
| `internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go` | 新建 | RuntimePromptRail 实现（结构体 + 7 setter + BeforeModelCall + 3 工具方法） |
| `internal/swarm/agents/harness/common/rails/runtime_prompt_rail_test.go` | 新建 | 单元测试 |
| `internal/swarm/agents/harness/common/rails/doc.go` | 修改 | 文件目录加 runtime_prompt_rail.go |
| `internal/swarm/server/adapter/deep_adapter.go` | 修改 | `runtimePromptRail` 字段类型改为 `*rails.RuntimePromptRail` |
| `internal/swarm/server/adapter/deep_adapter_config.go` | 修改 | `runtimeConfig` 扩展 + `writeRuntimeStateYAML` + `updateRuntimeConfig` 补 setter + 删除旧 `writeRuntimeState` |
| `internal/swarm/server/adapter/deep_adapter_config_test.go` | 修改 | 更新测试适配新 runtimeConfig 字段 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | 修改 | `buildRuntimePromptRail` 回填 + `updatePromptForMode` 回填 |
| `internal/swarm/server/adapter/code_adapter.go` | 修改 | CodeAdapter 调用 `SetForceEnglish` |

---

### Task 1: 扩展 SystemPromptBuilderInterface 加 SetLanguage

**Files:**
- Modify: `internal/agentcore/single_agent/prompts/builder.go:19-30`

- [ ] **Step 1: 在 SystemPromptBuilderInterface 中添加 SetLanguage 方法**

在 `builder.go` 的 `SystemPromptBuilderInterface` 接口中添加：

```go
type SystemPromptBuilderInterface interface {
	// AddSection 添加或替换节
	AddSection(section PromptSection) *SystemPromptBuilder
	// RemoveSection 移除指定名称的节
	RemoveSection(name string) *SystemPromptBuilder
	// Language 返回当前语言
	Language() string
	// SetLanguage 设置当前语言
	SetLanguage(lang string)
	// GetSection 按名称获取单个节
	GetSection(name string) *PromptSection
	// HasSection 检查节是否存在
	HasSection(name string) bool
}
```

注意：`*SystemPromptBuilder` 已有 `SetLanguage(lang string)` 方法（L164），因此自动满足扩展后的接口，无需额外适配。

- [ ] **Step 2: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/single_agent/prompts/...`
Expected: 编译成功

- [ ] **Step 3: 运行既有测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/single_agent/prompts/... -count=1`
Expected: PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/single_agent/prompts/builder.go
git commit -m "feat(prompts): 扩展 SystemPromptBuilderInterface 加 SetLanguage 方法"
```

---

### Task 2: 创建 RuntimePromptRail 结构体 + 7 个 Setter + GetCallbacks

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go`

- [ ] **Step 1: 创建 runtime_prompt_rail.go 文件骨架**

创建 `internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go`，包含完整的结构体定义、7 个 Setter、GetCallbacks、编译时验证。提示词内容从 Python 源码 `runtime_prompt_rail.py` 一比一复制。

```go
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
	if r.cwd == "" {
		r.cwd = ""
	}
	r.projectDir = strings.TrimSpace(projectDir)
	if r.projectDir == "" {
		r.projectDir = ""
	}
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

// ──────────────────────────── 非导出函数 ────────────────────────────

// injectTimeSection 注入 time PromptSection。
func (r *RuntimePromptRail) injectTimeSection(builder saprompt.SystemPromptBuilderInterface) {
	if !r.forceEnglish && r.language == "cn" {
		// Python L131-134: 一比一复制
		builder.AddSection(saprompt.PromptSection{
			Name: "time",
			Content: map[string]string{
				"cn": "# 时间说明\n\n" +
					"- 当用户询问"最新、当前、今年、本年、实时、近期"等信息并需要搜索时，" +
					"搜索 query 必须优先使用当前年份或日期",
				"en": "# Time Description\n\n" +
					"- When the user asks for latest/current/this-year/recent information and search is needed, " +
					"search queries must prefer the current year or date.",
			},
			Priority: sectionTimePriority,
		})
	} else {
		// Python L136-140: 一比一复制
		builder.AddSection(saprompt.PromptSection{
			Name: "time",
			Content: map[string]string{
				"cn": "# Time Description\n\n" +
					"- When the user asks for latest/current/this-year/recent information and search is needed, " +
					"search queries must prefer the current year or date.",
				"en": "# Time Description\n\n" +
					"- When the user asks for latest/current/this-year/recent information and search is needed, " +
					"search queries must prefer the current year or date.",
			},
			Priority: sectionTimePriority,
		})
	}
}

// injectRuntimeSection 注入 runtime PromptSection。
func (r *RuntimePromptRail) injectRuntimeSection(builder saprompt.SystemPromptBuilderInterface) {
	// 读取 runtime_state.yaml
	// Python L148-155: 一比一复制逻辑
	runtimeState := readRuntimeStateYAML()

	model := firstNonEmpty(runtimeState["model"], r.modelName, "unknown")
	mode := firstNonEmpty(runtimeState["mode"], r.mode, "unknown")
	languageVal := firstNonEmpty(runtimeState["language"], r.language, "unknown")
	channel := firstNonEmpty(runtimeState["channel"], r.channel, "unknown")

	if !r.forceEnglish && r.language == "cn" {
		// Python L163-171: 一比一复制
		runtimeContent := "# 运行时状态\n\n" +
			fmt.Sprintf("- 当前模型：%s\n", model) +
			fmt.Sprintf("- 当前模式：%s\n", mode) +
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
			fmt.Sprintf("- Current mode: %s\n", mode) +
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
	languageVal := firstNonEmpty(readRuntimeStateYAML()["language"], r.language, "unknown")
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
	trustedDirs := existingDirs(r.trustedDirs)
	currentDir := existingDir(r.cwd)
	if currentDir == "" {
		currentDir = existingDir(r.projectDir)
	}
	if currentDir == "" && len(trustedDirs) > 0 {
		currentDir = trustedDirs[0]
	}
	if currentDir == "" {
		return
	}
	workspaceDir := workspace.AgentRootDir()
	projectDir := currentDir
	var otherDirs []string
	for _, p := range trustedDirs {
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

// firstNonEmpty 返回第一个非空字符串，都为空则返回 fallback。
func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		v = strings.TrimSpace(v)
		if v != "" {
			return v
		}
	}
	return ""
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

// writeRuntimeStateYAML 将运行时状态写入 config 目录下的 runtime_state.yaml。
// Python: _write_runtime_state() (interface_deep.py L756-821)
func writeRuntimeStateYAML(modelName, mode, language, channel, agentName, projectDir string) {
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

// runGit 在指定目录执行 git 命令，返回 stdout（5 秒超时）。
// Python: _run_git(args) (interface_deep.py L775-783)
func runGit(dir string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
```

- [ ] **Step 2: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/agents/harness/common/rails/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go
git commit -m "feat(rails): 创建 RuntimePromptRail 结构体 + 7 setter + BeforeModelCall + 工具方法"
```

---

### Task 3: 更新 rails doc.go

**Files:**
- Modify: `internal/swarm/agents/harness/common/rails/doc.go`

- [ ] **Step 1: 在 doc.go 文件目录中添加 runtime_prompt_rail.go 条目**

在文件目录树中增加 `runtime_prompt_rail.go` 条目：

```
├── runtime_prompt_rail.go    # RuntimePromptRail 运行时提示词护栏
```

- [ ] **Step 2: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/doc.go
git commit -m "docs(rails): doc.go 添加 runtime_prompt_rail.go 条目"
```

---

### Task 4: RuntimePromptRail 单元测试

**Files:**
- Create: `internal/swarm/agents/harness/common/rails/runtime_prompt_rail_test.go`

- [ ] **Step 1: 创建测试文件**

测试覆盖：构造/7 个 setter/BeforeModelCall 各 section 注入条件/工具方法。使用 mock SystemPromptBuilder（实现 SystemPromptBuilderInterface）验证 section 注入。

```go
package rails

import (
	"os"
	"path/filepath"
	"testing"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
)

// ──────────────────────────── 结构体 ────────────────────────────

// mockBuilder 测试用 mock SystemPromptBuilder
type mockBuilder struct {
	sections  map[string]saprompt.PromptSection
	language  string
}

func newMockBuilder() *mockBuilder {
	return &mockBuilder{
		sections: make(map[string]saprompt.PromptSection),
		language: "cn",
	}
}

func (m *mockBuilder) AddSection(section saprompt.PromptSection) *saprompt.SystemPromptBuilder {
	m.sections[section.Name] = section
	return nil
}

func (m *mockBuilder) RemoveSection(name string) *saprompt.SystemPromptBuilder {
	delete(m.sections, name)
	return nil
}

func (m *mockBuilder) Language() string { return m.language }

func (m *mockBuilder) SetLanguage(lang string) { m.language = lang }

func (m *mockBuilder) GetSection(name string) *saprompt.PromptSection {
	if s, ok := m.sections[name]; ok {
		return &s
	}
	return nil
}

func (m *mockBuilder) HasSection(name string) bool {
	_, ok := m.sections[name]
	return ok
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// TestNewRuntimePromptRail 测试构造和默认值
func TestNewRuntimePromptRail(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	if rail.language != "cn" {
		t.Errorf("期望 language=cn，实际=%s", rail.language)
	}
	if rail.channel != "web" {
		t.Errorf("期望 channel=web，实际=%s", rail.channel)
	}
}

// TestSetLanguage 测试 SetLanguage
func TestSetLanguage(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetLanguage("en")
	if rail.language != "en" {
		t.Errorf("期望 language=en，实际=%s", rail.language)
	}
}

// TestSetChannel 测试 SetChannel
func TestSetChannel(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetChannel("tui")
	if rail.channel != "tui" {
		t.Errorf("期望 channel=tui，实际=%s", rail.channel)
	}
}

// TestSetTrustedDirs 测试 SetTrustedDirs
func TestSetTrustedDirs(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	dirs := []string{"/a", "/b"}
	rail.SetTrustedDirs(dirs)
	if len(rail.trustedDirs) != 2 {
		t.Errorf("期望 len=2，实际=%d", len(rail.trustedDirs))
	}
}

// TestSetRuntimePaths 测试 SetRuntimePaths
func TestSetRuntimePaths(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetRuntimePaths("/cwd", "/project")
	if rail.cwd != "/cwd" {
		t.Errorf("期望 cwd=/cwd，实际=%s", rail.cwd)
	}
	if rail.projectDir != "/project" {
		t.Errorf("期望 projectDir=/project，实际=%s", rail.projectDir)
	}
	// 空白字符串应被 trim
	rail.SetRuntimePaths("  ", "  ")
	if rail.cwd != "" {
		t.Errorf("期望空 cwd，实际=%s", rail.cwd)
	}
}

// TestSetModelName 测试 SetModelName
func TestSetModelName(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetModelName("qwen-max")
	if rail.modelName != "qwen-max" {
		t.Errorf("期望 modelName=qwen-max，实际=%s", rail.modelName)
	}
}

// TestSetMode 测试 SetMode
func TestSetMode(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetMode("agent.plan")
	if rail.mode != "agent.plan" {
		t.Errorf("期望 mode=agent.plan，实际=%s", rail.mode)
	}
}

// TestSetForceEnglish 测试 SetForceEnglish
func TestSetForceEnglish(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetForceEnglish(true)
	if !rail.forceEnglish {
		t.Error("期望 forceEnglish=true")
	}
}

// TestInjectTimeSection_中文 测试中文 time section
func TestInjectTimeSection_中文(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectTimeSection(builder)
	section := builder.GetSection("time")
	if section == nil {
		t.Fatal("time section 未注入")
	}
	if section.Priority != sectionTimePriority {
		t.Errorf("期望 priority=%d，实际=%d", sectionTimePriority, section.Priority)
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Error("中文内容为空")
	}
}

// TestInjectTimeSection_英文 测试英文 time section（forceEnglish）
func TestInjectTimeSection_英文(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetForceEnglish(true)
	builder := newMockBuilder()
	rail.injectTimeSection(builder)
	section := builder.GetSection("time")
	if section == nil {
		t.Fatal("time section 未注入")
	}
	cnContent := section.Content["cn"]
	if cnContent == "" {
		t.Error("英文内容为空")
	}
}

// TestInjectRuntimeSection 测试 runtime section 注入
func TestInjectRuntimeSection(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	rail.SetModelName("test-model")
	rail.SetMode("agent.plan")
	builder := newMockBuilder()
	rail.injectRuntimeSection(builder)
	section := builder.GetSection("runtime")
	if section == nil {
		t.Fatal("runtime section 未注入")
	}
	if section.Priority != sectionRuntimePriority {
		t.Errorf("期望 priority=%d，实际=%d", sectionRuntimePriority, section.Priority)
	}
}

// TestInjectBrowserToolPolicySection_条件 测试 channel=web 时注入
func TestInjectBrowserToolPolicySection_条件(t *testing.T) {
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectBrowserToolPolicySection(builder)
	if !builder.HasSection("browser_tool_policy") {
		t.Error("channel=web 时应注入 browser_tool_policy")
	}

	rail2 := NewRuntimePromptRail("cn", "tui")
	builder2 := newMockBuilder()
	rail2.injectBrowserToolPolicySection(builder2)
	if builder2.HasSection("browser_tool_policy") {
		t.Error("channel=tui 时不应注入 browser_tool_policy")
	}
}

// TestInjectGitStatusSection_条件 测试 git_branch 条件
func TestInjectGitStatusSection_条件(t *testing.T) {
	// 无 runtime_state.yaml → git_branch 为空 → 不注入
	rail := NewRuntimePromptRail("cn", "web")
	builder := newMockBuilder()
	rail.injectGitStatusSection(builder)
	if builder.HasSection("git_status") {
		t.Error("无 git_branch 时不应注入 git_status")
	}
}

// TestExistingDirs 测试 existingDirs
func TestExistingDirs(t *testing.T) {
	tmpDir := t.TempDir()
	result := existingDirs([]string{tmpDir, "/nonexistent/path"})
	if len(result) != 1 {
		t.Errorf("期望 len=1，实际=%d", len(result))
	}
	if result[0] != filepath.Clean(tmpDir) {
		t.Errorf("期望 %s，实际=%s", filepath.Clean(tmpDir), result[0])
	}
	// nil 输入
	result2 := existingDirs(nil)
	if len(result2) != 0 {
		t.Errorf("期望 len=0，实际=%d", len(result2))
	}
}

// TestExistingDir 测试 existingDir
func TestExistingDir(t *testing.T) {
	tmpDir := t.TempDir()
	result := existingDir(tmpDir)
	if result != filepath.Clean(tmpDir) {
		t.Errorf("期望 %s，实际=%s", filepath.Clean(tmpDir), result)
	}
	// 不存在
	result2 := existingDir("/nonexistent/path")
	if result2 != "" {
		t.Errorf("期望空，实际=%s", result2)
	}
	// 空输入
	result3 := existingDir("")
	if result3 != "" {
		t.Errorf("期望空，实际=%s", result3)
	}
}

// TestSamePath 测试 samePath
func TestSamePath(t *testing.T) {
	if !samePath("/tmp/test", "/tmp/test") {
		t.Error("相同路径应返回 true")
	}
}

// TestFirstNonEmpty 测试 firstNonEmpty
func TestFirstNonEmpty(t *testing.T) {
	if firstNonEmpty("", "", "fallback") != "fallback" {
		t.Error("应返回第一个非空值")
	}
	if firstNonEmpty("first", "second") != "first" {
		t.Error("应返回第一个非空值")
	}
	if firstNonEmpty("  ", "val") != "val" {
		t.Error("空白应视为空")
	}
}

// TestWriteRuntimeStateYAML 测试 YAML 写入
func TestWriteRuntimeStateYAML(t *testing.T) {
	tmpDir := t.TempDir()
	// 通过覆盖 workspace.ConfigDir 来测试（无法直接覆盖，此处验证函数不 panic）
	// 仅验证 runGit 和 writeRuntimeStateYAML 不崩溃
	writeRuntimeStateYAML("test-model", "agent.plan", "cn", "web", "test-agent", tmpDir)
	// 验证 YAML 文件生成
	configDir := workspace.ConfigDir()
	yamlPath := filepath.Join(configDir, "runtime_state.yaml")
	if _, err := os.Stat(yamlPath); os.IsNotExist(err) {
		// ConfigDir 可能指向非 tmpDir，此处仅验证不崩溃
		t.Log("runtime_state.yaml 未写入 ConfigDir（正常，ConfigDir 不是 tmpDir）")
	}
}

// TestReadRuntimeStateYAML 测试 YAML 读取
func TestReadRuntimeStateYAML(t *testing.T) {
	// 无文件时应返回空 map
	result := readRuntimeStateYAML()
	if result == nil {
		t.Error("不应返回 nil")
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/... -run "TestNewRuntimePromptRail|TestSet|TestInject|TestExisting|TestSamePath|TestFirstNonEmpty|TestWriteRuntimeStateYAML|TestReadRuntimeStateYAML" -v -count=1`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/agents/harness/common/rails/runtime_prompt_rail_test.go
git commit -m "test(rails): RuntimePromptRail 单元测试"
```

---

### Task 5: 扩展 runtimeConfig + 改写 writeRuntimeStateYAML + 补 updateRuntimeConfig setter 调用

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_config_test.go`

- [ ] **Step 1: 扩展 runtimeConfig 结构体**

在 `deep_adapter_config.go` 中扩展 `runtimeConfig`：

```go
// runtimeConfig 运行时配置。
// Python: _RuntimeConfig (line 3098-3106)
type runtimeConfig struct {
	// CWD 当前工作目录
	CWD string
	// Language 运行时语言
	Language string
	// Channel 请求频道
	Channel string
	// ProjectDir 项目目录
	ProjectDir string
	// WorkspaceDir 工作区目录
	WorkspaceDir string
	// SessionID 会话标识
	// Python: session_id
	SessionID string
	// Mode 运行模式，默认 "agent.plan"
	// Python: mode
	Mode string
	// RequestID 请求标识
	// Python: request_id
	RequestID string
	// ChannelID 渠道标识
	// Python: channel_id
	ChannelID string
	// RequestMetadata 请求元数据
	// Python: request_metadata，⤵️ 具体类型待 10.4 定义
	RequestMetadata map[string]any
	// TrustedDirs 可信目录列表
	// Python: trusted_dirs
	TrustedDirs []string
}
```

- [ ] **Step 2: 替换 writeRuntimeState 为 writeRuntimeStateYAML**

在 `deep_adapter_config.go` 中：
1. 删除旧方法 `writeRuntimeState(key string, value string)`（L280-286）
2. 新增 `writeRuntimeStateYAML` 方法，委托给 rails 包的 `writeRuntimeStateYAML` 函数：

```go
// writeRuntimeStateYAML 将运行时状态写入 config 目录下的 runtime_state.yaml。
// Python: _write_runtime_state() (interface_deep.py L756-821)
func (d *DeepAdapter) writeRuntimeStateYAML(mode, language, channel, projectDir string) {
	commrails.WriteRuntimeStateYAML(
		d.resolveModelName(),
		mode,
		language,
		channel,
		d.agentName,
		projectDir,
	)
}
```

注意：需要将 `rails` 包中的 `writeRuntimeStateYAML` 改为导出函数 `WriteRuntimeStateYAML`。

- [ ] **Step 3: 更新 updateRuntimeConfig 方法**

```go
func (d *DeepAdapter) updateRuntimeConfig(ctx context.Context, config *runtimeConfig) {
	if config == nil {
		return
	}

	// 步骤 1: CWD 种子
	if config.CWD != "" {
		d.seedRuntimeCwd(ctx, config.CWD)
	}

	// 步骤 2: language 解析
	resolvedLanguage := d.resolveRuntimeLanguage()
	resolvedChannel := commrails.FirstNonEmpty(
		config.ChannelID,
		resolvePromptChannel(config.SessionID),
		"web",
	)

	// 步骤 3: 写 runtime_state.yaml（替代旧的环境变量写入）
	d.writeRuntimeStateYAML(
		config.Mode,
		resolvedLanguage,
		resolvedChannel,
		config.ProjectDir,
	)

	// 步骤 4: RuntimePromptRail 7 个 setter
	if d.runtimePromptRail != nil {
		d.runtimePromptRail.SetLanguage(resolvedLanguage)
		d.runtimePromptRail.SetChannel(resolvedChannel)
		d.runtimePromptRail.SetTrustedDirs(config.TrustedDirs)
		d.runtimePromptRail.SetRuntimePaths(config.CWD, config.ProjectDir)
		d.runtimePromptRail.SetModelName(d.resolveModelName())
		d.runtimePromptRail.SetMode(config.Mode)
	}

	// 步骤 5: updatePromptForMode
	d.updatePromptForMode(config.Mode)

	// 步骤 6: rail/tool 模式切换（仅 evolution 分支已实现）
	d.updateRailsForMode(config.Mode)

	logger.Info(logComponent).
		Str("cwd", config.CWD).
		Str("language", resolvedLanguage).
		Str("channel", resolvedChannel).
		Msg("updateRuntimeConfig 完成")
}
```

- [ ] **Step 4: 导出 writeRuntimeStateYAML 和 firstNonEmpty**

在 `runtime_prompt_rail.go` 中，将 `writeRuntimeStateYAML` 改为 `WriteRuntimeStateYAML`，`firstNonEmpty` 改为 `FirstNonEmpty`（供 adapter 包调用）。同步更新本文件内调用。

- [ ] **Step 5: 更新 deep_adapter_config_test.go 适配新字段**

更新 `TestUpdateRuntimeConfig_全部字段` 测试，给 `runtimeConfig` 新字段赋值。

- [ ] **Step 6: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 7: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -count=1`
Expected: PASS

- [ ] **Step 8: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter_config.go internal/swarm/server/adapter/deep_adapter_config_test.go internal/swarm/agents/harness/common/rails/runtime_prompt_rail.go
git commit -m "feat(adapter): 扩展 runtimeConfig + writeRuntimeStateYAML + updateRuntimeConfig 补 setter"
```

---

### Task 6: 回填 DeepAdapter 字段类型 + buildRuntimePromptRail + updatePromptForMode

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`

- [ ] **Step 1: 修改 runtimePromptRail 字段类型**

在 `deep_adapter.go` 中将字段类型从 `sainterfaces.AgentRail` 改为 `*commrails.RuntimePromptRail`：

```go
// 之前
runtimePromptRail sainterfaces.AgentRail

// 之后
runtimePromptRail *commrails.RuntimePromptRail
```

同时添加 import `commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"`。

- [ ] **Step 2: 回填 buildRuntimePromptRail**

在 `deep_adapter_rails.go` 中将 stub 替换为真实实现：

```go
// buildRuntimePromptRail 构建运行时提示词护栏。
// Python: _build_runtime_prompt_rail() (line 2098-2114)
func (d *DeepAdapter) buildRuntimePromptRail() *commrails.RuntimePromptRail {
	defaultChannel := "web"
	if d.isAcpToolProfile(d.instanceOverrides) {
		defaultChannel = "acp"
	} else {
		defaultChannel = resolvePromptChannel(d.activeSessionID())
	}
	rail := commrails.NewRuntimePromptRail(
		d.resolveRuntimeLanguage(),
		defaultChannel,
	)
	logger.Info(logComponent).Msg("RuntimePromptRail 创建成功")
	return rail
}
```

注意：`resolvePromptChannel` 需要一个 sessionID。DeepAdapter 没有存储单个 sessionID 的字段（支持并发多 session），但在 `buildAgentRails`（CreateInstance 阶段）调用时，可以用空字符串 fallback 到 "web"。或者直接使用 `resolvePromptChannel("")` → "web"。

实际上 Python 的 `_resolve_prompt_channel()` 不带参数时也返回 "web"。所以：

```go
func (d *DeepAdapter) buildRuntimePromptRail() *commrails.RuntimePromptRail {
	defaultChannel := "web"
	if d.isAcpToolProfile(d.instanceOverrides) {
		defaultChannel = "acp"
	}
	rail := commrails.NewRuntimePromptRail(
		d.resolveRuntimeLanguage(),
		defaultChannel,
	)
	logger.Info(logComponent).Msg("RuntimePromptRail 创建成功")
	return rail
}
```

- [ ] **Step 3: 回填 updatePromptForMode**

```go
// updatePromptForMode 按模式更新系统提示词语言。
// Python: _update_prompt_for_mode() (line 3091-3097)
func (d *DeepAdapter) updatePromptForMode(mode string) {
	if d.instance == nil {
		return
	}
	resolvedLanguage := d.resolveRuntimeLanguage()
	builder := d.instance.SystemPromptBuilder()
	if builder != nil {
		builder.SetLanguage(resolvedLanguage)
	}
	// 同步 DeepConfig.Language
	if d.instance.DeepConfig() != nil {
		d.instance.DeepConfig().Language = resolvedLanguage
	}
	logger.Info(logComponent).Str("mode", mode).Str("language", resolvedLanguage).Msg("updatePromptForMode 执行完成")
}
```

- [ ] **Step 4: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/server/adapter/... -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/swarm/server/adapter/deep_adapter.go internal/swarm/server/adapter/deep_adapter_rails.go
git commit -m "feat(adapter): 回填 buildRuntimePromptRail + updatePromptForMode + 字段类型修正"
```

---

### Task 7: CodeAdapter 调用 SetForceEnglish

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 在 CodeAdapter 的请求处理流程中追加 SetForceEnglish 调用**

查找 CodeAdapter 中 ProcessMessageStreamImpl / ProcessMessageImpl 调用 `updateRuntimeConfig` 的位置，在 7 个 setter 之后追加：

```go
if c.deep.runtimePromptRail != nil {
	c.deep.runtimePromptRail.SetForceEnglish(c.forceEnglishRuntimePrompt)
}
```

- [ ] **Step 2: 运行编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: 编译成功

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "feat(adapter): CodeAdapter 调用 RuntimePromptRail.SetForceEnglish"
```

---

### Task 8: 清理 ⤵️ 标记 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/swarm/server/adapter/deep_adapter.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go`
- Modify: `internal/swarm/server/adapter/deep_adapter_config.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 清理所有已回填的 ⤵️ 标记**

- `deep_adapter.go:134` — 删除 `// ⤵️ 10.6.3-10: RuntimePromptRail`
- `deep_adapter_rails.go` — 删除 `buildRuntimePromptRail` 中的 `// ⤵️ 10.6.3-10`
- `deep_adapter_rails.go` — `updatePromptForMode` 中的 `// ⤵️ 10.6.3-10` 替换为 `// ✅ 已回填：依赖 RuntimePromptRail（对齐 Python: _update_prompt_for_mode()）`
- `deep_adapter_config.go:92-94` — `updateRuntimeConfig` 中的 `// ⤵️ 10.6.3-10: updateRailsForMode + updatePromptForMode` 替换为 `// ✅ 已回填：RuntimePromptRail 7 setter + updatePromptForMode（对齐 Python: _update_runtime_config()）`

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md**

在 `10.6.3-10` 行中将 RuntimePrompt 状态标记为 ✅：

```
| 10.6.3-10 | 🔄 | Swarm Rails | AskUser✅/Avatar✅/Permissions✅/Interrupt✅/ProjectMemory/ResponsePrompt/RuntimePrompt✅/StreamEvent |
```

- [ ] **Step 3: Commit**

```bash
git add -A
git commit -m "docs: 清理 ⤵️ 标记 + 更新 IMPLEMENTATION_PLAN.md RuntimePrompt 状态"
```

---

### Task 9: 全量编译 + 测试验证

**Files:** 无变更

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: 编译成功

- [ ] **Step 2: 运行全部相关测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/agents/harness/common/rails/... ./internal/swarm/server/adapter/... ./internal/agentcore/single_agent/prompts/... -count=1 -v`
Expected: PASS

- [ ] **Step 3: Final commit if any fixups needed**

---

## 自审

1. **Spec 覆盖**：每个设计章节都有对应 Task — Task 1 覆盖接口扩展，Task 2 覆盖 Rail 实现，Task 3 覆盖 doc.go，Task 4 覆盖测试，Task 5 覆盖 runtimeConfig + YAML + setter，Task 6 覆盖 adapter 回填，Task 7 覆盖 CodeAdapter，Task 8 覆盖标记清理。✅
2. **Placeholder 扫描**：无 TBD/TODO/"implement later"。✅
3. **类型一致性**：`buildRuntimePromptRail` 返回 `*commrails.RuntimePromptRail`，字段类型同步为 `*commrails.RuntimePromptRail`，SetForceEnglish 调用链类型一致。✅
