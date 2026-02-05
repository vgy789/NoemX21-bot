package crypto

import (
	"encoding/hex"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCrypter(t *testing.T) {
	validKey := hex.EncodeToString(make([]byte, 32))

	t.Run("valid key", func(t *testing.T) {
		c, err := NewCrypter(validKey)
		require.NoError(t, err)
		assert.NotNil(t, c)
	})

	t.Run("invalid hex", func(t *testing.T) {
		_, err := NewCrypter("zzzz")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode")
	})

	t.Run("wrong key length", func(t *testing.T) {
		_, err := NewCrypter(hex.EncodeToString(make([]byte, 16)))
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "32")
	})
}

func TestCrypter_EncryptDecrypt(t *testing.T) {
	key := make([]byte, 32)
	for i := range key {
		key[i] = byte(i)
	}
	c, err := NewCrypter(hex.EncodeToString(key))
	require.NoError(t, err)

	plaintext := []byte("secret data")
	aad := []byte("login")

	ciphertext, nonce, err := c.Encrypt(plaintext, aad)
	require.NoError(t, err)
	require.Len(t, nonce, 12)
	require.NotEmpty(t, ciphertext)

	decrypted, err := c.Decrypt(ciphertext, nonce, aad)
	require.NoError(t, err)
	assert.Equal(t, plaintext, decrypted)
}

func TestCrypter_Decrypt_wrongAAD(t *testing.T) {
	key := make([]byte, 32)
	c, err := NewCrypter(hex.EncodeToString(key))
	require.NoError(t, err)

	ciphertext, nonce, err := c.Encrypt([]byte("x"), []byte("aad1"))
	require.NoError(t, err)

	_, err = c.Decrypt(ciphertext, nonce, []byte("aad2"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "decryption")
}

func TestCrypter_Decrypt_invalidNonceLength(t *testing.T) {
	c, err := NewCrypter(hex.EncodeToString(make([]byte, 32)))
	require.NoError(t, err)

	_, err = c.Decrypt([]byte("x"), make([]byte, 8), nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "nonce")
}
