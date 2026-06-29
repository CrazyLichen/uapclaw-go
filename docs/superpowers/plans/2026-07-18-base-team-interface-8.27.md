# 8.27 BaseTeam 接口实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 定义 BaseTeam Go 接口及其配套类型（TeamOption、TeamCard 占位、TeamConfig 占位、AgentTeamProvider），并回填 AgentTeamMgr/ResourceManager 中的 ⤵️ 预留标记。

**Architecture:** 在 `internal/agentcore/multi_agent/` 包下定义纯 Go 接口 BaseTeam，完整对齐 Python BaseTeam 的所有方法。签名风格对齐已有 Agent 接口（ctx + inputs + opts），核心参数显式、可选参数用 TeamOption 传递。回填 AgentTeamMgr 改用 AbstractManager[BaseTeam] Provider 模式。

**Tech Stack:** Go 1.22+, 项目已有 AbstractManager 泛型、exception 包、logger 包、stream 包

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|---------|------|
| 创建 | `internal/agentcore/multi_agent/doc.go` | 包文档 |
| 创建 | `internal/agentcore/multi_agent/team.go` | BaseTeam 接口 + AgentTeamProvider + TeamCard/TeamConfig 占位 |
| 创建 | `internal/agentcore/multi_agent/team_option.go` | TeamOptions + TeamOption + WithXxx |
| 创建 | `internal/agentcore/multi_agent/team_test.go` | 编译时接口检查测试 |
| 创建 | `internal/agentcore/multi_agent/team_option_test.go` | TeamOption 测试 |
| 修改 | `internal/agentcore/runner/resources_manager/base.go` | 新增 AgentTeamEntry |
| 修改 | `internal/agentcore/runner/resources_manager/agent_team_manager.go` | 回填：改用 AbstractManager[BaseTeam] |
| 修改 | `internal/agentcore/runner/resources_manager/agent_team_manager_test.go` | 回填后的测试更新 |
| 修改 | `internal/agentcore/runner/resources_manager/resource_manager.go` | 回填：team 相关 dispatch/case 补全 |

---

### Task 1: 创建 multi_agent 包文档

**Files:**
- Create: `internal/agentcore/multi_agent/doc.go`

- [ ] **Step 1: 创建 doc.go**

```go
// Package multi_agent 提供多 Agent 团队的核心抽象与运行时基础设施。
//
// 定义 BaseTeam 接口作为多 Agent 团队体系的根基契约，
// 具体团队实现（HandoffTeam、HierarchicalTeam）均实现此接口。
// 团队内部的 Agent 通信通过 TeamRuntime（8.30）的 P2P 和 Pub-Sub 消息机制完成。
//
// 文件目录：
//
//	multi_agent/
//	├── doc.go            # 包文档
//	├── team.go           # BaseTeam 接口 + AgentTeamProvider + TeamCard/TeamConfig 占位
//	└── team_option.go    # TeamOptions 结构体 + TeamOption 函数类型 + WithXxx
//
// 对应 Python 代码：openjiuwen/core/multi_agent/
package multi_agent
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/...`
Expected: PASS（空包可以编译）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/doc.go
git commit -m "feat(multi_agent): 添加包文档 doc.go"
```

---

### Task 2: 创建 team.go — BaseTeam 接口 + 占位类型

**Files:**
- Create: `internal/agentcore/multi_agent/team.go`

- [ ] **Step 1: 创建 team.go**

```go
package multi_agent

import (
	"context"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamCard 团队身份卡片（最小占位，8.28 完整实现）。
//
// 对应 Python: openjiuwen/core/multi_agent/schema/team_card.py (TeamCard)
// Python 继承 BaseCard: id/name/description + agent_cards/topic/version/tags
type TeamCard struct {
	schema.BaseCard
}

// TeamConfig 团队运行时配置（最小占位，8.28 完整实现）。
//
// 对应 Python: openjiuwen/core/multi_agent/config.py (TeamConfig)
// Python 字段: max_agents=10, max_concurrent_messages=100, message_timeout=30.0
type TeamConfig struct{}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// BaseTeam 多 Agent 团队核心行为契约。
//
// 对应 Python: openjiuwen/core/multi_agent/team.py (BaseTeam)
//
// 设计原则：
//   - Card 是必填项（定义团队身份）
//   - Config 是可选项（定义团队运行时行为）
//   - BaseTeam 与 BaseAgent 是平行的两个体系，不继承 BaseAgent
//   - Invoke/Stream 是对整个团队的调用，由子类实现
//   - AddAgent/RemoveAgent/Send/Publish/Subscribe 等方法在具体团队中实现
type BaseTeam interface {
	// ── 核心执行 ──

	// Invoke 非流式调用团队。
	//
	// 对应 Python: BaseTeam.invoke(message, session)
	Invoke(ctx context.Context, inputs map[string]any, opts ...TeamOption) (any, error)

	// Stream 流式调用团队。
	//
	// 对应 Python: BaseTeam.stream(message, session) -> AsyncIterator
	Stream(ctx context.Context, inputs map[string]any, opts ...TeamOption) (<-chan stream.Schema, error)

	// ── Agent 管理 ──

	// AddAgent 向团队注册 Agent。
	//
	// 对应 Python: BaseTeam.add_agent(card, provider) -> self
	AddAgent(ctx context.Context, card *agentschema.AgentCard, provider resources_manager.AgentProvider) error

	// RemoveAgent 从团队注销 Agent。
	//
	// 对应 Python: BaseTeam.remove_agent(agent) -> self
	RemoveAgent(ctx context.Context, agentID string) error

	// ── 消息通信 ──

	// Send 点对点消息发送。
	//
	// 对应 Python: BaseTeam.send(message, recipient, sender, session_id, timeout)
	Send(ctx context.Context, message map[string]any, recipient string, sender string, opts ...TeamOption) (any, error)

	// Publish 发布消息到主题。
	//
	// 对应 Python: BaseTeam.publish(message, topic_id, sender, session_id)
	Publish(ctx context.Context, message map[string]any, topicID string, sender string, opts ...TeamOption) error

	// Subscribe 订阅主题。
	//
	// 对应 Python: BaseTeam.subscribe(agent_id, topic)
	Subscribe(ctx context.Context, agentID string, topic string) error

	// Unsubscribe 取消订阅。
	//
	// 对应 Python: BaseTeam.unsubscribe(agent_id, topic)
	Unsubscribe(ctx context.Context, agentID string, topic string) error

	// ── 配置 ──

	// Configure 配置团队。
	//
	// 对应 Python: BaseTeam.configure(config) -> self
	Configure(ctx context.Context, config TeamConfig) error

	// ── 查询 ──

	// GetAgentCard 获取 Agent 卡片。
	//
	// 对应 Python: BaseTeam.get_agent_card(agent_id)
	GetAgentCard(agentID string) (*agentschema.AgentCard, error)

	// GetAgentCount 获取 Agent 数量。
	//
	// 对应 Python: BaseTeam.get_agent_count()
	GetAgentCount() int

	// ListAgents 列出所有 Agent ID。
	//
	// 对应 Python: BaseTeam.list_agents()
	ListAgents() []string

	// ── 访问器 ──

	// Card 返回团队身份卡片。
	//
	// 对应 Python: BaseTeam.card 属性
	Card() *TeamCard

	// Config 返回团队配置。
	//
	// 对应 Python: BaseTeam.config 属性
	Config() TeamConfig
}

// AgentTeamProvider 团队资源提供者函数，接受 TeamCard 返回 BaseTeam 实例。
//
// 对应 Python: AgentTeamProvider = Callable[[TeamCard], Awaitable[BaseTeam]] | Callable[[TeamCard], BaseTeam]
// 用于延迟加载团队资源，注册时传入工厂函数而非实例。
type AgentTeamProvider func(ctx context.Context, card *TeamCard) (BaseTeam, error)

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team.go
git commit -m "feat(multi_agent): 添加 BaseTeam 接口 + TeamCard/TeamConfig 占位 + AgentTeamProvider"
```

---

### Task 3: 创建 team_option.go — TeamOptions + TeamOption

**Files:**
- Create: `internal/agentcore/multi_agent/team_option.go`

- [ ] **Step 1: 创建 team_option.go**

```go
package multi_agent

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamOptions 团队调用选项。
//
// 对应 Python: BaseTeam 各方法的可选参数（session、session_id、timeout、stream_modes）
type TeamOptions struct {
	// Session 团队会话（可选）
	//
	// 对应 Python: invoke(message, session) 的 session 参数
	// ⤵️ 8.30 TeamSession 实现后替换为具体类型
	Session any
	// SessionID 会话标识（可选）
	//
	// 对应 Python: send/publish 的 session_id 参数
	SessionID string
	// Timeout 响应超时秒数（可选）
	//
	// 对应 Python: send 的 timeout 参数
	Timeout float64
	// StreamModes 流式输出模式（可选）
	//
	// 对应 Python: stream 的 stream_modes 参数
	StreamModes []stream.StreamMode
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TeamOption 团队调用选项函数。
type TeamOption func(*TeamOptions)

// WithTeamSession 设置团队会话。
//
// ⤵️ 8.30 TeamSession 实现后参数类型从 any 替换为具体类型。
func WithTeamSession(sess any) TeamOption {
	return func(o *TeamOptions) { o.Session = sess }
}

// WithTeamSessionID 设置会话标识。
func WithTeamSessionID(sessionID string) TeamOption {
	return func(o *TeamOptions) { o.SessionID = sessionID }
}

// WithTeamTimeout 设置响应超时。
func WithTeamTimeout(timeout float64) TeamOption {
	return func(o *TeamOptions) { o.Timeout = timeout }
}

// WithTeamStreamModes 设置流式输出模式。
func WithTeamStreamModes(modes []stream.StreamMode) TeamOption {
	return func(o *TeamOptions) { o.StreamModes = modes }
}

// NewTeamOptions 从选项列表构建 TeamOptions。
func NewTeamOptions(opts ...TeamOption) *TeamOptions {
	o := &TeamOptions{}
	for _, opt := range opts {
		opt(o)
	}
	return o
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/multi_agent/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team_option.go
git commit -m "feat(multi_agent): 添加 TeamOptions + TeamOption + WithXxx 便捷函数"
```

---

### Task 4: 创建 team_test.go — 编译时接口检查

**Files:**
- Create: `internal/agentcore/multi_agent/team_test.go`

- [ ] **Step 1: 创建 team_test.go**

```go
package multi_agent

import (
	"context"
	"testing"

	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/runner/resources_manager"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// ──────────────────────────── 结构体 ────────────────────────────

// stubTeam 用于编译时检查 BaseTeam 接口满足的桩实现。
type stubTeam struct {
	card   *TeamCard
	config TeamConfig
}

// ──────────────────────────── 枚 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────

// 编译时检查 stubTeam 满足 BaseTeam 接口。
var _ BaseTeam = (*stubTeam)(nil)

func (t *stubTeam) Invoke(_ context.Context, _ map[string]any, _ ...TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Stream(_ context.Context, _ map[string]any, _ ...TeamOption) (<-chan stream.Schema, error) {
	return nil, nil
}

func (t *stubTeam) AddAgent(_ context.Context, _ *agentschema.AgentCard, _ resources_manager.AgentProvider) error {
	return nil
}

func (t *stubTeam) RemoveAgent(_ context.Context, _ string) error {
	return nil
}

func (t *stubTeam) Send(_ context.Context, _ map[string]any, _ string, _ string, _ ...TeamOption) (any, error) {
	return nil, nil
}

func (t *stubTeam) Publish(_ context.Context, _ map[string]any, _ string, _ string, _ ...TeamOption) error {
	return nil
}

func (t *stubTeam) Subscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Unsubscribe(_ context.Context, _ string, _ string) error {
	return nil
}

func (t *stubTeam) Configure(_ context.Context, _ TeamConfig) error {
	return nil
}

func (t *stubTeam) GetAgentCard(_ string) (*agentschema.AgentCard, error) {
	return nil, nil
}

func (t *stubTeam) GetAgentCount() int {
	return 0
}

func (t *stubTeam) ListAgents() []string {
	return nil
}

func (t *stubTeam) Card() *TeamCard {
	return t.card
}

func (t *stubTeam) Config() TeamConfig {
	return t.config
}

// TestBaseTeam_编译时接口检查 验证 stubTeam 满足 BaseTeam 接口。
func TestBaseTeam_编译时接口检查(t *testing.T) {
	card := &TeamCard{BaseCard: schema.BaseCard{ID: "test-team", Name: "test"}}
	team := &stubTeam{card: card, config: TeamConfig{}}

	// 基本调用验证
	_ = team.Card()
	_ = team.Config()
	_ = team.GetAgentCount()
	_ = team.ListAgents()
}
```

- [ ] **Step 2: 修复 import（需要添加 stream 包引用）**

在 team_test.go 的 import 中添加：

```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -v -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/multi_agent/team_test.go
git commit -m "test(multi_agent): 添加 BaseTeam 编译时接口检查测试"
```

---

### Task 5: 创建 team_option_test.go — TeamOption 测试

**Files:**
- Create: `internal/agentcore/multi_agent/team_option_test.go`

- [ ] **Step 1: 创建 team_option_test.go**

```go
package multi_agent

import (
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/session/stream"
)

// TestNewTeamOptions_空选项 测试无选项时默认值。
func TestNewTeamOptions_空选项(t *testing.T) {
	opts := NewTeamOptions()
	if opts.Session != nil {
		t.Error("Session 应为 nil")
	}
	if opts.SessionID != "" {
		t.Error("SessionID 应为空")
	}
	if opts.Timeout != 0 {
		t.Error("Timeout 应为 0")
	}
	if opts.StreamModes != nil {
		t.Error("StreamModes 应为 nil")
	}
}

// TestWithTeamSession_设置会话 测试 WithTeamSession 选项。
func TestWithTeamSession_设置会话(t *testing.T) {
	sess := "test-session"
	opts := NewTeamOptions(WithTeamSession(sess))
	if opts.Session != sess {
		t.Errorf("Session 期望 %v, 实际 %v", sess, opts.Session)
	}
}

// TestWithTeamSessionID_设置会话标识 测试 WithTeamSessionID 选项。
func TestWithTeamSessionID_设置会话标识(t *testing.T) {
	opts := NewTeamOptions(WithTeamSessionID("sess-123"))
	if opts.SessionID != "sess-123" {
		t.Errorf("SessionID 期望 sess-123, 实际 %s", opts.SessionID)
	}
}

// TestWithTeamTimeout_设置超时 测试 WithTeamTimeout 选项。
func TestWithTeamTimeout_设置超时(t *testing.T) {
	opts := NewTeamOptions(WithTeamTimeout(30.0))
	if opts.Timeout != 30.0 {
		t.Errorf("Timeout 期望 30.0, 实际 %f", opts.Timeout)
	}
}

// TestWithTeamStreamModes_设置流模式 测试 WithTeamStreamModes 选项。
func TestWithTeamStreamModes_设置流模式(t *testing.T) {
	modes := []stream.StreamMode{stream.StreamModeOutput}
	opts := NewTeamOptions(WithTeamStreamModes(modes))
	if len(opts.StreamModes) != 1 || opts.StreamModes[0] != stream.StreamModeOutput {
		t.Errorf("StreamModes 期望 [StreamModeOutput], 实际 %v", opts.StreamModes)
	}
}

// TestNewTeamOptions_多选项组合 测试多个选项组合。
func TestNewTeamOptions_多选项组合(t *testing.T) {
	opts := NewTeamOptions(
		WithTeamSessionID("sess-456"),
		WithTeamTimeout(60.0),
	)
	if opts.SessionID != "sess-456" {
		t.Errorf("SessionID 期望 sess-456, 实际 %s", opts.SessionID)
	}
	if opts.Timeout != 60.0 {
		t.Errorf("Timeout 期望 60.0, 实际 %f", opts.Timeout)
	}
}
```

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... -v -count=1`
Expected: PASS（所有 5 个测试）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/multi_agent/team_option_test.go
git commit -m "test(multi_agent): 添加 TeamOption 单元测试"
```

---

### Task 6: 回填 base.go — 新增 AgentTeamEntry

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/base.go`

- [ ] **Step 1: 在 base.go 的 AgentEntry 之后添加 AgentTeamEntry**

在 `AgentEntry` 结构体之后（第 53 行后），添加：

```go
// AgentTeamEntry AgentTeam 批量添加条目。
//
// 对应 Python: (TeamCard, AgentTeamProvider) 元组
type AgentTeamEntry struct {
	// Card 团队身份元数据
	Card *multiagent.TeamCard
	// Provider 团队提供者
	Provider multiagent.AgentTeamProvider
}
```

同时在 import 中添加：

```go
"github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
```

别名为 `multiagent`（因为 `multi_agent` 含下划线不合法）：

```go
multiagent "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/resources_manager/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/resources_manager/base.go
git commit -m "feat(resources_manager): 添加 AgentTeamEntry 结构体"
```

---

### Task 7: 回填 agent_team_manager.go — 改用 AbstractManager[BaseTeam]

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/agent_team_manager.go`

- [ ] **Step 1: 重写 agent_team_manager.go**

```go
package resources_manager

import (
	"context"

	multiagent "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// AgentTeamMgr Agent 团队资源管理器，嵌入 AbstractManager 复用 provider 注册/获取/注销能力。
//
// 对应 Python: AgentTeamMgr (openjiuwen/core/runner/resources_manager/agent_team_manager.py)
// Python 继承 AbstractManager[BaseTeam]，三个方法直接委托给父类。
type AgentTeamMgr struct {
	AbstractManager[multiagent.BaseTeam]
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgentTeamMgr 创建 Agent 团队资源管理器。
func NewAgentTeamMgr() AgentTeamMgr {
	return AgentTeamMgr{
		AbstractManager: NewAbstractManager[multiagent.BaseTeam](),
	}
}

// AddAgentTeam 注册 Agent 团队提供者。
//
// 对应 Python: AgentTeamMgr.add_agent_team(agent_team_id, agent_team) → self._register_resource_provider(...)
func (m *AgentTeamMgr) AddAgentTeam(agentTeamID string, provider multiagent.AgentTeamProvider) error {
	if agentTeamID == "" {
		return exception.BuildError(exception.StatusResourceIDValueInvalid,
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", "agent team id is empty"),
		)
	}
	if provider == nil {
		return exception.BuildError(exception.StatusResourceProviderInvalid,
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", "agent team provider is nil"),
		)
	}

	// 将 AgentTeamProvider 包装为 AbstractManager 所需的 func(context.Context) (T, error) 签名
	wrappedProvider := func(ctx context.Context) (multiagent.BaseTeam, error) {
		// AgentTeamProvider 签名为 func(ctx, card) → (BaseTeam, error)
		// 此处 card 为 nil，由 provider 内部自行处理
		return provider(ctx, nil)
	}

	err := m.registerProvider(agentTeamID, wrappedProvider)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_TEAM_ADD_ERROR").
			Str("agent_team_id", agentTeamID).
			Err(err).
			Msg("添加 Agent 团队失败")
		return exception.BuildError(exception.StatusResourceAddError,
			exception.WithParam("card", agentTeamID),
			exception.WithParam("reason", err.Error()),
		)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "AGENT_TEAM_ADD_SUCCESS").
		Str("agent_team_id", agentTeamID).
		Msg("添加 Agent 团队成功")
	return nil
}

// RemoveAgentTeam 注销 Agent 团队提供者，返回被注销的 provider。
//
// 对应 Python: AgentTeamMgr.remove_agent_team(agent_team_id) → self._unregister_resource_provider(...)
func (m *AgentTeamMgr) RemoveAgentTeam(agentTeamID string) (multiagent.AgentTeamProvider, error) {
	unwrapped, err := m.unregisterProvider(agentTeamID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_TEAM_REMOVE_ERROR").
			Str("agent_team_id", agentTeamID).
			Err(err).
			Msg("移除 Agent 团队失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", agentTeamID),
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", err.Error()),
		)
	}

	// 将 wrapped provider 还原为 AgentTeamProvider
	provider := func(ctx context.Context, card *multiagent.TeamCard) (multiagent.BaseTeam, error) {
		return unwrapped(ctx)
	}

	logger.Info(logger.ComponentAgentCore).
		Str("event_type", "AGENT_TEAM_REMOVE_SUCCESS").
		Str("agent_team_id", agentTeamID).
		Msg("移除 Agent 团队成功")
	return provider, nil
}

// GetAgentTeam 获取 Agent 团队实例。
//
// 对应 Python: AgentTeamMgr.get_agent_team(agent_team_id) → await self._get_resource(...)
func (m *AgentTeamMgr) GetAgentTeam(ctx context.Context, agentTeamID string) (multiagent.BaseTeam, error) {
	team, err := m.getResource(ctx, agentTeamID)
	if err != nil {
		logger.Error(logger.ComponentAgentCore).
			Str("event_type", "AGENT_TEAM_GET_ERROR").
			Str("agent_team_id", agentTeamID).
			Err(err).
			Msg("获取 Agent 团队失败")
		return nil, exception.BuildError(exception.StatusResourceGetError,
			exception.WithParam("resource_id", agentTeamID),
			exception.WithParam("resource_type", "team"),
			exception.WithParam("reason", err.Error()),
		)
	}
	return team, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/resources_manager/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/runner/resources_manager/agent_team_manager.go
git commit -m "feat(resources_manager): 回填 AgentTeamMgr 改用 AbstractManager[BaseTeam] Provider 模式"
```

---

### Task 8: 回填 resource_manager.go — team 相关方法补全

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager.go`

此任务需要修改多处，每处精确替换：

- [ ] **Step 1: 修改 AddAgentTeam 方法签名和实现（L1215-1221）**

将：
```go
// AddAgentTeam 注册 Agent 团队。
//
// 对应 Python: ResourceManager.add_agent_team(agent_team_id, provider, **kwargs)
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *ResourceMgr) AddAgentTeam(agentTeamID string, provider any, opts ...ResourceOption) error {
	return fmt.Errorf("agent team not implemented, agent_team_id=%s", agentTeamID)
}
```

替换为：
```go
// AddAgentTeam 注册 Agent 团队。
//
// 对应 Python: ResourceManager.add_agent_team(agent_team_id, provider, **kwargs)
func (m *ResourceMgr) AddAgentTeam(card *multiagent.TeamCard, provider multiagent.AgentTeamProvider, opts ...ResourceOption) error {
	if err := m.innerValidateProvider(provider, "team"); err != nil {
		return err
	}
	return m.innerAddResource(card.ID, "team", provider, card, "", "")
}
```

- [ ] **Step 2: 修改 RemoveAgentTeam 方法签名和实现（L1223-1229）**

将：
```go
// RemoveAgentTeam 注销 Agent 团队。
//
// 对应 Python: ResourceManager.remove_agent_team(agent_team_id, **kwargs)
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *ResourceMgr) RemoveAgentTeam(agentTeamIDs []string, opts ...ResourceOption) (any, error) {
	return nil, fmt.Errorf("agent team not implemented, agent_team_ids=%v", agentTeamIDs)
}
```

替换为：
```go
// RemoveAgentTeam 注销 Agent 团队。
//
// 对应 Python: ResourceManager.remove_agent_team(agent_team_id, **kwargs)
func (m *ResourceMgr) RemoveAgentTeam(agentTeamIDs []string, opts ...ResourceOption) ([]multiagent.AgentTeamProvider, error) {
	results := make([]multiagent.AgentTeamProvider, 0, len(agentTeamIDs))
	for _, id := range agentTeamIDs {
		provider, err := m.registry.AgentTeam().RemoveAgentTeam(id)
		if err != nil {
			return nil, err
		}
		results = append(results, provider)
	}
	return results, nil
}
```

- [ ] **Step 3: 修改 GetAgentTeam 方法签名和实现（L1231-1237）**

将：
```go
// GetAgentTeam 获取 Agent 团队。
//
// 对应 Python: ResourceManager.get_agent_team(agent_team_id, **kwargs)
// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
func (m *ResourceMgr) GetAgentTeam(agentTeamIDs []string, opts ...ResourceOption) (any, error) {
	return nil, fmt.Errorf("agent team not implemented, agent_team_ids=%v", agentTeamIDs)
}
```

替换为：
```go
// GetAgentTeam 获取 Agent 团队。
//
// 对应 Python: ResourceManager.get_agent_team(agent_team_id, **kwargs)
func (m *ResourceMgr) GetAgentTeam(ctx context.Context, agentTeamIDs []string, opts ...ResourceOption) ([]multiagent.BaseTeam, error) {
	results := make([]multiagent.BaseTeam, 0, len(agentTeamIDs))
	for _, id := range agentTeamIDs {
		team, err := m.registry.AgentTeam().GetAgentTeam(ctx, id)
		if err != nil {
			return nil, err
		}
		results = append(results, team)
	}
	return results, nil
}
```

- [ ] **Step 4: 修改 dispatchAdd case "team"（L1427-1430）**

将：
```go
	case "team":
		// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
		provider := resource
		return m.registry.AgentTeam().AddAgentTeam(resourceID, provider)
```

替换为：
```go
	case "team":
		provider, ok := resource.(multiagent.AgentTeamProvider)
		if !ok {
			return exception.BuildError(exception.StatusResourceProviderInvalid,
				exception.WithParam("resource_type", "team"),
				exception.WithParam("reason", "resource is not AgentTeamProvider"),
			)
		}
		return m.registry.AgentTeam().AddAgentTeam(resourceID, provider)
```

- [ ] **Step 5: 修改 dispatchGet case "team"（L1511-1513）**

将：
```go
	case "team":
		// ⤵️ 预留：等 TeamCard/BaseTeam 类型实现后回填
		return m.registry.AgentTeam().GetAgentTeam(resourceID)
```

替换为：
```go
	case "team":
		return m.registry.AgentTeam().GetAgentTeam(ctx, resourceID)
```

- [ ] **Step 6: 修改 resourceTypeOf 添加 TeamCard 分支（L1887-1891）**

在 `case reflect.TypeOf((*agentschema.AgentCard)(nil)):` 之后，`default:` 之前，添加：

```go
	case reflect.TypeOf((*multiagent.TeamCard)(nil)):
		return "team"
```

- [ ] **Step 7: 添加 import**

在 resource_manager.go 的 import 中添加：

```go
multiagent "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
```

- [ ] **Step 8: 验证编译**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/resources_manager/...`
Expected: PASS

- [ ] **Step 9: 提交**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager.go
git commit -m "feat(resources_manager): 回填 team 相关方法，补全 dispatch/case 分支和 resourceTypeOf"
```

---

### Task 9: 更新 resource_manager_test.go — 修复编译错误

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/resource_manager_test.go`

resource_manager.go 的签名变更可能导致测试编译错误，需要逐个修复。

- [ ] **Step 1: 检查编译错误**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/runner/resources_manager/... 2>&1`
Expected: 如果有编译错误，列出具体错误位置

- [ ] **Step 2: 修复所有编译错误**

根据 Step 1 的输出，修复测试文件中引用旧签名的位置。可能的修改点：
- `AddAgentTeam` 调用需要改为传 `*TeamCard` + `AgentTeamProvider`
- `GetAgentTeam` 调用需要添加 `ctx` 参数
- `RemoveAgentTeam` 返回类型从 `(any, error)` 变为 `([]AgentTeamProvider, error)`

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/... -v -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/resource_manager_test.go
git commit -m "test(resources_manager): 更新测试以适配 AgentTeamMgr 回填后的签名"
```

---

### Task 10: 更新 agent_team_manager_test.go

**Files:**
- Modify: `internal/agentcore/runner/resources_manager/agent_team_manager_test.go`

- [ ] **Step 1: 检查现有测试文件**

Run: `cat internal/agentcore/runner/resources_manager/agent_team_manager_test.go`
Expected: 查看现有内容，可能为空或有旧的桩测试

- [ ] **Step 2: 重写测试**

```go
package resources_manager

import (
	"context"
	"testing"

	multiagent "github.com/uapclaw/uapclaw-go/internal/agentcore/multi_agent"
	agentschema "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/schema"
	"github.com/uapclaw/uapclaw-go/internal/common/schema"
)

// TestAgentTeamMgr_添加团队 测试 AddAgentTeam 正常注册。
func TestAgentTeamMgr_添加团队(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ *multiagent.TeamCard) (multiagent.BaseTeam, error) {
		return nil, nil
	}
	err := mgr.AddAgentTeam("team-1", provider)
	if err != nil {
		t.Fatalf("AddAgentTeam 失败: %v", err)
	}
}

// TestAgentTeamMgr_重复添加报错 测试重复注册同一 ID。
func TestAgentTeamMgr_重复添加报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ *multiagent.TeamCard) (multiagent.BaseTeam, error) {
		return nil, nil
	}
	_ = mgr.AddAgentTeam("team-dup", provider)
	err := mgr.AddAgentTeam("team-dup", provider)
	if err == nil {
		t.Fatal("重复添加应返回错误")
	}
}

// TestAgentTeamMgr_空ID报错 测试空 ID 校验。
func TestAgentTeamMgr_空ID报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ *multiagent.TeamCard) (multiagent.BaseTeam, error) {
		return nil, nil
	}
	err := mgr.AddAgentTeam("", provider)
	if err == nil {
		t.Fatal("空 ID 应返回错误")
	}
}

// TestAgentTeamMgr_nilProvider报错 测试 nil provider 校验。
func TestAgentTeamMgr_nilProvider报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	err := mgr.AddAgentTeam("team-nil", nil)
	if err == nil {
		t.Fatal("nil provider 应返回错误")
	}
}

// TestAgentTeamMgr_移除团队 测试 RemoveAgentTeam 正常注销。
func TestAgentTeamMgr_移除团队(t *testing.T) {
	mgr := NewAgentTeamMgr()
	provider := func(_ context.Context, _ *multiagent.TeamCard) (multiagent.BaseTeam, error) {
		return nil, nil
	}
	_ = mgr.AddAgentTeam("team-rm", provider)
	removed, err := mgr.RemoveAgentTeam("team-rm")
	if err != nil {
		t.Fatalf("RemoveAgentTeam 失败: %v", err)
	}
	if removed == nil {
		t.Fatal("移除后应返回 provider")
	}
}

// TestAgentTeamMgr_移除不存在报错 测试移除不存在的团队。
func TestAgentTeamMgr_移除不存在报错(t *testing.T) {
	mgr := NewAgentTeamMgr()
	_, err := mgr.RemoveAgentTeam("notexist")
	if err == nil {
		t.Fatal("移除不存在的团队应返回错误")
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/runner/resources_manager/... -run TestAgentTeamMgr -v -count=1`
Expected: PASS（所有 6 个测试）

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/runner/resources_manager/agent_team_manager_test.go
git commit -m "test(resources_manager): 更新 AgentTeamMgr 测试以适配 AbstractManager 回填"
```

---

### Task 11: 全量编译和测试验证

**Files:** 无新增

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && go build ./...`
Expected: PASS

- [ ] **Step 2: 运行全量测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/multi_agent/... ./internal/agentcore/runner/resources_manager/... -v -count=1 -timeout 120s`
Expected: PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md 状态**

将 8.27 的状态从 `☐` 改为 `✅`。

- [ ] **Step 4: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "chore: 更新 8.27 状态为 ✅"
```
