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

删除接口对已核对的 1 KiB 测试对象返回 HTTP 409；响应正文为 HTML 403 页面。按固定参数顺序和上游浏览器请求头重试后仍相同。其余尺寸的删除未逐个执行；矩阵中的对象目前仍保留。

此前还有一个 1 B 测试对象留在父目录 `/测试/`（目录 ID `347025631`）：ID `41026194253`，名称 `ilz-check-623788fb039e83831d7e27f9-1.bin`。该对象不在 `/测试/ilanzouDriver/` 中，未在本次尺寸矩阵中操作；对它的删除请求也被服务端以 HTTP 409/403 拒绝。

## 已发现问题与代码调整

- 初版将 iLanZou API 查询参数交给 `url.Values.Encode()` 排序。现已改为按上游顺序构造登录和普通 API 请求参数，并补齐上游请求头；下载重定向本身也使用固定顺序。
- 参数顺序修复后，文件列表和账号访问正常，但删除请求仍被服务端返回 HTTP 409/403。删除问题未解决。
- 1 B 上传返回 Qiniu `BadToken`，虽然对应对象的下载字节正确；该上传响应异常尚未解决。
- 早期验证脚本在成功清理路径中清空测试目录 ID 后，延迟清理仍用空 ID 查询并删除返回对象。用户报告发生误删；该脚本逻辑是误删风险来源。用户表示自行恢复，未执行恢复操作。

## Git 状态

- 最近提交：`9d4d9fe Preserve iLanZou API query parameter order`
- 提交身份：`tqluffy <tqluffy@qq.com>`
- 该提交包含 API 参数顺序修复、README 更新及重新构建的 Linux/Windows CLI。
- 日志写入前工作区干净。
