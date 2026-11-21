package constants

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestMaxConstants_取值对齐Python 对齐 constant.py:25-27
func TestMaxConstants_取值对齐Python(t *testing.T) {
	assert.Equal(t, 100000, MaxCollectionSize)
	assert.Equal(t, 5000, MaxExpressionLength)
	assert.Equal(t, 50, MaxASTDepth)
}
