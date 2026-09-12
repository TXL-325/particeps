package core

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"particeps/internal/auth"
	"particeps/internal/store"
)

var ErrTokenNotFound = errors.New("token not found")
var ErrInvalidToken = errors.New("invalid token request")

type Token struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	CreatedAt int64  `json:"createdAt"`
	Revoked   bool   `json:"revoked"`
}

func (a *App) TokenCreate(name, role string) (id, plain string, err error) {
	if role != "read" && role != "manage" {
		return "", "", fmt.Errorf("%w: role must be read or manage", ErrInvalidToken)
	}
	name = strings.TrimSpace(name)
	if name == "" || utf8.RuneCountInString(name) > 100 {
		return "", "", fmt.Errorf("%w: name must contain 1 to 100 characters", ErrInvalidToken)
	}
	plain, id = auth.NewTokenPlain(), store.NewID()
	_, err = a.Store.DB.Exec(`INSERT INTO tokens(id,name,hash,role,created_at) VALUES(?,?,?,?,?)`, id, name, auth.HashToken(plain), role, store.Now())
	return id, plain, err
}

func (a *App) TokenRevoke(id string) error {
	result, err := a.Store.DB.Exec(`UPDATE tokens SET revoked=1 WHERE id=?`, id)
	if err != nil {
		return err
	}
	count, err := result.RowsAffected()
	if err != nil {
		return err
	}
	if count == 0 {
		return ErrTokenNotFound
	}
	return nil
}

func (a *App) Tokens() ([]Token, error) {
	rows, err := a.Store.DB.Query(`SELECT id,name,role,created_at,revoked FROM tokens ORDER BY created_at DESC,id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []Token{}
	for rows.Next() {
		var token Token
		if err := rows.Scan(&token.ID, &token.Name, &token.Role, &token.CreatedAt, &token.Revoked); err != nil {
			return nil, err
		}
		result = append(result, token)
	}
	return result, rows.Err()
}
