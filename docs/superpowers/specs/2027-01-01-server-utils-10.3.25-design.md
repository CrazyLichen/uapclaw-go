# 10.3.25 Server Utils 完整实现设计

> 对齐 Python `jiuwenswarm/server/utils/` 三个模块，完整提取 `parseStreamChunk` 到独立包 + 实现 `GetChatID/IsTeamParams` + 实现 `DiffService`（含写入端 `appendOpHistory`）

## 一、背景与流程位置

在 Agent 会话生命周期中，`server/utils` 模块在以下阶段发挥作用：

1. **stream_utils.py** → **流式响应处理阶段**：Agent 流式输出 → `parseStreamChunk` 解析 chunk → 前端消费。每次对话的核心路径。
2. **utils.py** → **请求接收阶段**：`get_chat_id` 提取聊天 ID（会话路由/消息追踪），`is_team_params` 判断团队模式（适配器选择）。
3. **diff_service.py** → **Slash 命令阶段**：用户 `/diff` 查看改动、`/rewind` 回退 turn。

## 二、Go 侧现有状态

| Python 模块 | Go 状态 | 位置 |
|---|---|---|
| `stream_utils.py`（518行） | ✅ 已在 `DeepAdapter.parseStreamChunk` 方法中完整实现 | `adapter/deep_adapter_stream.go` |
| `utils.py`（42行） | ❌ 完全缺失 | 无 |
| `diff_service.py`（476行） | ❌ 完全缺失 | 无 |
| `_append_op_history`（写入端） | ❌ 完全缺失 | 无 |

## 三、包结构与文件划分

### 3.1 `internal/swarm/server/utils/` 包（新增）

```
server/utils/
├── doc.go                  # 包文档
├── stream_utils.go         # ParseStreamChunk + 10 helper 纯函数
├── stream_utils_test.go    # 测试
├── utils.go                # GetChatID + IsTeamParams
├── utils_test.go           # 测试
├── diff_service.go         # DiffService + GetTurnDiffs/GetFilesToRestore + 12 helper
├── diff_service_test.go    # 测试
```

### 3.2 其他新增/改动文件

| 文件 | 类型 | 说明 |
|------|------|------|
| `internal/agentcore/harness/tools/filesystem/history.go` | 新增 | appendOpHistory + buildHistoryPath + recordRmTargetsBeforeDeletion + detectAndRecordDeletions |
| `internal/agentcore/harness/tools/filesystem/history_test.go` | 新增 | 测试 |
| `internal/agentcore/harness/rails/interrupt/helpers.go` | 新增 | ConvertInteractionsToAskUserQuestion + ConvertActivateConfirm |
| `internal/agentcore/harness/rails/interrupt/helpers_test.go` | 新增 | 测试 |
| `internal/swarm/server/adapter/deep_adapter_stream.go` | 改动 | 删除 parseStreamChunk 方法 + usageAccumulator，迁移到 utils |
| `internal/swarm/server/adapter/deep_adapter.go` | 改动 | 调用改为 utils.ParseStreamChunk + 注入 interactionConverter |
| `internal/agentcore/harness/tools/filesystem/write_file.go` | 改动 | 回填 appendOpHistory 调用 |
| `internal/agentcore/harness/tools/filesystem/edit_file.go` | 改动 | 回填 appendOpHistory 调用 |
| `internal/agentcore/harness/tools/shell/bash.go` | 改动 | 回填 recordRmTargetsBeforeDeletion + detectAndRecordDeletions |
| `internal/swarm/server/handle_command.go` | 改动 | handleCommandDiff 从 stub 改为调用 DiffService |

## 四、stream_utils.go 详细设计

### 4.1 导出函数

| 函数 | 签名 | 对齐 Python |
|------|------|-------------|
| `ParseStreamChunk` | `func ParseStreamChunk(output *stream.OutputSchema, usage *UsageAccumulator, emittedAskUserIDs map[string]bool, converter InteractionConverterFunc) map[string]any` | `parse_stream_chunk` |
| `ParseDictChunk` | `func ParseDictChunk(chunk map[string]any, hasStreamedContent bool) map[string]any` | `_parse_dict_chunk` |
| `ParseTypedChunk` | `func ParseTypedChunk(chunkType string, payload any, hasStreamedContent bool, converter InteractionConverterFunc) map[string]any` | `_parse_typed_chunk` |
| `ParseEventTypedChunk` | `func ParseEventTypedChunk(chunk map[string]any) map[string]any` | `_parse_event_typed_chunk` |
| `ParseResponseChunk` | `func ParseResponseChunk(chunk map[string]any, hasStreamedContent bool) map[string]any` | `_parse_response_chunk` |
| `SerializeChunkRecursive` | `func SerializeChunkRecursive(obj any) any` | `_serialize_chunk_recursive` |
| `SerializeValue` | `func SerializeValue(value any) any` | `_serialize_value` |
| `FindInteractionPayloads` | `func FindInteractionPayloads(obj any, opts ...FindOption) []any` | `_find_interaction_payloads` |
| `FindInteractionPayload` | `func FindInteractionPayload(obj any, opts ...FindOption) any` | `_find_interaction_payload` |
| `ParseInteractionPayload` | `func ParseInteractionPayload(payload any, converter InteractionConverterFunc) map[string]any` | `_parse_interaction_payload` |

### 4.2 导出类型

| 类型 | 定义 | 说明 |
|------|------|------|
| `UsageAccumulator` | struct（InputTokens/OutputTokens/TotalTokens/InputCost/OutputCost/TotalCost） | 从 DeepAdapter 迁移 |
| `InteractionConverterFunc` | `func(payload any) map[string]any` | 注入 interaction 转换逻辑 |
| `FindOption` | struct（MaxDepth int / SeenSet map[uintptr]bool） | FindInteractionPayloads 可选参数 |

### 4.3 ParseStreamChunk 分发逻辑

```
input → nil → skip
      → dict → ParseDictChunk
      → 有 Type+Payload → ParseTypedChunk（最复杂，15+ chunkType）
      → 有 EventType → ParseEventTypedChunk
      → 有 Payload+RequestID → ParseResponseChunk
      → fallback → chat.delta + str(chunk)
```

### 4.4 ParseTypedChunk chunkType 分发

| chunkType | Go 事件 | 说明 |
|-----------|---------|------|
| `context.compression_state` | `chat.context_compression_state` | 上下文压缩状态 |
| `controller_output` | 内嵌二次分发 | task_completion/task_failed 等 |
| `llm_output` | `chat.llm_output` | LLM 输出 |
| `llm_reasoning` | `chat.llm_reasoning` | LLM 推理 |
| `content_chunk` | `chat.content_chunk` | 内容块 |
| `answer` | `chat.answer` | 最终答案 |
| `tool_call` → `tool.use` | `chat.tool_use` | 工具调用 |
| `tool_update` | `chat.tool_update` | 工具更新 |
| `tool_result` → `tool.result` | `chat.tool_result` | 工具结果 |
| `error` | `chat.error` | 错误 |
| `thinking` | `chat.thinking` | 思考过程 |
| `todo.updated` | `chat.todo_updated` | TODO 更新 |
| `context.usage` | 累加到 usage | 上下文用量 |
| `chat.ask_user_question` | dedup + 直传 | 用户提问 |
| `__interaction__` | ParseInteractionPayload | 交互请求 |
| dot-notation 类型 | `chat.{chunkType}` | 通用点分类型 |
| team.* namespace | 直传 payload | 团队事件直传 |

### 4.5 关键差异（Go vs Python）

| 差异点 | Python | Go |
|--------|--------|-----|
| datetime 序列化 | `datetime.isoformat()` | `time.Time.Format(time.RFC3339)` |
| Pydantic model_dump | `model_dump(mode="json")` | 直接从 map[string]any 提取 |
| interaction 转换 | lazy import | InteractionConverterFunc 参数注入 |
| `_has_streamed_content` | bool 参数 | 保留参数但默认 false |

## 五、utils.go 详细设计

### 5.1 导出函数

| 函数 | 签名 | 对齐 Python |
|------|------|-------------|
| `GetChatID` | `func GetChatID(req *schema.AgentRequest) string` | `get_chat_id` |
| `IsTeamParams` | `func IsTeamParams(params map[string]any) bool` | `is_team_params` |

### 5.2 GetChatID 逻辑

```
1. req.ChatID（*string）非 nil 且非空 → 返回该值
2. 回退 req.Metadata 依次检查：feishu_chat_id / wecom_chat_id / dingtalk_chat_id / xiaoyi_session_id
3. 全部为空 → 返回 ""
```

### 5.3 IsTeamParams 逻辑

```
1. params 为 nil → false
2. params["team"] truthy（非 nil/非 false/非 ""/非 0）→ true
3. params["mode"]（trim+lowercase）为 "team"/"team.plan"/"code.team" → true
4. 其他 → false
```

## 六、diff_service.go 详细设计

### 6.1 DiffService 结构体

```go
type DiffService struct {
    agentID string // 默认 "uapclaw"
}
```

### 6.2 导出函数/方法

| 函数 | 签名 | 对齐 Python |
|------|------|-------------|
| `NewDiffService` | `func NewDiffService() *DiffService` | `DiffService.__init__` |
| `GetDiffService` | `func GetDiffService() *DiffService` | `get_diff_service()`（sync.Once 单例） |
| `GetTurnDiffs` | `func (ds *DiffService) GetTurnDiffs(sessionID string, projectDir ...string) []TurnDiff` | `get_turn_diffs` |
| `GetFilesToRestore` | `func (ds *DiffService) GetFilesToRestore(sessionID string, turnIndex int, projectDir ...string) map[string]RestoreFileAction` | `get_files_to_restore` |

### 6.3 导出类型

| 类型 | 字段 | 对齐 Python |
|------|------|-------------|
| `TurnDiff` | TurnIndex/Timestamp/ISOTime/UserMessage/Files/Stats | turn diff dict |
| `FileDiff` | FilePath/Action/Hunks/Stats | file diff dict |
| `Hunk` | OldStart/OldLines/NewStart/NewLines/Lines | hunk dict |
| `DiffStats` | FilesChanged/LinesAdded/LinesRemoved | stats dict |
| `RestoreFileAction` | RestoreContent（*string）/Action | restore dict |
| `FileEdit` | FilePath/Action/Timestamp/OldContent/NewContent | file edit dict |

### 6.4 非导出方法

| 方法 | 对齐 Python | 说明 |
|------|-------------|------|
| `computeTurnDiffs` | `_compute_turn_diffs` | 核心计算逻辑 |
| `isTurnEnd` | `_is_turn_end` | 判断 turn 结束 |
| `findNextUserTime` | `_find_next_user_time` | 查找下一用户消息时间 |
| `readHistory` | `_read_history` | 读取 history.json |
| `getProjectDirFromMetadata` | `_get_project_dir_from_metadata` | 从 metadata.json 取项目目录 |
| `isValidFileOpsFile` | `_is_valid_file_ops_file` | 校验 file_ops 文件名 |
| `readAgentHistory` | `_read_agent_history` | 读取并合并 file_ops 文件 |
| `findFileEditsByTimeRange` | `_find_file_edits_by_time_range` | 按时间范围查找编辑 |
| `isoToTimestamp` | `_iso_to_timestamp` | ISO → Unix timestamp |
| `timestampToISO` | `_timestamp_to_iso` | Unix timestamp → ISO |
| `computeHunks` | `_compute_hunks` | 用 diffmatchpatch 计算 hunks |
| `finalizeTurn` | `_finalize_turn` | 计算统计信息 |

### 6.5 关键设计决策

| 决策点 | Python | Go | 理由 |
|--------|--------|-----|------|
| diff 算法 | `difflib.SequenceMatcher` | `github.com/sergi/go-diff/diffmatchpatch` | Go 标准库无 difflib |
| 单例模式 | `_diff_service = None` | `sync.Once` + 包级变量 | Go 并发安全惯用法 |
| projectDir 参数 | `str | None` | `...string` 可变参数 | Go Optional 惯用法 |
| workspace 路径 | `get_*_dir()` | `workspace.*()` | Go 已有 workspace 包 |
| dedup 时间容差 | 2 秒 | `dedupTimeToleranceSec = 2.0` 常量 | 对齐 Python |
| RestoreContent nil | 文件不存在→删除 | `*string` nil 表示删除 | Go 指针表达 Optional |

## 七、history.go 详细设计（写入端）

### 7.1 位置

`internal/agentcore/harness/tools/filesystem/history.go`

对齐 Python `openjiuwen/harness/tools/filesystem.py` 第 73-239 行的模块级函数。

### 7.2 非导出函数

| 函数 | 签名 | 对齐 Python | 说明 |
|------|------|-------------|------|
| `appendOpHistory` | `func appendOpHistory(historyPath string, filePath string, action string, oldContent *string, newContent *string) error` | `_append_op_history` | 核心写入：追加 entry 到 JSON 文件 |
| `buildHistoryPath` | `func buildHistoryPath(session any) (string, error)` | `_build_history_path`（4份合并） | 构建路径：`<workspace>/.agent_history/file_ops_<agentID>_<sessionID>.json` |
| `recordRmTargetsBeforeDeletion` | `func recordRmTargetsBeforeDeletion(historyPath string, rmTargets []string, operation string) error` | `_record_rm_targets_before_deletion` | rm 前备份文件内容 |
| `detectAndRecordDeletions` | `func detectAndRecordDeletions(historyPath string) error` | `_detect_and_record_deletions` | 执行后检测消失文件 |

### 7.3 非导出常量/变量

| 名称 | 值 | 对齐 Python |
|------|-----|-------------|
| `maxHistoryPerFile` | `100` | `MAX_HISTORY_PER_FILE` |
| `historyMu` | `sync.Mutex` | `_HISTORY_LOCK`（asyncio.Lock） |

### 7.4 Python 的设计缺陷改进

Python `_build_history_path` 被 4 个类各自复制一份（WriteFile/EditFile/Bash/PowerShell），Go 合并为 `filesystem` 包的一个 `buildHistoryPath` 函数，所有工具统一调用。

### 7.5 回填调用点

| 工具 | 文件 | 回填逻辑 |
|------|------|---------|
| WriteFile | `filesystem/write_file.go` | invoke 成功后调用 `appendOpHistory(path, "write", old, new)` |
| EditFile | `filesystem/edit_file.go` | invoke 成功后调用 `appendOpHistory(path, "edit", old, new)` |
| Bash | `shell/bash.go` | rm 前调用 `recordRmTargetsBeforeDeletion`；执行成功后调用 `detectAndRecordDeletions` |

## 八、interaction helpers 详细设计

### 8.1 位置

`internal/agentcore/harness/rails/interrupt/helpers.go`

### 8.2 导出函数

| 函数 | 签名 | 对齐 Python | 说明 |
|------|------|-------------|------|
| `ConvertInteractionsToAskUserQuestion` | `func ConvertInteractionsToAskUserQuestion(payload any) map[string]any` | `convert_interactions_to_ask_user_question` | 主入口 |
| `ConvertActivateConfirm` | `func ConvertActivateConfirm(payload map[string]any) map[string]any` | activate_confirm 分支 | 构建 `harness.activate_interaction` 事件 |

### 8.3 逻辑

```
ConvertInteractionsToAskUserQuestion(payload):
  1. payload 是 map → 检查 type 字段
  2. type == "activate_confirm" → ConvertActivateConfirm → harness.activate_interaction
  3. 其他 → 构建 chat.ask_user_question { questions: [...] }
```

## 九、handleCommandDiff 回填

当前 `server/handle_command.go` 中 `handleCommandDiff` 是 stub，返回空 `[]any{}`。

回填后调用 `utils.GetDiffService().GetTurnDiffs(sessionID)` 并序列化为响应 payload。

## 十、依赖分析

### 10.1 已有依赖（✅）

| 依赖 | Go 等价 |
|------|---------|
| `get_agent_sessions_dir` | `workspace.AgentSessionsDir()` |
| `get_agent_workspace_dir` | `workspace.AgentWorkspaceDir()` |
| `get_user_workspace_dir` | `workspace.WorkspaceDir()` |
| `datetime/time` | `time.Parse()` + `time.Time.UTC()` |
| `json` | `encoding/json` |
| `pathlib.Path` | `path/filepath` |
| `history.json` 读写 | `session/session_history.go` |
| `metadata.json` 读写 | `session/session_metadata.go` |
| `/rewind` 命令 | `command_parser/slash_command.go` |
| `ParseRmTargets/ParsePSRemoveTargets` | `shell/rm_tracker.go` |

### 10.2 需新增依赖

| 依赖 | 处理 |
|------|------|
| `difflib.SequenceMatcher` | 新增 `github.com/sergi/go-diff/diffmatchpatch` 到 go.mod |
| `.agent_history/file_ops_*.json` 写入 | 本次实现 `history.go` |

### 10.3 第三方库

需新增一个直接依赖：`github.com/sergi/go-diff/diffmatchpatch v1.0.0`。该库已在 go.sum 中（间接依赖），需升级为直接依赖。

## 十一、测试策略

| 包 | 测试文件 | 核心场景 | 覆盖率 |
|---|---------|---------|--------|
| `server/utils` | `stream_utils_test.go` | ParseStreamChunk 各 chunkType、ParseDictChunk/ParseTypedChunk 15+ 类型、ParseInteractionPayload、SerializeValue、FindInteractionPayloads 嵌套+循环检测 | ≥85% |
| `server/utils` | `utils_test.go` | GetChatID 顶层+4 种 metadata 回退、IsTeamParams 各种组合 | ≥85% |
| `server/utils` | `diff_service_test.go` | GetTurnDiffs 空/单/多 turn、GetFilesToRestore、computeHunks、readAgentHistory 合并+dedup、时间转换 | ≥85% |
| `harness/tools/filesystem` | `history_test.go` | appendOpHistory 创建/追加/trim、buildHistoryPath、recordRmTargets、detectAndRecordDeletions | ≥85% |
| `harness/rails/interrupt` | `helpers_test.go` | ConvertInteractionsToAskUserQuestion activate_confirm/普通类型 | ≥85% |

DiffService 测试使用 `t.TempDir()` 创建临时文件系统数据，通过接口注入 workspace 路径函数，避免 build tag 逃避。

## 十二、IMPLEMENTATION_PLAN 更新

完成本章节后：

| 步骤 | 原状态 | 新状态 |
|------|--------|--------|
| 10.3.25 | ☐ 延后 | ✅ |
| 9.38-49 | 🔄 | 🔄（工具本身已完成，history 回填已完成） |

需要更新 `doc.go` 文件：
- `server/utils/doc.go`（新增）
- `server/adapter/doc.go`（标注 stream_utils 迁移）
- `harness/tools/filesystem/doc.go`（添加 history.go 条目）
- `harness/rails/interrupt/doc.go`（添加 helpers.go 条目）
