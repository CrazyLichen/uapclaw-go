# 7.3 + CodingMemoryRail 审查修复设计

> 日期：2027-03-05
> 状态：审查修复

## 背景

7.3 + CodingMemoryRail + MemoryRail + MemberMemoryToolkit 实现完成后，审查发现以下问题需要修复。

## 修复清单

### 修复 1：CodingMemoryRail.registerCodingMemoryTools 中 GetNodePath 错误 + 缺少回填逻辑

**问题**：`GetNodePath("memory")` 应为 `GetNodePath("coding_memory")`，且缺少 Python 中的回填步骤——创建工具后应用 `r.codingMemoryDir` 回填 `toolCtx.CodingMemoryDir`。

**对齐 Python**：
```python
# Python CodingMemoryRail._register_coding_memory_tools
self._tool_ctx = CodingMemoryToolContext(
    coding_memory_dir="coding_memory",  # 默认值
    node_name="coding_memory",
)
memory_tools = create_coding_memory_tools(self._tool_ctx, ...)
# 关键回填
if self._tool_ctx and self._coding_memory_dir:
    self._tool_ctx.coding_memory_dir = self._coding_memory_dir
```

**修复**：
- `coding_memory_rail.go`：`GetNodePath("memory")` → `GetNodePath("coding_memory")`
- 创建工具后加回填：`if r.codingMemoryDir != "" { toolCtx.CodingMemoryDir = r.codingMemoryDir }`

### 修复 2：code_adapter.buildCodingMemoryRail instance 时序问题 + 完全对齐 Python create_coding_memory_rail

**问题**：`buildCodingMemoryRail` 从 `c.deep.instance` 获取 workspace，但 instance 此时还未创建。

**对齐 Python**：Python 的 `create_coding_memory_rail` 函数（`interface_code.py`）：
1. 从 `config["embed"]` 解析 `embed_api_key` / `embed_base_url` / `embed_model`
2. 配置不完整时仍创建 Rail（降级到 fallback provider）
3. `codingMemoryDir = agentWorkspaceDir/coding_memory/projectName`
4. 始终返回 CodingMemoryRail（非 nil）

**修复**：

#### 2a. workspace 包添加 ResolveCodingMemoryDir

```go
// ResolveCodingMemoryDir 从 workspace 解析 coding_memory 目录路径。
// 对齐 Python _resolve_coding_memory_dir(workspace)
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

#### 2b. code_adapter.resolveEmbeddingConfig 实现解析

```go
func (c *CodeAdapter) resolveEmbeddingConfig() *embedding.EmbeddingConfig {
    // 从 configCache["embed"] 解析
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
    // 配置不完整时返回 EmbeddingConfig（api_key 为空），让 Rail 降级
    return &embedding.EmbeddingConfig{
        ModelName: modelName,
        BaseURL:   baseURL,
        APIKey:    apiKey,
    }
}
```

#### 2c. code_adapter.buildCodingMemoryRail 重写

```go
func (c *CodeAdapter) buildCodingMemoryRail() sainterfaces.AgentRail {
    // 对齐 Python create_coding_memory_rail
    embCfg := c.resolveEmbeddingConfig()

    // 获取 codingMemoryDir = agentWorkspaceDir/coding_memory/projectName
    agentWorkspaceDir := workspace.AgentRootDir()
    projectName := "default"
    if c.deep.projectDir != "" {
        projectName = filepath.Base(c.deep.projectDir)
    }
    codingMemoryDir := filepath.Join(agentWorkspaceDir, "coding_memory", projectName)

    // 创建目录（对齐 Python os.makedirs）
    os.MkdirAll(codingMemoryDir, 0o755)

    // 语言
    language := "en"

    // 始终创建 Rail（对齐 Python：即使 embedding 不完整也创建，降级到 fallback provider）
    if embCfg == nil {
        embCfg = &embedding.EmbeddingConfig{}
    }
    return memoryrail.NewCodingMemoryRail(codingMemoryDir, embCfg, language)
}
```

#### 2d. code_agent_factory.go 启用 CodingMemoryRail 注入

```go
// 在 CreateCodeAgent 中，当 EmbeddingConfig 可用时注入 CodingMemoryRail
if params.Workspace != nil {
    codingMemoryDir := hworkspace.ResolveCodingMemoryDir(params.Workspace)
    finalRails = append(finalRails, memoryrail.NewCodingMemoryRail(
        codingMemoryDir, params.EmbeddingConfig, language,
    ))
}
```

注意：需要先给 `SubagentCreateParams` 添加 `EmbeddingConfig` 字段，否则路径 A 仍被阻塞。

### 修复 3：setToSortedSlice 重复

**修复**：删除 `coding_memory_rail.go` 中的 `setToSortedSlice`，改为调用 `memory_rail.go` 中的 `memorySetToSortedSlice`。测试文件同步更新。

### 修复 4：harness/rails/doc.go 补充 memory 子包

在 doc.go 的功能列表和文件目录中添加 `memory/` 子包说明。

### 修复 5：残留标记清理

| 文件 | 标记 | 处理 |
|------|------|------|
| `code_adapter.go` | `resolveEmbeddingConfig` TODO | 实现后删除 TODO |
| `code_adapter.go` | `buildCodingMemoryRail` 中的 instance 时序 | 重写后删除 |
| `code_agent_factory.go` | TODO: 等 SubagentCreateParams 添加 EmbeddingConfig 字段 | 保留（SubagentCreateParams.EmbeddingConfig 字段还不存在） |
| `subagents/code_agent.go` | ⤵️ 9.19-23 | 保留（同上） |
| `manager.go` | ⤵️ 7.2+7.3 | 7.3 去掉只保留 7.2 |

## 不修改项

| 项目 | 原因 |
|------|------|
| `liteToolCtx` 接口 | 保留不导出，替代 any 是合理改进 |
| `GetCallbacks` 中 `railCtx any` | 框架级约定，对应 Python **kwargs |
| `MemoryRail` 缺少 `manager` 字段 | 与 Python 一致，不修改 |
