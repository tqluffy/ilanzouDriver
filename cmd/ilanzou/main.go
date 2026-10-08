package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"ilanzou"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return nil
	}
	command := args[0]
	commandArgs := args[1:]
	if isHelp(command) {
		usage()
		return nil
	}
	if command == "help" {
		if len(commandArgs) == 0 {
			usage()
			return nil
		}
		return commandUsage(commandArgs[0])
	}
	for _, arg := range commandArgs {
		if isHelp(arg) {
			return commandUsage(command)
		}
	}
	username, password := os.Getenv("ILANZOU_USERNAME"), os.Getenv("ILANZOU_PASSWORD")
	if username == "" || password == "" {
		return errors.New("请先设置环境变量 ILANZOU_USERNAME 和 ILANZOU_PASSWORD")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	client := ilanzou.NewClient(username, password).SetIP(os.Getenv("ILANZOU_IP"))
	if err := client.Init(ctx); err != nil {
		return err
	}

	args = commandArgs
	switch command {
	case "ls":
		folderID := "0"
		if len(args) > 1 {
			return errors.New("usage: ilanzou ls [folder-id]")
		}
		if len(args) == 1 {
			folderID = args[0]
		}
		entries, err := client.List(ctx, folderID)
		if err != nil {
			return err
		}
		return printJSON(entries)
	case "mkdir":
		if len(args) != 2 {
			return errors.New("usage: ilanzou mkdir <parent-folder-id> <name>")
		}
		entry, err := client.MakeDir(ctx, args[0], args[1])
		if err != nil {
			return err
		}
		return printJSON(entry)
	case "upload":
		if len(args) != 2 {
			return errors.New("usage: ilanzou upload <folder-id> <local-file>")
		}
		entry, err := client.Upload(ctx, args[0], args[1])
		if err != nil {
			return err
		}
		return printJSON(entry)
	case "download":
		if len(args) != 2 {
			return errors.New("usage: ilanzou download <file-id> <local-file>")
		}
		reader, err := client.Download(ctx, args[0])
		if err != nil {
			return err
		}
		defer reader.Close()
		file, err := os.Create(args[1])
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, reader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		fmt.Println(filepath.Clean(args[1]))
		return nil
	case "move":
		if len(args) != 3 {
			return errors.New("usage: ilanzou move <file|dir> <id> <target-folder-id>")
		}
		isDir, err := entryKind(args[0])
		if err != nil {
			return err
		}
		if err := client.Move(ctx, args[1], isDir, args[2]); err != nil {
			return err
		}
		fmt.Println("moved")
		return nil
	case "rename":
		if len(args) != 3 {
			return errors.New("usage: ilanzou rename <file|dir> <id> <new-name>")
		}
		isDir, err := entryKind(args[0])
		if err != nil {
			return err
		}
		if err := client.Rename(ctx, args[1], isDir, args[2]); err != nil {
			return err
		}
		fmt.Println("renamed")
		return nil
	case "delete":
		if len(args) != 2 {
			return errors.New("usage: ilanzou delete <file|dir> <id>")
		}
		isDir, err := entryKind(args[0])
		if err != nil {
			return err
		}
		if err := client.Remove(ctx, args[1], isDir); err != nil {
			return err
		}
		fmt.Println("deleted")
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

func entryKind(value string) (bool, error) {
	switch strings.ToLower(value) {
	case "dir", "folder":
		return true, nil
	case "file":
		return false, nil
	default:
		return false, fmt.Errorf("entry type must be file or dir, got %q", value)
	}
}

func printJSON(value interface{}) error {
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	return encoder.Encode(value)
}

func usage() {
	fmt.Print(`iLanZou 独立文件操作 CLI

通过 iLanZou API 管理目录和文件，通过七牛云传输文件内容。
运行时不需要 Alist 或其他外部组件。

用法：
  ilanzou <命令> [参数...]
  ilanzou <命令> -h
  ilanzou help [命令]

凭据：
  从环境变量读取，不写入配置文件或命令行参数：
  ILANZOU_USERNAME     iLanZou 账号
  ILANZOU_PASSWORD     iLanZou 密码
  ILANZOU_IP           可选；作为 X-Forwarded-For 发送，与上游驱动的 Ip 设置对应

命令：
  ls [目录ID]                           列出目录，省略时列出根目录
  mkdir <父目录ID> <目录名>              新建目录
  upload <目录ID> <本地文件>              上传文件
  download <文件ID> <本地文件>            下载文件
  move <file|dir> <对象ID> <目标目录ID>    移动文件或目录
  rename <file|dir> <对象ID> <新名称>      重命名文件或目录
  delete <file|dir> <对象ID>              永久删除文件或目录
  help [命令]                            显示完整或指定命令帮助

参数说明：
  目录ID和文件ID由 ls、mkdir、upload 的 JSON 输出提供。
  根目录ID为 0。对象类型使用 file 表示文件，dir 表示目录。
  download 会覆盖同名本地文件；delete 会删除网盘中的对象。

示例（Linux/macOS）：
  export ILANZOU_USERNAME='你的账号'
  export ILANZOU_PASSWORD='你的密码'
  # 可选：设置在原 iLanZou 驱动 Ip 字段中使用的客户端 IP
  export ILANZOU_IP='你的客户端公网 IP'
  ./ilanzou ls
  ./ilanzou mkdir 0 backup
  ./ilanzou upload 目录ID ./报告.pdf
  ./ilanzou download 文件ID ./报告.pdf
  ./ilanzou move file 文件ID 目标目录ID
  ./ilanzou rename file 文件ID 新名称.pdf
  ./ilanzou delete file 文件ID

示例（Windows PowerShell）：
  $env:ILANZOU_USERNAME = '你的账号'
  $env:ILANZOU_PASSWORD = '你的密码'
  # 可选
  $env:ILANZOU_IP = '你的客户端公网 IP'
  .\ilanzou.exe ls
  .\ilanzou.exe upload 目录ID .\报告.pdf
  .\ilanzou.exe download 文件ID .\报告.pdf

选项：
  -h, --help     显示帮助；不需要登录
`)
}

func isHelp(value string) bool {
	return value == "-h" || value == "--help"
}

func commandUsage(command string) error {
	var help string
	switch command {
	case "ls":
		help = `用法：ilanzou ls [目录ID]

列出指定目录的直接子项并输出 JSON。省略目录ID时列出根目录（ID 0）。
每个对象包含 id、name、size（字节）、modified 和 is_dir。

示例：
  ilanzou ls
  ilanzou ls 348006267
`
	case "mkdir":
		help = `用法：ilanzou mkdir <父目录ID> <目录名>

在指定目录下新建目录，成功后输出包含新目录ID的 JSON。

示例：
  ilanzou mkdir 0 backup
`
	case "upload":
		help = `用法：ilanzou upload <目录ID> <本地文件>

将本地文件上传到指定 iLanZou 目录。小文件使用七牛表单上传，较大文件使用分片上传。
成功后输出包含网盘文件ID的 JSON。

示例：
  ilanzou upload 0 ./报告.pdf
`
	case "download":
		help = `用法：ilanzou download <文件ID> <本地文件>

下载指定网盘文件到本地路径。若目标文件已存在，会被覆盖。

示例：
  ilanzou download 123456789 ./报告.pdf
`
	case "move":
		help = `用法：ilanzou move <file|dir> <对象ID> <目标目录ID>

移动文件或目录到目标目录。对象类型必须指定为 file 或 dir。

示例：
  ilanzou move file 123456789 348006267
  ilanzou move dir 348006267 0
`
	case "rename":
		help = `用法：ilanzou rename <file|dir> <对象ID> <新名称>

重命名文件或目录。对象类型必须指定为 file 或 dir。

示例：
  ilanzou rename file 123456789 报告-归档.pdf
  ilanzou rename dir 348006267 backup-old
`
	case "delete":
		help = `用法：ilanzou delete <file|dir> <对象ID>

永久删除指定文件或目录。对象类型必须指定为 file 或 dir。

示例：
  ilanzou delete file 123456789
  ilanzou delete dir 348006267
`
	default:
		return fmt.Errorf("未知命令 %q；运行 ilanzou -h 查看命令列表", command)
	}
	fmt.Print(help)
	return nil
}
