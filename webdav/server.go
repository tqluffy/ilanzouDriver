package webdav

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"ilanzou"
)

var (
	errNotFound     = errors.New("webdav path not found")
	errNotDirectory = errors.New("webdav parent is not a directory")
)

type Handler struct {
	client        *ilanzou.ScopedClient
	uploadSlots   chan struct{}
	downloadSlots chan struct{}
}

type resolvedPath struct {
	entry    ilanzou.Entry
	parentID string
	path     string
}

func NewHandler(client *ilanzou.ScopedClient, uploadConcurrency, downloadConcurrency int) http.Handler {
	return &Handler{
		client:        client,
		uploadSlots:   make(chan struct{}, uploadConcurrency),
		downloadSlots: make(chan struct{}, downloadConcurrency),
	}
}

func (handler *Handler) ServeHTTP(response http.ResponseWriter, request *http.Request) {
	switch request.Method {
	case http.MethodOptions:
		response.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, PROPFIND, MOVE, COPY")
		response.Header().Set("DAV", "1")
		response.WriteHeader(http.StatusOK)
	case "PROPFIND":
		handler.propfind(response, request)
	case http.MethodHead:
		handler.get(response, request)
	case http.MethodGet:
		if !handler.acquire(response, request, handler.downloadSlots) {
			return
		}
		defer handler.release(handler.downloadSlots)
		handler.get(response, request)
	case http.MethodPut:
		if !handler.acquire(response, request, handler.uploadSlots) {
			return
		}
		defer handler.release(handler.uploadSlots)
		handler.put(response, request)
	case "MKCOL":
		handler.mkdir(response, request)
	case http.MethodDelete:
		handler.remove(response, request)
	case "MOVE":
		handler.move(response, request)
	case "COPY":
		if !handler.acquire(response, request, handler.downloadSlots, handler.uploadSlots) {
			return
		}
		defer handler.release(handler.downloadSlots, handler.uploadSlots)
		handler.copy(response, request)
	default:
		response.Header().Set("Allow", "OPTIONS, GET, HEAD, PUT, DELETE, MKCOL, PROPFIND, MOVE, COPY")
		http.Error(response, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (handler *Handler) acquire(response http.ResponseWriter, request *http.Request, slots ...chan struct{}) bool {
	acquired := make([]chan struct{}, 0, len(slots))
	for _, slot := range slots {
		select {
		case slot <- struct{}{}:
			acquired = append(acquired, slot)
		case <-request.Context().Done():
			handler.release(acquired...)
			http.Error(response, "request canceled", http.StatusRequestTimeout)
			return false
		}
	}
	return true
}

func (handler *Handler) release(slots ...chan struct{}) {
	for index := len(slots) - 1; index >= 0; index-- {
		<-slots[index]
	}
}

func (handler *Handler) resolve(ctx context.Context, requestPath string) (resolvedPath, error) {
	cleanPath := path.Clean("/" + requestPath)
	rootID := handler.client.RootFolderID()
	current := resolvedPath{entry: ilanzou.Entry{ID: rootID, IsDir: true}, path: "/"}
	if cleanPath == "/" {
		return current, nil
	}
	segments := strings.Split(strings.TrimPrefix(cleanPath, "/"), "/")
	for _, segment := range segments {
		if !current.entry.IsDir {
			return resolvedPath{}, errNotFound
		}
		entries, err := handler.client.List(ctx, current.entry.ID)
		if err != nil {
			return resolvedPath{}, err
		}
		found := false
		for _, entry := range entries {
			if entry.Name == segment {
				current = resolvedPath{entry: entry, parentID: current.entry.ID, path: path.Join(current.path, segment)}
				found = true
				break
			}
		}
		if !found {
			return resolvedPath{}, errNotFound
		}
	}
	return current, nil
}

func (handler *Handler) parent(ctx context.Context, requestPath string) (resolvedPath, string, error) {
	cleanPath := path.Clean("/" + requestPath)
	if cleanPath == "/" {
		return resolvedPath{}, "", errNotDirectory
	}
	parent, err := handler.resolve(ctx, path.Dir(cleanPath))
	if err != nil {
		return resolvedPath{}, "", err
	}
	if !parent.entry.IsDir {
		return resolvedPath{}, "", errNotDirectory
	}
	return parent, path.Base(cleanPath), nil
}

func (handler *Handler) propfind(response http.ResponseWriter, request *http.Request) {
	item, err := handler.resolve(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	depth := request.Header.Get("Depth")
	if depth == "" {
		depth = "infinity"
	}
	if depth != "0" && depth != "1" && depth != "infinity" {
		http.Error(response, "invalid Depth", http.StatusBadRequest)
		return
	}
	items := []resolvedPath{item}
	if item.entry.IsDir && depth != "0" {
		if err := handler.appendChildren(request.Context(), &items, item, depth == "infinity"); err != nil {
			writeError(response, err)
			return
		}
	}
	response.Header().Set("Content-Type", "application/xml; charset=utf-8")
	response.WriteHeader(207)
	_, _ = io.WriteString(response, xml.Header)
	_, _ = io.WriteString(response, `<D:multistatus xmlns:D="DAV:">`)
	for _, entry := range items {
		writePropResponse(response, entry)
	}
	_, _ = io.WriteString(response, `</D:multistatus>`)
}

func (handler *Handler) appendChildren(ctx context.Context, output *[]resolvedPath, parent resolvedPath, recursive bool) error {
	entries, err := handler.client.List(ctx, parent.entry.ID)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		childPath := path.Join(parent.path, entry.Name)
		child := resolvedPath{entry: entry, parentID: parent.entry.ID, path: childPath}
		*output = append(*output, child)
		if recursive && entry.IsDir {
			if err := handler.appendChildren(ctx, output, child, true); err != nil {
				return err
			}
		}
	}
	return nil
}

func writePropResponse(response io.Writer, item resolvedPath) {
	href := escapedHref(item.path, item.entry.IsDir)
	_, _ = fmt.Fprintf(response, `<D:response><D:href>%s</D:href><D:propstat><D:prop>`, xmlText(href))
	if item.entry.IsDir {
		_, _ = io.WriteString(response, `<D:resourcetype><D:collection/></D:resourcetype>`)
	} else {
		_, _ = io.WriteString(response, `<D:resourcetype/>`)
		_, _ = fmt.Fprintf(response, `<D:getcontentlength>%d</D:getcontentlength>`, item.entry.Size)
		contentType := "application/octet-stream"
		if detected := mimeType(item.entry.Name); detected != "" {
			contentType = detected
		}
		_, _ = fmt.Fprintf(response, `<D:getcontenttype>%s</D:getcontenttype>`, xmlText(contentType))
	}
	if !item.entry.Modified.IsZero() {
		_, _ = fmt.Fprintf(response, `<D:getlastmodified>%s</D:getlastmodified>`, item.entry.Modified.UTC().Format(http.TimeFormat))
	}
	if item.entry.ID != "" {
		etag := fmt.Sprintf(`"%s-%d-%d"`, item.entry.ID, item.entry.Size, item.entry.Modified.Unix())
		_, _ = fmt.Fprintf(response, `<D:getetag>%s</D:getetag>`, xmlText(etag))
	}
	_, _ = io.WriteString(response, `</D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat></D:response>`)
}

func escapedHref(value string, directory bool) string {
	clean := path.Clean("/" + value)
	if clean == "/" {
		return "/"
	}
	segments := strings.Split(strings.TrimPrefix(clean, "/"), "/")
	for index := range segments {
		segments[index] = url.PathEscape(segments[index])
	}
	href := "/" + strings.Join(segments, "/")
	if directory {
		href += "/"
	}
	return href
}

func xmlText(value string) string {
	var output strings.Builder
	_ = xml.EscapeText(&output, []byte(value))
	return output.String()
}

func mimeType(name string) string {
	return mime.TypeByExtension(filepath.Ext(name))
}

func (handler *Handler) get(response http.ResponseWriter, request *http.Request) {
	item, err := handler.resolve(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	if item.entry.IsDir {
		http.Error(response, "cannot read a collection", http.StatusMethodNotAllowed)
		return
	}
	if item.entry.Size >= 0 {
		response.Header().Set("Content-Length", strconv.FormatInt(item.entry.Size, 10))
	}
	if !item.entry.Modified.IsZero() {
		response.Header().Set("Last-Modified", item.entry.Modified.UTC().Format(http.TimeFormat))
	}
	if detected := mimeType(item.entry.Name); detected != "" {
		response.Header().Set("Content-Type", detected)
	}
	if request.Method == http.MethodHead {
		response.WriteHeader(http.StatusOK)
		return
	}
	reader, err := handler.client.Download(request.Context(), item.entry.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	defer reader.Close()
	response.WriteHeader(http.StatusOK)
	_, _ = io.Copy(response, reader)
}

func (handler *Handler) put(response http.ResponseWriter, request *http.Request) {
	parent, name, err := handler.parent(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	entries, err := handler.client.List(request.Context(), parent.entry.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	var existing *ilanzou.Entry
	for index := range entries {
		if entries[index].Name == name {
			existing = &entries[index]
			break
		}
	}
	if existing != nil && existing.IsDir {
		http.Error(response, "target is a collection", http.StatusConflict)
		return
	}
	if existing != nil && request.Header.Get("Overwrite") == "F" {
		response.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	entry, err := handler.upload(request.Context(), parent.entry.ID, name, request.Body)
	if err != nil {
		writeError(response, err)
		return
	}
	status := http.StatusCreated
	if existing != nil {
		if err := handler.client.Remove(request.Context(), existing.ID, false); err != nil {
			writeError(response, err)
			return
		}
		status = http.StatusNoContent
	}
	response.Header().Set("ETag", fmt.Sprintf(`"%s-%d"`, entry.ID, entry.Size))
	response.WriteHeader(status)
}

func (handler *Handler) upload(ctx context.Context, parentID, name string, input io.Reader) (ilanzou.Entry, error) {
	directory, err := os.MkdirTemp("", "ilanzou-webdav-")
	if err != nil {
		return ilanzou.Entry{}, err
	}
	defer os.RemoveAll(directory)
	localPath := filepath.Join(directory, name)
	file, err := os.Create(localPath)
	if err != nil {
		return ilanzou.Entry{}, err
	}
	_, copyErr := io.Copy(file, input)
	closeErr := file.Close()
	if copyErr != nil {
		return ilanzou.Entry{}, copyErr
	}
	if closeErr != nil {
		return ilanzou.Entry{}, closeErr
	}
	return handler.client.Upload(ctx, parentID, localPath)
}

func (handler *Handler) mkdir(response http.ResponseWriter, request *http.Request) {
	parent, name, err := handler.parent(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	entries, err := handler.client.List(request.Context(), parent.entry.ID)
	if err != nil {
		writeError(response, err)
		return
	}
	for _, entry := range entries {
		if entry.Name == name {
			http.Error(response, "target already exists", http.StatusMethodNotAllowed)
			return
		}
	}
	if _, err := handler.client.MakeDir(request.Context(), parent.entry.ID, name); err != nil {
		writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusCreated)
}

func (handler *Handler) remove(response http.ResponseWriter, request *http.Request) {
	item, err := handler.resolve(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	if item.path == "/" {
		http.Error(response, "cannot delete the WebDAV root", http.StatusForbidden)
		return
	}
	if err := handler.client.Remove(request.Context(), item.entry.ID, item.entry.IsDir); err != nil {
		writeError(response, err)
		return
	}
	response.WriteHeader(http.StatusNoContent)
}

func (handler *Handler) move(response http.ResponseWriter, request *http.Request) {
	source, err := handler.resolve(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	if source.path == "/" {
		http.Error(response, "cannot move the WebDAV root", http.StatusForbidden)
		return
	}
	destinationPath, err := destinationPath(request)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	targetParent, targetName, existing, err := handler.target(request.Context(), destinationPath, source.entry.Name)
	if err != nil {
		writeError(response, err)
		return
	}
	if path.Clean(destinationPath) == source.path || (existing != nil && existing.ID == source.entry.ID) {
		response.WriteHeader(http.StatusNoContent)
		return
	}
	if existing != nil && request.Header.Get("Overwrite") == "F" {
		response.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	if existing != nil {
		if err := handler.client.Remove(request.Context(), existing.ID, existing.IsDir); err != nil {
			writeError(response, err)
			return
		}
	}
	if err := handler.client.Move(request.Context(), source.entry.ID, source.entry.IsDir, targetParent.entry.ID); err != nil {
		writeError(response, err)
		return
	}
	if source.entry.Name != targetName {
		if err := handler.client.Rename(request.Context(), source.entry.ID, source.entry.IsDir, targetName); err != nil {
			writeError(response, err)
			return
		}
	}
	if existing != nil {
		response.WriteHeader(http.StatusNoContent)
	} else {
		response.WriteHeader(http.StatusCreated)
	}
}

func (handler *Handler) target(ctx context.Context, destinationPath, sourceName string) (resolvedPath, string, *ilanzou.Entry, error) {
	destination, err := handler.resolve(ctx, destinationPath)
	if err == nil && destination.entry.IsDir {
		parent := destination
		name := sourceName
		entries, listErr := handler.client.List(ctx, parent.entry.ID)
		if listErr != nil {
			return resolvedPath{}, "", nil, listErr
		}
		for index := range entries {
			if entries[index].Name == name {
				return parent, name, &entries[index], nil
			}
		}
		return parent, name, nil, nil
	}
	if err != nil && !errors.Is(err, errNotFound) {
		return resolvedPath{}, "", nil, err
	}
	parent, name, parentErr := handler.parent(ctx, destinationPath)
	if parentErr != nil {
		return resolvedPath{}, "", nil, parentErr
	}
	if err == nil {
		entries, listErr := handler.client.List(ctx, parent.entry.ID)
		if listErr != nil {
			return resolvedPath{}, "", nil, listErr
		}
		for index := range entries {
			if entries[index].Name == name {
				return parent, name, &entries[index], nil
			}
		}
	}
	return parent, name, nil, nil
}

func destinationPath(request *http.Request) (string, error) {
	value := request.Header.Get("Destination")
	if value == "" {
		return "", errors.New("Destination header is required")
	}
	destination, err := url.Parse(value)
	if err != nil {
		return "", err
	}
	if destination.Host != "" && !strings.EqualFold(destination.Host, request.Host) {
		return "", errors.New("Destination must use this server")
	}
	if destination.Path == "" {
		return "", errors.New("Destination path is required")
	}
	return path.Clean("/" + destination.Path), nil
}

func (handler *Handler) copy(response http.ResponseWriter, request *http.Request) {
	source, err := handler.resolve(request.Context(), request.URL.Path)
	if err != nil {
		writeError(response, err)
		return
	}
	if source.path == "/" {
		http.Error(response, "cannot copy the WebDAV root", http.StatusForbidden)
		return
	}
	destinationPath, err := destinationPath(request)
	if err != nil {
		http.Error(response, err.Error(), http.StatusBadRequest)
		return
	}
	if path.Clean(destinationPath) == source.path {
		http.Error(response, "source and destination are the same", http.StatusForbidden)
		return
	}
	depth := request.Header.Get("Depth")
	if depth == "" {
		depth = "infinity"
	}
	if depth != "0" && depth != "infinity" {
		http.Error(response, "invalid Depth", http.StatusBadRequest)
		return
	}
	targetParent, targetName, existing, err := handler.target(request.Context(), destinationPath, source.entry.Name)
	if err != nil {
		writeError(response, err)
		return
	}
	if existing != nil && request.Header.Get("Overwrite") == "F" {
		response.WriteHeader(http.StatusPreconditionFailed)
		return
	}
	if existing != nil {
		if err := handler.client.Remove(request.Context(), existing.ID, existing.IsDir); err != nil {
			writeError(response, err)
			return
		}
	}
	if source.entry.IsDir {
		created, err := handler.client.MakeDir(request.Context(), targetParent.entry.ID, targetName)
		if err != nil {
			writeError(response, err)
			return
		}
		if depth == "infinity" {
			if err := handler.copyChildren(request.Context(), source.entry.ID, created.ID); err != nil {
				writeError(response, err)
				return
			}
		}
	} else {
		if err := handler.copyFile(request.Context(), source.entry.ID, targetParent.entry.ID, targetName); err != nil {
			writeError(response, err)
			return
		}
	}
	if existing != nil {
		response.WriteHeader(http.StatusNoContent)
	} else {
		response.WriteHeader(http.StatusCreated)
	}
}

func (handler *Handler) copyChildren(ctx context.Context, sourceFolderID, targetFolderID string) error {
	entries, err := handler.client.List(ctx, sourceFolderID)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if entry.IsDir {
			created, err := handler.client.MakeDir(ctx, targetFolderID, entry.Name)
			if err != nil {
				return err
			}
			if err := handler.copyChildren(ctx, entry.ID, created.ID); err != nil {
				return err
			}
		} else if err := handler.copyFile(ctx, entry.ID, targetFolderID, entry.Name); err != nil {
			return err
		}
	}
	return nil
}

func (handler *Handler) copyFile(ctx context.Context, fileID, targetFolderID, name string) error {
	reader, err := handler.client.Download(ctx, fileID)
	if err != nil {
		return err
	}
	defer reader.Close()
	_, err = handler.upload(ctx, targetFolderID, name, reader)
	return err
}

func writeError(response http.ResponseWriter, err error) {
	status := http.StatusInternalServerError
	switch {
	case errors.Is(err, errNotFound):
		status = http.StatusNotFound
	case errors.Is(err, errNotDirectory):
		status = http.StatusConflict
	}
	http.Error(response, err.Error(), status)
}
