package core

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func accountLibraryFixture(t *testing.T, raw string) map[string]any {
	t.Helper()
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()
	var result map[string]any
	if err := decoder.Decode(&result); err != nil {
		t.Fatal(err)
	}
	return result
}

func accountLibraryTestSession() (*hongguoAccountCredentials, *hongguoAccountDevice) {
	return &hongguoAccountCredentials{UID: "123456789", Cookies: []*http.Cookie{{Name: "sessionid", Value: "synthetic_session"}}}, &hongguoAccountDevice{
		Params:    map[string]string{"aid": "8662", "device_id": "123456789012345", "iid": "123456789012346", "version_code": "73932", "version_name": "7.3.9.32"},
		UserAgent: "SyntheticTest/1", Cookies: map[string]string{},
	}
}

func TestHongguoAccountLibraryPreparedRequests(t *testing.T) {
	credentials, device := accountLibraryTestSession()
	for _, kind := range []string{"favorites", "history", "likes"} {
		var request *http.Request
		var err error
		if kind == "likes" {
			request, err = prepareHongguoAccountLikes(context.Background(), credentials, device, 500)
		} else {
			request, err = prepareHongguoAccountLibrary(context.Background(), credentials, device, kind, 500)
		}
		if err != nil {
			t.Fatal(err)
		}
		query := request.URL.Query()
		if request.Method != http.MethodGet || request.Header.Get("X-Medusa") == "" || request.Header.Get("X-Helios") == "" || request.Header.Get("Cookie") == "" {
			t.Fatalf("missing signed GET fields for %s", kind)
		}
		if kind == "favorites" && (request.URL.Host != "api5-normal-lq.fqnovel.com" || query.Get("offset") != "0" || query.Get("limit") != "20") {
			t.Fatal("favorites did not request complete identifier table")
		}
		if kind == "history" && (request.URL.Host != "api5-normal-sinfonlineb.fqnovel.com" || query.Get("offset") != "500" || query.Get("is_first_load") != "false" || query.Get("query_soft_deleted") != "false") {
			t.Fatal("history server cursor was changed")
		}
		if kind == "likes" && (query.Get("object_type_list") != "6,15,10" || query.Get("count") != "100" || query.Get("action_type") != "3" || query.Get("offset") != "500") {
			t.Fatal("likes query regressed")
		}
	}
	for _, offset := range []int{-1, 2001} {
		if _, err := prepareHongguoAccountLibrary(context.Background(), credentials, device, "history", offset); err == nil {
			t.Fatal("invalid cursor accepted")
		}
	}
}

func TestHongguoAccountLibraryStatusPreservesSession(t *testing.T) {
	for _, kind := range []string{"favorites", "history"} {
		for _, raw := range []string{`{"code":1009,"data":{}}`, `{"code":0,"data":{"error_code":"1009"}}`, `{"data":{}}`, `{"code":null,"data":{}}`, `{"code":0,"error_code":9,"data":{}}`} {
			_, err := hongguoAccountLibraryData(accountLibraryFixture(t, raw), kind)
			var endpointError *hongguoAccountLibraryError
			if err == nil || errors.Is(err, errHongguoAccountSessionExpired) || !errors.As(err, &endpointError) || endpointError.kind != kind {
				t.Fatalf("independent failure missing for %s", kind)
			}
		}
		for _, raw := range []string{`{"code":0,"data":{}}`, `{"code":"0","error_code":null,"data":{"error_code":null}}`} {
			if _, err := hongguoAccountLibraryData(accountLibraryFixture(t, raw), kind); err != nil {
				t.Fatal("optional null error_code incorrectly rejected")
			}
		}
	}
}

func TestHongguoAccountHistoryNormalizedCursorAndProgress(t *testing.T) {
	root := accountLibraryFixture(t, `{"code":0,"data":{"data_list":[
	{"book_id_str":"7399988776655443322","book_name":"合成剧","thumb_url":"https://example.test/poster","episode_cnt":24,"content_type":1,"vid_index":0,"current_play_position":12345,"read_timestamp_ms":1795182000000,"is_delete":false},
	{"book_id_str":"7399988776655443322","book_name":"合成剧","episode_cnt":24,"content_type":1,"vid_index":3,"current_play_position":30001,"read_timestamp_ms":1795182001000,"is_delete":false},
	{"book_id_str":"7399988776655443323","book_name":"已删除","content_type":1,"is_delete":true},
	{"book_id_str":"7399988776655443324","book_name":"不是短剧","content_type":6,"is_delete":false}
	],"next_offset":500,"has_more":true,"total":977}}`)
	result, err := parseHongguoAccountHistory(root, 0)
	if err != nil {
		t.Fatal(err)
	}
	items := result["items"].([]map[string]any)
	if len(items) != 1 || result["nextOffset"] != int64(500) || result["total"] != int64(977) || result["retrievedCount"] != 4 || result["unsupportedCount"] != 1 {
		t.Fatal("history counts or server cursor incorrect")
	}
	progress := items[0]["history"].(map[string]any)
	if progress["episodeIndex"] != int64(3) || progress["positionSeconds"] != int64(30) || progress["watchedAt"] != int64(1795182001000) {
		t.Fatal("history units or newest duplicate incorrect")
	}
}

func TestHongguoAccountHistoryProgressBoundsAndMissingFields(t *testing.T) {
	for _, raw := range []string{`{}`, `{"vid_index":-1,"current_play_position":-1,"read_timestamp_ms":0}`, `{"vid_index":24,"current_play_position":86400001,"read_timestamp_ms":4102444800001}`} {
		if value := hongguoAccountHistoryProgress(accountLibraryFixture(t, raw), 24); len(value) != 0 {
			t.Fatal("unknown or out-of-range progress emitted")
		}
	}
	progress := hongguoAccountHistoryProgress(accountLibraryFixture(t, `{"progress":{"vid_index":"0","current_play_time":"0"},"update_timestamp_ms":"1795182000000"}`), 24)
	if progress["episodeIndex"] != int64(0) || progress["positionSeconds"] != int64(0) || progress["watchedAt"] != int64(1795182000000) {
		t.Fatal("known zero values lost")
	}
	row := accountLibraryFixture(t, `{"code":0,"data":{"history_list":[{"series_id":"7399988776655443322","book_id_str":"7399988776655443323","book_name":"合成","episode_cnt":4,"content_type":1}],"next_offset":"977","has_more":0}}`)
	result, err := parseHongguoAccountHistory(row, 500)
	if err != nil || result["nextOffset"] != int64(977) {
		t.Fatal("terminal server offset rejected")
	}
	item := result["items"].([]map[string]any)[0]
	if item["series"].(map[string]any)["seriesId"] != "7399988776655443322" || item["history"] != nil {
		t.Fatal("explicit series identity or absent progress changed")
	}
	if _, exists := result["total"]; exists {
		t.Fatal("unknown total invented")
	}
}

func TestHongguoAccountHistoryRejectsMalformedPages(t *testing.T) {
	for _, raw := range []string{
		`{"code":0,"data":{"data_list":[],"has_more":true,"next_offset":500}}`,
		`{"code":0,"data":{"data_list":[{}],"has_more":true,"next_offset":0}}`,
		`{"code":0,"data":{"data_list":[{}],"has_more":true,"next_offset":2001}}`,
		`{"code":0,"data":{"data_list":[],"has_more":"unknown"}}`,
		`{"code":0,"data":{"data_list":[null],"has_more":false}}`,
		`{"code":0,"data":{"data_list":[{"is_delete":"unknown"}],"has_more":false}}`,
	} {
		if _, err := parseHongguoAccountHistory(accountLibraryFixture(t, raw), 0); err == nil {
			t.Fatal("malformed page accepted")
		}
	}
}

func TestHongguoAccountFavoritesLocalPagingAndMetadata(t *testing.T) {
	rows := make([]any, 0, 23)
	for index := 0; index < 23; index++ {
		rows = append(rows, map[string]any{"series_id": strconv.FormatInt(7399988776655443300+int64(index), 10), "content_type": 1, "collect_time": index})
	}
	root := map[string]any{"code": 0, "data": map[string]any{"video_shelf_info": rows}}
	var inFlight, maxFlight atomic.Int32
	metadata := func(ctx context.Context, id string) (map[string]any, error) {
		current := inFlight.Add(1)
		defer inFlight.Add(-1)
		for old := maxFlight.Load(); old < current && !maxFlight.CompareAndSwap(old, current); old = maxFlight.Load() {
		}
		time.Sleep(time.Millisecond)
		return map[string]any{"series_id_str": id, "series_title": "合成收藏", "series_cover": "javascript:invalid", "episode_cnt": 24}, nil
	}
	first, err := parseHongguoAccountFavorites(context.Background(), root, 0, metadata)
	if err != nil {
		t.Fatal(err)
	}
	if first["total"] != 23 || first["retrievedCount"] != 20 || first["nextOffset"] != int64(20) || first["hasMore"] != true || len(first["items"].([]map[string]any)) != 20 || maxFlight.Load() > 3 {
		t.Fatal("favorites page or bounded metadata incorrect")
	}
	series := first["items"].([]map[string]any)[0]["series"].(map[string]any)
	if series["seriesId"] != "7399988776655443322" || series["cover"] != "" {
		t.Fatal("newest favorites order or URL sanitization incorrect")
	}
	last, err := parseHongguoAccountFavorites(context.Background(), root, 20, metadata)
	if err != nil || last["hasMore"] != false || last["nextOffset"] != int64(23) || len(last["items"].([]map[string]any)) != 3 {
		t.Fatal("terminal favorites page incorrect")
	}
}

func TestHongguoAccountFavoritesInvalidDetailsAndEmptyList(t *testing.T) {
	root := accountLibraryFixture(t, `{"code":0,"data":{"video_shelf_info":[{"series_id":"7399988776655443322","content_type":1},{"series_id":"7399988776655443323","content_type":1},{"series_id":"7399988776655443324","content_type":6}]}}`)
	metadata := func(ctx context.Context, id string) (map[string]any, error) {
		if id == "7399988776655443323" {
			return nil, errors.New("synthetic unavailable")
		}
		return map[string]any{"series_id_str": id, "series_title": "合成收藏", "episode_cnt": 2}, nil
	}
	result, err := parseHongguoAccountFavorites(context.Background(), root, 0, metadata)
	if err != nil || len(result["items"].([]map[string]any)) != 1 || result["unsupportedCount"] != 2 {
		t.Fatal("partial metadata did not retain good entries")
	}
	if _, err := parseHongguoAccountFavorites(context.Background(), root, 0, func(context.Context, string) (map[string]any, error) { return nil, errors.New("offline") }); err == nil {
		t.Fatal("all details failures reported as successful empty favorites")
	}
	if _, err := parseHongguoAccountFavorites(context.Background(), root, 0, func(context.Context, string) (map[string]any, error) {
		return map[string]any{"series_id_str": "7399988776655443300", "series_title": "wrong identity"}, nil
	}); err == nil {
		t.Fatal("wrong series identities accepted")
	}
	empty, err := parseHongguoAccountFavorites(context.Background(), accountLibraryFixture(t, `{"code":0,"data":{"video_shelf_info":[]}}`), 0, metadata)
	if err != nil || empty["total"] != 0 || empty["hasMore"] != false {
		t.Fatal("confirmed empty list rejected")
	}
}

func TestHongguoAccountFavoritesUnavailablePlaybackAndCancellation(t *testing.T) {
	root := accountLibraryFixture(t, `{"code":0,"data":{"video_shelf_info":[{"series_id":"7399988776655443322","content_type":1}]}}`)
	metadata := func(ctx context.Context, id string) (map[string]any, error) {
		return map[string]any{"series_id_str": id, "series_title": "合成收藏", "disable_play": true}, nil
	}
	result, err := parseHongguoAccountFavorites(context.Background(), root, 0, metadata)
	if err != nil {
		t.Fatal(err)
	}
	item := result["items"].([]map[string]any)[0]
	if item["playable"] != false || item["note"] == "" || result["unsupportedCount"] != 0 {
		t.Fatal("unavailable playback confused with missing metadata")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := parseHongguoAccountFavorites(ctx, root, 0, metadata); err == nil {
		t.Fatal("cancelled metadata request reported success")
	}
	for _, offset := range []int{-1, 2} {
		if _, err := parseHongguoAccountFavorites(context.Background(), root, offset, metadata); err == nil {
			t.Fatal("out-of-range favorites page accepted")
		}
	}
}

func TestHongguoAccountLibraryMetadataUsesConfiguredTV(t *testing.T) {
	for _, scenario := range []struct {
		name     string
		body     string
		valid    bool
		playable bool
	}{
		{"good", `{"meta":{"series_id":"7399988776655443322","title":"合成收藏","cover":"https://example.test/poster","intro":"合成介绍"},"episodes":[{"index":1,"vid":"7399988776655443300"}]}`, true, true},
		{"without episodes", `{"meta":{"series_id":"7399988776655443322","title":"合成收藏"},"episodes":[]}`, true, false},
		{"wrong identity", `{"meta":{"series_id":"7399988776655443323","title":"错误收藏"},"episodes":[]}`, false, false},
		{"missing title", `{"meta":{"series_id":"7399988776655443322"},"episodes":[]}`, false, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			d := &Downloader{cfg: Config{Retries: 1, HongguotvURL: "http://tv.example.test:8000"}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(request *http.Request) (*http.Response, error) {
				if request.URL.Host != "tv.example.test:8000" || request.URL.Path != "/episodes" || request.URL.Query().Get("series_id") != "7399988776655443322" || request.Method != http.MethodGet {
					t.Fatal("metadata escaped configured TV episodes endpoint")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(scenario.body))}, nil
			})}}
			result, err := d.hongguoAccountLibraryMetadata(context.Background(), "7399988776655443322")
			if !scenario.valid {
				if err == nil {
					t.Fatal("unconfirmed metadata accepted")
				}
				return
			}
			if err != nil || result["series_id_str"] != "7399988776655443322" || result["disable_play"] != !scenario.playable {
				t.Fatal("TV metadata or availability incorrect")
			}
		})
	}
}
