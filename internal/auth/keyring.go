package auth

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const keyringService = "buttons-cli"

type KeyringStore struct{}

func keyringAccount(registryURL string) string {
	hash := sha256.Sum256([]byte(registryURL))
	return "oauth:" + base64.RawURLEncoding.EncodeToString(hash[:])
}

func (KeyringStore) Load(registryURL string) (Credentials, error) {
	encoded, err := keyring.Get(keyringService, keyringAccount(registryURL))
	if errors.Is(err, keyring.ErrNotFound) {
		return Credentials{}, ErrCredentialsNotFound
	}
	if err != nil {
		return Credentials{}, err
	}
	var credential Credentials
	if err := json.Unmarshal([]byte(encoded), &credential); err != nil {
		return Credentials{}, fmt.Errorf("decode keychain credential: %w", err)
	}
	return credential, nil
}

func (KeyringStore) Save(registryURL string, credential Credentials) error {
	encoded, err := json.Marshal(credential)
	if err != nil {
		return err
	}
	return keyring.Set(keyringService, keyringAccount(registryURL), string(encoded))
}

func (KeyringStore) Delete(registryURL string) error {
	err := keyring.Delete(keyringService, keyringAccount(registryURL))
	if errors.Is(err, keyring.ErrNotFound) {
		return nil
	}
	return err
}
