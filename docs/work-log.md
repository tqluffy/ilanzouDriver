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
- 尺寸矩阵使用的大小为 1 B、1 KiB、1 MiB、8 MiB−1 B、8 MiB、8 MiB+1 B、32 MiB、100,000,000 B、200,000,000 B。
- 1 KiB 至 200,000,000 B 的上传 API 均返回成功；下载长度及 MD5 与本地生成文件一致。8 MiB 分片边界两侧及边界值均完成验证。
- 1 B 样本上传时 Qiniu 返回 HTTP 401 `BadToken`，但 iLanZou 列表中出现对应对象；随后下载得到 1 B，内容与生成字节一致。CLI 的上传调用仍报告错误，因此 1 B 上传接口结果记为异常。

## 测试对象现状

以下对象仍在 `/测试/ilanzouDriver/`（目录 ID `404038274`）中。它们均为本次生成的测试文件，下载内容已通过校验：

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

对上述 9 个对象逐一执行移动、重命名和删除测试。9 个对象都成功移入 `/测试/ilanzouDriver/` 下的临时目录、完成重命名、再移回并恢复原名；随后按原 ID/名称尝试删除，9 次均返回 HTTP 409。已复核 9 个对象仍以原 ID 和原名位于目标目录。临时操作目录 ID `404039154` 当前为空，但删除该目录也返回 HTTP 409，因此目录仍存在。

固定参数顺序和上游浏览器请求头下，删除接口仍返回 HTTP 409；正文曾观测到 HTML 403 页面。没有测试对象被成功删除。

此前还有一个 1 B 测试对象留在父目录 `/测试/`（目录 ID `347025631`）：ID `41026194253`，名称 `ilz-check-623788fb039e83831d7e27f9-1.bin`。该对象不在 `/测试/ilanzouDriver/` 中，未在本次尺寸矩阵中操作；对它的删除请求也被服务端以 HTTP 409/403 拒绝。

在用户提供 IP 并加入 XFF 支持后，另有 4 个 1 B 重测对象创建在 `/测试/ilanzouDriver/`，均可下载到匹配的单字节内容，但上传最终结果仍返回“token不能为空”错误：

| 对象 ID | 文件名 |
|---:|---|
| `41026503936` | `ilz-xff-onebyte-a51c0d453c15eb83.txt` |
| `41026504393` | `ilz-xff-onebyte-75f0037e8aa71431.txt` |
| `41026505093` | `ilz-xff-onebyte-cae5e8014eab9343.txt` |
| `41026519010` | `ilz-xff-onebyte-4ec7a598ad371542.txt` |

## 已发现问题与代码调整

- 初版将 iLanZou API 查询参数交给 `url.Values.Encode()` 排序。现已改为按上游顺序构造登录和普通 API 请求参数，并补齐上游请求头；下载重定向本身也使用固定顺序。
- 参数顺序修复后，文件列表和账号访问正常，但删除请求仍被服务端返回 HTTP 409/403。删除问题未解决。
- 1 B 上传返回 Qiniu `BadToken`，虽然对应对象的下载字节正确；该上传响应异常尚未解决。
- 原仓库历史显示小文件上传的 `fileSize/1024 + 1` 修复（`a2dc45a8`）已包含在当前独立版。
- 原仓库还包含 iLanZou IP 封锁修复（`f25be154`）：对 API 和下载重定向附加可配置的 `X-Forwarded-For`；以及 API `Accept-Encoding` 头修复（`6812ec9a`）。此前独立版缺少这两项，目前已加入可选 `ILANZOU_IP`/`Client.SetIP` 和对应请求头。
- API 请求现在声明与上游相同的 `Accept-Encoding`，并可解码 gzip/deflate；加入后，对目标目录 ID `404038274` 的只读列目录成功，返回 10 个条目。
- 已使用用户提供的 IP 进行 XFF 重测；读取目标目录成功，但删除请求仍返回 HTTP 409。1 B 上传仍由结果接口返回“token不能为空”，尽管生成文件存在且其字节可正常下载。IP 转发已实测，未解决这两项现象。
- 原仓库 Resty 通过 `createMultipartHeader` 按 `name` 后 `filename` 顺序构造文件 part，并对非 2xx Qiniu 响应继续尝试 `/7n/results`。独立版现已对齐该 multipart 头格式，并以结果接口作为提交结果判断；1 B 重测后仍返回 token 为空。
- 小文件 multipart 现在使用完整 `multipart.Writer` 生成请求体，并对齐原仓库的 `Content-Disposition` 参数顺序；Qiniu 非 2xx 响应不再提前中断，而是继续走 `/7n/results`。使用 XFF 的 1 B 样本仍在 `/7n/results` 返回“token不能为空”，但下载字节与源数据相同。
- 早期验证脚本在成功清理路径中清空测试目录 ID 后，延迟清理仍用空 ID 查询并删除返回对象。用户报告发生误删；该脚本逻辑是误删风险来源。用户表示自行恢复，未执行恢复操作。

## Git 状态

- 查询参数顺序修复提交：`9d4d9fe Preserve iLanZou API query parameter order`
- XFF 配置支持提交：`c61f35d Add upstream iLanZou IP forwarding support`
- Resty 小文件上传行为对齐提交：`262cf13 Match upstream iLanZou upload request behavior`
- 文件操作尺寸矩阵日志提交：`f8dcce8 Record file operation test results`
- 首次工作日志提交：`728e1a8 Document iLanZou implementation status`
- 提交身份：`tqluffy <tqluffy@qq.com>`
- 最近的 Resty 对齐提交包含重新构建的 Linux/Windows CLI。
