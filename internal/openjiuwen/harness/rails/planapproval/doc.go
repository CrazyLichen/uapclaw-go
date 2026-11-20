// Package planapproval 提供计划审批文案的识别、剥离与结构化选项生成。
//
// 本包是「计划审批文案处理」的公共归属地：`exit_plan_mode` 的确认中断会把
// 整份计划正文塞进 message，中断投影层拿到后要判断这是否是计划审批、剥离
// 原生文字版选项列表、抽出计划正文与语言，再生成前端可用的结构化选项。
//
// # 为什么独立成包
//
// 这批函数原先住在 `swarm/agents/harness/code/rails`（9.90e 8b-2 落地）。
// 9.90e 8b-6 的 interrupt 包渲染审批卡片时也要用它们，于是
// `agentcore/harness/rails/interrupt` 反向 import 了 `swarm/agents/harness/code/rails`
// ——agentcore 依赖上层 swarm，属层级倒置（全仓 agentcore 目录下独此一例）。
//
// 这些函数是纯字符串处理，只依赖标准库，与 rail 生命周期无关，因此抽成
// 本包。interrupt 与 code/rails 都可依赖它，倒置消除。
//
// 文件目录：
//
//	planapproval/
//	├── doc.go              # 包文档
//	├── message.go          # 计划审批文案的识别/剥离/抽正文/生成选项
//	└── confirm_message.go  # 确认中断文案构建（switch_mode 专用文案 + 参数预览）
//
// 对应 Python 代码：
//   - jiuwenswarm/agents/harness/code/rails/code_plan_approval_interrupt_rail.py（L93-190）
//   - jiuwenswarm/agents/harness/code/rails/code_confirm_interrupt_rail.py（L28-76）
//
// # 核心类型/接口索引
//
//   - IsPlanApprovalMessage：判断文案是否为计划审批中断
//   - StripInlinePlanApprovalChoices：剥离文字版选项列表
//   - ExtractPlanApprovalContent：抽出（计划正文, 语言）
//   - BuildPlanApprovalOptionsFromMessage：生成批准/拒绝结构化选项
//   - BuildConfirmInterruptMessage：构建确认中断提示（switch_mode 专用文案）
package planapproval
