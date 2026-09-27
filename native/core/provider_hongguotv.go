package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// 红果TV（hongguotv）原生站源：指向用户自建的 hongguo-api 服务。
//
// 该服务已在服务端完成「取流 + CENC 离线解密 + H.264 720p 转码」，
// 客户端拿到的 /stream 是标准 MP4（支持 HTTP Range），无需本地解密，
// 也绕开了红果 bytevc1/HEVC 在电视端无法硬解的问题。
//
// 上游接口（自建服务，默认 192.168.5.145:8000）：
//   GET /catalog?category=&page=&pagesize=   全量剧库分页
//   GET /episodes?series_id=                 剧集列表
//   GET /stream?vid=&codec=h264&res=720      解密转码后的 MP4
//   GET /search?q=&page=                     搜索
//   GET /img?url=                            封面代理
//
// 鉴权：query 参数 api_key（服务端强制校验）。

const (
	hongguotvSiteBaseURL = "http://192.168.5.145:8000"
	hongguotvAPIKey      = "changeme-client-key"
	hongguotvPageSize    = 60
)

var hongguotvCategories = []nativeCategory{
	{ID: "guoman", Name: "国漫"},
	{ID: "chuanyue", Name: "穿越"},
	{ID: "xuanhuan", Name: "玄幻"},
	{ID: "dushi", Name: "都市"},
	{ID: "nvpin", Name: "女频"},
	{ID: "moshi", Name: "末世"},
}

// hongguotvRequest 发起带 api_key 的 GET，返回响应体文本。
func (d *Downloader) hongguotvRequest(ctx context.Context, path string, query url.Values) (string, error) {
	site := d.providerBaseURL(sourceHongguotv)
	if query == nil {
		query = url.Values{}
	}
	query.Set("api_key", hongguotvAPIKey)
	address := site + path + "?" + query.Encode()
	return d.fetchProviderText(ctx, address, site+"/")
}

// hongguotvJSON 请求并解析 JSON 到 out。
func (d *Downloader) hongguotvJSON(ctx context.Context, path string, query url.Values, out any) error {
	body, err := d.hongguotvRequest(ctx, path, query)
	if err != nil {
		return err
	}
	if err := json.Unmarshal([]byte(body), out); err != nil {
		return fmt.Errorf("红果TV 返回格式异常: %w", err)
	}
	return nil
}

func (d *Downloader) fetchHongguotvCategories() []nativeCategory {
	return append([]nativeCategory(nil), hongguotvCategories...)
}

func validHongguotvCategory(category string) bool {
	if category == "" || category == "all" {
		return true
	}
	if len(category) > 16 || strings.ContainsAny(category, "|/\\") {
		return false
	}
	for _, r := range category {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	for _, item := range hongguotvCategories {
		if item.ID == category {
			return true
		}
	}
	return false
}

// hongguotvCatalogResponse 对应 /catalog 的返回。
type hongguotvCatalogResponse struct {
	Total      int `json:"total"`
	Page       int `json:"page"`
	Pagesize   int `json:"pagesize"`
	Categories []struct {
		ID    string `json:"id"`
		Name  string `json:"name"`
		Count int    `json:"count"`
	} `json:"categories"`
	Items []struct {
		SeriesID   string `json:"series_id"`
		Title      string `json:"title"`
		Cover      string `json:"cover"`
		EpisodeCnt int    `json:"episode_cnt"`
		Score      string `json:"score"`
		Hot        string `json:"hot"`
		Copyright  string `json:"copyright"`
		Intro      string `json:"intro"`
	} `json:"items"`
}

func (d *Downloader) fetchHongguotvCatalogPage(ctx context.Context, page int, category, query string) ([]Drama, bool, error) {
	if page < 1 {
		page = 1
	}
	values := url.Values{}
	values.Set("page", strconv.Itoa(page))
	values.Set("pagesize", strconv.Itoa(hongguotvPageSize))
	if category != "" && category != "all" {
		values.Set("category", category)
	}
	if keyword := strings.TrimSpace(query); keyword != "" {
		values.Set("q", keyword)
	}
	var payload hongguotvCatalogResponse
	if err := d.hongguotvJSON(ctx, "/catalog", values, &payload); err != nil {
		return nil, false, err
	}
	items := make([]Drama, 0, len(payload.Items))
	for _, row := range payload.Items {
		id := strings.TrimSpace(row.SeriesID)
		if id == "" || row.Title == "" {
			continue
		}
		cover := strings.TrimSpace(row.Cover)
		items = append(items, Drama{
			ID: providerDramaID(sourceHongguotv, id), Source: sourceHongguotv, SourceID: id,
			Title: truncate(row.Title, 512), Name: truncate(row.Title, 512),
			Intro: truncate(row.Intro, 512), Desc: truncate(row.Intro, 512),
			Cover: cover, CoverURL: cover, ChannelName: "红果TV",
			Category: row.Copyright, CategoryName: row.Copyright,
			Score: row.Score, Views: row.Hot,
			EpisodeCount: row.EpisodeCnt, TotalEpisode: row.EpisodeCnt,
		})
	}
	hasMore := payload.Page*payload.Pagesize < payload.Total && len(payload.Items) > 0
	return items, hasMore, nil
}

// hongguotvEpisodesResponse 对应 /episodes 的返回。
type hongguotvEpisodesResponse struct {
	Meta struct {
		SeriesID string `json:"series_id"`
		Title    string `json:"title"`
		Intro    string `json:"intro"`
		Cover    string `json:"cover"`
	} `json:"meta"`
	Episodes []struct {
		Index        int    `json:"index"`
		VID          string `json:"vid"`
		Title        string `json:"title"`
		Duration     int    `json:"duration"`
		Cover        string `json:"cover"`
		CommentCount int    `json:"comment_count"`
	} `json:"episodes"`
}

func (d *Downloader) hongguotvEpisodes(ctx context.Context, seriesID string) (hongguotvEpisodesResponse, error) {
	var payload hongguotvEpisodesResponse
	values := url.Values{}
	values.Set("series_id", seriesID)
	err := d.hongguotvJSON(ctx, "/episodes", values, &payload)
	return payload, err
}

func (d *Downloader) fetchHongguotvDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	payload, err := d.hongguotvEpisodes(ctx, sourceID)
	if err != nil {
		return Drama{}, nil, err
	}
	title := strings.TrimSpace(payload.Meta.Title)
	if title == "" {
		title = "红果TV"
	}
	cover := strings.TrimSpace(payload.Meta.Cover)
	drama := Drama{
		ID: providerDramaID(sourceHongguotv, sourceID), Source: sourceHongguotv, SourceID: sourceID,
		Title: truncate(title, 512), Name: truncate(title, 512),
		Intro: truncate(payload.Meta.Intro, 512), Desc: truncate(payload.Meta.Intro, 512),
		Cover: cover, CoverURL: cover, ChannelName: "红果TV",
		EpisodeCount: len(payload.Episodes), TotalEpisode: len(payload.Episodes),
	}
	chapters := make([]Chapter, 0, len(payload.Episodes))
	for position, episode := range payload.Episodes {
		vid := strings.TrimSpace(episode.VID)
		if vid == "" {
			continue
		}
		index := episode.Index
		if index <= 0 {
			index = position + 1
		}
		episodeTitle := strings.TrimSpace(episode.Title)
		if episodeTitle == "" {
			episodeTitle = fmt.Sprintf("第%d集", index)
		}
		chapters = append(chapters, Chapter{
			ID:       providerChapterID(sourceHongguotv, sourceID, vid),
			Source:   sourceHongguotv,
			Title:    truncate(episodeTitle, 512),
			PageURL:  providerDramaID(sourceHongguotv, vid),
			Referer:  d.providerBaseURL(sourceHongguotv) + "/",
			MediaSize: int64(episode.Duration),
		})
	}
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("红果TV 未返回可播放分集")
	}
	return drama, chapters, nil
}

// resolveHongguotvMedia 直接返回服务端 /stream 地址。
// 服务端已完成解密与 720p H.264 转码，客户端无需再处理 CENC。
func (d *Downloader) resolveHongguotvMedia(ctx context.Context, task Task) (providerMedia, error) {
	vid := strings.TrimSpace(task.Chapter.PageURL)
	if _, sourceID, ok := splitProviderDramaID(vid); ok {
		vid = sourceID
	}
	if vid == "" {
		return providerMedia{}, errors.New("红果TV 分集标识缺失")
	}
	site := d.providerBaseURL(sourceHongguotv)
	values := url.Values{}
	values.Set("vid", vid)
	values.Set("codec", "h264")
	values.Set("res", "720")
	values.Set("api_key", hongguotvAPIKey)
	duration := time.Duration(task.Chapter.MediaSize) * time.Second
	return providerMedia{
		URL:      site + "/stream?" + values.Encode(),
		Referer:  site + "/",
		Duration: duration,
		Quality:  720,
	}, nil
}

func (d *Downloader) searchHongguotv(ctx context.Context, page int, query string) ([]Drama, bool, error) {
	return d.fetchHongguotvCatalogPage(ctx, page, "", query)
}

// ───────────────────────── 弹幕 ─────────────────────────

// hongguotvDanmakuPage 对应自建服务 /danmaku 的返回。
type hongguotvDanmakuResponse struct {
	Items []struct {
		T     float64 `json:"t"`
		Text  string  `json:"text"`
		Color string  `json:"color"`
		Src   string  `json:"src"`
	} `json:"items"`
}

// hongguotvPlaybackIDs 从播放任务解析出弹幕所需的 series_id 与 vid。
// 红果TV 的 chapter.PageURL 存放 vid（见 fetchHongguotvDetail）。
func hongguotvPlaybackIDs(task Task) (seriesID, videoID string, ok bool) {
	source, seriesID, valid := splitProviderDramaID(task.DramaID)
	if !valid || source != sourceHongguotv {
		return "", "", false
	}
	videoID = strings.TrimSpace(task.Chapter.PageURL)
	if _, id, ok2 := splitProviderDramaID(videoID); ok2 {
		videoID = id
	}
	if !hongguoNumericID.MatchString(seriesID) || !hongguoNumericID.MatchString(videoID) {
		return "", "", false
	}
	return seriesID, videoID, true
}

// hongguotvDanmaku 取弹幕。自建服务已聚合红果官方弹幕与本地弹幕。
func (d *Downloader) hongguotvDanmaku(ctx context.Context, seriesID, videoID string, start, duration int64) (hongguoDanmakuPage, error) {
	if err := ctx.Err(); err != nil {
		return hongguoDanmakuPage{}, err
	}
	if !hongguoNumericID.MatchString(seriesID) || !hongguoNumericID.MatchString(videoID) {
		return hongguoDanmakuPage{}, errors.New("弹幕请求参数无效")
	}
	// 服务端按集返回整集弹幕，start/duration 仅用于本地过滤
	values := url.Values{}
	values.Set("series_id", seriesID)
	values.Set("vid", videoID)
	values.Set("ep", strconv.FormatInt(start/danmakuSegmentMS+1, 10))
	body, err := d.hongguotvRequest(ctx, "/danmaku", values)
	if err != nil {
		return hongguoDanmakuPage{}, err
	}
	var payload hongguotvDanmakuResponse
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return hongguoDanmakuPage{}, fmt.Errorf("弹幕返回格式异常: %w", err)
	}
	page := hongguoDanmakuPage{EpisodeID: videoID, StartMS: start, NextMS: start + duration, Total: int64(len(payload.Items))}
	seen := map[string]bool{}
	for index, item := range payload.Items {
		text := strings.TrimSpace(item.Text)
		if text == "" {
			continue
		}
		timeMS := int64(item.T * 1000)
		if timeMS < start || timeMS >= start+duration {
			continue
		}
		id := fmt.Sprintf("%s-%d-%d", videoID, timeMS, index)
		if seen[id] {
			continue
		}
		seen[id] = true
		page.Items = append(page.Items, hongguoDanmakuItem{
			ID:     id,
			Text:   truncate(text, 200),
			TimeMS: timeMS,
		})
	}
	return page, nil
}

// danmakuSegmentMS 单次弹幕请求覆盖的时长；服务端按集返回，此处用整集区间。
const danmakuSegmentMS = 24 * 60 * 60 * 1000
