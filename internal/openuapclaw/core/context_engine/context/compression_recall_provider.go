// Package context 的压缩召回配置可选能力面。
//
// Python 侧 SessionMemoryCompressor/归档逻辑通过
// `getattr(context, "compression_recall_config", None)` 可选协议读取引擎级
// 召回配置，未实现的上下文按 nil（未启用）处理；Go 用窄接口 + 类型断言
// 等价表达该可选语义（不改 ModelContext 主接口，避免无关实现者连锁改动；
// 且 schema ↔ interface 存在既有循环依赖约束，接口只能落在本包）。
//
// 对应 Python 代码：openjiuwen/core/context_engine/schema/config.py
package context

import (
	ceschema "github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/context_engine/schema"
)

// CompressionRecallConfigProvider 提供压缩召回全局配置的可选能力面。
// SessionModelContext 实现；forked 压缩器/归档逻辑按需类型断言消费。
type CompressionRecallConfigProvider interface {
	// CompressionRecallConfig 返回压缩召回配置，未配置时返回 nil
	CompressionRecallConfig() *ceschema.CompressionRecallConfig
}
