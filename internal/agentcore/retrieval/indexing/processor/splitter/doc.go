// Package splitter 提供句级文本分割：把整篇文档文本按句子边界切分，
// 再按 token 预算打包成块，供 chunker 层（如 TokenizerChunker）消费。
//
// 本包是纯算法层：构造时接收已解析的 encode/decode 函数对（由 chunker
// 包的 IndexSentenceSplitter 解析 Tokenizer 接口三态后传入），不做接口
// 探测。分句底层依赖 sentencizer（pysbd 启发的 Go 实现，对齐 Python pysbd）。
//
// 与 Python 的两处有意偏差（详见 chunker 包设计文档
// docs/superpowers/specs/2026-10-18-tokenizerchunker-13.7-design.md 所述）：
//   - find-span 恢复逻辑省略：sentencizer TextSpans 原生产出坐标，
//     Python 的 text.find 循环 + used_spans 去重 + 失败 warning 不可达
//   - 两处 Python 日志消失："Failed to get max length"（hasattr 探测在
//     Go 接口化后不存在）、"Span recovery failed"（随 find-span 省略）
//
// 文件目录：
//
//	splitter/
//	├── doc.go       # 包文档
//	├── base.go      # Splitter 接口 + BaseSplitter 基类（构造校验+共享方法）
//	└── splitter.go  # SentenceSplitter（中英文检测+分句+token 预算打包）
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/processor/splitter/
package splitter
