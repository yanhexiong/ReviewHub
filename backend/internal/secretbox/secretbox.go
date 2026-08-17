package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"io"
	"strings"
)

// The Node application stores secrets as base64url(iv).base64url(tag).base64url(ciphertext)
// using SHA-256(encryption-key) as the AES-256-GCM key. Keep this format stable while
// the API is being migrated so existing credentials remain readable.
func Encrypt(key, value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", nil
	}
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	iv := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nil, iv, []byte(value), nil)
	tagSize := gcm.Overhead()
	tag := sealed[len(sealed)-tagSize:]
	ciphertext := sealed[:len(sealed)-tagSize]
	encode := base64.RawURLEncoding.EncodeToString
	return strings.Join([]string{encode(iv), encode(tag), encode(ciphertext)}, "."), nil
}

func Decrypt(key, encoded string) (string, error) {
	parts := strings.Split(encoded, ".")
	if len(parts) != 3 {
		return "", errors.New("invalid encrypted secret")
	}
	decode := base64.RawURLEncoding.DecodeString
	iv, err := decode(parts[0])
	if err != nil {
		return "", err
	}
	tag, err := decode(parts[1])
	if err != nil {
		return "", err
	}
	ciphertext, err := decode(parts[2])
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(digest[:])
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(iv) != gcm.NonceSize() || len(tag) != gcm.Overhead() {
		return "", errors.New("invalid encrypted secret dimensions")
	}
	sealed := append(append([]byte(nil), ciphertext...), tag...)
	plaintext, err := gcm.Open(nil, iv, sealed, nil)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}
