// Package crypto шифрует секреты (ключи провайдеров ИИ) для хранения в БД.
// AES-256-GCM, ключ шифрования — из env APP_ENC_KEY (base64, 32 байта).
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"io"
)

// ErrNoKey — APP_ENC_KEY не задан; персональные ключи недоступны (работает глобальный fallback).
var ErrNoKey = errors.New("crypto: APP_ENC_KEY не задан")

// Cipher инкапсулирует AEAD для шифрования/расшифровки секретов.
type Cipher struct {
	aead cipher.AEAD
}

// New создаёт Cipher из base64-ключа (ровно 32 байта после декода -> AES-256).
// Пустой ключ возвращает ErrNoKey — вызывающая сторона решает, включать ли персональные ключи.
func New(keyB64 string) (*Cipher, error) {
	if keyB64 == "" {
		return nil, ErrNoKey
	}
	key, err := base64.StdEncoding.DecodeString(keyB64)
	if err != nil {
		return nil, errors.New("crypto: APP_ENC_KEY не декодируется из base64")
	}
	if len(key) != 32 {
		return nil, errors.New("crypto: APP_ENC_KEY должен быть 32 байта (base64)")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Cipher{aead: aead}, nil
}

// Encrypt возвращает nonce||ciphertext.
func (c *Cipher) Encrypt(plain []byte) ([]byte, error) {
	nonce := make([]byte, c.aead.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	return c.aead.Seal(nonce, nonce, plain, nil), nil
}

// Decrypt принимает nonce||ciphertext, возвращает открытый текст.
func (c *Cipher) Decrypt(enc []byte) ([]byte, error) {
	ns := c.aead.NonceSize()
	if len(enc) < ns {
		return nil, errors.New("crypto: слишком короткий шифртекст")
	}
	nonce, ct := enc[:ns], enc[ns:]
	return c.aead.Open(nil, nonce, ct, nil)
}
