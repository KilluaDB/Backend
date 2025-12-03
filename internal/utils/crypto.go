package utils

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
	"os"
)

var ErrMissingEncryptionKey = errors.New("DB_CRED_ENCRYPTION_KEY environment variable is required for encrypting database credentials")

// getEncryptionKey returns a 32-byte key derived from the DB_CRED_ENCRYPTION_KEY env var.
// The key must be at least 32 bytes long.
func getEncryptionKey() ([]byte, error) {
	secret := os.Getenv("DB_CRED_ENCRYPTION_KEY")
	if secret == "" {
		return nil, ErrMissingEncryptionKey
	}

	key := []byte(secret)
	if len(key) < 32 {
		// Pad or trim to 32 bytes
		padded := make([]byte, 32)
		copy(padded, key)
		key = padded
	} else if len(key) > 32 {
		key = key[:32]
	}

	return key, nil
}

// EncryptString encrypts the given plaintext string using AES-GCM and returns a base64 string.
func EncryptString(plaintext string) (string, error) {
	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}

	ciphertext := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

// DecryptString decrypts a base64-encoded AES-GCM ciphertext string.
func DecryptString(ciphertextB64 string) (string, error) {
	key, err := getEncryptionKey()
	if err != nil {
		return "", err
	}

	data, err := base64.StdEncoding.DecodeString(ciphertextB64)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}

	if len(data) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}

	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}

	return string(plaintext), nil
}


