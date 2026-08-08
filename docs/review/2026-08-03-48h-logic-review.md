# 48小时逻辑审查报告 (2026-08-03)

> 审查范围：48小时内（2026-08-01 ~ 2026-08-03）提交的代码
> 审查目标：Go 移植代码与 Python 原始代码的逻辑一致性
> 涉及章节：9.38-49 多模态工具 / 10.3.23 Hooks / 9.72c-d 自演化 / 9.78 Checkpointing / 9.79 Experience / 9.65a-1/2 TeamDB / 9.64 Team Memory / 10.5 Extensions / 10.3.25 Utils / 10.3.15-18 Session / DeepAdapter multimodal 注册回填

---

## 问题分级说明

| 级别 | 定义 | 示例 |
|------|------|------|
| **严重** | 功能逻辑错误，会导致运行时错误、数据丢失或行为不一致 | 方法缺少步骤、参数类型不匹配、条件判断错误 |
| **一般** | 逻辑差异但不直接影响核心功能流程 | 配置校验不全、命名不一致、缺少防御性处理 |
| **提示** | 代码风格或防御性增强建议 | 日志缺失、注释不完整、命名可优化 |

---

## 严重问题（共 17 个）

---

### S01: `extractResponseText` 多 text part 只取第一个，丢失后续文本

**模块**: harness/tools/multimodal/vision_helpers.go  
**章节**: 9.38-49 多模态工具

**Python 代码** (`openjiuwen/harness/tools/multimodal/vision.py` L86-96):
```python
if isinstance(content, list):
    chunks = []
    for item in content:
        if isinstance(item, dict) and item.get("type") == "text":
            chunks.append(item.get("text", ""))
        else:
            text = getattr(item, "text", None)
            if text:
                chunks.append(text)
    return "\n".join(chunk.strip() for chunk in chunks if chunk).strip()
```

**Go 问题代码** (`vision_helpers.go` L223-231):
```go
parts := content.Parts()
for _, part := range parts {
    if part.Type == "text" && part.Text != "" {
        return strings.TrimSpace(part.Text)  // 只返回第一个 text part
    }
}
```

**问题**: Python 收集所有 text part 用 `\n` 拼接后返回，Go 只返回第一个 text part 并立即 `return`。模型返回多段文本时 Go 丢失后续内容。

**修复方案**:
```go
parts := content.Parts()
chunks := []string{}
for _, part := range parts {
    if part.Type == "text" && part.Text != "" {
        chunks = append(chunks, strings.TrimSpace(part.Text))
    }
}
if len(chunks) > 0 {
    return strings.Join(chunks, "\n")
}
```

---

### S02: `VideoModelConfig` 是 Go 新增结构体，Python 中不存在独立 VideoModelConfig

**模块**: harness/schema/config.go + adapter/multimodal_config.go  
**章节**: 9.38-49 多模态工具

**Python 代码** (`openjiuwen/harness/tools/multimodal/video_understanding.py` L12):
```python
from openjiuwen.harness.schema.config import VisionModelConfig
```
Python 的 `VideoUnderstandingTool` 使用 `VisionModelConfig`，没有独立的 `VideoModelConfig`。配置走 VISION_ 前缀环境变量。

**Go 问题代码** (`harness/schema/config.go` L43-54):
```go
type VideoModelConfig struct {
    APIKey         string `json:"api_key"`
    BaseURL        string `json:"base_url"`
    Model          string `json:"model"`
    MaxRetries     int    `json:"max_retries"`
    ThinkingEnabled bool  `json:"thinking_enabled"`
}
```

**问题**: Go 自创了 `VideoModelConfig` 和 VIDEO_ 前缀环境变量（`VIDEO_API_KEY`/`VIDEO_API_BASE`），但 Python 中视频工具使用 `VisionModelConfig` 和 VISION_ 前缀。配置链路完全不一致，部署时 YAML 中 video 和 vision 配置方式不同会导致工具无法正确加载模型。

**修复方案**: 
- 方案A（推荐）：如果确实需要 video/vision 使用不同模型，保留 `VideoModelConfig` 但明确注释这是 Go 扩展设计决策，非 Python 对齐
- 方案B（严格对齐）：删除 `VideoModelConfig`，`VideoUnderstandingTool` 使用 `VisionModelConfig`，环境变量回退链对齐 Python

---

### S03: ACR HMAC timestamp 格式不一致——Python 浮点数，Go 整数

**模块**: harness/tools/multimodal/audio_helpers.go  
**章节**: 9.38-49 多模态工具

**Python 代码** (`openjiuwen/harness/tools/multimodal/audio.py` L305):
```python
timestamp = str(time.time())  # 浮点数字符串，如 "1234567890.123456"
```

**Go 问题代码** (`audio_helpers.go` L140):
```go
timestamp := fmt.Sprintf("%d", time.Now().Unix())  // 整数字符串，如 "1234567890"
```

**问题**: ACRCloud HMAC 签名依赖 timestamp 格式。Python 浮点数（含小数秒），Go 整数（不含），签名可能不匹配导致 ACR 识别请求被拒绝。

**修复方案**:
```go
timestamp := fmt.Sprintf("%.6f", float64(time.Now().UnixNano())/1e9)
```

---

### S04: `refreshMultimodalConfigs` 预设 `videoToolRegistered=true` 导致 video 工具永远不注册

**模块**: swarm/server/adapter/deep_adapter_tools.go  
**章节**: DeepAdapter multimodal 注册回填

**Python 代码** (`interface_deep.py` L1304-1312):
```python
def _refresh_multimodal_configs(self, config_base):
    self._vision_model_config = self._build_vision_model_config(config_base)
    self._audio_model_config = self._build_audio_model_config(config_base)
    self._video_model_config = self._build_video_model_config(config_base)
    # Python 不修改 _video_tool_registered / _vision_tools_registered / _audio_tools_registered
```

**Go 问题代码** (`deep_adapter_tools.go` L321-327):
```go
func (d *DeepAdapter) refreshMultimodalConfigs(configBase map[string]any) {
    d.visionModelConfig = d.buildVisionModelConfig(configBase)
    d.audioModelConfig = d.buildAudioModelConfig(configBase)
    d.videoModelConfig = d.buildVideoModelConfig(configBase)
    d.videoToolRegistered = d.videoModelConfig != nil  // BUG: 提前设置 registered
    d.imageGenToolRegistered = d.buildImageGenModelConfig(configBase) // 同样 bug
}
```

**问题**: `syncMultimodalToolsForRuntime` 中注册条件是 `d.videoModelConfig != nil && !d.videoToolRegistered`。但 `refreshMultimodalConfigs` 已将 `videoToolRegistered` 设为与 `videoModelConfig != nil` 同值，注册分支永远为 false，video 工具永远不会被注册。同理 `imageGenToolRegistered`。

**修复方案**: 删除 `refreshMultimodalConfigs` 中的这两行：
```go
// 删除: d.videoToolRegistered = d.videoModelConfig != nil
// 删除: d.imageGenToolRegistered = d.buildImageGenModelConfig(configBase)
```
让 `syncMultimodalToolsForRuntime` 自行管理注册状态。

---

### S05: `ReloadAgentConfig` 未调用 `syncMultimodalToolsForRuntime` / `syncPaidSearchToolForRuntime`

**模块**: swarm/server/adapter/deep_adapter.go  
**章节**: DeepAdapter multimodal 注册回填

**Python 代码** (`interface_deep.py` L2690-2691):
```python
self._sync_multimodal_tools_for_runtime()
self._sync_paid_search_tool_for_runtime()
```

**Go 问题代码** (`deep_adapter.go` L602-604):
```go
// 步骤 8.5: 工具同步
// ⤵️ agentcore: _sync_multimodal_tools_for_runtime()
// ⤵️ agentcore: _sync_paid_search_tool_for_runtime()
```

**问题**: `syncMultimodalToolsForRuntime` 和 `syncPaidSearchToolForRuntime` 方法已存在于 `deep_adapter_tools.go` 中，但 `ReloadAgentConfig` 只标记了 ⤵️ 占位未实际调用。配置热重载时多模态工具不会同步注册/注销。

**修复方案**:
```go
d.syncMultimodalToolsForRuntime(ctx)
d.syncPaidSearchToolForRuntime()
```

---

### S06: 音频工具注册逻辑跳过了 Python 的 "metadata-only" 情况

**模块**: swarm/server/adapter/deep_adapter_tools.go  
**章节**: DeepAdapter multimodal 注册回填

**Python 代码** (`interface_deep.py` L1271-1302 + L1438):
```python
# _iter_runtime_audio_tools 有三种情况:
# 1. dedicated_multimodal_model_configured(audio) = False → 无音频工具
# 2. dedicated_multimodal_model_configured(audio) = True + audio_model_config = None → 仅 audio_metadata
# 3. dedicated_multimodal_model_configured(audio) = True + audio_model_config 存在 → 全部音频工具

self._audio_tools, self._audio_tools_registered = self._sync_tool_group(
    current_tools=self._audio_tools,
    registered=self._audio_tools_registered,
    enabled=True,  # 始终 True，过滤在 _iter_runtime_audio_tools 内部完成
    ...
)
```

**Go 问题代码** (`deep_adapter_tools.go` L267-280):
```go
if d.audioModelConfig != nil && !d.audioToolsRegistered {
    // 只在 audioModelConfig != nil 时注册
    client := d.resolveAudioModelClient()
    audioTools := multimodal.CreateAudioTools(client, d.audioModelConfig, ...)
    ...
}
```

**问题**: Go 仅在 `audioModelConfig != nil` 时注册音频工具，完全跳过了 Python 的情况2：dedicated key 存在但 `audio_model_config` 为 None 时仍注册 `audio_metadata` 工具。

**修复方案**: 重构音频同步逻辑对齐 Python 三种情况：
```go
if !DedicatedMultimodalModelConfigured(configBase, "audio") {
    // 情况1: 无独立音频 key → 不注册任何音频工具
    if d.audioToolsRegistered { d.removeRegisteredTools(...); d.audioToolsRegistered = false }
    return
}
if d.audioModelConfig == nil {
    // 情况2: 有独立 key 但无 model config → 仅注册 audio_metadata
    ...
}
if d.audioModelConfig != nil {
    // 情况3: 完整配置 → 全部音频工具
    ...
}
```

---

### S07: `removeRegisteredTools` 传工具名给 ResourceMgr，但 ResourceMgr 期望卡片ID

**模块**: swarm/server/adapter/deep_adapter_tools.go  
**章节**: DeepAdapter multimodal 注册回填

**Python 代码** (`interface_deep.py` L1352-1376):
```python
def _remove_registered_tools(self, tools):
    for tool in tools:
        Runner.resource_mgr.remove_tool(tool.card.id)    # 用卡片ID (如 "audio_transcription_uapclaw")
        self._instance.ability_manager.remove(tool.card.name)  # 用卡片名 (如 "audio_transcription")
```

**Go 问题代码** (`deep_adapter_tools.go` L125-143):
```go
func (d *DeepAdapter) removeRegisteredTools(toolIDs []string) {
    am.RemoveMany(toolIDs)                         // 传工具名 — AbilityManager 正确
    runner.GetResourceMgr().RemoveTool(toolIDs)    // 传工具名 — ResourceMgr 错误！
}
```
调用处传入 `ToolNameVision` ("vision")，但 ResourceMgr 用 `toolCard.ID`（如 `ImageOCRTool_uapclaw`）注册，名称不匹配导致移除静默失败。

**修复方案**: 区分名称和ID：
```go
func (d *DeepAdapter) removeRegisteredTools(toolNames []string) {
    am := d.Instance().AbilityManager()
    am.RemoveMany(toolNames)                     // 名称 — 正确
    for _, name := range toolNames {
        // 根据名称推断卡片ID格式: name + "_uapclaw"
        runner.GetResourceMgr().RemoveTool([]string{name + "_uapclaw"})
    }
}
```
或存储已注册工具的 Card 以便用真实 ID 移除。

---

### S08: Vision/Audio 注销只移除单个工具名，漏掉子工具

**模块**: swarm/server/adapter/deep_adapter_tools.go  
**章节**: DeepAdapter multimodal 注册回填

**Go 问题代码** (`deep_adapter_tools.go` L263, L279):
```go
d.removeRegisteredTools([]string{ToolNameVision})              // "vision" — 但注册了 image_ocr + visual_question_answering
d.removeRegisteredTools([]string{ToolNameAudioTranscription})  // "audio_transcription" — 但注册了 3 个音频工具
```

**问题**: Vision 工具集注册了 `image_ocr` 和 `visual_question_answering` 两个工具，Audio 注册了 `audio_transcription`、`audio_question_answering`、`audio_metadata` 三个。注销时只移除一个名称，其余残留。

**修复方案**:
```go
// Vision 注销
d.removeRegisteredTools([]string{"image_ocr", "visual_question_answering"})
// Audio 注销
d.removeRegisteredTools([]string{"audio_transcription", "audio_question_answering", "audio_metadata"})
```

---

### S09: Hooks `BeforeToolCall` 中 `modifiedInput` 赋值语义不一致

**模块**: swarm/server/hooks/user_hook_rail.go  
**章节**: 10.3.23 Hooks

**Python 代码** (`user_hook_rail.py` L64):
```python
ctx.inputs.tool_args = r.modified_input  # 整体替换
```

**Go 问题代码** (`user_hook_rail.go` L74-87):
```go
if result.ModifiedInput != nil {
    if modifiedArgs, ok := result.ModifiedInput["tool_args"]; ok {
        if s, ok := modifiedArgs.(string); ok {
            toolInputs.ToolArgs = s  // 只提取特定字段
        }
    }
    if newName, ok := result.ModifiedInput["_tool_name"]; ok {
        if s, ok := newName.(string); ok && s != "" {
            toolInputs.ToolName = s
        }
    }
}
```

**问题**: Python 将整个 `modified_input` 字典赋给 `tool_args`，Go 只提取 `tool_args` 和 `_tool_name` 两个特定字段。当 hook 返回 `{"modifiedInput": {"path": "/safe", "readonly": true}}` 时，Python 替换整个 `tool_args`，Go 什么都不修改。

**修复方案**: 对齐 Python 语义——将整个 `modifiedInput` 序列化为 JSON 字符串赋给 `ToolArgs`：
```go
if result.ModifiedInput != nil {
    jsonBytes, _ := json.Marshal(result.ModifiedInput)
    toolInputs.ToolArgs = string(jsonBytes)
}
```

---

### S10: Hooks `ParseCommandOutput` 中 `reason` 字段条件覆盖 vs Python 无条件覆盖

**模块**: swarm/server/hooks/executor.go  
**章节**: 10.3.23 Hooks

**Python 代码** (`executor.py` L164-169):
```python
if "reason" in data and decision != "block":
    result.additional_context = data["reason"]  # 无条件覆盖 additional_context
```

**Go 问题代码** (`executor.go` L159-164):
```go
if v, ok := data["reason"].(string); ok && decision != "block" {
    if result.AdditionalContext == "" {  // 只在空时设置，不覆盖
        result.AdditionalContext = v
    }
}
```

**问题**: 当 hook 输出 `{"decision": "allow", "additionalContext": "original", "reason": "override"}` 时，Python 的 `additional_context` 最终为 `"override"`，Go 为 `"original"`。

**修复方案**:
```go
if v, ok := data["reason"].(string); ok && decision != "block" {
    result.AdditionalContext = v  // 对齐 Python: 无条件覆盖
}
```

---

### S11: Hooks `AfterToolCall` 中 `ToolResult` 为 nil 时 `fmt.Sprintf("%v", nil)` 产生 `<nil>` 字符串

**模块**: swarm/server/hooks/user_hook_rail.go  
**章节**: 10.3.23 Hooks

**Python 代码** (`user_hook_rail.py` L105):
```python
current = ctx.inputs.tool_result or ""  # None → ""
```

**Go 问题代码** (`user_hook_rail.go` L136-141):
```go
current := fmt.Sprintf("%v", toolInputs.ToolResult)  // nil → "<nil>" 字符串
if current != "" {
    toolInputs.ToolResult = current + "\n[Hook 发现]: " + result.AdditionalContext
} else {
    toolInputs.ToolResult = "[Hook 发现]: " + result.AdditionalContext
}
```

**问题**: `ToolResult` 为 `nil` 时 `fmt.Sprintf("%v", nil)` 返回 `"<nil>"`，不是空字符串。`"<nil>" != ""` 为 true，导致 `ToolResult` 变为 `"<nil>\n[Hook 发现]: extra"`，污染工具输出。

**修复方案**:
```go
var current string
if toolInputs.ToolResult != nil {
    current = fmt.Sprintf("%v", toolInputs.ToolResult)
}
```

---

### S12: Cron `list_jobs` includeDisabled 默认值 false vs Python True

**模块**: harness/tools/cron/dispatch.go  
**章节**: 9.38 Cron

**Python 代码** (`cron.py` L32):
```python
async def list_jobs(self, *, include_disabled: bool = True) -> list[dict[str, Any]]:
```

**Go 问题代码** (`dispatch.go` L83):
```go
includeDisabled := boolVal(inputs, "includeDisabled")  // boolVal 默认 false
```

**问题**: Python 默认包含禁用任务，Go 默认不包含。用户调用 `list` 不传 `includeDisabled` 时，Go 返回不完整列表。

**修复方案**: 将默认值改为 true：
```go
includeDisabled := true
if v, ok := inputs["includeDisabled"]; ok {
    includeDisabled = toBool(v)
}
```

---

### S13: `TeamTaskManager.Claim` 缺少 Python 端的多层校验

**模块**: agent_teams/tools/task_manager.go  
**章节**: 9.65a-2 TaskDao + TeamTaskManager

**Python 代码** (`task_manager.py` L615-685):
```python
async def claim(self, task_id: str) -> TaskOpResult:
    member_name = self.member_name
    task = await self.get(task_id)                          # 1. 检查任务存在
    if not task: return TaskOpResult.fail(...)
    member = await self.db.member.get_member(...)           # 2. 检查成员存在
    if not member: return TaskOpResult.fail(...)
    if member.mode == MemberMode.PLAN_MODE.value: ...       # 3. PLAN_MODE 检查
    if task.assignee == member_name and ...: return success # 4. 幂等性检查
    if task.assignee: return TaskOpResult.fail(...)         # 5. 已被他人认领检查
    if not is_valid_transition(...): return fail(...)       # 6. FSM 状态转换合法性
    success = await self.db.task.claim_task(...)
    await self.messager.publish(...)                        # 7. 事件发布
```

**Go 问题代码** (`task_manager.go` L172):
```go
func (tm *TeamTaskManager) Claim(ctx context.Context, taskID string) error {
    ok, err := tm.db.Task().ClaimTask(ctx, taskID, tm.memberName)
    // 直接调用数据库，缺少 1-6 校验步骤
}
```

**问题**: Go 缺少成员存在性校验、PLAN_MODE 权限检查、幂等性处理、已认领冲突检查、FSM 状态转换合法性检查、事件发布。任何一步缺失都可能导致非法操作成功执行。

**修复方案**: 在 `Claim` 方法中逐步添加每一步校验逻辑，对齐 Python 的完整校验链。复杂流程示例：
```
1. db.Task().GetTask → nil → 返回错误
2. db.Member().GetMember → nil → 返回错误  
3. member.Mode == PLAN_MODE → 返回错误
4. task.Assignee == tm.memberName && task.Status == CLAIMED → 返回成功(幂等)
5. task.Assignee != "" → 返回错误(已被他人认领)
6. !isValidTransition(task.Status, CLAIMED, transitions) → 返回错误
7. db.Task().ClaimTask → ok
8. messager.Publish(TaskClaimedEvent) → 事件通知
```

---

### S14: `TeamTaskManager.Assign` 缺少 Python 端的多层校验

**模块**: agent_teams/tools/task_manager.go  
**章节**: 9.65a-2

同 S13，Go `Assign` 直接调用 `ClaimTask`，缺少成员存在性验证、幂等性检查、已认领冲突检查、事件发布。

**修复方案**: 对齐 Python 的完整校验链。

---

### S15: `TeamTaskManager.Complete` 缺少 PLAN_MODE 检查和事件发布

**模块**: agent_teams/tools/task_manager.go  
**章节**: 9.65a-2

**Python 代码** (`task_manager.py` L687-759):
```python
async def complete(self, task_id) -> TaskOpResult:
    member = await self.db.member.get_member(...)
    if member.mode == MemberMode.PLAN_MODE.value:
        # PLAN_MODE 只能完成 PLAN_APPROVED 任务
        if task.status != TaskStatus.PLAN_APPROVED.value: return fail(...)
    result = await self.db.task.complete_task(task_id)
    await self._publish_task_event(TaskCompletedEvent(...))
    await self._publish_unblocked_events(...)
    await self._maybe_publish_task_list_drained()
```

**Go 问题**: 缺少 PLAN_MODE 权限检查、TaskCompletedEvent 发布、TaskUnblockedEvent 发布、TaskListDrainedEvent 检查。

**修复方案**: 补充完整逻辑。

---

### S16: `CheckpointManager` 接口和 `DefaultCheckpointManager` 实现完全缺失

**模块**: agentcore/evolving/checkpointing/  
**章节**: 9.78 EvolveCheckpoint

**Python 代码** (`checkpointing/manager.py`):
```python
class CheckpointManager(Protocol):
    def should_save(self, *, epoch, improved) -> bool: ...
    def build_checkpoint(self, *, agent, progress, ...) -> EvolveCheckpoint: ...
    def restore(self, *, agent, checkpoint) -> Dict[str, Any]: ...

class DefaultCheckpointManager:
    def __init__(self, *, run_id, checkpoint_version, save_every_n_epochs, save_on_improve): ...
    def should_save(...): ...
    def build_checkpoint(...): ...
    def restore(...): ...
    # Plus: add_pending, get_pending, commit_pending, discard_pending
```

**Go 问题**: 仅定义了 `EvolveCheckpoint` struct，完全没有 `CheckpointManager` 接口或 `DefaultCheckpointManager` 实现。Trainer 断点续训无法工作。

**修复方案**: 在 `checkpointing/` 包中实现 `CheckpointManager` 接口和 `DefaultCheckpointManager`，对齐 Python 的 `should_save`、`build_checkpoint`、`restore`、`add_pending`、`get_pending`、`commit_pending`、`discard_pending`。

---

### S17: DiffService `computeHunks` 用 `strings.Split` 而非对齐 Python `splitlines(keepends=True)`

**模块**: swarm/server/utils/diff_service.go  
**章节**: 10.3.25 Server Utils

**Python 代码** (`diff_service.py` L358-359):
```python
old_lines = old_content.splitlines(keepends=True)
new_lines = new_content.splitlines(keepends=True)
```

**Go 问题代码** (`diff_service.go` L551-552):
```go
oldLines := strings.Split(*oldContent, "\n")
newLines := strings.Split(*newContent, "\n")
```

**问题**: 
1. Python `splitlines(keepends=True)` 正确处理 `\r\n`（保留为行尾），Go `strings.Split("\n")` 保留 `\r` 在行内容中
2. Python 正确处理尾部无换行符（不产生多余空行），Go 在尾部有 `\n` 时多出一个空字符串元素
3. 导致 diff 输出格式不一致，影响前端 diff 显示

**修复方案**: 实现自定义 `splitLinesKeepEnds` 函数对齐 Python `splitlines(keepends=True)`，在 hunk 生成时 `rstrip()` 对齐 Python。

---

## 一般问题（共 18 个）

---

### G01: Vision/Audio 注销不调用 `pruneToolCards`，`d.toolCards` 残留过期条目

**Python 代码** (`interface_deep.py` L1333-1337):
```python
self._prune_tool_cards({t.card.name for t in current_tools})
```

**Go 问题**: `syncMultimodalToolsForRuntime` 只从 AbilityManager/ResourceMgr 移除，不从 `d.toolCards` 移除。

**修复**: 注销时添加 `d.toolCards = d.pruneToolCards(...)` 调用。

---

### G02: `refreshMultimodalConfigs` 不刷新已注册工具实例的配置属性

**Python 代码** (`interface_deep.py` L1314-1317):
```python
for tool in self._vision_tools:
    tool.vision_model_config = self._vision_model_config
for tool in self._audio_tools:
    tool.audio_model_config = self._audio_model_config
```

**Go 问题**: 不维护工具实例列表（Python 的 `self._vision_tools`/`self._audio_tools`），无法刷新已注册工具的配置。API key 热重载后旧工具仍用旧 key。

**修复**: 存储工具实例引用列表，在 `refreshMultimodalConfigs` 中刷新配置。

---

### G03: AudioQA/AudioMetadata 缺少重试机制

**Python 代码** (`audio.py` L392-414):
```python
async def _call_with_retries(config, func, *args):
    # 429/500/502/503/504 自动指数退避重试
```

Python 所有三个音频工具都通过 `_call_with_retries` 调用。Go 的 AudioQA 和 AudioMetadata 直接调用 `client.Invoke` / `InvokeACRMetadata`，无重试。只有 Vision 的 `CallVisionModel` 有重试。

**修复**: 在 AudioQA 和 AudioMetadata 调用外层添加重试包装，对齐 Python `_call_with_retries`。

---

### G04: Video 工具配置校验只检查 `APIKey`，不检查 `BaseURL`

**Go 代码** (`video_understanding.go` L78):
```go
if config == nil || config.APIKey == "" {
```

Python 检查 `vision_model_config is None` 和 `self.model is None`。Go 只检查 `APIKey`，`BaseURL` 为空时后续调用必失败。

**修复**: `if config == nil || config.APIKey == "" || config.BaseURL == "" {`

---

### G05: `AudioModelConfig.FromEnv()` 中 `QAModel` 缺少 `AUDIO_MODEL_NAME` 回退

**Python 代码** (`interface_deep.py` L1215-1217):
```python
question_answering_model = str(
    os.getenv("AUDIO_QUESTION_ANSWERING_MODEL") or os.getenv("AUDIO_MODEL_NAME") or ""
)
```

**Go 代码** (`config.go` L410):
```go
QAModel: envOrWithDefault(DefaultOpenAIAudioQAModel, "AUDIO_QUESTION_ANSWERING_MODEL"),
```
只检查 `AUDIO_QUESTION_ANSWERING_MODEL`，不回退 `AUDIO_MODEL_NAME`。但 `TranscriptionModel` 正确包含 `AUDIO_MODEL_NAME` 回退。

**修复**: 添加 `AUDIO_MODEL_NAME` 回退：
```go
QAModel: envOrWithDefault("", "AUDIO_QUESTION_ANSWERING_MODEL", "AUDIO_MODEL_NAME"),
```

---

### G06: `handleHooksList` 返回 payload 格式与 Python 不一致

**Python 代码** (`agent_ws_server.py` L4221-4225):
```python
payload={"events": summary, "disable_all_hooks": hooks_config.disable_all_hooks, "source": "config.yaml"}
```

**Go 代码** (`handle_extensions.go` L64-68):
```go
schema.WithPayload(map[string]any{"hooks": summary})
```

差异：键名 `"events"` vs `"hooks"`；缺少 `disable_all_hooks` 和 `source` 字段。

**修复**: 
```go
schema.WithPayload(map[string]any{
    "events": summary, "disable_all_hooks": hooksCfg.DisableAllHooks, "source": "config.yaml",
})
```

---

### G07: `RunAll` 对未知 hook 类型处理不同

**Python 代码**: 未知类型静默跳过，返回列表长度可能小于 `hook_configs`。  
**Go 代码**: 未知类型生成 `NON_BLOCKING_ERROR` 条目，返回列表长度始终等于 `hook_configs`。

**修复**: 对齐 Python，未知类型不加入结果。

---

### G08: `json.Marshal` 默认转义非 ASCII vs Python `ensure_ascii=False`

**Python 代码**: `json.dumps(hook_input, ensure_ascii=False)` 保留中文原样。  
**Go 代码**: `json.Marshal(hookInput)` 将中文转为 `\uXXXX`。

hook 程序做字符串匹配时可能因转义格式不一致而匹配失败。

**修复**: 使用自定义 JSON encoder 禁用 HTML escaping：
```go
var buf bytes.Buffer
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.Encode(hookInput)
```

---

### G09: `TeamTaskManager.Add` 缺少 `taskID` 和 `dependencies` 参数

**Python 代码** (`task_manager.py` L189):
```python
async def add(self, title, content, task_id=None, dependencies=None):
```

**Go 代码** (`task_manager.go` L106):
```go
func (tm *TeamTaskManager) Add(ctx context.Context, title, content string) (*database.TeamTaskBase, error)
```

**修复**: 扩展签名支持可选 taskID 和 dependencies。

---

### G10: `SharedMemoryManager.ReadTeamSummary` / `WriteTeamSummary` 缺少 sysOperation 分支

**Python 代码**: sysOperation 优先，fallback 到本地文件系统。  
**Go 代码**: 标注 ⤵️ 回填标记，sysOperation 分支未实现。

**修复**: 实现 sysOperation 分支，对齐 Python 优先级逻辑。

---

### G11: `EvolutionStore.InstallSkillPackage` 推断 skill name 逻辑不完整

**Python 代码**: 先检查顶层目录唯一性（一步到位），其次扫描 SKILL.md。  
**Go 代码**: `inferSkillNameFromPackage` 只扫描 SKILL.md，不检查顶层目录唯一性。

**修复**: 在推断逻辑中添加顶层目录唯一性检查。

---

### G12: ExtensionRegistry `CreateInstance` 已存在时返回 nil 而非 error

**Python 代码**: `raise RuntimeError("ExtensionRegistry 已初始化...")`  
**Go 代码**: 返回 nil，调用方无法区分"创建成功但返回空"和"已存在失败"。

**修复**: `CreateInstance` 应返回 `(*ExtensionRegistry, error)`。

---

### G13: `BaseExtensionImpl.Metadata()` 缺少懒加载

**Python 代码**: `metadata` property 在缓存 nil 时自动调用 `_load_metadata_from_yaml()`。  
**Go 代码**: `Metadata()` 只返回缓存，缓存 nil 时直接返回 nil。

**修复**: 在 `Metadata()` 中添加懒加载逻辑。

---

### G14: `ListSkillTool` json.dumps 中文转义差异

**Python 代码**: `json.dumps(payload, ensure_ascii=False, indent=2)` 保留中文。  
**Go 代码**: `json.Marshal` 转义中文为 `\uXXXX`，无缩进。

LLM 接收到的 skills 描述中文被转义，影响路由质量。

**修复**: 使用 `json.MarshalIndent` + 自定义 encoder 禁用 HTML escaping。

---

### G15: `formatCreateResult` 消息使用中文而 Python 使用英文

**Go 代码**: `"已成功创建 %d 个任务"`  
**Python 代码**: `"Successfully created %d task(s)"`

根据项目规则"提示词一比一复刻Python"，结果消息字符串也应与 Python 原文一致。

**修复**: 改为英文消息一比一复刻 Python。

---

### G16: `cancelTodos` ids 不去重，cancelledIDs 可能包含重复项

**Python 代码**: `todo.id in ids`（list 包含检查），ids 有重复项时 todo 只匹配一次。  
**Go 代码**: 遍历 ids 每个元素，ids 有重复项时 cancelledIDs 包含重复项。

**修复**: Go 的 cancel 应先对 ids 去重，或改遍历 todos 而非 ids。

---

### G17: `EvolutionStore.NewEvolutionStore` 只接受 `string` 但 Python 接受 `Union[str, List[str]]`

**Python 代码**: `__init__(self, skills_base_dir: Union[str, List[str]])`  
**Go 代码**: `NewEvolutionStore(skillsBaseDir string, ...)`

Go 无法直接传入路径列表。

**修复**: 添加 `NewEvolutionStoreFromDirs(baseDirs []string, ...)` 工厂函数。

---

### G18: DiffService 删除/新建文件场景 splitlines vs strings.Split

**Python 代码**: 删除/新建场景用 `splitlines()`（不带 keepends）。  
**Go 代码**: 用 `strings.Split("\n")`，尾部 `\n` 时多出空字符串。

**修复**: 对齐 Python `splitlines()` 逻辑，去掉尾部空字符串。

---

## 提示问题（共 14 个）

---

### T01: `buildVideoModelConfig` Apply→Dedicated 顺序与 vision/audio 不同

vision/audio 先 Dedicated 门控再 Apply，video 先 Apply 再 Dedicated。门控失败时 video 已污染环境变量。

**建议**: 统一为先 Dedicated 再 Apply，避免环境变量污染。

---

### T02: AudioQA 配置校验只检查 `base_url` 不检查 `api_key`

Python 的 `_build_openai_client` 会检查 `api_key`，Go 只检查 `base_url`。适配器层已保证 APIKey 非空，运行时无影响，但校验链路与 Python 不完全一致。

---

### T03: `normalizeVideoURL` 不检查 IsDir

Python `Path.is_file()` 区分文件和目录，Go `os.Stat` 不区分。传入目录时错误信息不明确。

---

### T04: `BuildImageContent` 不检查 IsDir

同 T03。

---

### T05: DeepAdapter 注册 UserHookRail 缺少 try-catch 防御

Python `try/except` 包裹注册流程，失败时 warning 并继续。Go 无 error 处理，异常时 Agent 无法启动。

---

### T06: `handleHooksList` 缺少错误处理

Python `try/except` 返回 `ok=False` 错误响应。Go 无错误处理，配置加载失败时返回空列表而非错误。

---

### T07: `queryLLM` 多模态内容处理可能不完全对齐

Python 从内容块列表提取所有 text 字块用换行符连接。Go 用 `content.String()`，可能不完全对应。

---

### T08: `VisionModelConfig.FromEnv()` 有额外的 `OPENROUTER_API_KEY`/`OPENAI_API_KEY` 回退

Python 只读 `VISION_API_KEY`。Go 添加了两个额外回退。正常流程不被使用，但行为差异需注释说明。

---

### T09: `urlParse` 函数命名容易混淆

`urlParse` 返回 `u.Path` 字符串而非 URL 对象，变量名 `parsedURL` 实际是 path，容易误解。

---

### T10: `runCommandHook` stderr TrimSpace vs Python 不 TrimSpace

Python 不 TrimSpace，Go 先 TrimSpace。空白 stderr 时 Python 使用空白字符串，Go 使用 `"exit code N"`。TrimSpace 更合理，但与 Python 不一致。

---

### T11: DiffService `findNextUserTime` 返回 0 vs Python None

Go 返回 0 表示"无下一个用户消息"。0 作为 timestamp 可能被误认为 1970-01-01。建议注释说明 0 的语义。

---

### T12: DiffService `normalizePath` 差异

Python `Path.resolve()` 解析符号链接，Go `filepath.Abs()` 不解析。Linux 上通常无影响，Windows 上可能大小写不一致。

---

### T13: `BaseExtensionImpl._get_extension_dir` 缺少 inspect 回退

Python 用 `inspect.getmodule` 自动推断扩展目录，Go 无此能力。需文档标注此差异。

---

### T14: `EvolutionStore` RWMutex vs Python asyncio.Lock 语义差异

Python `asyncio.Lock` 所有操作排他，Go `RWMutex` 读操作并发。Go 设计合理但需注释说明。

---

## 总结统计

| 级别 | 数量 | 分布 |
|------|------|------|
| 严重 | 17 | 多模态6 + Hooks3 + Cron1 + TeamTask3 + Checkpointing1 + DiffService1 + multimodal注册3 |
| 一般 | 18 | 多模态3 + Hooks3 + Extensions3 + TeamTask1 + Team Memory2 + Evolving2 + Cron1 + Skills1 + Todo2 + DiffService2 |
| 提示 | 14 | 多模态4 + Hooks4 + DiffService3 + Extensions2 + Evolving1 |

**最优先修复（严重问题）**:
1. S04 + S05: adapter multimodal 注册回填 (`refreshMultimodalConfigs` 预设 registered + `ReloadAgentConfig` 未调用 sync)
2. S07 + S08: `removeRegisteredTools` 名称 vs ID + 注销只移除单个工具名
3. S01: `extractResponseText` 多 text part 不拼接
4. S09 + S10 + S11: Hooks 三处语义不一致
5. S13 + S14 + S15: TeamTaskManager 三方法缺校验
6. S16: CheckpointManager 完全缺失
