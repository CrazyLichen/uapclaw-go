// Package artifactrsi 提供 artifact 优化服务流的提供方契约层。
//
// 本包对应 Python openjiuwen/rsi/artifact_rsi/ 的契约部分：统一的
// ArtifactProvider 服务协议（由 AgentServer 路由消费）与 Provider 侧工单
// （ArtifactEngineRequest）及其公开请求校验/构建。本包不含任何优化引擎：
// 程序引擎（PUCT）在计划 17.3，论文流水线在 17.5/17.6，AgentServer 服务端在 17.9。
//
// 契约语义（由 17.3/17.5 的具体实现兑现）：
//
//   - artifact_type 字面量约束：program 实现恒返回 program，paper 恒返回 paper；
//     AgentServer 必须在运行时显式路由。
//   - 事件纪律：先持久化（状态/节点/报告/artifact）再发对应事件；
//     NodeStageEvent 只更新既有节点的阶段描述，不新建树节点。
//   - Paper 不支持 Resume：返回 SCENARIO_NOT_SUPPORTED 且不改变任务状态。
//   - model 已解析：工单中的 Model 是 AgentServer 初始化好的实例；实现方可调用
//     其 Invoke/Stream，但不得从任务配置解析模型 ID 或构造客户端。
//
// 子包 program_opt/（17.3）、paper_opt/（17.5）随对应章节创建（拍板方案 Ⅱ）；
// Python 子协议的 9 方法逐字重复声明在 Go 侧以接口嵌入替代（拍板方案 ①）。
//
// 文件目录：
//
//	artifact_rsi/
//	├── doc.go        # 包文档
//	├── provider.go   # ArtifactProvider 接口
//	└── request.go    # ArtifactEngineRequest + 校验 + 构建
//
// 对应 Python 代码：openjiuwen/rsi/artifact_rsi/
package artifactrsi
