# 从源码构建服务端

需要 Go 1.25 或更高版本；首次构建需要下载 go.mod/go.sum 锁定的依赖。服务端使用纯 Go SQLite，设置 CGO_ENABLED=0 即可交叉编译，不需要 C 编译器、Unity、Android SDK 或原游戏 APK。

运行需要发布包提供的客户端和服务端资源；游戏资源不由源码构建生成。资源包的选择和更新步骤见 [Releases](https://github.com/kuuhaku1314/kairisei-ma-ch/releases)。

在仓库根运行，默认先在本机执行测试和 vet，再生成 Windows x64、Linux x64、Linux ARM64 三个程序：

```powershell
# Windows PowerShell 5.1 或 PowerShell 7
.\tools\powershell\Build-Server.ps1
# 仅构建一个目标；已检查过的源码可加 -SkipTests
.\tools\powershell\Build-Server.ps1 -Targets windows-amd64 -SkipTests
```

```sh
# Linux / POSIX shell
sh tools/sh/Build-Server.sh
# 仅构建一个目标
sh tools/sh/Build-Server.sh linux-arm64 --skip-tests
```

输出均在 `_local/bin/`：`kairi-server.exe`、`kairi-server-linux-amd64`、`kairi-server-linux-arm64`。
构建会覆盖该目录中上次的同名编译结果；PowerShell 加 `-DryRun`、shell 加 `--dry-run` 可预览。
二进制内已包含运营后台网页、SQLite、可选 S3/R2 同步功能，不需要单独构建前端或安装 Python。
运行 `kairi-server.exe -version`（Linux 使用对应程序）可查看服务端版本；此命令直接退出，不启动服务或打开存档。

也可直接执行：

```sh
cd server
go test ./...
go vet ./...
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -buildvcs=false -trimpath '-ldflags=-s -w' -o ../_local/bin/kairi-server-linux-arm64 ./cmd/kairi-server
```

源码内的合成协议与领域测试不需要游戏资源。需要原始 master / 完整运行资源的检查会明确跳过；有完整资源时，将 `CN602_RUNTIME_SET` 设为含 `resource-set.json` 的绝对目录，并用 `CN602_PLATFORM_CONTENT` 指定配套的 `delivery.json` 绝对路径。多个路径在 Windows 用分号分隔，在 Linux 用冒号分隔。这不会启动常驻服务。通过交叉编译不代表目标设备的动态验收已经完成。

## Windows 图形启动器

```powershell
.\tools\powershell\Build-Launcher.ps1
```

使用 Windows 自带 .NET Framework 4 的 C# 编译器，输出 `_local/bin/Kairisei-Launcher.exe`。将它复制到完整服务端运行目录，与 `Start-Server.ps1`、`Stop-Server.ps1` 和 `deployment.json` 放在一起。启动器读取该目录的连接设置；本机连接默认使用 `127.0.0.1:26040`。

不传图标也能独立构建。需要自定义图标时可加 `-IconPath C:\Icons\launcher.ico`；图标和游戏资源不存入源码仓库。`-DryRun` 可以只查看构建目标。

## 源码和工具入口

| 路径 | 用途 |
| --- | --- |
| `server/cmd/kairi-server/` | 游戏服务端及独立的 CDN 同步命令 |
| `cmd/kairi-launcher-winforms/` | Windows 图形启动器源码，调用运行目录中的启停脚本 |
| `tools/powershell/Build-Server.ps1`、`tools/sh/Build-Server.sh` | 编译服务端 |
| `tools/powershell/Build-Launcher.ps1` | 编译 Windows 启动器 |
| `tools/powershell/Start-Server.ps1`、`Stop-Server.ps1` | 通过 `-PackageRoot` 调用已解压服务端的配套启停脚本 |
| `tools/portable/` | 当前完整服务端使用的运行脚本模板 |
| `tools/config/cdn-sync.example.json` | 可复制后填写的存储配置示例 |

`tools/portable/` 的脚本放到完整运行目录后使用。Windows 和 Linux 均通过 `-resource-set` 读取资源清单，通过 `deployment.json` 选择平台。两端均接入 Admin 运行日志和可选的运营图片同步配置。运行目录需要配套启动脚本、资源目录、`deployment.json` 及 CDN 配置。

## 使用构建结果

**完整服务端 7z 分卷解压出的目录是运行目录。保留其中的 `resource-set/` 和 `platform/`，在源码目录完成编译。**

构建输出可用于包含完整资源的运行目录。可执行文件、启动脚本与资源的布局如下：

运行目录的相对位置应保持如下（只列出主要文件）：

```text
KairiseiMA-Server/             ← 运行目录，也是 -PackageRoot 指向的位置
├── kairi-server.exe            ← Windows 服务端
├── kairi-server-linux-amd64    ← Linux x64 服务端
├── kairi-server-linux-arm64    ← Linux ARM64 服务端
├── Kairisei-Launcher.exe       ← Windows 图形启动器
├── Start-Server.ps1
├── Stop-Server.ps1
├── Start-Server-linux-amd64.sh
├── Start-Server-linux-arm64.sh
├── deployment.json            ← 保留包内配套文件
├── resource-set/               ← 保留解压出来的整个资源目录
│   ├── resource-set.json
│   └── ...                    ← 其余资源及配置，保持原来的内部路径
├── platform/                  ← PC／Android 资源清单与资源
├── cdn.json                    ← 可选 CDN 下载配置
└── _local/data/                ← 首次启动后生成的数据库与存档
```

例如，Windows 将 **源码目录的 `_local/bin/kairi-server.exe`** 复制到 **`D:\Games\KairiseiMA-Server\kairi-server.exe`**，然后双击同目录的 `Kairisei-Launcher.exe` 即可。更新启动器源码后，同样重新构建并替换 `Kairisei-Launcher.exe`。

**`resource-set/`、`platform/`、`deployment.json` 与服务端程序放在同一层。** 不需要把资源复制进源码的 `server/` 或 `_local/bin/`，也不需要单独拆开资源目录。发布版本升级时一起更新配套服务端、资源包和客户端，保留 `_local/data/` 中的数据库与自己的部署配置。

Linux 使用与架构相符的可执行文件和启动入口；运行目录结构相同。

### 启动已组装的服务端

请在完整服务端目录使用配套启动器或脚本。Windows 使用 `Kairisei-Launcher.exe`，Linux x64 可运行 `sh Start-Server-linux-amd64.sh 服务器IPv4 26040`，ARM64 使用对应的 `Start-Server-linux-arm64.sh`。将地址和端口换成自己的配置。

在 `server/` 目录运行 `go run ./cmd/kairi-server -h` 可查看参数。服务模式使用 `-resource-set <resource-set.json>`，平台资源清单通过 `-platform-content-config` 从完整包的 `deployment.json` 读取。封面和 master 路径由资源清单统一指定。

## 配置与持久化

`server/config/` 是默认配置；实际运行以完整资源包的配置入口初始化。运营后台编辑的卡池、Boss 掉落、活动／往期副本体力与经验、商店／兑换、公告、奖励、购买开关等保存到 `_local/data/` 中的 SQLite 公共配置，重启保留，不需重新编译；CDN 和监听地址使用部署文件。更新程序保留自己的数据库和部署配置，不提交到 Git。

礼盒内容、背包固定兑换配方和道具类型由配套资源集定义，客户端也读取相应主表。此类更新应一起升级服务端资源和客户端；编译服务端程序不会生成或更新这些游戏资源。

卡牌和 Boss 使用统一内容编号与主表结构，运行时按职业、类型和能力处理。账号存档保存玩家进度，公共主表提供定义和派生属性；加载主表不会重新授予初始道具或重置库存。

运行配置只保存实际使用的参数、目录和引用。修改配置时应满足对应加载器的字段、数值及引用约束；数据库格式升级和账号初始化仍由持久化层处理。维护测试应覆盖当前业务结果、存档、交易与并发边界，原生行为比对所需的原始输入读取器只用于测试。
