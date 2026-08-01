# 9.64 Team Memory 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 9.64 Team Memory — 团队共享记忆系统（薄接口+空实现策略），包含 agentcore/memory/lite/ 薄接口层、agent_teams/memory/ 团队记忆层、agent_teams/tools/ 薄接口层、models/allocator 回填、以及现有留桩回填。

**Architecture:** 薄接口+空实现，目录逐文件对齐 Python。回填标注：lite/ → 7.x，tools/ → 9.65a。SharedMemoryManager、ModelAllocator 三种策略、internal.go 纯函数为真实实现。

**Tech Stack:** Go 1.x, t.TempDir() for file tests, Go 编译验证

---

## Task 1: 更新 IMPLEMENTATION_PLAN.md + 创建 lite/ 包基础结构

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md` (新增 9.65a 行 + 9.64 行改 🔄)
- Create: `internal/agentcore/memory/lite/doc.go`

- [ ] **Step 1: 更新 IMPLEMENTATION_PLAN.md**

在 9.65 行之前新增：
```
| 9.65a | ☐ | Team Tools | TeamDatabase + TaskManager + MessageManager + InMemoryDB | openjiuwen/agent_teams/tools/ |
```

将 9.64 行状态从 `☐` 改为 `🔄`。

- [ ] **Step 2: 创建 lite/ 包 doc.go**

创建 `internal/agentcore/memory/lite/doc.go`，内容：

```go
// Package lite 提供轻量级记忆系统的接口定义。
//
// 本包对齐 Python openjiuwen/core/memory/lite/ 的目录结构，
// 定义 MemoryIndexManager、MemorySettings、工具上下文、工具操作等接口。
// 当前为薄接口+空实现阶段，真实逻辑由领域 7.x 各章节回填。
//
// 文件目录：
//
//	lite/
//	├── doc.go                       # 包文档
//	├── config.go                    # MemorySettings + IsMemoryEnabled + CreateMemorySettings    ⤵️ 7.4
//	├── types.go                     # MemoryChunk 数据类                                          ⤵️ 7.4
//	├── internal.go                  # 纯计算工具函数（部分真实实现）                               ⤵️ 7.4
//	├── frontmatter.go               # frontmatter 解析/验证/重建                                  ⤵️ 7.5
//	├── conflict_types.go            # WriteMode + WriteResult（真实实现）                         ⤵️ 7.2
//	├── embeddings.go                # EmbeddingProvider 接口 + Mock + resolve_from_env           ⤵️ 7.4
//	├── manager.go                   # MemoryIndexManager 接口 + Params + SessionDeltaState       ⤵️ 7.1
//	├── tool_context_base.go         # LiteMemoryToolContextBase                                   ⤵️ 7.3
//	├── tool_context.go              # MemoryToolContext                                           ⤵️ 7.3
//	├── coding_memory_tool_context.go # CodingMemoryToolContext                                   ⤵️ 7.3
//	├── tool_ops.go                  # memory_search/read/write/edit_with_context                  ⤵️ 7.2
//	├── coding_memory_tool_ops.go    # coding_memory_read/write/edit_with_context                  ⤵️ 7.2
//	└── tools.go                     # InitMemoryManagerAsync                                      ⤵️ 7.2
//
// 对应 Python 代码：openjiuwen/core/memory/lite/
package lite
```

- [ ] **Step 3: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/lite/...`
Expected: PASS

- [ ] **Step 4: Commit**

```
git add IMPLEMENTATION_PLAN.md internal/agentcore/memory/lite/doc.go
git commit -m "feat(9.64): 新增 9.65a 章节 + 创建 lite 包基础结构"
```

---

## Task 2: lite/ 薄接口层 — config + types + frontmatter

**Files:**
- Create: `internal/agentcore/memory/lite/config.go`
- Create: `internal/agentcore/memory/lite/types.go`
- Create: `internal/agentcore/memory/lite/frontmatter.go`

- [ ] **Step 1: 创建 config.go**

对齐 Python `core/memory/lite/config.py`，薄接口+空实现。

```go
package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySettings 记忆配置。⤵️ 回填: 7.4
type MemorySettings struct {
	// Provider 嵌入提供者类型，默认 "openai_compatible"
	Provider string
	// Model 嵌入模型名称，默认 "text-embedding-v3"
	Model string
	// Fallback 嵌入回退策略，默认 "mock"
	Fallback string
	// Sources 记忆来源目录列表，默认 ["memory", "sessions"]
	Sources []string
	// ExtraPaths 额外记忆路径
	ExtraPaths []string
	// Chunking 分块配置，含 tokens 和 overlap
	Chunking map[string]any
	// Query 查询配置，含 max_results/min_score/hybrid
	Query map[string]any
	// Store 存储配置，含 path/vector/fts
	Store map[string]any
	// Sync 同步配置，含 watch/intervalMinutes
	Sync map[string]any
	// Cache 缓存配置
	Cache map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsMemoryEnabled 判断记忆系统是否启用。
// ⤵️ 回填: 7.4 — 当前始终返回 false
func IsMemoryEnabled() bool { return false }

// CreateMemorySettings 创建默认记忆配置。
// ⤵️ 回填: 7.4 — 当前返回零值 MemorySettings
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	return &MemorySettings{}
}
```

- [ ] **Step 2: 创建 types.go**

对齐 Python `core/memory/lite/types.py`。

```go
package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryChunk 记忆分块。⤵️ 回填: 7.4
type MemoryChunk struct {
	// Text 分块文本内容
	Text string
	// StartLine 起始行号
	StartLine int
	// EndLine 结束行号
	EndLine int
}
```

- [ ] **Step 3: 创建 frontmatter.go**

对齐 Python `core/memory/lite/frontmatter.py`，薄接口。

```go
package lite

// ──────────────────────────── 常量 ────────────────────────────

// ValidTypes 合法的记忆类型
var ValidTypes = []string{"user", "feedback", "project", "reference"}

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseFrontmatter 解析 --- frontmatter。
// ⤵️ 回填: 7.5 — 当前返回 nil
func ParseFrontmatter(content string) map[string]string { return nil }

// ValidateFrontmatter 验证 name/description/type 字段。
// ⤵️ 回填: 7.5 — 当前返回 false
func ValidateFrontmatter(fm map[string]string) (bool, string) { return false, "" }

// EnrichFrontmatter 自动填充 created_at/updated_at。
// ⤵️ 回填: 7.5 — 当前返回 nil
func EnrichFrontmatter(fm map[string]string, isEdit bool) map[string]string { return nil }

// RebuildContentWithFrontmatter 用更新后的 frontmatter 重建文件内容。
// ⤵️ 回填: 7.5 — 当前返回空字符串
func RebuildContentWithFrontmatter(content string, fm map[string]string) string { return "" }
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/lite/...`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/agentcore/memory/lite/config.go internal/agentcore/memory/lite/types.go internal/agentcore/memory/lite/frontmatter.go
git commit -m "feat(9.64): lite 薄接口 — config/types/frontmatter"
```

---

## Task 3: lite/ 真实实现 — conflict_types + internal 纯函数 (TDD)

**Files:**
- Create: `internal/agentcore/memory/lite/conflict_types.go`
- Create: `internal/agentcore/memory/lite/conflict_types_test.go`
- Create: `internal/agentcore/memory/lite/internal.go`
- Create: `internal/agentcore/memory/lite/internal_test.go`

- [ ] **Step 1: 创建 conflict_types.go — 真实实现**

```go
package lite

// ──────────────────────────── 枚举 ────────────────────────────

// WriteMode 写入模式枚举
type WriteMode int

const (
	// WriteModeCreate 创建
	WriteModeCreate WriteMode = iota
	// WriteModeAppend 追加
	WriteModeAppend
	// WriteModeSkip 跳过
	WriteModeSkip
)

// ──────────────────────────── 结构体 ────────────────────────────

// WriteResult 写入结果。对齐 Python WriteResult (conflict_types.py)
type WriteResult struct {
	// Success 是否成功
	Success bool
	// Path 文件路径
	Path string
	// Mode 写入模式
	Mode WriteMode
	// ConflictDetected 是否检测到冲突
	ConflictDetected bool
	// ConflictingFiles 冲突文件列表
	ConflictingFiles []string
	// Note 备注
	Note string
	// Error 错误信息
	Error string
	// Type 类型
	Type string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// ToDict 转为字典。对齐 Python WriteResult.to_dict()
func (w *WriteResult) ToDict() map[string]any {
	return map[string]any{
		"success":           w.Success,
		"path":              w.Path,
		"mode":              int(w.Mode),
		"conflict_detected": w.ConflictDetected,
		"conflicting_files": w.ConflictingFiles,
		"note":              w.Note,
		"error":             w.Error,
		"type":              w.Type,
	}
}
```

- [ ] **Step 2: 创建 conflict_types_test.go**

```go
package lite

import "testing"

// TestWriteResult_ToDict 测试 WriteResult.ToDict
func TestWriteResult_ToDict(t *testing.T) {
	wr := WriteResult{
		Success:          true,
		Path:             "/tmp/test.md",
		Mode:             WriteModeCreate,
		ConflictDetected: false,
		ConflictingFiles: nil,
		Note:             "ok",
		Error:            "",
		Type:             "memory",
	}
	dict := wr.ToDict()
	if dict["success"] != true {
		t.Errorf("Expected success=true, got %v", dict["success"])
	}
	if dict["path"] != "/tmp/test.md" {
		t.Errorf("Expected path=/tmp/test.md, got %v", dict["path"])
	}
	if dict["mode"] != 0 {
		t.Errorf("Expected mode=0 (Create), got %v", dict["mode"])
	}
}
```

- [ ] **Step 3: 创建 internal.go — 部分真实实现**

对齐 Python `core/memory/lite/internal.py`。纯计算函数真实实现，依赖 workspace 的函数薄接口。

```go
package lite

import (
	"crypto/sha256"
	"strings"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// EstimateTokens 估算 token 数（~4字符/token）。
// 对齐 Python estimate_tokens — 真实实现
func EstimateTokens(text string) int {
	return len(text) / 4
}

// HashText SHA256 前16字符。对齐 Python hash_text — 真实实现
func HashText(text string) string {
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:8]) // 前 8 字节 = 16 字符
}

// CosineSimilarity 余弦相似度。对齐 Python cosine_similarity — 真实实现
func CosineSimilarity(vec1, vec2 []float64) float64 {
	if len(vec1) != len(vec2) || len(vec1) == 0 {
		return 0
	}
	var dot, norm1, norm2 float64
	for i := range vec1 {
		dot += vec1[i] * vec2[i]
		norm1 += vec1[i] * vec1[i]
		norm2 += vec2[i] * vec2[i]
	}
	if norm1 == 0 || norm2 == 0 {
		return 0
	}
	return dot / (sqrt(norm1) * sqrt(norm2))
}

// ChunkMarkdown 按 token 切分 Markdown。
// 对齐 Python chunk_markdown — 真实实现
func ChunkMarkdown(content string, maxTokens int, overlap int) []MemoryChunk {
	if maxTokens <= 0 {
		maxTokens = 256
	}
	if overlap <= 0 {
		overlap = 32
	}
	lines := strings.Split(content, "\n")
	var chunks []MemoryChunk
	var currentLines []string
	var currentTokens int
	startLine := 0

	for i, line := range lines {
		lineTokens := EstimateTokens(line)
		if currentTokens + lineTokens > maxTokens && len(currentLines) > 0 {
			chunks = append(chunks, MemoryChunk{
				Text:      strings.Join(currentLines, "\n"),
				StartLine: startLine,
				EndLine:   i - 1,
			})
			// overlap: 回退 overlap tokens 的行数
			overlapLines := 0
			overlapTokens := 0
			for j := len(currentLines) - 1; j >= 0 && overlapTokens < overlap; j-- {
				overlapTokens += EstimateTokens(currentLines[j])
				overlapLines++
			}
			currentLines = currentLines[len(currentLines)-overlapLines:]
			currentTokens = overlapTokens
			startLine = i - overlapLines
		}
		currentLines = append(currentLines, line)
		currentTokens += lineTokens
	}
	if len(currentLines) > 0 {
		chunks = append(chunks, MemoryChunk{
			Text:      strings.Join(currentLines, "\n"),
			StartLine: startLine,
			EndLine:   len(lines) - 1,
		})
	}
	return chunks
}

// EnsureDir 确保目录存在。对齐 Python ensure_dir — 真实实现
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0o755)
}

// ──────────────────────────── 薄接口函数 ────────────────────────────

// BuildFTSQuery 构建 FTS5 查询。⤵️ 回填: 7.4
func BuildFTSQuery(query string) string { return "" }

// BM25RankToScore BM25 排名转相似度分数。⤵️ 回填: 7.4
func BM25RankToScore(rank int) float64 { return 0 }

// IsMemoryPath 判断是否为记忆文件路径。⤵️ 回填: 7.4
func IsMemoryPath(relPath string) bool { return false }

// ListMemoryFiles 列出 workspace 下所有 .md 记忆文件。⤵️ 回填: 7.4
func ListMemoryFiles(workspace any, extraPaths []string, nodeName string) []string { return nil }

// NormalizeExtraMemoryPaths 归一化额外记忆路径。⤵️ 回填: 7.4
func NormalizeExtraMemoryPaths(paths []string, workspaceDir string) []string { return nil }
```

注意：internal.go 需要导入 `crypto/sha256`, `encoding/hex`, `math`, `os`, `strings`。`CosineSimilarity` 用 `math.Sqrt`；`hex.EncodeToString` 用于 `HashText`。

- [ ] **Step 4: 创建 internal_test.go**

```go
package lite

import (
	"math"
	"testing"
)

func TestEstimateTokens(t *testing.T) {
	if EstimateTokens("hello world") != 2 { // 11/4 ≈ 2 (整数除法)
		t.Errorf("Expected 2, got %d", EstimateTokens("hello world"))
	}
	if EstimateTokens("") != 0 {
		t.Errorf("Expected 0 for empty string")
	}
}

func TestHashText(t *testing.T) {
	h := HashText("test")
	if len(h) != 16 { // SHA256 前8字节=16字符
		t.Errorf("Expected 16 char hash, got %d chars", len(h))
	}
	// 相同输入相同输出
	if HashText("test") != h {
		t.Errorf("HashText not deterministic")
	}
}

func TestCosineSimilarity(t *testing.T) {
	// 相同向量 → 1.0
	vec := []float64{1.0, 2.0, 3.0}
	sim := CosineSimilarity(vec, vec)
	if math.Abs(sim-1.0) > 0.001 {
		t.Errorf("Expected ~1.0 for same vectors, got %f", sim)
	}
	// 长度不等 → 0
	if CosineSimilarity([]float64{1.0}, []float64{1.0, 2.0}) != 0 {
		t.Errorf("Expected 0 for mismatched lengths")
	}
	// 零向量 → 0
	if CosineSimilarity([]float64{0, 0}, []float64{1, 2}) != 0 {
		t.Errorf("Expected 0 for zero vector")
	}
}

func TestChunkMarkdown(t *testing.T) {
	content := "line1\nline2\nline3\nline4\nline5"
	chunks := ChunkMarkdown(content, 10, 2)
	if len(chunks) == 0 {
		t.Errorf("Expected at least 1 chunk")
	}
	// 验证第一个 chunk 的 StartLine
	if chunks[0].StartLine != 0 {
		t.Errorf("Expected StartLine=0, got %d", chunks[0].StartLine)
	}
}
```

- [ ] **Step 5: 运行测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add internal/agentcore/memory/lite/conflict_types.go internal/agentcore/memory/lite/conflict_types_test.go internal/agentcore/memory/lite/internal.go internal/agentcore/memory/lite/internal_test.go
git commit -m "feat(9.64): lite 真实实现 — conflict_types + internal 纯函数"
```

---

## Task 4: lite/ 薄接口层 — embeddings + manager + tool contexts

**Files:**
- Create: `internal/agentcore/memory/lite/embeddings.go`
- Create: `internal/agentcore/memory/lite/embeddings_test.go`
- Create: `internal/agentcore/memory/lite/manager.go`
- Create: `internal/agentcore/memory/lite/tool_context_base.go`
- Create: `internal/agentcore/memory/lite/tool_context.go`
- Create: `internal/agentcore/memory/lite/coding_memory_tool_context.go`

- [ ] **Step 1: 创建 embeddings.go**

对齐 Python `core/memory/lite/embeddings.py`。EmbeddingProvider 接口 + MockEmbeddingProvider 真实实现 + 其余薄接口。

导入需要：
- `context` 包
- `github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding` 包（EmbeddingConfig）

```go
package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 接口 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。⤵️ 回填: 7.4
type EmbeddingProvider interface {
	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 嵌入多个文档文本
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// MockEmbeddingProvider 模拟嵌入提供者。真实实现，返回零向量
type MockEmbeddingProvider struct{}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockEmbeddingProvider 创建模拟嵌入提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{}
}

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。
// ⤵️ 回填: 7.4 — 当前返回 nil
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *embedding.EmbeddingConfig {
	return nil
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。
// ⤵️ 回填: 7.4 — 当前返回 MockEmbeddingProvider
func CreateEmbeddingProvider(provider, model, fallback string, embeddingConfig *embedding.EmbeddingConfig) (EmbeddingProvider, error) {
	return NewMockEmbeddingProvider(), nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// EmbedQuery 真实实现，返回空向量
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, _ string) ([]float64, error) {
	return make([]float64, 0), nil
}

// EmbedDocuments 真实实现，返回空切片
func (m *MockEmbeddingProvider) EmbedDocuments(_ context.Context, _ []string) ([][]float64, error) {
	return nil, nil
}
```

- [ ] **Step 2: 创建 embeddings_test.go**

```go
package lite

import (
	"context"
	"testing"
)

func TestMockEmbeddingProvider_EmbedQuery(t *testing.T) {
	p := NewMockEmbeddingProvider()
	vec, err := p.EmbedQuery(context.Background(), "test")
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if len(vec) != 0 {
		t.Errorf("Expected empty vector, got len=%d", len(vec))
	}
}

func TestMockEmbeddingProvider_EmbedDocuments(t *testing.T) {
	p := NewMockEmbeddingProvider()
	vecs, err := p.EmbedDocuments(context.Background(), []string{"a", "b"})
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if vecs != nil {
		t.Errorf("Expected nil, got %v", vecs)
	}
}

func TestCreateEmbeddingProvider(t *testing.T) {
	p, err := CreateEmbeddingProvider("mock", "test-model", "mock", nil)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}
	if p == nil {
		t.Errorf("Expected non-nil provider")
	}
}

func TestResolveEmbeddingConfigFromEnv(t *testing.T) {
	cfg := ResolveEmbeddingConfigFromEnv("", "", "")
	if cfg != nil {
		t.Errorf("Expected nil when no env vars set, got %v", cfg)
	}
}
```

- [ ] **Step 3: 创建 manager.go**

对齐 Python `core/memory/lite/manager.py`。薄接口。

导入：`context`, `workspace`, `sysop`, `embedding`

```go
package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 接口 ────────────────────────────

// MemoryIndexManager 记忆索引管理器接口。⤵️ 回填: 7.1
type MemoryIndexManager interface {
	// Initialize 初始化管理器（打开数据库、建 schema、初始化 provider）
	Initialize(ctx context.Context) error
	// Sync 同步索引（增量或全量 reindex）
	Sync(ctx context.Context, reason string, force bool) error
	// Search 混合搜索（向量 + FTS5 关键词）
	Search(ctx context.Context, query string, opts map[string]any) ([]map[string]any, error)
	// ReadFile 读取记忆文件内容
	ReadFile(ctx context.Context, relPath string, fromLine *int, lines *int) (map[string]any, error)
	// Status 返回系统状态报告
	Status() map[string]any
	// Close 关闭管理器
	Close() error
}

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryManagerParams 记忆管理器构造参数
type MemoryManagerParams struct {
	// AgentID Agent 标识
	AgentID string
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// Settings 记忆配置
	Settings *MemorySettings
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
	// NodeName 节点名称（"memory" 或 "coding_memory"）
	NodeName string
}

// SessionDeltaState 会话增量状态。⤵️ 回填: 7.1
type SessionDeltaState struct {
	// LastSize 上次文件大小
	LastSize int
	// PendingBytes 待处理字节
	PendingBytes int
	// PendingMessages 待处理消息
	PendingMessages []any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemoryIndexManager 幂等获取管理器实例。⤵️ 回填: 7.1
func GetMemoryIndexManager(params MemoryManagerParams) (MemoryIndexManager, error) {
	return nil, nil
}

// ClearMemoryManagerCache 清除缓存。⤵️ 回填: 7.1
func ClearMemoryManagerCache() {}
```

- [ ] **Step 4: 创建 tool_context_base.go**

导入：`workspace`, `sysop`, `embedding`

```go
package lite

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 结构体 ────────────────────────────

// LiteMemoryToolContextBase 记忆工具上下文基类。⤵️ 回填: 7.3
type LiteMemoryToolContextBase struct {
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// Settings 记忆配置
	Settings *MemorySettings
	// AgentID Agent 标识
	AgentID string
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
	// Manager 记忆索引管理器
	Manager MemoryIndexManager
	// NodeName 节点名称
	NodeName string
}

// ──────────────────────────── 导出函数 ────────────────────────────

// EnsureManager 懒加载 manager。⤵️ 回填: 7.3 — 当前返回 false
func (b *LiteMemoryToolContextBase) EnsureManager() bool { return false }
```

- [ ] **Step 5: 创建 tool_context.go**

```go
package lite

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryToolContext 通用记忆工具上下文。⤵️ 回填: 7.3
type MemoryToolContext struct {
	LiteMemoryToolContextBase
}
```

- [ ] **Step 6: 创建 coding_memory_tool_context.go**

```go
package lite

// ──────────────────────────── 结构体 ────────────────────────────

// CodingMemoryToolContext 编程记忆工具上下文。⤵️ 回填: 7.3
type CodingMemoryToolContext struct {
	LiteMemoryToolContextBase
	// CodingMemoryDir 编程记忆目录路径
	CodingMemoryDir string
	// NodeName 节点名称，固定 "coding_memory"
	NodeName string
}
```

- [ ] **Step 7: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/lite/...`
Expected: PASS

- [ ] **Step 8: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/... -v`
Expected: PASS

- [ ] **Step 9: Commit**

```
git add internal/agentcore/memory/lite/embeddings.go internal/agentcore/memory/lite/embeddings_test.go internal/agentcore/memory/lite/manager.go internal/agentcore/memory/lite/tool_context_base.go internal/agentcore/memory/lite/tool_context.go internal/agentcore/memory/lite/coding_memory_tool_context.go
git commit -m "feat(9.64): lite 薄接口 — embeddings + manager + tool contexts"
```

---

## Task 5: lite/ 薄接口层 — tool_ops + coding_memory_tool_ops + tools

**Files:**
- Create: `internal/agentcore/memory/lite/tool_ops.go`
- Create: `internal/agentcore/memory/lite/coding_memory_tool_ops.go`
- Create: `internal/agentcore/memory/lite/tools.go`

- [ ] **Step 1: 创建 tool_ops.go**

对齐 Python `core/memory/lite/memory_tool_ops.py`。

```go
package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateMemoryPath 验证路径在 memory 目录内。⤵️ 回填: 7.2
func ValidateMemoryPath(path string, ws *workspace.Workspace) (bool, string) { return false, "" }

// MemorySearchWithContext 语义搜索记忆。⤵️ 回填: 7.2
func MemorySearchWithContext(ctx context.Context, toolCtx *MemoryToolContext, query string, maxResults *int, minScore *float64, sessionKey string) map[string]any { return nil }

// MemoryGetWithContext 获取记忆文件内容。⤵️ 回填: 7.2
func MemoryGetWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, fromLine *int, lines *int) map[string]any { return nil }

// ReadMemoryWithContext 读取记忆文件。⤵️ 回填: 7.2
func ReadMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, offset *int, limit *int) map[string]any { return nil }

// WriteMemoryWithContext 写入/追加记忆文件。⤵️ 回填: 7.2
func WriteMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, content string, appendMode bool) map[string]any { return nil }

// EditMemoryWithContext 编辑记忆文件。⤵️ 回填: 7.2
func EditMemoryWithContext(ctx context.Context, toolCtx *MemoryToolContext, path string, oldText string, newText string) map[string]any { return nil }
```

- [ ] **Step 2: 创建 coding_memory_tool_ops.go**

对齐 Python `core/memory/lite/coding_memory_tool_ops.py`。

```go
package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// ValidateCodingMemoryPath 验证路径在 coding_memory 目录内。⤵️ 回填: 7.2
func ValidateCodingMemoryPath(path string, ws *workspace.Workspace) (bool, string) { return false, "" }

// CodingMemoryReadWithContext 读取 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryReadWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, offset *int, limit *int) map[string]any { return nil }

// CodingMemoryWriteWithContext 写入 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryWriteWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, content string) map[string]any { return nil }

// CodingMemoryEditWithContext 编辑 coding_memory 文件。⤵️ 回填: 7.2
func CodingMemoryEditWithContext(ctx context.Context, toolCtx *CodingMemoryToolContext, path string, oldText string, newText string) map[string]any { return nil }
```

- [ ] **Step 3: 创建 tools.go**

对齐 Python `core/memory/lite/memory_tools.py` + `coding_memory_tools.py`。

```go
package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 导出函数 ────────────────────────────

// InitMemoryManagerAsync 初始化通用记忆管理器。⤵️ 回填: 7.2
func InitMemoryManagerAsync(ctx context.Context, ws *workspace.Workspace, agentID string, embeddingConfig *embedding.EmbeddingConfig, sysOp sysop.SysOperation) (MemoryIndexManager, error) {
	return nil, nil
}
```

- [ ] **Step 4: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agentcore/memory/lite/... && go test ./internal/agentcore/memory/lite/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```
git add internal/agentcore/memory/lite/tool_ops.go internal/agentcore/memory/lite/coding_memory_tool_ops.go internal/agentcore/memory/lite/tools.go
git commit -m "feat(9.64): lite 薄接口 — tool_ops + coding_memory_tool_ops + tools"
```

---

## Task 6: agent_teams/tools/ 薄接口层 — TeamDatabase + DAO + TaskManager + MessageManager

**Files:**
- Modify: `internal/agent_teams/tools/doc.go`
- Create: `internal/agent_teams/tools/database/database.go`
- Create: `internal/agent_teams/tools/database/engine.go`
- Create: `internal/agent_teams/tools/database/team_dao.go`
- Create: `internal/agent_teams/tools/database/member_dao.go`
- Create: `internal/agent_teams/tools/database/task_dao.go`
- Create: `internal/agent_teams/tools/database/message_dao.go`
- Modify: `internal/agent_teams/tools/database/doc.go`
- Create: `internal/agent_teams/tools/task_manager.go`
- Create: `internal/agent_teams/tools/message_manager.go`
- Create: `internal/agent_teams/tools/memory_database.go`
- Create: `internal/agent_teams/tools/models.go`

- [ ] **Step 1: 更新 tools/doc.go**

在文件目录中添加新文件的条目。

- [ ] **Step 2: 更新 database/doc.go**

在文件目录中添加新文件条目。

- [ ] **Step 3: 创建 database/database.go — TeamDatabase 门面接口**

```go
package database

import "context"

// ──────────────────────────── 接口 ────────────────────────────

// TeamDatabase 团队数据库门面接口。⤵️ 回填: 9.65a
type TeamDatabase interface {
	// Initialize 初始化数据库
	Initialize(ctx context.Context) error
	// CreateCurSessionTables 创建当前会话动态表
	CreateCurSessionTables(ctx context.Context) error
	// DropCurSessionTables 删除当前会话动态表
	DropCurSessionTables(ctx context.Context) error
	// CleanupAllRuntimeState 清理所有运行时状态
	CleanupAllRuntimeState(ctx context.Context) (droppedTables []string, droppedDirs []string, err error)
	// DropSessionTablesByID 按 sessionID 删除动态表
	DropSessionTablesByID(ctx context.Context, sessionID string) ([]string, error)
	// Close 关闭数据库
	Close() error
	// Team 返回团队 DAO
	Team() TeamDao
	// Member 返回成员 DAO
	Member() MemberDao
	// Task 返回任务 DAO
	Task() TaskDao
	// Message 返回消息 DAO
	Message() MessageDao
}

// TeamDao 团队 DAO 接口。⤵️ 回填: 9.65a
type TeamDao interface {
	// CreateTeam 创建团队
	CreateTeam(ctx context.Context, teamName string, displayName string, leaderMemberName string, desc string) error
	// GetTeam 获取团队信息
	GetTeam(ctx context.Context, teamName string) (any, error)
	// TeamExists 团队是否存在
	TeamExists(ctx context.Context, teamName string) bool
	// DeleteTeam 删除团队
	DeleteTeam(ctx context.Context, teamName string) error
}

// MemberDao 成员 DAO 接口。⤵️ 回填: 9.65a
type MemberDao interface {
	// CreateMember 创建成员
	CreateMember(ctx context.Context, memberName string, teamName string, displayName string, agentCard string, role string, desc string) error
	// GetMember 获取成员信息
	GetMember(ctx context.Context, teamName string, memberName string) (any, error)
	// GetTeamMembers 获取团队成员列表
	GetTeamMembers(ctx context.Context, teamName string) ([]any, error)
	// UpdateMemberStatus 更新成员状态
	UpdateMemberStatus(ctx context.Context, teamName string, memberName string, status string) error
}

// TaskDao 任务 DAO 接口。⤵️ 回填: 9.65a
type TaskDao interface {
	// CreateTask 创建任务
	CreateTask(ctx context.Context, taskID string, teamName string, title string, content string, status string, assignee string) error
	// GetTask 获取任务
	GetTask(ctx context.Context, teamName string, taskID string) (any, error)
	// GetTeamTasks 获取团队任务列表
	GetTeamTasks(ctx context.Context, teamName string) ([]any, error)
	// ClaimTask 认领任务
	ClaimTask(ctx context.Context, teamName string, taskID string, assignee string) error
	// UpdateTaskStatus 更新任务状态
	UpdateTaskStatus(ctx context.Context, teamName string, taskID string, status string) error
	// CancelTask 取消任务
	CancelTask(ctx context.Context, teamName string, taskID string) error
}

// MessageDao 消息 DAO 接口。⤵️ 回填: 9.65a
type MessageDao interface {
	// CreateMessage 创建消息
	CreateMessage(ctx context.Context, messageID string, teamName string, fromMemberName string, toMemberName string, content string, broadcast bool) error
	// GetTeamMessages 获取团队所有消息
	GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
	// GetMessages 获取指定成员的消息
	GetMessages(ctx context.Context, teamName string, toMemberName string, unreadOnly bool) ([]any, error)
	// MarkMessageRead 标记消息已读
	MarkMessageRead(ctx context.Context, teamName string, messageID string, memberName string) error
}
```

- [ ] **Step 4: 创建 database/engine.go — 薄接口**

```go
package database

import "context"

// ──────────────────────────── 导出函数 ────────────────────────────

// InitializeEngine 初始化数据库引擎。⤵️ 回填: 9.65a
func InitializeEngine(ctx context.Context, cfg DBConfigProvider) (any, error) { return nil, nil }

// CreateCurSessionTablesFromEngine 从引擎创建当前会话表。⤵️ 回填: 9.65a
func CreateCurSessionTablesFromEngine(ctx context.Context, engine any) error { return nil }

// DropCurSessionTablesFromEngine 从引擎删除当前会话表。⤵️ 回填: 9.65a
func DropCurSessionTablesFromEngine(ctx context.Context, engine any) error { return nil }

// CleanupAllRuntimeStateFromEngine 从引擎清理所有运行时状态。⤵️ 回填: 9.65a
func CleanupAllRuntimeStateFromEngine(ctx context.Context, engine any) ([]string, []string, error) { return nil, nil, nil }

// DropSessionTablesByIDFromEngine 按 ID 删除动态表。⤵️ 回填: 9.65a
func DropSessionTablesByIDFromEngine(ctx context.Context, engine any, sessionID string) ([]string, error) { return nil, nil }
```

- [ ] **Step 5: 创建 task_manager.go — 薄接口**

```go
package tools

import "context"

// ──────────────────────────── 接口 ────────────────────────────

// TeamTaskManager 团队任务管理器接口。⤵️ 回填: 9.65a
type TeamTaskManager interface {
	// Add 添加任务
	Add(ctx context.Context, title string, content string) (any, error)
	// Get 获取任务
	Get(ctx context.Context, taskID string) (any, error)
	// ListTasks 列出任务
	ListTasks(ctx context.Context, status string) ([]any, error)
	// Assign 分配任务
	Assign(ctx context.Context, taskID string, assignee string) error
	// Claim 认领任务
	Claim(ctx context.Context, taskID string) error
	// Complete 完成任务
	Complete(ctx context.Context, taskID string) error
	// Cancel 取消任务
	Cancel(ctx context.Context, taskID string) (any, error)
	// CancelAllTasks 批量取消任务
	CancelAllTasks(ctx context.Context, skipAssignees []string) ([]any, error)
	// GetClaimableTasks 获取可认领任务
	GetClaimableTasks(ctx context.Context) ([]any, error)
	// GetTasksByAssignee 按分配人查任务
	GetTasksByAssignee(ctx context.Context, memberName string, status string) ([]any, error)
}
```

- [ ] **Step 6: 创建 message_manager.go — 薄接口**

```go
package tools

import "context"

// ──────────────────────────── 接口 ────────────────────────────

// TeamMessageManager 团队消息管理器接口。⤵️ 回填: 9.65a
type TeamMessageManager interface {
	// SendMessage 发送消息
	SendMessage(ctx context.Context, content string, to string, from string) (string, error)
	// BroadcastMessage 广播消息
	BroadcastMessage(ctx context.Context, content string, from string) (string, error)
	// GetMessages 获取指定成员的消息
	GetMessages(ctx context.Context, to string, from string, unreadOnly bool) ([]any, error)
	// GetBroadcastMessages 获取广播消息
	GetBroadcastMessages(ctx context.Context, memberName string, unreadOnly bool) ([]any, error)
	// GetTeamMessages 获取团队所有消息
	GetTeamMessages(ctx context.Context, teamName string) ([]any, error)
	// HasUnreadMessages 是否有未读消息
	HasUnreadMessages(ctx context.Context, includeBroadcast bool) bool
}
```

- [ ] **Step 7: 创建 memory_database.go — 薄接口**

```go
package tools

// ──────────────────────────── 接口 ────────────────────────────

// InMemoryTeamDatabase 内存数据库替代实现接口。⤵️ 回填: 9.65a
type InMemoryTeamDatabase interface {
	database.TeamDatabase
}
```

导入需要 `tools/database` 包。

- [ ] **Step 8: 创建 models.go — 数据模型薄接口**

```go
package tools

import "time"

// ──────────────────────────── 结构体 ────────────────────────────

// TeamInfo 团队信息模型。⤵️ 回填: 9.65a
type TeamInfo struct {
	// TeamName 团队名称（主键）
	TeamName string
	// DisplayName 显示名称
	DisplayName string
	// LeaderMemberName Leader 成员名
	LeaderMemberName string
	// Desc 团队描述
	Desc string
	// Created 创建时间
	Created time.Time
	// UpdatedAt 更新时间
	UpdatedAt time.Time
}

// TeamMemberInfo 团队成员模型。⤵️ 回填: 9.65a
type TeamMemberInfo struct {
	// MemberName 成员名称（主键）
	MemberName string
	// TeamName 团队名称（主键）
	TeamName string
	// DisplayName 显示名称
	DisplayName string
	// Desc 成员描述
	Desc string
	// AgentCard Agent 卡片 JSON
	AgentCard string
	// Status 成员状态
	Status string
	// Role 角色
	Role string
	// ModelRefJSON 模型引用 JSON
	ModelRefJSON string
	// UpdatedAt 更新时间
	UpdatedAt time.Time
}
```

- [ ] **Step 9: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/tools/... && go build ./internal/agent_teams/tools/database/...`
Expected: PASS

- [ ] **Step 10: Commit**

```
git add internal/agent_teams/tools/doc.go internal/agent_teams/tools/database/doc.go internal/agent_teams/tools/database/database.go internal/agent_teams/tools/database/engine.go internal/agent_teams/tools/database/team_dao.go internal/agent_teams/tools/database/member_dao.go internal/agent_teams/tools/database/task_dao.go internal/agent_teams/tools/database/message_dao.go internal/agent_teams/tools/task_manager.go internal/agent_teams/tools/message_manager.go internal/agent_teams/tools/memory_database.go internal/agent_teams/tools/models.go
git commit -m "feat(9.64): tools 薄接口 — TeamDatabase + DAO + TaskManager + MessageManager"
```

---

## Task 7: agent_teams/memory/ — config 回填 + manager_params + SharedMemoryManager (TDD)

**Files:**
- Modify: `internal/agent_teams/memory/config.go` (EmbeddingConfig 类型回填 + ResolveEmbeddingConfig 实现)
- Modify: `internal/agent_teams/memory/config_test.go` (更新测试)
- Modify: `internal/agent_teams/memory/doc.go` (更新文件目录)
- Create: `internal/agent_teams/memory/manager_params.go`
- Create: `internal/agent_teams/memory/shared_memory.go`
- Create: `internal/agent_teams/memory/shared_memory_test.go`

- [ ] **Step 1: 回填 config.go**

修改 `internal/agent_teams/memory/config.go`：

1. 添加导入 `lite` 包和 `embedding` 包
2. `EmbeddingConfig` 字段从 `any` → `*embedding.EmbeddingConfig`
3. `ResolveEmbeddingConfig` 函数实现（调 lite.ResolveEmbeddingConfigFromEnv）

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

- [ ] **Step 2: 更新 config_test.go**

添加 ResolveEmbeddingConfig 测试：

```go
func TestResolveEmbeddingConfig(t *testing.T) {
	// 无内嵌配置 → 调 env（当前返回 nil）
	cfg := NewTeamMemoryConfig()
	result := ResolveEmbeddingConfig(&cfg)
	if result != nil {
		t.Errorf("Expected nil when no env vars, got %v", result)
	}
	// 有内嵌配置 → 直接返回
	embCfg := &embedding.EmbeddingConfig{ModelName: "test"}
	cfg.EmbeddingConfig = embCfg
	result = ResolveEmbeddingConfig(&cfg)
	if result != embCfg {
		t.Errorf("Expected embCfg, got %v", result)
	}
	// nil config → 返回 nil
	result = ResolveEmbeddingConfig(nil)
	if result != nil {
		t.Errorf("Expected nil for nil config, got %v", result)
	}
}
```

- [ ] **Step 3: 创建 manager_params.go**

```go
package memory

import (
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 枚举 ────────────────────────────

// TeamRole 团队角色类型别名
type TeamRole string

// TeamLifecycle 团队生命周期类型别名
type TeamLifecycle string

// TeamScenario 团队场景类型别名
type TeamScenario string

// TeamLanguage 团队语言类型别名
type TeamLanguage string

// PromptMode 提示模式类型别名
type PromptMode string

// ──────────────────────────── 常量 ────────────────────────────

const (
	// TeamRoleLeader Leader 角色
	TeamRoleLeader TeamRole = "leader"
	// TeamRoleTeammate Teammate 角色
	TeamRoleTeammate TeamRole = "teammate"
	// TeamLifecycleTemporary 临时生命周期
	TeamLifecycleTemporary TeamLifecycle = "temporary"
	// TeamLifecyclePersistent 持久生命周期
	TeamLifecyclePersistent TeamLifecycle = "persistent"
	// TeamScenarioGeneral 通用场景
	TeamScenarioGeneral TeamScenario = "general"
	// TeamScenarioCoding 编程场景
	TeamScenarioCoding TeamScenario = "coding"
	// TeamLanguageCN 中文
	TeamLanguageCN TeamLanguage = "cn"
	// TeamLanguageEN 英文
	TeamLanguageEN TeamLanguage = "en"
	// PromptModeProactive 主动模式
	PromptModeProactive PromptMode = "proactive"
	// PromptModePassive 被动模式
	PromptModePassive PromptMode = "passive"
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryManagerParams 记忆管理器构造参数。
// 对齐 Python TeamMemoryManagerParams (manager_params.py)
type TeamMemoryManagerParams struct {
	// MemberName 成员名称
	MemberName string
	// TeamName 团队名称
	TeamName string
	// Role 角色
	Role TeamRole
	// Lifecycle 生命周期
	Lifecycle TeamLifecycle
	// Scenario 场景
	Scenario TeamScenario
	// EmbeddingConfig 嵌入配置
	EmbeddingConfig *embedding.EmbeddingConfig
	// Workspace 工作空间
	Workspace *workspace.Workspace
	// SysOperation 系统操作接口
	SysOperation sysop.SysOperation
	// TeamMemoryDir 团队记忆目录路径
	TeamMemoryDir *string
	// Language 语言
	Language TeamLanguage
	// PromptMode 提示模式
	PromptMode PromptMode
	// EnableAutoExtract 是否自动提取记忆
	EnableAutoExtract bool
	// ReadOnlySourceWorkspace 只读来源工作空间路径
	ReadOnlySourceWorkspace *string
	// DB 团队数据库。⤵️ 回填: 9.65a
	DB tools.TeamDatabase
	// TaskManager 任务管理器。⤵️ 回填: 9.65a
	TaskManager tools.TeamTaskManager
	// ExtractionModel 提取模型。⤵️ 回填: 9.65a
	ExtractionModel any
	// TimezoneOffsetHours 时区偏移小时数
	TimezoneOffsetHours float64
}
```

- [ ] **Step 4: 创建 shared_memory.go — 真实实现**

对齐 Python `agent_teams/memory/shared_memory.py`。纯文件读写，无外部依赖。

```go
package memory

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// teamMemoryFilename 团队记忆文件名
	teamMemoryFilename = "TEAM_MEMORY.md"
	// teamMemoryMaxReadLines 读取最大行数
	teamMemoryMaxReadLines = 200
)

const logComponent = logger.ComponentCommon

// ──────────────────────────── 结构体 ────────────────────────────

// SharedMemoryManager 管理 {team_home}/team-memory/ 目录下的团队摘要文件。
// 对齐 Python SharedMemoryManager (shared_memory.py)
//
// 所有成员只读访问 ReadTeamSummary；
// 提取 agent（leader extract_after_round）通过工具或 WriteTeamSummary 写入。
type SharedMemoryManager struct {
	// dir 团队记忆目录路径
	dir string
	// sysOperation 系统操作接口（可选，nil 时用本地文件系统）
	sysOperation any // ⤵️ 回填: 7.2 — SysOperation 类型，当前用 any 避免循环
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSharedMemoryManager 创建共享记忆管理器
func NewSharedMemoryManager(teamMemoryDir string, sysOperation any) *SharedMemoryManager {
	return &SharedMemoryManager{dir: teamMemoryDir, sysOperation: sysOperation}
}

// EnsureDir 确保团队记忆目录存在。对齐 Python ensure_dir
func (m *SharedMemoryManager) EnsureDir() error {
	return os.MkdirAll(m.dir, 0o755)
}

// ReadTeamSummary 读取团队记忆摘要文件。
// 对齐 Python read_team_summary — 真实实现
// 最多前 teamMemoryMaxReadLines 行，不存在或错误时返回空字符串
func (m *SharedMemoryManager) ReadTeamSummary(_ context.Context) string {
	filePath := filepath.Join(m.dir, teamMemoryFilename)

	// ⤵️ 回填: 7.2 — sysOperation 分支，当前仅本地文件系统
	data, err := os.ReadFile(filePath)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	if len(lines) > teamMemoryMaxReadLines {
		lines = lines[:teamMemoryMaxReadLines]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

// WriteTeamSummary 写入团队记忆摘要（覆盖）。
// 对齐 Python write_team_summary — 真实实现（本地原子写入）
// ⤵️ 回填: 7.2 — sysOperation 分支优先，当前仅本地原子写入
func (m *SharedMemoryManager) WriteTeamSummary(_ context.Context, content string) error {
	if err := m.EnsureDir(); err != nil {
		return err
	}
	target := filepath.Join(m.dir, teamMemoryFilename)

	// 原子写入：先写临时文件，再 os.Rename 替换
	tmpFile, err := os.CreateTemp(m.dir, "team_memory_*.tmp")
	if err != nil {
		logger.Error(logComponent).Err(err).Str("path", target).
			Msg("WriteTeamSummary 创建临时文件失败")
		return err
	}
	tmpPath := tmpFile.Name()
	if _, err := tmpFile.WriteString(content); err != nil {
		tmpFile.Close()
		os.Remove(tmpPath)
		return err
	}
	tmpFile.Close()
	if err := os.Rename(tmpPath, target); err != nil {
		logger.Error(logComponent).Err(err).Str("tmp", tmpPath).Str("target", target).
			Msg("WriteTeamSummary 原子替换失败")
		os.Remove(tmpPath)
		return err
	}
	return nil
}

// AppendEntry 追加一条团队记忆。
// 对齐 Python append_entry — 真实实现
// 读取现有内容 + 分隔线 + 新条目 → 覆盖写（非原子，适合低频/单 writer）
func (m *SharedMemoryManager) AppendEntry(ctx context.Context, entry string) error {
	existing := m.ReadTeamSummary(ctx)
	var newContent string
	if existing != "" {
		newContent = existing + "\n\n---\n\n" + entry
	} else {
		newContent = entry
	}
	return m.WriteTeamSummary(ctx, newContent)
}
```

- [ ] **Step 5: 创建 shared_memory_test.go**

```go
package memory

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedMemoryManager_EnsureDir(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	if err := mgr.EnsureDir(); err != nil {
		t.Fatalf("EnsureDir failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "team-memory")); os.IsNotExist(err) {
		t.Errorf("Directory not created")
	}
}

func TestSharedMemoryManager_ReadTeamSummary_空文件(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()
	result := mgr.ReadTeamSummary(context.Background())
	if result != "" {
		t.Errorf("Expected empty string for nonexistent file, got %q", result)
	}
}

func TestSharedMemoryManager_WriteAndRead(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	content := "# 团队共享记忆\n\n### [decision] 选择了方案 A"
	if err := mgr.WriteTeamSummary(context.Background(), content); err != nil {
		t.Fatalf("WriteTeamSummary failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if result != content {
		t.Errorf("Expected %q, got %q", content, result)
	}
}

func TestSharedMemoryManager_AppendEntry(t *testing.T) {
	dir := t.TempDir()
	mgr := NewSharedMemoryManager(filepath.Join(dir, "team-memory"), nil)
	_ = mgr.EnsureDir()

	if err := mgr.WriteTeamSummary(context.Background(), "初始内容"); err != nil {
		t.Fatalf("WriteTeamSummary failed: %v", err)
	}
	if err := mgr.AppendEntry(context.Background(), "追加条目"); err != nil {
		t.Fatalf("AppendEntry failed: %v", err)
	}
	result := mgr.ReadTeamSummary(context.Background())
	if !contains(result, "初始内容") || !contains(result, "追加条目") {
		t.Errorf("Expected both contents, got %q", result)
	}
}

func contains(s, sub string) bool {
	return strings.Contains(s, sub)
}
```

注意：shared_memory_test.go 需要导入 `strings` 包。

- [ ] **Step 6: 更新 doc.go 文件目录**

添加 manager_params.go、shared_memory.go、manager.go、member_memory_toolkit.go、extractor.go 条目。

- [ ] **Step 7: 编译+测试验证**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/memory/... -v`
Expected: PASS

- [ ] **Step 8: Commit**

```
git add internal/agent_teams/memory/config.go internal/agent_teams/memory/config_test.go internal/agent_teams/memory/doc.go internal/agent_teams/memory/manager_params.go internal/agent_teams/memory/shared_memory.go internal/agent_teams/memory/shared_memory_test.go
git commit -m "feat(9.64): memory — config回填 + manager_params + SharedMemoryManager 真实实现"
```

---

## Task 8: agent_teams/memory/ — manager + member_memory_toolkit + extractor (薄接口)

**Files:**
- Create: `internal/agent_teams/memory/manager.go`
- Create: `internal/agent_teams/memory/member_memory_toolkit.go`
- Create: `internal/agent_teams/memory/extractor.go`

- [ ] **Step 1: 创建 manager.go — TeamMemoryManager 薄接口**

对齐 Python `agent_teams/memory/manager.py`。构造函数+5个生命周期方法。

```go
package memory

import (
	"context"

	saprompt "github.com/uapclaw/uapclaw-go/internal/agentcore/single_agent/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

const logComponentMgr = logger.ComponentCommon

// ──────────────────────────── 常量 ────────────────────────────

const (
	// SectionName 系统提示词段名称
	SectionName = "team_memory"
	// maxPersonalMemoryBytes 个人记忆注入最大字节数
	maxPersonalMemoryBytes = 10 * 1024
)

// ──────────────────────────── 结构体 ────────────────────────────

// TeamMemoryManager 团队记忆管理器。
// 对齐 Python TeamMemoryManager (manager.py)
//
// 生命周期：InitToolkit → RegisterTools → LoadAndInject → ExtractAfterRound → Close
type TeamMemoryManager struct {
	// memberName 成员名称
	memberName string
	// teamName 团队名称
	teamName string
	// role 角色
	role TeamRole
	// lifecycle 生命周期
	lifecycle TeamLifecycle
	// scenario 场景
	scenario TeamScenario
	// embeddingConfig 嵌入配置
	embeddingConfig *embedding.EmbeddingConfig
	// language 语言
	language TeamLanguage
	// promptMode 提示模式
	promptMode PromptMode
	// enableAutoExtract 是否自动提取
	enableAutoExtract bool
	// readOnlySource 只读来源工作空间路径
	readOnlySource *string
	// db 团队数据库。⤵️ 回填: 9.65a
	db tools.TeamDatabase
	// taskManager 任务管理器。⤵️ 回填: 9.65a
	taskManager tools.TeamTaskManager
	// extractionModel 提取模型。⤵️ 回填: 9.65a
	extractionModel any
	// tzOffset 时区偏移
	tzOffset float64
	// sysOperation 系统操作接口
	sysOperation any
	// workspace 工作空间
	workspace *workspace.Workspace
	// teamMemoryDir 团队记忆目录路径
	teamMemoryDir *string
	// toolkit 成员记忆工具集。⤵️ 回填: 7.2+7.3
	toolkit *MemberMemoryToolkit
	// ownedToolNames 已注册工具名集合。⤵️ 回填: 7.2
	ownedToolNames map[string]struct{}
	// ownedToolIDs 已注册工具ID集合。⤵️ 回填: 7.2
	ownedToolIDs map[string]struct{}
	// deepAgentForCleanup 用于清理的 DeepAgent。⤵️ 回填: 7.2
	deepAgentForCleanup any
	// sharedManager 共享记忆管理器
	sharedManager *SharedMemoryManager
	// cachedBaseSection 缓存的基础提示词段。⤵️ 回填: 7.2
	cachedBaseSection *saprompt.PromptSection
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewTeamMemoryManager 创建团队记忆管理器
func NewTeamMemoryManager(params TeamMemoryManagerParams) *TeamMemoryManager {
	mgr := &TeamMemoryManager{
		memberName:       params.MemberName,
		teamName:         params.TeamName,
		role:             params.Role,
		lifecycle:        params.Lifecycle,
		scenario:         params.Scenario,
		embeddingConfig:  params.EmbeddingConfig,
		language:         params.Language,
		promptMode:       params.PromptMode,
		enableAutoExtract: params.EnableAutoExtract,
		readOnlySource:   params.ReadOnlySourceWorkspace,
		db:               params.DB,
		taskManager:      params.TaskManager,
		extractionModel:  params.ExtractionModel,
		tzOffset:         params.TimezoneOffsetHours,
		sysOperation:     params.SysOperation,
		workspace:         params.Workspace,
		teamMemoryDir:    params.TeamMemoryDir,
		ownedToolNames:   make(map[string]struct{}),
		ownedToolIDs:     make(map[string]struct{}),
	}
	if params.TeamMemoryDir != nil && params.SharedMemory {
		mgr.sharedManager = NewSharedMemoryManager(*params.TeamMemoryDir, params.SysOperation)
	}
	return mgr
}

// InitToolkit 初始化成员记忆工具集。⤵️ 回填: 7.1+7.2+7.3 — 当前返回 false
func (m *TeamMemoryManager) InitToolkit(_ context.Context) (bool, error) {
	return false, nil
}

// RegisterTools 将记忆工具注册到 DeepAgent。⤵️ 回填: 7.2+9.65a — 当前空实现
func (m *TeamMemoryManager) RegisterTools(_ any) {}

// LoadAndInject 加载个人记忆+共享记忆→注入系统提示词。⤵️ 回填: 7.2 — 当前空实现
func (m *TeamMemoryManager) LoadAndInject(_ context.Context, _ any, _ string) error { return nil }

// ExtractAfterRound Leader 专属：提取 agent 蒸馏团队记忆。⤵️ 回填: 7.2+9.65a — 当前空实现
func (m *TeamMemoryManager) ExtractAfterRound(_ context.Context) error { return nil }

// Close 反注册工具+移除提示词段+关闭 toolkit。⤵️ 回填: 7.1+7.2 — 当前空实现
func (m *TeamMemoryManager) Close(_ context.Context) error { return nil }

// ExtractionModel 返回提取模型
func (m *TeamMemoryManager) ExtractionModel() any { return m.extractionModel }

// SetExtractionModel 设置提取模型
func (m *TeamMemoryManager) SetExtractionModel(model any) { m.extractionModel = model }
```

注意导入：`embedding`, `workspace`, `tools`, `sysop`, `saprompt`, `logger`

- [ ] **Step 2: 创建 member_memory_toolkit.go — 薄接口**

对齐 Python `agent_teams/memory/member_memory_toolkit.py`。

```go
package memory

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/tool/base"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemberMemoryToolkit 成员记忆工具集。
// 对齐 Python MemberMemoryToolkit (member_memory_toolkit.py)
type MemberMemoryToolkit struct {
	// memberName 成员名称
	memberName string
	// teamName 团队名称
	teamName string
	// workspace 工作空间
	workspace *workspace.Workspace
	// scenario 场景
	scenario TeamScenario
	// embeddingConfig 嵌入配置
	embeddingConfig *embedding.EmbeddingConfig
	// sysOperation 系统操作接口
	sysOperation sysop.SysOperation
	// readOnly 是否只读
	readOnly bool
	// manager 记忆索引管理器。⤵️ 回填: 7.1
	manager lite.MemoryIndexManager
	// ctx 工具上下文（MemoryToolContext 或 CodingMemoryToolContext）。⤵️ 回填: 7.3
	ctx any
	// tools 工具列表。⤵️ 回填: 7.2
	tools []base.Tool
	// initialized 是否已初始化
	initialized bool
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMemberMemoryToolkit 创建成员记忆工具集
func NewMemberMemoryToolkit(memberName string, teamName string, ws *workspace.Workspace, scenario TeamScenario, embCfg *embedding.EmbeddingConfig, sysOp sysop.SysOperation, readOnly bool) *MemberMemoryToolkit {
	return &MemberMemoryToolkit{
		memberName:      memberName,
		teamName:        teamName,
		workspace:       ws,
		scenario:        scenario,
		embeddingConfig: embCfg,
		sysOperation:    sysOp,
		readOnly:        readOnly,
	}
}

// Initialize 初始化工具集。⤵️ 回填: 7.1+7.2+7.3 — 当前返回 false
func (t *MemberMemoryToolkit) Initialize(_ context.Context) (bool, error) { return false, nil }

// GetTools 返回工具列表。⤵️ 回填: 7.2 — 当前返回 nil
func (t *MemberMemoryToolkit) GetTools() []base.Tool { return nil }

// GetToolCards 返回工具卡片列表。⤵️ 回填: 7.2 — 当前返回 nil
func (t *MemberMemoryToolkit) GetToolCards() []base.ToolCard { return nil }

// Close 关闭工具集。⤵️ 回填: 7.1 — 当前空实现
func (t *MemberMemoryToolkit) Close(_ context.Context) error { return nil }

// Manager 返回记忆索引管理器
func (t *MemberMemoryToolkit) Manager() lite.MemoryIndexManager { return t.manager }

// Ctx 返回工具上下文
func (t *MemberMemoryToolkit) Ctx() any { return t.ctx }

// TeamName 返回团队名称
func (t *MemberMemoryToolkit) TeamName() string { return t.teamName }

// MemberName 返回成员名称
func (t *MemberMemoryToolkit) MemberName() string { return t.memberName }
```

- [ ] **Step 3: 创建 extractor.go — 薄接口**

对齐 Python `agent_teams/memory/extractor.py`。提示词常量真实实现，函数薄接口。

```go
package memory

import (
	"context"

	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/agent_teams/tools"
	lite "github.com/uapclaw/uapclaw-go/internal/agentcore/memory/lite"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	// taskContentPreviewMax 任务内容预览最大字符数
	taskContentPreviewMax = 2000
	// messageContentPreviewMax 消息内容预览最大字符数
	messageContentPreviewMax = 1000
	// extractionAgentMaxIterations 提取 agent 最大迭代次数
	extractionAgentMaxIterations = 5
)

// ExtractionAgentPrompt 提取 agent 提示词。对齐 Python EXTRACTION_AGENT_PROMPT — 真实实现
const ExtractionAgentPrompt = `你是团队记忆提取 agent。你的工作目录是团队记忆目录，里面可能已有之前提取的记忆文件。

## 你的任务

分析提供的团队协作记录（任务和消息），从中提炼出对未来团队协作有价值的持久记忆，写入记忆文件。

## 工作流程

1. 先用 Read 读取已有的记忆文件（如 TEAM_MEMORY.md），了解已记录的内容
2. 分析新的协作记录，判断哪些信息值得记忆
3. 用 Write/Edit 更新记忆文件：
   - 更新已有记忆条目（如果新信息补充或修正了旧内容）
   - 添加新的记忆条目
   - 删除已过时的条目
   - 合并重复内容

## 提取什么

1. **[decision] 团队决策**: 为什么选择了某个方案、拒绝了哪些替代方案、关键权衡
2. **[lesson] 经验教训**: 什么做法有效、什么导致了返工或问题、值得复用的模式
3. **[member] 成员特长**: 谁擅长什么、谁负责哪个领域、协作模式
4. **[context] 项目背景**: 非代码可推导的业务约束、截止日期、利益相关方要求

## 不要提取什么

- 代码细节、具体文件路径、函数名（可从代码库获取）
- 临时状态、进行中的调试过程
- 原始对话内容的复述（提取的是洞察，不是摘要）
- 任何敏感信息（密钥、凭证、个人隐私）

## 记忆文件格式

TEAM_MEMORY.md 中每条记忆用三级标题 + 类型标签，示例：

    ### [decision] 选择了方案 A 而非 B
    原因是... 权衡是...

    ### [lesson] 并行任务需要先对齐接口
    上次因为没对齐导致返工 2 天...

保持 TEAM_MEMORY.md 在 200 行以内。超出时合并或删除最旧的条目。
如果没有值得提取的新信息，不要修改文件。`

// ──────────────────────────── 导出函数 ────────────────────────────

// BuildExtractionContext 构建提取上下文。⤵️ 回填: 7.2+9.65a
func BuildExtractionContext(tasks []any, messages []any, tzOffsetHours float64) string { return "" }

// CreateExtractionTools 创建提取 agent 限定工具。⤵️ 回填: 7.2
func CreateExtractionTools(teamMemoryDir string, sysOp sysop.SysOperation, teamName string) []any { return nil }

// ExtractTeamMemories 提取团队记忆。⤵️ 回填: 7.2+9.65a
func ExtractTeamMemories(ctx context.Context, teamName string, db tools.TeamDatabase, taskMgr tools.TeamTaskManager, teamMemoryDir string, sysOp sysop.SysOperation, model any, tzOffsetHours float64) error { return nil }
```

- [ ] **Step 4: 更新 doc.go 文件目录**

确认 manager.go、member_memory_toolkit.go、extractor.go 已在目录中。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/memory/... && go test ./internal/agent_teams/memory/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add internal/agent_teams/memory/manager.go internal/agent_teams/memory/member_memory_toolkit.go internal/agent_teams/memory/extractor.go internal/agent_teams/memory/doc.go
git commit -m "feat(9.64): memory 薄接口 — manager + member_memory_toolkit + extractor"
```

---

## Task 9: models/allocator 回填 — 3种 Allocator 真实实现 (TDD)

**Files:**
- Modify: `internal/agent_teams/models/allocator.go`
- Modify: `internal/agent_teams/models/allocator_test.go`
- Modify: `internal/agent_teams/models/doc.go`

- [ ] **Step 1: 编写 RoundRobinModelAllocator 测试**

在 `allocator_test.go` 中添加：

```go
func TestRoundRobinModelAllocator_Allocate(t *testing.T) {
	pool := []ModelPoolEntry{
		{ModelName: "model-a", APIBaseURL: "http://a", ModelID: "1"},
		{ModelName: "model-a", APIBaseURL: "http://a2", ModelID: "2"},
		{ModelName: "model-b", APIBaseURL: "http://b", ModelID: "3"},
	}
	alloc := &RoundRobinModelAllocator{pool: pool, poolDigest: poolDigest(pool), index: 0, groups: buildGroups(pool)}

	a1 := alloc.Allocate("")
	if a1 == nil { t.Fatalf("Expected allocation") }
	if a1.Entry.ModelName != "model-a" { t.Errorf("Expected model-a, got %s", a1.Entry.ModelName) }

	a2 := alloc.Allocate("")
	if a2.Entry.ModelName != "model-a" { t.Errorf("Expected model-a on second rotation") }

	a3 := alloc.Allocate("")
	if a3.Entry.ModelName != "model-b" { t.Errorf("Expected model-b") }
}

func TestRoundRobinModelAllocator_StateDict(t *testing.T) {
	pool := []ModelPoolEntry{{ModelName: "m", APIBaseURL: "http://x", ModelID: "1"}}
	alloc := &RoundRobinModelAllocator{pool: pool, poolDigest: poolDigest(pool), index: 5, groups: buildGroups(pool)}
	state := alloc.StateDict()
	if state["index"] != 5 { t.Errorf("Expected index=5") }
	if state["pool_digest"] != poolDigest(pool) { t.Errorf("Digest mismatch") }
}

func TestRoundRobinModelAllocator_LoadStateDict(t *testing.T) {
	pool := []ModelPoolEntry{{ModelName: "m", APIBaseURL: "http://x", ModelID: "1"}}
	alloc := &RoundRobinModelAllocator{pool: pool, poolDigest: poolDigest(pool), index: 0, groups: buildGroups(pool)}
	alloc.LoadStateDict(map[string]any{"index": 10, "pool_digest": poolDigest(pool)})
	if alloc.index != 10 { t.Errorf("Expected index=10, got %d", alloc.index) }
	// digest mismatch → reset
	alloc.LoadStateDict(map[string]any{"index": 20, "pool_digest": "wrong"})
	if alloc.index != 0 { t.Errorf("Expected reset to 0") }
}
```

- [ ] **Step 2: 编写 ByModelNameAllocator 测试**

```go
func TestByModelNameAllocator_Allocate(t *testing.T) {
	pool := []ModelPoolEntry{
		{ModelName: "model-a", APIBaseURL: "http://a", ModelID: "1"},
		{ModelName: "model-a", APIBaseURL: "http://a2", ModelID: "2"},
		{ModelName: "model-b", APIBaseURL: "http://b", ModelID: "3"},
	}
	alloc := &ByModelNameAllocator{groups: buildGroups(pool), poolDigest: poolDigest(pool), innerIndexes: map[string]int{"model-a": 0, "model-b": 0}}

	a1 := alloc.Allocate("model-a")
	if a1 == nil { t.Fatalf("Expected allocation") }
	if a1.Entry.APIBaseURL != "http://a" { t.Errorf("Expected first model-a endpoint") }

	a2 := alloc.Allocate("model-a")
	if a2.Entry.APIBaseURL != "http://a2" { t.Errorf("Expected second model-a endpoint") }

	// unknown name → nil
	a3 := alloc.Allocate("model-c")
	if a3 != nil { t.Errorf("Expected nil for unknown name") }
}
```

- [ ] **Step 3: 编写 RouterAllocator 测试**

```go
func TestRouterAllocator_Allocate(t *testing.T) {
	pool := []ModelPoolEntry{
		{ModelName: "model-a", APIBaseURL: "http://router", ModelID: "1"},
		{ModelName: "model-b", APIBaseURL: "http://router", ModelID: "2"},
	}
	alloc := NewRouterAllocator(pool)

	// nil name → first entry
	a1 := alloc.Allocate("")
	if a1 == nil { t.Fatalf("Expected allocation") }
	if a1.Entry.ModelName != "model-a" { t.Errorf("Expected model-a as default") }

	// known name → exact entry
	a2 := alloc.Allocate("model-b")
	if a2.Entry.ModelName != "model-b" { t.Errorf("Expected model-b") }

	// unknown name → nil
	a3 := alloc.Allocate("model-z")
	if a3 != nil { t.Errorf("Expected nil for unknown") }
}

func TestRouterAllocator_EmptyPool(t *testing.T) {
	_, err := NewRouterAllocator([]ModelPoolEntry{})
	if err == nil { t.Errorf("Expected error for empty pool") }
}
```

- [ ] **Step 4: 运行测试验证失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/models/... -v -run "TestRoundRobin|TestByModelName|TestRouter"`
Expected: FAIL (types not defined yet)

- [ ] **Step 5: 实现 RoundRobinModelAllocator**

在 allocator.go 中添加完整的 RoundRobinModelAllocator 结构体和方法实现。对齐 Python 1:1。

- [ ] **Step 6: 实现 ByModelNameAllocator**

在 allocator.go 中添加完整的 ByModelNameAllocator 结构体和方法实现。StateDict 用列表格式（对齐 Python 的 counters 为 list of {model_name, index}）。LoadStateDict 兼容旧 dict 格式。

- [ ] **Step 7: 实现 RouterAllocator**

在 allocator.go 中添加完整的 RouterAllocator 结构体和方法实现。Allocate(nil) → 首条目；Allocate(name) → 精确查找；空池 → 构造时 panic/error。

- [ ] **Step 8: 回填 BuildModelAllocatorForPool**

替换现有留桩实现为根据 strategy 分发到具体 allocator 的真实实现。

- [ ] **Step 9: 回填 ResolveMemberModelFromPool**

替换现有留桩实现为从 pool 按 modelName+modelIndex 解析的真实实现。

- [ ] **Step 10: 运行测试验证通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agent_teams/models/... -v`
Expected: PASS

- [ ] **Step 11: Commit**

```
git add internal/agent_teams/models/allocator.go internal/agent_teams/models/allocator_test.go internal/agent_teams/models/doc.go
git commit -m "feat(9.64): models allocator 回填 — RoundRobin/ByModelName/Router 真实实现"
```

---

## Task 10: 现有留桩回填 — resources + configurator + harness + shared_resources

**Files:**
- Modify: `internal/agent_teams/agent/resources.go`
- Modify: `internal/agent_teams/agent/agent_configurator.go`
- Modify: `internal/agent_teams/harness.go`
- Modify: `internal/agent_teams/spawn/shared_resources.go`

- [ ] **Step 1: 回填 resources.go**

将 `MemoryManager any` → `MemoryManager *memory.TeamMemoryManager`
将 `ModelAllocator any` → `ModelAllocator models.ModelAllocator`

添加导入：`memory` 包和 `models` 包。

- [ ] **Step 2: 回填 agent_configurator.go**

1. `BuildMemoryManager` — 构造 TeamMemoryManagerParams + new TeamMemoryManager
2. `UpdateModelPool` — 继承池 ID + 构建模型分配器
3. `RestoreAllocatorState` — 调 allocator.LoadStateDict
4. `leaderAllocation` 字段类型从 `any` → `*models.Allocation`

添加导入：`memory` 包和 `models` 包。

- [ ] **Step 3: 回填 harness.go**

1. `RegisterMemberTools` 参数从 `any` → `*memory.TeamMemoryManager`，调 manager.RegisterTools
2. `InjectMemberMemory` 参数从 `any` → `*memory.TeamMemoryManager`，调 manager.LoadAndInject

添加导入：`memory` 包。

- [ ] **Step 4: 回填 spawn/shared_resources.go**

`GetSharedDB` 方法实现（如果 resources 有 DB 则返回，否则返回 nil）。

- [ ] **Step 5: 编译验证**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/agent_teams/...`
Expected: PASS

- [ ] **Step 6: Commit**

```
git add internal/agent_teams/agent/resources.go internal/agent_teams/agent/agent_configurator.go internal/agent_teams/harness.go internal/agent_teams/spawn/shared_resources.go
git commit -m "feat(9.64): 现有留桩回填 — resources/configurator/harness 类型升级"
```

---

## Task 11: 全量编译 + 测试 + IMPLEMENTATION_PLAN.md 最终更新

- [ ] **Step 1: 全量编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./...`
Expected: PASS (注意编译前检查残留 go 进程：`pgrep -f 'go build' || true`)

- [ ] **Step 2: 全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/memory/lite/... ./internal/agent_teams/memory/... ./internal/agent_teams/models/... -v`
Expected: PASS

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 9.64 行状态从 `🔄` 改为 `✅`

- [ ] **Step 4: Final commit**

```
git add IMPLEMENTATION_PLAN.md
git commit -m "feat(9.64): IMPLEMENTATION_PLAN 9.64 标记完成"
```
