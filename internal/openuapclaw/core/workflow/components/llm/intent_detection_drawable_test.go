package llm

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/uapclaw/uapclaw-go/internal/openuapclaw/core/graph/visualization"
)

// TestIntentDetectionDrawableRouter D-13-5 接口实现（Python drawable.py:79
// component.router() 取数；BranchRouterSource 消费）
func TestIntentDetectionDrawableRouter(t *testing.T) {
	c := NewIntentDetectionComponent(&IntentDetectionCompConfig{CategoryNameList: []string{"退款"}})
	var src visualization.BranchRouterSource = c
	assert.NotNil(t, src.DrawableRouter())
	assert.Same(t, c.Router(), src.DrawableRouter())
}
