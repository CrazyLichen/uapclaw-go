# 系统提示词实时捕获集成测试 + Daily Real-LLM CI 设计

- **日期**: 2026-10-29
- **状态**: 已确认（用户已拍板全部决策点）
- **Python 原测试**: `tests/system_tests/test_system_prompt_live_capture.py`
- **任务背景**: 领域 11 集成测试对齐工作，承接 CLI 路由全链路测试（已完结）之后的下一个 system_test 移植

## 1. 目标

移植 Python `test_system_prompt_live_capture.py` 到 Go：通过回调实时捕获**真实发送给 LLM 的完整消息**，断言系统提示词的完整结构（安全原则/可用工具/技能/持久化存储体系/消息说明/工作空间/项目上下文/上下文压缩/当前日期与时间/运行时等 section），并覆盖 `skill_mode` 的 `auto_list` / `all` 两个渲染分支。

同时新增每日定时 CI workflow，把仓库既有的 real_llm 真实 LLM 测试套件（67 个测试方法，11 个文件）纳入每日自动化运行。

## 2. 用户已拍板的决策（不再讨论）

| 决策点 | 结论 |
|--------|------|
| 测试层级 | 真实 LLM（非 mock），放 `tests/integration/real_llm/` |
| CI 运行 | **默认 CI 就要运行，不做环境变量门禁**（无 Skip 守卫；key 缺失 = 失败） |
| Daily 频率 | 全量 real_llm 套件 + 新提示词捕获测试，每天一次，**北京时间凌晨 2 点** |
| Workflow 形态 | 独立 workflow 文件 + 手动触发按钮（workflow_dispatch） |
| 提示词捕获测试归属 | **放入 daily 部分**（对齐 Python 原测试 RUN_LIVE_LLM_TESTS 门禁形态；push CI 保持零外部 API 依赖） |
| 模型配置 | `LLM_API_KEY`（GitHub secret，用户已提供 ark key）+ `LLM_MODEL_NAME=deepseek-v4.1-flash`（代码默认值或 workflow env） |
| API 端点 | ark coding 端点 `https://ark.cn-beijing.volces.com/api/coding/v3`（RealLLMSuite 既有默认值，已连通验证） |

## 3. 代码变更清单

### 3.1 新增测试文件

`tests/integration/real_llm/system_prompt_capture_test.go`（`//go:build integration && llm`）：

```
结构体区:
  systemPromptCaptureSuite struct { isuite.RealLLMSuite }   — 嵌入 RealLLMSuite 复用配置读取
  promptCapture struct                                       — 捕获器（events 累积 + Mutex）
  capturedEvent struct                                       — 单次捕获：systemMessages []string, toolCount int

导出函数区:
  TestSystemPromptCaptureSuite(t)                            — suite 入口
  (s) TestSystemPromptCapture_AutoList分支()
  (s) TestSystemPromptCapture_All分支()

非导出函数区:
  runLiveCapture(skillMode) (prompt, events, error)          — 单分支完整链路
  registerCapture() / unregisterCapture()                    — OnLLM/OffLLM 双事件注册
  setupPromptEnv(skillMode) (cleanup, error)                 — UAPCLAW_DATA_DIR 三件套
  writeCaptureConfigYAML(dataDir, skillMode)                 — config.yaml 手写
  seedTestSkill(dataDir)                                     — 测试技能 SKILL.md
  assertCommonPromptStructure(t, prompt)                     — Python _assert_common_prompt_structure 对齐
  assertSkillModeAutoList(t, prompt) / assertSkillModeAll(t, prompt)
  (c) record(ctx, data) any                                  — 回调本体
```

### 3.2 新增 workflow

`.github/workflows/real-llm-ci.yml`：

```yaml
name: Real LLM CI
on:
  schedule:
    - cron: '0 18 * * *'    # UTC 18:00 = 北京时间 02:00
  workflow_dispatch: {}
concurrency:
  group: real-llm-ci
  cancel-in-progress: false
jobs:
  real-llm-test:
    runs-on: ubuntu-latest
    timeout-minutes: 40
    steps:
      - uses: actions/checkout@v6
      - uses: actions/setup-go@v6
        with: { go-version: "1.26", cache: true }
      - name: 运行 real_llm 全量测试
        env:
          LLM_API_KEY: ${{ secrets.LLM_API_KEY }}
          RUN_REAL_LLM_TESTS: "1"
          LLM_MODEL_NAME: deepseek-v4.1-flash
          CGO_ENABLED: 1
        run: go test -mod=mod -tags "sqlite_fts5 test integration llm" ./tests/integration/real_llm/...
```

**fail-not-skip 决策落地**：无前置 key 检查步骤。测试代码对 `LLM_API_KEY` 为空的处理 = `t.Fatal`（而非 `t.Skip`），失败信息提示"需要配置 GitHub secret LLM_API_KEY"。key 缺失 → 全套件红灯，配置后自动恢复。

### 3.3 既有文件改动

| 文件 | 改动 |
|------|------|
| `tests/integration/real_llm/doc.go` | 文件目录追加 `system_prompt_capture_test.go` 条目 + 对应 Python 代码路径 |
| `IMPLEMENTATION_PLAN.md` | 11.9/11.15 相关行状态更新（若适用） |

## 4. 测试机制设计

### 4.1 捕获机制（对齐 Python LLM_INPUT 回调）

Python 用 `Runner.callback_framework.register_sync(LLMCallEvents.LLM_INPUT, ...)`。Go 等价物是 **Model 门面层**事件（不是客户端层的 `callback.LLMInput`——后者不携带 Messages）：

```go
fw := runner.GetCallbackFramework()
fw.OnLLM(callback.LLMInvokeInput, c.record)    // model.go:141，Invoke 前触发，携带 Messages
fw.OnLLM(callback.LLMStreamInput, c.record)    // model.go:239，Stream 前触发
// 测试结束:
fw.OffLLM(callback.LLMInvokeInput, c.record)
fw.OffLLM(callback.LLMStreamInput, c.record)   // 按 %p 函数指针匹配（framework.go:201）
```

**为什么双事件**：Python LLM_INPUT 在 invoke/stream 双路径都触发。Go 端 `LLMInvokeInput`（model.go:141）与 `LLMStreamInput`（model.go:239）分立，plan 模式走哪条取决于 DeepAgent 内部实现，双注册兜底。

**record 回调骨架**：

```go
func (c *promptCapture) record(ctx context.Context, data *callback.LLMCallEventData) any {
    c.mu.Lock()
    defer c.mu.Unlock()
    if mp, ok := data.Messages.(model_clients.MessagesParam); ok {
        ev := capturedEvent{toolCount: ...}
        for _, m := range mp.Messages() {
            if m.GetRole() == "system" {          // 需核实 BaseMessage 接口的 role 获取方式
                ev.systemMessages = append(ev.systemMessages, m.GetContent())
            }
        }
        c.events = append(c.events, ev)
    }
    return nil
}
```

### 4.2 捕获点选择依据（为什么不是 ModelCallObserver）

| 捕获点 | 位置 | 携带 RuntimePromptRail 注入的 # 运行时状态？ |
|--------|------|------------------------------------------|
| `callback.LLMInput`（客户端层） | openai/inference_affinity/siliconflow 客户端 | 不携带 Messages/Tools，不可用 |
| testhelpers ModelCallObserver | BeforeModelCall previewMsgs | **否**（捕获早于 RuntimePromptRail 动态注入） |
| **`callback.LLMInvokeInput` / `LLMStreamInput`（本设计）** | Model 门面层，Invoke/Stream 前 | **是**（完整 messages，含运行时状态节） |

Python 断言 `"# 运行时" in prompt`（runtime_prompt_rail.go:186/364 的动态注入），故必须选 Model 门面层回调。

### 4.3 环境装配（每个测试方法独立）

| 步骤 | 实现 | 对齐 Python |
|------|------|------------|
| 1. 数据目录 | `t.Setenv("UAPCLAW_DATA_DIR", t.TempDir())` + `pathutil.ResetCache()` — **必须在任何路径函数首次调用前**（sync.Once 缓存，同进程多 suite 污染坑，记忆有案） | 临时 HOME |
| 2. workspace 模板 | `workspace.Prepare(InitOption{WorkspaceDir: tmp, Language: "zh"})` — Go 端已有完整移植（AGENT/SOUL/IDENTITY/HEARTBEAT/MEMORY.md + memory/ + config.yaml + .env），比 Python 手工 copy2 标准化。注意：Prepare 会写默认 config.yaml，测试需在其后**覆盖写入**自定义 models.defaults 条目 + `react.skill_mode` | `_sync_prompt_workspace_templates` + `mock.patch get_config` |
| 3. 测试技能 | 自建 `tmp/agent/workspace/skills/test-skill/SKILL.md`（frontmatter: name/description，正文标题+两句话）。**Go 仓库 resources 无内置技能模板**（Python 有 10 个），自建保证 all 分支断言确定性 | copytree skills 模板 |
| 4. browser env | `t.Setenv("BROWSER_RUNTIME_MCP_ENABLED", "true")` | mock.patch.dict os.environ |

**config.yaml 覆盖写入格式**（对齐 `buildModelCacheFromDefaults` deep_adapter.go:2122 的读取格式 + `LLM_API_KEY` env 读取，buildModelFromEntry deep_adapter.go:2049 读 `mcc["api_key"]`）：

```yaml
preferred_language: zh
react:
  skill_mode: <auto_list | all>
models:
  defaults:
    - model_client_config:
        client_provider: OpenAI
        model_name: deepseek-v4.1-flash
        api_key: ${LLM_API_KEY}      # 运行时从环境变量替换，不落盘
```

**api_key 注入方式**（实施时二选一，倾向 A）：
- **A（推荐）**：YAML 写占位符，测试代码读 `os.Getenv("LLM_API_KEY")` 后字符串替换再落盘——key 永不出现在测试文件里
- B：YAML 直接写死（CI 环境无文件泄密面，但本地跑时 key 落入 t.TempDir，可接受但不如 A 优雅）

**为何 config.yaml 手写而非复用 `workspace.Prepare` 的默认骨架**：`resources/config.yaml` 是最小骨架（无 models 段，已核实），手写 15 行精确控制 models.defaults + skill_mode，测试意图更清晰。

### 4.4 执行链路

```
adapter := adapter.NewDeepAdapter()          — 构造（无参）
adapter.CreateInstance(ctx, nil, "agent.plan", "")   — 从 config.yaml 建真实模型缓存
adapter.ProcessMessageImpl(request, inputs)  — query="只回复 PONG。不要调用任何工具。不要解释。"
  → runner.RunAgent(...)                     — DeepAgent 全链路：rails → RuntimePromptRail 动态注入
    → Model.Invoke → TriggerLLM(LLMInvokeInput) — 回调捕获完整 system prompt
```

**request/inputs 构造对齐 Python**（`AgentRequest{request_id, channel_id: "web", session_id: "web_prompt_capture_<hex>", req_method: CHAT_SEND, params: {query, mode: "agent.plan", files: {}}, is_stream: false, metadata}`，inputs `{conversation_id, query: buildUserPrompt(...), channel: "web", language: "zh"}`）。

**build_user_prompt 对应**：Go 端 `buildUserPrompt`（server 层，需核实导出路径），`files={}` 空文件列表。

### 4.5 Python 两个 noop patch 的 Go 处理（有意偏差）

| Python patch | Go 处理 | 理由 |
|--------------|---------|------|
| `mock.patch.set_checkpoint` → noop | **不跳过** | `UAPCLAW_DATA_DIR` 重定向后 checkpoint 落临时目录，天然无害且多覆盖真实代码 |
| `mock.patch.load_user_rails` → noop | **不跳过** | 重定向后的空数据目录里无用户 rails 可加载，自然为空 |

### 4.6 断言清单（逐条对齐 Python）

**公共结构（两个测试都断言，`_assert_common_prompt_structure` 20 条 = 18 正向 + 2 负向）**：

| # | 断言 | Python 原文 |
|---|------|------------|
| 1 | `"# 安全原则" in prompt` | `assert "# 安全原则" in first_system_prompt` |
| 2 | `"# 可用工具" in prompt` | 同上 |
| 3 | `"- bash: 执行 Shell 命令" in prompt` | 同上 |
| 4 | `"- code: 执行 Python 或 JavaScript 代码" in prompt` | 同上 |
| 5 | `"## bash 使用原则" in prompt` | 同上 |
| 6 | `"## task_tool 使用原则" in prompt` | 同上 |
| 7 | 负向：`"## task_tool 用于创建临时子代理" not in prompt` | 同上 |
| 8 | 负向：`"- bash / code:" not in prompt` | 同上 |
| 9 | `- "code_agent":` in prompt | 同上 |
| 10 | `- "research_agent":` in prompt | 同上 |
| 11 | `- "browser_agent":` in prompt | 同上 |
| 12 | `"# 技能" in prompt` | 同上 |
| 13 | `"# 持久化存储体系" in prompt` | 同上 |
| 14 | `"# 消息说明" in prompt` | 同上 |
| 15 | `"# 工作空间" in prompt` | 同上 |
| 16 | `"# 项目上下文" in prompt` | 同上 |
| 17 | `"使用 todo 工具（todo_create、todo_modify、todo_list）" in prompt` | 同上 |
| 18 | `"# 上下文压缩" in prompt` | 同上 |
| 19 | `"# 当前日期与时间" in prompt` | 同上 |
| 20 | `"# 运行时" in prompt` | 同上（靠 Model 门面层回调捕获点保证） |

**auto_list 分支专属（4 条，含 mode/skill_mode 元断言）**：
- `result["request"]["mode"] == "agent.plan"`（元断言：mode 传递正确）
- `result["request"]["skill_mode"] == "auto_list"`（元断言：skill_mode 传递正确）
- `"- list_skill: 列出可用技能" in prompt`
- `"需要时先调用 list_skill 查看可用技能" in prompt`

**all 分支专属（8 条，含 mode/skill_mode 元断言）**：
- `result["request"]["mode"] == "agent.plan"`（元断言：mode 传递正确）
- `result["request"]["skill_mode"] == "all"`（元断言：skill_mode 传递正确）
- 负向：`"- list_skill: 列出可用技能" not in prompt`
- 负向：`"需要时先调用 list_skill 查看可用技能" not in prompt`
- `"执行前先用 read_file 阅读相关 SKILL.md。" in prompt`
- `"可用技能：" in prompt`
- 正则 `\n\d+\.\s+.+?: .+`（技能编号列表）
- `"Path: " in prompt`（技能路径展示）

**断言总数校验**（机器核对已完成 2026-10-29）：公共 20 条（18 正 + 2 负，与 Python `_assert_common_prompt_structure` 逐条 1:1）+ auto_list 4 条 + all 8 条 = 单测试 24 条结构断言；另加 `_run_live_capture` 主流程的 3 条前置断言（error is nil / events ≥ 1 / system_messages 非空）。实施时以 `grep -c` 机器核对落盘代码的断言数与本清单一致。

### 4.7 断言文案的 Go 端核实结果（研究阶段已完成）

全部文案已在 Go 端 sections 代码核实存在：
- `# 安全原则`：sections/safety.go:12 BuildSafetySection
- `# 可用工具`：sections/context.go:165/273/299 BuildToolsContent
- `# 技能`：sections/skills.go:125 BuildSkillsSection → all→BuildAllModeSkillPrompt(:100) / auto_list→BuildAutoListModeSkillPrompt(:116)
- `# 持久化存储体系`：sections/memory.go:16/54/186/252 BuildMemorySection
- `# 工作空间`：sections/workspace.go:136；`# 项目上下文`：sections/context.go:428 BuildContextSection
- `# 上下文压缩`：sections/reload.go:12 reloadHintCN
- `# 当前日期与时间` / `# 运行时`：runtime_prompt_rail.go:186/364 动态注入
- `- "code_agent":` / `- "research_agent":` / `- "browser_agent":`：subagent 描述列表 section

**断言前逐条 grep 核实规则**：实施时任何断言文案与 Go 端渲染不一致，先核实是"提示词一比一复刻约束"下的文案偏差还是测试断言问题（记忆 feedback-prompt-copy-python.md）。

## 5. Daily workflow 设计

### 5.1 语义分层（最终形态）

| 层 | 频率 | 内容 | LLM_API_KEY 缺失时 |
|----|------|------|---------------------|
| ci.yml push/PR CI | 每次 push | mock 集成测试 + 单元测试 | 不需要 key（无外部 API） |
| real-llm-ci.yml daily | 每天北京 02:00 | 全量 real_llm 67 测试 + 新增 2 个提示词捕获测试 | **测试失败（红灯）** |
| real-llm-ci.yml 手动 | 手动触发 | 同 daily | 同上 |

**用户的 "fail-not-skip" 决策**体现在测试代码：`LLM_API_KEY == ""` → `s.T().Fatal("需要配置 GitHub secret LLM_API_KEY...")`（real_llm 套件内其他测试仍保留 SkipIfNoRealLLM——只有新测试按用户决策 fail）。

### 5.2 时间设定

- cron `0 18 * * *` = UTC 18:00 = 北京 02:00（用户指定凌晨 2 点）
- GitHub schedule 有 15~30 分钟漂移，属正常
- `timeout-minutes: 40`：全量 67 测试历史耗时 < 15 分钟，40 分钟余量充分

### 5.3 与 push CI 的关系

- 完全解耦：独立 workflow 文件、独立 concurrency group、独立 badge
- **不动 ci.yml**：push CI 保持零外部 API 依赖，永不因 API 投诉阻塞 PR
- integration-audit 合规性：real_llm 目录在 CI 套件外（`integration && llm` 双 tag），静态审计跳过（audit.go L201-214 inactive 目录处理，记忆有案）——新增测试沿用 `integration && llm` tag 不引入新合规负担

### 5.4 secret 配置（用户操作，1 分钟）

GitHub repo **Settings → Secrets and variables → Actions** → 新增 `LLM_API_KEY` = 用户提供的 ark key。配置后点 workflow_dispatch 手动触发验证一次，之后每日自动。

## 6. 飞行前检查清单（实施时执行）

1. **`LLM_MODEL_NAME` 默认值**：`RealLLMSuite` 的默认值当前是 `minimax-m3`（real_llm_suite.go:53）。用户最新指定 `deepseek-v4.1-flash`。方案：workflow env 显式设置 `LLM_MODEL_NAME: deepseek-v4.1-flash`（不动 suite 默认值——既有测试在 minimax-m3 下已验证，贸然切换既有 67 测试的默认模型有回归风险）
2. **`skills_state.json` 是否必需**：Go 端 skillManager 可能读取；若 LoadSkillsFromDir 报错则补写空结构（Python 模板实测：`{"marketplaces": [{"name": "baoyu", "url": "...", "enabled": false, ...}]}`，仅空 marketplaces 列表）
3. **plan 模式 Invoke/Stream 路径**：双事件注册兜底（LLMInvokeInput + LLMStreamInput）
4. **`BROWSER_RUNTIME_MCP_ENABLED=true` 在 CI 无 playwright 二进制**：若 browser MCP 启动失败导致 Fatal 则移除该 env（`- "browser_agent":` 断言来自 subagent 描述列表，不依赖 browser MCP 工具本身）
5. **文案逐字一致性**：断言前逐条 grep Go 端 sections/*.go 实际渲染文案；任何偏差先按"提示词一比一复刻"约束核实
6. **`workspace.Prepare` 顺序**：先 Prepare 再覆盖写 config.yaml（否则 Prepare 的默认 config.yaml 覆盖测试注入的 models.defaults）
7. **回调反注册**：`t.Cleanup` 中 OffLLM 双事件，防止泄漏影响同包其他测试
8. **推理模型特性**：deepseek-v4.1-flash 是推理模型（响应含 reasoning_content），对本测试无影响（断言 system prompt，不涉及响应内容），但 timeout 需给足（推理模型首 token 延迟较高，60s ctx 内已验证可用）
9. **subagent 列表断言**：`- "code_agent":` / `- "research_agent":` / `- "browser_agent":` 这三条断言依赖 subagent 配置存在；若默认 agent.plan 模式下 subagent 列表为空，需在 config.yaml 补 `subagents` 配置段（实施时核实 Go 端默认 subagents 列表的 config 读取路径）
10. **首次运行验证**：用户配置 secret 后手动触发 workflow_dispatch；本地先跑通再推
