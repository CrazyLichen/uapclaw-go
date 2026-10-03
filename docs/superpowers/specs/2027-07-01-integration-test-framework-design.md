# 集成测试框架设计

## 概述

将 Python 项目 `system_tests/` 层的集成测试模式迁移到 Go 项目，建立 `tests/integration/` 目录和配套基础设施，实现 **Mock LLM + 真实 Agent 逻辑** 的集成测试覆盖。

对应 Python 源码：
- `agent-core/tests/system_tests/`（核心 Agent 逻辑集成测试）
- `agent-core/tests/unit_tests/fixtures/mock_llm.py`（共享 Mock LLM）
- `jiuwenswarm-develop/tests/integration/`（平台层集成测试）
- `jiuwenbox/tests/integration/`（沙箱集成测试）

---

## 1. 测试层级定义

| 层级 | Python 目录 | Go 目录 | Go Build Tag | 定义 | 运行方式 |
|------|------------|---------|-------------|------|---------|
| 单元测试 | `tests/unit_tests/` | 源码同目录 `_test.go` | 无 | Mock 一切，测单个函数/结构体 | `make test` |
| 集成测试 | `tests/system_tests/` | `tests/integration/` | `//go:build integration` | Mock LLM + 真实 Agent 逻辑 | `make test-integration` |
| E2E 测试 | `tests/cli/e2e/` | `tests/e2e/` | `//go:build e2e` | 真实 LLM API，端到端 | `make test-e2e` |
| LLM 真实调用 | 混在 e2e 中 | 源码同目录 | `//go:build llm` | 真实 LLM API 调用 | `make test-llm` |

**核心原则**：
- **Mock LLM + 真实逻辑**：LLM 调用全部走 MockModelClient，Runner、Session、Tool、Rail、Memory 等组件全部真实执行
- **独立目录**：所有集成测试集中在 `tests/integration/`，源码目录不再有 `//go:build integration`
- **内部共享**：Mock 基础设施仅在 `tests/integration/` 内部共享，单元测试各包自理
- **TestSuite 模式**：用 testify/suite 管理 setup/teardown 生命周期，模拟 conftest fixture 链

---

## 2. 目录结构

```
tests/
├── integration/                          # 集成测试根目录
│   ├── doc.go                            # 包文档
│   ├── mockllm/                          # 共享 Mock LLM 基础设施
│   │   ├── doc.go
│   │   ├── mock_client.go               # MockModelClient 实现
│   │   └── mock_client_test.go          # Mock 自身的单元测试
│   ├── suite/                            # 共享 TestSuite 基类
│   │   ├── doc.go
│   │   ├── base_suite.go                # BaseIntegrationSuite（MockLLM + 日志 + 上下文）
│   │   ├── runner_suite.go             # RunnerSuite（Base + Runner 启动/停止）
│   │   ├── session_suite.go            # SessionSuite（Runner + Session 创建/销毁）
│   │   └── agent_suite.go             # AgentSuite（Session + Agent 注册/执行）
│   ├── agentcore/                        # 对齐 Python system_tests
│   │   ├── runner/                       #   Runner 集成测试
│   │   ├── deep_agent/                  #   DeepAgent（ReAct 循环）
│   │   ├── memory/                      #   Memory 系统集成
│   │   │   ├── coding/                  #     CodingMemory
│   │   │   ├── ltm/                     #     LongTermMemory
│   │   │   └── graph/                   #     GraphMemory
│   │   ├── security/                    #   Security/Guardrail
│   │   ├── harness/                     #   Harness（Rail/Tool）
│   │   │   ├── rails/
│   │   │   └── tools/
│   │   │       └── worktree/
│   │   ├── workflow/                    #   Workflow 集成
│   │   └── retrieval/                   #   Retrieval 索引/检索
│   ├── multi_agent/                      # 多 Agent 协作
│   │   ├── team/
│   │   └── coordination/
│   ├── swarm/                            # 平台层集成
│   │   ├── gateway/
│   │   ├── server/
│   │   └── channel/
│   ├── evolving/                         # Agent 演进/优化
│   └── external/                         # 外部服务真实连接（原散落文件迁入）
│       ├── chromadb/
│       ├── gaussdb/
│       ├── spawn/
│       └── mcp/
├── e2e/                                  # E2E 预留目录
│   └── doc.go
```

**关键设计点**：
- `mockllm/`：等价于 Python `tests/unit_tests/fixtures/mock_llm.py`，仅集成测试可用
- `suite/`：等价于 Python conftest.py 的 fixture 链，通过 Suite 嵌套实现依赖链
- `external/`：原散落在源码目录的真实外部服务连接测试统一迁入此处
- 目录结构与 Python `system_tests/` 一一对应，便于逐模块迁移

---

## 3. Mock LLM 基础设施

对齐 Python `MockLLMModel` 的全部能力，位于 `tests/integration/mockllm/`。

### 3.1 MockModelClient 核心接口

```go
// MockModelClient 集成测试专用的 LLM Mock 客户端
// 对齐 Python tests/unit_tests/fixtures/mock_llm.py:MockLLMModel
type MockModelClient struct {
    mu          sync.Mutex
    responses   []MockResponse    // 响应队列，依次消费
    streamCh    chan AssistantMessageChunk
    invokeCalls []InvokeCall      // 记录所有 invoke 调用（用于断言）
    streamCalls []StreamCall      // 记录所有 stream 调用
}

// MockResponse 预设响应（支持纯文本、工具调用、混合）
type MockResponse struct {
    Text      string             // 纯文本响应
    ToolCalls []ToolCall         // 工具调用响应
    Err       error              // 模拟错误
}

// InvokeCall 记录一次 Invoke 调用的参数
type InvokeCall struct {
    Messages []Message
    Opts     []Option
}
```

### 3.2 核心方法

```go
// 构造
func NewMockModelClient(responses ...MockResponse) *MockModelClient

// BaseModelClient 接口实现
func (m *MockModelClient) Invoke(ctx context.Context, messages []Message, opts ...Option) (*AssistantMessage, error)
func (m *MockModelClient) Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan AssistantMessageChunk, error)

// 响应配置（链式调用）
func (m *MockModelClient) SetResponses(responses ...MockResponse) *MockModelClient
func (m *MockModelClient) AddTextResponse(text string) *MockModelClient
func (m *MockModelClient) AddToolCallResponse(toolName string, args map[string]any) *MockModelClient
func (m *MockModelClient) AddErrorResponse(err error) *MockModelClient

// 调用断言
func (m *MockModelClient) InvokeCallCount() int
func (m *MockModelClient) GetInvokeCall(idx int) InvokeCall
func (m *MockModelClient) LastInvokeCall() InvokeCall
func (m *MockModelClient) ResetCalls()
```

### 3.3 与 Python MockLLMModel 对照

| Python 能力 | Go 对应 |
|------------|---------|
| `set_responses(list)` | `SetResponses(...MockResponse)` |
| `set_response(str)` | `AddTextResponse(str)` |
| `create_text_response(text)` | `MockResponse{Text: text}` |
| `create_tool_call_response(name, args)` | `AddToolCallResponse(name, args)` |
| `invoke` 内部消费响应队列 | `Invoke` 按 mutex 保护依次消费 |
| `stream` 支持 | `Stream` 返回预设 chunk channel |
| 无调用记录 | `invokeCalls` / `streamCalls` 记录调用历史（Go 增强） |

### 3.4 注册到 ClientRegistry

MockModelClient 必须能注册到 ClientRegistry，让 Runner 等组件通过正常路径获取：

```go
func (m *MockModelClient) ClientName() string { return "mock" }

// 使用方式：
// registry := NewClientRegistry()
// registry.Register("mock", m.client)
// runner := NewRunner(WithClientRegistry(registry))
```

---

## 4. TestSuite 基类（conftest.py 等价物）

Python conftest.py 的核心是 fixture 依赖链：`mock_llm → session → runner → agent`。Go 端用 Suite 嵌套实现同样的效果。

### 4.1 四层 Suite 继承链

```
BaseIntegrationSuite          # 第 1 层：MockLLM + Context + 日志
  └── RunnerSuite            # 第 2 层：+ Runner 启动/停止
       └── SessionSuite      # 第 3 层：+ Session 创建/销毁
            └── AgentSuite   # 第 4 层：+ Agent 注册/执行
```

### 4.2 各层职责

**BaseIntegrationSuite** — 等价于 conftest.py 中的 mock_llm fixture

```go
type BaseIntegrationSuite struct {
    suite.Suite
    Ctx     context.Context
    Cancel  context.CancelFunc
    MockLLM *mockllm.MockModelClient
}

func (s *BaseIntegrationSuite) SetupSuite() {
    s.Ctx, s.Cancel = context.WithTimeout(context.Background(), 5*time.Minute)
    s.MockLLM = mockllm.NewMockModelClient()
}

func (s *BaseIntegrationSuite) TearDownSuite() {
    s.Cancel()
}
```

**RunnerSuite** — 等价于 conftest.py 中的 runner fixture

```go
type RunnerSuite struct {
    BaseIntegrationSuite
    Registry *ClientRegistry
    Runner   *Runner
}

func (s *RunnerSuite) SetupSuite() {
    s.BaseIntegrationSuite.SetupSuite()
    s.Registry = NewClientRegistry()
    s.Registry.Register("mock", s.MockLLM)
    s.Runner = NewRunner(WithClientRegistry(s.Registry))
}

func (s *RunnerSuite) TearDownSuite() {
    s.Runner.Stop()
    s.BaseIntegrationSuite.TearDownSuite()
}
```

**SessionSuite** — 等价于 conftest.py 中的 session fixture

```go
type SessionSuite struct {
    RunnerSuite
    Session SessionFacade
}

func (s *SessionSuite) SetupSuite() {
    s.RunnerSuite.SetupSuite()
    s.Session = s.Runner.NewSession(s.Ctx, /* config */)
}

func (s *SessionSuite) TearDownSuite() {
    s.Session.Close()
    s.RunnerSuite.TearDownSuite()
}
```

**AgentSuite** — 等价于 conftest.py 中注册 AgentCard + 创建 Agent 的 fixture

```go
type AgentSuite struct {
    SessionSuite
    AgentCard *AgentCard
    Agent     Agent
}

func (s *AgentSuite) SetupSuite() {
    s.SessionSuite.SetupSuite()
    s.AgentCard = &AgentCard{ID: "test-agent", Name: "TestAgent", ...}
    s.Runner.Register(s.AgentCard)
    s.Agent = s.Runner.GetAgent("test-agent")
}

func (s *AgentSuite) TearDownSuite() {
    // Agent 随 Runner 停止而清理
    s.SessionSuite.TearDownSuite()
}
```

### 4.3 使用示例

```go
// tests/integration/agentcore/deep_agent/react_test.go

//go:build integration

package deep_agent_test

type ReactAgentSuite struct {
    suite.AgentSuite
}

func TestReactAgentSuite(t *testing.T) {
    suite.Run(t, new(ReactAgentSuite))
}

func (s *ReactAgentSuite) SetupSuite() {
    s.AgentSuite.SetupSuite()
    // 预设 MockLLM 响应：第一次返回 tool_call，第二次返回最终文本
    s.MockLLM.SetResponses(
        mockllm.MockResponse{ToolCalls: []mockllm.ToolCall{
            {Name: "read_file", Arguments: `{"path": "/tmp/test.txt"}`},
        }},
        mockllm.MockResponse{Text: "文件内容是 hello world"},
    )
}

func (s *ReactAgentSuite) TestReActLoop_工具调用后返回结果() {
    result, err := s.Agent.Run(s.Ctx, "读取 /tmp/test.txt 的内容")
    s.Require().NoError(err)
    s.Contains(result, "hello world")
    s.Equal(2, s.MockLLM.InvokeCallCount())  // 验证 LLM 被调用 2 次
}
```

---

## 5. 现有 integration 测试迁移

### 5.1 迁移清单

| 原位置 | 迁入目标 | 备注 |
|--------|---------|------|
| `internal/agentcore/foundation/store/vector/chroma_integration_test.go` | `tests/integration/external/chromadb/` | 需改包路径，仅测导出 API |
| `internal/agentcore/foundation/store/vector/gauss_integration_test.go` | `tests/integration/external/gaussdb/` | 同上 |
| `internal/agentcore/runner/spawn/process_test.go` | `tests/integration/external/spawn/` | 真实子进程测试 |
| `internal/agentcore/runner/spawn/child_test.go` | `tests/integration/external/spawn/` | 合并到同文件 |
| `internal/agentcore/foundation/tool/mcp/client/openapi_client_test.go` | `tests/integration/external/mcp/` | OpenAPI 客户端 |
| `internal/agentcore/harness/rails/interrupt/ask_user_rail_integration_test.go` | `tests/integration/agentcore/harness/rails/` | 改用 Suite 基类 |
| `internal/agentcore/memory/ltm/integration_test.go` | `tests/integration/agentcore/memory/ltm/` | 当前空壳，保留 TODO |
| `internal/swarm/server/runtime/skill/skill_manager_integration_test.go` | `tests/integration/swarm/server/` | SkillManager |
| `internal/swarm/agents/harness/common/memory/forbidden_integration_test.go` | `tests/integration/swarm/memory/` | Forbidden Memory |
| `cmd/uapclaw/app_integration_test.go` | `tests/integration/cmd/` | CLI 入口测试 |

### 5.2 迁移注意事项

- **包路径变更**：从同包测试（`package xxx`）变为外部包测试（`package xxx_test`），失去访问非导出函数的能力，需确保被测对象通过导出 API 可达
- **build tag 保留**：所有文件保留 `//go:build integration` 标签
- **Suite 适配**：非 external 目录的测试改用对应 Suite 基类；external 目录下的真实连接测试可用简化的 BaseIntegrationSuite + 环境变量跳过

---

## 6. 分批实施计划

### 第一批：基础设施骨架

1. 创建 `tests/integration/` 目录结构 + 所有 `doc.go`
2. 实现 `mockllm/` 包（MockModelClient + 响应队列 + 调用记录 + 注册能力）
3. 实现 `suite/` 包（四层 Suite 基类）
4. 迁移现有 10 个散落 integration 文件到 `tests/integration/`
5. 更新 Makefile（新增 `test-integration` / `test-llm` / `test-e2e` / `test-all` 目标）
6. 编写 `mockllm/` 自身的单元测试

**产出**：骨架可运行，`make test-integration` 能跑通迁移后的测试。

### 第二批：核心模块（Runner + DeepAgent + Session）

7. Runner 集成测试 — 启动/停止生命周期、注册 AgentCard、ClientRegistry 注入 MockLLM
8. DeepAgent (ReAct) 集成测试 — 单轮工具调用、多轮 ReAct 循环、工具失败错误传播、Rail 拦截
9. Session 集成测试 — 会话创建/恢复/销毁、消息历史持久化

**产出**：最核心的 Agent 执行链路被集成测试覆盖。

### 第三批：Memory + Security + Worktree

10. Memory 集成测试 — CodingMemory（读写/上下文关联）、LongTermMemory（存储/检索）、GraphMemory（实体/关系抽取）
11. Security 集成测试 — Guardrail 触发拦截、Shell 命令权限检查
12. Worktree 集成测试 — 真实 git 仓库初始化、Worktree 创建/进入/退出

### 第四批：Workflow + Multi Agent + 其余模块

13. Workflow 集成测试
14. Multi Agent / Team 集成测试
15. Agent Evolving 集成测试
16. Retrieval 集成测试
17. 按需补充 E2E 骨架

---

## 7. Makefile 目标

### 7.1 新增目标

```makefile
# 现有（不动）
test:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" ./...

test-cover:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" -coverprofile=coverage.out ./...
    $(GOCMD) tool cover -html=coverage.out -o coverage.html

# 新增
test-integration:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test integration" ./tests/integration/...

test-llm:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test llm" ./...

test-e2e:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test e2e" ./tests/e2e/...

test-all:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test integration llm" ./... ./tests/integration/...

test-integration-cover:
    CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test integration" -coverprofile=integration_coverage.out ./tests/integration/...
    $(GOCMD) tool cover -html=integration_coverage.out -o integration_coverage.html
```

### 7.2 运行场景

| 场景 | 命令 | 说明 |
|------|------|------|
| 日常开发 | `make test` | 仅单元测试，快速反馈 |
| 集成测试 | `make test-integration` | Mock LLM + 真实逻辑，无需外部服务 |
| 真实 LLM | `make test-llm` | 需要 API Key 环境变量 |
| E2E | `make test-e2e` | 端到端，需要完整环境 |
| 全量 | `make test-all` | 单元 + 集成 + LLM |
| CI 流水线 | `make test && make test-integration` | 分步运行，失败时定位清晰 |

### 7.3 环境变量与跳过机制

`tests/integration/` 下的测试（Mock LLM 驱动）不应依赖任何外部服务，任何环境都能跑。`tests/integration/external/` 下的真实连接测试需要环境变量：

```go
// tests/integration/external/chromadb/chroma_test.go

//go:build integration

package chromadb_test

func (s *ChromaSuite) SetupSuite() {
    if os.Getenv("CHROMA_ENDPOINT") == "" {
        s.T().Skip("跳过：未设置 CHROMA_ENDPOINT 环境变量")
    }
    // ...
}
```

这样 `make test-integration` 默认跳过 external 下的真实连接测试，设了环境变量才会运行。
