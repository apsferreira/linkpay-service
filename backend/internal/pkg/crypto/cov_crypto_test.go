package crypto

import (
	crand "crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// covFailingReader always fails, simulating an unavailable entropy source.
type covFailingReader struct{}

func (covFailingReader) Read([]byte) (int, error) {
	return 0, errors.New("entropy source unavailable")
}

// TestCovEncryptNonceGenerationError covers the io.ReadFull(rand.Reader, nonce)
// failure branch by swapping crypto/rand.Reader for a failing reader.
func TestCovEncryptNonceGenerationError(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	orig := crand.Reader
	crand.Reader = covFailingReader{}
	t.Cleanup(func() { crand.Reader = orig })

	_, err := Encrypt("payload")
	if err == nil {
		t.Fatal("expected error when nonce generation fails")
	}
	if !strings.Contains(err.Error(), "nonce") {
		t.Fatalf("expected nonce error, got %q", err)
	}
}

// TestCovEncryptEmptyPlaintext ensures Encrypt handles an empty plaintext and
// that Decrypt recovers it unchanged.
func TestCovEncryptEmptyPlaintext(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	enc, err := Encrypt("")
	if err != nil {
		t.Fatalf("Encrypt(empty) returned error: %v", err)
	}
	if enc == "" {
		t.Fatal("expected non-empty ciphertext for empty plaintext")
	}

	dec, err := Decrypt(enc)
	if err != nil {
		t.Fatalf("Decrypt of empty-plaintext ciphertext failed: %v", err)
	}
	if dec != "" {
		t.Fatalf("expected empty plaintext, got %q", dec)
	}
}

// TestCovDecryptInvalidHex covers the hex.DecodeString failure branch in Decrypt.
func TestCovDecryptInvalidHex(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	if _, err := Decrypt("not-hex-at-all!!"); err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext for non-hex input, got %v", err)
	}
	if _, err := Decrypt("zzzz"); err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext for invalid hex chars, got %v", err)
	}
}

// TestCovDecryptTooShort covers the len(data) < nonceSize branch.
func TestCovDecryptTooShort(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	// 4 bytes of valid hex — below the 12-byte nonce size.
	if _, err := Decrypt("deadbeef"); err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext for short ciphertext, got %v", err)
	}
	if _, err := Decrypt(""); err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext for empty ciphertext, got %v", err)
	}
}

// TestCovDecryptTamperedCiphertext covers the GCM authentication failure branch.
func TestCovDecryptTamperedCiphertext(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	enc, err := Encrypt("sensitive-payload")
	if err != nil {
		t.Fatalf("Encrypt failed: %v", err)
	}
	raw, err := hex.DecodeString(enc)
	if err != nil {
		t.Fatalf("ciphertext is not valid hex: %v", err)
	}
	// Flip a bit in the trailing tag.
	raw[len(raw)-1] ^= 0xFF

	if _, err := Decrypt(hex.EncodeToString(raw)); err != ErrInvalidCiphertext {
		t.Fatalf("expected ErrInvalidCiphertext for tampered ciphertext, got %v", err)
	}
}

// TestCovLoadKeyMalformed covers every loadKey validation branch.
func TestCovLoadKeyMalformed(t *testing.T) {
	tests := []struct {
		name string
		key  string
	}{
		{"short key", "abcd"},
		{"63 hex chars", testKey[:63]},
		{"65 hex chars", testKey + "0"},
		{"64 non-hex chars", strings.Repeat("z", 64)},
		{"unset", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("ENCRYPTION_KEY", tt.key)

			if _, err := Encrypt("payload"); err != ErrKeyNotConfigured {
				t.Fatalf("Encrypt: expected ErrKeyNotConfigured, got %v", err)
			}
			if _, err := Decrypt("deadbeefdeadbeefdeadbeef"); err != ErrKeyNotConfigured {
				t.Fatalf("Decrypt: expected ErrKeyNotConfigured, got %v", err)
			}
		})
	}
}

// TestCovLoadKeyTrimsNothing sanity-checks that a valid key still works after
// the malformed-key cases, and that loadKey returns the decoded 32 bytes.
func TestCovLoadKeyValid(t *testing.T) {
	t.Setenv("ENCRYPTION_KEY", testKey)

	key, err := loadKey()
	if err != nil {
		t.Fatalf("loadKey returned error: %v", err)
	}
	if len(key) != 32 {
		t.Fatalf("expected 32-byte key, got %d", len(key))
	}
}
