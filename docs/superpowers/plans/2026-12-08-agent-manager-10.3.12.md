# 10.3.12 AgentManager 多实例实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 将 AgentManager 从 stub 升级为完整的多实例管理器，支持按 channel/mode/projectDir 区分和管理多个 UapClaw 实例，1:1 对齐 Python agent_manager.py

**Architecture:** AgentManager 使用两层嵌套 map（channel_key → cache_key → agentEntry）存储多个 UapClaw 实例。agentEntry 包装 UapClaw 及其元数据（mode/subMode/projectDir），agentCreateParams 独立存储创建参数用于 recreate_agent 重建。ProcessMessage/ProcessMessageStream 提供统一请求入口。

**Tech Stack:** Go 1.22+, sync.RWMutex, context.Context

---

## 文件结构

| 文件 | 操作 | 职责 |
|------|------|------|
| `internal/swarm/server/runtime/agent_manager.go` | 重写 | AgentManager 完整实现 |
| `internal/swarm/server/runtime/agent_manager_test.go` | 重写 | AgentManager 完整测试 |
| `internal/swarm/server/agent_server.go` | 修改 | 回填 cancelAllInflightWork + handleUnary/handleStream 改用 ProcessMessage/Stream |
| `internal/swarm/server/handle_envelope.go` | 修改 | handleUnary/handleStream 简化 + injectACPCapabilities 回填 |
| `internal/swarm/server/handle_initialize.go` | 修改 | 回填 AgentManager.Initialize 调用 |
| `internal/swarm/server/handle_config.go` | 修改 | ReloadAgentsConfig 签名更新（加 ctx） |
| `internal/swarm/server/runtime/doc.go` | 修改 | 更新文件目录描述 |
| `IMPLEMENTATION_PLAN.md` | 修改 | 更新 10.3.12 状态 |

---

### Task 1: 重写 AgentManager 核心结构体和 normalize 辅助函数

**Files:**
- Modify: `internal/swarm/server/runtime/agent_manager.go`

- [ ] **Step 1: 重写 agent_manager.go — 结构体 + normalize 辅助函数 + makeAgentCacheKey**

替换整个文件内容为：

```go
package runtime

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

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
	latestEnvOverrides map[string]any

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

// ──────────────────────────── 全局变量 ────────────────────────────

// logComponent 日志组件
const logComponent = logger.ComponentAgentServer

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentManager 创建 AgentManager 实例。
// 对齐 Python: AgentManager.__init__()
func NewAgentManager() *AgentManager {
	return &AgentManager{
		agents:                      make(map[string]map[string]*agentEntry),
		agentCreateParams:           make(map[string]map[string]*agentCreateParamsEntry),
		clientCapabilitiesByChannel: make(map[string]map[string]any),
		latestEnvOverrides:          make(map[string]any),
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

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
// 对齐 Python: _normalize_project_dir — None→""，strip+abspath+expanduser+normcase
func normalizeProjectDir(projectDir string) string {
	raw := strings.TrimSpace(projectDir)
	if raw == "" {
		return ""
	}
	// 展开 ~
	if strings.HasPrefix(raw, "~") {
		home, err := os.UserHomeDir()
		if err == nil {
			raw = strings.Replace(raw, "~", home, 1)
		}
	}
	// 绝对路径
	abs, err := filepath.Abs(raw)
	if err != nil {
		return raw
	}
	// normcase（Linux 下无操作，Windows 下转小写）
	return filepath.Clean(abs)
}

// makeAgentCacheKey 生成 Agent 缓存键。
// 格式：{mode}:{subMode}:{projectDir}
// 对齐 Python: _make_agent_cache_key
func makeAgentCacheKey(mode, subMode, projectDir string) string {
	return fmt.Sprintf("%s:%s:%s", normalizeMode(mode), normalizeSubMode(subMode), normalizeProjectDir(projectDir))
}
```

- [ ] **Step 2: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill 2>/dev/null; true`

Then: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/server/runtime/...`

Expected: 编译失败（缺少方法实现），但结构体和辅助函数无语法错误

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/agent_manager.go
git commit -m "refactor: 重写 AgentManager 结构体和 normalize 辅助函数（10.3.12 WIP）"
```

---

### Task 2: 实现 createAgent + GetAgent + GetAgentNoWait

**Files:**
- Modify: `internal/swarm/server/runtime/agent_manager.go`

- [ ] **Step 1: 在 agent_manager.go 的导出函数区块添加 createAgent、GetAgent、GetAgentNoWait**

在 `NewAgentManager()` 函数之后、非导出函数区块之前添加：

```go
// GetAgent 获取 Agent 实例，不存在则自动创建。
// 对齐 Python AgentManager.get_agent：异步，自动创建。
func (am *AgentManager) GetAgent(ctx context.Context, channelID, mode, projectDir, subMode string) (*UapClaw, error) {
	channelKey := normalizeChannelID(channelID)
	modeKey := normalizeMode(mode)
	subModeKey := normalizeSubMode(subMode)
	projectKey := normalizeProjectDir(projectDir)
	cacheKey := makeAgentCacheKey(modeKey, subModeKey, projectKey)

	am.mu.RLock()
	channelAgents, ok := am.agents[channelKey]
	if ok {
		if entry, ok2 := channelAgents[cacheKey]; ok2 {
			am.mu.RUnlock()
			return entry.agent, nil
		}
	}
	am.mu.RUnlock()

	config := make(map[string]any)
	if projectKey != "" {
		config["project_dir"] = projectKey
	}
	// ⤵️ ACP: channel_key=="acp" 时合并 _build_acp_agent_config

	return am.createAgent(ctx, channelKey, modeKey, config, subModeKey, cacheKey)
}

// GetAgentNoWait 获取已有 Agent 实例，不自动创建。
// 对齐 Python AgentManager.get_agent_nowait：同步，不创建。
// 找不到时返回 nil。
func (am *AgentManager) GetAgentNoWait(channelID, mode, projectDir, subMode string) *UapClaw {
	channelKey := normalizeChannelID(channelID)

	am.mu.RLock()
	defer am.mu.RUnlock()

	channelAgents, ok := am.agents[channelKey]
	if !ok || len(channelAgents) == 0 {
		return nil
	}

	// 1. 按 cacheKey 精确查找
	if mode != "" || projectDir != "" || subMode != "" {
		cacheKey := makeAgentCacheKey(mode, subMode, projectDir)
		if entry, ok2 := channelAgents[cacheKey]; ok2 {
			return entry.agent
		}
	}

	// 2. 按字段过滤遍历
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

	// 3. 三个参数都为空时，优先返回 mode="agent" 的第一个，否则返回任意第一个
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
```

在非导出函数区块添加 `createAgent`（在 normalize 函数之后）：

```go
// createAgent 创建 Agent 实例。
// 对齐 Python: AgentManager._create_agent
func (am *AgentManager) createAgent(ctx context.Context, channelKey, mode string, config map[string]any, subMode, cacheKey string) (*UapClaw, error) {
	// 1. 注入最新 env overrides（对齐 Python: for env_key, env_value in self._latest_env_overrides.items()）
	am.mu.RLock()
	envOverrides := am.latestEnvOverrides
	am.mu.RUnlock()

	for key, val := range envOverrides {
		s, ok := val.(string)
		if !ok && val != nil {
			s = fmt.Sprintf("%v", val)
		}
		if val == nil || s == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, s)
		}
	}

	// 2. normalize project_dir
	projectDir := normalizeProjectDir("")
	if config != nil {
		if pd, ok := config["project_dir"].(string); ok {
			projectDir = normalizeProjectDir(pd)
			if projectDir != "" {
				config["project_dir"] = projectDir
			}
		}
	}

	logger.Info(logComponent).
		Str("channel_key", channelKey).
		Str("mode", mode).
		Str("sub_mode", subMode).
		Str("project_dir", projectDir).
		Msg("[AgentManager] Creating agent")

	// 3. 创建 UapClaw + CreateInstance
	agent := NewUapClaw()
	if err := agent.CreateInstance(config, mode, subMode); err != nil {
		return nil, fmt.Errorf("[AgentManager] 创建 Agent 实例失败: %w", err)
	}

	// 4. 构建 agentEntry
	entry := &agentEntry{
		agent:      agent,
		cacheKey:   cacheKey,
		mode:       normalizeMode(mode),
		subMode:    normalizeSubMode(subMode),
		projectDir: projectDir,
	}

	// 5. 写入 agents map
	am.mu.Lock()
	if _, ok := am.agents[channelKey]; !ok {
		am.agents[channelKey] = make(map[string]*agentEntry)
	}
	am.agents[channelKey][cacheKey] = entry

	// 6. 写入 agentCreateParams
	if _, ok := am.agentCreateParams[channelKey]; !ok {
		am.agentCreateParams[channelKey] = make(map[string]*agentCreateParamsEntry)
	}
	am.agentCreateParams[channelKey][cacheKey] = &agentCreateParamsEntry{
		mode:     normalizeMode(mode),
		subMode:  normalizeSubMode(subMode),
		config:   copyMap(config),
		cacheKey: cacheKey,
	}
	am.mu.Unlock()

	logger.Info(logComponent).
		Str("channel_key", channelKey).
		Str("cache_key", cacheKey).
		Msg("[AgentManager] Agent created")

	return agent, nil
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
```

- [ ] **Step 2: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/server/runtime/...`

Expected: 编译失败（还缺少其他方法），但新增代码无语法错误

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/agent_manager.go
git commit -m "feat: 实现 createAgent + GetAgent + GetAgentNoWait（10.3.12 WIP）"
```

---

### Task 3: 实现剩余 AgentManager 方法

**Files:**
- Modify: `internal/swarm/server/runtime/agent_manager.go`

- [ ] **Step 1: 在导出函数区块添加 Initialize、CreateSession、GetClientCapabilities、ProcessMessage、ProcessMessageStream、ReloadAgentsConfig、RecreateAgent、CancelAllInflightWork、Cleanup**

在 `GetAgentNoWait` 之后添加：

```go
// Initialize 初始化 AgentManager。
// 对齐 Python AgentManager.initialize(channel_id, extra_config) -> dict|None。
// 非通道返回 nil；ACP 通道 ⤵️ 延后。
func (am *AgentManager) Initialize(ctx context.Context, channelID string, extraConfig map[string]any) (map[string]any, error) {
	channelKey := normalizeChannelID(channelID)
	// ⤵️ ACP: channel_key=="acp" 时创建 code agent + 返回 ACP_DEFAULT_CAPABILITIES
	// 对齐 Python: if channel_key == "acp": ... return ACP_DEFAULT_CAPABILITIES.copy()
	_ = channelKey
	_ = ctx
	_ = extraConfig
	return nil, nil
}

// CreateSession 创建会话。
// 对齐 Python AgentManager.create_session(channel_id, session_id) -> str。
func (am *AgentManager) CreateSession(channelID, sessionID string) (string, error) {
	explicitID := strings.TrimSpace(sessionID)
	if explicitID != "" {
		logger.Info(logComponent).
			Str("channel_id", channelID).
			Str("session_id", explicitID).
			Msg("[AgentManager] session ensured")
		return explicitID, nil
	}
	channelKey := normalizeChannelID(channelID)
	// ⤵️ ACP: ACP 通道生成 acp_{uuid[:8]}
	_ = channelKey
	return "default", nil
}

// GetClientCapabilities 获取客户端能力。
// 对齐 Python AgentManager.get_client_capabilities(channel_id)。
// 当前数据为空，等 ACP 实现后 initialize() 写入 clientCapabilitiesByChannel。
func (am *AgentManager) GetClientCapabilities(channelID string) map[string]any {
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
func (am *AgentManager) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	channelID := request.ChannelID
	mode, subMode := resolveModeFromRequest(request)
	projectDir := resolveWorkspaceDirFromRequest(request)

	agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("channel_id", channelID).
			Str("mode", mode).
			Msg("[AgentManager] No agent available for process_message")
		return nil, fmt.Errorf("[AgentManager] No agent available for channel %s: %w", channelID, err)
	}

	return agent.ProcessMessage(ctx, request)
}

// ProcessMessageStream 处理流式请求。
// 对齐 Python: AgentManager.process_message_stream(request)
func (am *AgentManager) ProcessMessageStream(ctx context.Context, request *schema.AgentRequest) (<-chan *schema.AgentResponseChunk, error) {
	channelID := request.ChannelID
	mode, subMode := resolveModeFromRequest(request)
	projectDir := resolveWorkspaceDirFromRequest(request)

	agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("channel_id", channelID).
			Str("mode", mode).
			Msg("[AgentManager] No agent available for process_message_stream")
		return nil, fmt.Errorf("[AgentManager] No agent available for channel %s: %w", channelID, err)
	}

	return agent.ProcessMessageStream(ctx, request)
}

// ReloadAgentsConfig 重载 Agent 配置。
// 对齐 Python AgentManager.reload_agents_config (agent_manager.py L308-340)。
func (am *AgentManager) ReloadAgentsConfig(ctx context.Context, configPayload map[string]any, envOverrides map[string]any) error {
	// 1. 保存最新 env overrides
	if envOverrides != nil {
		am.mu.Lock()
		am.latestEnvOverrides = copyMap(envOverrides)
		am.mu.Unlock()
	}

	// 2. env 注入 os.environ（对齐 Python: for env_key, env_value in env_overrides.items()）
	for key, val := range envOverrides {
		s, ok := val.(string)
		if !ok && val != nil {
			s = fmt.Sprintf("%v", val)
		}
		if val == nil || s == "" {
			_ = os.Unsetenv(key)
		} else {
			_ = os.Setenv(key, s)
		}
	}

	// 3. 遍历所有 agent 调用 reload_agent_config
	am.mu.RLock()
	agentsCopy := make(map[string]map[string]*agentEntry)
	for chKey, chAgents := range am.agents {
		agentsCopy[chKey] = make(map[string]*agentEntry)
		for ck, entry := range chAgents {
			agentsCopy[chKey][ck] = entry
		}
	}
	am.mu.RUnlock()

	for chKey, chAgents := range agentsCopy {
		for _, entry := range chAgents {
			if err := entry.agent.ReloadAgentConfig(configPayload, envOverrides); err != nil {
				logger.Warn(logComponent).
					Err(err).
					Str("channel_id", chKey).
					Str("cache_key", entry.cacheKey).
					Msg("[AgentManager] agent reload_agent_config failed")
			}
		}

		// TODO(⤵️ 10.3.2 Team): 更新 team evolution config
		// 对齐 Python: await get_team_manager(channel_id).update_evolution_config(team_config)

		logger.Info(logComponent).
			Str("channel_id", chKey).
			Msg("[AgentManager] channel reload agent config success")
	}

	return nil
}

// RecreateAgent 重建 Agent 实例。
// 对齐 Python AgentManager.recreate_agent。
func (am *AgentManager) RecreateAgent(ctx context.Context, channelID string, immediate bool) ([]string, error) {
	channelKey := normalizeChannelID(channelID)

	am.mu.Lock()
	channelAgents, ok := am.agents[channelKey]
	if !ok || len(channelAgents) == 0 {
		am.mu.Unlock()
		logger.Info(logComponent).
			Str("channel_key", channelKey).
			Msg("[AgentManager] recreate_agent: no active agent on channel")
		return nil, nil
	}

	// 1. 备份创建参数
	existingModes := make([]string, 0, len(channelAgents))
	backupParams := make(map[string]*agentCreateParamsEntry)
	channelParams := am.agentCreateParams[channelKey]
	for cacheKey := range channelAgents {
		existingModes = append(existingModes, cacheKey)
		if params, ok := channelParams[cacheKey]; ok {
			backupParams[cacheKey] = params
		} else {
			backupParams[cacheKey] = &agentCreateParamsEntry{
				mode:     cacheKey,
				subMode:  "",
				config:   nil,
				cacheKey: cacheKey,
			}
		}
	}

	// 2. cleanup + 删除
	for _, entry := range channelAgents {
		_ = entry.agent.Cleanup()
	}
	delete(am.agents, channelKey)
	delete(am.agentCreateParams, channelKey)
	am.mu.Unlock()

	logger.Info(logComponent).
		Str("channel_key", channelKey).
		Strs("modes", existingModes).
		Msg("[AgentManager] recreate_agent: agents dropped")

	if !immediate {
		logger.Info(logComponent).
			Str("channel_key", channelKey).
			Msg("[AgentManager] recreate_agent: will rebuild on next get_agent()")
		return existingModes, nil
	}

	// 3. 立即按原参数重建
	for cacheKey, params := range backupParams {
		_, err := am.createAgent(ctx, channelKey, params.mode, copyMap(params.config), params.subMode, cacheKey)
		if err != nil {
			logger.Error(logComponent).
				Err(err).
				Str("cache_key", cacheKey).
				Msg("[AgentManager] recreate_agent: rebuild failed")
		}
	}

	logger.Info(logComponent).
		Str("channel_key", channelKey).
		Strs("modes", existingModes).
		Msg("[AgentManager] recreate_agent: rebuilt")

	return existingModes, nil
}

// CancelAllInflightWork 取消所有在途任务。
// 对齐 Python AgentManager.cancel_all_inflight_work。
func (am *AgentManager) CancelAllInflightWork(ctx context.Context) error {
	am.mu.RLock()
	agentsCopy := make([]*UapClaw, 0)
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

	logger.Info(logComponent).Msg("[AgentManager] All agents cleaned up")
	return nil
}
```

- [ ] **Step 2: 在非导出函数区块添加 resolveModeFromRequest 和 resolveWorkspaceDirFromRequest**

在 `copyMap` 之后添加：

```go
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
```

注意：需要确保 import 中有 `encoding/json`。

- [ ] **Step 3: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/server/runtime/...`

Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/runtime/agent_manager.go
git commit -m "feat: 实现所有 AgentManager 方法（10.3.12 WIP）"
```

---

### Task 4: 重写 AgentManager 测试

**Files:**
- Modify: `internal/swarm/server/runtime/agent_manager_test.go`

- [ ] **Step 1: 重写测试文件，覆盖所有方法**

替换整个文件内容为完整测试，包括：
- `TestNewAgentManager`：初始化空 maps
- `TestNormalizeChannelID` / `TestNormalizeMode` / `TestNormalizeSubMode` / `TestNormalizeProjectDir` / `TestMakeAgentCacheKey`
- `TestAgentManager_GetAgent`：缓存命中 + 自动创建
- `TestAgentManager_GetAgent_不同mode创建不同实例`
- `TestAgentManager_GetAgentNoWait`：精确命中 + 字段过滤 + fallback + nil
- `TestAgentManager_Initialize`：非 ACP 返回 nil
- `TestAgentManager_CreateSession`：显式 ID + 空 ID 返回 "default"
- `TestAgentManager_GetClientCapabilities`：空 map
- `TestAgentManager_ProcessMessage`：委托
- `TestAgentManager_ProcessMessageStream`：委托
- `TestAgentManager_ReloadAgentsConfig`：env 注入 + agent reload
- `TestReloadAgentsConfig_环境变量注入`（保留已有）
- `TestReloadAgentsConfig_空字符串Unsetenv`（保留已有）
- `TestReloadAgentsConfig_nil值Unsetenv`（保留已有）
- `TestAgentManager_RecreateAgent`：immediate=true/false + 空 channel
- `TestAgentManager_CancelAllInflightWork`
- `TestAgentManager_Cleanup`

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test -tags=test -v ./internal/swarm/server/runtime/... -run TestAgentManager -count=1`

Expected: 所有测试通过

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/agent_manager_test.go
git commit -m "test: 重写 AgentManager 完整测试（10.3.12）"
```

---

### Task 5: 更新 handle_config.go — ReloadAgentsConfig 签名变化

**Files:**
- Modify: `internal/swarm/server/handle_config.go`

- [ ] **Step 1: 更新 ReloadAgentsConfig 调用签名（加 ctx）**

将：
```go
if err := s.agentManager.ReloadAgentsConfig(configPayload, envOverrides); err != nil {
```
改为：
```go
if err := s.agentManager.ReloadAgentsConfig(ctx, configPayload, envOverrides); err != nil {
```

注意：`handleAgentReloadConfig` 方法签名已有 `ctx context.Context` 参数。

- [ ] **Step 2: 运行编译检查**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/server/...`

Expected: 编译通过（如果其他调用点还没更新会报错，逐步修复）

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/handle_config.go
git commit -m "refactor: 更新 ReloadAgentsConfig 签名调用（10.3.12）"
```

---

### Task 6: 回填 agent_server.go — cancelAllInflightWork + handleUnary/handleStream 简化

**Files:**
- Modify: `internal/swarm/server/agent_server.go`
- Modify: `internal/swarm/server/handle_envelope.go`

- [ ] **Step 1: 回填 agent_server.go:cancelAllInflightWork**

替换 `cancelAllInflightWork` 方法体为：
```go
func (s *AgentServer) cancelAllInflightWork() {
	if s.agentManager == nil {
		return
	}
	_ = s.agentManager.CancelAllInflightWork(context.Background())
}
```

- [ ] **Step 2: 简化 handle_envelope.go:handleUnary**

将 handleUnary 中的手动编排逻辑（解析 mode → GetAgent → SwitchMode → ProcessMessage → writeResponse）替换为：

```go
func (s *AgentServer) handleUnary(ctx context.Context, request *schema.AgentRequest) {
	// 1. 特殊方法拦截（不变）
	switch request.ReqMethod {
	case schema.ReqMethodInitialize:
		resp, err := s.handleInitialize(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "INITIALIZE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodSessionCreate:
		resp, err := s.handleSessionCreate(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "SESSION_CREATE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodSessionFork:
		resp, err := s.handleSessionFork(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "SESSION_FORK_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	case schema.ReqMethodACPToolResponse:
		resp, err := s.handleACPToolResponse(ctx, request)
		if err != nil {
			s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "ACP_TOOL_RESPONSE_ERROR")
			return
		}
		s.writeResponse(request.RequestID, request.ChannelID, resp)
		return
	}

	// 2. 委托 AgentManager.ProcessMessage（内含 mode 解析 + GetAgent + SwitchMode）
	resp, err := s.agentManager.ProcessMessage(ctx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("Agent 处理请求失败")
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_PROCESS_ERROR")
		return
	}

	// 3. code 模式 switchMode（排除 team 子模式）
	// 注意：ProcessMessage 内部已通过 GetAgent 获取了 agent，
	// 但 SwitchMode 需要在 ProcessMessage 之前调用。
	// 保留 handleUnary 中的 SwitchMode 逻辑在 AgentManager.ProcessMessage 内部处理。

	s.writeResponse(request.RequestID, request.ChannelID, resp)
}
```

**重要**：SwitchMode 逻辑需要迁移到 AgentManager.ProcessMessage 中。在 ProcessMessage 的 `GetAgent` 成功后、调用 `agent.ProcessMessage` 前添加 SwitchMode 逻辑：

更新 AgentManager.ProcessMessage：
```go
func (am *AgentManager) ProcessMessage(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	channelID := request.ChannelID
	mode, subMode := resolveModeFromRequest(request)
	projectDir := resolveWorkspaceDirFromRequest(request)

	agent, err := am.GetAgent(ctx, channelID, mode, projectDir, subMode)
	if err != nil {
		return nil, fmt.Errorf("[AgentManager] No agent available for channel %s: %w", channelID, err)
	}

	// code 模式 switchMode（排除 team 子模式，对齐 Python sub_mode != "team"）
	if mode == "code" && subMode != "team" {
		sid := ""
		if request.SessionID != nil {
			sid = *request.SessionID
		}
		_ = agent.SwitchMode(ctx, sid, subMode)
	}

	return agent.ProcessMessage(ctx, request)
}
```

同理更新 ProcessMessageStream。

- [ ] **Step 3: 简化 handle_envelope.go:handleStream**

将 handleStream 中的手动编排逻辑替换为：

```go
func (s *AgentServer) handleStream(ctx context.Context, request *schema.AgentRequest) {
	sessionID := ""
	if request.SessionID != nil {
		sessionID = *request.SessionID
	}

	// 1. 创建子 context + cancel
	streamCtx, cancel := context.WithCancel(ctx)

	// 2. 注册流式任务
	s.registerStreamTask(sessionID, cancel)
	defer s.cancelStreamTask(sessionID)

	// 3. 启动心跳 goroutine
	heartbeatDone := make(chan struct{})
	heartbeatTrigger := make(chan struct{}, 1)
	go s.runKeepalive(streamCtx, request.RequestID, request.ChannelID, heartbeatDone, heartbeatTrigger)

	// 4. 委托 AgentManager.ProcessMessageStream
	ch, err := s.agentManager.ProcessMessageStream(streamCtx, request)
	if err != nil {
		logger.Error(logComponent).
			Err(err).
			Str("request_id", request.RequestID).
			Msg("Agent 流式处理失败")
		close(heartbeatTrigger)
		<-heartbeatDone
		s.writeErrorResponse(request.RequestID, request.ChannelID, err.Error(), "AGENT_STREAM_ERROR")
		return
	}

	// 5. 逐 chunk 从 ch 读取
	chunkCount := 0
	for chunk := range ch {
		chunkCount++
		select {
		case heartbeatTrigger <- struct{}{}:
		default:
		}
		s.writeChunk(request.RequestID, request.ChannelID, chunk, chunkCount, true)
	}

	// 6. 停止心跳，清理
	close(heartbeatTrigger)
	<-heartbeatDone

	logger.Debug(logComponent).
		Str("request_id", request.RequestID).
		Int("chunk_count", chunkCount).
		Msg("流式处理完成")
}
```

- [ ] **Step 4: 运行编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/server/...`

Expected: 编译通过

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test -tags=test ./internal/swarm/server/... -count=1`

Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/agent_server.go internal/swarm/server/handle_envelope.go internal/swarm/server/runtime/agent_manager.go
git commit -m "refactor: 简化 handleUnary/handleStream 委托 AgentManager + 回填 cancelAllInflightWork（10.3.12）"
```

---

### Task 7: 回填 handle_initialize.go + injectACPCapabilities

**Files:**
- Modify: `internal/swarm/server/handle_initialize.go`
- Modify: `internal/swarm/server/handle_envelope.go`

- [ ] **Step 1: 更新 handleInitialize 调用 AgentManager.Initialize**

替换 `handleInitialize` 方法为：

```go
func (s *AgentServer) handleInitialize(ctx context.Context, request *schema.AgentRequest) (*schema.AgentResponse, error) {
	// 1. 构建 extra_config（对齐 Python agent_ws_server.py L563-572）
	extraConfig := make(map[string]any)
	extraConfig["protocol_version"] = protocolVersion

	// 从 params 解析 clientCapabilities
	if len(request.Params) > 0 {
		var params map[string]any
		if err := json.Unmarshal(request.Params, &params); err == nil {
			if caps, ok := params["clientCapabilities"]; ok {
				extraConfig["client_capabilities"] = caps
			}
		}
	}

	// 2. 调用 AgentManager.Initialize
	caps, err := s.agentManager.Initialize(ctx, request.ChannelID, extraConfig)
	if err != nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"capabilities":     map[string]any{},
				"protocol_version": protocolVersion,
			}),
		), nil
	}

	// 3. 非 ACP 通道返回 nil → fallback 到默认 capabilities
	if caps == nil {
		return schema.NewAgentResponse(request.RequestID, request.ChannelID,
			schema.WithPayload(map[string]any{
				"capabilities":     map[string]any{},
				"protocol_version": protocolVersion,
			}),
		), nil
	}

	// 4. ACP 通道返回 capabilities（⤵️ 等 ACP 实现）
	caps["protocol_version"] = protocolVersion
	return schema.NewAgentResponse(request.RequestID, request.ChannelID,
		schema.WithPayload(caps),
	), nil
}
```

注意：需要添加 `encoding/json` import。

- [ ] **Step 2: 回填 injectACPCapabilities 的 fallback**

替换 `injectACPCapabilities` 方法末尾的 `⤵️ 10.3.12` 注释为：

```go
// fallback 到 agent_manager.get_client_capabilities("acp")
if s.agentManager != nil {
	if caps := s.agentManager.GetClientCapabilities("acp"); len(caps) > 0 {
		request.Metadata["client_capabilities"] = caps
	}
}
```

- [ ] **Step 3: 运行编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/server/...`

Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/handle_initialize.go internal/swarm/server/handle_envelope.go
git commit -m "refactor: 回填 handleInitialize + injectACPCapabilities（10.3.12）"
```

---

### Task 8: 更新其他调用点签名

**Files:**
- Modify: `internal/swarm/server/handle_command.go`（GetAgent 签名加 ctx）
- Modify: `internal/swarm/server/handle_envelope.go`（handleCancel 中 GetAgent 签名加 ctx）

- [ ] **Step 1: 更新 handle_command.go 中 GetAgent 调用**

将所有 `s.agentManager.GetAgent(channelID, mode, projectDir, subMode)` 调用改为 `s.agentManager.GetAgent(ctx, channelID, mode, projectDir, subMode)`。

- [ ] **Step 2: 更新 handle_envelope.go:handleCancel 中 GetAgent 调用**

将 `s.agentManager.GetAgent(request.ChannelID, mode, projectDir, subMode)` 改为 `s.agentManager.GetAgent(ctx, request.ChannelID, mode, projectDir, subMode)`。

- [ ] **Step 3: 更新 agent_server.go:Stop 中 Cleanup 调用（如签名有变化）**

确认 Cleanup 签名未变化（仍为 `Cleanup() error`），无需修改。

- [ ] **Step 4: 运行全量编译 + 测试**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go build ./internal/swarm/...`

Expected: 编译通过

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test -tags=test ./internal/swarm/server/... -count=1`

Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/swarm/server/handle_command.go internal/swarm/server/handle_envelope.go
git commit -m "refactor: 更新 GetAgent 签名调用（加 ctx）（10.3.12）"
```

---

### Task 9: 更新 doc.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/swarm/server/runtime/doc.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 doc.go 中 agent_manager.go 的描述**

将 `├── agent_manager.go      # AgentManager Agent 实例管理器（stub，10.3.12）` 改为 `├── agent_manager.go      # AgentManager Agent 实例管理器（10.3.12）`

- [ ] **Step 2: 更新 IMPLEMENTATION_PLAN.md 中 10.3.12 状态**

将 `| 10.3.12 | 🔄 | AgentManager | 多实例管理（按通道/模式），当前为 stub |` 改为 `| 10.3.12 | ✅ | AgentManager | 多实例管理（按通道/模式） |`

- [ ] **Step 3: 提交**

```bash
git add internal/swarm/server/runtime/doc.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 doc.go 和 IMPLEMENTATION_PLAN.md（10.3.12 完成）"
```

---

### Task 10: 全量测试验证

**Files:**
- 无新增修改

- [ ] **Step 1: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test -tags=test -cover ./internal/swarm/... -count=1`

Expected: 所有测试通过，覆盖率 ≥ 85%

- [ ] **Step 2: 检查覆盖率报告**

Run: `cd /home/opensource/uapclaw-gateway && GOPROXY=https://goproxy.cn,direct go test -tags=test -coverprofile=coverage.out ./internal/swarm/server/runtime/... && go tool cover -func=coverage.out | grep agent_manager`

Expected: agent_manager.go 覆盖率 ≥ 85%

- [ ] **Step 3: 最终提交（如有修复）**

```bash
git add -A
git commit -m "test: 补充测试覆盖（10.3.12）"
```
