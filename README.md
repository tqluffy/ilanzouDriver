# iLanZou 最小驱动

从 Alist 的 `drivers/ilanzou` 抽取出的独立 Go 客户端。只保留 iLanZou 登录、目录和文件操作，以及文件数据经七牛云上传/下载的链路；不包含 Alist 的存储注册、数据库、WebDAV、缓存、任务队列、后台服务或其他网盘驱动。

## 原仓库链路梳理

Alist 入口在 `drivers/ilanzou/driver.go`，签名和登录请求在 `util.go`，账号和服务常量在 `meta.go`。适配层通过 `model.Obj`、`driver.Driver` 和 Alist 的 Resty/流处理封装接入框架。本实现保留它们背后的远端请求：

1. 获取设备 UUID：`GET /unproved/getUuid`。
2. 账号登录：`POST /unproved/login`，提交用户名和密码，取得 `appToken`；随后 `GET /proved/user/account/map` 获取 `userId` 和七牛对象 key 所需的 account。鉴权参数包含 UUID、设备信息、时间戳和 AES 加密后的时间戳。凭据和 token 只保存在运行时内存。
3. 列目录：分页请求 `/proved/record/file/list`。iLanZou 维护文件名、文件 ID、父目录、大小和时间等元数据。
4. 上传：先以 MD5、文件名、大小和目录 ID 请求 `/proved/7n/getUpToken`；再将文件数据直接上传到七牛 `upload.qiniup.com`。不超过 8 MiB 使用 multipart form，较大文件按 8 MiB 分片并提交分片 ETag。上传完成后以 `/unproved/7n/results` 将七牛 token 交回 iLanZou，由 iLanZou 创建文件记录。
5. 下载：通过 `/unproved/file/redirect` 传入文件 ID、用户 ID 和签名，取得七牛/文件 CDN 的重定向地址，再从该地址读取文件内容。
6. 编辑：目录创建、文件/目录重命名、移动和删除均走 iLanZou API；文件内容不经过这些操作。删除使用原驱动的 `status: 0` 行为。

签名所需 AES 与 Alist 所引用的 `mopan-sdk-go` 实现一致：AES-ECB、PKCS7 填充，输出十六进制。TOML 配置由 `BurntSushi/toml` 解析；发布的 CLI 可独立运行，不需要额外运行时。

iLanZou API 对查询参数顺序敏感；本实现的登录、普通 API 和下载请求均按上游顺序拼接参数。

## 构建

需要 Go 1.22 或更高版本。首次构建会下载 TOML 解析依赖：

```sh
go build -o ilanzou ./cmd/ilanzou
```

仓库同时提供无需 Go 环境即可运行的单文件可执行程序：

- Linux x86-64：`dist/ilanzou-linux-amd64`
- Windows x86-64：`dist/ilanzou-windows-amd64.exe`

两种版本均以纯 Go 方式构建，运行时不依赖外部库。查看完整帮助：

```sh
./dist/ilanzou-linux-amd64 -h
./dist/ilanzou-linux-amd64 upload -h
```

Windows PowerShell：

```powershell
.\dist\ilanzou-windows-amd64.exe -h
```

## 使用

CLI 从 `ilanzou.toml` 读取配置。默认文件在可执行文件同级目录；缺省文件可以不存在。复制示例文件后填写账号：

```sh
cp ilanzou.toml.example ilanzou.toml
# 如果运行 dist 下的可执行文件，把 ilanzou.toml 放在 dist/ 同级。
./ilanzou ls
./ilanzou -c ./other.toml ls
./ilanzou ls 目录ID
./ilanzou upload ./本地文件
./ilanzou --upload-concurrency 4 upload ./a.bin ./b.bin
./ilanzou upload 348006267 ./a.bin ./b.bin
./ilanzou download 文件ID ./下载文件
./ilanzou --download-concurrency 8 download 文件ID1 ./a.bin 文件ID2 ./b.bin
./ilanzou mkdir 目录ID 新目录名
./ilanzou move file 文件ID 目标目录ID
./ilanzou rename file 文件ID 新文件名
./ilanzou delete file 文件ID
```

配置优先级为命令行参数 > 环境变量 > `ilanzou.toml` > 内置默认值。所有 TOML 配置项都能通过命令行参数覆盖：

| TOML 键 | 环境变量 | 命令行参数 | 默认值 |
|---|---|---|---|
| `username` | `ILANZOU_USERNAME` | `--username` | 空 |
| `password` | `ILANZOU_PASSWORD` | `--password` | 空 |
| `ip` | `ILANZOU_IP` | `--ip` | 空 |
| `root_folder_id` | `ILANZOU_ROOT_FOLDER_ID` | `--root-folder-id` | `"0"` |
| `upload_concurrency` | `ILANZOU_UPLOAD_CONCURRENCY` | `--upload-concurrency` | `4` |
| `download_concurrency` | `ILANZOU_DOWNLOAD_CONCURRENCY` | `--download-concurrency` | `32` |
| `request_timeout` | `ILANZOU_REQUEST_TIMEOUT` | `--request-timeout` | `"10m"` |

`-c/--config` 可在命令前或命令后指定配置文件路径；默认查找可执行文件同级的 `ilanzou.toml`。密码可以放在命令行参数中，但 shell 历史和进程参数可能会记录它，建议使用配置文件或环境变量。

`root_folder_id` 默认为账号根目录 `"0"`。设置为其他目录 ID 后，`ls`、新建、上传、下载、移动、重命名和删除都会限定在该目录及其子目录内；上级和同级目录中的对象会被拒绝。配置根目录本身不可移动、重命名或删除。CLI 会从配置根目录向下查找对象归属后再执行操作。

多文件上传和下载使用 Go worker 并行处理，分别受 `upload_concurrency`（默认 4）和 `download_concurrency`（默认 32）限制。并发数限制的是同时传输的文件任务；单个大文件仍按当前七牛分片流程传输。上传可省略目录 ID 以使用配置根目录；显式目标目录 ID 必须是数字 ID 并位于配置根目录内。下载参数按“文件 ID、本地路径”成对重复传入。多文件命令会输出含逐项状态的 JSON。

`move`、`rename` 和 `delete` 的类型参数可用 `file` 或 `dir`。删除为远端永久删除操作。程序输出 JSON 列表或新建/上传对象信息，包含可用于后续文件操作的 ID。

作为 Go 包使用时，创建 `ilanzou.NewClient(username, password)`，可选调用 `SetIP(ip)` 设置 `X-Forwarded-For`，再调用 `Init(ctx)` 和文件操作方法。`Download` 返回的 reader 需要由调用者关闭。

## 验证说明

账号验证采用一次性测试文件：登录并列出根目录、创建测试目录、上传小文件、下载并比较内容、重命名、移动，再删除测试文件和目录。测试凭据通过进程环境变量传入，不写入本仓库或 Git 历史。

## 来源与许可

本项目提取并改写自 [Alist](https://github.com/AlistGo/alist) 的 iLanZou 驱动（Apache License 2.0）。上游驱动中的 FeijiPan 注册项、Alist 框架适配层和与文件操作无关的能力均未保留。许可证见 [LICENSE](LICENSE)。

TOML 解析器 `BurntSushi/toml` 按 MIT License 分发，版权与许可文本见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
