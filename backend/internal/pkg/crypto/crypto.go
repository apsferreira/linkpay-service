// Package crypto fornece primitivas de criptografia para dados sensíveis (LGPD).
// Usa AES-256-GCM com nonce aleatório de 12 bytes, conforme recomendação NIST SP 800-38D.
// O ciphertext é armazenado como hex(nonce || ciphertext).
//
// Mesmo padrão adotado pelo customer-service (migrations 013/019) no ecossistema IIT.
//
// Variável de ambiente obrigatória: ENCRYPTION_KEY — 32 bytes em hex (64 caracteres).
// Gere com: openssl rand -hex 32
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
)

var (
	// ErrKeyNotConfigured indica que ENCRYPTION_KEY não está definida ou é inválida.
	ErrKeyNotConfigured = errors.New("ENCRYPTION_KEY não configurada ou inválida (deve ser 32 bytes em hex)")

	// ErrInvalidCiphertext indica dado corrompido ou formato incorreto.
	ErrInvalidCiphertext = errors.New("ciphertext inválido ou corrompido")
)

// loadKey lê ENCRYPTION_KEY do ambiente e decodifica os 32 bytes.
func loadKey() ([]byte, error) {
	hexKey := os.Getenv("ENCRYPTION_KEY")
	if len(hexKey) != 64 {
		return nil, ErrKeyNotConfigured
	}
	key, err := hex.DecodeString(hexKey)
	if err != nil || len(key) != 32 {
		return nil, ErrKeyNotConfigured
	}
	return key, nil
}

// Encrypt cifra plaintext com AES-256-GCM e retorna hex(nonce || ciphertext).
// O nonce de 12 bytes é gerado aleatoriamente a cada chamada (cada valor tem ciphertext único).
func Encrypt(plaintext string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: falha ao criar cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: falha ao criar GCM: %w", err)
	}

	nonce := make([]byte, aesGCM.NonceSize()) // 12 bytes
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("crypto: falha ao gerar nonce: %w", err)
	}

	ciphertext := aesGCM.Seal(nonce, nonce, []byte(plaintext), nil)
	return hex.EncodeToString(ciphertext), nil
}

// Decrypt decifra um valor produzido por Encrypt.
// Espera hex(nonce || ciphertext); retorna o plaintext original.
func Decrypt(encoded string) (string, error) {
	key, err := loadKey()
	if err != nil {
		return "", err
	}

	data, err := hex.DecodeString(encoded)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return "", fmt.Errorf("crypto: falha ao criar cipher: %w", err)
	}

	aesGCM, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("crypto: falha ao criar GCM: %w", err)
	}

	nonceSize := aesGCM.NonceSize()
	if len(data) < nonceSize {
		return "", ErrInvalidCiphertext
	}

	nonce, ciphertext := data[:nonceSize], data[nonceSize:]
	plaintext, err := aesGCM.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", ErrInvalidCiphertext
	}

	return string(plaintext), nil
}
