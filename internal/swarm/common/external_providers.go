// Python 源：jiuwenswarm/common/external_providers.py（64 行 1:1）
//
// server 侧存储/配置能力向 common 的注入点。分层规则 R1：common 禁止 import
// server / symphony。历史上 common 的 MCP 配置、配置面板等模块直接延迟 import
// server/runtime/*——gateway 独立成仓时 common 副本无法自带这些实现。本文件把
// 依赖反转：common 只面向 Provider(domain) 取实现，注册责任在使用方进程。
//
// 域清单（Python external_providers.py 头注释，与 check_layering.py R3 白名单对应）：
//
//   - mcp_state          → server.runtime.mcp.state_store
//   - mcp_registry       → server.runtime.mcp.registry
//   - mcp_credential     → server.runtime.mcp.credential
//   - a2ui               → server.runtime.a2ui.integration
//   - team_binding_store → server.runtime.team_binding_store
//   - team_entity_store  → server.runtime.team_entity_store

package common

import (
	"fmt"
	"sort"
	"sync"

	"github.com/uapclaw/uapclaw-go/internal/common/logger"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ExternalProviderUnregisteredError 请求的外部能力域尚未注册实现（进程未装配或功能不可用）。
// Python: ExternalProviderUnregistered(RuntimeError) (external_providers.py:37-39)
type ExternalProviderUnregisteredError struct {
	// Domain 未注册的能力域名
	Domain string
}

// Error 实现 error 接口，文案逐字对齐 Python f-string。
func (e *ExternalProviderUnregisteredError) Error() string {
	return fmt.Sprintf("external provider '%s' is not registered in this process", e.Domain)
}

// ──────────────────────────── 全局变量 ────────────────────────────

var (
	// providersMu 保护 providers map。Python 单线程事件循环下 dict 直读直写；
	// Go 侧按 asyncio→Go 并发移植惯例加锁（进程级注册表可能被多 goroutine 并发注册/读取）。
	providersMu sync.RWMutex
	// providers 能力域注册表。Python: _providers: dict[str, Any] (external_providers.py:42)
	providers = map[string]any{}
)

// ──────────────────────────── 导出函数 ────────────────────────────

// Register 注册一个能力域的实现（重复注册以后者为准，供测试覆盖）。
// Python: register (external_providers.py:45-48)
func Register(domain string, impl any) {
	providersMu.Lock()
	defer providersMu.Unlock()
	providers[domain] = impl
	logger.Debug(logger.ComponentCommon).Str("domain", domain).Msg("[external-providers] registered")
}

// Provider 取能力域实现；未注册时返回 *ExternalProviderUnregisteredError。
// Python: provider (external_providers.py:51-57)
func Provider(domain string) (any, error) {
	providersMu.RLock()
	impl, ok := providers[domain]
	providersMu.RUnlock()
	if !ok {
		return nil, &ExternalProviderUnregisteredError{Domain: domain}
	}
	return impl, nil
}

// RegisteredDomains 当前已注册的能力域（诊断用）。
// Python: registered_domains (external_providers.py:60-64)
func RegisteredDomains() []string {
	providersMu.RLock()
	defer providersMu.RUnlock()
	domains := make([]string, 0, len(providers))
	for domain := range providers {
		domains = append(domains, domain)
	}
	sort.Strings(domains)
	return domains
}
