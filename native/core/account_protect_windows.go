package core

import (
	"errors"
	"golang.org/x/sys/windows"
	"unsafe"
)

func hongguoAccountDPAPI(profile string, data []byte, decrypt bool) ([]byte, error) {
	if len(data) == 0 || len(data) > 256*1024 {
		return nil, errors.New("账号保护数据无效")
	}
	incoming := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	entropy := []byte("guoapp/hongguo/v1/" + profile)
	salt := windows.DataBlob{Size: uint32(len(entropy)), Data: &entropy[0]}
	var outgoing windows.DataBlob
	var err error
	if decrypt {
		err = windows.CryptUnprotectData(&incoming, nil, &salt, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outgoing)
	} else {
		err = windows.CryptProtectData(&incoming, nil, &salt, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outgoing)
	}
	if err != nil {
		return nil, errors.New("账号保护失败")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outgoing.Data)))
	return append([]byte(nil), unsafe.Slice(outgoing.Data, int(outgoing.Size))...), nil
}
func protectHongguoAccount(profile string, data, key []byte) ([]byte, error) {
	return hongguoAccountDPAPI(profile, data, false)
}
func unprotectHongguoAccount(profile string, data, key []byte) ([]byte, error) {
	return hongguoAccountDPAPI(profile, data, true)
}
