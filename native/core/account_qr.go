package core

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"golang.org/x/net/publicsuffix"
)

const hongguoQROrigin = "https://hongguoduanju.com"
const hongguoQRMaximumBody = 256 * 1024
const hongguoQRUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/130.0.0.0 Safari/537.36"

var hongguoQRIdentity = regexp.MustCompile("^[1-9][0-9]{4,29}$")
var hongguoQRLoginCookies = map[string]bool{
	"sessionid": true, "sessionid_ss": true, "sid_tt": true, "sid_guard": true,
	"uid_tt": true, "uid_tt_ss": true, "passport_csrf_token": true,
	"passport_csrf_token_default": true,
}

type hongguoAccountCredentials struct {
	UID     string         `json:"uid"`
	Name    string         `json:"name"`
	Token   string         `json:"token,omitempty"`
	Cookies []*http.Cookie `json:"cookies"`
}

func cloneHongguoAccountCredentials(value *hongguoAccountCredentials) *hongguoAccountCredentials {
	if value == nil {
		return nil
	}
	cloned := *value
	cloned.Cookies = make([]*http.Cookie, 0, len(value.Cookies))
	for _, cookie := range value.Cookies {
		if cookie != nil {
			copy := *cookie
			cloned.Cookies = append(cloned.Cookies, &copy)
		}
	}
	return &cloned
}

type hongguoQRJar struct {
	mu     sync.Mutex
	jar    *cookiejar.Jar
	values map[string]http.Cookie
	closed bool
}

func newHongguoQRJar() *hongguoQRJar {
	jar, _ := cookiejar.New(&cookiejar.Options{PublicSuffixList: publicsuffix.List})
	return &hongguoQRJar{jar: jar, values: map[string]http.Cookie{}}
}

func (jar *hongguoQRJar) Cookies(address *url.URL) []*http.Cookie {
	jar.mu.Lock()
	defer jar.mu.Unlock()
	if jar.closed {
		return nil
	}
	return jar.jar.Cookies(address)
}

func (jar *hongguoQRJar) SetCookies(address *url.URL, cookies []*http.Cookie) {
	jar.mu.Lock()
	defer jar.mu.Unlock()
	if jar.closed {
		return
	}
	jar.jar.SetCookies(address, cookies)
	if address.Scheme != "https" || address.Hostname() != "hongguoduanju.com" {
		return
	}
	for _, cookie := range cookies {
		if cookie == nil || !hongguoQRLoginCookies[cookie.Name] || cookie.Valid() != nil {
			continue
		}
		domain := strings.ToLower(strings.TrimPrefix(cookie.Domain, "."))
		if domain != "" && domain != "hongguoduanju.com" {
			continue
		}
		saved := *cookie
		saved.Domain = "hongguoduanju.com"
		if saved.Path == "" || saved.Path[0] != '/' {
			saved.Path = "/"
			if index := strings.LastIndex(address.Path, "/"); index > 0 {
				saved.Path = address.Path[:index]
			}
		}
		key := saved.Path + "\x00" + saved.Name
		if saved.MaxAge < 0 || saved.MaxAge == 0 && !saved.Expires.IsZero() && !saved.Expires.After(time.Now()) {
			delete(jar.values, key)
			continue
		}
		if saved.MaxAge > 0 {
			saved.Expires = time.Now().Add(time.Duration(saved.MaxAge) * time.Second)
		}
		jar.values[key] = saved
	}
}

func (jar *hongguoQRJar) close() {
	jar.mu.Lock()
	defer jar.mu.Unlock()
	jar.closed = true
	jar.values = nil
	jar.jar = nil
}

func (jar *hongguoQRJar) credentials(token string) (*hongguoAccountCredentials, error) {
	jar.mu.Lock()
	defer jar.mu.Unlock()
	if jar.closed {
		return nil, errors.New("二维码会话已取消")
	}
	if !hongguoQRHeaderValue(token) {
		return nil, errors.New("手机确认会话不可用，请重新获取二维码")
	}
	values := map[string]http.Cookie{}
	for _, cookie := range jar.values {
		if !cookie.Expires.IsZero() && !cookie.Expires.After(time.Now()) {
			continue
		}
		if !hongguoQRHeaderValue(cookie.Value) {
			return nil, errors.New("手机确认会话不可用，请重新获取二维码")
		}
		if previous, exists := values[cookie.Name]; exists && previous.Value != cookie.Value {
			return nil, errors.New("手机确认返回了冲突的会话，请重新获取二维码")
		}
		values[cookie.Name] = cookie
	}
	if values["sessionid"].Value == "" && values["sessionid_ss"].Value == "" && values["sid_tt"].Value == "" {
		return nil, errors.New("手机确认尚未返回完整会话，请重新获取二维码")
	}
	names := make([]string, 0, len(values))
	for name := range values {
		names = append(names, name)
	}
	sort.Strings(names)
	result := &hongguoAccountCredentials{Token: token}
	for _, name := range names {
		cookie := values[name]
		result.Cookies = append(result.Cookies, &cookie)
	}
	return result, nil
}

func hongguoQRHeaderValue(value string) bool {
	if len(value) > 8192 {
		return false
	}
	for _, character := range value {
		if character < 33 || character > 126 || character == ';' {
			return false
		}
	}
	return true
}

type hongguoQR struct {
	mu              sync.Mutex
	client          *http.Client
	jar             *hongguoQRJar
	id              string
	text            string
	token           string
	phase           string
	message         string
	scanExpires     time.Time
	handoffExpires  time.Time
	nextPoll        time.Time
	polls           int
	transientErrors int
	started         bool
	closed          bool
	pending         bool
	cancel          context.CancelFunc
	account         *hongguoAccountCredentials
}

func newHongguoQR() *hongguoQR {
	jar := newHongguoQRJar()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	client := &http.Client{
		Jar: jar, Timeout: 15 * time.Second, Transport: transport,
		CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &hongguoQR{client: client, jar: jar, phase: "idle"}
}

func hongguoQRParameters() url.Values {
	return url.Values{
		"aid": {"513755"}, "language": {"zh"}, "account_sdk_source": {"web"},
		"is_from_ttaccountsdk": {"1"}, "passport_jssdk_version": {"2.4.13"},
		"passport_jssdk_type": {"normal"}, "p_js_v": {"2.4.13"},
		"p_js_t": {"pro"}, "p_zt": {"3.3.5"}, "p_ver": {"1.0.29"},
		"p_bd": {"1.0.1.20"}, "request_host": {url.QueryEscape(hongguoQROrigin)},
		"next": {hongguoQROrigin}, "ts": {strconv.FormatInt(time.Now().UnixMilli(), 10)},
	}
}

func (qr *hongguoQR) closeLocked(phase, message string) {
	qr.phase, qr.message, qr.closed = phase, message, true
	qr.text, qr.token, qr.account = "", "", nil
	if qr.cancel != nil {
		qr.cancel()
	}
	qr.jar.close()
	qr.client.CloseIdleConnections()
}

func (qr *hongguoQR) expireLocked() {
	if qr.closed {
		return
	}
	deadline := qr.scanExpires
	if !qr.handoffExpires.IsZero() {
		deadline = qr.handoffExpires
	}
	if !deadline.IsZero() && !time.Now().Before(deadline) {
		qr.closeLocked("expired", "二维码已失效，请重新获取")
	}
}

func (qr *hongguoQR) snapshotLocked() map[string]any {
	qr.expireLocked()
	expires := 0
	if !qr.closed && !qr.scanExpires.IsZero() {
		expires = max(0, int(time.Until(qr.scanExpires).Seconds()))
	}
	result := map[string]any{
		"phase": qr.phase, "authenticated": false, "identityVerified": qr.account != nil,
		"expiresIn": expires, "retryAfterMs": max(int64(0), time.Until(qr.nextPoll).Milliseconds()),
		"pending": qr.pending, "message": qr.message,
	}
	if qr.id != "" {
		result["sessionId"] = qr.id
	}
	if qr.account != nil {
		result["displayName"] = qr.account.Name
	}
	return result
}

func (qr *hongguoQR) Snapshot() map[string]any {
	qr.mu.Lock()
	defer qr.mu.Unlock()
	return qr.snapshotLocked()
}

func (qr *hongguoQR) Cancel(sessionID string) bool {
	qr.mu.Lock()
	defer qr.mu.Unlock()
	if sessionID != "" && sessionID != qr.id || qr.phase == "committed" {
		return false
	}
	qr.closeLocked("cancelled", "")
	return true
}

func (qr *hongguoQR) Credentials(sessionID string) (*hongguoAccountCredentials, error) {
	qr.mu.Lock()
	defer qr.mu.Unlock()
	qr.expireLocked()
	if qr.closed || sessionID == "" || sessionID != qr.id || qr.account == nil {
		return nil, errors.New("扫码身份尚未确认，请完成当前二维码登录")
	}
	return cloneHongguoAccountCredentials(qr.account), nil
}

func (qr *hongguoQR) Commit(sessionID string, persist func(*hongguoAccountCredentials) error) error {
	qr.mu.Lock()
	defer qr.mu.Unlock()
	qr.expireLocked()
	if qr.closed || sessionID == "" || sessionID != qr.id || qr.account == nil {
		return errors.New("扫码身份尚未确认，请完成当前二维码登录")
	}
	if persist == nil {
		return errors.New("红果登录信息尚未保存，请重试")
	}
	if err := persist(cloneHongguoAccountCredentials(qr.account)); err != nil {
		return err
	}
	qr.closeLocked("committed", "")
	return nil
}
func (qr *hongguoQR) Start(ctx context.Context) (map[string]any, error) {
	qr.mu.Lock()
	if qr.started || qr.closed {
		qr.mu.Unlock()
		return nil, errors.New("请使用当前二维码，或取消后重新获取")
	}
	work, cancel := context.WithCancel(ctx)
	qr.started, qr.pending, qr.phase, qr.cancel = true, true, "creating", cancel
	qr.mu.Unlock()
	defer cancel()
	value, _, err := qr.get(work, "/passport/web/get_qrcode/", hongguoQRParameters())
	qr.mu.Lock()
	defer qr.mu.Unlock()
	qr.pending, qr.cancel = false, nil
	if qr.closed {
		return nil, errors.New("二维码会话已取消")
	}
	if err != nil {
		qr.closeLocked("error", "二维码未能创建，请重新获取")
		return nil, errors.New(qr.message)
	}
	data, ok := hongguoQRSuccess(value)
	if !ok {
		qr.closeLocked("error", "服务暂未提供可用二维码，请稍后重试")
		return nil, errors.New(qr.message)
	}
	token, tokenOK := data["token"].(string)
	text, textOK := data["qrcode_index_url"].(string)
	address, parseErr := url.Parse(text)
	if !tokenOK || !textOK || !hongguoQRText(token, 8192) || !hongguoQRText(text, 8192) ||
		parseErr != nil || address.Scheme != "https" || address.Hostname() == "" || address.User != nil {
		qr.closeLocked("error", "二维码信息不完整，请稍后重试")
		return nil, errors.New(qr.message)
	}
	duration := int64(180)
	if supplied, present := data["expire_time"]; present {
		number, valid := supplied.(json.Number)
		if !valid {
			qr.closeLocked("error", "二维码有效期不可用，请重新获取")
			return nil, errors.New(qr.message)
		}
		duration, err = number.Int64()
		if err != nil {
			duration = 0
		}
	}
	if duration > 1_000_000_000 {
		duration -= time.Now().Unix()
	}
	if duration < 1 || duration > 600 {
		qr.closeLocked("error", "二维码有效期不可用，请重新获取")
		return nil, errors.New(qr.message)
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		qr.closeLocked("error", "二维码会话未能创建，请重新获取")
		return nil, errors.New(qr.message)
	}
	qr.id, qr.text, qr.token = hex.EncodeToString(random[:]), text, token
	qr.scanExpires, qr.phase = time.Now().Add(time.Duration(duration)*time.Second), "waiting-for-scan"
	result := qr.snapshotLocked()
	result["qrText"] = qr.text
	return result, nil
}

func (qr *hongguoQR) Poll(ctx context.Context, sessionID string) (map[string]any, error) {
	qr.mu.Lock()
	qr.expireLocked()
	if qr.closed {
		result := qr.snapshotLocked()
		qr.mu.Unlock()
		return result, nil
	}
	if sessionID == "" || sessionID != qr.id {
		qr.mu.Unlock()
		return nil, errors.New("二维码会话已更新，请使用当前二维码")
	}
	if qr.pending || qr.account != nil || time.Now().Before(qr.nextPoll) {
		result := qr.snapshotLocked()
		qr.mu.Unlock()
		return result, nil
	}
	if qr.polls >= 300 {
		qr.closeLocked("expired", "二维码已失效，请重新获取")
		result := qr.snapshotLocked()
		qr.mu.Unlock()
		return result, nil
	}
	work, cancel := context.WithTimeout(ctx, 45*time.Second)
	qr.pending, qr.cancel = true, cancel
	qr.polls++
	qr.nextPoll = time.Now().Add(2 * time.Second)
	parameters := hongguoQRParameters()
	parameters.Set("token", qr.token)
	qr.mu.Unlock()
	defer cancel()

	value, transient, err := qr.get(work, "/passport/web/check_qrconnect/", parameters)
	qr.mu.Lock()
	qr.expireLocked()
	if qr.closed {
		qr.pending, qr.cancel = false, nil
		result := qr.snapshotLocked()
		qr.mu.Unlock()
		return result, nil
	}
	if err != nil {
		if transient {
			qr.transientErrors++
			if qr.transientErrors < 3 {
				qr.nextPoll = time.Now().Add(time.Duration(5*qr.transientErrors) * time.Second)
				qr.message = "扫码查询连接暂时中断，正在保留当前二维码重试"
			} else {
				qr.closeLocked("error", "扫码查询连续失败，请稍后重新获取二维码")
			}
		} else {
			qr.closeLocked("error", "二维码状态查询失败，请重新获取")
		}
		qr.pending, qr.cancel = false, nil
		result := qr.snapshotLocked()
		qr.mu.Unlock()
		return result, nil
	}
	data, valid := hongguoQRSuccess(value)
	var callback string
	if valid {
		switch data["status"] {
		case "new":
			if qr.phase != "waiting-for-confirmation" {
				qr.phase = "waiting-for-scan"
			}
		case "scanned":
			qr.phase = "waiting-for-confirmation"
		case "expired":
			qr.closeLocked("expired", "二维码已失效，请重新获取")
		case "confirmed":
			callback, valid = data["redirect_url"].(string)
			if valid {
				valid = hongguoQRCallback(callback)
			}
			if valid {
				qr.phase = "exchanging"
			}
		default:
			valid = false
		}
	}
	if !valid {
		qr.closeLocked("error", "二维码状态暂不可用，请重新获取")
	}
	if !qr.closed {
		qr.transientErrors, qr.message = 0, ""
	}
	if qr.closed || callback == "" {
		qr.pending, qr.cancel = false, nil
		result := qr.snapshotLocked()
		qr.mu.Unlock()
		return result, nil
	}
	qr.mu.Unlock()

	credentials, err := qr.exchange(work, callback)
	qr.mu.Lock()
	qr.expireLocked()
	if !qr.closed && err != nil {
		qr.closeLocked("error", "手机确认续接未完成，请重新获取二维码")
	}
	if !qr.closed {
		qr.handoffExpires = time.Now().Add(5 * time.Minute)
		qr.phase = "verifying-identity"
	}
	closed := qr.closed
	qr.mu.Unlock()
	var account *hongguoAccountCredentials
	if !closed {
		account, err = qr.verify(work, credentials)
	}
	qr.mu.Lock()
	defer qr.mu.Unlock()
	qr.expireLocked()
	if !qr.closed {
		if err != nil || account == nil {
			qr.closeLocked("error", "扫码身份核对未完成，请重新获取二维码")
		} else {
			qr.account, qr.phase = account, "identity-verified"
		}
	}
	qr.pending, qr.cancel = false, nil
	return qr.snapshotLocked(), nil
}

func hongguoQRText(value string, limit int) bool {
	if value == "" || !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit {
		return false
	}
	for _, character := range value {
		if character < 32 || character == 127 {
			return false
		}
	}
	return true
}

func hongguoQRSuccess(value map[string]any) (map[string]any, bool) {
	data, ok := value["data"].(map[string]any)
	if !ok || value["message"] != "success" {
		return nil, false
	}
	for _, object := range []map[string]any{value, data} {
		for _, key := range []string{"code", "error_code"} {
			if raw, exists := object[key]; exists {
				number, numeric := raw.(json.Number)
				if !numeric || number.String() != "0" {
					return nil, false
				}
			}
		}
	}
	return data, true
}

func hongguoQRCallback(value string) bool {
	if !hongguoQRText(value, 8192) || strings.ContainsAny(value, " \t\r\n") {
		return false
	}
	address, err := url.Parse(value)
	return err == nil && address.Scheme == "https" && address.Hostname() == "hongguoduanju.com" &&
		(address.Port() == "" || address.Port() == "443") && address.User == nil && address.Fragment == ""
}

func (qr *hongguoQR) request(ctx context.Context, address string, accept string) (*http.Response, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return nil, errors.New("二维码请求不可用")
	}
	request.Header.Set("Accept", accept)
	request.Header.Set("Referer", hongguoQROrigin+"/")
	request.Header.Set("User-Agent", hongguoQRUserAgent)
	response, err := qr.client.Do(request)
	if err != nil {
		return nil, err
	}
	return response, nil
}

func (qr *hongguoQR) get(ctx context.Context, path string, parameters url.Values) (map[string]any, bool, error) {
	if path != "/passport/web/get_qrcode/" && path != "/passport/web/check_qrconnect/" && path != "/passport/account/info/v2/" {
		return nil, false, errors.New("二维码请求不可用")
	}
	response, err := qr.request(ctx, hongguoQROrigin+path+"?"+parameters.Encode(), "application/json")
	if err != nil {
		var temporary net.Error
		transient := path == "/passport/web/check_qrconnect/" && errors.As(err, &temporary) &&
			(temporary.Timeout() || temporary.Temporary())
		return nil, transient, errors.New("二维码连接暂时中断")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		transient := false
		if path == "/passport/web/check_qrconnect/" {
			switch response.StatusCode {
			case 408, 500, 502, 503, 504:
				transient = true
			}
		}
		return nil, transient, errors.New("二维码暂时不可用")
	}
	content, err := io.ReadAll(io.LimitReader(response.Body, hongguoQRMaximumBody+1))
	if err != nil || len(content) == 0 || len(content) > hongguoQRMaximumBody {
		return nil, false, errors.New("二维码响应不可用")
	}
	var result map[string]any
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.UseNumber()
	if decoder.Decode(&result) != nil || result == nil {
		return nil, false, errors.New("二维码响应不可用")
	}
	if decoder.Decode(new(any)) != io.EOF {
		return nil, false, errors.New("二维码响应不可用")
	}
	return result, false, nil
}

func (qr *hongguoQR) exchange(ctx context.Context, address string) (*hongguoAccountCredentials, error) {
	if !hongguoQRCallback(address) {
		return nil, errors.New("手机确认地址不可用")
	}
	seen := map[string]bool{}
	token := ""
	for step := 0; step < 4; step++ {
		if seen[address] {
			return nil, errors.New("手机确认续接未完成")
		}
		seen[address] = true
		response, err := qr.request(ctx, address, "*/*")
		if err != nil {
			return nil, errors.New("手机确认续接未完成")
		}
		response.Body.Close()
		if issued, present := response.Header["X-Tt-Token"]; present {
			if len(issued) != 1 || !hongguoQRHeaderValue(issued[0]) {
				return nil, errors.New("手机确认会话不可用")
			}
			token = issued[0]
		}
		switch response.StatusCode {
		case 301, 302, 303, 307, 308:
			destination := response.Header.Get("Location")
			if destination == "" {
				return nil, errors.New("手机确认续接未完成")
			}
			current, _ := url.Parse(address)
			next, err := url.Parse(destination)
			if err != nil {
				return nil, errors.New("手机确认地址不可用")
			}
			address = current.ResolveReference(next).String()
			if !hongguoQRCallback(address) {
				return nil, errors.New("手机确认地址不可用")
			}
		case 200, 204:
			return qr.jar.credentials(token)
		default:
			return nil, errors.New("手机确认续接未完成")
		}
	}
	return nil, errors.New("手机确认续接未完成")
}

func (qr *hongguoQR) verify(ctx context.Context, credentials *hongguoAccountCredentials) (*hongguoAccountCredentials, error) {
	if credentials == nil {
		return nil, errors.New("扫码身份尚未确认")
	}
	value, _, err := qr.get(ctx, "/passport/account/info/v2/", hongguoQRParameters())
	if err != nil {
		return nil, errors.New("扫码身份尚未确认")
	}
	data, valid := hongguoQRSuccess(value)
	if !valid {
		return nil, errors.New("扫码账号资料不完整")
	}
	id := ""
	for _, key := range []string{"user_id_str", "user_id"} {
		if raw, exists := data[key]; exists {
			var value string
			switch raw := raw.(type) {
			case string:
				value = raw
			case json.Number:
				value = raw.String()
			}
			if !hongguoQRIdentity.MatchString(value) || id != "" && id != value {
				return nil, errors.New("扫码账号身份不一致")
			}
			id = value
		}
	}
	if id == "" {
		return nil, errors.New("扫码账号身份无效")
	}
	name, _ := data["screen_name"].(string)
	if name == "" {
		name, _ = data["name"].(string)
	}
	if !hongguoQRText(name, 100) {
		name = "红果账号"
	}
	account, err := qr.jar.credentials(credentials.Token)
	if err != nil {
		return nil, err
	}
	account.UID, account.Name = id, name
	return account, nil
}
