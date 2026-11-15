package pregel

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestRecursionError_Error 文案复刻 Python engine.py:139-141
func TestRecursionError_Error(t *testing.T) {
	e := &RecursionError{Limit: 25, Step: 26}
	assert.Equal(t, "Recursion limit of 25 reached at step 26", e.Error())
	// spec D3：engine.py:85 按类型名分类上抛，Go 以 errors.As 类型断言还原契约
	var target *RecursionError
	assert.True(t, errors.As(error(e), &target))
	assert.Equal(t, 25, target.Limit)
}
