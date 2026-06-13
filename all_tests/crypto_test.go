package utils

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEncryptDecryptString(t *testing.T) {
	t.Setenv("DB_CRED_ENCRYPTION_KEY", "01234567890123456789012345678901")
	plain := "postgresql://user:pass@host:5432/db"
	enc, err := EncryptString(plain)
	require.NoError(t, err)

	dec, err := DecryptString(enc)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)
}

func TestEncryptStringMissingKey(t *testing.T) {
	os.Unsetenv("DB_CRED_ENCRYPTION_KEY")
	_, err := EncryptString("x")
	assert.ErrorIs(t, err, ErrMissingEncryptionKey)
}

func TestGeneratePasswordBase64(t *testing.T) {
	pw, err := GeneratePasswordBase64(16)
	require.NoError(t, err)
	assert.NotEmpty(t, pw)

	pwDefault, err := GeneratePasswordBase64(0)
	require.NoError(t, err)
	assert.NotEmpty(t, pwDefault)
}

func TestGetEncryptionKey_padding(t *testing.T) {
	t.Setenv("DB_CRED_ENCRYPTION_KEY", "short")
	plain := "hello-world"
	enc, err := EncryptString(plain)
	require.NoError(t, err)
	dec, err := DecryptString(enc)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)
}

func TestGetEncryptionKey_exactLength(t *testing.T) {
	t.Setenv("DB_CRED_ENCRYPTION_KEY", "0123456789abcdef0123456789abcdef")
	plain := "exact-32-bytes-key-test"
	enc, err := EncryptString(plain)
	require.NoError(t, err)
	dec, err := DecryptString(enc)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)
}

func TestGetEncryptionKey_trimming(t *testing.T) {
	longKey := "0123456789abcdef0123456789abcdef0123456789"                         // 40 bytes
	shortKey := longKey[:32]                                                         // first 32 bytes

	t.Setenv("DB_CRED_ENCRYPTION_KEY", longKey)
	plain := "long-key-test"
	encWithLong, err := EncryptString(plain)
	require.NoError(t, err)

	t.Setenv("DB_CRED_ENCRYPTION_KEY", shortKey)
	dec, err := DecryptString(encWithLong)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)

	encWithShort, err := EncryptString(plain)
	require.NoError(t, err)

	t.Setenv("DB_CRED_ENCRYPTION_KEY", longKey)
	dec, err = DecryptString(encWithShort)
	require.NoError(t, err)
	assert.Equal(t, plain, dec)
}

func TestValidatePassword(t *testing.T) {
	tests := []struct {
		name    string
		pass    string
		wantErr bool
	}{
		{"valid", "SecurePass123!", false},
		{"empty", "", true},
		{"too short", "Short1!", true},
		{"no upper", "securepass123!", true},
		{"no lower", "SECUREPASS123!", true},
		{"no digit", "SecurePass!!!!", true},
		{"no special", "SecurePass1234", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidatePassword(tt.pass)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}
