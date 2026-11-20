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
	"io"
	"math/bits"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode"
)

type hongguoAccountDevice struct {
	Params      map[string]string `json:"params"`
	Cookies     map[string]string `json:"cookies"`
	UserAgent   string            `json:"userAgent"`
	DeviceToken string            `json:"deviceToken,omitempty"`
}

const hongguoAccountLikesURL = "https://api5-normal-lq.fqnovel.com/reading/ugc/action/mget/v"
const hongguoAccountMaxResponse = 2 * 1024 * 1024

var errHongguoAccountSessionExpired = errors.New("红果登录已过期，请退出后重新扫码登录")

var hongguoAccountDeviceIDPattern = regexp.MustCompile(`^[1-9][0-9]{4,29}$`)
var hongguoAccountSeriesIDPattern = regexp.MustCompile(`^[1-9][0-9]{7,23}$`)
var hongguoAccountVersionPattern = regexp.MustCompile(`^[0-9]{1,3}(\.[0-9]{1,3}){2,3}$`)
var hongguoAccountRegionPattern = regexp.MustCompile(`^[a-z]{2}(-[a-z0-9]{1,10})?$`)

func validHongguoAccountDevice(device *hongguoAccountDevice) bool {
	if device == nil || len(device.Params) > 100 || len(device.Cookies) > 16 {
		return false
	}
	if device.Params["aid"] != "8662" || !hongguoAccountDeviceIDPattern.MatchString(device.Params["device_id"]) || !hongguoAccountDeviceIDPattern.MatchString(device.Params["iid"]) || !hongguoAccountVersionPattern.MatchString(device.Params["version_name"]) {
		return false
	}
	if !hongguoAccountHeaderValue(device.UserAgent, 1024) {
		return false
	}
	for key, value := range device.Params {
		if len(key) > 128 || len(value) > 8192 || strings.ContainsAny(key+value, "\r\n\x00") {
			return false
		}
	}
	return true
}

func hongguoAccountHeaderValue(value string, limit int) bool {
	if len(value) == 0 || len(value) > limit {
		return false
	}
	for _, r := range value {
		if r < 32 || r > 126 {
			return false
		}
	}
	return true
}

func hongguoAccountHelios(seconds uint32, nonce []byte) string {
	digest := md5.Sum(append(append([]byte(nil), nonce...), "8662"...))
	text := []byte(hex.EncodeToString(digest[:]))
	left, right := binary.LittleEndian.Uint64(text), binary.LittleEndian.Uint64(text[8:])
	queue := []uint64{binary.LittleEndian.Uint64(text[16:]), binary.LittleEndian.Uint64(text[24:])}
	var keys [34]uint64
	for i := range keys {
		keys[i] = left
		mixed := (bits.RotateLeft64(right, -8) + left) ^ uint64(i)
		queue = append(queue, mixed)
		left = mixed ^ bits.RotateLeft64(left, 3)
		right, queue = queue[0], queue[1:]
	}
	data := hongguoCommentPad([]byte(strconv.FormatUint(uint64(seconds), 10) + "-1588093228-8662"))
	for offset := 0; offset < len(data); offset += 16 {
		left, right := binary.LittleEndian.Uint64(data[offset:]), binary.LittleEndian.Uint64(data[offset+8:])
		for _, key := range keys {
			right = key ^ (left + bits.RotateLeft64(right, -8))
			left = right ^ bits.RotateLeft64(left, 3)
		}
		binary.LittleEndian.PutUint64(data[offset:], left)
		binary.LittleEndian.PutUint64(data[offset+8:], right)
	}
	return base64.StdEncoding.EncodeToString(append(append([]byte(nil), nonce...), data...))
}

func prepareHongguoAccountLikes(ctx context.Context, credentials *hongguoAccountCredentials, device *hongguoAccountDevice, offset int) (*http.Request, error) {
	if offset < 0 || offset > 2000 {
		return nil, errors.New("点赞片单分页参数无效")
	}
	if credentials == nil || !hongguoAccountDeviceIDPattern.MatchString(credentials.UID) || len(credentials.Cookies) > 80 {
		return nil, errors.New("请先登录红果账号")
	}
	if !validHongguoAccountDevice(device) {
		return nil, errors.New("红果账号设备尚未准备完成，请重新登录")
	}
	query := url.Values{}
	for _, key := range strings.Fields("iid device_id ac channel aid app_name version_code version_name device_platform os ssmix device_type device_brand language os_api os_version manifest_version_code resolution dpi update_version_code host_abi dragon_device_type pv_player compliance_status need_personal_recommend player_so_load is_android_pad_screen rom_version cdid") {
		if value, exists := device.Params[key]; exists {
			query.Set(key, value)
		}
	}
	for key, value := range map[string]string{"dragon_device_type": "phone", "compliance_status": "0", "player_so_load": "1", "is_android_pad_screen": "0", "pv_player": device.Params["version_code"]} {
		if !query.Has(key) {
			query.Set(key, value)
		}
	}
	query.Set("object_type_list", "6,15,10")
	query.Set("action_type", "3")
	query.Set("offset", strconv.Itoa(offset))
	query.Set("count", "100")
	now := time.Now()
	if now.Unix() < 946684800 || now.Unix() > 4102444800 {
		return nil, errors.New("系统时间异常，请校准时间后重试")
	}
	seconds := uint32(now.Unix())
	query.Set("ts", strconv.FormatUint(uint64(seconds), 10))
	var raw string
	for n := int64(0); n < 256; n++ {
		query.Set("_rticket", strconv.FormatInt(now.UnixMilli()+n, 10))
		candidate := query.Encode()
		if (hongguoAccountDigestSelector(hongguoSM3([]byte(candidate)), [16]byte{}, seconds)&15)%3 == 2 {
			raw = candidate
			break
		}
	}
	if raw == "" {
		return nil, errors.New("红果账号请求准备失败，请稍后重试")
	}
	var random [16]byte
	if _, err := rand.Read(random[:]); err != nil {
		return nil, errors.New("红果账号请求准备失败，请稍后重试")
	}
	nonces := [3]uint32{binary.LittleEndian.Uint32(random[:]), binary.LittleEndian.Uint32(random[4:]), binary.LittleEndian.Uint32(random[8:])}
	signature := hongguoAccountMedusa([]byte(raw), seconds, device.Params["device_id"], device.Params["version_name"], nonces)
	if signature == "" {
		return nil, errors.New("红果账号请求准备失败，请稍后重试")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, hongguoAccountLikesURL+"?"+raw, nil)
	if err != nil {
		return nil, errors.New("点赞片单请求格式异常")
	}
	headers := map[string]string{
		"User-Agent":                device.UserAgent,
		"Accept":                    "application/json; charset=utf-8,application/x-protobuf",
		"Accept-Encoding":           "gzip",
		"Content-Type":              "application/x-www-form-urlencoded; charset=UTF-8",
		"X-Medusa":                  signature,
		"X-Helios":                  hongguoAccountHelios(seconds, random[12:]),
		"X-Khronos":                 strconv.FormatUint(uint64(seconds), 10),
		"X-SS-DP":                   "8662",
		"X-SS-Req-Ticket":           query.Get("_rticket"),
		"X-TT-Request-Tag":          "t=0;n=1",
		"Passport-SDK-Version":      "5051452",
		"Sdk-Version":               "2",
		"X-XS-From-Web":             "0",
		"X-VC-Bdturing-SDK-Version": "4.0.3.cn",
		"lc":                        "101",
		"X-TT-Store-Region":         "cn",
		"X-TT-Store-Region-Src":     "did",
	}
	cookies := make(map[string]string, len(device.Cookies)+len(credentials.Cookies))
	for key, value := range device.Cookies {
		switch key {
		case "odin_tt", "ttreq", "store-region", "store-region-src":
			cookies[key] = value
		}
	}
	authenticated := false
	for _, cookie := range credentials.Cookies {
		if cookie == nil || cookie.MaxAge < 0 || !cookie.Expires.IsZero() && !cookie.Expires.After(now) {
			continue
		}
		if cookie.Name == "" || len(cookie.Name) > 128 || !hongguoAccountHeaderValue(cookie.Value, 8192) || cookie.Valid() != nil {
			return nil, errors.New("红果登录会话格式异常，请重新登录")
		}
		cookies[cookie.Name] = cookie.Value
		if cookie.Name == "sessionid" || cookie.Name == "sessionid_ss" || cookie.Name == "sid_tt" {
			authenticated = true
		}
	}
	if credentials.Token != "" {
		if !hongguoAccountHeaderValue(credentials.Token, 8192) {
			return nil, errors.New("红果登录会话格式异常，请重新登录")
		}
		headers["X-TT-Token"] = credentials.Token
		authenticated = true
	}
	if !authenticated {
		return nil, errHongguoAccountSessionExpired
	}
	if region := cookies["store-region"]; hongguoAccountRegionPattern.MatchString(region) {
		headers["X-TT-Store-Region"] = region
		if source := cookies["store-region-src"]; source == "did" || source == "uid" {
			headers["X-TT-Store-Region-Src"] = source
		}
	}
	if csrf := cookies["passport_csrf_token"]; csrf != "" {
		headers["X-TT-Passport-Csrf-Token"] = csrf
	}
	for key, value := range headers {
		request.Header.Set(key, value)
	}
	for key, value := range cookies {
		cookie := &http.Cookie{Name: key, Value: value}
		if !hongguoAccountHeaderValue(value, 8192) || cookie.Valid() != nil {
			return nil, errors.New("红果登录会话格式异常，请重新登录")
		}
		request.AddCookie(cookie)
	}
	return request, nil
}

func fetchHongguoAccountLikes(ctx context.Context, credentials *hongguoAccountCredentials, device *hongguoAccountDevice, offset int) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	request, err := prepareHongguoAccountLikes(ctx, credentials, device, offset)
	if err != nil {
		return nil, err
	}
	transport := &http.Transport{
		DialContext:           (&net.Dialer{Timeout: 8 * time.Second}).DialContext,
		TLSHandshakeTimeout:   8 * time.Second,
		ResponseHeaderTimeout: 20 * time.Second,
		DisableKeepAlives:     true,
	}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 30 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	response, err := client.Do(request)
	if err != nil {
		return nil, errors.New("点赞片单连接失败，请稍后重试；原列表和登录已保留")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, errors.New("点赞片单服务暂不可用，请稍后重试；原列表和登录已保留")
	}
	var reader io.Reader = response.Body
	if strings.EqualFold(response.Header.Get("Content-Encoding"), "gzip") {
		gzipReader, err := gzip.NewReader(response.Body)
		if err != nil {
			return nil, errors.New("点赞片单响应格式异常，原列表和登录已保留")
		}
		defer gzipReader.Close()
		reader = gzipReader
	} else if encoding := response.Header.Get("Content-Encoding"); encoding != "" && !strings.EqualFold(encoding, "identity") {
		return nil, errors.New("点赞片单响应格式异常，原列表和登录已保留")
	}
	payload, err := io.ReadAll(io.LimitReader(reader, hongguoAccountMaxResponse+1))
	if err != nil || len(payload) == 0 || len(payload) > hongguoAccountMaxResponse {
		return nil, errors.New("点赞片单响应未完整接收，原列表和登录已保留")
	}
	var root map[string]any
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	if decoder.Decode(&root) != nil || root == nil {
		return nil, errors.New("点赞片单响应格式异常，原列表和登录已保留")
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return nil, errors.New("点赞片单响应格式异常，原列表和登录已保留")
	}
	return parseHongguoAccountLikes(root, offset)
}

func hongguoAccountInteger(value any) (int64, bool) {
	switch number := value.(type) {
	case json.Number:
		result, err := number.Int64()
		return result, err == nil
	case int:
		return int64(number), true
	case int64:
		return number, true
	default:
		return 0, false
	}
}

func hongguoAccountID(value any) string {
	if text, ok := value.(string); ok && hongguoAccountSeriesIDPattern.MatchString(text) {
		return text
	}
	if number, ok := value.(json.Number); ok && hongguoAccountSeriesIDPattern.MatchString(string(number)) {
		return string(number)
	}
	return ""
}

func hongguoAccountText(value any, limit int) string {
	text, ok := value.(string)
	if !ok {
		return ""
	}
	text = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) && r != '\n' && r != '\t' {
			return -1
		}
		return r
	}, strings.TrimSpace(text))
	characters := []rune(text)
	if len(characters) > limit {
		characters = characters[:limit]
	}
	return string(characters)
}

func hongguoAccountField(primary, fallback map[string]any, key, fallbackKey string) any {
	value, exists := primary[key]
	if !exists || value == nil {
		return fallback[fallbackKey]
	}
	if text, ok := value.(string); ok && strings.TrimSpace(text) == "" {
		return fallback[fallbackKey]
	}
	return value
}

func hongguoAccountContentKind(video, detail map[string]any) string {
	medium, valid := hongguoAccountInteger(video["content_type"])
	if !valid {
		return "unknown"
	}
	if medium == 1 {
		return "real"
	}
	if medium == 1004 {
		if video["video_category_type"] == "ai_video" || detail["video_category_type"] == "ai_video" {
			return "ai"
		}
		return "animated"
	}
	return "unknown"
}

func mergeHongguoAccountContentKind(previous, current string) string {
	if previous == current {
		return current
	}
	if previous == "unknown" || current == "unknown" {
		return "unknown"
	}
	if (previous == "animated" || previous == "ai") && (current == "animated" || current == "ai") {
		return "ai"
	}
	return "unknown"
}

func parseHongguoAccountLikes(root map[string]any, offset int) (map[string]any, error) {
	data, valid := root["data"].(map[string]any)
	codes := []any{root["code"]}
	for _, container := range []map[string]any{root, data} {
		if value, exists := container["error_code"]; exists {
			codes = append(codes, value)
		}
	}
	invalidCode, rejected := false, false
	for _, raw := range codes {
		var code string
		switch value := raw.(type) {
		case string:
			code = value
		case json.Number:
			code = string(value)
		case int:
			code = strconv.Itoa(value)
		default:
			invalidCode = true
			continue
		}
		if code == "1009" {
			return nil, errHongguoAccountSessionExpired
		}
		if code != "0" {
			rejected = true
		}
	}
	if invalidCode {
		return nil, errors.New("点赞片单返回状态未确认，原列表和登录已保留")
	}
	if rejected {
		return nil, errors.New("红果暂未接受点赞片单请求，原列表和登录已保留")
	}
	if !valid {
		return nil, errors.New("点赞片单资料不完整，原列表和登录已保留")
	}
	rows, valid := data["mixed_data_list"].([]any)
	more, validMore := data["has_more"].(bool)
	next, validNext := hongguoAccountInteger(data["next_offset"])
	if !valid || len(rows) > 200 || !validMore {
		return nil, errors.New("点赞片单格式尚未识别，原列表已保留")
	}
	if more && (len(rows) == 0 || !validNext || next <= int64(offset) || next > 2000) {
		return nil, errors.New("点赞片单分页没有前进，请重新刷新")
	}
	if !more {
		next = int64(offset)
	}
	items := make([]map[string]any, 0, len(rows))
	indices := map[string]int{}
	unsupported := 0
	for _, value := range rows {
		row, valid := value.(map[string]any)
		if !valid {
			return nil, errors.New("点赞片单条目格式异常，原列表已保留")
		}
		video, valid := row["video_data"].(map[string]any)
		if !valid {
			unsupported++
			continue
		}
		detail, _ := video["video_detail"].(map[string]any)
		id := firstNonEmpty(hongguoAccountID(detail["series_id_str"]), hongguoAccountID(video["series_id"]), hongguoAccountID(detail["series_id"]))
		if id == "" {
			unsupported++
			continue
		}
		title := hongguoAccountText(hongguoAccountField(detail, video, "series_title", "title"), 200)
		if title == "" {
			return nil, errors.New("点赞片单剧名资料不完整，原列表已保留")
		}
		cover := hongguoAccountText(hongguoAccountField(detail, video, "series_cover", "cover"), 8192)
		if parsed, err := url.Parse(cover); err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
			cover = ""
		}
		count, validCount := hongguoAccountInteger(hongguoAccountField(detail, video, "episode_cnt", "episode_cnt"))
		if !validCount || count < 0 || count > 100000 {
			count = 0
		}
		intro := hongguoAccountText(hongguoAccountField(detail, video, "series_intro", "video_desc"), 4000)
		disabled, _ := video["disable_play"].(bool)
		kind := hongguoAccountContentKind(video, detail)
		item := map[string]any{
			"series":   map[string]any{"seriesId": id, "title": title, "cover": cover, "intro": intro, "episodeCount": count},
			"playable": !disabled, "online": true, "note": "", "contentKind": kind,
		}
		if index, duplicate := indices[id]; duplicate {
			previousKind, _ := items[index]["contentKind"].(string)
			item["contentKind"] = mergeHongguoAccountContentKind(previousKind, kind)
			previousPlayable, _ := items[index]["playable"].(bool)
			item["playable"] = previousPlayable || !disabled
			items[index] = item
		} else {
			indices[id] = len(items)
			items = append(items, item)
		}
	}
	return map[string]any{
		"kind": "likes", "items": items, "offset": offset, "nextOffset": next, "hasMore": more,
		"retrievedCount": len(rows), "unsupportedCount": unsupported, "online": false, "syncedAt": time.Now().Unix(),
	}, nil
}
