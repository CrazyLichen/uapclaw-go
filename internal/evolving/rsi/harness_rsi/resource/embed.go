package resource

import _ "embed"

// DefaultConfigTemplate 默认配置模板字节内容（go:embed）
//
//go:embed orchestrating.default.yaml
var DefaultConfigTemplate []byte
