// Package parser 提供文档解析抽象与具体解析器基座：把外部文档源
// （文件路径/URL）解析为统一 reranker.Document，供 Chunker 分块。
//
// 知识检索入库管线：本包解析 → chunker 分块 → ComputeChunkEmbeddings
// 嵌入 → VectorStore 入库。Parser 是管线第一站，与 Chunker 层
// （13.5-13.9）呈前后手关系——Parser 产 Document，Chunker 吃 Document。
//
// 图片文件经 ImageParser 解析：归档到 images/ 目录并经 ImageCaptioner
// 生成 caption，产出 metadata["image_path"] 供 13.4 多模态嵌入消费。
//
// PDF 文档经 PDFParser 双通道解析：ledongthuc 逐页文本 + pdfcpu 抽取嵌入
// 图片（落盘 images/）经 ImageCaptioner 生成 caption，文本与 caption 按页
// 拼接为单一 Document 文本。
//
// 使用方式（对齐 Python Parser 继承体系）：
//   - 具体解析器：嵌入 BaseParser 并覆写 ParseContent（对齐 Python 仅覆写 _parse），
//     构造时 NewBaseParser(self) 注入自身
//   - 组合解析器（13.11 AutoParser）：嵌入 BaseParser 并覆写 Parse 整体路由
//   - 文件解析器注册：具体解析器文件 init() 调 RegisterParser 按扩展名注册工厂
//     （对齐 Python @register_parser 装饰器 import 时执行；重复扩展名 first-wins 跳过）
//
// 文件目录：
//
//	parser/
//	├── doc.go           # 包文档
//	├── base.go          # Parser 接口 + ParseOptions + BaseParser 模板适配器（13.10）
//	├── registry.go      # parserRegistry + RegisterParser 注册骨架（13.16 带出自 auto_file_parser.py）
//	├── json_parser.go   # JSONParser：JSON 格式化解析（13.16）
//	├── txt_md_parser.go # TxtMdParser：文本/Markdown 编码统计检测解析（13.16）
//	├── captioner.go     # ImageCaptioner 图片描述生成 + CpImage 归档（13.15）
//	├── image_parser.go  # ImageParser 图片解析（image_path 元数据产出）（13.15）
//	├── pdf_parser.go    # PDFParser：PDF 双通道解析（文本+图片 caption）（13.12）
//	├── docx_model.go    # docx stdlib 自研解析层：zip/rels/styles/块遍历/合并单元格/图片链（13.13）
//	├── word_parser.go   # WordParser：DOCX markdown 转换（13.13）
//	├── excel_value.go   # cellStr/pyFloatStr + excelize adapter（openpyxl 类型语义还原）（13.13）
//	└── excel_parser.go  # ExcelParser：行/列 Document 产出 + CSV/TSV（13.13）
//
// 对应 Python 代码：openjiuwen/core/retrieval/indexing/processor/parser/
package parser
