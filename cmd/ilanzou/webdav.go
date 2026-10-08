package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"ilanzou"
	"ilanzou/webdav"
)

func runWebDAVCommand(command string, options globalOptions, config settings) error {
	switch command {
	case "start":
		return startWebDAV(options, config)
	case "serve":
		return serveWebDAV(config)
	default:
		return fmt.Errorf("unknown WebDAV command %q", command)
	}
}

func startWebDAV(options globalOptions, config settings) error {
	if config.username == "" || config.password == "" {
		return errors.New("WebDAV 启动需要账号和密码，请在 ilanzou-webdav.toml、环境变量或命令行中设置")
	}
	baseDir := filepath.Dir(config.configPath)
	if absolute, err := filepath.Abs(baseDir); err == nil {
		baseDir = absolute
	}
	pidPath := filepath.Join(baseDir, "ilanzou-webdav.pid")
	logPath := filepath.Join(baseDir, "ilanzou-webdav.log")
	readyPath := filepath.Join(baseDir, "ilanzou-webdav.ready")
	if data, err := os.ReadFile(pidPath); err == nil {
		pid, parseErr := strconv.Atoi(strings.TrimSpace(string(data)))
		if parseErr == nil && processAlive(pid) {
			return fmt.Errorf("WebDAV 已在后台运行，PID %d；日志：%s", pid, logPath)
		}
		_ = os.Remove(pidPath)
	}
	_ = os.Remove(readyPath)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		logFile.Close()
		return err
	}
	childArgs := []string{"serve"}
	if options.noConfig {
		childArgs = append(childArgs, "--no-config")
	} else {
		childArgs = append(childArgs, "--config", config.configPath)
	}
	keys := make([]string, 0, len(options.values))
	for key := range options.values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		childArgs = append(childArgs, "--"+strings.ReplaceAll(key, "_", "-"), options.values[key])
	}
	child := exec.Command(executable, childArgs...)
	child.Env = append(os.Environ(), "ILANZOU_WEBDAV_PID_FILE="+pidPath, "ILANZOU_WEBDAV_READY_FILE="+readyPath)
	child.Stdin = nil
	child.Stdout = logFile
	child.Stderr = logFile
	configureDetached(child)
	if err := child.Start(); err != nil {
		logFile.Close()
		return err
	}
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(child.Process.Pid)+"\n"), 0644); err != nil {
		_ = child.Process.Kill()
		logFile.Close()
		return err
	}
	_ = logFile.Close()
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	deadline := time.NewTimer(30 * time.Second)
	defer deadline.Stop()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(readyPath); err == nil {
			fmt.Printf("WebDAV 已在后台启动，PID %d，地址 %s；日志：%s\n", child.Process.Pid, config.listen, logPath)
			return nil
		}
		select {
		case err := <-exited:
			_ = os.Remove(pidPath)
			log, _ := os.ReadFile(logPath)
			return fmt.Errorf("WebDAV 启动失败：%v\n%s", err, strings.TrimSpace(string(log)))
		case <-deadline.C:
			_ = child.Process.Kill()
			_ = os.Remove(pidPath)
			return fmt.Errorf("等待 WebDAV 启动超时；日志：%s", logPath)
		case <-ticker.C:
		}
	}
}

func serveWebDAV(config settings) error {
	pidPath := os.Getenv("ILANZOU_WEBDAV_PID_FILE")
	readyPath := os.Getenv("ILANZOU_WEBDAV_READY_FILE")
	if pidPath != "" {
		defer removePIDFile(pidPath, os.Getpid())
	}
	if readyPath != "" {
		defer os.Remove(readyPath)
	}
	if config.username == "" || config.password == "" {
		return errors.New("WebDAV 启动需要账号和密码，请在 ilanzou-webdav.toml、环境变量或命令行中设置")
	}
	ctx, cancel := signalContext()
	defer cancel()
	client := ilanzou.NewClient(config.username, config.password).
		SetIP(config.ip).
		SetTimeout(config.requestTimeout)
	if err := client.Init(ctx); err != nil {
		return err
	}
	listener, err := net.Listen("tcp", config.listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler: webdav.NewHandler(
			ilanzou.NewScopedClient(client, config.rootFolderID),
			config.username,
			config.password,
			config.uploadConcurrency,
			config.downloadConcurrency,
		),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if readyPath != "" {
		if err := os.WriteFile(readyPath, []byte(config.listen+"\n"), 0644); err != nil {
			listener.Close()
			return err
		}
	}
	serveResult := make(chan error, 1)
	go func() { serveResult <- server.Serve(listener) }()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer shutdownCancel()
		return server.Shutdown(shutdownCtx)
	}
}

func stopWebDAV(configPath string) error {
	baseDir := filepath.Dir(configPath)
	if absolute, err := filepath.Abs(baseDir); err == nil {
		baseDir = absolute
	}
	pidPath := filepath.Join(baseDir, "ilanzou-webdav.pid")
	data, err := os.ReadFile(pidPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return errors.New("WebDAV 当前未运行")
		}
		return err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 1 {
		return fmt.Errorf("无效的 PID 文件 %s", pidPath)
	}
	if err := signalProcess(pid); err != nil {
		if !processAlive(pid) {
			_ = os.Remove(pidPath)
			return errors.New("WebDAV 当前未运行，已清理过期 PID 文件")
		}
		return err
	}
	fmt.Printf("已向 WebDAV 进程 %d 发送停止信号\n", pid)
	return nil
}

func removePIDFile(pidPath string, pid int) {
	data, err := os.ReadFile(pidPath)
	if err != nil || strings.TrimSpace(string(data)) != strconv.Itoa(pid) {
		return
	}
	_ = os.Remove(pidPath)
}
