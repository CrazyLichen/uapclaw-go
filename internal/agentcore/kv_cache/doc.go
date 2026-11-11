// Package kv_cache 提供 KV cache 亲和协议的配置、身份血缘与 ReAct 调用钩子。
//
// 本包对齐上游两个位置：openjiuwen/core/kv_cache/（配置、元数据、身份类型、
// 运行时）与 openjiuwen/core/single_agent/kv_cache/（ReAct 模型调用钩子）。
// 上游 core/kv_cache/ 共 7 文件 / 1189 行，__init__.py 统一 re-export 22 个
// 符号；本包按「一个 Python 模块文件 → 一个 Go 文件」逐文件映射，已迁其中
// 4 文件，剩余运行时部分（kv_cache_model_hook.py / kv_cache_runtime.py /
// kv_cache_runtime_config.py / kv_cache_types.py 剩余类型 / __init__.py 导出面，
// 约 1000 行）登记为 ⤵️ 10.3.31f，不在本轮范围。
//
// 与 internal/agentcore/context_engine/context/KVCacheManager（负责 KV 释放）
// 是两套独立子系统，不合并、不互相替换。
//
// 文件目录：
//
//	kv_cache/
//	├── doc.go                          # 包文档
//	├── kv_cache_types.go               # KVCacheIdentity（provider 侧缓存身份）
//	├── kv_cache_metadata.go            # env 键 + LineageSession 窄接口 + 7 函数
//	├── kv_cache_config.go              # KVCacheAffinityConfig + 5 常量 + 超时解析
//	└── hook.go                         # KVCacheModelCallHook（ReAct 钩子，10.3.30 补）
//
// 对应 Python 代码：openjiuwen/core/kv_cache/{kv_cache_types,kv_cache_metadata,
//
//	kv_cache_config}.py · openjiuwen/core/single_agent/kv_cache/
//	kv_cache_react_model_call_hook.py
package kv_cache
