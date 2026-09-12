package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type sealedCredential struct {
	Nonce      []byte `json:"nonce"`
	Ciphertext []byte `json:"ciphertext"`
	ExpiresAt  int64  `json:"expiresAt"`
}

type taskResult struct {
	Credential *sealedCredential `json:"credential,omitempty"`
}

type InitialCredential struct {
	Name       string `json:"name"`
	InstanceID string `json:"instanceId"`
	Password   string `json:"password"`
}

func (a *App) credentialCipher(create bool) (cipher.AEAD, error) {
	a.credentialMu.Lock()
	defer a.credentialMu.Unlock()
	path := filepath.Join(a.Cfg.DataDir, "credential.key")
	key, err := os.ReadFile(path)
	if os.IsNotExist(err) && create {
		key = make([]byte, 32)
		if _, err = rand.Read(key); err != nil {
			return nil, err
		}
		file, openErr := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if os.IsExist(openErr) {
			key, err = os.ReadFile(path)
		} else if openErr != nil {
			return nil, openErr
		} else {
			_, err = file.Write(key)
			closeErr := file.Close()
			if err == nil {
				err = closeErr
			}
		}
	}
	if err != nil {
		return nil, fmt.Errorf("initial credential key is unavailable")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("initial credential key is invalid")
	}
	return cipher.NewGCM(block)
}

func (a *App) sealCredential(taskID, instanceID, password string) (string, error) {
	aead, err := a.credentialCipher(true)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	credential := &sealedCredential{
		Nonce:      nonce,
		Ciphertext: aead.Seal(nil, nonce, []byte(password), []byte(taskID+"\x00"+instanceID)),
		ExpiresAt:  time.Now().Add(15 * time.Minute).Unix(),
	}
	data, err := json.Marshal(taskResult{Credential: credential})
	return string(data), err
}

// ClaimInitialCredentials consumes encrypted credentials once. Ordinary task
// reads never decrypt them; a failed delivery can be recovered by password reset.
func (a *App) ClaimInitialCredentials(taskID string) ([]InitialCredential, error) {
	if err := a.Store.ClearExpiredCredentials(taskID, time.Now()); err != nil {
		return nil, err
	}
	tx, err := a.Store.DB.Begin()
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var exists string
	if err := tx.QueryRow(`SELECT id FROM tasks WHERE id=?`, taskID).Scan(&exists); err != nil {
		return nil, err
	}
	rows, err := tx.Query(`SELECT id,name,COALESCE(instance_id,''),result_json FROM task_items WHERE task_id=? AND status='ok' AND result_json!=''`, taskID)
	if err != nil {
		return nil, err
	}
	type item struct {
		id                    int
		name, instanceID, raw string
	}
	items := []item{}
	for rows.Next() {
		var entry item
		if err := rows.Scan(&entry.id, &entry.name, &entry.instanceID, &entry.raw); err != nil {
			rows.Close()
			return nil, err
		}
		items = append(items, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	result := []InitialCredential{}
	for _, entry := range items {
		var stored taskResult
		if err := json.Unmarshal([]byte(entry.raw), &stored); err != nil {
			return nil, fmt.Errorf("initial credential record is invalid")
		}
		credential := stored.Credential
		if credential != nil && credential.ExpiresAt > time.Now().Unix() {
			aead, err := a.credentialCipher(false)
			if err != nil {
				return nil, err
			}
			if len(credential.Nonce) != aead.NonceSize() {
				return nil, fmt.Errorf("initial credential record is invalid")
			}
			plain, err := aead.Open(nil, credential.Nonce, credential.Ciphertext, []byte(taskID+"\x00"+entry.instanceID))
			if err != nil {
				return nil, fmt.Errorf("initial credential record failed authentication")
			}
			result = append(result, InitialCredential{Name: entry.name, InstanceID: entry.instanceID, Password: string(plain)})
		}
		changed, err := tx.Exec(`UPDATE task_items SET result_json='' WHERE id=? AND result_json=?`, entry.id, entry.raw)
		if err != nil {
			return nil, err
		}
		count, err := changed.RowsAffected()
		if err != nil || count != 1 {
			return nil, fmt.Errorf("initial credential was already claimed")
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return result, nil
}
