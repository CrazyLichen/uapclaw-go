package exception

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestStatusDrawableGraphCodes drawable 族错误码值与文案对齐 Python codes.py:397-409
func TestStatusDrawableGraphCodes(t *testing.T) {
	assert.Equal(t, 112020, int(StatusDrawableGraphStartNodeInvalid.Code()))
	assert.Equal(t, 112021, int(StatusDrawableGraphEndNodeInvalid.Code()))
	assert.Equal(t, 112022, int(StatusDrawableGraphBreakNodeInvalid.Code()))
	// Python codes.py:409 DRAWABLE_GRAPH_TO_MERMAID_INVALID
	assert.Equal(t, 112043, int(StatusDrawableGraphToMermaidInvalid.Code()))
	assert.Equal(t,
		"drawable_graph to_mermaid error, reason={reason}",
		StatusDrawableGraphToMermaidInvalid.Message())
}
