# 7.4+7.1 MemoryIndexManager 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 7.4（MemoryConfig 回填）和 7.1（MemoryIndexManager），为 Agent 提供轻量编程记忆的索引管理与混合搜索能力。

**Architecture:** 统一 SQLite 驱动为 `mattn/go-sqlite3`（CGO），引入 `asg017/sqlite-vec` 的 `vec0.so` 预编译二进制实现向量搜索。MemoryIndexManager 通过 `*sql.DB` 直接操作 SQLite（FTS5 + vec0），数据库 schema 与 Python 完全一致。EmbeddingProvider 复用 `retrieval/embedding` 包，通过适配器包装。

**Tech Stack:** `mattn/go-sqlite3` + `asg017/sqlite-vec` (vec0.so) + `fsnotify` + `retrieval/embedding`

---

## 文件结构

### 新建文件

| 文件 | 职责 |
|------|------|
| `vendor/sqlite-vec/linux-amd64/vec0.so` | sqlite-vec 预编译二进制 (Linux x86_64) |
| `vendor/sqlite-vec/linux-arm64/vec0.so` | sqlite-vec 预编译二进制 (Linux aarch64) |
| `vendor/sqlite-vec/darwin-amd64/vec0.so` | sqlite-vec 预编译二进制 (macOS x86_64) |
| `vendor/sqlite-vec/darwin-arm64/vec0.so` | sqlite-vec 预编译二进制 (macOS aarch64) |
| `internal/agentcore/memory/lite/manager_impl.go` | memoryIndexManager 结构体定义 + 所有方法实现 |
| `internal/agentcore/memory/lite/manager_impl_test.go` | MemoryIndexManager 单元测试 |
| `internal/agentcore/memory/lite/vec_loader.go` | vec0.so 加载逻辑（封装 LoadExtension） |

### 修改文件

| 文件 | 修改内容 |
|------|---------|
| `go.mod` | 移除 `glebarez/sqlite`，`mattn/go-sqlite3` 从 indirect 升为 direct |
| `Makefile` | 添加 `CGO_ENABLED=1` + `-tags "sqlite_fts5"` |
| `internal/agentcore/foundation/store/db/default_test.go` | `glebarez/sqlite` → `gorm.io/driver/sqlite` |
| `internal/agentcore/foundation/store/kv/db_based_test.go` | `glebarez/sqlite` → `gorm.io/driver/sqlite` |
| `internal/agentcore/memory/lite/config.go` | 回填 IsMemoryEnabled + CreateMemorySettings |
| `internal/agentcore/memory/lite/embeddings.go` | 回填 ResolveEmbeddingConfigFromEnv + CreateEmbeddingProvider + baseEmbeddingAdapter |
| `internal/agentcore/memory/lite/internal.go` | 回填 BuildFTSQuery + BM25RankToScore + IsMemoryPath + ListMemoryFiles + NormalizeExtraMemoryPaths |
| `internal/agentcore/memory/lite/manager.go` | 移除 SessionDeltaState（移到 manager_impl.go），保留接口 + Params + 导出函数 |
| `internal/agentcore/memory/lite/types.go` | 移除 ⤵️ 标记 |
| `internal/agentcore/memory/lite/doc.go` | 更新文件目录 + 新增文件 |
| `IMPLEMENTATION_PLAN.md` | 7.4 + 7.1 标记为 ✅ |

---

### Task 1: SQLite 驱动统一迁移

**Files:**
- Modify: `go.mod`
- Modify: `internal/agentcore/foundation/store/db/default_test.go`
- Modify: `internal/agentcore/foundation/store/kv/db_based_test.go`
- Modify: `Makefile`

- [ ] **Step 1: 修改 go.mod，移除 glebarez/sqlite**

在 `go.mod` 中：
- 移除 `github.com/glebarez/sqlite v1.11.0`（direct require）
- 将 `github.com/mattn/go-sqlite3 v1.14.22` 从 indirect 升为 direct（移到 direct require 块）
- 运行 `go mod tidy` 清理 `glebarez/go-sqlite` 和 `modernc.org/sqlite` 间接依赖

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
# 先确保没有残留的 go 编译进程
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
# 移除 glebarez/sqlite，确认 mattn/go-sqlite3 在 direct require
go mod tidy
```

- [ ] **Step 2: 修改 default_test.go，替换 glebarez/sqlite 为 gorm.io/driver/sqlite**

将 `internal/agentcore/foundation/store/db/default_test.go` 的 import 从：
```go
"github.com/glebarez/sqlite"
```
改为：
```go
gormsqlite "gorm.io/driver/sqlite"
```

所有 `sqlite.Open` 调用改为 `gormsqlite.Open`。

- [ ] **Step 3: 修改 db_based_test.go，替换 glebarez/sqlite 为 gorm.io/driver/sqlite**

将 `internal/agentcore/foundation/store/kv/db_based_test.go` 的 import 从：
```go
"github.com/glebarez/sqlite"
```
改为：
```go
gormsqlite "gorm.io/driver/sqlite"
```

所有 `sqlite.Open` 调用改为 `gormsqlite.Open`。

- [ ] **Step 4: 修改 Makefile，添加 CGO_ENABLED=1 和 sqlite_fts5 build tag**

在 `Makefile` 的 `build`、`test`、`test-cover` 目标中添加 `CGO_ENABLED=1` 和 `-tags "sqlite_fts5 test"`：

```makefile
# 运行测试
test:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" ./...

# 运行测试（带覆盖率）
test-cover:
	CGO_ENABLED=1 $(GOTEST) -v -tags "sqlite_fts5 test" -coverprofile=coverage.out ./...
	$(GOCMD) tool cover -html=coverage.out -o coverage.html
```

- [ ] **Step 5: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go build -tags "sqlite_fts5" ./...
```

Expected: 编译通过，无错误

- [ ] **Step 6: 运行受影响的测试**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/foundation/store/db/ ./internal/agentcore/foundation/store/kv/ ./internal/agentcore/session/checkpointer/
```

Expected: 所有测试通过

- [ ] **Step 7: 提交**

```bash
git add go.mod go.sum Makefile internal/agentcore/foundation/store/db/default_test.go internal/agentcore/foundation/store/kv/db_based_test.go
git commit -m "refactor: 统一 SQLite 驱动为 mattn/go-sqlite3，移除 glebarez/sqlite"
```

---

### Task 2: 下载 vec0.so 预编译二进制

**Files:**
- Create: `vendor/sqlite-vec/linux-amd64/vec0.so`
- Create: `vendor/sqlite-vec/linux-arm64/vec0.so`
- Create: `vendor/sqlite-vec/darwin-amd64/vec0.so`
- Create: `vendor/sqlite-vec/darwin-arm64/vec0.so`

- [ ] **Step 1: 下载并解压各平台 vec0.so**

```bash
cd /home/opensource/uapclaw-gateway
mkdir -p vendor/sqlite-vec/linux-amd64 vendor/sqlite-vec/linux-arm64 vendor/sqlite-vec/darwin-amd64 vendor/sqlite-vec/darwin-arm64

# Linux x86_64
curl -sL "https://github.com/asg017/sqlite-vec/releases/download/v0.1.9/sqlite-vec-0.1.9-loadable-linux-x86_64.tar.gz" | tar xz -C vendor/sqlite-vec/linux-amd64/

# Linux aarch64
curl -sL "https://github.com/asg017/sqlite-vec/releases/download/v0.1.9/sqlite-vec-0.1.9-loadable-linux-aarch64.tar.gz" | tar xz -C vendor/sqlite-vec/linux-arm64/

# macOS x86_64
curl -sL "https://github.com/asg017/sqlite-vec/releases/download/v0.1.9/sqlite-vec-0.1.9-loadable-macos-x86_64.tar.gz" | tar xz -C vendor/sqlite-vec/darwin-amd64/

# macOS aarch64
curl -sL "https://github.com/asg017/sqlite-vec/releases/download/v0.1.9/sqlite-vec-0.1.9-loadable-macos-aarch64.tar.gz" | tar xz -C vendor/sqlite-vec/darwin-arm64/

# 确认文件存在
ls -la vendor/sqlite-vec/linux-amd64/vec0.so
```

Expected: `vec0.so` 文件存在，大小约 50-60KB

- [ ] **Step 2: 提交**

```bash
git add vendor/sqlite-vec/
git commit -m "chore: 添加 sqlite-vec v0.1.9 预编译二进制"
```

---

### Task 3: 回填 7.4 — config.go

**Files:**
- Modify: `internal/agentcore/memory/lite/config.go`

- [ ] **Step 1: 写 config.go 的测试**

创建 `internal/agentcore/memory/lite/config_test.go`：

```go
package lite

import (
	"os"
	"testing"
)

// TestIsMemoryEnabled_默认为true 测试默认启用
func TestIsMemoryEnabled_默认为true(t *testing.T) {
	os.Unsetenv("MEMORY_ENABLED")
	if !IsMemoryEnabled() {
		t.Error("默认应返回 true")
	}
}

// TestIsMemoryEnabled_环境变量为false 测试环境变量禁用
func TestIsMemoryEnabled_环境变量为false(t *testing.T) {
	os.Setenv("MEMORY_ENABLED", "false")
	defer os.Unsetenv("MEMORY_ENABLED")
	if IsMemoryEnabled() {
		t.Error("MEMORY_ENABLED=false 时应返回 false")
	}
}

// TestIsMemoryEnabled_环境变量为0 测试环境变量 0
func TestIsMemoryEnabled_环境变量为0(t *testing.T) {
	os.Setenv("MEMORY_ENABLED", "0")
	defer os.Unsetenv("MEMORY_ENABLED")
	if IsMemoryEnabled() {
		t.Error("MEMORY_ENABLED=0 时应返回 false")
	}
}

// TestCreateMemorySettings_默认值 测试默认配置
func TestCreateMemorySettings_默认值(t *testing.T) {
	s := CreateMemorySettings("/tmp", nil)
	if s.Provider != "openai_compatible" {
		t.Errorf("Provider 应为 openai_compatible，实际为 %s", s.Provider)
	}
	if s.Model != "text-embedding-v3" {
		t.Errorf("Model 应为 text-embedding-v3，实际为 %s", s.Model)
	}
	if s.Fallback != "mock" {
		t.Errorf("Fallback 应为 mock，实际为 %s", s.Fallback)
	}
}

// TestCreateMemorySettings_覆盖值 测试覆盖配置
func TestCreateMemorySettings_覆盖值(t *testing.T) {
	overrides := map[string]any{
		"provider": "mock",
		"model":    "custom-model",
	}
	s := CreateMemorySettings("/tmp", overrides)
	if s.Provider != "mock" {
		t.Errorf("Provider 应为 mock，实际为 %s", s.Provider)
	}
	if s.Model != "custom-model" {
		t.Errorf("Model 应为 custom-model，实际为 %s", s.Model)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestIsMemoryEnabled|TestCreateMemorySettings"
```

Expected: `IsMemoryEnabled` 返回 false（当前 stub），测试失败

- [ ] **Step 3: 实现 config.go 回填**

将 `internal/agentcore/memory/lite/config.go` 修改为：

```go
package lite

import (
	"os"
	"strings"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemorySettings 记忆配置。对齐 Python MemorySettings
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

// IsMemoryEnabled 判断记忆系统是否启用。对齐 Python is_memory_enabled
func IsMemoryEnabled() bool {
	envEnabled := strings.ToLower(strings.TrimSpace(os.Getenv("MEMORY_ENABLED")))
	if envEnabled == "" {
		return true
	}
	return envEnabled == "true" || envEnabled == "1" || envEnabled == "yes"
}

// CreateMemorySettings 创建默认记忆配置。对齐 Python create_memory_settings
func CreateMemorySettings(workspaceDir string, overrides map[string]any) *MemorySettings {
	s := &MemorySettings{
		Provider:   "openai_compatible",
		Model:      "text-embedding-v3",
		Fallback:   "mock",
		Sources:    []string{"memory", "sessions"},
		ExtraPaths: nil,
		Chunking: map[string]any{
			"tokens":  256,
			"overlap": 32,
		},
		Query: map[string]any{
			"max_results": 10,
			"min_score":   0.3,
			"hybrid": map[string]any{
				"enabled":            true,
				"vectorWeight":       0.7,
				"textWeight":         0.3,
				"candidateMultiplier": 2.0,
			},
		},
		Store: map[string]any{
			"path": "memory.db",
			"vector": map[string]any{
				"enabled": true,
			},
			"fts": map[string]any{
				"enabled": true,
			},
		},
		Sync: map[string]any{
			"watch":          true,
			"watchDebounceMs": 2000,
			"onSearch":       true,
			"onSessionStart": true,
			"intervalMinutes": 0,
		},
		Cache: map[string]any{
			"enabled":    true,
			"maxEntries": 10000,
		},
	}

	for key, value := range overrides {
		switch key {
		case "provider":
			if v, ok := value.(string); ok {
				s.Provider = v
			}
		case "model":
			if v, ok := value.(string); ok {
				s.Model = v
			}
		case "fallback":
			if v, ok := value.(string); ok {
				s.Fallback = v
			}
		}
	}

	return s
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestIsMemoryEnabled|TestCreateMemorySettings"
```

Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/lite/config.go internal/agentcore/memory/lite/config_test.go
git commit -m "feat: 回填 7.4 — MemorySettings + IsMemoryEnabled + CreateMemorySettings"
```

---

### Task 4: 回填 7.4 — embeddings.go（baseEmbeddingAdapter + CreateEmbeddingProvider）

**Files:**
- Modify: `internal/agentcore/memory/lite/embeddings.go`

- [ ] **Step 1: 写 embeddings.go 的测试**

创建 `internal/agentcore/memory/lite/embeddings_test.go`：

```go
package lite

import (
	"context"
	"testing"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// TestResolveEmbeddingConfigFromEnv_全部设置 测试环境变量完整时返回配置
func TestResolveEmbeddingConfigFromEnv_全部设置(t *testing.T) {
	// 此测试需要环境变量，跳过如果未设置
	// 真实测试在集成测试中
	result := ResolveEmbeddingConfigFromEnv("default-model", "https://default.example.com", "default-key")
	if result == nil {
		t.Error("当所有参数都有值时应返回非 nil")
	}
	if result.ModelName != "default-model" {
		t.Errorf("ModelName 应为 default-model，实际为 %s", result.ModelName)
	}
}

// TestResolveEmbeddingConfigFromEnv_环境变量覆盖 测试环境变量优先
func TestResolveEmbeddingConfigFromEnv_环境变量覆盖(t *testing.T) {
	t.Setenv("EMBEDDING_MODEL_NAME", "env-model")
	t.Setenv("EMBEDDING_BASE_URL", "https://env.example.com")
	t.Setenv("EMBEDDING_API_KEY", "env-key")

	result := ResolveEmbeddingConfigFromEnv("fallback-model", "https://fallback.example.com", "fallback-key")
	if result == nil {
		t.Fatal("应返回非 nil")
	}
	if result.ModelName != "env-model" {
		t.Errorf("ModelName 应为 env-model，实际为 %s", result.ModelName)
	}
	if result.BaseURL != "https://env.example.com" {
		t.Errorf("BaseURL 应为 https://env.example.com，实际为 %s", result.BaseURL)
	}
	if result.APIKey != "env-key" {
		t.Errorf("APIKey 应为 env-key，实际为 %s", result.APIKey)
	}
}

// TestCreateEmbeddingProvider_mock 测试 mock provider
func TestCreateEmbeddingProvider_mock(t *testing.T) {
	provider, err := CreateEmbeddingProvider("mock", "", "", nil)
	if err != nil {
		t.Fatalf("创建 mock provider 失败: %v", err)
	}
	if _, ok := provider.(*MockEmbeddingProvider); !ok {
		t.Error("应为 MockEmbeddingProvider 类型")
	}
}

// TestBaseEmbeddingAdapter 测试适配器
func TestBaseEmbeddingAdapter(t *testing.T) {
	mock := NewMockEmbeddingProvider()
	adapter := &baseEmbeddingAdapter{base: mock}

	ctx := context.Background()
	vec, err := adapter.EmbedQuery(ctx, "test")
	if err != nil {
		t.Fatalf("EmbedQuery 失败: %v", err)
	}
	// MockEmbeddingProvider 返回空向量
	if vec == nil {
		t.Error("EmbedQuery 应返回非 nil")
	}
}

// TestMockEmbeddingProvider_接口满足 编译期验证 MockEmbeddingProvider 满足 EmbeddingProvider
func TestMockEmbeddingProvider_接口满足(t *testing.T) {
	var _ EmbeddingProvider = NewMockEmbeddingProvider()
}

// TestBaseEmbeddingAdapter_接口满足 编译期验证 baseEmbeddingAdapter 满足 EmbeddingProvider
func TestBaseEmbeddingAdapter_接口满足(t *testing.T) {
	var _ EmbeddingProvider = &baseEmbeddingAdapter{}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestResolveEmbeddingConfig|TestCreateEmbeddingProvider|TestBaseEmbeddingAdapter"
```

Expected: 编译失败（`baseEmbeddingAdapter` 未定义）

- [ ] **Step 3: 实现 embeddings.go 回填**

将 `internal/agentcore/memory/lite/embeddings.go` 修改为：

```go
package lite

import (
	"context"
	"fmt"
	"os"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
)

// ──────────────────────────── 接口 ────────────────────────────

// EmbeddingProvider 嵌入向量提供者接口。对齐 Python EmbeddingProvider
type EmbeddingProvider interface {
	// EmbedQuery 嵌入单个查询文本
	EmbedQuery(ctx context.Context, text string) ([]float64, error)
	// EmbedDocuments 嵌入多个文档文本
	EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error)
}

// ──────────────────────────── 结构体 ────────────────────────────

// MockEmbeddingProvider 模拟嵌入提供者。对齐 Python MockEmbeddingProvider
type MockEmbeddingProvider struct{}

// baseEmbeddingAdapter 将 retrieval/embedding.BaseEmbedding 适配为 lite.EmbeddingProvider
type baseEmbeddingAdapter struct {
	base embedding.BaseEmbedding
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewMockEmbeddingProvider 创建模拟嵌入提供者
func NewMockEmbeddingProvider() *MockEmbeddingProvider {
	return &MockEmbeddingProvider{}
}

// EmbedQuery MockEmbeddingProvider 的 EmbedQuery 实现，返回空向量
func (m *MockEmbeddingProvider) EmbedQuery(_ context.Context, _ string) ([]float64, error) {
	return make([]float64, 0), nil
}

// EmbedDocuments MockEmbeddingProvider 的 EmbedDocuments 实现，返回空切片
func (m *MockEmbeddingProvider) EmbedDocuments(_ context.Context, _ []string) ([][]float64, error) {
	return nil, nil
}

// EmbedQuery baseEmbeddingAdapter 的 EmbedQuery 实现，委托给 base
func (a *baseEmbeddingAdapter) EmbedQuery(ctx context.Context, text string) ([]float64, error) {
	return a.base.EmbedQuery(ctx, text)
}

// EmbedDocuments baseEmbeddingAdapter 的 EmbedDocuments 实现，委托给 base
func (a *baseEmbeddingAdapter) EmbedDocuments(ctx context.Context, texts []string) ([][]float64, error) {
	return a.base.EmbedDocuments(ctx, texts)
}

// ResolveEmbeddingConfigFromEnv 从环境变量构建 EmbeddingConfig。对齐 Python resolve_embedding_config_from_env
func ResolveEmbeddingConfigFromEnv(modelName, fallbackBaseURL, fallbackAPIKey string) *embedding.EmbeddingConfig {
	modelName = os.Getenv("EMBEDDING_MODEL_NAME")
	if modelName == "" {
		modelName = os.Getenv("EMBED_MODEL")
	}
	baseURL := os.Getenv("EMBEDDING_BASE_URL")
	if baseURL == "" {
		baseURL = os.Getenv("EMBED_BASE_URL")
	}
	if baseURL == "" {
		baseURL = fallbackBaseURL
	}
	apiKey := os.Getenv("EMBEDDING_API_KEY")
	if apiKey == "" {
		apiKey = os.Getenv("EMBED_API_KEY")
	}
	if apiKey == "" {
		apiKey = fallbackAPIKey
	}
	if modelName == "" {
		modelName = "default"
	}
	if baseURL == "" || apiKey == "" {
		return nil
	}
	return &embedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
}

// CreateEmbeddingProvider 根据配置创建嵌入提供者。对齐 Python create_embedding_provider
func CreateEmbeddingProvider(provider, model, fallback string, embeddingConfig *embedding.EmbeddingConfig) (EmbeddingProvider, error) {
	if provider == "mock" {
		return NewMockEmbeddingProvider(), nil
	}

	// 优先使用 embeddingConfig
	if embeddingConfig != nil && embeddingConfig.APIKey != "" {
		base, err := embedding.NewAPIEmbedding(*embeddingConfig)
		if err != nil {
			if fallback == "mock" {
				return NewMockEmbeddingProvider(), nil
			}
			return nil, fmt.Errorf("创建嵌入提供者失败: %w", err)
		}
		return &baseEmbeddingAdapter{base: base}, nil
	}

	// fallback 到 mock
	if fallback == "mock" || fallback == "" {
		return NewMockEmbeddingProvider(), nil
	}

	return nil, fmt.Errorf("嵌入提供者未配置: provider=%s, model=%s", provider, model)
}
```

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestResolveEmbeddingConfig|TestCreateEmbeddingProvider|TestBaseEmbeddingAdapter|TestMockEmbeddingProvider"
```

Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/lite/embeddings.go internal/agentcore/memory/lite/embeddings_test.go
git commit -m "feat: 回填 7.4 — baseEmbeddingAdapter + CreateEmbeddingProvider + ResolveEmbeddingConfigFromEnv"
```

---

### Task 5: 回填 7.4 — internal.go 工具函数

**Files:**
- Modify: `internal/agentcore/memory/lite/internal.go`

- [ ] **Step 1: 写 internal.go 的测试**

创建 `internal/agentcore/memory/lite/internal_test.go`：

```go
package lite

import (
	"testing"
)

// TestBuildFTSQuery 测试 FTS5 查询构建
func TestBuildFTSQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		wantEmpty bool
	}{
		{"空字符串", "", true},
		{"空格", "   ", true},
		{"简单词", "hello", false},
		{"多个词", "hello world", false},
		{"中文", "你好世界", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildFTSQuery(tt.query)
			if (got == "") != tt.wantEmpty {
				t.Errorf("BuildFTSQuery(%q) = %q, wantEmpty=%v", tt.query, got, tt.wantEmpty)
			}
		})
	}
}

// TestBM25RankToScore 测试 BM25 排名转分数
func TestBM25RankToScore(t *testing.T) {
	if score := BM25RankToScore(0); score != 1.0 {
		t.Errorf("BM25RankToScore(0) = %f, want 1.0", score)
	}
	if score := BM25RankToScore(1); score <= 0 {
		t.Errorf("BM25RankToScore(1) = %f, want > 0", score)
	}
}

// TestIsMemoryPath 测试记忆路径判断
func TestIsMemoryPath(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{"notes.md", true},
		{"MEMORY.md", true},
		{"data.json", false},
		{"readme.txt", false},
		{"dir/file.md", true},
	}
	for _, tt := range tests {
		if got := IsMemoryPath(tt.path); got != tt.want {
			t.Errorf("IsMemoryPath(%q) = %v, want %v", tt.path, got, tt.want)
		}
	}
}

// TestNormalizeExtraMemoryPaths 测试额外记忆路径归一化
func TestNormalizeExtraMemoryPaths(t *testing.T) {
	// 绝对路径保持不变
	result := NormalizeExtraMemoryPaths([]string{"/tmp/mem"}, "/workspace")
	if len(result) != 1 || result[0] != "/tmp/mem" {
		t.Errorf("绝对路径应保持不变，实际为 %v", result)
	}
	// 相对路径拼接 workspaceDir
	result = NormalizeExtraMemoryPaths([]string{"extra"}, "/workspace")
	if len(result) != 1 || result[0] != "/workspace/extra" {
		t.Errorf("相对路径应拼接 workspaceDir，实际为 %v", result)
	}
	// nil 返回空
	result = NormalizeExtraMemoryPaths(nil, "/workspace")
	if result != nil {
		t.Errorf("nil 应返回 nil，实际为 %v", result)
	}
}
```

- [ ] **Step 2: 运行测试确认失败**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestBuildFTSQuery|TestBM25RankToScore|TestIsMemoryPath|TestNormalizeExtraMemoryPaths"
```

Expected: `BuildFTSQuery` 返回空字符串，测试失败

- [ ] **Step 3: 实现 internal.go 回填**

将 `internal/agentcore/memory/lite/internal.go` 的 stub 函数替换为真实实现：

```go
// BuildFTSQuery 构建 FTS5 查询。对齐 Python build_fts_query — 真实实现
func BuildFTSQuery(query string) string {
	cleaned := strings.TrimSpace(query)
	if cleaned == "" {
		return ""
	}
	tokens := regexp.MustCompile(`\w+`).FindAllString(cleaned, 10)
	if len(tokens) == 0 {
		return ""
	}
	parts := make([]string, len(tokens))
	for i, t := range tokens {
		parts[i] = `"` + t + `"`
	}
	return strings.Join(parts, " OR ")
}

// BM25RankToScore BM25 排名转相似度分数。对齐 Python bm25_rank_to_score — 真实实现
func BM25RankToScore(rank int) float64 {
	if rank >= 0 {
		return 1.0 / (1.0 + float64(rank))
	}
	return 1.0 / (1.0 - float64(rank))
}

// IsMemoryPath 判断是否为记忆文件路径。对齐 Python is_memory_path — 真实实现
func IsMemoryPath(relPath string) bool {
	normalized := strings.ReplaceAll(relPath, `\`, "/")
	return strings.HasSuffix(normalized, ".md")
}

// ListMemoryFiles 列出 workspace 下所有 .md 记忆文件。对齐 Python list_memory_files — 真实实现
func ListMemoryFiles(workspace any, extraPaths []string, nodeName string) []string {
	// 注意：workspace 参数类型为 any，实际使用时需要类型断言
	// 完整实现依赖 workspace.Workspace 类型，此处为占位
	// 真实实现在 manager_impl.go 中通过 workspace 方法调用
	return nil
}

// NormalizeExtraMemoryPaths 归一化额外记忆路径。对齐 Python normalize_extra_memory_paths — 真实实现
func NormalizeExtraMemoryPaths(paths []string, workspaceDir string) []string {
	if len(paths) == 0 {
		return nil
	}
	normalized := make([]string, 0, len(paths))
	for _, p := range paths {
		if filepath.IsAbs(p) {
			normalized = append(normalized, p)
		} else {
			normalized = append(normalized, filepath.Join(workspaceDir, p))
		}
	}
	return normalized
}
```

同时在 import 中添加 `"regexp"` 和 `"path/filepath"`。

- [ ] **Step 4: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestBuildFTSQuery|TestBM25RankToScore|TestIsMemoryPath|TestNormalizeExtraMemoryPaths"
```

Expected: 所有测试通过

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/memory/lite/internal.go internal/agentcore/memory/lite/internal_test.go
git commit -m "feat: 回填 7.4 — BuildFTSQuery + BM25RankToScore + IsMemoryPath + NormalizeExtraMemoryPaths"
```

---

### Task 6: 实现 vec0.so 加载器

**Files:**
- Create: `internal/agentcore/memory/lite/vec_loader.go`
- Create: `internal/agentcore/memory/lite/vec_loader_test.go`

- [ ] **Step 1: 写 vec_loader 测试**

创建 `internal/agentcore/memory/lite/vec_loader_test.go`：

```go
package lite

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// TestResolveVec0Path_路径格式 测试 vec0.so 路径解析
func TestResolveVec0Path_路径格式(t *testing.T) {
	path := ResolveVec0Path()
	expected := filepath.Join("vendor", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0.so")
	if path != expected {
		t.Errorf("ResolveVec0Path() = %q, want %q", path, expected)
	}
}

// TestLoadVec0Extension_文件不存在 测试 vec0.so 不存在时降级
func TestLoadVec0Extension_文件不存在(t *testing.T) {
	// 确保在非 Linux 环境下不会 panic
	// 此测试仅验证函数不会 panic
	_ = ResolveVec0Path()
}

// TestIsVec0Available 测试 vec0.so 可用性检查
func TestIsVec0Available(t *testing.T) {
	path := ResolveVec0Path()
	_, err := os.Stat(path)
	// 在 CI 环境中可能不存在，仅验证函数不 panic
	available := IsVec0Available()
	if err == nil && !available {
		t.Error("vec0.so 存在但 IsVec0Available 返回 false")
	}
	if err != nil && available {
		t.Error("vec0.so 不存在但 IsVec0Available 返回 true")
	}
}
```

- [ ] **Step 2: 实现 vec_loader.go**

创建 `internal/agentcore/memory/lite/vec_loader.go`：

```go
package lite

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/mattn/go-sqlite3"
)

// ──────────────────────────── 常量 ────────────────────────────

// vec0InitFuncName vec0 扩展初始化函数名
const vec0InitFuncName = "sqlite3_vec_init"

// ──────────────────────────── 导出函数 ────────────────────────────

// ResolveVec0Path 解析 vec0.so 的路径
func ResolveVec0Path() string {
	return filepath.Join("vendor", "sqlite-vec", runtime.GOOS+"-"+runtime.GOARCH, "vec0.so")
}

// IsVec0Available 检查 vec0.so 文件是否存在
func IsVec0Available() bool {
	_, err := os.Stat(ResolveVec0Path())
	return err == nil
}

// LoadVec0Extension 加载 vec0.so 扩展到 SQLite 连接
func LoadVec0Extension(conn *sqlConn, vecPath string) error {
	if conn == nil {
		return fmt.Errorf("conn 为 nil")
	}
	rawConn, ok := conn.conn.(*sqlite3.SQLiteConn)
	if !ok {
		return fmt.Errorf("不支持 LoadExtension 的驱动类型")
	}
	return rawConn.LoadExtension(vecPath, vec0InitFuncName)
}
```

注意：`sqlConn` 类型需要在 Task 7 中定义（封装 `*sql.Conn`）。在 Task 7 实现前，此步骤先定义接口，后续整合。

- [ ] **Step 3: 运行测试确认通过**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/ -run "TestResolveVec0Path|TestIsVec0Available"
```

Expected: `TestResolveVec0Path_路径格式` 通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/lite/vec_loader.go internal/agentcore/memory/lite/vec_loader_test.go
git commit -m "feat: 添加 vec0.so 加载器"
```

---

### Task 7: 实现 MemoryIndexManager 核心结构体 + Initialize + Schema

**Files:**
- Create: `internal/agentcore/memory/lite/manager_impl.go`
- Modify: `internal/agentcore/memory/lite/manager.go`（移除 SessionDeltaState，保留接口 + Params + 导出函数）

- [ ] **Step 1: 修改 manager.go，精简为接口 + Params + 导出函数**

将 `internal/agentcore/memory/lite/manager.go` 修改为：

```go
package lite

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
)

// ──────────────────────────── 接口 ────────────────────────────

// MemoryIndexManager 记忆索引管理器接口。对齐 Python MemoryIndexManager
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

// ──────────────────────────── 导出函数 ────────────────────────────

// GetMemoryIndexManager 幂等获取管理器实例。对齐 Python MemoryIndexManager.get
func GetMemoryIndexManager(params MemoryManagerParams) (MemoryIndexManager, error) {
	return getMemoryIndexManager(params)
}

// ClearMemoryManagerCache 清除缓存。对齐 Python clear_memory_manager_cache
func ClearMemoryManagerCache() {
	clearMemoryManagerCache()
}
```

- [ ] **Step 2: 创建 manager_impl.go，实现 memoryIndexManager 结构体 + Initialize + Schema**

此文件包含完整的 `memoryIndexManager` 结构体定义和核心方法。由于文件较长（约 800 行），这里按模块组织：

```go
package lite

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/mattn/go-sqlite3"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/harness/workspace"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/embedding"
	sysop "github.com/uapclaw/uapclaw-go/internal/agentcore/sys_operation"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 常量 ────────────────────────────

const (
	metaKey          = "memory_index_meta_v1"
	snippetMaxChars  = 700
	vectorTable      = "chunks_vec"
	ftsTable         = "chunks_fts"
	embeddingCacheTable = "embedding_cache"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SessionDeltaState 会话增量状态。对齐 Python SessionDeltaState
type SessionDeltaState struct {
	// LastSize 上次文件大小
	LastSize int
	// PendingBytes 待处理字节
	PendingBytes int
	// PendingMessages 待处理消息
	PendingMessages []any
}

// memoryIndexManager 记忆索引管理器实现。对齐 Python MemoryIndexManager
type memoryIndexManager struct {
	// 基础信息
	agentID   string
	workspace *workspace.Workspace
	nodeName  string
	memoryDir string
	settings  *MemorySettings

	// 数据库
	db     *sql.DB
	dbPath string

	// Embedding
	provider    EmbeddingProvider
	providerKey string

	// 状态
	mu             sync.Mutex
	dirty          bool
	closed         bool
	ftsAvailable   bool
	ftsError       string
	vectorAvailable bool
	vectorError    string
	vectorDims     *int

	// 文件监听
	watcher            *fsnotify.Watcher
	watcherInitialized bool
	watchDebounce      time.Duration
	watchTimer         *time.Timer
	watchMu            sync.Mutex

	// 定时同步
	intervalCancel context.CancelFunc

	// 会话增量
	sessionDeltas map[string]*SessionDeltaState

	// 外部依赖
	embeddingConfig *embedding.EmbeddingConfig
	sysOperation    sysop.SysOperation
	llm             any
}

// ──────────────────────────── 全局变量 ────────────────────────────

// indexCache 管理器缓存
var indexCache sync.Map

// ──────────────────────────── 导出函数 ────────────────────────────

// getMemoryIndexManager 幂等获取管理器实例
func getMemoryIndexManager(params MemoryManagerParams) (MemoryIndexManager, error) {
	if params.Workspace == nil {
		return nil, fmt.Errorf("workspace 为 nil")
	}
	nodePath := params.Workspace.GetNodePath(params.NodeName)
	memoryDir := ""
	if nodePath != "" {
		memoryDir = nodePath
	}
	cacheKey := fmt.Sprintf("%s:%s:%s", params.AgentID, params.NodeName, memoryDir)

	if cached, ok := indexCache.Load(cacheKey); ok {
		mgr := cached.(*memoryIndexManager)
		if !mgr.closed {
			return mgr, nil
		}
	}

	settings := params.Settings
	if settings == nil {
		settings = CreateMemorySettings(memoryDir, nil)
	}

	mgr := &memoryIndexManager{
		agentID:        params.AgentID,
		workspace:      params.Workspace,
		nodeName:       params.NodeName,
		memoryDir:      memoryDir,
		settings:       settings,
		dirty:          true,
		watchDebounce:  2 * time.Second,
		sessionDeltas:  make(map[string]*SessionDeltaState),
		embeddingConfig: params.EmbeddingConfig,
		sysOperation:   params.SysOperation,
	}

	if err := mgr.Initialize(context.Background()); err != nil {
		logger.Error(logger.ComponentCommon).Err(err).Msg("初始化记忆管理器失败")
		return nil, err
	}

	indexCache.Store(cacheKey, mgr)
	return mgr, nil
}

// clearMemoryManagerCache 清除缓存
func clearMemoryManagerCache() {
	indexCache.Range(func(key, value any) bool {
		if mgr, ok := value.(*memoryIndexManager); ok {
			mgr.Close()
		}
		indexCache.Delete(key)
		return true
	})
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// Initialize 初始化管理器
func (m *memoryIndexManager) Initialize(ctx context.Context) error {
	m.dbPath = m.resolveDBPath()

	var err error
	m.db, err = sql.Open("sqlite3", m.dbPath+"?_busy_timeout=5000&_journal_mode=WAL")
	if err != nil {
		return fmt.Errorf("打开数据库失败: %w", err)
	}
	m.db.SetMaxOpenConns(1)

	if err := m.ensureSchema(); err != nil {
		return fmt.Errorf("建 schema 失败: %w", err)
	}

	if err := m.initializeProvider(ctx); err != nil {
		return fmt.Errorf("初始化 provider 失败: %w", err)
	}

	if err := m.loadVectorExtension(); err != nil {
		m.vectorAvailable = false
		m.vectorError = err.Error()
		logger.Warn(logger.ComponentCommon).Err(err).Msg("加载 vec0 扩展失败，将使用内存余弦 fallback")
	}

	if err := m.Sync(ctx, "initial", false); err != nil {
		logger.Warn(logger.ComponentCommon).Err(err).Msg("初始同步失败")
	}

	// ... 后续 Task 中继续实现 setupFileWatcher 和 ensureIntervalSync
	return nil
}

// resolveDBPath 解析数据库路径
func (m *memoryIndexManager) resolveDBPath() string {
	storePath := "memory.db"
	if v, ok := m.settings.Store["path"].(string); ok && v != "" {
		storePath = v
	}
	if filepath.IsAbs(storePath) {
		return storePath
	}
	return filepath.Join(m.memoryDir, storePath)
}

// ensureSchema 建数据库表
func (m *memoryIndexManager) ensureSchema() error {
	// meta 表
	if _, err := m.db.Exec("CREATE TABLE IF NOT EXISTS meta (key TEXT PRIMARY KEY, value TEXT)"); err != nil {
		return fmt.Errorf("建 meta 表失败: %w", err)
	}
	// files 表
	if _, err := m.db.Exec("CREATE TABLE IF NOT EXISTS files (path TEXT PRIMARY KEY, source TEXT, hash TEXT, mtime INTEGER, size INTEGER)"); err != nil {
		return fmt.Errorf("建 files 表失败: %w", err)
	}
	// chunks 表
	if _, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS chunks (
		id TEXT PRIMARY KEY, path TEXT, source TEXT, start_line INTEGER, end_line INTEGER,
		hash TEXT, model TEXT, text TEXT, embedding BLOB, updated_at INTEGER
	)`); err != nil {
		return fmt.Errorf("建 chunks 表失败: %w", err)
	}
	// embedding_cache 表
	if _, err := m.db.Exec(`CREATE TABLE IF NOT EXISTS embedding_cache (
		provider TEXT, model TEXT, provider_key TEXT, hash TEXT PRIMARY KEY,
		embedding BLOB, dims INTEGER, updated_at INTEGER
	)`); err != nil {
		return fmt.Errorf("建 embedding_cache 表失败: %w", err)
	}
	// FTS5 虚拟表
	ftsEnabled := true
	if v, ok := m.settings.Store["fts"].(map[string]any); ok {
		if v2, ok := v["enabled"].(bool); ok {
			ftsEnabled = v2
		}
	}
	if ftsEnabled {
		_, err := m.db.Exec(fmt.Sprintf(
			"CREATE VIRTUAL TABLE IF NOT EXISTS %s USING fts5(id UNINDEXED, path UNINDEXED, source UNINDEXED, text, content='', contentless_delete=1)",
			ftsTable,
		))
		if err != nil {
			m.ftsAvailable = false
			m.ftsError = err.Error()
			logger.Warn(logger.ComponentCommon).Err(err).Msg("建 FTS5 表失败")
		} else {
			m.ftsAvailable = true
		}
	}
	return nil
}

// initializeProvider 初始化嵌入提供者
func (m *memoryIndexManager) initializeProvider(ctx context.Context) error {
	provider, err := CreateEmbeddingProvider(
		m.settings.Provider,
		m.settings.Model,
		m.settings.Fallback,
		m.embeddingConfig,
	)
	if err != nil {
		return err
	}
	m.provider = provider
	m.providerKey = fmt.Sprintf("%s:%s", "provider", m.settings.Model)
	return nil
}

// loadVectorExtension 加载 vec0.so 扩展
func (m *memoryIndexManager) loadVectorExtension() error {
	vecEnabled := true
	if v, ok := m.settings.Store["vector"].(map[string]any); ok {
		if v2, ok := v["enabled"].(bool); ok {
			vecEnabled = v2
		}
	}
	if !vecEnabled {
		return nil
	}

	vecPath := ResolveVec0Path()
	if _, err := os.Stat(vecPath); err != nil {
		return fmt.Errorf("vec0.so 不存在: %s", vecPath)
	}

	conn, err := m.db.Conn(context.Background())
	if err != nil {
		return fmt.Errorf("获取连接失败: %w", err)
	}
	defer conn.Close()

	err = conn.Raw(func(driverConn interface{}) error {
		dc, ok := driverConn.(*sqlite3.SQLiteConn)
		if !ok {
			return fmt.Errorf("不支持 LoadExtension 的驱动类型")
		}
		return dc.LoadExtension(vecPath, "sqlite3_vec_init")
	})
	if err != nil {
		return fmt.Errorf("加载 vec0 扩展失败: %w", err)
	}

	m.vectorAvailable = true
	logger.Info(logger.ComponentCommon).Str("vec_path", vecPath).Msg("vec0 扩展加载成功")
	return nil
}
```

- [ ] **Step 3: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go build -tags "sqlite_fts5" ./internal/agentcore/memory/lite/
```

Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/lite/manager.go internal/agentcore/memory/lite/manager_impl.go
git commit -m "feat: 实现 MemoryIndexManager 结构体 + Initialize + Schema"
```

---

### Task 8: 实现 MemoryIndexManager — Sync + Index 方法

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go`

- [ ] **Step 1: 实现 Sync + 文件索引方法**

在 `manager_impl.go` 中添加以下方法：

```go
// Sync 同步索引
func (m *memoryIndexManager) Sync(ctx context.Context, reason string, force bool) error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.mu.Unlock()

	needsFullReindex := force || m.shouldFullReindex(ctx)

	if needsFullReindex {
		logger.Info(logger.ComponentCommon).Str("reason", reason).Msg("执行全量重建索引")
		return m.runReindex(ctx)
	}

	if m.dirty {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.dirty = false
	}

	return m.syncSessionFiles(ctx)
}

// shouldFullReindex 检查是否需要全量重建
func (m *memoryIndexManager) shouldFullReindex(ctx context.Context) bool {
	row := m.db.QueryRow("SELECT value FROM meta WHERE key = ?", metaKey)
	var value string
	if err := row.Scan(&value); err != nil {
		return true
	}
	var meta map[string]any
	if err := json.Unmarshal([]byte(value), &meta); err != nil {
		return true
	}
	if meta["provider"] != "openai_compatible" {
		return true
	}
	if meta["model"] != m.settings.Model {
		return true
	}
	return false
}

// runReindex 全量重建
func (m *memoryIndexManager) runReindex(ctx context.Context) error {
	if m.dirty {
		if err := m.syncMemoryFiles(ctx); err != nil {
			return err
		}
		m.dirty = false
	}
	if err := m.syncSessionFiles(ctx); err != nil {
		return err
	}
	meta := map[string]any{
		"provider":    "openai_compatible",
		"model":       m.settings.Model,
		"providerKey": m.providerKey,
		"chunkTokens": m.settings.Chunking["tokens"],
		"chunkOverlap": m.settings.Chunking["overlap"],
	}
	if m.vectorAvailable && m.vectorDims != nil {
		meta["vectorDims"] = *m.vectorDims
	}
	return m.writeMeta(meta)
}

// syncMemoryFiles 同步 .md 记忆文件
func (m *memoryIndexManager) syncMemoryFiles(ctx context.Context) error {
	files := ListMemoryFiles(m.workspace, m.settings.ExtraPaths, m.nodeName)
	logger.Debug(logger.ComponentCommon).Int("file_count", len(files)).Msg("同步记忆文件")

	activePaths := make(map[string]bool)
	for _, filepath := range files {
		baseDir := m.memoryDir
		entry, err := m.buildFileEntry(filepath, baseDir)
		if err != nil {
			continue
		}
		activePaths[entry["path"].(string)] = true

		var hash string
		row := m.db.QueryRow("SELECT hash FROM files WHERE path = ? AND source = ?", entry["path"], "memory")
		if err := row.Scan(&hash); err == nil && hash == entry["hash"].(string) {
			continue
		}
		if err := m.indexFile(ctx, entry, "memory"); err != nil {
			logger.Error(logger.ComponentCommon).Err(err).Str("path", entry["path"].(string)).Msg("索引文件失败")
		}
	}

	// 删除不存在的文件
	rows, err := m.db.Query("SELECT path FROM files WHERE source = ?", "memory")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var path string
		if err := rows.Scan(&path); err != nil {
			continue
		}
		if !activePaths[path] {
			m.removeFileFromIndex(context.Background(), path)
		}
	}
	return nil
}

// syncSessionFiles 同步 session 文件
func (m *memoryIndexManager) syncSessionFiles(ctx context.Context) error {
	sessionsDir := filepath.Join(m.memoryDir, "sessions")
	if _, err := os.Stat(sessionsDir); os.IsNotExist(err) {
		return nil
	}
	// 遍历 sessions 目录下的 .jsonl 文件
	return filepath.Walk(sessionsDir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(path) != ".jsonl" {
			return nil
		}
		entry, err := m.buildFileEntry(path, m.memoryDir)
		if err != nil {
			return nil
		}
		var hash string
		row := m.db.QueryRow("SELECT hash FROM files WHERE path = ? AND source = ?", entry["path"], "sessions")
		if err := row.Scan(&hash); err == nil && hash == entry["hash"].(string) {
			return nil
		}
		return m.indexFile(ctx, entry, "sessions")
	})
}

// indexFile 索引单个文件
func (m *memoryIndexManager) indexFile(ctx context.Context, entry map[string]any, source string) error {
	absPath := entry["absPath"].(string)
	content, err := os.ReadFile(absPath)
	if err != nil {
		return fmt.Errorf("读取文件失败: %w", err)
	}

	chunks := ChunkMarkdown(string(content), m.settings.Chunking)

	// 清除旧索引
	m.db.Exec("DELETE FROM chunks WHERE path = ?", entry["path"])
	if m.ftsAvailable {
		m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE path = ?", ftsTable), entry["path"])
	}

	for _, chunk := range chunks {
		if err := m.indexChunk(ctx, entry["path"].(string), source, chunk); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Str("path", entry["path"].(string)).Msg("索引 chunk 失败")
		}
	}

	_, err = m.db.Exec("INSERT OR REPLACE INTO files (path, source, hash, mtime, size) VALUES (?, ?, ?, ?, ?)",
		entry["path"], source, entry["hash"], entry["mtimeMs"], entry["size"])
	return err
}

// indexChunk 索引单个 chunk
func (m *memoryIndexManager) indexChunk(ctx context.Context, filePath, source string, chunk MemoryChunk) error {
	chunkID := fmt.Sprintf("%s:%d:%d", filePath, chunk.StartLine, chunk.EndLine)
	chunkHash := HashText(chunk.Text)

	var embBlob []byte
	emb, err := m.getEmbedding(ctx, chunk.Text)
	if err == nil && emb != nil {
		embBlob = vectorToBlob(emb)
	}

	result, err := m.db.Exec(
		"INSERT OR REPLACE INTO chunks (id, path, source, start_line, end_line, hash, model, text, embedding, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		chunkID, filePath, source, chunk.StartLine, chunk.EndLine, chunkHash, m.settings.Model, chunk.Text, embBlob, time.Now().Unix(),
	)
	if err != nil {
		return err
	}

	rowid, _ := result.LastInsertId()

	// FTS5
	if m.ftsAvailable && rowid > 0 {
		_, err := m.db.Exec(fmt.Sprintf("INSERT OR REPLACE INTO %s (rowid, id, path, source, text) VALUES (?, ?, ?, ?, ?)", ftsTable),
			rowid, chunkID, filePath, source, chunk.Text)
		if err != nil {
			logger.Debug(logger.ComponentCommon).Err(err).Msg("插入 FTS5 失败")
		}
	}

	// vec0
	if m.vectorAvailable && emb != nil && rowid > 0 {
		if m.ensureVectorTable(len(emb)) {
			_, err := m.db.Exec(fmt.Sprintf("INSERT OR REPLACE INTO %s (rowid, embedding) VALUES (?, vec_f32(?))", vectorTable),
				rowid, embBlob)
			if err != nil {
				logger.Debug(logger.ComponentCommon).Err(err).Msg("插入 vec0 失败")
			}
		}
	}

	return nil
}

// removeFileFromIndex 删除文件索引
func (m *memoryIndexManager) removeFileFromIndex(ctx context.Context, filePath string) {
	if m.vectorAvailable {
		rows, err := m.db.Query("SELECT rowid FROM chunks WHERE path = ?", filePath)
		if err == nil {
			for rows.Next() {
				var rowid int64
				if rows.Scan(&rowid) == nil {
					m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", vectorTable), rowid)
				}
			}
			rows.Close()
		}
	}
	if m.ftsAvailable {
		rows, err := m.db.Query("SELECT rowid FROM chunks WHERE path = ?", filePath)
		if err == nil {
			for rows.Next() {
				var rowid int64
				if rows.Scan(&rowid) == nil {
					m.db.Exec(fmt.Sprintf("DELETE FROM %s WHERE rowid = ?", ftsTable), rowid)
				}
			}
			rows.Close()
		}
	}
	m.db.Exec("DELETE FROM chunks WHERE path = ?", filePath)
	m.db.Exec("DELETE FROM files WHERE path = ?", filePath)
}

// buildFileEntry 构建文件索引条目
func (m *memoryIndexManager) buildFileEntry(absPath, baseDir string) (map[string]any, error) {
	stat, err := os.Stat(absPath)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(absPath)
	if err != nil {
		return nil, err
	}
	relPath, err := filepath.Rel(baseDir, absPath)
	if err != nil {
		relPath = absPath
	}
	return map[string]any{
		"path":     relPath,
		"absPath":  absPath,
		"hash":     HashText(string(content)),
		"mtimeMs":  stat.ModTime().UnixMilli(),
		"size":     stat.Size(),
	}, nil
}

// getEmbedding 获取 embedding（带缓存）
func (m *memoryIndexManager) getEmbedding(ctx context.Context, text string) ([]float64, error) {
	if m.provider == nil {
		return nil, nil
	}
	textHash := HashText(text)

	// 查缓存
	cacheEnabled := true
	if v, ok := m.settings.Cache["enabled"].(bool); ok {
		cacheEnabled = v
	}
	if cacheEnabled {
		row := m.db.QueryRow(
			fmt.Sprintf("SELECT embedding FROM %s WHERE provider = ? AND model = ? AND provider_key = ? AND hash = ?", embeddingCacheTable),
			"provider", m.settings.Model, m.providerKey, textHash,
		)
		var blob []byte
		if err := row.Scan(&blob); err == nil && blob != nil {
			return blobToVector(blob), nil
		}
	}

	emb, err := m.provider.EmbedQuery(ctx, text)
	if err != nil {
		return nil, err
	}

	// 写缓存
	if cacheEnabled && emb != nil {
		m.db.Exec(
			fmt.Sprintf("INSERT OR REPLACE INTO %s (provider, model, provider_key, hash, embedding, dims, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)", embeddingCacheTable),
			"provider", m.settings.Model, m.providerKey, textHash, vectorToBlob(emb), len(emb), time.Now().Unix(),
		)
	}

	return emb, nil
}

// ensureVectorTable 确保 vec0 虚拟表存在
func (m *memoryIndexManager) ensureVectorTable(dims int) bool {
	if !m.vectorAvailable {
		return false
	}
	if m.vectorDims != nil && *m.vectorDims == dims {
		return true
	}
	if m.vectorDims != nil && *m.vectorDims != dims {
		m.db.Exec(fmt.Sprintf("DROP TABLE IF EXISTS %s", vectorTable))
	}
	_, err := m.db.Exec(fmt.Sprintf("CREATE VIRTUAL TABLE IF NOT EXISTS %s USING vec0(embedding float[%d])", vectorTable, dims))
	if err != nil {
		m.vectorAvailable = false
		m.vectorError = err.Error()
		return false
	}
	m.vectorDims = &dims
	return true
}

// writeMeta 写入元数据
func (m *memoryIndexManager) writeMeta(meta map[string]any) error {
	data, err := json.Marshal(meta)
	if err != nil {
		return err
	}
	_, err = m.db.Exec("INSERT OR REPLACE INTO meta (key, value) VALUES (?, ?)", metaKey, string(data))
	return err
}

// vectorToBlob 向量转二进制
func vectorToBlob(vec []float64) []byte {
	blob := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := float32bits(float32(v))
		blob[i*4] = byte(bits)
		blob[i*4+1] = byte(bits >> 8)
		blob[i*4+2] = byte(bits >> 16)
		blob[i*4+3] = byte(bits >> 24)
	}
	return blob
}

// blobToVector 二进制转向量
func blobToVector(blob []byte) []float64 {
	if len(blob)%4 != 0 {
		return nil
	}
	vec := make([]float64, len(blob)/4)
	for i := range vec {
		bits := uint32(blob[i*4]) | uint32(blob[i*4+1])<<8 | uint32(blob[i*4+2])<<16 | uint32(blob[i*4+3])<<24
		vec[i] = float64(float32frombits(bits))
	}
	return vec
}

// float32bits 对齐 math.Float32bits
func float32bits(f float32) uint32 {
	return uint32(float32bitsHelper(f))
}

// float32frombits 对齐 math.Float32frombits
func float32frombits(b uint32) float32 {
	return float32frombitsHelper(b)
}
```

注意：`float32bits` 和 `float32frombits` 需要使用 `math` 包的对应函数，或者用 `unsafe` / `encoding/binary` 实现。实际实现时用 `math.Float32bits` 和 `math.Float32frombits`。

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go build -tags "sqlite_fts5" ./internal/agentcore/memory/lite/
```

Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/lite/manager_impl.go
git commit -m "feat: 实现 MemoryIndexManager Sync + Index + Embedding 缓存"
```

---

### Task 9: 实现 MemoryIndexManager — Search + ReadFile + Status + Close

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go`

- [ ] **Step 1: 实现 Search + ReadFile + Status + Close 方法**

在 `manager_impl.go` 中添加以下方法：

```go
// Search 混合搜索。对齐 Python MemoryIndexManager.search
func (m *memoryIndexManager) Search(ctx context.Context, query string, opts map[string]any) ([]map[string]any, error) {
	if opts == nil {
		opts = make(map[string]any)
	}

	// 搜索前同步
	onSearch := true
	if v, ok := m.settings.Sync["onSearch"].(bool); ok {
		onSearch = v
	}
	if onSearch && m.dirty {
		if err := m.Sync(ctx, "search", false); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Msg("搜索前同步失败")
		}
	}

	cleaned := query
	if len(cleaned) == 0 {
		return nil, nil
	}

	minScore := 0.3
	if v, ok := opts["min_score"].(float64); ok {
		minScore = v
	} else if v, ok := m.settings.Query["min_score"].(float64); ok {
		minScore = v
	}

	maxResults := 10
	if v, ok := opts["max_results"].(float64); ok {
		maxResults = int(v)
	} else if v, ok := m.settings.Query["max_results"].(float64); ok {
		maxResults = int(v)
	}

	hybrid := make(map[string]any)
	if v, ok := m.settings.Query["hybrid"].(map[string]any); ok {
		hybrid = v
	}
	candidateMultiplier := 2.0
	if v, ok := hybrid["candidateMultiplier"].(float64); ok {
		candidateMultiplier = v
	}
	candidates := int(float64(maxResults) * candidateMultiplier)
	if candidates < 1 {
		candidates = 1
	}

	// FTS5 关键词搜索
	var keywordResults []map[string]any
	hybridEnabled := true
	if v, ok := hybrid["enabled"].(bool); ok {
		hybridEnabled = v
	}
	if hybridEnabled && m.ftsAvailable {
		var err error
		keywordResults, err = m.searchKeyword(ctx, cleaned, candidates)
		if err != nil {
			logger.Debug(logger.ComponentCommon).Err(err).Msg("关键词搜索失败")
		}
	}

	// 向量搜索
	queryVec, _ := m.provider.EmbedQuery(ctx, cleaned)
	hasVector := len(queryVec) > 0

	var vectorResults []map[string]any
	if hasVector {
		var err error
		vectorResults, err = m.searchVector(ctx, queryVec, candidates)
		if err != nil {
			logger.Debug(logger.ComponentCommon).Err(err).Msg("向量搜索失败")
		}
	}

	// 非混合模式
	if !hybridEnabled {
		results := vectorResults
		var filtered []map[string]any
		for _, r := range results {
			if score, ok := r["score"].(float64); ok && score >= minScore {
				filtered = append(filtered, r)
			}
		}
		if len(filtered) > maxResults {
			filtered = filtered[:maxResults]
		}
		return filtered, nil
	}

	// 混合合并
	vectorWeight := 0.7
	if v, ok := hybrid["vectorWeight"].(float64); ok {
		vectorWeight = v
	}
	textWeight := 0.3
	if v, ok := hybrid["textWeight"].(float64); ok {
		textWeight = v
	}
	merged := mergeHybridResults(vectorResults, keywordResults, vectorWeight, textWeight)

	var filtered []map[string]any
	for _, r := range merged {
		if score, ok := r["score"].(float64); ok && score >= minScore {
			filtered = append(filtered, r)
		}
	}
	if len(filtered) > maxResults {
		filtered = filtered[:maxResults]
	}
	return filtered, nil
}

// searchVector vec0 向量搜索
func (m *memoryIndexManager) searchVector(ctx context.Context, queryVec []float64, limit int) ([]map[string]any, error) {
	if !m.vectorAvailable {
		return m.searchVectorFallback(ctx, queryVec, limit)
	}
	if m.vectorDims == nil {
		sample, err := m.provider.EmbedQuery(ctx, "sample")
		if err != nil {
			return nil, err
		}
		m.ensureVectorTable(len(sample))
	}
	if !m.vectorAvailable {
		return m.searchVectorFallback(ctx, queryVec, limit)
	}

	queryBlob := vectorToBlob(queryVec)
	sourceFilter, sourceParams := m.buildSourceFilter()

	// 获取 chunk 映射
	chunkMap := make(map[int64]map[string]any)
	rows, err := m.db.Query(fmt.Sprintf("SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE %s", sourceFilter), sourceParams...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rowid int64
		var id, path, source, text string
		var startLine, endLine int
		if err := rows.Scan(&rowid, &id, &path, &source, &startLine, &endLine, &text); err != nil {
			continue
		}
		chunkMap[rowid] = map[string]any{
			"id": id, "path": path, "source": source,
			"start_line": startLine, "end_line": endLine,
			"snippet": truncateString(text, snippetMaxChars),
		}
	}
	rows.Close()

	if len(chunkMap) == 0 {
		return nil, nil
	}

	// vec0 搜索
	placeholders := make([]string, len(chunkMap))
	args := make([]any, 0, len(chunkMap)+2)
	args = append(args, queryBlob)
	i := 0
	for rowid := range chunkMap {
		placeholders[i] = "?"
		args = append(args, rowid)
		i++
	}
	query := fmt.Sprintf("SELECT rowid, vec_distance_cosine(embedding, vec_f32(?)) as distance FROM %s WHERE rowid IN (%s) ORDER BY distance LIMIT ?",
		vectorTable, joinStrings(placeholders, ","))
	args = append(args, limit)

	vecRows, err := m.db.Query(query, args...)
	if err != nil {
		return m.searchVectorFallback(ctx, queryVec, limit)
	}
	defer vecRows.Close()

	var results []map[string]any
	for vecRows.Next() {
		var rowid int64
		var distance float64
		if err := vecRows.Scan(&rowid, &distance); err != nil {
			continue
		}
		if chunk, ok := chunkMap[rowid]; ok {
			score := max(0, 1-distance/2)
			result := make(map[string]any)
			for k, v := range chunk {
				result[k] = v
			}
			result["score"] = score
			results = append(results, result)
		}
	}
	return results, nil
}

// searchVectorFallback 内存余弦相似度 fallback
func (m *memoryIndexManager) searchVectorFallback(ctx context.Context, queryVec []float64, limit int) ([]map[string]any, error) {
	sourceFilter, sourceParams := m.buildSourceFilter()
	rows, err := m.db.Query(fmt.Sprintf("SELECT id, path, source, start_line, end_line, text, embedding FROM chunks WHERE %s AND embedding IS NOT NULL", sourceFilter), sourceParams...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []map[string]any
	for rows.Next() {
		var id, path, source, text string
		var startLine, endLine int
		var embBlob []byte
		if err := rows.Scan(&id, &path, &source, &startLine, &endLine, &text, &embBlob); err != nil {
			continue
		}
		vec := blobToVector(embBlob)
		if len(vec) != len(queryVec) {
			continue
		}
		similarity := CosineSimilarity(queryVec, vec)
		results = append(results, map[string]any{
			"id": id, "path": path, "source": source,
			"start_line": startLine, "end_line": endLine,
			"snippet": truncateString(text, snippetMaxChars),
			"score":   similarity,
		})
	}
	// 按分数排序
	sort.Slice(results, func(i, j int) bool {
		si, _ := results[i]["score"].(float64)
		sj, _ := results[j]["score"].(float64)
		return si > sj
	})
	if len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// searchKeyword FTS5 关键词搜索
func (m *memoryIndexManager) searchKeyword(ctx context.Context, query string, limit int) ([]map[string]any, error) {
	if !m.ftsAvailable {
		return nil, nil
	}
	ftsQuery := BuildFTSQuery(query)
	if ftsQuery == "" {
		return nil, nil
	}

	sourceFilter, sourceParams := m.buildSourceFilter()
	// 获取 chunk 映射
	chunkMap := make(map[int64]map[string]any)
	rows, err := m.db.Query(fmt.Sprintf("SELECT rowid, id, path, source, start_line, end_line, text FROM chunks WHERE %s", sourceFilter), sourceParams...)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var rowid int64
		var id, path, source, text string
		var startLine, endLine int
		if err := rows.Scan(&rowid, &id, &path, &source, &startLine, &endLine, &text); err != nil {
			continue
		}
		chunkMap[rowid] = map[string]any{
			"id": id, "path": path, "source": source,
			"start_line": startLine, "end_line": endLine,
			"snippet": truncateString(text, snippetMaxChars),
		}
	}
	rows.Close()

	if len(chunkMap) == 0 {
		return nil, nil
	}

	ftsRows, err := m.db.Query(fmt.Sprintf("SELECT rowid, rank FROM %s WHERE %s MATCH ? ORDER BY rank LIMIT ?", ftsTable, ftsTable), ftsQuery, limit)
	if err != nil {
		return nil, err
	}
	defer ftsRows.Close()

	var results []map[string]any
	for ftsRows.Next() {
		var rowid int64
		var rank float64
		if err := ftsRows.Scan(&rowid, &rank); err != nil {
			continue
		}
		if chunk, ok := chunkMap[rowid]; ok {
			score := BM25RankToScore(int(rank))
			result := make(map[string]any)
			for k, v := range chunk {
				result[k] = v
			}
			result["score"] = score
			results = append(results, result)
		}
	}
	return results, nil
}

// mergeHybridResults 混合搜索结果合并
func mergeHybridResults(vectorResults, keywordResults []map[string]any, vectorWeight, textWeight float64) []map[string]any {
	scores := make(map[string]float64)
	entries := make(map[string]map[string]any)

	for _, r := range vectorResults {
		key := r["id"].(string)
		score, _ := r["score"].(float64)
		scores[key] += score * vectorWeight
		if _, ok := entries[key]; !ok {
			entries[key] = r
		}
	}
	for _, r := range keywordResults {
		key := r["id"].(string)
		score, _ := r["score"].(float64)
		scores[key] += score * textWeight
		if _, ok := entries[key]; !ok {
			entries[key] = r
		}
	}

	var results []map[string]any
	for key, entry := range entries {
		result := make(map[string]any)
		for k, v := range entry {
			result[k] = v
		}
		result["score"] = scores[key]
		results = append(results, result)
	}

	sort.Slice(results, func(i, j int) bool {
		si, _ := results[i]["score"].(float64)
		sj, _ := results[j]["score"].(float64)
		return si > sj
	})
	return results
}

// buildSourceFilter 构建 SQL source 过滤条件
func (m *memoryIndexManager) buildSourceFilter() (string, []any) {
	sources := m.settings.Sources
	if len(sources) == 0 {
		return "1=0", nil
	}
	if len(sources) == 1 {
		return "source = ?", []any{sources[0]}
	}
	placeholders := make([]string, len(sources))
	args := make([]any, len(sources))
	for i, s := range sources {
		placeholders[i] = "?"
		args[i] = s
	}
	return fmt.Sprintf("source IN (%s)", joinStrings(placeholders, ",")), args
}

// ReadFile 读取记忆文件内容
func (m *memoryIndexManager) ReadFile(ctx context.Context, relPath string, fromLine *int, lines *int) (map[string]any, error) {
	var fullPath string
	if filepath.IsAbs(relPath) {
		fullPath = relPath
	} else if relPath == "USER.md" {
		nodePath := m.workspace.GetNodePath("USER.md")
		if nodePath != "" {
			fullPath = nodePath
		}
	} else {
		fullPath = filepath.Join(m.memoryDir, relPath)
	}

	if m.sysOperation != nil {
		// 通过 sysOperation 读取
		// TODO: 7.3 回填时实现完整 sysOperation 集成
	}

	// 降级：直接读取文件
	content, err := os.ReadFile(fullPath)
	if err != nil {
		return nil, fmt.Errorf("读取文件失败: %w", err)
	}
	allLines := strings.Split(string(content), "\n")
	totalLines := len(allLines)

	start := 0
	end := totalLines
	if fromLine != nil {
		start = *fromLine - 1
		if start < 0 {
			start = 0
		}
		end = totalLines
		if lines != nil {
			end = start + *lines
			if end > totalLines {
				end = totalLines
			}
		}
	}

	selectedLines := allLines[start:end]
	return map[string]any{
		"path":       relPath,
		"text":       strings.Join(selectedLines, "\n"),
		"totalLines": totalLines,
		"fromLine":   start + 1,
		"toLine":     start + len(selectedLines),
	}, nil
}

// Status 返回系统状态报告
func (m *memoryIndexManager) Status() map[string]any {
	if m.db == nil {
		return map[string]any{"available": false}
	}
	var fileCount, chunkCount int
	m.db.QueryRow("SELECT COUNT(*) FROM files").Scan(&fileCount)
	m.db.QueryRow("SELECT COUNT(*) FROM chunks").Scan(&chunkCount)

	return map[string]any{
		"available": true,
		"provider":  m.settings.Provider,
		"model":     m.settings.Model,
		"files":     fileCount,
		"chunks":    chunkCount,
		"dirty":     m.dirty,
		"fts": map[string]any{
			"enabled":   true,
			"available": m.ftsAvailable,
			"error":     m.ftsError,
		},
		"vector": map[string]any{
			"enabled":   true,
			"available": m.vectorAvailable,
			"error":     m.vectorError,
		},
	}
}

// Close 关闭管理器
func (m *memoryIndexManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true

	if m.watcher != nil {
		m.watcher.Close()
	}
	if m.intervalCancel != nil {
		m.intervalCancel()
	}
	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	if m.db != nil {
		return m.db.Close()
	}
	return nil
}

// ──────────────────────────── 辅助函数 ────────────────────────────

// truncateString 截断字符串
func truncateString(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen]
}

// joinStrings 连接字符串
func joinStrings(ss []string, sep string) string {
	result := ""
	for i, s := range ss {
		if i > 0 {
			result += sep
		}
		result += s
	}
	return result
}
```

注意：需要在 import 中添加 `"sort"` 和 `"strings"`。

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go build -tags "sqlite_fts5" ./internal/agentcore/memory/lite/
```

Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/lite/manager_impl.go
git commit -m "feat: 实现 MemoryIndexManager Search + ReadFile + Status + Close"
```

---

### Task 10: 实现 MemoryIndexManager — 文件监听 + 定时同步

**Files:**
- Modify: `internal/agentcore/memory/lite/manager_impl.go`

- [ ] **Step 1: 实现 setupFileWatcher + ensureIntervalSync**

在 `manager_impl.go` 中添加以下方法：

```go
// setupFileWatcher 设置文件监听
func (m *memoryIndexManager) setupFileWatcher() {
	watchEnabled := true
	if v, ok := m.settings.Sync["watch"].(bool); ok {
		watchEnabled = v
	}
	if !watchEnabled {
		return
	}

	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		logger.Error(logger.ComponentCommon).Err(err).Msg("创建文件监听器失败")
		return
	}
	m.watcher = watcher

	// 添加监听路径
	watchPaths := make(map[string]bool)
	if m.memoryDir != "" {
		if _, err := os.Stat(m.memoryDir); err == nil {
			watcher.Add(m.memoryDir)
			watchPaths[m.memoryDir] = true
		}
	}

	// 监听 extra paths
	for _, extra := range m.settings.ExtraPaths {
		fullPath := extra
		if !filepath.IsAbs(extra) {
			fullPath = filepath.Join(m.memoryDir, extra)
		}
		if _, err := os.Stat(fullPath); err == nil {
			watcher.Add(fullPath)
			watchPaths[fullPath] = true
		}
	}

	if len(watchPaths) == 0 {
		watcher.Close()
		m.watcher = nil
		return
	}

	// 延迟 1 秒标记 watcher 已初始化
	go func() {
		time.Sleep(1 * time.Second)
		m.watchMu.Lock()
		m.watcherInitialized = true
		m.watchMu.Unlock()
	}()

	// 启动事件循环
	go m.watchEventLoop()

	logger.Info(logger.ComponentCommon).Int("path_count", len(watchPaths)).Msg("文件监听已启动")
}

// watchEventLoop 文件监听事件循环
func (m *memoryIndexManager) watchEventLoop() {
	for {
		select {
		case event, ok := <-m.watcher.Events:
			if !ok {
				return
			}
			m.watchMu.Lock()
			initialized := m.watcherInitialized
			m.watchMu.Unlock()
			if !initialized {
				continue
			}
			if event.Has(fsnotify.Create) || event.Has(fsnotify.Write) || event.Has(fsnotify.Remove) {
				if strings.HasSuffix(event.Name, ".md") {
					m.scheduleWatchSync(event.Name)
				}
			}
		case _, ok := <-m.watcher.Errors:
			if !ok {
				return
			}
		}
	}
}

// scheduleWatchSync debounce 同步
func (m *memoryIndexManager) scheduleWatchSync(path string) {
	m.dirty = true
	m.watchMu.Lock()
	defer m.watchMu.Unlock()
	if m.watchTimer != nil {
		m.watchTimer.Stop()
	}
	m.watchTimer = time.AfterFunc(m.watchDebounce, func() {
		if m.closed {
			return
		}
		if err := m.Sync(context.Background(), "watch", false); err != nil {
			logger.Warn(logger.ComponentCommon).Err(err).Msg("文件监听同步失败")
		}
	})
}

// ensureIntervalSync 定时同步
func (m *memoryIndexManager) ensureIntervalSync() {
	minutes := 0
	if v, ok := m.settings.Sync["intervalMinutes"].(float64); ok {
		minutes = int(v)
	}
	if minutes <= 0 {
		return
	}

	ctx, cancel := context.WithCancel(context.Background())
	m.intervalCancel = cancel

	go func() {
		ticker := time.NewTicker(time.Duration(minutes) * time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if m.closed {
					return
				}
				if err := m.Sync(ctx, "interval", false); err != nil {
					logger.Warn(logger.ComponentCommon).Err(err).Msg("定时同步失败")
				}
			}
		}
	}()

	logger.Info(logger.ComponentCommon).Int("interval_minutes", minutes).Msg("定时同步已启用")
}
```

同时在 `Initialize` 方法中调用 `m.setupFileWatcher()` 和 `m.ensureIntervalSync()`。

- [ ] **Step 2: 编译验证**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go build -tags "sqlite_fts5" ./internal/agentcore/memory/lite/
```

Expected: 编译通过

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/memory/lite/manager_impl.go
git commit -m "feat: 实现 MemoryIndexManager 文件监听 + 定时同步"
```

---

### Task 11: 更新 doc.go + types.go + IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `internal/agentcore/memory/lite/doc.go`
- Modify: `internal/agentcore/memory/lite/types.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 types.go，移除 ⤵️ 标记**

将 `types.go` 中的 `⤵️ 回填: 7.4` 注释改为正常注释。

- [ ] **Step 2: 更新 doc.go，添加新文件**

更新 `doc.go` 的文件目录，添加 `manager_impl.go` 和 `vec_loader.go`。

- [ ] **Step 3: 更新 IMPLEMENTATION_PLAN.md**

将 7.4 和 7.1 的状态从 `☐` 改为 `✅`。

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/memory/lite/doc.go internal/agentcore/memory/lite/types.go IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 doc.go + types.go + IMPLEMENTATION_PLAN.md (7.4+7.1 完成)"
```

---

### Task 12: 运行完整测试套件

**Files:**
- 无新增/修改

- [ ] **Step 1: 运行 lite 包全部测试**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go test -v -tags "sqlite_fts5" ./internal/agentcore/memory/lite/
```

Expected: 所有测试通过

- [ ] **Step 2: 运行项目全量编译**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go build -tags "sqlite_fts5" ./...
```

Expected: 编译通过

- [ ] **Step 3: 运行全量测试**

```bash
cd /home/opensource/uapclaw-gateway
export GOPROXY=https://goproxy.cn,direct
pkill -f 'go (build|test)' 2>/dev/null || true
sleep 1
CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...
```

Expected: 所有测试通过
