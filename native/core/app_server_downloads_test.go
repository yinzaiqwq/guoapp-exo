package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestServerDownloadPlan(t *testing.T) {
	downloader := &Downloader{cfg: Config{HongguotvURL: "http://example.test:8000/"}}
	plan, err := downloader.serverDownloadPlan("合成短剧", "第 1 集.mp4")
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(plan.URL)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.Scheme != "http" || parsed.Host != "example.test:8000" || parsed.Path != "/download/file" {
		t.Fatalf("unexpected playback URL: %s", plan.URL)
	}
	query := parsed.Query()
	if query.Get("folder") != "合成短剧" || query.Get("filename") != "第 1 集.mp4" || query.Get("api_key") == "" {
		t.Fatalf("unexpected playback query: %v", query)
	}
	if plan.Local || plan.RouteCount != 1 || plan.Headers["Referer"] != "http://example.test:8000/" {
		t.Fatalf("unexpected playback plan: %#v", plan)
	}
}

func TestRecentPlaybackPrefersServerDownload(t *testing.T) {
	for _, force := range []bool{false, true} {
		requests := []string{}
		d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
			requests = append(requests, r.URL.Path)
			body := `{"items":[{"folder_name":"合成第二季","series_id":"100","episodes":[{"index":9,"filename":"009.mp4"},{"index":2,"filename":"002.mp4"}]},{"folder_name":"同名第一季","series_id":"101","episodes":[{"index":9,"filename":"wrong.mp4"}]}]}`
			status := 200
			if r.URL.Path == "/download/file" {
				if r.URL.Query().Get("folder") != "合成第二季" || r.URL.Query().Get("filename") != "009.mp4" || r.Header.Get("Range") != "bytes=0-0" {
					t.Fatal("wrong server episode selected")
				}
				status, body = 206, "x"
			} else if r.URL.Path != "/download/list" {
				t.Fatal("downloaded playback contacted online stream")
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		engine := &nativeEngine{downloader: d}
		plan, err := engine.nativeResolve(context.Background(), nativeInput{
			Drama: nativeDrama{ID: "hongguotv:100"}, Index: 1, Force: force, Quality: 720,
			Chapter: Chapter{CurrentEpisode: json.RawMessage(`9`), PageURL: "hongguotv:999"},
		})
		if err != nil || !plan.ServerDownloaded || plan.Local || plan.Session == "" || plan.DanmakuID != "999" || plan.Quality != 0 || len(plan.Qualities) != 0 {
			t.Fatalf("unexpected direct plan: %+v, %v", plan, err)
		}
		if !reflect.DeepEqual(requests, []string{"/download/list", "/download/file"}) {
			t.Fatalf("unexpected requests: %v", requests)
		}
		if engine.playbacks[plan.Session].danmakuEpisode != 9 {
			t.Fatal("downloaded playback lost danmaku episode")
		}
	}
}

func TestPreferredServerDownloadMisses(t *testing.T) {
	for _, tc := range []struct {
		name, items, file string
		status            int
	}{
		{"not downloaded", `[]`, "x", 206},
		{"different season", `[{"folder_name":"show","series_id":"101","episodes":[{"index":9,"filename":"009.mp4"}]}]`, "x", 206},
		{"episode missing", `[{"folder_name":"show","series_id":"100","episodes":[{"index":2,"filename":"002.mp4"}]}]`, "x", 206},
		{"file removed", `[{"folder_name":"show","series_id":"100","episodes":[{"index":9,"filename":"009.mp4"}]}]`, "missing", 404},
		{"file empty", `[{"folder_name":"show","series_id":"100","episodes":[{"index":9,"filename":"009.mp4"}]}]`, "", 206},
		{"ambiguous", `[{"folder_name":"show","series_id":"100","episodes":[{"index":9,"filename":"a.mp4"},{"index":9,"filename":"b.mp4"}]}]`, "x", 206},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
				body, status := `{"items":`+tc.items+`}`, 200
				if r.URL.Path == "/download/file" {
					body, status = tc.file, tc.status
				}
				return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
			})}}
			_, found, err := d.preferredServerDownload(context.Background(), nativeInput{Drama: nativeDrama{ID: "hongguotv:100"}, Index: 9})
			if found || err != nil {
				t.Fatalf("unexpected match: %v %v", found, err)
			}
		})
	}
}

func TestServerDownloadDetailUsesRealEpisodeNumbers(t *testing.T) {
	d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/download/list" {
			t.Fatal("folder history must not request online metadata")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"items":[{"folder_name":"合成 show","title":"合成","episodes":[{"index":9,"filename":"009.mp4"},{"index":2,"filename":"002.mp4"}]}]}`))}, nil
	})}}
	drama, chapters, err := d.fetchHongguotvDetail(context.Background(), "server-download/"+url.PathEscape("合成 show"))
	if err != nil || drama.Title != "合成" || len(chapters) != 2 || chapters[0].EpisodeString(0) != "2" || chapters[1].EpisodeString(0) != "9" {
		t.Fatalf("invalid server history detail: %+v, %+v, %v", drama, chapters, err)
	}
}

func TestServerDownloadMissFallsBackToOnline(t *testing.T) {
	for _, missingFile := range []bool{false, true} {
		d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
			body, status := `{"items":[]}`, 200
			switch r.URL.Path {
			case "/download/list":
				if missingFile {
					body = `{"items":[{"folder_name":"show","series_id":"100","episodes":[{"index":9,"filename":"009.mp4"}]}]}`
				}
			case "/download/file":
				status = 404
			case "/stream/qualities":
				body = `{"profile":"short-edge-v2","items":[{"quality":1080,"width":1920,"height":1080}]}`
			case "/stream/cache":
				body = `{"profile":"server-cache-v1","vid":"999","hit":false}`
			default:
				t.Fatal("unexpected request: " + r.URL.Path)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		engine := &nativeEngine{downloader: d}
		plan, err := engine.nativeResolve(context.Background(), nativeInput{
			Drama: nativeDrama{ID: "hongguotv:100"}, Index: 9,
			Chapter: Chapter{PageURL: "hongguotv:999", CurrentEpisode: json.RawMessage(`9`)},
		})
		if engine.stream != nil {
			defer engine.stream.server.Close()
		}
		if err != nil || plan.ServerDownloaded || plan.Quality != 1080 || plan.Session == "" {
			t.Fatalf("online fallback failed: %+v, %v", plan, err)
		}
	}
}

func TestServerDownloadLookupHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	d := &Downloader{cfg: Config{Retries: 1}}
	_, found, err := d.preferredServerDownload(ctx, nativeInput{Drama: nativeDrama{ID: "hongguotv:100"}, Index: 1})
	if found || err != context.Canceled {
		t.Fatalf("canceled playback lookup continued: %v, %v", found, err)
	}
}

func TestServerDownloadPlanRejectsInvalidPath(t *testing.T) {
	downloader := &Downloader{cfg: defaultConfig()}
	for _, input := range [][2]string{{"", "1.mp4"}, {"show", ""}, {"../show", "1.mp4"}, {"show", "../1.mp4"}} {
		if _, err := downloader.serverDownloadPlan(input[0], input[1]); err == nil {
			t.Fatalf("expected invalid path to fail: %#v", input)
		}
	}
}

func TestServerCachePlaybackKeepsDanmakuWithoutOnlineMediaRequests(t *testing.T) {
	for _, force := range []bool{false, true} {
		paths := []string{}
		d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
			paths = append(paths, r.URL.Path)
			body, status := `{"items":[]}`, 200
			switch r.URL.Path {
			case "/download/list":
			case "/stream/cache":
				if r.URL.Query().Get("vid") != "999" {
					t.Fatal("wrong video")
				}
				body = `{"profile":"server-cache-v1","vid":"999","hit":true,"cacheId":"` + strings.Repeat("a", 64) + `","quality":1080,"width":1920,"height":1080}`
			case "/stream/cache/file":
				if r.Header.Get("Range") != "bytes=0-0" {
					t.Fatal("missing cache preflight")
				}
				body, status = "x", 206
			case "/danmaku":
				q := r.URL.Query()
				if q.Get("series_id") != "100" || q.Get("vid") != "999" || q.Get("ep") != "9" {
					t.Fatal("wrong danmaku identity")
				}
				body = `{"items":[{"t":0.5,"text":"合成弹幕"}]}`
			default:
				t.Fatal("cache hit must not fetch source media or qualities: " + r.URL.Path)
			}
			return &http.Response{StatusCode: status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		engine := &nativeEngine{downloader: d}
		plan, err := engine.nativeResolve(context.Background(), nativeInput{
			Drama: nativeDrama{ID: "hongguotv:100"}, Index: 9, Quality: 1080, Force: force,
			Chapter: Chapter{PageURL: "hongguotv:999", CurrentEpisode: json.RawMessage(`9`)},
		})
		if err != nil || !plan.ServerDownloaded || plan.Quality != 1080 || plan.DanmakuSource != sourceHongguotv || plan.DanmakuID != "999" || plan.Session == "" {
			t.Fatalf("invalid cache plan: %+v %v", plan, err)
		}
		page, err := engine.nativeDanmaku(context.Background(), nativeInput{PlaybackSession: plan.Session, DurationMS: 60000})
		if err != nil || len(page.Items) != 1 || page.Items[0].TimeMS != 500 {
			t.Fatalf("cache danmaku failed: %+v %v", page, err)
		}
		if !reflect.DeepEqual(paths, []string{"/download/list", "/stream/cache", "/stream/cache/file", "/danmaku"}) {
			t.Fatal(paths)
		}
		engine.nativeReleasePlayback(plan.Session)
		if _, err := engine.nativeDanmaku(context.Background(), nativeInput{PlaybackSession: plan.Session, DurationMS: 60000}); err == nil {
			t.Fatal("released cache session still active")
		}
	}
}

func TestServerDownloadButtonRegistersDanmakuByEpisode(t *testing.T) {
	d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.Path != "/danmaku" || r.URL.Query().Get("series_id") != "100" || r.URL.Query().Get("ep") != "9" || r.URL.Query().Get("vid") != "" {
			t.Fatal("wrong series/episode danmaku query")
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"items":[{"t":1,"text":"合成下载弹幕"}]}`))}, nil
	})}}
	engine := &nativeEngine{downloader: d}
	plan, err := engine.serverDownloadPlayback(context.Background(), nativeInput{
		Directory: "synthetic", Command: "009.mp4", Drama: nativeDrama{ID: "hongguotv:100"}, Index: 9,
		Chapter: Chapter{CurrentEpisode: json.RawMessage(`9`)},
	})
	if err != nil || plan.DanmakuSource != sourceHongguotv || plan.DanmakuID != "episode:9" || plan.Session == "" {
		t.Fatalf("missing direct danmaku session: %+v %v", plan, err)
	}
	page, err := engine.nativeDanmaku(context.Background(), nativeInput{PlaybackSession: plan.Session, DurationMS: 60000})
	if err != nil || len(page.Items) != 1 {
		t.Fatalf("direct download danmaku failed: %+v %v", page, err)
	}
}

func TestServerCacheRejectsWrongVideoOrResolution(t *testing.T) {
	for _, body := range []string{
		`{"profile":"server-cache-v1","vid":"998","hit":true,"cacheId":"` + strings.Repeat("a", 64) + `","quality":1080,"width":1920,"height":1080}`,
		`{"profile":"server-cache-v1","vid":"999","hit":true,"cacheId":"` + strings.Repeat("a", 64) + `","quality":1080,"width":406,"height":720}`,
		`{"profile":"server-cache-v1","vid":"999","hit":true,"cacheId":"../../escape","quality":1080,"width":1920,"height":1080}`,
	} {
		d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/stream/cache" {
				t.Fatal("invalid cache descriptor was used")
			}
			return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
		})}}
		_, found, err := d.preferredServerCache(context.Background(), nativeInput{Drama: nativeDrama{ID: "hongguotv:100"}, Index: 9, Quality: 1080, Chapter: Chapter{PageURL: "hongguotv:999"}})
		if found || err != nil {
			t.Fatalf("invalid cache matched %v %v", found, err)
		}
	}
}

func TestHongguotvDanmakuWindowUsesAbsoluteEnd(t *testing.T) {
	d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"items":[{"t":74.9,"text":"后段"},{"t":30,"text":"前段"},{"t":45,"text":"开始"},{"t":75,"text":"下一窗"}]}`))}, nil
	})}}
	page, err := d.hongguotvDanmaku(context.Background(), "100", "999", 9, 45000, 120000)
	if err != nil || page.NextMS != 75000 || len(page.Items) != 2 || page.Items[0].TimeMS != 45000 || page.Items[1].TimeMS != 74900 {
		t.Fatalf("bad absolute timeline: %+v %v", page, err)
	}
	empty, err := d.hongguotvDanmaku(context.Background(), "100", "999", 9, 90000, 120000)
	if err != nil || empty.NextMS != 120000 || empty.Items == nil || len(empty.Items) != 0 {
		t.Fatalf("empty window is not valid JSON list: %+v %v", empty, err)
	}
}

func TestServerCacheLive(t *testing.T) {
	address := os.Getenv("TEST_HONGGUOTV_CACHE_URL")
	if address == "" {
		t.Skip("explicit live cache server not configured")
	}
	series, video := os.Getenv("TEST_HONGGUOTV_SERIES_ID"), os.Getenv("TEST_HONGGUOTV_VIDEO_ID")
	number, err := strconv.Atoi(os.Getenv("TEST_HONGGUOTV_EPISODE"))
	if err != nil || number < 1 || !hongguoNumericID.MatchString(series) || !hongguoNumericID.MatchString(video) {
		t.Fatal("invalid live test identity")
	}
	d := &Downloader{cfg: Config{Retries: 1, HongguotvURL: address}, client: &http.Client{Timeout: 30 * time.Second}, limiter: newRequestLimiter(1, 0)}
	engine := &nativeEngine{downloader: d}
	started := time.Now()
	plan, err := engine.nativeResolve(context.Background(), nativeInput{
		Drama: nativeDrama{ID: providerDramaID(sourceHongguotv, series)}, Index: number, Quality: 1080,
		Chapter: Chapter{PageURL: providerDramaID(sourceHongguotv, video), CurrentEpisode: json.RawMessage(strconv.Itoa(number))},
	})
	if err != nil {
		t.Fatal("live cache resolve failed")
	}
	defer engine.nativeReleasePlayback(plan.Session)
	addressURL, _ := url.Parse(plan.URL)
	if !plan.ServerDownloaded || addressURL.Path != "/stream/cache/file" || plan.Quality != 1080 || plan.DanmakuSource != sourceHongguotv || plan.DanmakuID != video {
		t.Fatal("live playback did not select matching 1080 cache and danmaku")
	}
	count := 0
	for start := int64(0); start < 120000; {
		page, err := engine.nativeDanmaku(context.Background(), nativeInput{PlaybackSession: plan.Session, StartMS: start, DurationMS: 120000})
		if err != nil || page.NextMS <= start || page.NextMS > 120000 {
			t.Fatal("live danmaku pagination failed")
		}
		count += len(page.Items)
		start = page.NextMS
	}
	if count == 0 {
		t.Fatal("no danmaku returned for live verification episode")
	}
	t.Logf("direct cached 1080 playback and %d timeline-filtered comments verified in %s", count, time.Since(started).Round(time.Millisecond))
}
