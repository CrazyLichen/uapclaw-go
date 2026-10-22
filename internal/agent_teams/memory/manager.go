package memory

import (
	"context"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"time"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools/database"
	llm "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/interfaces"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/prompts/sections"
	memoryrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	runner "github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryManager 团队记忆管理器。
// Python: TeamMemoryManager (manager.py)
//
// 生命周期：InitToolkit → RegisterTools → LoadAndInject → ExtractAfterRound → Close
type TeamMemoryManager struct {
	// memberName 成员名称
	memberName string
	// teamName 团队名称
	teamName string
	// role 角色
	role TeamRole
	// lifecycle 生命周期
	lifecycle TeamLifecycle
	// scenario 场景
	scenario TeamScenario
	// embeddingConfig 嵌入配置
	embeddingConfig *embedding.EmbeddingConfig
	// language 语言
	language TeamLanguage
	// promptMode 提示模式
	promptMode PromptMode
	// enableAutoExtract 是否自动提取
	enableAutoExtract bool
	// readOnlySource 只读来源工作空间路径
	readOnlySource *string
	// db 团队数据库。⤵️ 回填: 9.65a
	db database.TeamDatabase
	// taskManager 任务管理器。⤵️ 回填: 9.65a
	taskManager *tools.TeamTaskManager
	// extractionModel 提取模型
	extractionModel *llm.Model
	// tzOffset 时区偏移
	tzOffset float64
	// sysOperation 系统操作接口。⤴️ 9.64 具体类型回填
	sysOperation sysop.SysOperation
	// workspace 工作空间
	workspace *workspace.Workspace
	// teamMemoryDir 团队记忆目录路径
	teamMemoryDir *string
	// toolkit 成员记忆工具集。⤵️ 回填: 7.2
	toolkit *MemberMemoryToolkit
	// ownedToolNames 已注册工具名集合。⤵️ 回填: 7.2
	ownedToolNames map[string]struct{}
	// ownedToolIDs 已注册工具ID集合。⤵️ 回填: 7.2
	ownedToolIDs map[string]struct{}
	// deepAgentForCleanup 用于清理的 DeepAgent
	deepAgentForCleanup interfaces.DeepAgentInterface
	// sharedManager 共享记忆管理器
	sharedManager *SharedMemoryManager
	// cachedBaseSection 缓存的基础提示词段。⤵️ 回填: 7.2
	cachedBaseSection *saprompt.PromptSection
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SectionName 系统提示词段名称
	SectionName = "team_memory"
	// maxPersonalMemoryBytes 个人记忆注入最大字节数
	maxPersonalMemoryBytes = 10 * 1024
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// logComponent 日志组件
	logComponent = logger.ComponentTeam
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMemoryManager 创建团队记忆管理器
func NewTeamMemoryManager(params TeamMemoryManagerParams) *TeamMemoryManager {
	mgr := &TeamMemoryManager{
		memberName:        params.MemberName,
		teamName:          params.TeamName,
		role:              params.Role,
		lifecycle:         params.Lifecycle,
		scenario:          params.Scenario,
		embeddingConfig:   params.EmbeddingConfig,
		language:          params.Language,
		promptMode:        params.PromptMode,
		enableAutoExtract: params.EnableAutoExtract,
		readOnlySource:    params.ReadOnlySourceWorkspace,
		db:                params.DB,
		taskManager:       params.TaskManager,
		extractionModel:   params.ExtractionModel,
		tzOffset:          params.TimezoneOffsetHours,
		sysOperation:      params.SysOperation,
		teamMemoryDir:     params.TeamMemoryDir,
		ownedToolNames:    make(map[string]struct{}),
		ownedToolIDs:      make(map[string]struct{}),
	}

	// Python: if self._read_only_source: self._workspace = Workspace(root_path=self._read_only_source)
	if params.ReadOnlySourceWorkspace != nil && *params.ReadOnlySourceWorkspace != "" {
		mgr.workspace = workspace.NewWorkspace(*params.ReadOnlySourceWorkspace, "")
	} else {
		mgr.workspace = params.Workspace
	}

	if params.TeamMemoryDir != nil && *params.TeamMemoryDir != "" {
		mgr.sharedManager = NewSharedMemoryManager(*params.TeamMemoryDir, params.SysOperation)
		if err := mgr.sharedManager.EnsureDir(); err != nil {
			logger.Warn(logComponent).Err(err).Str("team_memory_dir", *params.TeamMemoryDir).
				Msg("SharedMemoryManager EnsureDir failed")
		}
	}
	return mgr
}

// InitToolkit 初始化成员记忆工具集。
// S-12: 对齐 Python manager.py init_toolkit（L90-129）——
// 构造 MemberMemoryToolkit 并调用 Initialize；成功后构造 SharedMemoryManager。
// 原桩恒返回 false 导致团队记忆功能整体失效。
func (m *TeamMemoryManager) InitToolkit(ctx context.Context) (bool, error) {
	if m.toolkit != nil {
		return true, nil
	}
	if m.workspace == nil {
		logger.Warn(logComponent).
			Str("team_name", m.teamName).
			Str("member_name", m.memberName).
			Msg("[TeamMemoryManager] No workspace available, skipping init")
		return false, nil
	}

	m.toolkit = NewMemberMemoryToolkit(
		m.memberName,
		m.teamName,
		m.workspace,
		m.scenario,
		m.embeddingConfig,
		m.sysOperation,
		m.readOnlySource != nil,
	)

	success, err := m.toolkit.Initialize(ctx)
	if err != nil || !success {
		logger.Warn(logComponent).
			Str("team_name", m.teamName).
			Str("member_name", m.memberName).
			Err(err).
			Msg("[TeamMemoryManager] Toolkit init failed, memory tools unavailable")
		return false, err
	}

	// Python: if self._team_memory_dir → SharedMemoryManager + ensure_dir
	// （Go 构造函数已内置 SharedMemoryManager 创建与 EnsureDir）

	logger.Info(logComponent).
		Str("team_name", m.teamName).
		Str("member_name", m.memberName).
		Str("lifecycle", string(m.lifecycle)).
		Str("scenario", string(m.scenario)).
		Bool("read_only", m.readOnlySource != nil).
		Msg("[TeamMemoryManager] Initialized")
	return true, nil
}

// RegisterTools 将记忆工具注册到 DeepAgent。
// S-12: 对齐 Python register_tools（L141-178）——
// 幂等（已注册跳过）；先剥离 MemoryRail/CodingMemoryRail（团队场景由本管理器接管记忆）；
// toolkit 工具注册到 Runner.resource_mgr（不存在的）+ ability_manager（记录到 owned 集合）。
func (m *TeamMemoryManager) RegisterTools(deepAgent interfaces.DeepAgentInterface) {
	if len(m.ownedToolNames) > 0 {
		return
	}

	// Python: _strip_memory_rails_from_deep_agent
	if railsRemoved := stripMemoryRailsFromDeepAgent(deepAgent); railsRemoved > 0 {
		logger.Info(logComponent).
			Str("team_name", m.teamName).
			Str("member_name", m.memberName).
			Int("rails_removed", railsRemoved).
			Msg("[TeamMemoryManager] Stripped memory rail(s)")
	}

	if m.toolkit == nil || deepAgent.AbilityManager() == nil {
		return
	}

	m.deepAgentForCleanup = deepAgent

	for _, t := range m.toolkit.GetTools() {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(logComponent).
						Str("team_name", m.teamName).
						Msgf("[TeamMemoryManager] Failed to register tool: %v", rec)
				}
			}()
			toolCard := t.Card()
			if toolCard == nil || toolCard.ID == "" {
				return
			}
			// Python: existing = Runner.resource_mgr.get_tool(tool_card.id)
			// existing 为 None 时 add_tool 并记录 owned_tool_ids
			existing, getErr := runner.GetResourceMgr().GetTool([]string{toolCard.ID})
			if getErr == nil && len(existing) == 0 {
				if addErr := runner.GetResourceMgr().AddTool(t); addErr == nil {
					m.ownedToolIDs[toolCard.ID] = struct{}{}
				}
			}
			// Python: result = deep_agent.ability_manager.add(tool_card)
			result := deepAgent.AbilityManager().Add(toolCard)
			if result.Added {
				m.ownedToolNames[toolCard.Name] = struct{}{}
				logger.Info(logComponent).
					Str("tool_name", toolCard.Name).
					Str("tool_id", toolCard.ID).
					Msg("[TeamMemoryManager] Registered tool")
			}
		}()
	}
}

// LoadAndInject 加载个人记忆+共享记忆→注入系统提示词。
// S-12: 对齐 Python load_and_inject（L248-309）——
// 构建基础 section（coding/general 二选一）→ 拼接个人记忆与团队共享记忆 →
// RemoveSection + AddSection 替换提示词段。原桩空实现导致记忆不注入提示词。
func (m *TeamMemoryManager) LoadAndInject(ctx context.Context, deepAgent interfaces.DeepAgentInterface, query string) error {
	// Python: builder = getattr(deep_agent, "system_prompt_builder", None)——
	// deepAgent 为 nil 或无 builder 时直接返回
	if deepAgent == nil {
		return nil
	}
	builder := deepAgent.SystemPromptBuilder()
	if builder == nil {
		return nil
	}

	// Python: cached_base_section 缓存（首次构建）
	if m.cachedBaseSection == nil {
		var base saprompt.PromptSection
		if m.scenario == TeamScenarioCoding {
			// Python: build_coding_memory_section(language, read_only, memory_dir)
			memoryDir := "coding_memory/"
			if m.workspace != nil {
				if nodePath := m.workspace.GetNodePath("coding_memory"); nodePath != nil {
					memoryDir = *nodePath
				}
			}
			base = sections.BuildCodingMemorySection(memoryDir, m.readOnlySource != nil, string(m.language))
		} else {
			// Python: build_memory_section(language, read_only, is_proactive)
			mode := "proactive"
			if m.readOnlySource != nil {
				mode = "read_only"
			} else if m.promptMode != PromptModeProactive {
				mode = "inactive"
			}
			cst := time.FixedZone("CST", 8*3600)
			todayDate := time.Now().In(cst).Format("2006-01-02")
			base = sections.BuildMemorySection(mode, todayDate, string(m.language))
		}
		m.cachedBaseSection = &saprompt.PromptSection{
			Name:     SectionName,
			Content:  base.Content,
			Priority: base.Priority,
		}
	}

	// Python: section = PromptSection(name, dict(content), priority)（深拷贝内容）
	section := saprompt.PromptSection{
		Name:     m.cachedBaseSection.Name,
		Content:  make(map[string]string, len(m.cachedBaseSection.Content)),
		Priority: m.cachedBaseSection.Priority,
	}
	for k, v := range m.cachedBaseSection.Content {
		section.Content[k] = v
	}

	cn := m.language == TeamLanguageCN

	// Python: personal_content = await self._fetch_personal_memory_for_prompt(query)
	if personalContent := m.fetchPersonalMemoryForPrompt(ctx, query); personalContent != "" {
		header := "\n\n## Team shared memory\n\n"
		if cn {
			header = "\n\n## 你的相关记忆\n\n"
		}
		for key := range section.Content {
			section.Content[key] += header + personalContent
		}
	}

	// Python: team_summary = await self._shared_manager.read_team_summary()
	if m.sharedManager != nil {
		func() {
			defer func() {
				if rec := recover(); rec != nil {
					logger.Warn(logComponent).
						Str("team_name", m.teamName).
						Msgf("[TeamMemoryManager] Failed to read team summary: %v", rec)
				}
			}()
			if teamSummary := m.sharedManager.ReadTeamSummary(ctx); teamSummary != "" {
				header := "\n\n## Team shared memory\n\n"
				if cn {
					header = "\n\n## 团队共享记忆\n\n"
				}
				for key := range section.Content {
					section.Content[key] += header + teamSummary
				}
			}
		}()
	}

	builder.RemoveSection(SectionName)
	builder.AddSection(section)
	return nil
}

// ExtractAfterRound Leader 专属：提取 agent 蒸馏团队记忆。
// S-12: 对齐 Python extract_after_round（L311-334）——
// 四重前置条件（auto_extract/persistent/leader/team_memory_dir+db）后调
// ExtractTeamMemories，失败仅告警不中断。原桩空实现导致提取功能失效。
func (m *TeamMemoryManager) ExtractAfterRound(ctx context.Context) error {
	if !m.enableAutoExtract {
		return nil
	}
	if m.lifecycle != TeamLifecyclePersistent {
		return nil
	}
	if m.role != TeamRoleLeader {
		return nil
	}
	if m.teamMemoryDir == nil || m.db == nil {
		return nil
	}

	if err := ExtractTeamMemories(
		ctx,
		m.teamName,
		m.db,
		m.taskManager,
		*m.teamMemoryDir,
		m.sysOperation,
		m.extractionModel,
		m.tzOffset,
	); err != nil {
		// Python: except → logger.warning（不中断）
		logger.Warn(logComponent).
			Str("team_name", m.teamName).
			Err(err).
			Msg("[TeamMemoryManager] extract_after_round failed")
	}
	return nil
}

// Close 反注册工具+移除提示词段+关闭 toolkit。
// S-12: 对齐 Python close（L336-375）——
// 对称清理：remove_section / ability_manager.remove / resource_mgr.remove_tool / toolkit.close。
func (m *TeamMemoryManager) Close(ctx context.Context) error {
	toolIDs := make([]string, 0, len(m.ownedToolIDs))
	for id := range m.ownedToolIDs {
		toolIDs = append(toolIDs, id)
	}
	toolNames := make([]string, 0, len(m.ownedToolNames))
	for name := range m.ownedToolNames {
		toolNames = append(toolNames, name)
	}
	deepAgent := m.deepAgentForCleanup

	if deepAgent != nil {
		// Python: builder.remove_section(SECTION_NAME)
		if builder := deepAgent.SystemPromptBuilder(); builder != nil {
			builder.RemoveSection(SectionName)
		}
		// Python: am.remove(tool_names)
		if am := deepAgent.AbilityManager(); am != nil && len(toolNames) > 0 {
			sort.Strings(toolNames)
			am.RemoveMany(toolNames)
		}
	}

	m.deepAgentForCleanup = nil
	m.ownedToolNames = make(map[string]struct{})
	m.ownedToolIDs = make(map[string]struct{})

	// Python: for tid → Runner.resource_mgr.remove_tool(tid)
	if len(toolIDs) > 0 {
		sort.Strings(toolIDs)
		if _, err := runner.GetResourceMgr().RemoveTool(toolIDs); err != nil {
			logger.Warn(logComponent).
				Str("team_name", m.teamName).
				Err(err).
				Msg("[TeamMemoryManager] remove_tool failed")
		}
	}

	if m.toolkit != nil {
		if err := m.toolkit.Close(ctx); err != nil {
			logger.Warn(logComponent).
				Str("team_name", m.teamName).
				Err(err).
				Msg("[TeamMemoryManager] toolkit close failed")
		}
		m.toolkit = nil
	}

	m.cachedBaseSection = nil

	logger.Info(logComponent).
		Str("team_name", m.teamName).
		Str("member_name", m.memberName).
		Msg("[TeamMemoryManager] Closed")
	return nil
}

// ExtractionModel 返回提取模型
func (m *TeamMemoryManager) ExtractionModel() *llm.Model { return m.extractionModel }

// SetExtractionModel 设置提取模型
func (m *TeamMemoryManager) SetExtractionModel(model *llm.Model) { m.extractionModel = model }

// SharedManager 返回共享记忆管理器
func (m *TeamMemoryManager) SharedManager() *SharedMemoryManager { return m.sharedManager }

// MemberName 返回成员名称
func (m *TeamMemoryManager) MemberName() string { return m.memberName }

// TeamName 返回团队名称
func (m *TeamMemoryManager) TeamName() string { return m.teamName }

// Role 返回角色
func (m *TeamMemoryManager) Role() TeamRole { return m.role }

// ──────────────────────────── 非导出函数 ────────────────────────────

// stripMemoryRailsFromDeepAgent 从 DeepAgent 剥离 MemoryRail/CodingMemoryRail。
// Python: _strip_memory_rails_from_deep_agent（L131-139）——
// 团队场景由 TeamMemoryManager 接管记忆职责，Rail 注入逻辑须先移除。
func stripMemoryRailsFromDeepAgent(deepAgent interfaces.DeepAgentInterface) int {
	// Python: strip_fn = getattr(deep_agent, "strip_rails_by_type", None)
	type railStripper interface {
		StripRailsByType(railTypes ...reflect.Type) int
	}
	stripper, ok := deepAgent.(railStripper)
	if !ok {
		return 0
	}
	return stripper.StripRailsByType(
		reflect.TypeOf((*memoryrail.MemoryRail)(nil)),
		reflect.TypeOf((*memoryrail.CodingMemoryRail)(nil)),
	)
}

// fetchPersonalMemoryForPrompt 拉取个人记忆注入提示词。
// Python: _fetch_personal_memory_for_prompt（L180-246）——
// 优先索引搜索分支（query+workspace+sysOp+index 齐备时，最多 5 条，
// 跳过 MEMORY.md，累计 10KB 截断）；否则回退读 MEMORY.md 全文。
func (m *TeamMemoryManager) fetchPersonalMemoryForPrompt(ctx context.Context, query string) string {
	coding := strings.TrimSpace(strings.ToLower(string(m.scenario))) == string(TeamScenarioCoding)
	ws := m.workspace
	var indexManager lite.MemoryIndexManager
	if m.toolkit != nil {
		indexManager = m.toolkit.Manager()
	}
	sysOperation := m.sysOperation

	hasQuery := query != ""
	hasRuntime := ws != nil && sysOperation != nil
	hasIndex := indexManager != nil

	nodeName := "memory"
	if coding {
		nodeName = "coding_memory"
	}
	memoryDirPath := ""
	if ws != nil {
		if nodePath := ws.GetNodePath(nodeName); nodePath != nil {
			memoryDirPath = *nodePath
		}
	}

	// Python: 搜索分支
	if hasQuery && hasRuntime && hasIndex {
		results, err := indexManager.Search(ctx, query, lite.SearchOpts{MaxResults: 5})
		if err == nil && len(results) > 0 {
			var parts []string
			totalBytes := 0
			for _, r := range results {
				rPath := r.Path
				if strings.HasSuffix(rPath, "MEMORY.md") {
					continue
				}
				fullPath := filepath.Join(memoryDirPath, rPath)
				content := ""
				if fsOp := sysOperation.Fs(); fsOp != nil {
					if fileResult, readErr := fsOp.ReadFile(ctx, fullPath); readErr == nil &&
						fileResult != nil && fileResult.Data != nil {
						content = fileResult.Data.Content
					}
				}
				if content == "" {
					continue
				}
				contentBytes := len([]byte(content))
				if totalBytes+contentBytes > maxPersonalMemoryBytes {
					remaining := maxPersonalMemoryBytes - totalBytes
					if remaining > 200 {
						truncated := content
						if len(truncated) > remaining {
							truncated = truncated[:remaining]
						}
						parts = append(parts, "### "+rPath+"\n\n"+truncated+"\n... (truncated)")
					}
					break
				}
				parts = append(parts, "### "+rPath+"\n\n"+content)
				totalBytes += contentBytes
			}
			if len(parts) > 0 {
				return strings.Join(parts, "\n\n---\n\n")
			}
		}
	}

	// Python: MEMORY.md 回退分支
	if ws != nil && sysOperation != nil && memoryDirPath != "" {
		indexPath := filepath.Join(memoryDirPath, "MEMORY.md")
		if fsOp := sysOperation.Fs(); fsOp != nil {
			if result, err := fsOp.ReadFile(ctx, indexPath); err == nil &&
				result != nil && result.Data != nil {
				return strings.TrimSpace(result.Data.Content)
			}
		}
	}

	return ""
}
