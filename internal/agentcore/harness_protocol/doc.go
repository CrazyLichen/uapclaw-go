// Package harness_protocol 提供第三方 agent harness 的 provider-neutral 协议值对象层。
//
// 本包是「OpenJiuwen 宿主」与「Claude Code / Codex / DSH 等外部 CLI」之间的
// 边界数据契约：所有跨进程交换的数据都在这层做构造期校验与 JSON 冻结，
// 避免坏值进到子进程才暴露。本包不调用模型、不注册工具、不发起 IO。
//
// 对应 Python 代码：openjiuwen/harness_protocol/
package harness_protocol
