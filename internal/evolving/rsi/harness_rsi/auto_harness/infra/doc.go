// Package infra 提供 auto_harness orchestrator 的基础设施层。
//
// 对齐 Python openjiuwen/rsi/harness_rsi/auto_harness/infra/：
// git 操作、worktree 管理、CI 门禁执行、LLM 输出解析、
// 预算/重试/范围控制、运行时扩展合并、技能源管理、GitHub CLI 预检。
//
// 所有外部命令（git/gh/python/uv 等）一律经由 CommandRunner 注入缝
// 执行（Go 侧新增件，拍板 #3）；默认实现 execCommandRunner 走
// exec.CommandContext（asyncio.create_subprocess_exec 的等价物，D-e2-4），
// 测试注入 fake。
//
// 文件目录：
//
//	infra/
//	├── doc.go                          # 包文档
//	├── command_runner.go              # 统一命令执行注入缝（Go 新增件）
//	├── git_auth.go                    # GitCode 认证环境构造
//	├── git_operations.go              # git 分支/提交/推送/PR
//	├── worktree_manager.go            # worktree 创建与清理
//	├── parsers.go                     # LLM 输出解析
//	├── session_budget.go              # 会话预算控制
//	├── pipeline_selector.go           # 流水线信号选择
//	├── fix_loop.go                    # 修复重试循环
//	├── commit_scope.go                # 提交范围控制
//	├── edit_scope.go                  # 编辑范围控制
//	├── runtime_manifest.go            # 运行时清单校验
//	├── runtime_extension_loader.go    # 运行时扩展加载登记
//	├── runtime_extension_merger.go    # 运行时扩展合并
//	├── runtime_extension_static_checks.go # 扩展静态检查
//	├── skill_source_manager.go        # 技能源管理
//	├── github_cli.go                  # gh 预检（零调用死代码照抄）
//	├── gitcode_pr_template.go         # GitCode PR 模板
//	└── ci_gate_runner.go             # CI 门禁执行
//
// 对应 Python 代码：openjiuwen/rsi/harness_rsi/auto_harness/infra/
package infra
