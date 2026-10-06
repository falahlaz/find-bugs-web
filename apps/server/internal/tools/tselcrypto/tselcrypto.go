// Package tselcrypto reimplements the handful of module-common-function
// (helper/encryption.js) ciphers the MyTelkomsel tools need. Every function
// is pinned byte-for-byte to the Node implementation by testdata fixtures.
package tselcrypto

import (
	"bytes"
	"compress/gzip"
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"golang.org/x/crypto/scrypt"
)

// Derivation is how a v1 package ID key is derived from its password.
type Derivation string

// The two incompatible v1 derivations found in the wild: Node's removed
// crypto.createCipher (EVP_BytesToKey) on Node <= 21, and the scrypt patch
// module-common-function installs on Node >= 22.
const (
	Legacy Derivation = "legacy"
	Scrypt Derivation = "scrypt"
)

// ErrBadDecrypt mirrors OpenSSL's "bad decrypt": the padding didn't check
// out, which almost always means a wrong key.
var ErrBadDecrypt = errors.New("bad decrypt")

// evpBytesToKey is OpenSSL's EVP_BytesToKey (MD5, no salt, one iteration).
func evpBytesToKey(password string, keyLen, ivLen int) (key, iv []byte) {
	var block, derived []byte
	for len(derived) < keyLen+ivLen {
		h := md5.Sum(append(block, password...))
		block = h[:]
		derived = append(derived, block...)
	}
	return derived[:keyLen], derived[keyLen : keyLen+ivLen]
}

func packageKey(password string, d Derivation) (key, iv []byte, err error) {
	if d == Scrypt {
		// Node's scryptSync defaults: N=16384, r=8, p=1.
		key, err = scrypt.Key([]byte(password), []byte("salt"), 16384, 8, 1, 32)
		return key, make([]byte, aes.BlockSize), err
	}
	key, iv = evpBytesToKey(password, 32, aes.BlockSize)
	return key, iv, nil
}

func pad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(b, bytes.Repeat([]byte{byte(n)}, n)...)
}

func unpad(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, ErrBadDecrypt
	}
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize {
		return nil, ErrBadDecrypt
	}
	for _, c := range b[len(b)-n:] {
		if int(c) != n {
			return nil, ErrBadDecrypt
		}
	}
	return b[:len(b)-n], nil
}

func cbcEncrypt(key, iv, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize {
		return nil, errors.New("invalid initialization vector")
	}
	out := pad(append([]byte(nil), plaintext...))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(out, out)
	return out, nil
}

func cbcDecrypt(key, iv, ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize {
		return nil, errors.New("invalid initialization vector")
	}
	if len(ciphertext) == 0 || len(ciphertext)%aes.BlockSize != 0 {
		return nil, errors.New("wrong final block length")
	}
	out := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(out, ciphertext)
	return unpad(out)
}

// EncryptPackageID is encryptPackageID: AES-256-CBC, hex output, no prefix.
func EncryptPackageID(bid, password string, d Derivation) (string, error) {
	key, iv, err := packageKey(password, d)
	if err != nil {
		return "", err
	}
	out, err := cbcEncrypt(key, iv, []byte(bid))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(out), nil
}

// DecryptPackageID is decryptPackageID, for the v1 (unprefixed) format.
func DecryptPackageID(encrypted, password string, d Derivation) (string, error) {
	raw, err := hex.DecodeString(encrypted)
	if err != nil {
		return "", errors.New("not hexadecimal")
	}
	key, iv, err := packageKey(password, d)
	if err != nil {
		return "", err
	}
	out, err := cbcDecrypt(key, iv, raw)
	return string(out), err
}

// packageGCM is AES-256-GCM keyed by sha256(password) with a 16-byte zero IV.
func packageGCM(password string) (cipher.AEAD, []byte, error) {
	key := sha256.Sum256([]byte(password))
	block, err := aes.NewCipher(key[:])
	if err != nil {
		return nil, nil, err
	}
	g, err := cipher.NewGCMWithNonceSize(block, 16)
	return g, make([]byte, 16), err
}

// EncryptPackageIDWithGCM is encryptPackageIDWithGCM: auth tag appended,
// "v2::" prefixed. Identical on every Node version.
func EncryptPackageIDWithGCM(bid, password string) (string, error) {
	if password == "" {
		return "", errors.New("GCM encryption needs a package cipher password -- it has no built-in default")
	}
	g, nonce, err := packageGCM(password)
	if err != nil {
		return "", err
	}
	return "v2::" + hex.EncodeToString(g.Seal(nil, nonce, []byte(bid), nil)), nil
}

// DecryptPackageIDWithGCM accepts the value with or without the "v2::" prefix.
func DecryptPackageIDWithGCM(encrypted, password string) (string, error) {
	raw, err := hex.DecodeString(strings.TrimPrefix(encrypted, "v2::"))
	if err != nil {
		return "", errors.New("not hexadecimal")
	}
	g, nonce, err := packageGCM(password)
	if err != nil {
		return "", err
	}
	if len(raw) <= g.Overhead() {
		return "", errors.New("too short to hold an auth tag")
	}
	out, err := g.Open(nil, nonce, raw, nil)
	if err != nil {
		return "", errors.New("Unsupported state or unable to authenticate data")
	}
	return string(out), nil
}

// EncryptDeeplinkPayment is encryptDeeplinkPayment: AES-256-CBC with an
// explicit key and IV.
func EncryptDeeplinkPayment(param, password, iv string) (string, error) {
	out, err := cbcEncrypt([]byte(password), []byte(iv), []byte(param))
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(out), nil
}

// DecryptDeeplinkPayment is the inverse of EncryptDeeplinkPayment.
func DecryptDeeplinkPayment(param, password, iv string) (string, error) {
	raw, err := hex.DecodeString(param)
	if err != nil {
		return "", errors.New("not hexadecimal")
	}
	out, err := cbcDecrypt([]byte(password), []byte(iv), raw)
	return string(out), err
}

// ParsePrivateKey reads a PKCS#1 or PKCS#8 RSA private key PEM.
func ParsePrivateKey(pemData []byte) (*rsa.PrivateKey, error) {
	b, _ := pem.Decode(pemData)
	if b == nil {
		return nil, errors.New("no PEM block found")
	}
	if k, err := x509.ParsePKCS1PrivateKey(b.Bytes); err == nil {
		return k, nil
	}
	k, err := x509.ParsePKCS8PrivateKey(b.Bytes)
	if err != nil {
		return nil, fmt.Errorf("not an RSA private key: %w", err)
	}
	rk, ok := k.(*rsa.PrivateKey)
	if !ok {
		return nil, errors.New("not an RSA private key")
	}
	return rk, nil
}

// RSAEncrypt is Node's publicEncrypt with the private PEM: RSA-OAEP with
// SHA-1, base64. The keypair is used "backwards" by the services.
func RSAEncrypt(plaintext string, key *rsa.PrivateKey) (string, error) {
	out, err := rsa.EncryptOAEP(sha1.New(), rand.Reader, &key.PublicKey, []byte(plaintext), nil)
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(out), nil
}

// RSADecrypt is Node's privateDecrypt: RSA-OAEP with SHA-1 over base64.
func RSADecrypt(ciphertext string, key *rsa.PrivateKey) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(ciphertext))
	if err != nil {
		// Node's Buffer.from(..., 'base64') is lenient about padding.
		if raw, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(strings.TrimSpace(ciphertext), "=")); err != nil {
			return "", errors.New("not base64")
		}
	}
	out, err := rsa.DecryptOAEP(sha1.New(), nil, key, raw, nil)
	if err != nil {
		return "", errors.New("oaep decoding error")
	}
	return string(out), nil
}

var lowerHex = regexp.MustCompile(`^[0-9a-f]+$`)

// DecryptIdentifierByGCM is decryptIdentifierByGCM: hex of iv(12) |
// ciphertext | tag(16), gzipped plaintext, the password as the raw key.
func DecryptIdentifierByGCM(hexPayload, password string) (string, error) {
	if len(password) != 32 {
		return "", fmt.Errorf("the identifier password must be 32 bytes for aes-256-gcm, got %d", len(password))
	}
	if !lowerHex.MatchString(hexPayload) {
		return "", errors.New("not lowercase hexadecimal")
	}
	raw, err := hex.DecodeString(hexPayload)
	if err != nil {
		return "", errors.New("odd number of hex digits")
	}
	if len(raw) <= 12+16 {
		return "", errors.New("too short to hold an IV and an auth tag")
	}
	block, err := aes.NewCipher([]byte(password))
	if err != nil {
		return "", err
	}
	g, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	compressed, err := g.Open(nil, raw[:12], raw[12:], nil)
	if err != nil {
		return "", errors.New("Unsupported state or unable to authenticate data")
	}
	zr, err := gzip.NewReader(bytes.NewReader(compressed))
	if err != nil {
		return "", errors.New("incorrect header check")
	}
	out, err := io.ReadAll(io.LimitReader(zr, 1<<20))
	if err != nil {
		return "", errors.New("unexpected end of file")
	}
	return string(out), nil
}

// DecryptIdentifierByCBC is the unprefixed legacy "<iv hex>-<ciphertext hex>".
func DecryptIdentifierByCBC(identifier, password string) (string, error) {
	parts := strings.Split(identifier, "-")
	if len(parts) < 2 || parts[1] == "" {
		return "", errors.New(`expected "<iv>-<ciphertext>"`)
	}
	iv, err := hex.DecodeString(parts[0])
	if err != nil {
		return "", errors.New("invalid initialization vector")
	}
	raw, err := hex.DecodeString(parts[1])
	if err != nil {
		return "", errors.New("ciphertext is not hexadecimal")
	}
	if len(password) != 32 {
		return "", errors.New("invalid key length")
	}
	out, err := cbcDecrypt([]byte(password), iv, raw)
	return string(out), err
}

// Identifier is a decrypted identity cookie.
type Identifier struct {
	Version   string // "v3", "v2" or "cbc"
	CipherID  string // v3 only
	Plaintext string
}

var v3CipherID = regexp.MustCompile(`^[A-Z0-9]{5}$`)

// DecryptIdentifier dispatches on the prefix like decryptIdentifier.
// v3Passwords maps an upper-case cipher ID to its password.
func DecryptIdentifier(identifier, password string, v3Passwords map[string]string) (Identifier, error) {
	switch {
	case strings.HasPrefix(identifier, "v3::"):
		rest := identifier[4:]
		id := strings.ToUpper(rest[:min(5, len(rest))])
		res := Identifier{Version: "v3", CipherID: id}
		if !v3CipherID.MatchString(id) {
			return res, fmt.Errorf("invalid v3 cipher ID %q (expected 5 alphanumeric characters)", id)
		}
		pw := v3Passwords[id]
		if pw == "" {
			return res, fmt.Errorf("no password configured for v3 cipher ID %s", id)
		}
		pt, err := DecryptIdentifierByGCM(rest[5:], pw)
		res.Plaintext = pt
		return res, err
	case strings.HasPrefix(identifier, "v2::"):
		pt, err := DecryptIdentifierByGCM(identifier[4:], password)
		return Identifier{Version: "v2", Plaintext: pt}, err
	default:
		pt, err := DecryptIdentifierByCBC(identifier, password)
		return Identifier{Version: "cbc", Plaintext: pt}, err
	}
}
