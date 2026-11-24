// Package io 提供 Symphony 检索索引的产物读写基础件。
//
// 对应 Python 代码：openjiuwen/symphony/retrieval/build/io/（388 行，一比一对齐）。
// S3 物化分支未实现（偏差 D-16-4-1，spec 2026-11-24-symphony-discovery-16.4-design.md §4）：
// is_s3_uri 判定保留，读 S3 文本返回登记错误；本地路径分支全量实现。
//
// 文件目录：
//
//	io/
//	├── doc.go           # 包文档
//	├── tree.go          # tree_index.yaml 读写/节点排序/路径规范化
//	├── items_jsonl.go   # items JSONL 读取与解析、passthrough URI 判定
//	├── manifest.go      # manifest.json 读写
//	├── catalog.go       # catalog.jsonl 读取
//	└── config_loader.go # 配置文本读取与 JSON/YAML 双解析
package io
