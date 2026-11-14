package agent

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	mcpTypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	cliprompts "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/prompts"
	clirails "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/cli/rails"
	hconfig "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/context_engineer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/memory"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/skills"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/subagents"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/web_tools"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// CLI agent 的后端装配。
//
// Python: openjiuwen/harness/cli/agent/factory.py（774 行）
//
// 全量符号：CreateAgent 全量 rails 装配（AskUserRail / ConfirmInterruptRail /
// SkillUseRail / ContextProcessorRail / MemoryRail / subagents / vision /
// audio / runtime_extensions 热加载）+ LocalBackend 真身 + CreateBackend 工厂。
//
// 已知偏差（对应 spec 已登记或本批新增登记）：
//   - D-JM（本批新增）：JiuwenMemoryRail 未迁移（Python
//     harness/rails/memory/jiuwen_memory_rail.py 690 行），且其 provider 来自
//     外部依赖包 jiuwen_memory_adapter——Python 侧 import 失败也返回 None。
//     Go 侧 buildJiuwenMemoryRail 保留环境开关门卫，恒返回 nil。
//     回收条件：领域九/后续补 JiuwenMemoryRail + adapter 后回填。
//   - D-LP（已回填）：DeepAgent.LoadPlugin 热加载链于 15.15 落地，
//     LocalBackend.loadPluginFn 默认走真实现；SetLoadPlugin 仅保留测试覆写。
//   - D-embed：workspace 内容资源以 //go:embed 打包（prompts/content.go），
//     替代 Python 的文件系统路径读取。

// logComponent 本包的日志组件。
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 常量 ────────────────────────────

// DefaultSkillDirs CLI agent 启动时扫描的 skill 根目录。
// 降序优先：两个目录有同名 skill 时取靠前的（更高优先级）。
// Python: factory.py:49-55 _DEFAULT_SKILL_DIRS
var DefaultSkillDirs = []string{
	"~/.openjiuwen/workspace/skills",
	"~/.claude/skills",
	"~/.codex/skills",
	"~/.jiuwenclaw/workspace/skills",
}

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultSkillDirsSnapshot 返回默认 skill 目录的副本。
// Python: factory.py:112-127 _default_skill_dirs
func DefaultSkillDirsSnapshot() []string {
	return append([]string(nil), DefaultSkillDirs...)
}

// GetCliContentBaseDir CLI 专属 workspace 内容文件的基目录。
// Python: factory.py:57-59 _get_cli_content_base_dir（prompts/workspace_content）
//
// Go 侧内容以 //go:embed 打包在 prompts 包内（偏差 D-embed），此函数保留
// 路径语义供日志与测试引用。
func GetCliContentBaseDir() string {
	return filepath.Join("prompts", "workspace_content")
}

// LoadCliContent 加载 CLI 专属 workspace 内容。
// Python: factory.py:62-79 _load_cli_content
//
// 文件不存在时返回空串；实际读取走 prompts 包的嵌入资源（偏差 D-embed）。
func LoadCliContent(language, filePath string) string {
	return cliprompts.LoadWorkspaceContent(language, filePath)
}

// BuildCliWorkspace 构建带 CLI 身份覆写的 Workspace。
// Python: factory.py:81-109 _build_cli_workspace
//
// 从默认 schema 出发，用预定义的编码助手身份替换 IDENTITY.md
// （而不是通用的"自行填写"模板）。
func BuildCliWorkspace(cfg CLIConfig, language string) (*workspace.Workspace, error) {
	ws := workspace.NewWorkspace(cfg.Workspace, language)

	cliIdentity := LoadCliContent(language, "IDENTITY.md")
	if cliIdentity != "" {
		if err := ws.SetDirectory(map[string]any{
			"name":            "IDENTITY.md",
			"description":     "Identity credentials and permissions",
			"path":            "IDENTITY.md",
			"is_file":         true,
			"children":        []any{},
			"default_content": cliIdentity,
		}); err != nil {
			return nil, err
		}
	}

	return ws, nil
}

// BuildMemoryRail 在 embedding 配置可用时构建 MemoryRail。
// Python: factory.py:128-166 _build_memory_rail
//
// embedding 设置读环境变量（EMBEDDING_MODEL_NAME / EMBEDDING_BASE_URL /
// EMBEDDING_API_KEY，后两者回退 cfg 的 api_base / api_key）；
// 解析不出配置时返回 nil（对齐 Python 的 except 分支 Debug 日志）。
func BuildMemoryRail(cfg CLIConfig) *memory.MemoryRail {
	embeddingConfig := lite.ResolveEmbeddingConfigFromEnv(
		"text-embedding-3-small", cfg.APIBase, cfg.APIKey)
	if embeddingConfig == nil {
		logger.Debug(logComponent).Msg("MemoryRail not available")
		return nil
	}
	// Python: MemoryRail(embedding_config=...)，is_proactive 取默认 True
	// （memory_rail.py:50 is_proactive: bool = True）。
	return memory.NewMemoryRail(embeddingConfig, true)
}

// BuildJiuwenMemoryRail 在显式启用时构建 JiuwenMemoryRail。
// Python: factory.py:168-224 _build_jiuwen_memory_rail
//
// 门卫：JIUWEN_MEMORY_ENABLED 必须为真值（1/true/yes/on）。
//
// 偏差 D-JM：Python 的 provider 来自外部依赖包 jiuwen_memory_adapter
// （import 失败时返回 None），且 Go 侧尚未迁移 JiuwenMemoryRail 本体
// （harness/rails/memory/jiuwen_memory_rail.py，690 行）。此处保留开关
// 门卫与 Debug 日志，恒返回 nil；回收条件为该 rail 与 adapter 的对齐落地。
func BuildJiuwenMemoryRail(cfg CLIConfig) *memory.MemoryRail {
	_ = cfg
	enabled := strings.ToLower(strings.TrimSpace(os.Getenv("JIUWEN_MEMORY_ENABLED")))
	if enabled != "1" && enabled != "true" && enabled != "yes" && enabled != "on" {
		return nil
	}
	logger.Debug(logComponent).Msg("JiuwenMemoryRail not available")
	return nil
}

// BuildSubagents 构建 CLI agent 的子 Agent 规格列表。
// Python: factory.py:226-320 _build_subagents
//
// code_agent / research_agent 直接构造 SubAgentConfig（language="en"，
// rails=[SysOperationRail()]）；browser_agent 走 BuildBrowserAgentConfig，
// 失败时 Debug 日志后跳过。
func BuildSubagents(model *llm.Model) []*hschema.SubAgentConfig {
	var specs []*hschema.SubAgentConfig

	// --- code_agent ç¼ç  Agent ---
	codeSpec := hschema.NewSubAgentConfig()
	codeSpec.AgentCard = agentschema.NewAgentCard(
		agentschema.WithAgentName("code_agent"),
		agentschema.WithAgentDescription(subagents.DefaultCodeAgentDescription("en")),
	)
	codeSpec.SystemPrompt = subagents.DefaultCodeAgentSystemPrompt("en")
	codeSpec.Model = model
	codeSpec.Rails = []sainterfaces.AgentRail{rails.NewSysOperationRail()}
	codeSpec.Language = "en"
	specs = append(specs, codeSpec)

	// --- research_agent ç ç©¶ Agent ---
	researchSpec := hschema.NewSubAgentConfig()
	researchSpec.AgentCard = agentschema.NewAgentCard(
		agentschema.WithAgentName("research_agent"),
		agentschema.WithAgentDescription(subagents.DefaultResearchAgentDescription("en")),
	)
	researchSpec.SystemPrompt = subagents.DefaultResearchAgentSystemPrompt("en")
	researchSpec.Model = model
	researchSpec.Rails = []sainterfaces.AgentRail{rails.NewSysOperationRail()}
	researchSpec.Language = "en"
	specs = append(specs, researchSpec)

	// --- browser_agent æµè§å¨ Agent ---
	browserSpec := subagents.BuildBrowserAgentConfig(model, &hschema.SubagentCreateParams{
		Model:    model,
		Language: "en",
	})
	if browserSpec != nil {
		specs = append(specs, browserSpec)
	}

	return specs
}

// LoadVisionConfig 加载视觉模型配置。
// Python: factory.py:398-426 _load_vision_config
//
// VISION_API_KEY 已设置时用 FromEnv；否则回退主模型的 api_key / api_base。
func LoadVisionConfig(cfg CLIConfig) *hschema.VisionModelConfig {
	var visionCfg hschema.VisionModelConfig
	if os.Getenv("VISION_API_KEY") != "" {
		return ptr(hschema.VisionModelConfig{}.FromEnv())
	}
	// ååºï¼å¤ç¨ä¸»æ¨¡åå­æ®
	visionCfg = hschema.VisionModelConfig{APIKey: cfg.APIKey, BaseURL: cfg.APIBase}
	return &visionCfg
}

// LoadAudioConfig 加载音频模型配置。
// Python: factory.py:428-456 _load_audio_config
//
// AUDIO_API_KEY 已设置时用 FromEnv；否则回退主模型的 api_key / api_base。
func LoadAudioConfig(cfg CLIConfig) *hschema.AudioModelConfig {
	var audioCfg hschema.AudioModelConfig
	if os.Getenv("AUDIO_API_KEY") != "" {
		return ptr(hschema.AudioModelConfig{}.FromEnv())
	}
	// ååºï¼å¤ç¨ä¸»æ¨¡åå­æ®
	audioCfg = hschema.AudioModelConfig{APIKey: cfg.APIKey, BaseURL: cfg.APIBase}
	return &audioCfg
}

// CreateAgent 创建 DeepAgent 及其 TokenTrackingRail。
// Python: factory.py:492-656 create_agent
//
// 用 harness 的 SysOperationRail 自动注册文件系统工具（BashTool、
// ReadFileTool 等），SecurityRail 由 create_deep_agent 自动挂载；Web 工具
// 手动加入（不属于 SysOperationRail）。另集成 ToolTrackingRail /
// AskUserRail / ConfirmInterruptRail / ContextProcessorRail /
// ContextAssembleRail / MemoryRail / SessionRail（subagents 存在时由工厂
// 自动注入，不手动加）/ 子 Agent / MCP / 视听多模态。
func CreateAgent(ctx context.Context, cfg CLIConfig) (*harness.DeepAgent, *clirails.TokenTrackingRail, error) {
	model, err := llm.InitModel(
		cfg.Provider,
		cfg.Model,
		cfg.APIKey,
		cfg.APIBase,
		llm.WithInitMaxTokens(cfg.MaxTokens),
	)
	if err != nil {
		return nil, nil, err
	}

	systemPrompt := cliprompts.BuildSystemPrompt(cfg.Cwd, cfg.Model, cfg.Provider, "en")

	tracker := clirails.NewTokenTrackingRail()
	toolTracker := clirails.NewToolTrackingRail()
	fsRail := rails.NewSysOperationRail()

	// 构建 rails 列表
	agentRails := []sainterfaces.AgentRail{tracker, toolTracker, fsRail}

	// --- 中断 rails ---
	// AskUserRail：拦截 ask_user 工具调用，经中断流把问题呈现给用户。
	agentRails = append(agentRails, interrupt.NewAskUserRail())

	// ConfirmInterruptRail：潜在危险文件变更工具的人工确认门。
	agentRails = append(agentRails,
		interrupt.NewConfirmInterruptRail(interrupt.WithConfirmToolNames("write_file", "edit_file")))

	// 默认 skill 目录——SkillUseRail 在扫描时静默跳过不存在的目录。
	skillRail := skills.NewSkillUseRail(
		DefaultSkillDirsSnapshot(),
		skills.WithSkillMode("all"),
		skills.WithIncludeTools(false),
	)
	agentRails = append(agentRails, skillRail)

	// ContextProcessorRail + ContextAssembleRail——上下文窗口管理
	//（含 /compact 用的 DialogueCompressor）。
	agentRails = append(agentRails,
		context_engineer.NewContextProcessorRail(context_engineer.WithPreset(true)),
		context_engineer.NewContextAssembleRail())

	// --- MemoryRail è®°å¿ rail ---
	// embedding 配置可用时集成向量记忆工具。
	if memoryRail := BuildMemoryRail(cfg); memoryRail != nil {
		agentRails = append(agentRails, memoryRail)
	}

	// --- JiuwenMemoryRail ä¹æè®°å¿ rail ---
	// 外部 JiuwenMemory（agent-memory）服务集成，JIUWEN_MEMORY_ENABLED
	// 门卫（默认关）。偏差 D-JM：Go 侧恒 nil。
	if jiuwenRail := BuildJiuwenMemoryRail(cfg); jiuwenRail != nil {
		agentRails = append(agentRails, jiuwenRail)
	}

	// --- SessionRail ä¼è¯ rail ---
	// 异步子 Agent 后台任务能力：subagents 存在且
	// enable_async_subagent=True 时由 create_deep_agent 自动注入，
	// 此处不手动添加（对齐 Python 注释）。

	// Web 工具不属于 SysOperationRail
	webTools := web_tools.CreateWebTools("en", "", true, true, true)

	// ~/.openjiuwen/mcp.json 的 MCP 服务器
	mcpConfigs := LoadMcpConfigs()

	// 多模态配置（回退主模型凭据）
	visionConfig := LoadVisionConfig(cfg)
	audioConfig := LoadAudioConfig(cfg)

	// --- 子 Agent ---
	subagentList := BuildSubagents(model)

	// create_deep_agent 额外参数
	var mcps []*mcpTypes.McpServerConfig
	for _, raw := range mcpConfigs {
		decoded, err := decodeMcpConfig(raw)
		if err != nil {
			return nil, nil, err
		}
		mcps = append(mcps, decoded)
	}

	params := hconfig.CreateDeepAgentParams{
		Model:              model,
		SystemPrompt:       systemPrompt,
		ToolInstances:      webTools,
		Mcps:               mcps,
		Rails:              agentRails,
		EnableTaskLoop:     true,
		EnableTaskPlanning: true,
		MaxIterations:      cfg.MaxIterations,
		Language:           "en",
	}
	if len(subagentList) > 0 {
		for _, spec := range subagentList {
			params.Subagents = append(params.Subagents, spec)
		}
		params.EnableAsyncSubagent = true
	}
	if visionConfig != nil {
		params.VisionModelConfig = visionConfig
	}
	if audioConfig != nil {
		params.AudioModelConfig = audioConfig
	}

	// 构建 CLI 专属 workspace（覆写 IDENTITY.md）
	ws, err := BuildCliWorkspace(cfg, "en")
	if err != nil {
		return nil, nil, err
	}
	params.Workspace = ws

	agent, err := harness.CreateDeepAgent(ctx, params)
	if err != nil {
		return nil, nil, err
	}

	// 覆写 workspace root_path，绕开工厂自动加的 {agent_id}_workspace 后缀。
	// 这是安全的：restrict_to_work_dir=False 使 SysOperation 的作用域与
	// workspace 路径无关，rails 在首次 invoke() 时才惰性读
	// deep_config.workspace。
	if dc := agent.DeepConfig(); dc != nil && dc.Workspace != nil {
		dc.Workspace.RootPath = cfg.Workspace
	}

	return agent, tracker, nil
}

// LoadMcpConfigs 从 ~/.openjiuwen/mcp.json 加载 MCP 服务器配置。
// Python: factory.py:334-396 _load_mcp_configs
//
// 文件格式遵循 Claude Code 约定：顶层 mcpServers → 名字 → 规格。
// transport 取 spec.transport（回退 client_type，再回退 stdio）；
// server_path 取 spec.url（回退 server_path）；params 收 command/args/
// env/cwd 并过滤 None；坏文件静默返回空。
func LoadMcpConfigs() []map[string]any {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	mcpPath := filepath.Join(home, ".openjiuwen", "mcp.json")
	content, err := os.ReadFile(mcpPath)
	if err != nil {
		return nil
	}
	var data struct {
		McpServers map[string]map[string]any `json:"mcpServers"`
	}
	if json.Unmarshal(content, &data) != nil {
		logger.Debug(logComponent).Str("path", mcpPath).
			Msg("failed to load MCP config")
		return nil
	}
	configs := make([]map[string]any, 0, len(data.McpServers))
	for name, spec := range data.McpServers {
		transport := "stdio"
		if t, ok := spec["transport"].(string); ok && t != "" {
			transport = t
		} else if t, ok := spec["client_type"].(string); ok && t != "" {
			transport = t
		}
		serverPath := ""
		if u, ok := spec["url"].(string); ok && u != "" {
			serverPath = u
		} else if p, ok := spec["server_path"].(string); ok {
			serverPath = p
		}
		configs = append(configs, map[string]any{
			"server_name":  name,
			"server_path":  serverPath,
			"client_type":  transport,
			"params":       filterNoneValues(mcpParams(spec)),
			"auth_headers": spec["auth_headers"],
		})
	}
	return configs
}

// ──────────────────────────── AgentBackend 后端接口 ────────────────────────────

// AgentBackend 本地 / 远程 agent 执行的抽象。
// Python: factory.py:463-484 AgentBackend（Protocol，4 方法）
type AgentBackend interface {
	// Start 初始化后端（启动 Runner / 连接）
	Start(ctx context.Context) error
	// Stop 释放资源（停止 Runner / 断开）
	Stop(ctx context.Context) error
	// RunStreaming 执行 query 并流式吐 OutputSchema 块。
	// 通道元素具体化为 stream.OutputSchema（曾用 any，2026-11-27 审查
	// 裁决具体化：实际承载的就是块类型，any 会逼消费者做类型断言并
	// 静默丢弃非块项）。
	RunStreaming(ctx context.Context, query any, sessionID *string) (<-chan stream.OutputSchema, error)
	// Abort 中止当前运行中的查询
	Abort(ctx context.Context) error
}

// ──────────────────────────── LocalBackend 本地后端实现 ────────────────────────────

// LocalBackend 直接调 SDK Runner 的后端（MVP）。
// Python: factory.py:664-748 LocalBackend
type LocalBackend struct {
	// cfg CLI 配置
	cfg CLIConfig
	// agent 当前 agent（Python: self.agent，create_agent 产物）
	agent *harness.DeepAgent
	// tracker token 跟踪 rail（Python: self.tracker）
	tracker *clirails.TokenTrackingRail
	// sessionID 会话标识（cli- + 8 位随机）
	sessionID string
	// loadedExtensions 已热加载的运行时扩展（Python: _loaded_extensions）
	loadedExtensions map[string]bool
	// loadPlugin 插件热加载测试覆写点（nil 时走 DeepAgent.LoadPlugin 真实现）。
	// Python: agent.load_plugin(path)（deep_agent.py:2232）。
	loadPlugin func(path string) (labels []string, err error)
	// abortCount 累计 Abort 调用次数
	abortCount int
}

// NewLocalBackend 构造本地后端。
// Python: factory.py:667-672 __init__
func NewLocalBackend(cfg CLIConfig) *LocalBackend {
	return &LocalBackend{
		cfg:              cfg,
		sessionID:        "cli-" + randomID8(),
		loadedExtensions: map[string]bool{},
	}
}

// SetLoadPlugin 覆写插件热加载实现（测试专用；生产链路走 DeepAgent.LoadPlugin）。
func (b *LocalBackend) SetLoadPlugin(fn func(path string) (labels []string, err error)) {
	b.loadPlugin = fn
}

// Start 创建 agent 并启动 Runner。
// Python: factory.py:674-677 start
func (b *LocalBackend) Start(ctx context.Context) error {
	agent, tracker, err := CreateAgent(ctx, b.cfg)
	if err != nil {
		return err
	}
	b.agent = agent
	b.tracker = tracker
	return runner.Start(ctx)
}

// Stop 停止 Runner。
// Python: factory.py:679-681 stop
func (b *LocalBackend) Stop(ctx context.Context) error {
	return runner.Stop(ctx)
}

// RunStreaming 流式执行查询。
// Python: factory.py:683-701 run_streaming
//
// query 可以是普通字符串（普通轮次）或 InteractiveInput（中断恢复）。
func (b *LocalBackend) RunStreaming(ctx context.Context, query any, sessionID *string) (<-chan stream.OutputSchema, error) {
	// agent 未装配时不能进 Runner（nil 实例会 panic）；Python 侧此处
	// 由 Runner 内部对 None agent 抛错，Go 以显式守卫等价。
	if b.agent == nil {
		return nil, errNotImplemented("LocalBackend is not started; call Start before RunStreaming")
	}
	if err := b.loadRuntimeExtensions(ctx); err != nil {
		return nil, err
	}
	sid := b.sessionID
	if sessionID != nil {
		sid = *sessionID
	}
	schemaCh, err := runner.RunAgentStreaming(ctx, runner.ByAgent(b.agent), map[string]any{"query": query}, runner.BySessionID(sid), nil, nil, nil)
	if err != nil {
		return nil, err
	}
	out := make(chan stream.OutputSchema)
	go func() {
		defer close(out)
		for chunk := range schemaCh {
			out <- outputSchemaOf(chunk)
		}
	}()
	return out, nil
}

// outputSchemaOf 把通用 Schema 帧换形为 OutputSchema。
// runner 输出的帧绝大多数本身就是 *OutputSchema / OutputSchema；
// 其余类型（TraceSchema / CustomSchema）按 Python「chunk 原样透传」的
// 语义降级为 type+payload 两字段（payload 取 ToMap 的 payload 键）。
func outputSchemaOf(chunk stream.Schema) stream.OutputSchema {
	switch value := chunk.(type) {
	case *stream.OutputSchema:
		return *value
	case stream.OutputSchema:
		return value
	default:
		payload := any(nil)
		if m := value.ToMap(); m != nil {
			payload = m["payload"]
		}
		return stream.OutputSchema{Type: value.SchemaType(), Payload: payload}
	}
}

// Tracker 返回 token 跟踪 rail（/cost、/status 数据源）。
func (b *LocalBackend) Tracker() *clirails.TokenTrackingRail { return b.tracker }

// Agent 返回当前 agent（REPL 交互面 / interrupt 恢复使用）。
func (b *LocalBackend) Agent() *harness.DeepAgent { return b.agent }

// Abort 请求 agent 中止当前任务循环。
// Python: factory.py:703-709 abort（失败吞掉）
func (b *LocalBackend) Abort(ctx context.Context) error {
	if b.agent != nil {
		b.agent.Abort(ctx)
		b.abortCount++
	}
	return nil
}

// AbortCount 返回累计 Abort 调用次数（测试与诊断用）。
func (b *LocalBackend) AbortCount() int { return b.abortCount }

// SessionID 返回后端会话标识。
func (b *LocalBackend) SessionID() string { return b.sessionID }

// loadRuntimeExtensions 扫描 runtime_extensions/ 并热加载新配置。
// Python: factory.py:711-749 _load_runtime_extensions
//
// agent 为 nil 时直接返回；目录不存在直接返回；每个子目录找
// harness_config.yaml，已加载过的跳过；加载成功记录 Info 日志（含
// kind:identity 标签），失败记录 Warning。
func (b *LocalBackend) loadRuntimeExtensions(ctx context.Context) error {
	if b.agent == nil {
		return nil
	}
	extRoot := filepath.Join(b.cfg.Workspace, "auto_harness", "runtime_extensions")
	entries, err := os.ReadDir(extRoot)
	if err != nil {
		return nil
	}
	// Python: sorted(ext_root.iterdir())——按名字排序保证确定性
	sort.Slice(entries, func(i, j int) bool { return entries[i].Name() < entries[j].Name() })
	for _, child := range entries {
		if !child.IsDir() {
			continue
		}
		configPath := filepath.Join(extRoot, child.Name(), "harness_config.yaml")
		if info, err := os.Stat(configPath); err != nil || info.IsDir() {
			continue
		}
		extKey := configPath
		if b.loadedExtensions[extKey] {
			continue
		}
		labels, err := b.loadPluginFn()(ctx, extKey)
		if err != nil {
			logger.Warn(logComponent).Err(err).Str("config_path", configPath).
				Msg("Failed to load runtime extension")
			continue
		}
		b.loadedExtensions[extKey] = true
		if len(labels) > 0 {
			logger.Info(logComponent).
				Str("labels", strings.Join(labels, ", ")).
				Msg("Hot-loaded runtime extension")
		}
	}
	return nil
}

// loadPluginFn 返回当前生效的插件加载函数。
// 15.15 回填完成：默认走 DeepAgent.LoadPlugin 真实现（labels 为台账资源标签）；
// SetLoadPlugin 仅保留测试覆写能力。
func (b *LocalBackend) loadPluginFn() func(ctx context.Context, path string) (labels []string, err error) {
	if b.loadPlugin != nil {
		return func(ctx context.Context, path string) ([]string, error) { return b.loadPlugin(path) }
	}
	return func(ctx context.Context, path string) ([]string, error) {
		// Python: factory.py 的 runtime extension 热加载走 agent.load_harness_config
		// 的 deprecated 转调链（load_harness_config → load_plugin → labels）。
		if b.agent == nil {
			return nil, errNotImplemented("agent not started; cannot load plugin")
		}
		record, err := b.agent.LoadPlugin(ctx, path, nil)
		if err != nil {
			return nil, err
		}
		return harness.LoadRecordLabels(*record), nil
	}
}

// CreateBackend 选择并实例化后端。
// Python: factory.py:756-774 create_backend
//
// server_url 非空时报错（远程模式 MVP 尚未支持）。
func CreateBackend(cfg CLIConfig) (*LocalBackend, error) {
	if cfg.ServerURL != "" {
		return nil, errNotImplemented(
			"RemoteBackend is not supported in the MVP. " +
				"Remove OPENJIUWEN_SERVER_URL to use local mode.")
	}
	return NewLocalBackend(cfg), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// decodeMcpConfig 把 LoadMcpConfigs 产出的 map 转成 McpServerConfig。
// Python: _load_mcp_configs 直接构造 McpServerConfig；Go 侧 map → 结构体
// 的换形集中在此（McpServerConfig 无 JSON tag，须显式映射 snake_case 键）。
func decodeMcpConfig(raw map[string]any) (*mcpTypes.McpServerConfig, error) {
	cfg := &mcpTypes.McpServerConfig{}
	if v, ok := raw["server_name"].(string); ok {
		cfg.ServerName = v
	}
	if v, ok := raw["server_path"].(string); ok {
		cfg.ServerPath = v
	}
	if v, ok := raw["client_type"].(string); ok {
		cfg.ClientType = v
	}
	if v, ok := raw["params"].(map[string]any); ok {
		cfg.Params = v
	}
	if v, ok := raw["auth_headers"].(map[string]any); ok {
		headers := map[string]string{}
		for k, hv := range v {
			if s, ok := hv.(string); ok {
				headers[k] = s
			}
		}
		cfg.AuthHeaders = headers
	}
	return cfg, nil
}

// mcpParams 收集 command/args/env/cwd 四键（缺失跳过）。
// Python: factory.py:373-378 的 params 字典
func mcpParams(spec map[string]any) map[string]any {
	params := map[string]any{}
	for _, key := range []string{"command", "args", "env", "cwd"} {
		if value, exists := spec[key]; exists {
			params[key] = value
		}
	}
	return params
}

// filterNoneValues 剔除 nil 值。
// Python: factory.py:327-332 _filter_none_values
func filterNoneValues(data map[string]any) map[string]any {
	filtered := make(map[string]any, len(data))
	for key, value := range data {
		if value == nil {
			continue
		}
		filtered[key] = value
	}
	return filtered
}

// randomID8 生成 8 位十六进制随机串。
// Python: factory.py:671 uuid4().hex[:8]
func randomID8() string {
	return strings.ReplaceAll(uuid.NewString(), "-", "")[:8]
}

// ptr 返回值指针（FromEnv 值类型的便利包装）。
func ptr[T any](value T) *T { return &value }

// errNotImplemented 远程模式未实现的错误。
func errNotImplemented(message string) error {
	return &notImplementedError{message}
}

// notImplementedError 功能未实现。
type notImplementedError struct {
	message string
}

// Error 实现 error。
func (e *notImplementedError) Error() string { return e.message }

// 编译期断言
var _ AgentBackend = (*LocalBackend)(nil)
