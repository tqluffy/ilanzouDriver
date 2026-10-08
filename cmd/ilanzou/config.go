package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
)

type settings struct {
	username            string
	password            string
	ip                  string
	rootFolderID        string
	uploadConcurrency   int
	downloadConcurrency int
	requestTimeout      time.Duration
}

type fileSettings struct {
	Username            string `toml:"username"`
	Password            string `toml:"password"`
	IP                  string `toml:"ip"`
	RootFolderID        string `toml:"root_folder_id"`
	UploadConcurrency   int    `toml:"upload_concurrency"`
	DownloadConcurrency int    `toml:"download_concurrency"`
	RequestTimeout      string `toml:"request_timeout"`
}

type globalOptions struct {
	values          map[string]string
	configPath      string
	configSpecified bool
	noConfig        bool
	help            bool
}

var configEnvironment = map[string]string{
	"username":             "ILANZOU_USERNAME",
	"password":             "ILANZOU_PASSWORD",
	"ip":                   "ILANZOU_IP",
	"root_folder_id":       "ILANZOU_ROOT_FOLDER_ID",
	"upload_concurrency":   "ILANZOU_UPLOAD_CONCURRENCY",
	"download_concurrency": "ILANZOU_DOWNLOAD_CONCURRENCY",
	"request_timeout":      "ILANZOU_REQUEST_TIMEOUT",
}

var commandLineConfigKeys = map[string]string{
	"--username":             "username",
	"--password":             "password",
	"--ip":                   "ip",
	"--root-folder-id":       "root_folder_id",
	"--upload-concurrency":   "upload_concurrency",
	"--download-concurrency": "download_concurrency",
	"--request-timeout":      "request_timeout",
}

func parseInvocation(args []string) (globalOptions, []string, error) {
	options := globalOptions{values: make(map[string]string)}
	positionals := make([]string, 0, len(args))
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positionals = append(positionals, args[i+1:]...)
			break
		}
		if arg == "-h" || arg == "--help" {
			options.help = true
			continue
		}
		if arg == "--no-config" {
			options.noConfig = true
			continue
		}
		if arg == "-c" || arg == "--config" {
			if i+1 >= len(args) {
				return options, nil, fmt.Errorf("%s requires a path", arg)
			}
			i++
			options.configPath = args[i]
			options.configSpecified = true
			continue
		}

		name, value, hasValue := stringsCutOption(arg)
		if name == "-c" || name == "--config" {
			if !hasValue {
				if i+1 >= len(args) {
					return options, nil, fmt.Errorf("%s requires a path", name)
				}
				i++
				value = args[i]
			}
			options.configPath = value
			options.configSpecified = true
			continue
		}
		key, ok := commandLineConfigKeys[name]
		if !ok {
			if strings.HasPrefix(arg, "-") {
				return options, nil, fmt.Errorf("unknown option %q", name)
			}
			positionals = append(positionals, arg)
			continue
		}
		if !hasValue {
			if i+1 >= len(args) {
				return options, nil, fmt.Errorf("%s requires a value", name)
			}
			i++
			value = args[i]
		}
		options.values[key] = value
	}
	return options, positionals, nil
}

func stringsCutOption(value string) (string, string, bool) {
	for i := 0; i < len(value); i++ {
		if value[i] == '=' {
			return value[:i], value[i+1:], true
		}
	}
	return value, "", false
}

func resolveSettings(options globalOptions) (settings, error) {
	values := map[string]string{
		"username":             "",
		"password":             "",
		"ip":                   "",
		"root_folder_id":       "0",
		"upload_concurrency":   "4",
		"download_concurrency": "32",
		"request_timeout":      "10m",
	}
	if !options.noConfig {
		configPath := options.configPath
		if !options.configSpecified {
			executable, err := os.Executable()
			if err != nil {
				return settings{}, err
			}
			configPath = filepath.Join(filepath.Dir(executable), "ilanzou.toml")
		}
		file, err := os.Open(configPath)
		if err == nil {
			var configured fileSettings
			metadata, decodeErr := toml.NewDecoder(file).Decode(&configured)
			closeErr := file.Close()
			if decodeErr != nil {
				return settings{}, fmt.Errorf("decode config %q: %w", configPath, decodeErr)
			}
			if closeErr != nil {
				return settings{}, closeErr
			}
			if unknown := metadata.Undecoded(); len(unknown) != 0 {
				return settings{}, fmt.Errorf("unknown config key(s): %v", unknown)
			}
			setTomlValue := func(key string, value string) {
				if metadata.IsDefined(key) {
					values[key] = value
				}
			}
			setTomlValue("username", configured.Username)
			setTomlValue("password", configured.Password)
			setTomlValue("ip", configured.IP)
			setTomlValue("root_folder_id", configured.RootFolderID)
			if metadata.IsDefined("upload_concurrency") {
				values["upload_concurrency"] = strconv.Itoa(configured.UploadConcurrency)
			}
			if metadata.IsDefined("download_concurrency") {
				values["download_concurrency"] = strconv.Itoa(configured.DownloadConcurrency)
			}
			setTomlValue("request_timeout", configured.RequestTimeout)
		} else if options.configSpecified || !errors.Is(err, os.ErrNotExist) {
			return settings{}, fmt.Errorf("open config %q: %w", configPath, err)
		}
	}
	for key, envName := range configEnvironment {
		if value, ok := os.LookupEnv(envName); ok {
			values[key] = value
		}
	}
	return applyOverrides(values, options.values)
}

func applyOverrides(values, commandLine map[string]string) (settings, error) {
	for key, value := range commandLine {
		values[key] = value
	}
	uploadConcurrency, err := strconv.Atoi(values["upload_concurrency"])
	if err != nil || uploadConcurrency < 1 {
		return settings{}, fmt.Errorf("配置无效：upload_concurrency=%q；请使用正整数，通过 --upload-concurrency 或 ILANZOU_UPLOAD_CONCURRENCY 设置", values["upload_concurrency"])
	}
	downloadConcurrency, err := strconv.Atoi(values["download_concurrency"])
	if err != nil || downloadConcurrency < 1 {
		return settings{}, fmt.Errorf("配置无效：download_concurrency=%q；请使用正整数，通过 --download-concurrency 或 ILANZOU_DOWNLOAD_CONCURRENCY 设置", values["download_concurrency"])
	}
	requestTimeout, err := time.ParseDuration(values["request_timeout"])
	if err != nil || requestTimeout <= 0 {
		return settings{}, fmt.Errorf("配置无效：request_timeout=%q；请使用正时长（例如 30s、10m），通过 --request-timeout 或 ILANZOU_REQUEST_TIMEOUT 设置", values["request_timeout"])
	}
	if _, err := strconv.ParseUint(values["root_folder_id"], 10, 64); err != nil {
		return settings{}, fmt.Errorf("配置无效：root_folder_id=%q；请使用数字文件夹 ID，通过 --root-folder-id 或 ILANZOU_ROOT_FOLDER_ID 设置", values["root_folder_id"])
	}
	return settings{
		username:            values["username"],
		password:            values["password"],
		ip:                  values["ip"],
		rootFolderID:        values["root_folder_id"],
		uploadConcurrency:   uploadConcurrency,
		downloadConcurrency: downloadConcurrency,
		requestTimeout:      requestTimeout,
	}, nil
}
