# iLanZou 最小驱动

从 Alist 的 `drivers/ilanzou` 抽取出的独立 Go 客户端，并提供可运行的 WebDAV 服务。保留 iLanZou 登录、目录和文件操作，以及文件数据经七牛云上传/下载的链路。

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
go build -o ilanzou-webdav ./cmd/ilanzou
```

仓库同时提供无需 Go 环境即可运行的单文件可执行程序：

- Linux x86-64：`dist/ilanzou-linux-amd64`
- Windows x86-64：`dist/ilanzou-windows-amd64.exe`
- Linux x86-64 WebDAV 服务：`dist/ilanzou-webdav-linux-amd64`
- Windows x86-64 WebDAV 服务：`dist/ilanzou-webdav-windows-amd64.exe`

这些版本均以纯 Go 方式构建，运行时不依赖外部库。查看完整帮助：

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

`--no-config` 会跳过默认和显式指定的所有 TOML 文件，只从命令行和环境变量读取配置；账号、密码必须由这两种来源提供，其他可选项仍使用内置默认值。无配置运行示例：

```sh
ILANZOU_USERNAME='你的账号' ILANZOU_PASSWORD='你的密码' ./ilanzou --no-config ls
./ilanzou --no-config --username '你的账号' --password '你的密码' --root-folder-id 404038274 ls
```

参数缺失或数值无效时，CLI 会提示缺失项以及可用的命令行参数/环境变量名。`--no-config` 与 `-c/--config` 同时出现时，`--no-config` 生效，TOML 路径不会被打开。

`root_folder_id` 默认为账号根目录 `"0"`。设置为其他目录 ID 后，`ls`、新建、上传、下载、移动、重命名和删除都会限定在该目录及其子目录内；上级和同级目录中的对象会被拒绝。配置根目录本身不可移动、重命名或删除。CLI 会从配置根目录向下查找对象归属后再执行操作。

多文件上传和下载使用 Go worker 并行处理，分别受 `upload_concurrency`（默认 4）和 `download_concurrency`（默认 32）限制。并发数限制的是同时传输的文件任务；单个大文件仍按当前七牛分片流程传输。上传可省略目录 ID 以使用配置根目录；显式目标目录 ID 必须是数字 ID 并位于配置根目录内。下载参数按“文件 ID、本地路径”成对重复传入。多文件命令会输出含逐项状态的 JSON。

`move`、`rename` 和 `delete` 的类型参数可用 `file` 或 `dir`。删除为远端永久删除操作。程序输出 JSON 列表或新建/上传对象信息，包含可用于后续文件操作的 ID。

## WebDAV 服务

复制 `ilanzou-webdav.toml.example` 为 `ilanzou-webdav.toml`，分别填写 iLanZou 账号和 WebDAV 连接账号后启动。默认配置文件在可执行文件同级目录，服务默认监听 `127.0.0.1:8080`：

```sh
cp ilanzou-webdav.toml.example dist/ilanzou-webdav.toml
# 编辑配置后启动，命令返回时服务已在后台监听
./dist/ilanzou-webdav-linux-amd64 start
./dist/ilanzou-webdav-linux-amd64 stop
```

WebDAV URL 使用文件和目录名，例如 `http://127.0.0.1:8080/path/to/file-or-folder`。服务把每个路径段映射到 iLanZou 目录项，再以 ID 调用现有客户端；路径根映射到 `root_folder_id`，受该根目录范围约束。支持 `PROPFIND`、`GET`、`HEAD`、`PUT`、`MKCOL`、`DELETE`、`MOVE` 和 `COPY`。每个 WebDAV 请求都要求 HTTP Basic Auth，使用单独配置的 `webdav_username` 和 `webdav_password`；它们只验证 WebDAV 客户端，不参与 iLanZou 登录。当前服务使用 HTTP；跨不可信网络访问时应通过 HTTPS 反向代理提供加密连接。后台日志写入配置文件同目录的 `ilanzou-webdav.log`，PID 文件为 `ilanzou-webdav.pid`。

Zotero 连接检查可在 `/dav/zotero` 创建 `zotero-test-file.prop`。服务支持 1 字节文件的创建、同内容重复 PUT、内容覆盖和删除；iLanZou 对相同内容返回已有 ID 时，覆盖流程会保留该文件。

WebDAV 配置项及命令行覆盖：

| TOML 键 | 环境变量 | 命令行参数 | 默认值 |
|---|---|---|---|
| `username` | `ILANZOU_USERNAME` | `--username` | 空 |
| `password` | `ILANZOU_PASSWORD` | `--password` | 空 |
| `webdav_username` | `ILANZOU_WEBDAV_USERNAME` | `--webdav-username` | 空，必填 |
| `webdav_password` | `ILANZOU_WEBDAV_PASSWORD` | `--webdav-password` | 空，必填 |
| `ip` | `ILANZOU_IP` | `--ip` | 空 |
| `listen` | `ILANZOU_WEBDAV_LISTEN` | `--listen` | `127.0.0.1:8080` |
| `root_folder_id` | `ILANZOU_ROOT_FOLDER_ID` | `--root-folder-id` | `"0"` |
| `upload_concurrency` | `ILANZOU_UPLOAD_CONCURRENCY` | `--upload-concurrency` | `4` |
| `download_concurrency` | `ILANZOU_DOWNLOAD_CONCURRENCY` | `--download-concurrency` | `32` |
| `request_timeout` | `ILANZOU_REQUEST_TIMEOUT` | `--request-timeout` | `"10m"` |

优先级为命令行参数 > 环境变量 > `ilanzou-webdav.toml` > 内置默认值。命令行选项可放在 `start` 前后，例如：

```sh
./dist/ilanzou-webdav-linux-amd64 --config ./webdav.toml --listen 0.0.0.0:8080 start
```

Windows 可执行文件使用方式相同；`start` 会启动独立后台进程，`stop` 读取同目录 PID 文件并停止服务。

## 作为库引入

Go 程序可直接依赖本模块 `ilanzou`，使用 `NewClient` 或带根目录隔离的 `NewScopedClient`。`Client` 提供底层 API；`ScopedClient` 将列目录、新建、上传、下载、移动、重命名和删除限制在指定根目录及其子目录。调用前先运行 `Init`。

Linux x86-64 提供 C ABI 库，其他支持 C ABI/FFI 的语言也可以调用：

- `dist/libilanzou-linux-amd64.so`：动态库
- `dist/libilanzou-linux-amd64.a`：静态库
- `dist/libilanzou-linux-amd64.h`：对应头文件

Windows amd64 提供对应的 C ABI 文件：

- `dist/libilanzou-windows-amd64.dll`：动态库
- `dist/libilanzou-windows-amd64.a`：静态库
- `dist/libilanzou-windows-amd64.dll.a`：MinGW 动态库导入库
- `dist/libilanzou-windows-amd64.h`：对应头文件
- `dist/libilanzou-windows-amd64.def`：导出符号定义

C 接口采用 session handle。`IlanzouNew` 创建 handle，`IlanzouInit` 登录；列表、创建目录、上传等操作返回 JSON 字符串。下载写入指定本地路径。操作函数返回 `0` 表示成功，非 `0` 表示失败；错误文本和 JSON 字符串由库分配，调用方必须通过 `IlanzouFreeString` 释放，完成后通过 `IlanzouClose` 关闭 handle。创建 handle 时传入非空根目录 ID 会启用与 CLI 相同的目录范围限制；传空字符串或 `"0"` 表示账号根目录。请求超时参数单位为毫秒，传 `0` 使用默认 10 分钟。移动、重命名、删除函数中的 `isDir` 传 `0` 表示文件，非 `0` 表示目录。

从源码重新生成 Linux 库：

```sh
CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -buildmode=c-shared -o dist/libilanzou-linux-amd64.so ./cmd/ilanzouffi
CGO_ENABLED=1 go build -trimpath -ldflags='-s -w' -buildmode=c-archive -o dist/libilanzou-linux-amd64.a ./cmd/ilanzouffi
```

两条命令都会生成匹配的 `.h` 头文件。

使用 Windows amd64 MinGW 工具链构建：

```sh
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -trimpath -ldflags='-s -w' -buildmode=c-shared -o dist/libilanzou-windows-amd64.dll ./cmd/ilanzouffi
GOOS=windows GOARCH=amd64 CGO_ENABLED=1 CC=x86_64-w64-mingw32-gcc go build -trimpath -ldflags='-s -w' -buildmode=c-archive -o dist/libilanzou-windows-amd64.a ./cmd/ilanzouffi
x86_64-w64-mingw32-dlltool -d dist/libilanzou-windows-amd64.def -D libilanzou-windows-amd64.dll -l dist/libilanzou-windows-amd64.dll.a
```

构建 C/C++ 程序的示例：

```sh
# 动态链接
gcc app.c -Idist -Ldist -lilanzou-linux-amd64 -Wl,-rpath,'$ORIGIN' -o app

# 静态链接
gcc app.c dist/libilanzou-linux-amd64.a -pthread -ldl -lm -o app

# Windows MinGW 动态链接（把 DLL 放在 app.exe 同目录或 PATH 中）
x86_64-w64-mingw32-gcc app.c -Idist -Ldist -lilanzou-windows-amd64 -o app.exe

# Windows MinGW 静态链接
x86_64-w64-mingw32-gcc app.c dist/libilanzou-windows-amd64.a -o app.exe
```

最小 C 调用示例：

```c
#include "libilanzou-linux-amd64.h"
#include <stdio.h>
#include <stdlib.h>

int main(void) {
    uint64_t handle = 0;
    char *error = NULL;
    char *json = NULL;
    int status = IlanzouNew(getenv("ILANZOU_USERNAME"),
                            getenv("ILANZOU_PASSWORD"),
                            getenv("ILANZOU_IP"), "0", 0,
                            &handle, &error);
    if (status == 0) status = IlanzouInit(handle, &error);
    if (status == 0) status = IlanzouList(handle, "0", &json, &error);
    if (status == 0) puts(json);
    if (json != NULL) IlanzouFreeString(json);
    if (handle != 0) IlanzouClose(handle);
    if (error != NULL) {
        fprintf(stderr, "%s\n", error);
        IlanzouFreeString(error);
    }
    return status;
}
```

Windows 动态链接程序运行时需能找到对应 `.dll`；MinGW 链接时使用 `.dll.a` 导入库。Windows 静态库 `.a` 直接参与最终程序链接。

作为 Go 包使用时，创建 `ilanzou.NewClient(username, password)`，可选调用 `SetIP(ip)` 设置 `X-Forwarded-For`，再调用 `Init(ctx)`。需要根目录隔离时，用 `ilanzou.NewScopedClient(client, rootFolderID)` 执行文件操作；底层 `Client` 本身不限制目录范围。`Download` 返回的 reader 需要由调用者关闭。

## 验证说明

账号验证采用一次性测试文件：登录并列出根目录、创建测试目录、上传小文件、下载并比较内容、重命名、移动，再删除测试文件和目录。测试凭据通过进程环境变量传入，不写入本仓库或 Git 历史。

## 来源与许可

本项目提取并改写自 [Alist](https://github.com/AlistGo/alist) 的 iLanZou 驱动（Apache License 2.0）。上游驱动中的 FeijiPan 注册项、Alist 框架适配层和与文件操作无关的能力均未保留。许可证见 [LICENSE](LICENSE)。

TOML 解析器 `BurntSushi/toml` 按 MIT License 分发，版权与许可文本见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
