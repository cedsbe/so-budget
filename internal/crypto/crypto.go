// Package crypto holds the primitives for per-user encryption at rest.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base32"
	"encoding/hex"
	"errors"
	"io"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Key [32]byte

var ErrDecrypt = errors.New("crypto: decryption failed")

type KDFParams struct {
	Time    uint32
	Memory  uint32 // KiB
	Threads uint8
}

var (
	DefaultParams = KDFParams{Time: 3, Memory: 64 * 1024, Threads: 4}
	TestParams    = KDFParams{Time: 1, Memory: 8 * 1024, Threads: 1}
)

func NewKey() (Key, error) {
	var k Key
	_, err := io.ReadFull(rand.Reader, k[:])
	return k, err
}

func NewSalt() ([]byte, error) {
	s := make([]byte, 16)
	_, err := io.ReadFull(rand.Reader, s)
	return s, err
}

func RandomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// Derive turns a password or recovery code into a key with Argon2id.
func Derive(secret string, salt []byte, p KDFParams) Key {
	var k Key
	copy(k[:], argon2.IDKey([]byte(secret), salt, p.Time, p.Memory, p.Threads, 32))
	return k
}

func gcm(key Key) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal returns nonce || ciphertext.
func Seal(key Key, aad, plaintext []byte) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, g.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return g.Seal(nonce, nonce, plaintext, aad), nil
}

func Open(key Key, aad, box []byte) ([]byte, error) {
	g, err := gcm(key)
	if err != nil {
		return nil, err
	}
	if len(box) < g.NonceSize() {
		return nil, ErrDecrypt
	}
	pt, err := g.Open(nil, box[:g.NonceSize()], box[g.NonceSize():], aad)
	if err != nil {
		return nil, ErrDecrypt
	}
	return pt, nil
}

func Wrap(dek, kek Key) ([]byte, error) { return Seal(kek, []byte("wrap"), dek[:]) }

func Unwrap(box []byte, kek Key) (Key, error) {
	pt, err := Open(kek, []byte("wrap"), box)
	if err != nil {
		return Key{}, err
	}
	if len(pt) != 32 {
		return Key{}, ErrDecrypt
	}
	var k Key
	copy(k[:], pt)
	return k, nil
}

var b32 = base32.StdEncoding.WithPadding(base32.NoPadding)

// NewRecoveryCode returns 160 random bits as 8 groups of 4 base32 characters.
func NewRecoveryCode() (string, error) {
	b := make([]byte, 20)
	if _, err := io.ReadFull(rand.Reader, b); err != nil {
		return "", err
	}
	s := b32.EncodeToString(b) // 32 chars
	parts := make([]string, 0, 8)
	for i := 0; i < len(s); i += 4 {
		parts = append(parts, s[i:i+4])
	}
	return strings.Join(parts, "-"), nil
}

func NormalizeRecoveryCode(s string) string {
	s = strings.ToUpper(strings.TrimSpace(s))
	s = strings.ReplaceAll(s, "-", "")
	return strings.ReplaceAll(s, " ", "")
}

// HashID derives a per-user secret from the data key and HMACs the id with it and the server pepper.
func HashID(key Key, pepper []byte, id string) string {
	sub := hmac.New(sha256.New, key[:])
	sub.Write([]byte("id-hash"))
	mac := hmac.New(sha256.New, append(sub.Sum(nil), pepper...))
	mac.Write([]byte(id))
	return hex.EncodeToString(mac.Sum(nil))
}
