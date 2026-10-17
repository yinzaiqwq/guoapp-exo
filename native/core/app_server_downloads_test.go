package core

import (
	"net/url"
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

func TestServerDownloadPlanRejectsInvalidPath(t *testing.T) {
	downloader := &Downloader{cfg: defaultConfig()}
	for _, input := range [][2]string{{"", "1.mp4"}, {"show", ""}, {"../show", "1.mp4"}, {"show", "../1.mp4"}} {
		if _, err := downloader.serverDownloadPlan(input[0], input[1]); err == nil {
			t.Fatalf("expected invalid path to fail: %#v", input)
		}
	}
}
