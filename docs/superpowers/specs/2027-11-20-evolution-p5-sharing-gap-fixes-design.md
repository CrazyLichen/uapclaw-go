# 9.24 P5 跨用户共享差距修复设计

## 背景

9.24 EvolutionRail 的 P5（跨用户共享组合到 SkillEvolutionRail）标记为 ✅，回填来源于 9.80a（ExperienceSharing，也已 ✅）。
但逐方法对比 Python `SkillEvolutionSharingMixin` 与 Go `SkillEvolutionRail` 后发现 8 个差距，
导致 Go 实现在若干场景下行为与 Python 不一致。

## 差距汇总与修复方案

### G1：`_resolve_download_top_k()` 未实现

**问题**：`sharingConfig["download_top_k"]` 永远被忽略，`sharingDownloadTopK` 硬编码为 3。
Python 中 `_init_sharing` 调用 `_resolve_download_top_k(sharing_config)` 从配置解析，
带 int 类型容错和 `max(top_k, 1)` 保护。

**修复**：
1. 新增包级函数 `resolveDownloadTopK(sharingConfig map[string]any) int`
2. 在 `initSharing()` 中调用：`r.sharingDownloadTopK = resolveDownloadTopK(r.sharingConfig)`
3. 删除 `WithSharingConfig` 选项函数（其 enabled 解析逻辑移入 `initSharing`，见下方额外变更）

```go
func resolveDownloadTopK(sharingConfig map[string]any) int {
    rawTopK := defaultSharingDownloadTopK // 3
    if v, ok := sharingConfig["download_top_k"]; ok {
        switch n := v.(type) {
        case int:
            rawTopK = n
        case float64:
            rawTopK = int(n)
        case string:
            if parsed, err := strconv.Atoi(n); err == nil {
                rawTopK = parsed
            }
        }
    }
    if rawTopK < 1 {
        rawTopK = 1
    }
    return rawTopK
}
```

### G2：`isErrorNonePattern` 过于宽泛

**问题**：Python 用负向前瞻 `error(?!\s*=\s*None)` 仅排除特定匹配点。
Go 的 `!isErrorNonePattern(contentStr)` 检查整条内容，导致同时包含 `error = None` 和真实 `Exception` 的工具结果被错误排除。

**修复**：
1. 将 `failureRe` 改为只匹配 `\berror\b`（不含负向前瞻），提升为包级预编译变量
2. 删除 `isErrorNonePattern` 函数
3. 新增包级函数 `hasRealError(content string) bool`：用 `failureRe.FindAllIndex` 找所有匹配，
   对每个匹配检查后续文本是否紧跟 `= None` 模式，只要有一个匹配点不是 `= None` 就返回 true
4. `extractConversationExcerpt` 中改用 `hasRealError(contentStr)` 替代原来的 `failureRe.MatchString && !isErrorNonePattern`

```go
var (
    failureRe = regexp.MustCompile(`(?i)\berror\b`)
    errorNoneSuffixRe = regexp.MustCompile(`^\s*=\s*[Nn]one\b`)
)

func hasRealError(content string) bool {
    matches := failureRe.FindAllStringIndex(content, -1)
    if len(matches) == 0 {
        return false
    }
    for _, loc := range matches {
        after := content[loc[1]:]
        if !errorNoneSuffixRe.MatchString(after) {
            return true
        }
    }
    return false
}
```

注意：`failureRe` 现在只匹配 `error` 一个词，不再包含 `exception|traceback|...` 等其他关键词。
其余关键词仍然用单独的正则匹配（在 `extractConversationExcerpt` 中），只是 `error` 这个词需要特殊处理。
完整匹配逻辑改为：

```go
// otherFailureRe 匹配除 error 外的其他失败关键词（这些不需要负向前瞻）
otherFailureRe := regexp.MustCompile(`(?i)exception|traceback|failed|failure|timeout|timed out|errno|connectionerror|oserror|valueerror|typeerror|错误|异常|失败|超时|no such file|permission denied|access denied|command not found|not recognized|module not found|econnrefused|econnreset|enoent|enotfound|npm err!`)

if (hasRealError(contentStr) || otherFailureRe.MatchString(contentStr)) && strings.TrimSpace(contentStr) != "" {
    // ...
}
```

其中 `otherFailureRe` 也提升为包级预编译变量。

### G3：`buildExperienceSharer` 缺少 `backend_name` 校验

**问题**：Python 对不支持的后端名记录 warning 并回退到 `local_file`，Go 静默忽略。

**修复**：在 `buildExperienceSharer` 中、创建 backend 之前加：

```go
backendName := "local_file"
if v, ok := config["backend"]; ok {
    if s, ok := v.(string); ok && s != "" {
        backendName = strings.ToLower(s)
    }
}
if backendName != "local_file" {
    logger.Warn(logComponent).Str("backend", backendName).Msg("[SkillEvolutionRail] 不支持的后端，回退到 local_file")
}
```

### G4：`_upload_approved_records_for_sharing` 方法未抽出

**问题**：`stageRecordsForShare + flushShareUploads` 逻辑在 `ApproveRecord` 和 `sharingAfterAutoApproved` 中重复内联，
缺少 Python 的独立方法 `_upload_approved_records_for_sharing`。

**修复**：
1. 新增 `uploadApprovedRecordsForSharing(ctx context.Context, skillName string, messages []map[string]any, records []checkpointing.EvolutionRecord)`
   - 内含 `IsSharingEnabled` + `shareStager != nil` 检查
   - 调用 `stageRecordsForShare` + `flushShareUploads`
   - defer/recover 错误恢复 + warning 日志
2. `ApproveRecord` 和 `sharingAfterAutoApproved` 改为调用此方法

### G5：`_get_excerpt_offset_by_key` / `_set_excerpt_offset_by_key` 未独立

**问题**：Go 在 `resolveIncrementalMessages` 中内联，且空 key 时仍写入 `excerptOffsets[""]`。
Python 独立方法带空 key 保护。

**修复**：
1. 新增 `getExcerptOffsetByKey(key string) int`：空 key 返回 0
2. 新增 `setExcerptOffsetByKey(key string, offset int)`：空 key 跳过写入
3. `resolveIncrementalMessages` 改为调用这两个方法

### G6：`_SHARED_RECORD_CONTEXT_MARKER` 常量缺失

**问题**：Python 定义模块级常量 `_SHARED_RECORD_CONTEXT_MARKER = "[shared origin="`，Go 无常量。
经核实 Python 完整 marker 格式为 `[shared origin={bundle_id} skill_id={skill_id}]`，与 Go 当前一致。

**修复**：
1. 添加包级常量 `sharedRecordContextMarker = "[shared origin="`
2. `downloadSharedExperiences` 中改用常量拼接：
   `marker := "\n" + sharedRecordContextMarker + bundle.BundleID + " skill_id=" + bundle.SkillID + "]"`

### G7：`_FAILURE_KEYWORDS` 每次调用重新编译正则

**问题**：`failureRe` 在 `extractConversationExcerpt` 内每次调用编译，Python 编译一次存为类变量。

**修复**：已在 G2 修复中一并处理——`failureRe` 和 `otherFailureRe` 都提升为包级预编译变量。

### G8：`ExtractQueryKeywords` 缺少错误恢复

**问题**：Python 在 `_download_shared_experiences` 中用 try/except 包裹 `extract_query_keywords`，
失败时返回空关键词继续。Go 无错误恢复，可能导致整个下载循环中断。

**修复**：经确认 `ExtractQueryKeywords` 返回 `QueryKeywords`（不返回 error），需用 panic recovery：

```go
func (r *SkillEvolutionRail) downloadSharedExperiences(...) map[string][]checkpointing.EvolutionRecord {
    // ...
    var query sharing.QueryKeywords
    func() {
        defer func() {
            if rec := recover(); rec != nil {
                logger.Warn(logComponent).Any("error", rec).Msg("[SkillEvolutionRail] keyword extraction failed")
                query = sharing.QueryKeywords{}
            }
        }()
        query = r.keywordExtractor.ExtractQueryKeywords(ctx, excerpt)
    }()
    if len(query.Keywords) == 0 {
        return map[string][]checkpointing.EvolutionRecord{}
    }
    // ...
}
```

## 额外变更：删除 `WithSharingConfig`

G1 讨论中确认：`WithSharingConfig` 选项函数的职责（enabled 解析 + 保存 config）
全部移入 `initSharing(sharingConfig)`，对齐 Python `__init__` 传 `sharing_config=None` → `_init_sharing(sharing_config)`。

`initSharing` 签名变更：
```go
func (r *SkillEvolutionRail) initSharing(sharingConfig map[string]any, llmModel *llm.Model, model string, language string)
```

内部流程：
1. 解析 enabled（环境变量优先 + config 回退）→ `r.sharingEnabled`
2. 解析 download_top_k → `r.sharingDownloadTopK`
3. 保存 `r.sharingConfig = sharingConfig`
4. 若 enabled → `buildExperienceSharer()` → 创建 KeywordExtractor / ShareStager / 设置 context provider

## 改动范围

全部改动集中在 `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` 一个文件。

| 改动 | 影响行 |
|------|--------|
| 新增 `resolveDownloadTopK` 函数 | 新增 ~15 行 |
| 删除 `WithSharingConfig` | 删除 ~22 行 |
| 修改 `initSharing` 签名 + 内部逻辑 | 修改 ~30 行 |
| 新增 `hasRealError` + 包级正则变量 | 新增 ~20 行，删除 `isErrorNonePattern` ~5 行 |
| 修改 `extractConversationExcerpt` | 修改 ~5 行 |
| `buildExperienceSharer` 加 backend 校验 | 新增 ~8 行 |
| 新增 `uploadApprovedRecordsForSharing` | 新增 ~20 行 |
| 修改 `ApproveRecord` + `sharingAfterAutoApproved` | 修改 ~6 行 |
| 新增 `getExcerptOffsetByKey` / `setExcerptOffsetByKey` | 新增 ~10 行 |
| 修改 `resolveIncrementalMessages` | 修改 ~3 行 |
| 添加 `sharedRecordContextMarker` 常量 | 新增 ~2 行 |
| 修改 `downloadSharedExperiences` marker 拼接 | 修改 ~1 行 |
| `downloadSharedExperiences` 加 ExtractQueryKeywords error 恢复 | 修改 ~5 行 |

测试：修改对应的 `_test.go` 文件，确保新增/修改方法有测试覆盖。
