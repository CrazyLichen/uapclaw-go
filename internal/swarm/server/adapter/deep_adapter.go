package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	ceschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	modelclients "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool"
	mcptypes "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/mcp/types"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/harness_config"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/evolution"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/interrupt"
	secrail "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/security"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/rails/subagent"
	hschema "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/tools/todo"
	hworkspace "github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/checkpointer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
	sainterfaces "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/interfaces"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation/cwd"
	"github.com/uapclaw/uapclaw-go/internal/common/config"
	"github.com/uapclaw/uapclaw-go/internal/common/dotenv"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/common/workspace"
	commmem "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/memory"
	commonprompt "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/prompt"
	commrails "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/rails"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/recommendation"
	swarmtools "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools"
	cronbridge "github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/common/tools/cron"
	"github.com/uapclaw/uapclaw-go/internal/swarm/agents/harness/team"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/runtime/skill"
	sessionmd "github.com/uapclaw/uapclaw-go/internal/swarm/server/session"
	"github.com/uapclaw/uapclaw-go/internal/swarm/server/utils"
)

// ──────────────────────────── 结构体 ────────────────────────────

// DeepAdapter Deep SDK 适配器，实现 AgentAdapter 接口。
// 封装所有 Deep SDK 专属逻辑：
//   - DeepAgent 实例生命周期管理
//   - Deep runtime tools 注册
//   - Deep stream event 解析
//   - Deep evolution 绑定
//   - Deep interrupt / user_answer 处理
//
// Python: jiuwenswarm/server/runtime/agent_adapter/interface_deep.py (JiuWenClawDeepAdapter)
type DeepAdapter struct {
	// ─── 当前可用字段 ───

	// instance DeepAgent 实例。
	// Python: self._instance（create_deep_agent 返回的 DeepAgent）
	instance *harness.DeepAgent
	// agentName Agent 名称，默认 "main_agent"
	agentName string
	// projectDir 项目目录
	projectDir string
	// workspaceDir 工作区目录
	workspaceDir string
	// agentWorkspaceDir Agent 数据存储路径，始终指向系统 workspace。
	// Python: _agent_workspace_dir（用于 coding_memory、todo 文件等不应写入用户项目目录的数据）。
	// 默认空，使用时回退到 workspaceDir（对齐 Python: getattr(self, "_agent_workspace_dir", None) or self._workspace_dir）。
	agentWorkspaceDir string
	// isCodeAgent 是否编码 Agent 形态（Deep=false, Code=true）
	// 单点 source-of-truth，决定沙箱"主写入根"：
	//   - code-agent → project_dir（工程目录）
	//   - deep-agent → workspace_dir（工作区目录）
	isCodeAgent bool
	// mode 当前运行模式（agent.plan / agent.fast / code 等）
	mode string
	// subMode 子模式
	subMode string
	// configCache 配置缓存（react 配置段）
	configCache map[string]any
	// configBase 配置缓存（完整配置，非 react 子段）
	// Python: config_base = get_config()，用于 DedicatedMultimodalModelConfigured 等需访问根级 models 段的场景
	configBase map[string]any
	// activeSessionIDs 会话活跃计数（Counter 语义，允许并发同 session）
	activeSessionIDs map[string]int
	// streamRoundKindLatch 本轮输出归属（"user"/"goal"），整轮不变。
	// Python: interface_deep.py:2019 _stream_round_kind_latch
	streamRoundKindLatch string
	// streamRoundOutputEnded 上一轮是否已到终块。
	// Python: interface_deep.py:2020 _stream_round_output_ended
	streamRoundOutputEnded bool
	// streamRoundVisibleText 本轮已流出的可见文本（降级去重，上限 256KB）。
	// Python: interface_deep.py:2021 _stream_round_visible_text
	streamRoundVisibleText string
	// streamContentRunKind 上一个可见内容块的归属（判断 user→goal 切换点）。
	// Python: interface_deep.py _stream_content_run_kind
	streamContentRunKind string
	// interactionOutputHandoff 上一次输出的排空屏障（宿主自持，不碰 Core 私有字段）。
	// Python: interface_deep.py:16029 _interaction_output_handoff
	interactionOutputHandoff *outputHandoff
	// sessionAdapters 会话级子适配器缓存（按会话分身）
	// Python: interface_deep.py _session_adapters（:2126）
	sessionAdapters *sessionAdapterCache
	// isSessionScoped 本适配器是否为会话级子适配器。
	// Python: interface_deep.py _is_session_scoped_adapter（:2110 定义，:2711 置真）
	isSessionScoped bool
	// parentSessionID 子适配器所属的会话标识（仅 isSessionScoped 为真时有意义）。
	// Python: interface_deep.py _parent_session_id
	parentSessionID string
	// sessionChildFactory 造一个与原适配器同类的新适配器。
	// Python: type(self)()（_new_session_scoped_adapter :2680）
	//
	// Go 无继承：CodeAdapter 与 DeepAdapter 是两个类型，且 c.deep 不知道自己
	// 属于 CodeAdapter，故用构造期注入的工厂等价替代；实现直接复用
	// CreateAdapter，与 Python create_adapter 同构。
	sessionChildFactory func() (AgentAdapter, error)
	// childMode 子适配器使用的 mode（"agent" 或 "code"）。
	// Python 由 type(self)() 天然决定，Go 需显式注入。
	childMode string
	// sessionAgentTasks 会话标识 → 在飞请求标识集合。
	// Python: interface_deep.py _session_agent_tasks（_register_session_agent_task :4869）
	sessionAgentTasks map[string]map[string]struct{}
	// rootInstanceRequested 根是否已请求建立自己的 DeepAgent。
	// Python: interface_deep.py _root_instance_requested（:2124）
	rootInstanceRequested bool
	// rootInstanceMu 根 DeepAgent 惰建互斥锁。
	// Python: interface_deep.py _root_instance_lock（:2125）
	rootInstanceMu sync.Mutex
	// sessionInstanceConfig create_instance 的入参快照，供 EnsureInstance 惰建时复用。
	// Python: interface_deep.py _session_instance_config
	sessionInstanceConfig map[string]any
	// sessionInstanceMode 惰建时使用的 mode。
	// Python: interface_deep.py _session_instance_mode
	sessionInstanceMode string
	// sessionInstanceSubMode 惰建时使用的 sub_mode。
	// Python: interface_deep.py _session_instance_sub_mode
	sessionInstanceSubMode string
	// sessionInstanceAgentDefinition 声明式 root-Agent 定义快照，供重建时复用。
	// Python: interface_deep.py:13022-13023 self._session_instance_agent_definition
	sessionInstanceAgentDefinition map[string]any
	// channelID 本适配器所属渠道。
	// Python: interface_deep.py:13030-13033 self._channel_id
	channelID string
	// processCliRunOptions 根暂存的一次性 Process CLI 选项，等 child 消费。
	// Python: interface_deep.py:12436 self._process_cli_run_options
	processCliRunOptions *ProcessCliRunOptions
	// sessionMu 保护 activeSessionIDs。
	// 请求路径并发 mark/unmark，AbortOnGatewayDisconnect 与 otherActiveSessions 会
	// 全表遍历；无锁时"遍历 + 写入"并发触发 runtime fatal
	// （concurrent map iteration and map write，recover 不住）。
	sessionMu sync.Mutex
	// interactionConverter 交互 payload 转换函数，注入到 utils.ParseStreamChunk。
	// Python: lazy import convert_interactions_to_ask_user_question
	interactionConverter utils.InteractionConverterFunc

	// ─── 模型与配置 ───

	// stateMu 保护模型/配置状态（model/modelCache/modelNameToKeys/modelClientConfig/
	// modelRequestConfig/defaultModelName/configCache/configBase）。
	// 纪律：写侧必须"构建完再发布"（先建局部 map，再在锁内整体替换），发布后的
	// map 视为不可变；读侧统一走 snapshot 助手。这样 reload 与请求并发时不会出现
	// "一边 range 一边写 map" 的 fatal。
	stateMu sync.RWMutex
	// model Model 实例
	model *llm.Model
	// modelClientConfig 模型客户端配置
	modelClientConfig *llmschema.ModelClientConfig
	// modelRequestConfig 模型请求配置
	modelRequestConfig *llmschema.ModelRequestConfig
	// instanceOverrides 实例覆盖配置
	// instanceOverrides CreateInstance 传入的 config 字典
	instanceOverrides map[string]any
	// modelCache 模型缓存（按模型名缓存已创建的 Model 实例）
	modelCache map[string]*llm.Model
	// modelNameToKeys 模型名到 API key 列表的映射
	modelNameToKeys map[string][]string
	// defaultModelName 默认模型名称
	defaultModelName string
	// configLister 自定义 agent 配置列表接口（避免 adapter↔runtime 循环依赖）
	// Python: AgentConfigService
	configLister AgentConfigLister
	// runtimeConfigUpdater 运行时配置更新函数指针。
	// 默认指向 d.updateRuntimeConfig；CodeAdapter 通过覆写此字段实现子类行为，
	// 对齐 Python 继承中 self._update_runtime_config() 的动态分派。
	// 修复 S-17: Go 无继承多态，通过函数指针实现等价的动态分派。
	runtimeConfigUpdater func(ctx context.Context, config *runtimeConfig)
	// defaultModelConfigReader 从 config.yaml 读 models.default.model_client_config。
	// [Go 适配] 对齐 Python _has_valid_model_config 第 4 步的 get_config() 兜底；
	// 抽成函数字段便于单测注入（Python 侧靠模块级 get_config 缓存，无法在测试中替换）。
	defaultModelConfigReader func() map[string]any

	// ─── ⤵️ 10.6.3-10: 轨道（Rails）───

	// filesystemRail 文件系统护栏
	filesystemRail *rails.SysOperationRail
	// skillRail 技能使用护栏
	// ⤵️ 10.6.3-10: SkillUseRail
	skillRail sainterfaces.AgentRail
	// streamEventRail 流事件护栏
	streamEventRail *commrails.JiuClawStreamEventRail
	// taskPlanningRail 任务规划护栏
	taskPlanningRail *rails.TaskPlanningRail
	// contextAssembleRail 上下文组装护栏
	contextAssembleRail sainterfaces.AgentRail
	// contextAssembleMode 当前上下文组装模式（"agent.work.plan" / "agent.work.normal"）
	// 值已赋，按模式切换逻辑待 10.6.3-10
	contextAssembleMode string
	// contextProcessorRail 上下文处理护栏
	contextProcessorRail sainterfaces.AgentRail
	// runtimePromptRail 运行时提示词护栏
	// ✅ 已回填：RuntimePromptRail（对齐 Python: _runtime_prompt_rail: RuntimePromptRail | None）
	runtimePromptRail *commrails.RuntimePromptRail
	// responsePromptRail 响应提示词护栏
	// ✅ 10.6.3-10: ResponsePromptRail
	responsePromptRail sainterfaces.AgentRail
	// modelAnomalyRail 模型异常检测护栏（重复输出/流超时/瞬时失败/工具循环压缩）
	modelAnomalyRail sainterfaces.AgentRail
	// securityRail 安全护栏
	// ✅ 已回填：SafetyPromptRail（对齐 Python: _security_rail: SecurityRail | None）
	securityRail sainterfaces.AgentRail
	// memoryRail 记忆护栏
	// ✅ 已回填：MemoryRail（对齐 Python: _memory_rail: MemoryRail | None）
	memoryRail sainterfaces.AgentRail
	// externalMemoryRail 外接记忆护栏
	// ⤵️ 10.6.3-10: 外部记忆 rail
	externalMemoryRail sainterfaces.AgentRail
	// externalMemoryRailRegistered 外接记忆护栏是否已注册
	// ⤵️ 10.6.3-10: 防止重复注册
	externalMemoryRailRegistered bool
	// heartbeatRail 心跳护栏
	heartbeatRail *rails.HeartbeatRail
	// skillEvolutionRail 技能演进护栏
	// ✅ 已回填：SkillEvolutionRail（对齐 Python: _skill_evolution_rail: SkillEvolutionRail | None）
	skillEvolutionRail *evolution.SkillEvolutionRail
	// skillCreateRail 技能创建护栏（单 Agent 版）
	// 对齐 Python: _skill_create_rail: SkillCreateRail | None
	skillCreateRail *evolution.SkillCreateRail
	// subagentRail 子代理护栏
	// ✅ 已回填：SubagentRail（对齐 Python: _subagent_rail: SubagentRail | None）
	subagentRail *subagent.SubagentRail
	// permissionRail 权限护栏
	// ✅ 已回填：PermissionInterruptRail（对齐 Python: _permission_rail: PermissionInterruptRail | None）
	permissionRail sainterfaces.AgentRail
	// avatarRail 数字分身护栏
	avatarRail sainterfaces.AgentRail

	// ─── 运行时 ───

	// toolCards 工具卡片列表
	toolCards []*tool.ToolCard
	// sysOperation 系统操作实例（已回填 10.3.7-11 SysOpBuilder）
	sysOperation sysop.SysOperation
	// sysOperationCard 系统操作卡片（已回填 10.3.7-11 SysOpBuilder）
	sysOperationCard *sysop.SysOperationCard
	// visionModelConfig 视觉模型配置
	visionModelConfig *hschema.VisionModelConfig
	// visionTools 视觉工具实例列表（对齐 Python: self._vision_tools）
	visionTools []tool.Tool
	// visionToolsRegistered 视觉工具注册状态
	visionToolsRegistered bool
	// audioModelConfig 音频模型配置
	audioModelConfig *hschema.AudioModelConfig
	// audioTools 音频工具实例列表（对齐 Python: self._audio_tools）
	audioTools []tool.Tool
	// audioToolsRegistered 音频工具注册状态
	audioToolsRegistered bool
	// videoModelConfig 视频模型配置
	videoModelConfig *hschema.VideoModelConfig
	// videoTool 视频工具实例（对齐 Python: self._video_tools）
	videoTool tool.Tool
	// videoToolRegistered 视频工具注册状态
	videoToolRegistered bool
	// imageGenToolRegistered 图片生成工具是否已注册（对齐 Python: self._image_gen_tool_registered）
	imageGenToolRegistered bool
	// imageGenModelConfig 图片生成模型配置是否就绪（对齐 Python: self._image_gen_model_config: bool）
	imageGenModelConfig bool
	// skillManager 技能管理器（已回填 10.3.19-20）
	skillManager *skill.SkillManager
	// a2xClient A2X 客户端
	// ⤵️ A2X / 10.6.22
	a2xClient any
	// a2xConfig A2X 配置
	// ⤵️ A2X / 10.6.22
	a2xConfig map[string]any
	// a2xBlankServiceID A2X blank 服务 ID
	// ⤵️ A2X / 10.6.22
	a2xBlankServiceID string
	// a2xBlankDataset A2X blank 数据集
	// ⤵️ A2X / 10.6.22
	a2xBlankDataset string
	// cronRuntime Cron 运行时桥接（11.10 已回填）
	// Python: self._cron_runtime = CronRuntimeBridge()
	cronRuntime *cronbridge.CronRuntimeBridge
	// dreamingMode dreaming 模式
	dreamingMode string
	// dreamingStarted dreaming 是否已启动
	dreamingStarted bool
	// registeredMCPServerIDs 已注册 MCP 服务 ID 集合
	registeredMCPServerIDs map[string]bool
	// registeredMCPServers 已注册 MCP 服务配置
	registeredMCPServers map[string]*mcptypes.McpServerConfig
	// autoHarnessService 自动 Harness 服务
	// ⤵️ 10.6.11
	autoHarnessService any
	// sendFileToolkit 发送文件工具包
	// Python: self._send_file_toolkit: SendFileToolkit | None (interface_deep.py L551)
	// ✅ 10.6.24b 已回填
	sendFileToolkit *swarmtools.SendFileToolkit
	// isProactiveMemory 是否主动记忆
	isProactiveMemory *bool
	// paidSearchRegistered 付费搜索是否已注册
	paidSearchRegistered bool
	// paidSearchTool 付费搜索工具实例
	paidSearchTool tool.Tool
	// uapswarmProjectDir 项目目录，对齐 Python: _jiuwenswarm_project_dir
	uapswarmProjectDir string
	// uapswarmCodeProjectDir 代码项目目录，对齐 Python: _jiuwenswarm_code_project_dir
	uapswarmCodeProjectDir string
	// uapswarmProjectMemoryDir 项目记忆目录，对齐 Python: _jiuwenswarm_project_memory_dir
	uapswarmProjectMemoryDir string
	// sendPushFunc 推送函数，由 AgentServer 创建 DeepAdapter 时注入。
	// 替代原 globalSendPushFunc 全局变量，实现实例级隔离。
	// 避免 adapter→server→adapter 循环依赖：adapter 包不导入 server 包。
	sendPushFunc func(ctx context.Context, msg map[string]any) error
}

// ApprovalAnswer 审批回答条目，从前端 WebSocket 消息解析。
// Python: answers 参数为 list[dict]，每个 dict 包含 selected_options 字段。
// 对齐 Python: JiuWenClawDeepAdapter._handle_evolution_approval(answers: list)
type ApprovalAnswer struct {
	// SelectedOptions 已选项标签列表
	SelectedOptions []string `json:"selected_options"`
}

// ──────────────────────────── 常量 ────────────────────────────

// persistentCheckpointerInitTimeout 持久化检查点器初始化超时上限。
// SQLite 打开是磁盘 IO，卡住时不能无限占用全局初始化锁。
const persistentCheckpointerInitTimeout = 30 * time.Second

// ──────────────────────────── 全局变量 ────────────────────────────

// persistentCheckpointerReady 持久化检查点器是否已就绪。
// Python: interface_deep.py (_PERSISTENT_CHECKPOINTER_READY)
var persistentCheckpointerReady bool

// persistentCheckpointerLock 持久化检查点器初始化锁（double-check locking）。
// Python: interface_deep.py (_PERSISTENT_CHECKPOINTER_LOCK)
var persistentCheckpointerLock sync.Mutex

// defaultSendPushFunc 默认推送函数，由 AgentServer 启动时通过 SetDefaultSendPushFunc 设置。
// DeepAdapter 实例未显式注入 sendPushFunc 时使用此默认值。
// 避免 adapter→server→adapter 循环依赖：adapter 包不导入 server 包。
var defaultSendPushFunc func(ctx context.Context, msg map[string]any) error

// placeholderAPIBase 占位 api_base 集合：命中即视为「模型未配置」。
// Python: interface_deep.py (_PLACEHOLDER_API_BASES)
//
//	_PLACEHOLDER_API_BASES = frozenset({"https://example.com/compatible-mode/v1"})
//
// 这是 resources/config.yaml 里的示例地址：用户没配真实服务时，若不做拦截，
// 心跳/首次对话会真的向 example.com 发请求，报出底层网络错误而不是
// 「模型未正确配置」。新增示例地址时须同步此集合。
var placeholderAPIBase = map[string]struct{}{
	"https://example.com/compatible-mode/v1": {},
}

// ──────────────────────────── 导出函数 ────────────────────────────

// SetDefaultSendPushFunc 设置默认推送函数。
// 由 AgentServer 启动时调用，为未显式注入 sendPushFunc 的 DeepAdapter 实例提供默认值。
// 避免 adapter→server→adapter 循环依赖。
func SetDefaultSendPushFunc(fn func(ctx context.Context, msg map[string]any) error) {
	defaultSendPushFunc = fn
}

// ResetPersistentCheckpointer 重置持久化检查点器就绪标记（仅测试用）。
// Python 侧 _PERSISTENT_CHECKPOINTER_READY 是进程级标记，但 pytest 每个测试
// 重新 setup 临时目录时同样面临单例指向旧目录的问题（Python 测试通过
// subprocess/fixture 隔离规避）。Go 测试进程内多个测试连续 CreateInstance
// 且各自重定向 UAPCLAW_DATA_DIR 时，首次绑定的 SQLite 路径随 TempDir 清理
// 而失效，后续 PreRun→PreAgentExecute 报 disk I/O error。
// 测试在重定向 UAPCLAW_DATA_DIR 后调用本函数，使下次 CreateInstance 的
// set_checkpoint 重新按新目录创建检查点器。
func ResetPersistentCheckpointer() {
	persistentCheckpointerLock.Lock()
	defer persistentCheckpointerLock.Unlock()
	persistentCheckpointerReady = false
}

// SetSkillManager 设置技能管理器。
// Python: def set_skill_manager(self, skill_manager: SkillManager) -> None: self._skill_manager = skill_manager
func (d *DeepAdapter) SetSkillManager(skillMgr *skill.SkillManager) {
	d.skillManager = skillMgr
}

// SetSendPushFunc 设置推送函数，由 AgentServer 创建适配器后注入。
// 替代原 SetGlobalSendPushFunc，实现实例级隔离。
// 避免 adapter→server→adapter 循环依赖：adapter 包不导入 server 包。
func (d *DeepAdapter) SetSendPushFunc(fn func(ctx context.Context, msg map[string]any) error) {
	d.sendPushFunc = fn
}

// SetConfigLister 设置自定义 agent 配置列表接口。
// Python: AgentConfigService 依赖注入
func (d *DeepAdapter) SetConfigLister(lister AgentConfigLister) {
	d.configLister = lister
}

// ModelCache 返回模型缓存。
// Python: self._model_cache (interface_deep.py L543)
// AgentTool 通过类型断言获取，拿不到则 fallback nil。
func (d *DeepAdapter) ModelCache() map[string]*llm.Model {
	_, cache, _ := d.modelsSnapshot()
	return cache
}

// NewDeepAdapter 创建 DeepAdapter 实例。
// Python: JiuWenClawDeepAdapter.__init__()
func NewDeepAdapter() *DeepAdapter {
	d := &DeepAdapter{
		agentName:              "main_agent",
		isCodeAgent:            false,
		activeSessionIDs:       make(map[string]int),
		sessionAdapters:        newSessionAdapterCache(),
		sessionChildFactory:    func() (AgentAdapter, error) { return CreateAdapter("harness", "agent") },
		childMode:              "agent",
		sessionAgentTasks:      make(map[string]map[string]struct{}),
		modelCache:             make(map[string]*llm.Model),
		modelNameToKeys:        make(map[string][]string),
		registeredMCPServerIDs: make(map[string]bool),
		registeredMCPServers:   make(map[string]*mcptypes.McpServerConfig),
		interactionConverter:   interrupt.ConvertInteractionsToAskUserQuestion,
		// 11.10 已回填：CronRuntimeBridge 构造
		// Python: self._cron_runtime = CronRuntimeBridge() (interface_deep.py L538)
		cronRuntime: cronbridge.NewCronRuntimeBridge(),
	}
	// 默认指向 DeepAdapter 自身的 updateRuntimeConfig 方法
	// CodeAdapter 在构造后覆写此字段实现子类行为（修复 S-17）
	d.runtimeConfigUpdater = d.updateRuntimeConfig
	// 模型配置兜底读取器：对齐 Python _has_valid_model_config 第 4 步。
	// 优先读已发布的 configBase 快照，未初始化时才回退读盘（见 T-07）。
	d.defaultModelConfigReader = d.defaultModelClientConfigFromConfigBase
	return d
}

// CreateInstance 初始化底层 SDK Agent。
// Python: JiuWenClawDeepAdapter.create_instance() (line 2527-2621)
//
// Python 执行步骤：
//
//	Python: 1. await self.set_checkpoint()
//	Python: 2. self._dreaming_mode = mode if mode.startswith("agent") else "agent"
//	Python: 3. self._instance_overrides = dict(config or {})
//	Python: 4. load_dotenv(dotenv_path=get_env_file(), override=True)
//	Python: 5. config_base = get_config()
//	Python: 6. self._refresh_multimodal_configs(config_base)
//	Python: 7. config = config_base.get("react", {}).copy()
//	Python: 8. self._config_cache = config.copy()
//	Python: 9. self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
//	Python: 10. self._project_dir = overrides.get("project_dir", config.get("project_dir"))
//	Python: 11. self._workspace_dir = config.get("workspace_dir", str(get_agent_workspace_dir()))
//	Python: 12. model = self._create_model(config_base)
//	Python: 13. await self._try_init_a2x_client(config_base)
//	Python: 14. agent_card = AgentCard(name=self._agent_name, id='jiuwenswarm')
//	Python: 15. tool_cards = await self._get_tool_cards(agent_card.id)
//	Python: 16. rails_list = self._build_agent_rails(config, config_base, mode=mode)
//	Python: 17. sys_operation = self._create_sys_operation()
//	Python: 18. configured_subagents, should_add_general_agent = self._build_configured_subagents(...)
//	Python: 19. self._instance = create_deep_agent(**common_kwargs)
//	Python: 20. await self._instance.ensure_initialized()
//	Python: 21. self._seed_runtime_cwd(self._project_dir or self._workspace_dir)
//	Python: 22. self._sync_a2x_runtime_state()
//	Python: 23. self._registered_mcp_server_ids.clear()
//	Python: 24. await self._register_mcp_servers_from_config(config_base, tag=f"agent.{mode}")
//	Python: 25. await self.load_user_rails()
func (d *DeepAdapter) CreateInstance(ctx context.Context, configMap map[string]any, mode string, subMode string,
	agentDefinition map[string]any) error {
	// 步骤 1: set_checkpoint
	if err := d.setCheckpoint(ctx); err != nil {
		return fmt.Errorf("set_checkpoint 失败: %w", err)
	}

	// 步骤 2: dreaming_mode 设置
	// Python: self._dreaming_mode = mode if mode and mode.startswith("agent") else "agent"
	if mode != "" && strings.HasPrefix(mode, "agent") {
		d.dreamingMode = mode
	} else {
		d.dreamingMode = "agent"
	}

	// 步骤 3: instanceOverrides
	d.instanceOverrides = make(map[string]any)
	for k, v := range configMap {
		d.instanceOverrides[k] = v
	}

	// 步骤 4: load_dotenv
	if err := dotenv.Load(workspace.EnvFile()); err != nil {
		logger.Warn(logComponent).Err(err).Msg("load_dotenv failed, continuing with current env vars")
	}

	// 步骤 5: get_config → configBase
	// 对齐 Python get_config()：Load 后过 _normalize_config（custom_headers
	// 结构化 + channels.web.send_file_allowed 默认 true）——2026-10-28 审查接线
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return fmt.Errorf("创建配置管理器失败: %w", err)
	}
	configBase, err := cfg.Load()
	if err != nil {
		return fmt.Errorf("加载配置失败: %w", err)
	}

	// 步骤 6: _refresh_multimodal_configs(configBase)
	d.refreshMultimodalConfigs(configBase)

	// 步骤 7-8: 读取 react 配置段，缓存到 configCache
	// Python: config = config_base.get("react", {}).copy(); self._config_cache = config.copy()
	// 先构建局部副本，再一次性发布（发布后不可变，读侧可安全并发访问）。
	var config map[string]any
	newCache := make(map[string]any)
	if reactRaw, ok := configBase["react"]; ok {
		if reactMap, ok := reactRaw.(map[string]any); ok {
			config = make(map[string]any, len(reactMap))
			for k, v := range reactMap {
				config[k] = v
				newCache[k] = v
			}
		}
	}
	if config == nil {
		config = make(map[string]any)
	}
	d.setConfigSnapshots(newCache, configBase)

	// 步骤 9: agentName
	// Python: self._agent_name = overrides.get("agent_name", config.get("agent_name", "main_agent"))
	if v, ok := d.instanceOverrides["agent_name"]; ok {
		if s, ok := v.(string); ok {
			d.agentName = s
		}
	} else if v, ok := config["agent_name"]; ok {
		if s, ok := v.(string); ok {
			d.agentName = s
		}
	}
	// 步骤 9b: 声明式 root-Agent 的 name 覆盖上面两步
	// Python: interface_deep.py:13096-13097
	//   if agent_definition is not None: self._agent_name = agent_definition["name"]
	if agentDefinition != nil {
		if name, ok := agentDefinition["name"].(string); ok && strings.TrimSpace(name) != "" {
			d.agentName = strings.TrimSpace(name)
		}
	}

	// 步骤 10: projectDir
	if v, ok := d.instanceOverrides["project_dir"]; ok {
		if s, ok := v.(string); ok {
			d.projectDir = s
		}
	} else if v, ok := config["project_dir"]; ok {
		if s, ok := v.(string); ok {
			d.projectDir = s
		}
	}

	// 步骤 11: workspaceDir
	if v, ok := config["workspace_dir"]; ok {
		if s, ok := v.(string); ok && s != "" {
			d.workspaceDir = s
		}
	}
	if d.workspaceDir == "" {
		// Python: config.get("workspace_dir", str(get_agent_workspace_dir()))
		// 默认为 agent/workspace（get_agent_workspace_dir = agent_root/workspace），
		// 不是 agent 根目录——此前误用 AgentRootDir() 导致 ReadContextFiles
		// 的 GetNodePath 拼出 <data>/agent/AGENT.md（文件实际在
		// <data>/agent/workspace/AGENT.md），# 项目上下文 节永远为空。
		d.workspaceDir = workspace.AgentWorkspaceDir()
	}

	// Python: setattr(self._instance, "_jiuwenswarm_project_dir", self._project_dir or self._workspace_dir)
	d.uapswarmProjectDir = d.projectDir
	if d.uapswarmProjectDir == "" {
		d.uapswarmProjectDir = d.workspaceDir
	}

	// Python: self._jiuwenswarm_code_project_dir
	if v, ok := config["code_project_dir"]; ok {
		if s, ok := v.(string); ok && s != "" {
			d.uapswarmCodeProjectDir = s
		}
	}

	// 存储 mode/subMode
	d.mode = mode
	d.subMode = subMode

	// 步骤 11.5: 根只做 router/template holder，不建自己的 DeepAgent
	// Python: interface_deep.py :13109 if self._skip_own_instance_build(): return
	//
	// 位置与上游一致：在便宜的 config-cache 段之后、model 构建之前。
	// 先记下惰建所需的入参，供 EnsureInstance 复用。
	//
	// ⤵️ 上游此处还有 web channel 的 _start_mcp_prewarm()（fire-and-forget
	//    预热 connected MCP 的进程级连接缓存）；Go 侧 MCP 生命周期尚未对齐，
	//    随 R-6 一并回填。
	d.sessionInstanceConfig = configMap
	d.sessionInstanceMode = mode
	d.sessionInstanceSubMode = subMode
	// Python: interface_deep.py:13022-13023
	//   self._session_instance_agent_definition = deepcopy(agent_definition)
	d.sessionInstanceAgentDefinition = copyMap(agentDefinition)
	// Python: interface_deep.py:13030-13033
	//   self._channel_id = str(config.get("channel_id") or "").strip() or getattr(self, "_channel_id", "")
	if v, ok := config["channel_id"].(string); ok && strings.TrimSpace(v) != "" {
		d.channelID = strings.TrimSpace(v)
	}
	if d.skipOwnInstanceBuild() {
		logger.Info(logComponent).
			Str("mode", mode).
			Str("sub_mode", subMode).
			Msg("root adapter skips own DeepAgent build (router only)")
		return nil
	}

	// 步骤 12: model = d.createModel(configBase)
	// createModel 内部构建完再发布（stateMu 保护），调用方不要再直接赋值 d.model。
	d.createModel(configBase)

	// 步骤 13: _try_init_a2x_client(configBase)
	// ⤵️ A2X / 10.6.22: A2X 客户端初始化

	// 步骤 14: agentCard = AgentCard(name=agent_name, id='jiuwenswarm')
	// Python: agent_card = AgentCard(name=self._agent_name, id='uapclaw')
	// Python: interface_deep.py:13132-13133 —— 声明式定义时带 description
	cardOpts := []agentschema.AgentCardOption{
		agentschema.WithAgentName(d.agentName),
		agentschema.WithAgentID("uapclaw"),
	}
	if agentDefinition != nil {
		if desc, ok := agentDefinition["description"].(string); ok && strings.TrimSpace(desc) != "" {
			cardOpts = append(cardOpts, agentschema.WithAgentDescription(strings.TrimSpace(desc)))
		}
	}
	agentCard := agentschema.NewAgentCard(cardOpts...)

	// 步骤 15: tool_cards = await self._get_tool_cards(agent_card.id)
	// Python: tool_cards = await self._get_tool_cards(agent_card.id)
	// Python: self._tool_cards = tool_cards（G8: 存储到 adapter 字段）
	toolCards := d.getToolCards(agentCard.ID)
	d.toolCards = toolCards

	// 步骤 16: rails_list = _build_agent_rails(config, configBase, mode=mode)
	railsList := d.buildAgentRails(config, configBase, mode)

	// 步骤 17: sys_operation = _create_sys_operation()
	// Python: sys_operation = self._create_sys_operation()
	// Python: if sys_operation is None: raise RuntimeError (G7: nil 检查)
	sysOpInstance := d.createSysOperation(configBase)
	if sysOpInstance == nil {
		return fmt.Errorf("sys_operation 不可用，可能任务未在运行")
	}

	// 步骤 18: configured_subagents, should_add_general_agent = _build_configured_subagents(...)
	// Python: _build_configured_subagents(model, config, configBase)
	subagentSpecs, shouldEnableGeneralAgent := d.buildConfiguredSubagents(config, configBase)

	// Python: interface_deep.py:13163-13165
	//   if agent_definition is not None and agent_definition.get("tools") != "*":
	//       # Do not build delegates with a broader tool set than the root.
	//       configured_subagents = []
	// 显式工具白名单的声明式 root 不允许派生更宽工具集的子代理
	if agentDefinition != nil && !isWildcardTools(agentDefinition["tools"]) {
		subagentSpecs = nil
	}

	// Python: L2575-2577: should_enable_general_agent = should_add_general_agent and (sub_mode == "plan" or mode.startswith("agent"))
	shouldEnableGeneralAgent = shouldEnableGeneralAgent && (d.subMode == "plan" || strings.HasPrefix(d.mode, "agent"))

	// 步骤 19: 组装 CreateDeepAgentParams 并调用工厂
	// Python: L13239-13241 enable_model_anomaly_detection_rail=(execution_guard...).get("enabled", False)
	anomalyEnabled := resolveEnableModelAnomalyDetectionRail(configBase)
	// Python: self._instance = create_deep_agent(**common_kwargs, ...)
	resolvedLanguage := d.resolveRuntimeLanguage()
	systemPrompt := commonprompt.BuildAgentIdentityPrompt(d.resolvePromptLanguage())
	// Python: interface_deep.py:13184-13193 —— 声明式定义的 instructions 追加进 system prompt
	//   if agent_definition is not None:
	//       system_prompt = (f"{system_prompt.rstrip()}\n\n# Agent Instructions\n"
	//                        f"{agent_definition['instructions'].strip()}")
	//       system_prompt = apply_tool_policy_prompt(system_prompt, agent_definition.get("tools", "*"))
	var declaredSkills []string
	if agentDefinition != nil {
		instructions, _ := agentDefinition["instructions"].(string)
		systemPrompt = strings.TrimRight(systemPrompt, " \t\n") +
			"\n\n# Agent Instructions\n" + strings.TrimSpace(instructions)
		// 注意：上游传的是原始 tools 值（可能是 "*"），不是过滤后的名单
		systemPrompt = schema.ApplyToolPolicyPrompt(systemPrompt, toolsFromDefinition(agentDefinition))
		// Python: interface_deep.py:13200
		//   skills=list(agent_definition.get("skills") or ()) if agent_definition else None
		declaredSkills = stringSliceOf(agentDefinition["skills"])
	}

	params := harness_config.CreateDeepAgentParams{
		Model:                  d.modelSnapshot(),
		Card:                   agentCard,
		SystemPrompt:           systemPrompt,
		ToolCards:              toolCards,
		Mcps:                   nil, // Python: mcps 在 create_deep_agent 之后通过 _register_mcp_servers_from_config 单独注册
		Subagents:              subagentSpecs,
		Rails:                  railsList,
		EnableTaskLoop:         d.resolveEnableTaskLoop(config, configBase),
		AddGeneralPurposeAgent: shouldEnableGeneralAgent,
		MaxIterations:          paramsInt(config, "max_iterations", 15),
		Workspace:              hworkspace.NewWorkspace(d.workspaceDir, resolvedLanguage),
		// Python: interface_deep.py:13200
		//   skills=list(agent_definition.get("skills") or ()) if agent_definition else None
		// 无声明式定义时保持 nil（对齐 Python create_instance 不传 skills）
		Skills:             declaredSkills,
		Language:           resolvedLanguage,
		PromptMode:         d.resolvePromptMode(configBase),
		VisionModelConfig:  d.visionModelConfig,
		AudioModelConfig:   d.audioModelConfig,
		EnableTaskPlanning: d.resolveEnableTaskPlanning(config, configBase),
		// 开关与 builder 必须成对：builder 已构建时工厂不再补默认，反之亦然
		EnableModelAnomalyDetectionRail: &anomalyEnabled,
		// todo_compat 兼容层注入（对齐 Python interface_deep.py:230 install_todo_modify_compat_patch 模块级 patch）
		TodoModifyFactory:   swarmtools.NewCompatibleTodoModifyTool,
		AutoCreateWorkspace: false,                                              // Python: 硬编码 false
		CompletionTimeout:   paramsFloat(config, "completion_timeout", 3600.0),  // Python: config.get("completion_timeout", 3600.0)
		ContextEngineConfig: d.deepAgentContextEngineConfig(config, configBase), // Python: context_engine_config=_deep_agent_context_engine_config(config)
	}
	// 步骤 17 回填：SysOperation
	params.SysOperation = sysOpInstance

	agent, createErr := harness.CreateDeepAgent(ctx, params)
	if createErr != nil {
		return fmt.Errorf("CreateDeepAgent 失败: %w", createErr)
	}
	d.instance = agent

	// 对齐 Python: setattr(self._instance, "_jiuwenswarm_project_dir", ...)
	d.instance.SetUapswarmProjectDir(d.uapswarmProjectDir)
	if d.uapswarmCodeProjectDir != "" {
		d.instance.SetUapswarmCodeProjectDir(d.uapswarmCodeProjectDir)
	}
	if mode != "" {
		d.instance.SetUapswarmAdapterMode(mode)
	}

	// 步骤 20: d.instance.EnsureInitialized(ctx)
	// Python: await self._instance.ensure_initialized()
	if _, initErr := d.instance.EnsureInitialized(ctx); initErr != nil {
		return fmt.Errorf("DeepAgent EnsureInitialized 失败: %w", initErr)
	}

	// 步骤 21: _seed_runtime_cwd(d.projectDir or d.workspaceDir)
	initCwd := d.projectDir
	if initCwd == "" {
		initCwd = d.workspaceDir
	}
	d.seedRuntimeCwd(ctx, initCwd)

	// 步骤 21.1: _update_runtime_config()
	// 对齐 Python: CreateInstance 中需调用 updateRuntimeConfig 更新 Language/Channel/Mode 等
	d.runtimeConfigUpdater(ctx, &runtimeConfig{
		CWD:          initCwd,
		Language:     d.resolvePromptLanguage(),
		Mode:         mode,
		SessionID:    "",
		ProjectDir:   d.projectDir,
		ForceEnglish: d.isCodeAgent,
	})

	// 步骤 22: _sync_a2x_runtime_state()
	// ⤵️ A2X / 10.6.22: A2X 运行时状态同步

	// 步骤 23: d.registeredMCPServerIDs.clear()
	d.registeredMCPServerIDs = make(map[string]bool)
	d.registeredMCPServers = make(map[string]*mcptypes.McpServerConfig)

	// 步骤 24: _register_mcp_servers_from_config(configBase, tag)
	// Python: await self._register_mcp_servers_from_config(config_base, tag=f"agent.{mode}")
	if regErr := d.registerMcpServersFromConfig(ctx, configBase, fmt.Sprintf("agent.%s", mode)); regErr != nil {
		logger.Warn(logComponent).Err(regErr).Msg("MCP service registration failed, continuing")
	}

	// 步骤 25: load_user_rails()
	// 对齐 Python: load_user_rails() — 动态加载用户自定义的 Rail 扩展
	// Go 不支持 Python 的动态导入机制，此处为占位实现
	// TODO(#9.63): 等用户自定义 Rail 扩展机制设计完成后实现
	d.loadUserRails()

	// 步骤 26: start_interaction
	// Python: interface_deep.py:15069 —— create_instance 成功后立刻绑定会话并
	// 启动交互循环；GoalManager 只在这一步构造（deep_agent.py:3972）。
	// 只有会话级 child 才持有确定会话（28f 的 MarkAsSessionScoped 写入），
	// 根适配器不绑定会话，交由请求期惰建（见 startInteractionIfNeeded）。
	if d.isSessionScoped && d.parentSessionID != "" {
		if startErr := d.startInteractionIfNeeded(ctx, d.parentSessionID); startErr != nil {
			logger.Warn(logComponent).Err(startErr).
				Str("event_type", "GOAL_INTERACTION_START_FAILED").
				Str("session_id", d.parentSessionID).
				Msg("start interaction failed; goal capability unavailable for this session")
		}
	}

	logger.Info(logComponent).
		Str("agent_name", d.agentName).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Msg("DeepAdapter CreateInstance completed")
	return nil
}

// ReloadAgentConfig 热重载配置，不重启进程。
// Python: JiuWenClawDeepAdapter.reload_agent_config() (line 2646-2752)
//
// Python 执行步骤：
//
//	Python: 1. config_base = configBase or get_config()
//	Python: 2. if envOverrides: apply env overrides
//	Python: 3. config = config_base.get("react", {}).copy()
//	Python: 4. self._config_cache = config.copy()
//	Python: 5. self._refresh_multimodal_configs(config_base)
//	Python: 6. model = self._create_model(config_base)
//	Python: 7. self._model = model
//	Python: 8. rails_list = self._get_current_agent_rails(config, config_base)
//	Python: 9. new_tool_cards = await self._get_tool_cards("jiuwenswarm")
//	Python: 10. self._update_permission_rail(config_base)
//	Python: 11. await self._instance.configure(
//	   对应 Python: model=model, tools=new_tool_cards, rails=rails_list,
//	   对应 Python: subagents=subagents, enable_task_loop=..., max_iterations=...)
//	Python: 12. self._registered_mcp_server_ids.clear()
//	Python: 13. await self._register_mcp_servers_from_config(config_base, tag)
func (d *DeepAdapter) ReloadAgentConfig(ctx context.Context, configBase map[string]any, envOverrides map[string]any) error {
	// 步骤 0: 根无 instance 的分支
	// Python: _reload_agent_config :13623-13631
	//
	// 上游注释：根 adapter 的惰建 DeepAgent 若还没被任何人需要过，
	// "it has nothing of its own to reconfigure, but the cached config
	//  snapshot still has to move forward and the live session adapters
	//  still have to be reloaded."
	//
	// 所以根分支必须做三件实事，不能直接返回：
	//  1. 推进 config 快照（否则后续 child 建出来还是旧配置）
	//  2. 取消 skill retrieval 构建（⤵️ 依赖该链路，随 R-5）
	//  3. 扇出到所有 live child 重载（否则已在跑的会话继续用旧模型）
	if d.instance == nil {
		if d.isSessionScoped {
			return fmt.Errorf("DeepAdapter 未初始化，请先调用 CreateInstance()")
		}
		if err := d.applyReloadConfigSnapshot(configBase, envOverrides); err != nil {
			return err
		}
		// ⤵️ _cancel_skill_retrieval_build_after_reload：依赖 skill retrieval
		//    构建缓存，随 R-5（tool_retrieval 开关）回填。
		d.fanOutReloadToSessionAdapters(ctx, configBase, envOverrides)
		logger.Info(logComponent).
			Msg("config hot-reloaded (root instance not built; snapshot refreshed and session adapters cascaded)")
		return nil
	}

	// Python: clear_config_cache() + clear_memory_manager_cache()
	// （interface_deep.py import 源为 jiuwenswarm...memory，即 swarm 版记忆包）
	// ✅ 已回填：ClearConfigCache + ClearMemoryManagerCache（指向 swarm 侧 commmem，对齐 Python 双清缓存）
	commmem.ClearConfigCache()
	commmem.ClearMemoryManagerCache()

	// 步骤 1: configBase 或 get_config()
	// Python: if config_base is None: config_base = get_config()
	// Python: else: config_base = resolve_env_vars(config_base) (G10)
	if configBase == nil {
		// 对齐 Python get_config()：normalize 后处理一并应用（2026-10-28 审查接线）
		cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
		if err != nil {
			return fmt.Errorf("创建配置管理器失败: %w", err)
		}
		configBase, err = cfg.Load()
		if err != nil {
			return fmt.Errorf("加载配置失败: %w", err)
		}
	} else {
		// G10: 对齐 Python resolve_env_vars(config_base) — 外部传入的 configBase 也需要解析环境变量
		configBase = config.ResolveEnvVars(configBase, nil).(map[string]any)
	}

	// 步骤 2: 应用环境变量覆盖
	// Python: for env_key, env_value in env_overrides.items(): os.environ[str(env_key)] = str(env_value)
	for k, v := range envOverrides {
		if v == nil {
			_ = os.Unsetenv(k)
		} else {
			_ = os.Setenv(k, fmt.Sprintf("%v", v))
		}
	}

	// 步骤 3-4: 读取 react 配置段，更新 configCache
	// 构建局部副本 → 一次性发布，避免与并发读侧争用同一张 map。
	var config map[string]any
	newCache := make(map[string]any)
	if reactRaw, ok := configBase["react"]; ok {
		if reactMap, ok := reactRaw.(map[string]any); ok {
			config = make(map[string]any, len(reactMap))
			for k, v := range reactMap {
				config[k] = v
				newCache[k] = v
			}
		}
	}
	if config == nil {
		config = make(map[string]any)
	}

	// 对齐 Python L2710: if "agent_name" in self._instance_overrides: self._agent_name = ...
	if name, ok := d.instanceOverrides["agent_name"].(string); ok && name != "" {
		d.agentName = name
	} else if name, ok := config["agent_name"].(string); ok && name != "" {
		d.agentName = name
	}

	// 步骤 5: _refresh_multimodal_configs(configBase)
	d.setConfigSnapshots(newCache, configBase)
	d.refreshMultimodalConfigs(configBase)

	// 步骤 6-7: 重建模型（createModel 内部构建完再发布，不直接赋值字段）
	d.createModel(configBase)

	// 步骤 7.5: A2X 重载 + 状态同步
	// ⤵️ A2X / 10.6.22: _try_init_a2x_client(configBase, reload=True)
	// ⤵️ A2X / 10.6.22: _sync_a2x_runtime_state()

	// 步骤 8: _get_current_agent_rails(config, configBase)
	// Python: rails_list = self._get_current_agent_rails(config, config_base)
	railsList := d.getCurrentAgentRails(config, configBase)

	// 步骤 8.4: 加载用户自定义的 Rail 扩展
	// Python: await self.load_user_rails()
	d.loadUserRails()

	// 步骤 8.5: 工具同步
	// Python: self._sync_multimodal_tools_for_runtime() + self._sync_paid_search_tool_for_runtime()
	d.syncMultimodalToolsForRuntime(ctx)
	d.syncPaidSearchToolForRuntime()

	// 步骤 8.6: ACP filesystem rail 注销
	// Python: if not self._filesystem_rail_enabled_for_profile() and self._filesystem_rail is not None:
	//   await self._instance.unregister_rail(self._filesystem_rail)
	//   self._filesystem_rail = None
	if !d.filesystemRailEnabledForProfile(d.instanceOverrides) && d.filesystemRail != nil {
		if err := d.instance.UnregisterRail(ctx, d.filesystemRail); err != nil {
			logger.Warn(logComponent).Err(err).Msg("Failed to unregister ACP filesystem rail")
		}
		d.filesystemRail = nil
	}

	// 步骤 9: new_tool_cards = await self._get_tool_cards("jiuwenswarm")
	// Python: new_tool_cards = await self._get_tool_cards("jiuwenswarm")
	// Python: self._tool_cards = tool_cards（G8: 存储到 adapter 字段）
	newToolCards := d.getToolCards("uapclaw")
	d.toolCards = newToolCards

	// 步骤 10: _update_permission_rail(configBase)
	// ✅ 已回填：updatePermissionRail 热更新
	d.updatePermissionRail(configBase)

	// 步骤 11: instance.ConfigureDeepConfig(deepCfg)
	// Python: deep_cfg = self._make_deep_agent_config(model=model, config=config, agent_card=agent_card, tool_cards=..., rails=rails_list); self._instance.configure(deep_cfg)
	agentCard := agentschema.NewAgentCard(
		agentschema.WithAgentName(d.agentName),
		agentschema.WithAgentID("uapclaw"),
	)
	deepCfg := d.makeDeepAgentConfig(d.modelSnapshot(), config, agentCard, newToolCards, railsList)
	if cfgErr := d.instance.ConfigureDeepConfig(ctx, deepCfg); cfgErr != nil {
		return fmt.Errorf("DeepAgent ConfigureDeepConfig 失败: %w", cfgErr)
	}

	// 步骤 12-13: 重新注册 MCP
	// Python: await self._sync_mcp_servers_for_runtime(config_base, tag="agent.reload")
	d.registeredMCPServerIDs = make(map[string]bool)
	d.registeredMCPServers = make(map[string]*mcptypes.McpServerConfig)
	if syncErr := d.syncMcpServersForRuntime(ctx, configBase, "agent.reload"); syncErr != nil {
		logger.Warn(logComponent).Err(syncErr).Msg("MCP service hot-sync failed, continuing")
	}

	logger.Info(logComponent).Msg("DeepAdapter ReloadAgentConfig config hot-reloaded")
	return nil
}

// ProcessMessageImpl 执行非流式请求，返回完整响应。
// Python: JiuWenClawDeepAdapter.process_message_impl() (line 4409-4512)
//
// Python 执行步骤：
//
//	Python: 1. if self._instance is None: raise RuntimeError("未初始化")
//	Python: 2. _req_model = request.params.get("model_name", "")
//	Python: 3. if not self._has_valid_model_config(_req_model): return error response
//	Python: 4. session_id = request.session_id or "default"
//	Python: 5. query = request.params.get("query", "")
//	Python: 6. mode = request.params.get("mode", "agent")
//	Python: 7. slash_result = await self._handle_slash_command(query, session_id, mode)
//	Python: 8. if slash_result: handle approval_chunks or content
//	Python: 9. cron_context_tokens = self._bind_runtime_cron_context(...)
//	Python: 10. token_cid = TOOL_PERMISSION_CHANNEL_ID.set(...)
//	Python: 11. token_perm = setup_permission_context(request)
//	Python: 12. resolved_model = self._resolve_model_for_request(request)
//	Python: 13. self._apply_model_to_react_agent(resolved_model)
//	Python: 14. self._mark_session_active(session_id)
//	Python: 15. if self._stream_event_rail: self._stream_event_rail.reset_abort(session_id)
//	Python: 16. try:
//	Python: 17. await self._update_runtime_config(runtimeConfig)
//	Python: 18. result = await Runner.run_agent(agent=self._instance, inputs=inputs)
//	Python: 19. except asyncio.CancelledError: ...
//	Python: 20. finally: 清理（取消标记活跃会话、重置上下文变量）
//	Python: 21. return AgentResponse from result
func (d *DeepAdapter) ProcessMessageImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (*schema.AgentResponse, error) {
	// 步骤 0: 根分流
	// Python: process_message_impl :18175 → _get_session_adapter_for_request
	//
	// 根是路由器，不跑对话；把它转给该会话的子适配器，方法体其余步骤
	// 在 child 上执行，d.instance / d.rails / d.contextEngine 自然都是 child 的。
	if !d.isSessionScoped {
		sid := sessionAdapterKey(req.SessionID)
		child, err := d.getSessionAdapterForRequest(ctx, req, sessionAdapterRequestOptions{
			ReserveActivity: !isAttachOnlySubscription(req.Params),
		})
		if err != nil {
			return nil, err
		}
		defer d.evictIdleSessionAdapters(ctx)
		defer d.unregisterSessionAgentTask(sid, req.RequestID)
		// Python: :18987-18993（非流式入口同样消费 run options）
		if options := child.TakeProcessCliRunOptions(); options != nil {
			if cfgErr := child.ConfigureProcessCliRun(ctx, options.MaxTurns, options.HostTools); cfgErr != nil {
				return nil, cfgErr
			}
		}
		return child.ProcessMessageImpl(ctx, req, inputs)
	}

	// 步骤 1: 实例 nil 检查
	if d.instance == nil {
		return nil, fmt.Errorf("DeepAdapter 未初始化，请先调用 CreateInstance()")
	}

	// 步骤 2-3: 模型配置校验
	params := parseParams(req.Params)
	reqModelName := paramsString(params, "model_name", "")
	if !d.hasValidModelConfig(reqModelName) {
		return schema.NewAgentResponse(req.RequestID, req.ChannelID,
			schema.WithResponseOK(false),
			schema.WithPayload(map[string]any{
				"error": "模型未正确配置，请先配置模型信息",
			}),
		), nil
	}

	// 步骤 4: session_id 规范化
	sessionID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		sessionID = *req.SessionID
	}

	// 步骤 5-6: 提取 query/mode（query 唯一事实源为 params）
	query := paramsString(params, "query", "")
	mode := paramsString(params, "mode", "agent")

	// 步骤 7-8: slash 命令处理
	// Python: slash_result = await self._handle_slash_command(query, session_id, mode)
	slashResult, slashErr := d.handleSlashCommand(ctx, query, sessionID, mode, req.ChannelID)
	if slashErr != nil {
		logger.Warn(logComponent).Err(slashErr).Msg("handleSlashCommand failed, continuing normal flow")
	}
	if slashResult != nil {
		// Python: followup_prompt = self._extract_rebuild_followup_prompt(slash_result)
		followupPrompt := extractRebuildFollowupPrompt(slashResult)
		if followupPrompt != "" {
			// Python: inputs = dict(inputs); inputs["query"] = followup_prompt
			inputs = copyMap(inputs)
			inputs["query"] = followupPrompt
		} else {
			// Python: approval_chunks = slash_result.get("approval_chunks")
			approvalChunks := slashResult["approval_chunks"]
			if approvalChunks != nil {
				return schema.NewAgentResponse(req.RequestID, req.ChannelID,
					schema.WithPayload(map[string]any{"approval_chunks": approvalChunks}),
				), nil
			}
			// Python: content = slash_result.get("output", str(slash_result))
			content := slashResult["output"]
			if content == nil {
				content = fmt.Sprintf("%v", slashResult)
			}
			resultType, _ := slashResult["result_type"].(string)
			return schema.NewAgentResponse(req.RequestID, req.ChannelID,
				schema.WithResponseOK(resultType != "error"),
				schema.WithPayload(map[string]any{"content": content}),
			), nil
		}
	}

	// 步骤 9: cron 上下文绑定（11.10 已回填）
	// Python: cron_context_tokens = _bind_runtime_cron_context(channel_id=, session_id=, metadata=, request_id=, mode=)
	ctx = cronbridge.BindRuntimeCronContext(ctx, req.ChannelID, sessionID, req.Metadata, req.RequestID, mode)

	// 步骤 10-11: 权限上下文设置
	// Python: TOOL_PERMISSION_CHANNEL_ID.set(channel_id) + setup_permission_context(request)
	ctx = schema.WithToolPermissionChannelID(ctx, req.ChannelID)
	// Python 只有这一句：token_perm = setup_permission_context(request)
	// 原先的「req.PermissionContext 非 nil 则回退」分支已删除（9.90e）：
	// Python 的 AgentRequest 不含 permission_context 字段，
	// 权限上下文只从 metadata 构建后注入 ctx。
	if permCtx := schema.SetupPermissionContext(req.ChannelID, req.Metadata); permCtx != nil {
		ctx = schema.WithPermissionContextValue(ctx, permCtx)
	}

	// 步骤 12-13: 模型选择 + 应用到 ReActAgent
	resolvedModel := d.resolveModelForRequest(req)
	d.applyModelToReactAgent(resolvedModel)

	// 步骤 14: mark_session_active
	d.markSessionActive(sessionID)
	// 步骤 14b: 建议生成记账起点（对齐 interface.py:3544 forks.request_started）。
	// 非流式入口同样要登记，否则非流式轮的 turn_ended 永远拿不到 _turns 记录。
	recommendation.Forks.RequestStarted(sessionID,
		personDroveTurn(params, req.ChannelID, string(req.ReqMethod), req.Metadata,
			schema.IsTeamMode(mode)))

	// 步骤 15: streamEventRail.reset_abort(sessionID)
	if d.streamEventRail != nil {
		d.streamEventRail.ResetAbort(sessionID)
	}

	// 步骤 16-17: update_runtime_config（含 CWD 种子、语言/频道解析、RuntimePromptRail setter 等）
	// Python: await self._update_runtime_config(self._RuntimeConfig(...))
	requestCwd := paramsString(params, "cwd", "")
	trustedDirs := parseStringSlice(params, "trusted_dirs")
	projectDir := paramsString(params, "project_dir", "")
	d.runtimeConfigUpdater(ctx, &runtimeConfig{
		SessionID:       sessionID,
		Mode:            mode,
		RequestID:       req.RequestID,
		ChannelID:       req.ChannelID,
		RequestMetadata: req.Metadata,
		TrustedDirs:     trustedDirs,
		CWD:             requestCwd,
		ProjectDir:      projectDir,
	})

	// 步骤 17.5（已上移）：plan 状态同步归 AgentRuntime（对齐上游 runtime/service.py），
	// adapter 不再读写 plan 状态，也不再用原始 query 覆盖 inputs["query"]（信封由 BuildInputs 生成）。

	// 步骤 18: Runner.run_agent(agent=d.instance, inputs=inputs)
	// Python: result = await Runner.run_agent(agent=self._instance, inputs=inputs)
	var result map[string]any
	var runErr error
	func() {
		defer func() {
			// 步骤 20: finally 清理
			// Go ctx values 不可变值模式：_reset_runtime_cron_context 语义天然满足
			// （绑定后的 ctx 只在本次请求闭包内传递，函数返回即失效，无需显式 reset）
			d.unmarkSessionActive(sessionID)
			// 步骤 20b: 轮结束。先写完成行（Python: _complete_history_request），
			// 回执交给 fork 在推送前等待（广播 ⤵️ spec 10.7.2 D-3）。
			// 终态在 runErr 上体现；非流式无"等待用户"概念，恒 false。
			status := terminalStatusOf(runErr)
			var completionReceipt <-chan struct{}
			if !sessionMessageOwnsCompletion(params) {
				receipt, rcptErr := sessionmd.EnqueueHistoryRequestCompletion(sessionID, req.RequestID, status)
				if rcptErr != nil {
					logger.Warn(logComponent).Err(rcptErr).Str("request_id", req.RequestID).
						Msg("request completion row enqueue failed")
				} else {
					completionReceipt = receipt
				}
			}
			recommendation.Forks.TurnEnded(sessionID, req.RequestID, recommendation.TurnEndOptions{
				Status:         status,
				WaitingForUser: false,
				Completion:     completionReceipt,
			})
		}()
		// 步骤 18: 抢租约 + 投递输入，把租约帧聚合成响应载荷。
		// Python: interface_deep.py:18457-18560 —— 非流式同样先 attach 再 send_input。
		var opened interactionOpenResult
		opened, runErr = d.openInteractionStream(ctx, req, inputs, sessionID)
		if runErr == nil && opened.controlFrame != nil {
			// goal op 提前结束（confirm_required / error）：直接作为响应载荷。
			// Python: interface_deep.py:18480-18560
			result = opened.controlFrame
			return
		}
		if runErr == nil && opened.accepted {
			// 拿不到租约：只回受理确认（输入已投递，不读输出）。
			// Python: interface_deep.py:18470-18475
			result = map[string]any{"event_type": runtimeAcceptedEventType}
			return
		}
		if runErr == nil {
			result = d.aggregateInteractionStream(opened.rawCh)
		}
	}()

	if runErr != nil {
		logger.Error(logComponent).
			Err(runErr).
			Str("session_id", sessionID).
			Str("mode", mode).
			Msg("Runner.RunAgent execution failed")
		return nil, runErr
	}

	// 步骤 21: 构造 AgentResponse
	// Python: content = result if isinstance(result, (str, dict)) else str(result)
	content := result
	if content == nil {
		content = make(map[string]any)
	}

	_ = query

	return schema.NewAgentResponse(req.RequestID, req.ChannelID,
		schema.WithPayload(map[string]any{"content": content}),
	), nil
}

// ProcessMessageStreamImpl 执行流式请求，通过 channel 返回响应块。
// Python: JiuWenClawDeepAdapter.process_message_stream_impl() (line 4514-4750)
//
// Python 执行步骤：
//
//	Python: 1. if self._instance is None: raise RuntimeError("未初始化")
//	Python: 2. _req_model = request.params.get("model_name", "")
//	Python: 3. if not self._has_valid_model_config(_req_model): yield error chunk; return
//	Python: 4. session_id = request.session_id or "default"
//	Python: 5. query = request.params.get("query", "")
//	Python: 6. mode = request.params.get("mode", "agent")
//	Python: 7. if mode in ("team", "team.plan", "code.team"): → team_helpers.process_team_message_stream
//	Python: 8. if mode == "auto_harness": → auto_harness 分流
//	Python: 9. slash_result = await self._handle_slash_command(query, session_id, mode)
//	Python: 10. cron_context_tokens = self._bind_runtime_cron_context(...)
//	Python: 11. token_cid = TOOL_PERMISSION_CHANNEL_ID.set(...)
//	Python: 12. token_perm = setup_permission_context(request)
//	Python: 13. resolved_model = self._resolve_model_for_request(request)
//	Python: 14. self._apply_model_to_react_agent(resolved_model)
//	Python: 15. self._mark_session_active(session_id)
//	Python: 16. try:
//	Python: 17. await self._update_runtime_config(runtimeConfig)
//	Python: 18. async for chunk in Runner.run_agent_streaming(agent=self._instance, inputs=inputs):
//	Python: 19. yield chunk 产出消息块
//	Python: 20. except asyncio.CancelledError: ...
//	Python: 21. finally: 清理
func (d *DeepAdapter) ProcessMessageStreamImpl(ctx context.Context, req *schema.AgentRequest, inputs map[string]any) (<-chan *schema.AgentResponseChunk, error) {
	// 步骤 0: 根分流
	// Python: process_message_stream_impl :18963 → _get_session_adapter_for_request
	if !d.isSessionScoped {
		sid := sessionAdapterKey(req.SessionID)
		child, err := d.getSessionAdapterForRequest(ctx, req, sessionAdapterRequestOptions{
			ReserveActivity: !isAttachOnlySubscription(req.Params),
		})
		if err != nil {
			ch := make(chan *schema.AgentResponseChunk)
			close(ch)
			return ch, err
		}
		// 流式不能 defer 淘汰：淘汰必须在流消费完之后，而调用方持有 channel。
		// 用转接 goroutine 在流关闭后触发（对齐 Python 的 finally 在 generator 结束时执行）。
		return d.streamThroughChild(ctx, sid, req.RequestID, func() (<-chan *schema.AgentResponseChunk, error) {
			// Python: :18987-18993 取用并应用 run options（取用即清空，失败运行不泄漏）
			if options := child.TakeProcessCliRunOptions(); options != nil {
				if cfgErr := child.ConfigureProcessCliRun(ctx, options.MaxTurns, options.HostTools); cfgErr != nil {
					ch := make(chan *schema.AgentResponseChunk)
					close(ch)
					return ch, cfgErr
				}
			}
			return child.ProcessMessageStreamImpl(ctx, req, inputs)
		})
	}

	// 步骤 1: 实例 nil 检查
	if d.instance == nil {
		ch := make(chan *schema.AgentResponseChunk)
		close(ch)
		return ch, fmt.Errorf("DeepAdapter 未初始化，请先调用 CreateInstance()")
	}

	// 步骤 2-3: 模型配置校验
	params := parseParams(req.Params)
	reqModelName := paramsString(params, "model_name", "")
	if !d.hasValidModelConfig(reqModelName) {
		ch := make(chan *schema.AgentResponseChunk)
		// 发送错误 chunk 并关闭
		go func() {
			defer close(ch)
			ch <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
				"event_type": "chat.error",
				"error":      "模型未正确配置，请先配置模型信息",
			}, schema.WithChunkIsComplete(true))
		}()
		return ch, nil
	}

	// 步骤 4: session_id 规范化
	sessionID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		sessionID = *req.SessionID
	}

	// 步骤 5-6: 提取 mode（query 由 AgentRuntime 在 params 上改写，adapter 不再读写）
	mode := paramsString(params, "mode", "agent")

	// 步骤 7: team 模式分流
	// Python: if mode in ("team", "team.plan", "code.team"): → team_helpers.process_team_message_stream
	if schema.IsTeamMode(mode) {
		// S-07: 对齐 Python L4549-4561——team 分支 6 步运行时设置
		// （原实现直接进 processTeamMessageStream，模型不切换、language/channel 不解析、
		//   prompt_rail 不配置、runtime_state.yaml 不更新）
		// 步骤 1-2: 模型解析与应用
		// Python: resolved_model = self._resolve_model_for_request(request)
		//         self._apply_model_to_react_agent(resolved_model)
		d.applyModelToReactAgent(d.resolveModelForRequest(req))
		// 步骤 3: 语言解析
		// Python: resolved_language = self._resolve_runtime_language()
		resolvedLanguage := d.resolveRuntimeLanguage()
		// 步骤 4: 频道解析（cid 优先，其次 session 前缀推断，兜底 "web"）
		// Python: resolved_channel = str(cid or self._resolve_prompt_channel(session_id) or "web").strip() or "web"
		resolvedChannel := commrails.FirstNonEmpty(
			req.ChannelID,
			resolvePromptChannel(sessionID),
			"web",
		)
		// 步骤 5: RuntimePromptRail 双 setter（team 分支仅设置 model_name/mode）
		// Python: if self._runtime_prompt_rail:
		//   self._runtime_prompt_rail.set_model_name(self._resolve_model_name())
		//   self._runtime_prompt_rail.set_mode(mode)
		if d.runtimePromptRail != nil {
			d.runtimePromptRail.SetModelName(d.resolveModelName())
			d.runtimePromptRail.SetMode(mode)
		}
		// 步骤 6: 写 runtime_state.yaml（project_dir 三级回退：
		// inputs.project_dir → d.projectDir → d.workspaceDir）
		// Python: self._write_runtime_state(mode=..., language=..., channel=...,
		//   project_dir=inputs.get("project_dir") or self._project_dir or self._workspace_dir)
		d.writeRuntimeStateYAML(
			ctx, mode, resolvedLanguage, resolvedChannel,
			commrails.FirstNonEmpty(
				paramsString(inputs, "project_dir", ""),
				d.projectDir,
				d.workspaceDir,
			),
		)
		return d.processTeamMessageStream(ctx, req, inputs)
	}
	// 步骤 8: auto_harness 分流
	// ⤵️ 10.6.11: if mode == "auto_harness" → autoHarnessService.run()

	// 步骤 9: slash 命令处理
	// Python: slash_result = await self._handle_slash_command(query, session_id, mode)
	streamQuery := paramsString(params, "query", "")
	slashResultStream, slashErrStream := d.handleSlashCommand(ctx, streamQuery, sessionID, mode, req.ChannelID)
	if slashErrStream != nil {
		logger.Warn(logComponent).Err(slashErrStream).Msg("handleSlashCommand failed, continuing normal flow")
	}
	if slashResultStream != nil {
		// Python: followup_prompt = self._extract_rebuild_followup_prompt(slash_result)
		followupPrompt := extractRebuildFollowupPrompt(slashResultStream)
		if followupPrompt != "" {
			// Python: inputs = dict(inputs); inputs["query"] = followup_prompt
			inputs = copyMap(inputs)
			inputs["query"] = followupPrompt
		} else {
			// Python: approval_chunks → yield chunks; else content → yield chat.final
			slashCh := make(chan *schema.AgentResponseChunk, 8)
			go func() {
				defer close(slashCh)
				approvalChunks := slashResultStream["approval_chunks"]
				if approvalChunks != nil {
					// 输出审批块。
					// M-14：兼容三种切片形状——handleEvolveCommand 产出
					// []map[string]any（展平后的审批块），team 路径亦用
					// []map[string]any；旧 /evolve_simplify 产出 []any。
					// 原 []any 单一断言会静默丢弃 map 切片形状的审批块。
					emitted := false
					if slice, ok := approvalChunks.([]map[string]any); ok {
						for _, chunkMap := range slice {
							slashCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, chunkMap)
							emitted = true
						}
					}
					if !emitted {
						if slice, ok := approvalChunks.([]any); ok {
							for _, item := range slice {
								if chunkMap, ok := item.(map[string]any); ok {
									slashCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, chunkMap)
								}
							}
						}
					}
					slashCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID,
						map[string]any{"event_type": "chat.done"}, schema.WithChunkIsComplete(true))
				} else {
					content := slashResultStream["output"]
					if content == nil {
						content = fmt.Sprintf("%v", slashResultStream)
					}
					slashCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID,
						map[string]any{"event_type": "chat.final", "content": content},
						schema.WithChunkIsComplete(true))
				}
			}()
			return slashCh, nil
		}
	}

	// 步骤 10: cron 上下文绑定（11.10 已回填）
	// Python: cron_context_tokens = _bind_runtime_cron_context(channel_id=, session_id=, metadata=, request_id=, mode=)
	ctx = cronbridge.BindRuntimeCronContext(ctx, req.ChannelID, sessionID, req.Metadata, req.RequestID, mode)

	// 步骤 10-11: 权限上下文设置
	// Python: TOOL_PERMISSION_CHANNEL_ID.set(channel_id) + setup_permission_context(request)
	ctx = schema.WithToolPermissionChannelID(ctx, req.ChannelID)
	// Python 只有这一句：token_perm = setup_permission_context(request)
	// 原先的「req.PermissionContext 非 nil 则回退」分支已删除（9.90e）：
	// Python 的 AgentRequest 不含 permission_context 字段，
	// 权限上下文只从 metadata 构建后注入 ctx。
	if permCtx := schema.SetupPermissionContext(req.ChannelID, req.Metadata); permCtx != nil {
		ctx = schema.WithPermissionContextValue(ctx, permCtx)
	}

	// 步骤 12-13: 模型选择 + 应用到 ReActAgent
	resolvedModelStream := d.resolveModelForRequest(req)
	d.applyModelToReactAgent(resolvedModelStream)

	// 步骤 15: mark_session_active
	d.markSessionActive(sessionID)
	// 步骤 15b: 建议生成记账起点（对齐 interface.py:3906 / :4292 的 forks.request_started）。
	recommendation.Forks.RequestStarted(sessionID,
		personDroveTurn(params, req.ChannelID, string(req.ReqMethod), req.Metadata,
			schema.IsTeamMode(mode)))

	// 修复 S-18: 对齐 Python process_message_stream_impl 中的 reset_abort
	// Python: if self._stream_event_rail: self._stream_event_rail.reset_abort(session_id)
	if d.streamEventRail != nil {
		d.streamEventRail.ResetAbort(sessionID)
	}

	// 步骤 16-17: update_runtime_config（含 CWD 种子、语言/频道解析、RuntimePromptRail setter 等）
	// Python: await self._update_runtime_config(self._RuntimeConfig(...))
	streamRequestCwd := paramsString(params, "cwd", "")
	streamTrustedDirs := parseStringSlice(params, "trusted_dirs")
	streamProjectDir := paramsString(params, "project_dir", "")
	d.runtimeConfigUpdater(ctx, &runtimeConfig{
		SessionID:       sessionID,
		Mode:            mode,
		RequestID:       req.RequestID,
		ChannelID:       req.ChannelID,
		RequestMetadata: req.Metadata,
		TrustedDirs:     streamTrustedDirs,
		CWD:             streamRequestCwd,
		ProjectDir:      streamProjectDir,
	})

	// 步骤 17.5（已上移）：同非流式路径，plan 同步与 query 改写由 AgentRuntime 负责。

	// 步骤 18: Runner.RunAgentStreaming(agent=d.instance, inputs=inputs)
	// Python: async for chunk in Runner.run_agent_streaming(agent=self._instance, inputs=inputs):
	// 步骤 18: 抢输出租约 + 投递输入，从租约队列取帧。
	// Python: interface_deep.py:19953-20010 —— attach → send_input → for chunk in stream
	//
	// 下游解析（usage 累加 / askUser 去重 / evolution 块 / 控制事件）原样保留：
	// 租约队列与 runner 输出同为 stream.Schema，只换数据源。
	opened, streamErr := d.openInteractionStream(ctx, req, inputs, sessionID)
	if streamErr != nil {
		d.unmarkSessionActive(sessionID)
		ch := make(chan *schema.AgentResponseChunk)
		close(ch)
		return ch, fmt.Errorf("interaction attach/send_input failed: %w", streamErr)
	}
	if opened.controlFrame != nil {
		// goal op 提前结束（confirm_required / error）：只发这一帧。
		// Python: interface_deep.py:19842-19875
		d.unmarkSessionActive(sessionID)
		ch := make(chan *schema.AgentResponseChunk, 1)
		ch <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, opened.controlFrame,
			schema.WithChunkIsComplete(true))
		close(ch)
		return ch, nil
	}
	rawCh := opened.rawCh
	if opened.accepted {
		// 拿不到租约（另一读者持有）：只回受理确认，不投递。
		// Python: interface_deep.py:20000-20005
		d.unmarkSessionActive(sessionID)
		ch := make(chan *schema.AgentResponseChunk, 1)
		ch <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
			"event_type": runtimeAcceptedEventType,
			"request_id": req.RequestID,
		})
		close(ch)
		return ch, nil
	}

	// goroutine 从 rawCh 读取，parseStreamChunk 转换，写入 outCh
	outCh := make(chan *schema.AgentResponseChunk, 64)

	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				logger.Error(logComponent).Any("recover", rec).Msg("ProcessMessageStreamImpl panic recovered")
			}
		}()
		defer close(outCh)
		// 轮终态与"等待用户"检测（Python: interface.py:3926-3928 的流式循环）。
		// 声明在 defer 之前：Go 词法作用域要求闭包引用的变量先声明。
		status := "success"
		waitingForUser := false
		defer func() {
			d.unmarkSessionActive(sessionID)
			// 轮结束（对齐 interface.py:3957 / :4348 forks.turn_ended）。
			// 流式终态由流内事件决定：chat.error → error、ctx 取消 → cancelled。
			if ctx.Err() != nil {
				status = "cancelled"
			}
			var completionReceipt <-chan struct{}
			if !sessionMessageOwnsCompletion(params) {
				receipt, rcptErr := sessionmd.EnqueueHistoryRequestCompletion(sessionID, req.RequestID, status)
				if rcptErr != nil {
					logger.Warn(logComponent).Err(rcptErr).Str("request_id", req.RequestID).
						Msg("request completion row enqueue failed")
				} else {
					completionReceipt = receipt
				}
			}
			recommendation.Forks.TurnEnded(sessionID, req.RequestID, recommendation.TurnEndOptions{
				Status:         status,
				WaitingForUser: waitingForUser,
				Completion:     completionReceipt,
			})
		}()

		// usage 累加器
		usage := &utils.UsageAccumulator{}
		// askUser 去重集合
		emittedAskUserIDs := make(map[string]bool)
		// Python: has_streamed_content = False
		hasStreamedContent := false
		// 累积文本/reasoning
		accumulatedText := ""
		accumulatedReasoning := ""

		for raw := range rawCh {
			// 值/指针双分支（对齐 deep_adapter_team.go T-01 遗留点 C 的修复模式）：
			// session 流写入路径（normalizeOutputStream）将 *OutputSchema 解引用为
			// 值 OutputSchema 出队；仅断言指针时全部业务 chunk 静默跳过
			// （chat.delta/chat.final/usage_metadata 全丢，只剩终止哨兵）。
			var output *stream.OutputSchema
			switch v := raw.(type) {
			case *stream.OutputSchema:
				output = v
			case stream.OutputSchema:
				output = &v
			default:
				// 非输出帧（TraceSchema/CustomSchema），跳过
				continue
			}

			// 轮次盖章：每轮只在首块采样一次归属，不信 live active_round。
			// Python: interface_deep.py:15542 _track_round_output_boundary
			d.trackRoundOutputBoundary(output)
			chunkType := output.Type
			// payloadToMap 桥接 map 与 *cschema.ControllerOutputPayload 两种形态
			// （task_loop 产出结构体指针，只断言 map 会让整块 chunk 不可读）
			payload := payloadToMap(output.Payload)

			// 10.7.2: 交互事件 → waitingForUser；chat.error → 终态 error
			// （Python: interface.py:3926-3928 waiting_for_user = ... or payload.get("event_type") in USER_INTERACTION_EVENTS）
			if eventType, _ := payload["event_type"].(string); eventType != "" {
				if recommendation.UserInteractionEvents[eventType] {
					waitingForUser = true
				}
				if eventType == "chat.error" {
					status = "error"
				}
			}

			switch chunkType {
			case "llm_usage":
				// 累加 usage → yield chat.usage_metadata
				utils.AccumulateUsage(usage, payload)
				usagePayload := map[string]any{
					"event_type":    "chat.usage_metadata",
					"input_tokens":  usage.InputTokens,
					"output_tokens": usage.OutputTokens,
					"total_tokens":  usage.TotalTokens,
					"input_cost":    usage.InputCost,
					"output_cost":   usage.OutputCost,
					"total_cost":    usage.TotalCost,
				}
				// Python: metadata 和 session_id 字段
				if metadata, ok := payload["metadata"]; ok {
					usagePayload["metadata"] = metadata
				}
				if sessionID != "" {
					usagePayload["session_id"] = sessionID
				}
				outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, usagePayload)

			case "llm_reasoning":
				// 待实现：产出推理内容 yield chat.reasoning
				reasoningContent := extractReasoningContent(payload)
				accumulatedReasoning += reasoningContent
				outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
					"event_type": "chat.reasoning",
					"content":    reasoningContent,
				})

			case "llm_output":
				// 先 flush reasoning，再 yield chat.delta
				if accumulatedReasoning != "" {
					accumulatedReasoning = ""
				}
				textContent := extractTextContent(payload)
				// M-02: 对齐 Python — llm_output 直接 yield + continue，不累加 accumulated_text
				// Python: if chunk_type == "llm_output": yield ... ; continue
				hasStreamedContent = true
				d.noteRoundVisibleText(textContent)
				outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
					"event_type": "chat.delta",
					"content":    textContent,
				})

			case "answer":
				// Python: if chunk_type == "answer": 先 flush 累积，再调用 _parse_stream_chunk
				if accumulatedText != "" {
					outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
						"event_type": "chat.delta",
						"content":    accumulatedText,
					})
					accumulatedText = ""
				}
				if accumulatedReasoning != "" {
					outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
						"event_type": "chat.reasoning",
						"content":    accumulatedReasoning,
					})
					accumulatedReasoning = ""
				}
				// Python: if has_streamed_content: parsed = self._parse_stream_chunk(chunk, _has_streamed_content=True)
				parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs, d.interactionConverter, hasStreamedContent)
				if parsed != nil {
					if text, ok := parsed["content"].(string); ok {
						d.noteRoundVisibleText(text)
					}
					// goal attempt 边界的 chat.final 必须降级，否则每个 attempt
					// 都终结气泡，10 轮就有 10 个"答案"。
					// Python: interface_deep.py:15619 _adapt_goal_intermediate_final
					adapted, keep := d.adaptGoalIntermediateFinal(parsed)
					if !keep {
						continue
					}
					// user→goal 切换处补分气泡 final，否则两个答案糊进同一气泡。
					// Python: interface_deep.py:15574 _begin_visible_chat_content
					if boundary := d.beginVisibleChatContent(req, true); boundary != nil {
						outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, boundary)
					}
					// 目标完成卡片幂等落盘（覆盖所有 goal.updated 来源）。
					// Python: interface_deep.py:17677 _record_goal_completed_history_if_needed
					if eventType, _ := adapted["event_type"].(string); eventType != "" {
						if recommendation.UserInteractionEvents[eventType] {
							waitingForUser = true
						}
						if eventType == "chat.error" {
							status = "error"
						}
					}
					if eventType, _ := adapted["event_type"].(string); eventType == goalUpdatedEventType {
						if goalPayload, ok := adapted["goal"].(map[string]any); ok {
							sid := "default"
							if req.SessionID != nil && *req.SessionID != "" {
								sid = *req.SessionID
							}
							d.recordGoalCompletedHistoryIfNeeded(sid, req.ChannelID, req.Metadata,
								paramsString(parseParams(req.Params), "mode", ""), goalPayload)
						}
					}
					outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, adapted)
				}

			default:
				// flush 累积（对齐 Python: 先 emit 再置空）
				if accumulatedText != "" {
					outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
						"event_type": "chat.delta",
						"content":    accumulatedText,
					})
					accumulatedText = ""
				}
				if accumulatedReasoning != "" {
					outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
						"event_type": "chat.reasoning",
						"content":    accumulatedReasoning,
					})
					accumulatedReasoning = ""
				}
				// ParseStreamChunk 处理其他类型
				// Python: _has_streamed_content 在流式场景下为 true（已有内容输出后）
				parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs, d.interactionConverter, hasStreamedContent)
				if parsed != nil {
					if text, ok := parsed["content"].(string); ok {
						d.noteRoundVisibleText(text)
					}
					// goal attempt 边界的 chat.final 必须降级，否则每个 attempt
					// 都终结气泡，10 轮就有 10 个"答案"。
					// Python: interface_deep.py:15619 _adapt_goal_intermediate_final
					adapted, keep := d.adaptGoalIntermediateFinal(parsed)
					if !keep {
						continue
					}
					// user→goal 切换处补分气泡 final，否则两个答案糊进同一气泡。
					// Python: interface_deep.py:15574 _begin_visible_chat_content
					if boundary := d.beginVisibleChatContent(req, true); boundary != nil {
						outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, boundary)
					}
					// 目标完成卡片幂等落盘（覆盖所有 goal.updated 来源）。
					// Python: interface_deep.py:17677 _record_goal_completed_history_if_needed
					if eventType, _ := adapted["event_type"].(string); eventType != "" {
						if recommendation.UserInteractionEvents[eventType] {
							waitingForUser = true
						}
						if eventType == "chat.error" {
							status = "error"
						}
					}
					if eventType, _ := adapted["event_type"].(string); eventType == goalUpdatedEventType {
						if goalPayload, ok := adapted["goal"].(map[string]any); ok {
							sid := "default"
							if req.SessionID != nil && *req.SessionID != "" {
								sid = *req.SessionID
							}
							d.recordGoalCompletedHistoryIfNeeded(sid, req.ChannelID, req.Metadata,
								paramsString(parseParams(req.Params), "mode", ""), goalPayload)
						}
					}
					outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, adapted)
				}
			}
		}

		// flush 残留 accumulatedText/accumulatedReasoning
		if accumulatedText != "" {
			outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
				"event_type": "chat.final",
				"content":    accumulatedText,
			})
		}

		// 用量摘要
		// Python: interface_deep._parse_stream_chunk L4906-4972
		if usage.TotalTokens > 0 {
			// 对齐 Python: usage 子对象（仅 token 字段 + 条件 cost 字段）
			usageSubObj := map[string]any{
				"input_tokens":  usage.InputTokens,
				"output_tokens": usage.OutputTokens,
				"total_tokens":  usage.TotalTokens,
			}
			if usage.InputCost > 0 {
				usageSubObj["input_cost"] = usage.InputCost
			}
			if usage.OutputCost > 0 {
				usageSubObj["output_cost"] = usage.OutputCost
			}
			if usage.TotalCost > 0 {
				usageSubObj["total_cost"] = usage.TotalCost
			}

			usageSummary := map[string]any{
				"event_type": "chat.usage_summary",
				"session_id": sessionID,
				"usage":      usageSubObj,
				"model":      d.defaultModelNameSnapshot(),
			}
			// 对齐 Python: 从 DeepAgent.get_context_usage 获取 usage_percent/context_window_tokens
			if d.instance != nil {
				daUsage, daErr := d.instance.GetContextUsage(ctx, sessionID, "")
				if daErr == nil && daUsage != nil {
					if rawPct, ok := daUsage["usage_percent"]; ok && rawPct != nil {
						switch v := rawPct.(type) {
						case float64:
							usageSummary["usage_percent"] = v
						case int:
							usageSummary["usage_percent"] = float64(v)
						case int64:
							usageSummary["usage_percent"] = float64(v)
						}
					}
					if rawCW, ok := daUsage["context_window_tokens"]; ok && rawCW != nil {
						switch v := rawCW.(type) {
						case int:
							if v > 0 {
								usageSummary["context_window_tokens"] = v
							}
						case int64:
							if v > 0 {
								usageSummary["context_window_tokens"] = int(v)
							}
						case float64:
							if v > 0 {
								usageSummary["context_window_tokens"] = int(v)
							}
						}
					}
				}
			}
			outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, usageSummary)
		}

		// 演化监视器启动
		// Python: if self._skill_evolution_rail is not None:
		//   task = asyncio.create_task(self._watch_evolution_and_push(rid, cid, session_id))
		//   task.add_done_callback(self._on_evolution_watcher_done)
		//   self._evolution_watcher_tasks.add(task)
		if d.skillEvolutionRail != nil {
			go func() {
				_ = d.watchEvolutionAndPush(ctx, sessionID, req.ChannelID, req.RequestID)
				d.onEvolutionWatcherDone(sessionID)
			}()
		}

		// 终止哨兵
		outCh <- schema.NewTerminalChunk(req.RequestID, req.ChannelID)
	}()

	_ = mode
	return outCh, nil
}

// ProcessInterrupt 处理中断请求（pause/resume/cancel/supplement）。
// Python: JiuWenClawDeepAdapter.process_interrupt() (line 3268-3578)
//
// Python 执行步骤：
//
//	Python: 1. intent = request.params.get("intent", "cancel")
//	Python: 2. new_input = request.params.get("new_input")
//	Python: 3. _normalized_sid = request.session_id or "default"
//	Python: 4. _session_is_active = self._is_session_active(_normalized_sid)
//	Python: 5. 如果会话不活跃则记录日志并跳过中断操作
//	Python: 6. if intent == "pause": streamEventRail.pause(session_id)
//	Python: 7. elif intent == "resume": streamEventRail.resume(session_id)
//	Python: 8. elif intent == "supplement": abort + optional instance.abort()
//	Python: 9. elif intent == "cancel": abort + unmark_session_active
//	Python: 10. 清理进化观察器
//	Python: 11. return AgentResponse with interrupt_result
func (d *DeepAdapter) ProcessInterrupt(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 步骤 1-2: 解析 intent 和 new_input
	params := parseParams(req.Params)
	intent := paramsString(params, "intent", "cancel")
	newInput := params["new_input"] // 可能为 nil

	// 步骤 0: 根分流（双分支）
	// Python: process_interrupt :16330
	//
	// cancel/supplement 只查缓存：无缓存即"无在飞流"，fallthrough 到共享
	// teardown。不能为它造 child——造 child 会跑完整 agent 初始化
	// （17 rail + ensure_initialized，含 ProjectMemoryRail 扫描），把 cancel
	// 本身卡住数十秒，后续 ESC 全堵在队列里（上游 :16331-16341 注释）。
	// pause/resume 需要 session-scoped adapter 的 StreamEventRail，按原逻辑创建。
	if !d.isSessionScoped {
		sid := sessionAdapterKey(req.SessionID)
		if intent == "cancel" || intent == "supplement" {
			if cached := d.getCachedSessionAdapter(sid); cached != nil {
				defer d.evictIdleSessionAdapters(ctx)
				return cached.ProcessInterrupt(ctx, req)
			}
			// fallthrough：无缓存 → 走下方共享 teardown
		} else {
			child, err := d.getOrCreateSessionAdapter(ctx, sid, sessionAdapterOptions{})
			if err != nil {
				return nil, err
			}
			defer d.evictIdleSessionAdapters(ctx)
			return child.ProcessInterrupt(ctx, req)
		}
	}

	// 步骤 3-4: session 规范化 + 活跃检查
	normalizedSID := "default"
	if req.SessionID != nil && *req.SessionID != "" {
		normalizedSID = *req.SessionID
	}
	sessionActive := d.isSessionActive(normalizedSID)

	if !sessionActive {
		logger.Info(logComponent).
			Str("intent", intent).
			Str("session_id", normalizedSID).
			Msg("interrupt: session inactive, skipping abort")
	}

	// 步骤 6-9: 按 intent 分支
	// Python: process_interrupt() (line 3268-3476)
	interruptMsg := ""
	var cancelledTodos []map[string]any
	switch intent {
	case "pause":
		if sessionActive && d.streamEventRail != nil {
			d.streamEventRail.Pause(normalizedSID)
		}
		interruptMsg = "执行已暂停"
		logger.Info(logComponent).Str("intent", "pause").Msg("Interrupt: execution paused")
	case "resume":
		if sessionActive && d.streamEventRail != nil {
			d.streamEventRail.Resume(normalizedSID)
		}
		interruptMsg = "执行已恢复"
		logger.Info(logComponent).Str("intent", "resume").Msg("Interrupt: execution resumed")
	case "supplement":
		if newInput != nil {
			d.markSessionActive(normalizedSID)
		}
		// rail.abort(sessionID)，对齐 Python: self._stream_event_rail.abort(request.session_id)
		if sessionActive && d.streamEventRail != nil {
			d.streamEventRail.Abort(normalizedSID)
		}
		// Python: interface_deep.py:16473 —— 仅当本会话确有在飞工作时才考虑全局中止；
		// 会话不活跃时别的会话可能刚起来，abort 共享实例会误伤。
		if sessionActive {
			d.abortSharedAgentIfSafe(ctx, normalizedSID, intent)
		}
		interruptMsg = "supplement 已处理"
		logger.Info(logComponent).Str("intent", "supplement").Msg("Interrupt: supplement processed")
	case "cancel":
		// rail.abort(sessionID) + rail.reset_for_new_task(sessionID)，对齐 Python
		if sessionActive && d.streamEventRail != nil {
			d.streamEventRail.Abort(normalizedSID)
			d.streamEventRail.CollectCancelledToolUpdates(normalizedSID)
			d.streamEventRail.ResetForNewTask(normalizedSID)
		}
		// Python: interface_deep.py:16505 —— 同上，goal 轮在飞或有其它会话活跃时跳过。
		if sessionActive {
			d.abortSharedAgentIfSafe(ctx, normalizedSID, intent)
		}
		d.unmarkSessionActive(normalizedSID)
		// Python: cancelled_todos = await self._cancel_pending_todos(session_id)
		// Python: if cancelled_todos: payload["todos"] = cancelled_todos
		cancelledTodos = d.cancelPendingTodos(ctx, normalizedSID)
		interruptMsg = "执行已取消"
		logger.Info(logComponent).Str("intent", "cancel").Msg("Interrupt: cancel processed")
	}

	// 步骤 10: 清理 evolution watchers
	// ⤵️ 9.55-9.65 EvolutionHelpers（进化助手）: 取消进化监听任务

	// 步骤 11: 构造响应
	// Python: AgentResponse(payload={"event_type": "chat.interrupt_result", "intent": intent, "success": True, ...})
	payload := map[string]any{
		"event_type": "chat.interrupt_result",
		"intent":     intent,
		"success":    true,
		"message":    interruptMsg,
	}

	// Python: payload["new_input"] = new_input（如果存在）
	if newInput != nil {
		payload["new_input"] = newInput
	}

	// cancel 后附带被中断的工具执行结果，通知前端更新状态
	// 对齐 Python: if cancelled_tool_results: payload["cancelled_tools"] = cancelled_tool_results
	if d.streamEventRail != nil {
		cancelledToolResults := d.streamEventRail.GetCancelledToolResults(normalizedSID)
		if len(cancelledToolResults) > 0 {
			payload["cancelled_tools"] = cancelledToolResults
			d.streamEventRail.ClearCancelledToolResults(normalizedSID)
		}
	}

	// Python: if cancelled_todos: payload["todos"] = cancelled_todos
	if cancelledTodos != nil {
		payload["todos"] = cancelledTodos
	}

	return schema.NewAgentResponse(req.RequestID, req.ChannelID,
		schema.WithPayload(payload),
	), nil
}

// cancelPendingTodos 将未完成的 todo 项标记为 cancelled。
// Python: _cancel_pending_todos (interface_deep.py:4351-4407)
// 返回前端格式 todo 列表（附加到 interrupt_result 通知前端刷新），无 todo 或失败返回 nil。
// Python 先从 ability_manager.get("todo_modify") + resource_mgr.get_tool() 取已注册实例，
// 失败才 fallback 构造。Go 的 tool.Tool 接口无法还原 TodoTool 基类（LoadTodos/SaveTodos
// 在基类上），因此统一走 fallback 构造——语义等价：todos 文件路径相同（同一 workspace 的
// todo 节点），读写同一持久层（偏差 #2 说明栏）。
func (d *DeepAdapter) cancelPendingTodos(ctx context.Context, sessionID string) []map[string]any {
	if d.instance == nil {
		return nil
	}

	defer func() {
		if r := recover(); r != nil {
			logger.Warn(logComponent).Any("panic", r).Str("session_id", sessionID).Msg("cancel pending todos failed")
		}
	}()

	// fallback 构造：从 DeepConfig 的 workspace todo 节点构造 TodoTool
	if d.instance.DeepConfig() == nil || d.instance.DeepConfig().Workspace == nil {
		return nil
	}
	todoPath := d.instance.DeepConfig().Workspace.GetNodePath(hworkspace.WorkspaceNodeTODO)
	if todoPath == nil {
		return nil
	}
	var fs sysop.FsOperation
	if d.sysOperation != nil {
		fs = d.sysOperation.Fs()
	}
	_, todoBase := todo.CreateTodosTool(*todoPath, fs, d.resolveRuntimeLanguage(), "")
	modifyTool := &todoBase

	todos, err := modifyTool.LoadTodos(ctx, sessionID)
	if err != nil || len(todos) == 0 {
		return nil
	}

	// 收集非 completed/cancelled 的 ID
	var idsToCancel []string
	for _, item := range todos {
		if item.Status != hschema.TodoStatusCompleted && item.Status != hschema.TodoStatusCancelled {
			idsToCancel = append(idsToCancel, item.ID)
		}
	}

	if len(idsToCancel) > 0 {
		// Python: modify_tool._cancel_todos(ids, todos)（SDK 版内含 save）
		// Go: CancelTodos 不含 save，外层显式两步（spec 偏差 #2）
		updated, _, cErr := todo.CancelTodos(todos, idsToCancel)
		if cErr != nil {
			logger.Warn(logComponent).Err(cErr).Str("session_id", sessionID).Msg("cancel pending todos failed")
			return nil
		}
		if sErr := modifyTool.SaveTodos(ctx, sessionID, updated); sErr != nil {
			logger.Warn(logComponent).Err(sErr).Str("session_id", sessionID).Msg("cancel pending todos save failed")
			return nil
		}
		logger.Info(logComponent).Str("session_id", sessionID).Int("cancelled_count", len(idsToCancel)).Msg("cancelled pending todos marked as cancelled")
	}

	// 重新加载并返回前端格式
	updatedTodos, err := modifyTool.LoadTodos(ctx, sessionID)
	if err != nil || len(updatedTodos) == 0 {
		return nil
	}
	// Python: if updated_todos and self._stream_event_rail is not None —— 无 rail 返回 None
	// （interrupt_result 事件不带 todos 刷新数据）
	if d.streamEventRail == nil {
		return nil
	}
	// 复用 commrails 的共享实现（stream_event_emit.go），避免映射逻辑两处漂移
	return commrails.FormatTodosForFrontend(updatedTodos)
}

// HandleUserAnswer 处理用户回答（evolution 审批或权限审批）。
// Python: JiuWenClawDeepAdapter.handle_user_answer() (line 3579-3605)
//
// Python 执行步骤：
//
//	Python: 1. request_id = request.params.get("request_id", "")
//	Python: 2. answers = request.params.get("answers", [])
//	Python: 3. session_id = request.session_id
//	Python: 4. resolved = False
//	Python: 5. if request_id.startswith("team_skill_evolve_"): resolved = handle_team_skill_evolve_approval(...)
//	Python: 6. elif request_id.startswith("evolve_simplify_"): resolved = _handle_governance_approval(...)
//	Python: 7. elif request_id.startswith("skill_evolve_"): resolved = _handle_evolution_approval(...)
//	Python: 8. return AgentResponse(ok=True, payload={"accepted": True, "resolved": resolved})
func (d *DeepAdapter) HandleUserAnswer(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 步骤 0: 根分流
	// Python: handle_user_answer :16826 → _get_or_create_session_adapter
	if !d.isSessionScoped {
		sid := sessionAdapterKey(req.SessionID)
		child, err := d.getOrCreateSessionAdapter(ctx, sid, sessionAdapterOptions{})
		if err != nil {
			return nil, err
		}
		defer d.evictIdleSessionAdapters(ctx)
		return child.HandleUserAnswer(ctx, req)
	}

	// 步骤 1-2: 解析 request_id 和 answers
	params := parseParams(req.Params)
	requestID := paramsString(params, "request_id", "")
	answers := params["answers"]
	parsedAnswers := parseApprovalAnswersFromAny(answers)

	// 步骤 4: resolved 默认 false
	resolved := false

	// 步骤 5-7: 按 request_id 前缀分发
	switch {
	case strings.HasPrefix(requestID, "team_skill_evolve_"):
		// ✅ P4 已回填：handle_team_skill_evolve_approval 依赖 TeamSkillEvolutionRail
		sid := ""
		if req.SessionID != nil {
			sid = *req.SessionID
		}
		resolved = d.handleTeamSkillEvolveApproval(ctx, requestID, answers, sid, req.ChannelID)
	case strings.HasPrefix(requestID, "evolve_simplify_"):
		// ✅ 已回填：_handle_governance_approval(requestID, answers, approvalType)
		// Python 中 approvalType 从 answers 推断：approve → "approve", reject → "reject"
		approvalType := "reject"
		if parseApprovalAnswers(parsedAnswers) {
			approvalType = "approve"
		}
		resolved = d.handleGovernanceApproval(ctx, requestID, parsedAnswers, approvalType)
	case strings.HasPrefix(requestID, "skill_evolve_"):
		// ✅ 已回填：_handle_evolution_approval(requestID, answers)
		resolved = d.handleEvolutionApproval(ctx, requestID, parsedAnswers)
	}

	// 步骤 8: 构造响应
	return schema.NewAgentResponse(req.RequestID, req.ChannelID,
		schema.WithPayload(map[string]any{"accepted": true, "resolved": resolved}),
	), nil
}

// HandleHeartbeat 处理心跳请求。
// Python: JiuWenClawDeepAdapter.handle_heartbeat() (line 3607-3624)
//
// Python 执行步骤：
//
//	Python: 1. sid = str(request.session_id or "")
//	Python: 2. if not sid.startswith("heartbeat"): return None
//	Python: 3. request.params["query"] = "这是一次心跳请求任务..."
//	Python: 4. 记录心跳查询注入
//	Python: 5. return None（继续正常流程，query 已注入）
//
// 返回 nil 表示非心跳请求或心跳已处理（query 已注入），上层应继续正常流程。
func (d *DeepAdapter) HandleHeartbeat(ctx context.Context, req *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 步骤 1: session_id 前缀检查
	sid := ""
	if req.SessionID != nil {
		sid = *req.SessionID
	}

	// 步骤 2: 非 heartbeat → 返回 nil
	if !strings.HasPrefix(sid, "heartbeat") {
		return nil, nil
	}

	// 步骤 3: 注入心跳 prompt
	heartbeatQuery := "这是一次心跳请求任务，请根据</heartbeat_user_task>标签中的内容进行回复"
	if len(req.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(req.Params, &params); err != nil {
			params = make(map[string]any)
		}
		params["query"] = heartbeatQuery
		if updated, err := json.Marshal(params); err == nil {
			req.Params = updated
		}
	} else {
		// Python: 直接赋值 request.params["query"]，用 json.Marshal 避免注入风险
		params := map[string]any{"query": heartbeatQuery}
		if updated, err := json.Marshal(params); err == nil {
			req.Params = updated
		}
	}

	// 步骤 4: 日志
	logger.Info(logComponent).
		Str("request_id", req.RequestID).
		Str("session_id", sid).
		Msg("heartbeat query injected")

	// 步骤 5: 返回 nil，继续正常流程
	return nil, nil
}

// SwitchMode 切换运行模式，执行完整的 session 生命周期。
// 流程：preRun → switchMode → loadState → updateState → postRun
//
// Python: jiuwenswarm/server/agent_ws_server.py:1145-1154
func (d *DeepAdapter) SwitchMode(ctx context.Context, sessionID, subMode string) error {
	// 注意：本方法服务 ws-server 的切模式流程（preRun → switch_mode → postRun，
	// Python: agent_ws_server.py）。plan 状态同步走的是另一条流程
	// （plan.py:ensure_state → open_state_session → switch_mode → session.commit），
	// 由 planStateStore.SwitchPlanMode 实现，**不要**复用本方法：
	// 这里自建 Session 会把状态写到 rail 读不到的对象上。

	// 优先落到正在承接该会话的 live instance/session。
	// Python: plan.py 的 starter = agent.ensure_live_session_instance —— 状态必须
	// 写在即将执行本轮的那个 Session 上。create_instance 现在会 start_interaction
	// 绑定 live 会话，若这里仍 EnsureInstance + 新建 Session，就会写出两个
	// Session 对象（写一个读另一个），rail / CheckPostProcessExit 都读不到。
	if live := d.GetLiveSessionInstance(sessionID); live != nil {
		if bound := resolveLiveAgentSession(live, sessionID); bound != nil {
			live.SwitchMode(bound, subMode)
			return nil
		}
	}
	if d.instance != nil {
		if bound := resolveLiveAgentSession(d.instance, sessionID); bound != nil {
			d.instance.SwitchMode(bound, subMode)
			return nil
		}
	}
	// 10.3.28f：SwitchMode 是根上的独立 RPC，需要 DeepAgent 句柄。
	// 根不再在 create_instance 时自建 instance，故此处惰建。
	inst, err := d.EnsureInstance(ctx)
	if err != nil {
		return err
	}
	if inst == nil {
		return nil
	}

	card := inst.Card()
	if card == nil {
		return nil
	}

	// 创建 session 并执行生命周期
	sess := session.CreateAgentSession(sessionID, card, nil)
	if err := sess.PreRun(ctx); err != nil {
		return fmt.Errorf("SwitchMode PreRun 失败: %w", err)
	}

	inst.SwitchMode(sess, subMode)
	state := inst.LoadState(sess)

	sess.UpdateState(map[string]any{
		hschema.SessionStateKey: state.ToSessionDict(),
	})

	if err := sess.PostRun(ctx); err != nil {
		return fmt.Errorf("SwitchMode PostRun 失败: %w", err)
	}

	return nil
}

// Instance 返回底层 DeepAgent 实例。
// Python: JiuWenClaw.get_instance() → self._adapter._instance
func (d *DeepAdapter) Instance() *harness.DeepAgent {
	if d == nil {
		return nil
	}
	return d.instance
}

// IsSessionScoped 是否为会话级子适配器。
// Python: interface_deep.py _is_session_scoped_adapter（:2110）
func (d *DeepAdapter) IsSessionScoped() bool {
	return d.isSessionScoped
}

// MarkAsSessionScoped 标记为指定会话的子适配器。
// Python: interface_deep.py mark_as_session_scoped（:2711-2714）
func (d *DeepAdapter) MarkAsSessionScoped(sessionID string) {
	d.isSessionScoped = true
	d.parentSessionID = sessionID
}

// GetLiveSessionInstance 返回正在跑该会话的 DeepAgent。
// Python: interface_deep.py get_live_session_instance（:12744-12769）
//
// child 返回自己的 instance；根查缓存后递归一跳（子适配器一定是
// session 级的，递归只会走上面的 isSessionScoped 分支返回它自己的实例）。
func (d *DeepAdapter) GetLiveSessionInstance(sessionID string) *harness.DeepAgent {
	if d.isSessionScoped {
		return d.instance
	}
	sid := sessionAdapterKey(&sessionID)
	child := d.getCachedSessionAdapter(sid)
	if child == nil {
		return nil
	}
	return child.GetLiveSessionInstance(sid)
}

// HandleSwarmflowReply 处理人对 swarmflow 人工轮次的回复。
// Python: interface_deep.py handle_swarmflow_reply（:16971）
//
// 方法体归团队域；10.3.28f 只保证接口面与分流结构（spec DEC-10）。
func (d *DeepAdapter) HandleSwarmflowReply(
	ctx context.Context, req *schema.AgentRequest,
) (*schema.AgentResponse, error) {
	// 分流头
	// Python: :16979-16984
	if !d.isSessionScoped {
		sid := sessionAdapterKey(req.SessionID)
		child, err := d.getOrCreateSessionAdapter(ctx, sid, sessionAdapterOptions{})
		if err != nil {
			return nil, err
		}
		defer d.evictIdleSessionAdapters(ctx)
		return child.HandleSwarmflowReply(ctx, req)
	}
	return nil, errors.New("swarmflow reply handling not implemented; owned by team domain")
}

// PendingInteractions 会话被暂停的轮次仍在询问什么。
// Python: interface_deep.py pending_interactions（:15984）
//
// 该方法是根上的查询入口，不转 child（对齐上游：它自己查 child）。
// 方法体依赖 paused_interaction_events，归后续章节。
func (d *DeepAdapter) PendingInteractions(sessionID string) ([]map[string]any, error) {
	return nil, nil
}

// Cleanup 清理适配器资源。
// Python: JiuWenClawDeepAdapter.cleanup() (line 3245-3248)
//
// Python 执行步骤：
//
//	Python: 1. await self._close_a2x_client()
func (d *DeepAdapter) Cleanup() error {
	// 步骤 1: 关闭 a2x 客户端
	_ = d.closeA2xClient()

	// 步骤 2: 停止交互循环
	// Python: interface_deep.py:15147-15171 —— 根逐个停掉缓存的会话级 child 并清缓存；
	// 会话级 child 只停自己。此前 Go 侧只 Abort 从不 Stop，supervisor goroutine
	// 永不退出：进程内多套件连续 CreateInstance 会累积泄漏的监督器，
	// 它们继续消费 work、继续写输出，污染后续用例（集成包失败集合随运行变化）。
	if !d.isSessionScoped {
		if d.sessionAdapters != nil {
			for _, sid := range d.sessionAdapters.sessionIDs() {
				child := d.sessionAdapters.get(sid)
				if child == nil {
					continue
				}
				if stopper, ok := child.(interface{ stopInteraction() }); ok {
					stopper.stopInteraction()
				}
				if cleaner, ok := child.(interface{ Cleanup() error }); ok {
					_ = cleaner.Cleanup()
				}
				d.sessionAdapters.forget(sid)
			}
		}
	} else {
		d.stopInteraction()
	}

	logger.Info(logComponent).Msg("DeepAdapter Cleanup completed")
	return nil
}

// AbortOnGatewayDisconnect Gateway 断连时全局中止。
// Python: JiuWenClawDeepAdapter.abort_on_gateway_disconnect() (line 3511-3538)
//
// 与 interrupt(cancel) 不同：无 other_sessions 保护，
// gateway 断开意味着前端已无法接收响应，无条件 abort 所有 session。
func (d *DeepAdapter) AbortOnGatewayDisconnect(ctx context.Context) {
	// 步骤 1: 中止 rail 上所有活跃 session
	// Python: if self._stream_event_rail is not None:
	//   对应 Python: active = [sid for sid, count in self._active_session_ids.items() if count > 0]
	//   对应 Python: for sid in active: self._stream_event_rail.abort(sid)
	// ⤵️ 10.6.3-10: streamEventRail abort 所有活跃 session
	if d.streamEventRail != nil {
		for _, sid := range d.activeSessionIDSnapshot() {
			d.streamEventRail.Abort(sid)
		}
	}

	// 步骤 2: 中止 DeepAgent 实例（协作式，无法中断进行中的 LLM HTTP 请求）
	// Python: await self._instance.abort()，try/except 捕获异常
	if d.instance != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Warn(logComponent).Any("panic", r).Msg("AbortOnGatewayDisconnect instance.Abort panicked")
				}
			}()
			d.instance.Abort(ctx)
		}()
	}

	// 步骤 3: 取消调度器任务，在 await 点注入 CancelledError
	// Python: self._cancel_scheduler_running_tasks()（abort_on_gateway_disconnect 步骤 3）
	d.cancelSchedulerRunningTasks()

	logger.Info(logComponent).Msg("DeepAdapter AbortOnGatewayDisconnect completed")
}

// EnsurePersistentCheckpointer 确保进程级默认检查点器使用 SQLite 持久化。
// Python: interface_deep.py ensure_persistent_checkpointer() (line 393-424)
//
// 并发语义：初始化被包级互斥锁串行化（首次调用创建 SQLite 检查点器并设为全局默认），
// 就绪标志的读与写全部在锁内完成——此前的"锁外快路径读普通 bool"是 data race
// （与 ResetPersistentCheckpointer 的写并发），且 race detector 会直接报。
// 初始化本身只发生一次；后续调用的代价是一次 mutex 加解锁（约 20ns），
// 调用频率为"每次会话创建/删除"，无需快路径。
// 初始化步骤：
//  1. 加锁
//  2. 若已就绪直接返回
//  3. PersistenceCheckpointerProvider() 已由 init() 注册，无需显式调用
//  4. 获取 checkpoint_dir，构造 SQLite 路径
//  5. 通过 CheckpointerFactory.Create 创建 persistence 实例（带 30s 超时）
//  6. SetDefaultCheckpointer 设为全局默认
func EnsurePersistentCheckpointer(ctx context.Context) error {
	persistentCheckpointerLock.Lock()
	defer persistentCheckpointerLock.Unlock()

	if persistentCheckpointerReady {
		return nil
	}

	// 步骤 3: PersistenceCheckpointerProvider() — Go 中 persistence provider 已在 init() 时注册
	// 无需显式调用，等价于 Python 的 PersistenceCheckpointerProvider()

	// 步骤 4: 获取 checkpoint 目录
	checkpointDir := workspace.CheckpointDir()
	dbPath := filepath.Join(checkpointDir, "checkpoint")

	// 步骤 5-6: 创建 persistence checkpointer 并设为全局默认
	// M-15: 打开 SQLite 属于磁盘 IO，必须受调用方 ctx 控制；这里再加 30s 上限，
	// 避免初始化卡住时把后续所有会话创建/删除阻塞在全局锁上。
	initCtx, cancel := context.WithTimeout(ctx, persistentCheckpointerInitTimeout)
	defer cancel()

	cp, err := checkpointer.CreateCheckpointer(
		initCtx,
		checkpointer.CheckpointerFactoryConfig{
			Type: "persistence",
			Conf: map[string]any{
				"db_type": "sqlite",
				"db_path": dbPath,
			},
		},
	)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("db_path", dbPath).
			Msg("persistent checkpointer initialization failed")
		return fmt.Errorf("persistent checkpointer initialization failed: %w", err)
	}

	checkpointer.SetDefaultCheckpointer(cp)
	persistentCheckpointerReady = true

	logger.Info(logComponent).
		Str("db_path", dbPath+".db").
		Msg("persistent checkpointer ready")

	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getSendPushFunc 获取推送函数。优先使用实例级 sendPushFunc，回退到 defaultSendPushFunc。
func (d *DeepAdapter) getSendPushFunc() func(ctx context.Context, msg map[string]any) error {
	if d.sendPushFunc != nil {
		return d.sendPushFunc
	}
	return defaultSendPushFunc
}

// loadUserRails 用户自定义 Rail 扩展加载点。
// Python: load_user_rails() —— 经 get_rail_manager() 从
// {agent_workspace}/extensions/<name>/rail.py 动态 import 用户手写的 Python Rail
// （common/plugins/rail_manager.py，约 550 行，含 import/校验/热重载/删除）。
// S-19（归档）：这是**语言边界**导致的不可移植项，不是待实现功能——
// Go 是编译型语言，无法 import 用户现写的 .py；1:1 对齐需另立设计
// （Go 原生扩展注册表或 plugin 机制），本轮不做。
// 同一缺口在 team_manager_spec.go 的 rail_manager 扩展循环处也有引用。
func (d *DeepAdapter) loadUserRails() {
	logger.Debug(logComponent).Msg("loadUserRails: Python-only capability (dynamic import of user .py rails), no Go equivalent")
}

// parseApprovalAnswersFromAny 从 any 类型解析为 []ApprovalAnswer。
// answers 来自 req.Params 的 JSON 反序列化，可能是 []any 或 []map[string]any。
// 对齐 Python: answers 为 list[dict]，每个 dict 包含 selected_options。
func parseApprovalAnswersFromAny(answers any) []ApprovalAnswer {
	if answers == nil {
		return nil
	}
	// 尝试 []any（JSON 反序列化默认）
	if slice, ok := answers.([]any); ok {
		var result []ApprovalAnswer
		for _, item := range slice {
			if m, ok := item.(map[string]any); ok {
				var opts []string
				if raw, ok := m["selected_options"]; ok {
					if optSlice, ok := raw.([]any); ok {
						for _, o := range optSlice {
							if s, ok := o.(string); ok {
								opts = append(opts, s)
							}
						}
					}
				}
				result = append(result, ApprovalAnswer{SelectedOptions: opts})
			}
		}
		return result
	}
	return nil
}

// resolveVisionModelClient 基于 visionModelConfig 构建 BaseModelClient。
func (d *DeepAdapter) resolveVisionModelClient() modelclients.BaseModelClient {
	return d.resolveModelClientFromConfig(d.visionModelConfig.APIKey, d.visionModelConfig.BaseURL, d.visionModelConfig.Model, d.visionModelConfig.MaxRetries)
}

// resolveAudioModelClient 基于 audioModelConfig 构建 BaseModelClient。
func (d *DeepAdapter) resolveAudioModelClient() modelclients.BaseModelClient {
	return d.resolveModelClientFromConfig(d.audioModelConfig.APIKey, d.audioModelConfig.BaseURL, d.audioModelConfig.QAModel, d.audioModelConfig.MaxRetries)
}

// resolveVideoModelClient 基于 videoModelConfig 构建 BaseModelClient。
func (d *DeepAdapter) resolveVideoModelClient() modelclients.BaseModelClient {
	return d.resolveModelClientFromConfig(d.videoModelConfig.APIKey, d.videoModelConfig.BaseURL, d.videoModelConfig.Model, d.videoModelConfig.MaxRetries)
}

// resolveModelClientFromConfig 统一的模型客户端构建逻辑。
// Python: init_model(provider="OpenAI", ...)
func (d *DeepAdapter) resolveModelClientFromConfig(apiKey, apiBase, model string, maxRetries int) modelclients.BaseModelClient {
	m, err := llm.InitModel("OpenAI", model, apiKey, apiBase,
		llm.WithInitMaxRetries(maxRetries),
		llm.WithInitVerifySSL(false),
	)
	if err != nil {
		logger.Error(logComponent).Err(err).Str("model", model).Msg("Failed to build model client")
		return nil
	}
	return m.GetClient()
}

// deepAgentContextEngineConfig 从 react 配置与 models 段构建上下文引擎配置。
// Python: _deep_agent_context_engine_config(react_cfg)
//
// 与 Python 保持同构：
//   - Python 以 ReActAgentConfig().context_engine_config.model_copy(update=...) 起底，
//     因此 max_context_message_num=200 / default_window_round_num=10 始终保留；
//   - 仅切换 enable_kv_cache_release，其余字段取默认值。
//
// 本实现额外（对齐上游 model_config_obj.context_window）：
//   - 把 models.defaults[].model_config_obj.context_window 收集成当前模型的
//     model_context_window_tokens_override（低优先于全局 context_window_tokens）；
//   - 阈值比例由各压缩器自身的 trigger_context_ratio 提供，本层不再收口。
func (d *DeepAdapter) deepAgentContextEngineConfig(reactCfg, configBase map[string]any) *ceschema.ContextEngineConfig {
	if reactCfg == nil {
		return nil
	}
	cecRaw, ok := reactCfg["context_engine_config"]
	if !ok {
		return nil
	}
	cecMap, ok := cecRaw.(map[string]any)
	if !ok {
		return nil
	}

	// Python: 仅根据 enable_kv_cache_release 切换
	result := ceschema.NewContextEngineConfig()
	// 对齐 Python ReActAgentConfig 默认值，避免零值导致消息数/轮数不限（spec §4.5）
	result.MaxContextMessageNum = 200
	result.DefaultWindowRoundNum = 10

	if v, ok := cecMap["enable_kv_cache_release"]; ok {
		if b, ok := v.(bool); ok {
			result.EnableKVCacheRelease = b
		}
	}

	// 收集各模型条目声明的上下文窗口（同模型多条目冲突时默认条目优先）
	defaultModelName := ""
	firstModelName := ""
	defaultWindow := 0
	firstWindow := 0
	windowsByModel := make(map[string]int)
	for _, entry := range config.GetDefaultModels(configBase) {
		mcc, _ := entry["model_client_config"].(map[string]any)
		if mcc == nil {
			continue
		}
		modelName, _ := mcc["model_name"].(string)
		if modelName == "" {
			continue
		}
		if firstModelName == "" {
			firstModelName = modelName
		}
		isDefault, _ := entry["is_default"].(bool)
		if isDefault && defaultModelName == "" {
			defaultModelName = modelName
		}
		mco, _ := entry["model_config_obj"].(map[string]any)
		window, ok := config.ParsePositiveTokens(mco["context_window"])
		if !ok {
			continue
		}
		if firstWindow == 0 {
			firstWindow = window
		}
		if isDefault && defaultWindow == 0 {
			defaultWindow = window
		}
		if prev, exists := windowsByModel[modelName]; !exists || (isDefault && prev != window) {
			if exists && prev != window {
				logger.Warn(logComponent).
					Str("model_name", modelName).
					Int("previous", prev).
					Int("applied", window).
					Msg("context_window 在同名模型条目间不一致，默认条目覆盖了先前取值")
			}
			windowsByModel[modelName] = window
		}
	}

	// 当前生效模型名：react.model_name 优先，其次默认条目，最后首个条目
	if v, ok := reactCfg["model_name"].(string); ok && v != "" {
		result.ModelName = v
	} else if defaultModelName != "" {
		result.ModelName = defaultModelName
	} else {
		result.ModelName = firstModelName
	}

	// 当前模型的窗口写入 override：按模型名命中优先，其次默认条目，最后首个条目
	if window, ok := windowsByModel[result.ModelName]; ok {
		result.ModelContextWindowTokensOverride = window
	} else if defaultWindow > 0 {
		result.ModelContextWindowTokensOverride = defaultWindow
	} else if firstWindow > 0 {
		result.ModelContextWindowTokensOverride = firstWindow
	}

	return &result
}

// seedRuntimeCwd 从请求/运行时 CWD 种子 CwdState。
// Python: JiuWenClawDeepAdapter._seed_runtime_cwd(cwd) (interface_deep.py:3098-3106)
//
// 解析优先级：
//   - runtimeCwd：传入的 cwd 参数
//   - → d.projectDir：项目目录
//   - → workspaceRoot：workspaceDir 或 projectDir 或 os.Getwd()
//
// Python: workspace_root = workspaceDir or projectDir or os.Getwd()
func (d *DeepAdapter) seedRuntimeCwd(ctx context.Context, cwdArg string) context.Context {
	// Python: workspace_root = str(self._workspace_dir or self._project_dir or os.getcwd())
	workspaceRoot := d.workspaceDir
	if workspaceRoot == "" {
		workspaceRoot = d.projectDir
	}
	if workspaceRoot == "" {
		workspaceRoot, _ = os.Getwd()
	}

	// Python: runtime_cwd = str(cwd or "").strip()
	runtimeCwd := strings.TrimSpace(cwdArg)
	// Python: 目录不存在检测
	if runtimeCwd == "" || !isDir(runtimeCwd) {
		runtimeCwd = strings.TrimSpace(d.projectDir)
	}
	if runtimeCwd == "" || !isDir(runtimeCwd) {
		runtimeCwd = workspaceRoot
	}

	// Python: init_cwd(runtime_cwd, workspace=workspace_root)
	cwdState := cwd.InitCwd(runtimeCwd, cwd.WithWorkspace(workspaceRoot))
	ctx = cwd.WithCwdState(ctx, cwdState)

	logger.Info(logComponent).
		Str("runtime_cwd", runtimeCwd).
		Str("workspace_root", workspaceRoot).
		Msg("CWD initialized from runtime seed")

	return ctx
}

// isDir 检查路径是否为有效目录。
// Python: os.path.isdir(path)
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// setCheckpoint 设置持久化检查点。
// Python: JiuWenClawDeepAdapter.set_checkpoint() (line 1479-1480)
//
// Python 实现：
//
//	async def set_checkpoint() -> None: — 设置检查点
//	    await ensure_persistent_checkpointer() — 确保持久化检查点器可用
//
// Go 中 ensurePersistentCheckpointer 已同步化（SQLite 打开无需异步），
// 检查点器通过 EnsurePersistentCheckpointer 全局绑定，DeepAgent 实例自动使用。
// M-15: ctx 透传（初始化内部有 30s 上限），使创建流程可被父请求取消。
func (d *DeepAdapter) setCheckpoint(ctx context.Context) error {
	return EnsurePersistentCheckpointer(ctx)
}

// buildModelCacheFromDefaults 从 models.defaults 列表构建模型缓存。
// Python: JiuWenClawDeepAdapter._build_model_cache_from_defaults() (line 1495-1533)
//
// key 使用 {model_name}#{index} 格式以支持同名模型共存。
// 同时记录 modelNameToKeys 映射以便按 model_name 查找。
// cache/nameToKeys 由调用方提供并独占（构建期间不对外发布），
// 避免直接写 d.modelCache 与并发读取打擂台。
// 返回值 defaultOrder 按 config 出现顺序记录「带纯 model_name 键」的模型名。
// 默认模型必须按该顺序取第一个（对齐 Python `for name in self._model_name_to_keys`
// 的 dict 插入序）：Go 直接 range map 是随机序，多模型并存时默认模型会随进程漂移。
func (d *DeepAdapter) buildModelCacheFromDefaults(
	configBase map[string]any,
	cache map[string]*llm.Model,
	nameToKeys map[string][]string,
) (defaultOrder []string, orderedKeys []string) {
	nameCounter := make(map[string]int)
	defaultOrder = make([]string, 0, 4)
	orderedKeys = make([]string, 0, 4)

	defaults := config.GetDefaultModels(configBase)
	for _, entry := range defaults {
		mcc, _ := entry["model_client_config"].(map[string]any)
		if mcc == nil {
			continue
		}
		modelName, _ := mcc["model_name"].(string)
		if modelName == "" {
			continue
		}

		idx := nameCounter[modelName]
		nameCounter[modelName] = idx + 1
		cacheKey := fmt.Sprintf("%s#%d", modelName, idx)

		mco, _ := entry["model_config_obj"].(map[string]any)
		if mco == nil {
			mco = make(map[string]any)
		}
		m, err := BuildModelFromEntry(mcc, mco)
		if err != nil {
			logger.Warn(logComponent).
				Err(err).
				Str("model_name", modelName).
				Msg("skipping invalid model entry")
			continue
		}
		cache[cacheKey] = m
		orderedKeys = append(orderedKeys, cacheKey)
		nameToKeys[modelName] = append(nameToKeys[modelName], cacheKey)

		// 同时用纯 model_name 作为 key 指向 is_default=true 的条目
		if isDefault, _ := entry["is_default"].(bool); isDefault {
			cache[modelName] = m
			defaultOrder = append(defaultOrder, modelName)
		}

		alias, _ := entry["alias"].(string)
		if alias != "" && alias != modelName {
			if _, exists := cache[alias]; !exists {
				cache[alias] = m
			}
		}
	}
	return defaultOrder, orderedKeys
}

// buildModelCacheLegacy 回退到旧格式（models.default / react 段）构建单条目缓存。
// Python: JiuWenClawDeepAdapter._build_model_cache_legacy() (line 1535-1560)
func (d *DeepAdapter) buildModelCacheLegacy(configBase map[string]any, cache map[string]*llm.Model) {
	modelsSection, _ := configBase["models"].(map[string]any)
	reactSection, _ := configBase["react"].(map[string]any)

	var defaultModelConfig map[string]any
	if modelsSection != nil {
		defaultModelConfig, _ = modelsSection["default"].(map[string]any)
	}

	var mcc map[string]any
	if defaultModelConfig != nil {
		if v, ok := defaultModelConfig["model_client_config"].(map[string]any); ok {
			mcc = v
		}
	}
	if mcc == nil && reactSection != nil {
		if v, ok := reactSection["model_client_config"].(map[string]any); ok {
			mcc = v
		}
	}
	if mcc == nil {
		mcc = make(map[string]any)
	}

	modelName, _ := mcc["model_name"].(string)
	if modelName == "" && reactSection != nil {
		if v, ok := reactSection["model_name"].(string); ok {
			modelName = v
		}
	}
	if modelName == "" {
		modelName = "gpt-4"
	}
	if _, ok := mcc["model_name"]; !ok {
		mcc["model_name"] = modelName
	}

	var mco map[string]any
	if defaultModelConfig != nil {
		if v, ok := defaultModelConfig["model_config_obj"].(map[string]any); ok {
			mco = v
		}
	}
	if mco == nil && reactSection != nil {
		if v, ok := reactSection["model_config_obj"].(map[string]any); ok {
			mco = v
		}
	}
	if mco == nil {
		mco = make(map[string]any)
	}

	m, err := BuildModelFromEntry(mcc, mco)
	if err != nil {
		logger.Warn(logComponent).
			Err(err).
			Str("model_name", modelName).
			Msg("skipping invalid model entry(legacy)")
		return
	}
	cache[modelName] = m
}

// createModel 从配置创建 Model 实例并构建模型缓存。
// Python: JiuWenClawDeepAdapter._create_model() (line 1562-1593)
//
// 构建期间只操作局部 map，全部就绪后在 stateMu 内整体发布，
// 保证并发读侧不会看到"半成品 map"。
func (d *DeepAdapter) createModel(configBase map[string]any) *llm.Model {
	cache := make(map[string]*llm.Model)
	nameToKeys := make(map[string][]string)
	defaultOrder, orderedKeys := d.buildModelCacheFromDefaults(configBase, cache, nameToKeys)
	if len(cache) == 0 {
		d.buildModelCacheLegacy(configBase, cache)
	}

	if len(cache) == 0 {
		logger.Error(logComponent).Msg("No valid model entries found in config")
		d.publishModelState(nil, cache, nameToKeys, "")
		return nil
	}

	// 优先取 is_default=true 的条目（纯 model_name key），按 config 顺序取第一个。
	// 不能用 map range：Go 的 range 顺序随机，多模型并存时默认模型会随进程漂移，
	// Python 侧 `_model_name_to_keys` 是插入序字典，等价于「列表里第一个 default 条目」。
	defaultName := ""
	for _, name := range defaultOrder {
		if _, ok := cache[name]; ok {
			defaultName = name
			break
		}
	}
	if defaultName == "" {
		// 回退：取第一个 #index key（同样按 config 顺序，避免随机）
		for _, key := range orderedKeys {
			if strings.Contains(key, "#") {
				defaultName = key
				break
			}
		}
	}
	model := cache[defaultName]
	d.publishModelState(model, cache, nameToKeys, defaultName)

	logger.Info(logComponent).
		Str("default_model_name", defaultName).
		Int("model_cache_size", len(cache)).
		Msg("model cache build completed")

	return model
}

// publishModelState 原子发布模型状态。
// cache/nameToKeys 在发布后视为不可变：写侧只能构建新 map 再整体替换，
// 不能就地修改已发布的 map，否则并发 range 会 fatal。
func (d *DeepAdapter) publishModelState(
	model *llm.Model,
	cache map[string]*llm.Model,
	nameToKeys map[string][]string,
	defaultName string,
) {
	var clientCfg *llmschema.ModelClientConfig
	var requestCfg *llmschema.ModelRequestConfig
	if model != nil {
		clientCfg = model.ClientConfig
		requestCfg = model.ModelConfig
	}
	d.stateMu.Lock()
	d.model = model
	d.modelCache = cache
	d.modelNameToKeys = nameToKeys
	d.defaultModelName = defaultName
	d.modelClientConfig = clientCfg
	d.modelRequestConfig = requestCfg
	d.stateMu.Unlock()
}

// modelsSnapshot 返回模型与两张缓存 map 的一致快照。
func (d *DeepAdapter) modelsSnapshot() (*llm.Model, map[string]*llm.Model, map[string][]string) {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.model, d.modelCache, d.modelNameToKeys
}

// modelSnapshot 返回当前模型实例快照。
func (d *DeepAdapter) modelSnapshot() *llm.Model {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.model
}

// defaultModelNameSnapshot 返回默认模型名快照。
func (d *DeepAdapter) defaultModelNameSnapshot() string {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.defaultModelName
}

// configCacheSnapshot 返回 react 配置段缓存快照（发布后不可变）。
func (d *DeepAdapter) configCacheSnapshot() map[string]any {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.configCache
}

// configBaseSnapshot 返回完整配置快照（发布后不可变）。
func (d *DeepAdapter) configBaseSnapshot() map[string]any {
	d.stateMu.RLock()
	defer d.stateMu.RUnlock()
	return d.configBase
}

// setConfigSnapshots 原子发布配置快照（configCache/configBase）。
func (d *DeepAdapter) setConfigSnapshots(configCache, configBase map[string]any) {
	d.stateMu.Lock()
	d.configCache = configCache
	d.configBase = configBase
	d.stateMu.Unlock()
}

// setAppliedModel 记录请求实际使用的模型（stateMu 保护）。
func (d *DeepAdapter) setAppliedModel(model *llm.Model) {
	d.stateMu.Lock()
	d.model = model
	if model != nil {
		d.modelClientConfig = model.ClientConfig
		d.modelRequestConfig = model.ModelConfig
	}
	d.stateMu.Unlock()
}

// resolveModelForRequest 根据请求中的 model_name 参数查找对应模型。
// Python: JiuWenClawDeepAdapter._resolve_model_for_request() (line 1595-1612)
//
// 支持两种格式：
//   - 纯 model_name：查找 is_default=true 的条目
//   - {model_name}#{index}：查找指定索引的条目
func (d *DeepAdapter) resolveModelForRequest(req *schema.AgentRequest) *llm.Model {
	params := parseParams(req.Params)
	requested := ""
	if v, ok := params["model_name"]; ok {
		if s, ok := v.(string); ok {
			requested = strings.TrimSpace(s)
		}
	}
	if requested == "" {
		return d.modelSnapshot()
	}

	model, cache, nameToKeys := d.modelsSnapshot()
	// 精确匹配（#index 格式或纯 model_name key）
	if m, ok := cache[requested]; ok {
		return m
	}

	// 回退：按纯 model_name 查找 is_default=true 的条目
	if _, ok := nameToKeys[requested]; ok {
		if m, ok := cache[requested]; ok {
			return m
		}
	}

	return model
}

// applyModelToReactAgent 将解析后的模型应用到 ReActAgent 并同步 adapter 模型字段。
// Python: JiuWenClawDeepAdapter._apply_model_to_react_agent(resolved_model)
// （非流式/流式/team 三条路径共用的模型应用逻辑，抽出消重）
func (d *DeepAdapter) applyModelToReactAgent(resolvedModel *llm.Model) {
	if resolvedModel == nil {
		return
	}
	if d.instance != nil {
		if reactAgent := d.instance.ReactAgent(); reactAgent != nil {
			reactAgent.SetLLM(resolvedModel)
		}
	}
	// Python: _apply_model_to_react_agent 中同步 adapter 字段
	d.setAppliedModel(resolvedModel)
}

// hasValidModelConfig 检查请求的模型是否有可用配置（凭据维度）。
// Python: JiuWenClawDeepAdapter._has_valid_model_config() (interface_deep.py:3540-3580)
//
// 判定顺序与 Python 逐条一致：
//  1. 请求指定的模型在缓存中且凭据可用 → true
//  2. 默认模型凭据可用 → true
//  3. 缓存中任意模型凭据可用 → true
//  4. config.yaml 的 models.default.model_client_config 凭据可用 → true
//  5. 否则 false（调用方回 "模型未正确配置，请先配置模型信息"）
//
// 注意：仅「模型名存在于缓存」不足以判定可用——首次安装时 config.yaml 带着
// api_base=https://example.com/compatible-mode/v1 的占位配置，旧实现会放行，
// 于是心跳/首次对话真的向 example.com 发请求并报底层网络错误（2026-11-02 实测）。
func (d *DeepAdapter) hasValidModelConfig(requestedModelName string) bool {
	// 快照一次，后续三次判定用同一份视图，避免中途被 reload 换掉。
	model, cache, _ := d.modelsSnapshot()
	// 1. 请求指定的模型
	if requestedModelName != "" {
		if m, ok := cache[requestedModelName]; ok && modelClientUsable(m) {
			return true
		}
	}
	// 2. 默认模型
	if modelClientUsable(model) {
		return true
	}
	// 3. 缓存中任意可用模型
	for _, m := range cache {
		if modelClientUsable(m) {
			return true
		}
	}
	// 4. 配置兜底（Python: get_config().get("models", {}).get("default", {})...）
	//    仅在前三步都判定不出可用模型时才走，避免每请求一次文件 IO。
	if d.defaultModelConfigReader != nil && modelClientConfigUsable(d.defaultModelConfigReader()) {
		return true
	}
	return false
}

// modelClientUsable 判断模型对象上的客户端配置是否含可用凭据。
// Python: _mcc_obj_looks_usable (interface_deep.py:3546-3553)：
// 先 isinstance(ModelClientConfig)，再交给 _mcc_looks_usable。
func modelClientUsable(m *llm.Model) bool {
	if m == nil || m.ClientConfig == nil {
		return false
	}
	return modelClientConfigUsable(map[string]any{
		"api_key":  m.ClientConfig.APIKey,
		"api_base": m.ClientConfig.APIBase,
	})
}

// modelClientConfigUsable 判断 model_client_config 字典是否含可用凭据。
// Python: _mcc_looks_usable (interface_deep.py:292-296)：
// api_key 非空 且 api_base 非空 且 api_base 不在占位符黑名单（均先 strip）。
// Python 用 str(...) 强转后 strip，Go 侧对非字符串值等价用 fmt.Sprint。
func modelClientConfigUsable(mcc map[string]any) bool {
	if mcc == nil {
		return false
	}
	apiKey := strings.TrimSpace(anyToConfigString(mcc["api_key"]))
	apiBase := strings.TrimSpace(anyToConfigString(mcc["api_base"]))
	if apiKey == "" || apiBase == "" {
		return false
	}
	_, isPlaceholder := placeholderAPIBase[apiBase]
	return !isPlaceholder
}

// defaultModelClientConfig 从 config.yaml 读取 models.default.model_client_config。
// Python: get_config().get("models", {}).get("default", {}).get("model_client_config", {})
// 任何一层缺失或读取失败都返回 nil（Python 侧 try/except 仅 Warn，不阻断请求）。
func defaultModelClientConfig() map[string]any {
	cfg, err := config.New("", config.WithNormalize(config.NormalizeConfig))
	if err != nil {
		return nil
	}
	data, err := cfg.Load()
	if err != nil {
		return nil
	}
	return modelClientConfigFromBase(data)
}

// modelClientConfigFromBase 从完整配置字典里取 models.default.model_client_config。
// 任一层缺失返回 nil（Python 侧 try/except 仅 Warn，不阻断请求）。
func modelClientConfigFromBase(data map[string]any) map[string]any {
	models, ok := data["models"].(map[string]any)
	if !ok {
		return nil
	}
	def, ok := models["default"].(map[string]any)
	if !ok {
		return nil
	}
	mcc, ok := def["model_client_config"].(map[string]any)
	if !ok {
		return nil
	}
	return mcc
}

// defaultModelClientConfigFromConfigBase 优先从 adapter 已发布的 configBase 快照取配置，
// 仅在快照为空（未初始化）时回退读盘。
// 对齐 Python：get_config() 是进程级缓存，_has_valid_model_config 第 4 步不会每次读盘。
// Go 的 configBase 在 CreateInstance / ReloadAgentConfig 时已加载并随配置保存刷新，
// 等价于该缓存；避免心跳/首启等"配置未就绪"场景每次都解析 YAML。
func (d *DeepAdapter) defaultModelClientConfigFromConfigBase() map[string]any {
	if mcc := modelClientConfigFromBase(d.configBaseSnapshot()); mcc != nil {
		return mcc
	}
	return defaultModelClientConfig()
}

// anyToConfigString 复刻 Python `str(v or "")` 语义。
// 注意假值处理：Python 的 `v or ""` 会把 None / "" / 0 / False / 空容器统一变成 ""，
// 所以 api_key: 0 在 Python 侧是「未配置」。这里按 kind 判零值，
// 其余类型走 fmt.Sprint（等价 str()）。
func anyToConfigString(v any) string {
	if v == nil {
		return ""
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.String:
		return rv.String()
	case reflect.Bool:
		if !rv.Bool() {
			return ""
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		if rv.Int() == 0 {
			return ""
		}
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		if rv.Uint() == 0 {
			return ""
		}
	case reflect.Float32, reflect.Float64:
		if rv.Float() == 0 {
			return ""
		}
	case reflect.Map, reflect.Slice, reflect.Array:
		if rv.Len() == 0 {
			return ""
		}
	}
	return fmt.Sprint(v)
}

// markSessionActive 递增 session 活跃任务计数。
// Python: _mark_session_active() (line 576-578)
// Counter 语义：允许并发同 session（如 supplement 同时旧任务还在），
// 避免第一个任务结束时驱逐第二个。
func (d *DeepAdapter) markSessionActive(sessionID string) {
	d.sessionMu.Lock()
	d.activeSessionIDs[sessionID]++
	d.sessionMu.Unlock()
}

// unmarkSessionActive 递减 session 活跃任务计数，归零时移除。
// Python: _unmark_session_active() (line 580-599)
// 归零时清理 StreamEventRail 的 per-session 状态，防止长期运行内存泄漏。
func (d *DeepAdapter) unmarkSessionActive(sessionID string) {
	d.sessionMu.Lock()
	count := d.activeSessionIDs[sessionID]
	needCleanup := count <= 1
	if needCleanup {
		delete(d.activeSessionIDs, sessionID)
	} else {
		d.activeSessionIDs[sessionID] = count - 1
	}
	d.sessionMu.Unlock()

	// rail 清理放在锁外：避免把外部组件的耗时操作塞进计数表临界区。
	if needCleanup && d.streamEventRail != nil {
		d.streamEventRail.CleanupSession(sessionID)
	}
}

// isSessionActive 检查 session 是否有活跃任务。
// Python: _is_session_active() (line 601-603)
func (d *DeepAdapter) isSessionActive(sessionID string) bool {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	return d.activeSessionIDs[sessionID] > 0
}

// otherActiveSessions 返回除指定 session 外的活跃任务总数。
// Python: _other_active_sessions() (line 605-610)
// activeSessionCount 返回某会话的活跃计数（0 表示不在跑）。
// Python: dict(self._active_session_ids).get(session_id, 0)
func (d *DeepAdapter) activeSessionCount(sessionID string) int {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	return d.activeSessionIDs[sessionID]
}

func (d *DeepAdapter) otherActiveSessions(sessionID string) int {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	total := 0
	for sid, count := range d.activeSessionIDs {
		if sid != sessionID {
			total += count
		}
	}
	return total
}

// activeSessionIDSnapshot 返回当前活跃 session ID 列表（计数 > 0）。
// 遍历必须在锁内完成，取值后立刻释放，调用方在锁外做 abort 等耗时操作。
func (d *DeepAdapter) activeSessionIDSnapshot() []string {
	d.sessionMu.Lock()
	defer d.sessionMu.Unlock()
	ids := make([]string, 0, len(d.activeSessionIDs))
	for sid, count := range d.activeSessionIDs {
		if count > 0 {
			ids = append(ids, sid)
		}
	}
	return ids
}

// cancelSchedulerRunningTasks 取消 TaskScheduler 运行中的任务。
// Python: _cancel_scheduler_running_tasks (interface_deep.py L3478-3506)。
// 协作式中断（rail.abort + instance.abort）只在检查点生效，
// in-flight LLM HTTP 需要在 await 点直接注入取消才能立即中止。
func (d *DeepAdapter) cancelSchedulerRunningTasks() {
	if d.instance == nil {
		return
	}
	controller := d.instance.LoopController()
	if controller == nil {
		return
	}
	scheduler := controller.TaskScheduler()
	if scheduler == nil {
		return
	}
	cancelled := scheduler.CancelRunningTasks()
	if cancelled > 0 {
		logger.Info(logComponent).
			Int("cancelled", cancelled).
			Msg("[DeepAdapter] interrupt: cancelled running TaskScheduler tasks")
	}
}

// parseParams 将 json.RawMessage 解析为 map[string]any。
// Python: 中 request.params 直接作为 dict 使用。
func parseParams(raw json.RawMessage) map[string]any {
	if len(raw) == 0 {
		return make(map[string]any)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return make(map[string]any)
	}
	return m
}

// paramsString 从 params 中取字符串值，支持默认值。
// Python: request.params.get(key, default)
func paramsString(params map[string]any, key string, defaultVal string) string {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	s, ok := v.(string)
	if !ok {
		return defaultVal
	}
	return s
}

// parseStringSlice 从 params 中取字符串切片值。
// Python: inputs.get("trusted_dirs") → list[str] | None
func parseStringSlice(params map[string]any, key string) []string {
	v, ok := params[key]
	if !ok || v == nil {
		return nil
	}
	// JSON 反序列化通常是 []any
	if slice, ok := v.([]any); ok {
		var result []string
		for _, item := range slice {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	// 直接是 []string
	if slice, ok := v.([]string); ok {
		return slice
	}
	return nil
}

// extractRebuildFollowupPrompt 从 slash 结果中提取 rebuild followup prompt。
// Python: _extract_rebuild_followup_prompt() (interface_deep.py L4273-4283)
func extractRebuildFollowupPrompt(slashResult map[string]any) string {
	if slashResult == nil {
		return ""
	}
	action, _ := slashResult["action"].(string)
	if action != "run_rebuild_followup" {
		return ""
	}
	prompt, _ := slashResult["followup_prompt"].(string)
	return strings.TrimSpace(prompt)
}

// copyMap 浅拷贝 map[string]any。
// Python: inputs = dict(inputs)
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return make(map[string]any)
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// paramsInt 从 params 中取整数值，支持默认值。
func paramsInt(params map[string]any, key string, defaultVal int) int {
	v, ok := params[key]
	if !ok {
		return defaultVal
	}
	switch n := v.(type) {
	case float64:
		return int(n)
	case int:
		return n
	default:
		return defaultVal
	}
}

// resolveEnableModelAnomalyDetectionRail 读取 execution_guard 下的模型异常检测开关。
// 缺省为 false（对齐上游 retry_cfg.get("enabled", False)）；开关与
// buildModelAnomalyDetectionRail 必须读同一段配置，否则会出现"配置关不掉/开不了"。
func resolveEnableModelAnomalyDetectionRail(configBase map[string]any) bool {
	guard, ok := configBase["execution_guard"].(map[string]any)
	if !ok {
		return false
	}
	guardCfg, ok := guard["model_anomaly_detection_rail"].(map[string]any)
	if !ok {
		return false
	}
	enabled, _ := guardCfg["enabled"].(bool)
	return enabled
}

// paramsFloat 从配置映射中获取浮点数值，不存在时返回默认值
func paramsFloat(m map[string]any, key string, defaultVal float64) float64 {
	// 双类型兼容：YAML 整数解出 int（如 completion_timeout: 3600）
	if f, ok := config.Float(m, key); ok {
		return f
	}
	return defaultVal
}

// resolveEnableTaskLoop 解析是否启用任务循环。
// Python: _resolve_enable_task_loop(config, config_base)
//
// skill_create=True 时强制启用：SkillCreateRail 依赖 AFTER_TASK_ITERATION 事件与
// enqueue_follow_up()，单轮模式下无法工作（Python interface_deep.py:2215-2248）。
func (d *DeepAdapter) resolveEnableTaskLoop(config map[string]any, configBase map[string]any) bool {
	configured := true
	if v, ok := config["enable_task_loop"]; ok {
		if b, ok := v.(bool); ok {
			configured = b
		}
	}
	if team.GetSkillCreateEnabled(configBase) {
		if !configured {
			logger.Warn(logComponent).
				Bool("configured_value", configured).
				Msg("skill_create requires enable_task_loop, overriding user config")
		}
		return true
	}
	return configured
}

// resolveEnableTaskPlanning 解析是否启用任务规划。
// Python: _resolve_enable_task_planning(config, config_base)
func (d *DeepAdapter) resolveEnableTaskPlanning(config map[string]any, configBase map[string]any) bool {
	if v, ok := config["enable_task_planning"]; ok {
		if b, ok := v.(bool); ok {
			return b
		}
	}
	return true
}

// resolvePromptMode 解析提示词模式。
// Python: _resolve_prompt_mode(config_base)
func (d *DeepAdapter) resolvePromptMode(configBase map[string]any) hschema.PromptMode {
	if v, ok := configBase["prompt_mode"]; ok {
		if s, ok := v.(string); ok {
			switch s {
			case "minimal":
				return hschema.PromptModeMinimal
			case "none":
				return hschema.PromptModeNone
			default:
				return hschema.PromptModeFull
			}
		}
	}
	return hschema.PromptModeFull
}

// makeDeepAgentConfig 构造 DeepAgentConfig 用于热重载。
// Python: _make_deep_agent_config() (interface_deep.py L2245-2283)
//
// 必须与 CreateInstance 的 CreateDeepAgentParams 同源：Python 侧两条路径都带
// workspace / sys_operation / tools / subagents / context_engine_config 等字段。
// 历史缺陷是本函数只填 Model/Card/Prompt/Rails，于是 `agent.reload_config`
// 热重载后 deepConfig.Workspace 与 deepConfig.SysOperation 变成 nil：
//   - 下一轮 ensureInitialized 把 nil 注入所有 pending rail，SysOperationRail
//     重建出的 fs 工具持有 nil op，read_file 执行即 nil 解引用 panic；
//   - MemoryRail / initMemoryManager 判定「无 workspace」直接跳过。
func (d *DeepAdapter) makeDeepAgentConfig(model *llm.Model, config map[string]any, card *agentschema.AgentCard, toolCards []*tool.ToolCard, railsList []sainterfaces.AgentRail) *hschema.DeepAgentConfig {
	resolvedLanguage := d.resolveRuntimeLanguage()

	// Python: Workspace(root_path=self._workspace_dir or "./", language=resolved_language)
	workspaceRoot := d.workspaceDir
	if workspaceRoot == "" {
		workspaceRoot = "./"
	}

	// Python: configured_subagents, should_add_general_agent = self._build_configured_subagents(model, config, config_base)
	configBase := d.configBaseSnapshot()
	subagentSpecs, shouldEnableGeneralAgent := d.buildConfiguredSubagents(config, configBase)
	// Python: L2575-2577: should_add_general_agent and (sub_mode == "plan" or mode.startswith("agent"))
	shouldEnableGeneralAgent = shouldEnableGeneralAgent && (d.subMode == "plan" || strings.HasPrefix(d.mode, "agent"))

	return &hschema.DeepAgentConfig{
		Model:                  model,
		Card:                   card,
		SystemPrompt:           commonprompt.BuildAgentIdentityPrompt(d.resolvePromptLanguage()),
		ContextEngineConfig:    d.deepAgentContextEngineConfig(config, configBase),
		EnableTaskLoop:         d.resolveEnableTaskLoop(config, configBase),
		MaxIterations:          paramsInt(config, "max_iterations", 15),
		Subagents:              subagentSpecs,
		AddGeneralPurposeAgent: shouldEnableGeneralAgent,
		Tools:                  toolCards,
		Workspace:              hworkspace.NewWorkspace(workspaceRoot, resolvedLanguage),
		SysOperation:           d.sysOperation,
		// Python 的 _make_deep_agent_config 未显式传 auto_create_workspace，
		// 取 dataclass 默认值 True（与 CreateInstance 的硬编码 false 并列存在，
		// 由 Python 侧自身决定）。Go 的 InitWorkspace 目前是占位实现，无副作用。
		AutoCreateWorkspace: true,
		CompletionTimeout:   paramsFloat(config, "completion_timeout", 3600.0),
		Language:            resolvedLanguage,
		PromptMode:          d.resolvePromptMode(nil),
		Rails:               railsList, // 对齐 Python: rails=rails (interface_deep.py L2280)
		VisionModelConfig:   d.visionModelConfig,
		AudioModelConfig:    d.audioModelConfig,
		// 注：EnableReadImageMultimodal 保持零值（false）。Python 热重载会回落默认
		// True，但 Go 的 create 路径（CreateDeepAgentParams 未传）恒为 false，
		// 中途中翻转会让 read_file 的图片多模态行为在会话内突变。
	}
}

// getCurrentAgentRails 获取热重载时需要重新装配的 rail 列表。
// Python: _get_current_agent_rails(config, config_base) (interface_deep.py:2304-2352)
//
// 与 buildAgentRails 的冷启动语义完全不同：
//  1. 就地更新：仅演进 rail（换 LLM 与 auto_scan），实例与注册状态不动；
//  2. 新建：仅 skillRail；
//  3. 复用现有实例重新 init：contextAssemble / contextProcessor / memory / avatar / permission；
//  4. 其余 rail（runtimePrompt/responsePrompt/streamEvent/taskPlanning/security/heartbeat/
//     subagent/filesystem/evolution/skillCreate 等）不进列表，保持原实例与原注册状态。
//     全量重建会清空 StreamEventRail 的 abortRequested 与 TaskPlanningRail 的 usageRecords。
func (d *DeepAdapter) getCurrentAgentRails(config map[string]any, configBase map[string]any) []sainterfaces.AgentRail {
	// 1. 就地更新演进 rail（不重建、不重新注册），对齐 Python :2314-2328
	d.updateSkillEvolutionRail(config)

	// 2. 唯一新建项：skillRail（include_tools 取决于 profile，靠重建切换）
	d.skillRail = d.buildSkillRailWithIncludeTools(d.skillIncludeToolsForProfile(d.instanceOverrides))

	// 3. 复用现有实例（非 nil 才入列；同实例传给 configure 后只重跑 Init，对象不变）
	railsList := make([]sainterfaces.AgentRail, 0, 6)
	if d.skillRail != nil {
		railsList = append(railsList, d.skillRail)
	}
	if d.contextAssembleRail != nil {
		railsList = append(railsList, d.contextAssembleRail)
	}
	if d.contextProcessorRail != nil {
		railsList = append(railsList, d.contextProcessorRail)
	}
	if d.memoryRail != nil {
		railsList = append(railsList, d.memoryRail)
	}
	if d.avatarRail != nil {
		railsList = append(railsList, d.avatarRail)
	}
	if d.permissionRail != nil {
		railsList = append(railsList, d.permissionRail)
	}

	logger.Info(logComponent).Int("rails_count", len(railsList)).Msg("getCurrentAgentRails completed")
	return railsList
}

// extractTextContent 从 payload 提取文本内容。
func extractTextContent(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["content"].(string); ok {
		return v
	}
	if v, ok := payload["text"].(string); ok {
		return v
	}
	return ""
}

// extractReasoningContent 从 payload 提取推理内容。
func extractReasoningContent(payload map[string]any) string {
	if payload == nil {
		return ""
	}
	if v, ok := payload["content"].(string); ok {
		return v
	}
	if v, ok := payload["output"].(string); ok {
		return v
	}
	if v, ok := payload["reasoning"].(string); ok {
		return v
	}
	return ""
}

// updatePermissionRail 原地更新已有 PermissionRail 配置，或在首次启用时新建。
// Python: _update_permission_rail(config_base) (interface_deep.py L2286-2302)
//
// Python 执行步骤：
//
//	Python: 1. permission_config = config_base.get("permissions", {})
//	Python: 2. if self._permission_rail is not None: self._permission_rail.update_config(permission_config)
//	Python: 3. elif permission_config.get("enabled", False): self._permission_rail = build_permission_rail(...)
func (d *DeepAdapter) updatePermissionRail(configBase map[string]any) {
	permissionConfig, _ := configBase["permissions"].(map[string]any)

	if d.permissionRail != nil {
		// Python: self._permission_rail.update_config(permission_config)
		if rail, ok := d.permissionRail.(*secrail.PermissionInterruptRail); ok {
			rail.UpdateConfig(permissionConfig, nil)
			logger.Info(logComponent).Msg("permissionRail config hot-reloaded")
		}
		return
	}

	// Python: elif permission_config.get("enabled", False):
	if permissionConfig != nil {
		if enabled, _ := permissionConfig["enabled"].(bool); enabled {
			rail := d.buildPermissionRail(configBase)
			if rail != nil {
				d.permissionRail = rail
				logger.Info(logComponent).Msg("permissionRail created for first time (hot-reload triggered)")
			}
		}
	}
}

// toPositiveInt 将任意数值或字符串转为正整数；非正数或不可解析返回 (0,false)。
// 用于把 yaml/前端传来的 token 上限（可能是 int、float64 或字符串）统一成 int。
func toPositiveInt(v any) (int, bool) {
	// 数值转换统一走 config.IntValue（int 族/float 族/json.Number/数字字符串），
	// 这里只额外表达"<=0 视同未配置"这一层语义。
	n, ok := config.IntValue(v)
	if !ok || n <= 0 {
		return 0, false
	}
	return n, true
}

// HasSessionRuntime 该会话是否还占用 adapter 侧运行时。
// Python: interface_deep.py has_session_runtime（:4565）
//
// 两个来源任一非空即为真：会话级子适配器缓存、活跃执行计数。
func (d *DeepAdapter) HasSessionRuntime(sessionID string) bool {
	sid := sessionAdapterKey(&sessionID)
	// child 只认自己的会话
	if d.isSessionScoped {
		return d.parentSessionID == sid
	}
	if d.sessionAdapters != nil {
		if d.sessionAdapters.has(sid) || d.sessionAdapters.hasGuard(sid) {
			return true
		}
	}
	if d.isSessionActive(sid) {
		return true
	}
	return d.hasRegisteredSessionTasks(sid)
}

// isWildcardTools 该 tools 值是否为通配 "*"。
// Python: agent_definition.get("tools") != "*"（interface_deep.py:13164/13193）
//
// 注意上游 RuntimeAgentDefinition 已把 "*" 归一化为 ToolsWildcard 标志，
// 但 adapter 拿到的是 to_dict() 结果，仍以 "*" 字符串或单元素列表形式出现。
func isWildcardTools(value any) bool {
	switch v := value.(type) {
	case nil:
		// 缺省即 "*"（Python agent_definition.get("tools", "*")）
		return true
	case string:
		return v == "*"
	case []string:
		return len(v) == 1 && v[0] == "*"
	case []any:
		return len(v) == 1 && v[0] == "*"
	}
	return false
}

// toolsFromDefinition 取 definition 的 tools 值，缺省为 "*"。
// Python: agent_definition.get("tools", "*")
func toolsFromDefinition(definition map[string]any) []string {
	if definition == nil {
		return nil
	}
	return stringSliceOf(definition["tools"])
}

// stringSliceOf 把 definition 里的 []string / []any 统一成 []string。
// Python: list(agent_definition.get("skills") or ())
func stringSliceOf(value any) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		return v
	case []any:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case string:
		return []string{v}
	}
	return nil
}
