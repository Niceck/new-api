package service

import (
	"crypto/sha256"
	"errors"
	"math/big"
)

const tronMainnetAddressPrefix byte = 0x41

var tronBase58Alphabet = []byte("123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz")

func validateTronAddress(address string) error {
	decoded, err := decodeTronBase58(address)
	if err != nil {
		return err
	}
	if len(decoded) != 25 || decoded[0] != tronMainnetAddressPrefix {
		return errors.New("invalid TRON mainnet address")
	}
	first := sha256.Sum256(decoded[:21])
	second := sha256.Sum256(first[:])
	for i := 0; i < 4; i++ {
		if decoded[21+i] != second[i] {
			return errors.New("invalid TRON address checksum")
		}
	}
	return nil
}

func decodeTronBase58(value string) ([]byte, error) {
	if value == "" {
		return nil, errors.New("TRON address is empty")
	}

	decoded := big.NewInt(0)
	base := big.NewInt(58)
	for i := 0; i < len(value); i++ {
		index := -1
		for j, char := range tronBase58Alphabet {
			if value[i] == char {
				index = j
				break
			}
		}
		if index < 0 {
			return nil, errors.New("invalid TRON Base58 character")
		}
		decoded.Mul(decoded, base)
		decoded.Add(decoded, big.NewInt(int64(index)))
	}

	result := decoded.Bytes()
	leadingZeros := 0
	for leadingZeros < len(value) && value[leadingZeros] == tronBase58Alphabet[0] {
		leadingZeros++
	}
	if leadingZeros == 0 {
		return result, nil
	}
	withZeros := make([]byte, leadingZeros+len(result))
	copy(withZeros[leadingZeros:], result)
	return withZeros, nil
}
