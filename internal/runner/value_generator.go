package runner

import (
	"crypto/rand"
	"fmt"
	"math/big"
	"strings"
)

const base62Alphabet = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

type cryptoValueGenerator struct{}

func (cryptoValueGenerator) UUID() (string, error) {
	data := make([]byte, 16)
	if _, err := rand.Read(data); err != nil {
		return "", err
	}
	data[6] = (data[6] & 0x0f) | 0x40
	data[8] = (data[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		data[0:4], data[4:6], data[6:8], data[8:10], data[10:16]), nil
}

func (cryptoValueGenerator) Base62(length int) (string, error) {
	var result strings.Builder
	result.Grow(length)
	maximum := big.NewInt(int64(len(base62Alphabet)))
	for range length {
		index, err := rand.Int(rand.Reader, maximum)
		if err != nil {
			return "", err
		}
		result.WriteByte(base62Alphabet[index.Int64()])
	}
	return result.String(), nil
}
