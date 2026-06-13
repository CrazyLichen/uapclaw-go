package embedding

import (
	"encoding/base64"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseBase64Embedding(t *testing.T) {
	// 构造 float32 数组的 base64 编码
	vec := []float32{1.0, 2.0, 3.0}
	bytes := make([]byte, len(vec)*4)
	for i, v := range vec {
		bits := math.Float32bits(v)
		bytes[i*4] = byte(bits)
		bytes[i*4+1] = byte(bits >> 8)
		bytes[i*4+2] = byte(bits >> 16)
		bytes[i*4+3] = byte(bits >> 24)
	}
	b64Str := base64.StdEncoding.EncodeToString(bytes)

	result, err := ParseBase64Embedding(b64Str)
	require.NoError(t, err)
	assert.Len(t, result, 3)
	assert.InDelta(t, 1.0, result[0], 0.001)
	assert.InDelta(t, 2.0, result[1], 0.001)
	assert.InDelta(t, 3.0, result[2], 0.001)
}

func TestParseBase64Embedding_无效Base64(t *testing.T) {
	_, err := ParseBase64Embedding("!!!invalid!!!")
	assert.Error(t, err)
}

func TestParseBase64Embedding_长度不对齐(t *testing.T) {
	// 3 字节不是 4 的倍数
	b64Str := base64.StdEncoding.EncodeToString([]byte{1, 2, 3})
	_, err := ParseBase64Embedding(b64Str)
	assert.Error(t, err)
}
