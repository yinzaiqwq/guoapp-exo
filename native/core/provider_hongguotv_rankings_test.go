package core

import (
	"context"
	"net/http"
	"os"
	"testing"
	"time"
)

func TestHongguotvRankingBoardsAndResponse(t *testing.T) {
	for _, key := range []string{"recommend", "hot", "new"} {
		board, ok := findRankingBoard("hongguotv-" + key)
		if !ok || board.Source != sourceHongguotv || !singlePageRankingBoard(board) {
			t.Fatal("missing TV ranking", key)
		}
		d := rankingTestDownloader(t, func(r *http.Request) (*http.Response, error) {
			if r.URL.Path != "/rank" || r.URL.Query().Get("board") != key || r.URL.Query().Get("limit") != "100" || r.URL.Query().Get("api_key") == "" {
				t.Fatal("wrong ranking request")
			}
			return rankingHTTPResponse(r, 200, `{"board":"`+key+`","items":[{"rank":1,"series_id":"123","title":"合成短剧","episode_cnt":"12","hot":"100万热度","score":"8.0"}]}`), nil
		})
		page, err := d.loadRankingPage(context.Background(), board, 1, false)
		if err != nil || len(page.Items) != 1 || page.Items[0].Drama.Source != sourceHongguotv || page.Items[0].Drama.EpisodeCount != 12 || page.Items[0].Metric != "100万热度" || page.HasMore {
			t.Fatalf("ranking mismatch: %+v, %v", page, err)
		}
		if _, err := d.fetchHongguotvRanking(context.Background(), board, 2); err == nil {
			t.Fatal("unsupported extra page accepted")
		}
	}
}

func TestHongguotvRankingRejectsInvalidResponse(t *testing.T) {
	board, _ := findRankingBoard("hongguotv-hot")
	for _, body := range []string{
		`{"board":"new","items":[]}`,
		`{"board":"hot"}`,
		`{"board":"hot","items":[{"rank":1,"series_id":"bad","title":"无效"}]}`,
		`{"board":"hot","items":[{"rank":0,"series_id":"123","title":"无效"}]}`,
	} {
		d := rankingTestDownloader(t, func(r *http.Request) (*http.Response, error) { return rankingHTTPResponse(r, 200, body), nil })
		if _, err := d.fetchHongguotvRanking(context.Background(), board, 1); err == nil {
			t.Fatal("invalid ranking accepted")
		}
	}
}

func TestHongguotvRankingDeduplicatesWithoutRenumbering(t *testing.T) {
	board, _ := findRankingBoard("hongguotv-hot")
	d := rankingTestDownloader(t, func(r *http.Request) (*http.Response, error) {
		return rankingHTTPResponse(r, 200, `{"board":"hot","items":[{"rank":1,"series_id":"123","title":"合成"},{"rank":2,"series_id":"123","title":"重复"},{"rank":3,"series_id":"456","title":"第三"}]}`), nil
	})
	page, err := d.fetchHongguotvRanking(context.Background(), board, 1)
	if err != nil || len(page.Items) != 2 || page.Items[1].Rank != 3 || page.Warning == "" {
		t.Fatalf("deduplication changed upstream ranks: %+v, %v", page, err)
	}
}

func TestLiveHongguotvRankings(t *testing.T) {
	if os.Getenv("DUANJU_LIVE_TV_RANKINGS") != "1" {
		t.Skip("live server check is opt-in")
	}
	d := &Downloader{cfg: defaultConfig(), client: &http.Client{Timeout: 30 * time.Second}, limiter: newRequestLimiter(1, 0)}
	for _, key := range []string{"recommend", "hot", "new"} {
		board, _ := findRankingBoard("hongguotv-" + key)
		page, err := d.fetchHongguotvRanking(context.Background(), board, 1)
		if err != nil || len(page.Items) == 0 {
			t.Fatalf("%s ranking: count=%d err=%v", key, len(page.Items), err)
		}
		t.Logf("%s ranking: %d distinct entries", key, len(page.Items))
	}
}
