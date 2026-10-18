package core

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
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
