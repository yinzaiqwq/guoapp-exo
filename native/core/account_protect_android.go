package core

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
)

func newHongguoAccountAEAD(key []byte) (cipher.AEAD, error) {
	if len(key) != 32 {
		return nil, errors.New("账号保护未就绪")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}
func protectHongguoAccount(profile string, data, key []byte) ([]byte, error) {
	codec, err := newHongguoAccountAEAD(key)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, codec.NonceSize())
	if _, err = rand.Read(nonce); err != nil {
		return nil, err
	}
	return codec.Seal(nonce, nonce, data, []byte("guoapp/hongguo/v1/"+profile)), nil
}
func unprotectHongguoAccount(profile string, data, key []byte) ([]byte, error) {
	codec, err := newHongguoAccountAEAD(key)
	if err != nil {
		return nil, err
	}
	size := codec.NonceSize()
	if len(data) < size+codec.Overhead() {
		return nil, errors.New("账号保护数据无效")
	}
	return codec.Open(nil, data[:size], data[size:], []byte("guoapp/hongguo/v1/"+profile))
}
