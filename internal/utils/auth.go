package utils

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters – tuned for server-side use. You can adjust these if needed.
const (
	argonTime    uint32 = 1        // Number of iterations
	argonMemory  uint32 = 64 * 1024 // Memory in KiB (64 MiB)
	argonThreads uint8  = 4         // Number of threads
	argonKeyLen  uint32 = 32        // Length of the derived key
)

// Hash generates an Argon2id hash for the given password and returns it as an encoded string ([]byte).
// The format is: argon2id$v=19$m=...,t=...,p=...$<salt_b64>$<hash_b64>
func Hash(password string) ([]byte, error) {
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encoded := fmt.Sprintf("argon2id$v=19$m=%d,t=%d,p=%d$%s$%s",
		argonMemory, argonTime, argonThreads, b64Salt, b64Hash)

	return []byte(encoded), nil
}

// VerifyPassword compares a password with an Argon2id encoded hash.
func VerifyPassword(encodedHash, password string) error {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 5 {
		return errors.New("invalid hash format")
	}

	// parts[0] = "argon2id"
	// parts[1] = "v=19"
	// parts[2] = "m=...,t=...,p=..."
	// parts[3] = salt
	// parts[4] = hash

	paramPart := parts[2]
	var memory uint32
	var time uint32
	var threads uint8

	_, err := fmt.Sscanf(paramPart, "m=%d,t=%d,p=%d", &memory, &time, &threads)
	if err != nil {
		return errors.New("invalid hash parameters")
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[3])
	if err != nil {
		return errors.New("invalid salt encoding")
	}

	hash, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return errors.New("invalid hash encoding")
	}

	calculated := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(hash)))

	if subtle.ConstantTimeCompare(hash, calculated) == 1 {
		return nil
	}

	return errors.New("invalid password")
}

func GenerateStateOauthCookie() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	state := base64.URLEncoding.EncodeToString(b);
	return state, nil
}