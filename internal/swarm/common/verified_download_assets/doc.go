// Package verified_download_assets 提供授权 send_file 投递的密封资产账本。
//
// 授权模式（require_execution_authorization=True）下，send_file 在 push 前
// 把授权那一刻的文件字节密封拷贝到资产目录并写 sidecar（asset_id/digest/大小/过期），
// download_token 只指向密封副本；push 成功 commit（保留至 TTL），失败 revoke。
// 下载路由 serve 前校验 digest/过期/symlink，保证用户下载到的字节即授权字节。
//
// 文件目录：
//
//	verified_download_assets/
//	├── doc.go                     # 包文档
//	└── verified_download_assets.go # Asset/Owner 类型与账本逻辑
//
// 对应 Python 代码：jiuwenswarm/common/verified_download_assets.py（409 行）
package verified_download_assets
