package ip

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"io"

	"golang.org/x/crypto/pbkdf2"

	"go.chrisrx.dev/webos/ip/internal"
)

// LG documentation about the encryption used indicate the salt is hardcoded to
// this value
var salt = []byte{0x63, 0x61, 0xb8, 0x0e, 0x9b, 0xdc, 0xa6, 0x63, 0x8d, 0x07, 0x20, 0xf2, 0xcc, 0x56, 0x8f, 0xb9}

type Encoder struct {
	b cipher.Block
}

func NewEncoder(key string) (*Encoder, error) {
	block, err := aes.NewCipher(pbkdf2.Key([]byte(key), salt, 16384, 16, sha256.New))
	if err != nil {
		return nil, err
	}
	e := &Encoder{
		b: block,
	}
	return e, nil
}

func (e *Encoder) Encode(plaintext []byte) []byte {
	plaintext = pad(append(plaintext, []byte("\r")...), e.b.BlockSize())
	ciphertext := make([]byte, len(plaintext))
	iv := make([]byte, aes.BlockSize)
	if _, err := io.ReadFull(rand.Reader, iv); err != nil {
		panic(err)
	}
	ivEnc := make([]byte, len(iv))
	internal.NewECBEncrypter(e.b).CryptBlocks(ivEnc, iv)
	cipher.NewCBCEncrypter(e.b, iv).CryptBlocks(ciphertext, plaintext)
	return append(ivEnc, ciphertext...)
}

func (e *Encoder) Decode(ciphertext []byte) ([]byte, error) {
	iv := make([]byte, aes.BlockSize)
	internal.NewECBDecrypter(e.b).CryptBlocks(iv, ciphertext[:e.b.BlockSize()])
	ciphertext = ciphertext[16:]
	mode := cipher.NewCBCDecrypter(e.b, iv)
	mode.CryptBlocks(ciphertext, ciphertext)
	ciphertext = trim(ciphertext)
	return ciphertext, nil
}

func pad(data []byte, blockSize int) []byte {
	padding := (blockSize - len(data)%blockSize)
	padtext := bytes.Repeat([]byte{byte(padding)}, padding)
	return append(data, padtext...)
}

func trim(data []byte) []byte {
	padding := data[len(data)-1]
	if len(data)-int(padding) < 0 {
		return data
	}
	return data[:len(data)-int(padding)]
}
