# iLanZou 驱动工作日志

记录日期：2026-10-08（Asia/Shanghai）

## 当前实现

- 工作区 `/home/codex/project/ilanzouDriver` 是独立 Go 模块，不依赖 Alist 运行时或第三方 Go 模块。
- `client.go` 实现 iLanZou 登录、目录列表/创建、文件上传/下载、移动、重命名和删除；文件数据使用七牛上传接口及 iLanZou 下载重定向。
- `cmd/ilanzou/main.go` 提供独立 CLI。帮助支持 `-h`、`--help` 和子命令帮助；账号凭据从 `ILANZOU_USERNAME`、`ILANZOU_PASSWORD` 环境变量读取。
- `README.md` 记录来源调用链、构建方式、CLI 用法和验证说明。
- `/opt/source-code/alist` 仅用于读取分析，未修改。

## 可执行文件

- `dist/ilanzou-linux-amd64`：Linux x86-64，静态链接的 ELF 可执行文件。
- `dist/ilanzou-windows-amd64.exe`：Windows x86-64 控制台 PE 可执行文件。
- 两个文件都可直接显示完整 `-h` 帮助，且不需要外部运行库。

## 账号与目录验证

- 使用用户提供的 iLanZou 账号完成登录和文件操作验证；凭据没有写入仓库或本日志。
- 目标目录 `/测试/ilanzouDriver/` 位于目录 ID `347025631` 下。原目录不存在，本次创建后目录 ID 为 `404038274`。
- 中大型矩阵覆盖 1 B、1 KiB、1 MiB、8 MiB−1 B、8 MiB、8 MiB+1 B、32 MiB、100,000,000 B、200,000,000 B；上传 API 成功，下载长度及 MD5 均匹配，8 MiB 分片边界通过。
- 使用用户提供的 IP 配置 `ILANZOU_IP` 后，执行 40 个 1 B–1 MiB 小文件尺寸样本。样本包括 1、2、3、4、7、8、15、16 B，以及 1 KiB 至 512 KiB 多个二进制边界前后值和 1 MiB−1/1 MiB。40 次上传 API 均成功，下载长度及 MD5 均匹配。
- 同一 40 个小文件样本均通过移入临时目录、重命名、恢复原名/位置和删除。最终删除测试通过。
- 删除使用旧参数/请求路径时曾连续返回 HTTP 409；使用原仓库 `Ip` 对应的 XFF 后，矩阵删除成功。
- 最终只读复核确认目标目录 `404038274` 的条目数为 0；临时操作目录也已删除。

## 测试对象现状

以下是较早中大型矩阵的对象记录；下载校验通过，后续已按准确 ID 和名称删除：

| 大小 | 对象 ID | 文件名 |
|---:|---:|---|
| 1 B | `41026240146` | `ilz-check-30bd5c189d9dfbbd3fc36f71-1.bin` |
| 1 KiB | `41026246017` | `ilz-sweep-8c909b1346e98d08-1024.bin` |
| 1 MiB | `41026246078` | `ilz-sweep-8c909b1346e98d08-1048576.bin` |
| 8 MiB−1 B | `41026246219` | `ilz-sweep-8c909b1346e98d08-8388607.bin` |
| 8 MiB | `41026246524` | `ilz-sweep-8c909b1346e98d08-8388608.bin` |
| 8 MiB+1 B | `41026246618` | `ilz-sweep-8c909b1346e98d08-8388609.bin` |
| 32 MiB | `41026247123` | `ilz-sweep-8c909b1346e98d08-33554432.bin` |
| 100,000,000 B | `41026248211` | `ilz-sweep-8c909b1346e98d08-100000000.bin` |
| 200,000,000 B | `41026254224` | `ilz-sweep-8c909b1346e98d08-200000000.bin` |

- 这 9 个对象的移动、重命名和删除均在后续测试中通过，测试文件已清理。

此前还有一个 1 B 测试对象留在父目录 `/测试/`（目录 ID `347025631`）：ID `41026194253`，名称 `ilz-check-623788fb039e83831d7e27f9-1.bin`。该对象不在 `/测试/ilanzouDriver/` 中，未在本次尺寸矩阵中操作；对它的删除请求也被服务端以 HTTP 409/403 拒绝。

在修复前曾有 4 个 1 B 诊断对象创建在 `/测试/ilanzouDriver/`，均可下载到匹配的单字节内容，但当时上传结果接口返回“token不能为空”。这 4 个对象后续已清理：

| 对象 ID | 文件名 |
|---:|---|
| `41026503936` | `ilz-xff-onebyte-a51c0d453c15eb83.txt` |
| `41026504393` | `ilz-xff-onebyte-75f0037e8aa71431.txt` |
| `41026505093` | `ilz-xff-onebyte-cae5e8014eab9343.txt` |
| `41026519010` | `ilz-xff-onebyte-4ec7a598ad371542.txt` |

40 个小文件矩阵对象的文件名统一使用前缀 `ilz-small-9e7d3adc0265296f-`，已在移动/重命名/删除矩阵中全部删除。

## 已发现问题与代码调整

- 初版将 iLanZou API 查询参数交给 `url.Values.Encode()` 排序。现已改为按上游顺序构造登录和普通 API 请求参数，并补齐上游请求头；下载重定向本身也使用固定顺序。
- 参数顺序修复后，文件列表和账号访问正常，但删除请求仍被服务端返回 HTTP 409/403。删除问题未解决。
- 1 B 上传返回 Qiniu `BadToken`，虽然对应对象的下载字节正确；该上传响应异常尚未解决。
- 原仓库历史显示小文件上传的 `fileSize/1024 + 1` 修复（`a2dc45a8`）已包含在当前独立版。
- 原仓库还包含 iLanZou IP 封锁修复（`f25be154`）：对 API 和下载重定向附加可配置的 `X-Forwarded-For`；以及 API `Accept-Encoding` 头修复（`6812ec9a`）。此前独立版缺少这两项，目前已加入可选 `ILANZOU_IP`/`Client.SetIP` 和对应请求头。
- API 请求现在声明与上游相同的 `Accept-Encoding`，并可解码 gzip/deflate；加入后，对目标目录 ID `404038274` 的只读列目录成功，返回 10 个条目。
- 原仓库 Resty 通过 `createMultipartHeader` 按 `name` 后 `filename` 顺序构造文件 part，并对非 2xx Qiniu 响应继续尝试 `/7n/results`。独立版现已对齐该 multipart 头格式、完整 multipart 写入和结果接口判定。
- 初期的 IP 转发和小文件差异修正后，使用用户提供的 IP 执行 40 个 1 B–1 MiB 样本：上传 API、下载长度/MD5 校验、移动、重命名、恢复和删除均通过。1 B 上传异常已消除。
- 同一 IP 下的 XFF 读取目标目录正常；此前 HTTP 409/403 属于未配置 XFF 时的结果。1 B 的“token不能为空”属于更早版本的诊断结果，已被最终 40 项成功矩阵覆盖。
- 早期验证脚本在成功清理路径中清空测试目录 ID 后，延迟清理仍用空 ID 查询并删除返回对象。用户报告发生误删；该脚本逻辑是误删风险来源。用户表示自行恢复，未执行恢复操作。

## Git 状态

- 查询参数顺序修复提交：`9d4d9fe Preserve iLanZou API query parameter order`
- XFF 配置支持提交：`c61f35d Add upstream iLanZou IP forwarding support`
- Resty 小文件上传行为对齐提交：`262cf13 Match upstream iLanZou upload request behavior`
- 文件操作尺寸矩阵日志提交：`f8dcce8 Record file operation test results`
- 首次工作日志提交：`728e1a8 Document iLanZou implementation status`
- 提交身份：`tqluffy <tqluffy@qq.com>`
- 最近的 Resty 对齐提交包含重新构建的 Linux/Windows CLI。

## 并发测试（2026-10-08）

- 全部远端文件操作限定在测试目录 ID `404038274`（`/测试/ilanzouDriver/`）；没有对其他远端目录执行文件操作。并发测试使用 IP `39.176.153.187`，每批使用随机唯一文件名。
- 单请求上传/下载测试文件为 1 MiB，阶梯为 1、2、4、8、16：并发 1、2、4 的首轮上传和下载均通过。并发 8 首轮出现 2 项失败；复跑 8 时 8 个上传对象、下载内容校验均通过。并发 16 时 5 个 Upload 返回 `/unproved/7n/results: token不能为空！`；其中 4 个仍出现在目录并成功下载校验，1 个未出现在目录，因此该档未全通过。
- 分片上传/下载测试文件为 8 MiB+1 B，阶梯为 1、2、4、8、16：并发 1、2、4 均通过。并发 8 的多次运行有 1–2 个 Upload 返回相同的空 token 错误；其余可见对象下载长度和 MD5 均匹配。由于 8 并发不稳定，没有继续升到 16。
- 单独下载测试先串行上传了 64 个 1 MiB 文件，再对 1、2、4、8、16、32、64 并发档逐档下载并校验长度和 MD5；所有档位均通过。串行预置期间除首个样本外，其他 Upload 也返回空 token 错误，但 64 个对象均可在目标目录列出并完成下载校验。
- 并发测试显示下载至少通过 64 路同时下载。上传没有测出稳定的精确上限：1 MiB 单请求上传在 8 并发复跑通过一次、在 16 并发发生登记错误；8 MiB+1 B 分片上传在 4 并发通过、在 8 并发间歇发生登记错误。空 token 错误发生于 iLanZou `/unproved/7n/results` 登记步骤；有些对应文件已经可见并可下载，错误响应与对象实际落盘状态不一致。
- 64 文件下载测试的清理在删除前 50 个样本后，第 51 个请求返回 HTTP 409，响应体为 HTML `403`。只读核对确认目标目录还留有该批的 14 个对象；对这 14 个精确名称/ID 做清理重试（含等待 60 秒冷却）仍全部被服务端拒绝。已停止继续删除请求；未操作其他 ID。

仍未清理的 14 个并发下载样本：

| 文件 ID | 文件名 |
|---:|---|
| `41026681451` | `ilz-dlconc-7837757076b95d78-50.bin` |
| `41026681462` | `ilz-dlconc-7837757076b95d78-51.bin` |
| `41026681465` | `ilz-dlconc-7837757076b95d78-52.bin` |
| `41026681470` | `ilz-dlconc-7837757076b95d78-53.bin` |
| `41026681474` | `ilz-dlconc-7837757076b95d78-54.bin` |
| `41026681479` | `ilz-dlconc-7837757076b95d78-55.bin` |
| `41026681483` | `ilz-dlconc-7837757076b95d78-56.bin` |
| `41026681486` | `ilz-dlconc-7837757076b95d78-57.bin` |
| `41026681487` | `ilz-dlconc-7837757076b95d78-58.bin` |
| `41026681490` | `ilz-dlconc-7837757076b95d78-59.bin` |
| `41026681494` | `ilz-dlconc-7837757076b95d78-60.bin` |
| `41026681500` | `ilz-dlconc-7837757076b95d78-61.bin` |
| `41026682844` | `ilz-dlconc-7837757076b95d78-62.bin` |
| `41026684642` | `ilz-dlconc-7837757076b95d78-63.bin` |

- 本轮没有修改驱动源代码，也没有重编 CLI；临时诊断改动已恢复。工作区交付变化仅为本并发结果日志。

## TOML 配置、根目录隔离与多文件并发

- CLI 新增 `ilanzou.toml` 配置，默认从可执行文件同级目录读取；`-c`/`--config` 可指定其他 TOML 文件。缺省配置文件不存在时可继续从环境变量或命令行获取配置，显式指定但不存在的文件会报错。
- 生效优先级为命令行参数、环境变量、TOML、内置默认值。支持账号、密码、IP、根目录 ID、上传并发数、下载并发数和 HTTP 请求超时；配置样例位于 `ilanzou.toml.example`，真实 `ilanzou.toml` 已加入 Git 忽略规则。
- 默认根目录为 `0`，上传/下载并发上限均为 4，请求超时为 10 分钟。非 0 根目录启用后，CLI 在执行列表、新建、上传、下载、移动、重命名和删除前，仅从该根目录向下查找和校验目标；根目录本身不可移动、重命名或删除。
- `upload` 支持多个本地文件并按上传并发上限执行；`download` 支持多个文件 ID/路径对并按下载并发上限执行。文件级并发通过 Go worker 并行处理。上传对象 key 的时间片段改为进程内原子递增，避免同一进程中的并发上传生成相同 key。
- TOML 使用 `BurntSushi/toml` v1.6.0 解析。已重新构建 `dist/ilanzou-linux-amd64` 和 `dist/ilanzou-windows-amd64.exe`；Linux CLI 的完整 `-h` 和 `upload -h` 输出已核对。未运行网盘远端操作或功能测试。

## 默认下载并发调整（2026-10-08）

- 按用户要求，将未设置 `download_concurrency` 时的默认值从 4 调整为 32；上传并发默认值仍为 4，账号根目录和请求超时默认值保持不变。
- 同步更新 `cmd/ilanzou/config.go` 的运行时默认值、CLI `-h` 帮助、README 参数表和 `ilanzou.toml.example`。
- 重新构建 Linux 与 Windows CLI；核对 Linux `-h` 中显示下载默认并发为 32。未执行网盘远端操作或功能测试。

## 静态和动态库构建（2026-10-08）

- 增加公开 Go `ScopedClient`，复用根目录范围校验，并提供列目录、创建目录、上传、下载、移动、重命名和删除方法。CLI 已改用该类型执行文件操作。
- 增加 `cmd/ilanzouffi` C ABI 入口，提供会话创建/初始化/关闭、JSON 列表和创建/上传、下载到本地路径、移动/重命名/删除及 C 字符串释放接口。C ABI 会话通过 `ScopedClient` 保持根目录隔离。
- Linux x86-64 构建产物：`dist/libilanzou-linux-amd64.a`（C 静态库）、`dist/libilanzou-linux-amd64.so`（C 动态库）、`dist/libilanzou-linux-amd64.h`（cgo 生成头文件）。使用当前 Linux GCC 构建。
- 现有 Linux 与 Windows CLI 均已重编，以使用提取到 Go 公共库的根目录范围实现。
- 当前环境没有 Windows amd64 cgo 交叉编译器（MinGW），因此未构建 Windows DLL/静态库；原 Linux/Windows CLI 文件仍是独立交付物。
- README 增加 Go/C 接口、库文件用法和构建说明。未运行网盘远端操作或功能测试。

## Windows C 库核对与补充（2026-10-08）

- 用户提供 MinGW 交叉编译器及 Windows DLL/头文件后，核对 `dist/libilanzou-windows-amd64.dll` 为 PE32+ x86-64；Go build info 显示 `GOOS=windows`、`GOARCH=amd64`、`-buildmode=c-shared`、Git revision `5979c2924cf795fbfc2734fd58d6b77dafd022d1`，且构建时工作树干净。
- DLL 导出 11 个公开 `Ilanzou*` C ABI 函数；外部 DLL 依赖为 `KERNEL32.dll` 和 `msvcrt.dll`。头文件与使用当前源码生成的 Windows 头文件 SHA-256 相同：`189b3bdb37c026f3f58ce0875a94f6ab88df6b040cd48a44d6a5fdda9bd7e3bf`。
- 当前工作树复构建 DLL 的哈希与用户 DLL 不同，因为复构建工作树含未跟踪产物并带有 `vcs.modified=true`；复构建头文件完全一致，导出接口一致。
- 用 MinGW 成功构建 Windows C 静态库 `dist/libilanzou-windows-amd64.a`，并从现有 DLL 的公开导出生成 MinGW 导入库 `dist/libilanzou-windows-amd64.dll.a`；`dist/libilanzou-windows-amd64.def` 保存对应导出名。
- 使用最小 C 客户端分别通过静态库和 DLL 导入库成功链接为 Windows amd64 EXE；未执行这些 EXE，也未执行网盘操作。
- README 已补充 Windows DLL、静态库、导入库与构建命令。未执行网盘远端操作或功能测试。

## `--no-config` 运行模式（2026-10-08）

- 新增全局参数 `--no-config`，可放在命令前后；启用后不计算默认 TOML 路径、不打开 `-c/--config` 指定路径，也不解析任何 TOML 内容。
- 此模式仅应用命令行参数、环境变量和内置默认值；账号、密码必须由命令行或环境变量提供。其他配置仍采用内置默认值（根目录 `0`、上传并发 4、下载并发 32、请求超时 10 分钟）。
- 参数不足时给出账号/密码来源提示；并发数、请求超时或根目录 ID 无效时提示对应参数名和环境变量名。若同时指定 `--no-config` 和 `-c/--config`，以 `--no-config` 为准并跳过配置文件。
- 已更新完整 `-h`、README 和本工作日志，并重编 Linux/Windows CLI。使用损坏的 README TOML 路径配合 `--no-config`、缺失账号密码时，程序跳过 TOML 并显示必填提示；并发数设为 0、根目录 ID 非数字时均显示对应参数/环境变量提示。未执行网盘操作。

## 上传空 token 排查与修复分支（2026-10-08）

- 分支：`fix/ilanzou-upload-token`。本节修改和构建均在该分支。
- 错误原路径：上传 API 从 `/proved/7n/getUpToken` 取 `upToken`，Qiniu 单次/分片完成响应中取 `token`，然后以 `tokenList` 调用 `/unproved/7n/results`。原代码没有校验 Qiniu token 是否为空；`qiniuDo` 也没有检查非 2xx 状态，因此 Qiniu JSON 错误响应可能被当成正常响应继续传递，最终只显示 iLanZou 的“token不能为空”。
- 对照 OpenList 当前 iLanZou 驱动，增加 `upToken="-1"` 秒传分支，直接返回其 `map.fileId/fileName`，跳过 Qiniu 和 `/results`；文件大小改为向上取整 KiB（至少 1）；Qiniu key 改为 `disk/YYYY/MM/DD/account/<timestamp>.rar`，对齐 OpenList 当前控制台格式。
- Qiniu 单次、分片初始化、分片上传和完成阶段现在检查 HTTP 状态以及必需的 `uploadId`、`etag`、`token` 字段。Qiniu 非 2xx 错误（579 除外）会在原请求阶段返回状态码和 JSON `error`，Qiniu 响应无 token 时停止，不再以空 token 请求 iLanZou results。HTTP 579 按 Qiniu“上传成功但回调失败”响应保留正文，后续仍要求结果 token。
- 既有 64 文件下载预置脚本用相同内容写入所有不同名称的文件；该模式会重复 MD5。结合 OpenList 的 `upToken="-1"` 处理，此前除首个样本外的空 token 错误很可能由未处理秒传响应触发。不同内容的并发上传也曾出现空 token，因此 Qiniu 状态/响应字段诊断仍有价值。
- OpenList 当前同样会从普通 Qiniu 响应读取 `token` 并调用 `/results`，没有通用的空 token 校验；本分支增加了该校验及 Qiniu 状态错误透传。公开同类报告包括 [Alist #5928](https://github.com/AlistGo/alist/issues/5928)、[Alist #8104](https://github.com/AlistGo/alist/issues/8104) 和 [Alist 文档讨论 #380](https://github.com/AlistGo/docs/discussions/380)。
- 重新构建 Linux/Windows CLI、Linux `.so/.a`、Windows `.dll/.a`；未运行网盘上传或远端功能测试。源码依据：[Alist iLanZou](https://github.com/AlistGo/alist/blob/main/drivers/ilanzou/driver.go)、[OpenList iLanZou](https://github.com/OpenListTeam/OpenList/blob/main/drivers/ilanzou/driver.go)、[七牛直传响应](https://developer.qiniu.com/kodo/1312/upload)、[七牛分片完成响应](https://developer.qiniu.com/kodo/6368/complete-multipart-upload)。

## 修复分支实际文件操作验证（2026-10-08）

- 使用 `dist/ilanzou.toml` 提供账号、密码和 IP；该文件的 `root_folder_id` 是 `347497588`，本次 CLI 明确用 `--root-folder-id 404038274` 覆盖，将操作限制在 `/测试/ilanzouDriver/`。
- 用修复分支构建的 Linux CLI 完成四组上传、下载和内容校验：1 B、1 MiB、内容相同但文件名不同的第二个 1 MiB 文件、8 MiB+1 B。四组上传成功，下载长度及 SHA-256 均匹配。
- 本轮生成并已删除的测试对象：

| 文件 ID | 文件名 | 大小 |
|---:|---|---:|
| `41048377977` | `ilz-fix-4589c4a476b86c47-1byte.bin` | 1 B |
| `41048378033` | `ilz-fix-4589c4a476b86c47-single-1mib.bin` | 1 MiB |
| `41048378058` | `ilz-fix-4589c4a476b86c47-duplicate-1mib.bin` | 1 MiB |
| `41048378092` | `ilz-fix-4589c4a476b86c47-multipart-8mib-plus-1.bin` | 8 MiB+1 B |

- 四个对象的精确 ID 删除均成功；按本轮唯一前缀复查，剩余条目数为 0。其他远端目录和旧测试前缀均未操作。
- 重复内容第二个样本返回了新 ID；CLI 未记录 `/getUpToken` 的原始响应，因此无法确认该样本是否实际命中 `upToken="-1"` 分支。以上通过不代表高并发或 Qiniu 非成功响应路径均已验证。

## 修复分支并发上传/下载复测（2026-10-08）

- 使用 `dist/ilanzou.toml`，通过 CLI 覆盖 `root_folder_id=404038274`，在 `/测试/ilanzouDriver/` 执行；每批先检查随机唯一前缀未出现，再测试，未列出或操作其他路径。
- 1 MiB 单请求上传路径按并发 1、2、4、8 逐档执行；每档上传 N 个随机内容文件后，以 `--download-concurrency N` 并发下载同一批文件。四档上传均成功，所有下载字节数和 SHA-256 匹配。
- 8 MiB+1 B 分片路径按并发 1、2、4、8 执行相同上传/下载校验，四档均成功，内容 SHA-256 匹配。
- 每档的测试文件都通过精确生成前缀列出并按确切 ID 删除，随后复查本批前缀剩余 0 个对象。共清理本轮全部 30 个测试对象；旧测试前缀和其他目录未触碰。
- 本轮没有观察到空 token 错误；并发实测上限仅确认到 8，未继续升高。未执行 1MiB 重复内容的并发秒传用例，也未运行网盘外功能测试。

## 修复合并状态（2026-10-08）

- 修复分支 `fix/ilanzou-upload-token` 已快进合并至 `master`；主分支当前 HEAD 为 `eddb74e Record concurrent upload and download checks`。
- 修复提交为 `dbde940 Fix iLanZou upload token handling`，端到端验证记录为 `417ff72`，并发复测记录为 `eddb74e`。
- 合并前工作区干净；本轮并发样本已清理，测试操作仍限定在目录 ID `404038274`。旧测试前缀没有在本轮触碰或重新核查。

## WebDAV 服务（2026-10-08）

- 在现有 CLI 上增加 `start`、`stop` 和前台 `serve` 子命令；`start` 以独立进程运行，等待登录和监听成功后返回。日志和 PID 文件放在配置文件同目录。
- 新增 `ilanzou-webdav.toml` 配置读取，命令行参数优先于环境变量和 TOML。默认 WebDAV 监听 `127.0.0.1:8080`。
- 新增 WebDAV 路径适配，按目录名逐段列目录并映射到底层对象 ID；使用 `ScopedClient` 限定配置根目录。支持 PROPFIND、GET、HEAD、PUT、MKCOL、DELETE、MOVE 和 COPY。
- 已运行 `go test ./...`、Windows amd64 交叉编译、Linux/Windows CLI 构建及命令帮助/缺省账号提示检查。未进行 iLanZou 远端写入验证。

## WebDAV 配置实测（2026-10-08）

- 使用 `dist/ilanzou-webdav.toml`，根目录 ID 为 `347497588`；仅将监听地址覆盖为 `127.0.0.1:8080`，未改变配置中的账号或目录范围。
- 实测 MKCOL、PUT、PROPFIND、GET、MOVE 和 COPY；下载及复制后的文件内容均与上传内容一致。测试用随机目录及其中对象已清理，测试服务已停止。
- 小文件的 iLanZou 列目录元数据按 KiB 返回近似大小。服务不再把该值作为 HTTP `Content-Length` 或 DAV `getcontentlength` 返回，避免客户端因长度偏大而截断读取。
