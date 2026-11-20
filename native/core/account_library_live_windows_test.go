package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

func accountLibraryReadLegacyDPAPI(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil || info.Size() <= 0 || info.Size() > 256*1024 {
		return nil, errors.New("fixed backup file unavailable")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("fixed backup file could not be read")
	}
	incoming := windows.DataBlob{Size: uint32(len(data)), Data: &data[0]}
	var outgoing windows.DataBlob
	if err := windows.CryptUnprotectData(&incoming, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &outgoing); err != nil {
		return nil, errors.New("fixed backup DPAPI decryption failed")
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(outgoing.Data)))
	return append([]byte(nil), unsafe.Slice(outgoing.Data, int(outgoing.Size))...), nil
}

func accountLibraryLiveShape(value any, depth int) any {
	if depth > 5 {
		return "nested"
	}
	switch value := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, item := range value {
			switch strings.ToLower(key) {
			case "token", "cookies", "uid", "user_id", "sessionid", "x_tt_token":
				continue
			}
			result[key] = accountLibraryLiveShape(item, depth+1)
		}
		return result
	case []any:
		result := map[string]any{"count": len(value)}
		if len(value) > 0 {
			result["sample"] = accountLibraryLiveShape(value[0], depth+1)
		}
		return result
	case json.Number:
		return "number"
	case string:
		return "string"
	case bool:
		return "bool"
	case nil:
		return "null"
	default:
		return "other"
	}
}

func accountLibraryLiveErrorReason(err error) string {
	if errors.Is(err, errNativeBuildSource) {
		return "source_guard"
	}
	if errors.Is(err, context.Canceled) {
		return "context_cancelled"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "context_timeout"
	}
	var backoff *requestBackoff
	if errors.As(err, &backoff) {
		return "source_backoff"
	}
	var network net.Error
	if errors.As(err, &network) {
		if network.Timeout() {
			return "network_timeout"
		}
		return "network_error"
	}
	text := err.Error()
	for _, test := range []struct{ needle, reason string }{
		{"HTTP", "http_status"}, {"接口暂不可用", "upstream_rejected"},
		{"格式", "response_format"}, {"未返回有效", "empty_response"},
		{"不匹配", "identity_mismatch"}, {"剧名", "missing_title"},
		{"编号无效", "invalid_identifier"}, {"保护", "credential_protection"},
	} {
		if strings.Contains(text, test.needle) {
			return test.reason
		}
	}
	return "unclassified_response_error"
}

func TestLiveHongguoAccountLibraryReadOnly(t *testing.T) {
	if os.Getenv("CHECK_LIVE_HONGGUO_LIBRARY") != "true" {
		t.Skip("CHECK_LIVE_HONGGUO_LIBRARY is not enabled")
	}
	backup := os.Getenv("HONGGUO_LIBRARY_BACKUP")
	if !filepath.IsAbs(backup) {
		t.Fatal("HONGGUO_LIBRARY_BACKUP must be an absolute fixed backup directory")
	}
	deviceBytes, err := accountLibraryReadLegacyDPAPI(filepath.Join(backup, "account-device.dpapi"))
	if err != nil {
		t.Fatal(err)
	}
	var savedDevice struct {
		Schema      int               `json:"schema"`
		Params      map[string]string `json:"params"`
		Cookies     map[string]string `json:"cookies"`
		UserAgent   string            `json:"user_agent"`
		DeviceToken string            `json:"device_token"`
	}
	if json.Unmarshal(deviceBytes, &savedDevice) != nil || savedDevice.Schema != 1 {
		t.Fatal("fixed device backup format invalid")
	}
	clear(deviceBytes)
	device := &hongguoAccountDevice{Params: savedDevice.Params, Cookies: savedDevice.Cookies, UserAgent: savedDevice.UserAgent, DeviceToken: savedDevice.DeviceToken}
	sessionBytes, err := accountLibraryReadLegacyDPAPI(filepath.Join(backup, "account-session.dpapi"))
	if err != nil {
		t.Fatal(err)
	}
	var savedSession struct {
		Version  int               `json:"version"`
		Remember bool              `json:"remember"`
		Created  float64           `json:"created"`
		UID      string            `json:"uid"`
		Token    string            `json:"token"`
		Cookies  map[string]string `json:"cookies"`
	}
	if json.Unmarshal(sessionBytes, &savedSession) != nil || savedSession.Version != 1 || !savedSession.Remember || float64(time.Now().Unix())-savedSession.Created < 0 || float64(time.Now().Unix())-savedSession.Created >= 30*86400 {
		t.Fatal("fixed session backup unavailable or expired")
	}
	clear(sessionBytes)
	credentials := &hongguoAccountCredentials{UID: savedSession.UID, Token: savedSession.Token}
	for key, value := range savedSession.Cookies {
		credentials.Cookies = append(credentials.Cookies, &http.Cookie{Name: key, Value: value})
	}
	if !validHongguoAccountDevice(device) {
		t.Fatal("fixed device backup is not accepted by current backend")
	}
	engine, err := newNativeEngine(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(engine.downloads.close)
	probeTimeout := 3 * time.Minute
	fullFavorites := os.Getenv("CHECK_FULL_HONGGUO_FAVORITES") == "true"
	if fullFavorites {
		probeTimeout = 5 * time.Minute
	}
	ctx, cancel := context.WithTimeout(context.Background(), probeTimeout)
	defer cancel()
	report := map[string]any{}
	failed := false
	for _, scenario := range []struct {
		kind   string
		offset int
	}{{"favorites", 0}, {"history", 0}, {"history", 500}} {
		key := scenario.kind
		if scenario.offset > 0 {
			key += "Page2"
		}
		entry := map[string]any{}
		report[key] = entry
		request, err := prepareHongguoAccountLibrary(ctx, credentials, device, scenario.kind, scenario.offset)
		if err != nil {
			entry["error"] = err.Error()
			failed = true
			continue
		}
		root, err := fetchHongguoAccountGet(request, hongguoAccountLibraryLabel(scenario.kind))
		if err != nil {
			entry["error"] = err.Error()
			failed = true
			continue
		}
		entry["shape"] = accountLibraryLiveShape(root, 0)
		entry["code"] = root["code"]
		data, err := hongguoAccountLibraryData(root, scenario.kind)
		if err != nil {
			entry["error"] = err.Error()
			failed = true
			continue
		}
		for _, name := range []string{"next_offset", "has_more", "total"} {
			if value, exists := data[name]; exists {
				entry[name] = value
			}
		}
		if scenario.kind == "history" {
			page, err := parseHongguoAccountHistory(root, scenario.offset)
			if err != nil {
				entry["parseError"] = err.Error()
				failed = true
			} else {
				entry["normalizedCount"] = len(page["items"].([]map[string]any))
				entry["unsupportedCount"] = page["unsupportedCount"]
			}
		}
		rowsKey := "video_shelf_info"
		if scenario.kind == "history" {
			rowsKey = "data_list"
		}
		rows, _ := data[rowsKey].([]any)
		entry["rowCount"] = len(rows)
		if scenario.offset > 0 {
			continue
		}
		samples := make([]map[string]any, 0, 3)
		matches := 0
		for _, value := range rows {
			row, _ := value.(map[string]any)
			medium, validMedium := hongguoAccountFlexibleInteger(row["content_type"])
			if !validMedium || (medium != 1 && medium != 1004) {
				continue
			}
			id := firstNonEmpty(hongguoAccountID(row["series_id_str"]), hongguoAccountID(row["series_id"]), hongguoAccountID(row["book_id_str"]))
			if id == "" {
				continue
			}
			sample := map[string]any{}
			detail, err := engine.downloader.hongguoAccountLibraryMetadata(ctx, id)
			if err != nil {
				sample["metadataError"] = "详情读取失败"
				sample["metadataErrorType"] = fmt.Sprintf("%T", err)
				sample["metadataErrorReason"] = accountLibraryLiveErrorReason(err)
			} else {
				sample["shape"] = accountLibraryLiveShape(detail, 0)
				matched := firstNonEmpty(hongguoAccountID(detail["series_id_str"]), hongguoAccountID(detail["series_id"])) == id
				sample["identityMatches"] = matched
				sample["hasTitle"] = hongguoAccountLibrarySeries(id, detail) != nil
				if scenario.kind == "history" {
					sample["titleMatches"] = hongguoAccountText(detail["series_title"], 200) == hongguoAccountText(row["book_name"], 200)
				}
				if matched {
					matches++
				}
			}
			samples = append(samples, sample)
			if len(samples) == 3 {
				break
			}
		}
		entry["metadataSamples"] = samples
		entry["metadataIdentityMatches"] = matches
		if len(samples) != 3 || matches == 0 {
			failed = true
		}
		t.Logf("%s: rows=%d metadata requests=%d identity matches=%d", key, len(rows), len(samples), matches)
	}
	if fullFavorites {
		pages := make([]map[string]any, 0, 2)
		for _, offset := range []int{0, 20} {
			entry := map[string]any{"requestedOffset": offset}
			page, err := fetchHongguoAccountLibrary(ctx, credentials, device, "favorites", offset, engine.downloader.hongguoAccountLibraryMetadata)
			if err != nil {
				entry["error"] = "收藏分页读取失败"
				entry["errorType"] = fmt.Sprintf("%T", err)
				entry["errorReason"] = accountLibraryLiveErrorReason(err)
				failed = true
				pages = append(pages, entry)
				t.Logf("favorites full page offset=%d: request was not verified", offset)
				continue
			}
			items, valid := page["items"].([]map[string]any)
			if !valid {
				entry["error"] = "收藏分页归一化条目格式未确认"
				failed = true
			} else {
				entry["normalizedCount"] = len(items)
			}
			for _, name := range []string{"offset", "nextOffset", "hasMore", "total", "retrievedCount", "unsupportedCount"} {
				if value, exists := page[name]; exists {
					entry[name] = value
				}
			}
			pages = append(pages, entry)
			t.Logf("favorites full page offset=%d: normalized=%d unsupported=%v next=%v total=%v", offset, len(items), page["unsupportedCount"], page["nextOffset"], page["total"])
		}
		report["favoritesFullPages"] = pages
	}
	if reportPath := os.Getenv("HONGGUO_LIBRARY_REPORT"); reportPath != "" {
		if !filepath.IsAbs(reportPath) {
			t.Fatal("HONGGUO_LIBRARY_REPORT must be absolute")
		}
		payload, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		temporary, err := os.CreateTemp(filepath.Dir(reportPath), ".library-live-*.tmp")
		if err != nil {
			t.Fatal(err)
		}
		temporaryPath := temporary.Name()
		defer os.Remove(temporaryPath)
		if _, err := temporary.Write(payload); err != nil {
			temporary.Close()
			t.Fatal(err)
		}
		if err := temporary.Close(); err != nil {
			t.Fatal(err)
		}
		if err := os.Rename(temporaryPath, reportPath); err != nil {
			t.Fatal(err)
		}
	}
	if failed {
		t.Fatal("one or more list or metadata probes were not verified; see redacted schema report")
	}
}
