# Go 编码规范

## 规范 1：所有注释使用中文

所有 Go 源码文件中的注释必须使用中文，包括：
- 包注释（`// Package xxx 提供...`）
- 函数/方法注释（`// FuncName 执行...`）
- 结构体/接口注释（`// User 用户信息`）
- 字段注释（`// ID 唯一标识`）
- 常量/枚举注释
- 行内注释
- TODO/FIXME 标注

**正确示例：**

```go
// 模型客户端接口
type BaseModelClient interface {
    // 调用模型并返回完整响应
    Invoke(ctx context.Context, messages []Message, opts ...Option) (*AssistantMessage, error)
    // 流式调用模型，返回消息块通道
    Stream(ctx context.Context, messages []Message, opts ...Option) (<-chan AssistantMessageChunk, error)
}
```

**错误示例（禁止）：**

```go
// Model client interface
type BaseModelClient interface {
    // Invoke model and return full response
    Invoke(ctx context.Context, messages []Message, opts ...Option) (*AssistantMessage, error)
}
```

## 规范 2：源码声明排列顺序

每个 Go 源码文件内部，必须严格按以下顺序排列声明：

```
1. 结构体 (type struct / type interface)    — 接口排在结构体之前
2. 枚举   (type iota)
3. 常量   (const)
4. 全局变量 (var)
5. 导出函数 (func Xxx)                      — 大写开头
6. 非导出函数 (func xxx)                    — 小写开头
```

各类声明之间用分隔注释区分：

```go
// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// ──────────────────────────── 非导出函数 ────────────────────────────
```

**完整示例：**

```go
package agent

// ──────────────────────────── 结构体 ────────────────────────────

// AgentCard Agent 身份元数据
type AgentCard struct {
    // ID 唯一标识
    ID string `json:"id"`
    // Name 名称
    Name string `json:"name"`
    // Description 描述
    Description string `json:"description"`
}

// AgentResult Agent 执行结果
type AgentResult struct {
    // TaskID 任务标识
    TaskID string `json:"task_id"`
    // Status 任务状态
    Status TaskStatus `json:"status"`
}

// ──────────────────────────── 枚举 ────────────────────────────

// TaskStatus 任务状态枚举
type TaskStatus int

const (
    // TaskStatusPending 待执行
    TaskStatusPending TaskStatus = iota
    // TaskStatusRunning 执行中
    TaskStatusRunning
    // TaskStatusCompleted 已完成
    TaskStatusCompleted
    // TaskStatusFailed 已失败
    TaskStatusFailed
)

// ──────────────────────────── 常量 ────────────────────────────

const (
    // MaxIterations ReAct 循环最大迭代次数
    MaxIterations = 10
    // DefaultModelName 默认模型名称
    DefaultModelName = "qwen-max"
)

// ──────────────────────────── 全局变量 ────────────────────────────

var (
    // globalRegistry 全局 Agent 注册表
    globalRegistry = make(map[string]*AgentCard)
)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewAgent 创建新的 Agent 实例
func NewAgent(card *AgentCard) *Agent {
    return &Agent{card: card}
}

// Register 向全局注册表注册 Agent
func Register(card *AgentCard) error {
    globalRegistry[card.ID] = card
    return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// buildSystemPrompt 构建系统提示词
func buildSystemPrompt(card *AgentCard) string {
    return ""
}

// validateCard 校验 AgentCard 字段
func validateCard(card *AgentCard) error {
    return nil
}
```

**特例说明：**
- 接口 (type interface) 归类到结构体区块，排在结构体之前
- 类型别名 (type X = Y) 归类到枚举区块
- init() 函数归类到非导出函数区块
- 文件头只保留 package 和 import，不要在 import 后直接写 const/var

## 规范 3：单元测试覆盖率与构建标签

每个 Go 源码文件都必须配备对应的单元测试文件（`_test.go`），**整体测试覆盖率必须 ≥ 85%**。无法 mock 的外部依赖用 `//go:build` 标签隔离，被隔离的代码不纳入覆盖率计算基线。

### 3.1 测试文件要求

- 每个包下必须有 `*_test.go` 文件
- 导出函数必须有测试用例覆盖
- 非导出函数尽量覆盖（通过同包测试）
- 结构体字段序列化/校验逻辑必须有测试
- 枚举/常量的边界值必须有测试
- **覆盖率目标：≥ 85%**（`go test -cover ./...`），未达标的包需补充测试

### 3.2 依赖外部环境无法 mock 的代码

对于依赖真实外部环境（如数据库连接、LLM API 调用、文件系统特定路径、网络端口监听等）且无法在单元测试中 mock 的代码，使用 `//go:build` 标签隔离，确保 `go test ./...` 默认不执行这些测试。**被隔离的代码不纳入覆盖率计算基线。**

**build tag 豁免规则（仅以下情况不计入覆盖率基线）：**

1. 无法 mock 的真实外部服务：LLM API、第三方 SaaS、远程 RPC
2. 依赖外部运行环境：数据库、消息队列、文件系统特定路径
3. 硬件/OS 绑定：CGO 调用、系统调用、平台特定行为

**标签命名约定：**

| 标签 | 用途 | 运行方式 |
|------|------|---------|
| `integration` | 集成测试（需要真实数据库/消息队列等） | `go test -tags=integration ./...` |
| `llm` | LLM API 真实调用测试 | `go test -tags=llm ./...` |
| `e2e` | 端到端测试 | `go test -tags=e2e ./...` |

**示例 — 需要 LLM API 的测试：**

```go
//go:build llm

package llm

import "testing"

// TestOpenAIClientInvoke_真实调用 测试 OpenAI 客户端真实调用
// 运行方式: go test -tags=llm ./internal/agentcore/llm/...
func TestOpenAIClientInvoke_真实调用(t *testing.T) {
    // 需要真实 API Key 和网络连接
    client := NewOpenAIClient(ModelClientConfig{
        APIKey:  os.Getenv("OPENAI_API_KEY"),
        APIBase: "https://api.openai.com/v1",
    })
    // ...
}
```

**示例 — 需要真实数据库的测试：**

```go
//go:build integration

package store

import "testing"

// TestPostgresStore_真实数据库 测试 PostgreSQL 存储真实读写
// 运行方式: go test -tags=integration ./internal/agentcore/store/db/...
func TestPostgresStore_真实数据库(t *testing.T) {
    // 需要真实 PostgreSQL 实例
    store, err := NewPostgresStore("postgres://localhost:5432/testdb")
    // ...
}
```

### 3.3 可以 mock 的代码禁止使用 build tag

以下场景应通过接口 mock / 依赖注入实现测试，**不得**使用 build tag 逃避：

- 外部 HTTP 服务 → 使用 `net/http/httptest` 起本地服务
- 数据库操作 → 通过 Repository 接口 mock
- 文件读写 → 使用 `t.TempDir()` 创建临时目录
- 时间依赖 → 通过时钟接口注入
- 配置加载 → 注入配置对象而非读取文件

```go
// ✅ 正确：通过接口 mock 测试
type ModelClient interface {
    Invoke(ctx context.Context, req *Request) (*Response, error)
}

// fakeModelClient 用于测试的模拟客户端
type fakeModelClient struct {
    response *Response
    err      error
}

func (f *fakeModelClient) Invoke(ctx context.Context, req *Request) (*Response, error) {
    return f.response, f.err
}

// ✅ 正确：使用 httptest 测试 HTTP 客户端
func TestRestfulApi_调用(t *testing.T) {
    server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        w.WriteHeader(http.StatusOK)
        json.NewEncoder(w).Encode(map[string]string{"result": "ok"})
    }))
    defer server.Close()
    // 用 server.URL 作为 API 地址进行测试
}
```

### 3.4 测试函数命名规范

```
TestXxx                    — 基本测试
TestXxx_场景描述            — 带场景的测试（中文描述场景）
TestXxx_真实调用            — 带 build tag 的集成测试统一后缀
```

示例：
```go
func TestNewAgent(t *testing.T) {}
func TestNewAgent_配置缺失时返回错误(t *testing.T) {}
func TestOpenAIClientInvoke_真实调用(t *testing.T) {}  // +build llm
```

### 3.5 禁止的行为

- ❌ 没有测试文件的 Go 源码包
- ❌ 可 mock 的代码使用 build tag 逃避测试
- ❌ 测试文件中硬编码外部环境地址（应从环境变量读取）
- ❌ build tag 命名随意（必须使用 `integration`/`llm`/`e2e` 之一）
- ❌ `//go:build` 注释格式错误（注意 `//go:build` 前面没有空格，与 `// go:build` 不同）

**特例说明：**
- 如果某个包暂时无法编写测试，需在测试文件中用 `// TODO: 补充单元测试` 标注，并创建空的测试文件占位
- 后续将补充具体的 mock 框架选型和测试工具链方案

**禁止的行为：**
- 常量夹在结构体中间
- 导出函数和非导出函数交叉排列
- 全局变量散落在文件各处
- 中英文混合注释

### 3.6 覆盖率检查命令

```bash
# 运行单元测试并查看覆盖率
go test -cover ./...

# 生成覆盖率详情
go test -coverprofile=coverage.out ./...
go tool cover -func=coverage.out

# 排除 build tag 隔离文件后计算覆盖率
go test -cover -tags=!integration,!llm,!e2e ./...
```
