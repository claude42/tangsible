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

// Header identifies an unlabeled AES256 vault (format 1.1). A value
// encrypted under a vault id carries the label in its header instead:
// "$ANSIBLE_VAULT;1.2;AES256;<label>" (see Label, HeaderFor). The body is
// the same in both formats.
const Header = "$ANSIBLE_VAULT;1.1;AES256"

// DefaultID is the label Ansible gives a password supplied without one
// (--vault-id path, --vault-password-file). A value encrypted under it is
// written in the unlabeled 1.1 format, as ansible-vault does.
const DefaultID = "default"

// Secret is one vault password together with the vault id (label) it was
// given.
type Secret struct {
	ID       string
	Password string
}

// ErrNoPassword means no password was supplied at all.
var ErrNoPassword = errors.New("vaultcrypto: no vault password available")

// HeaderFor is the header line of a value encrypted under label: the
// unlabeled 1.1 header for "" and DefaultID, "1.2" with the label otherwise.
func HeaderFor(label string) string {
	if label == "" || label == DefaultID {
		return Header
	}
	return "$ANSIBLE_VAULT;1.2;AES256;" + label
}

// Label returns the vault id recorded in vaultString's header: "" for the
// unlabeled 1.1 format (or a 1.2 header with an empty label). The
// supported formats are 1.1 and 1.2 with AES256.
func Label(vaultString string) (string, error) {
	header, _, err := splitHeader(vaultString)
	if err != nil {
		return "", err
	}
	return parseHeader(header)
}

func parseHeader(header string) (label string, err error) {
	parts := strings.Split(header, ";")
	if len(parts) < 3 || parts[0] != "$ANSIBLE_VAULT" || parts[2] != "AES256" {
		return "", fmt.Errorf("vaultcrypto: unsupported vault header %q (only AES256, format 1.1 or 1.2, is supported)", header)
	}
	switch {
	case parts[1] == "1.1" && len(parts) == 3:
		return "", nil
	case parts[1] == "1.2" && len(parts) <= 4:
		if len(parts) == 4 {
			return parts[3], nil
		}
		return "", nil
	}
	return "", fmt.Errorf("vaultcrypto: unsupported vault header %q (only AES256, format 1.1 or 1.2, is supported)", header)
}

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
	return EncryptLabeled(plaintext, password, "")
}

// EncryptLabeled is Encrypt under a vault id: the label goes into the
// header (HeaderFor), "" and DefaultID give the unlabeled format.
func EncryptLabeled(plaintext, password, label string) (string, error) {
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

	return HeaderFor(label) + "\n" + wrapLines(outerHex, wrapWidth), nil
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
	if _, err := parseHeader(header); err != nil {
		return "", err
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

// DecryptWith decrypts vaultString with the first of secrets that works,
// the way Ansible does: the secrets whose id equals the label in the value's
// header go first, then all the others in the order given (the label is a
// hint, not a check - any password that decrypts the value will do). The
// secret that worked is returned. When none does the error is
// ErrHMACMismatch; a value that is malformed is reported as such at once.
func DecryptWith(vaultString string, secrets []Secret) (plaintext string, used Secret, err error) {
	if len(secrets) == 0 {
		return "", Secret{}, ErrNoPassword
	}
	label, err := Label(vaultString)
	if err != nil {
		return "", Secret{}, err
	}
	ordered := make([]Secret, 0, len(secrets))
	if label != "" {
		for _, s := range secrets {
			if s.ID == label {
				ordered = append(ordered, s)
			}
		}
	}
	for _, s := range secrets {
		if label == "" || s.ID != label {
			ordered = append(ordered, s)
		}
	}
	for _, s := range ordered {
		plaintext, err := Decrypt(vaultString, s.Password)
		if err == nil {
			return plaintext, s, nil
		}
		if !errors.Is(err, ErrHMACMismatch) {
			return "", Secret{}, err
		}
	}
	return "", Secret{}, ErrHMACMismatch
}
