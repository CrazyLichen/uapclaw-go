package protocol

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestCurrentSchemaVersion 验证版本常量。
// Python: version.py:10 CURRENT_SCHEMA_VERSION = "0.1"
func TestCurrentSchemaVersion(t *testing.T) {
	require.Equal(t, "0.1", CurrentSchemaVersion)
}

// TestIsSchemaVersionSupported 验证四种入参。
// Python: version.py:14-19 is_schema_version_supported
func TestIsSchemaVersionSupported(t *testing.T) {
	require.True(t, IsSchemaVersionSupported("0.1"))
	require.False(t, IsSchemaVersionSupported("0.2"))
	require.False(t, IsSchemaVersionSupported(0.1), "非字符串")
	require.False(t, IsSchemaVersionSupported(nil))
}

// TestRequireSupportedSchemaVersion_通过 验证支持版本原样返回。
func TestRequireSupportedSchemaVersion_通过(t *testing.T) {
	got, err := RequireSupportedSchemaVersion("0.1")
	require.NoError(t, err)
	require.Equal(t, "0.1", got)
}

// TestRequireSupportedSchemaVersion_非字符串报TypeError
// Python: version.py:26-27 if not isinstance(version, str): raise TypeError
func TestRequireSupportedSchemaVersion_非字符串报TypeError(t *testing.T) {
	_, err := RequireSupportedSchemaVersion(1)
	require.Error(t, err)
	require.Contains(t, err.Error(), "schema_version must be a string")
}

// TestRequireSupportedSchemaVersion_不支持报ValueError
// Python: version.py:28-29 raise ValueError(f"unsupported schema_version: {version}")
func TestRequireSupportedSchemaVersion_不支持报ValueError(t *testing.T) {
	_, err := RequireSupportedSchemaVersion("9.9")
	require.Error(t, err)
	require.Contains(t, err.Error(), "unsupported schema_version: 9.9")
}
