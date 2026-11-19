package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestString(t *testing.T) {
	assert.Equal(t, "a", String("a"))
	assert.Equal(t, "", String(1))
	assert.Equal(t, "", String(nil))
	assert.Equal(t, "", String([]any{"x"}))
}

func TestStringList(t *testing.T) {
	assert.Equal(t, []string{"a"}, StringList([]string{"a"})) // []string 透传
	assert.Equal(t, []string{"a", "b"}, StringList([]any{"a", 1, "b", nil}))
	assert.Nil(t, StringList("x"))
	assert.Nil(t, StringList(nil))
}

func TestStringListOrEmpty(t *testing.T) {
	assert.Equal(t, []string{"a"}, StringListOrEmpty([]any{"a"}))
	assert.Equal(t, []string{}, StringListOrEmpty("x")) // JSON 序列化为 []，非 null
	b, err := json.Marshal(StringListOrEmpty(nil))
	assert.NoError(t, err)
	assert.Equal(t, "[]", string(b))
}

func TestDictList(t *testing.T) {
	d := map[string]any{"k": "v"}
	got := DictList([]any{d, "x", 1})
	assert.Equal(t, []map[string]any{d}, got)
	assert.Empty(t, DictList("x"))
}

func TestIsEmptyList(t *testing.T) {
	assert.True(t, IsEmptyList([]any{}))
	assert.True(t, IsEmptyList("not-a-list"))
	assert.True(t, IsEmptyList(nil))
	assert.False(t, IsEmptyList([]any{1}))
}
