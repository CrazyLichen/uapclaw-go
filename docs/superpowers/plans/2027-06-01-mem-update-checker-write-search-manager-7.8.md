# 7.8 MemUpdateChecker / WriteManager / SearchManager 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 实现 7.8 章节的三个组件（MemUpdateChecker LLM 驱动冲突检查、WriteManager 写入路由器、SearchManager 搜索路由器），并回填所有标注点。

**Architecture:** 先实现 PromptApplier 基础设施（运行时读 .md 模板+缓存），然后实现 MemUpdateChecker.Check() LLM 驱动逻辑，接着回填 FragmentMemoryManager 和 lite 包的冲突检查，最后实现 WriteManager 和 SearchManager 路由器。

**Tech Stack:** Go 1.x, foundation/llm (Model.Invoke + JsonOutputParser), foundation/prompt (PromptTemplate), sync.Map 缓存, sync.Once 单例

**Spec:** `docs/superpowers/specs/2027-06-01-mem-update-checker-write-search-manager-7.8-design.md`

---

## Task 1: 复制 Python 提示词模板文件

**Files:**
- Create: `internal/agentcore/memory/prompts/fragment_memory_prompt.md`
- Create: `internal/agentcore/memory/prompts/memory_analysis_prompt.md`
- Create: `internal/agentcore/memory/prompts/memory_update_check.md`
- Create: `internal/agentcore/memory/prompts/semantic_validation.md`

- [ ] **Step 1: 创建 prompts 目录并复制 4 个 .md 文件**

从 Python 项目 `/home/opensource/agent-core/openjiuwen/core/memory/prompts/` 1:1 复制以下文件到 `internal/agentcore/memory/prompts/`，不做任何翻译或修改：

```bash
mkdir -p internal/agentcore/memory/prompts
cp /home/opensource/agent-core/openjiuwen/core/memory/prompts/fragment_memory_prompt.md internal/agentcore/memory/prompts/
cp /home/opensource/agent-core/openjiuwen/core/memory/prompts/memory_analysis_prompt.md internal/agentcore/memory/prompts/
cp /home/opensource/agent-core/openjiuwen/core/memory/prompts/memory_update_check.md internal/agentcore/memory/prompts/
cp /home/opensource/agent-core/openjiuwen/core/memory/prompts/semantic_validation.md internal/agentcore/memory/prompts/
```

- [ ] **Step 2: 验证文件内容**

```bash
wc -l internal/agentcore/memory/prompts/*.md
```

Expected: 4 个文件，行数分别为 ~110, ~61, ~53, ~44（与 Python 原文件一致）

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/prompts/
git commit -m "feat(memory): add 4 prompt template .md files from Python (1:1 copy)"
```

---

## Task 2: 创建 prompts 包 doc.go

**Files:**
- Create: `internal/agentcore/memory/prompts/doc.go`

- [ ] **Step 1: 编写 doc.go**

```go
// Package prompts 提供记忆系统的提示词模板及运行时加载器。
//
// 本包实现 PromptApplier 单例，运行时从 .md 文件加载提示词模板并缓存，
// 支持变量替换后输出完整提示词文本。4 个 .md 模板文件从 Python 项目 1:1 复制，
// 不做翻译，保持原始语言。
//
// 文件目录：
//
//	prompts/
//	├── doc.go                      # 包文档
//	├── prompt_applier.go           # PromptApplier 单例（运行时读文件 + 缓存）
//	├── fragment_memory_prompt.md   # 碎片记忆提取提示词
//	├── memory_analysis_prompt.md   # 记忆分析提示词
//	├── memory_update_check.md      # 记忆冲突检查提示词
//	└── semantic_validation.md      # 语义一致性校验提示词
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/prompts/
package prompts
```

- [ ] **Step 2: Commit**

```bash
git add internal/agentcore/memory/prompts/doc.go
git commit -m "feat(memory): add prompts package doc.go"
```

---

## Task 3: 实现 PromptApplier

**Files:**
- Create: `internal/agentcore/memory/prompts/prompt_applier.go`
- Test: `internal/agentcore/memory/prompts/prompt_applier_test.go`

- [ ] **Step 1: 编写 PromptApplier 实现**

创建 `internal/agentcore/memory/prompts/prompt_applier.go`：

```go
package prompts

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// PromptApplier 提示词模板加载器，运行时读取 .md 文件并缓存。
//
// 单例模式（对齐 Python: PromptApplier(metaclass=Singleton)），
// 缓存已加载的 PromptTemplate 实例，避免重复 I/O。
//
// 对应 Python: openjiuwen/core/memory/prompts/prompt_applier.py (PromptApplier)
type PromptApplier struct {
	// cache 已加载的模板缓存：file_prefix → *prompt.PromptTemplate
	cache sync.Map
	// promptDir 模板文件目录路径
	promptDir string
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// defaultApplierInstance 全局单例实例
	defaultApplierInstance *PromptApplier
	// defaultApplierOnce 单例初始化控制
	defaultApplierOnce sync.Once
)

// ──────────────────────────── 导出函数 ────────────────────────────

// DefaultApplier 返回全局 PromptApplier 单例。
// 模板目录通过 runtime.Caller(0) 获取当前文件所在目录（对齐 Python: Path(__file__).parent）。
func DefaultApplier() *PromptApplier {
	defaultApplierOnce.Do(func() {
		_, thisFile, _, ok := runtime.Caller(0)
		if !ok {
			panic("无法获取 PromptApplier 源文件路径")
		}
		dir := filepath.Dir(thisFile)
		defaultApplierInstance = NewPromptApplier(dir)
		logger.Info(logComponent).Msg("PromptApplier 单例初始化")
	})
	return defaultApplierInstance
}

// NewPromptApplier 创建 PromptApplier 实例。
// dir 为模板 .md 文件所在目录。
func NewPromptApplier(dir string) *PromptApplier {
	return &PromptApplier{
		promptDir: dir,
	}
}

// Apply 加载模板并替换变量，返回填充后的字符串。
//
// 对齐 Python: PromptApplier.apply(file_prefix, variables)
//
// 流程：
//  1. 缓存命中 → template.Format(variables) → 返回 Content 字符串
//  2. 缓存未命中 → 读取 {promptDir}/{filePrefix}.md → 创建 PromptTemplate → 缓存 → Format → 返回
//  3. 文件不存在 → 返回 error
func (a *PromptApplier) Apply(filePrefix string, variables map[string]any) (string, error) {
	tmpl, err := a.GetTemplate(filePrefix)
	if err != nil {
		return "", err
	}
	formatted, err := tmpl.Format(variables)
	if err != nil {
		return "", fmt.Errorf("应用提示词模板 %q 变量替换失败: %w", filePrefix, err)
	}
	content, ok := formatted.Content.(string)
	if !ok {
		return "", fmt.Errorf("提示词模板 %q 格式化后内容类型不是 string", filePrefix)
	}
	logger.Debug(logComponent).Str("file_prefix", filePrefix).Msg("已应用提示词模板")
	return content, nil
}

// GetTemplate 获取已缓存的 PromptTemplate，未缓存则加载。
//
// 对齐 Python: PromptApplier.get_template(file_prefix)
func (a *PromptApplier) GetTemplate(filePrefix string) (*prompt.PromptTemplate, error) {
	if cached, ok := a.cache.Load(filePrefix); ok {
		logger.Debug(logComponent).Str("file_prefix", filePrefix).Msg("使用缓存的提示词模板")
		return cached.(*prompt.PromptTemplate), nil
	}

	filePath := filepath.Join(a.promptDir, filePrefix+".md")
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("提示词模板文件不存在: %s: %w", filePath, err)
	}

	tmpl := prompt.NewPromptTemplate(filePrefix, string(content))
	a.cache.Store(filePrefix, tmpl)
	logger.Info(logComponent).Str("file_prefix", filePrefix).Msg("加载并缓存提示词模板")
	return tmpl, nil
}

// ClearCache 清除缓存。
//
// 对齐 Python: PromptApplier.clear_cache(file_prefix=None)
// 无参数时清除所有缓存；指定 filePrefix 时只清除该条目。
func (a *PromptApplier) ClearCache(filePrefix ...string) {
	if len(filePrefix) == 0 {
		a.cache = sync.Map{}
		logger.Info(logComponent).Msg("清除所有提示词模板缓存")
	} else {
		for _, prefix := range filePrefix {
			a.cache.Delete(prefix)
			logger.Info(logComponent).Str("file_prefix", prefix).Msg("清除提示词模板缓存")
		}
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 2: 编写 PromptApplier 测试**

创建 `internal/agentcore/memory/prompts/prompt_applier_test.go`：

```go
package prompts

import (
	"os"
	"path/filepath"
	"testing"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// TestNewPromptApplier 测试构造函数
func TestNewPromptApplier(t *testing.T) {
	dir := t.TempDir()
	applier := NewPromptApplier(dir)
	if applier == nil {
		t.Fatal("NewPromptApplier 返回 nil")
	}
	if applier.promptDir != dir {
		t.Errorf("promptDir = %q, want %q", applier.promptDir, dir)
	}
}

// TestPromptApplier_Apply_缓存命中 测试缓存命中时不再读文件
func TestPromptApplier_Apply_缓存命中(t *testing.T) {
	dir := t.TempDir()
	templateContent := "Hello {{name}}, welcome!"
	err := os.WriteFile(filepath.Join(dir, "greeting.md"), []byte(templateContent), 0644)
	if err != nil {
		t.Fatal(err)
	}

	applier := NewPromptApplier(dir)

	// 第一次调用 — 加载并缓存
	result1, err := applier.Apply("greeting", map[string]any{"name": "World"})
	if err != nil {
		t.Fatal(err)
	}
	if result1 != "Hello World, welcome!" {
		t.Errorf("result1 = %q, want %q", result1, "Hello World, welcome!")
	}

	// 删除文件后再次调用 — 应从缓存返回
	os.Remove(filepath.Join(dir, "greeting.md"))
	result2, err := applier.Apply("greeting", map[string]any{"name": "Go"})
	if err != nil {
		t.Fatal(err)
	}
	if result2 != "Hello Go, welcome!" {
		t.Errorf("result2 = %q, want %q", result2, "Hello Go, welcome!")
	}
}

// TestPromptApplier_Apply_文件不存在 测试文件不存在时返回错误
func TestPromptApplier_Apply_文件不存在(t *testing.T) {
	dir := t.TempDir()
	applier := NewPromptApplier(dir)

	_, err := applier.Apply("nonexistent", nil)
	if err == nil {
		t.Error("期望返回错误，实际返回 nil")
	}
}

// TestPromptApplier_ClearCache_全部清除 测试清除所有缓存
func TestPromptApplier_ClearCache_全部清除(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("{{x}}"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("{{y}}"), 0644)

	applier := NewPromptApplier(dir)
	applier.Apply("a", map[string]any{"x": "1"})
	applier.Apply("b", map[string]any{"y": "2"})

	applier.ClearCache()

	// 清除缓存后删除文件，应报错（证明缓存已清除）
	os.Remove(filepath.Join(dir, "a.md"))
	_, err := applier.Apply("a", nil)
	if err == nil {
		t.Error("清除缓存后应重新加载文件，文件已删应报错")
	}
}

// TestPromptApplier_ClearCache_指定前缀 测试清除指定前缀的缓存
func TestPromptApplier_ClearCache_指定前缀(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.md"), []byte("{{x}}"), 0644)
	os.WriteFile(filepath.Join(dir, "b.md"), []byte("{{y}}"), 0644)

	applier := NewPromptApplier(dir)
	applier.Apply("a", map[string]any{"x": "1"})
	applier.Apply("b", map[string]any{"y": "2"})

	applier.ClearCache("a")

	// a 的缓存被清除，删除文件后应报错
	os.Remove(filepath.Join(dir, "a.md"))
	_, err := applier.Apply("a", nil)
	if err == nil {
		t.Error("指定前缀清除后应重新加载文件，文件已删应报错")
	}

	// b 的缓存仍在，删除文件后应正常
	os.Remove(filepath.Join(dir, "b.md"))
	result, err := applier.Apply("b", map[string]any{"y": "3"})
	if err != nil {
		t.Errorf("b 缓存未被清除，应返回成功: %v", err)
	}
	if result != "3" {
		t.Errorf("result = %q, want %q", result, "3")
	}
}

// TestPromptApplier_GetTemplate 测试获取模板
func TestPromptApplier_GetTemplate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "test.md"), []byte("{{var}}"), 0644)

	applier := NewPromptApplier(dir)
	tmpl, err := applier.GetTemplate("test")
	if err != nil {
		t.Fatal(err)
	}
	if tmpl.Name != "test" {
		t.Errorf("tmpl.Name = %q, want %q", tmpl.Name, "test")
	}
}

// TestDefaultApplier_单例 测试全局单例
func TestDefaultApplier_单例(t *testing.T) {
	a1 := DefaultApplier()
	a2 := DefaultApplier()
	if a1 != a2 {
		t.Error("DefaultApplier 应返回同一实例")
	}
}

// TestPromptApplier_Apply_实际模板 测试实际 .md 模板文件加载
func TestPromptApplier_Apply_实际模板(t *testing.T) {
	// 使用 DefaultApplier（指向实际 prompts 目录）
	applier := DefaultApplier()
	applier.ClearCache()

	result, err := applier.Apply("memory_update_check", map[string]any{
		"old_information": "1: 用户喜欢阅读",
		"new_information": "2: 用户喜欢编程",
	})
	if err != nil {
		t.Fatalf("加载 memory_update_check.md 失败: %v", err)
	}
	if result == "" {
		t.Error("应用模板后结果不应为空")
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────
```

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/prompts/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/prompts/prompt_applier.go internal/agentcore/memory/prompts/prompt_applier_test.go
git commit -m "feat(memory): implement PromptApplier with runtime file loading + cache"
```

---

## Task 4: 实现 MemUpdateChecker.Check() LLM 驱动逻辑

**Files:**
- Modify: `internal/agentcore/memory/manage/update/update_checker.go`
- Test: `internal/agentcore/memory/manage/update/update_checker_test.go`（已有测试文件需更新）

- [ ] **Step 1: 修改 update_checker.go — 增加辅助函数和完整 Check() 逻辑**

在 `update_checker.go` 中：
1. 修改 import 添加 `context`, `fmt`, `sort` 和 prompts/llm 相关包
2. 增加签名 `ctx context.Context` 到 `Check()`
3. 添加 `formatInput` 非导出函数
4. 添加 `mapCheckItemsToActionItems` 非导出函数
5. 实现 Check() 完整逻辑
6. 添加 `parseCheckResult` 辅助函数（从 LLM JSON 解析 CheckResult 枚举）
7. 删除 `⤵️ 回填: 7.8` 标注
8. 同步所有 Python 日志点

完整的新文件内容：

```go
package update

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/model_clients"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm/output_parsers"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/prompt"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/prompts"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// MemoryActionItem 记忆动作项，表示一条记忆的 ADD 或 DELETE 动作。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryActionItem)
type MemoryActionItem struct {
	// ID 记忆 ID
	ID string
	// Content 记忆内容
	Content string
	// Status 动作状态
	Status MemoryStatus
}

// MemCheckItem 记忆检查结果项。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemCheckItem)
type MemCheckItem struct {
	// InfoID 记忆 ID
	InfoID string
	// InfoText 记忆内容
	InfoText string
	// Result 检查结果
	Result CheckResult
	// RelatedInfos 关联的旧记忆
	RelatedInfos map[string]string
}

// MemUpdateChecker 记忆冲突检查器。
//
// 使用 LLM 驱动的提示词模板分析新旧记忆之间的冗余和冲突关系。
// 对齐 Python: MemUpdateChecker.check(new_memories, old_memories, base_chat_model, retries=3)
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemUpdateChecker)
type MemUpdateChecker struct{}

// checkConfig Check 配置。
type checkConfig struct {
	// model LLM 模型（对齐 Python: base_chat_model）
	model *llm.Model
	// retries 重试次数（对齐 Python: retries=3）
	retries int
}

// ──────────────────────────── 枚举 ────────────────────────────

// CheckResult 记忆检查结果枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (CheckResult)
type CheckResult int

const (
	// CheckResultRedundant 冗余
	CheckResultRedundant CheckResult = iota
	// CheckResultConflicting 冲突
	CheckResultConflicting
	// CheckResultNone 共存
	CheckResultNone
)

// MemoryStatus 记忆动作状态枚举。
//
// 对应 Python: openjiuwen/core/memory/manage/update/mem_update_checker.py (MemoryStatus)
type MemoryStatus int

const (
	// MemoryStatusAdd 添加
	MemoryStatusAdd MemoryStatus = iota
	// MemoryStatusDelete 删除
	MemoryStatusDelete
)

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// CheckOption Check 可选参数。
type CheckOption func(*checkConfig)

// WithModel 设置 LLM 模型（对齐 Python: base_chat_model）。
func WithModel(m *llm.Model) CheckOption {
	return func(c *checkConfig) { c.model = m }
}

// WithRetries 设置重试次数（对齐 Python: retries）。
func WithRetries(n int) CheckOption {
	return func(c *checkConfig) { c.retries = n }
}

// Check 检查新记忆与旧记忆的冗余/冲突。
//
// 对齐 Python: MemUpdateChecker.check(new_memories, old_memories, base_chat_model, retries=3)
//
// 流程：
//  1. 无 LLM 模型时直接返回所有新记忆为 ADD
//  2. 格式化输入 → 加载提示词模板 → LLM 调用 → JSON 解析（最多 retries 次）
//  3. 映射结果：REDUNDANT→跳过 / CONFLICTING→新ADD+旧DELETE / NONE→新ADD
//  4. 解析失败 fallback：所有新记忆 ADD
func (c *MemUpdateChecker) Check(ctx context.Context, newMemories map[string]string, oldMemories map[string]string, opts ...CheckOption) ([]*MemoryActionItem, error) {
	cfg := &checkConfig{retries: 3}
	for _, opt := range opts {
		opt(cfg)
	}

	// 无 LLM 模型 → 直接返回所有新记忆为 ADD（对齐 Python: if not base_chat_model）
	if cfg.model == nil {
		logger.Debug(logComponent).
			Int("new_count", len(newMemories)).
			Int("old_count", len(oldMemories)).
			Msg("无 LLM 模型，跳过记忆冲突检查")
		return allAddItems(newMemories), nil
	}

	// 检查新旧记忆 ID 重复（对齐 Python: duplicate_ids = set(new) & set(old)）
	duplicateIDs := checkDuplicateIDs(newMemories, oldMemories)
	if len(duplicateIDs) > 0 {
		logger.Debug(logComponent).
			Int("duplicate_count", len(duplicateIDs)).
			Msg("发现重复记忆 ID")
	}

	// 格式化输入（对齐 Python: _format_input）
	newInfoStr, oldInfoStr := formatInput(newMemories, oldMemories)

	// 加载提示词模板并替换变量（对齐 Python: PromptApplier.apply）
	userPrompt, err := prompts.DefaultApplier().Apply("memory_update_check", map[string]any{
		"new_information": newInfoStr,
		"old_information": oldInfoStr,
	})
	if err != nil {
		return allAddItems(newMemories), fmt.Errorf("加载记忆冲突检查提示词模板失败: %w", err)
	}

	// 构造消息（对齐 Python: messages = [{"role": "user", "content": user_prompt}]）
	formatted := prompt.NewPromptTemplate("memory_update_check_user", userPrompt)
	messages, err := formatted.ToMessages()
	if err != nil {
		return allAddItems(newMemories), fmt.Errorf("构造冲突检查消息失败: %w", err)
	}
	msgsParam := model_clients.NewMessagesParam(messages...)

	logger.Debug(logComponent).Msg("开始记忆冲突检查")

	// LLM 调用 + JSON 解析（对齐 Python: for attempt in range(retries)）
	parser := output_parsers.NewJsonOutputParser()
	var checkItems []*MemCheckItem

	for attempt := 0; attempt < cfg.retries; attempt++ {
		response, invokeErr := cfg.model.Invoke(ctx, msgsParam,
			model_clients.WithInvokeOutputParser(parser))
		if invokeErr != nil {
			if attempt < cfg.retries-1 {
				logger.Warn(logComponent).
					Int("attempt", attempt+1).
					Int("retries", cfg.retries).
					Err(invokeErr).
					Msg("记忆冲突检查 LLM 调用失败，重试中")
				continue
			}
			logger.Error(logComponent).Err(invokeErr).Msg("记忆冲突检查 LLM 调用全部失败")
			return allAddItems(newMemories), nil
		}

		parsedResult := response.ParserContent
		if parsedResult == nil {
			if attempt < cfg.retries-1 {
				logger.Warn(logComponent).
					Int("attempt", attempt+1).
					Int("retries", cfg.retries).
					Msg("记忆冲突检查解析结果为 nil，重试中")
				continue
			}
			logger.Error(logComponent).Msg("记忆冲突检查解析结果为 nil，全部重试失败")
			return allAddItems(newMemories), nil
		}

		items, parseErr := parseCheckItems(parsedResult)
		if parseErr != nil {
			if attempt < cfg.retries-1 {
				logger.Warn(logComponent).
					Int("attempt", attempt+1).
					Int("retries", cfg.retries).
					Err(parseErr).
					Msg("记忆冲突检查解析错误，重试中")
				continue
			}
			logger.Error(logComponent).Err(parseErr).Msg("记忆冲突检查重试全部失败")
			return allAddItems(newMemories), nil
		}

		checkItems = items
		logger.Debug(logComponent).
			Int("result_count", len(checkItems)).
			Msg("记忆冲突检查 LLM 返回成功")
		break
	}

	// 映射结果为动作项（对齐 Python: check → action_items 逻辑）
	actionItems := mapCheckItemsToActionItems(checkItems, newMemories)

	logger.Debug(logComponent).
		Int("action_count", len(actionItems)).
		Msg("记忆冲突检查完成")

	return actionItems, nil
}

// String 实现 fmt.Stringer 接口，对齐 Python CheckResult.value
func (cr CheckResult) String() string {
	switch cr {
	case CheckResultRedundant:
		return "redundant"
	case CheckResultConflicting:
		return "conflicting"
	case CheckResultNone:
		return "none"
	default:
		return "unknown"
	}
}

// String 实现 fmt.Stringer 接口，对齐 Python MemoryStatus.value
func (ms MemoryStatus) String() string {
	switch ms {
	case MemoryStatusAdd:
		return "add"
	case MemoryStatusDelete:
		return "delete"
	default:
		return "unknown"
	}
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// formatInput 格式化新旧记忆字典为提示词输入文本。
//
// 对齐 Python: _format_input(new_memories, old_memories)
// 新记忆行倒序排列，旧记忆行正序排列。
func formatInput(newMemories map[string]string, oldMemories map[string]string) (string, string) {
	// 新记忆：收集行后倒序排列（对齐 Python: new_info_lines[::-1]）
	newLines := make([]string, 0, len(newMemories))
	for id, content := range newMemories {
		newLines = append(newLines, fmt.Sprintf("%s: %s", id, content))
	}
	sort.Sort(sort.Reverse(sort.StringSlice(newLines)))
	newInfoStr := ""
	for i, line := range newLines {
		if i > 0 {
			newInfoStr += "\n"
		}
		newInfoStr += line
	}

	// 旧记忆：正序排列
	oldLines := make([]string, 0, len(oldMemories))
	for id, content := range oldMemories {
		oldLines = append(oldLines, fmt.Sprintf("%s: %s", id, content))
	}
	sort.Strings(oldLines)
	oldInfoStr := ""
	for i, line := range oldLines {
		if i > 0 {
			oldInfoStr += "\n"
		}
		oldInfoStr += line
	}

	return newInfoStr, oldInfoStr
}

// mapCheckItemsToActionItems 将 LLM 检查结果映射为动作项列表。
//
// 对齐 Python: check() 方法中的 action_items 映射逻辑。
// REDUNDANT → 跳过 / CONFLICTING → 新ADD+旧DELETE / NONE → 新ADD
func mapCheckItemsToActionItems(checkItems []*MemCheckItem, newMemories map[string]string) []*MemoryActionItem {
	var actionItems []*MemoryActionItem

	for _, item := range checkItems {
		switch item.Result {
		case CheckResultRedundant:
			// 冗余 → 跳过（对齐 Python: if check_item.result == CheckResult.REDUNDANT）
			logger.Debug(logComponent).
				Str("mem_id", item.InfoID).
				Msg("记忆冗余，跳过")
			continue

		case CheckResultConflicting:
			// 冲突 → 新记忆 ADD + 关联旧记忆 DELETE
			newContent, ok := newMemories[item.InfoID]
			if !ok {
				newContent = item.InfoText
			}
			actionItems = append(actionItems, &MemoryActionItem{
				ID:      item.InfoID,
				Content: newContent,
				Status:  MemoryStatusAdd,
			})
			for oldID, oldContent := range item.RelatedInfos {
				actionItems = append(actionItems, &MemoryActionItem{
					ID:      oldID,
					Content: oldContent,
					Status:  MemoryStatusDelete,
				})
			}

		case CheckResultNone:
			// 共存 → 新记忆 ADD
			newContent, ok := newMemories[item.InfoID]
			if !ok {
				newContent = item.InfoText
			}
			actionItems = append(actionItems, &MemoryActionItem{
				ID:      item.InfoID,
				Content: newContent,
				Status:  MemoryStatusAdd,
			})
		}
	}

	return actionItems
}

// parseCheckItems 从 LLM 解析后的 any 结果中提取 MemCheckItem 列表。
//
// 对齐 Python: parsed_result → MemCheckItem.model_validate(item)
// 支持单对象（map）和数组（slice）两种格式。
func parseCheckItems(parsed any) ([]*MemCheckItem, error) {
	// 将解析结果转为 []map[string]any
	var items []map[string]any

	switch v := parsed.(type) {
	case map[string]any:
		items = []map[string]any{v}
	case []any:
		for _, elem := range v {
			if m, ok := elem.(map[string]any); ok {
				items = append(items, m)
			}
		}
	default:
		return nil, fmt.Errorf("解析结果类型不支持: %T", parsed)
	}

	if len(items) == 0 {
		return nil, fmt.Errorf("解析结果为空")
	}

	var result []*MemCheckItem
	for _, item := range items {
		infoID, _ := item["info_id"].(string)
		infoText, _ := item["info_text"].(string)
		resultStr, _ := item["result"].(string)
		checkResult := parseCheckResult(resultStr)

		relatedInfos := make(map[string]string)
		if ri, ok := item["related_infos"].(map[string]any); ok {
			for k, v := range ri {
				if vs, ok := v.(string); ok {
					relatedInfos[k] = vs
				}
			}
		}

		result = append(result, &MemCheckItem{
			InfoID:       infoID,
			InfoText:     infoText,
			Result:       checkResult,
			RelatedInfos: relatedInfos,
		})
	}

	return result, nil
}

// parseCheckResult 从字符串解析 CheckResult 枚举。
func parseCheckResult(s string) CheckResult {
	switch s {
	case "redundant":
		return CheckResultRedundant
	case "conflicting":
		return CheckResultConflicting
	case "none":
		return CheckResultNone
	default:
		return CheckResultNone
	}
}

// allAddItems 返回所有新记忆为 ADD 动作项（fallback 行为）。
func allAddItems(newMemories map[string]string) []*MemoryActionItem {
	result := make([]*MemoryActionItem, 0, len(newMemories))
	for id, content := range newMemories {
		result = append(result, &MemoryActionItem{
			ID:      id,
			Content: content,
			Status:  MemoryStatusAdd,
		})
	}
	return result
}

// checkDuplicateIDs 检查新旧记忆 ID 重复。
func checkDuplicateIDs(newMemories map[string]string, oldMemories map[string]string) []string {
	var duplicates []string
	for id := range newMemories {
		if _, ok := oldMemories[id]; ok {
			duplicates = append(duplicates, id)
		}
	}
	return duplicates
}
```

- [ ] **Step 2: 更新 update 包 doc.go**

将 `internal/agentcore/memory/manage/update/doc.go` 内容替换为：

```go
// Package update 提供记忆冲突检查器。
//
// 本包实现 MemUpdateChecker，用于检测新记忆与旧记忆之间的冗余和冲突。
// 使用 LLM 驱动的提示词模板（memory_update_check.md）进行冲突分析，
// 返回 MemoryActionItem 列表指示每条记忆应执行 ADD 或 DELETE 操作。
//
// 文件目录：
//
//	update/
//	├── doc.go              # 包文档
//	└── update_checker.go   # MemUpdateChecker 记忆冲突检查器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/update/
package update
```

- [ ] **Step 3: 运行编译检查**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/manage/update/...
```

Expected: 编译通过

- [ ] **Step 4: 更新 update_checker_test.go**

更新测试文件以覆盖新逻辑。关键测试用例：

1. `TestFormatInput` — formatInput 新旧记忆格式化（倒序/正序）
2. `TestMapCheckItemsToActionItems_冗余跳过` — REDUNDANT 不产生 action
3. `TestMapCheckItemsToActionItems_冲突` — CONFLICTING 产生 ADD+DELETE
4. `TestMapCheckItemsToActionItems_共存` — NONE 仅 ADD
5. `TestParseCheckResult` — 枚举解析
6. `TestParseCheckItems_单对象` — map 输入
7. `TestParseCheckItems_数组` — slice 输入
8. `TestAllAddItems` — fallback 函数
9. `TestCheckDuplicateIDs` — 重复 ID 检测

（不测试 LLM 调用——需要真实模型的测试用 `//go:build llm` 标签隔离）

- [ ] **Step 5: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/update/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 6: Commit**

```bash
git add internal/agentcore/memory/manage/update/
git commit -m "feat(memory): implement MemUpdateChecker.Check() with LLM-driven conflict detection"
```

---

## Task 5: 回填 FragmentMemoryManager

**Files:**
- Modify: `internal/agentcore/memory/manage/index/fragment_manager.go:146-163`

- [ ] **Step 1: 修改 FragmentMemoryManager.AddMemories 步骤 3**

将 `fragment_manager.go` 第 146-163 行替换为：

```go
	// 步骤 3：MemUpdateChecker 冲突检查
	// 对齐 Python: MemUpdateChecker.check(new_memories, old_memories, base_chat_model, retries=3)
	checker := &update.MemUpdateChecker{}
	// 提取 llmModel 参数（对齐 Python: base_chat_model=llm）
	var model *llm.Model
	if len(llmModel) > 0 {
		model = llmModel[0]
	}
	actionItems, err := checker.Check(ctx, newMemContent, oldMemories, update.WithModel(model))
	if err != nil {
		return nil, m.wrapException(err, exception.StatusMemoryAddMemoryExecutionError, m.memType)
	}
	logger.Info(logComponent).
		Int("action_count", len(actionItems)).
		Str("event_type", "MEMORY_PROCESS").
		Msg("记忆冲突检查完成")
```

同时删除第 65-72 行的 `⤵️ 回填: 7.8` 注释（AddMemories 函数注释中的回填说明），将注释修改为：

```go
// AddMemories 批量添加记忆（含冲突检查和冗余消除）。
//
// 流程（对齐 Python FragmentMemoryManager.add_memories）：
//  1. 分离 ADD/UPDATE/DELETE 操作
//  2. 搜索相关旧记忆（top_k=5, score>0.75）
//  3. 无旧记忆且仅 1 条新记忆 → 直接写入，跳过冲突检查
//  4. MemUpdateChecker 冲突检查（LLM 驱动）
//  5. 执行删除 + 添加
//
// 对齐 Python: FragmentMemoryManager.add_memories
// llm 可选参数用于 LLM 驱动冲突检查（对齐 Python: add_memories(llm=None)）
```

- [ ] **Step 2: 运行编译检查**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/manage/index/...
```

Expected: 编译通过

- [ ] **Step 3: 运行已有测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/manage/index/fragment_manager.go
git commit -m "feat(memory): backfill FragmentMemoryManager with LLM-driven conflict check"
```

---

## Task 6: 回填 lite/coding_memory_tool_ops.go

**Files:**
- Modify: `internal/agentcore/memory/lite/coding_memory_tool_ops.go:218-220, 445-450`

- [ ] **Step 1: 修改 runChecker() 空壳**

将第 445-450 行的 `runChecker` 替换为：

```go
// runChecker 调用 MemUpdateChecker 执行 LLM 冲突检测。
// 对齐 Python: coding_memory_tool_ops.py 中 runChecker 调用 MemUpdateChecker
func runChecker(ctx context.Context, model *llm.Model, newID string, newBody string, oldMemories map[string]string) []*update.MemoryActionItem {
	if model == nil {
		return nil
	}
	checker := &update.MemUpdateChecker{}
	items, err := checker.Check(ctx, map[string]string{newID: newBody}, oldMemories, update.WithModel(model))
	if err != nil {
		logger.Warn(logComponent).Err(err).Msg("记忆冲突检查失败")
		return nil
	}
	return items
}
```

需在文件顶部 import 中添加 `"context"`, `"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"`, `"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/update"`。

- [ ] **Step 2: 修改第 218-220 行的 SKIP 逻辑**

将第 218 行附近的 `⤵️ 回填: 7.8` 注释替换为实际 SKIP 判断：

```go
			// LLM 冗余判断：若新记忆不在 actions 中（即 REDUNDANT），返回 SKIP
			// 对齐 Python: runChecker → actions 不含 newID → WriteResult(mode=SKIP)
			actions := runChecker(toolCtx.Ctx, toolCtx.Model, newID, body, similarFiles)
			if len(actions) == 0 || !containsActionForID(actions, newID) {
				return WriteResult{Mode: WriteModeSkip, Path: basename, Note: "LLM 判断记忆冗余，跳过写入"}
			}
```

需要在文件底部添加辅助函数：

```go
// containsActionForID 检查 action 列表中是否包含指定 ID 的 ADD 操作。
func containsActionForID(actions []*update.MemoryActionItem, id string) bool {
	for _, a := range actions {
		if a.ID == id && a.Status == update.MemoryStatusAdd {
			return true
		}
	}
	return false
}
```

注意：`toolCtx` 当前可能没有 `Ctx` 和 `Model` 字段。需要检查 `CodingMemoryToolContext` 结构体并添加这两个字段。如果当前没有，暂时传 `nil`（lite 包的冲突检查为可选增强，模型不可用时退化为当前行为）。实际实现时需检查 `CodingMemoryToolContext` 的字段情况并做最小化适配。

- [ ] **Step 3: 运行编译检查**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./internal/agentcore/memory/lite/...
```

Expected: 编译通过

- [ ] **Step 4: 运行已有测试确认无回归**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/lite/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/memory/lite/coding_memory_tool_ops.go
git commit -m "feat(memory): backfill lite runChecker with MemUpdateChecker, add SKIP logic"
```

---

## Task 7: 实现 WriteManager

**Files:**
- Create: `internal/agentcore/memory/manage/index/write_manager.go`
- Test: `internal/agentcore/memory/manage/index/write_manager_test.go`

- [ ] **Step 1: 编写 WriteManager 实现**

创建 `internal/agentcore/memory/manage/index/write_manager.go`：

```go
package index

import (
	"context"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/llm"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// WriteManager 写入操作统一路由器。
// 根据记忆类型分发到对应子 Manager；按 ID 操作时先从 memory_index 查类型再路由。
//
// 对应 Python: openjiuwen/core/memory/manage/index/write_manager.py (WriteManager)
type WriteManager struct {
	// managers 记忆类型 → Manager 实例映射
	managers map[string]BaseMemoryManager
	// memoryIndex 记忆索引，用于按 ID 查询记忆类型
	memoryIndex index.BaseMemoryIndex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const writeLogComponent = logger.ComponentAgentCore

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

// NewWriteManager 创建写入管理器。
//
// 对齐 Python: WriteManager.__init__(managers, memory_index)
func NewWriteManager(managers map[string]BaseMemoryManager, memoryIndex index.BaseMemoryIndex) *WriteManager {
	return &WriteManager{
		managers:    managers,
		memoryIndex: memoryIndex,
	}
}

// AddMemories 批量添加记忆。
//
// 遍历 managers 去重后调用各 Manager 的 AddMemories。
// 去重是因为三种 Fragment 类型共享同一个 FragmentMemoryManager 实例（对齐 Python: set(self.managers.values())）。
//
// 对齐 Python: WriteManager.add_memories(user_id, scope_id, memories, llm)
func (w *WriteManager) AddMemories(ctx context.Context, userID string, scopeID string,
	memories map[string][]mem_model.MemoryUnit, llmModel ...*llm.Model) ([]mem_model.MemoryUnit, error) {

	if len(memories) == 0 {
		logger.Debug(writeLogComponent).Msg("无记忆单元需要添加")
		return nil, nil
	}

	var result []mem_model.MemoryUnit
	// 去重：同一 Manager 只调用一次（对齐 Python: set(self.managers.values())）
	seen := make(map[BaseMemoryManager]bool)
	for _, manager := range w.managers {
		if seen[manager] {
			continue
		}
		seen[manager] = true

		memUnits, err := manager.AddMemories(ctx, userID, scopeID, memories, llmModel...)
		if err != nil {
			logger.Error(writeLogComponent).
				Str("memory_type", manager.getMemType()).
				Err(err).
				Str("event_type", "MEMORY_STORE").
				Msg("添加记忆失败")
			return nil, err
		}
		result = append(result, memUnits...)
	}
	return result, nil
}

// UpdateMemByID 按 ID 更新记忆内容。
//
// 先从 memory_index 查 mem_type，再路由到对应 Manager 的 Update。
//
// 对齐 Python: WriteManager.update_mem_by_id(user_id, scope_id, mem_id, memory)
func (w *WriteManager) UpdateMemByID(ctx context.Context, userID string, scopeID string, memID string, newMemory string) error {
	memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
	if err != nil || memType == "" {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("跳过更新，无法获取记忆类型")
		return nil
	}
	manager, ok := w.managers[memType]
	if !ok {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("event_type", "MEMORY_STORE").
			Msg("不支持的记忆类型")
		return nil
	}
	_, err = manager.Update(ctx, userID, scopeID, memID, newMemory)
	return err
}

// DeleteMemByID 按 ID 删除记忆。
//
// 先从 memory_index 查 mem_type，再路由到对应 Manager 的 Delete。
//
// 对齐 Python: WriteManager.delete_mem_by_id(user_id, scope_id, mem_id)
func (w *WriteManager) DeleteMemByID(ctx context.Context, userID string, scopeID string, memID string) error {
	memType, err := w.getMemTypeFromIndex(ctx, userID, scopeID, memID)
	if err != nil || memType == "" {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("跳过删除，无法获取记忆类型")
		return nil
	}
	manager, ok := w.managers[memType]
	if !ok {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", memType).
			Str("event_type", "MEMORY_STORE").
			Msg("不支持的记忆类型")
		return nil
	}
	_, err = manager.Delete(ctx, userID, scopeID, memID)
	return err
}

// DeleteMemByUserID 删除用户+scope 下所有记忆。
//
// 遍历所有 Manager 调用 DeleteByUserID（对齐 Python: set(self.managers.values()) 去重）。
//
// 对齐 Python: WriteManager.delete_mem_by_user_id(user_id, scope_id)
func (w *WriteManager) DeleteMemByUserID(ctx context.Context, userID string, scopeID string) error {
	seen := make(map[BaseMemoryManager]bool)
	for _, manager := range w.managers {
		if seen[manager] {
			continue
		}
		seen[manager] = true
		_, err := manager.DeleteByUserID(ctx, userID, scopeID)
		if err != nil {
			return err
		}
	}
	return nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// getMemTypeFromIndex 从 memory_index 查询记忆类型。
//
// 对齐 Python: WriteManager.__get_mem_type_from_index(user_id, scope_id, mem_id)
func (w *WriteManager) getMemTypeFromIndex(ctx context.Context, userID string, scopeID string, memID string) (string, error) {
	doc, err := w.memoryIndex.GetByID(ctx, userID, scopeID, memID)
	if err != nil {
		return "", err
	}
	if doc == nil || doc.Type == "" {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("user_id", userID).
			Str("scope_id", scopeID).
			Str("event_type", "MEMORY_STORE").
			Msg("记忆不存在或类型为空")
		return "", nil
	}
	if _, ok := w.managers[doc.Type]; !ok {
		logger.Warn(writeLogComponent).
			Str("memory_id", memID).
			Str("memory_type", doc.Type).
			Str("event_type", "MEMORY_STORE").
			Msg("不支持的记忆类型")
		return "", nil
	}
	return doc.Type, nil
}
```

注意：`memoryManagerBase` 需要暴露 `getMemType()` 方法。检查当前 `memoryManagerBase` 是否已有此字段。如果没有，需在 `base_manager.go` 中添加：

```go
// getMemType 返回管理器类型（供 WriteManager 等外部路由使用）。
func (b *memoryManagerBase) getMemType() string {
	return b.memType
}
```

- [ ] **Step 2: 编写 WriteManager 测试**

创建 `internal/agentcore/memory/manage/index/write_manager_test.go`。使用 fakeMemoryIndex（已有）和 mock BaseMemoryManager 测试路由和去重逻辑。

关键测试用例：
- `TestWriteManager_AddMemories_去重` — 三种 Fragment 类型共享 Manager 只调用一次
- `TestWriteManager_UpdateMemByID_路由` — 按 mem_type 路由到正确 Manager
- `TestWriteManager_DeleteMemByID_路由` — 同上
- `TestWriteManager_DeleteMemByUserID_遍历` — 遍历所有 Manager
- `TestWriteManager_AddMemories_空输入` — 空输入返回 nil

- [ ] **Step 3: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/index/... -v -count=1 -run TestWriteManager
```

Expected: 所有测试 PASS

- [ ] **Step 4: Commit**

```bash
git add internal/agentcore/memory/manage/index/write_manager.go internal/agentcore/memory/manage/index/write_manager_test.go
git commit -m "feat(memory): implement WriteManager as unified write router"
```

---

## Task 8: 实现 SearchManager

**Files:**
- Create: `internal/agentcore/memory/manage/search/doc.go`
- Create: `internal/agentcore/memory/manage/search/search_manager.go`
- Test: `internal/agentcore/memory/manage/search/search_manager_test.go`

- [ ] **Step 1: 创建 search 包 doc.go**

```go
// Package search 提供搜索操作统一路由器。
//
// 本包实现 SearchManager，按记忆类型分发语义搜索请求，
// 聚合结果并按 score 排序截断。同时提供分页列表、用户画像、
// 摘要和变量查询等便利方法。
//
// 文件目录：
//
//	search/
//	├── doc.go               # 包文档
//	└── search_manager.go    # SearchManager 搜索路由器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/search/
package search
```

- [ ] **Step 2: 编写 SearchManager 实现**

创建 `internal/agentcore/memory/manage/search/search_manager.go`：

```go
package search

import (
	"context"
	"fmt"
	"sort"

	"github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/index"
	"github.com/uapclaw/uapclaw-go/internal/agentcore/memory/manage/mem_model"
	"github.com/uapclaw/uapclaw-go/internal/common/exception"
	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// SearchParams 搜索参数。
//
// 对应 Python: openjiuwen/core/memory/manage/search/search_manager.py (SearchParams)
type SearchParams struct {
	// UserID 用户 ID
	UserID string
	// ScopeID 范围 ID
	ScopeID string
	// Query 搜索查询文本
	Query string
	// TopK 返回的最大结果数
	TopK int
	// Threshold 匹配阈值
	Threshold float64
	// SearchType 指定搜索的记忆类型（可选）
	SearchType []string
}

// SearchManager 搜索操作统一路由器。
// 语义搜索按 search_type 分发到各 Manager；列表/分页直接走 memory_index。
//
// 对应 Python: openjiuwen/core/memory/manage/search/search_manager.py (SearchManager)
type SearchManager struct {
	// managers 记忆类型 → Manager 实例映射
	managers map[string]index.BaseMemoryManager
	// cryptoKey 加密密钥
	cryptoKey []byte
	// memoryIndex 记忆索引
	memoryIndex index.BaseMemoryIndex
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// logComponent 日志组件标识
const logComponent = logger.ComponentAgentCore

// DefaultTopK 默认返回结果数
const DefaultTopK = 5

// DefaultThreshold 默认匹配阈值
const DefaultThreshold = 0.3

// ──────────────────────────── 全局变量 ────────────────────────────

// allMemManagerList 所有合法的记忆类型（对齐 Python: all_mem_manager_list）
var allMemManagerList = mem_model.AllMemoryTypeValues()

// ──────────────────────────── 导出函数 ────────────────────────────

// NewSearchManager 创建搜索管理器。
//
// 对齐 Python: SearchManager.__init__(managers, crypto_key, memory_index)
func NewSearchManager(managers map[string]index.BaseMemoryManager, cryptoKey []byte, memoryIndex index.BaseMemoryIndex) *SearchManager {
	return &SearchManager{
		managers:    managers,
		cryptoKey:   cryptoKey,
		memoryIndex: memoryIndex,
	}
}

// NewSearchParams 创建默认搜索参数。
func NewSearchParams(userID, scopeID, query string) *SearchParams {
	return &SearchParams{
		UserID:     userID,
		ScopeID:    scopeID,
		Query:      query,
		TopK:       DefaultTopK,
		Threshold:  DefaultThreshold,
		SearchType: nil,
	}
}

// Search 语义搜索记忆。
//
// 按 search_type 分发到对应 Manager 的 Search()；无类型时遍历所有 Manager；
// 结果按 score 降序截断 top_k，过滤 threshold。
//
// 对齐 Python: SearchManager.search(params, **kwargs)
func (s *SearchManager) Search(ctx context.Context, params *SearchParams) ([]*index.MemorySearchResult, error) {
	userID := params.UserID
	scopeID := params.ScopeID
	query := params.Query
	topK := params.TopK
	threshold := params.Threshold
	searchType := params.SearchType

	// 校验 search_type 合法性（对齐 Python: if st not in self.all_mem_manager_list）
	if searchType != nil {
		for _, st := range searchType {
			if !containsString(allMemManagerList, st) {
				return nil, exception.NewBaseError(
					exception.StatusMemoryGetMemoryExecutionError,
					exception.WithMsg(fmt.Sprintf("%s 不是合法的搜索类型", st)),
				)
			}
		}
	}

	// 校验 search_type 对应 Manager 是否已初始化
	usedTypes := make(map[index.BaseMemoryManager][]string)
	if searchType != nil {
		for _, st := range searchType {
			manager, ok := s.managers[st]
			if !ok {
				return nil, exception.NewBaseError(
					exception.StatusMemoryGetMemoryExecutionError,
					exception.WithMsg(fmt.Sprintf("%s 记忆管理器未初始化", st)),
				)
			}
			if _, exists := usedTypes[manager]; !exists {
				usedTypes[manager] = nil
			}
			usedTypes[manager] = append(usedTypes[manager], st)
		}
	}

	var allResults []*index.MemorySearchResult

	if searchType == nil {
		// 无 search_type → 遍历所有 Manager（去重）
		seen := make(map[index.BaseMemoryManager]bool)
		for _, manager := range s.managers {
			if seen[manager] {
				continue
			}
			seen[manager] = true
			res, err := manager.Search(ctx, userID, scopeID, query, topK, nil)
			if err != nil {
				continue
			}
			allResults = append(allResults, res...)
		}
	} else {
		// 按 search_type 路由
		for manager, types := range usedTypes {
			res, err := manager.Search(ctx, userID, scopeID, query, topK, types)
			if err != nil {
				continue
			}
			allResults = append(allResults, res...)
		}
	}

	// 排序 + 截断 + threshold 过滤（对齐 Python: sorted + [:top_k] + threshold）
	sort.Slice(allResults, func(i, j int) bool {
		return allResults[i].Score > allResults[j].Score
	})
	var filtered []*index.MemorySearchResult
	for _, r := range allResults {
		if r.Score >= threshold {
			filtered = append(filtered, r)
		}
	}
	if topK > 0 && len(filtered) > topK {
		filtered = filtered[:topK]
	}
	return filtered, nil
}

// ListUserMem 分页列出用户记忆。
//
// 对齐 Python: SearchManager.list_user_mem(user_id, scope_id, nums, pages, mem_type)
func (s *SearchManager) ListUserMem(ctx context.Context, userID string, scopeID string, nums int, pages int, memType string) ([]*index.MemoryDoc, error) {
	if s.memoryIndex == nil {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("memory_index 未初始化"),
		)
	}
	start := nums * (pages - 1)
	var memTypes []string
	if memType != "" {
		memTypes = []string{memType}
	}
	return s.memoryIndex.ListMemories(ctx, userID, scopeID, start, nums, memTypes)
}

// ListUserProfile 列出用户画像记忆。
//
// 对齐 Python: SearchManager.list_user_profile(user_id, scope_id)
func (s *SearchManager) ListUserProfile(ctx context.Context, userID string, scopeID string) ([]*index.MemoryDoc, error) {
	manager, ok := s.managers[mem_model.MemoryTypeUserProfile.String()]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("fragment_memory 管理器未初始化"),
		)
	}
	fm, ok := manager.(*index.FragmentMemoryManager)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("fragment_memory 管理器类型不是 FragmentMemoryManager"),
		)
	}
	return fm.ListFragmentMemories(ctx, userID, scopeID, 0, 0, "")
}

// ListUserSummary 列出用户摘要记忆。
//
// 对齐 Python: SearchManager.list_user_summary(user_id, scope_id)
func (s *SearchManager) ListUserSummary(ctx context.Context, userID string, scopeID string) ([]*index.MemoryDoc, error) {
	manager, ok := s.managers[mem_model.MemoryTypeSummary.String()]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("summary 管理器未初始化"),
		)
	}
	sm, ok := manager.(*index.SummaryManager)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("summary 管理器类型不是 SummaryManager"),
		)
	}
	return sm.ListUserSummary(ctx, userID, scopeID)
}

// GetUserVariable 获取用户变量。
//
// 对齐 Python: SearchManager.get_user_variable(user_id, scope_id, var_name)
func (s *SearchManager) GetUserVariable(ctx context.Context, userID string, scopeID string, varName string) (string, error) {
	manager, ok := s.managers[mem_model.MemoryTypeVariable.String()]
	if !ok {
		return "", exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("variable 管理器未初始化"),
		)
	}
	vm, ok := manager.(*index.VariableManager)
	if !ok {
		return "", exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("variable 管理器类型不是 VariableManager"),
		)
	}
	res, err := vm.QueryVariable(ctx, userID, scopeID, varName)
	if err != nil {
		return "", err
	}
	if res == nil {
		return "", nil
	}
	if v, exists := res[varName]; exists {
		return v, nil
	}
	return "", nil
}

// GetAllUserVariable 获取用户所有变量。
//
// 对齐 Python: SearchManager.get_all_user_variable(user_id, scope_id)
func (s *SearchManager) GetAllUserVariable(ctx context.Context, userID string, scopeID string) (map[string]string, error) {
	manager, ok := s.managers[mem_model.MemoryTypeVariable.String()]
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("variable 管理器未初始化"),
		)
	}
	vm, ok := manager.(*index.VariableManager)
	if !ok {
		return nil, exception.NewBaseError(
			exception.StatusMemoryGetMemoryExecutionError,
			exception.WithMsg("variable 管理器类型不是 VariableManager"),
		)
	}
	return vm.QueryVariable(ctx, userID, scopeID, "")
}

// ──────────────────────────── 非导出函数 ────────────────────────────

// containsString 检查字符串是否在切片中。
func containsString(slice []string, target string) bool {
	for _, s := range slice {
		if s == target {
			return true
		}
	}
	return false
}
```

注意：需要确认 `mem_model` 包是否有 `AllMemoryTypeValues()` 函数和 `MemoryTypeUserProfile`/`MemoryTypeSummary`/`MemoryTypeVariable` 常量。如果没有，需在 Task 8 前先在 `mem_model/memory_unit.go` 中补充。

- [ ] **Step 3: 编写 SearchManager 测试**

创建 `internal/agentcore/memory/manage/search/search_manager_test.go`。

关键测试用例：
- `TestSearchManager_Search_指定类型` — 按 search_type 路由
- `TestSearchManager_Search_全部类型` — 无 search_type 时遍历
- `TestSearchManager_Search_排序截断` — score 降序 + top_k + threshold
- `TestSearchManager_Search_非法类型` — 不合法的 search_type 返回错误
- `TestSearchManager_ListUserMem` — 分页列表
- `TestSearchManager_ListUserProfile` — 委托 FragmentMemoryManager
- `TestSearchManager_GetUserVariable` — 委托 VariableManager

- [ ] **Step 4: 运行测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/manage/search/... -v -count=1
```

Expected: 所有测试 PASS

- [ ] **Step 5: Commit**

```bash
git add internal/agentcore/memory/manage/search/
git commit -m "feat(memory): implement SearchManager as unified search router"
```

---

## Task 9: 更新 doc.go 文件

**Files:**
- Modify: `internal/agentcore/memory/manage/doc.go`
- Modify: `internal/agentcore/memory/manage/index/doc.go`

- [ ] **Step 1: 更新 manage/doc.go**

在文件目录中添加 `search/` 和 `write_manager.go`：

```go
// Package manage 提供记忆管理器及其子组件。
//
// 本包实现记忆系统的核心管理器（FragmentMemoryManager、SummaryManager、VariableManager）
// 以及统一写入管理器（WriteManager）和搜索管理器（SearchManager）。
// FragmentMemoryManager 和 SummaryManager 通过 BaseMemoryManager 接口统一抽象，
// 通过 BaseMemoryIndex 接口委托存储操作。
// VariableManager 独立实现 BaseMemoryManager 接口，通过 BaseKVStore 委托 KV 存储操作。
//
// 文件目录：
//
//	manage/
//	├── doc.go              # 包文档
//	├── index/              # 记忆管理器实现
//	│   ├── doc.go          # index 包文档
//	│   ├── base_manager.go # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	│   ├── fragment_manager.go # FragmentMemoryManager 碎片记忆管理器
//	│   ├── summary_manager.go  # SummaryManager 摘要记忆管理器
//	│   ├── variable_manager.go # VariableManager 变量记忆管理器
//	│   └── write_manager.go    # WriteManager 写入操作统一路由器
//	├── mem_model/          # 记忆数据模型和数据库操作
//	│   ├── doc.go          # mem_model 包文档
//	│   ├── memory_unit.go  # 记忆数据模型（MemoryType/OperationType/FragmentMemoryUnit 等）
//	│   └── ...             # 其他数据模型和数据库操作
//	├── search/             # 搜索操作统一路由器
//	│   ├── doc.go          # search 包文档
//	│   └── search_manager.go # SearchManager 搜索路由器
//	└── update/             # 记忆冲突检查
//	    ├── doc.go          # update 包文档
//	    └── update_checker.go # MemUpdateChecker 记忆冲突检查器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/
package manage
```

- [ ] **Step 2: 更新 manage/index/doc.go**

在文件目录中添加 `write_manager.go`：

```go
// Package index 提供记忆管理器实现。
//
// 本包实现记忆系统的核心管理器：FragmentMemoryManager、SummaryManager、
// VariableManager 以及统一写入路由器 WriteManager。
// FragmentMemoryManager 和 SummaryManager 嵌入 memoryManagerBase，
// 通过 BaseMemoryIndex 委托存储操作。
// VariableManager 独立实现 BaseMemoryManager，通过 BaseKVStore 委托 KV 存储操作。
// WriteManager 作为写入操作统一路由器，按 mem_type 分发到对应子 Manager。
//
// 文件目录：
//
//	index/
//	├── doc.go               # 包文档
//	├── base_manager.go      # BaseMemoryManager 接口 + memoryManagerBase 嵌入结构体
//	├── fragment_manager.go  # FragmentMemoryManager 碎片记忆管理器
//	├── summary_manager.go   # SummaryManager 摘要记忆管理器
//	├── variable_manager.go  # VariableManager 变量记忆管理器
//	└── write_manager.go     # WriteManager 写入操作统一路由器
//
// 对应 Python 代码：
//
//	openjiuwen/core/memory/manage/index/
package index
```

- [ ] **Step 3: Commit**

```bash
git add internal/agentcore/memory/manage/doc.go internal/agentcore/memory/manage/index/doc.go
git commit -m "docs(memory): update doc.go with WriteManager and SearchManager entries"
```

---

## Task 10: 更新 IMPLEMENTATION_PLAN.md

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md`

- [ ] **Step 1: 更新 7.8 状态为 ✅**

将 `| 7.8 | ☐ | WriteManager / SearchManager / MemUpdateChecker | 写入与搜索管理（含冲突检查回填） |` 改为：

```
| 7.8 | ✅ | WriteManager / SearchManager / MemUpdateChecker | ✅ MemUpdateChecker LLM 驱动冲突检查 + ✅ PromptApplier（运行时读文件+缓存） + ✅ WriteManager 写入路由器 + ✅ SearchManager 搜索路由器 + ✅ FragmentMemoryManager 冲突检查回填 + ✅ lite runChecker 回填 | `openjiuwen/core/memory/manage/update/` · `search/` · `index/write_manager.py` |
```

- [ ] **Step 2: 更新 7.10 状态为 ✅**

将 `| 7.10 | ☐ | Memory Index | 记忆索引 |` 改为：

```
| 7.10 | ✅ | Memory Index | ✅ BaseMemoryIndex 接口 + SimpleMemoryIndex 实现 + BaseMemoryManager 接口 + FragmentMemoryManager + SummaryManager + VariableManager（实际已在 7.6/7.7/7.8 中完成） | `openjiuwen/core/memory/manage/index/` · `openjiuwen/core/foundation/store/` |
```

- [ ] **Step 3: Commit**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: update IMPLEMENTATION_PLAN.md — mark 7.8 and 7.10 as completed"
```

---

## Task 11: 全量编译和测试验证

- [ ] **Step 1: 全量编译**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go build ./...
```

Expected: 编译通过

- [ ] **Step 2: 运行所有记忆相关包的测试**

```bash
cd /home/opensource/uapclaw-gateway && export GOPROXY=https://goproxy.cn,direct && go test ./internal/agentcore/memory/... -v -count=1
```

Expected: 所有测试 PASS，覆盖率 ≥ 85%

- [ ] **Step 3: 最终 Commit（如有遗漏修复）**

```bash
git add -A && git commit -m "fix(memory): final cleanup for 7.8 implementation"
```
