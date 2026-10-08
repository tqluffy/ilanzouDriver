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
