# 10.5 扩展框架最小子集 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 Python 扩展系统的最小子集（6 个核心文件），覆盖 ExtensionRegistry 的数据结构和回调触发机制，使现有回填点可以闭环。

**Architecture:** 在 `internal/swarm/extensions/` 下创建扩展框架包，复用已有 `callback.CallbackFramework` 的 `OnCustom/TriggerCustom/OffCustom` 作为底层回调机制。方法名用 Go CamelCase 映射对齐 Python snake_case。延后部分留 stub 占位标注 ⤵️。

**Tech Stack:** Go 1.22+, 依赖已有包：`swarm/schema`（HookEventBase）、`agentcore/runner/callback`（CallbackFramework）、`swarm/transport`（AgentTransport）

---

## File Structure

| 文件 | 职责 | 状态 |
|------|------|------|
| `internal/swarm/extensions/doc.go` | 包文档 | 新建 |
| `internal/swarm/extensions/types.go` | ExtensionMetadata + ExtensionConfig | 新建 |
| `internal/swarm/extensions/types_test.go` | 类型测试 | 新建 |
| `internal/swarm/extensions/hook_event.go` | GatewayHookEvents + AgentServerHookEvents | 新建 |
| `internal/swarm/extensions/hook_event_test.go` | 事件常量测试 | 新建 |
| `internal/swarm/extensions/hooks_context.go` | 4 种 HookContext 结构体 | 新建 |
| `internal/swarm/extensions/hooks_context_test.go` | 上下文结构测试 | 新建 |
| `internal/swarm/extensions/registry.go` | ExtensionRegistry 单例 | 新建 |
| `internal/swarm/extensions/registry_test.go` | Registry 测试 | 新建 |
| `internal/swarm/extensions/loader.go` | ExtensionLoader stub | 新建 |
| `internal/swarm/extensions/manager.go` | ExtensionManager stub | 新建 |
| `internal/swarm/extensions/callback_compat.go` | 注释说明已覆盖 | 新建 |
| `internal/swarm/extensions/sdk/doc.go` | SDK 子包文档 | 新建 |
| `internal/swarm/extensions/sdk/base.go` | BaseExtension 接口 + 默认实现 | 新建 |
| `internal/swarm/extensions/sdk/base_test.go` | BaseExtension 测试 | 新建 |
| `internal/swarm/extensions/sdk/agent_server_client.go` | AgentServerClientExtension | 新建 |
| `internal/swarm/extensions/sdk/agent_server_client_test.go` | AgentServerClientExtension 测试 | 新建 |
| `internal/swarm/extensions/sdk/crypto_utility.go` | CryptoUtility stub | 新建 |
| `IMPLEMENTATION_PLAN.md` | 更新 10.5.1-10 拆分 | 修改 |

---

### Task 1: 数据类型 ExtensionMetadata + ExtensionConfig (10.5.1)

**Files:**
- Create: `internal/swarm/extensions/doc.go`
- Create: `internal/swarm/extensions/types.go`
- Create: `internal/swarm/extensions/types_test.go`

- [ ] **Step 1: Write the failing test**

创建 `internal/swarm/extensions/types_test.go`:

```go
package extensions

import (
	"testing"
)

// TestExtensionMetadata_字段完整性 测试 ExtensionMetadata 所有字段可正确赋值
func TestExtensionMetadata_字段完整性(t *testing.T) {
	m := ExtensionMetadata{
		ID:                    "ext-001",
		Name:                  "测试扩展",
		Version:               "1.0.0",
		Description:           "这是一个测试扩展",
		Author:                "test-author",
		MinJiuwenSwarmVersion: "0.1.0",
		Dependencies:          map[string]string{"core": ">=0.1.0"},
		ConfigSchema:          map[string]any{"type": "object"},
	}

	if m.ID != "ext-001" {
		t.Errorf("ID = %q, want %q", m.ID, "ext-001")
	}
	if m.Name != "测试扩展" {
		t.Errorf("Name = %q, want %q", m.Name, "测试扩展")
	}
	if m.Version != "1.0.0" {
		t.Errorf("Version = %q, want %q", m.Version, "1.0.0")
	}
	if m.Dependencies["core"] != ">=0.1.0" {
		t.Errorf("Dependencies[core] = %q, want %q", m.Dependencies["core"], ">=0.1.0")
	}
	if m.ConfigSchema["type"] != "object" {
		t.Errorf("ConfigSchema[type] = %v, want %q", m.ConfigSchema["type"], "object")
	}
}

// TestExtensionMetadata_可选字段 测试 ConfigSchema 为 nil 时默认值
func TestExtensionMetadata_可选字段(t *testing.T) {
	m := ExtensionMetadata{
		ID:      "ext-002",
		Name:    "无配置扩展",
		Version: "0.5.0",
	}
	if m.ConfigSchema != nil {
		t.Errorf("ConfigSchema should be nil when not set, got %v", m.ConfigSchema)
	}
}

// TestExtensionConfig_字段完整性 测试 ExtensionConfig 字段赋值
func TestExtensionConfig_字段完整性(t *testing.T) {
	cfg := ExtensionConfig{
		Config: map[string]any{"key": "value"},
	}
	if cfg.Config["key"] != "value" {
		t.Errorf("Config[key] = %v, want %q", cfg.Config["key"], "value")
	}
}

// TestExtensionConfig_默认值 测试 ExtensionConfig 空 Config
func TestExtensionConfig_默认值(t *testing.T) {
	cfg := ExtensionConfig{}
	if cfg.Config != nil {
		t.Errorf("Config should be nil when not set, got %v", cfg.Config)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestExtension -v`
Expected: FAIL — package does not exist yet

- [ ] **Step 3: Create doc.go + types.go**

创建 `internal/swarm/extensions/doc.go`:

```go
// Package extensions 提供扩展系统基础设施，对齐 Python jiuwenswarm/extensions/。
//
// 本包定义了扩展注册中心（ExtensionRegistry）、数据类型（ExtensionMetadata/ExtensionConfig）、
// 钩子事件常量（GatewayHookEvents/AgentServerHookEvents）、钩子上下文（HookContext 系列）
// 以及扩展 SDK 基类（BaseExtension/AgentServerClientExtension）。
//
// 最小子集已实现（10.5.1~10.5.6），回调机制复用 agentcore/runner/callback.CallbackFramework。
// 延后部分：ExtensionLoader（10.5.7 ⤵️）、ExtensionManager（10.5.8 ⤵️）、CryptoUtility（10.5.10 ⤵️）。
// CallbackCompat 不需要（Go 已有 CallbackFramework，10.5.9 ⤴️）。
//
// 文件目录：
//
//	extensions/
//	├── doc.go                 # 包文档
//	├── types.go               # ExtensionMetadata + ExtensionConfig（10.5.1）
//	├── hook_event.go          # GatewayHookEvents + AgentServerHookEvents（10.5.2）
//	├── hooks_context.go       # MemoryHookContext/GatewayChatHookContext/AgentServerChatHookContext/SystemPromptHookContext（10.5.3）
//	├── registry.go            # ExtensionRegistry 单例 + 回调触发（10.5.6）
//	├── loader.go              # ExtensionLoader stub（10.5.7 ⤵️）
//	├── manager.go             # ExtensionManager stub（10.5.8 ⤵️）
//	├── callback_compat.go     # 注释说明已覆盖（10.5.9 ⤴️）
//	└── sdk/
//	    ├── doc.go             # SDK 子包文档
//	    ├── base.go            # BaseExtension 接口 + 默认实现（10.5.4）
//	    ├── agent_server_client.go # AgentServerClientExtension（10.5.5）
//	    └── crypto_utility.go  # CryptoUtility stub（10.5.10 ⤵️）
//
// 对应 Python 代码：jiuwenswarm/extensions/
package extensions
```

创建 `internal/swarm/extensions/types.go`:

```go
package extensions

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionMetadata 扩展元数据，对齐 Python ExtensionMetadata dataclass
type ExtensionMetadata struct {
	// ID 扩展唯一标识
	ID string `json:"id"`
	// Name 扩展名称
	Name string `json:"name"`
	// Version 扩展版本
	Version string `json:"version"`
	// Description 扩展描述
	Description string `json:"description"`
	// Author 扩展作者
	Author string `json:"author"`
	// MinJiuwenSwarmVersion 最小兼容版本
	MinJiuwenSwarmVersion string `json:"min_jiuwenswarm_version"`
	// Dependencies 扩展依赖 {"extension_id": ">=1.0.0"}
	Dependencies map[string]string `json:"dependencies"`
	// ConfigSchema 配置模式 (JSON Schema)，可选
	ConfigSchema map[string]any `json:"config_schema,omitempty"`
}

// ExtensionConfig 扩展配置，对齐 Python ExtensionConfig dataclass
type ExtensionConfig struct {
	// Config 全局配置字典
	Config map[string]any
	// Logger 日志实例（实际使用 logger.ComponentXxx）
	Logger any
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestExtension -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/doc.go internal/swarm/extensions/types.go internal/swarm/extensions/types_test.go
git commit -m "feat(10.5.1): 添加 ExtensionMetadata + ExtensionConfig 数据类型，对齐 Python extensions/types.py"
```

---

### Task 2: 钩子事件常量 GatewayHookEvents + AgentServerHookEvents (10.5.2)

**Files:**
- Create: `internal/swarm/extensions/hook_event.go`
- Create: `internal/swarm/extensions/hook_event_test.go`

- [ ] **Step 1: Write the failing test**

创建 `internal/swarm/extensions/hook_event_test.go`:

```go
package extensions

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/schema"
)

// TestGatewayHookEvents_作用域 测试 Gateway 事件作用域
func TestGatewayHookEvents_作用域(t *testing.T) {
	events := NewGatewayHookEvents()
	if events.Scope != "gateway" {
		t.Errorf("Scope = %q, want %q", events.Scope, "gateway")
	}
}

// TestGatewayHookEvents_常量值 测试 Gateway 事件常量值格式
func TestGatewayHookEvents_常量值(t *testing.T) {
	wantStarted := "gateway:gateway_started"
	wantStopped := "gateway:gateway_stopped"
	wantBeforeChat := "gateway:before_chat_request"

	if GatewayStarted != wantStarted {
		t.Errorf("GatewayStarted = %q, want %q", GatewayStarted, wantStarted)
	}
	if GatewayStopped != wantStopped {
		t.Errorf("GatewayStopped = %q, want %q", GatewayStopped, wantStopped)
	}
	if GatewayBeforeChatRequest != wantBeforeChat {
		t.Errorf("GatewayBeforeChatRequest = %q, want %q", GatewayBeforeChatRequest, wantBeforeChat)
	}
}

// TestGatewayHookEvents_GetEvent 测试 GetEvent 方法构建 scoped 事件名
func TestGatewayHookEvents_GetEvent(t *testing.T) {
	events := NewGatewayHookEvents()
	result := events.GetEvent("custom_event")
	want := "gateway:custom_event"
	if result != want {
		t.Errorf("GetEvent(custom_event) = %q, want %q", result, want)
	}
}

// TestAgentServerHookEvents_作用域 测试 AgentServer 事件作用域
func TestAgentServerHookEvents_作用域(t *testing.T) {
	events := NewAgentServerHookEvents()
	if events.Scope != "agent_server" {
		t.Errorf("Scope = %q, want %q", events.Scope, "agent_server")
	}
}

// TestAgentServerHookEvents_常量值 测试 AgentServer 事件常量值格式
func TestAgentServerHookEvents_常量值(t *testing.T) {
	wantStarted := "agent_server:agent_server_started"
	wantStopped := "agent_server:agent_server_stopped"
	wantBeforeChat := "agent_server:before_chat_request"
	wantMemoryBeforeChat := "agent_server:memory_before_chat"
	wantMemoryAfterChat := "agent_server:memory_after_chat"
	wantBeforePromptBuild := "agent_server:before_system_prompt_build"

	if AgentServerStarted != wantStarted {
		t.Errorf("AgentServerStarted = %q, want %q", AgentServerStarted, wantStarted)
	}
	if AgentServerStopped != wantStopped {
		t.Errorf("AgentServerStopped = %q, want %q", AgentServerStopped, wantStopped)
	}
	if AgentServerBeforeChatRequest != wantBeforeChat {
		t.Errorf("AgentServerBeforeChatRequest = %q, want %q", AgentServerBeforeChatRequest, wantBeforeChat)
	}
	if AgentServerMemoryBeforeChat != wantMemoryBeforeChat {
		t.Errorf("AgentServerMemoryBeforeChat = %q, want %q", AgentServerMemoryBeforeChat, wantMemoryBeforeChat)
	}
	if AgentServerMemoryAfterChat != wantMemoryAfterChat {
		t.Errorf("AgentServerMemoryAfterChat = %q, want %q", AgentServerMemoryAfterChat, wantMemoryAfterChat)
	}
	if AgentServerBeforeSystemPromptBuild != wantBeforePromptBuild {
		t.Errorf("AgentServerBeforeSystemPromptBuild = %q, want %q", AgentServerBeforeSystemPromptBuild, wantBeforePromptBuild)
	}
}

// TestAgentServerHookEvents_GetEvent 测试 GetEvent 方法构建 scoped 事件名
func TestAgentServerHookEvents_GetEvent(t *testing.T) {
	events := NewAgentServerHookEvents()
	result := events.GetEvent("custom_event")
	want := "agent_server:custom_event"
	if result != want {
		t.Errorf("GetEvent(custom_event) = %q, want %q", result, want)
	}
}

// TestHookEventBase_ParseEventName_一致性 测试事件名解析与 BuildEventName 一致
func TestHookEventBase_ParseEventName_一致性(t *testing.T) {
	scope, name := schema.ParseEventName(GatewayBeforeChatRequest)
	if scope != "gateway" || name != "before_chat_request" {
		t.Errorf("ParseEventName(%q) = (%q, %q), want (%q, %q)",
			GatewayBeforeChatRequest, scope, name, "gateway", "before_chat_request")
	}

	rebuilt := schema.BuildEventName(scope, name)
	if rebuilt != GatewayBeforeChatRequest {
		t.Errorf("BuildEventName(%q, %q) = %q, want %q", scope, name, rebuilt, GatewayBeforeChatRequest)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestHookEvent -v`
Expected: FAIL — NewGatewayHookEvents/GatewayStarted 等未定义

- [ ] **Step 3: Write implementation**

创建 `internal/swarm/extensions/hook_event.go`:

```go
package extensions

import "github.com/uapclaw/uapclaw-go/internal/swarm/schema"

// ──────────────────────────── 结构体 ────────────────────────────

// GatewayHookEvents Gateway 侧钩子事件，对齐 Python GatewayHookEvents
// scope = "gateway"，继承 HookEventBase
type GatewayHookEvents struct {
	*schema.HookEventBase
}

// AgentServerHookEvents AgentServer 侧钩子事件，对齐 Python AgentServerHookEvents
// scope = "agent_server"，继承 HookEventBase
type AgentServerHookEvents struct {
	*schema.HookEventBase
}

// ──────────────────────────── 常量 ────────────────────────────

// Gateway 事件常量，对齐 Python GatewayHookEvents 类属性
// 格式: scope:event_name
const (
	// GatewayStarted Gateway 启动事件，对齐 Python GATEWAY_STARTED
	GatewayStarted = "gateway:gateway_started"
	// GatewayStopped Gateway 停止事件，对齐 Python GATEWAY_STOPPED
	GatewayStopped = "gateway:gateway_stopped"
	// GatewayBeforeChatRequest Gateway 聊天请求前事件，对齐 Python BEFORE_CHAT_REQUEST
	GatewayBeforeChatRequest = "gateway:before_chat_request"
)

// AgentServer 事件常量，对齐 Python AgentServerHookEvents 类属性
// 格式: scope:event_name
const (
	// AgentServerStarted AgentServer 启动事件，对齐 Python AGENT_SERVER_STARTED
	AgentServerStarted = "agent_server:agent_server_started"
	// AgentServerStopped AgentServer 停止事件，对齐 Python AGENT_SERVER_STOPPED
	AgentServerStopped = "agent_server:agent_server_stopped"
	// AgentServerBeforeChatRequest AgentServer 聊天请求前事件，对齐 Python BEFORE_CHAT_REQUEST
	AgentServerBeforeChatRequest = "agent_server:before_chat_request"
	// AgentServerMemoryBeforeChat 记忆对话前事件，对齐 Python MEMORY_BEFORE_CHAT
	AgentServerMemoryBeforeChat = "agent_server:memory_before_chat"
	// AgentServerMemoryAfterChat 记忆对话后事件，对齐 Python MEMORY_AFTER_CHAT
	AgentServerMemoryAfterChat = "agent_server:memory_after_chat"
	// AgentServerBeforeSystemPromptBuild 系统提示词构建前事件，对齐 Python BEFORE_SYSTEM_PROMPT_BUILD
	AgentServerBeforeSystemPromptBuild = "agent_server:before_system_prompt_build"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewGatewayHookEvents 创建 Gateway 钩子事件实例，对齐 Python GatewayHookEvents(scope="gateway")
func NewGatewayHookEvents() *GatewayHookEvents {
	return &GatewayHookEvents{
		HookEventBase: &schema.HookEventBase{Scope: "gateway"},
	}
}

// NewAgentServerHookEvents 创建 AgentServer 钩子事件实例，对齐 Python AgentServerHookEvents(scope="agent_server")
func NewAgentServerHookEvents() *AgentServerHookEvents {
	return &AgentServerHookEvents{
		HookEventBase: &schema.HookEventBase{Scope: "agent_server"},
	}
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestHookEvent -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/hook_event.go internal/swarm/extensions/hook_event_test.go
git commit -m "feat(10.5.2): 添加 GatewayHookEvents + AgentServerHookEvents 事件常量，对齐 Python extensions/hook_event.py"
```

---

### Task 3: 钩子上下文结构 4 种 HookContext (10.5.3)

**Files:**
- Create: `internal/swarm/extensions/hooks_context.go`
- Create: `internal/swarm/extensions/hooks_context_test.go`

- [ ] **Step 1: Write the failing test**

创建 `internal/swarm/extensions/hooks_context_test.go`:

```go
package extensions

import (
	"testing"
)

// TestMemoryHookContext_字段完整性 测试 MemoryHookContext 字段赋值
func TestMemoryHookContext_字段完整性(t *testing.T) {
	channelID := "ch-001"
	assistantMsg := "hello"
	ctx := MemoryHookContext{
		SessionID:        "sess-001",
		RequestID:        "req-001",
		ChannelID:        &channelID,
		AgentName:        "agent-001",
		WorkspaceDir:     "/tmp/workspace",
		AssistantMessage: &assistantMsg,
		Extra:            map[string]any{"key": "val"},
		MemoryBlocks:     []string{"block1", "block2"},
		Metadata:         map[string]any{"meta": "data"},
	}
	if ctx.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", ctx.SessionID, "sess-001")
	}
	if *ctx.ChannelID != "ch-001" {
		t.Errorf("ChannelID = %q, want %q", *ctx.ChannelID, "ch-001")
	}
	if len(ctx.MemoryBlocks) != 2 {
		t.Errorf("MemoryBlocks len = %d, want 2", len(ctx.MemoryBlocks))
	}
}

// TestMemoryHookContext_ToMap 测试 ToMap 序列化
func TestMemoryHookContext_ToMap(t *testing.T) {
	ctx := MemoryHookContext{
		SessionID:    "sess-001",
		RequestID:    "req-001",
		AgentName:    "agent-001",
		WorkspaceDir: "/tmp/workspace",
	}
	m := ctx.ToMap()
	if m["session_id"] != "sess-001" {
		t.Errorf("ToMap()[session_id] = %q, want %q", m["session_id"], "sess-001")
	}
	if m["agent_name"] != "agent-001" {
		t.Errorf("ToMap()[agent_name] = %q, want %q", m["agent_name"], "agent-001")
	}
}

// TestMemoryHookContext_可选字段 测试 nil 可选字段
func TestMemoryHookContext_可选字段(t *testing.T) {
	ctx := MemoryHookContext{
		SessionID:    "sess-001",
		RequestID:    "req-001",
		AgentName:    "agent-001",
		WorkspaceDir: "/tmp/workspace",
	}
	if ctx.ChannelID != nil {
		t.Error("ChannelID should be nil")
	}
	if ctx.AssistantMessage != nil {
		t.Error("AssistantMessage should be nil")
	}
}

// TestGatewayChatHookContext_字段完整性 测试 GatewayChatHookContext 字段赋值
func TestGatewayChatHookContext_字段完整性(t *testing.T) {
	sessionID := "sess-001"
	reqMethod := "chat.send"
	ctx := GatewayChatHookContext{
		RequestID:  "req-001",
		ChannelID:  "ch-001",
		SessionID:  &sessionID,
		ReqMethod:  &reqMethod,
		Params:     map[string]any{"mode": "agent"},
	}
	if ctx.RequestID != "req-001" {
		t.Errorf("RequestID = %q, want %q", ctx.RequestID, "req-001")
	}
	if *ctx.SessionID != "sess-001" {
		t.Errorf("SessionID = %q, want %q", *ctx.SessionID, "sess-001")
	}
	if ctx.Params["mode"] != "agent" {
		t.Errorf("Params[mode] = %v, want %q", ctx.Params["mode"], "agent")
	}
}

// TestGatewayChatHookContext_ToMap 测试 ToMap 序列化
func TestGatewayChatHookContext_ToMap(t *testing.T) {
	ctx := GatewayChatHookContext{
		RequestID: "req-001",
		ChannelID: "ch-001",
		Params:    map[string]any{},
	}
	m := ctx.ToMap()
	if m["request_id"] != "req-001" {
		t.Errorf("ToMap()[request_id] = %q, want %q", m["request_id"], "req-001")
	}
}

// TestAgentServerChatHookContext_字段完整性 测试 AgentServerChatHookContext 字段赋值
func TestAgentServerChatHookContext_字段完整性(t *testing.T) {
	sessionID := "sess-001"
	reqMethod := "chat.send"
	ctx := AgentServerChatHookContext{
		RequestID:  "req-001",
		ChannelID:  "ch-001",
		SessionID:  &sessionID,
		ReqMethod:  &reqMethod,
		Params:     map[string]any{"mode": "agent"},
	}
	if ctx.ChannelID != "ch-001" {
		t.Errorf("ChannelID = %q, want %q", ctx.ChannelID, "ch-001")
	}
}

// TestAgentServerChatHookContext_ToMap 测试 ToMap 序列化
func TestAgentServerChatHookContext_ToMap(t *testing.T) {
	ctx := AgentServerChatHookContext{
		RequestID: "req-001",
		ChannelID: "ch-001",
		Params:    map[string]any{},
	}
	m := ctx.ToMap()
	if m["channel_id"] != "ch-001" {
		t.Errorf("ToMap()[channel_id] = %q, want %q", m["channel_id"], "ch-001")
	}
}

// TestSystemPromptHookContext_字段完整性 测试 SystemPromptHookContext 字段赋值
func TestSystemPromptHookContext_字段完整性(t *testing.T) {
	homeDir := "/home/test"
	skillDir := "/skills"
	ctx := SystemPromptHookContext{
		HomeDir:  &homeDir,
		SkillDir: &skillDir,
	}
	if *ctx.HomeDir != "/home/test" {
		t.Errorf("HomeDir = %q, want %q", *ctx.HomeDir, "/home/test")
	}
	if *ctx.SkillDir != "/skills" {
		t.Errorf("SkillDir = %q, want %q", *ctx.SkillDir, "/skills")
	}
}

// TestSystemPromptHookContext_ToMap 测试 ToMap 序列化
func TestSystemPromptHookContext_ToMap(t *testing.T) {
	ctx := SystemPromptHookContext{}
	m := ctx.ToMap()
	// nil 字段序列化为 nil
	if m["home_dir"] != nil {
		t.Errorf("ToMap()[home_dir] = %v, want nil", m["home_dir"])
	}
	if m["skill_dir"] != nil {
		t.Errorf("ToMap()[skill_dir] = %v, want nil", m["skill_dir"])
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestHookContext -v`
Expected: FAIL — MemoryHookContext 等未定义

- [ ] **Step 3: Write implementation**

创建 `internal/swarm/extensions/hooks_context.go`:

```go
package extensions

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryHookContext 记忆钩子上下文，对齐 Python MemoryHookContext dataclass
// before_chat 扩展写入 memory_blocks，宿主从本字段读取拼接结果
type MemoryHookContext struct {
	// SessionID 会话标识
	SessionID string `json:"session_id"`
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// ChannelID 渠道标识（可选）
	ChannelID *string `json:"channel_id,omitempty"`
	// AgentName Agent 名称
	AgentName string `json:"agent_name"`
	// WorkspaceDir 工作目录
	WorkspaceDir string `json:"workspace_dir"`
	// AssistantMessage 助手消息（输出字段，可选）
	AssistantMessage *string `json:"assistant_message,omitempty"`
	// Extra 输入扩展字段
	Extra map[string]any `json:"extra,omitempty"`
	// MemoryBlocks 记忆内容块（before_chat 扩展写入，宿主读取拼接结果）
	MemoryBlocks []string `json:"memory_blocks,omitempty"`
	// Metadata 输出扩展字段
	Metadata map[string]any `json:"metadata,omitempty"`
}

// GatewayChatHookContext Gateway 聊天钩子上下文，对齐 Python GatewayChatHookContext dataclass
// 扩展可直接原地修改 Params，Gateway 会将其传给 AgentRequest.params
type GatewayChatHookContext struct {
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// SessionID 会话标识（可选）
	SessionID *string `json:"session_id,omitempty"`
	// ReqMethod 请求方法（可选）
	ReqMethod *string `json:"req_method,omitempty"`
	// Params 扩展可直接原地修改，Gateway 会将其传给 AgentRequest.params
	Params map[string]any `json:"params,omitempty"`
}

// AgentServerChatHookContext AgentServer 聊天钩子上下文，对齐 Python AgentServerChatHookContext dataclass
// 扩展可直接原地修改 Params，AgentServer 后续逻辑继续使用 request.params
type AgentServerChatHookContext struct {
	// RequestID 请求标识
	RequestID string `json:"request_id"`
	// ChannelID 渠道标识
	ChannelID string `json:"channel_id"`
	// SessionID 会话标识（可选）
	SessionID *string `json:"session_id,omitempty"`
	// ReqMethod 请求方法（可选）
	ReqMethod *string `json:"req_method,omitempty"`
	// Params 扩展可直接原地修改，AgentServer 后续逻辑继续使用 request.params
	Params map[string]any `json:"params,omitempty"`
}

// SystemPromptHookContext 系统提示词钩子上下文，对齐 Python SystemPromptHookContext dataclass
type SystemPromptHookContext struct {
	// HomeDir 扩展可设置此目录覆盖默认 home_dir（可选）
	HomeDir *string `json:"home_dir,omitempty"`
	// SkillDir 扩展可设置此目录扩展默认 skill_dir（可选）
	SkillDir *string `json:"skill_dir,omitempty"`
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToMap 将 MemoryHookContext 转为字典，对齐 Python MemoryHookContext.to_dict()
func (c *MemoryHookContext) ToMap() map[string]any {
	result := map[string]any{
		"session_id":    c.SessionID,
		"request_id":    c.RequestID,
		"channel_id":    c.ChannelID,
		"agent_name":    c.AgentName,
		"workspace_dir": c.WorkspaceDir,
		"assistant_message": c.AssistantMessage,
		"extra":         c.Extra,
		"memory_blocks": c.MemoryBlocks,
		"metadata":      c.Metadata,
	}
	return result
}

// ToMap 将 GatewayChatHookContext 转为字典，对齐 Python GatewayChatHookContext.to_dict()
func (c *GatewayChatHookContext) ToMap() map[string]any {
	result := map[string]any{
		"request_id":  c.RequestID,
		"channel_id":  c.ChannelID,
		"session_id":  c.SessionID,
		"req_method":  c.ReqMethod,
		"params":      c.Params,
	}
	return result
}

// ToMap 将 AgentServerChatHookContext 转为字典，对齐 Python AgentServerChatHookContext.to_dict()
func (c *AgentServerChatHookContext) ToMap() map[string]any {
	result := map[string]any{
		"request_id":  c.RequestID,
		"channel_id":  c.ChannelID,
		"session_id":  c.SessionID,
		"req_method":  c.ReqMethod,
		"params":      c.Params,
	}
	return result
}

// ToMap 将 SystemPromptHookContext 转为字典，对齐 Python SystemPromptHookContext.to_dict()
func (c *SystemPromptHookContext) ToMap() map[string]any {
	result := map[string]any{
		"home_dir":  c.HomeDir,
		"skill_dir": c.SkillDir,
	}
	return result
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestHookContext -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/hooks_context.go internal/swarm/extensions/hooks_context_test.go
git commit -m "feat(10.5.3): 添加 4 种 HookContext 结构体，对齐 Python extensions/hooks_context.py"
```

---

### Task 4: BaseExtension 抽象基类 + 默认实现 (10.5.4)

**Files:**
- Create: `internal/swarm/extensions/sdk/doc.go`
- Create: `internal/swarm/extensions/sdk/base.go`
- Create: `internal/swarm/extensions/sdk/base_test.go`

- [ ] **Step 1: Write the failing test**

创建 `internal/swarm/extensions/sdk/base_test.go`:

```go
package sdk

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
)

// TestBaseExtensionImpl_Metadata_缓存 测试元数据缓存机制
func TestBaseExtensionImpl_Metadata_缓存(t *testing.T) {
	impl := &BaseExtensionImpl{}
	// 无 metadata 缓存时 Metadata() 返回 nil
	m := impl.Metadata()
	if m != nil {
		t.Errorf("Metadata() = %v, want nil (no cache)", m)
	}
}

// TestBaseExtensionImpl_SetExtensionDir 测试 SetExtensionDir 清除缓存
func TestBaseExtensionImpl_SetExtensionDir(t *testing.T) {
	impl := &BaseExtensionImpl{}

	// 设置 metadata 缓存
	impl.metadataCache = &extensions.ExtensionMetadata{ID: "cached-ext"}
	m := impl.Metadata()
	if m.ID != "cached-ext" {
		t.Errorf("Metadata() = %q, want %q", m.ID, "cached-ext")
	}

	// SetExtensionDir 应清除缓存
	impl.SetExtensionDir("/test/ext/dir")
	m2 := impl.Metadata()
	if m2 != nil {
		t.Errorf("Metadata() after SetExtensionDir = %v, want nil (cache cleared)", m2)
	}
	if impl.extensionDir == nil || *impl.extensionDir != "/test/ext/dir" {
		t.Errorf("extensionDir = %v, want %q", impl.extensionDir, "/test/ext/dir")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_文件不存在 测试 YAML 文件不存在时返回错误
func TestBaseExtensionImpl_LoadMetadataFromYAML_文件不存在(t *testing.T) {
	impl := &BaseExtensionImpl{}
	impl.SetExtensionDir(t.TempDir()) // 空目录，无 extension.yaml

	_, err := impl.LoadMetadataFromYAML()
	if err == nil {
		t.Error("LoadMetadataFromYAML() should return error when file does not exist")
	}
}

// TestBaseExtensionImpl_LoadMetadataFromYAML_成功 测试从 YAML 加载元数据
func TestBaseExtensionImpl_LoadMetadataFromYAML_成功(t *testing.T) {
	impl := &BaseExtensionImpl{}
	dir := t.TempDir()

	// 写入 extension.yaml
	yamlContent := `id: test-ext
name: 测试扩展
version: 1.0.0
description: 测试描述
author: test-author
min_jiuwenswarm_version: "0.1.0"
dependencies:
  core: ">=0.1.0"
`
	yamlPath := dir + "/extension.yaml"
	if err := writeFile(yamlPath, yamlContent); err != nil {
		t.Fatalf("write yaml: %v", err)
	}

	impl.SetExtensionDir(dir)
	m, err := impl.LoadMetadataFromYAML()
	if err != nil {
		t.Fatalf("LoadMetadataFromYAML() error: %v", err)
	}
	if m.ID != "test-ext" {
		t.Errorf("ID = %q, want %q", m.ID, "test-ext")
	}
	if m.Name != "测试扩展" {
		t.Errorf("Name = %q, want %q", m.Name, "测试扩展")
	}
	if m.Dependencies["core"] != ">=0.1.0" {
		t.Errorf("Dependencies[core] = %q, want %q", m.Dependencies["core"], ">=0.1.0")
	}
}

// TestBaseExtensionImpl_LoadConfigFromYAML_不存在 测试 config.yaml 不存在返回空字典
func TestBaseExtensionImpl_LoadConfigFromYAML_不存在(t *testing.T) {
	impl := &BaseExtensionImpl{}
	impl.SetExtensionDir(t.TempDir())

	cfg := impl.LoadConfigFromYAML()
	if cfg != nil {
		t.Errorf("LoadConfigFromYAML() = %v, want nil when file not exists", cfg)
	}
}

// TestManifestFilename 测试常量对齐
func TestManifestFilename(t *testing.T) {
	if ManifestFilename != "extension.yaml" {
		t.Errorf("ManifestFilename = %q, want %q", ManifestFilename, "extension.yaml")
	}
}

// TestBaseExtension_接口契约 测试 BaseExtension 接口方法签名
func TestBaseExtension_接口契约(t *testing.T) {
	// 验证 BaseExtension 接口包含所需方法
	var _ BaseExtension = (*mockExtension)(nil)
}

// mockExtension 用于测试 BaseExtension 接口实现的 mock
type mockExtension struct {
	BaseExtensionImpl
}

func (e *mockExtension) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

func (e *mockExtension) Shutdown(ctx context.Context) error {
	return nil
}
```

需要在测试文件中添加辅助函数：

```go
// writeFile 辅助函数：写入文件内容
func writeFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0644)
}
```

注意：测试文件需要 `import "os"` 添加到 imports。

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/sdk/... -run TestBase -v`
Expected: FAIL — package sdk does not exist yet

- [ ] **Step 3: Create sdk/doc.go + base.go**

创建 `internal/swarm/extensions/sdk/doc.go`:

```go
// Package sdk 提供扩展 SDK 基类，对齐 Python jiuwenswarm/extensions/sdk/。
//
// 本包定义了扩展开发的抽象接口：BaseExtension（基础扩展）、
// AgentServerClientExtension（AgentServer 客户端扩展）、CryptoUtility（加解密扩展）。
//
// 文件目录：
//
//	sdk/
//	├── doc.go                # 子包文档
//	├── base.go               # BaseExtension 接口 + BaseExtensionImpl 默认实现（10.5.4）
//	├── agent_server_client.go # AgentServerClientExtension 接口（10.5.5）
//	└── crypto_utility.go     # CryptoUtility stub（10.5.10 ⤵️）
//
// 对应 Python 代码：jiuwenswarm/extensions/sdk/
package sdk
```

创建 `internal/swarm/extensions/sdk/base.go`:

```go
package sdk

import (
	"context"
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"gopkg.in/yaml.v3"
)

// ──────────────────────────── 常量 ────────────────────────────

// ManifestFilename 扩展清单文件名，对齐 Python MANIFEST_FILENAME
const ManifestFilename = "extension.yaml"

// ──────────────────────────── 结构体 ────────────────────────────

// BaseExtension 扩展抽象基类接口，对齐 Python BaseExtension ABC
type BaseExtension interface {
	// Initialize 扩展初始化入口，对齐 Python initialize(config: ExtensionConfig)
	Initialize(ctx context.Context, config *extensions.ExtensionConfig) error
	// Shutdown 扩展关闭释放资源，对齐 Python shutdown()
	Shutdown(ctx context.Context) error
	// Metadata 返回扩展元数据，对齐 Python metadata @property
	Metadata() *extensions.ExtensionMetadata
	// SetExtensionDir 设置扩展目录，对齐 Python set_extension_dir(path)
	SetExtensionDir(path string)
}

// BaseExtensionImpl BaseExtension 默认实现（嵌入使用），对齐 Python BaseExtension 类字段和方法
type BaseExtensionImpl struct {
	metadataCache  *extensions.ExtensionMetadata
	extensionDir   *string
	configCache    map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// Metadata 返回扩展元数据（有缓存则返回缓存，无缓存则返回 nil），
// 对齐 Python BaseExtension.metadata @property
func (b *BaseExtensionImpl) Metadata() *extensions.ExtensionMetadata {
	return b.metadataCache
}

// SetExtensionDir 设置扩展目录，同时清除 metadata 和 config 缓存，
// 对齐 Python BaseExtension.set_extension_dir(path)
func (b *BaseExtensionImpl) SetExtensionDir(path string) {
	b.extensionDir = &path
	b.metadataCache = nil
	b.configCache = nil
}

// LoadMetadataFromYAML 从扩展目录的 extension.yaml 加载元数据，
// 对齐 Python BaseExtension._load_metadata_from_yaml()
func (b *BaseExtensionImpl) LoadMetadataFromYAML() (*extensions.ExtensionMetadata, error) {
	if b.extensionDir == nil {
		return nil, fmt.Errorf("无法确定扩展目录，请在子类中设置目录或调用 SetExtensionDir")
	}

	yamlPath := *b.extensionDir + "/" + ManifestFilename
	data, err := os.ReadFile(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("扩展元数据文件不存在（期望 %s）: %w", ManifestFilename, err)
	}

	var raw map[string]any
	if err := yaml.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("解析 %s 失败: %w", ManifestFilename, err)
	}

	m := &extensions.ExtensionMetadata{
		ID:                    strVal(raw, "id"),
		Name:                  strVal(raw, "name"),
		Version:               strVal(raw, "version"),
		Description:           strVal(raw, "description"),
		Author:                strVal(raw, "author"),
		MinJiuwenSwarmVersion: strVal(raw, "min_jiuwenswarm_version"),
	}
	if deps, ok := raw["dependencies"].(map[string]any); ok {
		m.Dependencies = make(map[string]string, len(deps))
		for k, v := range deps {
			m.Dependencies[k] = fmt.Sprintf("%v", v)
		}
	}
	if cs, ok := raw["config_schema"]; ok {
		m.ConfigSchema = cs.(map[string]any)
	}

	b.metadataCache = m
	return m, nil
}

// LoadConfigFromYAML 从扩展目录的 config.yaml 加载配置，
// 对齐 Python BaseExtension._load_config_from_yaml()
// 文件不存在时返回 nil
func (b *BaseExtensionImpl) LoadConfigFromYAML() map[string]any {
	if b.configCache != nil {
		return b.configCache
	}

	if b.extensionDir == nil {
		return nil
	}

	configPath := *b.extensionDir + "/config.yaml"
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil
	}

	var cfg map[string]any
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil
	}

	b.configCache = cfg
	return cfg
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// strVal 从 map 中安全提取字符串值
func strVal(m map[string]any, key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}
	return fmt.Sprintf("%v", v)
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/sdk/... -run TestBase -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/sdk/doc.go internal/swarm/extensions/sdk/base.go internal/swarm/extensions/sdk/base_test.go
git commit -m "feat(10.5.4): 添加 BaseExtension 接口 + BaseExtensionImpl 默认实现，对齐 Python extensions/sdk/base.py"
```

---

### Task 5: AgentServerClientExtension (10.5.5)

**Files:**
- Create: `internal/swarm/extensions/sdk/agent_server_client.go`
- Create: `internal/swarm/extensions/sdk/agent_server_client_test.go`

- [ ] **Step 1: Write the failing test**

创建 `internal/swarm/extensions/sdk/agent_server_client_test.go`:

```go
package sdk

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// TestAgentServerClientExtension_接口契约 测试接口实现
func TestAgentServerClientExtension_接口契约(t *testing.T) {
	var _ AgentServerClientExtension = (*mockAgentServerClientExt)(nil)
}

// TestAgentServerClientExtensionImpl_GetClient 测试 GetClient 返回 AgentTransport
func TestAgentServerClientExtensionImpl_GetClient(t *testing.T) {
	// 使用 ChannelTransport 作为测试中的 AgentTransport 实现
	chTransport := transport.NewChannelTransport()
	impl := &AgentServerClientExtensionImpl{
		client: chTransport,
	}
	client := impl.GetClient()
	if client == nil {
		t.Error("GetClient() = nil, want non-nil AgentTransport")
	}
}

// TestAgentServerClientExtensionImpl_Initialize 测试 Initialize 方法
func TestAgentServerClientExtensionImpl_Initialize(t *testing.T) {
	impl := &AgentServerClientExtensionImpl{}
	cfg := &extensions.ExtensionConfig{
		Config: map[string]any{"test": true},
	}
	err := impl.Initialize(context.Background(), cfg)
	if err != nil {
		t.Errorf("Initialize() error: %v", err)
	}
}

// TestAgentServerClientExtensionImpl_Shutdown 测试 Shutdown 方法
func TestAgentServerClientExtensionImpl_Shutdown(t *testing.T) {
	impl := &AgentServerClientExtensionImpl{}
	err := impl.Shutdown(context.Background())
	if err != nil {
		t.Errorf("Shutdown() error: %v", err)
	}
}

// mockAgentServerClientExt 用于测试 AgentServerClientExtension 接口
type mockAgentServerClientExt struct {
	BaseExtensionImpl
	mockClient transport.AgentTransport
}

func (e *mockAgentServerClientExt) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

func (e *mockAgentServerClientExt) Shutdown(ctx context.Context) error {
	return nil
}

func (e *mockAgentServerClientExt) GetClient() transport.AgentTransport {
	return e.mockClient
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/sdk/... -run TestAgentServerClient -v`
Expected: FAIL — AgentServerClientExtension 未定义

- [ ] **Step 3: Write implementation**

创建 `internal/swarm/extensions/sdk/agent_server_client.go`:

```go
package sdk

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentServerClientExtension AgentServer 客户端扩展接口，对齐 Python AgentServerClientExtension
// 持有真正的 AgentTransport 实现，通过 GetClient() 暴露
type AgentServerClientExtension interface {
	BaseExtension
	// GetClient 返回与 AgentServer 通信的客户端，对齐 Python get_client() @abstractmethod
	// 返回 transport.AgentTransport 接口（Go 用 AgentTransport 对齐规则 6 Transport 架构）
	GetClient() transport.AgentTransport
}

// AgentServerClientExtensionImpl AgentServerClientExtension 默认实现
type AgentServerClientExtensionImpl struct {
	BaseExtensionImpl
	// client 与 AgentServer 通信的传输层客户端
	client transport.AgentTransport
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetClient 返回与 AgentServer 通信的客户端，对齐 Python AgentServerClientExtension.get_client()
func (e *AgentServerClientExtensionImpl) GetClient() transport.AgentTransport {
	return e.client
}

// Initialize 扩展初始化，对齐 Python AgentServerClientExtension.initialize()
func (e *AgentServerClientExtensionImpl) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error {
	return nil
}

// Shutdown 扩展关闭，对齐 Python AgentServerClientExtension.shutdown()
func (e *AgentServerClientExtensionImpl) Shutdown(ctx context.Context) error {
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/sdk/... -run TestAgentServerClient -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/sdk/agent_server_client.go internal/swarm/extensions/sdk/agent_server_client_test.go
git commit -m "feat(10.5.5): 添加 AgentServerClientExtension 接口，对齐 Python extensions/sdk/agent_server_client.py"
```

---

### Task 6: ExtensionRegistry 单例 + 回调触发 (10.5.6)

**Files:**
- Create: `internal/swarm/extensions/registry.go`
- Create: `internal/swarm/extensions/registry_test.go`

- [ ] **Step 1: Write the failing test**

创建 `internal/swarm/extensions/registry_test.go`:

```go
package extensions

import (
	"context"
	"sync"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
)

// TestExtensionRegistry_CreateInstance 测试创建单例
func TestExtensionRegistry_CreateInstance(t *testing.T) {
	ResetInstance() // 确保干净状态
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	if reg == nil {
		t.Error("CreateInstance() = nil, want non-nil")
	}
	// 再次获取应返回同一实例
	reg2 := GetInstance()
	if reg2 != reg {
		t.Error("GetInstance() != CreateInstance() result, want same instance")
	}
	ResetInstance()
}

// TestExtensionRegistry_CreateInstance_重复调用 测试重复创建应报错
func TestExtensionRegistry_CreateInstance_重复调用(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	_ = CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	second := CreateInstance(fw, map[string]any{}, nil)
	if second != nil {
		t.Error("second CreateInstance() should return nil when instance already exists")
	}
}

// TestExtensionRegistry_GetInstance_未初始化 测试未初始化时 GetInstance 应报错
func TestExtensionRegistry_GetInstance_未初始化(t *testing.T) {
	ResetInstance()
	_, err := GetInstanceErr()
	if err == nil {
		t.Error("GetInstanceErr() should return error when registry not initialized")
	}
}

// TestExtensionRegistry_ResetInstance 测试重置单例
func TestExtensionRegistry_ResetInstance(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	_ = CreateInstance(fw, map[string]any{}, nil)

	ResetInstance()
	_, err := GetInstanceErr()
	if err == nil {
		t.Error("after ResetInstance(), GetInstanceErr() should return error")
	}
}

// TestExtensionRegistry_RegisterAgentServerClient 测试注册 AgentServerClient 扩展
func TestExtensionRegistry_RegisterAgentServerClient(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	chTransport := transport.NewChannelTransport()
	ext := &testAgentServerClientExt{client: chTransport}
	reg.RegisterAgentServerClient(ext)

	gotExt := reg.GetAgentServerClientExtension()
	if gotExt != ext {
		t.Error("GetAgentServerClientExtension() != registered extension")
	}

	gotClient := reg.GetAgentServerClient()
	if gotClient != chTransport {
		t.Error("GetAgentServerClient() != chTransport")
	}
}

// TestExtensionRegistry_RegisterAgentServerClient_无注册 测试未注册时返回 nil
func TestExtensionRegistry_RegisterAgentServerClient_无注册(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	ext := reg.GetAgentServerClientExtension()
	if ext != nil {
		t.Error("GetAgentServerClientExtension() should be nil when no extension registered")
	}

	client := reg.GetAgentServerClient()
	if client != nil {
		t.Error("GetAgentServerClient() should be nil when no extension registered")
	}
}

// TestExtensionRegistry_RegisterAndTrigger 测试回调注册和触发
func TestExtensionRegistry_RegisterAndTrigger(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	var triggered bool
	var mu sync.Mutex
	reg.Register(GatewayBeforeChatRequest, func(ctx context.Context, data map[string]any) any {
		mu.Lock()
		triggered = true
		mu.Unlock()
		return nil
	}, 100)

	ctx := context.Background()
	reg.Trigger(ctx, GatewayBeforeChatRequest, map[string]any{"test": true})

	mu.Lock()
	if !triggered {
		t.Error("callback was not triggered")
	}
	mu.Unlock()
}

// TestExtensionRegistry_Trigger_无上下文 测试 trigger 传 nil context 不触发回调
func TestExtensionRegistry_Trigger_无上下文(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	var triggered bool
	reg.Register(GatewayStarted, func(ctx context.Context, data map[string]any) any {
		triggered = true
		return nil
	}, 100)

	// nil context 不触发
	reg.Trigger(nil, GatewayStarted, nil)
	if triggered {
		t.Error("callback should not trigger with nil context")
	}
}

// TestExtensionRegistry_Config 测试 Config 属性
func TestExtensionRegistry_Config(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{"key": "value"}, nil)
	defer ResetInstance()

	cfg := reg.Config()
	if cfg.Config["key"] != "value" {
		t.Errorf("Config[key] = %v, want %q", cfg.Config["key"], "value")
	}
}

// TestExtensionRegistry_Unregister 测试回调注销
func TestExtensionRegistry_Unregister(t *testing.T) {
	ResetInstance()
	fw := callback.NewCallbackFramework()
	reg := CreateInstance(fw, map[string]any{}, nil)
	defer ResetInstance()

	var callCount int
	handler := func(ctx context.Context, data map[string]any) any {
		callCount++
		return nil
	}
	reg.Register(GatewayStarted, handler, 100)
	reg.Unregister(GatewayStarted, handler)

	reg.Trigger(context.Background(), GatewayStarted, nil)
	if callCount != 0 {
		t.Errorf("callCount = %d, want 0 after unregister", callCount)
	}
}

// testAgentServerClientExt 测试用的 AgentServerClientExtension 实现
type testAgentServerClientExt struct {
	client transport.AgentTransport
}

func (e *testAgentServerClientExt) Initialize(ctx context.Context, config *ExtensionConfig) error { return nil }
func (e *testAgentServerClientExt) Shutdown(ctx context.Context) error                          { return nil }
func (e *testAgentServerClientExt) Metadata() *ExtensionMetadata                                { return nil }
func (e *testAgentServerClientExt) SetExtensionDir(path string)                                  {}
func (e *testAgentServerClientExt) GetClient() transport.AgentTransport                          { return e.client }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestExtensionRegistry -v`
Expected: FAIL — ExtensionRegistry 未定义

- [ ] **Step 3: Write implementation**

创建 `internal/swarm/extensions/registry.go`:

```go
package extensions

import (
	"context"
	"fmt"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/callback"
	"github.com/uapclaw/uapclaw-go/internal/swarm/transport"
	sdk "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/sdk"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionRegistry 扩展注册中心单例，对齐 Python ExtensionRegistry
// 回调机制复用 callback.CallbackFramework.OnCustom/TriggerCustom/OffCustom
type ExtensionRegistry struct {
	mu                   sync.RWMutex
	callbackFramework    *callback.CallbackFramework
	config               *ExtensionConfig
	agentServerClientExt sdk.AgentServerClientExtension
	// cryptoUtil 加解密扩展，⤵️ 10.5.10 延后实现
	cryptoUtil sdk.CryptoUtility
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	registryInstance *ExtensionRegistry
	registryOnce     sync.Once
	registryMu       sync.RWMutex
)

// ──────────────────────────── 导出函数 ────────────────────────────

// GetInstance 获取 ExtensionRegistry 单例，对齐 Python ExtensionRegistry.get_instance()
// 未初始化时返回 nil，应使用 GetInstanceErr() 获取错误信息
func GetInstance() *ExtensionRegistry {
	registryMu.RLock()
	defer registryMu.RUnlock()
	return registryInstance
}

// GetInstanceErr 获取 ExtensionRegistry 单例，带错误返回
// 对齐 Python ExtensionRegistry.get_instance() 的 RuntimeError 行为
func GetInstanceErr() (*ExtensionRegistry, error) {
	registryMu.RLock()
	defer registryMu.RUnlock()
	if registryInstance == nil {
		return nil, fmt.Errorf("ExtensionRegistry 尚未初始化，请先调用 CreateInstance()")
	}
	return registryInstance, nil
}

// CreateInstance 创建 ExtensionRegistry 单例，对齐 Python ExtensionRegistry.create_instance()
// 已存在时返回 nil（不 panic，通过返回值传递错误）
func CreateInstance(framework *callback.CallbackFramework, config map[string]any, logger any) *ExtensionRegistry {
	registryMu.Lock()
	defer registryMu.Unlock()
	if registryInstance != nil {
		return nil // 对齐 Python: 已存在时 raise RuntimeError
	}
	registryInstance = &ExtensionRegistry{
		callbackFramework: framework,
		config:            &ExtensionConfig{Config: config, Logger: logger},
	}
	return registryInstance
}

// ResetInstance 重置 ExtensionRegistry 单例为 nil，对齐 Python ExtensionRegistry.reset_instance()
func ResetInstance() {
	registryMu.Lock()
	defer registryMu.Unlock()
	registryInstance = nil
}

// RegisterAgentServerClient 注册 AgentServerClient 扩展，对齐 Python register_agent_server_client(ext)
func (r *ExtensionRegistry) RegisterAgentServerClient(ext sdk.AgentServerClientExtension) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.agentServerClientExt = ext
}

// RegisterCryptoUtility 注册 CryptoUtility 扩展，对齐 Python register_crypto_utility(ext)
// ⤵️ 10.5.10 延后：当前 cryptoUtil 字段为 nil
func (r *ExtensionRegistry) RegisterCryptoUtility(ext sdk.CryptoUtility) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.cryptoUtil = ext
}

// GetAgentServerClientExtension 获取 AgentServerClient 扩展实例，
// 对齐 Python get_agent_server_client_extension()
func (r *ExtensionRegistry) GetAgentServerClientExtension() sdk.AgentServerClientExtension {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.agentServerClientExt
}

// GetAgentServerClient 获取底层 AgentTransport 客户端，
// 对齐 Python get_agent_server_client()
// Python 返回 AgentServerClient，Go 返回 transport.AgentTransport（对齐规则 6）
func (r *ExtensionRegistry) GetAgentServerClient() transport.AgentTransport {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.agentServerClientExt == nil {
		return nil
	}
	return r.agentServerClientExt.GetClient()
}

// GetCryptoUtilityExtension 获取 CryptoUtility 扩展实例，
// 对齐 Python get_crypto_utility_extension()
// ⤵️ 10.5.10 延后：当前始终返回 nil
func (r *ExtensionRegistry) GetCryptoUtilityExtension() sdk.CryptoUtility {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.cryptoUtil
}

// GetCryptoProvider 获取 CryptoProvider，对齐 Python get_crypto_provider()
// ⤵️ 10.5.10 延后：当前始终返回 nil
func (r *ExtensionRegistry) GetCryptoProvider() any {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if r.cryptoUtil == nil {
		return nil
	}
	// ⤵️ 10.5.10: 调 cryptoUtil.GetCrypto() 返回 CryptoProvider
	return nil
}

// Register 注册回调到 CallbackFramework，对齐 Python register(event, handler, priority=100)
// 内部调 callbackFramework.OnCustom(event, fn, WithPriority(priority))
func (r *ExtensionRegistry) Register(event string, handler callback.CustomCallbackFunc, priority int) {
	r.callbackFramework.OnCustom(event, handler, callback.WithPriority(priority))
}

// Unregister 注销回调，对齐 Python unregister(event, handler)
// 内部调 callbackFramework.OffCustom(event, fn)
func (r *ExtensionRegistry) Unregister(event string, handler callback.CustomCallbackFunc) {
	r.callbackFramework.OffCustom(event, handler)
}

// Trigger 触发事件，对齐 Python trigger(event, context)
// 内部调 callbackFramework.TriggerCustom(ctx, event, data)
// nil context 不触发（对齐 CallbackFramework 行为）
func (r *ExtensionRegistry) Trigger(ctx context.Context, event string, data map[string]any) []any {
	return r.callbackFramework.TriggerCustom(ctx, event, data)
}

// Config 获取 ExtensionConfig，对齐 Python ExtensionRegistry.config @property
func (r *ExtensionRegistry) Config() *ExtensionConfig {
	return r.config
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -run TestExtensionRegistry -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/swarm/extensions/registry.go internal/swarm/extensions/registry_test.go
git commit -m "feat(10.5.6): 添加 ExtensionRegistry 单例 + 回调触发（复用 CallbackFramework），对齐 Python extensions/registry.py"
```

---

### Task 7: 延后 stub 文件 + 回填注释 (10.5.7~10.5.10)

**Files:**
- Create: `internal/swarm/extensions/loader.go`
- Create: `internal/swarm/extensions/manager.go`
- Create: `internal/swarm/extensions/callback_compat.go`
- Create: `internal/swarm/extensions/sdk/crypto_utility.go`

- [ ] **Step 1: Write stub files**

创建 `internal/swarm/extensions/loader.go`:

```go
package extensions

import (
	"context"
	"fmt"

	sdk "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/sdk"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionLoader 扩展加载器，对齐 Python ExtensionLoader
// ⤵️ 10.5.7 延后实现：Go 插件加载机制待定（Python 用 importlib 动态加载 extension.py）
// 当前仅定义接口占位，方法返回空结果或错误
type ExtensionLoader struct {
	registry *ExtensionRegistry
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionLoader 创建 ExtensionLoader，对齐 Python ExtensionLoader(registry)
func NewExtensionLoader(registry *ExtensionRegistry) *ExtensionLoader {
	return &ExtensionLoader{registry: registry}
}

// AddSearchPath 添加搜索路径，对齐 Python ExtensionLoader.add_search_path(path)
// ⤵️ 10.5.7 延后：当前空实现
func (l *ExtensionLoader) AddSearchPath(path string) {}

// DiscoverExtensionRoots 发现扩展目录，对齐 Python ExtensionLoader.discover_extension_roots()
// ⤵️ 10.5.7 延后：当前返回空列表
func (l *ExtensionLoader) DiscoverExtensionRoots() []string { return nil }

// LoadExtension 加载单个扩展，对齐 Python ExtensionLoader.load_extension(root)
// ⤵️ 10.5.7 延后：当前返回错误
func (l *ExtensionLoader) LoadExtension(ctx context.Context, root string) (sdk.BaseExtension, error) {
	return nil, fmt.Errorf("ExtensionLoader 尚未实现，⤵️ 10.5.7（Go 插件加载机制待定）")
}
```

创建 `internal/swarm/extensions/manager.go`:

```go
package extensions

import (
	"context"
	"fmt"

	sdk "github.com/uapclaw/uapclaw-go/internal/swarm/extensions/sdk"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExtensionManager 扩展生命周期管理器，对齐 Python ExtensionManager
// ⤵️ 10.5.8 延后实现：依赖 ExtensionLoader
// 当前仅定义接口占位，方法返回空结果或错误
type ExtensionManager struct {
	registry *ExtensionRegistry
	loader   *ExtensionLoader
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewExtensionManager 创建 ExtensionManager，对齐 Python ExtensionManager(registry)
func NewExtensionManager(registry *ExtensionRegistry) *ExtensionManager {
	return &ExtensionManager{
		registry: registry,
		loader:   NewExtensionLoader(registry),
	}
}

// LoadAllExtensions 加载所有扩展，对齐 Python ExtensionManager.load_all_extensions()
// ⤵️ 10.5.8 延后：当前返回错误
func (m *ExtensionManager) LoadAllExtensions(ctx context.Context) error {
	return fmt.Errorf("ExtensionManager.LoadAllExtensions 尚未实现，⤵️ 10.5.8（依赖 ExtensionLoader）")
}

// ShutdownAllExtensions 关闭所有扩展，对齐 Python ExtensionManager.shutdown_all_extensions()
// ⤵️ 10.5.8 延后：当前空实现
func (m *ExtensionManager) ShutdownAllExtensions(ctx context.Context) error { return nil }

// ListExtensions 列出已加载扩展，对齐 Python ExtensionManager.list_extensions()
// ⤵️ 10.5.8 延后：当前返回空列表
func (m *ExtensionManager) ListExtensions() []map[string]string { return nil }
```

创建 `internal/swarm/extensions/callback_compat.go`:

```go
// 本文件对应 Python jiuwenswarm/extensions/callback_compat.py。
//
// Python 的 callback_compat.py 是为兼容 openjiuwen <0.1.9 的
// AsyncCallbackFramework.unregister_sync API 缺口而创建的。
//
// Go 项目中已有 callback.CallbackFramework 提供完整的
// OnCustom/TriggerCustom/OffCustom 方法，对应 Python 的
// register_sync/trigger/unregister_sync API，无需额外兼容层。
//
// 此文件标注 ⤴️ 10.5.9 已覆盖，不需要 Go 实现。
package extensions
```

创建 `internal/swarm/extensions/sdk/crypto_utility.go`:

```go
package sdk

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/swarm/extensions"
)

// ──────────────────────────── 结构体 ────────────────────────────

// CryptoUtility 加解密扩展接口，对齐 Python CryptoUtility
// ⤵️ 10.5.10 延后实现：CryptoProvider 接口待定义
type CryptoUtility interface {
	BaseExtension
	// GetCrypto 返回实际执行 encrypt/decrypt 的实例，对齐 Python get_crypto()
	// ⤵️ 10.5.10 延后：返回类型待定为 CryptoProvider 接口
	GetCrypto() any
}

// CryptoUtilityStub CryptoUtility 的 stub 实现，⤵️ 10.5.10 延后
type CryptoUtilityStub struct {
	BaseExtensionImpl
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetCrypto 返回 nil，⤵️ 10.5.10 延后实现后返回 CryptoProvider
func (c *CryptoUtilityStub) GetCrypto() any { return nil }

// Initialize 空实现，⤵️ 10.5.10 延后
func (c *CryptoUtilityStub) Initialize(ctx context.Context, config *extensions.ExtensionConfig) error { return nil }

// Shutdown 空实现，对齐 Python CryptoUtility.shutdown()
func (c *CryptoUtilityStub) Shutdown(ctx context.Context) error { return nil }
```

- [ ] **Step 2: Run full test suite to verify no breakage**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... -v`
Expected: PASS (all existing tests still pass, stub files compile correctly)

- [ ] **Step 3: Commit**

```bash
git add internal/swarm/extensions/loader.go internal/swarm/extensions/manager.go internal/swarm/extensions/callback_compat.go internal/swarm/extensions/sdk/crypto_utility.go
git commit -m "feat(10.5.7-10): 添加延后 stub 文件（Loader/Manager/CryptoUtility）+ callback_compat 注释覆盖"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md 拆分 + 状态标记

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` (line 685~686)

- [ ] **Step 1: Read current 10.5 section**

Run: `grep -n "10.5" /home/opensource/uapclaw-gateway/IMPLEMENTATION_PLAN.md`

Expected output shows line 685-686 with the current merged row.

- [ ] **Step 2: Replace the merged row with 10 individual rows**

Replace:
```
| **10.5 扩展系统** | — | | | |
| 10.5.1-10 | ☐ | 扩展框架 | BaseExtension/Registry/Manager/Hooks/Loader/Types | `jiuwenswarm/extensions/` |
```

With:
```
| **10.5 扩展系统** | — | | | |
| 10.5.1 | ✅ | 数据类型 | ExtensionMetadata + ExtensionConfig | `jiuwenswarm/extensions/types.py` |
| 10.5.2 | ✅ | 钩子事件常量 | GatewayHookEvents + AgentServerHookEvents | `jiuwenswarm/extensions/hook_event.py` |
| 10.5.3 | ✅ | 钩子上下文结构 | 4 种 HookContext（Memory/GatewayChat/AgentServerChat/SystemPrompt） | `jiuwenswarm/extensions/hooks_context.py` |
| 10.5.4 | ✅ | BaseExtension 基类 | BaseExtension 接口 + BaseExtensionImpl 默认实现 | `jiuwenswarm/extensions/sdk/base.py` |
| 10.5.5 | ✅ | AgentServerClientExtension | GetClient() 返回 AgentTransport | `jiuwenswarm/extensions/sdk/agent_server_client.py` |
| 10.5.6 | ✅ | ExtensionRegistry | 单例 + 回调触发（复用 CallbackFramework） | `jiuwenswarm/extensions/registry.py` |
| 10.5.7 | ☐ ⤵️ | ExtensionLoader | 延后，Go 插件加载机制待定 | `jiuwenswarm/extensions/loader.py` |
| 10.5.8 | ☐ ⤵️ | ExtensionManager | 延后，依赖 loader | `jiuwenswarm/extensions/manager.py` |
| 10.5.9 | ⤴️ | CallbackCompat | Go 已有 CallbackFramework，无需额外实现 | `jiuwenswarm/extensions/callback_compat.py` |
| 10.5.10 | ☐ ⤵️ | CryptoUtility | 延后，CryptoProvider 接口待定义 | `jiuwenswarm/extensions/sdk/crypto_utility.py` |
```

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 10.5 拆分为 10 个子步骤，标注最小子集 ✅ 和延后 ⤵️"
```

---

### Task 9: 回填现有 TODO 注释闭环

**Files:**
- Modify: `cmd/uapclaw/cmd.go:197-199`
- Modify: `internal/swarm/server/handle_envelope.go:63-67`
- Modify: `internal/swarm/server/runtime/uapclaw.go:152`

- [ ] **Step 1: Update cmd.go 扩展系统初始化 TODO**

Replace the TODO comment at cmd.go lines 197-199 with actual ExtensionRegistry initialization code:

```go
	// 6. 初始化扩展系统（对齐 Python: app_gateway.py L814-822, app_agentserver.py L131-141）
	// 最小子集已实现：ExtensionRegistry.CreateInstance + 回调触发机制
	// ⤵️ 10.5.8: ExtensionManager.LoadAllExtensions 待 loader 实现后回填
	fw := callback.GetCallbackFramework()
	extRegistry := extensions.CreateInstance(fw, map[string]any{}, nil)
	if extRegistry == nil {
		logger.Error(logComponent).Msg("ExtensionRegistry 初始化失败（可能已存在实例）")
	}
	// ⤵️ 10.5.8: extManager := extensions.NewExtensionManager(extRegistry)
	// ⤵️ 10.5.8: err := extManager.LoadAllExtensions(ctx)
```

注意：需要添加 imports `callback` 和 `extensions`。

- [ ] **Step 2: Update handle_envelope.go before_chat_request 钩子**

Replace the TODO comment at handle_envelope.go lines 63-67 with actual trigger code:

```go
	// 3. before_chat_request 钩子（对齐 Python: agent_ws_server.py _trigger_before_chat_request_hook()）
	// 当 req_method 为 CHAT_SEND/CHAT_RESUME/CHAT_ANSWER 时触发
	extReg, err := extensions.GetInstanceErr()
	if err == nil && extReg != nil {
		hookCtx := &extensions.AgentServerChatHookContext{
			RequestID:  request.RequestID,
			ChannelID:  request.ChannelID,
			SessionID:  nil, // ⤵️ 后续从 request 中提取
			ReqMethod:  &request.ReqMethod,
			Params:     request.Params,
		}
		extReg.Trigger(ctx, extensions.AgentServerBeforeChatRequest, hookCtx.ToMap())
	}
```

注意：需要添加 import `extensions`。

- [ ] **Step 3: Update uapclaw.go 云端记忆对话前钩子**

Replace the TODO comment at runtime/uapclaw.go line 152:

```go
	// 云端记忆对话前钩子（对齐 Python: interface.py MEMORY_BEFORE_CHAT trigger）
	extReg, extErr := extensions.GetInstanceErr()
	if extErr == nil && extReg != nil {
		memCtx := &extensions.MemoryHookContext{
			SessionID:    request.SessionID,
			RequestID:    request.RequestID,
			ChannelID:    &request.ChannelID,
			AgentName:    uc.agentName,
			WorkspaceDir: uc.workspaceDir,
			Extra:        request.Params,
		}
		extReg.Trigger(ctx, extensions.AgentServerMemoryBeforeChat, memCtx.ToMap())
		// ⤵️ 10.3.2: 从 memCtx.MemoryBlocks 拼接记忆注入 system prompt
	}
```

注意：需要添加 import `extensions`。

- [ ] **Step 4: Run build to verify compilation**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 5: Run full test suite**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/swarm/extensions/... ./cmd/... -v -count=1`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add cmd/uapclaw/cmd.go internal/swarm/server/handle_envelope.go internal/swarm/server/runtime/uapclaw.go
git commit -m "feat: 回填现有 TODO 注释，闭环 ExtensionRegistry 初始化 + BEFORE_CHAT_REQUEST trigger + MEMORY_BEFORE_CHAT trigger"
```

---

### Task 10: 最终验证 — 全量编译 + 测试

**Files:**
- 无新增/修改，仅验证

- [ ] **Step 1: Run full project build**

Run: `cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: BUILD SUCCESS

- [ ] **Step 2: Run full project test (excluding integration/llm/e2e)**

Run: `cd /home/opensource/uapclaw-gateway && go test -cover -tags=!integration,!llm,!e2e ./internal/swarm/extensions/... ./internal/swarm/extensions/sdk/... -v`
Expected: PASS, coverage ≥ 85%

- [ ] **Step 3: Verify IMPLEMENTATION_PLAN.md updates are correct**

Read the updated section in IMPLEMENTATION_PLAN.md to confirm all 10.5.x rows are correctly updated.

---

## Self-Review

**1. Spec coverage check:**
- 10.5.1 types → Task 1 ✅
- 10.5.2 hook_event → Task 2 ✅
- 10.5.3 hooks_context → Task 3 ✅
- 10.5.4 sdk/base → Task 4 ✅
- 10.5.5 sdk/agent_server_client → Task 5 ✅
- 10.5.6 registry → Task 6 ✅
- 10.5.7 loader stub → Task 7 ✅
- 10.5.8 manager stub → Task 7 ✅
- 10.5.9 callback_compat → Task 7 ✅
- 10.5.10 crypto_utility stub → Task 7 ✅
- IMPLEMENTATION_PLAN update → Task 8 ✅
- 回填点闭环 → Task 9 ✅
- 最终验证 → Task 10 ✅

**2. Placeholder scan:**
- No TBD/TODO/fill-in-later patterns in implementation steps
-延后部分 clearly marked with ⤵️ and specific section references
- All code blocks contain complete implementation

**3. Type consistency:**
- `BaseExtension` interface defined in Task 4, used in Tasks 5, 6, 7 consistently
- `AgentServerClientExtension` interface defined in Task 5, `GetClient()` returns `transport.AgentTransport` consistently
- `ExtensionMetadata` defined in Task 1, used in Tasks 4, 5 consistently
- `ExtensionConfig` defined in Task 1, used in Tasks 4, 5, 6, 9 consistently
- Hook event constants format `scope:event_name` consistent across Task 2 and Task 9
- `CustomCallbackFunc` type from `callback` package used consistently in Tasks 6, 9
