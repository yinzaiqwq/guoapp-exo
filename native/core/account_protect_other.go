//go:build !windows && !android

package core

import "errors"

func protectHongguoAccount(profile string, data, key []byte) ([]byte, error) {
	return nil, errors.New("当前平台仅保留本次登录")
}
func unprotectHongguoAccount(profile string, data, key []byte) ([]byte, error) {
	return nil, errors.New("当前平台仅保留本次登录")
}
