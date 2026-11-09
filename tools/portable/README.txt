服务端运行脚本

这些文件用于包含完整资源的服务端运行目录。Windows 使用 Kairisei-Launcher.exe 或
Start-Server.cmd，正常停止使用 Stop-Server.cmd。默认主端口 26040，战斗端口
为主端口 +1，Admin 为主端口 +2，仅在本机监听。

Windows 从 deployment.json 读取连接设置和平台资源。Linux 示例：
  sh Start-Server-linux-amd64.sh 192.168.1.100 26040
  sh Start-Server-linux-arm64.sh 192.168.1.100 26040
Linux 按 Ctrl+C 或发送 SIGTERM 正常停服。加 --dry-run 可查看参数而不启动。

完整运行目录还需要对应平台的服务端程序、resource-set/、platform/ 和 deployment.json。
固定资源从 resource-set/resource-set.json 读取。源码工具目录不包含游戏资源，
请保留完整发布包中的这些文件。更新脚本时保留自己的部署、CDN 配置和存档。

Windows 每次运行的 stdout.log、stderr.log 位于 _local/runtime/server-runs/。
Linux 启动时打印本次 stdout.log 路径；Admin“运行日志”自动读取该日志。

Sync-CDN.cmd 和 Sync-CDN.sh 上传当前部署选择的平台资源与共用资源。
先将 cdn-sync.example.json 复制为 cdn-sync.json 并填写自己的存储配置。
加 --dry-run 只检查输入。详细配置参见仓库 docs/resources/CDN.md。
玩家连接和存档备份说明参见仓库 README.md；源码构建参见 docs/BUILD.md。
