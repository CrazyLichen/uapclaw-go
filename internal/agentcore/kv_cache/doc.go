// Package kv_cache 提供 KV cache 血缘身份与缓存亲和元数据。
//
// 本包只迁出"身份"这一层：provider 面向的 cache id 与父 cache id，以及
// 团队成员缓存身份的拼接规则。运行期（binding/lease/action）尚未迁移，
// 见下方文件目录标注。
//
// 文件目录：
//
//	kv_cache/
//	├── doc.go               # 包文档
//	├── kv_cache_types.go    # KVCacheIdentity（provider 侧缓存身份）
//	└── kv_cache_metadata.go # 缓存亲和 env 键 + TeamMemberCacheIdentity
//
// 对应 Python 代码：openjiuwen/core/kv_cache/（本包仅迁 kv_cache_types.KVCacheIdentity
// 与 kv_cache_metadata 的 env 常量、team_member_cache_identity；runtime/model_hook/
// 其余类型未迁移）
package kv_cache
