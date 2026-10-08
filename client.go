package ilanzou

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	apiBase     = "https://api.ilanzou.com"
	siteBase    = "https://www.ilanzou.com"
	apiSecret   = "lanZouY-disk-app"
	qiniuBucket = "wpanstore-lanzou"
	partSize    = 8 * 1024 * 1024
	userAgent   = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/125.0.0.0 Safari/537.36 Edg/125.0.0.0"
)

// Client contains the account session used by the iLanZou API.
type Client struct {
	username   string
	password   string
	client     *http.Client
	noRedirect *http.Client

	mu      sync.Mutex
	uuid    string
	token   string
	userID  string
	account string
}

// Entry is one file or folder returned by List or Upload.
type Entry struct {
	ID       string    `json:"id"`
	Name     string    `json:"name"`
	Size     int64     `json:"size"`
	Modified time.Time `json:"modified"`
	IsDir    bool      `json:"is_dir"`
}

type stringValue string

func (value *stringValue) UnmarshalJSON(data []byte) error {
	if len(data) > 0 && data[0] == '"' {
		var decoded string
		if err := json.Unmarshal(data, &decoded); err != nil {
			return err
		}
		*value = stringValue(decoded)
		return nil
	}
	*value = stringValue(data)
	return nil
}

// NewClient creates a client for an iLanZou account. Call Init before file operations.
func NewClient(username, password string) *Client {
	return &Client{
		username: username,
		password: password,
		client:   &http.Client{Timeout: 10 * time.Minute},
		noRedirect: &http.Client{
			Timeout:       2 * time.Minute,
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
	}
}

// Init obtains a device UUID and account metadata, logging in when required.
func (c *Client) Init(ctx context.Context) error {
	body, err := c.api(ctx, "/unproved/getUuid", http.MethodGet, false, nil, nil, false)
	if err != nil {
		return err
	}
	var uuidResp struct {
		UUID stringValue `json:"uuid"`
	}
	if err := json.Unmarshal(body, &uuidResp); err != nil {
		return err
	}
	c.mu.Lock()
	c.uuid = string(uuidResp.UUID)
	c.mu.Unlock()

	body, err = c.api(ctx, "/proved/user/account/map", http.MethodGet, true, nil, nil, false)
	if err != nil {
		return err
	}
	var accountResp struct {
		Map struct {
			UserID  stringValue `json:"userId"`
			Account stringValue `json:"account"`
		} `json:"map"`
	}
	if err := json.Unmarshal(body, &accountResp); err != nil {
		return err
	}
	c.mu.Lock()
	c.userID = string(accountResp.Map.UserID)
	c.account = string(accountResp.Map.Account)
	c.mu.Unlock()
	return nil
}

// List returns all entries directly inside folderID. Use "0" for the root.
func (c *Client) List(ctx context.Context, folderID string) ([]Entry, error) {
	var entries []Entry
	for offset := 1; ; offset++ {
		query := url.Values{}
		query.Set("offset", strconv.Itoa(offset))
		query.Set("limit", "60")
		query.Set("folderId", folderID)
		query.Set("type", "0")
		body, err := c.api(ctx, "/proved/record/file/list", http.MethodGet, true, query, nil, false)
		if err != nil {
			return nil, err
		}
		var response struct {
			Offset    int `json:"offset"`
			TotalPage int `json:"totalPage"`
			List      []struct {
				FolderID   int64  `json:"folderId"`
				FolderName string `json:"folderName"`
				FileID     int64  `json:"fileId"`
				FileName   string `json:"fileName"`
				FileSize   int64  `json:"fileSize"`
				FileType   int    `json:"fileType"`
				UpdTime    string `json:"updTime"`
			} `json:"list"`
		}
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, err
		}
		for _, item := range response.List {
			modified, err := time.ParseInLocation("2006-01-02 15:04:05", item.UpdTime, time.Local)
			if err != nil {
				return nil, err
			}
			entry := Entry{Modified: modified}
			if item.FileType == 2 {
				entry.ID = strconv.FormatInt(item.FolderID, 10)
				entry.Name = item.FolderName
				entry.IsDir = true
			} else {
				entry.ID = strconv.FormatInt(item.FileID, 10)
				entry.Name = item.FileName
				entry.Size = item.FileSize * 1024
			}
			entries = append(entries, entry)
		}
		if response.Offset >= response.TotalPage {
			return entries, nil
		}
	}
}

// Download opens a file's content stream. The caller must close the returned reader.
func (c *Client) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	c.mu.Lock()
	token := c.token
	c.mu.Unlock()
	if token == "" {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	var location *url.URL
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.Lock()
		uuid, token, userID := c.uuid, c.token, c.userID
		c.mu.Unlock()
		timestamp := time.Now().UnixMilli()
		params := []string{
			"uuid=" + url.QueryEscape(uuid),
			"devType=6",
			"devCode=" + url.QueryEscape(uuid),
			"devModel=chrome",
			"devVersion=125",
			"appVersion=",
			"timestamp=" + encryptHex(strconv.FormatInt(timestamp, 10)),
			"appToken=" + url.QueryEscape(token),
			"enable=0",
			"downloadId=" + url.QueryEscape(encryptHex(fileID+"|"+userID)),
			"auth=" + url.QueryEscape(encryptHex(fmt.Sprintf("%s|%d", fileID, timestamp))),
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/unproved/file/redirect?"+strings.Join(params, "&"), nil)
		if err != nil {
			return nil, err
		}
		setWebHeaders(request)
		response, err := c.noRedirect.Do(request)
		if err != nil {
			return nil, err
		}
		if response.StatusCode == http.StatusFound {
			location, err = response.Location()
			response.Body.Close()
			if err != nil {
				return nil, err
			}
			break
		}
		if attempt == 1 {
			body, _ := io.ReadAll(io.LimitReader(response.Body, 4096))
			response.Body.Close()
			var apiError struct {
				Code int    `json:"code"`
				Msg  string `json:"msg"`
			}
			_ = json.Unmarshal(body, &apiError)
			return nil, fmt.Errorf("download redirect failed: HTTP %d, content-type %q, code %d: %s", response.StatusCode, response.Header.Get("Content-Type"), apiError.Code, apiError.Msg)
		}
		response.Body.Close()
		if err := c.login(ctx); err != nil {
			return nil, err
		}
	}
	if location == nil {
		return nil, errors.New("download redirect returned no location")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, location.String(), nil)
	if err != nil {
		return nil, err
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		response.Body.Close()
		return nil, fmt.Errorf("download failed: HTTP %d", response.StatusCode)
	}
	return response.Body, nil
}

// MakeDir creates a folder below parentID.
func (c *Client) MakeDir(ctx context.Context, parentID, name string) (Entry, error) {
	body, err := c.api(ctx, "/proved/file/folder/save", http.MethodPost, true, nil, map[string]string{
		"folderDesc": "", "folderId": parentID, "folderName": name,
	}, false)
	if err != nil {
		return Entry{}, err
	}
	var response struct {
		List []struct {
			ID stringValue `json:"id"`
		} `json:"list"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return Entry{}, err
	}
	return Entry{ID: string(response.List[0].ID), Name: name, Modified: time.Now(), IsDir: true}, nil
}

// Move changes an entry's parent folder.
func (c *Client) Move(ctx context.Context, id string, isDir bool, targetID string) error {
	fileIDs, folderIDs := "", ""
	if isDir {
		folderIDs = id
	} else {
		fileIDs = id
	}
	_, err := c.api(ctx, "/proved/file/folder/move", http.MethodPost, true, nil, map[string]string{
		"folderIds": folderIDs, "fileIds": fileIDs, "targetId": targetID,
	}, false)
	return err
}

// Rename changes a file or folder name.
func (c *Client) Rename(ctx context.Context, id string, isDir bool, name string) error {
	path := "/proved/file/edit"
	body := map[string]string{"fileDesc": "", "fileId": id, "fileName": name}
	if isDir {
		path = "/proved/file/folder/edit"
		body = map[string]string{"folderDesc": "", "folderId": id, "folderName": name}
	}
	_, err := c.api(ctx, path, http.MethodPost, true, nil, body, false)
	return err
}

// Remove permanently removes a file or folder.
func (c *Client) Remove(ctx context.Context, id string, isDir bool) error {
	fileIDs, folderIDs := "", ""
	if isDir {
		folderIDs = id
	} else {
		fileIDs = id
	}
	_, err := c.api(ctx, "/proved/file/delete", http.MethodPost, true, nil, map[string]interface{}{
		"folderIds": folderIDs, "fileIds": fileIDs, "status": 0,
	}, false)
	return err
}

// Upload uploads a local file to folderID using Qiniu's single-part or multipart API.
func (c *Client) Upload(ctx context.Context, folderID, localPath string) (Entry, error) {
	file, err := os.Open(localPath)
	if err != nil {
		return Entry{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return Entry{}, err
	}
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return Entry{}, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return Entry{}, err
	}
	name := filepath.Base(localPath)
	md5sum := hex.EncodeToString(hash.Sum(nil))
	body, err := c.api(ctx, "/proved/7n/getUpToken", http.MethodPost, true, nil, map[string]interface{}{
		"fileId": "", "fileName": name, "fileSize": info.Size()/1024 + 1,
		"folderId": folderID, "md5": md5sum, "type": 1,
	}, false)
	if err != nil {
		return Entry{}, err
	}
	var tokenResponse struct {
		UpToken string `json:"upToken"`
	}
	if err := json.Unmarshal(body, &tokenResponse); err != nil {
		return Entry{}, err
	}
	if tokenResponse.UpToken == "" {
		return Entry{}, errors.New("iLanZou returned an empty Qiniu upload token")
	}
	c.mu.Lock()
	account := c.account
	c.mu.Unlock()
	now := time.Now()
	key := fmt.Sprintf("disk/%d/%d/%d/%s/%016d", now.Year(), now.Month(), now.Day(), account, now.UnixMilli())
	var token string
	if info.Size() <= partSize {
		token, err = c.uploadSingle(ctx, tokenResponse.UpToken, key, name, file, info.Size())
	} else {
		token, err = c.uploadMultipart(ctx, tokenResponse.UpToken, key, name, file, info.Size())
	}
	if err != nil {
		return Entry{}, err
	}
	var result struct {
		List []struct {
			FileID   int64  `json:"fileId"`
			FileName string `json:"fileName"`
			Status   int    `json:"status"`
		} `json:"list"`
	}
	for attempt := 0; attempt < 10; attempt++ {
		query := url.Values{}
		query.Set("tokenList", token)
		query.Set("tokenTime", time.Now().Format("Mon Jan 02 2006 15:04:05 GMT-0700 (MST)"))
		body, err = c.api(ctx, "/unproved/7n/results", http.MethodPost, false, query, nil, false)
		if err != nil {
			return Entry{}, err
		}
		if err := json.Unmarshal(body, &result); err != nil {
			return Entry{}, err
		}
		if len(result.List) == 0 {
			return Entry{}, errors.New("upload commit returned no files")
		}
		if result.List[0].Status == 1 {
			return Entry{ID: strconv.FormatInt(result.List[0].FileID, 10), Name: result.List[0].FileName, Size: info.Size(), Modified: info.ModTime()}, nil
		}
		if attempt < 9 {
			select {
			case <-ctx.Done():
				return Entry{}, ctx.Err()
			case <-time.After(time.Second):
			}
		}
	}
	return Entry{}, fmt.Errorf("upload commit failed with status %d", result.List[0].Status)
}

func (c *Client) uploadSingle(ctx context.Context, token, key, name string, file io.Reader, size int64) (string, error) {
	body, contentType, contentLength, err := singleUploadBody(token, key, name, file, size)
	if err != nil {
		return "", err
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://upload.qiniup.com/", body)
	if err != nil {
		return "", err
	}
	request.Header.Set("Content-Type", contentType)
	request.ContentLength = contentLength
	responseBody, err := c.qiniuDo(request)
	if err != nil {
		return "", fmt.Errorf("Qiniu single-part upload: %w", err)
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(responseBody, &response); err != nil {
		return "", err
	}
	return response.Token, nil
}

func (c *Client) uploadMultipart(ctx context.Context, token, key, name string, file io.Reader, size int64) (string, error) {
	encodedKey := url.PathEscape(base64URL([]byte(key)))
	baseURL := "https://upload.qiniup.com/buckets/" + qiniuBucket + "/objects/" + encodedKey
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/uploads", nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "UpToken "+token)
	body, err := c.qiniuDo(request)
	if err != nil {
		return "", err
	}
	var initResponse struct {
		UploadID string `json:"uploadId"`
	}
	if err := json.Unmarshal(body, &initResponse); err != nil {
		return "", err
	}
	parts := make([]struct {
		PartNumber int    `json:"partNumber"`
		ETag       string `json:"etag"`
	}, 0, (size+partSize-1)/partSize)
	for number, remaining := 1, size; remaining > 0; number++ {
		length := int64(partSize)
		if remaining < length {
			length = remaining
		}
		partURL := fmt.Sprintf("%s/uploads/%s/%d", baseURL, url.PathEscape(initResponse.UploadID), number)
		request, err = http.NewRequestWithContext(ctx, http.MethodPut, partURL, io.LimitReader(file, length))
		if err != nil {
			return "", err
		}
		request.ContentLength = length
		request.Header.Set("Authorization", "UpToken "+token)
		body, err = c.qiniuDo(request)
		if err != nil {
			return "", err
		}
		var partResponse struct {
			ETag string `json:"etag"`
		}
		if err := json.Unmarshal(body, &partResponse); err != nil {
			return "", err
		}
		parts = append(parts, struct {
			PartNumber int    `json:"partNumber"`
			ETag       string `json:"etag"`
		}{number, partResponse.ETag})
		remaining -= length
	}
	completeBody, err := json.Marshal(struct {
		Name  string      `json:"fnmae"`
		Parts interface{} `json:"parts"`
	}{name, parts})
	if err != nil {
		return "", err
	}
	request, err = http.NewRequestWithContext(ctx, http.MethodPost, baseURL+"/uploads/"+url.PathEscape(initResponse.UploadID), bytes.NewReader(completeBody))
	if err != nil {
		return "", err
	}
	request.Header.Set("Authorization", "UpToken "+token)
	request.Header.Set("Content-Type", "application/json")
	body, err = c.qiniuDo(request)
	if err != nil {
		return "", err
	}
	var response struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return "", err
	}
	return response.Token, nil
}

func (c *Client) qiniuDo(request *http.Request) ([]byte, error) {
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("Qiniu request failed: HTTP %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c *Client) api(ctx context.Context, path, method string, proved bool, query url.Values, payload interface{}, retried bool) ([]byte, error) {
	if proved {
		c.mu.Lock()
		token := c.token
		c.mu.Unlock()
		if token == "" {
			if err := c.login(ctx); err != nil {
				return nil, err
			}
		}
	}
	c.mu.Lock()
	uuid, token := c.uuid, c.token
	c.mu.Unlock()
	timestamp := time.Now().UnixMilli()
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	requestURL := apiBase + path + "?" + orderedAPIQuery(uuid, token, proved, timestamp, query)
	request, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, err
	}
	setWebHeaders(request)
	request.Header.Set("Origin", siteBase)
	if payload != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	responseBody, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("iLanZou request failed: HTTP %d", response.StatusCode)
	}
	var envelope struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if err := json.Unmarshal(responseBody, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code == 200 {
		return responseBody, nil
	}
	if proved && !retried && (envelope.Code == -1 || envelope.Code == -2 || token == "") {
		if err := c.login(ctx); err != nil {
			return nil, err
		}
		return c.api(ctx, path, method, proved, query, payload, true)
	}
	return nil, fmt.Errorf("iLanZou API %s: %s", path, envelope.Msg)
}

func (c *Client) login(ctx context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	payload, err := json.Marshal(map[string]string{"loginName": c.username, "loginPwd": c.password})
	if err != nil {
		return err
	}
	timestamp := time.Now().UnixMilli()
	query := orderedAPIQuery(c.uuid, "", false, timestamp, nil)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, apiBase+"/unproved/login?"+query, bytes.NewReader(payload))
	if err != nil {
		return err
	}
	setWebHeaders(request)
	request.Header.Set("Origin", siteBase)
	request.Header.Set("Content-Type", "application/json")
	response, err := c.client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	body, err := io.ReadAll(response.Body)
	if err != nil {
		return err
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("iLanZou login failed: HTTP %d", response.StatusCode)
	}
	var loginResponse struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			Token string `json:"appToken"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &loginResponse); err != nil {
		return err
	}
	if loginResponse.Code != 200 || loginResponse.Data.Token == "" {
		return fmt.Errorf("iLanZou login failed: %s", loginResponse.Msg)
	}
	c.token = loginResponse.Data.Token
	return nil
}

func setWebHeaders(request *http.Request) {
	request.Header.Set("Accept", "application/json, text/plain, */*")
	request.Header.Set("Referer", siteBase+"/")
	request.Header.Set("User-Agent", userAgent)
	request.Header.Set("Accept-Language", "zh-CN,zh;q=0.9,en;q=0.8,en-GB;q=0.7,en-US;q=0.6,mt;q=0.5")
}

func encryptHex(plain string) string {
	block, _ := aes.NewCipher([]byte(apiSecret))
	padding := block.BlockSize() - len(plain)%block.BlockSize()
	data := append([]byte(plain), bytes.Repeat([]byte{byte(padding)}, padding)...)
	for offset := 0; offset < len(data); offset += block.BlockSize() {
		block.Encrypt(data[offset:offset+block.BlockSize()], data[offset:offset+block.BlockSize()])
	}
	return hex.EncodeToString(data)
}

func orderedAPIQuery(uuid, token string, proved bool, timestamp int64, extra url.Values) string {
	params := []string{
		"uuid=" + url.QueryEscape(uuid),
		"devType=6",
		"devCode=" + url.QueryEscape(uuid),
		"devModel=chrome",
		"devVersion=125",
		"appVersion=",
		"timestamp=" + encryptHex(strconv.FormatInt(timestamp, 10)),
	}
	if proved {
		params = append(params, "appToken="+url.QueryEscape(token))
	}
	params = append(params, "extra=2")
	if len(extra) == 0 {
		return strings.Join(params, "&")
	}
	orderedKeys := []string{"offset", "limit", "folderId", "type", "tokenList", "tokenTime"}
	seen := make(map[string]bool, len(extra))
	appendValues := func(key string) {
		for _, value := range extra[key] {
			params = append(params, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
		seen[key] = true
	}
	for _, key := range orderedKeys {
		if _, ok := extra[key]; ok {
			appendValues(key)
		}
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		if !seen[key] {
			keys = append(keys, key)
		}
	}
	sort.Strings(keys)
	for _, key := range keys {
		appendValues(key)
	}
	return strings.Join(params, "&")
}

func singleUploadBody(token, key, name string, file io.Reader, size int64) (io.Reader, string, int64, error) {
	var prefix bytes.Buffer
	writer := multipart.NewWriter(&prefix)
	for _, field := range [][2]string{{"token", token}, {"key", key}, {"fname", name}} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			return nil, "", 0, err
		}
	}
	contentType := mime.TypeByExtension(filepath.Ext(name))
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	header := make(textproto.MIMEHeader)
	header.Set("Content-Disposition", mime.FormatMediaType("form-data", map[string]string{"name": "file", "filename": name}))
	header.Set("Content-Type", contentType)
	if _, err := writer.CreatePart(header); err != nil {
		return nil, "", 0, err
	}
	closeStart := prefix.Len()
	if err := writer.Close(); err != nil {
		return nil, "", 0, err
	}
	all := prefix.Bytes()
	start := append([]byte(nil), all[:closeStart]...)
	end := append([]byte(nil), all[closeStart:]...)
	return io.MultiReader(bytes.NewReader(start), file, bytes.NewReader(end)), writer.FormDataContentType(), int64(len(start)+len(end)) + size, nil
}

func base64URL(value []byte) string {
	return base64.URLEncoding.EncodeToString(value)
}
