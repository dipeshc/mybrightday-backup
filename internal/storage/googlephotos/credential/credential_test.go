package credential

import (
	"strings"
	"testing"
)

func TestObscureRevealRoundtrip(t *testing.T) {
	tests := []string{
		"",
		"secret-value",
		"with spaces and symbols !@#$%^&*()",
		"unicode: 写真 🎉",
	}
	for _, plaintext := range tests {
		t.Run(plaintext, func(t *testing.T) {
			obscured, err := Obscure(plaintext)
			if err != nil {
				t.Fatalf("Obscure: %v", err)
			}
			revealed, err := Reveal(obscured)
			if err != nil {
				t.Fatalf("Reveal: %v", err)
			}
			if revealed != plaintext {
				t.Errorf("roundtrip = %q, want %q", revealed, plaintext)
			}
		})
	}
}

func TestObscureUsesRandomIV(t *testing.T) {
	a, err := Obscure("same input")
	if err != nil {
		t.Fatalf("Obscure: %v", err)
	}
	b, err := Obscure("same input")
	if err != nil {
		t.Fatalf("Obscure: %v", err)
	}
	if a == b {
		t.Error("two Obscure calls produced identical output, IV is not random")
	}

	ra, _ := Reveal(a)
	rb, _ := Reveal(b)
	if ra != rb || ra != "same input" {
		t.Errorf("reveals = %q / %q, want both %q", ra, rb, "same input")
	}
}

func TestRevealErrors(t *testing.T) {
	if _, err := Reveal("!!!not base64url!!!"); err == nil || !strings.Contains(err.Error(), "base64 decode") {
		t.Errorf("err = %v, want base64 decode error", err)
	}
	if _, err := Reveal("c2hvcnQ"); err == nil || !strings.Contains(err.Error(), "too short") {
		t.Errorf("err = %v, want too-short error", err)
	}
}

func TestClientSecret(t *testing.T) {
	if ClientSecret() == "" {
		t.Error("ClientSecret() is empty")
	}
	if ClientID == "" {
		t.Error("ClientID is empty")
	}
}
