// Saving passkeys, encrypted on disk.
package main

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
)

// ---- storage -------------------------------------------------------------

type passkey struct {
	ID          string `json:"id"` // credential id, base64url
	RPID        string `json:"rp_id"`
	UserHandle  string `json:"user_handle"` // base64url
	UserName    string `json:"user_name"`
	DisplayName string `json:"display_name"`
	Key         string `json:"key"` // base64url(nonce || AES-GCM(PKCS#8 private key))
	Created     int64  `json:"created"`
}

type passkeyStore struct {
	mu   sync.Mutex
	path string
	list []passkey
}

func passkeys() *passkeyStore {
	s := &passkeyStore{path: filepath.Join(dataDir(), "passkeys.json")}
	if b, err := os.ReadFile(s.path); err == nil {
		json.Unmarshal(b, &s.list)
	}
	return s
}

func (s *passkeyStore) save() error {
	b, _ := json.MarshalIndent(s.list, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

// masterKey is the key passkeys are encrypted with: in the system keyring if
// there is one, otherwise a 0600 file next to them.
func masterKey() ([]byte, error) {
	if _, err := exec.LookPath("secret-tool"); err == nil {
		out, err := exec.Command("secret-tool", "lookup", "application", appName, "kind", "passkeys").Output()
		if err == nil && len(bytes.TrimSpace(out)) > 0 {
			if k, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out))); err == nil && len(k) == 32 {
				return k, nil
			}
		}
		k := make([]byte, 32)
		rand.Read(k)
		cmd := exec.Command("secret-tool", "store", "--label=Fasty passkeys", "application", appName, "kind", "passkeys")
		cmd.Stdin = strings.NewReader(base64.StdEncoding.EncodeToString(k))
		if err := cmd.Run(); err == nil {
			return k, nil
		}
		log.Printf("fasty: passkeys: the keyring wouldn't take the key, using a file")
	}
	p := filepath.Join(dataDir(), "passkeys.key")
	if b, err := os.ReadFile(p); err == nil && len(b) == 32 {
		return b, nil
	}
	k := make([]byte, 32)
	rand.Read(k)
	return k, os.WriteFile(p, k, 0o600)
}

func sealKey(master, id, plain []byte) (string, error) {
	blk, err := aes.NewCipher(master)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	rand.Read(nonce)
	return b64.EncodeToString(gcm.Seal(nonce, nonce, plain, id)), nil
}

func openKey(master, id []byte, sealed string) ([]byte, error) {
	raw, err := b64.DecodeString(sealed)
	if err != nil {
		return nil, err
	}
	blk, err := aes.NewCipher(master)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("short")
	}
	return gcm.Open(nil, raw[:gcm.NonceSize()], raw[gcm.NonceSize():], id)
}
