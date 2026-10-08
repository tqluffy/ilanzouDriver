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
	if len(args) == 0 || args[0] == "help" || args[0] == "-h" || args[0] == "--help" {
		usage()
		return nil
	}
	username, password := os.Getenv("ILANZOU_USERNAME"), os.Getenv("ILANZOU_PASSWORD")
	if username == "" || password == "" {
		return errors.New("set ILANZOU_USERNAME and ILANZOU_PASSWORD in the environment")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	client := ilanzou.NewClient(username, password)
	if err := client.Init(ctx); err != nil {
		return err
	}

	command, args := args[0], args[1:]
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
	fmt.Print(`iLanZou minimal client

Credentials are read from ILANZOU_USERNAME and ILANZOU_PASSWORD.

Commands:
  ls [folder-id]                         list a folder (default root: 0)
  mkdir <parent-folder-id> <name>        create a folder
  upload <folder-id> <local-file>        upload a file
  download <file-id> <local-file>        download a file
  move <file|dir> <id> <target-folder>   move a file or folder
  rename <file|dir> <id> <new-name>      rename a file or folder
  delete <file|dir> <id>                 permanently delete a file or folder
`)
}
