// Package crypto handles wire framing and the room encryption.
//
// Every datagram on the wire is:
//
//	magic (4 bytes) || nonce (12 bytes) || AES-256-GCM ciphertext(+tag)
//
// The key is derived from the room name and passphrase, so a datagram can only
// be read by someone who knows both. A different room or a different passphrase
// yields a different key, which is also how rooms stay isolated: you literally
// cannot decrypt traffic that isn't addressed to your key.
//
// Encryption is Go's standard library only — no third-party cryptography.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"io"
)

const (
	magic    = "TC02"  // protocol/version tag; bump on incompatible changes
	keyIters = 210_000 // PBKDF2 iterations (OWASP-ish for SHA-256)
	keyLen   = 32      // AES-256
	nonceLen = 12      // GCM standard nonce size
)

// AEAD is the authenticated cipher used to seal and open frames. It aliases the
// standard library type so callers need not import crypto/cipher directly.
type AEAD = cipher.AEAD

// ErrBadFrame is returned by Open for anything that is not a valid frame for the
// given key: a stray packet, another room's traffic, or a corrupted datagram.
var ErrBadFrame = errors.New("frame not addressed to this room/key")

// New derives a room key from (room, passphrase) and returns a ready-to-use
// AES-256-GCM cipher for that room.
func New(room, passphrase string) (AEAD, error) {
	key, err := DeriveKey(room, passphrase)
	if err != nil {
		return nil, err
	}
	return NewAEAD(key)
}

// DeriveKey turns (room, passphrase) into a 32-byte AES key. The room name is
// folded into the salt so the same passphrase used in two different rooms
// produces two different keys.
func DeriveKey(room, passphrase string) ([]byte, error) {
	salt := []byte("tchat-v2|room|" + room)
	return pbkdf2.Key(sha256.New, passphrase, salt, keyIters, keyLen)
}

// NewAEAD wraps a 32-byte key in an AES-256-GCM cipher.
func NewAEAD(key []byte) (AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// Seal encrypts plaintext into a self-describing wire frame.
func Seal(aead AEAD, plaintext []byte) ([]byte, error) {
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	out := make([]byte, 0, len(magic)+nonceLen+len(plaintext)+aead.Overhead())
	out = append(out, magic...)
	out = append(out, nonce...)
	return aead.Seal(out, nonce, plaintext, nil), nil
}

// Open reverses Seal. Anything that isn't a valid frame for this key returns
// ErrBadFrame rather than garbage, so callers can simply skip it.
func Open(aead AEAD, frame []byte) ([]byte, error) {
	if len(frame) < len(magic)+nonceLen {
		return nil, ErrBadFrame
	}
	if string(frame[:len(magic)]) != magic {
		return nil, ErrBadFrame
	}
	nonce := frame[len(magic) : len(magic)+nonceLen]
	ciphertext := frame[len(magic)+nonceLen:]
	plaintext, err := aead.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrBadFrame
	}
	return plaintext, nil
}
