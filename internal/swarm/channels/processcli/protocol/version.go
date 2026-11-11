package protocol

import "fmt"

// ──────────────────────────── 常量 ────────────────────────────

const (
	// CurrentSchemaVersion 当前实现的机器协议 schema 版本。
	// Python: version.py:10 CURRENT_SCHEMA_VERSION = "0.1"
	CurrentSchemaVersion = "0.1"
)

// ──────────────────────────── 全局变量 ────────────────────────────

// SupportedSchemaVersions 所有精确实现的机器协议 schema 版本。
// Python: version.py:11 SUPPORTED_SCHEMA_VERSIONS = (CURRENT_SCHEMA_VERSION,)
var SupportedSchemaVersions = []string{CurrentSchemaVersion}

// ──────────────────────────── 导出函数 ────────────────────────────

// IsSchemaVersionSupported 该版本是否为精确实现的机器协议版本。
// Python: version.py:14-19 is_schema_version_supported
func IsSchemaVersionSupported(version any) bool {
	s, ok := version.(string)
	if !ok {
		return false
	}
	for _, v := range SupportedSchemaVersions {
		if v == s {
			return true
		}
	}
	return false
}

// RequireSupportedSchemaVersion 校验版本，不做常驻连接协商。
// Python: version.py:22-30 require_supported_schema_version
func RequireSupportedSchemaVersion(version any) (string, error) {
	s, ok := version.(string)
	if !ok {
		// Python: raise TypeError("schema_version must be a string")
		return "", fmt.Errorf("schema_version must be a string")
	}
	if !IsSchemaVersionSupported(s) {
		// Python: raise ValueError(f"unsupported schema_version: {version}")
		return "", fmt.Errorf("unsupported schema_version: %s", s)
	}
	return s, nil
}
