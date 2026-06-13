// Package reranker 提供重排序模型的抽象接口和数据模型。
//
// 本包定义了所有重排序模型实现必须满足的 BaseReranker 接口，
// 以及 RerankerConfig 配置、Document 文档模型、RerankOption 可选参数
// 和 rerankerBase 默认实现基类。
// 具体实现类（如 StandardReranker）嵌入 rerankerBase 后
// 只需实现核心 Rerank/RerankDocs 等方法即可满足 BaseReranker 接口。
//
// 文件目录：
//
//	reranker/
//	├── doc.go              # 包文档
//	├── base.go             # BaseReranker 接口 + RerankerConfig + Document + RerankOption
//	├── reranker_base.go    # rerankerBase 基类 + 通用方法
//	└── base_test.go        # 单元测试
//
// 对应 Python 代码：
//
//	openjiuwen/core/foundation/store/base_reranker.py
//
// 核心类型/接口索引：
//
//	BaseReranker  — 重排序模型抽象接口（Rerank/RerankDocs/RerankSync/RerankDocsSync）
//	RerankerConfig — 重排序模型配置（APIKey/APIBase/ModelName/Timeout 等）
//	Document      — 文档数据模型（ID/Text/Metadata）
//	RerankOption  — 重排序可选参数（InstructEnabled/CustomInstruct/TopN/ExtraParams）
//	rerankerBase  — 默认实现基类，提供通用 HTTP 请求/响应处理方法
package reranker
