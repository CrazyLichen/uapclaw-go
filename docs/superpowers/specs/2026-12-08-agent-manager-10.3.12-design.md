# 10.3.12 AgentManager 多实例实现设计

## 概述

将 AgentManager 从 stub 升级为完整的多实例管理器，支持按 channel/mode/projectDir 区分和管理多个 UapClaw 实例，1:1 对齐 Python `jiuwenswarm/server/runtime/agent_manager.py`。

## 在 Agent 会话中的流程位置与作用

```
请求流入路径：

Gateway → E2A协议 → AgentServer.handleEnvelope
  ├─ handleUnary → AgentManager.ProcessMessage(ctx, request)
  │                  ├─ 解析 channel_id / mode / workspace_dir
  │                  ├─ GetAgent() → 查找或自动创建 UapClaw
  │                  └─ agent.ProcessMessage(ctx, request)
  │
  ├─ handleStream → AgentManager.ProcessMessageStream(ctx, request)
  │                   └─ 同上但流式
  │
  └─ handleCancel → AgentManager.GetAgentNoWait() → agent.ProcessInterrupt()
```

AgentManager 是所有请求到达 UapClaw 之前的**实例路由层**，职责：
1. 按 (channelID, mode, subMode, projectDir) 四元组查找或创建 Agent 实例
2. 管理实例生命周期（创建、重建、清理、配置重载）
3. 统一请求入口（ProcessMessage / ProcessMessageStream）

## 核心数据结构

### Python 对照

```python
# Python: 二级嵌套 map
self.agents: dict[str, dict[str, "JiuWenClaw"]] = {}
#                 channel_key     cache_key
#                 "default"       "agent:plan:/home/user/proj"

self._agent_create_params: dict[str, dict[str, dict[str, Any]]] = {}
self._client_capabilities_by_channel: dict[str, dict[str, Any]] = {}
self._latest_env_overrides: dict[str, Any] = {}
```

### Go 结构体

```go
// AgentManager Agent 实例管理器（10.3.12）。
// 管理 UapClaw 实例的创建、获取和配置重载。
// 对齐 Python: jiuwenswarm/server/runtime/agent_manager.py
type AgentManager struct {
    // agents 存储: channel_key → cache_key → agentEntry
    agents map[string]map[string]*agentEntry

    // agentCreateParams 记录创建参数: channel_key → cache_key → agentCreateParamsEntry
    agentCreateParams map[string]map[string]*agentCreateParamsEntry

    // clientCapabilitiesByChannel ACP 客户端能力: channel_key → capabilities
    // ⤵️ ACP 章节：等 ACP 实现后 initialize() 写入数据
    clientCapabilitiesByChannel map[string]map[string]any

    // latestEnvOverrides 最近一次 env 覆盖
    latestEnvOverrides map[string]any

    // mu 保护并发访问
    mu sync.RWMutex
}

// agentEntry Agent 实例条目，记录元数据。
// 对齐 Python: setattr(agent, "_jiuwenswarm_agent_cache_key/mode/sub_mode/project_dir", ...)
type agentEntry struct {
    agent      *UapClaw
    cacheKey   string
    mode       string
    subMode    string
    projectDir string
}

// agentCreateParamsEntry 创建参数记录，用于 recreate_agent 重建。
// 对齐 Python: self._agent_create_params[channel][cache_key] = {mode, sub_mode, config, cache_key}
type agentCreateParamsEntry struct {
    mode     string
    subMode  string
    config   map[string]any
    cacheKey string
}
```

## 已确认决策

| 决策项 | 结论 |
|--------|------|
| agent 元数据存储 | 方案 B：agentEntry 包装，UapClaw 不感知 AgentManager 元数据 |
| createParams 存放 | 方案 1：独立 map，和 Python 一致 |
| create_session | 对齐 Python 简化：纯字符串逻辑，目录创建归 SessionManager |
| ACP 分支 | ⤵️ 延后，标注等待 ACP 具体章节回填 |
| process_message/stream | 加上，1:1 对齐 Python，AgentServer 中重复编排逻辑删除 |
| team evolution | ⤵️ 延后，标记 `TODO(⤵️ 10.3.2 Team)` |
| GetAgentNoWait | 完整实现：精确查找 → 字段过滤遍历 → fallback |
| GetClientCapabilities | 本次加上，数据为空，注释标记 ACP 回填点 |
| normalize 辅助函数 | 一并实现：4 个 normalize + makeAgentCacheKey |
| RecreateAgent | 完整实现：备份 → cleanup+删除 → 按 immediate 决定是否重建 |
| CancelAllInflightWork | 完整实现 + 回填 agent_server.go |
| Cleanup | 完整实现：遍历 cleanup + 清空所有 map |
| ReloadAgentsConfig | 实现 agent reload 部分，team evolution 标 ⤵️ |
| handle_initialize.go | 一并回填：调 Initialize + fallback，ACP 标 ⤵️ |
| injectACPCapabilities | 回填：GetClientCapabilities fallback，ACP 实现后自动生效 |

## 方法实现清单

### 导出方法

| 方法 | 行为 | 对齐 Python |
|------|------|-------------|
| `NewAgentManager()` | 初始化空 maps（不再创建 stubAgent） | `__init__` |
| `GetAgent(ctx, channelID, mode, projectDir, subMode)` | normalize → cacheKey → 精确查找 → 未命中 createAgent | `get_agent` |
| `GetAgentNoWait(channelID, mode, projectDir, subMode)` | normalize → 精确查找 → 字段过滤 → fallback | `get_agent_nowait` |
| `Initialize(ctx, channelID, extraConfig)` | ACP ⤵️；非 ACP 返回 nil | `initialize` |
| `CreateSession(channelID, sessionID)` | 显式ID→返回；ACP ⤵️；其他→"default" | `create_session` |
| `ReloadAgentsConfig(ctx, config, env)` | env注入 + 遍历ReloadAgentConfig + team ⤵️ | `reload_agents_config` |
| `RecreateAgent(ctx, channelID, immediate)` | 备份 → cleanup+删除 → 按immediate重建 | `recreate_agent` |
| `CancelAllInflightWork(ctx)` | 遍历调 CancelInflightWork | `cancel_all_inflight_work` |
| `GetClientCapabilities(channelID)` | 从 map 读取，当前为空 | `get_client_capabilities` |
| `ProcessMessage(ctx, request)` | 解析channel/mode/workspace → GetAgent → agent.ProcessMessage | `process_message` |
| `ProcessMessageStream(ctx, request)` | 同上但流式 | `process_message_stream` |
| `Cleanup()` | 遍历 Cleanup → 清空 maps | `cleanup` |

### 非导出方法

| 方法 | 行为 | 对齐 Python |
|------|------|-------------|
| `createAgent(ctx, channelKey, mode, config, subMode, cacheKey)` | 注入env → NewUapClaw → CreateInstance → 写入maps | `_create_agent` |
| `normalizeChannelID(channelID)` | None→"default"，strip | `_normalize_channel_id` |
| `normalizeMode(mode)` | None→"agent"，strip | `_normalize_mode` |
| `normalizeSubMode(subMode)` | None→""，strip | `_normalize_sub_mode` |
| `normalizeProjectDir(projectDir)` | None→""，strip+abspath+expanduser | `_normalize_project_dir` |
| `makeAgentCacheKey(mode, subMode, projectDir)` | `{mode}:{subMode}:{projectDir}` | `_make_agent_cache_key` |

## cacheKey 规则

```
cacheKey = normalizeMode(mode) + ":" + normalizeSubMode(subMode) + ":" + normalizeProjectDir(projectDir)

示例：
- "agent:plan:/home/user/project"
- "code:normal:/home/user/project"
- "code:team:"
- "agent:plan:"
```

两层 map 结构示例：

```
agents = {
    "default": {
        "agent:plan:/home/user/project":     agentEntry{agent1, ...},
        "code:normal:/home/user/project":    agentEntry{agent2, ...},
    },
    "acp": {
        "code::":                            agentEntry{agent3, ...},
    },
    "web": {
        "agent:plan:/home/other/project":    agentEntry{agent4, ...},
    }
}
```

## GetAgentNoWait 模糊查找逻辑

1. 按 cacheKey 精确查找 → 命中则返回
2. 精确未命中时，按 mode/subMode/projectDir 逐字段过滤遍历 channel 下所有 entry
3. 如果三个参数都为空（mode==nil && projectDir==nil && subMode==nil）：
   - 优先返回 mode="agent" 的第一个
   - 否则返回任意第一个
4. 全部未找到 → 返回 nil

## 回填点清单

| 文件 | 回填内容 |
|------|---------|
| `agent_server.go:cancelAllInflightWork` | 调 `s.agentManager.CancelAllInflightWork(ctx)` |
| `agent_server.go:handleUnary` | 改调 `s.agentManager.ProcessMessage(ctx, request)`，删除手动编排 |
| `agent_server.go:handleStream` | 改调 `s.agentManager.ProcessMessageStream(ctx, request)`，删除手动编排 |
| `handle_initialize.go:handleInitialize` | 调 `s.agentManager.Initialize` + fallback，ACP 标 ⤵️ |
| `handle_envelope.go:injectACPCapabilities` | fallback 调 `s.agentManager.GetClientCapabilities("acp")` |

## 延后项（⤵️ 标记）

| 延后项 | 标注 | 等待章节 |
|--------|------|---------|
| Initialize ACP 分支 | `TODO(⤵️ ACP): ACP 通道创建 code agent + 返回 ACP_DEFAULT_CAPABILITIES` | ACP 章节 |
| GetClientCapabilities 数据写入 | `// ⤵️ ACP 章节：等 ACP 实现后 initialize() 写入 clientCapabilitiesByChannel` | ACP 章节 |
| CreateSession ACP 分支 | `// ⤵️ ACP: ACP 通道生成 acp_{uuid[:8]}` | ACP 章节 |
| ReloadAgentsConfig team evolution | `TODO(⤵️ 10.3.2 Team): 更新 team evolution config` | 10.3.2 Team |
| createAgent ACP config | `// ⤵️ ACP: channel_key=="acp" 时合并 _build_acp_agent_config` | ACP 章节 |

## 测试计划

### 基础方法

- `TestNewAgentManager`：初始化空 maps，不再有 stubAgent
- `TestNormalizeChannelID`：空→"default"，已有值→strip
- `TestNormalizeMode`：空→"agent"，已有值→strip
- `TestNormalizeSubMode`：空→""
- `TestNormalizeProjectDir`：空→""，相对路径→绝对路径，`~`→expand
- `TestMakeAgentCacheKey`：各组合拼接正确

### 核心方法

- `TestAgentManager_GetAgent`：缓存命中、未命中自动创建
- `TestAgentManager_GetAgent_不同mode创建不同实例`：agent mode 和 code mode 共存
- `TestAgentManager_GetAgentNoWait`：精确命中、字段过滤、fallback、全空返回第一个
- `TestAgentManager_GetAgentNoWait_未找到返回nil`：空 channel
- `TestAgentManager_Initialize`：非 ACP 返回 nil
- `TestAgentManager_CreateSession`：显式 ID、空 ID 返回 "default"

### 生命周期方法

- `TestAgentManager_ReloadAgentsConfig`：env 注入 + agent reload
- `TestAgentManager_ReloadAgentsConfig_环境变量注入`：已有测试保留
- `TestAgentManager_RecreateAgent`：immediate=true 立即重建、immediate=false 不重建、空 channel 返回空
- `TestAgentManager_CancelAllInflightWork`：遍历调用
- `TestAgentManager_Cleanup`：遍历 + 清空 maps

### ProcessMessage/Stream

- `TestAgentManager_ProcessMessage`：正确委托到 agent
- `TestAgentManager_ProcessMessageStream`：正确委托到 agent 流式

### GetClientCapabilities

- `TestAgentManager_GetClientCapabilities`：空 map 返回

## 对应 Python 代码

`jiuwenswarm/server/runtime/agent_manager.py`
