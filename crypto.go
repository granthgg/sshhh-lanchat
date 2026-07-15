package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

// Wire framing and encryption.
//
// Every datagram on the wire is:
//
//	magic (4 bytes) || nonce (12 bytes) || AES-256-GCM ciphertext(+tag)
//
// The key is derived from the room name and passphrase, so a datagram can
// only be read by someone who knows both. A different room or a different
// passphrase yields a different key, which is also how rooms stay isolated:
// you literally cannot decrypt traffic that isn't addressed to your key.

const (
	magic    = "TC02"  // protocol/version tag; bump on incompatible changes
	keyIters = 210_000 // PBKDF2 iterations (OWASP-ish for SHA-256)
	keyLen   = 32      // AES-256
	nonceLen = 12      // GCM standard nonce size
)

var errBadFrame = errors.New("frame not addressed to this room/key")

// deriveKey turns (room, passphrase) into a 32-byte AES key. The room name is
// folded into the salt so the same passphrase used in two different rooms
// produces two different keys.
func deriveKey(room, passphrase string) ([]byte, error) {
	salt := []byte("tchat-v2|room|" + room)
	return pbkdf2.Key(sha256.New, passphrase, salt, keyIters, keyLen)
}

func newAEAD(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// seal encrypts plaintext into a self-describing wire frame.
func seal(aead cipher.AEAD, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(magic)+nonceLen+len(plaintext)+aead.Overhead())
	out = append(out, magic...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, nil), nil
}

// open reverses seal. Anything that isn't a valid frame for this key — a
// stray packet, another room's traffic, a corrupted datagram — returns
// errBadFrame rather than garbage, so callers can simply skip it.
func open(aead cipher.AEAD, frame []byte) ([]byte, error) {
	if len(frame) < len(magic)+nonceLen {
		return nil, errBadFrame
	}
	if string(frame[:len(magic)]) != magic {
		return nil, errBadFrame
	}
	nonce := frame[len(magic) : len(magic)+nonceLen]
	ciphertext := frame[len(magic)+nonceLen:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, errBadFrame
	}
	return plaintext, nil
}
