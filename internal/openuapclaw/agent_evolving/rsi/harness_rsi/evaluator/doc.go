// Package evaluator 提供 RSI 评估器侧的运行时适配件。
//
// 本包对应 Python openjiuwen/rsi/harness_rsi/evaluator/。17.6a/17.6b/17.6c
// 已全量落地：judger 裁判组（judger/ 子包）、运行时适配三轨、case_backend
// 考场、case_runner 监考编排、TeamEvaluator 评估门面（17.6c 真身，17.4f
// 桩与哨兵 ErrEvaluatorNotImplemented 已按规则 8 删除，回填件①②③④兑现）。
// 17.6d 外部基准落地：swebench_runtime（官方镜像考场准备/官方验证收卷）、
// terminal_bench_runtime（容器 shell 录音/patch sync），case_backend swebench
// 分支同包直调（Python 同款），SWE-bench 官方验证经 judger 反转缝注册真身。
//
// 文件目录：
//
//	evaluator/
//	├── doc.go                            # 包文档
//	├── runtime_adapters.go              # RSISysOperationRail/RSITrajectoryRail/RSISkillUseRail/空响应恢复
//	├── case_backend.go                  # 考场：CaseExecutionResult 契约（Task 4 前置切片）
//	├── case_backend_main.go             # 考场主链：SingleHarnessExecutionBackend（swebench 分支直调容器 runtime）
//	├── case_backend_helpers.go          # 考场助手：快照对比/资产备场/工作区判定/testbed 环境
//	├── case_runner.go                   # 监考编排：CaseRunner 六步主链/判分/轨迹/trace/收卷（17.6b）
//	├── baseline_context.go              # 基线判分上下文（17.4d 前置件）
//	├── controlled_skill_treatment_rail.go # 受控技能处置轨（Task 7 前置）
//	├── harness_input_rail.go            # harness 输入轨：FileGuardChecker（Task 7 前置）
//	├── metrics_collector.go             # 指标收集
//	├── optimization_signals.go          # 优化信号契约（反转缝注册）
//	├── requirement_results.go           # RequirementResults 契约（反转缝注册）
//	├── transport.go                     # 瞬态传输错误注册（反转缝）
//	├── trajectory_paths.go              # 角色轨迹路径与 RoleFileTrajectoryStore
//	├── trajectory_usage.go              # 轨迹用量归集
//	├── errors.go                        # EvaluationInfrastructureError
//	├── swebench_runtime.go              # SWE-bench 镜像/考场准备/官方验证收卷（17.6d，Python 833 行）
//	├── terminal_bench_runtime.go        # Terminal-Bench 容器运行时（录音/patch sync/容器启停，17.6d，Python 533 行）
//	├── resource/
//	│   └── swebench_official_support/   # sitecustomize.py embed 资产（Python 进程注入文件，17.6d）
//	├── team_evaluator.go                # TeamEvaluator 评估门面真身（17.6c，Python team_evaluator.py 501 行）
//	│                                      （指纹经 source_evidence.MaterialIdentity 正向依赖，原 RegisterMaterialIdentity 反转缝已删）
//	└── judger/                          # 17.6a 裁判组子包（评分五件+提示词 embed+判分器工厂）
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/evaluator/
package evaluator
