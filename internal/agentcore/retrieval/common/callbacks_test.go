package common

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	storeEmbedding "github.com/uapclaw/uapclaw-go/internal/agentcore/foundation/store/embedding"
)

// TestDocIndexCallback_类型别名 验证 DocIndexCallback 是 embedding.Callback 的类型别名
func TestDocIndexCallback_类型别名(t *testing.T) {
	// NoOpDocIndexCallback 应实现 DocIndexCallback 接口
	var _ DocIndexCallback = (*NoOpDocIndexCallback)(nil)
	// NoOpDocIndexCallback 也应实现 embedding.Callback 接口
	var _ storeEmbedding.Callback = (*NoOpDocIndexCallback)(nil)
	// LoggingDocIndexCallback 应实现 DocIndexCallback 接口
	var _ DocIndexCallback = (*LoggingDocIndexCallback)(nil)
}

// TestNoOpDocIndexCallback_调用计数 测试计数器递增
func TestNoOpDocIndexCallback_调用计数(t *testing.T) {
	cb := &NoOpDocIndexCallback{}
	cb.OnBatchComplete(0, 10, []string{"a", "b"})
	cb.OnBatchComplete(10, 20, []string{"c", "d"})
	assert.Equal(t, 2, cb.CallCounter())
}

// TestNoOpDocIndexCallback_并发安全 测试并发调用计数器
func TestNoOpDocIndexCallback_并发安全(t *testing.T) {
	cb := &NoOpDocIndexCallback{}
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cb.OnBatchComplete(0, 1, nil)
		}()
	}
	wg.Wait()
	assert.Equal(t, 100, cb.CallCounter())
}

// TestLoggingDocIndexCallback_总数小于100不打日志 测试小批次不打日志
func TestLoggingDocIndexCallback_总数小于100不打日志(t *testing.T) {
	cb := NewLoggingDocIndexCallback(5)
	assert.NotNil(t, cb)
	// 不 panic 即可
	for i := 0; i < 5; i++ {
		cb.OnBatchComplete(i*10, (i+1)*10, []string{"text"})
	}
}

// TestLoggingDocIndexCallback_总数大于100每100批打日志 测试大批次打日志
func TestLoggingDocIndexCallback_总数大于100每100批打日志(t *testing.T) {
	cb := NewLoggingDocIndexCallback(250)
	assert.NotNil(t, cb)
	for i := 0; i < 250; i++ {
		cb.OnBatchComplete(i*10, (i+1)*10, []string{"text"})
	}
}

// TestLoggingDocIndexCallback_最后一批打日志 测试最后一批总是打日志
func TestLoggingDocIndexCallback_最后一批打日志(t *testing.T) {
	cb := NewLoggingDocIndexCallback(3)
	for i := 0; i < 3; i++ {
		cb.OnBatchComplete(i*10, (i+1)*10, []string{"text"})
	}
	// 第3批（processed=3 >= total=3）会打日志，不 panic
}
