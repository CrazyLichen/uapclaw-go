package rails

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"

	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails/project_memory"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ProjectMemoryRail 项目记忆护栏 — 在 before_model_call 中从缓存发现结果重建
// project_memory section。
//
// 缓存失效发生在写操作工具调用、模式/工作空间切换时，
// 同时退回到轻量级文件系统快照检查以保证正确性。
//
// 本 Rail 位于 uapclawswarm（非 agent-core），以便：
//   - agent-core 保持不变
//   - uapclawswarm 无需上游 PR 即可演进 memory-loading 语义
//
// Python: ProjectMemoryRail(DeepAgentRail) — project_memory_rail.py (222 行)
type ProjectMemoryRail struct {
	rails.DeepAgentRail
	// workspacePath 构造期传入的工作空间路径（私有，避免被 SetWorkspace 覆盖）
	// Python: self._workspace_path
	workspacePath string
	// language 每次请求的语言
	// Python: self._language
	language string
	// maxChars 最大字符数
	// Python: self._max_chars
	maxChars int
	// additionalDirectories 额外扫描目录
	// Python: self._additional_directories
	additionalDirectories []string
	// systemPromptBuilder 系统提示词构建器引用
	// Python: self._system_prompt_builder
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// sectionPriority ProjectMemory section 优先级。
	// 高于 MEMORY(85) / TOOLS(~100)；低于 RUNTIME（参见
	// agent-core prompts/sections/__init__.py SectionName 常规范围）。
	// Python: SECTION_PRIORITY = 120
	sectionPriority = 120
)

// ──────────────────────────── 全局变量 ────────────────────────────

// writeLikeTools 写操作工具名称集合。
// Python: ProjectMemoryRail.WRITE_LIKE_TOOLS (frozenset)
var writeLikeTools = map[string]struct{}{
	"write_file":      {},
	"edit_file":       {},
	"write_text_file": {},
	"write":           {},
	"delete_file":     {},
	"delete":          {},
	"move_file":       {},
	"rename_file":     {},
}

// pmrLogComponent 日志组件标识
var pmrLogComponent = logger.ComponentAgentServer

// 编译时验证 ProjectMemoryRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*ProjectMemoryRail)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewProjectMemoryRail 创建 ProjectMemoryRail 实例。
//
// Python: ProjectMemoryRail.__init__(workspace, language="cn", max_chars=60000, additional_directories=None)
func NewProjectMemoryRail(workspace string, language string, maxChars int, additionalDirectories []string) *ProjectMemoryRail {
	if language == "" {
		language = "cn"
	}
	if maxChars <= 0 {
		maxChars = project_memory.DefaultMaxChars
	}
	if additionalDirectories == nil {
		additionalDirectories = []string{}
	}
	r := &ProjectMemoryRail{
		workspacePath:         workspace,
		language:              language,
		maxChars:              maxChars,
		additionalDirectories: additionalDirectories,
	}
	return r
}

// Init Rail 初始化钩子。
// Python: ProjectMemoryRail.init(agent)
func (r *ProjectMemoryRail) Init(_ context.Context, agent agentinterfaces.BaseAgent) error {
	// Python: self._system_prompt_builder = getattr(agent, "system_prompt_builder", None)
	if agent != nil {
		r.systemPromptBuilder = agent.SystemPromptBuilder()
	}
	if r.systemPromptBuilder == nil {
		logger.Warn(pmrLogComponent).
			Str("event_type", "ProjectMemoryRail_init_no_builder").
			Msg("agent 没有 system_prompt_builder；ProjectMemoryRail 禁用")
		return nil
	}
	logger.Info(pmrLogComponent).
		Str("event_type", "ProjectMemoryRail_init").
		Str("workspace", r.ResolveWorkspacePath()).
		Str("language", r.language).
		Msg("ProjectMemoryRail 初始化完成")
	return nil
}

// Uninit Rail 注销钩子，清除注入的 section 以避免 rail 切换后残留。
// Python: ProjectMemoryRail.uninit(agent)
func (r *ProjectMemoryRail) Uninit(_ agentinterfaces.BaseAgent) error {
	// Python: clear_project_memory_cache(self.resolve_workspace_path())
	project_memory.ClearProjectMemoryCache(r.ResolveWorkspacePath())

	if r.systemPromptBuilder != nil {
		// Python: self._system_prompt_builder.remove_section(SECTION_NAME)
		// 防御性：teardown 永远不崩溃
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(pmrLogComponent).
						Str("event_type", "ProjectMemoryRail_uninit_remove_section_failed").
						Str("section", project_memory.SectionName).
						Any("recover", rec).
						Msg("uninit 时 remove_section 失败")
				}
			}()
			r.systemPromptBuilder.RemoveSection(project_memory.SectionName)
		}()
	}
	return nil
}

// SetLanguage per-request 更新语言。
// Python: ProjectMemoryRail.set_language(language)
func (r *ProjectMemoryRail) SetLanguage(language string) {
	// Python: if language and language != self._language: self._language = language
	if language != "" && language != r.language {
		r.language = language
	}
}

// GetLanguage 获取当前语言设置。
// Python: ProjectMemoryRail.get_language()
func (r *ProjectMemoryRail) GetLanguage() string {
	return r.language
}

// SetAdditionalDirectories per-request 热更新额外扫描目录。
//
// 由 adapter 在 trusted_dirs 到达时调用，
// 确保 rail 始终搜索 /init 写 UAPCLAWSWARM.md 的目录
// （通常是 CLI 进程的 cwd，与 AgentServer 进程 cwd 不同）。
//
// Python: ProjectMemoryRail.set_additional_directories(dirs)
func (r *ProjectMemoryRail) SetAdditionalDirectories(dirs []string) {
	extra := dirs
	if extra == nil {
		extra = []string{}
	}
	// Python: base_resolved = {os.path.realpath(d) for d in self._additional_directories}
	baseResolved := make(map[string]struct{})
	for _, d := range r.additionalDirectories {
		absPath, absErr := filepath.Abs(d)
		resolved := d
		if absErr == nil {
			if evRes, evErr := filepath.EvalSymlinks(absPath); evErr == nil {
				resolved = evRes
			} else {
				resolved = absPath // 降级：EvalSymlinks 失败时使用 Abs 路径
			}
		}
		baseResolved[resolved] = struct{}{}
	}
	// Python: merged = list(self._additional_directories)
	merged := make([]string, len(r.additionalDirectories))
	copy(merged, r.additionalDirectories)

	// Python: for d in extra: if os.path.realpath(d) not in base_resolved: merged.append(d)
	for _, d := range extra {
		d = strings.TrimSpace(d)
		if d == "" {
			continue
		}
		absPath, absErr := filepath.Abs(d)
		resolved := d
		if absErr == nil {
			if evRes, evErr := filepath.EvalSymlinks(absPath); evErr == nil {
				resolved = evRes
			} else {
				resolved = absPath // 降级：EvalSymlinks 失败时使用 Abs 路径
			}
		}
		if _, exists := baseResolved[resolved]; !exists {
			merged = append(merged, d)
			baseResolved[resolved] = struct{}{}
		}
	}
	r.additionalDirectories = merged
}

// BeforeModelCall 模型调用前，从缓存发现结果刷新 project_memory section。
// Python: ProjectMemoryRail.before_model_call(ctx)
func (r *ProjectMemoryRail) BeforeModelCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	workspacePath := r.ResolveWorkspacePath()

	// Python: try/except (OSError, ValueError, TypeError) — discovery 失败时优雅降级
	// Go 版本增加 defer/recover 保护，防止 DiscoverAndLoadMemoryFiles 内部 panic 导致 BeforeModelCall 崩溃
	var files []project_memory.LoadedMemoryFile
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(pmrLogComponent).
					Str("event_type", "ProjectMemoryRail_discovery_failed").
					Str("workspace", workspacePath).
					Any("recover", rec).
					Msg("DiscoverAndLoadMemoryFiles panic，降级为空文件列表")
			}
		}()
		files = project_memory.DiscoverAndLoadMemoryFiles(
			workspacePath,
			workspacePath, // target_path = workspace（对齐 Python: paths: scoped rules evaluated against active workspace/cwd）
			r.additionalDirectories,
		)
	}()

	merged := project_memory.MergeMemoryContent(files, r.maxChars)

	// Python: self._system_prompt_builder.remove_section(SECTION_NAME)
	func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Warn(pmrLogComponent).
					Str("event_type", "ProjectMemoryRail_remove_section_failed").
					Str("section", project_memory.SectionName).
					Any("recover", rec).
					Msg("remove_section 失败（继续）")
			}
		}()
		r.systemPromptBuilder.RemoveSection(project_memory.SectionName)
	}()

	// Python: if not merged.strip(): return
	if strings.TrimSpace(merged) == "" {
		return nil
	}

	// Python: section = build_project_memory_section(merged, language=self._language, priority=self.SECTION_PRIORITY)
	section := project_memory.BuildProjectMemorySection(merged, sectionPriority)
	if section != nil {
		// Python: self._system_prompt_builder.add_section(section)
		r.systemPromptBuilder.AddSection(*section)
	}

	return nil
}

// AfterToolCall 写操作工具调用后，显式失效记忆发现缓存。
// Python: ProjectMemoryRail.after_tool_call(ctx)
func (r *ProjectMemoryRail) AfterToolCall(_ context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// Python: tool_name = str(getattr(ctx.inputs, "tool_name", "") or "").strip()
	if cbc == nil || cbc.Inputs() == nil {
		return nil
	}
	toolName := ""
	if tc, ok := cbc.Inputs().(*agentinterfaces.ToolCallInputs); ok {
		toolName = strings.TrimSpace(tc.ToolName)
	}
	if toolName == "" {
		return nil
	}

	// Python: if tool_name not in self.WRITE_LIKE_TOOLS: return
	if _, isWriteLike := writeLikeTools[toolName]; !isWriteLike {
		return nil
	}

	// Python: clear_project_memory_cache(self.resolve_workspace_path())
	project_memory.ClearProjectMemoryCache(r.ResolveWorkspacePath())
	return nil
}

// ResolveWorkspacePath 解析工作空间根路径为字符串。
//
// 优先级：
//  1. self.Workspace().RootPath — DeepAgent.register_rail 通过
//     DeepAgentRail.SetWorkspace(Workspace(...)) 注入。最新。
//  2. self.workspacePath — 构造期传入的字符串路径。
//
// Python: ProjectMemoryRail.resolve_workspace_path()
func (r *ProjectMemoryRail) ResolveWorkspacePath() string {
	// Python: ws_obj = getattr(self, "workspace", None)
	ws := r.Workspace()
	if ws != nil {
		// Python: root = getattr(ws_obj, "root_path", None); if root: return str(root)
		if ws.RootPath != "" {
			return ws.RootPath
		}
	}
	// Python: return self._workspace_path
	return r.workspacePath
}

// GetCallbacks 覆写基类回调映射，注册 BeforeModelCall + AfterToolCall。
func (r *ProjectMemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackAfterToolCall] = func(ctx context.Context, railCtx any) error {
		return r.AfterToolCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// safeResolveDir 安全解析目录路径，不存在返回空字符串。
func safeResolveDir(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	info, err := os.Stat(abs)
	if err != nil || !info.IsDir() {
		return ""
	}
	return abs
}
