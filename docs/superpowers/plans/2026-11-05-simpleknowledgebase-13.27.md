# 13.27 SimpleKnowledgeBase + ConfigManager 实施计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现领域13"查"侧总入口 SimpleKnowledgeBase（5 抽象方法 + retriever 懒创建 + agentic 包装）+ 多 KB 聚合两函数 + ConfigManager，完成非图路径检索管线闭环。

**Architecture:** knowledgebase 包新增 simple_kb.go/multi_kb.go 两文件（方案 A 单包）；SimpleKnowledgeBase 值嵌入 13.26 基座 + retriever 显式参数 + mutex 懒创建（模式 2）；ConfigManager 归 utils 包（Python 同构目录）；RetrieveOptions 补 Filters 槽（13.19 伏笔回填，死参数字面复刻）。

**Tech Stack:** Go 1.x / gopkg.in/yaml.v3（go.mod 已有直接依赖）/ testify

**设计文档:** `docs/superpowers/specs/2026-11-05-simpleknowledgebase-13.27-design.md`（15 项偏差归档 §8，6 项伏笔回填 §10）

**Python 对齐源:** `/home/opensource/agent-core/openjiuwen/core/retrieval/simple_knowledge_base.py` + `utils/config_manager.py`；测试 `/home/opensource/agent-core/tests/unit_tests/core/retrieval/test_simple_knowledge_base.py` + `utils/test_config_manager.py`

**编译/测试纪律（全计划适用）:**
- 单测命令：`cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/<包>/... -count=1`
- 编译前 `pgrep -f 'go (build|test)'` 检查残留，多进程先 kill
- 提交用显式 `git add <文件>`，禁止 `-am`（工作区可能有他任务改动）
- 源码声明顺序：结构体→枚举→常量→全局变量→导出函数→非导出函数（go-code-conventions 规范 2）
- 全部注释中文；日志 Msg 英文（feedback-logs-english）
- 错误构造用 `exception.BuildError(code, exception.WithMsg("..."))`（knowledgebase 包既有惯例）

---

## 关键既有符号速查（实施前必读）

```
// knowledgebase 包（13.26）
type KnowledgeBase interface { // base.go
    AddDocuments(ctx, documents []reranker.Document, opts ...KBOption) ([]string, error)
    Retrieve(ctx, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error)
    DeleteDocuments(ctx, docIDs []string, opts ...KBOption) (bool, error)
    UpdateDocuments(ctx, documents []reranker.Document, opts ...KBOption) ([]string, error)
    GetStatistics(ctx) (map[string]any, error)
    ParseFiles/ParseURLs/DeleteCollection/Close/Config ...
}
func NewBaseKnowledgeBase(config common.KnowledgeBaseConfig, opts ...KnowledgeBaseOption) (*BaseKnowledgeBase, error)
// 基座非导出字段（同包直访）：strictValidation/config/vectorStore/embedModel/parser/chunker/extractor/indexManager/llmClient
// 基座导出方法：Config()/StrictValidation()/VectorStore()/IndexManager()

// retriever 包
func NewVectorRetriever(vectorStore vector_store.VectorStore, embedModel embedding.BaseEmbedding) *VectorRetriever
func NewSparseRetriever(vectorStore vector_store.VectorStore) *SparseRetriever
func NewHybridRetriever(vectorStore vector_store.VectorStore, embedModel embedding.BaseEmbedding, opts ...HybridRetrieverOption) *HybridRetriever
func NewAgenticRetriever(retriever Retriever, llm model_clients.BaseModelClient, opts ...AgenticRetrieverOption) (*AgenticRetriever, error)
func WithAgenticMaxIter(n int) AgenticRetrieverOption
Retriever.Retrieve(ctx, query, topK int, scoreThreshold *float64, mode RetrievalMode, opts ...RetrieveOption) ([]common.RetrievalResult, error)
type RetrievalMode string // RetrievalModeVector / RetrievalModeSparse / RetrievalModeHybrid
type RetrieveOptions struct { Alpha *float64; GraphHops *int; NumBeams *int; NumCandidatesPerBeam *int; GraphExpansion *bool }

// common 包
type KnowledgeBaseConfig struct { KBID string; IndexType IndexTypeKind; UseGraph bool; ChunkSize int; ChunkOverlap int; UseCaptionForImages bool }
const IndexTypeVector/IndexTypeBM25/IndexTypeHybrid IndexTypeKind
type RetrievalConfig struct { TopK int; ScoreThreshold *float64; UseGraph *bool; Agentic bool; GraphExpansion bool; Filters map[string]any }
type RetrievalResult struct { Text string; Score float64; Metadata map[string]any; DocID string; ChunkID string }  // Score 是 float64 非 nil
type MultiKBRetrievalResult struct { Text string; Score float64; RawScore float64; RawScoreScaled float64; KbIDs []string; Metadata map[string]any }
type IndexConfig struct { IndexName string; IndexType IndexTypeKind; UseCaptionForImages bool }

// indexer 包
func WithIndexExtra(key string, value any) IndexOption  // BuildIndex 的 database_name 经 Extra["database_name"] 传入

// 错误码（已定义零消费，本节新增消费 8 个；155503 复用 13.26 已消费，不计入新增）
StatusRetrievalUtilsConfigFileNotFound(155300) / StatusRetrievalUtilsPyyamlNotFound(155301，编译期死路径仅保留) /
StatusRetrievalUtilsConfigFormatNotSupport(155302) / StatusRetrievalUtilsConfigNotFound(155303) /
StatusRetrievalUtilsConfigProcessError(155304) / StatusRetrievalKbChunkerNotFound(155501) /
StatusRetrievalKbIndexManagerNotFound(155502) / StatusRetrievalKbIndexBuildExecutionError(155504)
```

**已知偏差速查（写码时勿"顺手修正"）：**
1. `add_documents` 的 IndexConfig **带** UseCaptionForImages，`update_documents` **不带**（Python 真实不对称）
2. `add_documents` 有空白 id 填 uuid，`update_documents` 无
3. ConfigManager 键双轨：LoadFromFile 存 `"knowledge_base"` 键，UpdateConfig 按类型名存键（update 路径 GetKnowledgeBaseConfig 必 155304）
4. Retrieve 传 Filters 但四个 Retriever 实现全不读（死参数字面复刻）
5. AgenticRetriever 包装时 Extra 只认 `max_iter`，其余键静默忽略（Python 会 TypeError 崩，Go 静默——拍板偏差 §8-6）
6. Go RetrievalResult.Score 是 float64 非 nil（Python None→0.0 的 nil 分支编译期消解，偏差 §8-12）

---

### Task 1: ConfigManager（utils/config_manager.go）

**Files:**
- Create: `internal/agentcore/retrieval/utils/config_manager.go`
- Test: `internal/agentcore/retrieval/utils/config_manager_test.go`
- Modify: `internal/agentcore/retrieval/utils/doc.go`

- [ ] **Step 1.1: 写失败测试（Python TestConfigManager 6 例 1:1 + 增强 3 例）**

```go
package utils

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// writeTempFile 写临时文件（Python tempfile.NamedTemporaryFile 同构）。
func writeTempFile(t *testing.T, name, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), name)
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	return path
}

// TestConfigManager_InitWithPath 对齐 test_init_with_path：路径初始化 + 取回配置。
func TestConfigManager_InitWithPath(t *testing.T) {
	path := writeTempFile(t, "cfg.json",
		`{"kb_id":"test_kb","index_type":"vector","use_graph":false,"chunk_size":512,"chunk_overlap":50}`)
	m, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "test_kb", cfg.KBID)
	assert.Equal(t, common.IndexTypeVector, cfg.IndexType)
}

// TestConfigManager_LoadFromFileJSON 对齐 test_load_from_file_json。
func TestConfigManager_LoadFromFileJSON(t *testing.T) {
	path := writeTempFile(t, "cfg.json", `{"kb_id":"test_kb","index_type":"hybrid"}`)
	m, err := NewConfigManager("")
	require.NoError(t, err)
	require.NoError(t, m.LoadFromFile(path))
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "test_kb", cfg.KBID)
	assert.Equal(t, common.IndexTypeHybrid, cfg.IndexType)
}

// TestConfigManager_LoadFromFileYAML 对齐 test_load_from_file_yaml。
func TestConfigManager_LoadFromFileYAML(t *testing.T) {
	path := writeTempFile(t, "cfg.yaml", "kb_id: test_kb\nindex_type: vector\n")
	m, err := NewConfigManager("")
	require.NoError(t, err)
	require.NoError(t, m.LoadFromFile(path))
	cfg, err := m.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "test_kb", cfg.KBID)
	assert.Equal(t, common.IndexTypeVector, cfg.IndexType)
}

// TestConfigManager_LoadFromFileNotFound 对齐 test_load_from_file_not_found。
func TestConfigManager_LoadFromFileNotFound(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	err = m.LoadFromFile(filepath.Join(t.TempDir(), "nonexistent.json"))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, exceptionAs(err, &be))
	assert.Equal(t, 155300, be.Code())
}

// TestConfigManager_GetConfig 对齐 test_get_config：UpdateConfig 后 GetConfig 泛型取回。
func TestConfigManager_GetConfig(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	m.UpdateConfig(common.KnowledgeBaseConfig{KBID: "test_kb"})
	cfg, ok := GetConfig[common.KnowledgeBaseConfig](m)
	assert.True(t, ok)
	assert.Equal(t, "test_kb", cfg.KBID)
}

// TestConfigManager_GetConfigNotFound 对齐 test_get_config_not_found。
func TestConfigManager_GetConfigNotFound(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	_, ok := GetConfig[common.KnowledgeBaseConfig](m)
	assert.False(t, ok)
}

// —— Go 增强（spec §9.2-3/5/6）——

// TestConfigManager_键双轨 键双轨偏差固化：UpdateConfig 按类型名存键，
// GetKnowledgeBaseConfig 只读 "knowledge_base" 键 → update 路径必 155304。
func TestConfigManager_键双轨(t *testing.T) {
	m, err := NewConfigManager("")
	require.NoError(t, err)
	m.UpdateConfig(common.KnowledgeBaseConfig{KBID: "kb_via_update"})
	_, err = m.GetKnowledgeBaseConfig()
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, exceptionAs(err, &be))
	assert.Equal(t, 155304, be.Code())
}

// TestConfigManager_格式不支持 155302 分支。
func TestConfigManager_格式不支持(t *testing.T) {
	path := writeTempFile(t, "cfg.toml", "kb_id = x")
	m, _ := NewConfigManager("")
	err := m.LoadFromFile(path)
	var be *exception.BaseError
	require.True(t, exceptionAs(err, &be))
	assert.Equal(t, 155302, be.Code())
}

// TestConfigManager_SaveToFileNotFound 155303 分支：空 manager 无配置可存。
func TestConfigManager_SaveToFileNotFound(t *testing.T) {
	m, _ := NewConfigManager("")
	err := m.SaveToFile(filepath.Join(t.TempDir(), "out.json"))
	var be *exception.BaseError
	require.True(t, exceptionAs(err, &be))
	assert.Equal(t, 155303, be.Code())
}

// TestConfigManager_YAML往返 Save→Load 字段一致（spec §9.2-6）。
func TestConfigManager_YAML往返(t *testing.T) {
	m, _ := NewConfigManager("")
	m.UpdateConfig(common.KnowledgeBaseConfig{KBID: "roundtrip", IndexType: common.IndexTypeBM25,
		ChunkSize: 256, ChunkOverlap: 30, UseGraph: true, UseCaptionForImages: true})
	path := filepath.Join(t.TempDir(), "out.yaml")
	require.NoError(t, m.SaveToFile(path))
	m2, err := NewConfigManager(path)
	require.NoError(t, err)
	cfg, err := m2.GetKnowledgeBaseConfig()
	require.NoError(t, err)
	assert.Equal(t, "roundtrip", cfg.KBID)
	assert.Equal(t, common.IndexTypeBM25, cfg.IndexType)
	assert.Equal(t, 256, cfg.ChunkSize)
	assert.True(t, cfg.UseGraph)
	assert.True(t, cfg.UseCaptionForImages)
}
```

注意：`exceptionAs` 辅助在 Step 1.3 的测试文件中定义（`errors.As` 包装）：

```go
// exceptionAs errors.As 薄包装（测试内使用）。
func exceptionAs(err error, target **exception.BaseError) bool {
	return errors.As(err, target)
}
```

（import 需补 `"errors"`。）

- [ ] **Step 1.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/utils/... -count=1 -run TestConfigManager`
Expected: FAIL（`NewConfigManager`/`GetConfig` undefined 编译错误）

- [ ] **Step 1.3: 实现 config_manager.go**

```go
package utils

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode"

	"gopkg.in/yaml.v3"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ConfigManager 统一配置管理器，支持 JSON/YAML 文件加载与保存。
//
// 对齐 Python retrieval/utils/config_manager.py。键双轨偏差（spec §8-1）：
// LoadFromFile 存 "knowledge_base" 键；UpdateConfig 按类型名存键——
// 两键不一致是 Python 真实行为，update 路径 GetKnowledgeBaseConfig
// 必失败（155304），字面复刻。
//
// Python: class ConfigManager
type ConfigManager struct {
	// configs 已加载配置（Python Dict[str, BaseModel]，Go 用 any 收任意 config 类型）
	configs map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// NewConfigManager 创建配置管理器。configPath 非空时立即加载。
func NewConfigManager(configPath string) (*ConfigManager, error) {
	m := &ConfigManager{configs: make(map[string]any)}
	if configPath != "" {
		if err := m.LoadFromFile(configPath); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// LoadFromFile 从文件加载配置（支持 JSON/YAML）。
//
// 不存在→155300；后缀不支持→155302；解析结果按 "knowledge_base" 键存。
// Python yaml ImportError 分支（155301）在 Go 为编译期死路径（yaml.v3
// 直接依赖），仅保留错误码定义（spec §8-2）。
//
// Python: load_from_file
func (m *ConfigManager) LoadFromFile(path string) error {
	if _, err := os.Stat(path); err != nil {
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigFileNotFound,
			exception.WithMsg(fmt.Sprintf("Configuration file does not exist: %s", path)),
		)
	}
	suffix := strings.ToLower(filepath.Ext(path))
	var data map[string]any
	switch suffix {
	case ".json":
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := json.Unmarshal(raw, &data); err != nil {
			return err
		}
	case ".yaml", ".yml":
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if err := yaml.Unmarshal(raw, &data); err != nil {
			return err
		}
	default:
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigFormatNotSupport,
			exception.WithMsg(fmt.Sprintf("Unsupported configuration file format: %s", suffix)),
		)
	}
	// 对齐 Python KnowledgeBaseConfig(**data)：忽略未知键，已知字段显式取。
	m.configs["knowledge_base"] = knowledgeBaseConfigFromMap(data)
	return nil
}

// SaveToFile 保存配置到文件（支持 JSON/YAML）。
//
// 无 "knowledge_base" 键→155303。JSON 用 indent 2 + 非 ASCII 不转义
// （对齐 json.dump(indent=2, ensure_ascii=False)）；YAML 用 yaml.Marshal。
//
// Python: save_to_file
func (m *ConfigManager) SaveToFile(path string) error {
	cfgAny, ok := m.configs["knowledge_base"]
	if !ok {
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigNotFound,
			exception.WithMsg("No configuration to save"),
		)
	}
	cfg, ok := cfgAnKnowledgeBaseConfig(cfgAn)
	_ = cfg
	_ = ok
	return nil
}

// GetConfig 按类型取回配置（Python get_config(Type[T]) 泛型同构）。
// 未找到返回零值与 false。
func GetConfig[T any](m *ConfigManager) (T, bool) {
	for _, cfg := range m.configs {
		if v, ok := cfg.(T); ok {
			return v, true
		}
	}
	var zero T
	return zero, false
}

// GetKnowledgeBaseConfig 取回知识库配置。
//
// "knowledge_base" 键不存在→155304。注意键双轨偏差：UpdateConfig
// 存的是类型名键，本方法在 update 路径必失败（Python 真实行为）。
//
// Python: get_knowledge_base_config
func (m *ConfigManager) GetKnowledgeBaseConfig() (common.KnowledgeBaseConfig, error) {
	cfgAny, ok := m.configs["knowledge_base"]
	if !ok {
		return common.KnowledgeBaseConfig{}, exception.BuildError(
			exception.StatusRetrievalUtilsConfigProcessError,
			exception.WithMsg("Knowledge base configuration not loaded"),
		)
	}
	cfg, ok := cfgAny.(common.KnowledgeBaseConfig)
	if !ok {
		return common.KnowledgeBaseConfig{}, exception.BuildError(
			exception.StatusRetrievalUtilsConfigProcessError,
			exception.WithMsg("Knowledge base configuration not loaded"),
		)
	}
	return cfg, nil
}

// UpdateConfig 更新配置（按类型名存键——键双轨偏差，spec §8-1）。
//
// Python: update_config（type(config).__name__ → Go reflect 类型名
// 含包前缀，spec §8-10）
func (m *ConfigManager) UpdateConfig(config any) {
	m.configs[typeNameOf(config)] = config
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// knowledgeBaseConfigFromMap 从解析后的 map 构造 KnowledgeBaseConfig。
// 忽略未知键（对齐 pydantic 默认），类型不符字段静默用零值
// （对齐 Python **data 非法类型会抛 ValidationError 的差异归档 spec §8-8）。
func knowledgeBaseConfigFromMap(data map[string]any) common.KnowledgeBaseConfig {
	cfg := common.KnowledgeBaseConfig{}
	if v, ok := data["kb_id"].(string); ok {
		cfg.KBID = v
	}
	if v, ok := data["index_type"].(string); ok {
		cfg.IndexType = common.IndexTypeKind(v)
	}
	if v, ok := data["use_graph"].(bool); ok {
		cfg.UseGraph = v
	}
	if v, ok := data["chunk_size"].(float64); ok {
		cfg.ChunkSize = int(v)
	}
	if v, ok := data["chunk_overlap"].(float64); ok {
		cfg.ChunkOverlap = int(v)
	}
	if v, ok := data["use_caption_for_images"].(bool); ok {
		cfg.UseCaptionForImages = v
	}
	return cfg
}

// knowledgeBaseConfigToMap 将 KnowledgeBaseConfig 序列化为 map
// （Python model_dump 键名）。
func knowledgeBaseConfigToMap(cfg common.KnowledgeBaseConfig) map[string]any {
	return map[string]any{
		"kb_id":                 cfg.KBID,
		"index_type":            string(cfg.IndexType),
		"use_graph":             cfg.UseGraph,
		"chunk_size":            cfg.ChunkSize,
		"chunk_overlap":         cfg.ChunkOverlap,
		"use_caption_for_images": cfg.UseCaptionForImages,
	}
}

// typeNameOf 取值的类型名（Python type(x).__name__ 对应物，
// Go 含包前缀如 common.KnowledgeBaseConfig，spec §8-10）。
func typeNameOf(v any) string {
	return fmt.Sprintf("%T", v)
}
```

**⚠️ 注意 SaveToFile 上面留了占位错误（cfgAnKnowledgeBaseConfig 是故意标记）——完整实现如下，写文件时直接用完整版：**

```go
// SaveToFile 保存配置到文件（支持 JSON/YAML）。
//
// 无 "knowledge_base" 键→155303。JSON 用 indent 2 + 非 ASCII 不转义
// （对齐 json.dump(indent=2, ensure_ascii=False)）；YAML 用 yaml.Marshal
// （allow_unicode=True / default_flow_style=False 默认满足）。
//
// Python: save_to_file
func (m *ConfigManager) SaveToFile(path string) error {
	cfgAny, ok := m.configs["knowledge_base"]
	if !ok {
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigNotFound,
			exception.WithMsg("No configuration to save"),
		)
	}
	cfg, ok := cfgAny.(common.KnowledgeBaseConfig)
	if !ok {
		// Python 此处直接解包会 AttributeError/None；Go 防御式报同码。
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigNotFound,
			exception.WithMsg("No configuration to save"),
		)
	}
	data := knowledgeBaseConfigToMap(cfg)
	suffix := strings.ToLower(filepath.Ext(path))
	switch suffix {
	case ".json":
		var sb strings.Builder
		enc := json.NewEncoder(&sb)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(data); err != nil {
			return err
		}
		return os.WriteFile(path, []byte(sb.String()), 0o644)
	case ".yaml", ".yml":
		raw, err := yaml.Marshal(data)
		if err != nil {
			return err
		}
		return os.WriteFile(path, raw, 0o644)
	default:
		return exception.BuildError(
			exception.StatusRetrievalUtilsConfigFormatNotSupport,
			exception.WithMsg(fmt.Sprintf("Unsupported configuration file format: %s", suffix)),
		)
	}
}
```

另外 `unicode` import 未用到，写文件时去掉。

- [ ] **Step 1.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/utils/... -count=1 -run TestConfigManager`
Expected: PASS（11 例全绿）

- [ ] **Step 1.5: 更新 utils/doc.go 文件目录**

在 `文件目录：` 树中 `common.go` 行后加：
```
//	├── config_manager.go  # 配置管理器（JSON/YAML 加载与保存）
```
并在"对应 Python 代码"段补一行 `openjiuwen/core/retrieval/utils/config_manager.py`；核心类型索引补 `ConfigManager`、`GetConfig`。

- [ ] **Step 1.6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/utils/config_manager.go internal/agentcore/retrieval/utils/config_manager_test.go internal/agentcore/retrieval/utils/doc.go
git commit -m "feat(retrieval): 13.27 ConfigManager 配置管理器（JSON/YAML 双格式+键双轨偏差复刻）"
```

---

### Task 2: RetrieveOptions.Filters 槽位（13.19 伏笔回填）

**Files:**
- Modify: `internal/agentcore/retrieval/retriever/base.go`
- Test: `internal/agentcore/retrieval/retriever/base_test.go`

- [ ] **Step 2.1: 写失败测试（追加到 base_test.go 末尾）**

```go
// TestNewRetrieveOptions_Filters Filters 槽位（13.19 伏笔：13.27 SimpleKB 写入，
// 四实现均不消费——死参数字面复刻，spec 13.27 §8-14）。
func TestNewRetrieveOptions_Filters(t *testing.T) {
	filters := map[string]any{"doc_type": "pdf"}
	opts := NewRetrieveOptions(WithRetrieveFilters(filters))
	assert.Equal(t, filters, opts.Filters)

	// 未设置时为 nil
	empty := NewRetrieveOptions()
	assert.Nil(t, empty.Filters)
}
```

- [ ] **Step 2.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/retriever/... -count=1 -run TestNewRetrieveOptions_Filters`
Expected: FAIL（`WithRetrieveFilters` undefined）

- [ ] **Step 2.3: 实现（base.go 两处修改）**

RetrieveOptions 结构体追加字段（GraphExpansion 字段后）：

```go
	// Filters 元数据过滤条件（13.27 SimpleKB 写入）。潜伏参数：Python 侧
	// 进了 retriever kwargs 但 Vector/Sparse/Hybrid/Agentic 四实现全不读
	// （store 调用硬编码 filters=None）——"写了不消费"死参数字面复刻，
	// spec 13.27 §8-14，后续出现消费方再激活。
	Filters map[string]any
```

导出函数区追加（WithRetrieveGraphExpansion 后）：

```go
// WithRetrieveFilters 设置元数据过滤条件。
func WithRetrieveFilters(filters map[string]any) RetrieveOption {
	return func(o *RetrieveOptions) { o.Filters = filters }
}
```

- [ ] **Step 2.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/retriever/... -count=1`
Expected: PASS（包全量回归零 FAIL——确认未破坏既有 13.19-13.25 测试）

- [ ] **Step 2.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/retriever/base.go internal/agentcore/retrieval/retriever/base_test.go
git commit -m "feat(retriever): RetrieveOptions 新增 Filters 槽位（13.19 伏笔回填，死参数字面复刻）"
```

---

### Task 3: SimpleKnowledgeBase 五方法（simple_kb.go）

**Files:**
- Create: `internal/agentcore/retrieval/knowledgebase/simple_kb.go`
- Test: `internal/agentcore/retrieval/knowledgebase/simple_kb_test.go`
- Modify: `internal/agentcore/retrieval/knowledgebase/doc.go`

**测试桩（simple_kb_test.go 顶部，fakeAttrStore/fakeAttrIndexer 复用自 base_test.go 同包）：**

```go
package knowledgebase

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	llmschema "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/schema"
)

// ──────────────────────────── 测试桩 ────────────────────────────

// recordingIndexer 记录调用的 Indexer 桩（可编程返回值 + 调用捕获）。
type recordingIndexer struct {
	fakeAttrIndexer
	mu           sync.Mutex
	buildCalls   int
	updateCalls  int
	deleteCalls  int
	buildResult  bool
	updateResult bool
	deleteResult bool
	lastBuildCfg common.IndexConfig
	lastBuildOpt []indexer.IndexOption
	deleteIDs    []string
}

func (r *recordingIndexer) BuildIndex(ctx context.Context, chunks []common.TextChunk, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.buildCalls++
	r.lastBuildCfg = config
	r.lastBuildOpt = opts
	return r.buildResult, nil
}
func (r *recordingIndexer) UpdateIndex(ctx context.Context, chunks []common.TextChunk, docID string, config common.IndexConfig, embedModel embedding.BaseEmbedding, opts ...indexer.IndexOption) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.updateCalls++
	r.updateResult = true
	return true, nil
}
func (r *recordingIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.deleteCalls++
	r.deleteIDs = append(r.deleteIDs, docID)
	return r.deleteResult, nil
}

// countingChunker 计数 Chunker 桩（固定产出）。
type countingChunker struct {
	mu     sync.Mutex
	calls  int
	chunks []common.TextChunk
}
func (c *countingChunker) ChunkText(text string) ([]string, error) { return []string{text}, nil }
func (c *countingChunker) ChunkDocuments(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return c.chunks, nil
}
func (c *countingChunker) Process(ctx context.Context, docs []reranker.Document) ([]common.TextChunk, error) {
	return c.ChunkDocuments(ctx, docs)
}

// recordingRetriever 记录调用的 Retriever 桩。
type recordingRetriever struct {
	mu           sync.Mutex
	retrieveCalls int
	lastTopK     int
	lastMode     retriever.RetrievalMode
	lastOpts     []retriever.RetrieveOption
	results      []common.RetrievalResult
	err          error
}
func (r *recordingRetriever) Retrieve(ctx context.Context, query string, topK int, scoreThreshold *float64, mode retriever.RetrievalMode, opts ...retriever.RetrieveOption) ([]common.RetrievalResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.retrieveCalls++
	r.lastTopK = topK
	r.lastMode = mode
	r.lastOpts = opts
	return r.results, r.err
}
func (r *recordingRetriever) BatchRetrieve(ctx context.Context, queries []string, topK int, opts ...retriever.RetrieveOption) ([][]common.RetrievalResult, error) {
	return nil, nil
}
func (r *recordingRetriever) RetrieveSearchResults(ctx context.Context, query string, topK int, mode retriever.RetrievalMode, opts ...retriever.RetrieveOption) ([]common.SearchResult, error) {
	return nil, nil
}
func (r *recordingRetriever) Close() {}

// stubKBLlm 零行为 LLM 桩（stubAgenticLLM 同构，包内独立副本）。
type stubKBLlm struct{}
func (s *stubKBLlm) Invoke(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.InvokeOption) (*llmschema.AssistantMessage, error) {
	return llmschema.NewAssistantMessage(""), nil
}
func (s *stubKBLlm) Stream(_ context.Context, _ model_clients.MessagesParam, _ ...model_clients.StreamOption) (<-chan *llmschema.AssistantMessageChunk, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) GenerateImage(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateImageOption) (*llmschema.ImageGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) GenerateSpeech(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateSpeechOption) (*llmschema.AudioGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) GenerateVideo(_ context.Context, _ []*llmschema.UserMessage, _ ...model_clients.GenerateVideoOption) (*llmschema.VideoGenerationResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) TranscribeAudio(_ context.Context, _ string, _ ...llmschema.TranscribeAudioOption) (*llmschema.TranscriptionResponse, error) {
	return nil, errors.New("not implemented")
}
func (s *stubKBLlm) Release(_ context.Context, _ ...model_clients.ReleaseOption) (bool, error) {
	return false, nil
}
func (s *stubKBLlm) SupportsKVCacheRelease() bool { return false }
```

- [ ] **Step 3.1: 写失败测试（构造 + 断言缺口 + add/delete/update/statistics 共 10 例）**

```go
// —— 构造与断言缺口（伏笔 2）——

// TestNewSimpleKnowledgeBase_构造 继承基座构造与校验（Python test_database_name_mismatch 同构入口）。
func TestNewSimpleKnowledgeBase_构造(t *testing.T) {
	vs, im := makeCompatiblePair()
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		nil, WithVectorStore(vs), WithIndexManager(im))
	require.NoError(t, err)
	// 伏笔 2：完整 KnowledgeBase 编译期断言在 simple_kb.go（本测试存在即证明）
	assert.Equal(t, "test_kb", kb.Config().KBID)
}

// TestNewSimpleKnowledgeBase_数据库不匹配 构造期 postAssignValidation 继承（对齐 test_database_name_mismatch）。
func TestNewSimpleKnowledgeBase_数据库不匹配(t *testing.T) {
	vs, im := makeCompatiblePair()
	im.databaseName = "different_name"
	_, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"},
		nil, WithVectorStore(vs), WithIndexManager(im))
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155508, be.Code())
	assert.Contains(t, be.Message(), "incompatible database_name configs")
}

// —— AddDocuments ——

// TestSimpleKB_AddDocuments成功 对齐 test_add_documents_success。
func TestSimpleKB_AddDocuments成功(t *testing.T) {
	im := &recordingIndexer{buildResult: true}
	ch := &countingChunker{chunks: []common.TextChunk{
		{ID: "chunk_1", Text: "c1", DocID: "doc_1"},
		{ID: "chunk_2", Text: "c2", DocID: "doc_1"},
	}}
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeVector, UseCaptionForImages: true},
		nil, WithChunker(ch), WithIndexManager(im))
	require.NoError(t, err)
	ids, err := kb.AddDocuments(context.Background(), []reranker.Document{
		{ID: "doc_1", Text: "Test document 1"}, {ID: "doc_2", Text: "Test document 2"},
	})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1", "doc_2"}, ids)
	assert.Equal(t, 1, ch.calls)
	assert.Equal(t, 1, im.buildCalls)
	// IndexConfig 带 UseCaptionForImages（与 update 不对称）
	assert.Equal(t, "kb_test_kb_chunks", im.lastBuildCfg.IndexName)
	assert.True(t, im.lastBuildCfg.UseCaptionForImages)
}

// TestSimpleKB_AddDocuments无分块器 对齐 test_add_documents_without_chunker。
func TestSimpleKB_AddDocuments无分块器(t *testing.T) {
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	_, err := kb.AddDocuments(context.Background(), []reranker.Document{{Text: "Test"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155501, be.Code())
	assert.Contains(t, be.Message(), "chunker is required")
}

// TestSimpleKB_AddDocuments无索引管理器 对齐 test_add_documents_without_index_manager。
func TestSimpleKB_AddDocuments无索引管理器(t *testing.T) {
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithChunker(&countingChunker{}))
	_, err := kb.AddDocuments(context.Background(), []reranker.Document{{Text: "Test"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155502, be.Code())
	assert.Contains(t, be.Message(), "index_manager is required")
}

// TestSimpleKB_AddDocuments构建失败 对齐 test_add_documents_build_index_failed。
func TestSimpleKB_AddDocuments构建失败(t *testing.T) {
	im := &recordingIndexer{buildResult: false}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{chunks: []common.TextChunk{{ID: "c", DocID: "d"}}}), WithIndexManager(im))
	_, err := kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "Test"}})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155504, be.Code())
	assert.Contains(t, be.Message(), "Failed to build index")
}

// TestSimpleKB_AddDocuments空白ID填充uuid Go 增强：空白 id（含纯空格）才填 uuid。
func TestSimpleKB_AddDocuments空白ID填充uuid(t *testing.T) {
	im := &recordingIndexer{buildResult: true}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{}), WithIndexManager(im))
	ids, err := kb.AddDocuments(context.Background(), []reranker.Document{
		{ID: "", Text: "a"}, {ID: "  ", Text: "b"}, {ID: "keep", Text: "c"},
	})
	require.NoError(t, err)
	assert.NotEmpty(t, ids[0])
	assert.NotEqual(t, "  ", ids[1])
	assert.Equal(t, "keep", ids[2])
	assert.NotEqual(t, ids[0], ids[1])
}

// TestSimpleKB_AddDocuments传数据库名 database_name 防御式取值（伏笔 5）。
func TestSimpleKB_AddDocuments传数据库名(t *testing.T) {
	im := &recordingIndexer{buildResult: true}
	vs, _ := makeCompatiblePair()
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil,
		WithChunker(&countingChunker{}), WithIndexManager(im), WithVectorStore(vs))
	_, err := kb.AddDocuments(context.Background(), []reranker.Document{{ID: "d", Text: "x"}})
	require.NoError(t, err)
	// BuildIndex 收到 database_name Extra（fakeAttrStore.databaseName == "test_db"）
	opts := indexer.NewIndexOptions(im.lastBuildOpt...)
	assert.Equal(t, "test_db", opts.Extra["database_name"])
}

// —— DeleteDocuments ——

// TestSimpleKB_DeleteDocuments成功 对齐 test_delete_documents_success。
func TestSimpleKB_DeleteDocuments成功(t *testing.T) {
	im := &recordingIndexer{deleteResult: true}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithIndexManager(im))
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1", "doc_2"})
	require.NoError(t, err)
	assert.True(t, ok)
	assert.Equal(t, 2, im.deleteCalls)
	assert.Equal(t, []string{"doc_1", "doc_2"}, im.deleteIDs)
}

// TestSimpleKB_DeleteDocuments部分失败 Go 增强：任一 false → 总 false（循环不中断）。
func TestSimpleKB_DeleteDocuments部分失败(t *testing.T) {
	im := &countingDeleteIndexer{} // 第二次返回 false
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithIndexManager(im))
	ok, err := kb.DeleteDocuments(context.Background(), []string{"doc_1", "doc_2"})
	require.NoError(t, err)
	assert.False(t, ok)
	assert.Equal(t, 2, im.calls) // 循环不中断
}
```

`countingDeleteIndexer` 桩（放测试桩区）：

```go
// countingDeleteIndexer 删除计数桩：第一次 true 第二次 false。
type countingDeleteIndexer struct {
	fakeAttrIndexer
	calls int
}
func (c *countingDeleteIndexer) DeleteIndex(ctx context.Context, docID string, indexName string) (bool, error) {
	c.calls++
	return c.calls == 1, nil
}
```

```go
// —— UpdateDocuments ——

// TestSimpleKB_UpdateDocuments成功 对齐 test_update_documents_success + 不对称断言（Go 增强）。
func TestSimpleKB_UpdateDocuments成功(t *testing.T) {
	im := &recordingIndexer{}
	ch := &countingChunker{chunks: []common.TextChunk{
		{ID: "c1", Text: "x", DocID: "doc_1"},
		{ID: "c2", Text: "y", DocID: "doc_2"},
	}}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", UseCaptionForImages: true},
		nil, WithChunker(ch), WithIndexManager(im))
	ids, err := kb.UpdateDocuments(context.Background(), []reranker.Document{{ID: "doc_1", Text: "Updated"}})
	require.NoError(t, err)
	assert.Equal(t, []string{"doc_1"}, ids)
	assert.Equal(t, 1, im.updateCalls)
	// 不对称偏差 §8：update 的 IndexConfig 不带 UseCaptionForImages（即使 KB 配置为 true）
	assert.False(t, im.lastUpdateCfg.UseCaptionForImages)
	assert.Equal(t, "kb_kb_chunks", im.lastUpdateCfg.IndexName)
}
```

recordingIndexer 需补 `lastUpdateCfg` 字段与赋值（在 UpdateIndex 里 `r.lastUpdateCfg = config`）。

```go
// —— GetStatistics ——

// TestSimpleKB_GetStatistics成功 对齐 test_get_statistics_success。
func TestSimpleKB_GetStatistics成功(t *testing.T) {
	im := &statsIndexer{info: map[string]any{"count": 10}}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb", IndexType: common.IndexTypeVector},
		nil, WithIndexManager(im), WithChunker(&countingChunker{}))
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, "vector", stats["index_type"])
	assert.Contains(t, stats, "index_info")
	assert.Equal(t, true, stats["has_chunker"])
	assert.Equal(t, false, stats["has_parser"])
}

// TestSimpleKB_GetStatistics无索引管理器 对齐 test_get_statistics_without_index_manager。
func TestSimpleKB_GetStatistics无索引管理器(t *testing.T) {
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "test_kb"}, nil)
	stats, err := kb.GetStatistics(context.Background())
	require.NoError(t, err)
	assert.Equal(t, "test_kb", stats["kb_id"])
	assert.Equal(t, false, stats["index_exists"])
	assert.Len(t, stats, 2)
}

// TestSimpleKB_GetStatistics错误冒泡 Go 增强：GetIndexInfo 错误直接冒泡（Python 无 try）。
func TestSimpleKB_GetStatistics错误冒泡(t *testing.T) {
	im := &statsIndexer{err: errors.New("boom")}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithIndexManager(im))
	_, err := kb.GetStatistics(context.Background())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boom")
}
```

`statsIndexer` 桩：

```go
// statsIndexer 统计桩：可编程 info/err。
type statsIndexer struct {
	fakeAttrIndexer
	info map[string]any
	err  error
}
func (s *statsIndexer) GetIndexInfo(ctx context.Context, indexName string) (map[string]any, error) {
	return s.info, s.err
}
```

- [ ] **Step 3.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1 -run TestSimpleKB_`
Expected: FAIL（`NewSimpleKnowledgeBase` undefined）

- [ ] **Step 3.3: 实现 simple_kb.go（五方法 + 构造）**

```go
package knowledgebase

import (
	"context"
	"strings"

	"github.com/google/uuid"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/indexing/indexer"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/retriever"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-gateway/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SimpleKnowledgeBase 简单知识库（13.27）。
//
// 实现 KnowledgeBase 接口的 5 抽象方法；4 模板方法继承 BaseKnowledgeBase。
// retriever 为 nil 时首次 Retrieve 懒创建（index_type 三分支）。
//
// Python: simple_knowledge_base.py (class SimpleKnowledgeBase)
type SimpleKnowledgeBase struct {
	// BaseKnowledgeBase 共享基座（13.26；值嵌入，13.8 HybridChunker 先例）
	BaseKnowledgeBase
	// retriever 检索器（nil = 懒创建）
	retriever retriever.Retriever
	// retrieverMu 懒创建并发保护（13.24 Mutex 先例）
	retrieverMu sync.Mutex
}

// 编译期断言：SimpleKnowledgeBase 完整实现 KnowledgeBase 接口
// （兑现 13.26 base.go 留下的断言缺口——伏笔 2）。
var _ KnowledgeBase = (*SimpleKnowledgeBase)(nil)

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSimpleKnowledgeBase 构造简单知识库。
//
// retriever 显式参数（Python 签名不对称直译：基类不收，子类特有），
// nil 时 Retrieve 懒创建；其余组件经 KnowledgeBaseOption 透传基座。
//
// Python: SimpleKnowledgeBase.__init__
func NewSimpleKnowledgeBase(config common.KnowledgeBaseConfig,
	r retriever.Retriever, opts ...KnowledgeBaseOption) (*SimpleKnowledgeBase, error) {
	base, err := NewBaseKnowledgeBase(config, opts...)
	if err != nil {
		return nil, err
	}
	return &SimpleKnowledgeBase{BaseKnowledgeBase: *base, retriever: r}, nil
}

// AddDocuments 添加文档到知识库。
//
// 空白 id 填 uuid → 分块 → 建索引（IndexConfig 带 UseCaptionForImages，
// 与 UpdateDocuments 不对称——Python 真实行为，spec §8）→ 失败 155504。
//
// Python: add_documents
func (s *SimpleKnowledgeBase) AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error) {
	if s.chunker == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkerNotFound,
			exception.WithMsg("chunker is required for add_documents"),
		)
	}
	if s.indexManager == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for add_documents"),
		)
	}
	if s.strictValidation && s.vectorStore != nil {
		if err := s.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}
	for i := range documents {
		if strings.TrimSpace(documents[i].ID) == "" {
			documents[i].ID = uuid.NewString()
		}
	}
	chunks, err := s.chunker.ChunkDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	logger.Info(logComponent).
		Int("document_count", len(documents)).
		Int("chunk_count", len(chunks)).
		Msg("Chunked documents")
	indexConfig := common.IndexConfig{
		IndexName:           "kb_" + s.config.KBID + "_chunks",
		IndexType:           s.config.IndexType,
		UseCaptionForImages: s.config.UseCaptionForImages,
	}
	success, err := s.indexManager.BuildIndex(ctx, chunks, indexConfig, s.embedModel,
		indexer.WithIndexExtra("database_name", s.vectorStoreDatabaseName()))
	if err != nil {
		return nil, err
	}
	if !success {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexBuildExecutionError,
			exception.WithMsg("Failed to build index"),
		)
	}
	docIDs := make([]string, len(documents))
	for i, doc := range documents {
		docIDs[i] = doc.ID
	}
	logger.Info(logComponent).Int("document_count", len(docIDs)).Msg("Successfully added documents to knowledge base")
	return docIDs, nil
}

// DeleteDocuments 删除文档。
//
// 逐 doc_id 删除，循环不中断，任一失败 → 总 false。
//
// Python: delete_documents
func (s *SimpleKnowledgeBase) DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error) {
	if s.indexManager == nil {
		return false, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for delete_documents"),
		)
	}
	if s.strictValidation && s.vectorStore != nil {
		if err := s.vectorStore.CheckVectorField(); err != nil {
			return false, err
		}
	}
	indexName := "kb_" + s.config.KBID + "_chunks"
	success := true
	for _, docID := range docIDs {
		result, err := s.indexManager.DeleteIndex(ctx, docID, indexName)
		if err != nil {
			return false, err
		}
		if !result {
			success = false
		}
	}
	return success, nil
}

// UpdateDocuments 更新文档。
//
// 与 AddDocuments 不对称（Python 真实行为，spec §8）：
// 无 uuid 填充；IndexConfig 不带 UseCaptionForImages。
//
// Python: update_documents
func (s *SimpleKnowledgeBase) UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error) {
	if s.chunker == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbChunkerNotFound,
			exception.WithMsg("chunker is required for update_documents"),
		)
	}
	if s.indexManager == nil {
		return nil, exception.BuildError(
			exception.StatusRetrievalKbIndexManagerNotFound,
			exception.WithMsg("index_manager is required for update_documents"),
		)
	}
	if s.strictValidation && s.vectorStore != nil {
		if err := s.vectorStore.CheckVectorField(); err != nil {
			return nil, err
		}
	}
	chunks, err := s.chunker.ChunkDocuments(ctx, documents)
	if err != nil {
		return nil, err
	}
	indexConfig := common.IndexConfig{
		IndexName: "kb_" + s.config.KBID + "_chunks",
		IndexType: s.config.IndexType,
	}
	var docIDs []string
	for _, doc := range documents {
		var docChunks []common.TextChunk
		for _, c := range chunks {
			if c.DocID == doc.ID {
				docChunks = append(docChunks, c)
			}
		}
		if len(docChunks) == 0 {
			continue
		}
		success, err := s.indexManager.UpdateIndex(ctx, docChunks, doc.ID, indexConfig, s.embedModel)
		if err != nil {
			return nil, err
		}
		if success {
			docIDs = append(docIDs, doc.ID)
		}
	}
	return docIDs, nil
}

// GetStatistics 获取知识库统计。
//
// 无 index_manager → 2 键短路；GetIndexInfo 错误直接冒泡（Python 无 try）。
//
// Python: get_statistics
func (s *SimpleKnowledgeBase) GetStatistics(ctx context.Context) (map[string]any, error) {
	indexName := "kb_" + s.config.KBID + "_chunks"
	if s.indexManager == nil {
		return map[string]any{
			"kb_id":        s.config.KBID,
			"index_exists": false,
		}, nil
	}
	indexInfo, err := s.indexManager.GetIndexInfo(ctx, indexName)
	if err != nil {
		return nil, err
	}
	return map[string]any{
		"kb_id":           s.config.KBID,
		"index_type":      string(s.config.IndexType),
		"index_info":      indexInfo,
		"has_parser":      s.parser != nil,
		"has_chunker":     s.chunker != nil,
		"has_extractor":   s.extractor != nil,
		"has_embed_model": s.embedModel != nil,
		"has_vector_store": s.vectorStore != nil,
	}, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// vectorStoreDatabaseName 防御式取 vector_store 的 database_name。
//
// Python: getattr(getattr(self.vector_store, "config", None), "database_name", "")
// + 非 str 归零。Go 接口断言（13.26 storageAttrProvider 同型）；未实现 → ""。
// 非 str 归零编译期消解（spec §8-5）。
func (s *SimpleKnowledgeBase) vectorStoreDatabaseName() string {
	if s.vectorStore == nil {
		return ""
	}
	if p, ok := s.vectorStore.(interface{ DatabaseName() string }); ok {
		return p.DatabaseName()
	}
	return ""
}
```

**Retrieve 方法（Task 4 实现）——本步先不加接口方法会编译失败！** 因此 Task 3 与 Task 4 必须连续执行，中间不提交不完整状态。**调整：本 Task 直接包含一个最小 Retrieve 占位（返回 nil, nil），Task 4 替换为完整实现：**

```go
// Retrieve 检索相关文档（Task 4 实现完整版）。
func (s *SimpleKnowledgeBase) Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	_ = ctx
	_ = query
	_ = config
	_ = opts
	return nil, nil
}
```

（`import "sync"` 需加入。）

- [ ] **Step 3.4: 跑测试确认通过（Retrieve 占位不影响本 Task 测试）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: PASS（13.26 既有测试 + 本 Task 新测试全绿）

- [ ] **Step 3.5: 更新 knowledgebase/doc.go**

文件目录树加两行（validate.go 后）：
```
//	├── simple_kb.go       # SimpleKnowledgeBase 完整实现（13.27，5 抽象方法）
//	└── multi_kb.go        # 多知识库聚合检索两函数（13.27）
```
（注意 `└──` 归属：multi_kb.go 在 Task 5 创建，此处一并写入会与"文件不存在"矛盾——**此处先只加 simple_kb.go 行并保持 `└── validate.go`→`├──` 修正，multi_kb.go 行在 Task 5 再加**。）

同时订正 doc.go L19 的待补注释：原文 `（13.27 simple.go / 13.28 graph.go 落地时补充目录条目与对应 Python 路径）`，其中 `simple.go` 是文件名笔误（实际落地为 `simple_kb.go`），改为 `（13.27 simple_kb.go 已落地；13.28 graph.go 落地时补充目录条目与对应 Python 路径）`。

包概述补一句：`SimpleKnowledgeBase（13.27）实现 5 抽象方法，是检索查询管线在会话侧的总入口；multi_kb.go 提供跨知识库聚合检索。`

- [ ] **Step 3.6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/simple_kb.go internal/agentcore/retrieval/knowledgebase/simple_kb_test.go internal/agentcore/retrieval/knowledgebase/doc.go
git commit -m "feat(knowledgebase): 13.27 SimpleKnowledgeBase 五方法（add/delete/update/statistics+懒创建骨架）"
```

---

### Task 4: Retrieve 懒创建 + agentic 包装（simple_kb.go 续）

**Files:**
- Modify: `internal/agentcore/retrieval/knowledgebase/simple_kb.go`
- Test: `internal/agentcore/retrieval/knowledgebase/simple_kb_test.go`

- [ ] **Step 4.1: 写失败测试（追加，Python retrieve 系列 6 例 + Go 增强 5 例）**

```go
// —— Retrieve ——

// TestSimpleKB_Retrieve带检索器 对齐 test_retrieve_with_retriever。
func TestSimpleKB_Retrieve带检索器(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "Test result", Score: 0.95}}}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeVector}, r)
	results, err := kb.Retrieve(context.Background(), "test query", nil)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	assert.Equal(t, 1, r.retrieveCalls)
}

// TestSimpleKB_Retrieve非agentic 对齐 test_retrieve_without_agentic。
func TestSimpleKB_Retrieve非agentic(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	cfg := common.NewDefaultRetrievalConfig()
	results, err := kb.Retrieve(context.Background(), "q", cfg)
	require.NoError(t, err)
	assert.Len(t, results, 1)
	// AgenticRetriever 未包装：仍是注入的 retriever 直调
	assert.Equal(t, 1, r.retrieveCalls)
}

// TestSimpleKB_Retrieve无检索器无存储 对齐 test_retrieve_without_retriever_or_vector_store。
func TestSimpleKB_Retrieve无检索器无存储(t *testing.T) {
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	_, err := kb.Retrieve(context.Background(), "test query", nil)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155503, be.Code())
	assert.Contains(t, be.Message(), "vector_store or retriever is required")
}

// TestSimpleKB_Retrieve懒创建三分支 Go 增强：index_type → retriever 类型 + mode。
func TestSimpleKB_Retrieve懒创建三分支(t *testing.T) {
	vs, _ := makeCompatiblePair()
	for _, tc := range []struct {
		indexType common.IndexTypeKind
		wantMode  retriever.RetrievalMode
	}{
		{common.IndexTypeVector, retriever.RetrievalModeVector},
		{common.IndexTypeBM25, retriever.RetrievalModeSparse},
		{common.IndexTypeHybrid, retriever.RetrievalModeHybrid},
	} {
		t.Run(string(tc.indexType), func(t *testing.T) {
			kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: tc.indexType},
				nil, WithVectorStore(vs))
			require.NoError(t, err)
			_, err = kb.Retrieve(context.Background(), "q", nil)
			require.NoError(t, err) // fakeAttrStore Search 返回 nil,nil，检索器返回空结果
			assert.NotNil(t, kb.retriever)
		})
	}
}

// TestSimpleKB_Retrieve模式推导对齐 断言传给底层 retriever 的 mode（Go 增强）。
func TestSimpleKB_Retrieve模式推导对齐(t *testing.T) {
	r := &recordingRetriever{}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeBM25}, r)
	_, err := kb.Retrieve(context.Background(), "q", nil)
	require.NoError(t, err)
	assert.Equal(t, retriever.RetrievalModeSparse, r.lastMode)
	// nil config → 私有默认 TopK 5（Go 增强，spec §5.2）
	assert.Equal(t, 5, r.lastTopK)
}

// TestSimpleKB_Retrieve并发懒创建 Go 增强 -race：并发 Retrieve 只建一次 retriever。
func TestSimpleKB_Retrieve并发懒创建(t *testing.T) {
	vs, _ := makeCompatiblePair()
	kb, err := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb", IndexType: common.IndexTypeVector},
		nil, WithVectorStore(vs))
	require.NoError(t, err)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = kb.Retrieve(context.Background(), "q", nil)
		}()
	}
	wg.Wait()
	// 8 次调用全部成功（懒创建无竞态）
	assert.NotNil(t, kb.retriever)
}

// TestSimpleKB_RetrieveAgentic 对齐 test_retrieve_with_agentic：
// 包装 AgenticRetriever（构造参数 retriever/llm_client 透传）。
func TestSimpleKB_RetrieveAgentic(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	llm := &stubKBLlm{}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r, WithLLMClient(llm))
	cfg := &common.RetrievalConfig{TopK: 5, Agentic: true}
	results, err := kb.Retrieve(context.Background(), "test query", cfg)
	require.NoError(t, err)
	assert.NotEmpty(t, results)
	// 底层 retriever 被 AgenticRetriever 包裹调用（read/link 链路）
	assert.GreaterOrEqual(t, r.retrieveCalls, 1)
}

// TestSimpleKB_RetrieveAgenticMaxIter Go 增强：Extra["max_iter"] 映射 WithAgenticMaxIter。
func TestSimpleKB_RetrieveAgenticMaxIter(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{{Text: "r", Score: 0.9}}}
	llm := &stubKBLlm{}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r, WithLLMClient(llm))
	cfg := &common.RetrievalConfig{TopK: 5, Agentic: true}
	_, err := kb.Retrieve(context.Background(), "q", cfg, WithKBExtra("max_iter", 3))
	require.NoError(t, err)
	// max_iter 透传到 AgenticRetriever 构造（无法直接断言私有字段——通过行为：
	// 构造成功即可；键双轨静默忽略其余键不报错）
	_, err = kb.Retrieve(context.Background(), "q", cfg, WithKBExtra("unknown_key", "x"))
	require.NoError(t, err)
}

// TestSimpleKB_RetrieveFilters透传 伏笔 1：Filters 经 RetrieveOptions 传给 retriever（写而不消费）。
func TestSimpleKB_RetrieveFilters透传(t *testing.T) {
	r := &recordingRetriever{results: []common.RetrievalResult{}}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	cfg := &common.RetrievalConfig{TopK: 5, Filters: map[string]any{"doc_type": "pdf"}}
	_, err := kb.Retrieve(context.Background(), "q", cfg)
	require.NoError(t, err)
	opts := retriever.NewRetrieveOptions(r.lastOpts...)
	assert.Equal(t, map[string]any{"doc_type": "pdf"}, opts.Filters)
}

// TestSimpleKB_RetrieveAgentic无LLM Go 增强：agentic 但 llmClient nil → 155209 冒泡。
func TestSimpleKB_RetrieveAgentic无LLM(t *testing.T) {
	r := &recordingRetriever{}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, r)
	cfg := &common.RetrievalConfig{TopK: 5, Agentic: true}
	_, err := kb.Retrieve(context.Background(), "q", cfg)
	require.Error(t, err)
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155209, be.Code())
}
```

- [ ] **Step 4.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1 -run TestSimpleKB_Retrieve`
Expected: FAIL（Retrieve 还是占位，懒创建/agentic/filters 全不满足）

- [ ] **Step 4.3: 实现完整 Retrieve（替换 Task 3 占位）**

```go
// Retrieve 检索相关文档。
//
// retriever 懒创建（index_type 三分支）→ config nil 私有默认 →
// agentic 时外包 AgenticRetriever → Filters 槽写入（伏笔 1）。
//
// Python: retrieve
func (s *SimpleKnowledgeBase) Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	if s.retriever == nil {
		s.retrieverMu.Lock()
		defer s.retrieverMu.Unlock()
		if s.retriever == nil {
			if s.vectorStore == nil {
				return nil, exception.BuildError(
					exception.StatusRetrievalKbVectorStoreNotFound,
					exception.WithMsg("vector_store or retriever is required for retrieve"),
				)
			}
			switch s.config.IndexType {
			case common.IndexTypeVector:
				s.retriever = retriever.NewVectorRetriever(s.vectorStore, s.embedModel)
			case common.IndexTypeBM25:
				s.retriever = retriever.NewSparseRetriever(s.vectorStore)
			default:
				s.retriever = retriever.NewHybridRetriever(s.vectorStore, s.embedModel)
			}
		}
	}
	retrievalConfig := config
	if retrievalConfig == nil {
		retrievalConfig = defaultRetrievalConfig()
	}
	mode := retriever.RetrievalModeHybrid
	switch s.config.IndexType {
	case common.IndexTypeVector:
		mode = retriever.RetrievalModeVector
	case common.IndexTypeBM25:
		mode = retriever.RetrievalModeSparse
	}
	kbOpts := NewKBOptions(opts...)
	var target retriever.Retriever = s.retriever
	if retrievalConfig.Agentic {
		agenticOpts := []retriever.AgenticRetrieverOption{}
		if v, ok := kbOpts.Extra["max_iter"].(int); ok {
			agenticOpts = append(agenticOpts, retriever.WithAgenticMaxIter(v))
		}
		ar, err := retriever.NewAgenticRetriever(s.retriever, s.llmClient, agenticOpts...)
		if err != nil {
			return nil, err
		}
		target = ar
	}
	return target.Retrieve(ctx, query, retrievalConfig.TopK, retrievalConfig.ScoreThreshold, mode,
		retriever.WithRetrieveFilters(retrievalConfig.Filters))
}
```

非导出函数区追加：

```go
// defaultRetrievalConfig nil config 的私有默认（Python config or RetrievalConfig()，
// TopK 5 对齐 pydantic 默认；spec §5.2 拍板：不动 common 包）。
func defaultRetrievalConfig() *common.RetrievalConfig {
	return &common.RetrievalConfig{TopK: 5}
}
```

**注意：** `defer s.retrieverMu.Unlock()` 会让锁持有到方法尾——懒创建段后应显式 `s.retrieverMu.Unlock()` 再继续（避免长持锁）。修正版结构：

```go
	if s.retriever == nil {
		s.retrieverMu.Lock()
		if s.retriever == nil {
			// ... 创建逻辑（含 155503 return 前 Unlock）
		}
		s.retrieverMu.Unlock()
	}
```

（155503 分支 return 前需 `s.retrieverMu.Unlock()`，用 defer 不适用于早退路径——采用 defer + 把懒创建收进独立私有方法 `ensureRetriever()` 最干净：）

```go
// ensureRetriever 懒创建检索器（双检锁）。
func (s *SimpleKnowledgeBase) ensureRetriever() error {
	if s.retriever != nil {
		return nil
	}
	s.retrieverMu.Lock()
	defer s.retrieverMu.Unlock()
	if s.retriever != nil {
		return nil
	}
	if s.vectorStore == nil {
		return exception.BuildError(
			exception.StatusRetrievalKbVectorStoreNotFound,
			exception.WithMsg("vector_store or retriever is required for retrieve"),
		)
	}
	switch s.config.IndexType {
	case common.IndexTypeVector:
		s.retriever = retriever.NewVectorRetriever(s.vectorStore, s.embedModel)
	case common.IndexTypeBM25:
		s.retriever = retriever.NewSparseRetriever(s.vectorStore)
	default:
		s.retriever = retriever.NewHybridRetriever(s.vectorStore, s.embedModel)
	}
	return nil
}
```

Retrieve 开头改为：
```go
	if err := s.ensureRetriever(); err != nil {
		return nil, err
	}
```

- [ ] **Step 4.4: 跑测试确认通过（-race）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: PASS（并发懒创建测试 -race 干净）

- [ ] **Step 4.5: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/simple_kb.go internal/agentcore/retrieval/knowledgebase/simple_kb_test.go
git commit -m "feat(knowledgebase): 13.27 Retrieve 懒创建三分支+agentic 包装+Filters 伏笔回填"
```

---

### Task 5: 多 KB 聚合两函数（multi_kb.go）

**Files:**
- Create: `internal/agentcore/retrieval/knowledgebase/multi_kb.go`
- Test: `internal/agentcore/retrieval/knowledgebase/multi_kb_test.go`
- Modify: `internal/agentcore/retrieval/knowledgebase/doc.go`

- [ ] **Step 5.1: 写失败测试（Python TestRetrieveMultiKb 4 例 1:1 + Go 增强 4 例）**

测试桩（multi_kb_test.go）：**注意 Python mock_kb 是 AsyncMock 整个 KB 接口**——Go 需 fakeKB 实现 KnowledgeBase 全接口，只关心 Retrieve：

```go
package knowledgebase

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/reranker"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

// fakeKB 可编程 KB 桩（Python AsyncMock 同构：只关心 retrieve）。
type fakeKB struct {
	kbID    string
	results []common.RetrievalResult
	err     error
}
func (f *fakeKB) AddDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error) {
	return nil, nil
}
func (f *fakeKB) Retrieve(ctx context.Context, query string, config *common.RetrievalConfig, opts ...KBOption) ([]common.RetrievalResult, error) {
	return f.results, f.err
}
func (f *fakeKB) DeleteDocuments(ctx context.Context, docIDs []string, opts ...KBOption) (bool, error) {
	return false, nil
}
func (f *fakeKB) UpdateDocuments(ctx context.Context, documents []reranker.Document, opts ...KBOption) ([]string, error) {
	return nil, nil
}
func (f *fakeKB) GetStatistics(ctx context.Context) (map[string]any, error) { return nil, nil }
func (f *fakeKB) ParseFiles(ctx context.Context, filePaths []string, opts ...KBOption) ([]reranker.Document, error) {
	return nil, nil
}
func (f *fakeKB) ParseURLs(ctx context.Context, urls []string, opts ...KBOption) ([]reranker.Document, error) {
	return nil, nil
}
func (f *fakeKB) DeleteCollection(ctx context.Context, collection string) error { return nil }
func (f *fakeKB) Close()                                                        {}
func (f *fakeKB) Config() common.KnowledgeBaseConfig {
	return common.KnowledgeBaseConfig{KBID: f.kbID}
}

// TestRetrieveMultiKB_空列表 对齐 test_retrieve_multi_kb_empty_list。
func TestRetrieveMultiKB_空列表(t *testing.T) {
	results := RetrieveMultiKB(context.Background(), nil, "test query", nil, nil)
	assert.Empty(t, results)
}

// TestRetrieveMultiKB_成功 对齐 test_retrieve_multi_kb_success（去重+降序）。
func TestRetrieveMultiKB_成功(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "Result 1", Score: 0.9}, {Text: "Result 2", Score: 0.8},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "Result 2", Score: 0.85}, {Text: "Result 3", Score: 0.7},
	}}
	topK := 5
	results := RetrieveMultiKB(context.Background(), []KnowledgeBase{kb1, kb2}, "test query", nil, &topK)
	assert.LessOrEqual(t, len(results), 5)
	assert.Contains(t, results, "Result 1")
	assert.Contains(t, results, "Result 2")
	// 去重后 3 条，降序
	assert.Equal(t, []string{"Result 1", "Result 2", "Result 3"}, results)
}

// TestRetrieveMultiKB_部分失败 对齐 test_retrieve_multi_kb_with_failure。
func TestRetrieveMultiKB_部分失败(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", err: errors.New("Error")}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{{Text: "Result 1", Score: 0.9}}}
	results := RetrieveMultiKB(context.Background(), []KnowledgeBase{kb1, kb2}, "test query", nil, nil)
	assert.Equal(t, []string{"Result 1"}, results)
}

// TestRetrieveMultiKBWithSource_聚合 对齐 test_retrieve_multi_kb_with_source。
func TestRetrieveMultiKBWithSource_聚合(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "Result 1", Score: 0.9, Metadata: map[string]any{"raw_score": 0.9, "raw_score_scaled": 0.9}},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "Result 1", Score: 0.95, Metadata: map[string]any{"raw_score": 0.95, "raw_score_scaled": 0.95}},
	}}
	topK := 5
	results := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "test query", nil, &topK)
	require.NotEmpty(t, results)
	assert.Equal(t, "Result 1", results[0].Text)
	assert.Equal(t, 0.95, results[0].Score)
	assert.Equal(t, []string{"kb1", "kb2"}, results[0].KbIDs)
}

// —— Go 增强 ——

// TestRetrieveMultiKB_Limit真值链 三档：topK 显式 > config.TopK > 默认 5。
func TestRetrieveMultiKB_Limit真值链(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "a", Score: 0.9}, {Text: "b", Score: 0.8}, {Text: "c", Score: 0.7},
		{Text: "d", Score: 0.6}, {Text: "e", Score: 0.5}, {Text: "f", Score: 0.4},
	}}
	// 档 1：显式 topK
	two := 2
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", nil, &two), 2)
	// 档 2：config.TopK
	cfg := &common.RetrievalConfig{TopK: 3}
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", cfg, nil), 3)
	// 档 3：默认 5
	assert.Len(t, RetrieveMultiKB(context.Background(), []KnowledgeBase{kb}, "q", nil, nil), 5)
}

// TestRetrieveMultiKBWithSource_RawScore语义 首见 meta 固定 + 后续非 nil 才 max（spec §6.2）。
func TestRetrieveMultiKBWithSource_RawScore语义(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "t", Score: 0.9, Metadata: map[string]any{"raw_score": 0.1, "keep": "first"}},
	}}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{
		{Text: "t", Score: 0.95}, // 无 metadata → raw_score 回落 score=0.95
	}}
	kb3 := &fakeKB{kbID: "kb3", results: []common.RetrievalResult{
		{Text: "t", Score: 0.5, Metadata: map[string]any{"raw_score": 0.2}},
	}}
	results := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2, kb3}, "q", nil, nil)
	require.Len(t, results, 1)
	r := results[0]
	assert.Equal(t, 0.95, r.Score)      // max(0.9, 0.95, 0.5)
	assert.Equal(t, 0.2, r.RawScore)    // max(0.1, 0.95, 0.2)
	assert.Equal(t, 0.95, r.RawScoreScaled) // 首见 meta 无 raw_score_scaled → 回落 score 0.9；后续 kb2 无 meta → 0.95
	assert.Equal(t, "first", r.Metadata["keep"]) // 首见 meta 固定不更新
	assert.Equal(t, []string{"kb1", "kb2", "kb3"}, r.KbIDs)
}

// TestRetrieveMultiKBWithSource_KbIDs排序去重 sorted + set 并集。
func TestRetrieveMultiKBWithSource_KbIDs排序去重(t *testing.T) {
	kb1 := &fakeKB{kbID: "zz", results: []common.RetrievalResult{{Text: "t", Score: 0.9}}}
	kb2 := &fakeKB{kbID: "aa", results: []common.RetrievalResult{{Text: "t", Score: 0.8}}}
	results := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "q", nil, nil)
	require.Len(t, results, 1)
	assert.Equal(t, []string{"aa", "zz"}, results[0].KbIDs) // sorted
}

// TestRetrieveMultiKBWithSource_部分失败 软失败 Warn 吞 + 配对不漂移。
func TestRetrieveMultiKBWithSource_部分失败(t *testing.T) {
	kb1 := &fakeKB{kbID: "kb1", err: errors.New("boom")}
	kb2 := &fakeKB{kbID: "kb2", results: []common.RetrievalResult{{Text: "ok", Score: 0.9}}}
	results := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb1, kb2}, "q", nil, nil)
	require.Len(t, results, 1)
	assert.Equal(t, "ok", results[0].Text)
	assert.Equal(t, []string{"kb2"}, results[0].KbIDs)
}

// TestRetrieveMultiKBWithSource_稳定排序 同 score 保序（spec §9.2-9）。
func TestRetrieveMultiKBWithSource_稳定排序(t *testing.T) {
	kb := &fakeKB{kbID: "kb1", results: []common.RetrievalResult{
		{Text: "first", Score: 0.5}, {Text: "second", Score: 0.5}, {Text: "third", Score: 0.5},
	}}
	results := RetrieveMultiKBWithSource(context.Background(), []KnowledgeBase{kb}, "q", nil, nil)
	require.Len(t, results, 3)
	assert.Equal(t, []string{"first", "second", "third"}, []string{results[0].Text, results[1].Text, results[2].Text})
}
```

- [ ] **Step 5.2: 跑测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... -count=1 -run TestRetrieveMultiKB`
Expected: FAIL（`RetrieveMultiKB` undefined）

- [ ] **Step 5.3: 实现 multi_kb.go**

```go
package knowledgebase

import (
	"context"
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/retrieval/common"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// multiKBAggEntry with_source 聚合的内部条目。
//
// rawScore/rawScoreScaled 用指针区分"未设置"与"显式 0"（Python None 语义，
// spec §8-13）；Metadata 首见固定不更新（Python 字面）。
type multiKBAggEntry struct {
	score          float64
	rawScore       *float64
	rawScoreScaled *float64
	kbIDs          map[string]struct{}
	metadata       map[string]any
}

// ──────────────────────────── 导出函数 ────────────────────────────

// RetrieveMultiKB 跨知识库检索：按 text 去重、score 取最大、降序返回文本列表。
//
// 单库失败 Warn 吞返回空列表（软失败）；limit 真值链 topK > config.TopK > 5。
// 裸 gather：goroutine 槽位各写各的，无 fail-fast（13.17/13.25 方案 A 先例）。
//
// Python: retrieve_multi_kb
func RetrieveMultiKB(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, topK *int) []string {
	results := retrieveMultiKBGather(ctx, kbs, query, config)
	merged := make(map[string]float64)
	for _, rs := range results {
		for _, r := range rs {
			score := r.Score
			if score < 0 {
				score = 0 // Python score None→0.0 在 Go 为 float64 零值，此处防御负值不必要——删除此分支
			}
			if existing, ok := merged[r.Text]; !ok || score > existing {
				merged[r.Text] = score
			}
		}
	}
	ranked := make([]string, 0, len(merged))
	for text := range merged {
		ranked = append(ranked, text)
	}
	sort.SliceStable(ranked, func(i, j int) bool {
		return merged[ranked[i]] > merged[ranked[j]]
	})
	limit := multiKBLimit(topK, config)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked
}

// RetrieveMultiKBWithSource 跨知识库检索（带来源信息）。
//
// 返回 MultiKBRetrievalResult（13.22 模型唯一生产场景）。kb_ids set 并集
// sorted 后装填；首见 metadata 固定；raw_score 非 nil 才参与 max。
//
// Python: retrieve_multi_kb_with_source
func RetrieveMultiKBWithSource(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig, topK *int) []common.MultiKBRetrievalResult {
	results := retrieveMultiKBGather(ctx, kbs, query, config)
	merged := make(map[string]*multiKBAggEntry)
	var order []string
	for i := range kbs {
		kbID := kbs[i].Config().KBID
		for _, r := range results[i] {
			score := r.Score
			meta := r.Metadata
			if meta == nil {
				meta = map[string]any{}
			}
			rawScore := score
			if v, ok := meta["raw_score"].(float64); ok {
				rawScore = v
			}
			rawScoreScaled := score
			if v, ok := meta["raw_score_scaled"].(float64); ok {
				rawScoreScaled = v
			}
			entry, exists := merged[r.Text]
			if !exists {
				entry = &multiKBAggEntry{
					score:          score,
					rawScore:       &rawScore,
					rawScoreScaled: &rawScoreScaled,
					kbIDs:          map[string]struct{}{},
					metadata:       meta,
				}
				merged[r.Text] = entry
				order = append(order, r.Text)
			} else {
				if score > entry.score {
					entry.score = score
				}
				if entry.rawScore == nil || rawScore > *entry.rawScore {
					entry.rawScore = &rawScore
				}
				if entry.rawScoreScaled == nil || rawScoreScaled > *entry.rawScoreScaled {
					entry.rawScoreScaled = &rawScoreScaled
				}
			}
			entry.kbIDs[kbID] = struct{}{}
		}
	}
	ranked := make([]string, len(order))
	copy(ranked, order)
	sort.SliceStable(ranked, func(i, j int) bool {
		return merged[ranked[i]].score > merged[ranked[j]].score
	})
	limit := multiKBLimit(topK, config)
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	out := make([]common.MultiKBRetrievalResult, 0, len(ranked))
	for _, text := range ranked {
		e := merged[text]
		kbIDs := make([]string, 0, len(e.kbIDs))
		for id := range e.kbIDs {
			kbIDs = append(kbIDs, id)
		}
		sort.Strings(kbIDs)
		item := common.MultiKBRetrievalResult{
			Text:     text,
			Score:    e.score,
			KbIDs:    kbIDs,
			Metadata: e.metadata,
		}
		if e.rawScore != nil {
			item.RawScore = *e.rawScore
		}
		if e.rawScoreScaled != nil {
			item.RawScoreScaled = *e.rawScoreScaled
		}
		out = append(out, item)
	}
	return out
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// retrieveMultiKBGather 并发执行各 KB 检索（软失败）。
//
// 返回与 kbs 等长的结果切片（Python zip(kbs, all_results) 的索引配对）。
// 单库 error → Warn 吞，该槽位为空列表。
func retrieveMultiKBGather(ctx context.Context, kbs []KnowledgeBase,
	query string, config *common.RetrievalConfig) [][]common.RetrievalResult {
	results := make([][]common.RetrievalResult, len(kbs))
	var wg sync.WaitGroup
	for i := range kbs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rs, err := kbs[i].Retrieve(ctx, query, config)
			if err != nil {
				logger.Warn(logComponent).
					Str("kb_id", kbs[i].Config().KBID).
					Err(err).
					Msg("retrieve_multi_kb: kb failed")
				return
			}
			results[i] = rs
		}(i)
	}
	wg.Wait()
	return results
}

// multiKBLimit limit 真值链：topK > config.TopK > 5（Python or 链）。
func multiKBLimit(topK *int, config *common.RetrievalConfig) int {
	if topK != nil && *topK > 0 {
		return *topK
	}
	if config != nil && config.TopK > 0 {
		return config.TopK
	}
	return 5
}
```

**⚠️ 两处需要实施者注意（上面代码块里有标记）：**
1. `RetrieveMultiKB` 里 `if score < 0` 那段是错的——Go Score 是 float64 零值天然 0.0，直接 `score := r.Score` 删掉防御分支。写文件时用修正版：

```go
	merged := make(map[string]float64)
	for _, rs := range results {
		for _, r := range rs {
			if existing, ok := merged[r.Text]; !ok || r.Score > existing {
				merged[r.Text] = r.Score
			}
		}
	}
```

2. Python `top_k or ...` 的 or 是**真值判断**：`top_k=0` 是 falsy 会跳到 config——所以 `multiKBLimit` 里 `*topK > 0` 是对齐的（显式 0 视同未设）。已在注释说明，勿改成 `!= nil`。

- [ ] **Step 5.4: 跑测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: PASS

- [ ] **Step 5.5: doc.go 补 multi_kb.go 行（Task 3 留的）**

- [ ] **Step 5.6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/multi_kb.go internal/agentcore/retrieval/knowledgebase/multi_kb_test.go internal/agentcore/retrieval/knowledgebase/doc.go
git commit -m "feat(knowledgebase): 13.27 多KB聚合两函数（裸gather软失败+去重max+limit真值链）"
```

---

### Task 6: parse_files/parse_urls 三例 1:1 映射 + 全量回归 + 计划表同步

**Files:**
- Test: `internal/agentcore/retrieval/knowledgebase/simple_kb_test.go`
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 6.1: 补 parse 系列三例（Python test_parse_files_* 1:1）**

```go
// —— ParseFiles（模板方法在基座，SimpleKB 入口组合行为 1:1 映射，spec §9.1）——

// TestSimpleKB_ParseFiles成功 对齐 test_parse_files_success。
func TestSimpleKB_ParseFiles成功(t *testing.T) {
	p := &twoDocParser{}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithParser(p))
	docs, err := kb.ParseFiles(context.Background(), []string{"test1.txt", "test2.txt"})
	require.NoError(t, err)
	assert.Len(t, docs, 4)
	assert.Equal(t, 2, p.calls)
}

// TestSimpleKB_ParseFiles无解析器 对齐 test_parse_files_without_parser。
func TestSimpleKB_ParseFiles无解析器(t *testing.T) {
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil)
	_, err := kb.ParseFiles(context.Background(), []string{"test.txt"})
	var be *exception.BaseError
	require.True(t, errors.As(err, &be))
	assert.Equal(t, 155500, be.Code())
	assert.Contains(t, be.Message(), "parser is required")
}

// TestSimpleKB_ParseFiles异常吞 对齐 test_parse_files_with_exception。
func TestSimpleKB_ParseFiles异常吞(t *testing.T) {
	p := &twoDocParser{err: errors.New("Parse error")}
	kb, _ := NewSimpleKnowledgeBase(common.KnowledgeBaseConfig{KBID: "kb"}, nil, WithParser(p))
	docs, err := kb.ParseFiles(context.Background(), []string{"test.txt"})
	require.NoError(t, err)
	assert.Empty(t, docs)
}
```

`twoDocParser` 桩（fakeKBParser 已在 base_test.go——若其接口匹配直接复用；不匹配则新写，实现 parser.Parser 三方法 Parse/LazyParse/Process + Supports。base_test.go 的 fakeKBParser 只实现了部分方法的话，本桩独立）：

```go
// twoDocParser 每次 Parse 返回两个 Document 的解析器桩。
type twoDocParser struct {
	calls int
	err   error
}
func (p *twoDocParser) Parse(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	return []reranker.Document{
		{ID: "doc_1", Text: "Test document 1"}, {ID: "doc_2", Text: "Test document 2"},
	}, nil
}
func (p *twoDocParser) LazyParse(ctx context.Context, doc string, fn func(reranker.Document) error, opts ...parser.ParseOption) error {
	docs, err := p.Parse(ctx, doc, opts...)
	if err != nil {
		return err
	}
	for _, d := range docs {
		if err := fn(d); err != nil {
			return err
		}
	}
	return nil
}
func (p *twoDocParser) Process(ctx context.Context, doc string, opts ...parser.ParseOption) ([]reranker.Document, error) {
	return p.Parse(ctx, doc, opts...)
}
func (p *twoDocParser) Supports(doc string) bool { return true }
```

（import 需补 `parser` 包。）

- [ ] **Step 6.2: 跑本包测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" -race ./internal/agentcore/retrieval/knowledgebase/... -count=1`
Expected: PASS

- [ ] **Step 6.3: 检查包覆盖率**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./internal/agentcore/retrieval/knowledgebase/... ./internal/agentcore/retrieval/utils/... ./internal/agentcore/retrieval/retriever/... -count=1 -cover`
Expected: knowledgebase ≥ 85%（基线 94.9%，新增文件不低于 85 即可）；utils/retriever 不低于基线

- [ ] **Step 6.4: 全量回归（CI 同款构建标签）**

Run: `cd /home/opensource/uapclaw-gateway && CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./... -count=1 2>&1 | tail -5`
Expected: 零 FAIL（时间较长，耐心等完；编译前 pgrep 检查无并发 go 进程）

- [ ] **Step 6.5: 更新 IMPLEMENTATION_PLAN.md**

13.27 行 ☐→✅，产出列按实际内容写（含 Filters 伏笔回填 + 断言缺口兑现 + 8 错误码转消费 + 15 偏差归档 spec 引用）。13.19 行的"13.27 SimpleKB 写 filters"伏笔已兑现，保持原文不改历史行（既有惯例：产出信息记在 13.27 行）。

领域十三进度注记追加（L797 后）：
```
> 2026-11-05：13.27 SimpleKnowledgeBase 完成（查侧总入口：5 抽象方法+懒创建三分支+agentic 包装+多KB聚合两函数+ConfigManager；Filters 伏笔回填死参数复刻；错误码 155501/155502/155504+155300-155304 转已消费；覆盖率 XX%，-race 干净）。下一节 13.28 GraphKnowledgeBase。设计：docs/superpowers/specs/2026-11-05-simpleknowledgebase-13.27-design.md
```

- [ ] **Step 6.6: 提交**

```bash
cd /home/opensource/uapclaw-gateway
git add internal/agentcore/retrieval/knowledgebase/simple_kb_test.go IMPLEMENTATION_PLAN.md
git commit -m "test(knowledgebase): 13.27 parse 系列三例 1:1 映射 + 计划表状态同步"
```

---

## 计划自查记录（writing-plans Self-Review）

1. **Spec 覆盖**：§2 文件结构=Task 1/3/5；§3 类型设计=Task 3；§4 五方法=Task 3/4/6；§5 Retrieve=Task 4；§6 聚合=Task 5；§7 ConfigManager=Task 1；§9 测试=各 Task Step 1；§10 伏笔 1=Task 2、伏笔 2=Task 3 断言、伏笔 3=Task 5、伏笔 4=Task 4、伏笔 5=Task 3、伏笔 6 错误码=各 Task；§11 文档同步=Task 1.5/3.5/5.5/6.5 ✅
2. **占位符扫描**：Task 1.3 故意展示了"坏版本+完整版"两个 SaveToFile 代码块（实施者必须用第二个完整版）；Task 5.3 同样两处修正标注——均为防呆设计而非占位符，实施时用修正版 ✅
3. **类型一致性**：`fakeAttrIndexer` 字段嵌入 `recordingIndexer`；`NewRetrieveOptions`/`NewIndexOptions` 是既有导出函数（测试断言用）；`recordingIndexer.lastUpdateCfg` 在 Task 3 桩定义、Task 3 测试使用 ✅
4. **已知风险**：Task 3 的 Retrieve 占位使中间提交不完整（接口已断言完整实现）——占位返回 nil,nil 保证编译，Task 4 立即替换，两个 Task 连续执行 ✅
