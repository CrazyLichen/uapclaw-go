package memory

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	cmt "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/coding_memory"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	cb "github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	agentinterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryRail 编程记忆护栏，面向 Coding Agent 场景。
//
// 特性:
//  1. 自动召回: 每个 user turn 启动 goroutine 预取，非阻塞
//  2. 互斥注入: 有召回结果 → 注入 top5 全文; 无结果 → 降级注入 MEMORY.md 索引
//  3. 数据隔离: coding_memory/ 目录与 personal memory/ 完全隔离
//
// 对齐 Python: CodingMemoryRail (openjiuwen/harness/rails/memory/coding_memory_rail.py)
type CodingMemoryRail struct {
	rails.DeepAgentRail
	// codingMemoryDir 编程记忆目录路径
	codingMemoryDir string
	// embeddingConfig 嵌入模型配置
	embeddingConfig *embedding.EmbeddingConfig
	// language 语言 ("cn" | "en")
	language string
	// manager 记忆索引管理器
	manager lite.MemoryIndexManager
	// managerInitialized 管理器是否已初始化
	managerInitialized bool
	// toolCtx 编程记忆工具上下文
	toolCtx *lite.CodingMemoryToolContext
	// systemPromptBuilder 系统提示词构建器引用
	systemPromptBuilder saprompt.SystemPromptBuilderInterface
	// agentID Agent 标识
	agentID string
	// recallResult 召回结果内容
	recallResult *recallResult
	// recallDone 预取完成信号通道
	recallDone chan struct{}
	// ownedToolNames 本 Rail 注册到 ability_manager 的工具名称集合
	ownedToolNames map[string]struct{}
	// ownedToolIDs 本 Rail 注册到 resource_mgr 的工具 ID 集合
	ownedToolIDs map[string]struct{}
}

// recallResult 自动召回结果
type recallResult struct {
	// content 召回内容文本
	content string
	// totalMemories 总记忆数
	totalMemories int
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// codingMemoryRailPriority CodingMemoryRail 优先级，与 MemoryRail 相同
	codingMemoryRailPriority = 80

	// maxRecallResults 最多召回 5 条记忆
	maxRecallResults = 5

	// maxRecallTotalBytes 召回内容总大小上限 10KB
	maxRecallTotalBytes = 10240

	// memoryIndexMaxLines MEMORY.md 索引最大读取行数
	memoryIndexMaxLines = 200
)

// ──────────────────────────── 全局变量 ────────────────────────────

// 编译时验证 CodingMemoryRail 满足 AgentRail 接口
var _ agentinterfaces.AgentRail = (*CodingMemoryRail)(nil)

// codingMemoryLogComponent 日志组件标识
var codingMemoryLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 导出函数 ────────────────────────────

// NewCodingMemoryRail 创建 CodingMemoryRail 实例。
//
// 对齐 Python: CodingMemoryRail.__init__(coding_memory_dir, embedding_config, language)
func NewCodingMemoryRail(codingMemoryDir string, embeddingConfig *embedding.EmbeddingConfig, language string) *CodingMemoryRail {
	r := &CodingMemoryRail{
		DeepAgentRail:   *rails.NewDeepAgentRail(),
		codingMemoryDir: codingMemoryDir,
		embeddingConfig: embeddingConfig,
		language:        language,
		ownedToolNames:  make(map[string]struct{}),
		ownedToolIDs:    make(map[string]struct{}),
	}
	r.WithPriority(codingMemoryRailPriority)
	return r
}

// Init 注册编程记忆工具到 agent。
//
// 对齐 Python: CodingMemoryRail.init(agent)
func (r *CodingMemoryRail) Init(agent agentinterfaces.BaseAgent) error {
	// 获取 systemPromptBuilder
	r.systemPromptBuilder = agent.SystemPromptBuilder()

	// 保存 agentID
	r.agentID = ""
	if card := agent.Card(); card != nil {
		r.agentID = card.ID
	}

	// 注册工具
	r.registerCodingMemoryTools(agent)

	return nil
}

// Uninit 注销编程记忆工具。
//
// 对齐 Python: CodingMemoryRail.uninit(agent)
func (r *CodingMemoryRail) Uninit(agent agentinterfaces.BaseAgent) error {
	// 从 ability_manager 移除工具
	am := agent.AbilityManager()
	if am != nil {
		for name := range r.ownedToolNames {
			func(name string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(codingMemoryLogComponent).
							Str("event_type", "coding_memory_rail_uninit").
							Str("tool_name", name).
							Msgf("从 ability_manager 移除工具失败: %v", rec)
					}
				}()
				am.Remove(name)
			}(name)
		}
	}

	// 从 resource_mgr 移除工具
	resourceMgr := runner.GetResourceMgr()
	if resourceMgr != nil {
		for toolID := range r.ownedToolIDs {
			func(toolID string) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(codingMemoryLogComponent).
							Str("event_type", "coding_memory_rail_uninit").
							Str("tool_id", toolID).
							Msgf("从 resource_mgr 移除工具失败: %v", rec)
					}
				}()
				_, _ = resourceMgr.RemoveTool([]string{toolID})
			}(toolID)
		}
	}

	// 清理状态
	r.ownedToolNames = make(map[string]struct{})
	r.ownedToolIDs = make(map[string]struct{})
	r.managerInitialized = false
	r.manager = nil
	r.toolCtx = nil

	// 从 systemPromptBuilder 移除 memory section
	if r.systemPromptBuilder != nil {
		r.systemPromptBuilder.RemoveSection(sections.SectionMemory)
		r.systemPromptBuilder = nil
	}

	logger.Info(codingMemoryLogComponent).
		Str("event_type", "coding_memory_rail_uninit").
		Msg("CodingMemoryRail 注销完成")

	return nil
}

// BeforeInvoke Invoke 开始前调用。
//
// 1. 初始化 MemoryIndexManager（首次）
// 2. 启动预取 goroutine（非阻塞）
//
// 对齐 Python: CodingMemoryRail.before_invoke(ctx)
func (r *CodingMemoryRail) BeforeInvoke(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	// 初始化 Coding Memory Manager（首次）
	// Go 行为差异：仅在初始化成功时设置 managerInitialized = true，失败后每次 BeforeInvoke 重试。
	// Python 行为：无条件设置 _manager_initialized = True，失败后不再重试。
	// Go 的重试策略更合理（初始化失败可能是临时问题），保留当前行为。
	if !r.managerInitialized {
		r.initCodingMemoryManager(ctx)
	}

	// 重置召回状态
	r.recallResult = nil
	r.recallDone = make(chan struct{}, 1)

	// 检查是否为只读模式（cron/heartbeat）
	readOnly := r.isReadOnly(cbc)

	// 启动预取 goroutine（非阻塞，与主流程并行）
	if !readOnly && r.manager != nil {
		query := r.extractLastUserQuery(cbc)
		if query != "" {
			go r.autoRecall(ctx, query)
		} else {
			// 无查询，直接标记完成
			r.recallDone <- struct{}{}
		}
	} else {
		// 只读或无 manager，直接标记完成
		r.recallDone <- struct{}{}
	}

	logger.Info(codingMemoryLogComponent).
		Str("event_type", "coding_memory_rail_before_invoke").
		Bool("read_only", readOnly).
		Bool("has_manager", r.manager != nil).
		Msg("CodingMemoryRail BeforeInvoke 完成")

	return nil
}

// BeforeModelCall Model Call 前调用。
//
// 1. 注入行为指令 prompt
// 2. 非阻塞检查预取 goroutine 结果
// 3. 互斥注入: 有召回结果 → 注入全文; 无结果 → 降级注入索引
//
// 对齐 Python: CodingMemoryRail.before_model_call(ctx)
func (r *CodingMemoryRail) BeforeModelCall(ctx context.Context, cbc *agentinterfaces.AgentCallbackContext) error {
	if r.systemPromptBuilder == nil {
		return nil
	}

	// 移除旧的 memory section
	r.systemPromptBuilder.RemoveSection(sections.SectionMemory)

	lang := r.systemPromptBuilder.Language()
	if lang == "" {
		lang = r.language
	}

	// 检查只读模式
	readOnly := r.isReadOnly(cbc)

	// 构建基础 section（行为指令）
	section := sections.BuildCodingMemorySection(r.codingMemoryDir, readOnly, lang)

	// 只读模式: 仅注入 MEMORY.md 索引
	if readOnly {
		index := r.readMemoryIndex(ctx)
		if index != "" {
			header := "## 当前记忆索引\n\n"
			if lang == "en" {
				header = "## Current memory index\n\n"
			}
			section.Content[lang] += "\n\n" + header + index
		}
		r.systemPromptBuilder.AddSection(section)
		return nil
	}

	// 非阻塞检查预取结果
	if r.recallResult == nil {
		select {
		case <-r.recallDone:
			// 预取完成，recallResult 已在 autoRecall 中设置
		default:
			// 预取未完成，本次降级，下次 model call 再检查
		}
	}

	// 互斥注入: 召回结果 vs 索引
	if r.recallResult != nil && r.recallResult.content != "" {
		// 有召回结果 → 注入全文
		header := "## 已加载的相关记忆\n\n"
		if lang == "en" {
			header = "## Loaded relevant memories\n\n"
		}
		footer := fmt.Sprintf(
			"\n\n（共 %d 条记忆，用 coding_memory_read 读取其他。）",
			r.recallResult.totalMemories,
		)
		if lang == "en" {
			footer = fmt.Sprintf(
				"\n\n(%d total. Use coding_memory_read for others.)",
				r.recallResult.totalMemories,
			)
		}
		section.Content[lang] += "\n\n" + header + r.recallResult.content + footer
	} else {
		// 无召回结果 → 降级注入索引
		index := r.readMemoryIndex(ctx)
		if index != "" {
			header := "## 当前记忆索引\n\n"
			if lang == "en" {
				header = "## Current memory index\n\n"
			}
			section.Content[lang] += "\n\n" + header + index
		}
	}

	r.systemPromptBuilder.AddSection(section)

	logger.Info(codingMemoryLogComponent).
		Str("event_type", "coding_memory_rail_before_model_call").
		Bool("has_recall", r.recallResult != nil && r.recallResult.content != "").
		Msg("CodingMemoryRail BeforeModelCall 完成")

	return nil
}

// GetCallbacks 覆盖基类回调映射，增加 BeforeInvoke/BeforeModelCall。
//
// 对齐 Python: CodingMemoryRail 隐式覆盖 before_invoke/before_model_call
// 注意: Init/Uninit 是 AgentRail 接口方法，由框架直接调用，不是回调事件
func (r *CodingMemoryRail) GetCallbacks() map[agentinterfaces.AgentCallbackEvent]cb.PerAgentCallbackFunc {
	callbacks := r.DeepAgentRail.GetCallbacks()

	callbacks[agentinterfaces.CallbackBeforeInvoke] = func(ctx context.Context, railCtx any) error {
		return r.BeforeInvoke(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}
	callbacks[agentinterfaces.CallbackBeforeModelCall] = func(ctx context.Context, railCtx any) error {
		return r.BeforeModelCall(ctx, railCtx.(*agentinterfaces.AgentCallbackContext))
	}

	return callbacks
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// registerCodingMemoryTools 注册编程记忆工具到 agent。
//
// 对齐 Python: CodingMemoryRail._register_coding_memory_tools(agent)
func (r *CodingMemoryRail) registerCodingMemoryTools(agent agentinterfaces.BaseAgent) {
	am := agent.AbilityManager()
	if am == nil {
		logger.Warn(codingMemoryLogComponent).
			Str("event_type", "coding_memory_rail_register_tools").
			Msg("Agent 无 ability_manager，跳过工具注册")
		return
	}

	agentID := r.agentID
	if agentID == "" {
		agentID = "default"
	}

	// 获取 language
	language := r.language
	if r.systemPromptBuilder != nil {
		lang := r.systemPromptBuilder.Language()
		if lang != "" {
			language = lang
		}
	}

	// 创建 CodingMemoryToolContext
	memoryDir := ""
	if r.Workspace() != nil {
		if nodePath := r.Workspace().GetNodePath("coding_memory"); nodePath != nil {
			memoryDir = *nodePath
		}
	}
	settings := lite.CreateMemorySettings(memoryDir, nil)

	r.toolCtx = lite.NewCodingMemoryToolContext().
		WithWorkspace(r.Workspace()).
		WithSettings(settings).
		WithAgentID(agentID).
		WithEmbeddingConfig(r.embeddingConfig).
		WithSysOperation(r.SysOperation()).
		WithCodingMemoryDir("coding_memory")

	// 创建工具
	memoryTools := cmt.CreateCodingMemoryTools(r.toolCtx, language, agentID)

	// 对齐 Python 注释: create_coding_memory_tools 会覆盖 ctx.coding_memory_dir，
	// 恢复构造时传入的绝对路径
	if r.toolCtx != nil && r.codingMemoryDir != "" {
		r.toolCtx.CodingMemoryDir = r.codingMemoryDir
	}

	// 注册到 resource_mgr 和 ability_manager
	resourceMgr := runner.GetResourceMgr()
	for _, t := range memoryTools {
		toolCard := t.Card()
		if toolCard == nil {
			logger.Warn(codingMemoryLogComponent).
				Str("event_type", "coding_memory_rail_register_tools").
				Msg("tool has no card, skipping registration")
			continue
		}

		// 注册到 resource_mgr
		if resourceMgr != nil {
			func(t tool.Tool) {
				defer func() {
					if rec := recover(); rec != nil {
						logger.Warn(codingMemoryLogComponent).
							Str("event_type", "coding_memory_rail_register_tools").
							Str("tool_id", t.Card().ID).
							Msgf("注册工具到 resource_mgr 失败: %v", rec)
					}
				}()
				existing, err := resourceMgr.GetTool([]string{t.Card().ID})
				if err != nil || len(existing) == 0 {
					if addErr := resourceMgr.AddTool(t); addErr != nil {
						logger.Warn(codingMemoryLogComponent).
							Str("event_type", "coding_memory_rail_register_tools").
							Str("tool_id", t.Card().ID).
							Err(addErr).
							Msg("注册工具到 resource_mgr 失败")
					} else {
						r.ownedToolIDs[t.Card().ID] = struct{}{}
					}
				}
			}(t)
		}

		// 注册到 ability_manager
		func(t tool.Tool) {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(codingMemoryLogComponent).
						Str("event_type", "coding_memory_rail_register_tools").
						Str("tool_name", t.Card().Name).
						Msgf("注册工具到 ability_manager 失败: %v", rec)
				}
			}()
			result := am.Add(t.Card())
			if result.Added {
				r.ownedToolNames[t.Card().Name] = struct{}{}
				logger.Info(codingMemoryLogComponent).
					Str("event_type", "coding_memory_rail_register_tools").
					Str("tool_name", t.Card().Name).
					Msg("注册工具成功")
			}
		}(t)
	}

	logger.Info(codingMemoryLogComponent).
		Str("event_type", "coding_memory_rail_register_tools").
		Strs("tool_names", memorySetToSortedSlice(r.ownedToolNames)).
		Msg("CodingMemoryRail 工具注册完成")
}

// initCodingMemoryManager 初始化 Coding Memory Index Manager。
//
// 对齐 Python: CodingMemoryRail._init_coding_memory_manager(ctx)
func (r *CodingMemoryRail) initCodingMemoryManager(ctx context.Context) {
	agentID := r.agentID
	if agentID == "" {
		agentID = "default"
	}

	// 无 workspace 时无法初始化
	if r.Workspace() == nil {
		logger.Warn(codingMemoryLogComponent).
			Str("event_type", "coding_memory_rail_init_manager").
			Msg("Workspace 为 nil，跳过管理器初始化")
		return
	}

	mgr, err := lite.InitCodingMemoryManagerAsync(
		ctx,
		r.Workspace(),
		agentID,
		r.embeddingConfig,
		r.SysOperation(),
		nil, // ⤵️ 7.8: LLM 实例待 MemUpdateChecker 实现后传入
	)
	if err != nil {
		logger.Error(codingMemoryLogComponent).
			Str("event_type", "coding_memory_rail_init_manager").
			Str("agent_id", agentID).
			Err(err).
			Msg("初始化 Coding Memory 管理器失败")
		return
	}

	if mgr != nil {
		r.manager = mgr
		r.managerInitialized = true
		if r.toolCtx != nil {
			r.toolCtx.Manager = mgr
		}
		logger.Info(codingMemoryLogComponent).
			Str("event_type", "coding_memory_rail_init_manager").
			Str("agent_id", agentID).
			Str("dir", r.codingMemoryDir).
			Msg("Coding Memory 管理器初始化成功")
	} else {
		logger.Warn(codingMemoryLogComponent).
			Str("event_type", "coding_memory_rail_init_manager").
			Msg("Coding Memory 管理器初始化返回 nil")
	}
}

// autoRecall 自动召回相关记忆。
//
// 只注入 body（不含 frontmatter），避免浪费 token。
// 标题附加时间标注辅助模型判断记忆时效性。
//
// 对齐 Python: CodingMemoryRail._auto_recall(query)
func (r *CodingMemoryRail) autoRecall(ctx context.Context, query string) {
	defer func() {
		// 确保通知完成
		select {
		case r.recallDone <- struct{}{}:
		default:
		}
	}()

	if r.manager == nil {
		return
	}

	// 执行混合检索
	opts := map[string]any{
		"max_results": maxRecallResults,
	}
	results, err := r.manager.Search(ctx, query, opts)
	if err != nil {
		logger.Warn(codingMemoryLogComponent).
			Str("event_type", "coding_memory_rail_auto_recall").
			Err(err).
			Msg("搜索记忆失败")
		return
	}

	// 统计总记忆数
	total := r.countMemoryFiles(ctx)

	if len(results) == 0 {
		r.recallResult = &recallResult{content: "", totalMemories: total}
		return
	}

	// 组装召回内容
	var parts []string
	totalBytes := 0

	for _, sr := range results {
		// 跳过 MEMORY.md 本身
		if sr.Path == "MEMORY.md" {
			continue
		}

		// 读取文件内容
		rPath := sr.Path
		content := r.readFileSafe(ctx, filepath.Join(r.codingMemoryDir, rPath))
		if content == "" {
			continue
		}

		// 提取 body（不含 frontmatter），避免注入时重复浪费 token
		fm := lite.ParseFrontmatter(content)
		body := lite.ExtractBody(content)
		if body == "" {
			continue
		}

		bodyBytes := len(body)

		// 检查大小限制
		if totalBytes+bodyBytes > maxRecallTotalBytes {
			remaining := maxRecallTotalBytes - totalBytes
			if remaining > 200 {
				// 至少保留 200 字节才截断
				truncated := body
				// 按 rune 截断，避免截断多字节字符
				if utf8.RuneCountInString(truncated) > remaining {
					runes := []rune(truncated)
					truncated = string(runes[:remaining]) + "\n\n... (truncated)"
				}
				title := rPath
				if fm != nil {
					if name, ok := fm["name"]; ok && name != "" {
						title = name
					}
				}
				dateTag := r.buildDateTag(fm)
				parts = append(parts, fmt.Sprintf("### %s [%s]%s\n\n%s", title, rPath, dateTag, truncated))
			}
			break
		}

		// 正常添加
		title := rPath
		if fm != nil {
			if name, ok := fm["name"]; ok && name != "" {
				title = name
			}
		}
		dateTag := r.buildDateTag(fm)
		parts = append(parts, fmt.Sprintf("### %s [%s]%s\n\n%s", title, rPath, dateTag, body))
		totalBytes += bodyBytes
	}

	if len(parts) == 0 {
		r.recallResult = &recallResult{content: "", totalMemories: total}
		return
	}

	r.recallResult = &recallResult{
		content:       strings.Join(parts, "\n\n---\n\n"),
		totalMemories: total,
	}
}

// buildDateTag 构建时间标注。
//
// 对齐 Python: CodingMemoryRail._auto_recall 中 date_tag 构建
func (r *CodingMemoryRail) buildDateTag(fm map[string]string) string {
	if fm == nil {
		return ""
	}
	updated := fm["updated_at"]
	if updated == "" {
		updated = fm["created_at"]
	}
	if updated == "" {
		return ""
	}
	return fmt.Sprintf(" (updated: %s)", updated)
}

// readMemoryIndex 读取 MEMORY.md 索引文件。
//
// 对齐 Python: CodingMemoryRail._read_memory_index()
func (r *CodingMemoryRail) readMemoryIndex(ctx context.Context) string {
	if r.SysOperation() == nil {
		return ""
	}
	fsOp := r.SysOperation().Fs()
	if fsOp == nil {
		return ""
	}
	indexPath := filepath.Join(r.codingMemoryDir, "MEMORY.md")
	readResult, err := fsOp.ReadFile(ctx, indexPath)
	if err != nil {
		return ""
	}
	if readResult == nil || readResult.Data == nil {
		return ""
	}
	content := readResult.Data.Content
	if content == "" {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > memoryIndexMaxLines {
		lines = lines[:memoryIndexMaxLines]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// readFileSafe 安全读取文件。
//
// 对齐 Python: CodingMemoryRail._read_file_safe(filepath)
func (r *CodingMemoryRail) readFileSafe(ctx context.Context, filepath string) string {
	if r.SysOperation() == nil {
		return ""
	}
	fsOp := r.SysOperation().Fs()
	if fsOp == nil {
		return ""
	}
	readResult, err := fsOp.ReadFile(ctx, filepath)
	if err != nil {
		return ""
	}
	if readResult == nil || readResult.Data == nil {
		return ""
	}
	return readResult.Data.Content
}

// countMemoryFiles 统计目录下的 .md 记忆文件数（排除 MEMORY.md）。
//
// 对齐 Python: CodingMemoryRail._count_memory_files(memory_dir)
func (r *CodingMemoryRail) countMemoryFiles(ctx context.Context) int {
	if r.SysOperation() == nil {
		return 0
	}
	fsOp := r.SysOperation().Fs()
	if fsOp == nil {
		return 0
	}
	listResult, err := fsOp.ListFiles(ctx, r.codingMemoryDir)
	if err != nil {
		return 0
	}
	if listResult == nil || listResult.Data == nil {
		return 0
	}
	count := 0
	for _, f := range listResult.Data.ListItems {
		if f.IsDirectory {
			continue
		}
		if !strings.HasSuffix(strings.ToLower(f.Name), ".md") {
			continue
		}
		if strings.EqualFold(f.Name, "memory.md") {
			continue
		}
		count++
	}
	return count
}

// extractLastUserQuery 从上下文中提取最后一条用户消息。
//
// 对齐 Python: CodingMemoryRail._extract_last_user_query(ctx)
func (r *CodingMemoryRail) extractLastUserQuery(cbc *agentinterfaces.AgentCallbackContext) string {
	inputs := cbc.Inputs()
	invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
	if !ok {
		return ""
	}
	if invokeInputs.Query == nil {
		return ""
	}
	return invokeInputs.Query.PlainText()
}

// isReadOnly 检查是否为 cron/heartbeat 只读模式。
//
// 对齐 Python: isinstance(ctx.inputs, InvokeInputs) and (ctx.inputs.is_cron() or ctx.inputs.is_heartbeat())
func (r *CodingMemoryRail) isReadOnly(cbc *agentinterfaces.AgentCallbackContext) bool {
	inputs := cbc.Inputs()
	invokeInputs, ok := inputs.(*agentinterfaces.InvokeInputs)
	if !ok {
		return false
	}
	return invokeInputs.IsCron() || invokeInputs.IsHeartbeat()
}
