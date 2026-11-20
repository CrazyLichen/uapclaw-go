package utils

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

// TestCreateMilvusAlias_别名非空 对齐 Python: if alias: return alias
func TestCreateMilvusAlias_别名非空(t *testing.T) {
	assert.Equal(t, "my_alias", CreateMilvusAlias("my_alias", "http://localhost:19530", ""))
}

// TestCreateMilvusAlias_无token 对齐 Python: auth_info = user or "noauth"; "-".join(["kb", uri, auth_info])
func TestCreateMilvusAlias_无token(t *testing.T) {
	result := CreateMilvusAlias("", "http://localhost:19530", "")
	assert.Equal(t, "kb-http://localhost:19530-noauth", result)
}

// TestCreateMilvusAlias_有token 对齐 Python: md5(token) 作为 auth_info
func TestCreateMilvusAlias_有token(t *testing.T) {
	token := "secret-token"
	md5sum := md5.Sum([]byte(token)) // #nosec G401 — 对齐 Python usedforsecurity=False
	expected := fmt.Sprintf("kb-http://localhost:19530-%s", hex.EncodeToString(md5sum[:]))
	assert.Equal(t, expected, CreateMilvusAlias("", "http://localhost:19530", token))
}

// TestCreateMilvusAlias_用户名 对齐 Python: auth_info = user or "noauth"
func TestCreateMilvusAlias_用户名(t *testing.T) {
	// Go 签名将 user 作为可选参数，对齐 Python 签名 (alias, uri, user="", token=None)
	assert.Equal(t, "kb-http://localhost:19530-root", CreateMilvusAlias("", "http://localhost:19530", "", "root"))
}

// TestCreateMilvusAlias_别名非空优先 对齐 Python: alias 覆盖 user/token
func TestCreateMilvusAlias_别名非空优先(t *testing.T) {
	assert.Equal(t, "fixed", CreateMilvusAlias("fixed", "http://x", "tok", "user"))
}

// TestDeduplicate_正常去重 对齐 Python deduplicate 保留顺序
func TestDeduplicate_正常去重(t *testing.T) {
	data := []string{"a", "b", "a", "c", "b"}
	result := Deduplicate(data, func(s string) string { return s })
	assert.Equal(t, []string{"a", "b", "c"}, result)
}

// TestDeduplicate_空切片 对齐 Python: 空输入返回空列表
func TestDeduplicate_空切片(t *testing.T) {
	result := Deduplicate([]int{}, func(i int) int { return i })
	assert.Empty(t, result)
}

// TestDeduplicate_自定义key 对齐 Python deduplicate(data, key=...)
func TestDeduplicate_自定义key(t *testing.T) {
	type item struct{ k, v string }
	data := []item{{"a", "1"}, {"b", "2"}, {"a", "3"}}
	result := Deduplicate(data, func(i item) string { return i.k })
	assert.Equal(t, []item{{"a", "1"}, {"b", "2"}}, result)
}

// TestDeduplicate_无重复 对齐 Python: 无重复时原样返回
func TestDeduplicate_无重复(t *testing.T) {
	data := []int{1, 2, 3}
	result := Deduplicate(data, func(i int) int { return i })
	assert.Equal(t, []int{1, 2, 3}, result)
}
