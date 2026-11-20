package core

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sync"
	"time"
)

var hongguoProfileID = regexp.MustCompile("^[a-zA-Z0-9_-]{1,64}$")

type hongguoAccountDisk struct {
	Version     int                        `json:"version"`
	Credentials *hongguoAccountCredentials `json:"credentials,omitempty"`
	Device      *hongguoAccountDevice      `json:"device,omitempty"`
	AttemptAt   int64                      `json:"attemptAt,omitempty"`
}

type hongguoAccountState struct {
	operationMu   sync.Mutex
	mu            sync.Mutex
	qr            *hongguoQR
	loaded        bool
	loadError     bool
	expired       bool
	devicePending bool
	disk          hongguoAccountDisk
}

func (engine *nativeEngine) accountState(profile string) (*hongguoAccountState, error) {
	if !hongguoProfileID.MatchString(profile) {
		return nil, errors.New("当前用户无效")
	}
	engine.accountMu.Lock()
	defer engine.accountMu.Unlock()
	if engine.accounts == nil {
		engine.accounts = map[string]*hongguoAccountState{}
	}
	if engine.accounts[profile] == nil {
		if len(engine.accounts) >= 64 {
			return nil, errors.New("账号会话过多，请重新打开应用")
		}
		engine.accounts[profile] = &hongguoAccountState{}
	}
	return engine.accounts[profile], nil
}

func (engine *nativeEngine) accountFile(profile string) string {
	hash := sha256.Sum256([]byte(profile))
	return filepath.Join(engine.directory, "private-accounts", hex.EncodeToString(hash[:])+".bin")
}

func (engine *nativeEngine) accountKey() []byte {
	engine.accountMu.Lock()
	defer engine.accountMu.Unlock()
	return append([]byte(nil), engine.accountStorageKey...)
}

func (engine *nativeEngine) accountPersistent() bool {
	return runtime.GOOS == "windows" || runtime.GOOS == "android" && len(engine.accountKey()) == 32
}

func (engine *nativeEngine) loadAccount(profile string, state *hongguoAccountState) {
	if state.loaded {
		return
	}
	state.loaded = true
	state.disk.Version = 1
	if !engine.accountPersistent() {
		return
	}
	file, err := os.Open(engine.accountFile(profile))
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		state.loadError = true
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() <= 0 || info.Size() > 256*1024 {
		state.loadError = true
		return
	}
	raw := make([]byte, info.Size())
	if _, err = io.ReadFull(file, raw); err != nil {
		state.loadError = true
		return
	}
	plain, err := unprotectHongguoAccount(profile, raw, engine.accountKey())
	if err != nil {
		state.loadError = true
		return
	}
	defer clear(plain)
	var saved hongguoAccountDisk
	if json.Unmarshal(plain, &saved) != nil || saved.Version != 1 || saved.Credentials != nil && !validSavedHongguoCredentials(saved.Credentials) {
		state.loadError = true
		return
	}
	if saved.Device != nil && !validHongguoAccountDevice(saved.Device) {
		state.loadError = true
		return
	}
	state.disk = saved
}

func validSavedHongguoCredentials(value *hongguoAccountCredentials) bool {
	if !hongguoQRIdentity.MatchString(value.UID) || len(value.Cookies) > 32 || len(value.Name) > 400 || !hongguoQRHeaderValue(value.Token) {
		return false
	}
	found := false
	for _, cookie := range value.Cookies {
		if cookie == nil || !hongguoQRLoginCookies[cookie.Name] || cookie.Valid() != nil || !hongguoQRHeaderValue(cookie.Value) {
			return false
		}
		if cookie.Name == "sessionid" || cookie.Name == "sessionid_ss" || cookie.Name == "sid_tt" {
			found = found || cookie.Value != ""
		}
	}
	return found
}

func (engine *nativeEngine) saveAccount(profile string, disk hongguoAccountDisk) error {
	if !engine.accountPersistent() {
		return nil
	}
	body, err := json.Marshal(disk)
	if err != nil || len(body) > 128*1024 {
		return errors.New("红果登录信息保存失败，请重试")
	}
	defer clear(body)
	encrypted, err := protectHongguoAccount(profile, body, engine.accountKey())
	if err != nil {
		return errors.New("红果登录信息加密失败，请重试")
	}
	if err = writeNativeCacheFile(engine.accountFile(profile), encrypted); err != nil {
		return errors.New("红果登录信息尚未保存，请检查存储空间后重试")
	}
	return nil
}

func (engine *nativeEngine) accountStatus(state *hongguoAccountState) map[string]any {
	result := map[string]any{"authenticated": state.disk.Credentials != nil && !state.expired, "persistent": engine.accountPersistent(), "storageError": state.loadError, "sessionExpired": state.expired}
	if state.disk.Credentials != nil {
		result["displayName"] = state.disk.Credentials.Name
	}
	if state.loadError {
		result["message"] = "保存的红果登录信息无法读取，请清除后重新登录"
	}
	return result
}

func (engine *nativeEngine) nativeHongguoAccount(ctx context.Context, input nativeInput) (any, error) {
	if input.Command == "prepare" {
		if runtime.GOOS != "android" {
			return map[string]any{"persistent": engine.accountPersistent()}, nil
		}
		key, err := base64.StdEncoding.DecodeString(input.AccountKey)
		if err != nil || len(key) != 32 {
			return nil, errors.New("本机账号保护尚未就绪，请重试")
		}
		engine.accountMu.Lock()
		if len(engine.accountStorageKey) != 0 && !bytes.Equal(key, engine.accountStorageKey) {
			engine.accountMu.Unlock()
			clear(key)
			return nil, errors.New("本机账号保护已变化，请重新打开应用")
		}
		engine.accountStorageKey = key
		engine.accountMu.Unlock()
		return map[string]any{"persistent": true}, nil
	}
	state, err := engine.accountState(input.ProfileID)
	if err != nil {
		return nil, err
	}
	if input.Command == "cancel" {
		state.mu.Lock()
		qr := state.qr
		state.mu.Unlock()
		accepted := false
		if qr != nil {
			accepted = qr.Cancel(input.Session)
		}
		return map[string]any{"cancelled": accepted}, nil
	}
	state.operationMu.Lock()
	defer state.operationMu.Unlock()
	if runtime.GOOS == "android" && !engine.accountPersistent() {
		return nil, errors.New("本机账号保护尚未就绪，请重试")
	}
	engine.loadAccount(input.ProfileID, state)
	switch input.Command {
	case "status":
		return engine.accountStatus(state), nil
	case "logout":
		next := hongguoAccountDisk{Version: 1, Device: state.disk.Device, AttemptAt: state.disk.AttemptAt}
		if state.loadError {
			if err := os.Remove(engine.accountFile(input.ProfileID)); err != nil && !errors.Is(err, os.ErrNotExist) {
				return nil, errors.New("本地登录信息清除失败，请重试")
			}
		} else if err := engine.saveAccount(input.ProfileID, next); err != nil {
			return nil, err
		}
		state.mu.Lock()
		if state.qr != nil {
			state.qr.Cancel("")
			state.qr = nil
		}
		state.mu.Unlock()
		state.disk, state.loadError, state.expired, state.devicePending = next, false, false, false
		return engine.accountStatus(state), nil
	}
	if state.loadError {
		return nil, errors.New("请先清除本地登录信息，再重新扫码")
	}
	switch input.Command {
	case "start":
		if state.disk.Credentials != nil {
			return nil, errors.New("请先退出当前红果账号")
		}
		state.mu.Lock()
		if state.qr != nil {
			state.qr.Cancel("")
		}
		qr := newHongguoQR()
		state.qr = qr
		state.mu.Unlock()
		return qr.Start(ctx)
	case "poll", "commit":
		state.mu.Lock()
		qr := state.qr
		state.mu.Unlock()
		if qr == nil {
			return nil, errors.New("二维码会话已结束，请重新扫码")
		}
		if input.Command == "poll" {
			return qr.Poll(ctx, input.Session)
		}
		if state.disk.Credentials != nil {
			return nil, errors.New("当前用户已登录红果账号")
		}
		err = qr.Commit(input.Session, func(credentials *hongguoAccountCredentials) error {
			next := state.disk
			next.Credentials = credentials
			if err := engine.saveAccount(input.ProfileID, next); err != nil {
				return err
			}
			state.disk, state.expired = next, false
			return nil
		})
		if err != nil {
			return nil, err
		}
		return engine.accountStatus(state), nil
	case "likes", "favorites", "history":
		if state.disk.Credentials == nil || state.expired {
			return nil, errHongguoAccountSessionExpired
		}
		if input.Offset < 0 || input.Offset > 2000 {
			return nil, errors.New("账号片单页码无效")
		}
		if state.disk.Device == nil {
			now := time.Now().Unix()
			if state.disk.AttemptAt != 0 && now-state.disk.AttemptAt < 60 {
				return nil, errors.New("账号连接刚尝试过，请稍候一分钟再刷新片单")
			}
			next := state.disk
			next.AttemptAt = now
			if err := engine.saveAccount(input.ProfileID, next); err != nil {
				return nil, err
			}
			state.disk = next
			device, err := newHongguoAccountDevice(ctx)
			if err != nil {
				return nil, err
			}
			next.Device = device
			state.disk = next
			state.devicePending = true
		}
		if state.devicePending {
			if err := engine.saveAccount(input.ProfileID, state.disk); err != nil {
				return nil, err
			}
			state.devicePending = false
		}
		var result map[string]any
		if input.Command == "likes" {
			result, err = fetchHongguoAccountLikes(ctx, state.disk.Credentials, state.disk.Device, input.Offset)
		} else {
			result, err = fetchHongguoAccountLibrary(ctx, state.disk.Credentials, state.disk.Device, input.Command, input.Offset, engine.downloader.hongguoAccountLibraryMetadata)
		}
		if errors.Is(err, errHongguoAccountSessionExpired) {
			state.expired = true
		}
		return result, err
	default:
		return nil, errors.New("账号操作无效")
	}
}
