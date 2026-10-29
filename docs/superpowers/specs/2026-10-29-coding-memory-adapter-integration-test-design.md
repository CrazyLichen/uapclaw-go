# Coding Memory 装配级集成测试设计

日期：2026-10-29
状态：已与用户对齐（方案A / 深度3 / 前置修复两个移植偏差）

## 1. 背景与目标

Python 侧（`/home/opensource/jiuwenswarm-develop` + `/home/opensource/agent-core`）的 coding memory 测试分布调研结论：

| 测试 | 状况 |
|------|------|
| `jiuwenswarm/tests/system_tests/test_coding_memory.py`（374行） | 空壳：临时文件读写自断言，不触碰真实组件 |
| `jiuwenswarm/.../test_coding_memory_rail.py`（613行） | Mock 重实现：文件内自定义假 CodingMemoryRail 类再测它 |
| `jiuwenswarm/.../test_coding_memory_tools.py`（594行） | 同上（MockSysOperation 自实现） |
| `agent-core/.../lite/test_coding_memory*.py` | 真实单元测试（Go 端已有对应物） |
| `jiuwenswarm/.../test_code_adapter_acp_chat_tool.py` | 唯一装配级测试（含 create_coding_memory_rail embedding 降级用例） |

**结论：Python 端没有现成的 coding memory 装配级集成测试可搬。** 装配级样板是姊妹功能 `test_project_memory.py::TestCodeModeIntegration`。

Go 端现状：
- agentcore 层已有 12 个 Rail 集成测试（`tests/integration/agentcore/harness/rails/memory/coding_memory_rail_test.go`）
- **swarm 装配层缺口**：`internal/swarm/server/adapter/code_adapter.go`（1765行）的 buildCodingMemoryRail / buildCodeAgentRails / buildConfiguredSubagents / CreateInstance 全链路无集成测试

**目标**：新建 CodeAdapter 装配级集成测试（方案A，深度3——装配 + MockLLM 工具调用全流程），并前置修复调查中发现的两个移植偏差。

## 2. 前置修复（提交1）：两个移植偏差

### 2.1 偏差一：embed 配置读取位置

| | Python | Go（现状） |
|---|---|---|
| 读取位置 | `create_coding_memory_rail(config=get_config())` → 顶层 `embed` 段 | `resolveEmbeddingConfig()` → `configCache["embed"]`，而 configCache 只从 `react` 段复制 → 实际读 `react.embed` |
| 顶层有 embed 段时 | 正常读取 | 读不到 → 降级空 EmbeddingConfig |

Python 默认 `config.yaml` 顶层就有 `embed:` 段（`resources/config.yaml:350`）。同一份配置文件，Python 能启用向量召回，Go 静默降级。

**修复**：`code_adapter.go::resolveEmbeddingConfig` 改读 `c.deep.configBase` 顶层 `embed` 段。

**顺带发现（偏差三）**：`CodeAdapter.CreateInstance` 漏了 `c.deep.configBase = configBase` 赋值（`DeepAdapter.CreateInstance` 步骤 6 有，`deep_adapter.go:412`；CodeAdapter 路径下 configBase 只是局部变量）。现有 `getToolCards` 中 `c.deep.configBase["acp_agents"]`（code_adapter.go:866）因该字段恒为 nil，acp_chat 在 CodeAdapter 路径下实际永不激活。修复 embed 时一并补上这行赋值（步骤 3 加载完成后）。

已 grep 确认 embed 读取仅 code_adapter.go 一处，无其他消费者。

### 2.2 偏差二：agentWorkspaceDir 少一层 /workspace

| | Python | Go（现状） |
|---|---|---|
| 赋值 | `self._agent_workspace_dir = str(get_agent_workspace_dir())` → `{DATA_DIR}/agent/workspace` | `c.deep.agentWorkspaceDir = workspace.AgentRootDir()` → `{DATA_DIR}/agent` |
| coding_memory 落盘 | `{DATA_DIR}/agent/workspace/coding_memory/{project_name}` | `{DATA_DIR}/agent/coding_memory/{project_name}` |

证据：Go 的 member 分支（`code_adapter.go:1519`）正确用了 `workspace.AgentWorkspaceDir()`，主分支步骤 10（`code_adapter.go:278`）用了 `AgentRootDir()`——同文件两处不一致，是笔误。

**连带影响**：`agentWorkspaceDir` 还喂给 `user_todos` 工具（`swarmtools.SetGlobalWorkspaceDir`），修复后 user_todos 落盘位置一并对齐。

**修复**：`code_adapter.go:278` 的 `workspace.AgentRootDir()` 改为 `workspace.AgentWorkspaceDir()`。

**不修**：`workspaceDir` 回退值（`code_adapter.go:273`，Python `get_agent_workspace_dir()` vs Go `AgentRootDir()`）。理由：workspaceDir 是项目上下文路径，code 模式下 projectDir 非空时不走回退分支；无 projectDir 时 Go 用 agent 根略宽但不影响 coding memory 落盘。留待后续审查裁决。

### 2.3 既有测试影响

修复会改变落盘路径，需同步检查：
- `internal/swarm/server/adapter/code_adapter_test.go` 中依赖 AgentRootDir 路径的断言（如有）
- user_todos 相关测试的落盘路径断言（如有）

## 3. 集成测试设计（提交2）

### 3.1 位置与结构

新目录 `tests/integration/swarm/server/adapter/`，单文件 `code_adapter_coding_memory_test.go`：

```go
//go:build integration

package adapter_test

type CodeAdapterCodingMemorySuite struct {
    isuite.RunnerSuite        // MockLLM + Runner 生命周期 + ResourceMgr
    providerName string       // ClientRegistry 注册名（config.yaml 引用）
    envBackup    [][3]string  // 环境变量备份
    dataDir      string       // 每测试隔离的数据目录
}
```

跨子系统（满足规则7 ≥3）：`swarm/server/adapter`（装配）+ `agentcore/harness`（DeepAgent/Rail）+ `agentcore/memory/lite`（工具落盘）+ `common/config` + `common/workspace`。

### 3.2 环境隔离（SetupTest）

全部来自已验证先例（`tui_cli_route_test.go::setupDataDir` + image_gen 坑记忆）：

1. `UAPCLAW_DATA_DIR` → `t.TempDir()`，**先 `path.ResetCache()` 再做任何路径调用**
2. 创建 `config/` 目录（模拟已初始化，防 getResolvedPaths 回退 ResourcesDir）
3. 创建 `agent/workspace/`（os.WriteFile 不建父目录）
4. 注册 MockLLM：`ClientRegistry.Register(providerName, "llm", s.MockLLM.Factory())`（先于写 config.yaml）
5. TearDownTest：恢复环境变量 + ResetCache + Unregister provider

### 3.3 config.yaml 模板

```yaml
models:
  defaults:
    - model_client_config:
        model_name: mock-model
        client_provider: mock_provider_xxx   # ClientRegistry 注册名
        api_key: mock-api-key
        api_base: http://127.0.0.1:1
        client_id: mock_provider_xxx
      is_default: true

embed:                        # 顶层段（提交1修复后的行为）
  embed_api_key: test-embed-key
  embed_base_url: http://127.0.0.1:1
  embed_model: text-embedding-test

modes:
  code:
    tools: []                 # 空 → getToolCards 回退逻辑
    rails: []                 # 不追加动态 rails
```

关键点：
- `model_name` 必须在 `model_client_config` **内层**（外层会被跳过）
- `api_base` 与 `client_id` 必填（缺失时条目被判无效）
- embedding 用假地址 127.0.0.1:1：manager 初始化网络调用失败 → 覆盖降级路径（initCodingMemoryManager 失败 → manager 为 nil → BeforeModelCall 降级注入 MEMORY.md 索引）。这正是 Python 端 `test_code_adapter_builds_coding_memory_rail_without_embedding_config` 的语义扩展

### 3.4 测试用例（7 个）

| # | 测试 | 断言要点 |
|---|------|---------|
| 1 | `TestCreateInstance_Rails装配` | CreateInstance 成功；`FindRailsByType(*memory.CodingMemoryRail)` 非空；目录已创建 |
| 2 | `TestBuildCodingMemoryRail_Embedding降级` | 顶层无 embed 段时 Rail 仍创建（非 nil），走空 EmbeddingConfig（对齐 Python test_code_adapter_builds_coding_memory_rail_without_embedding_config） |
| 3 | `TestCreateInstance_目录按项目名` | 传 `project_dir={tmp}/myproj`，断言目录 `{dataDir}/agent/workspace/coding_memory/myproj`（提交1修复后路径） |
| 4 | `TestCreateInstance_工具注册` | `Instance().AbilityManager()` 含 coding_memory_read/write/edit 三工具 |
| 5 | `TestInvoke_记忆Section注入` | MockLLM 文本响应 Invoke；`SystemPromptBuilder().HasSection(SectionMemory)` 且内容含 coding memory 指令文本 |
| 6 | `TestInvoke_写记忆全流程` | MockLLM 编排：`coding_memory_write`（frontmatter 内容）→ 文本收尾；断言 ① 文件落盘 `{dataDir}/agent/workspace/coding_memory/myproj/notes.md` ② MEMORY.md 索引含新条目 ③ 工具结果 success=true 回传到第二轮消息历史（MockLLM invoke 记录验证） |
| 7 | `TestCodeAgentSubagent_CodingMemory注入` | config 加 `react.subagents.code_agent.enabled: true`；CreateInstance 后 `CreateSubagent("code_agent", ...)`；断言 subagent 的 FindRailsByType 含 CodingMemoryRail 且 AbilityManager 含 3 工具 |

### 3.5 实现依赖的可观测 API（已验证存在）

- `DeepAgent.FindRailsByType(reflect.TypeOf(&memory.CodingMemoryRail{}))` — rails 装配验证
- `DeepAgent.AbilityManager().Get("coding_memory_write")` — 工具注册验证
- `DeepAgent.SystemPromptBuilder().HasSection(hsections.SectionMemory)` — section 注入验证
- `DeepAgent.CreateSubagent("code_agent", subSessionID)` — subagent 注入验证
- `mockllm.CreateToolCallResponse("coding_memory_write", argsJSON)` — 工具调用编排（`deep_agent_test.go::TestInvoke_多轮工具链` 先例）
- 落盘断言：`os.ReadFile(filepath.Join(dataDir, "agent", "workspace", "coding_memory", "myproj", "notes.md"))`

### 3.6 Invoke 入口

`CodeAdapter.ProcessMessageImpl`（完整装配链入口）。已验证先例：`real_llm/system_prompt_capture_test.go:372-391`——`swarmschema.NewAgentRequest(requestID, channel, ReqMethodChatSend, jsonParams, WithAgentSessionID(...))` + `adapterInst.ProcessMessageImpl(ctx, req, inputs)`，inputs 含 `query/conversation_id/channel/language`。

## 4. 范围外（YAGNI）

- ❌ 不迁移 Python 空壳系统测试（374行无价值）
- ❌ 不迁移 Mock 重实现的两个单元测试文件（agentcore 层已有更好的）
- ❌ 不测 memory_rpc 边界（方案B 已排除）
- ❌ 不测真实 embedding 网络调用（real_llm 目录独立机制）
- ❌ 不覆盖 updateRailsForMode/SwitchMode（与 coding memory 无直接耦合）

## 5. 完成标准

项目规则6：推送远端 → CI 四 job（Build/Lint/UnitTest/IntegrationTest）全绿，含规则7 integration-audit 两层门禁（静态调用跨度 ≥3 子系统 + 覆盖交叉验证 ≥3 子系统各 ≥30 行）。

## 6. 对应 Python 代码

- `jiuwenswarm/server/runtime/agent_adapter/interface_code.py`（create_coding_memory_rail / JiuwenClawCodeAdapter._build_coding_memory_rail / _build_agent_rails / _build_configured_subagents）
- `tests/unit_tests/agentserver/test_code_adapter_acp_chat_tool.py`（embedding 降级用例）
- `openjiuwen/harness/rails/memory/coding_memory_rail.py`（Rail 行为基准）
- `tests/system_tests/test_project_memory.py::TestCodeModeIntegration`（装配级测试形态样板）
