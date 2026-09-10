# 48 小时代码逻辑审查报告

> 审查时间：2026-09-10
> 审查范围：48 小时内 11 个提交（`0b6d02e7..8188d276`）
> Python 参考：`/home/opensource/agent-core/openjiuwen/` + `/home/opensource/jiuwenswarm-develop/jiuwenswarm/`

---

## 一、提交概览与涉及章节

| 提交 | 描述 | 涉及章节 | 变更文件数 |
|------|------|---------|-----------|
| `0b6d02e7` | 10.3.25 审查修复设计 | 10.3.25 | 1 |
| `c47ba1a4` | 10.3.25 审查修复实现计划 | 10.3.25 | 1 |
| `ded847d7` | fix(shell): 删除 bash.go 重复调用 DetectAndRecordDeletions | 9.38 | 2 |
| `3b57e6d1` | refactor(server/utils): 删除 4 个无调用方 stub 函数 | 10.3.25 | 4 |
| `babd228d` | feat(server/utils): ParseStreamChunk 一比一对齐 Python | 10.3.25 | 6 |
| `da5559e9` | feat(adapter): DeepAdapter 传入 hasStreamedContent 参数对齐 Python | 10.3.2 | 3 |
| `db225890` | style: 全局声明顺序与格式修正 | 全局 | 120+ |
| `2405f1e9` | refactor: 声明顺序重排 + 注释修正 + 依赖升级 | 全局 | 30+ |
| `45b446c5` | docs: 新增审查文档、设计规格、实现计划 | 10.3.23/10.3.25 | 8 |
| `c518f31e` | style: 补充遗漏的格式修正 | 全局 | 5 |
| `aac9d8db` | fix(ci): 修复 CI Lint 超时和测试断言 | CI | 2 |
| `8188d276` | fix(lint): reflect.Ptr → reflect.Pointer | 全局 | 5 |

**重点审查章节**：9.38（Shell 工具）、10.3.2（DeepAdapter）、10.3.25（Server Utils）、9.24（EvolutionRail）、10.5（Extensions）、9.38-49（SkillDev）、7.21（Migration）、10.6.3-10（Swarm Rails）

---

## 二、问题汇总

| 编号 | 严重级别 | 章节 | 文件 | 问题摘要 |
|------|---------|------|------|---------|
| **严重** | | | | |
| S-01 | 严重 | 10.3.2 | `deep_adapter.go` | hasStreamedContent 传参逻辑错误——始终为 false |
| S-02 | 严重 | 10.3.2 | `deep_adapter.go` | 缺少 `answer` 专用分支——answer 类型混入 default 处理 |
| S-03 | 严重 | 10.3.25 | `stream_utils.go` | `context.compression_state` event_type 错误 + 缺少字段提取 |
| S-04 | 严重 | 10.3.25 | `stream_utils.go` | `controller_output` 交互传递只取 interactions[0]，Python 传整个列表 |
| S-05 | 严重 | 9.24 | `evolution_rail.go` | `safeRunEvolution` 缺少超时机制（`_get_evolution_total_timeout_secs`） |
| S-06 | 严重 | 9.24 | `evolution_rail.go` | `safeRunEvolution` 缺少 `timed_out` 状态处理 |
| S-07 | 严重 | 10.5 | `owner_scopes.go` | `check_avatar_permission` severity 比较逻辑错误：PermissionLevelNone 映射为 0 |
| S-08 | 严重 | 10.5 | `owner_scopes.go` | `ParsePermissionLevel("none")` 不应返回 PermissionLevelNone |
| S-09 | 严重 | — | `skill_manager.go` | `setPluginEnabled` 类型断言错误——永远无法匹配已安装插件 |
| S-10 | 严重 | — | `skill_manager.go` | `HandlePluginsEnable/Disable` 锁外 saveState，存在窗口期状态不一致 |
| S-11 | 严重 | — | `skill_manager.go` | `addLocalSkill` 缺少去重逻辑和 saveState 调用 |
| S-12 | 严重 | — | `skill_manager.go` | `removeInstalledPlugin` 缺少 saveState 调用 |
| S-13 | 严重 | — | `skill_manager.go` | 缺少 `_normalize_state`——加载状态时未规范化 |
| S-14 | 严重 | — | `skill_manager.go` | 缺少 `_assert_import_local_download_url_allowed`——远程导入无白名单校验 |
| S-15 | 严重 | 9.38-49 | `skilldev/schema.go` | `CalcStats` 标准差公式使用总体标准差(n)，Python 用样本标准差(n-1) |
| S-16 | 严重 | 9.79 | `store_projection.go` | `normalizeSummaryText` 用 `len(string)` 按字节截断，中文会乱码 |
| S-17 | 严重 | 9.79 | `tracker.go` | `RecordPresented` 缺少顶层异常保护，Python 有 try-except 静默处理 |
| S-18 | 严重 | 10.3.7-11 | `code_adapter.go` | 缺失 `_update_rails_for_mode`——code 模式切换时 rail 生命周期管理完全缺失 |
| S-19 | 严重 | 10.3.7-11 | `code_adapter.go` | 缺失 `_update_runtime_config` 完整逻辑——Rail 状态同步/工具更新全部缺失 |
| S-20 | 严重 | 10.3.2 | `deep_adapter.go` | 缺失 `load_user_rails`——用户自定义 hooks 无法加载 |
| S-21 | 严重 | — | `deep_agent.go` | `loadHarnessConfig`/`unloadHarnessConfig` 计划 9.3 ✅ 但仍返回 error |
| S-22 | 严重 | — | `skill_manager.go` | TeamSkillsHub Publish 使用错误 API 端点（`/artifacts` → `/plugins`） |
| S-23 | 严重 | — | `skill_manager.go` | TeamSkillsHub Delete 使用错误 API 端点和参数名 |
| S-24 | 严重 | — | `skill_manager.go` | TeamSkillsHub Publish 缺少 auth 和版本参数 |
| S-25 | 严重 | — | `skill_manager.go` | `_safe_child_path` 路径遍历保护缺失 |
| S-26 | 严重 | — | `skill_manager.go` | HandleSkillsUninstall 缺少 `_remove_local_skill` 调用 |
| **一般** | | | | |
| M-01 | 一般 | 10.3.2 | `deep_adapter.go` | `llm_output` 缺少 hasStreamedContent=true 赋值（S-01 根因） |
| M-02 | 一般 | 10.3.2 | `deep_adapter.go` | `llm_output` 中 accumulatedText 累加逻辑与 Python 不一致 |
| M-03 | 一般 | 9.38 | `bash_stream.go` | 缺少显式 session 空值守卫，与 bash.go 风格不一致 |
| M-04 | 一般 | 9.38 | `bash.go` | `buildHistoryPathFromOpts` 被调用两次导致 opts 重复遍历 |
| M-05 | 一般 | 9.24 | `evolution_rail.go` | `DrainPendingHostEvents` 超时逻辑简化过度，注释标注"后续补充" |
| M-06 | 一般 | 9.24 | `evolution_rail.go` | `SetTrajectorySink` 缺少 `_DEFAULT_MEMBER_ROLE` 回退逻辑 |
| M-07 | 一般 | 9.24 | `extension.go` | `EvolutionExtension` 接口缺少 `GetEvolutionTotalTimeoutSecs` 方法 |
| M-08 | 一般 | 10.5 | `owner_scopes.go` | `check_avatar_permission` 缺少 `session_id` 参数 |
| M-09 | 一般 | 10.5 | `owner_scopes.go` | `check_avatar_permission` 缺少 `EvaluateGlobalPolicyDirectly` 错误处理 |
| M-10 | 一般 | 10.5 | `owner_scopes.go` | 日志缺少 `owner_scopes_keys` 字段 |
| M-11 | 一般 | 10.5 | `models.go` | `PermissionSceneHookFn` 和 `RequestPermissionConfirmationHook` 同步 vs Python 异步 |
| M-12 | 一般 | 10.5 | `models.go` | `ToolPermissionHost` 字段非 Optional，`PermissionYAMLPath` 空串 vs None |
| M-13 | 一般 | 10.5 | `policy.go` | `resolveProjectDir` 缺少 `instance_overrides` 回退源 |
| M-14 | 一般 | 10.5 | `policy.go` | `CreateSysOperationFromCard` 失败后直接返回 nil，Python 降级继续 |
| M-15 | 一般 | 10.5 | `policy.go` | `createSysOperation` 整体缺少 panic 保护，Python 有 try/except |
| M-16 | 一般 | 10.3.25 | `stream_utils.go` | 缺失 `chat.ask_user_question` chunk 类型处理 |
| M-17 | 一般 | 10.3.25 | `stream_utils.go` | 缺失 dot-namespace 通用处理（team.xxx/context.xxx 等） |
| M-18 | 一般 | 10.3.25 | `stream_utils.go` | 缺失 `_serialize_chunk_recursive`，嵌套 time.Time 不会被序列化 |
| M-19 | 一般 | 10.3.25 | `stream_utils.go` | `SerializeValue` 缺少 Enum 类型处理 |
| M-20 | 一般 | 10.3.25 | `stream_utils.go` | `ask_user_question` 去重逻辑不在 Python `parse_stream_chunk` 中 |
| M-21 | 一般 | 10.5 | `extensions/` | HookEvent 常量写死字符串，未通过 `BuildEventName` 生成 |
| M-22 | 一般 | — | `skill_manager.go` | `HandleSkillsClawhubSearch` detail_key 与 Python 不一致 |
| M-23 | 一般 | — | `skill_manager.go` | `HandleSkillsImportLocal` 缺少单文件导入支持 |
| M-24 | 一般 | — | `skill_manager.go` | 缺少 `normalize_marketplaces` 方法 |
| M-25 | 一般 | — | `skill_manager.go` | 缺少 `_collect_existing_local_skill_names` 方法 |
| M-26 | 一般 | — | `skilldev/pipeline.go` | Pipeline 事件转发 goroutine 可能丢事件（Emit 使用 select+default 丢弃策略） |
| M-27 | 一般 | 9.79 | `tracker.go` | `evaluate_presented` 缺少顶层 recover，Python 有 try-except |
| M-28 | 一般 | 9.79 | `tracker.go` | 包级 map `sessionPresentedRecords`/`sessionEvalCounter` 无并发保护 |
| M-29 | 一般 | 7.21 | `migration/doc.go` | doc.go 未标注 run_migrations.go 及 5 个 Migrator 的缺失 |
| M-30 | 一般 | 10.3.7-11 | `code_adapter.go` | 缺失 `configure_team_member_agent` 方法 |
| M-31 | 一般 | 10.3.7-11 | `code_adapter.go` | 缺失 `merge_member_mcp_configs` 方法 |
| M-32 | 一般 | 10.3.7-11 | `code_adapter.go` | `_RAIL_BUILD_NAMES` 映射不完整 |
| M-33 | 一般 | 9.38-49 | `skilldev/*.go` | 多处重复工具函数：nowISO/joinStrings/walkFilesSorted/readSkillFiles/toInt |
| M-34 | 一般 | 9.38-49 | `skilldev/evaluate_stage.go` | delta 计算类型断言失败时无日志提示 |
| M-35 | 一般 | 10.5 | `schema/permission.go` + `owner_scopes.go` | PermissionContext 在两个包中重复定义 |
| M-36 | 一般 | 10.5 | `permission_engine.go` | PermissionSceneHook 从 engine 调用时缺少 GoCtx |
| M-37 | 一般 | 9.38-49 | `skilldev/stages/*.go` | SkillDev 各阶段核心逻辑为占位实现 |
| **提示** | | | | |
| T-01 | 提示 | 10.3.2 | `deep_adapter.go` | `llm_usage` payload 字段结构与 Python 不同 |
| T-02 | 提示 | 10.3.2 | `deep_adapter.go` | 非 OutputSchema 帧直接跳过，Python 仍解析 |
| T-03 | 提示 | 9.38 | `bash.go` | 枚举区块与常量区块之间多了空行 |
| T-04 | 提示 | 9.24 | `evolution_rail.go` | `emitBackgroundOutcomeEvent` 缺少 `rail_kind="base"` 默认值 |
| T-05 | 提示 | 9.24 | `evolution_rail.go` | `triggerEvolution` 同步模式丢失 `cbc` 上下文 |
| T-06 | 提示 | 9.24 | `evolution_rail.go` | 缺少 `saveTrajectory` 独立方法 |
| T-07 | 提示 | 9.24 | `evolution_rail.go` | `normalizeSkillNamesGo` 多余包装函数 |
| T-08 | 提示 | 10.5 | `models.go` | `MatchWildcard` 参数顺序与 Python `match_pattern` 相反 |
| T-09 | 提示 | 10.5 | `models.go` | `ApprovalOverrideEntry.Tools` 空序列化行为差异 |
| T-10 | 提示 | 10.5 | `policy.go` | `recordRWBind` permissions 参数未标注不使用 |
| T-11 | 提示 | 10.5 | `policy.go` | `BuildFilesystemPolicy` 返回 error 未包装 os.ErrNotExist |
| T-12 | 提示 | 10.5 | `policy.go` | `ListAutoManagedSandboxPaths` agent_skills 排序位置与 Python 不一致 |
| T-13 | 提示 | 10.5 | `policy.go` | 环境变量名 JIUSWARM→UAPCLAW 迁移确认 |
| T-14 | 提示 | 10.5 | `extensions/` | `CryptoUtility.GetCrypto()` 返回 `any`，Python 返回 `CryptoProvider` |
| T-15 | 提示 | 10.5 | `extensions/` | `BaseExtensionImpl.LoadConfigFromYAML` 返回 nil vs Python 返回 {} |
| T-16 | 提示 | 10.3.25 | `stream_utils.go` | 缺失 `chat.tracer_agent` 特殊处理 |
| T-17 | 提示 | — | `skill_manager.go` | SkillNet 相关方法为 stub 实现 |
| T-18 | 提示 | 9.38-49 | `skilldev/schema.go` | `CalcStats` 缺少 round 四舍五入到 4 位小数 |
| T-19 | 提示 | 9.38-49 | `skilldev/validate_stage.go` | 使用 `fmt.Sprintf` 构建路径而非 `filepath.Join` |
| T-20 | 提示 | 9.79 | `user_mem_store.go` | `BatchGet` 中 json.Unmarshal 错误被静默吞掉 |
| T-21 | 提示 | 9.79 | `store_projection.go` | `clear_rendered_outputs` 需两次遍历（功能正确） |

---

## 三、问题详情

---

### S-01：hasStreamedContent 传参逻辑错误——始终为 false

**章节**：10.3.2 DeepAdapter
**文件**：`internal/swarm/server/adapter/deep_adapter.go`
**严重级别**：严重

**问题描述**：Go 用 `accumulatedText != ""` 模拟 Python 的 `has_streamed_content` 标志位，但在 default 分支中 `accumulatedText` 已经被 flush 重置为 `""`，导致 `accumulatedText != ""` 永远为 false。Python 中 `has_streamed_content` 是 bool 标志位，一旦在 `llm_output` 分支设为 `True`，后续所有调用都带 `_has_streamed_content=True`。

**Python 样例**：
```python
# interface_deep.py L4765-4773
if chunk_type == "llm_output":
    content = ...
    has_streamed_content = True  # ← 设置后不再重置
    ...
    continue

# 后续 answer/default 分支中
parsed = self._parse_stream_chunk(chunk, _has_streamed_content=True)
```

**Go 问题代码**：
```go
// deep_adapter.go L959-981
case "llm_output":
    accumulatedText += textContent   // ← 累加到 accumulatedText
    ...

default:
    if accumulatedText != "" {
        accumulatedText = ""  // ← 先 flush 重置为空
    }
    // ↓ accumulatedText 已被重置，永远为 ""
    parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs,
        d.interactionConverter, accumulatedText != "")
```

**修复方案**：在 goroutine 开头声明 `hasStreamedContent := false`，在 `llm_output` 分支设为 `true`，在 default/answer 分支传入：
```go
hasStreamedContent := false  // goroutine 开头

case "llm_output":
    ...
    hasStreamedContent = true  // ← 对齐 Python
    ...

default:
    ...
    parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs,
        d.interactionConverter, hasStreamedContent)
```

**流程示例**：
```
Python 流程: llm_output → has_streamed_content=True → answer → parse(has_streamed_content=True) → chat.final
Go 当前流程: llm_output → accumulatedText+="" → answer(default) → flush accumulatedText="" → parse(hasStreamedContent=false) → 错误 event_type
Go 修复后:  llm_output → hasStreamedContent=true → answer → parse(hasStreamedContent=true) → 正确 event_type
```

---

### S-02：缺少 `answer` 专用分支——answer 类型混入 default 处理

**章节**：10.3.2 DeepAdapter
**文件**：`internal/swarm/server/adapter/deep_adapter.go`
**严重级别**：严重

**问题描述**：Python 对 `answer` 类型有专门的分支，在调用 `_parse_stream_chunk` 前先 flush accumulatedText/accumulatedReasoning，并根据 `has_streamed_content` 条件决定是否传 `_has_streamed_content=True`。Go 没有 `case "answer":` 分支，`answer` 类型落入 `default` 分支，flush 逻辑和 hasStreamedContent 传参都不正确。

**Python 样例**：
```python
# interface_deep.py L4793-4835
if chunk_type == "answer":
    if accumulated_text:
        yield AgentResponseChunk(payload={"event_type": "chat.delta", "content": accumulated_text})
        accumulated_text = ""
    if accumulated_reasoning:
        yield AgentResponseChunk(payload={"event_type": "chat.reasoning", "content": accumulated_reasoning})
        accumulated_reasoning = ""
    if has_streamed_content:
        parsed = self._parse_stream_chunk(chunk, _has_streamed_content=True)
    else:
        parsed = self._parse_stream_chunk(chunk)
    ...
    continue
```

**Go 问题代码**：
```go
// 无 case "answer": 分支，落入 default
default:
    if accumulatedText != "" {
        outCh <- schema.NewAgentResponseChunk(...)
        accumulatedText = ""
    }
    // hasStreamedContent 传参逻辑错误（见 S-01）
    parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs,
        d.interactionConverter, accumulatedText != "")
```

**修复方案**：增加 `case "answer":` 分支，对齐 Python 的 flush + 条件传参逻辑：
```go
case "answer":
    if accumulatedText != "" {
        outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
            "event_type": "chat.delta", "content": accumulatedText,
        })
        accumulatedText = ""
    }
    if accumulatedReasoning != "" {
        outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, map[string]any{
            "event_type": "chat.reasoning", "content": accumulatedReasoning,
        })
        accumulatedReasoning = ""
    }
    parsed := utils.ParseStreamChunk(output, usage, emittedAskUserIDs,
        d.interactionConverter, hasStreamedContent)
    if parsed != nil {
        outCh <- schema.NewAgentResponseChunk(req.RequestID, req.ChannelID, parsed)
    }
```

---

### S-03：`context.compression_state` event_type 错误 + 缺少字段提取

**章节**：10.3.25 Server Utils
**文件**：`internal/swarm/server/utils/stream_utils.go`
**严重级别**：严重

**问题描述**：
1. event_type 不一致：Python 返回 `"context.compression_state"`，Go 返回 `"chat.context_compression_state"`
2. Python 提取 5 个结构化字段（status/phase/processor/summary/operation_id），Go 只是透传整个 payload 到 `compression_state` 键
3. Python payload_dict 为空时不返回此事件，Go 无此判断

**Python 样例**：
```python
# stream_utils.py L113-131
if chunk_type == "context.compression_state":
    ...
    return {
        "event_type": "context.compression_state",
        "status": payload_dict.get("status", ""),
        "phase": payload_dict.get("phase", ""),
        "processor": payload_dict.get("processor", ""),
        "summary": payload_dict.get("summary", ""),
        "operation_id": payload_dict.get("operation_id", ""),
    }
```

**Go 问题代码**：
```go
// stream_utils.go L250-255
case "context.compression_state":
    return map[string]any{
        "event_type":        "chat.context_compression_state",
        "compression_state": payload,
    }
```

**修复方案**：
```go
case "context.compression_state":
    payloadDict, _ := payload.(map[string]any)
    if payloadDict == nil || len(payloadDict) == 0 {
        return nil  // 对齐 Python: payload 为空时不返回
    }
    return map[string]any{
        "event_type":   "context.compression_state",
        "status":       toStringOrEmpty(payloadDict["status"]),
        "phase":        toStringOrEmpty(payloadDict["phase"]),
        "processor":    toStringOrEmpty(payloadDict["processor"]),
        "summary":      toStringOrEmpty(payloadDict["summary"]),
        "operation_id": toStringOrEmpty(payloadDict["operation_id"]),
    }
```

---

### S-04：`controller_output` 交互传递只取 interactions[0]

**章节**：10.3.25 Server Utils
**文件**：`internal/swarm/server/utils/stream_utils.go`
**严重级别**：严重

**问题描述**：Python 将整个 interactions 列表传给 `_parse_interaction_payload`，该函数内部迭代处理。Go 只取 `interactions[0]`，当 `controller_output` 包含多个交互时（如同时有 permission interrupt 和 ask_user interrupt），Go 只处理第一个。

**Python 样例**：
```python
# stream_utils.py L151-153
interactions = _find_interaction_payloads(payload)
if interactions:
    return _parse_interaction_payload(interactions)  # 传整个列表
```

**Go 问题代码**：
```go
// stream_utils.go L77-81
interactions := FindInteractionPayloads(payload)
if len(interactions) > 0 {
    if p, ok := interactions[0].(map[string]any); ok {
        return ParseInteractionPayload(p, converter)  // 只传第一个元素
    }
}
```

**修复方案**：改为传递整个 interactions 列表，在 `ParseInteractionPayload` 中迭代处理：
```go
interactions := FindInteractionPayloads(payload)
if len(interactions) > 0 {
    return ParseInteractionPayloads(interactions, converter)
}
```

---

### S-05：`safeRunEvolution` 缺少超时机制

**章节**：9.24 EvolutionRail
**文件**：`internal/agentcore/harness/rails/evolution/evolution_rail.go`
**严重级别**：严重

**问题描述**：Go 版本完全没有实现超时机制。Python 有 `_get_evolution_total_timeout_secs()` 返回 `Optional[float]`，子类可覆写；当超时时发出 `"timed_out"` 状态事件并记录日志。

**Python 样例**：
```python
# evolution_rail.py L601-631
async def _safe_run_evolution(self, snapshot: dict) -> None:
    total_timeout = self._get_evolution_total_timeout_secs()
    if total_timeout is None:
        async with self._evolution_sem:
            await self.run_evolution(trajectory, ctx=None, snapshot=snapshot)
    else:
        async with asyncio.timeout(total_timeout):
            async with self._evolution_sem:
                await self.run_evolution(trajectory, ctx=None, snapshot=snapshot)
    except TimeoutError:
        outcome = {"status": "timed_out", "message": f"background evolution timed out after {timeout_text}s"}
```

**Go 问题代码**：
```go
// evolution_rail.go L671-688
func (r *EvolutionRail) safeRunEvolution(ctx context.Context, snapshot *EvolutionSnapshot) error {
    r.evolutionSem <- struct{}{}
    defer func() { <-r.evolutionSem }()
    traj := snapshot.Trajectory
    err := r.ext.RunEvolution(ctx, traj, snapshot)
    // 无超时处理
}
```

**修复方案**：
1. 在 `EvolutionExtension` 接口增加 `GetEvolutionTotalTimeoutSecs() *float64` 方法（noOpExtension 返回 nil）
2. `safeRunEvolution` 中根据返回值决定是否用 `context.WithTimeout` 包裹
3. 捕获 `context.DeadlineExceeded`，发出 `{"status": "timed_out", ...}` 事件

```go
func (r *EvolutionRail) safeRunEvolution(ctx context.Context, snapshot *EvolutionSnapshot) error {
    r.evolutionSem <- struct{}{}
    defer func() { <-r.evolutionSem }()

    if timeoutSecs := r.ext.GetEvolutionTotalTimeoutSecs(); timeoutSecs != nil {
        var cancel context.CancelFunc
        ctx, cancel = context.WithTimeout(ctx, time.Duration(*timeoutSecs*float64(time.Second)))
        defer cancel()
    }

    traj := snapshot.Trajectory
    err := r.ext.RunEvolution(ctx, traj, snapshot)
    if err != nil {
        if ctx.Err() == context.DeadlineExceeded {
            timeoutText := fmt.Sprintf("%.0f", *r.ext.GetEvolutionTotalTimeoutSecs())
            outcome := map[string]string{"status": "timed_out", "message": fmt.Sprintf("background evolution timed out after %ss", timeoutText)}
            logger.Warn(logComponent).Str("timeout_secs", timeoutText).Msg("后台演化超时")
            r.emitBackgroundOutcomeEvent(outcome)
            return nil
        }
        ...
    }
    return nil
}
```

---

### S-06：`safeRunEvolution` 缺少 `timed_out` 状态处理

**章节**：9.24 EvolutionRail
**文件**：`internal/agentcore/harness/rails/evolution/evolution_rail.go`
**严重级别**：严重

与 S-05 关联，Go 只有 `"failed"` 状态，缺少 `"timed_out"` 状态。详见 S-05 修复方案。

**Python 样例**：
```python
# evolution_rail.py L618-628
except TimeoutError:
    outcome = {"status": "timed_out", "message": f"background evolution timed out after {timeout_text}s"}
    logger.warning("[EvolutionRail] background evolution timed out after %ss", timeout_text)
except Exception as exc:
    outcome = {"status": "failed", "message": str(exc)}
    logger.warning("[EvolutionRail] background evolution failed: %s", exc)
```

---

### S-07：`check_avatar_permission` severity 比较逻辑错误

**章节**：10.5 Permissions
**文件**：`internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`
**严重级别**：严重

**问题描述**：当 `EvaluateGlobalPolicyDirectly` 返回 `PermissionLevelNone`（对应 Python `None`），`globalLevelStr` 为 `"none"`，`severityMap["none"]` 返回 0（map 零值），导致 severity 比较逻辑错误——`PermissionLevelNone` 的 severity 被当作 0（最宽松），而 Python `_severity.get(global_level_value, 2)` 对未知值默认取最严格（2=deny）。

**Python 样例**：
```python
# owner_scopes.py L146-165
global_level_value = global_level.value if global_level is not None else None
if global_level_value is not None and _severity.get(global_level_value, 2) > _severity.get(level, 2):
    final_level = global_level_value
# global_level_value is None → 跳过 severity 比较
# 未知值 → severity=2(deny)，最严格
```

**Go 问题代码**：
```go
// owner_scopes.go L235-256
globalLevel, _ := engine.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
globalLevelStr := globalLevel.String()
// 无错误处理
if level == "" {
    if globalLevel == harnesssecurity.PermissionLevelAllow {
        return "allow", nil
    }
    return "deny", nil
}
globalSeverity, globalOk := severityMap[globalLevelStr]
if globalOk && globalSeverity > severityMap[level] {
    finalLevel = globalLevelStr
}
```

**修复方案**：
1. `PermissionLevelNone` 时跳过 severity 比较（对齐 Python `None`）
2. 对未知 level 值，severity 默认应取 2（最严）而非 0：
```go
globalLevel, _ := engine.EvaluateGlobalPolicyDirectly(toolName, toolArgs, true)
if globalLevel == harnesssecurity.PermissionLevelNone {
    // 对齐 Python: global_level is None → 跳过 severity 比较
    goto decideWithLevel
}
globalLevelStr := globalLevel.String()
globalSeverity := 2 // 未知值默认最严格，对齐 Python _severity.get(value, 2)
if s, ok := severityMap[globalLevelStr]; ok {
    globalSeverity = s
}
if globalSeverity > severityMap[level] {
    finalLevel = globalLevelStr
}
```

---

### S-08：`ParsePermissionLevel("none")` 不应返回 PermissionLevelNone

**章节**：10.5 Permissions
**文件**：`internal/agentcore/harness/security/models.go`
**严重级别**：严重

**问题描述**：Python 的 `PermissionLevel` 只有 ALLOW/ASK/DENY 三个值，用 `None` 表示"无匹配规则"。Go 新增了 `PermissionLevelNone` 作为枚举值且 `ParsePermissionLevel("none")` 允许从字符串 "none" 解析，这在 Python 端是不可能的——YAML 配置中出现 `"none"` 值时行为不一致。

**Python 样例**：
```python
# models.py L19-28
class PermissionLevel(str, Enum):
    ALLOW = "allow"
    ASK = "ask"
    DENY = "deny"
    # 无 NONE 成员
```

**Go 问题代码**：
```go
// models.go L189-199
const (
    PermissionLevelNone PermissionLevel = iota  // Go 额外添加
    PermissionLevelAllow
    PermissionLevelAsk
    PermissionLevelDeny
)
```

**修复方案**：`ParsePermissionLevel` 对 "none" 输入不应返回 `PermissionLevelNone`，应返回错误或特殊处理。`PermissionLevelNone` 仅作内部哨兵使用。

---

### S-09：`setPluginEnabled` 类型断言错误——永远无法匹配已安装插件

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：`installed_plugins` 的实际类型是 `[]any`（JSON 反序列化后每项是 `map[string]any`），但 Go 代码用 `[]map[string]any` 做类型断言，永远失败，导致所有插件的 enable/disable 操作都返回 "未找到插件"。

**Python 样例**：
```python
# skill_manager.py
def _set_plugin_enabled(self, name: str, enabled: bool) -> bool:
    plugins = self._state.get("installed_plugins", [])
    for p in plugins:
        if p.get("name") == name:
            p["enabled"] = bool(enabled)
            updated = True
            break
```

**Go 问题代码**：
```go
// skill_manager.go L2148
plugins, _ := sm.state["installed_plugins"].([]map[string]any)  // ← 永远为 nil
```

**修复方案**：先断言为 `[]any` 再遍历：
```go
func (sm *SkillManager) setPluginEnabled(name string, enabled bool) bool {
    raw, ok := sm.state["installed_plugins"]
    if !ok { return false }
    list, ok := raw.([]any)
    if !ok { return false }
    for _, item := range list {
        if p, ok := item.(map[string]any); ok {
            if toString(p["name"]) == name {
                p["enabled"] = enabled
                return true
            }
        }
    }
    return false
}
```

---

### S-10：`HandlePluginsEnable/Disable` 锁外 saveState

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Go 版本在 `HandlePluginsEnable` 中加锁修改 state → 解锁 → 锁外 `saveState()`，存在短暂窗口期状态不一致。Python 的 `_set_plugin_enabled` 内部调 `_save_state()`，在同一个逻辑步骤中完成。

**Python 样例**：
```python
def _set_plugin_enabled(self, name, enabled):
    ...
    self._save_state()  # 在方法内保存
    return True
```

**Go 问题代码**：
```go
func (sm *SkillManager) HandlePluginsEnable(...) {
    sm.mu.Lock()
    ok := sm.setPluginEnabled(name, true)  // 不含 saveState
    sm.mu.Unlock()
    if !ok { ... }
    sm.saveState()  // ← 锁外保存，有窗口期
```

**修复方案**：将 `saveState()` 移到锁内，或让 `setPluginEnabled` 内部调 `saveState()` 对齐 Python。

---

### S-11：`addLocalSkill` 缺少去重逻辑和 saveState 调用

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Python 的 `_add_local_skill` 会检查同名 skill 是否已存在，若存在则替换，且每次都调用 `_save_state()`。Go 版本只是追加，不去重，也不保存。

**Python 样例**：
```python
def _add_local_skill(self, skill):
    local = self._state.setdefault("local_skills", [])
    for i, s in enumerate(local):
        if s.get("name") == skill.get("name"):
            local[i] = skill
            self._save_state()
            return
    local.append(skill)
    self._save_state()
```

**Go 问题代码**：
```go
func (sm *SkillManager) addLocalSkill(skill map[string]any) {
    ...
    list = append(list, skill)  // ← 永远追加，不去重
    sm.state["local_skills"] = list
    // ← 没有 saveState
}
```

**修复方案**：增加去重检查和 `sm.saveState()` 调用。

---

### S-12：`removeInstalledPlugin` 缺少 saveState 调用

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

与 S-11 同类问题，`removeInstalledPlugin` 修改 state 后未调用 `saveState()`。

**Python 样例**：
```python
def _remove_installed_plugin(self, name):
    plugins = self._state.get("installed_plugins", [])
    self._state["installed_plugins"] = [p for p in plugins if p.get("name") != name]
    self._save_state()  # ← 保存
```

---

### S-13：缺少 `_normalize_state`——加载状态时未规范化

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Python 的 `_load_state` 加载 JSON 后调用 `_normalize_state`，该方法补全缺失的键、过滤无效 marketplace 条目、清理已删除的 local_skills 记录。Go 的 `loadState` 仅返回原始 JSON 解析结果，没有规范化步骤。

**Python 样例**：
```python
def _normalize_state(self):
    self._state.setdefault("marketplaces", [])
    self._state.setdefault("installed_plugins", [])
    self._state.setdefault("local_skills", [])
    self._state.setdefault("skill_configs", [])
    # 规范化 marketplaces
    self._state["marketplaces"] = self.normalize_marketplaces(...)
    # 规范化 local_skills（只保留目录仍存在的记录）
    existing = self._collect_existing_local_skill_names()
    self._state["local_skills"] = [s for s in local if s.get("name") in existing]
```

**修复方案**：添加 `normalizeState` 方法，在 `loadState` 末尾调用。

---

### S-14：远程导入缺少白名单校验

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`（`remote_import.go`）
**严重级别**：严重

**问题描述**：Python 的 `_import_skill_from_remote_archive` 在下载前调用 `_assert_import_local_download_url_allowed(download_url)` 校验 URL scheme 必须为 HTTPS 且 host 在白名单中。Go 版本完全缺少此校验，任意 HTTP URL 都可被下载。

**Python 样例**：
```python
self._assert_import_local_download_url_allowed(download_url)
# 必须是 https，host 必须匹配白名单
```

**Go 问题代码**：
```go
func importSkillFromRemoteArchive(...) {
    // ← 直接下载，没有白名单校验
    archivePath, err := downloadArchive(ctx, downloadURL, tmpDir)
```

**修复方案**：在下载前添加白名单校验，复用 `assertTeamSkillsHubDownloadURLAllowed` 的模式或新增 `assertImportLocalDownloadURLAllowed`。

---

### S-15：`CalcStats` 标准差公式错误

**章节**：9.38-49 SkillDev
**文件**：`internal/swarm/server/runtime/skill/skilldev/schema.go`
**严重级别**：严重

**问题描述**：Go 使用总体标准差（除以 n），Python 使用样本标准差（除以 n-1，Bessel 校正）。导致评测 benchmark 数据与 Python 不一致。

**Python 样例**：
```python
# schema.py L316-327
stddev = math.sqrt(sum((x - mean) ** 2 for x in values) / (n - 1)) if n > 1 else 0.0
```

**Go 问题代码**：
```go
// schema.go L729-758
variance /= float64(n)  // ← 应为 n-1
```

**修复方案**：
```go
if n > 1 {
    variance /= float64(n - 1)
} else {
    variance = 0
}
```

---

### S-16：`normalizeSummaryText` 按字节截断导致中文乱码

**章节**：9.79 Checkpointing
**文件**：`internal/evolving/checkpointing/store_projection.go`
**严重级别**：严重

**问题描述**：Go 的 `len(string)` 返回字节数而非字符数。如果文本包含中文等多字节字符，会在错误的边界截断，甚至在 UTF-8 多字节序列中间切断导致乱码。

**Python 样例**：
```python
# store_projection.py
if len(value) > max_chars:
    return value[: max_chars - 3].rstrip() + "..."
# Python len() 对 str 返回 Unicode 码点数
```

**Go 问题代码**：
```go
// store_projection.go
if len(value) > maxChars {
    return strings.TrimRight(value[:maxChars-3], " ") + "..."
}
```

**修复方案**：
```go
runes := []rune(value)
if len(runes) > maxChars {
    return strings.TrimRight(string(runes[:maxChars-3]), " ") + "..."
}
```

---

### S-17：`RecordPresented` 缺少顶层异常保护

**章节**：9.79 Experience
**文件**：`internal/evolving/experience/tracker.go`
**严重级别**：严重

**问题描述**：Python 的 `record_presented` 用 try-except 包裹，静默处理所有异常（这只是追踪辅助功能）。Go 版本返回 error，调用方如果不处理会导致上层失败，而 Python 永远不会因此失败。

**Python 样例**：
```python
# tracker.py
async def record_presented(self, *, session, skill_name, presentation_snippet) -> None:
    try:
        ...
    except Exception as exc:
        logger.debug("[ExperienceTracker] track presented records failed: %s", exc)
```

**Go 问题代码**：
```go
// tracker.go
func (t *ExperienceTracker) RecordPresented(ctx context.Context, sessionID string, skillName string, presentationSnippet string) error {
    ...
    if _, err := t.store.UpdateRecordScores(ctx, skillName, updatesToMap(updates)); err != nil {
        logger.Error(logComponent).Str("skill", skillName).Err(err).Msg("[ExperienceTracker] update_record_scores 失败")
        return err  // ← 返回 error 而非静默处理
    }
```

**修复方案**：在 `RecordPresented` 方法内部捕获所有 panic 和 error，仅记录日志不向上传播（对齐 Python 静默行为），或改为 defer-recover + 降级日志。

---

### S-18：缺失 `_update_rails_for_mode`——code 模式 rail 生命周期管理缺失

**章节**：10.3.7-11 适配器辅助
**文件**：`internal/swarm/server/adapter/code_adapter.go`
**严重级别**：严重

**问题描述**：Python `JiuwenClawCodeAdapter._update_rails_for_mode(mode)` 在 code 模式下卸载 TaskPlanningRail/SkillEvolutionRail，保留 SubagentRail/ProjectMemoryRail/CodingMemoryRail。Go 中 `updateRailsForMode` 只有占位日志，无实际逻辑，导致 code 模式切换时 rail 生命周期管理完全缺失。

**Python 样例**：
```python
# interface_code.py L770-824
def _update_rails_for_mode(self, mode):
    if mode == "code":
        self._remove_rail_by_type(TaskPlanningRail)
        self._remove_rail_by_type(SkillEvolutionRail)
        # 保留 SubagentRail, ProjectMemoryRail, CodingMemoryRail
```

**修复方案**：在 CodeAdapter 上实现 `updateRailsForMode`，对齐 Python 的 rail 增删逻辑。

---

### S-19：缺失 `_update_runtime_config` 完整逻辑

**章节**：10.3.7-11 适配器辅助
**文件**：`internal/swarm/server/adapter/deep_adapter_config.go`
**严重级别**：严重

**问题描述**：Python 的 `_update_runtime_config()` 包含 CWD 种子、language/channel 解析、RuntimePromptRail 状态同步（7 个 set 方法）、ProjectMemoryRail 状态同步、_write_runtime_state、_update_rails_for_mode、_update_tools_for_mode 等 15+ 步骤。Go 版本只有 CWD/language/channel/project_dir/workspace_dir 写入，缺失全部 Rail 状态同步和工具更新逻辑。

**Python 样例**（节选）：
```python
# interface_code.py L852-911
def _update_runtime_config(self):
    self._seed_cwd_from_context()
    self._runtime_prompt_rail.set_language(self._language)
    self._runtime_prompt_rail.set_force_english(self._force_english)
    self._runtime_prompt_rail.set_channel(self._channel)
    self._runtime_prompt_rail.set_model_name(self._model_name)
    self._runtime_prompt_rail.set_mode(self._mode)
    self._runtime_prompt_rail.set_trusted_dirs(self._trusted_dirs)
    self._runtime_prompt_rail.set_runtime_paths(self._runtime_paths)
    self._project_memory_rail.set_language(self._language)
    self._project_memory_rail.set_additional_directories(...)
    self._write_runtime_state()
    self._update_rails_for_mode(self._mode)
    self._update_tools_for_mode(self._mode)
    self._update_session_tools()
    self._refresh_acp_runtime_tools()
    self._update_prompt_for_mode(self._mode)
    # user_todos channel_id sync
```

**修复方案**：对照 Python 实现完整 runtime config 更新链，当前所有 Rail 返回 nil（⤵️ 10.6.3-10），需在 Rail 实现后回填此方法。

---

### S-20：缺失 `load_user_rails`——用户自定义 hooks 无法加载

**章节**：10.3.2 DeepAdapter
**文件**：`internal/swarm/server/adapter/deep_adapter.go`
**严重级别**：严重

**问题描述**：Python 在 `create_instance` 末尾调用 `await self.load_user_rails()`，从配置文件动态加载用户自定义 Rail 扩展。Go 步骤 24/25 标记 `⤵️ 10.6.3-10`，未实现，导致用户自定义 hooks 无法加载。

**Python 样例**：
```python
# interface_deep.py
await self.load_user_rails()  # create_instance 末尾
```

**修复方案**：实现动态加载用户自定义 Rail 扩展。依赖 10.5.7 ExtensionLoader。

---

### S-21：`loadHarnessConfig`/`unloadHarnessConfig` 计划 ✅ 但仍返回 error

**章节**：9.3 DeepAgentConfig
**文件**：`internal/agentcore/harness/deep_agent.go`
**严重级别**：严重

**问题描述**：IMPLEMENTATION_PLAN.md 中 9.3 标记为 ✅，但 `deep_agent.go` 中的 `loadHarnessConfig` 和 `unloadHarnessConfig` 仍返回 `fmt.Errorf("尚未实现")`，调用方会得到运行时错误。这是计划状态与代码状态的严重不一致。

**修复方案**：要么实现这两个函数并确认测试通过，要么将计划中 9.3 的状态修正为 🔄。

---

### M-01 ~ M-34：一般问题

#### M-01：`llm_output` 缺少 hasStreamedContent=true 赋值
- **文件**：`deep_adapter.go`
- **问题**：这是 S-01 的根因。Python 在 `llm_output` 分支设 `has_streamed_content = True`，Go 缺失。

#### M-02：`llm_output` 中 accumulatedText 累加逻辑与 Python 不一致
- **文件**：`deep_adapter.go`
- **问题**：Python `llm_output` 直接 yield 不累加，Go 中 `accumulatedText += textContent` 累加。`accumulatedText` 在 Python 中仅用于暂存非标准 chunk 文本。
- **修复**：移除 `accumulatedText += textContent`。

#### M-03：`bash_stream.go` 缺少显式 session 空值守卫
- **文件**：`internal/agentcore/harness/tools/shell/bash_stream.go`
- **问题**：bash.go 前台路径有 `if session != nil` 守卫，bash_stream.go 没有，风格不一致。功能正确（`buildHistoryPathFromOpts` 内部做了空值检查返回 `""`）。

#### M-04：`buildHistoryPathFromOpts` 被调用两次
- **文件**：`bash.go`
- **问题**：`buildHistoryPathFromOpts` 内部也调用 `tool.NewToolCallOptions(opts...)`，等于 opts 被遍历两次。
- **修复**：修改 `buildHistoryPathFromOpts` 接受 `*tool.ToolCallOptions` 参数。

#### M-05：`DrainPendingHostEvents` 超时逻辑简化过度
- **文件**：`evolution_rail.go`
- **问题**：注释明确写了"简化：不实现精确超时，后续补充"，无论超时与否都 delete 任务（Python 只删除已完成的），缺默认超时。

#### M-06：`SetTrajectorySink` 缺少 `_DEFAULT_MEMBER_ROLE` 回退
- **文件**：`evolution_rail.go`
- **问题**：Python 有 `_DEFAULT_MEMBER_ROLE` 类属性回退逻辑，Go 缺失。当 `memberRole` 未传入时 Go 不设置字段。

#### M-07：`EvolutionExtension` 接口缺少 `GetEvolutionTotalTimeoutSecs` 方法
- **文件**：`extension.go`
- **问题**：Python 中 `_get_evolution_total_timeout_secs` 是虚方法，被 `safeRunEvolution` 和 `drainPendingHostEvents` 调用。Go 接口缺失。
- **修复**：增加 `GetEvolutionTotalTimeoutSecs() *float64`，noOpExtension 返回 nil。

#### M-08：`check_avatar_permission` 缺少 `session_id` 参数
- **文件**：`owner_scopes.go`
- **问题**：Python 签名有 `session_id: str | None`，Go 完全没有。虽然 Python 当前未使用该参数，但它是接口契约的一部分。

#### M-09：`check_avatar_permission` 缺少 `EvaluateGlobalPolicyDirectly` 错误处理
- **文件**：`owner_scopes.go`
- **问题**：Python 用 `try/except` 包裹，Go 没有任何错误处理。若评估函数异常，Go 会导致 severity 比较异常。

#### M-10：日志缺少 `owner_scopes_keys` 字段
- **文件**：`owner_scopes.go`
- **问题**：Python 日志含 `owner_scopes_keys` 字段（调试权限配置时很有用），Go 遗漏。

#### M-11：`PermissionSceneHookFn` 同步 vs Python 异步
- **文件**：`models.go`
- **问题**：Python 两个钩子都是 `Awaitable`，Go 版本为同步。如果钩子实现需要 I/O，Go 的同步签名会阻塞当前 goroutine。

#### M-12：`ToolPermissionHost` 字段非 Optional
- **文件**：`models.go`
- **问题**：Python 所有字段都是 `| None = None`，Go 的 `PermissionYAMLPath` 零值为 `""` 而非 `nil`，调用方需确认 `""` 与 Python `None` 等价。

#### M-13：`resolveProjectDir` 缺少 `instance_overrides` 回退
- **文件**：`deep_adapter_config.go`
- **问题**：Python 有两级回退（`self._project_dir` → `instance_overrides["project_dir"]`），Go 只检查 `d.projectDir`。

#### M-14：`CreateSysOperationFromCard` 失败后直接返回 nil
- **文件**：`builder.go`
- **问题**：Python 隔离键计算失败不阻止注册，Go 端 `NewSysOperation` 失败时直接返回 nil。

#### M-15：`createSysOperation` 整体缺少 panic 保护
- **文件**：`deep_adapter_config.go`
- **问题**：Python 有整体 `try/except Exception` 保护，Go 没有 `defer-recover`。

#### M-16：缺失 `chat.ask_user_question` chunk 类型
- **文件**：`stream_utils.go`
- **问题**：Python 处理 `"chat.ask_user_question"` 直接透传，Go 缺少此 case。

#### M-17：缺失 dot-namespace 通用处理
- **文件**：`stream_utils.go`
- **问题**：Python 对含 `.` 的 chunkType（如 `team.xxx`）保留原始 event_type，Go 会加 `chat.` 前缀。

#### M-18：缺失 `_serialize_chunk_recursive`
- **文件**：`stream_utils.go`
- **问题**：Python 递归序列化嵌套 dict/list 中的 datetime/Enum，Go 只处理顶层 time.Time。

#### M-19：`SerializeValue` 缺少 Enum 处理
- **文件**：`stream_utils.go`
- **问题**：Python 处理 `isinstance(value, Enum)` 返回 `value.value`，Go 缺失。

#### M-20：`ask_user_question` 去重逻辑位置不一致
- **文件**：`stream_utils.go`
- **问题**：Go 的去重在 `ParseStreamChunk` 中做，Python 的去重在 `process_message_stream_impl` 中做。

#### M-21：HookEvent 常量写死字符串
- **文件**：`extensions/hook_event.go`
- **问题**：Go 常量手动拼写，未通过 `BuildEventName` 生成。应在测试中验证 `BuildEventName(scope, name) == Constant`。

#### M-22：`HandleSkillsClawhubSearch` detail_key 不一致
- **文件**：`skill_manager.go`
- **问题**：Python 返回 `"skills.clawhub.errors.tokenNotConfigured"`，Go 返回 `"skills.clawhub.errors.noToken"`。

#### M-23：`HandleSkillsImportLocal` 缺少单文件导入
- **文件**：`skill_manager.go`
- **问题**：Python 支持单文件 SKILL.md 导入，Go 只支持目录导入。

#### M-24：缺少 `normalize_marketplaces` 方法
- **文件**：`skill_manager.go`
- **问题**：Python 过滤掉 name/url 为空的 marketplace 条目并补全 enabled 字段。

#### M-25：缺少 `_collect_existing_local_skill_names` 方法
- **文件**：`skill_manager.go`
- **问题**：Python 扫描磁盘实际存在的技能目录名，用于 `_normalize_state` 中清理已删除记录。

#### M-26：Pipeline Emit 可能丢事件
- **文件**：`skilldev/pipeline.go`
- **问题**：Go 的 `Emit` 使用 `select+default` 丢弃策略，Python 使用无限缓冲的 asyncio.Queue。如果事件产生速度超过消费速度，Go 会静默丢弃事件。

#### M-27：`evaluate_presented` 缺少顶层 recover
- **文件**：`tracker.go`
- **问题**：Python 有整体 try-except，Go 无 recover 保护，panic 会导致程序崩溃。

#### M-28：包级 map 无并发保护
- **文件**：`tracker.go`
- **问题**：`sessionPresentedRecords` 和 `sessionEvalCounter` 是包级 map，无 `sync.RWMutex` 保护。
- **修复**：添加 `sync.RWMutex` 或改用 `sync.Map`。

#### M-29：migration/doc.go 未标注缺失文件
- **文件**：`internal/agentcore/memory/migration/doc.go`
- **问题**：doc.go 未标注 `run_migrations.go` 和 5 个 Migrator 的缺失状态（`⤵️` 标记）。

#### M-30：缺失 `configure_team_member_agent`
- **文件**：`code_adapter.go`
- **问题**：Python 的 `configure_team_member_agent()` 将 code runtime profile 应用到 team member DeepAgent，Go 无对应方法。

#### M-31：缺失 `merge_member_mcp_configs`
- **文件**：`code_adapter.go`
- **问题**：Python 合并 code 模式 MCP 配置到 team member，Go 无对应方法。

#### M-32：`_RAIL_BUILD_NAMES` 映射不完整
- **文件**：`code_adapter.go`
- **问题**：缺少 LspRail、ProjectMemoryRail、CodingMemoryRail 三个条目。

#### M-33：SkillDev 多处重复工具函数
- **文件**：`skilldev/*.go`
- **问题**：`nowISO`（2 处）、`joinStrings`（应使用 `strings.Join`）、`walkFilesSorted`/`walkFiles`（2 处）、`readSkillFiles`（2 处）、`toIntFromAny`/`toInt`（2 处）重复定义。
- **修复**：提取共享工具函数。

#### M-34：delta 计算类型断言失败时无日志
- **文件**：`skilldev/evaluate_stage.go`
- **问题**：如果 `pass_rate` 不是 `map[string]any`，Go 静默得到 nil 并跳过，Python 会抛 KeyError。
- **修复**：在 delta 计算失败时添加 Warn 日志。

---

### T-01 ~ T-21：提示问题

（简要列出，详见各 agent 报告）

| 编号 | 文件 | 问题 |
|------|------|------|
| T-01 | `deep_adapter.go` | `llm_usage` payload 字段结构（展开字段 vs metadata 键）与 Python 不同 |
| T-02 | `deep_adapter.go` | 非 OutputSchema 帧直接 `continue` 跳过，Python 仍解析 |
| T-03 | `bash.go` | 枚举区块与常量区块之间多了空行（格式问题） |
| T-04 | `evolution_rail.go` | `emitBackgroundOutcomeEvent` 缺少 `rail_kind="base"` 默认值 |
| T-05 | `evolution_rail.go` | `triggerEvolution` 同步模式传 `context.Background()`，丢失 cbc 上下文 |
| T-06 | `evolution_rail.go` | 缺少 `saveTrajectory` 独立方法，子类无法覆写保存行为 |
| T-07 | `evolution_rail.go` | `normalizeSkillNamesGo` 多余包装函数（未使用） |
| T-08 | `models.go` | `MatchWildcard(value, pattern)` 参数顺序与 Python `match_pattern(pattern, value)` 相反 |
| T-09 | `models.go` | `ApprovalOverrideEntry.Tools` 空序列化行为差异（nil vs []） |
| T-10 | `policy.go` | `recordRWBind` 的 `permissions` 参数未标注"保留签名对称，不使用" |
| T-11 | `policy.go` | `BuildFilesystemPolicy` 返回 error 未包装 `os.ErrNotExist` |
| T-12 | `policy.go` | `ListAutoManagedSandboxPaths` agent_skills 排序位置与 Python 不一致 |
| T-13 | `policy.go` | 环境变量名 `JIUSWARM`→`UAPCLAW` 迁移确认 |
| T-14 | `extensions/` | `CryptoUtility.GetCrypto()` 返回 `any`，Python 返回 `CryptoProvider`（⤵️ 10.5.10） |
| T-15 | `extensions/` | `LoadConfigFromYAML` 返回 nil vs Python 返回 `{}` |
| T-16 | `stream_utils.go` | 缺失 `chat.tracer_agent` 特殊处理 |
| T-17 | `skill_manager.go` | SkillNet 相关方法为 stub（⤵️ 已标记） |
| T-18 | `skilldev/schema.go` | `CalcStats` 缺少 round 四舍五入到 4 位小数 |
| T-19 | `skilldev/validate_stage.go` | 使用 `fmt.Sprintf` 构建路径而非 `filepath.Join`（跨平台问题） |
| T-20 | `user_mem_store.go` | `BatchGet` 中 json.Unmarshal 错误被静默吞掉 |
| T-21 | `store_projection.go` | `clear_rendered_outputs` 需两次遍历（功能正确，效率略低） |

---

## 四、计划状态与代码不一致

| 章节 | 计划状态 | 实际代码 | 问题 |
|------|---------|---------|------|
| 9.3 DeepAgentConfig | ✅ | `loadHarnessConfig`/`unloadHarnessConfig` 返回 error | 严重不一致，函数仍为 stub |
| 9.38-49 Harness 工具集 | ✅ | `buildBuiltinToolGroup` 中 package/entry_point 类型仍返回 error | 部分不一致 |
| 9.19-23 Rails | ✅ | `buildBuiltinRail` 中 package/entry_point 类型仍返回 error | 部分不一致 |
| 9.24 EvolutionRail | 🔄 (P3-P6 ☐) | 超时机制/团队演化未实现 | 一致（P3+ 确实未完成） |
| 10.6.3-10 Swarm Rails | 🔄 | 多个 build*Rail 返回 nil | 一致 |
| 10.5.7-8 ExtensionLoader/Manager | ☐ ⤵️ | 有结构体和方法签名，方法体全部为空/返回错误 | 一致（确认是 stub） |

---

## 五、优先修复建议

### P0（立即修复——功能完全不可用/安全风险）

1. **S-09**：`setPluginEnabled` 类型断言 bug——所有插件 enable/disable 操作不可用
2. **S-22/S-23/S-24**：TeamSkillsHub API 端点错误 + 缺少 auth——Publish/Delete 完全不可用
3. **S-14**：远程导入无白名单校验（安全隐患）
4. **S-25**：`_safe_child_path` 路径遍历保护缺失（安全隐患）
5. **S-15**：`CalcStats` 标准差公式错误——评测数据与 Python 不一致
6. **S-16**：`normalizeSummaryText` 中文截断乱码
7. **S-21**：`loadHarnessConfig`/`unloadHarnessConfig` 计划 ✅ 但仍为 stub

### P1（尽快修复——核心功能影响）

8. **S-01/S-02/M-01/M-02**：DeepAdapter 流式处理 hasStreamedContent + answer 分支（一组关联修复）
9. **S-03/S-04**：ParseStreamChunk event_type 错误 + 交互传递逻辑
10. **S-07/S-08**：权限 severity 比较逻辑 + PermissionLevelNone 处理
11. **S-10/S-11/S-12/S-13/S-26**：SkillManager 状态持久化系列问题（saveState/去重/规范化/卸载清理）
12. **S-05/S-06/M-07**：EvolutionRail 超时机制系列问题
13. **S-17/M-27/M-28**：ExperienceTracker 异常保护 + 并发安全

### P2（计划修复——功能缺失但不阻塞核心流程）

11. **S-17/M-27/M-28**：ExperienceTracker 异常保护 + 并发安全
12. **S-18/S-19/S-20**：CodeAdapter/DeepAdapter 缺失方法（依赖 ⤵️ 10.6.3-10 回填）
13. **M-05/M-06**：EvolutionRail DrainPendingHostEvents + SetTrajectorySink
14. **M-16/M-17/M-18/M-19/M-20**：ParseStreamChunk 补充分支和递归序列化
15. **M-22~M-25**：SkillManager 细节对齐

---

## 六、⤵️ 占位代码审计总结

- **⤵️ 标记数量**：约 160+ 处，分布在 40+ 文件中
- **计划状态不匹配**：2 处 HIGH（9.3 ✅ 但代码仍为 stub；9.38-49 buildBuiltinToolGroup/Rail 部分 stub）
- **误标为 ⤵️ 的已实现代码**：未发现
- **应标但未标的缺失**：`run_migrations.go` + 5 个 Migrator（7.23 ☐），`normalizeState`（S-13）
- **最大集中区**：`deep_adapter.go`（~40+ 标记）、`agent_teams/`（~30+ 标记）

---

## 七、补充发现（后续 agent 深度审查合并）

### S-22：TeamSkillsHub Publish 使用错误 API 端点

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Go 的 Publish 使用 `POST /api/v1/artifacts`，Python 使用 `POST /api/v1/plugins`。端点不同导致 Publish 请求 404。

**Python 样例**：
```python
# skill_manager.py L2775
resp = await session.post(f"{base_url}/api/v1/plugins", ...)
```

**Go 问题代码**：
```go
// skill_manager.go L1772
url := fmt.Sprintf("%s/api/v1/artifacts", baseURL)
```

**修复方案**：将 `/api/v1/artifacts` 改为 `/api/v1/plugins`。

---

### S-23：TeamSkillsHub Delete 使用错误 API 端点和参数名

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Go 的 Delete 使用 `DELETE /api/v1/artifacts/{asset_id}`，Python 使用 `DELETE /api/v1/plugins/{skill_id}/versions/{version}`，且 Python 需要 auth 解析和 URL 编码。

**Python 样例**：
```python
# skill_manager.py L2818
resp = await session.delete(
    f"{base_url}/api/v1/plugins/{quote(skill_id)}/versions/{quote(version)}",
    headers=auth_headers,
)
```

**Go 问题代码**：
```go
// skill_manager.go L1814
url := fmt.Sprintf("%s/api/v1/artifacts/%s", baseURL, assetID)
```

**修复方案**：改为 `/api/v1/plugins/{skill_id}/versions/{version}`，补充 auth 和 URL 编码。

---

### S-24：TeamSkillsHub Publish 缺少 auth 和版本参数

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Go 版 Publish 缺少 auth 解析（Python 通过 `_resolve_teamskills_hub_auth` 获取 token 或 system_token），默认版本硬编码 "1.0.0"（Python 要求 `version` 参数），且 Go 不发送 multipart data 字段（`force`/`version_desc`/`plugin_version`/`plugin_id`）。

**修复方案**：补充 auth 解析逻辑、version 必填参数和 multipart data 字段。

---

### S-25：`_safe_child_path` 路径遍历保护缺失

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Python 的 `_safe_child_path(base, name, label)` 解析路径后验证仍在 base 目录内。Go 的 `safePathName` 只验证名称组件（无斜杠、非绝对路径），不验证解析后路径是否仍在 base 目录内，存在路径遍历风险。

**Python 样例**：
```python
# skill_manager.py L212-220
def _safe_child_path(base, name, label):
    resolved = (base / name).resolve()
    if not str(resolved).startswith(str(base.resolve())):
        raise ValueError(f"{label} path traversal")
    return resolved
```

**修复方案**：添加 `safeChildPath` 函数，解析路径后验证 `strings.HasPrefix(resolved, baseResolved)`。

---

### S-26：HandleSkillsUninstall 缺少 `_remove_local_skill` 调用

**章节**：10.3.19-20 Skill 管理
**文件**：`internal/swarm/server/runtime/skill/skill_manager.go`
**严重级别**：严重

**问题描述**：Go 版卸载只调 `removeInstalledPlugin`，Python 还调 `_remove_local_skill(name)` 清理 local_skills 记录。导致卸载后 local_skills 状态文件中残留脏数据。

**Python 样例**：
```python
# skill_manager.py L1880
self._remove_local_skill(name)
```

**修复方案**：在 `HandleSkillsUninstall` 中补充 `removeLocalSkill(name)` 调用。

---

### M-38：PermissionContext 在两个包中重复定义（后续 agent 报告合并）

### M-35：PermissionContext 在两个包中重复定义

**章节**：10.5 Permissions
**文件**：`internal/swarm/schema/permission.go` + `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`
**严重级别**：一般

**问题描述**：Go 有两个独立的 `PermissionContext` 类型：
1. `schema.PermissionContext`（含 `WebUserID` 字段）
2. `permissions.OwnerScopesPermissionContext`（不含 `WebUserID`）

Python 只有一个 `PermissionContext` 类。Go 的重复导致分裂：一个用于上下文传播，另一个作为独立结构体。

**修复方案**：统一为 `schema.PermissionContext`，删除 `owner_scopes.go` 中的重复定义。

---

### M-36：PermissionSceneHook 从 engine 调用时缺少 GoCtx

**章节**：10.5 Permissions
**文件**：`internal/agentcore/harness/security/permission_engine.go`
**严重级别**：一般

**问题描述**：`permission_engine.go` L150-154 中 `CheckPermission` 调用 sceneHook 时未填充 `GoCtx` 字段。而 `tool_security_rail.go` L344-352 通过 rail 调用时传了 `GoCtx: ctx`。`deep_adapter_rails.go` 的 `permissionSceneHook` 依赖 `input.GoCtx` 获取 PermissionContext，从 engine 路径调用时 `GoCtx` 为 nil，导致 owner_scopes/avatar 权限检查被静默跳过。

**修复方案**：在 `permission_engine.go` 的 `CheckPermission` 中传入 `GoCtx: ctx`。

---

### S-22：PermissionSceneHook 从 engine 调用时缺少 GoCtx——权限检查被静默跳过

**章节**：10.5 Permissions
**文件**：`internal/agentcore/harness/security/permission_engine.go`
**严重级别**：严重

**问题描述**：`permission_engine.go` L150-154 中 `CheckPermission` 调用 sceneHook 时未填充 `GoCtx` 字段。而 `tool_security_rail.go` L344-352 通过 rail 调用时传了 `GoCtx: ctx`。`deep_adapter_rails.go` 的 `permissionSceneHook` 依赖 `input.GoCtx` 获取 PermissionContext（`PermissionContextFromCtx(input.GoCtx)`），从 engine 路径调用时 `GoCtx` 为 nil，导致 owner_scopes/avatar 权限检查被**静默跳过**。

**Python 样例**：
```python
# host.py — scene hook 通过参数传递 context，不存在此问题
```

**Go 问题代码**：
```go
// permission_engine.go L150-154
sceneOut, err := e.sceneHook(PermissionSceneHookInput{
    NormalizedToolName: toolName,
    ToolArgs:           toolArgs,
    Engine:             e,
    // ← 缺少 GoCtx: ctx
})

// deep_adapter_rails.go L571
permCtx := sschema.PermissionContextFromCtx(input.GoCtx)  // GoCtx 为 nil → permCtx 为 nil → 跳过检查
```

**修复方案**：在 `permission_engine.go` 的 `CheckPermission` 中传入 `GoCtx: ctx`。

---

### S-23：`DedicatedMultimodalModelConfigured` 传入 `configCache` 而非 `configBase`——音频模型检测失败

**章节**：10.3.7-11 适配器辅助
**文件**：`internal/swarm/server/adapter/deep_adapter_tools.go`
**严重级别**：严重

**问题描述**：`getToolCards` step 5 中 `DedicatedMultimodalModelConfigured(d.configCache, "audio")` 传入的是 `configCache`（`react` 子段），但 `models.audio` 位于根配置层级。Python 传入完整的 `config_base`，不是 `react` 子字典。这导致音频专用模型检测失败，音频工具注册条件错误。

**Python 样例**：
```python
# interface_deep.py
if self._dedicated_multimodal_model_configured(config_base, "audio"):
    # config_base 是完整根配置，models.audio 在根层级
```

**Go 问题代码**：
```go
// deep_adapter_tools.go L592
if DedicatedMultimodalModelConfigured(d.configCache, "audio") {
    // configCache 是 react 子段，不包含 models.audio
```

**修复方案**：将 `d.configCache` 改为 `d.configBase`（完整根配置），同文件 `syncMultimodalToolsForRuntime` L290 处也需修改。

---

### M-38：PermissionContext 在两个包中重复定义

**章节**：10.5 Permissions
**文件**：`internal/swarm/schema/permission.go` + `internal/swarm/agents/harness/common/rails/permissions/owner_scopes.go`
**严重级别**：一般

**问题描述**：Go 有两个独立的 PermissionContext 类型（`schema.PermissionContext` 含 `WebUserID`，`permissions.OwnerScopesPermissionContext` 不含），Python 只有一个。Go 的重复导致分裂。

**修复方案**：统一为 `schema.PermissionContext`，删除 `owner_scopes.go` 中的重复定义。

---

### T-22：owner_scopes 路径未检查全局策略严格度

**章节**：10.6.3-10 Swarm Rails
**文件**：`internal/swarm/server/adapter/deep_adapter_rails.go`
**严重级别**：提示

**问题描述**：`permissionSceneHook` 中 `owner_scopes` 场景只检查 owner level，不检查全局策略严格度；而 `group_digital_avatar` 场景同时检查两者取更严格值。Python 中两个场景行为应一致。

---

### M-37：SkillDev 各阶段核心逻辑为占位实现

**章节**：9.38-49 SkillDev Pipeline
**文件**：`internal/swarm/server/runtime/skill/skilldev/stages/*.go`
**严重级别**：一般

**问题描述**：所有 StageHandler 的核心逻辑（`generatePlan`/`generateAllFiles`/`designEvals`/等）都返回硬编码占位数据，标注"待实现"。Python 中这些阶段也同样是占位实现（`raise NotImplementedError`），因为两边都阻塞在 `CreateStageAgent` 未接入。

**说明**：Go 和 Python 状态一致，这是已知的共享阻塞项。

---

### M-39：Pipeline 未知阶段未设置 ERROR 状态

**章节**：9.38-49 SkillDev Pipeline
**文件**：`internal/swarm/server/runtime/skill/skilldev/pipeline.go`
**严重级别**：一般

**问题描述**：Go 中未知阶段时设置 `p.runErr` 后直接退出 goroutine，但未将 `p.State.Stage` 设为 `SkillDevStageError`，也未 emit ERROR 事件。Python 中未知阶段 `raise RuntimeError`，被 `except Exception` 捕获后设置 `state.stage = ERROR` 并 emit ERROR 事件。

**Python 样例**：
```python
# pipeline.py L105-106
raise RuntimeError(f"阶段 {self.state.stage} 没有对应的处理器")
# → 被 except Exception 捕获 → state.stage = ERROR → emit ERROR event
```

**Go 问题代码**：
```go
// pipeline.go L118-128
default:
    logger.Error(logComponent).Str("stage", string(p.State.Stage)).Msg("阶段没有对应的处理器")
    p.runErr = fmt.Errorf("阶段 %s 没有对应的处理器", p.State.Stage)
    return  // ← 未设置 State.Stage = ERROR，未 emit 事件
```

**修复方案**：对齐 Python，在退出前设置 `p.State.Stage = SkillDevStageError` 并 emit ERROR 事件。

---

### M-40：desc_optimize_stage 最佳迭代平局时选择最后一个（Python 选第一个）

**章节**：9.38-49 SkillDev Pipeline
**文件**：`internal/swarm/server/runtime/skill/skilldev/stages/desc_optimize_stage.go`
**严重级别**：一般

**问题描述**：Python 的 `max()` 在平局时返回第一个最大值，Go 手动循环使用 `>` 比较会在平局时选择最后一个。虽然影响较小，但与 Python 行为不一致。

**修复方案**：将 `>` 改为 `>=` 以匹配 Python `max()` 的"后出现者优先"语义，或明确文档说明。

---

### T-24：desc_optimize_stage 有未使用的 `sortedStringKeys` 函数

**章节**：9.38-49 SkillDev Pipeline
**文件**：`internal/swarm/server/runtime/skill/skilldev/stages/desc_optimize_stage.go`
**严重级别**：提示

Go 中定义了 `sortedStringKeys` 函数但从未调用。Python 无对应。

**修复**：删除未使用的函数。

---

### M-41：FragmentMemoryManager.Search/Get/ListFragmentMemories 返回类型与 Python 不同

**章节**：7.1 Memory
**文件**：`internal/agentcore/memory/manage/index/fragment_manager.go`
**严重级别**：一般

**问题描述**：Python 的 `search()`/`get()`/`list_fragment_memories()` 返回 `list[dict]`（通过 `_doc_to_dict` 转换，含 `id/mem/mem_type/timestamp/score/source_id` 扁平键），Go 返回原始 `*index.MemoryDoc` 结构体。下游消费者如果按 Python 字典格式访问（如取 `source_id` 顶层键）将失败。

**修复方案**：添加 `DocToDict` 辅助方法，将 `MemoryDoc` 转换为与 Python `_doc_to_dict` 等价的 `map[string]any`。

---

### M-42：FragmentMemoryManager 缺少 `_process_conflict_info` 和 `_add_memory_to_store` 方法

**章节**：7.1 Memory
**文件**：`internal/agentcore/memory/manage/index/fragment_manager.go`
**严重级别**：一般

**问题描述**：
1. Python 的 `_process_conflict_info` 将 LLM 返回的整数冲突 ID 映射为字符串 memory ID（`id==0` 映射为 `"-1"`），Go 缺失
2. Python 的 `_add_memory_to_store` 对单条记忆写入有独立的验证路径（user_id/scope_id/content 校验），Go 缺失

**修复方案**：补充这两个方法，对齐 Python 的验证和映射逻辑。

---

*审查完成。共发现 26 项严重问题、42 项一般问题、24 项提示问题。*
