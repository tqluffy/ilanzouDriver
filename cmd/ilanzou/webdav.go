package main

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/BurntSushi/toml"
	"ilanzou"
	"ilanzou/webdav"
)

const webDAVConfigSuffix = ".ilanzou-webdav.toml"

type webDAVAccount struct {
	config  settings
	mu      sync.Mutex
	handler http.Handler
}

type webDAVAccountsHandler struct {
	accounts map[string]*webDAVAccount
}

func runWebDAVCommand(command string, options globalOptions) error {
	switch command {
	case "start":
		return startWebDAV(options)
	case "serve":
		return serveWebDAV(options)
	default:
		return fmt.Errorf("unknown WebDAV command %q", command)
	}
}

func webDAVConfigDir(options globalOptions) string {
	if options.configSpecified {
		return options.configPath
	}
	executable, err := os.Executable()
	if err != nil {
		return "ilanzou-config"
	}
	return filepath.Join(filepath.Dir(executable), "ilanzou-config")
}

func validateWebDAVOptions(options globalOptions) error {
	if options.noConfig {
		return errors.New("多用户 WebDAV 必须从 ilanzou-config 目录读取每个账号的 TOML")
	}
	for key := range options.values {
		if key != "listen" {
			return fmt.Errorf("WebDAV 多用户配置应写入账号 TOML；仅支持用 --listen 覆盖全局监听地址，%s 不能作为全局参数使用", key)
		}
	}
	return nil
}

func loadWebDAVAccounts(options globalOptions) (string, map[string]*webDAVAccount, string, error) {
	if err := validateWebDAVOptions(options); err != nil {
		return "", nil, "", err
	}
	configDir := webDAVConfigDir(options)
	if absolute, err := filepath.Abs(configDir); err == nil {
		configDir = absolute
	}
	entries, err := os.ReadDir(configDir)
	if err != nil {
		return "", nil, "", fmt.Errorf("read WebDAV config directory %q: %w", configDir, err)
	}
	accounts := make(map[string]*webDAVAccount)
	configuredListeners := make(map[string]bool)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), webDAVConfigSuffix) {
			continue
		}
		configPath := filepath.Join(configDir, entry.Name())
		config, hasListen, err := readWebDAVAccountConfig(configPath)
		if err != nil {
			return "", nil, "", err
		}
		if config.username == "" || config.password == "" {
			return "", nil, "", fmt.Errorf("%s must set iLanZou username and password", configPath)
		}
		if config.webdavUsername == "" || config.webdavPassword == "" {
			return "", nil, "", fmt.Errorf("%s must set webdav_username and webdav_password", configPath)
		}
		expectedName := config.webdavUsername + webDAVConfigSuffix
		if entry.Name() != expectedName {
			return "", nil, "", fmt.Errorf("WebDAV config %q must be named after webdav_username: %s", entry.Name(), expectedName)
		}
		accounts[config.webdavUsername] = &webDAVAccount{config: config}
		if hasListen {
			configuredListeners[config.listen] = true
		}
	}
	if len(accounts) == 0 {
		return "", nil, "", fmt.Errorf("no account configs found in %q; expected <webdav-username>%s", configDir, webDAVConfigSuffix)
	}

	if listen, ok := options.values["listen"]; ok {
		return configDir, accounts, listen, nil
	}
	if listen, ok := os.LookupEnv("ILANZOU_WEBDAV_LISTEN"); ok {
		return configDir, accounts, listen, nil
	}
	if len(configuredListeners) > 1 {
		return "", nil, "", fmt.Errorf("all account configs must use the same listen address, or override it with --listen")
	}
	for listen := range configuredListeners {
		return configDir, accounts, listen, nil
	}
	return configDir, accounts, "127.0.0.1:8080", nil
}

func readWebDAVAccountConfig(configPath string) (settings, bool, error) {
	var configured fileSettings
	metadata, err := toml.DecodeFile(configPath, &configured)
	if err != nil {
		return settings{}, false, fmt.Errorf("decode config %q: %w", configPath, err)
	}
	if unknown := metadata.Undecoded(); len(unknown) != 0 {
		return settings{}, false, fmt.Errorf("unknown config key(s) in %q: %v", configPath, unknown)
	}
	values := map[string]string{
		"username":             configured.Username,
		"password":             configured.Password,
		"webdav_username":      configured.WebDAVUsername,
		"webdav_password":      configured.WebDAVPassword,
		"ip":                   configured.IP,
		"root_folder_id":       "0",
		"listen":               "127.0.0.1:8080",
		"upload_concurrency":   "4",
		"download_concurrency": "32",
		"request_timeout":      "10m",
	}
	if metadata.IsDefined("root_folder_id") {
		values["root_folder_id"] = configured.RootFolderID
	}
	if metadata.IsDefined("listen") {
		values["listen"] = configured.Listen
	}
	if metadata.IsDefined("upload_concurrency") {
		values["upload_concurrency"] = strconv.Itoa(configured.UploadConcurrency)
	}
	if metadata.IsDefined("download_concurrency") {
		values["download_concurrency"] = strconv.Itoa(configured.DownloadConcurrency)
	}
	if metadata.IsDefined("request_timeout") {
		values["request_timeout"] = configured.RequestTimeout
	}
	resolved, err := applyOverrides(values, nil)
	if err != nil {
		return settings{}, false, err
	}
	return resolved, metadata.IsDefined("listen"), nil
}

func (account *webDAVAccount) requestHandler(ctx context.Context) (http.Handler, error) {
	account.mu.Lock()
	defer account.mu.Unlock()
	if account.handler != nil {
		return account.handler, nil
	}
	client := ilanzou.NewClient(account.config.username, account.config.password).
		SetIP(account.config.ip).
		SetTimeout(account.config.requestTimeout)
	if err := client.Init(ctx); err != nil {
		return nil, err
	}
	account.handler = webdav.NewHandler(
		ilanzou.NewScopedClient(client, account.config.rootFolderID),
		account.config.webdavUsername,
		account.config.webdavPassword,
		account.config.uploadConcurrency,
		account.config.downloadConcurrency,
	)
	return account.handler, nil
}

func (handler *webDAVAccountsHandler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	username, password, ok := request.BasicAuth()
	account := handler.accounts[username]
	if !ok || account == nil || subtle.ConstantTimeCompare([]byte(password), []byte(account.config.webdavPassword)) != 1 {
		response.Header().Set("WWW-Authenticate", `Basic realm="iLanZou WebDAV", charset="UTF-8"`)
		http.Error(response, "authentication required", http.StatusUnauthorized)
		return
	}
	accountHandler, err := account.requestHandler(request.Context())
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadGateway)
		return
	}
	accountHandler.ServeHTTP(response, request)
}

func startWebDAV(options globalOptions) error {
	configDir, _, listen, err := loadWebDAVAccounts(options)
	if err != nil {
		return err
	}
	pidPath := filepath.Join(configDir, "ilanzou-webdav.pid")
	logPath := filepath.Join(configDir, "ilanzou-webdav.log")
	readyPath := filepath.Join(configDir, "ilanzou-webdav.ready")
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
	childArgs := []string{"serve", "--config", configDir}
	if override, ok := options.values["listen"]; ok {
		childArgs = append(childArgs, "--listen", override)
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
			fmt.Printf("WebDAV 已在后台启动，PID %d，地址 %s；日志：%s\n", child.Process.Pid, listen, logPath)
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

func serveWebDAV(options globalOptions) error {
	pidPath := os.Getenv("ILANZOU_WEBDAV_PID_FILE")
	readyPath := os.Getenv("ILANZOU_WEBDAV_READY_FILE")
	if pidPath != "" {
		defer removePIDFile(pidPath, os.Getpid())
	}
	if readyPath != "" {
		defer os.Remove(readyPath)
	}
	_, accounts, listen, err := loadWebDAVAccounts(options)
	if err != nil {
		return err
	}
	ctx, cancel := signalContext()
	defer cancel()
	listener, err := net.Listen("tcp", listen)
	if err != nil {
		return err
	}
	server := &http.Server{
		Handler:           &webDAVAccountsHandler{accounts: accounts},
		ReadHeaderTimeout: 10 * time.Second,
	}
	if readyPath != "" {
		if err := os.WriteFile(readyPath, []byte(listen+"\n"), 0644); err != nil {
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

func stopWebDAV(configDir string) error {
	if absolute, err := filepath.Abs(configDir); err == nil {
		configDir = absolute
	}
	pidPath := filepath.Join(configDir, "ilanzou-webdav.pid")
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
