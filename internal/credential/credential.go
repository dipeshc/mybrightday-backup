// Package credential stores the default Google OAuth client credentials in obfuscated form.
//
// Obfuscation (AES-256-CTR) prevents the plaintext secret from appearing in the compiled
// binary, protecting against accidental exposure via binary inspection ("strings ./binary")
// and automated secret scanners. It is not cryptographic security — the AES key is hardcoded
// and publicly visible in this file. Anyone with access to the source can recover the
// plaintext secret by calling Reveal directly.
package credential

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
)

// cryptKey is a 32-byte AES-256 key used solely for obfuscation.
// It is not a secret: its purpose is scanner evasion and eyedrop protection, not security.
var cryptKey = []byte{
	0x3f, 0xa2, 0x7c, 0x11, 0x58, 0xe4, 0x9d, 0x06,
	0xb1, 0x4f, 0x23, 0x87, 0xcd, 0x5a, 0x19, 0xe3,
	0x72, 0x0b, 0xf6, 0x48, 0x9e, 0x31, 0xd7, 0xac,
	0x64, 0x15, 0x8b, 0xf0, 0x2e, 0x93, 0x46, 0x7d,
}

// ClientID is the Google OAuth application client ID.
// Client IDs are not secrets — they are exchanged publicly during the OAuth flow.
const ClientID = "1063275621143-n5l79mj96ni075aiql9ranvjmnubnumu.apps.googleusercontent.com"

// encryptedClientSecret is the Google OAuth client secret in obfuscated form.
// Generated once with: go run ./tools/obscure <plaintext>
const encryptedClientSecret = "HMI1wmL8bpb4gE91qlbRiPE4ZYmqp5OUDjqKs0E4Sba3CRwFwElXDtd_xGdz4R6H2jVz"

// ClientSecret returns the plaintext OAuth client secret.
func ClientSecret() string {
	return mustReveal(encryptedClientSecret)
}

// Obscure encodes a plaintext string using AES-256-CTR with a random IV.
// The output format is base64url([16-byte IV][ciphertext]).
// Running Obscure twice on the same input produces different output due to the random IV.
func Obscure(x string) (string, error) {
	plaintext := []byte(x)
	buf := make([]byte, aes.BlockSize+len(plaintext))
	iv := buf[:aes.BlockSize]
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		return "", fmt.Errorf("generating IV: %w", err)
	}
	block, err := aes.NewCipher(cryptKey)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	cipher.NewCTR(block, iv).XORKeyStream(buf[aes.BlockSize:], plaintext)
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// Reveal decodes an obfuscated string produced by Obscure.
func Reveal(x string) (string, error) {
	data, err := base64.RawURLEncoding.DecodeString(x)
	if err != nil {
		return "", fmt.Errorf("base64 decode: %w", err)
	}
	if len(data) < aes.BlockSize {
		return "", fmt.Errorf("value too short to contain IV")
	}
	block, err := aes.NewCipher(cryptKey)
	if err != nil {
		return "", fmt.Errorf("creating cipher: %w", err)
	}
	out := make([]byte, len(data)-aes.BlockSize)
	cipher.NewCTR(block, data[:aes.BlockSize]).XORKeyStream(out, data[aes.BlockSize:])
	return string(out), nil
}

func mustReveal(x string) string {
	s, err := Reveal(x)
	if err != nil {
		panic(fmt.Sprintf("credential: failed to reveal obfuscated value: %v", err))
	}
	return s
}
