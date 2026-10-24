package crypto

import (
	"strings"
	"testing"
)

const testKey = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"

func TestEncryptDecryptRoundTrip(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	plaintext := "12345678901"
	enc, err := Encrypt(plaintext)
	if err != nil {
		t.Fatalf("Encrypt falhou: %v", err)
	}
	if enc == plaintext {
		t.Fatal("ciphertext não pode ser igual ao plaintext")
	}

	dec, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt falhou: %v", err)
	}
	if dec != plaintext {
		t.Fatalf("round-trip divergiu: got %q want %q", dec, plaintext)
	}
}

func TestEncryptNonDeterministic(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	a, err := Encrypt("12345678901")
	if err != nil {
		t.Fatalf("Encrypt falhou: %v", err)
	}
	b, err := Encrypt("12345678901")
	if err != nil {
		t.Fatalf("Encrypt falhou: %v", err)
	}
	if a == b {
		t.Fatal("AES-256-GCM deve produzir ciphertexts distintos a cada chamada (nonce aleatório)")
	}
}

func TestEncryptWithoutKey(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", "")

	if _, err := Encrypt("12345678901"); err != ErrKeyNotConfigured {
		t.Fatalf("esperava ErrKeyNotConfigured, got %v", err)
	}
	if _, err := Decrypt("abcd"); err != ErrKeyNotConfigured {
		t.Fatalf("esperava ErrKeyNotConfigured, got %v", err)
	}
}

func TestDecryptCorruptedCiphertext(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	// ciphertext hex válido seria >= 56 chars; valor curto deve falhar.
	if _, err := Decrypt(strings.Repeat("ab", 8)); err != ErrInvalidCiphertext {
		t.Fatalf("esperava ErrInvalidCiphertext, got %v", err)
	}
}
