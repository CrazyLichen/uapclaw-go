# 9.64 Team Memory 设计文档

## 概述

9.64 Team Memory 是团队共享记忆系统的实现章节，对齐 Python `openjiuwen/agent_teams/memory/` 及关联的 `core/memory/lite/`、`agent_teams/models/allocator.py`。

核心作用：Team Memory 是团队的"长期记忆系统"，解决多轮协作中跨轮次保留决策、经验、成员特长等知识。三个层次：

1. **个人记忆（MemberMemoryToolkit）**：每个成员独立的记忆空间，通过嵌入搜索或文件读写访问
2. **共享记忆（SharedMemoryManager）**：全团队共享的 `TEAM_MEMORY.md`，所有成员只读，Leader 的提取 agent 负责更新
3. **记忆提取（Extractor）**：Leader 每轮结束后，启动子 Agent 分析任务和对话记录，提炼出决策/经验/特长/背景

9.64 还包含模型分配器（Model Allocator）的回填：RoundRobin/ByModelName/Router 三种策略。

## 在 Agent 会话流程中的位置

```
Agent 启动 (_setup_agent)
  ├── 9.64 init_toolkit()          ← 初始化成员记忆工具集
  ├── 9.64 register_tools()        ← 将记忆工具注册到 DeepAgent（剥离旧 MemoryRail）
  │
协调启动 (_start_coordination)
  ├── 9.64 load_and_inject()       ← 加载个人记忆 + 共享记忆 → 注入系统提示词
  │
每轮结束 (_finalize_round)
  ├── 9.64 extract_after_round()   ← Leader 专属：提取 agent 蒸馏团队记忆
  │
协调停止 (_stop_coordination)
  └── 9.64 close()                 ← 反注册工具 + 移除提示词段 + 关闭 MemoryIndexManager
```

## 实现策略

**A1：薄接口 + 空实现**。所有未实现的子包定义完整接口/结构体签名，方法体为空实现（返回零值/nil）+ `⤵️ 回填: X.X` 注释。

**目录逐文件对齐 Python**：Go 文件名与 Python 文件名一一对应，任何人看 Python 代码都能立刻找到 Go 对应文件。

**9.65a 不在本次范围**：tools 层（TeamDatabase/TaskManager/MessageManager）仅定义接口，`⤵️ 回填: 9.65a`。

## 回填标注体系

| 标注 | 含义 | 回填章节 |
|------|------|---------|
| `⤵️ 回填: 7.1` | MemoryIndexManager 真实逻辑（SQLite/FTS5/向量搜索/文件监视） | 领域7.1 CodingMemoryManager |
| `⤵️ 回填: 7.2` | 记忆工具操作真实执行逻辑（搜索/读写/编辑/冲突检测） | 领域7.2 CodingMemoryTools |
| `⤵️ 回填: 7.3` | 记忆工具上下文懒加载 manager 等逻辑 | 领域7.3 CodingMemoryToolContext |
| `⤵️ 回填: 7.4` | MemorySettings 完整配置 + isMemoryEnabled + EmbeddingProvider 创建 | 领域7.4 MemoryConfig |
| `⤵️ 回填: 7.5` | Frontmatter 解析/验证/重建的真实逻辑 | 领域7.5 Frontmatter 解析 |
| `⤵️ 回填: 9.65a` | TeamDatabase/TaskManager/MessageManager 真实实现 | 9.65a Team Tools（新增章节） |

## 依赖对照

### Go 端已完整实现（9.64 直接使用）

| Python 模块 | Go 对应 | 状态 |
|------------|--------|------|
| `harness/workspace/workspace.py` | `agentcore/harness/workspace/workspace.go` | ✅ 完整 |
| `core/sys_operation/sys_operation.py` | `agentcore/sys_operation/sys_operation.go` | ✅ 完整 |
| `harness/deep_agent.py` | `agentcore/harness/deep_agent.go` | ✅ 完整 |
| `core/runner/runner.py` | `agentcore/runner/runner.go` | ✅ 完整 |
| `core/foundation/tool/` (LocalFunction/ToolCard) | `agentcore/foundation/tool/` | ✅ 完整 |
| `single_agent/prompts/builder.py` (PromptSection) | `agentcore/single_agent/prompts/builder.go` | ✅ 完整 |
| `harness/prompts/sections/memory.py` | `harness/prompts/sections/memory.go` | ✅ 完整 |
| `harness/prompts/sections/coding_memory.py` | `harness/prompts/sections/coding_memory.go` | ✅ 完整 |
| `core/foundation/store/base_embedding.py` (EmbeddingConfig) | `agentcore/retrieval/embedding/common.go` | ✅ 完整 |
| `agent_teams/tools/database/config.py` | `agent_teams/tools/database/config.go` | ✅ 配置层 |

### Go 端未实现（9.64 内新建薄接口）

详见下方"各文件详细设计"。

## 目录结构

```
internal/agentcore/memory/lite/          ← 新建，对齐 core/memory/lite/
├── doc.go                              # 包文档
├── config.go                           # MemorySettings + isMemoryEnabled + createMemorySettings   ⤵️ 7.4
├── types.go                            # MemoryChunk 数据类                                       ⤵️ 7.4
├── internal.go                         # 纯计算工具函数（真实实现）                                ⤵️ 7.4（部分真实）
├── frontmatter.go                      # frontmatter 解析/验证/重建                               ⤵️ 7.5
├── conflict_types.go                   # WriteMode + WriteResult（真实实现）                       ⤵️ 7.2
├── embeddings.go                       # EmbeddingProvider 接口 + Mock + resolve_from_env         ⤵️ 7.4
├── manager.go                          # MemoryIndexManager 接口 + Params + SessionDeltaState     ⤵️ 7.1
├── tool_context_base.go                # LiteMemoryToolContextBase                                ⤵️ 7.3
├── tool_context.go                     # MemoryToolContext                                        ⤵️ 7.3
├── coding_memory_tool_context.go       # CodingMemoryToolContext                                  ⤵️ 7.3
├── tool_ops.go                         # memory_search/read/write/edit_with_context               ⤵️ 7.2
├── coding_memory_tool_ops.go           # coding_memory_read/write/edit_with_context               ⤵️ 7.2
└── tools.go                            # initMemoryManagerAsync                                   ⤵️ 7.2

internal/agent_teams/memory/             ← 扩展现有
├── doc.go                              # 更新文件目录
├── config.go                           # 回填：EmbeddingConfig 类型 + ResolveEmbeddingConfig      ⤴️ 9.64 完成
├── config_test.go                      # 更新测试
├── manager_params.go                   # TeamMemoryManagerParams + 类型别名                       ← 新建
├── manager.go                          # TeamMemoryManager 5个生命周期方法                        ⤵️ 7.1+7.2+9.65a
├── shared_memory.go                    # SharedMemoryManager（真实实现）                          ← 新建
├── shared_memory_test.go               # SharedMemoryManager 测试
├── member_memory_toolkit.go            # MemberMemoryToolkit + 工具创建                           ⤵️ 7.2+7.3
├── extractor.go                        # ExtractTeamMemories + EXTRACTION_AGENT_PROMPT            ⤵️ 7.2+9.65a
└── extractor_test.go                   # Extractor 测试

internal/agent_teams/tools/              ← 扩展现有（仅接口定义）
├── doc.go                              # 更新文件目录
├── database/
│   ├── doc.go                          # 更新
│   ├── config.go                       # 已有，不动
│   ├── config_test.go                  # 已有，不动
│   ├── engine.go                       # 引擎初始化                                              ⤵️ 9.65a
│   ├── team_dao.go                     # TeamDao 接口                                            ⤵️ 9.65a
│   ├── member_dao.go                   # MemberDao 接口                                          ⤵️ 9.65a
│   ├── task_dao.go                     # TaskDao 接口                                            ⤵️ 9.65a
│   ├── message_dao.go                  # MessageDao 接口                                         ⤵️ 9.65a
│   └── database.go                     # TeamDatabase 门面接口                                   ⤵️ 9.65a
├── models.go                           # 数据模型（Team/TeamMember/Task/Message）                 ⤵️ 9.65a
├── task_manager.go                     # TeamTaskManager 接口                                    ⤵️ 9.65a
├── message_manager.go                  # TeamMessageManager 接口                                 ⤵️ 9.65a
└── memory_database.go                  # InMemoryTeamDatabase 接口                               ⤵️ 9.65a

internal/agent_teams/models/             ← 回填现有留桩
├── allocator.go                        # RoundRobin/ByModelName/Router 真实实现                   ⤴️ 9.64 完成
├── allocator_test.go                   # 补充测试

现有文件回填：
├── agent/agent_configurator.go         # BuildMemoryManager + UpdateModelPool                     ⤴️ 9.64 完成
├── agent/resources.go                  # MemoryManager/ModelAllocator 类型                       ⤴️ 9.64 完成
├── harness.go                          # RegisterMemberTools + InjectMemberMemory                ⤴️ 9.64 完成
└── spawn/shared_resources.go           # GetSharedDB                                             ⤴️ 9.64 完成
```

## 各文件详细设计

### `agentcore/memory/lite/` — 薄接口层

#### doc.go

包文档说明本包为薄接口阶段，真实逻辑由领域7.x回填。列出文件目录和对应Python路径。

#### config.go

对齐 Python `core/memory/lite/config.py`。

```go
// MemorySettings 记忆配置。⤵️ 回填: 7.4
type MemorySettings struct {
    Provider   string            // 默认 "openai_compatible"
    Model      string            // 默认 "text-embedding-v3"
    Fallback   string            // 默认 "mock"
    Sources    []string          // 默认 ["memory", "sessions"]
    ExtraPaths []string
    Chunking   map[string]any    // {tokens:256, overlap:32}
    Query      map[string]any    // max_results/min_score/hybrid
    Store      map[string]any    // path/vector/fts
    Sync       map[string]any    // watch/intervalMinutes
    Cache      map[string]any    // enabled
}

// IsMemoryEnabled 判断记忆系统是否启用。⤵️ 回填: 7.4 — 当前返回 false
func IsMemoryEnabled() bool { return false }

// CreateMemorySettings 创建默认记忆配置。⤵️ 回填: 7.4 — 当前返回零值
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings { return &MemorySettings{} }
```

#### types.go

对齐 Python `core/memory/lite/types.py`。

```go
// MemoryChunk 记忆分块。⤵️ 回填: 7.4
type MemoryChunk struct {
    Text      string
    StartLine int
    EndLine   int
}
```

#### internal.go — 真实实现

对齐 Python `core/memory/lite/internal.py`。纯计算函数，无外部依赖，**真实实现**。

```go
// EstimateTokens 估算 token 数（~4字符/token）。对齐 Python estimate_tokens
func EstimateTokens(text string) int { return len(text) / 4 }

// ChunkMarkdown 按 token 切分 Markdown。对齐 Python chunk_markdown
func ChunkMarkdown(content string, settings *MemorySettings) []MemoryChunk { ... }

// HashText SHA256 前16字符。对齐 Python hash_text
func HashText(text string) string { ... }

// CosineSimilarity 余弦相似度。对齐 Python cosine_similarity
func CosineSimilarity(vec1, vec2 []float64) float64 { ... }

// BuildFTSQuery 构建 FTS5 查询。⤵️ 回填: 7.4
func BuildFTSQuery(query string) string { return "" }

// BM25RankToScore BM25 排名转相似度分数。⤵️ 回填: 7.4
func BM25RankToScore(rank int) float64 { return 0 }

// IsMemoryPath 判断是否为记忆文件路径。⤵️ 回填: 7.4
func IsMemoryPath(relPath string) bool { return false }

// ListMemoryFiles 列出 workspace 下所有 .md 记忆文件。⤵️ 回填: 7.4
func ListMemoryFiles(workspace any, extraPaths []string, nodeName string) []string { return nil }

// EnsureDir 确保目录存在。对齐 Python ensure_dir
func EnsureDir(path string) error { return os.MkdirAll(path, 0o755) }

// NormalizeExtraMemoryPaths 归一化额外记忆路径。⤵️ 回填: 7.4
func NormalizeExtraMemoryPaths(paths []string, workspaceDir string) []string { return nil }
```

#### frontmatter.go — 薄接口

对齐 Python `core/memory/lite/frontmatter.py`。

```go
// ValidTypes 合法的记忆类型
var ValidTypes = []string{"user", "feedback", "project", "reference"}

// ParseFrontmatter 解析 --- frontmatter。⤵️ 回填: 7.5
func ParseFrontmatter(content string) map[string]string { return nil }

// ValidateFrontmatter 验证 name/description/type 字段。⤵️ 回填: 7.5
func ValidateFrontmatter(fm map[string]string) (bool, string) { return false, "" }

// EnrichFrontmatter 自动填充 created_at/updated_at。⤵️ 回填: 7.5
func EnrichFrontmatter(fm map[string]string, isEdit bool) map[string]string { return nil }

// RebuildContentWithFrontmatter 用更新后的 frontmatter 重建文件内容。⤵️ 回填: 7.5
func RebuildContentWithFrontmatter(content string, fm map[string]string) string { return "" }
```

#### conflict_types.go — 真实实现

对齐 Python `core/memory/lite/conflict_types.py`。纯数据结构。

```go
// WriteMode 写入模式枚举
type WriteMode int

const (
    WriteModeCreate WriteMode = iota  // 创建
    WriteModeAppend                   // 追加
    WriteModeSkip                     // 跳过
)

// WriteResult 写入结果
type WriteResult struct {
    Success          bool
    Path             string
    Mode             WriteMode
    ConflictDetected bool
    ConflictingFiles []string
    Note             string
    Error            string
    Type             string
}

// ToDict 转为字典。对齐 Python WriteResult.to_dict()
func (w *WriteResult) ToDict() map[string]any { ... }
```

#### embeddings.go — 薄接口（MockProvider 真实实现）

对齐 Python `core/memory/lite/embeddings.py`。

```go
// EmbeddingProvider 嵌入向量提供者接口。⤵️ 回填: 7.4
type EmbeddingProvider interface {
    EmbedQuery(ctx context.Context, text string) ([]float64, error)
    EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
}

// MockEmbeddingProvider 模拟嵌入提供者（真实实现，返回零向量）
type MockEmbeddingProvider struct{}

func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, text string) ([]float64, error) {
    return make([]float64, 0), nil
}
func (m *MockEmbeddingProvider) EmbedDocuments(_ context.Context, texts []string) ([][]float64, error) {
    return nil, nil
}

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。⤵️ 回填: 7.4
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *embedding.EmbeddingConfig {
    return nil
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。⤵️ 回填: 7.4
func CreateEmbeddingProvider(provider, model, fallback string, embeddingConfig *embedding.EmbeddingConfig) (EmbeddingProvider, error) {
    return &MockEmbeddingProvider{}, nil
}
```

#### manager.go — 薄接口（核心）

对齐 Python `core/memory/lite/manager.py`。

```go
// MemoryIndexManager 记忆索引管理器接口。⤵️ 回填: 7.1
type MemoryIndexManager interface {
    Initialize(ctx context.Context) error
    Sync(ctx context.Context, reason string, force bool) error
    Search(ctx context.Context, query string, opts map[string]any) ([]map[string]any, error)
    ReadFile(ctx context.Context, relPath string, fromLine *int, lines *int) (map[string]any, error)
    Status() map[string]any
    Close() error
}

// MemoryManagerParams 记忆管理器构造参数
type MemoryManagerParams struct {
    AgentID         string
    Workspace       *workspace.Workspace
    Settings        *MemorySettings
    EmbeddingConfig *embedding.EmbeddingConfig
    SysOperation    sysop.SysOperation
    NodeName        string
}

// GetMemoryIndexManager 幂等获取管理器实例。⤵️ 回填: 7.1
func GetMemoryIndexManager(params MemoryManagerParams) (MemoryIndexManager, error) { return nil, nil }

// ClearMemoryManagerCache 清除缓存。⤵️ 回填: 7.1
func ClearMemoryManagerCache() {}

// SessionDeltaState 会话增量状态。⤵️ 回填: 7.1
type SessionDeltaState struct {
    LastSize         int
    PendingBytes     int
    PendingMessages  []any
}
```

#### tool_context_base.go — 薄接口

对齐 Python `core/memory/lite/memory_tool_context_base.py`。

```go
// LiteMemoryToolContextBase 记忆工具上下文基类。⤵️ 回填: 7.3
type LiteMemoryToolContextBase struct {
    Workspace       *workspace.Workspace
    Settings        *MemorySettings
    AgentID         string
    EmbeddingConfig *embedding.EmbeddingConfig
    SysOperation    sysop.SysOperation
    Manager         MemoryIndexManager
    NodeName        string
}

// EnsureManager 懒加载 manager。⤵️ 回填: 7.3
func (b *LiteMemoryToolContextBase) EnsureManager() bool { return false }
```

#### tool_context.go — 薄接口

对齐 Python `core/memory/lite/memory_tool_context.py`。

```go
// MemoryToolContext 通用记忆工具上下文。⤵️ 回填: 7.3
type MemoryToolContext struct {
    LiteMemoryToolContextBase
}
```

#### coding_memory_tool_context.go — 薄接口

对齐 Python `core/memory/lite/coding_memory_tool_context.py`。

```go
// CodingMemoryToolContext 编程记忆工具上下文。⤵️ 回填: 7.3
type CodingMemoryToolContext struct {
    LiteMemoryToolContextBase
    CodingMemoryDir string
    NodeName        string // "coding_memory"
}
```

#### tool_ops.go — 薄接口

对齐 Python `core/memory/lite/memory_tool_ops.py`。

```go
// ValidateMemoryPath 验证路径在 memory 目录内。⤵️ 回填: 7.2
func ValidateMemoryPath(path string, ws *workspace.Workspace) (bool, string) { return false, "" }

// MemorySearchWithContext 语义搜索记忆。⤵️ 回填: 7.2
func MemorySearchWithContext(ctx *MemoryToolContext, query string, maxResults *int, minScore *float64, sessionKey string) map[string]any { return nil }

// MemoryGetWithContext 获取记忆文件内容。⤵️ 回填: 7.2
func MemoryGetWithContext(ctx *MemoryToolContext, path string, fromLine *int, lines *int) map[string]any { return nil }

// ReadMemoryWithContext 读取记忆文件。⤵️ 回填: 7.2
func ReadMemoryWithContext(ctx *MemoryToolContext, path string, offset *int, limit *int) map[string]any { return nil }

// WriteMemoryWithContext 写入/追加记忆文件。⤵️ 回填: 7.2
func WriteMemoryWithContext(ctx *MemoryToolContext, path string, content string, append bool) map[string]any { return nil }

// EditMemoryWithContext 编辑记忆文件。⤵️ 回填: 7.2
func EditMemoryWithContext(ctx *MemoryToolContext, path string, oldText string, newText string) map[string]any { return nil }
```

#### coding_memory_tool_ops.go — 薄接口

对齐 Python `core/memory/lite/coding_memory_tool_ops.py`。

```go
// ValidateCodingMemoryPath 验证路径在 coding_memory 目录内。⤵️ 回填: 7.2
func ValidateCodingMemoryPath(path string, ws *workspace.Workspace) (bool, string) { return false, "" }

// CodingMemoryReadWithContext 读取 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryReadWithContext(ctx *CodingMemoryToolContext, path string, offset *int, limit *int) map[string]any { return nil }

// CodingMemoryWriteWithContext 写入 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryWriteWithContext(ctx *CodingMemoryToolContext, path string, content string) map[string]any { return nil }

// CodingMemoryEditWithContext 编辑 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryEditWithContext(ctx *CodingMemoryToolContext, path string, oldText string, newText string) map[string]any { return nil }
```

#### tools.go — 薄接口

对齐 Python `core/memory/lite/memory_tools.py` + `coding_memory_tools.py`。

```go
// InitMemoryManagerAsync 初始化通用记忆管理器。⤵️ 回填: 7.2
func InitMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
    return nil, nil
}
```

### `agent_teams/memory/` — 团队记忆层

#### config.go 回填

已有骨架，回填两项：

1. `EmbeddingConfig` 字段从 `any` → `*embedding.EmbeddingConfig`
2. `ResolveEmbeddingConfig` 函数实现（调 `lite.ResolveEmbeddingConfigFromEnv`）

```go
// EmbeddingConfig 嵌入配置。⤴️ 9.64 回填完成
EmbeddingConfig *embedding.EmbeddingConfig `json:"-"`

// ResolveEmbeddingConfig 解析嵌入配置。⤴️ 9.64 回填完成
// 优先级：config 内嵌配置 → 环境变量 → nil
func ResolveEmbeddingConfig(cfg *TeamMemoryConfig) *embedding.EmbeddingConfig {
    if cfg != nil && cfg.EmbeddingConfig != nil {
        return cfg.EmbeddingConfig
    }
    return lite.ResolveEmbeddingConfigFromEnv("", "", "")
}
```

#### manager_params.go — 真实实现（纯数据结构+类型别名）

对齐 Python `agent_teams/memory/manager_params.py`。

```go
// TeamRole 团队角色类型别名
type TeamRole string     // "leader" | "teammate"
// TeamLifecycle 团队生命周期类型别名
type TeamLifecycle string // "temporary" | "persistent"
// TeamScenario 团队场景类型别名
type TeamScenario string  // "general" | "coding"
// TeamLanguage 团队语言类型别名
type TeamLanguage string  // "cn" | "en"
// PromptMode 提示模式类型别名
type PromptMode string    // "proactive" | "passive"

// TeamMemoryManagerParams 记忆管理器构造参数
type TeamMemoryManagerParams struct {
    MemberName              string
    TeamName                string
    Role                    TeamRole
    Lifecycle               TeamLifecycle
    Scenario                TeamScenario
    EmbeddingConfig         *embedding.EmbeddingConfig
    Workspace               *workspace.Workspace
    SysOperation            sysop.SysOperation
    TeamMemoryDir           *string
    Language                TeamLanguage
    PromptMode              PromptMode
    EnableAutoExtract       bool
    ReadOnlySourceWorkspace *string
    DB                      tools.TeamDatabase         // ⤵️ 回填: 9.65a
    TaskManager             tools.TeamTaskManager      // ⤵️ 回填: 9.65a
    ExtractionModel         any                        // ⤵️ 回填: 9.65a (Model类型)
    TimezoneOffsetHours     float64
}
```

#### manager.go — 薄接口（核心管理器）

对齐 Python `agent_teams/memory/manager.py`。

```go
const (
    SectionName        = "team_memory"
    maxPersonalMemoryBytes = 10 * 1024
)

type TeamMemoryManager struct {
    memberName          string
    teamName            string
    role                TeamRole
    lifecycle           TeamLifecycle
    scenario            TeamScenario
    embeddingConfig     *embedding.EmbeddingConfig
    language            TeamLanguage
    promptMode          PromptMode
    enableAutoExtract   bool
    readOnlySource      *string
    db                  tools.TeamDatabase         // ⤵️ 回填: 9.65a
    taskManager         tools.TeamTaskManager      // ⤵️ 回填: 9.65a
    extractionModel     any                        // ⤵️ 回填: 9.65a
    tzOffset            float64
    sysOperation        sysop.SysOperation
    workspace           *workspace.Workspace
    teamMemoryDir       *string
    toolkit             *MemberMemoryToolkit       // ⤵️ 回填: 7.2+7.3
    ownedToolNames      map[string]struct{}        // ⤵️ 回填: 7.2
    ownedToolIDs        map[string]struct{}        // ⤵️ 回填: 7.2
    deepAgentForCleanup any                        // ⤵️ 回填: 7.2
    sharedManager       *SharedMemoryManager
    cachedBaseSection   *saprompt.PromptSection    // ⤵️ 回填: 7.2
}

// InitToolkit 初始化成员记忆工具集。⤵️ 回填: 7.1+7.2+7.3
func (m *TeamMemoryManager) InitToolkit(ctx context.Context) (bool, error) { return false, nil }

// RegisterTools 将记忆工具注册到 DeepAgent。⤵️ 回填: 7.2+9.65a
func (m *TeamMemoryManager) RegisterTools(deepAgent any) {}

// LoadAndInject 加载个人记忆 + 共享记忆 → 注入系统提示词。⤵️ 回填: 7.2
func (m *TeamMemoryManager) LoadAndInject(ctx context.Context, deepAgent any, query string) error { return nil }

// ExtractAfterRound Leader 专属：提取 agent 蒸馏团队记忆。⤵️ 回填: 7.2+9.65a
func (m *TeamMemoryManager) ExtractAfterRound(ctx context.Context) error { return nil }

// Close 反注册工具 + 移除提示词段 + 关闭 toolkit。⤵️ 回填: 7.1+7.2
func (m *TeamMemoryManager) Close(ctx context.Context) error { return nil }

// ExtractionModel 返回提取模型。⤵️ 回填: 9.65a
func (m *TeamMemoryManager) ExtractionModel() any { return m.extractionModel }

// SetExtractionModel 设置提取模型。
func (m *TeamMemoryManager) SetExtractionModel(model any) { m.extractionModel = model }
```

#### shared_memory.go — 真实实现

对齐 Python `agent_teams/memory/shared_memory.py`。纯文件读写，**无外部依赖**，**真实实现**。

```go
const (
    teamMemoryFilename     = "TEAM_MEMORY.md"
    teamMemoryMaxReadLines = 200
)

type SharedMemoryManager struct {
    dir          string
    sysOperation sysop.SysOperation // 可选
}

// EnsureDir 确保团队记忆目录存在。对齐 Python ensure_dir
func (m *SharedMemoryManager) EnsureDir() error { return os.MkdirAll(m.dir, 0o755) }

// ReadTeamSummary 读取团队记忆摘要文件。对齐 Python read_team_summary
// 最多前 200 行，不存在或错误时返回空字符串
func (m *SharedMemoryManager) ReadTeamSummary(ctx context.Context) string { ... }

// WriteTeamSummary 写入团队记忆摘要（覆盖）。对齐 Python write_team_summary
// 优先 sysOperation.fs().WriteFile；失败回退本地原子写入（tempfile + os.replace）
func (m *SharedMemoryManager) WriteTeamSummary(ctx context.Context, content string) error { ... }

// AppendEntry 追加一条团队记忆。对齐 Python append_entry
// 读取现有内容 + 分隔线 + 新条目 → 覆盖写（非原子，适合低频/单 writer）
func (m *SharedMemoryManager) AppendEntry(ctx context.Context, entry string) error { ... }
```

#### member_memory_toolkit.go — 薄接口

对齐 Python `agent_teams/memory/member_memory_toolkit.py`。

```go
type MemberMemoryToolkit struct {
    memberName      string
    teamName        string
    workspace       *workspace.Workspace
    scenario        TeamScenario
    embeddingConfig *embedding.EmbeddingConfig
    sysOperation    sysop.SysOperation
    readOnly        bool
    manager         lite.MemoryIndexManager  // ⤵️ 回填: 7.1
    ctx             any                       // MemoryToolContext 或 CodingMemoryToolContext ⤵️ 回填: 7.3
    tools           []toolbase.Tool           // ⤵️ 回填: 7.2
    initialized     bool
}

// Initialize 初始化工具集。⤵️ 回填: 7.1+7.2+7.3
func (t *MemberMemoryToolkit) Initialize(ctx context.Context) (bool, error) { return false, nil }

// GetTools 返回工具列表。⤵️ 回填: 7.2
func (t *MemberMemoryToolkit) GetTools() []toolbase.Tool { return nil }

// GetToolCards 返回工具卡片列表。⤵️ 回填: 7.2
func (t *MemberMemoryToolkit) GetToolCards() []toolbase.ToolCard { return nil }

// Close 关闭工具集。⤵️ 回填: 7.1
func (t *MemberMemoryToolkit) Close(ctx context.Context) error { return nil }
```

#### extractor.go — 薄接口

对齐 Python `agent_teams/memory/extractor.py`。

```go
const (
    taskContentPreviewMax     = 2000
    messageContentPreviewMax  = 1000
    extractionAgentMaxIterations = 5
)

// ExtractionAgentPrompt 提取 agent 提示词（真实实现，纯字符串常量）
// 对齐 Python EXTRACTION_AGENT_PROMPT
const ExtractionAgentPrompt = `...`  // 完整中文提示词

// BuildExtractionContext 构建提取上下文。⤵️ 回填: 7.2+9.65a
func BuildExtractionContext(tasks []any, messages []any, tzOffsetHours float64) string { return "" }

// CreateExtractionTools 创建提取 agent 限定工具。⤵️ 回填: 7.2
func CreateExtractionTools(teamMemoryDir string, sysOp sysop.SysOperation, teamName string) []toolbase.Tool { return nil }

// ExtractTeamMemories 提取团队记忆。⤵️ 回填: 7.2+9.65a
func ExtractTeamMemories(ctx context.Context, teamName string, db tools.TeamDatabase, taskMgr tools.TeamTaskManager, teamMemoryDir string, sysOp sysop.SysOperation, model any, tzOffsetHours float64) error { return nil }
```

### `agent_teams/tools/` — 薄接口层（仅定义，不实现）

#### database/database.go — TeamDatabase 门面接口

对齐 Python `agent_teams/tools/database/__init__.py`。

```go
// TeamDatabase 团队数据库门面接口。⤵️ 回填: 9.65a
type TeamDatabase interface {
    Initialize(ctx context.Context) error
    CreateCurSessionTables(ctx context.Context) error
    DropCurSessionTables(ctx context.Context) error
    CleanupAllRuntimeState(ctx context.Context) (droppedTables []string, droppedDirs []string, err error)
    DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error)
    Close() error
    Team() TeamDao
    Member() MemberDao
    Task() TaskDao
    Message() MessageDao
}

// TeamDao 团队 DAO 接口。⤵️ 回填: 9.65a
type TeamDao interface {
    CreateTeam(ctx context.Context, ...) error
    GetTeam(ctx context.Context, teamName string) (any, error)
    TeamExists(ctx context.Context, teamName string) bool
    DeleteTeam(ctx context.Context, teamName string) error
}

// MemberDao 成员 DAO 接口。⤵️ 回填: 9.65a
type MemberDao interface {
    CreateMember(ctx context.Context, ...) error
    GetMember(ctx context.Context, teamName, memberName string) (any, error)
    GetTeamMembers(ctx context.Context, teamName string) ([]any, error)
    UpdateMemberStatus(ctx context.Context, ...) error
}

// TaskDao 任务 DAO 接口。⤵️ 回填: 9.65a
type TaskDao interface {
    CreateTask(ctx context.Context, ...) error
    GetTask(ctx context.Context, teamName, taskID string) (any, error)
    GetTeamTasks(ctx context.Context, teamName string) ([]any, error)
    ClaimTask(ctx context.Context, ...) error
    UpdateTaskStatus(ctx context.Context, ...) error
    CancelTask(ctx context.Context, ...) error
}

// MessageDao 消息 DAO 接口。⤵️ 回填: 9.65a
type MessageDao interface {
    CreateMessage(ctx context.Context, ...) error
    GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
    GetMessages(ctx context.Context, ...) ([]any, error)
    MarkMessageRead(ctx context.Context, ...) error
}
```

#### task_manager.go — 薄接口

```go
// TeamTaskManager 团队任务管理器接口。⤵️ 回填: 9.65a
type TeamTaskManager interface {
    Add(ctx context.Context, title, content string, ...) (any, error)
    Get(ctx context.Context, taskID string) (any, error)
    ListTasks(ctx context.Context, status string) ([]any, error)
    Assign(ctx context.Context, taskID, assignee string) error
    Claim(ctx context.Context, taskID string) error
    Complete(ctx context.Context, taskID string) error
    Cancel(ctx context.Context, taskID string) (any, error)
    CancelAllTasks(ctx context.Context, skipAssignees []string) ([]any, error)
    GetClaimableTasks(ctx context.Context) ([]any, error)
    GetTasksByAssignee(ctx context.Context, memberName string, status string) ([]any, error)
}
```

#### message_manager.go — 薄接口

```go
// TeamMessageManager 团队消息管理器接口。⤵️ 回填: 9.65a
type TeamMessageManager interface {
    SendMessage(ctx context.Context, content, to, from string) (string, error)
    BroadcastMessage(ctx context.Context, content, from string) (string, error)
    GetMessages(ctx context.Context, to, from string, unreadOnly bool) ([]any, error)
    GetBroadcastMessages(ctx context.Context, memberName string, unreadOnly bool) ([]any, error)
    GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
    HasUnreadMessages(ctx context.Context, includeBroadcast bool) bool
}
```

#### memory_database.go — 薄接口

```go
// InMemoryTeamDatabase 内存数据库替代实现接口。⤵️ 回填: 9.65a
type InMemoryTeamDatabase interface {
    TeamDatabase
}
```

### `agent_teams/models/allocator.go` — 回填3种 Allocator（真实实现）

对齐 Python `agent_teams/models/allocator.py` 1:1。纯计算+计数器，无外部依赖。

#### RoundRobinModelAllocator

```go
type RoundRobinModelAllocator struct {
    pool       []ModelPoolEntry
    poolDigest string
    index      int
    groups     map[string][]ModelPoolEntry
}

func (a *RoundRobinModelAllocator) Allocate(modelName string) *Allocation { ... }  // 轮询
func (a *RoundRobinModelAllocator) StateDict() map[string]any { ... }
func (a *RoundRobinModelAllocator) LoadStateDict(state map[string]any) { ... }
```

#### ByModelNameAllocator

```go
type ByModelNameAllocator struct {
    groups       map[string][]ModelPoolEntry
    poolDigest   string
    innerIndexes map[string]int
}

func (a *ByModelNameAllocator) Allocate(modelName string) *Allocation { ... }  // 按名+组内轮询
func (a *ByModelNameAllocator) StateDict() map[string]any { ... }  // counters 为列表格式
func (a *ByModelNameAllocator) LoadStateDict(state map[string]any) { ... }  // 兼容旧格式
```

#### RouterAllocator

```go
type RouterAllocator struct {
    pool       []ModelPoolEntry
    byName     map[string]ModelPoolEntry
    poolDigest string
}

func (a *RouterAllocator) Allocate(modelName string) *Allocation { ... }  // 无名→首条目；有名→精确查找
func (a *RouterAllocator) StateDict() map[string]any { ... }
func (a *RouterAllocator) LoadStateDict(state map[string]any) { ... }  // 无计数器，仅校验 digest
```

#### BuildModelAllocatorForPool 回填

```go
func BuildModelAllocatorForPool(pool []ModelPoolEntry, strategy string, teamName string) ModelAllocator {
    if len(pool) == 0 { return nil }
    switch strategy {
    case "round_robin": return &RoundRobinModelAllocator{...}
    case "by_model_name": return &ByModelNameAllocator{...}
    case "router": return &RouterAllocator{...}
    default: return nil  // 未知策略回退
    }
}
```

#### ResolveMemberModelFromPool 回填

```go
func ResolveMemberModelFromPool(pool []ModelPoolEntry, modelName string, modelIndex int) *TeamModelConfig {
    if len(pool) == 0 || modelName == "" { return nil }
    group := filterByName(pool, modelName)
    if len(group) == 0 { return nil }
    idx := modelIndex
    if idx < 0 || idx >= len(group) { idx = 0 }
    return group[idx].ToTeamModelConfig()
}
```

### 现有留桩回填

#### agent/resources.go

```go
// MemoryManager 从 any → *memory.TeamMemoryManager
MemoryManager *memory.TeamMemoryManager  // ⤴️ 9.64 回填完成

// ModelAllocator 从 any → models.ModelAllocator
ModelAllocator models.ModelAllocator  // ⤴️ 9.64 回填完成
```

#### agent/agent_configurator.go

```go
// BuildMemoryManager 构造 TeamMemoryManagerParams + 调 new TeamMemoryManager
func (c *AgentConfigurator) BuildMemoryManager(...) *memory.TeamMemoryManager { ... }  // ⤴️ 9.64 回填完成

// UpdateModelPool 继承池ID + 构建模型分配器
func (c *AgentConfigurator) UpdateModelPool(newPool any) { ... }  // ⤴️ 9.64 回填完成

// RestoreAllocatorState 调 allocator.LoadStateDict
func (c *AgentConfigurator) RestoreAllocatorState(state map[string]any) { ... }  // ⤴️ 9.64 回填完成
```

#### harness.go

```go
// RegisterMemberTools 调 manager.RegisterTools(deepAgent)
func (h *TeamHarness) RegisterMemberTools(memoryManager *memory.TeamMemoryManager) { ... }  // ⤴️ 9.64 回填完成

// InjectMemberMemory 调 manager.LoadAndInject(ctx, deepAgent, query)
func (h *TeamHarness) InjectMemberMemory(ctx context.Context, memoryManager *memory.TeamMemoryManager, query string) error { ... }  // ⤴️ 9.64 回填完成
```

## 测试策略

| 层 | 文件 | 测试覆盖 |
|---|------|---------|
| lite/internal.go | EstimateTokens/ChunkMarkdown/HashText/CosineSimilarity | 真实单元测试 |
| lite/conflict_types.go | WriteResult.ToDict | 真实单元测试 |
| lite/embeddings.go | MockEmbeddingProvider.EmbedQuery/EmbedDocuments | 真实单元测试 |
| memory/shared_memory.go | EnsureDir/ReadTeamSummary/WriteTeamSummary/AppendEntry | 真实测试（t.TempDir()） |
| models/allocator.go | RoundRobin/ByModelName/Router 分配+状态序列化 | 真实单元测试 |
| memory/config.go | ResolveEmbeddingConfig | 真实测试 |
| memory/manager_params.go | 构造+默认值 | 真实测试 |
| 薄接口文件 | 编译验证 | 不写功能测试 |
| 现有留桩回填 | 编译验证 + 类型转换 | 真实测试 |

## 实现计划更新

在 IMPLEMENTATION_PLAN.md 中：

1. 新增行 `| 9.65a | ☐ | Team Tools | TeamDatabase + TaskManager + MessageManager + InMemoryDB | openjiuwen/agent_teams/tools/ |`
2. 9.64 行状态从 `☐` → `🔄`
