// Package artifacts 提供检索索引产物（catalog/tree/manifest）的加载与组装。
//
// 对应 Python 代码：openjiuwen/symphony/retrieval/search/artifacts/（loading 274 + merge 40）。
// 注意：本包 CatalogRecord（choice_id/payload 视角）与 build.CatalogRecord
// （worker_id 视角）是两个不同类型，对齐 Python 两处同名 dataclass。
//
// 文件目录：
//
//	artifacts/
//	├── doc.go       # 包文档
//	├── loading.go   # load_retriever_index/load_catalog_records/build_tree_root/load_tree_root
//	└── merge.go    # hits_to_search_result
package artifacts
