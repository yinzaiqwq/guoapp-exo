package core

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/md5"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const hongguoAccountRegisterURL = "https://log.snssdk.com/service/2/device_register/"
const hongguoAccountDeviceVersionCode = "73932"
const hongguoAccountDeviceVersionName = "7.3.9.32"
const hongguoAccountDeviceUserAgent = "com.phoenix.read/73932 (Linux; U; Android 14; zh_CN; Xiaomi 14; Build/UKQ1.230804.001; Cronet/TTNetVersion:8d40f833 QuicVersion:462f352c 2026-08-31)"

func hongguoAccountUUID() (string, error) {
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return "", err
	}
	random[6] = random[6]&0x0f | 0x40
	random[8] = random[8]&0x3f | 0x80
	value := hex.EncodeToString(random[:])
	return value[:8] + "-" + value[8:12] + "-" + value[12:16] + "-" + value[16:20] + "-" + value[20:], nil
}

func prepareHongguoAccountDevice(ctx context.Context) (*http.Request, map[string]string, error) {
	cdid, err := hongguoAccountUUID()
	if err != nil {
		return nil, nil, errors.New("设备初始化随机数不可用")
	}
	clientID, err := hongguoAccountUUID()
	if err != nil {
		return nil, nil, errors.New("设备初始化随机数不可用")
	}
	requestID, err := hongguoAccountUUID()
	if err != nil {
		return nil, nil, errors.New("设备初始化随机数不可用")
	}
	var deviceBytes [8]byte
	if _, err := rand.Read(deviceBytes[:]); err != nil {
		return nil, nil, errors.New("设备初始化随机数不可用")
	}
	openID := hex.EncodeToString(deviceBytes[:])
	now := time.Now()
	milliseconds := now.UnixMilli()
	ticket, timestamp := strconv.FormatInt(milliseconds, 10), strconv.FormatInt(now.Unix(), 10)
	parameters := map[string]string{
		"aid": "8662", "app_name": "novelread", "channel": "xiaomi_8662_64",
		"version_code": hongguoAccountDeviceVersionCode, "version_name": hongguoAccountDeviceVersionName,
		"manifest_version_code": hongguoAccountDeviceVersionCode, "update_version_code": hongguoAccountDeviceVersionCode,
		"device_platform": "android", "os": "android", "ssmix": "a",
		"device_type": "23127PN0CC", "device_brand": "xiaomi", "os_api": "34", "os_version": "14",
		"resolution": "1200*2670", "dpi": "460", "ac": "wifi", "language": "zh",
		"openudid": openID, "cdid": cdid, "tt_data": "a", "_rticket": ticket, "ts": timestamp,
		"package": "com.phoenix.read", "first_launch_timestamp": timestamp,
		"last_deeplink_update_version_code": "0", "cpu_support64": "true", "host_abi": "arm64-v8a",
		"is_guest_mode": "0", "app_type": "normal", "minor_status": "0", "appTheme": "light",
		"is_preinstall": "0", "need_personal_recommend": "1", "is_android_pad": "0",
		"is_android_fold": "0", "md": "0", "cronet_version": "8d40f833_2026-03-03",
		"ttnet_version": "4.2.243.31-douyin", "use_store_region_cookie": "1",
	}
	header := map[string]any{
		"access": "wifi", "aid": 8662, "app_version": hongguoAccountDeviceVersionName, "app_version_minor": "",
		"apk_first_install_time": milliseconds, "cdid": cdid, "channel": parameters["channel"],
		"clientudid": clientID, "cpu_abi": "arm64-v8a", "density_dpi": 460,
		"device_brand": "xiaomi", "device_category": "phone", "device_manufacturer": "Xiaomi",
		"device_model": "Xiaomi 14", "device_platform": "android", "display_density": "xxhdpi",
		"display_name": "红果短剧", "git_hash": "fe95b95", "guest_mode": 0, "ipv6_list": []string{},
		"is_system_app": 0, "language": "zh", "manifest_version_code": 73932,
		"not_request_sender": 0, "oaid_may_support": true, "openudid": openID, "os": "Android",
		"os_api": 34, "os_version": "14", "package": "com.phoenix.read", "region": "CN",
		"release_build": "UKQ1.230804.001_1701234567", "req_id": requestID,
		"resolution": "2670x1200", "rom": "hyperos", "rom_version": "OS1.0.32.0.UNCCNXM",
		"sdk_flavor": "china", "sdk_target_version": 29, "sdk_version": "3.7.3-rc.116-douyin",
		"sig_hash": "aea615ab910015038f73c47e45d21466", "sim_serial_number": []string{},
		"timezone": 8, "tz_name": "Asia/Shanghai", "tz_offset": 28800,
		"update_version_code": 73932, "version_code": 73932,
		"custom": map[string]any{
			"is_android_fold": 0, "filter_warn": 0, "is_android_pad": 0, "client_ipv4": "0.0.0.0",
			"web_ua": "Mozilla/5.0 (Linux; Android 14; Xiaomi 14 Build/UKQ1.230804.001; wv) AppleWebKit/537.36 (KHTML, like Gecko) Version/4.0 Chrome/120.0.0.0 Mobile Safari/537.36",
		},
	}
	payload, err := json.Marshal(map[string]any{"magic_tag": "ss_app_log", "header": header, "_gen_time": milliseconds})
	if err != nil {
		return nil, nil, errors.New("设备初始化数据未能生成")
	}
	body, err := hongguoTTEncrypt(payload)
	if err != nil {
		return nil, nil, err
	}
	query := url.Values{}
	for key, value := range parameters {
		query.Set(key, value)
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, hongguoAccountRegisterURL+"?"+query.Encode(), bytes.NewReader(body))
	if err != nil {
		return nil, nil, errors.New("设备初始化请求未能生成")
	}
	request.GetBody = nil
	request.Header.Set("Content-Type", "application/octet-stream;tt-data=a")
	request.Header.Set("Content-Encoding", "gzip")
	request.Header.Set("Accept-Encoding", "gzip")
	request.Header.Set("User-Agent", hongguoAccountDeviceUserAgent)
	request.Header.Set("Accept", "application/json; charset=utf-8,application/x-protobuf")
	digest := md5.Sum(body)
	request.Header.Set("X-SS-STUB", fmt.Sprintf("%X", digest))
	request.Header.Set("X-SS-DP", "8662")
	request.Header.Set("X-SS-REQ-TICKET", ticket)
	request.Header.Set("X-Khronos", timestamp)
	var argus [4]byte
	binary.LittleEndian.PutUint32(argus[:], uint32(now.Unix()))
	request.Header.Set("X-Argus", base64.StdEncoding.EncodeToString(argus[:]))
	request.Header.Set("X-TT-Request-Tag", "t=0;n=1")
	request.Header.Set("Log-Encode-Type", "gzip")
	request.Header.Set("Passport-SDK-Version", "60755")
	request.Header.Set("Passport-SDK-Settings", "device_transfer_s_0,device_transfer_ab_1")
	request.Header.Set("Sdk-Version", "2")
	request.Header.Set("lc", "102")
	request.Header.Set("X-TT-Store-Region", "cn")
	request.Header.Set("X-TT-Store-Region-Src", "did")
	request.Header.Set("X-VC-Bdturing-SDK-Version", "4.0.3.cn")
	return request, parameters, nil
}

func newHongguoAccountDevice(ctx context.Context) (*hongguoAccountDevice, error) {
	work, cancel := context.WithTimeout(ctx, 35*time.Second)
	defer cancel()
	request, parameters, err := prepareHongguoAccountDevice(work)
	if err != nil {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = (&net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}).DialContext
	transport.ResponseHeaderTimeout = 25 * time.Second
	client := &http.Client{
		Timeout: 35 * time.Second, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	defer client.CloseIdleConnections()
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("设备初始化连接中断，请稍后重试")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("红果暂未接受设备初始化，请稍后重试")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, hongguoAccountMaxResponse+1))
	if err != nil || len(content) == 0 || len(content) > hongguoAccountMaxResponse {
		return nil, errors.New("设备初始化响应不可用")
	}
	if strings.EqualFold(response.Header.Get("Content-Encoding"), "gzip") {
		reader, err := gzip.NewReader(bytes.NewReader(content))
		if err != nil {
			return nil, errors.New("设备初始化响应不可用")
		}
		content, err = io.ReadAll(io.LimitReader(reader, hongguoAccountMaxResponse+1))
		reader.Close()
		if err != nil || len(content) == 0 || len(content) > hongguoAccountMaxResponse {
			return nil, errors.New("设备初始化响应不可用")
		}
	} else if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil, errors.New("设备初始化响应编码暂不支持")
	}
	if bytes.HasPrefix(content, hongguoTTMagic[:4]) {
		content, err = hongguoTTDecrypt(content)
		if err != nil {
			return nil, err
		}
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	var result map[string]any
	if decoder.Decode(&result) != nil || result == nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("设备初始化响应格式异常")
	}
	deviceID, err := hongguoAccountRegistrationID(result, "device_id_str", "device_id")
	if err != nil {
		return nil, err
	}
	installID, err := hongguoAccountRegistrationID(result, "install_id_str", "install_id")
	if err != nil {
		return nil, err
	}
	token := ""
	if raw, present := result["device_token"]; present {
		var valid bool
		token, valid = raw.(string)
		if !valid || !hongguoQRHeaderValue(token) {
			return nil, errors.New("设备初始化凭据不可用")
		}
	}
	cookies := map[string]string{}
	allowed := map[string]bool{"odin_tt": true, "ttreq": true, "store-region": true, "store-region-src": true}
	for index, cookie := range response.Cookies() {
		if index >= 32 {
			break
		}
		if allowed[cookie.Name] && len(cookie.Value) < 8192 && hongguoQRHeaderValue(cookie.Value) {
			cookies[cookie.Name] = cookie.Value
		}
	}
	parameters["device_id"], parameters["iid"] = deviceID, installID
	return &hongguoAccountDevice{
		Params: parameters, Cookies: cookies, DeviceToken: token, UserAgent: hongguoAccountDeviceUserAgent,
	}, nil
}

func hongguoAccountRegistrationID(value map[string]any, names ...string) (string, error) {
	id := ""
	for _, name := range names {
		raw, present := value[name]
		if !present {
			continue
		}
		var current string
		switch raw := raw.(type) {
		case string:
			current = raw
		case json.Number:
			current = raw.String()
		}
		if !hongguoQRIdentity.MatchString(current) || id != "" && id != current {
			return "", errors.New("红果暂未返回有效的设备标识")
		}
		id = current
	}
	if id == "" {
		return "", errors.New("红果暂未返回有效的设备标识")
	}
	return id, nil
}
