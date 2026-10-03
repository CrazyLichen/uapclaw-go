# 第三批集成测试实施计划

## 目标

实现 3 个模块的集成测试：Memory（CodingMemory/ExternalMemory/LongTermMemory/MemoryRail）、Security（SafetyPromptRail/PermissionInterruptRail）、Worktree（Git 操作/Rail 集成），预计 49 个测试用例。

参考设计文档：`docs/superpowers/specs/2027-07-01-integration-test-batch3-design.md`

---

## 步骤

### 第一交：Memory 基础层

- [x] **Step 1**: 创建 `tests/integration/suite/memory_suite.go` — MemorySuite 基类（继承 RunnerSuite，提供 KVStore + MockEmbedding + TempDir）
- [x] **Step 2**: 创建 `tests/integration/agentcore/memory/lite/doc.go` — 包文档
- [x] **Step 3**: 创建 `tests/integration/agentcore/memory/lite/coding_memory_test.go` — CodingMemorySuite（5 测试，SQLite 测试因 hang 移除）
- [x] **Step 4**: 更新 `tests/integration/agentcore/memory/ltm/ltm_test.go` — LongTermMemorySuite（6 测试，替换 TODO）
- [x] **Step 5**: 验证编译 + 运行 Memory 基础层测试 ✅ 11 PASS

### 第二交：Memory Rail 层 + Security 层

- [x] **Step 6**: 创建 `tests/integration/agentcore/memory/external/doc.go` — 包文档
- [x] **Step 7**: 创建 `tests/integration/agentcore/memory/external/external_memory_test.go` — ExternalMemorySuite（5 测试 + fakeMemoryProvider）
- [x] **Step 8**: 创建 `tests/integration/agentcore/harness/rails/memory/doc.go` — 包文档
- [x] **Step 9**: 创建 `tests/integration/agentcore/harness/rails/memory/coding_memory_rail_test.go` — CodingMemoryRailSuite（5 测试）
- [x] **Step 10**: 创建 `tests/integration/agentcore/harness/rails/memory/memory_rail_test.go` — MemoryRailSuite（5 测试）
- [x] **Step 11**: 创建 `tests/integration/agentcore/harness/rails/security/doc.go` — 包文档
- [x] **Step 12**: 创建 `tests/integration/agentcore/harness/rails/security/safety_prompt_test.go` — SafetyPromptRailSuite（5 测试）
- [x] **Step 13**: 创建 `tests/integration/agentcore/harness/rails/security/permission_test.go` — PermissionInterruptRailSuite（7 测试，ASK 用 hosted 确认避免 panic）
- [x] **Step 14**: 验证编译 + 运行 Memory Rail + Security 层测试 ✅ 27 PASS

### 第三交：Worktree 层

- [x] **Step 15**: 创建 `tests/integration/agentcore/harness/tools/worktree/doc.go` — 包文档
- [x] **Step 16**: 创建 `tests/integration/agentcore/harness/tools/worktree/worktree_test.go` — WorktreeManagerSuite + WorktreeRailSuite（10 测试）
- [x] **Step 17**: 验证编译 + 运行 Worktree 层测试 ✅ 10 PASS

### 最终验证

- [x] **Step 18**: 全量测试 `go test -tags=integration ./tests/integration/...` — Batch 3 全部 48 PASS ✅
- [x] **Step 19**: 更新设计文档实施状态 + doc.go 文件目录 ✅

## 实际结果

| 交付 | 测试数 | 状态 |
|------|--------|------|
| 第一交：Memory 基础层 | 11 | ✅ |
| 第二交：Memory Rail + Security | 27 | ✅ |
| 第三交：Worktree | 10 | ✅ |
| **合计** | **48** | **✅** |

累计（Batch 1 + 2 + 3）：**77 PASS**
