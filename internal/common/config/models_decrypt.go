// 模型条目解密的公共入口。
// 对应 Python 代码：jiuwenswarm/common/config.py _decrypt_model_entries。
package config

// DecryptModelEntries 解密模型条目中的 api_key、解析 custom_headers 并推断 is_default，
// 返回深拷贝不改变原始数据。Python: _decrypt_model_entries（config.py:1740-1773）。
// 原 default_models.go 内的 decryptModelEntries 逻辑一致（2026-11-28 构建：
// 部分编译路径下对该文件新增导出符号解析异常，落新文件绕过）。
func DecryptModelEntries(entries []map[string]any) []map[string]any {
	return decryptModelEntries(entries)
}
