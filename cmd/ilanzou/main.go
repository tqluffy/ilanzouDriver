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
	"sync"

	"ilanzou"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	options, positional, err := parseInvocation(args)
	if err != nil {
		return err
	}
	if len(positional) == 0 {
		usage()
		return nil
	}
	command, commandArgs := positional[0], positional[1:]
	if options.help {
		if command == "help" && len(commandArgs) > 0 {
			return commandUsage(commandArgs[0])
		}
		if command != "help" {
			return commandUsage(command)
		}
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
	if command == "stop" {
		return stopWebDAV(configPathFor(options, "ilanzou-webdav.toml"))
	}
	defaultConfigName := "ilanzou.toml"
	if command == "start" || command == "serve" {
		defaultConfigName = "ilanzou-webdav.toml"
	}
	config, err := resolveSettings(options, defaultConfigName)
	if err != nil {
		return err
	}
	if command == "start" || command == "serve" {
		return runWebDAVCommand(command, options, config)
	}
	if config.username == "" || config.password == "" {
		if options.noConfig {
			return errors.New("已启用 --no-config，账号和密码必须通过 ILANZOU_USERNAME/ILANZOU_PASSWORD 环境变量或 --username/--password 参数提供")
		}
		return errors.New("请通过 ilanzou.toml、环境变量或 --username/--password 提供账号和密码")
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	client := ilanzou.NewClient(config.username, config.password).
		SetIP(config.ip).
		SetTimeout(config.requestTimeout)
	if err := client.Init(ctx); err != nil {
		return err
	}
	scope := ilanzou.NewScopedClient(client, config.rootFolderID)

	switch command {
	case "ls":
		folderID := config.rootFolderID
		if len(commandArgs) > 1 {
			return errors.New("usage: ilanzou ls [folder-id]")
		}
		if len(commandArgs) == 1 {
			folderID = commandArgs[0]
		}
		entries, err := scope.List(ctx, folderID)
		if err != nil {
			return err
		}
		return printJSON(entries)
	case "mkdir":
		parentID, name := config.rootFolderID, ""
		if len(commandArgs) == 1 {
			name = commandArgs[0]
		} else if len(commandArgs) == 2 {
			parentID, name = commandArgs[0], commandArgs[1]
		} else {
			return errors.New("usage: ilanzou mkdir [parent-folder-id] <name>")
		}
		entry, err := scope.MakeDir(ctx, parentID, name)
		if err != nil {
			return err
		}
		return printJSON(entry)
	case "upload":
		folderID := config.rootFolderID
		files := commandArgs
		if len(commandArgs) == 0 {
			return errors.New("usage: ilanzou upload [folder-id] <local-file> [local-file...]")
		}
		if len(commandArgs) > 1 && isNumericID(commandArgs[0]) {
			folderID, files = commandArgs[0], commandArgs[1:]
		}
		entries := make([]ilanzou.Entry, len(files))
		outcomes := make([]uploadOutcome, len(files))
		err := runConcurrent(config.uploadConcurrency, len(files), func(index int) error {
			entry, err := scope.Upload(ctx, folderID, files[index])
			if err != nil {
				outcomes[index] = uploadOutcome{LocalFile: files[index], Error: err.Error()}
				return fmt.Errorf("upload %q: %w", files[index], err)
			}
			entries[index] = entry
			outcomes[index] = uploadOutcome{LocalFile: files[index], Entry: &entries[index]}
			return nil
		})
		if len(files) == 1 {
			if err != nil {
				return err
			}
			return printJSON(entries[0])
		}
		if printErr := printJSON(outcomes); printErr != nil {
			return printErr
		}
		return err
	case "download":
		if len(commandArgs) < 2 || len(commandArgs)%2 != 0 {
			return errors.New("usage: ilanzou download <file-id> <local-file> [<file-id> <local-file>...]")
		}
		tasks := make([]downloadTask, len(commandArgs)/2)
		outcomes := make([]downloadOutcome, len(tasks))
		for i := range tasks {
			tasks[i] = downloadTask{fileID: commandArgs[2*i], path: commandArgs[2*i+1]}
		}
		if err := distinctDownloadPaths(tasks); err != nil {
			return err
		}
		err := runConcurrent(config.downloadConcurrency, len(tasks), func(index int) error {
			if err := downloadTo(ctx, scope, tasks[index].fileID, tasks[index].path); err != nil {
				outcomes[index] = downloadOutcome{FileID: tasks[index].fileID, LocalFile: tasks[index].path, Error: err.Error()}
				return fmt.Errorf("download %q to %q: %w", tasks[index].fileID, tasks[index].path, err)
			}
			outcomes[index] = downloadOutcome{FileID: tasks[index].fileID, LocalFile: filepath.Clean(tasks[index].path)}
			return nil
		})
		if len(tasks) == 1 {
			if err != nil {
				return err
			}
			fmt.Println(outcomes[0].LocalFile)
			return nil
		}
		if printErr := printJSON(outcomes); printErr != nil {
			return printErr
		}
		return err
	case "move":
		if len(commandArgs) != 3 {
			return errors.New("usage: ilanzou move <file|dir> <id> <target-folder-id>")
		}
		isDir, err := entryKind(commandArgs[0])
		if err != nil {
			return err
		}
		if err := scope.Move(ctx, commandArgs[1], isDir, commandArgs[2]); err != nil {
			return err
		}
		fmt.Println("moved")
		return nil
	case "rename":
		if len(commandArgs) != 3 {
			return errors.New("usage: ilanzou rename <file|dir> <id> <new-name>")
		}
		isDir, err := entryKind(commandArgs[0])
		if err != nil {
			return err
		}
		if err := scope.Rename(ctx, commandArgs[1], isDir, commandArgs[2]); err != nil {
			return err
		}
		fmt.Println("renamed")
		return nil
	case "delete":
		if len(commandArgs) != 2 {
			return errors.New("usage: ilanzou delete <file|dir> <id>")
		}
		isDir, err := entryKind(commandArgs[0])
		if err != nil {
			return err
		}
		if err := scope.Remove(ctx, commandArgs[1], isDir); err != nil {
			return err
		}
		fmt.Println("deleted")
		return nil
	default:
		return fmt.Errorf("unknown command %q", command)
	}
}

type uploadOutcome struct {
	LocalFile string         `json:"local_file"`
	Entry     *ilanzou.Entry `json:"entry,omitempty"`
	Error     string         `json:"error,omitempty"`
}

type downloadTask struct {
	fileID string
	path   string
}

type downloadOutcome struct {
	FileID    string `json:"file_id"`
	LocalFile string `json:"local_file"`
	Error     string `json:"error,omitempty"`
}

func runConcurrent(limit, count int, work func(int) error) error {
	if count == 0 {
		return nil
	}
	workers := limit
	if workers > count {
		workers = count
	}
	jobs := make(chan int)
	errorsByIndex := make([]error, count)
	var group sync.WaitGroup
	for worker := 0; worker < workers; worker++ {
		group.Add(1)
		go func() {
			defer group.Done()
			for index := range jobs {
				errorsByIndex[index] = work(index)
			}
		}()
	}
	for index := 0; index < count; index++ {
		jobs <- index
	}
	close(jobs)
	group.Wait()
	return errors.Join(errorsByIndex...)
}

func downloadTo(ctx context.Context, client *ilanzou.ScopedClient, fileID, path string) error {
	reader, err := client.Download(ctx, fileID)
	if err != nil {
		return err
	}
	defer reader.Close()
	file, err := os.Create(path)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func distinctDownloadPaths(tasks []downloadTask) error {
	seen := make(map[string]bool, len(tasks))
	for _, task := range tasks {
		absolute, err := filepath.Abs(filepath.Clean(task.path))
		if err != nil {
			return err
		}
		if seen[absolute] {
			return fmt.Errorf("download output path is repeated: %q", task.path)
		}
		seen[absolute] = true
	}
	return nil
}

func isNumericID(value string) bool {
	if value == "" {
		return false
	}
	for _, character := range value {
		if character < '0' || character > '9' {
			return false
		}
	}
	return true
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

	配置优先级：命令行参数 > 环境变量 > ilanzou.toml > 默认值
	ilanzou.toml 默认在可执行文件同级目录；也可用 -c/--config 指定。
	WebDAV 的 start/serve 使用同目录的 ilanzou-webdav.toml。
  缺省配置文件可以不存在。示例配置见 ilanzou.toml.example。
  --no-config 强制跳过所有 TOML 文件；仅使用命令行、环境变量和内置默认值。
  --no-config 时账号和密码必填，缺失或参数无效会显示对应提示。

配置项、环境变量和命令行参数：
  username             ILANZOU_USERNAME             --username
  password             ILANZOU_PASSWORD             --password
  ip                   ILANZOU_IP                   --ip
  root_folder_id       ILANZOU_ROOT_FOLDER_ID       --root-folder-id
  upload_concurrency   ILANZOU_UPLOAD_CONCURRENCY   --upload-concurrency
  download_concurrency ILANZOU_DOWNLOAD_CONCURRENCY --download-concurrency
  request_timeout      ILANZOU_REQUEST_TIMEOUT      --request-timeout
  listen               ILANZOU_WEBDAV_LISTEN        --listen (WebDAV)
  密码作为命令行参数会出现在 shell 历史和进程参数中，建议使用 TOML 或环境变量。

默认值：root_folder_id="0"，上传并发数为 4、下载并发数为 32，request_timeout="10m"。
设置非 0 根目录后，所有网盘文件操作只允许访问该目录及其子目录；该根目录本身不能移动、重命名或删除。

命令：
  start                                  启动 WebDAV 服务并在后台运行
  stop                                   停止后台 WebDAV 服务
  ls [目录ID]                           列出目录，省略时列出配置根目录
  mkdir [父目录ID] <目录名>               新建目录，省略父目录时使用配置根目录
  upload [目录ID] <本地文件> [本地文件...] 上传一个或多个文件
  download <文件ID> <本地文件> [...]       按 ID/路径对下载一个或多个文件
  move <file|dir> <对象ID> <目标目录ID>    移动文件或目录
  rename <file|dir> <对象ID> <新名称>      重命名文件或目录
  delete <file|dir> <对象ID>              永久删除文件或目录
  help [命令]                            显示完整或指定命令帮助

参数说明：
  目录ID和文件ID由 ls、mkdir、upload 的 JSON 输出提供。
  文件 ID 与本地路径按对重复传入。对象类型使用 file 表示文件，dir 表示目录。
  download 会覆盖同名本地文件；delete 会删除网盘中的对象。

示例（Linux/macOS）：
  cp ilanzou.toml.example ilanzou.toml
  # 编辑同级 ilanzou.toml 填入账号、密码及根目录设置
  ./ilanzou ls
  ./ilanzou -c ./ilanzou.toml ls
  ILANZOU_USERNAME='你的账号' ILANZOU_PASSWORD='你的密码' ./ilanzou --no-config ls
  ./ilanzou --no-config --username '你的账号' --password '你的密码' ls
  ./ilanzou mkdir backup
  ./ilanzou upload 348006267 ./报告.pdf
  ./ilanzou --upload-concurrency 4 upload ./a.bin ./b.bin
  ./ilanzou download 文件ID ./报告.pdf
  ./ilanzou --download-concurrency=8 download 文件ID1 ./a.bin 文件ID2 ./b.bin
  ./ilanzou move file 文件ID 目标目录ID
  ./ilanzou rename file 文件ID 新名称.pdf
  ./ilanzou delete file 文件ID

示例（Windows PowerShell）：
  Copy-Item ilanzou.toml.example ilanzou.toml
  .\ilanzou.exe ls
  .\ilanzou.exe -c .\ilanzou.toml ls
  .\ilanzou.exe --upload-concurrency 4 upload .\a.bin .\b.bin
  .\ilanzou.exe download 文件ID .\报告.pdf

选项：
  -c, --config PATH       配置文件路径
      --no-config         禁止读取 ilanzou.toml 和其他 TOML 配置文件
      --username VALUE    iLanZou 账号
      --password VALUE    iLanZou 密码
      --ip VALUE          作为 X-Forwarded-For 的可选客户端 IP
      --root-folder-id ID 限定可操作的根目录
      --upload-concurrency N   最大并发上传数
      --download-concurrency N 最大并发下载数
      --request-timeout DURATION 每个 HTTP 请求超时，如 30s、10m
      --listen VALUE      WebDAV 监听地址，如 127.0.0.1:8080
  -h, --help              显示帮助；不需要登录
`)
}

func commandUsage(command string) error {
	var help string
	switch command {
	case "ls":
		help = `用法：ilanzou ls [目录ID]

列出指定目录的直接子项并输出 JSON。省略目录ID时列出配置根目录。
指定目录必须是配置根目录或其子目录。
每个对象包含 id、name、size（字节）、modified 和 is_dir。

示例：
  ilanzou ls
  ilanzou ls 348006267
`
	case "mkdir":
		help = `用法：ilanzou mkdir [父目录ID] <目录名>

在指定目录下新建目录。省略父目录ID时使用配置根目录。
父目录必须在配置根目录及其子目录中。成功后输出包含新目录ID的 JSON。

示例：
  ilanzou mkdir backup
  ilanzou mkdir 348006267 backup
`
	case "upload":
		help = `用法：ilanzou upload [目录ID] <本地文件> [本地文件...]

将一个或多个本地文件上传到目录。省略目录ID时使用配置根目录。
多个文件按 --upload-concurrency 设置的并发数上传；显式目录ID必须在配置根目录内。
小文件使用七牛表单上传，较大文件使用分片上传。
成功后输出包含网盘文件ID的 JSON。

示例：
  ilanzou upload ./报告.pdf
  ilanzou --upload-concurrency 4 upload ./a.bin ./b.bin
  ilanzou upload 348006267 ./报告.pdf
`
	case "download":
		help = `用法：ilanzou download <文件ID> <本地文件> [<文件ID> <本地文件>...]

下载一个或多个网盘文件。文件ID必须在配置根目录及其子目录内。
多个文件按 --download-concurrency 设置的并发数下载。若目标文件已存在，会被覆盖。

示例：
  ilanzou download 123456789 ./报告.pdf
  ilanzou --download-concurrency 8 download 123456789 ./a.bin 123456790 ./b.bin
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
	case "start":
		help = `用法：ilanzou-webdav start [选项]

读取 ilanzou-webdav.toml 并在后台启动 WebDAV 服务。
服务地址由 listen 配置项设置，默认 127.0.0.1:8080。

示例：
  ./ilanzou-webdav start
  ./ilanzou-webdav --config ./webdav.toml --listen 0.0.0.0:8080 start
  ./ilanzou-webdav stop
`
	case "stop":
		help = `用法：ilanzou-webdav stop [-c 配置文件]

停止 start 启动的后台 WebDAV 服务。
`
	default:
		return fmt.Errorf("未知命令 %q；运行 ilanzou -h 查看命令列表", command)
	}
	fmt.Print(help)
	return nil
}
