// utils 包提供通用工具函数。
//
// singleton.go 实现泛型单例持有器，使用 sync.Once 保证线程安全。
// 对应 Python：openjiuwen/core/common/utils/singleton.py（元类单例模式）

package utils

import (
	"sync"
)

// Singleton 提供泛型单例持有器，线程安全。
//
// 对应 Python 的 Singleton 元类，但使用 Go 惯用的 sync.Once 实现。
// 泛型参数 T 避免全局 map 的类型断言问题，每个需要单例的类型
// 只需声明一个包级 Singleton[T] 变量即可。
//
// 用法：
//
//	var poolManager = Singleton[ConnectorPoolManager]{}
//	mgr := poolManager.Get(NewConnectorPoolManager)
type Singleton[T any] struct {
	once     sync.Once
	instance *T
}

// Get 获取单例实例，若未初始化则调用 factory 创建。
//
// factory 函数只在首次调用时执行一次，后续调用直接返回已创建的实例。
// 该方法是并发安全的，多个 goroutine 同时调用 Get 时，
// 只有第一个调用会执行 factory，其余调用会等待并获取同一实例。
func (s *Singleton[T]) Get(factory func() *T) *T {
	s.once.Do(func() {
		s.instance = factory()
	})
	return s.instance
}
