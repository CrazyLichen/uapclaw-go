package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/uapclaw/uapclaw-go/internal/common/exception"
)

func TestValidateToolInputs_非map拒绝(t *testing.T) {
	_, err := validateToolInputs("not a map")
	require.Error(t, err)
	be, ok := err.(*exception.BaseError)
	require.True(t, ok)
	assert.Equal(t, 102001, be.Code())
}

func TestValidateToolInputs_透传(t *testing.T) {
	in := map[string]any{"sku": "a", "count": 3}
	out, err := validateToolInputs(in)
	require.NoError(t, err)
	assert.Equal(t, in, out)
}

func TestCreateOutput_驼峰信封(t *testing.T) {
	out := createOutput(map[string]any{
		errCodeKey:     182012,
		errMessageKey:  "boom",
		restfulDataKey: "payload",
	})
	assert.Equal(t, map[string]any{"errCode": 182012, "errMessage": "boom", "data": "payload"}, out)
}

func TestCreateOutput_缺省值兜底(t *testing.T) {
	out := createOutput(map[string]any{})
	assert.Equal(t, map[string]any{"errCode": 0, "errMessage": "", "data": ""}, out)
}
