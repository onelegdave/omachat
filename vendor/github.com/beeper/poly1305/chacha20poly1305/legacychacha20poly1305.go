package chacha20poly1305

import (
	"crypto/cipher"
	"errors"
)

type legacychacha20poly1305 struct {
	key [KeySize]byte
}

// NewLegacy returns a legacy ChaCha20-Poly1305 AEAD that uses the given 256-bit key.
//
// ChaCha20-Poly1305 is a ChaCha20-Poly1305 variant that takes a shorter nonce,
// for compatibility purposes.
func NewLegacy(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySize {
		return nil, errors.New("chacha20poly1305: bad key length")
	}
	ret := new(legacychacha20poly1305)
	copy(ret.key[:], key)
	return ret, nil
}

func (*legacychacha20poly1305) NonceSize() int {
	return NonceSizeLegacy
}

func (*legacychacha20poly1305) Overhead() int {
	return Overhead
}

func (l *legacychacha20poly1305) Seal(dst, nonce, plaintext, additionalData []byte) []byte {
	if len(nonce) != NonceSizeLegacy {
		panic("chacha20poly1305: bad nonce length passed to Seal")
	}

	// Legacy ChaCha20-Poly1305 technically supports a 64-bit counter, so there is no
	// size limit. However, since we reuse the ChaCha20-Poly1305 implementation,
	// the second half of the counter is not available. This is unlikely to be
	// an issue because the cipher.AEAD API requires the entire message to be in
	// memory, and the counter overflows at 256 GB.
	if uint64(len(plaintext)) > (1<<38)-64 {
		panic("chacha20poly1305: plaintext too large")
	}

	return l.sealLegacyGeneric(dst, nonce, plaintext, additionalData)
}

func (l *legacychacha20poly1305) Open(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	if len(nonce) != NonceSizeX {
		panic("chacha20poly1305: bad nonce length passed to Open")
	}
	if len(ciphertext) < 16 {
		return nil, errOpen
	}
	if uint64(len(ciphertext)) > (1<<38)-48 {
		panic("chacha20poly1305: ciphertext too large")
	}

	return l.openLegacyGeneric(dst, nonce, ciphertext, additionalData)
}
