package main

import (
	"context"
	"errors"
	"fmt"

	"ilanzou"
)

type remoteScope struct {
	client  *ilanzou.Client
	rootID  string
	listing map[string][]ilanzou.Entry
	parents map[string]string
}

func newRemoteScope(client *ilanzou.Client, rootID string) *remoteScope {
	return &remoteScope{
		client:  client,
		rootID:  rootID,
		listing: make(map[string][]ilanzou.Entry),
		parents: make(map[string]string),
	}
}

func (scope *remoteScope) find(ctx context.Context, id string) (ilanzou.Entry, error) {
	if id == scope.rootID {
		return ilanzou.Entry{ID: id, IsDir: true}, nil
	}
	if id == "" {
		return ilanzou.Entry{}, errors.New("object ID cannot be empty")
	}
	visited := make(map[string]bool)
	folders := []string{scope.rootID}
	for len(folders) > 0 {
		folderID := folders[0]
		folders = folders[1:]
		if visited[folderID] {
			continue
		}
		visited[folderID] = true
		entries, ok := scope.listing[folderID]
		if !ok {
			var err error
			entries, err = scope.client.List(ctx, folderID)
			if err != nil {
				return ilanzou.Entry{}, err
			}
			scope.listing[folderID] = entries
		}
		for _, entry := range entries {
			scope.parents[entry.ID] = folderID
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
	return ilanzou.Entry{}, fmt.Errorf("object %q is outside configured root folder %q or does not exist", id, scope.rootID)
}

func (scope *remoteScope) requireFolder(ctx context.Context, id string) error {
	entry, err := scope.find(ctx, id)
	if err != nil {
		return err
	}
	if !entry.IsDir {
		return fmt.Errorf("object %q is not a folder", id)
	}
	return nil
}

func (scope *remoteScope) requireObject(ctx context.Context, id string, isDir bool) (ilanzou.Entry, error) {
	if id == scope.rootID {
		return ilanzou.Entry{}, errors.New("the configured root folder cannot be moved, renamed, or deleted")
	}
	entry, err := scope.find(ctx, id)
	if err != nil {
		return ilanzou.Entry{}, err
	}
	if entry.IsDir != isDir {
		if isDir {
			return ilanzou.Entry{}, fmt.Errorf("object %q is not a folder", id)
		}
		return ilanzou.Entry{}, fmt.Errorf("object %q is not a file", id)
	}
	return entry, nil
}

func (scope *remoteScope) isDescendant(folderID, possibleAncestorID string) bool {
	for id := folderID; id != ""; id = scope.parents[id] {
		if id == possibleAncestorID {
			return true
		}
		if id == scope.rootID {
			return false
		}
	}
	return false
}
