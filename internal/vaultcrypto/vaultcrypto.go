// Package vaultcrypto implements Ansible Vault's AES256 cipher (format
// version 1.1) natively, rather than shelling out to ansible-vault. The
// format itself is small, stable, and well documented - unlike Ansible's
// playbook execution engine, which this project deliberately avoids
// reimplementing (see Purpose.md), the vault cipher is tractable to
// reimplement directly and avoids spawning a subprocess (and re-piping a
// password) per secret. See design-docs/Vault.md point 7.
package vaultcrypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

// Header identifies an unlabeled AES256 vault (format 1.1) - the only
// variant v1 supports. Labeled multi-vault-id headers
// ($ANSIBLE_VAULT;1.2;AES256;<label>) are deferred to v2 per
// design-docs/Vault.md's "Deferred to v2" section.
const Header = "$ANSIBLE_VAULT;1.1;AES256"

const (
	keyLength     = 32    // AES-256 key size
	ivLength      = 16    // AES block size, used as the CTR IV
	pbkdf2Iters   = 10000 // matches Ansible's own VaultAES256 key derivation
	derivedLength = 2*keyLength + ivLength
	saltLength    = 32
	wrapWidth     = 80 // cosmetic parity with real ansible-vault output; Decrypt never depends on any particular wrap width
)

// ErrHMACMismatch means the supplied password is wrong, or the ciphertext
// has been corrupted - kept distinct from other decode errors so a caller
// can show a specific "wrong password" message instead of a generic one.
var ErrHMACMismatch = errors.New("vaultcrypto: HMAC verification failed (wrong password or corrupted data)")

// Encrypt encrypts plaintext under password, producing a full
// "$ANSIBLE_VAULT;1.1;AES256" block: header line followed by a
// line-wrapped hex body, matching what a `!vault |` YAML block scalar
// holds. A fresh random salt is generated on every call - by design, since
// that's what makes two encryptions of the same plaintext produce
// different ciphertext (design-docs/Vault.md point 1, "the salt problem" -
// the reason the diff logic elsewhere must never blindly re-encrypt an
// unchanged value).
func Encrypt(plaintext, password string) (string, error) {
	salt := make([]byte, saltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("vaultcrypto: generating salt: %w", err)
	}

	aesKey, hmacKey, iv, err := deriveKeys(password, salt)
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("vaultcrypto: %w", err)
	}
	padded := pkcs7Pad([]byte(plaintext), aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCTR(block, iv).XORKeyStream(ciphertext, padded)

	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(ciphertext)
	hmacHex := hex.EncodeToString(mac.Sum(nil))

	body := hex.EncodeToString(salt) + "\n" + hmacHex + "\n" + hex.EncodeToString(ciphertext)
	outerHex := hex.EncodeToString([]byte(body))

	return Header + "\n" + wrapLines(outerHex, wrapWidth), nil
}

// Decrypt decrypts vaultString - a full "$ANSIBLE_VAULT;..." block, header
// and body together, any whitespace/line-wrapping tolerated - under
// password. Returns ErrHMACMismatch specifically when the password is
// wrong or the data is corrupted.
func Decrypt(vaultString, password string) (string, error) {
	header, body, err := splitHeader(vaultString)
	if err != nil {
		return "", err
	}
	if header != Header {
		return "", fmt.Errorf("vaultcrypto: unsupported vault header %q (only unlabeled 1.1/AES256 is supported)", header)
	}

	outer, err := hex.DecodeString(stripWhitespace(body))
	if err != nil {
		return "", fmt.Errorf("vaultcrypto: malformed vault body: %w", err)
	}
	parts := strings.SplitN(string(outer), "\n", 3)
	if len(parts) != 3 {
		return "", errors.New("vaultcrypto: malformed vault body: expected salt/hmac/ciphertext")
	}
	salt, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", fmt.Errorf("vaultcrypto: malformed salt: %w", err)
	}
	wantHMACHex := parts[1]
	ciphertext, err := hex.DecodeString(parts[2])
	if err != nil {
		return "", fmt.Errorf("vaultcrypto: malformed ciphertext: %w", err)
	}

	aesKey, hmacKey, iv, err := deriveKeys(password, salt)
	if err != nil {
		return "", err
	}

	mac := hmac.New(sha256.New, hmacKey)
	mac.Write(ciphertext)
	gotHMACHex := hex.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(gotHMACHex), []byte(wantHMACHex)) {
		return "", ErrHMACMismatch
	}

	block, err := aes.NewCipher(aesKey)
	if err != nil {
		return "", fmt.Errorf("vaultcrypto: %w", err)
	}
	padded := make([]byte, len(ciphertext))
	cipher.NewCTR(block, iv).XORKeyStream(padded, ciphertext)

	plaintext, err := pkcs7Unpad(padded, aes.BlockSize)
	if err != nil {
		return "", err
	}
	return string(plaintext), nil
}

// deriveKeys implements Ansible's own PBKDF2HMAC-SHA256 key derivation:
// one 80-byte block derived from (password, salt), split into a 32-byte
// AES key, a 32-byte HMAC key, and a 16-byte CTR IV, in that order.
func deriveKeys(password string, salt []byte) (aesKey, hmacKey, iv []byte, err error) {
	derived, err := pbkdf2.Key(sha256.New, password, salt, pbkdf2Iters, derivedLength)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("vaultcrypto: deriving keys: %w", err)
	}
	return derived[:keyLength], derived[keyLength : 2*keyLength], derived[2*keyLength : derivedLength], nil
}

// pkcs7Pad pads b to a multiple of blockSize per PKCS#7 (RFC 5652 §6.3).
// Mandatory here even though CTR mode has no block-alignment requirement
// of its own: real ansible-vault always unpads on decrypt, so omitting
// this breaks interop with the real tool even though unpadded output
// "looks" correct in isolation - see the package doc comment.
func pkcs7Pad(b []byte, blockSize int) []byte {
	padLen := blockSize - len(b)%blockSize
	padded := make([]byte, len(b)+padLen)
	copy(padded, b)
	for i := len(b); i < len(padded); i++ {
		padded[i] = byte(padLen)
	}
	return padded
}

func pkcs7Unpad(b []byte, blockSize int) ([]byte, error) {
	if len(b) == 0 || len(b)%blockSize != 0 {
		return nil, errors.New("vaultcrypto: invalid padded plaintext length")
	}
	padLen := int(b[len(b)-1])
	if padLen == 0 || padLen > blockSize || padLen > len(b) {
		return nil, errors.New("vaultcrypto: invalid PKCS7 padding")
	}
	for _, c := range b[len(b)-padLen:] {
		if int(c) != padLen {
			return nil, errors.New("vaultcrypto: invalid PKCS7 padding")
		}
	}
	return b[:len(b)-padLen], nil
}

func splitHeader(vaultString string) (header, rest string, err error) {
	lines := strings.SplitN(strings.TrimSpace(vaultString), "\n", 2)
	if len(lines) != 2 {
		return "", "", errors.New("vaultcrypto: vault text has no body")
	}
	return strings.TrimSpace(lines[0]), lines[1], nil
}

func stripWhitespace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r':
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func wrapLines(s string, width int) string {
	var b strings.Builder
	for i := 0; i < len(s); i += width {
		end := i + width
		if end > len(s) {
			end = len(s)
		}
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(s[i:end])
	}
	return b.String()
}
