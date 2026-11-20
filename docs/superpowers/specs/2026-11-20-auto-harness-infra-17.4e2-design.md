# 17.4e2 设计：auto_harness schema.go + infra 基建层

- 日期：2026-11-20
- 章节：17.4e2（IMPLEMENTATION_PLAN.md 第 1106 行）
- 状态：设计已批准（2026-11-20 会话拍板）
- Python 基线：`/home/opensource/agent-core/openjiuwen/rsi/harness_rsi/auto_harness/{schema.py, infra/, resources/}`（~6,700 行）

## 一、范围

在 `internal/evolving/rsi/harness_rsi/auto_harness/` 下新增：

1. **schema.go**（对齐 schema.py，901 行）：AutoHarnessConfig 全量字段与默认值、TaskStatus/ExperienceType/StageSlot 枚举、NormalizePipelinePreference 及别名表、build_paths 目录排布、bootstrap 起始配置生成、ResolveGitcodeToken/Username（配置优先 + env 兜底）。
2. **infra/ 子包**（18 文件，~5,300 行）：
   - ci_gate_runner（1,090）— 本地 CI 门禁执行与结果解读
   - git_operations（738）— git 子进程统一出口（分支/提交/回滚/fork/push）
   - runtime_extension_merger（667）+ loader（286）+ static_checks（280）+ manifest（185）— 运行时扩展四件
   - skill_source_manager（585）— 技能源获取与放置
   - parsers（498）— LLM 文本输出 → 结构化数据（gaps/tasks/pr_draft/learnings）
   - worktree_manager（414）— `_ensure_base_repo`（local_repo fetch 或远端 clone 缓存）+ worktree add/清理
   - github_cli（241）— gh CLI 封装；gitcode_pr_template（137）— GitCode PR 模板；git_auth（50）— 认证
   - session_budget（116）、pipeline_selector（105）、commit_scope（118）、edit_scope（68）、fix_loop（136）— 预算/选择/范围/重试控制
3. **resource/ 子包**：go:embed `config.yaml`（auto_harness 出厂配置模板）+ `ci_gate.yaml`（CI 门禁规则，schema.py `_default_immutable_files` 列为不可改文件）。风格对齐既有 `harness_rsi/resource/embed.go` 与 `member_optimizer/agents/embed.go` 先例。

## 二、拍板事项（2026-11-20）

| # | 事项 | 拍板 |
|---|------|------|
| 1 | schema.go 依赖 pipelines 常量 | **方案A**：`base.py` 中的流水线名常量、`NormalizePipelineName`、`EXTENDED_EVOLVE_PIPELINE`/`META_EVOLVE_PIPELINE` 前置进 e2（估 200-300 行，与 schema.py 同包或 infra 上游小文件）；完整三条流水线留 e4，计划登记 ⤴️e4 回填 |
| 2 | 缺 git/gh | **严格对齐 Python**：不加启动自检，首次调用报错（exec.ErrNotFound 包装为清晰错误 → 任务 failed） |
| 3 | exec 注入缝 | **统一 CommandRunner 窄接口** `Run(ctx, name, args, opts)`，git_operations/github_cli/ci_gate_runner/skill_source_manager 等共享；默认实现走 `exec.CommandContext`（asyncio subprocess 的等价物）；测试注入 fake |
| 4 | resources 位置 | **auto_harness 包内 `resource/`**，不放项目根 resources/（根 resources 对应 Python jiuwenswarm/resources 顶层，体系不同） |
| 5 | GitLab | **不支持**（Python 无任何 gitlab 代码路径），不加新能力 |

## 三、关键行为语义（已核实 Python 源码）

1. **配置零门槛**：所有键有默认值（repo_url 默认 gitcode agent-core、base_branch 默认 develop-auto-harness、pipeline 默认 auto）；配置文件不存在时 bootstrap 复制出厂模板；gitcode_token 可走 env `GITCODE_ACCESS_TOKEN` 兜底（`gitcode_token_env` 键）。
2. **无本地仓库时自动 clone**：`_ensure_base_repo` — 有 local_repo → fetch + worktree add；无 → 目标路径无 .git 则从远端 clone 缓存，有则 fetch，再 worktree add。clone 失败 → RuntimeError → 任务 failed。不 `git init` 凭空建仓。
3. **PR 创建仅 GitCode 一条通道**：走 `api.gitcode.com/api/v5` REST，硬编码域名，无平台切换开关。`github_cli.py` 不是 PR 通道，是"GitHub-first 源码策略"调研阶段的 gh 预制检查（检测/自动安装/登录态检查），且当前在 Python 侧**零调用**（预留死代码，照抄复刻不新增调用点）。
4. **出厂模板 vs 用户配置**：出厂 config.yaml 仅在用户配置缺失时复制为起点；运行时一律读用户文件。

## 四、对齐纪律

- 规则 9/12 结构忠实：一文件对一文件、方法归属不变、步骤顺序不变、snake_case→CamelCase 唯一命名转换。
- 规则 3 日志同步：逐条对照 Python logger 调用，组件级 logger（ComponentAgentCore 下按既有 rsi 包惯例）。
- 提示词/YAML 模板逐字同步（品牌映射 jiuwenswarm→uapclawswarm，第三方 env 名不改）。
- 规则 1/规范：中文注释、声明排列顺序、doc.go（schema.go/infra/resource 各自登记）。
- 覆盖率 ≥85%：CommandRunner 注入 fake 覆盖全部 exec 路径；parsers 用 golden 文本样例。

## 五、既有回填锚点（不丢失，均不在 e2 范围）

- e3 evaluator 走 17.4f 评估桩（17.6 回填）——e2 不建 evaluator。
- 17.4f `runtime_cancellation.go` 桩不动（17.7 回填删）；auto_harness 自身 cancel/should_cancel 在 e2 若被 infra 引用则照抄，桩不动。
- 17.6 三条回填（删桩/cases 载入/any 槽收窄）不受影响。
- e1 已完成（improver_evolution），不动。

## 六、交付标准

本地 `go build ./...` + `go vet ./...` + 受影响单测（含 -race）+ 全量回归 `CGO_ENABLED=1 go test -tags "sqlite_fts5 test" ./...` 通过后推送。纯库不接 gateway（17.9 消费，e4 装配）。
