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

签名所需 AES 与 Alist 所引用的 `mopan-sdk-go` 实现一致：AES-ECB、PKCS7 填充，输出十六进制。此项目用标准库实现，因此构建无第三方 Go 依赖。

iLanZou API 对查询参数顺序敏感；本实现的登录、普通 API 和下载请求均按上游顺序拼接参数。

## 构建

需要 Go 1.22 或更高版本：

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

通过环境变量提供凭据，避免密码进入命令行历史：

```sh
export ILANZOU_USERNAME='你的账号'
export ILANZOU_PASSWORD='你的密码'
# 可选：映射上游驱动的 Ip 字段，作为 X-Forwarded-For 发送
export ILANZOU_IP='你的客户端公网 IP'
./ilanzou ls
./ilanzou ls 目录ID
./ilanzou upload 目录ID ./本地文件
./ilanzou download 文件ID ./下载文件
./ilanzou mkdir 目录ID 新目录名
./ilanzou move file 文件ID 目标目录ID
./ilanzou rename file 文件ID 新文件名
./ilanzou delete file 文件ID
```

`ILANZOU_IP` 是可选项，对应 Alist iLanZou 驱动的 `Ip` 附加设置；CLI 不会自动猜测或伪造该地址。

`move`、`rename` 和 `delete` 的类型参数可用 `file` 或 `dir`。删除为远端永久删除操作。程序输出 JSON 列表或新建/上传对象信息，包含可用于后续文件操作的 ID。

作为 Go 包使用时，创建 `ilanzou.NewClient(username, password)`，可选调用 `SetIP(ip)` 设置 `X-Forwarded-For`，再调用 `Init(ctx)` 和文件操作方法。`Download` 返回的 reader 需要由调用者关闭。

## 验证说明

账号验证采用一次性测试文件：登录并列出根目录、创建测试目录、上传小文件、下载并比较内容、重命名、移动，再删除测试文件和目录。测试凭据通过进程环境变量传入，不写入本仓库或 Git 历史。

## 来源与许可

本项目提取并改写自 [Alist](https://github.com/AlistGo/alist) 的 iLanZou 驱动（Apache License 2.0）。上游驱动中的 FeijiPan 注册项、Alist 框架适配层和与文件操作无关的能力均未保留。许可证见 [LICENSE](LICENSE)。
