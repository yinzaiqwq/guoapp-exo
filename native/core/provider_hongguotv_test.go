package core

import (
	"context"
	"io"
	"net/http"
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
