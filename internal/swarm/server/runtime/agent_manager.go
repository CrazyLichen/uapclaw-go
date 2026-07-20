package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentFactory 创建 Agent 实例的工厂函数，返回 (*UapClaw, error)。
// 生产环境使用默认的 NewUapClaw + CreateInstance；测试环境可注入 mock 工厂跳过真实 LLM 初始化。
type AgentFactory func(config map[string]any, mode, subMode string) (*UapClaw, error)

// AgentManager Agent 实例管理器（10.3.12）。
//
// 管理 UapClaw 实例的创建、获取和配置重载。
// 使用两层嵌套 map：channel_key → cache_key → agentEntry。
// 对齐 Python: jiuwenswarm/server/runtime/agent_manager.py
type AgentManager struct {
	// agents 存储: channel_key → cache_key → agentEntry
	// 对齐 Python: self.agents: dict[str, dict[str, "JiuWenClaw"]]
	agents map[string]map[string]*agentEntry

	// agentCreateParams 记录创建参数: channel_key → cache_key → agentCreateParamsEntry
	// 对齐 Python: self._agent_create_params: dict[str, dict[str, dict[str, Any]]]
	agentCreateParams map[string]map[string]*agentCreateParamsEntry

	// clientCapabilitiesByChannel ACP 客户端能力: channel_key → capabilities
	// 对齐 Python: self._client_capabilities_by_channel
	// ⤵️ ACP 章节：等 ACP 实现后 initialize() 写入数据
	clientCapabilitiesByChannel map[string]map[string]any

	// latestEnvOverrides 最近一次 env 覆盖
	// 对齐 Python: self._latest_env_overrides
	// 收窄为 map[string]string：Python 中 dict[str, Any]，但所有 value 均作为 os.environ 字符串使用
	latestEnvOverrides map[string]string

	// createAgentFactory 创建 Agent 的工厂函数（默认 NewUapClaw + CreateInstance，测试可注入 mock）。
	createAgentFactory AgentFactory

	// mu 保护并发访问
	mu sync.RWMutex
}

// agentEntry Agent 实例条目，记录元数据。
// 对齐 Python: setattr(agent, "_jiuwenswarm_agent_cache_key/mode/sub_mode/project_dir", ...)
type agentEntry struct {
	// agent UapClaw 实例
	agent *UapClaw
	// cacheKey 缓存键
	cacheKey string
	// mode 运行模式
	mode string
	// subMode 子模式
	subMode string
	// projectDir 项目目录
	projectDir string
}

// agentCreateParamsEntry 创建参数记录，用于 recreate_agent 重建。
// 对齐 Python: self._agent_create_params[channel][cache_key] = {mode, sub_mode, config, cache_key}
type agentCreateParamsEntry struct {
	// mode 运行模式
	mode string
	// subMode 子模式
	subMode string
	// config 创建时的配置
	config map[string]any
	// cacheKey 缓存键
	cacheKey string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件
const amLogComponent = logger.ComponentAgentServer

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentManager 创建 AgentManager 实例。
// 对齐 Python: AgentManager.__init__()
func NewAgentManager() *AgentManager {
	return &AgentManager{
		agents:                      make(map[string]map[string]*agentEntry),
		agentCreateParams:           make(map[string]map[string]*agentCreateParamsEntry),
		clientCapabilitiesByChannel: make(map[string]map[string]any),
		latestEnvOverrides:          make(map[string]string),
		createAgentFactory:          defaultAgentFactory,
	}
}

// SetAgentFactory 设置 Agent 创建工厂函数（供测试注入 mock，生产环境不需要调用）。
func (am *AgentManager) SetAgentFactory(factory AgentFactory) {
	am.mu.Lock()
	defer am.mu.Unlock()
	am.createAgentFactory = factory
}

// PreSetAgent 预注册 Agent 实例到指定 channel 和 cacheKey。
// 用于测试或特殊初始化路径，跳过 createAgent 工厂调用。
// 对齐 Python: 外部创建 agent 后 setattr 并放入 self.agents 的场景。
func (am *AgentManager) PreSetAgent(channelID, cacheKey string, agent *UapClaw, mode, subMode, projectDir string) {
	channelKey := normalizeChannelID(channelID)
	entry := &agentEntry{
		agent:      agent,
		cacheKey:   cacheKey,
		mode:       mode,
		subMode:    subMode,
		projectDir: projectDir,
	}
	am.mu.Lock()
	defer am.mu.Unlock()
	if _, ok := am.agents[channelKey]; !ok {
		am.agents[channelKey] = make(map[string]*agentEntry)
	}
	am.agents[channelKey][cacheKey] = entry
}

// defaultAgentFactory 默认 Agent 工厂：NewUapClaw + CreateInstance。
func defaultAgentFactory(config map[string]any, mode, subMode string) (*UapClaw, error) {
	agent := NewUapClaw()
	if err := agent.CreateInstance(config, mode, subMode); err != nil {
		return nil, fmt.Errorf("[AgentManager] 创建 Agent 实例失败: %w", err)
	}
	return agent, nil
}

// GetAgent 获取 Agent 实例，不存在则自动创建。
// 对齐 Python AgentManager.get_agent：异步，自动创建。
func (am *AgentManager) GetAgent(ctx context.Context, channelID, mode, projectDir, subMode string) (*UapClaw, error) {
	// 对齐 Python L238-242: normalize → cacheKey → 查找
	channelKey := normalizeChannelID(channelID)
	modeKey := normalizeMode(mode)
	subModeKey := normalizeSubMode(subMode)
	projectKey := normalizeProjectDir(projectDir)
	cacheKey := makeAgentCacheKey(modeKey, subModeKey, projectKey)

	// 对齐 Python L243-245: cache hit → return
	am.mu.RLock()
	channelAgents, ok := am.agents[channelKey]
	if ok {
		if entry, ok2 := channelAgents[cacheKey]; ok2 {
			am.mu.RUnlock()
			return entry.agent, nil
		}
	}
	am.mu.RUnlock()

	// 对齐 Python L247-261: cache miss → _create_agent
	config := make(map[string]any)
	if projectKey != "" {
		config["project_dir"] = projectKey
	}
	// ⤵️ ACP: channel_key=="acp" 时合并 _build_acp_agent_config
	// 对齐 Python L250-254: if channel_key == "acp": config = {**config, **_build_acp_agent_config()}

	return am.createAgent(ctx, channelKey, modeKey, config, subModeKey, cacheKey)
}

// GetAgentNoWait 获取已有 Agent 实例，不自动创建。
// 对齐 Python AgentManager.get_agent_nowait：同步，不创建。
// 找不到时返回 nil。
func (am *AgentManager) GetAgentNoWait(channelID, mode, projectDir, subMode string) *UapClaw {
	// 对齐 Python L278-279: normalize channel
	channelKey := normalizeChannelID(channelID)

	am.mu.RLock()
	defer am.mu.RUnlock()

	channelAgents, ok := am.agents[channelKey]
	if !ok || len(channelAgents) == 0 {
		return nil
	}

	// 对齐 Python L283-287: 1. 按 cacheKey 精确查找
	if mode != "" || projectDir != "" || subMode != "" {
		cacheKey := makeAgentCacheKey(mode, subMode, projectDir)
		if entry, ok2 := channelAgents[cacheKey]; ok2 {
			return entry.agent
		}
	}

	// 对齐 Python L289-299: 2. 按字段过滤遍历
	requestedMode := normalizeMode(mode)
	requestedSubMode := normalizeSubMode(subMode)
	requestedProjectDir := normalizeProjectDir(projectDir)

	for _, entry := range channelAgents {
		if requestedMode != "" && entry.mode != requestedMode {
			continue
		}
		if requestedSubMode != "" && entry.subMode != requestedSubMode {
			continue
		}
		if requestedProjectDir != "" && entry.projectDir != requestedProjectDir {
			continue
		}
		return entry.agent
	}

	// 对齐 Python L301-305: 3. 三个参数都为空时，优先返回 mode="agent" 的第一个，否则返回任意第一个
	if mode == "" && projectDir == "" && subMode == "" {
		for _, entry := range channelAgents {
			if entry.mode == "agent" {
				return entry.agent
			}
		}
		for _, entry := range channelAgents {
			return entry.agent
		}
	}

	return nil
}

// Initialize 初始化 AgentManager。
// 对齐 Python AgentManager.initialize(channel_id, extra_config) -> dict|None。
// 非 ACP 通道返回 nil；ACP 通道 ⤵️ 延后。
func (am *AgentManager) Initialize(ctx context.Context, channelID string, extraConfig map[string]any) (map[string]any, error) {
	// 对齐 Python L160-182: channel_key == "acp" → 特殊处理
	channelKey := normalizeChannelID(channelID)
	// ⤵️ ACP: channel_key=="acp" 时：
	//   1. 记录 client_capabilities 到 clientCapabilitiesByChannel
	//   2. cleanup 已有 ACP agents
	//   3. _build_acp_agent_config(extra_config) → _create_agent("acp", "code", config)
	//   4. return ACP_DEFAULT_CAPABILITIES.copy()
	_ = channelKey
	_ = ctx
	_ = extraConfig
	return nil, nil
}

// CreateSession 创建会话。
// 对齐 Python AgentManager.create_session(channel_id, session_id) -> str。
func (am *AgentManager) CreateSession(channelID, sessionID string) (string, error) {
	// 对齐 Python L207-210: 有显式 sessionID → 直接返回
	explicitID := strings.TrimSpace(sessionID)
	if explicitID != "" {
		logger.Info(amLogComponent).
			Str("channel_id", channelID).
			Str("session_id", explicitID).
			Msg("[AgentManager] session ensured")
		return explicitID, nil
	}
	// 对齐 Python L211-215: ACP 通道 → acp_{uuid[:8]}
	channelKey := normalizeChannelID(channelID)
	// ⤵️ ACP: if channel_key == "acp": return fmt.Sprintf("acp_%s", randomHex[:8])
	_ = channelKey
	// 对齐 Python L216: 其他 → "default"
	return "default", nil
}

// GetClientCapabilities 获取客户端能力。
// 对齐 Python AgentManager.get_client_capabilities(channel_id)。
// 当前数据为空，等 ACP 实现后 initialize() 写入 clientCapabilitiesByChannel。
func (am *AgentManager) GetClientCapabilities(channelID string) map[string]any {
	// 对齐 Python L193-196: strip → get → copy or empty
	channelKey := strings.TrimSpace(channelID)
	am.mu.RLock()
	defer am.mu.RUnlock()
	caps, ok := am.clientCapabilitiesByChannel[channelKey]
	if !ok || caps == nil {
		return map[string]any{}
	}
	return copyMap(caps)
}

// ProcessMessage 处理非流式请求。
// 对齐 Python: AgentManager.process_message(request)
// 供 TenantAgentPool 使用，不含 SwitchMode（SwitchMode 在 _handle_unary/_handle_stream 中）。
func (am *AgentManager) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 对齐 Python L439-456: 从 request 解析 channel_id/mode/workspace_dir → get_agent → agent.process_message
	channelID := request.ChannelID
	mode, subMode := resolveModeFromRequest(request)
	projectDir := resolveWorkspaceDirFromRequest(request)

	agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)
	if err != nil {
		// 对齐 Python L454-456: logger.error + raise
		logger.Error(amLogComponent).
			Err(err).
			Str("channel_id", channelID).
			Str("mode", mode).
			Msg("[AgentManager] Error in process_message: no agent available")
		return nil, fmt.Errorf("[AgentManager] No agent available for channel %s: %w", channelID, err)
	}

	return agent.ProcessMessage(ctx, request)
}

// ProcessMessageStream 处理流式请求。
// 对齐 Python: AgentManager.process_message_stream(request)
// 供 TenantAgentPool 使用，不含 SwitchMode（SwitchMode 在 _handle_unary/_handle_stream 中）。
func (am *AgentManager) ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	// 对齐 Python L468-487: 同 process_message 但流式
	channelID := request.ChannelID
	mode, subMode := resolveModeFromRequest(request)
	projectDir := resolveWorkspaceDirFromRequest(request)

	agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(amLogComponent).
			Err(err).
			Str("channel_id", channelID).
			Str("mode", mode).
			Msg("[AgentManager] Error in process_message_stream: no agent available")
		return nil, fmt.Errorf("[AgentManager] No agent available for channel %s: %w", channelID, err)
	}

	return agent.ProcessMessageStream(ctx, request)
}

// ReloadAgentsConfig 重载 Agent 配置。
// 对齐 Python AgentManager.reload_agents_config (agent_manager.py L308-340)。
func (am *AgentManager) ReloadAgentsConfig(ctx context.Context, configPayload map[string]any, envOverrides map[string]string) error {
	// 对齐 Python L310-316: 1. 保存最新 env overrides + 注入 os.environ
	if envOverrides != nil {
		am.mu.Lock()
		am.latestEnvOverrides = copyStringMap(envOverrides)
		am.mu.Unlock()
	}

	for key, val := range envOverrides {
		if val == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, val)
		}
	}

	// 对齐 Python L318-326: 2. 遍历所有 agent 调用 reload_agent_config
	am.mu.RLock()
	agentsSnapshot := make(map[string]map[string]*agentEntry)
	for chKey, chAgents := range am.agents {
		agentsSnapshot[chKey] = make(map[string]*agentEntry)
		for ck, entry := range chAgents {
			agentsSnapshot[chKey][ck] = entry
		}
	}
	am.mu.RUnlock()

	for chKey, chAgents := range agentsSnapshot {
		for _, entry := range chAgents {
			// 对齐 Python L327-330: await agent.reload_agent_config(config_base=config, env_overrides=env)
			// 将 map[string]string 转为 map[string]any 以匹配 adapter 接口签名
			envAny := stringMapToAny(envOverrides)
			if err := entry.agent.ReloadAgentConfig(configPayload, envAny); err != nil {
				// 对齐 Python: 不中断，仅 warn
				logger.Warn(amLogComponent).
					Err(err).
					Str("channel_id", chKey).
					Str("cache_key", entry.cacheKey).
					Msg("[AgentManager] agent reload_agent_config failed")
			}
		}

		// TODO(⤵️ 10.3.2 Team): 更新 team evolution config
		// 对齐 Python L331-339:
		//   team_config = config if isinstance(config, dict) else get_config()
		//   await get_team_manager(channel_id).update_evolution_config(team_config)

		// 对齐 Python L340: logger.info(f"channel {channel_id} reload agent config success.")
		logger.Info(amLogComponent).
			Str("channel_id", chKey).
			Msg("[AgentManager] channel reload agent config success")
	}

	return nil
}

// RecreateAgent 重建 Agent 实例。
// 对齐 Python AgentManager.recreate_agent。
func (am *AgentManager) RecreateAgent(ctx context.Context, channelID string, immediate bool) ([]string, error) {
	// 对齐 Python L360-361: channel_key
	channelKey := normalizeChannelID(channelID)

	am.mu.Lock()
	channelAgents, ok := am.agents[channelKey]
	if !ok || len(channelAgents) == 0 {
		am.mu.Unlock()
		// 对齐 Python L362-367: no active agent → return []
		logger.Info(amLogComponent).
			Str("channel_key", channelKey).
			Msg("[AgentManager] recreate_agent: no active agent on channel")
		return nil, nil
	}

	// 对齐 Python L369-378: 1. 备份创建参数
	existingModes := make([]string, 0, len(channelAgents))
	backupParams := make(map[string]*agentCreateParamsEntry)
	channelParams := am.agentCreateParams[channelKey]
	for cacheKey := range channelAgents {
		existingModes = append(existingModes, cacheKey)
		if params, ok2 := channelParams[cacheKey]; ok2 {
			backupParams[cacheKey] = params
		} else {
			// 对齐 Python L376-377: 未记录创建参数兜底
			backupParams[cacheKey] = &agentCreateParamsEntry{
				mode:     cacheKey,
				subMode:  "",
				config:   nil,
				cacheKey: cacheKey,
			}
		}
	}

	// 对齐 Python L380-397: 2. cleanup + 删除
	for _, entry := range channelAgents {
		_ = entry.agent.Cleanup()
	}
	delete(am.agents, channelKey)
	delete(am.agentCreateParams, channelKey)
	am.mu.Unlock()

	// 对齐 Python L393-397
	logger.Info(amLogComponent).
		Str("channel_key", channelKey).
		Strs("modes", existingModes).
		Msg("[AgentManager] recreate_agent: agents dropped")

	// 对齐 Python L399-404: immediate=False → 不重建
	if !immediate {
		logger.Info(amLogComponent).
			Str("channel_key", channelKey).
			Msg("[AgentManager] recreate_agent: will rebuild on next get_agent()")
		return existingModes, nil
	}

	// 对齐 Python L406-426: 3. 立即按原参数重建
	for cacheKey, params := range backupParams {
		_, err := am.createAgent(ctx, channelKey, params.mode, copyMap(params.config), params.subMode, cacheKey)
		if err != nil {
			// 对齐 Python L416-421: rebuild failed → error log but continue
			logger.Error(amLogComponent).
				Err(err).
				Str("cache_key", cacheKey).
				Msg("[AgentManager] recreate_agent: rebuild failed")
		}
	}

	// 对齐 Python L422-426
	logger.Info(amLogComponent).
		Str("channel_key", channelKey).
		Strs("modes", existingModes).
		Msg("[AgentManager] recreate_agent: rebuilt")

	return existingModes, nil
}

// CancelAllInflightWork 取消所有在途任务。
// 对齐 Python AgentManager.cancel_all_inflight_work。
func (am *AgentManager) CancelAllInflightWork(ctx context.Context) error {
	// 对齐 Python L186-191: 遍历所有 agents 调用 cancel_inflight_work
	am.mu.RLock()
	var agentsCopy []*UapClaw
	for _, chAgents := range am.agents {
		for _, entry := range chAgents {
			agentsCopy = append(agentsCopy, entry.agent)
		}
	}
	am.mu.RUnlock()

	for _, agent := range agentsCopy {
		_ = agent.CancelInflightWork()
	}
	return nil
}

// Cleanup 清理资源。
// 对齐 Python AgentManager.cleanup。
func (am *AgentManager) Cleanup() error {
	// 对齐 Python L491-501: 遍历 cleanup → 清空 maps
	am.mu.Lock()
	defer am.mu.Unlock()

	for chKey, chAgents := range am.agents {
		for _, entry := range chAgents {
			_ = entry.agent.Cleanup()
		}
		delete(am.agents, chKey)
	}

	am.agentCreateParams = make(map[string]map[string]*agentCreateParamsEntry)
	am.clientCapabilitiesByChannel = make(map[string]map[string]any)

	logger.Info(amLogComponent).Msg("[AgentManager] All agents cleaned up")
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// createAgent 创建 Agent 实例。
// 对齐 Python: AgentManager._create_agent
func (am *AgentManager) createAgent(ctx context.Context, channelKey, mode string, config map[string]any, subMode, cacheKey string) (*UapClaw, error) {
	// 对齐 Python L108-113: 1. 注入最新 env overrides
	am.mu.RLock()
	envOverrides := am.latestEnvOverrides
	am.mu.RUnlock()

	for key, val := range envOverrides {
		if val == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, val)
		}
	}

	// 对齐 Python L114-120: 2. normalize + 更新 config 中的 project_dir
	modeKey := normalizeMode(mode)
	subModeKey := normalizeSubMode(subMode)
	projectDir := normalizeProjectDir("")
	if config != nil {
		if pd, ok := config["project_dir"].(string); ok {
			projectDir = normalizeProjectDir(pd)
			if projectDir != "" {
				config["project_dir"] = projectDir
			}
		}
	}
	// 对齐 Python L121: agent_cache_key = cache_key or _make_agent_cache_key(...)
	if cacheKey == "" {
		cacheKey = makeAgentCacheKey(modeKey, subModeKey, projectDir)
	}

	// 对齐 Python L122-128
	logger.Info(amLogComponent).
		Str("channel_key", channelKey).
		Str("mode", modeKey).
		Str("sub_mode", subModeKey).
		Str("project_dir", projectDir).
		Msg("[AgentManager] Creating agent")

	// 对齐 Python L129-130: 3. 创建 UapClaw + CreateInstance（通过工厂函数）
	am.mu.RLock()
	factory := am.createAgentFactory
	am.mu.RUnlock()
	agent, err := factory(config, modeKey, subModeKey)
	if err != nil {
		return nil, err
	}

	// 对齐 Python L131-134: 4. setattr → agentEntry
	entry := &agentEntry{
		agent:      agent,
		cacheKey:   cacheKey,
		mode:       modeKey,
		subMode:    subModeKey,
		projectDir: projectDir,
	}

	// 对齐 Python L135: 5. 写入 agents map
	am.mu.Lock()
	if _, ok := am.agents[channelKey]; !ok {
		am.agents[channelKey] = make(map[string]*agentEntry)
	}
	am.agents[channelKey][cacheKey] = entry

	// 对齐 Python L137-142: 6. 写入 agentCreateParams
	if _, ok := am.agentCreateParams[channelKey]; !ok {
		am.agentCreateParams[channelKey] = make(map[string]*agentCreateParamsEntry)
	}
	am.agentCreateParams[channelKey][cacheKey] = &agentCreateParamsEntry{
		mode:     modeKey,
		subMode:  subModeKey,
		config:   copyMap(config),
		cacheKey: cacheKey,
	}
	am.mu.Unlock()

	// 对齐 Python L143
	logger.Info(amLogComponent).
		Str("channel_key", channelKey).
		Str("cache_key", cacheKey).
		Msg("[AgentManager] agent created")

	return agent, nil
}

// normalizeChannelID 规范化通道 ID。
// 对齐 Python: _normalize_channel_id — None→"default"，strip
func normalizeChannelID(channelID string) string {
	s := strings.TrimSpace(channelID)
	if s == "" {
		return "default"
	}
	return s
}

// normalizeMode 规范化模式。
// 对齐 Python: _normalize_mode — None→"agent"，strip
func normalizeMode(mode string) string {
	s := strings.TrimSpace(mode)
	if s == "" {
		return "agent"
	}
	return s
}

// normalizeSubMode 规范化子模式。
// 对齐 Python: _normalize_sub_mode — None→""，strip
func normalizeSubMode(subMode string) string {
	return strings.TrimSpace(subMode)
}

// normalizeProjectDir 规范化项目目录。
// 对齐 Python: _normalize_project_dir — None→""，strip+abspath+expanduser
func normalizeProjectDir(projectDir string) string {
	raw := strings.TrimSpace(projectDir)
	if raw == "" {
		return ""
	}
	// 对齐 Python: os.path.expanduser
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			raw = strings.Replace(raw, "~", home, 1)
		}
	}
	// 对齐 Python: os.path.abspath
	abs, err := filepath.Abs(raw)
	if err != nil {
		return raw
	}
	// 对齐 Python: os.path.normcase（Linux 下 filepath.Clean 等价）
	return filepath.Clean(abs)
}

// makeAgentCacheKey 生成 Agent 缓存键。
// 格式：{mode}:{subMode}:{projectDir}
// 对齐 Python: _make_agent_cache_key
func makeAgentCacheKey(mode, subMode, projectDir string) string {
	return fmt.Sprintf("%s:%s:%s", normalizeMode(mode), normalizeSubMode(subMode), normalizeProjectDir(projectDir))
}

// copyMap 深拷贝 map[string]any（仅一层）。
func copyMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// copyStringMap 深拷贝 map[string]string。
func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	result := make(map[string]string, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// stringMapToAny 将 map[string]string 转为 map[string]any，
// 用于适配 adapter 接口中 map[string]any 的参数签名。
func stringMapToAny(m map[string]string) map[string]any {
	if m == nil {
		return nil
	}
	result := make(map[string]any, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

// resolveModeFromRequest 从请求中解析 mode。
// 对齐 Python: AgentManager.process_message 中 params.get("mode", "agent.plan")
func resolveModeFromRequest(request *schema.AgentRequest) (mode, subMode string) {
	if request.Params == nil {
		return "agent", ""
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return "agent", ""
	}
	modeFull, _ := params["mode"].(string)
	if modeFull == "" {
		modeFull = "agent.plan"
	}
	// 对齐 Python L441: mode = str(mode_full).split(".")[0]
	parts := strings.SplitN(modeFull, ".", 2)
	mode = parts[0]
	if mode == "" {
		mode = "agent"
	}
	if len(parts) > 1 {
		subMode = parts[1]
	}
	return mode, subMode
}

// resolveWorkspaceDirFromRequest 从请求中解析 workspace_dir。
// 对齐 Python: params.get("workspace_dir")
func resolveWorkspaceDirFromRequest(request *schema.AgentRequest) string {
	if request.Params == nil {
		return ""
	}
	var params map[string]any
	if err := json.Unmarshal(request.Params, &params); err != nil {
		return ""
	}
	if wd, ok := params["workspace_dir"].(string); ok {
		return wd
	}
	return ""
}
