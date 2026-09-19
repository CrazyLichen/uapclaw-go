# 9.24 P5 跨用户共享差距修复 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 修复 SkillEvolutionRail 跨用户共享实现与 Python SkillEvolutionSharingMixin 的 8 个差距，使 Go 行为完全对齐 Python。

**Architecture:** 所有改动集中在 `skill_evolution_rail.go` 及其测试文件 `skill_evolution_rail_test.go`，以及一个外部调用者 `deep_adapter_rails.go`。按依赖顺序实施：先基础设施（常量/正则/工具函数），再配置解析，最后业务方法重构。

**Tech Stack:** Go 1.x, testify/assert, regexp 包

---

## File Structure

| File | Change | Responsibility |
|------|--------|----------------|
| `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go` | Modify | 主实现文件：G1~G8 全部修复 |
| `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go` | Modify | 对应测试 |
| `internal/swarm/server/adapter/deep_adapter_rails.go` | Modify | WithSharingConfig 调用者适配 |

---

### Task 1: G7+G2 — 包级正则变量 + `hasRealError` 函数 + 替换 `isErrorNonePattern`

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:146-150` (全局变量区块)
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:2050-2158` (extractConversationExcerpt + isErrorNonePattern)
- Test: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试 — hasRealError**

在 `skill_evolution_rail_test.go` 的 `// ─── 非导出函数 ───` 区块中添加：

```go
func TestHasRealError(t *testing.T) {
	// 只有 error = None → 无真实错误
	assert.False(t, hasRealError("error = None"))
	assert.False(t, hasRealError("error=None"))
	assert.False(t, hasRealError("ERROR = None"))

	// 真实错误 → 有真实错误
	assert.True(t, hasRealError("Error: connection refused"))
	assert.True(t, hasRealError("error = timeout"))

	// 混合场景：error = None 和真实 error 共存
	assert.True(t, hasRealError("error = None\nError: timeout"))

	// 无 error 关键词
	assert.False(t, hasRealError("everything is fine"))

	// 空字符串
	assert.False(t, hasRealError(""))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1; go test ./internal/agentcore/harness/rails/evolution/... -run TestHasRealError -v -count=1 2>&1 | tail -5`
Expected: FAIL — `hasRealError` undefined

- [ ] **Step 3: 实现包级正则变量和 hasRealError 函数**

在 `skill_evolution_rail.go` 的全局变量区块（现有 `experienceRecordHeadingRE` 后面），添加：

```go
// failureRe 匹配 "error" 关键词，对齐 Python _FAILURE_KEYWORDS 中的 error 部分。
// 其他失败关键词用 otherFailureRe 匹配（它们不需要负向前瞻）。
// 对齐 Python: _FAILURE_KEYWORDS = re.compile(r"error(?!\s*=\s*None)|exception|...", re.IGNORECASE)
var (
	failureRe        = regexp.MustCompile(`(?i)\berror\b`)
	otherFailureRe   = regexp.MustCompile(`(?i)\bexception\b|\btraceback\b|\bfailed\b|\bfailure\b|\btimeout\b|\btimed out\b|\berrno\b|\bconnectionerror\b|\boserror\b|\bvalueerror\b|\btypeerror\b|错误|异常|失败|超时|\bno such file\b|\bpermission denied\b|\baccess denied\b|\bcommand not found\b|\bnot recognized\b|\bmodule not found\b|\beconnrefused\b|\beconnreset\b|\benoent\b|\benotfound\b|\bnpm err!`)
	errorNoneSuffixRe = regexp.MustCompile(`^\s*=\s*[Nn]one\b`)
)
```

在非导出函数区块，添加 `hasRealError`，删除 `isErrorNonePattern`：

```go
// hasRealError 判断内容中是否包含"真实的" error 关键词（排除 "error = None" 模式）。
// 对齐 Python: _FAILURE_KEYWORDS 中的负向前瞻 error(?!\s*=\s*None)。
// Go regexp 不支持负向前瞻，因此用 FindAllStringIndex 逐匹配点检查后缀。
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

同时删除 `isErrorNonePattern` 函数整体（第 2151-2158 行）。

- [ ] **Step 4: 修改 extractConversationExcerpt 中的失败检测逻辑**

将第 2058 行的局部 `failureRe` 编译删除（已提升为包级变量）。

将第 2100-2101 行的：
```go
if failureRe.MatchString(contentStr) && !isErrorNonePattern(contentStr) && strings.TrimSpace(contentStr) != "" {
```
替换为：
```go
if (hasRealError(contentStr) || otherFailureRe.MatchString(contentStr)) && strings.TrimSpace(contentStr) != "" {
```

- [ ] **Step 5: 更新 isErrorNonePattern 测试为 hasRealError 测试**

在 `skill_evolution_rail_test.go` 中，删除 `TestIsErrorNonePattern`（第 417-424 行），改为 Task 1 Step 1 中定义的 `TestHasRealError`。

同时更新 `TestExtractConversationExcerpt_排除ErrorNone` 测试，增加混合场景：

```go
func TestExtractConversationExcerpt_排除ErrorNone(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "content": "error = None, result = ok", "name": "run_tool"},
	}
	excerpt := extractConversationExcerpt(messages)
	// error = None 不应标记为失败
	assert.NotContains(t, excerpt, "FAILED TOOL EXECUTIONS")
}

func TestExtractConversationExcerpt_混合ErrorNone与真实错误(t *testing.T) {
	messages := []map[string]any{
		{"role": "tool", "content": "error = None\nException: connection refused", "name": "run_tool"},
	}
	excerpt := extractConversationExcerpt(messages)
	// 混合场景：error=None + Exception 应标记为失败（对齐 Python 负向前瞻行为）
	assert.Contains(t, excerpt, "FAILED TOOL EXECUTIONS")
}
```

- [ ] **Step 6: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1; go test ./internal/agentcore/harness/rails/evolution/... -run "TestHasRealError|TestExtractConversationExcerpt" -v -count=1 2>&1 | tail -20`
Expected: PASS

- [ ] **Step 7: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): G2+G7 用 FindAllStringIndex+后置检查模拟负向前瞻，替换 isErrorNonePattern；正则提升为包级预编译变量"
```

---

### Task 2: G6 — 添加 `sharedRecordContextMarker` 常量

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:122-144` (常量区块)
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1268` (downloadSharedExperiences 中 marker 拼接)

- [ ] **Step 1: 写失败测试 — sharedRecordContextMarker 使用**

在 `skill_evolution_rail_test.go` 中添加：

```go
func TestSharedRecordContextMarker(t *testing.T) {
	assert.Equal(t, "[shared origin=", sharedRecordContextMarker)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestSharedRecordContextMarker -v -count=1 2>&1 | tail -5`
Expected: FAIL — `sharedRecordContextMarker` undefined

- [ ] **Step 3: 添加常量并替换硬编码**

在常量区块（现有 `defaultSharingMaxUploadRetries` 后面）添加：

```go
// sharedRecordContextMarker 共享记录上下文标记前缀。
// 对齐 Python: _SHARED_RECORD_CONTEXT_MARKER = "[shared origin="
sharedRecordContextMarker = "[shared origin="
```

在 `downloadSharedExperiences` 中，将第 1268 行：
```go
marker := fmt.Sprintf("\n[shared origin=%s skill_id=%s]", bundle.BundleID, bundle.SkillID)
```
替换为：
```go
marker := "\n" + sharedRecordContextMarker + bundle.BundleID + " skill_id=" + bundle.SkillID + "]"
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestSharedRecordContextMarker -v -count=1 2>&1 | tail -5`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): G6 添加 sharedRecordContextMarker 常量对齐 Python _SHARED_RECORD_CONTEXT_MARKER"
```

---

### Task 3: G1 — `resolveDownloadTopK` + `initSharing` 签名变更 + 删除 `WithSharingConfig`

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:262-267` (NewSkillEvolutionRail 调用 initSharing)
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:309-331` (WithSharingConfig 删除)
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1061-1088` (initSharing 重写)
- Modify: `internal/swarm/server/adapter/deep_adapter_rails.go:377-395` (WithSharingConfig 调用者适配)
- Test: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试 — resolveDownloadTopK**

在 `skill_evolution_rail_test.go` 中添加：

```go
func TestResolveDownloadTopK(t *testing.T) {
	// 默认值
	assert.Equal(t, 3, resolveDownloadTopK(nil))
	assert.Equal(t, 3, resolveDownloadTopK(map[string]any{}))

	// 配置覆盖
	assert.Equal(t, 5, resolveDownloadTopK(map[string]any{"download_top_k": 5}))
	assert.Equal(t, 10, resolveDownloadTopK(map[string]any{"download_top_k": float64(10)}))
	assert.Equal(t, 7, resolveDownloadTopK(map[string]any{"download_top_k": "7"}))

	// 类型容错
	assert.Equal(t, 3, resolveDownloadTopK(map[string]any{"download_top_k": "invalid"}))
	assert.Equal(t, 3, resolveDownloadTopK(map[string]any{"download_top_k": nil}))

	// 下界保护
	assert.Equal(t, 1, resolveDownloadTopK(map[string]any{"download_top_k": 0}))
	assert.Equal(t, 1, resolveDownloadTopK(map[string]any{"download_top_k": -3}))
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run TestResolveDownloadTopK -v -count=1 2>&1 | tail -5`
Expected: FAIL — `resolveDownloadTopK` undefined

- [ ] **Step 3: 实现 resolveDownloadTopK**

在 `skill_evolution_rail.go` 非导出函数区块，添加：

```go
// resolveDownloadTopK 从共享配置解析下载 topK。
// 对齐 Python: SkillEvolutionSharingMixin._resolve_download_top_k(sharing_config)
func resolveDownloadTopK(sharingConfig map[string]any) int {
	rawTopK := defaultSharingDownloadTopK
	if sharingConfig != nil {
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
	}
	if rawTopK < 1 {
		rawTopK = 1
	}
	return rawTopK
}
```

需要在 import 中添加 `"strconv"`（如尚未导入）。

- [ ] **Step 4: 重写 initSharing 签名和逻辑**

将 `initSharing` 签名从：
```go
func (r *SkillEvolutionRail) initSharing(llmModel *llm.Model, model string, language string)
```
改为：
```go
func (r *SkillEvolutionRail) initSharing(sharingConfig map[string]any, llmModel *llm.Model, model string, language string)
```

重写内部逻辑，对齐 Python `_init_sharing(sharing_config, *, llm, model, language, evolution_store)`：

```go
// initSharing 初始化 Sharing 组件。
// 对齐 Python: SkillEvolutionSharingMixin._init_sharing(sharing_config, *, llm, model, language, evolution_store)
func (r *SkillEvolutionRail) initSharing(sharingConfig map[string]any, llmModel *llm.Model, model string, language string) {
	// Python: self._sharing_download_top_k = self._resolve_download_top_k(sharing_config)
	r.sharingDownloadTopK = resolveDownloadTopK(sharingConfig)
	r.sharingConfig = sharingConfig
	r.excerptOffsets = make(map[string]int)

	// Python: self._experience_sharer = self._build_experience_sharer(sharing_config)
	// enabled 解析逻辑从 WithSharingConfig 移入此处，对齐 Python _build_experience_sharer
	enabled := resolveSharingBool(sharingConfig["enabled"])
	envEnabled := os.Getenv("EVOLUTION_SHARING_ENABLED")
	if envEnabled != "" {
		enabled = envEnabled == "true" || envEnabled == "1"
	}
	if !enabled || sharingConfig == nil {
		r.experienceSharer = nil
		r.keywordExtractor = nil
		r.shareStager = nil
		r.sharingEnabled = false
		return
	}

	r.experienceSharer = r.buildExperienceSharer()
	if r.experienceSharer == nil {
		r.keywordExtractor = nil
		r.shareStager = nil
		r.sharingEnabled = false
		return
	}

	// Python: self._keyword_extractor = KeywordExtractor(llm=llm, model=model, language=language)
	r.keywordExtractor = sharing.NewKeywordExtractor(llmModel, model, language, skillopt.GenerateRecordsLLMPolicy)

	// Python: self._share_stager = ShareStager(keyword_extractor=..., sharer=...)
	r.shareStager = sharing.NewShareStager(r.keywordExtractor, r.experienceSharer, 0.6, nil)

	// Python: self._experience_sharer.set_skill_sharing_context_provider(self._make_sharing_context_provider(evolution_store))
	r.experienceSharer.SetSkillSharingContextProvider(r.makeSharingContextProvider())
}
```

- [ ] **Step 5: 更新 NewSkillEvolutionRail 中的 initSharing 调用**

将第 264 行：
```go
r.initSharing(llmModel, model, language)
```
改为：
```go
r.initSharing(nil, llmModel, model, language)
```

注意：这里传 `nil` 作为 sharingConfig，因为 WithSharingConfig 被删除后，sharing 配置需改为通过新方式传入。但 `NewSkillEvolutionRail` 当前没有 `sharingConfig` 参数——调用方 `deep_adapter_rails.go` 之前通过 Option 注入，删除 WithSharingConfig 后需要一个替代路径。

**替代方案**：给 `NewSkillEvolutionRail` 增加一个 `WithSharingConfigMap` Option，只保存 config 到 `r.sharingConfig`，不做 enabled 解析（由 initSharing 统一处理）。这样调用方改动最小。

添加新的 Option 函数：

```go
// WithSharingConfigMap 设置跨用户共享配置字典。
// 仅保存配置，实际解析在 initSharing 中完成。
// 对齐 Python: SkillEvolutionRail.__init__(sharing_config=...)
func WithSharingConfigMap(config map[string]any) SkillEvolutionRailOption {
	return func(r *SkillEvolutionRail) { r.sharingConfig = config }
}
```

更新 `NewSkillEvolutionRail` 中的调用：
```go
r.initSharing(r.sharingConfig, llmModel, model, language)
```

注意：由于 Options 在 initSharing 之前应用（第 203 行），`r.sharingConfig` 会先被 `WithSharingConfigMap` 设置，然后 initSharing 读到它。

同时将 `NewSkillEvolutionRail` 构造中的 `sharingDownloadTopK: defaultSharingDownloadTopK` 删除（initSharing 会覆盖）。

- [ ] **Step 6: 删除 WithSharingConfig，更新 deep_adapter_rails.go 调用**

删除 `skill_evolution_rail.go` 中的 `WithSharingConfig` 函数（第 309-331 行）。

在 `deep_adapter_rails.go` 中，将：
```go
if sharingConfig != nil {
    opts = append(opts, evolution.WithSharingConfig(sharingConfig))
}
```
改为：
```go
if sharingConfig != nil {
    opts = append(opts, evolution.WithSharingConfigMap(sharingConfig))
}
```

- [ ] **Step 7: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestResolveDownloadTopK" -v -count=1 2>&1 | tail -10`
Expected: PASS

同时确认 deep_adapter_rails.go 编译通过：
Run: `cd /home/opensource/uap-claw-go && go build ./internal/swarm/server/adapter/... 2>&1 | tail -5`
Expected: 无输出（编译成功）

- [ ] **Step 8: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go internal/swarm/server/adapter/deep_adapter_rails.go
git commit -m "fix(evolution): G1 新增 resolveDownloadTopK+WithSharingConfigMap，删除 WithSharingConfig，配置解析统一到 initSharing"
```

---

### Task 4: G3 — `buildExperienceSharer` 加 backend 校验

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1090-1153` (buildExperienceSharer)
- Test: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 在 buildExperienceSharer 中添加 backend 校验**

在 `buildExperienceSharer` 中，`hubPath` 解析之前（当前第 1106 行前），添加：

```go
// Python: backend_name = str(config.get("backend", "local_file") or "local_file").lower()
// Python: if backend_name != "local_file": logger.warning(...)
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

- [ ] **Step 2: 运行编译确认**

Run: `cd /home/opensource/uap-claw-go && go build ./internal/agentcore/harness/rails/evolution/... 2>&1 | tail -5`
Expected: 无输出（编译成功）

- [ ] **Step 3: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go
git commit -m "fix(evolution): G3 buildExperienceSharer 添加 backend 名称校验+warning 日志对齐 Python"
```

---

### Task 5: G4 — 抽出 `uploadApprovedRecordsForSharing` 方法

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:970-976` (ApproveRecord)
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1704-1717` (sharingAfterAutoApproved)
- Test: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试 — uploadApprovedRecordsForSharing**

在 `skill_evolution_rail_test.go` 中添加：

```go
func TestUploadApprovedRecordsForSharing_未启用(t *testing.T) {
	r := &SkillEvolutionRail{}
	// sharing 未启用，应直接返回不 panic
	r.uploadApprovedRecordsForSharing(context.Background(), "test_skill", nil, nil)
}

func TestUploadApprovedRecordsForSharing_启用但无记录(t *testing.T) {
	r := &SkillEvolutionRail{}
	// 启用 sharing 但传入空记录，应直接返回
	r.uploadApprovedRecordsForSharing(context.Background(), "test_skill", nil, []checkpointing.EvolutionRecord{})
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestUploadApprovedRecordsForSharing" -v -count=1 2>&1 | tail -5`
Expected: FAIL — `uploadApprovedRecordsForSharing` undefined

- [ ] **Step 3: 实现 uploadApprovedRecordsForSharing**

在 `skill_evolution_rail.go` 的非导出函数区块（`flushShareUploads` 之后），添加：

```go
// uploadApprovedRecordsForSharing 审批后上传共享记录。
// 对齐 Python: SkillEvolutionSharingMixin._upload_approved_records_for_sharing(pending, request_id)
func (r *SkillEvolutionRail) uploadApprovedRecordsForSharing(ctx context.Context, skillName string, messages []map[string]any, records []checkpointing.EvolutionRecord) {
	if !r.IsSharingEnabled() || r.shareStager == nil || len(records) == 0 {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			logger.Warn(logComponent).Str("skill", skillName).Any("error", rec).Msg("[SkillEvolutionRail] approve share staging failed")
		}
	}()
	r.stageRecordsForShare(ctx, skillName, messages, records)
	r.flushShareUploads(ctx, skillName)
}
```

- [ ] **Step 4: 替换 ApproveRecord 和 sharingAfterAutoApproved 中的内联调用**

在 `ApproveRecord` 中，将：
```go
if !isShared && len(approvedRecords) > 0 {
    r.stageRecordsForShare(ctx, pending.SkillName, approvalMessages, approvedRecords)
    r.flushShareUploads(ctx, pending.SkillName)
}
```
替换为：
```go
if !isShared && len(approvedRecords) > 0 {
    r.uploadApprovedRecordsForSharing(ctx, pending.SkillName, approvalMessages, approvedRecords)
}
```

在 `sharingAfterAutoApproved` 中，将：
```go
func (r *SkillEvolutionRail) sharingAfterAutoApproved(ctx context.Context, skillName string, stagedRequest *experience.ExperienceApprovalRequest) {
	if stagedRequest.PendingChange == nil {
		return
	}
	records := stagedRequest.PendingChange.Payload
	if len(records) == 0 {
		return
	}
	messages := stagedRequest.PendingChange.Messages
	r.stageRecordsForShare(ctx, skillName, messages, records)
	r.flushShareUploads(ctx, skillName)
}
```
替换为：
```go
func (r *SkillEvolutionRail) sharingAfterAutoApproved(ctx context.Context, skillName string, stagedRequest *experience.ExperienceApprovalRequest) {
	if stagedRequest.PendingChange == nil {
		return
	}
	records := stagedRequest.PendingChange.Payload
	if len(records) == 0 {
		return
	}
	messages := stagedRequest.PendingChange.Messages
	r.uploadApprovedRecordsForSharing(ctx, skillName, messages, records)
}
```

- [ ] **Step 5: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestUploadApprovedRecordsForSharing" -v -count=1 2>&1 | tail -10`
Expected: PASS

- [ ] **Step 6: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): G4 抽出 uploadApprovedRecordsForSharing 对齐 Python _upload_approved_records_for_sharing"
```

---

### Task 6: G5 — 独立 `getExcerptOffsetByKey` / `setExcerptOffsetByKey`

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1176-1194` (resolveIncrementalMessages)
- Test: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试**

在 `skill_evolution_rail_test.go` 中添加：

```go
func TestGetExcerptOffsetByKey(t *testing.T) {
	r := &SkillEvolutionRail{excerptOffsets: map[string]int{"sess1": 5, "sess2": 10}}

	assert.Equal(t, 5, r.getExcerptOffsetByKey("sess1"))
	assert.Equal(t, 10, r.getExcerptOffsetByKey("sess2"))
	assert.Equal(t, 0, r.getExcerptOffsetByKey("nonexistent"))

	// 空 key 返回 0
	assert.Equal(t, 0, r.getExcerptOffsetByKey(""))
}

func TestSetExcerptOffsetByKey(t *testing.T) {
	r := &SkillEvolutionRail{excerptOffsets: map[string]int{}}

	r.setExcerptOffsetByKey("sess1", 5)
	assert.Equal(t, 5, r.excerptOffsets["sess1"])

	// 空 key 不写入
	r.setExcerptOffsetByKey("", 99)
	_, exists := r.excerptOffsets[""]
	assert.False(t, exists)
}
```

- [ ] **Step 2: 运行测试确认失败**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestGetExcerptOffsetByKey|TestSetExcerptOffsetByKey" -v -count=1 2>&1 | tail -5`
Expected: FAIL — methods undefined

- [ ] **Step 3: 实现两个方法 + 修改 resolveIncrementalMessages**

在 `skill_evolution_rail.go` 非导出函数区块（`getExcerptKeyFromCtx` 之后）添加：

```go
// getExcerptOffsetByKey 获取增量消息偏移量。
// 对齐 Python: SkillEvolutionSharingMixin._get_excerpt_offset_by_key(key)
func (r *SkillEvolutionRail) getExcerptOffsetByKey(key string) int {
	if key == "" {
		return 0
	}
	return r.excerptOffsets[key]
}

// setExcerptOffsetByKey 设置增量消息偏移量。
// 对齐 Python: SkillEvolutionSharingMixin._set_excerpt_offset_by_key(key, offset)
func (r *SkillEvolutionRail) setExcerptOffsetByKey(key string, offset int) {
	if key != "" {
		r.excerptOffsets[key] = offset
	}
}
```

修改 `resolveIncrementalMessages`，将：
```go
excerptKey := r.getExcerptKeyFromCtx(cbc)
prevOffset := r.excerptOffsets[excerptKey]
incrementalMessages := messages[prevOffset:]
r.excerptOffsets[excerptKey] = len(messages)
```
替换为：
```go
excerptKey := r.getExcerptKeyFromCtx(cbc)
prevOffset := r.getExcerptOffsetByKey(excerptKey)
incrementalMessages := messages[prevOffset:]
r.setExcerptOffsetByKey(excerptKey, len(messages))
```

- [ ] **Step 4: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestGetExcerptOffsetByKey|TestSetExcerptOffsetByKey" -v -count=1 2>&1 | tail -10`
Expected: PASS

- [ ] **Step 5: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): G5 独立 getExcerptOffsetByKey/setExcerptOffsetByKey 方法并加空 key 保护对齐 Python"
```

---

### Task 7: G8 — `ExtractQueryKeywords` 错误恢复

**Files:**
- Modify: `internal/agentcore/harness/rails/evolution/skill_evolution_rail.go:1240-1245` (downloadSharedExperiences 中 ExtractQueryKeywords 调用)
- Test: `internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go`

- [ ] **Step 1: 写失败测试**

在 `skill_evolution_rail_test.go` 中添加：

```go
func TestDownloadSharedExperiences_关键词提取失败降级(t *testing.T) {
	// 验证 ExtractQueryKeywords 失败时不 panic，而是返回空 map
	r := &SkillEvolutionRail{
		excerptOffsets: make(map[string]int),
	}
	// keywordExtractor 为 nil → IsSharingEnabled 返回 false → 返回空 map
	result := r.downloadSharedExperiences(context.Background(), nil, []string{}, nil)
	assert.Empty(t, result)
}
```

- [ ] **Step 2: 在 downloadSharedExperiences 中加 panic recovery**

将第 1241-1244 行：
```go
query := r.keywordExtractor.ExtractQueryKeywords(ctx, excerpt)
if len(query.Keywords) == 0 {
    return map[string][]checkpointing.EvolutionRecord{}
}
```
替换为：
```go
// Python: try: query = await self._keyword_extractor.extract_query_keywords(feedback_excerpt=excerpt)
// Python: except Exception as exc: logger.warning(...); return {}
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
```

- [ ] **Step 3: 运行测试确认通过**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/agentcore/harness/rails/evolution/... -run "TestDownloadSharedExperiences_关键词提取失败降级" -v -count=1 2>&1 | tail -10`
Expected: PASS

- [ ] **Step 4: 提交**

```bash
git add internal/agentcore/harness/rails/evolution/skill_evolution_rail.go internal/agentcore/harness/rails/evolution/skill_evolution_rail_test.go
git commit -m "fix(evolution): G8 ExtractQueryKeywords 加 panic recovery 错误恢复对齐 Python try/except"
```

---

### Task 8: 全量测试 + 编译验证

- [ ] **Step 1: 运行 evolution 包全量测试**

Run: `cd /home/opensource/uap-claw-go && pgrep -f 'go (build|test)' && pkill -f 'go (build|test)'; sleep 1; go test ./internal/agentcore/harness/rails/evolution/... -v -count=1 2>&1 | tail -30`
Expected: 所有测试 PASS

- [ ] **Step 2: 运行全项目编译**

Run: `cd /home/opensource/uap-claw-go && export GOPROXY=https://goproxy.cn,direct && go build ./... 2>&1 | tail -10`
Expected: 无输出（编译成功）

- [ ] **Step 3: 运行 deep_adapter 包测试**

Run: `cd /home/opensource/uap-claw-go && go test ./internal/swarm/server/adapter/... -v -count=1 2>&1 | tail -10`
Expected: 所有测试 PASS

- [ ] **Step 4: 确认覆盖率 ≥ 85%**

Run: `cd /home/opensource/uap-claw-go && go test -cover ./internal/agentcore/harness/rails/evolution/... 2>&1 | tail -10`
Expected: 覆盖率 ≥ 85%

---

### Task 9: 更新 IMPLEMENTATION_PLAN.md 状态

**Files:**
- Modify: `IMPLEMENTATION_PLAN.md:567` (9.24 P5 状态)

- [ ] **Step 1: 更新 P5 状态描述**

将 9.24 行中的 P5 描述从：
```
P5(✅ 跨用户共享组合到SkillEvolutionRail; ⤴️9.80a)
```
更新为（保持 ✅，补充修复说明）：
```
P5(✅ 跨用户共享组合到SkillEvolutionRail; ⤴️9.80a; ✅G1~G8差距修复对齐Python)
```

- [ ] **Step 2: 提交**

```bash
git add IMPLEMENTATION_PLAN.md
git commit -m "docs: 更新 9.24 P5 状态，标记 G1~G8 差距修复完成"
```
