package core

import (
	"fmt"
	"os"
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
	plain, err := bootstrapPassword()
	if err != nil {
		return "", err
	}
	if err := a.Auth.SetAdminPassword(plain); err != nil {
		return "", err
	}
	return plain, nil
}

func bootstrapPassword() (string, error) {
	plain := strings.TrimSpace(os.Getenv("PARTICEPS_ADMIN_PASSWORD"))
	if plain == "" {
		plain = auth.NewTokenPlain()[:20]
	}
	if plain == "" || len(plain) > 72 || strings.ContainsAny(plain, "\r\n\x00") {
		return "", fmt.Errorf("administrator password is invalid")
	}
	return plain, nil
}
