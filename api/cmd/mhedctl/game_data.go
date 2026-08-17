package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type gameManifest struct {
	Format   string `json:"format"`
	Database struct {
		File   string `json:"file"`
		SHA256 string `json:"sha256"`
		Bytes  int64  `json:"bytes"`
	} `json:"database"`
}

func copyVerifiedGameData(source, manifestPath, destination string) error {
	source = normalizePath(source)
	manifestPath = normalizePath(manifestPath)
	destination = normalizePath(destination)
	manifestBytes, err := os.ReadFile(manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	var manifest gameManifest
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if manifest.Format != "mh3g-save-editor-data-manifest-v1" || manifest.Database.File != "mh3g.sqlite" {
		return errInvalidManifest
	}
	input, err := os.Open(source)
	if err != nil {
		return fmt.Errorf("open game database: %w", err)
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if info.Size() != manifest.Database.Bytes {
		return errInvalidManifest
	}
	hash := sha256.New()
	if _, err := io.Copy(hash, input); err != nil {
		return err
	}
	if hex.EncodeToString(hash.Sum(nil)) != manifest.Database.SHA256 {
		return errInvalidManifest
	}
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := os.MkdirAll(destination, 0o750); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(destination, ".mh3g-*.sqlite")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if _, err = io.Copy(temporary, input); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	if err := os.Chmod(temporaryName, 0o640); err != nil {
		return err
	}
	if err := os.Rename(temporaryName, filepath.Join(destination, "mh3g.sqlite")); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(destination, "manifest.json"), manifestBytes, 0o640); err != nil {
		return err
	}
	return nil
}
