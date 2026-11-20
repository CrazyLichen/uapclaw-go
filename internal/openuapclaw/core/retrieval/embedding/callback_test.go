package embedding

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNoopCallback_OnBatchComplete(t *testing.T) {
	cb := NewNoopCallback()
	assert.Equal(t, 0, cb.CallCounter())

	cb.OnBatchComplete(0, 8, []string{"a", "b"})
	assert.Equal(t, 1, cb.CallCounter())

	cb.OnBatchComplete(8, 16, []string{"c"})
	assert.Equal(t, 2, cb.CallCounter())
}

func TestNoopCallback_并发安全(t *testing.T) {
	cb := NewNoopCallback()
	done := make(chan struct{})

	for i := 0; i < 100; i++ {
		go func() {
			cb.OnBatchComplete(0, 1, nil)
			done <- struct{}{}
		}()
	}

	for i := 0; i < 100; i++ {
		<-done
	}

	assert.Equal(t, 100, cb.CallCounter())
}
