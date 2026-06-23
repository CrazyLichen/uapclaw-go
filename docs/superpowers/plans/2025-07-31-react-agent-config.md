# 6.3 ReActAgentConfig 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 ReActAgentConfig 结构体及 AgentConfig 接口，完成 6.3 步骤并回填所有 `any` 占位类型。

**Architecture:** ReActAgentConfig 采用扁平结构体 + Option 模式，字段与 Python 一一对应。AgentConfig 接口定义最小通用方法集，放在 interfaces 包。回填 BaseAgent/SessionConfig 中的 any → AgentConfig。

**Tech Stack:** Go 1.24+, testify/assert, 项目已有 Option 模式

**设计文档:** docs/superpowers/specs/2025-07-31-react-agent-config-design.md

---

## 文件结构

| 操作 | 文件路径 | 职责 |
|------|----------|------|
| 新增 | `schema/react_agent_config.go` | ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate |
| 新增 | `schema/react_agent_config_test.go` | ReActAgentConfig 单元测试 |
| 修改 | `interfaces/interface.go` | 新增 AgentConfig 接口；BaseAgent.Config() → AgentConfig；Configure 参数 → AgentConfig |
| 修改 | `single_agent/base.go` | WarpBaseAgent.config 字段 any → AgentConfig |
| 修改 | `single_agent/base_test.go` | 测试适配 AgentConfig 类型 |
| 修改 | `single_agent/ability_manager_test.go` | fakeAgent 适配 AgentConfig |
| 修改 | `session/config/config.go` | GetAgentConfig/SetAgentConfig any → AgentConfig |
| 修改 | `session/config/config_test.go` | 测试适配 AgentConfig |
| 修改 | `session/internal/agent_session.go` | AgentID() 简化逻辑 |
| 修改 | `session/doc.go` | AgentConfigProvider 说明更新 |
| 修改 | `schema/doc.go` | 文件目录新增 react_agent_config.go |
| 修改 | `interfaces/doc.go` | 核心类型索引新增 AgentConfig |
| 修改 | `IMPLEMENTATION_PLAN.md` | 6.3 状态 ☐ → ✅ |

---

### Task 1: 在 interfaces/interface.go 新增 AgentConfig 接口

**Files:**
- Modify: `internal/agentcore/single_agent/interfaces/interface.go`

- [ ] **Step 1: 新增 AgentConfig 接口定义**

在 `interface.go` 的结构体区块，`BaseAgent` 接口之前，新增 `AgentConfig` 接口：

```go
// AgentConfig Agent 配置接口，所有 Agent 配置必须实现。
//
// 定义所有 Agent 子类共有的配置访问方法，
// ReActAgentConfig、ControllerAgentConfig 等具体配置均实现此接口。
//
// 对应 Python: BaseAgent.config 属性（无类型约束，子类各自持有具体 config 类型）
type AgentConfig interface {
	// ModelName 返回模型名称
	ModelName() string
	// MemScopeID 返回内存作用域标识
	MemScopeID() string
	// ContextEngineConfig 返回上下文引擎配置
	ContextEngineConfig() contextengineschema.ContextEngineConfig
	// ModelClientConfig 返回模型客户端配置
	ModelClientConfig() *llmschema.ModelClientConfig
}
```

需要在 import 中新增：
```go
contextengineschema "github.com/uapclaw/uapclaw-go/internal/agentcore/context_engine/schema"
llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
```

- [ ] **Step 2: 修改 BaseAgent 接口的 Config() 和 Configure() 签名**

将 `Config() any` 改为 `Config() AgentConfig`，
将 `Configure(ctx context.Context, config any) error` 改为 `Configure(ctx context.Context, config AgentConfig) error`。

删除相关 ⤵️ 回填注释（因为本步骤即完成回填）。

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/interfaces/...`
Expected: 编译失败（因为 base.go 等文件仍使用 any），记录错误用于下一步修复

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/interfaces/interface.go
git commit -m "feat(6.3): 新增 AgentConfig 接口，BaseAgent.Config()/Configure() 改为 AgentConfig"
```

---

### Task 2: 修改 WarpBaseAgent 适配 AgentConfig

**Files:**
- Modify: `internal/agentcore/single_agent/base.go`
- Modify: `internal/agentcore/single_agent/base_test.go`
- Modify: `internal/agentcore/single_agent/ability_manager_test.go`

- [ ] **Step 1: 修改 WarpBaseAgent.config 字段类型**

在 `base.go` 中：
- `config any` → `config interfaces.AgentConfig`
- `Configure(_ context.Context, config any) error` → `Configure(_ context.Context, config interfaces.AgentConfig) error`
- `Config() any` → `Config() interfaces.AgentConfig`

- [ ] **Step 2: 修改 base_test.go 测试适配**

在 `TestWarpBaseAgent_Configure` 中，将 `map[string]any` 类型的 config 改为使用 `*ReActAgentConfig`（此时 ReActAgentConfig 尚未实现，先用 nil 占位，改用简单的 nil 传入测试，或暂时将测试标记为跳过）。

由于 ReActAgentConfig 尚未实现，最安全的做法是将该测试暂时改为：
```go
func TestWarpBaseAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("cfg_agent"), schema.WithDescription("配置测试"))
	agent := NewWarpBaseAgent(card, nil)

	// ReActAgentConfig 实现后使用具体实例测试
	// 当前测试 Configure 接受 nil
	err := agent.Configure(context.Background(), nil)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
}
```

在 `TestWarpBaseAgent_访问器` 中，Config 类型断言也需要调整。

- [ ] **Step 3: 修改 ability_manager_test.go 的 fakeAgent**

```go
func (f *fakeAgent) Configure(_ context.Context, _ interfaces.AgentConfig) error { return nil }
func (f *fakeAgent) Config() interfaces.AgentConfig { return nil }
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/...`
Expected: PASS（单_agent 包内编译通过）

- [ ] **Step 5: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... -count=1`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/single_agent/base.go internal/agentcore/single_agent/base_test.go internal/agentcore/single_agent/ability_manager_test.go
git commit -m "feat(6.3): WarpBaseAgent.config 从 any 改为 AgentConfig，测试适配"
```

---

### Task 3: 回填 SessionConfig 的 AgentConfig 类型

**Files:**
- Modify: `internal/agentcore/session/config/config.go`
- Modify: `internal/agentcore/session/config/config_test.go`
- Modify: `internal/agentcore/session/internal/agent_session.go`
- Modify: `internal/agentcore/session/doc.go`

**循环依赖说明：**
`single_agent/interfaces` 导入 `session`（因 BaseAgent 接口使用 `session/stream`），
而 `session` 导入 `session/config`。因此 `session/config` **不能**直接导入 `single_agent/interfaces`，
否则形成循环：`interfaces → session → config → interfaces`。

**解决方案：** `session/config` 中 `GetAgentConfig()/SetAgentConfig()` 的签名保持 `any`，
但更新注释说明调用方应传入 `AgentConfig` 实现者。
在 `agent_session.go` 中（位于 `session/internal` 子包，可安全导入 `single_agent/interfaces`）
做类型断言 `ac.(interfaces.AgentConfig)` 获取 `AgentConfig` 接口。

- [ ] **Step 1: 更新 config.go 注释**

在 `config.go` 中，将 `⤵️ 6.3 回填` 注释改为更明确的说明：
- `GetAgentConfig()` 注释改为：`// ⤵️ 6.3 已完成：调用方应传入 AgentConfig 实现者，因循环依赖限制暂保留 any 类型`
- `SetAgentConfig()` 注释同步更新
- `agentConfig` 字段注释同步更新

- [ ] **Step 2: 修改 agent_session.go 使用 AgentConfig 类型断言**

`AgentID()` 方法中，通过类型断言获取 `AgentConfig` 接口：
```go
func (s *AgentSession) AgentID() string {
	if s.config != nil {
		if ac := s.config.GetAgentConfig(); ac != nil {
			// 通过类型断言获取 AgentConfig 接口
			type agentConfigProvider interface {
				ModelName() string
				MemScopeID() string
			}
			if provider, ok := ac.(agentConfigProvider); ok {
				if id := provider.MemScopeID(); id != "" {
					return id
				}
			}
		}
	}
	if s.card != nil {
		return s.card.AbilityID()
	}
	return ""
}
```

删除旧的 `agentConfigIDProvider` 本地接口定义和 ⤵️ 注释。
更新注释说明：`ReActAgentConfig` 实现 `AgentConfig` 接口，类型断言会成功。

- [ ] **Step 3: 更新 config_test.go 注释**

`TestNewSessionConfig_AgentConfig` 中的 `⤵️ 6.3 回填` 注释更新为：
`// 6.3 已完成：AgentConfig 已实现，因循环依赖 config 包保留 any，调用方通过类型断言使用`

- [ ] **Step 4: 修改 session/doc.go**

将 `AgentConfigProvider  — Agent 配置提供者接口（占位，⤵️ 6.3 回填）` 改为：
`AgentConfig         — Agent 配置接口（定义于 single_agent/interfaces，config 包因循环依赖保留 any）`

并在核心类型索引中提及 `AgentConfig`。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/session/...`
Expected: PASS

- [ ] **Step 6: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/session/... -count=1`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/session/config/config.go internal/agentcore/session/config/config_test.go internal/agentcore/session/internal/agent_session.go internal/agentcore/session/doc.go
git commit -m "feat(6.3): SessionConfig AgentConfig 回填（config 包保留 any 避免循环依赖），agent_session 类型断言优化"
```

---

### Task 4: 实现 ReActAgentConfig 结构体

**Files:**
- Create: `internal/agentcore/single_agent/schema/react_agent_config.go`

- [ ] **Step 1: 编写 ReActAgentConfig 完整实现**

文件 `schema/react_agent_config.go`，包含：

1. **ReActAgentConfig 结构体**（17 个字段 + Workspace any）
2. **ReActAgentConfigOption 类型**
3. **NewReActAgentConfig 构造函数**（默认值：ModelProvider="openai", MaxIterations=5, LLMTopLogprobs=1, ContextEngineConfig 设置 MaxContextMessageNum=200 + DefaultWindowRoundNum=10）
4. **基础 Option 函数**（18 个 With* 函数）
5. **复合 Option 函数**（WithModelClient, WithModelProviderDetails, WithContextEngine, WithCustomHeadersSync + ModelClientExtraOption）
6. **AgentConfig 接口实现**（4 个方法）
7. **Validate 方法**
8. **编译期接口检查** `var _ interfaces.AgentConfig = (*ReActAgentConfig)(nil)`

所有注释使用中文，声明排列遵循规范 2 顺序。

- [ ] **Step 2: 编译验证**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/single_agent/schema/...`
Expected: PASS

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/schema/react_agent_config.go
git commit -m "feat(6.3): 实现 ReActAgentConfig 结构体 + Option + AgentConfig 接口实现"
```

---

### Task 5: 编写 ReActAgentConfig 单元测试

**Files:**
- Create: `internal/agentcore/single_agent/schema/react_agent_config_test.go`

- [ ] **Step 1: 编写测试**

包含以下测试函数：

1. `TestNewReActAgentConfig` — 默认值验证（ModelProvider="openai", MaxIterations=5, LLMTopLogprobs=1, ContextEngineConfig 的 MaxContextMessageNum=200, DefaultWindowRoundNum=10）
2. `TestNewReActAgentConfig_WithOptions` — 各基础 Option 函数逐一测试
3. `TestNewReActAgentConfig_WithModelClient` — 复合 Option 联动（验证 ModelClientConfig 和 ModelRequestConfig 被创建，顶层字段被设置）
4. `TestNewReActAgentConfig_WithModelProviderDetails` — 复合 Option（验证 ModelProvider/APIKey/APIBase 被设置，ModelClientConfig 未创建）
5. `TestNewReActAgentConfig_WithContextEngine` — 复合 Option（验证 ContextEngineConfig 各字段）
6. `TestNewReActAgentConfig_WithCustomHeadersSync` — 复合 Option（验证 CustomHeaders 被设置且同步到 ModelClientConfig）
7. `TestReActAgentConfig_Validate` — 校验逻辑（LLMTopLogprobs 边界 0/20/-1/21，MaxIterations 0/1/-1，子配置递归校验）
8. `TestReActAgentConfig_AgentConfig接口` — 接口实现验证（断言 *ReActAgentConfig 实现 AgentConfig）
9. `TestReActAgentConfig_JSON序列化` — JSON round-trip（构造实例 → Marshal → Unmarshal → 比较关键字段）

- [ ] **Step 2: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/schema/... -count=1 -v`
Expected: PASS

- [ ] **Step 3: 检查覆盖率**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/single_agent/schema/...`
Expected: 覆盖率 ≥ 85%

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/schema/react_agent_config_test.go
git commit -m "test(6.3): ReActAgentConfig 单元测试，覆盖 Option/Validate/接口/JSON"
```

---

### Task 6: 回填 base_test.go 使用 ReActAgentConfig

**Files:**
- Modify: `internal/agentcore/single_agent/base_test.go`
- Modify: `internal/agentcore/session/config/config_test.go`

- [ ] **Step 1: 更新 TestWarpBaseAgent_Configure 使用真实 ReActAgentConfig**

```go
func TestWarpBaseAgent_Configure(t *testing.T) {
	card := agentschema.NewAgentCard(schema.WithName("cfg_agent"), schema.WithDescription("配置测试"))
	agent := NewWarpBaseAgent(card, nil)

	cfg := agentschema.NewReActAgentConfig(
		agentschema.WithModelName("qwen-max"),
		agentschema.WithMaxIterations(10),
	)
	err := agent.Configure(context.Background(), cfg)
	if err != nil {
		t.Fatalf("不应有错误: %v", err)
	}
	got, ok := agent.Config().(*agentschema.ReActAgentConfig)
	if !ok {
		t.Fatalf("Config 类型应为 *ReActAgentConfig，实际 %T", agent.Config())
	}
	if got.ModelName != "qwen-max" {
		t.Errorf("ModelName = %v, want qwen-max", got.ModelName)
	}
}
```

- [ ] **Step 2: 更新 config_test.go 使用真实 ReActAgentConfig**

由于 `session/config` 包因循环依赖保留 `any`，测试仍使用 `any` 但传入 `ReActAgentConfig` 实例：
```go
func TestNewSessionConfig_AgentConfig(t *testing.T) {
	ctx := context.Background()
	cfg := NewSessionConfig(ctx)
	assert.Nil(t, cfg.GetAgentConfig())

	agentCfg := agentschema.NewReActAgentConfig(
		agentschema.WithModelName("test-model"),
	)
	cfg.SetAgentConfig(agentCfg) // 传入 AgentConfig 实现者，config 包以 any 存储
	ac := cfg.GetAgentConfig()
	assert.NotNil(t, ac)
	// 通过类型断言验证
	type agentConfigProvider interface {
		ModelName() string
	}
	if provider, ok := ac.(agentConfigProvider); ok {
		assert.Equal(t, "test-model", provider.ModelName())
	}
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/single_agent/... ./internal/agentcore/session/... -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/single_agent/base_test.go internal/agentcore/session/config/config_test.go
git commit -m "test(6.3): 回填测试使用真实 ReActAgentConfig 替代 any"
```

---

### Task 7: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/single_agent/schema/doc.go`
- Modify: `internal/agentcore/single_agent/interfaces/doc.go`

- [ ] **Step 1: 更新 schema/doc.go**

在文件目录中新增 `react_agent_config.go` 条目，更新包功能概述提及 ReActAgentConfig：

```
//	schema/
//	├── doc.go                  # 包文档
//	├── agent_card.go           # AgentCard 结构体 + 构造函数 + Ability 接口实现
//	├── agent_result.go         # Part/Artifact/AgentResult 结果模型 + RawBytes 自定义 JSON marshal
//	└── react_agent_config.go   # ReActAgentConfig 结构体 + Option + AgentConfig 接口实现 + Validate
```

核心类型索引新增：`ReActAgentConfig — ReAct Agent 配置`

- [ ] **Step 2: 更新 interfaces/doc.go**

核心类型索引新增：`AgentConfig — Agent 配置接口（所有 Agent 配置的通用抽象）`

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/single_agent/schema/doc.go internal/agentcore/single_agent/interfaces/doc.go
git commit -m "docs(6.3): 更新 doc.go 文件目录和核心类型索引"
```

---

### Task 8: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 6.3 状态**

将 `6.3 | ☐ | ReActAgentConfig` 改为 `6.3 | ✅ | ReActAgentConfig`

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 IMPLEMENTATION_PLAN.md 6.3 状态为 ✅"
```

---

### Task 9: 全量编译和测试验证

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./...`
Expected: PASS

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test ./... -count=1`
Expected: PASS

- [ ] **Step 3: 覆盖率检查**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go test -cover ./internal/agentcore/single_agent/schema/...`
Expected: 覆盖率 ≥ 85%
