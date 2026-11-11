// Package protocol 提供 Process CLI 一次性执行协议的机器面契约。
//
// 本包是「机器协议」的纯数据层：定义 JSONL 输入/输出 DTO、字段校验、
// 编解码、schema 诊断与能力自描述。它刻意不实现常驻服务、JSON-RPC 方法
// 或会话控制面——Process CLI 仍是传输持有者，把共享 Runtime 事件流转换成
// 这些记录（编排逻辑见上层 channels/processcli 的 machine/client 等）。
//
// 文件目录：
//
//	protocol/
//	├── doc.go           # 包文档
//	├── version.go       # schema 版本常量与校验、输入字节上限
//	├── model.go         # 10 个核心 DTO、枚举、冻结/解冻、严格对象校验
//	├── jsonl.go         # NDJSON 编解码与五条序列不变量
//	├── contracts.go     # 六种稳定事件的公开字段补齐
//	├── query.go         # 10 个只读查询操作与终态结果
//	├── schema.go        # schema.json 嵌入、字段级错误诊断
//	├── capabilities.go  # 协议能力自描述
//	└── schema.json      # 对外发布的 JSON Schema（原样复制，勿手改）
//
// 对应 Python 代码：jiuwenswarm/channels/process_cli/protocol/
package protocol
