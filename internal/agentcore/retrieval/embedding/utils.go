package embedding

import (
	"encoding/base64"
	"errors"
	"math"
)

// ──────────────────────────── 结构体 ────────────────────────────

// ──────────────────────────── 枚举 ────────────────────────────

// ──────────────────────────── 常量 ────────────────────────────

// ──────────────────────────── 全局变量 ────────────────────────────

// errInvalidBase64Length base64 解码后长度不是 4 的倍数
var errInvalidBase64Length = errors.New("base64 解码后长度不是 float32 对齐的（必须为 4 的倍数）")

// ──────────────────────────── 导出函数 ────────────────────────────

// ParseBase64Embedding 将 base64 编码的嵌入向量解码为 []float64。
//
// Python 用 numpy.frombuffer(decoded, dtype=np.float32).tolist()，
// Go 用 encoding/base64 解码 + float32 字节序解析为 float64。
//
// 对应 Python: parse_base64_embedding()
func ParseBase64Embedding(b64Str string) ([]float64, error) {
	decoded, err := base64.StdEncoding.DecodeString(b64Str)
	if err != nil {
		return nil, err
	}

	// 每 4 字节一个 float32
	if len(decoded)%4 != 0 {
		return nil, errInvalidBase64Length
	}

	n := len(decoded) / 4
	result := make([]float64, n)
	for i := 0; i < n; i++ {
		bits := uint32(decoded[i*4]) | uint32(decoded[i*4+1])<<8 |
			uint32(decoded[i*4+2])<<16 | uint32(decoded[i*4+3])<<24
		result[i] = float64(math.Float32frombits(bits))
	}

	return result, nil
}

// ──────────────────────────── 非导出函数 ────────────────────────────
