package main

/*
#include <stdint.h>
#include <stdlib.h>
*/
import "C"

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"sync/atomic"
	"time"
	"unsafe"

	"ilanzou"
)

type session struct {
	client *ilanzou.Client
	scope  *ilanzou.ScopedClient
}

var sessions = struct {
	sync.RWMutex
	byID map[uint64]*session
}{byID: make(map[uint64]*session)}

var nextSessionID atomic.Uint64

func main() {}

//export IlanzouNew
func IlanzouNew(username, password, ip, rootFolderID *C.char, requestTimeoutMS C.int64_t, outHandle *C.uint64_t, outError **C.char) C.int {
	if outHandle == nil {
		return fail(outError, fmt.Errorf("outHandle cannot be null"))
	}
	client := ilanzou.NewClient(goString(username), goString(password)).SetIP(goString(ip))
	if requestTimeoutMS > 0 {
		client.SetTimeout(time.Duration(requestTimeoutMS) * time.Millisecond)
	}
	handle := nextSessionID.Add(1)
	sessions.Lock()
	sessions.byID[handle] = &session{client: client, scope: ilanzou.NewScopedClient(client, goString(rootFolderID))}
	sessions.Unlock()
	*outHandle = C.uint64_t(handle)
	clearError(outError)
	return 0
}

//export IlanzouInit
func IlanzouInit(handle C.uint64_t, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := current.client.Init(ctx); err != nil {
		return fail(outError, err)
	}
	clearError(outError)
	return 0
}

//export IlanzouList
func IlanzouList(handle C.uint64_t, folderID *C.char, outJSON **C.char, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entries, err := current.scope.List(ctx, goString(folderID))
	if err != nil {
		return fail(outError, err)
	}
	return succeedJSON(outJSON, outError, entries)
}

//export IlanzouMakeDir
func IlanzouMakeDir(handle C.uint64_t, parentID, name *C.char, outJSON **C.char, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entry, err := current.scope.MakeDir(ctx, goString(parentID), goString(name))
	if err != nil {
		return fail(outError, err)
	}
	return succeedJSON(outJSON, outError, entry)
}

//export IlanzouUpload
func IlanzouUpload(handle C.uint64_t, folderID, localPath *C.char, outJSON **C.char, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	entry, err := current.scope.Upload(ctx, goString(folderID), goString(localPath))
	if err != nil {
		return fail(outError, err)
	}
	return succeedJSON(outJSON, outError, entry)
}

//export IlanzouDownload
func IlanzouDownload(handle C.uint64_t, fileID, localPath *C.char, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	reader, err := current.scope.Download(ctx, goString(fileID))
	if err != nil {
		return fail(outError, err)
	}
	defer reader.Close()
	file, err := os.Create(goString(localPath))
	if err != nil {
		return fail(outError, err)
	}
	_, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return fail(outError, copyErr)
	}
	if closeErr != nil {
		return fail(outError, closeErr)
	}
	clearError(outError)
	return 0
}

//export IlanzouMove
func IlanzouMove(handle C.uint64_t, id *C.char, isDir C.int, targetID *C.char, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := current.scope.Move(ctx, goString(id), isDir != 0, goString(targetID)); err != nil {
		return fail(outError, err)
	}
	clearError(outError)
	return 0
}

//export IlanzouRename
func IlanzouRename(handle C.uint64_t, id *C.char, isDir C.int, name *C.char, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := current.scope.Rename(ctx, goString(id), isDir != 0, goString(name)); err != nil {
		return fail(outError, err)
	}
	clearError(outError)
	return 0
}

//export IlanzouDelete
func IlanzouDelete(handle C.uint64_t, id *C.char, isDir C.int, outError **C.char) C.int {
	current, err := getSession(handle)
	if err != nil {
		return fail(outError, err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := current.scope.Remove(ctx, goString(id), isDir != 0); err != nil {
		return fail(outError, err)
	}
	clearError(outError)
	return 0
}

//export IlanzouClose
func IlanzouClose(handle C.uint64_t) {
	sessions.Lock()
	delete(sessions.byID, uint64(handle))
	sessions.Unlock()
}

//export IlanzouFreeString
func IlanzouFreeString(value *C.char) {
	C.free(unsafe.Pointer(value))
}

func getSession(handle C.uint64_t) (*session, error) {
	sessions.RLock()
	current := sessions.byID[uint64(handle)]
	sessions.RUnlock()
	if current == nil {
		return nil, fmt.Errorf("invalid or closed iLanZou handle %d", uint64(handle))
	}
	return current, nil
}

func goString(value *C.char) string {
	if value == nil {
		return ""
	}
	return C.GoString(value)
}

func fail(outError **C.char, err error) C.int {
	if outError != nil {
		*outError = C.CString(err.Error())
	}
	return 1
}

func clearError(outError **C.char) {
	if outError != nil {
		*outError = nil
	}
}

func succeedJSON(outJSON, outError **C.char, value interface{}) C.int {
	if outJSON == nil {
		return fail(outError, fmt.Errorf("outJSON cannot be null"))
	}
	data, err := json.Marshal(value)
	if err != nil {
		return fail(outError, err)
	}
	*outJSON = C.CString(string(data))
	clearError(outError)
	return 0
}
