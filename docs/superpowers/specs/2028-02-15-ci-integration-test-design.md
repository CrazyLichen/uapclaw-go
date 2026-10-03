# CI 集成测试流水线设计

## 1. 目标

在 GitHub Actions CI 中增加集成测试 job，确保每次 push 到 main 和 PR 到 main 都：
1. 运行集成测试
2. 检查集成测试覆盖率（总覆盖率 ≥ 15%、修改文件覆盖率 ≥ 30%）
3. 验证集成测试合规性（防止伪集成测试混入）

## 2. Job 拓扑

```
build (linux + windows) ──→ lint ────────────────┐
                          unit-test ─────────────┤──→ 完成
                          integration-test ──────┘
```

| Job | needs | 并行关系 | 失败效果 |
|-----|-------|---------|---------|
| `build` | 无 | 首个（编译门禁） | CI 立即失败 |
| `lint` | `[build]` | 与 unit-test、integration-test 并行 | CI 失败 |
| `unit-test` | `[build]` | 与 lint、integration-test 并行 | CI 失败 |
| `integration-test` | `[build]` | 与 lint、unit-test 并行 | CI 失败 |

## 3. build（编译门禁）

从尾部移到头部，验证代码能在双平台编译通过。

| 项目 | 说明 |
|------|------|
| 矩阵 | linux-amd64 (ubuntu-latest) + windows-amd64 (windows-latest) |
| 命令 | `go build ./...` |
| 产物 | 不保留二进制（只验证编译） |
| 版本注入 | 不做（门禁只需验证编译通过） |
| fail-fast | `true` |

## 4. lint

与现有逻辑相同，无变化。仅增加 `needs: [build]`。

## 5. unit-test

从原 `test` job 重命名为 `unit-test`，增加 `needs: [build]`，内部逻辑不变。

### 覆盖率检查

| 检查项 | 阈值 | 条件 |
|--------|------|------|
| 总覆盖率 | ≥ 73.0% | 始终 |
| 修改文件覆盖率 | ≥ 85% | 始终 |
| PR 回归下降 | ≤ 1.5% | 仅 PR |

### 关键参数

- 测试命令：`go test -mod=mod -tags=test -race -coverprofile=coverage.out ./...`
- 豁免文件：`.coverage-exempt`
- 基线缓存 key：`coverage-baseline-main-*`

## 6. integration-test（新增）

`needs: [build]`，与 lint 和 unit-test 并行运行。

### Step 1: 验证集成测试合规性

在运行测试之前检查所有 `tests/integration/**/*_test.go` 文件：

| # | 规则 | 检测方法 | 失败效果 |
|---|------|---------|---------|
| 1 | 必须有 `//go:build integration` | 检查每个 `_test.go` 首行 | job 失败 |
| 2 | 必须使用 suite 框架 | grep `suite.Run(` | job 失败 |
| 3 | 必须至少 import 2 个 `internal/` 包 | 统计 import 块中 `internal/` 的完整路径去重数量 | job 失败 |

检查逻辑伪代码：
```bash
for file in $(find tests/integration -name '*_test.go'); do
  # 规则 1: build tag
  head -1 "$file" | grep -q '//go:build integration' || fail

  # 规则 2: suite 框架
  grep -q 'suite.Run(' "$file" || fail

  # 规则 3: 至少 2 个 internal/ import（完整路径去重）
  internal_count=$(grep '"github.com/uapclaw/uapclaw-go/internal/' "$file" | \
    sed 's|.*"\(github.com/uapclaw/uapclaw-go/internal/[^"]*\)".*|\1|' | \
    sort -u | wc -l)
  [ "$internal_count" -ge 2 ] || fail
done
```

### Step 2: 运行集成测试（带覆盖率）

```bash
# 收集集成测试直接 import 的 internal 包作为覆盖率追踪目标
COVER_PKGS=$(grep -rh '"github.com/uapclaw/uapclaw-go/internal/' tests/integration/ --include='*.go' \
  | sed 's|.*"\(github.com/uapclaw/uapclaw-go/internal/[^"]*\)".*|\1|' \
  | sort -u | tr '\n' ',' | sed 's/,$//')

CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" \
  -coverprofile=integration_coverage.out \
  -coverpkg="${COVER_PKGS}" \
  ./tests/integration/...
```

**注意：** 必须使用 `-coverpkg` 显式指定要追踪的 internal 包。Go 默认的 `-coverprofile` 只记录测试文件所在包的覆盖率，不会自动追踪 `internal/` 的跨包覆盖率。使用 `-coverpkg` 后，覆盖率数据会包含集成测试间接执行到的 `internal/` 包的语句。

### Step 3: 检查总覆盖率 ≥ 15.0%

```bash
COVERAGE=$(go tool cover -func=integration_coverage.out | tail -1 | awk '{print $3}' | sed 's/%//')
# COVERAGE >= 15.0 则通过
```

**阈值说明：** 当前集成测试总覆盖率为 17.9%（2025-10），初始阈值设为 15.0%。随着集成测试深度增加，此阈值应逐步提升。

### Step 4: 检查修改文件覆盖率 ≥ 30%

复用 unit-test 同款逻辑，但：
- 覆盖率文件：`integration_coverage.out`
- 阈值：30%（而非 85%）
- 豁免文件：`.integration-coverage-exempt`

**关键行为：**
- 如果修改文件完全未出现在 `integration_coverage.out` 中，覆盖率为 0%，输出 `notice` 级别提醒（不阻断 CI）。初期鼓励补充集成测试，而非强制阻断。
- 如果修改文件覆盖率低于 30%，触发 `error` 级别失败。

### Step 5: 上传覆盖率报告

- artifact name: `integration-coverage-report`
- path: `integration_coverage.out`

### Step 6: 保存基线

仅 push 到 main 时：
- 写入 `integration-coverage-baseline.txt`
- 缓存 key: `integration-coverage-baseline-main-${{ github.sha }}`

**不做回归检查**（按决策：集成测试不做下降比例限制）。

### PR 时恢复基线缓存

- key: `integration-coverage-baseline-pr-${{ github.sha }}`
- restore-keys: `integration-coverage-baseline-main-`
- `continue-on-error: true`（基线缺失不阻塞）

## 7. 新增文件

### `.integration-coverage-exempt`

集成测试覆盖率豁免列表，格式同 `.coverage-exempt`：

```
# 集成测试覆盖率豁免文件列表
# 格式：每行一个相对于项目根目录的文件路径
# 仅用于确实无法通过集成测试覆盖的代码
# 注意：能通过集成测试覆盖的代码，不得加入此列表
```

初始为空（只有注释头）。

## 8. 修改文件

### `.github/workflows/ci.yml`

完整重组 workflow：
1. `build` 移到头部作为编译门禁
2. `test` 重命名为 `unit-test`，增加 `needs: [build]`
3. `lint` 增加 `needs: [build]`
4. 新增 `integration-test` job

### 删除

- 原 build 尾部 job 的版本注入步骤（门禁不需要）

## 9. 对比总结

| | unit-test | integration-test |
|---|-----------|-----------------|
| needs | `[build]` | `[build]` |
| build tags | `-tags=test` | `-tags="sqlite_fts5 test integration"` |
| 测试路径 | `./...` | `./tests/integration/...` |
| CGO | 不需要 | `CGO_ENABLED=1` |
| 覆盖率文件 | `coverage.out` | `integration_coverage.out` |
| 总覆盖率阈值 | ≥ 73.0% | ≥ 15.0% |
| 修改文件阈值 | ≥ 85% | ≥ 30% |
| 回归检查 | 有（≤ 1.5%） | 无 |
| 豁免文件 | `.coverage-exempt` | `.integration-coverage-exempt` |
| 基线缓存 key | `coverage-baseline-main-*` | `integration-coverage-baseline-main-*` |
| artifact name | `coverage-report` | `integration-coverage-report` |
| 合规性检查 | 无 | 有（build tag + suite + ≥2 internal imports） |

## 10. 集成测试覆盖率工作原理

Go 的 `-coverprofile` 默认只记录**测试文件所在包**的覆盖率，不会自动追踪 `internal/` 的跨包覆盖率。因此必须使用 `-coverpkg` 参数显式指定要追踪的目标包。

CI 中使用以下命令动态收集覆盖率追踪包：

```bash
# 从集成测试文件中提取所有 import 的 internal/ 包路径
COVER_PKGS=$(grep -rh '"github.com/uapclaw/uapclaw-go/internal/' tests/integration/ --include='*.go' \
  | sed 's|.*"\(github.com/uapclaw/uapclaw-go/internal/[^"]*\)".*|\1|' \
  | sort -u | tr '\n' ',' | sed 's/,$//')

# 运行测试时指定 -coverpkg
CGO_ENABLED=1 go test -tags "sqlite_fts5 test integration" \
  -coverprofile=integration_coverage.out \
  -coverpkg="${COVER_PKGS}" \
  ./tests/integration/...
```

这样 `integration_coverage.out` 会包含：

- `tests/integration/.../xxx_test.go` — 测试代码本身
- `internal/agentcore/.../xxx.go` — 被集成测试调用到的源码
- `internal/swarm/.../xxx.go` 等

**循环导入注意事项：** `tests/integration/mockllm/` 是 `tests/integration/suite/` 的上游依赖，因此 `mockllm_test.go` 不能导入 `suite` 包（会形成循环导入）。mockllm 的测试应直接内嵌 `suite.Suite` 而非 `BaseIntegrationSuite`。

因此修改文件覆盖率检查能反映 `internal/` 源码被集成测试真实覆盖的情况。如果某个修改文件完全未出现在 `integration_coverage.out` 中（覆盖率为 0%），说明没有任何集成测试间接执行到它。
