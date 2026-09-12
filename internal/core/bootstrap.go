package core

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"particeps/internal/auth"
)

func (a *App) BootstrapAdmin() (string, error) {
	a.bootstrapMu.Lock()
	defer a.bootstrapMu.Unlock()
	exists, err := a.Auth.HasAdmin()
	if err != nil {
		return "", fmt.Errorf("check administrator: %w", err)
	}
	if exists {
		return "", nil
	}
	path := a.Cfg.Bootstrap()
	plain, err := bootstrapPassword(path)
	if err != nil {
		return "", err
	}
	// A crash or database failure after delivery is recoverable: the next
	// initialization reuses the completed file instead of losing the password.
	if err := a.Auth.SetAdminPassword(plain); err != nil {
		return "", err
	}
	log.Printf("admin password written to %s", path)
	return plain, nil
}

func bootstrapPassword(path string) (string, error) {
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("administrator bootstrap path must be a regular file")
		}
		if err := os.Chmod(path, 0600); err != nil {
			return "", err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return "", err
		}
		plain := strings.TrimSuffix(string(data), "\n")
		if plain == "" || len(plain) > 72 || strings.ContainsAny(plain, "\r\n\x00") {
			return "", fmt.Errorf("administrator bootstrap file is invalid")
		}
		return plain, nil
	} else if !os.IsNotExist(err) {
		return "", err
	}
	plain := auth.NewTokenPlain()[:20]
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	_, writeErr := file.WriteString(plain + "\n")
	if writeErr == nil {
		writeErr = file.Sync()
	}
	closeErr := file.Close()
	if writeErr == nil {
		writeErr = closeErr
	}
	if writeErr != nil {
		_ = os.Remove(path)
		return "", writeErr
	}
	// Persist the directory entry before making the database record durable.
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return "", err
	}
	err = directory.Sync()
	closeErr = directory.Close()
	if err != nil {
		return "", err
	}
	if closeErr != nil {
		return "", closeErr
	}
	return plain, nil
}
