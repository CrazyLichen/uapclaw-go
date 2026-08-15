# 7.3 + CodingMemoryRail 审查修复 实现计划

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 7.3 + CodingMemoryRail 实现审查中发现的 5 类问题：GetNodePath 错误、code_adapter 时序问题、函数重复、doc.go 缺失、残留标记清理。

**Architecture:** 逐个修复，每个 Task 专注一个修复点。修复 2（code_adapter）最复杂，涉及 workspace 包新增函数 + resolveEmbeddingConfig 实现 + buildCodingMemoryRail 重写。

**Tech Stack:** Go 1.23+, 项目模块路径 `github.com/uapclaw/uapclaw-go`

---

## 文件结构

| 操作 | 文件 | 职责 |
|------|------|------|
| 修改 | `internal/agentcore/harness/rails/memory/coding_memory_rail.go` | 修复 GetNodePath + 删除 setToSortedSlice |
| 修改 | `internal/agentcore/harness/rails/memory/coding_memory_rail_test.go` | setToSortedSlice → memorySetToSortedSlice |
| 修改 | `internal/agentcore/harness/workspace/workspace.go` | 新增 ResolveCodingMemoryDir 函数 |
| 修改 | `internal/agentcore/harness/workspace/workspace_test.go` | 新增 ResolveCodingMemoryDir 测试 |
| 修改 | `internal/swarm/server/adapter/code_adapter.go` | 重写 resolveEmbeddingConfig + buildCodingMemoryRail |
| 修改 | `internal/agentcore/harness/rails/doc.go` | 补充 memory 子包说明 |
| 修改 | `internal/agent_teams/memory/manager.go` | 清理 7.3 相关 ⤵️ 标记 |
| 修改 | `internal/agentcore/harness/rails/memory/doc.go` | 更新文件目录（如有变化） |

---

### Task 1: 修复 CodingMemoryRail.registerCodingMemoryTools 中 GetNodePath 错误

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/coding_memory_rail.go:372`

- [ ] **Step 1: 修改 GetNodePath 参数**

将 `coding_memory_rail.go` 第 372 行：
```go
if nodePath := r.Workspace().GetNodePath("memory"); nodePath != nil {
```
改为：
```go
if nodePath := r.Workspace().GetNodePath("coding_memory"); nodePath != nil {
```

- [ ] **Step 2: 验证回填逻辑已存在**

确认第 391-393 行的回填逻辑已存在（之前实现时已添加）：
```go
if r.toolCtx != nil && r.codingMemoryDir != "" {
    r.toolCtx.CodingMemoryDir = r.codingMemoryDir
}
```

- [ ] **Step 3: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/memory/... -v -run TestCodingMemoryRail -count=1`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/memory/coding_memory_rail.go
git commit -m "fix: CodingMemoryRail.registerCodingMemoryTools GetNodePath('memory') → GetNodePath('coding_memory')"
```

---

### Task 2: workspace 包添加 ResolveCodingMemoryDir 函数

**Files:**
- Modify: `internal/agentcore/harness/workspace/workspace.go`
- Modify: `internal/agentcore/harness/workspace/workspace_test.go`

- [ ] **Step 1: 写测试**

在 `workspace_test.go` 中添加：

```go
func TestResolveCodingMemoryDir(t *testing.T) {
	t.Parallel()

	t.Run("nil workspace", func(t *testing.T) {
		result := ResolveCodingMemoryDir(nil)
		if result != filepath.Join(".", "coding_memory") {
			t.Errorf("ResolveCodingMemoryDir(nil) = %q, want %q", result, filepath.Join(".", "coding_memory"))
		}
	})

	t.Run("workspace with coding_memory node", func(t *testing.T) {
		ws := NewWorkspace(t.TempDir(), "cn")
		// 添加 coding_memory 目录节点
		ws.Directories = append(ws.Directories, DirectoryNode{
			"name": "coding_memory",
			"path": "coding_memory",
		})
		result := ResolveCodingMemoryDir(ws)
		expected := filepath.Join(ws.RootPath, "coding_memory")
		if result != expected {
			t.Errorf("ResolveCodingMemoryDir = %q, want %q", result, expected)
		}
	})

	t.Run("workspace without coding_memory node", func(t *testing.T) {
		ws := NewWorkspace(t.TempDir(), "cn")
		result := ResolveCodingMemoryDir(ws)
		expected := filepath.Join(ws.RootPath, "coding_memory")
		if result != expected {
			t.Errorf("ResolveCodingMemoryDir = %q, want %q", result, expected)
		}
	})

	t.Run("workspace with custom path", func(t *testing.T) {
		ws := NewWorkspace(t.TempDir(), "cn")
		ws.Directories = append(ws.Directories, DirectoryNode{
			"name": "coding_memory",
			"path": "custom_mem_path",
		})
		result := ResolveCodingMemoryDir(ws)
		expected := filepath.Join(ws.RootPath, "custom_mem_path")
		if result != expected {
			t.Errorf("ResolveCodingMemoryDir = %q, want %q", result, expected)
		}
	})
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/workspace/... -v -run TestResolveCodingMemoryDir -count=1`
Expected: FAIL — `undefined: ResolveCodingMemoryDir`

- [ ] **Step 3: 实现 ResolveCodingMemoryDir**

在 `workspace.go` 的导出函数区块（`GetNodePath` 之后）添加：

```go
// ResolveCodingMemoryDir 从 workspace 解析 coding_memory 目录路径。
// 对齐 Python _resolve_coding_memory_dir(workspace) (code_agent.py)
//
// 优先从 workspace.GetNodePath(WorkspaceNodeCodingMemory) 获取，
// 如果没有则 fallback 到 {RootPath}/coding_memory。
// workspace 为 nil 时返回 ./coding_memory。
func ResolveCodingMemoryDir(ws *Workspace) string {
	if ws != nil {
		nodePath := ws.GetNodePath(WorkspaceNodeCodingMemory)
		if nodePath != nil {
			return *nodePath
		}
		return filepath.Join(ws.RootPath, "coding_memory")
	}
	return filepath.Join(".", "coding_memory")
}
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/workspace/... -v -run TestResolveCodingMemoryDir -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/workspace/workspace.go internal/agentcore/harness/workspace/workspace_test.go
git commit -m "feat: workspace 包添加 ResolveCodingMemoryDir — 对齐 Python _resolve_coding_memory_dir"
```

---

### Task 3: 重写 code_adapter.resolveEmbeddingConfig + buildCodingMemoryRail

**Files:**
- Modify: `internal/swarm/server/adapter/code_adapter.go`

- [ ] **Step 1: 实现 resolveEmbeddingConfig**

将 `code_adapter.go` 中的 `resolveEmbeddingConfig` 方法替换为：

```go
// resolveEmbeddingConfig 从 configCache["embed"] 解析嵌入配置。
// 对齐 Python create_coding_memory_rail() (interface_code.py)
//
// 从 config 解析 embed_api_key/embed_base_url/embed_model，
// 配置不完整时返回 EmbeddingConfig（api_key 为空），让 Rail 降级到 fallback provider。
func (c *CodeAdapter) resolveEmbeddingConfig() *embedding.EmbeddingConfig {
	embedRaw, ok := c.deep.configCache["embed"]
	if !ok {
		return nil
	}
	embedConfig, ok := embedRaw.(map[string]any)
	if !ok {
		return nil
	}

	apiKey, _ := embedConfig["embed_api_key"].(string)
	baseURL, _ := embedConfig["embed_base_url"].(string)
	if baseURL == "" {
		baseURL, _ = embedConfig["embed_api_base"].(string)
	}
	modelName, _ := embedConfig["embed_model"].(string)
	if modelName == "" {
		modelName = "text-embedding-v3"
	}

	// 对齐 Python: 即使配置不完整也返回 EmbeddingConfig（api_key 为空时降级）
	return &embedding.EmbeddingConfig{
		ModelName: modelName,
		BaseURL:   baseURL,
		APIKey:    apiKey,
	}
}
```

- [ ] **Step 2: 重写 buildCodingMemoryRail**

将 `code_adapter.go` 中的 `buildCodingMemoryRail` 方法替换为：

```go
// buildCodingMemoryRail 构建编码记忆护栏。
// 对齐 Python: JiuwenClawCodeAdapter._build_coding_memory_rail() → create_coding_memory_rail() (interface_code.py)
//
// 始终创建 CodingMemoryRail（即使 embedding 不完整也创建，降级到 fallback provider）。
// codingMemoryDir = agentWorkspaceDir/coding_memory/projectName
func (c *CodeAdapter) buildCodingMemoryRail() sainterfaces.AgentRail {
	// 对齐 Python create_coding_memory_rail: 获取 embedding 配置
	embCfg := c.resolveEmbeddingConfig()

	// 对齐 Python: coding_memory_dir = agent_workspace_dir/coding_memory/project_name
	agentWorkspaceDir := cworkspace.AgentRootDir()
	projectName := "default"
	if c.deep.projectDir != "" {
		projectName = filepath.Base(c.deep.projectDir)
	}
	codingMemoryDir := filepath.Join(agentWorkspaceDir, "coding_memory", projectName)

	// 对齐 Python: os.makedirs(coding_memory_dir, exist_ok=True)
	if err := os.MkdirAll(codingMemoryDir, 0o755); err != nil {
		logger.Warn(logComponent).
			Str("event_type", "build_coding_memory_rail").
			Str("dir", codingMemoryDir).
			Err(err).
			Msg("创建 coding_memory 目录失败")
	}

	// 对齐 Python: 始终创建 Rail（embedding 不完整时降级到 fallback provider）
	if embCfg == nil {
		embCfg = &embedding.EmbeddingConfig{}
	}
	return memoryrail.NewCodingMemoryRail(codingMemoryDir, embCfg, "en")
}
```

注意：需要添加 `cworkspace` 别名导入（`cworkspace "github.com/uapclaw/uapclaw-go/internal/common/workspace"`），以及 `"os"` 和 `"path/filepath"` 导入。检查现有 import 是否已包含这些包。

- [ ] **Step 3: 运行编译**

Run: `cd /home/opensource/uapclaw-gateway && go build ./internal/swarm/server/adapter/...`
Expected: 编译通过

- [ ] **Step 4: 提交**

```bash
git add internal/swarm/server/adapter/code_adapter.go
git commit -m "feat: 重写 resolveEmbeddingConfig + buildCodingMemoryRail — 完全对齐 Python create_coding_memory_rail"
```

---

### Task 4: 删除 coding_memory_rail.go 中重复的 setToSortedSlice

**Files:**
- Modify: `internal/agentcore/harness/rails/memory/coding_memory_rail.go`
- Modify: `internal/agentcore/harness/rails/memory/coding_memory_rail_test.go`

- [ ] **Step 1: 修改 coding_memory_rail.go 中的调用**

将 `coding_memory_rail.go` 第 456 行的 `setToSortedSlice` 调用改为 `memorySetToSortedSlice`：
```go
// 原来:
Strs("tool_names", setToSortedSlice(r.ownedToolNames)).
// 改为:
Strs("tool_names", memorySetToSortedSlice(r.ownedToolNames)).
```

- [ ] **Step 2: 删除 setToSortedSlice 函数**

删除 `coding_memory_rail.go` 第 752-760 行的 `setToSortedSlice` 函数定义：
```go
// 删除以下代码:
func setToSortedSlice(s map[string]struct{}) []string {
	result := make([]string, 0, len(s))
	for k := range s {
		result = append(result, k)
	}
	sort.Strings(result)
	return result
}
```

- [ ] **Step 3: 修改测试文件中的调用**

将 `coding_memory_rail_test.go` 中的 `setToSortedSlice` 调用改为 `memorySetToSortedSlice`：
```go
// 原来:
result := setToSortedSlice(s)
// 改为:
result := memorySetToSortedSlice(s)
```

- [ ] **Step 4: 运行测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/memory/... -v -count=1`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/memory/coding_memory_rail.go internal/agentcore/harness/rails/memory/coding_memory_rail_test.go
git commit -m "refactor: 删除重复的 setToSortedSlice，统一使用 memorySetToSortedSlice"
```

---

### Task 5: 补充 harness/rails/doc.go 中 memory 子包说明

**Files:**
- Modify: `internal/agentcore/harness/rails/doc.go`

- [ ] **Step 1: 在功能列表中添加 memory 子包**

在 doc.go 的功能列表中，`context_engineer 子包` 之后添加：
```
//   - memory 子包：编程记忆和通用记忆护栏（CodingMemoryRail/MemoryRail）
```

- [ ] **Step 2: 在文件目录中添加 memory 子包**

在文件目录树中，`context_engineer/` 子包之后添加：
```
//	└── memory/              # 记忆护栏子包
//	    ├── doc.go                  # 包文档
//	    ├── coding_memory_rail.go   # CodingMemoryRail 编程记忆护栏（自动召回 + 互斥注入 + 数据隔离）
//	    ├── coding_memory_rail_test.go # 测试
//	    ├── memory_rail.go          # MemoryRail 通用记忆护栏（工具注册 + 提示词注入 + 管理器初始化）
//	    └── memory_rail_test.go     # 测试
```

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/doc.go
git commit -m "docs: 补充 rails/doc.go 中 memory 子包说明"
```

---

### Task 6: 清理残留 ⤵️/TODO 标记

**Files:**
- Modify: `internal/agent_teams/memory/manager.go`
- Modify: `internal/agent_teams/memory/doc.go`

- [ ] **Step 1: 清理 manager.go 中 7.3 相关的 ⤵️ 标记**

将 `manager.go` 第 56 行：
```go
// toolkit 成员记忆工具集。⤵️ 回填: 7.2+7.3
```
改为：
```go
// toolkit 成员记忆工具集。⤵️ 回填: 7.2
```

将 `manager.go` 第 119 行：
```go
// InitToolkit 初始化成员记忆工具集。⤵️ 回填: 7.1+7.2+7.3 — 当前返回 false
```
改为：
```go
// InitToolkit 初始化成员记忆工具集。⤵️ 回填: 7.1+7.2 — 当前返回 false
```

- [ ] **Step 2: 清理 doc.go 中 7.3 相关的 ⤵️ 标记**

读取 `internal/agent_teams/memory/doc.go`，将其中包含 `7.3` 的 ⤵️ 标记去掉 7.3 部分，只保留 7.2。

- [ ] **Step 3: 提交**

```bash
git add internal/agent_teams/memory/manager.go internal/agent_teams/memory/doc.go
git commit -m "chore: 清理 7.3 相关 ⤵️ 标记（7.3 已完成，7.2 保留）"
```

---

### Task 7: 全量编译 + 测试验证

**Files:** 无新增/修改

- [ ] **Step 1: 编译检查**

Run: `cd /home/opensource/uapclaw-gateway && pgrep -f 'go (build|test)' | xargs -r kill; go build ./...`
Expected: 编译通过

- [ ] **Step 2: 运行相关包测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./internal/agentcore/harness/rails/memory/... ./internal/agentcore/harness/workspace/... ./internal/swarm/server/adapter/... -count=1`
Expected: PASS

- [ ] **Step 3: 运行全量测试**

Run: `cd /home/opensource/uapclaw-gateway && go test ./... -count=1`
Expected: PASS

- [ ] **Step 4: 推送**

```bash
git push
```
