// utils 包提供通用工具函数。
//
// singleton.go 实现泛型单例持有器，使用 sync.Once 保证线程安全。
// 对应 Python：openjiuwen/core/common/utils/singleton.py（元类单例模式）

package utils

import (
	"log"
	"sync"
)

// ──────────────────────────── 结构体 ────────────────────────────

// resettable 重置时可清理的接口。
// 如果单例持有类型实现了该接口，Reset 时会自动调用其 Cleanup。
type resettable interface {
	Cleanup() error
}

// Singleton 提供泛型单例持有器，线程安全。
//
// 对应 Python 的 Singleton 元类，但使用 Go 惯用的 sync.Once 实现。
// 泛型参数 T 避免全局 map 的类型断言问题，每个需要单例的类型
// 只需声明一个包级 Singleton[T] 变量即可。
//
// 注意：Go 没有弱引用（weakref.WeakValueDictionary）机制，Python 单例的
// _instances 字典存储弱引用，当外部不再持有实例时自动清理；Go 的普通 map
// 不会自动回收。Go 通过 ResetWithCleanup（resettable 接口）显式清理替代
// Python 的自动弱引用回收，测试时调用 Reset 即可触发 Cleanup。
//
// 用法：
//
//	var poolManager = Singleton[ConnectorPoolManager]{}
//	mgr := poolManager.Get(NewConnectorPoolManager)
type Singleton[T any] struct {
	// once 单次执行器
	once sync.Once
	// instance 单例实例
	instance *T
}

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// ──────────────────────────── 导出函数 ────────────────────────────

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

// Reset 重置单例，使下次 Get 重新调用 factory 创建新实例。
// 若当前实例实现了 resettable 接口，Reset 时会先调用其 Cleanup；
// Cleanup 失败仅记日志不阻断重置流程。
// 仅用于测试。
func (s *Singleton[T]) Reset() {
	if s.instance != nil {
		if c, ok := any(s.instance).(resettable); ok {
			if err := c.Cleanup(); err != nil {
				log.Printf("[Singleton] Cleanup failed during Reset: %v", err)
			}
		}
	}
	s.once = sync.Once{}
	s.instance = nil
}
