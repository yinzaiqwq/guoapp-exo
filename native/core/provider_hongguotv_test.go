package core

import (
	"context"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestHongguotvCatalogValidatesPagination(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		more, fails   bool
	}{
		{"missing pagination defaults", `{"total":61,"items":[{"series_id":"1","title":"合成月亮"}]}`, true, false},
		{"last page", `{"page":1,"pagesize":60,"total":1,"items":[{"series_id":"1","title":"合成月亮"}]}`, false, false},
		{"empty match", `{"page":1,"pagesize":60,"total":0,"items":[]}`, false, false},
		{"wrong page", `{"page":2,"pagesize":60,"total":61,"items":[]}`, false, true},
		{"incomplete page", `{"page":1,"pagesize":60,"total":34,"items":[]}`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Query().Get("q") != "月亮" || r.URL.Query().Get("pagesize") != "60" {
					t.Fatal("query changed")
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.payload))}, nil
			})}}
			_, more, err := d.fetchHongguotvCatalogPage(context.Background(), 1, "", "月亮")
			if (err != nil) != tc.fails || more != tc.more {
				t.Fatalf("more=%v err=%v", more, err)
			}
		})
	}
}

func TestHongguotvRealQualityVariants(t *testing.T) {
	for _, tc := range []struct {
		name, payload string
		qualities     []int
		fails         bool
	}{
		{"portrait full HD", `{"profile":"short-edge-v2","items":[{"quality":720,"width":720,"height":1280},{"quality":1080,"width":1080,"height":1920}]}`, []int{1080, 720}, false},
		{"landscape full HD", `{"profile":"short-edge-v2","items":[{"quality":1080,"width":1920,"height":1080}]}`, []int{1080}, false},
		{"only real 720", `{"profile":"short-edge-v2","items":[{"quality":720,"width":720,"height":1280},{"quality":1080,"width":406,"height":720}]}`, []int{720}, false},
		{"old server", `{"items":[{"quality":1080,"width":1080,"height":1920}]}`, nil, true},
		{"empty", `{"profile":"short-edge-v2","items":[]}`, nil, true},
		{"invalid and duplicate", `{"profile":"short-edge-v2","items":[{"quality":9999,"width":9999,"height":9999},{"quality":720,"width":720,"height":1280},{"quality":720,"width":720,"height":1280}]}`, []int{720}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := &Downloader{cfg: Config{Retries: 1}, limiter: newRequestLimiter(1, 0), client: &http.Client{Transport: suggestionTransport(func(r *http.Request) (*http.Response, error) {
				if r.URL.Path != "/stream/qualities" || r.URL.Query().Get("vid") != "123" {
					t.Fatalf("unexpected metadata request: %s", r.URL.Path)
				}
				return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.payload))}, nil
			})}}
			media, err := d.resolveHongguotvMedia(context.Background(), Task{Chapter: Chapter{PageURL: "hongguotv:123", MediaSize: 45}})
			if (err != nil) != tc.fails {
				t.Fatalf("err=%v", err)
			}
			if tc.fails {
				return
			}
			choice := nativePlaybackChoices(media, 0)
			if !reflect.DeepEqual(choice.qualities, tc.qualities) {
				t.Fatalf("qualities=%v", choice.qualities)
			}
			for _, variant := range choice.media {
				address, _ := url.Parse(variant.URL)
				if address.Query().Get("res") != strconv.Itoa(variant.Quality) || address.Query().Get("quality") != strconv.Itoa(variant.Quality)+"p" || address.Query().Get("codec") != "h264" {
					t.Fatalf("quality request does not match actual variant")
				}
			}
			if choice.media[0].Quality != tc.qualities[0] {
				t.Fatal("auto did not prefer highest real quality")
			}
			for _, quality := range tc.qualities {
				selected := nativePlaybackChoices(media, quality)
				if len(selected.media) != 1 || selected.media[0].Quality != quality {
					t.Fatalf("quality selection failed: %d", quality)
				}
			}
		})
	}
}
