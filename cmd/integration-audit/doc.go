// Package main 提供集成测试真实性静态审计 + 覆盖交叉验证工具。
//
// 两层判定（详见 .codebuddy/rules/project_ruler.md 规则 7）：
//
// 第一层（静态，audit.go）：AST CallExpr 调用分析 + 断言密度，击杀：
//   - 符号引用凑数：import 包后仅引用函数值/常量/类型而不调用
//     （如 s.NotNil(runner.GetResourceMgr)）→ 只统计 pkg.Fn(...) 形式的真实调用
//   - 空壳测试：测试方法体为空或仅 s.T().Skip → 有效测试计数 + 断言密度门禁
//   - 复合 build tag 误判：用 go/build/constraint 求值 integration 标签下是否激活
//     （integration && llm 文件不在 CI 套件内，跳过而非误报）
//
// 第二层（运行时，verify.go）：对静态跨度不足的灰区目录（-verify 开启时不直接判罚，
// 标记 PendingCoverage），逐目录独立运行 go test 收集覆盖档案，按 internal 二级子系统
// 聚合覆盖行数，要求 ≥ MinSubsystems 个子系统各 ≥ MinLines 行（默认 3×30）。
// 共享 suite 基类的 SetupTest 会给每个目录带来约 15~24 行"基线污染"，
// MinLines=30 高于基线最高值，用于区分污染与测试体驱动的真实覆盖。
// 典型裁决（实测）：静跨度=1 的 heartbeat 覆盖 7 个子系统 → 真集成；
// 冒充目录 swarm/server 各子系统 ≤24 行（纯基线污染）→ 不通过。
//
// 同时保留原有三条静态规则：build tag、suite.Run、import 存在性。
//
// 输出：按目录聚合的审计结果 + 覆盖验证结果（JSON 或文本），CI 消费 exit code 判定失败。
//
// 文件目录：
//
//	integration-audit/
//	├── doc.go           # 包文档
//	├── main.go          # Execute() + main()（build tag !test 隔离）
//	├── audit.go         # 第一层：AST 调用分析与目录聚合
//	├── audit_test.go    # 第一层单元测试
//	├── verify.go        # 第二层：覆盖交叉验证引擎
//	├── verify_test.go   # 第二层单元测试（fake runner + 合成档案）
//	├── report.go        # 报告输出与 CLI 参数解析
//	└── report_test.go   # CLI 层单元测试
//
// 对应 Python 代码：无（Go 端 CI 基础设施，Python 无此工具）。
package main
