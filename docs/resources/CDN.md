# 可选资源 CDN

默认关闭。`cdn.json` 只保存公开下载地址，重启服务、重新登录后生效：

```json
{"base_url":"https://pub-你的编号.r2.dev/cn602"}
```

清空 `base_url` 即恢复本地下载。Windows 启动器、命令行和 Linux x64／ARM64 共用此配置。
使用配套启动脚本时，编辑解压目录中的 `cdn.json`。自行组装启动参数时，使用 `-cdn-config <cdn.json 路径>`。

CDN 可使用任意能提供原样文件的 HTTP(S) 源站或域名，不绑定厂商。只改变登录响应中的
`res_patch_url`、`res_cpk_url`；API、战斗、版本目录、动态 banner 和 Admin 保持原地址。
服务端仍需完整 `resource-set`。切换 CDN 后重新登录即可。客户端与服务端资源需配套，包的选择和更新步骤见 Release 说明。
不会自动回源重试；CDN 故障时可关闭配置后重新登录。

## 一份 JSON，一次运行

在服务端解压目录，把 `cdn-sync.example.json` 复制为 `cdn-sync.json`，只需填写：

```json
{
  "endpoint_url": "https://你的账户ID.r2.cloudflarestorage.com",
  "bucket": "kairisei",
  "access_key_id": "你的访问密钥ID",
  "secret_access_key": "你的机密访问密钥",
  "public_url": "https://pub-你的编号.r2.dev"
}
```

`endpoint_url` 是 S3 上传 API 地址；`public_url` 是**桶的公开下载根地址**，不要添加 `/cn602`。
先在 R2 启用公开读取，密钥需对该桶有对象读写权限；使用访问密钥ID与机密访问密钥，不用“令牌值”。
密钥只存在你本地的 `cdn-sync.json`，不要把填好的文件随包分享；启用运营图片同步时，服务端也会读取这份文件中的存储配置。

- **Windows**：双击 `Sync-CDN.cmd`，窗口显示进度，结束后保留结果。
- **Linux x64／ARM64**：`sh Sync-CDN.sh`，脚本自动选择架构。
- **只检查输入**：Windows `Sync-CDN.cmd --dry-run`，Linux `sh Sync-CDN.sh --dry-run`；不联网、不上传、不修改 CDN 配置。

同步能力内置于 server，无需 Python、Go、AWS CLI 或其他安装；命令独立退出，不启动游戏服务、不打开账号库。
随包同步脚本读取 `deployment.json` 选择的平台资源，以及完整资源集里的共用资源。上传范围是客户端下载的资源。
默认8并发，流式上传，不复制资源、不把整包放进内存；存在且大小／SHA元数据一致则跳过。
失败或 Ctrl+C 后直接重跑，已完成的文件无需重传；单个未完成文件会重新上传。
记录自动写入 `_local/cdn-sync/`，不含密钥或代理密码。

上传完成后抽查公开 HEAD、Range 206 和原始字节。全部成功才更新 `cdn.json`；失败保留原配置。
成功后重启游戏服务、配套客户端重新登录即可。脚本不自动重启正在运行的服务。

## 代理与其他存储

上传和公开下载检查都使用进程环境变量 `HTTP_PROXY`、`HTTPS_PROXY`、`NO_PROXY`，兼容小写；大写优先。
HTTPS目标使用 `HTTPS_PROXY`，其值可以是 HTTP 代理（通过 CONNECT）；只有 `HTTP_PROXY` 不会代理 HTTPS。
不读取 Windows 系统代理面板，也不读取 `ALL_PROXY`；无需在 JSON 重复配置。
例如先在 PowerShell 设置 `$env:HTTPS_PROXY='http://127.0.0.1:7890'`，再运行 `.\Sync-CDN.cmd`；
Linux 可执行 `HTTPS_PROXY=http://127.0.0.1:7890 sh Sync-CDN.sh`。`NO_PROXY` 指定的目标直连。

可选 JSON 字段：`prefix`（默认 `cn602`）、`region`（默认 `auto`）、`workers`（1–32，默认8）、
`addressing_style`（`path`／`virtual`／`auto`，默认 `path`）、`session_token`（临时凭据）。
`public_url` 加上 `prefix` 就是最终下载地址。AWS S3 可省略 endpoint，填写实际 region 和 `virtual`；
其他兼容存储按厂商说明填写。对象接口需支持 HEAD、条件 PUT、Content-MD5 与 SHA元数据。
不填 JSON 中的两项密钥时，可通过 `AWS_ACCESS_KEY_ID`、`AWS_SECRET_ACCESS_KEY`、`AWS_SESSION_TOKEN` 提供。

高级命令：`kairi-server.exe -sync-cdn cdn-sync.json`；可加 `-cdn-sync-dry-run`、
`-resource-set 路径`、`-cdn-config 输出路径`。默认相对当前目录定位完整资源集和 `cdn.json`；脚本会先切换到自身目录。
源码仍可用 `-export-cdn-manifest 新文件路径` 单独导出同一份映射，供审计和其他上传方式使用。

远端同名对象内容不同会拒绝覆盖，不删除旧文件。首次配置时可以删除冲突对象再运行；
**更新已发布资源时应提升版本，或换 prefix**，否则客户端／CDN中的旧缓存可能继续生效。
`r2.dev` 适合测试，有速率限制且不使用 CDN 缓存；换同桶自定义域名无需重传资源。
实现依据：[Cloudflare Go SDK示例](https://developers.cloudflare.com/r2/examples/aws/aws-sdk-go/)、
[S3兼容合同](https://developers.cloudflare.com/r2/api/s3/api/)、[公开桶说明](https://developers.cloudflare.com/r2/buckets/public-buckets/)。

## 版本与别名的唯一来源

- Windows 和 Android 资源按各自的平台清单分发。保持 `deployment.json`、`platform/` 与 `resource-set/` 的原有相对路径，使用随包脚本生成上传映射。
- 语音对象名是 `cpk/CPK/<逻辑名>.v<整数版本>`。源文件、逻辑名、大小、版本在 `resources/cpk.File` 中一次解析，
  文件列表、版本表、本地下载和 CDN 导出共用。别名必须直指物理 CPK，不允许链式别名或大小写重名。
  查找时忽略大小写，输出路径保留客户端约定的拼写，不把 Unity 内部名字、磁盘文件名和逻辑名混成一个名字。
- CPK 使用原客户端的整数版本协议，与 bundle 的 CRC 不是同一种字段。完整资源集的CPK根目录必须提供
  `versions.json`，并在 `resource-set.json` 登记大小／SHA。它是版本唯一来源，未登记的文件默认1；
  别名至少继承物理源版本。格式示例：
  `{"schema_version":1,"versions":[{"name":"cv_navi_5.cpk","version":4}]}`。
  资源维护时应沿用已发布版本并为变更字节递增；CDN导出拒绝未登记或哈希不符的元数据，不把它上传为音频对象。
  **今后修改CPK内容必须提升对应版本并登记新完整集**；无需修改服务端角色判断或重新编译。
  不直接改动既有只读来源／完整集。上传冲突检查是最后一道保护，不代替版本登记。
- SHA-256 是完整资源与上传字节的审计身份，不替代客户端 CRC／整数版本；ETag 也不当作 SHA-256。
  `resource-set.json` 的 SHA 绑定导出清单，移动完整资源目录不改变清单中的相对路径。
  登录返回的版本目录namespace由最终catalog、CPK文件表及CPK版本表共同生成，资源变更自动刷新目录缓存；
  原 `version.dat` 的790保持不变。切换CDN不修改namespace，旧登录URL仍可读取当前清单。

公开源站应允许匿名 GET／HEAD、返回正确长度并保留 Range 206；禁用 gzip／转码／文件内容重写。
已版本化对象使用长期不可变缓存，不缓存错误响应；不要在客户端地址中填带临时签名查询串的 URL。
下载一直停在 0% 时，可在出现问题的设备上检查到 CDN 域名的 DNS、TLS 和网络连接，并确认公开地址允许匿名下载。
