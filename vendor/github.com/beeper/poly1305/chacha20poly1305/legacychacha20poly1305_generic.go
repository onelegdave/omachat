package chacha20poly1305

import (
	"golang.org/x/crypto/chacha20"
	"github.com/beeper/poly1305/internal/alias"
	"github.com/beeper/poly1305/internal/poly1305"
)

func (l *legacychacha20poly1305) sealLegacyGeneric(dst, nonce, plaintext, additionalData []byte) []byte {
	ret, out := sliceForAppend(dst, len(plaintext)+poly1305.TagSize)
	ciphertext, tag := out[:len(plaintext)], out[len(plaintext):]
	if alias.InexactOverlap(out, plaintext) {
		panic("chacha20poly1305: invalid buffer overlap")
	}

	var polyKey [32]byte
	// append 4 zero bytes to the nonce to pad it out
	nonce = append([]byte{0, 0, 0, 0}, nonce...)
	s, _ := chacha20.NewUnauthenticatedCipher(l.key[:], nonce)
	s.XORKeyStream(polyKey[:], polyKey[:])
	s.SetCounter(1) // set the counter to 1, skipping 32 bytes
	s.XORKeyStream(ciphertext, plaintext)

	// This is the main difference between ChaCha20-Poly1305 and its legacy version.
	p := poly1305.New(&polyKey)
	p.Write(additionalData)
	writeUint64(p, len(additionalData))
	p.Write(ciphertext)
	writeUint64(p, len(plaintext))
	p.Sum(tag[:0])

	return ret
}

func (l *legacychacha20poly1305) openLegacyGeneric(dst, nonce, ciphertext, additionalData []byte) ([]byte, error) {
	tag := ciphertext[len(ciphertext)-16:]
	ciphertext = ciphertext[:len(ciphertext)-16]

	var polyKey [32]byte
	s, _ := chacha20.NewUnauthenticatedCipher(l.key[:], nonce)
	s.XORKeyStream(polyKey[:], polyKey[:])
	s.SetCounter(1) // set the counter to 1, skipping 32 bytes

	p := poly1305.New(&polyKey)
	p.Write(additionalData)
	writeUint64(p, len(additionalData))
	p.Write(ciphertext)
	writeUint64(p, len(ciphertext))

	ret, out := sliceForAppend(dst, len(ciphertext))
	if alias.InexactOverlap(out, ciphertext) {
		panic("chacha20poly1305: invalid buffer overlap")
	}
	if !p.Verify(tag) {
		for i := range out {
			out[i] = 0
		}
		return nil, errOpen
	}

	s.XORKeyStream(out, ciphertext)
	return ret, nil
}
