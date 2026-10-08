package ilanzou

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
)

// ScopedClient limits file operations to rootFolderID and its descendants.
// Use "0" for the account root. The configured root itself cannot be moved,
// renamed, or removed.
type ScopedClient struct {
	client  *Client
	rootID  string
	mu      sync.Mutex
	listing map[string][]Entry
	parents map[string]string
}

// NewScopedClient creates a file-operation view of client limited to rootFolderID.
func NewScopedClient(client *Client, rootFolderID string) *ScopedClient {
	if rootFolderID == "" {
		rootFolderID = "0"
	}
	return &ScopedClient{
		client:  client,
		rootID:  rootFolderID,
		listing: make(map[string][]Entry),
		parents: make(map[string]string),
	}
}

// RootFolderID returns the configured operation boundary.
func (client *ScopedClient) RootFolderID() string { return client.rootID }

// List lists the root folder or one of its descendants.
func (client *ScopedClient) List(ctx context.Context, folderID string) ([]Entry, error) {
	if err := client.requireFolder(ctx, folderID); err != nil {
		return nil, err
	}
	return client.client.List(ctx, folderID)
}

// MakeDir creates a folder below a folder within the configured scope.
func (client *ScopedClient) MakeDir(ctx context.Context, parentID, name string) (Entry, error) {
	if err := client.requireFolder(ctx, parentID); err != nil {
		return Entry{}, err
	}
	entry, err := client.client.MakeDir(ctx, parentID, name)
	if err == nil {
		client.invalidate()
	}
	return entry, err
}

// Upload uploads a file to a folder within the configured scope.
func (client *ScopedClient) Upload(ctx context.Context, folderID, localPath string) (Entry, error) {
	if err := client.requireFolder(ctx, folderID); err != nil {
		return Entry{}, err
	}
	entry, err := client.client.Upload(ctx, folderID, localPath)
	if err == nil {
		client.invalidate()
	}
	return entry, err
}

// Download opens a file only if it belongs to the configured scope.
func (client *ScopedClient) Download(ctx context.Context, fileID string) (io.ReadCloser, error) {
	if _, err := client.requireObject(ctx, fileID, false); err != nil {
		return nil, err
	}
	return client.client.Download(ctx, fileID)
}

// Move moves an in-scope file or folder to an in-scope destination folder.
func (client *ScopedClient) Move(ctx context.Context, id string, isDir bool, targetID string) error {
	if _, err := client.requireObject(ctx, id, isDir); err != nil {
		return err
	}
	if err := client.requireFolder(ctx, targetID); err != nil {
		return err
	}
	if isDir && client.isDescendant(targetID, id) {
		return errors.New("cannot move a folder into itself or one of its descendants")
	}
	if err := client.client.Move(ctx, id, isDir, targetID); err != nil {
		return err
	}
	client.invalidate()
	return nil
}

// Rename renames an in-scope file or folder.
func (client *ScopedClient) Rename(ctx context.Context, id string, isDir bool, name string) error {
	if _, err := client.requireObject(ctx, id, isDir); err != nil {
		return err
	}
	if err := client.client.Rename(ctx, id, isDir, name); err != nil {
		return err
	}
	client.invalidate()
	return nil
}

// Remove permanently removes an in-scope file or folder.
func (client *ScopedClient) Remove(ctx context.Context, id string, isDir bool) error {
	if _, err := client.requireObject(ctx, id, isDir); err != nil {
		return err
	}
	if err := client.client.Remove(ctx, id, isDir); err != nil {
		return err
	}
	client.invalidate()
	return nil
}

func (client *ScopedClient) requireFolder(ctx context.Context, id string) error {
	entry, err := client.find(ctx, id)
	if err != nil {
		return err
	}
	if !entry.IsDir {
		return fmt.Errorf("object %q is not a folder", id)
	}
	return nil
}

func (client *ScopedClient) requireObject(ctx context.Context, id string, isDir bool) (Entry, error) {
	if id == client.rootID {
		return Entry{}, errors.New("the configured root folder cannot be moved, renamed, or deleted")
	}
	entry, err := client.find(ctx, id)
	if err != nil {
		return Entry{}, err
	}
	if entry.IsDir != isDir {
		if isDir {
			return Entry{}, fmt.Errorf("object %q is not a folder", id)
		}
		return Entry{}, fmt.Errorf("object %q is not a file", id)
	}
	return entry, nil
}

func (client *ScopedClient) find(ctx context.Context, id string) (Entry, error) {
	if id == client.rootID {
		return Entry{ID: id, IsDir: true}, nil
	}
	if id == "" {
		return Entry{}, errors.New("object ID cannot be empty")
	}
	client.mu.Lock()
	defer client.mu.Unlock()
	visited := make(map[string]bool)
	folders := []string{client.rootID}
	for len(folders) > 0 {
		folderID := folders[0]
		folders = folders[1:]
		if visited[folderID] {
			continue
		}
		visited[folderID] = true
		entries, ok := client.listing[folderID]
		if !ok {
			var err error
			entries, err = client.client.List(ctx, folderID)
			if err != nil {
				return Entry{}, err
			}
			client.listing[folderID] = entries
		}
		for _, entry := range entries {
			client.parents[entry.ID] = folderID
			if entry.ID == id {
				return entry, nil
			}
		}
		for _, entry := range entries {
			if entry.IsDir && !visited[entry.ID] {
				folders = append(folders, entry.ID)
			}
		}
	}
	return Entry{}, fmt.Errorf("object %q is outside configured root folder %q or does not exist", id, client.rootID)
}

func (client *ScopedClient) isDescendant(folderID, possibleAncestorID string) bool {
	client.mu.Lock()
	defer client.mu.Unlock()
	for id := folderID; id != ""; id = client.parents[id] {
		if id == possibleAncestorID {
			return true
		}
		if id == client.rootID {
			return false
		}
	}
	return false
}

func (client *ScopedClient) invalidate() {
	client.mu.Lock()
	client.listing = make(map[string][]Entry)
	client.parents = make(map[string]string)
	client.mu.Unlock()
}
