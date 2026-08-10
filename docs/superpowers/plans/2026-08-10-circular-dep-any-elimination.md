# 9.65-1 循环依赖重构：消除 `any`，恢复类型安全

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 通过搬移配置结构体到 schema 包打断循环依赖链，逐步消除 Messager 接口和 tools 包中的 `any` 妥协，恢复类型安全。

**Architecture:** 分 4 步重构——Step 1 搬配置打断循环链；Step 2 Messager 接口改回 schema.EventMessage + 删除 SenderIDStamper；Step 3 tools 包清理（删除 events.go 重复常量、删除 sessionID 字段）；Step 4 MessageID 改用 UUID v4。

**Tech Stack:** Go 1.22+, github.com/google/uuid

**设计文档:** `docs/superpowers/specs/2026-08-10-circular-dep-any-elimination-design.md`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 新增 | `internal/agent_teams/schema/transport_config.go` | 搬入 MessagerTransportConfig + MessagerPeerConfig |
| 新增 | `internal/agent_teams/schema/memory_config.go` | 搬入 TeamMemoryConfig |
| 新增 | `internal/agent_teams/schema/session_context.go` | 搬入 SessionState + context 函数 |
| 修改 | `internal/agent_teams/messager/base.go` | 删除配置结构体，改用 schema |
| 修改 | `internal/agent_teams/messager/inprocess.go` | config 字段类型 + 删除 SenderIDStamper |
| 修改 | `internal/agent_teams/messager/messager.go` | any → schema.EventMessage |
| 修改 | `internal/agent_teams/messager/inprocess_test.go` | 删除 testEventMessage |
| 修改 | `internal/agent_teams/messager/base_test.go` | 更新 import |
| 修改 | `internal/agent_teams/messager/doc.go` | 更新文件目录 |
| 修改 | `internal/agent_teams/memory/config.go` | 删除 TeamMemoryConfig，改用 schema |
| 修改 | `internal/agent_teams/memory/config_test.go` | 更新 import |
| 修改 | `internal/agent_teams/context.go` | 委托到 schema |
| 修改 | `internal/agent_teams/context_test.go` | 更新 |
| 修改 | `internal/agent_teams/schema/blueprint.go` | 删除 memory/messager import |
| 修改 | `internal/agent_teams/schema/team.go` | 删除 messager import |
| 修改 | `internal/agent_teams/schema/blueprint_test.go` | 删除 messager import |
| 修改 | `internal/agent_teams/schema/events.go` | 删除 GetSenderID/SetSenderID |
| 修改 | `internal/agent_teams/schema/doc.go` | 更新文件目录 |
| 修改 | `internal/agent_teams/agent/agent_configurator.go` | 更新 import |
| 修改 | `internal/agent_teams/agent/payload.go` | 更新 import |
| 修改 | `internal/agent_teams/agent/stream_controller.go` | schema.GetSessionID |
| 修改 | `internal/agent_teams/agent/team_agent.go` | schema.GetSessionID |
| 删除 | `internal/agent_teams/tools/events.go` | 重复常量，改用 schema |
| 修改 | `internal/agent_teams/tools/task_manager.go` | TypedEvent + 删除 sessionID |
| 修改 | `internal/agent_teams/tools/message_manager.go` | 删除 sessionID + UUID v4 |
| 修改 | `internal/agent_teams/tools/message_manager_test.go` | 更新 |
| 修改 | `internal/agent_teams/tools/task_manager_test.go` | 更新 |
| 修改 | `internal/agent_teams/tools/doc.go` | 更新文件目录 |
| 修改 | `internal/agent_teams/agent/infra.go` | 删除 sessionID 参数 |

---

## Task 1: Step 1 — 搬 MessagerTransportConfig 到 schema

**Files:**
- Create: `internal/agent_teams/schema/transport_config.go`
- Modify: `internal/agent_teams/messager/base.go`
- Modify: `internal/agent_teams/messager/inprocess.go`
- Modify: `internal/agent_teams/messager/base_test.go`
- Modify: `internal/agent_teams/schema/blueprint.go`
- Modify: `internal/agent_teams/schema/team.go`
- Modify: `internal/agent_teams/schema/blueprint_test.go`
- Modify: `internal/agent_teams/agent/payload.go`
- Modify: `internal/agent_teams/messager/doc.go`

- [ ] **Step 1.1: 创建 `schema/transport_config.go`**

从 `messager/base.go` 搬入以下内容，保持字段和注释不变：

```go
package schema

import "fmt"

// ──────────────────────────── 结构体 ────────────────────────────

// MessagerPeerConfig 消息通信对等节点配置。
// 从 messager 包搬入，打断 schema→messager 循环依赖。
type MessagerPeerConfig struct {
	// AgentID Agent 标识
	AgentID string `json:"agent_id"`
	// PeerID 对等节点标识
	PeerID string `json:"peer_id,omitempty"`
	// Addrs 地址列表
	Addrs []string `json:"addrs,omitempty"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// MessagerTransportConfig 消息通信传输配置。
// 从 messager 包搬入，打断 schema→messager 循环依赖。
type MessagerTransportConfig struct {
	// Backend 通信后端，"inprocess" | "pyzmq" 等
	Backend string `json:"backend"`
	// TeamName 团队名称
	TeamName string `json:"team_name"`
	// NodeID 节点标识
	NodeID string `json:"node_id,omitempty"`
	// DirectAddr 直连地址
	DirectAddr string `json:"direct_addr,omitempty"`
	// PubsubPublishAddr 发布订阅发布地址
	PubsubPublishAddr string `json:"pubsub_publish_addr,omitempty"`
	// PubsubSubscribeAddr 发布订阅订阅地址
	PubsubSubscribeAddr string `json:"pubsub_subscribe_addr,omitempty"`
	// ListenAddrs 监听地址列表
	ListenAddrs []string `json:"listen_addrs,omitempty"`
	// BootstrapPeers 引导对等节点列表
	BootstrapPeers []MessagerPeerConfig `json:"bootstrap_peers,omitempty"`
	// KnownPeers 已知对等节点列表
	KnownPeers []MessagerPeerConfig `json:"known_peers,omitempty"`
	// RequestTimeout 请求超时秒数
	RequestTimeout float64 `json:"request_timeout"`
	// Metadata 元数据
	Metadata map[string]any `json:"metadata,omitempty"`
}

// SubscriptionHandle 订阅句柄。
type SubscriptionHandle struct {
	// SubscriptionID 订阅标识
	SubscriptionID string `json:"subscription_id"`
	// Topic 订阅主题
	Topic string `json:"topic"`
	// AgentID Agent 标识
	AgentID string `json:"agent_id,omitempty"`
	// BackendMetadata 后端元数据
	BackendMetadata map[string]any `json:"backend_metadata,omitempty"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMessagerTransportConfig 创建默认消息通信传输配置。
// 默认值：backend="inprocess", team_name="default", request_timeout=10.0
func NewMessagerTransportConfig() MessagerTransportConfig {
	return MessagerTransportConfig{
		Backend:        "inprocess",
		TeamName:       "default",
		RequestTimeout: 10.0,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// BroadcastTopic 返回广播主题名称，格式为 "team:{team_name}:broadcast"。
func (c MessagerTransportConfig) BroadcastTopic() string {
	return fmt.Sprintf("team:%s:broadcast", c.TeamName)
}
```

- [ ] **Step 1.2: 修改 `messager/base.go` — 删除配置结构体，改用 schema**

删除 `MessagerPeerConfig`、`MessagerTransportConfig`、`SubscriptionHandle`、`NewMessagerTransportConfig()`、`BroadcastTopic()` 定义。添加 `schema` import。改为：

```go
package messager

import (
	"fmt"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CreateMessager 根据 config 构建 Messager 实例。
// 对齐 Python: create_messager(config) (openjiuwen/agent_teams/messager/base.py)
func CreateMessager(config schema.MessagerTransportConfig) (Messager, error) {
	switch config.Backend {
	case "inprocess":
		return NewInProcessMessager(config), nil
	// ⤵️ 9.65-2: pyzmq 后端
	default:
		return nil, fmt.Errorf("unsupported messager backend: %s", config.Backend)
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 1.3: 修改 `messager/inprocess.go` — config 字段类型改为 schema**

修改 import 和字段类型：

```go
import (
	"context"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)
```

`InProcessMessager` 结构体中：
- `config MessagerTransportConfig` → `config schema.MessagerTransportConfig`

`NewInProcessMessager` 签名：
- `func NewInProcessMessager(config MessagerTransportConfig)` → `func NewInProcessMessager(config schema.MessagerTransportConfig)`

- [ ] **Step 1.4: 修改 `messager/base_test.go` — 更新 import**

添加 `schema` import，将所有 `NewMessagerTransportConfig()` 改为 `schema.NewMessagerTransportConfig()`，`MessagerPeerConfig{}` 改为 `schema.MessagerPeerConfig{}`，`SubscriptionHandle{}` 改为 `schema.SubscriptionHandle{}`。

- [ ] **Step 1.5: 修改 `schema/blueprint.go` — 删除 messager import**

删除 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"` import。

将所有 `messager.MessagerTransportConfig` 替换为 `MessagerTransportConfig`（同包）。
将所有 `messager.NewMessagerTransportConfig()` 替换为 `NewMessagerTransportConfig()`。

- [ ] **Step 1.6: 修改 `schema/team.go` — 删除 messager import**

删除 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"` import。

将 `*messager.MessagerTransportConfig` 替换为 `*MessagerTransportConfig`。

- [ ] **Step 1.7: 修改 `schema/blueprint_test.go` — 删除 messager import**

删除 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"` import。

将所有 `messager.MessagerTransportConfig` 替换为 `schema.MessagerTransportConfig`（如果测试文件 import 了 schema 别名）或直接 `MessagerTransportConfig`（如果同包测试）。
将所有 `messager.NewMessagerTransportConfig()` 替换为 `schema.NewMessagerTransportConfig()` 或 `NewMessagerTransportConfig()`。

- [ ] **Step 1.8: 修改 `agent/payload.go` — 更新 import**

将 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/messager"` import 替换为 `atschema "github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"`（已有此别名）。

将 `*messager.MessagerTransportConfig` 替换为 `*atschema.MessagerTransportConfig`。

- [ ] **Step 1.9: 更新 `messager/doc.go`**

更新文件目录，标注配置结构体已迁移到 schema 包。

- [ ] **Step 1.10: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 1.11: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 1.12: 提交**

```bash
git add -A && git commit -m "refactor: 搬入 MessagerTransportConfig 到 schema 包，打断 schema→messager 循环依赖"
```

---

## Task 2: Step 1 — 搬 TeamMemoryConfig 到 schema

**Files:**
- Create: `internal/agent_teams/schema/memory_config.go`
- Modify: `internal/agent_teams/memory/config.go`
- Modify: `internal/agent_teams/memory/config_test.go`
- Modify: `internal/agent_teams/schema/blueprint.go`
- Modify: `internal/agent_teams/agent/agent_configurator.go`

- [ ] **Step 2.1: 创建 `schema/memory_config.go`**

从 `memory/config.go` 搬入 `TeamMemoryConfig` 和 `NewTeamMemoryConfig()`：

```go
package schema

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryConfig 团队记忆配置，对齐 Python TeamMemoryConfig。
// 从 memory 包搬入，打断 schema→memory 循环依赖。
type TeamMemoryConfig struct {
	// Enabled 是否启用团队记忆
	Enabled bool `json:"enabled"`
	// Scenario 记忆场景，"general" | "coding"
	Scenario string `json:"scenario"`
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig `json:"-"`
	// AutoExtract 是否自动提取记忆
	AutoExtract bool `json:"auto_extract"`
	// SharedMemory 是否启用共享记忆
	SharedMemory bool `json:"shared_memory"`
	// MemberMemoryPromptMode 成员记忆提示模式
	MemberMemoryPromptMode string `json:"member_memory_prompt_mode"`
	// TimezoneOffsetHours 时区偏移小时数
	TimezoneOffsetHours float64 `json:"timezone_offset_hours"`
	// ParentWorkspacePath 父工作空间路径，不序列化
	ParentWorkspacePath string `json:"-"`
	// TeamMemoryDir 团队记忆目录，不序列化
	TeamMemoryDir string `json:"-"`
}

// ──────────────────────────── 枚举────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMemoryConfig 创建默认团队记忆配置。
// 默认值：enabled=false, scenario="general", auto_extract=true,
// 对齐 Python: shared_memory=true, member_memory_prompt_mode="proactive", timezone_offset_hours=8.0
func NewTeamMemoryConfig() TeamMemoryConfig {
	return TeamMemoryConfig{
		Enabled:                false,
		Scenario:               "general",
		AutoExtract:            true,
		SharedMemory:           true,
		MemberMemoryPromptMode: "proactive",
		TimezoneOffsetHours:    8.0,
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2.2: 修改 `memory/config.go` — 删除 TeamMemoryConfig，保留 ResolveEmbeddingConfig**

删除 `TeamMemoryConfig` 结构体定义和 `NewTeamMemoryConfig()` 函数。添加 `schema` import。`ResolveEmbeddingConfig` 参数改为 `*schema.TeamMemoryConfig`：

```go
package memory

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveEmbeddingConfig 解析嵌入配置。
// 优先级：config 内嵌配置 → 环境变量 → nil
func ResolveEmbeddingConfig(cfg *schema.TeamMemoryConfig) *embedding.EmbeddingConfig {
	if cfg != nil && cfg.EmbeddingConfig != nil {
		return cfg.EmbeddingConfig
	}
	return lite.ResolveEmbeddingConfigFromEnv("", "", "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2.3: 修改 `memory/config_test.go` — 更新 import**

添加 `schema` import。将 `NewTeamMemoryConfig()` 替换为 `schema.NewTeamMemoryConfig()`。将 `ResolveEmbeddingConfig(&cfg)` 中的 `cfg` 类型对应更新。

- [ ] **Step 2.4: 修改 `schema/blueprint.go` — 删除 memory import**

删除 `"github.com/uapclaw/uapclaw-go/internal/agent_teams/memory"` import。

将 `*memory.TeamMemoryConfig` 替换为 `*TeamMemoryConfig`（同包）。

- [ ] **Step 2.5: 修改 `agent/agent_configurator.go` — 更新 import**

将 `memory.NewTeamMemoryConfig()` 替换为 `atschema.NewTeamMemoryConfig()`（已有 `atschema` 别名）。
将 `memory.ResolveEmbeddingConfig(&memCfg)` 替换为 `memory.ResolveEmbeddingConfig(&memCfg)`（不变，参数类型已改为 `*schema.TeamMemoryConfig`）。
如果 `memory` import 不再被其他地方使用，可以删除。

- [ ] **Step 2.6: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 2.7: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 2.8: 提交**

```bash
git add -A && git commit -m "refactor: 搬入 TeamMemoryConfig 到 schema 包，打断 schema→memory 循环依赖"
```

---

## Task 3: Step 1 — 搬 SessionState + context 函数到 schema

**Files:**
- Create: `internal/agent_teams/schema/session_context.go`
- Modify: `internal/agent_teams/context.go`
- Modify: `internal/agent_teams/context_test.go`
- Modify: `internal/agent_teams/agent/stream_controller.go`
- Modify: `internal/agent_teams/agent/team_agent.go`

- [ ] **Step 3.1: 创建 `schema/session_context.go`**

从 `agent_teams/context.go` 搬入全部内容：

```go
package schema

import (
	"context"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionState 每-TeamAgent 的可变 session 状态容器。
// 对齐 Python: _session_id_context (contextvars.ContextVar)
//
// 通过 context.Value 传播 *SessionState 指针：
//   - 同一 TeamAgent 内的 goroutine 共享同一 SessionState 引用，SetSessionID 后立即可见
//   - 子 Teammate 调用 InitSessionState 创建新实例 + WithSessionState 派生新 ctx，父不受影响
//
// 并发安全：所有字段读写通过 sync.RWMutex 保护。
// Python 不需要锁因为 asyncio 是单线程协程。
// 从 agent_teams 根包搬入，使 tools 包可通过 schema.GetSessionID(ctx) 获取 sessionID。
type SessionState struct {
	mu        sync.RWMutex
	sessionID string
}

// sessionStateKeyType SessionState 的 context key 类型。
type sessionStateKeyType struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSessionState 创建新的 SessionState 实例。
// 对齐 Python: _session_id_context = ContextVar("session_id", default=None)
func InitSessionState() *SessionState {
	return &SessionState{}
}

// WithSessionState 将 SessionState 注入 context。
// 对齐 Python: set_session_id(session_id) — 但 Go 通过 context.Value 传播指针
func WithSessionState(ctx context.Context, state *SessionState) context.Context {
	return context.WithValue(ctx, sessionStateKeyType{}, state)
}

// SessionStateFromCtx 从 context 中获取 SessionState。
// 返回 nil 表示当前 context 未绑定 SessionState。
func SessionStateFromCtx(ctx context.Context) *SessionState {
	if s, ok := ctx.Value(sessionStateKeyType{}).(*SessionState); ok {
		return s
	}
	return nil
}

// GetSessionID 从 context 中获取当前 session_id。
// 对齐 Python: get_session_id() -> Optional[str]
// 读取优先级：SessionState.sessionID → ""（空字符串）
func GetSessionID(ctx context.Context) string {
	if s := SessionStateFromCtx(ctx); s != nil {
		return s.GetSessionID()
	}
	return ""
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// GetSessionID 获取当前 session_id。
func (s *SessionState) GetSessionID() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.sessionID
}

// SetSessionID 设置当前 session_id。
// 对齐 Python: set_session_id(session_id) -> Token
// Go 不需要 Token，直接原地修改，同一指针的 goroutine 立即可见。
func (s *SessionState) SetSessionID(sessionID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sessionID = sessionID
}
```

- [ ] **Step 3.2: 修改 `agent_teams/context.go` — 委托到 schema**

将 `context.go` 改为从 schema 包导出，保持 agent_teams 根包的 API 兼容：

```go
package agent_teams

import (
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionState 每-TeamAgent 的可变 session 状态容器。
// 已迁移到 schema 包，此处保留类型别名以兼容现有调用方。
type SessionState = schema.SessionState

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// InitSessionState 创建新的 SessionState 实例。
// 委托到 schema.InitSessionState()。
func InitSessionState() *SessionState {
	return schema.InitSessionState()
}

// WithSessionState 将 SessionState 注入 context。
// 委托到 schema.WithSessionState()。
func WithSessionState(ctx context.Context, state *SessionState) context.Context {
	return schema.WithSessionState(ctx, state)
}

// SessionStateFromCtx 从 context 中获取 SessionState。
// 委托到 schema.SessionStateFromCtx()。
func SessionStateFromCtx(ctx context.Context) *SessionState {
	return schema.SessionStateFromCtx(ctx)
}

// GetSessionID 从 context 中获取当前 session_id。
// 委托到 schema.GetSessionID()。
func GetSessionID(ctx context.Context) string {
	return schema.GetSessionID(ctx)
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

注意：`context.go` 需要保留 `"context"` import。

- [ ] **Step 3.3: 修改 `context_test.go` — 无需改动**

`context_test.go` 在 `agent_teams` 包内测试，通过类型别名和委托函数仍可正常工作。无需改动。

- [ ] **Step 3.4: 修改 `agent/stream_controller.go` — 无需改动**

当前使用 `agentteams.GetSessionID(ctx)`，agent_teams 根包的委托函数仍可正常工作。无需改动。

- [ ] **Step 3.5: 修改 `agent/team_agent.go` — 无需改动**

当前使用 `agentteams.GetSessionID(ctx)`，agent_teams 根包的委托函数仍可正常工作。无需改动。

- [ ] **Step 3.6: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 3.7: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 3.8: 提交**

```bash
git add -A && git commit -m "refactor: 搬入 SessionState + context 函数到 schema 包，使 tools 包可调用 GetSessionID"
```

---

## Task 4: Step 1 — 更新 schema/doc.go

**Files:**
- Modify: `internal/agent_teams/schema/doc.go`

- [ ] **Step 4.1: 更新文件目录**

在文件目录中添加新文件：

```
//	schema/             # Schema 类型定义
//	├── doc.go               # 包文档
//	├── team.go              # TeamRole/TeamSpec/TeamRuntimeContext 等团队级类型
//	├── deep_agent_spec.go   # DeepAgentSpec/SubAgentSpec 等单角色规格定义
//	├── blueprint.go         # TeamAgentSpec/LeaderSpec/TransportSpec 等团队规格与校验
//	├── status.go            # 成员/任务状态枚举与状态转换表
//	├── events.go            # 事件类型与事件消息 Schema
//	├── stream.go            # TeamOutputSchema 团队流式输出 Schema
//	├── task.go              # 任务视图响应类型
//	├── transport_config.go  # MessagerTransportConfig/MessagerPeerConfig 消息通信配置
//	├── memory_config.go     # TeamMemoryConfig 团队记忆配置
//	└── session_context.go   # SessionState/GetSessionID 会话上下文管理
```

- [ ] **Step 4.2: 提交**

```bash
git add -A && git commit -m "docs: 更新 schema/doc.go 文件目录"
```

---

## Task 5: Step 2 — Messager 接口改回 schema.EventMessage，删除 SenderIDStamper

**Files:**
- Modify: `internal/agent_teams/messager/messager.go`
- Modify: `internal/agent_teams/messager/inprocess.go`
- Modify: `internal/agent_teams/messager/inprocess_test.go`
- Modify: `internal/agent_teams/schema/events.go`

- [ ] **Step 5.1: 修改 `messager/messager.go` — any → schema.EventMessage**

添加 `schema` import，将 `any` 替换为 `schema.EventMessage`：

```go
package messager

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agent_teams/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// MessagerHandler 消息处理回调函数类型。
// 对齐 Python: MessagerHandler = Callable[[EventMessage], Awaitable[None]]
type MessagerHandler func(ctx context.Context, msg schema.EventMessage) error

// Messager 团队事件消息通信接口。
// 对齐 Python: Messager (openjiuwen/agent_teams/messager/messager.py)
type Messager interface {
	// Start 启动消息传输层
	Start(ctx context.Context) error
	// Stop 停止消息传输层
	Stop(ctx context.Context) error
	// Publish 向主题发布事件消息
	Publish(ctx context.Context, topicID string, message schema.EventMessage) error
	// Subscribe 订阅主题，注册回调
	Subscribe(ctx context.Context, topicID string, handler MessagerHandler) error
	// Unsubscribe 取消订阅
	Unsubscribe(ctx context.Context, topicID string) error
	// Send 点对点发送消息给指定 agent
	Send(ctx context.Context, agentID string, message schema.EventMessage) error
	// RegisterDirectMessageHandler 注册点对点消息回调
	RegisterDirectMessageHandler(ctx context.Context, handler MessagerHandler) error
	// UnregisterDirectMessageHandler 取消注册点对点消息回调
	UnregisterDirectMessageHandler(ctx context.Context) error
}
```

- [ ] **Step 5.2: 修改 `messager/inprocess.go` — 删除 SenderIDStamper，直接操作 SenderID**

删除 `SenderIDStamper` 接口定义和 `stampSenderID` 函数。修改 `Publish` 方法中直接操作 `message.SenderID`：

```go
// Publish 向主题发布事件消息。
// 自动设置 SenderID 过滤自发布（对齐 Python message.model_copy(update={"sender_id": self._agent_id})）。
func (m *InProcessMessager) Publish(ctx context.Context, topicID string, message schema.EventMessage) error {
	agentID := m.agentID()
	// Stamp SenderID：直接设置消息的 SenderID
	if message.SenderID == "" {
		message.SenderID = agentID
	}
	b := getBus()
	b.mu.Lock()
	defer b.mu.Unlock()
	subs, ok := b.topicSubs[topicID]
	if !ok {
		return nil
	}
	for aid, handler := range subs {
		if err := handler(ctx, message); err != nil {
			logger.Error(logger.ComponentAgentCore).Err(err).
				Str("agent_id", aid).Str("topic", topicID).
				Msg("InProcess Publish 处理失败")
		}
	}
	return nil
}
```

同样修改 `Send` 方法签名：
```go
func (m *InProcessMessager) Send(ctx context.Context, agentID string, message schema.EventMessage) error {
```

删除文件末尾的 `SenderIDStamper` 接口和 `stampSenderID` 函数。

- [ ] **Step 5.3: 修改 `messager/inprocess_test.go` — 删除 testEventMessage，改用 schema.EventMessage**

删除 `testEventMessage` 结构体定义。添加 `schema` import。所有测试用例改用 `schema.EventMessage`：

- `&testEventMessage{eventType: "task_created", payload: map[string]any{"team_name": "t1"}}` → `schema.NewEventMessage(schema.TeamEventTaskCreated, map[string]any{"team_name": "t1"}, "")`
- handler 中 `msg any` → `msg schema.EventMessage`
- SenderID 检查：`em.senderID` → `msg.SenderID`
- 删除 `TestInProcessMessager_Publish_NoSenderIDStamper` 测试（不再有非 SenderIDStamper 消息）
- `TestInProcessMessager_Publish_SenderIDStamper` 重命名为 `TestInProcessMessager_Publish_SenderID`

- [ ] **Step 5.4: 修改 `schema/events.go` — 删除 GetSenderID/SetSenderID 方法**

删除以下两行：
```go
func (m *EventMessage) GetSenderID() string { return m.SenderID }
func (m *EventMessage) SetSenderID(id string) { m.SenderID = id }
```

- [ ] **Step 5.5: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 5.6: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 5.7: 提交**

```bash
git add -A && git commit -m "refactor: Messager 接口改回 schema.EventMessage，删除 SenderIDStamper 鸭子类型"
```

---

## Task 6: Step 2 — 更新 tools 包 publishTaskEvent/publishMessageEvent 使用 schema.EventMessage

**Files:**
- Modify: `internal/agent_teams/tools/task_manager.go`
- Modify: `internal/agent_teams/tools/message_manager.go`

- [ ] **Step 6.1: 修改 `tools/task_manager.go` — publishTaskEvent 改用 schema.TypedEvent**

添加 `schema` import。修改 `publishTaskEvent` 签名和实现：

```go
// publishTaskEvent 发布任务事件到 TeamTopic。
func (tm *TeamTaskManager) publishTaskEvent(ctx context.Context, event schema.TypedEvent) {
	if tm.messager == nil {
		return
	}
	msg := schema.EventMessageFromEvent(event)
	msg.TeamName = tm.teamName
	topicID := schema.TeamTopicTask.Build(tm.sessionID, tm.teamName)
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("event_type", event.EventTypeName()).
			Msg("发布任务事件失败")
	}
}
```

修改所有调用 `publishTaskEvent` 的地方，将 `map[string]any` 载荷替换为对应的 `schema.TypedEvent` 结构体。例如：

```go
// 修改前
tm.publishTaskEvent(ctx, eventTaskCreated, map[string]any{
    "team_name": tm.teamName, "task_id": task.TaskID, "status": task.Status,
})

// 修改后
tm.publishTaskEvent(ctx, schema.TaskCreatedEvent{
    BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
    TaskID:           task.TaskID,
    Status:           task.Status,
})
```

注意：需要逐一检查 task_manager.go 中所有 `publishTaskEvent` 调用点（约 12 处），将 `eventXxx` 常量 + `map[string]any` 替换为对应的 `schema.TypedEvent` 结构体。

修改 `publishUnblockedEvents`：

```go
func (tm *TeamTaskManager) publishUnblockedEvents(ctx context.Context, unblockedIDs []string) {
	for _, id := range unblockedIDs {
		tm.publishTaskEvent(ctx, schema.TaskUnblockedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskID:           id,
		})
	}
}
```

修改 `maybePublishTaskListDrained`：

```go
func (tm *TeamTaskManager) maybePublishTaskListDrained(ctx context.Context) {
	tasks, err := tm.db.Task().GetTeamTasks(ctx, tm.teamName, "")
	if err != nil || len(tasks) == 0 {
		return
	}
	allTerminal := true
	for _, task := range tasks {
		if task.Status != fsm.TaskStatusCompleted && task.Status != fsm.TaskStatusCancelled {
			allTerminal = false
			break
		}
	}
	if allTerminal {
		tm.publishTaskEvent(ctx, schema.TaskListDrainedEvent{
			BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
			TaskCount:        len(tasks),
		})
	}
}
```

- [ ] **Step 6.2: 修改 `tools/message_manager.go` — publishMessageEvent 改用 schema.EventMessage**

添加 `schema` import。修改 `publishMessageEvent`：

```go
func (tm *TeamMessageManager) publishMessageEvent(ctx context.Context, event schema.TypedEvent) {
	if tm.messager == nil {
		return
	}
	msg := schema.EventMessageFromEvent(event)
	msg.TeamName = tm.teamName
	topicID := schema.TeamTopicMessage.Build(schema.GetSessionID(ctx), tm.teamName)
	if err := tm.messager.Publish(ctx, topicID, msg); err != nil {
		logger.Error(logger.ComponentAgentCore).Err(err).
			Str("event_type", event.EventTypeName()).
			Msg("发布消息事件失败")
	}
}
```

修改 `SendMessage` 中的事件发布：

```go
tm.publishMessageEvent(ctx, schema.MessageEvent{
    BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
    MessageID:        messageID,
    FromMemberName:   sender,
    ToMemberName:     toMemberName,
})
```

修改 `BroadcastMessage` 中的事件发布：

```go
tm.publishMessageEvent(ctx, schema.BroadcastEvent{
    BaseEventMessage: schema.BaseEventMessage{TeamName: tm.teamName},
    MessageID:        messageID,
    FromMemberName:   sender,
})
```

- [ ] **Step 6.3: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 6.4: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 6.5: 提交**

```bash
git add -A && git commit -m "refactor: tools 包 publishTaskEvent/publishMessageEvent 改用 schema.TypedEvent + EventMessageFromEvent"
```

---

## Task 7: Step 3 — 删除 tools/events.go + 删除 sessionID 字段

**Files:**
- Delete: `internal/agent_teams/tools/events.go`
- Modify: `internal/agent_teams/tools/task_manager.go`
- Modify: `internal/agent_teams/tools/message_manager.go`
- Modify: `internal/agent_teams/tools/task_manager_test.go`
- Modify: `internal/agent_teams/tools/message_manager_test.go`
- Modify: `internal/agent_teams/agent/infra.go`
- Modify: `internal/agent_teams/tools/doc.go`

- [ ] **Step 7.1: 删除 `tools/events.go`**

文件内容已不再使用（Step 6 已将所有调用改为 schema.TypedEvent）。

- [ ] **Step 7.2: 修改 `tools/task_manager.go` — 删除 sessionID 字段**

删除 `TeamTaskManager` 结构体中的 `sessionID string` 字段。

修改 `NewTeamTaskManager` 签名，删除 `sessionID string` 参数：

```go
func NewTeamTaskManager(db database.TeamDatabase, teamName, memberName string, messager messager.Messager, plansDir, teamPlanID, leaderMemberName string) *TeamTaskManager {
```

删除 `sessionID: sessionID,` 字段赋值。

在 `publishTaskEvent` 中，将 `tm.sessionID` 替换为 `schema.GetSessionID(ctx)`：

```go
topicID := schema.TeamTopicTask.Build(schema.GetSessionID(ctx), tm.teamName)
```

注意：`publishTaskEvent` 的 `ctx` 参数已经存在，可以直接使用。

- [ ] **Step 7.3: 修改 `tools/message_manager.go` — 删除 sessionID 字段**

删除 `TeamMessageManager` 结构体中的 `sessionID string` 字段。

修改 `NewTeamMessageManager` 签名，删除 `sessionID string` 参数：

```go
func NewTeamMessageManager(db database.TeamDatabase, teamName, memberName string, msg messager.Messager) *TeamMessageManager {
```

删除 `sessionID: sessionID,` 字段赋值。

在 `publishMessageEvent` 中，`schema.GetSessionID(ctx)` 已在 Step 6.2 中使用。

- [ ] **Step 7.4: 修改 `tools/task_manager_test.go` — 删除 sessionID 参数**

所有 `NewTeamTaskManager(...)` 调用中删除最后一个 `""` 参数（空字符串 sessionID）。

- [ ] **Step 7.5: 修改 `tools/message_manager_test.go` — 删除 sessionID 参数**

所有 `NewTeamMessageManager(...)` 调用中删除最后一个 `""` 参数。

- [ ] **Step 7.6: 修改 `agent/infra.go` — 删除 sessionID 参数（如果存在构造调用）**

检查 `agent/infra.go` 或 `agent/agent_configurator.go` 中是否有 `NewTeamTaskManager` / `NewTeamMessageManager` 的构造调用，删除 `sessionID` 参数。

- [ ] **Step 7.7: 更新 `tools/doc.go`**

从文件目录中删除 `events.go` 条目。

- [ ] **Step 7.8: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 7.9: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 7.10: 提交**

```bash
git add -A && git commit -m "refactor: 删除 tools/events.go 重复常量，删除 sessionID 字段，改用 schema.GetSessionID(ctx)"
```

---

## Task 8: Step 4 — MessageID 改用 UUID v4

**Files:**
- Modify: `internal/agent_teams/tools/message_manager.go`
- Modify: `internal/agent_teams/tools/message_manager_test.go`

- [ ] **Step 8.1: 修改 `tools/message_manager.go` — UUID v4**

添加 `"github.com/google/uuid"` import。

将 `SendMessage` 和 `BroadcastMessage` 中的 MessageID 生成：

```go
// 修改前
messageID := fmt.Sprintf("msg_%s_%d_%d", tm.teamName, time.Now().UnixMilli(), time.Now().UnixNano()%1000)

// 修改后
messageID := uuid.New().String()
```

检查 `"fmt"` 和 `"time"` import 是否仍被其他地方使用，如果不再使用则删除。

- [ ] **Step 8.2: 修改 `tools/message_manager_test.go` — 更新 MessageID 校验**

测试中不再硬编码 MessageID 格式，改为校验非空和唯一性：

```go
// 修改前
if msgID == "" { ... }

// 修改后（保持不变，空字符串校验已足够）
// UUID v4 格式校验可选
if msgID == "" { ... }
```

- [ ] **Step 8.3: 编译验证**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./internal/agent_teams/...
```

- [ ] **Step 8.4: 运行测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1
```

- [ ] **Step 8.5: 提交**

```bash
git add -A && git commit -m "refactor: MessageID 改用 UUID v4，对齐 Python uuid4()"
```

---

## Task 9: 最终验证 + 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 9.1: 全量编译**

```bash
pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1
export GOPROXY=https://goproxy.cn,direct && go build ./...
```

- [ ] **Step 9.2: 全量测试**

```bash
export GOPROXY=https://goproxy.cn,direct && go test ./internal/agent_teams/... -count=1 -cover
```

- [ ] **Step 9.3: 检查覆盖率**

确认各包覆盖率仍 ≥ 85%：
- messager
- tools/database
- tools
- schema

- [ ] **Step 9.4: 更新 IMPLEMENTATION_PLAN.md**

将 9.65-1 相关状态标记为 ✅（循环依赖重构完成）。

- [ ] **Step 9.5: 最终提交**

```bash
git add -A && git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md，9.65-1 循环依赖重构完成"
```
