package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// serverInt 解析服务器返回的数值字段；它们可能是数字或字符串。
func serverInt(m map[string]any, key string) int {
	value, ok := m[key]
	if !ok || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(typed))
		if err != nil {
			return 0
		}
		return parsed
	default:
		parsed, err := strconv.Atoi(strings.TrimSpace(fmt.Sprint(typed)))
		if err != nil {
			return 0
		}
		return parsed
	}
}

// 服务器下载管理：读取与删除自建 hongguo-api 服务上已下载的剧集。
//
// 服务端接口：
//   GET /download/list?api_key=...              列出全部已下载剧集与分集
//   GET /download/delete?folder=&ep=&api_key=...  删除整部或指定单集
//
// 客户端自身的下载管理的是设备本地文件，这里管理的是服务器磁盘上的副本，
// 两者互不影响：删除服务器副本后仍可重新下载或直接在线播放。

type serverDownloadEpisode struct {
	Index     int    `json:"index"`
	Filename  string `json:"filename"`
	SizeHuman string `json:"sizeHuman"`
	HasASS    bool   `json:"hasDanmaku"`
}

type serverDownloadDrama struct {
	Folder        string                  `json:"folder"`
	Title         string                  `json:"title"`
	SeriesID      string                  `json:"seriesId,omitempty"`
	TotalEpisodes int                     `json:"totalEpisodes"`
	Downloaded    int                     `json:"downloaded"`
	SizeHuman     string                  `json:"sizeHuman"`
	UpdatedTime   string                  `json:"updatedTime"`
	Episodes      []serverDownloadEpisode `json:"episodes"`
}

type serverDownloadList struct {
	Dramas      []serverDownloadDrama `json:"dramas"`
	TotalSize   string                `json:"totalSize"`
	TotalSizeMB int64                 `json:"totalSizeMb"`
	DiskFree    string                `json:"diskFree,omitempty"`
	DiskUsed    string                `json:"diskUsed,omitempty"`
}

// serverDownloadList 请求服务器并解析下载清单。
func (d *Downloader) serverDownloadList(ctx context.Context) (serverDownloadList, error) {
	body, err := d.hongguotvRequest(ctx, "/download/list", nil)
	if err != nil {
		return serverDownloadList{}, fmt.Errorf("读取服务器下载列表失败: %w", err)
	}
	var payload struct {
		Items       []map[string]any `json:"items"`
		TotalSize   string           `json:"total_size_human"`
		TotalSizeMB int64            `json:"total_size_bytes"`
		Disk        map[string]any   `json:"disk"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return serverDownloadList{}, errors.New("服务器下载列表格式无效")
	}
	result := serverDownloadList{
		TotalSize:   payload.TotalSize,
		TotalSizeMB: payload.TotalSizeMB / (1024 * 1024),
	}
	if disk := payload.Disk; disk != nil {
		result.DiskFree = mapString(disk, "free_human")
		result.DiskUsed = mapString(disk, "used_human")
	}
	for _, item := range payload.Items {
		folder := strings.TrimSpace(mapString(item, "folder_name"))
		if folder == "" {
			continue
		}
		drama := serverDownloadDrama{
			Folder:        folder,
			Title:         firstNonEmpty(mapString(item, "title"), folder),
			SeriesID:      mapString(item, "series_id"),
			TotalEpisodes: serverInt(item, "total_episodes"),
			Downloaded:    serverInt(item, "downloaded_cnt"),
			SizeHuman:     mapString(item, "size_human"),
			UpdatedTime:   mapString(item, "updated_time"),
		}
		for _, row := range anyList(item["episodes"]) {
			episode, _ := row.(map[string]any)
			if episode == nil {
				continue
			}
			hasDanmaku, _ := episode["has_ass"].(bool)
			if !hasDanmaku {
				hasDanmaku, _ = episode["has_xml"].(bool)
			}
			drama.Episodes = append(drama.Episodes, serverDownloadEpisode{
				Index:     serverInt(episode, "index"),
				Filename:  mapString(episode, "filename"),
				SizeHuman: mapString(episode, "size_human"),
				HasASS:    hasDanmaku,
			})
		}
		result.Dramas = append(result.Dramas, drama)
	}
	return result, nil
}

// serverDownloadDelete 删除服务器上整部剧集（ep 为空）或指定单集。
func (d *Downloader) serverDownloadDelete(ctx context.Context, folder string, episode string) (string, error) {
	folder = strings.TrimSpace(folder)
	if folder == "" {
		return "", errors.New("缺少要删除的剧集")
	}
	values := url.Values{}
	values.Set("folder", folder)
	if episode = strings.TrimSpace(episode); episode != "" {
		values.Set("ep", episode)
	}
	body, err := d.hongguotvRequest(ctx, "/download/delete", values)
	if err != nil {
		return "", fmt.Errorf("删除失败: %w", err)
	}
	var payload struct {
		Message string `json:"message"`
		Detail  string `json:"detail"`
	}
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return "", errors.New("删除响应格式无效")
	}
	if payload.Message == "" {
		return "", errors.New(firstNonEmpty(payload.Detail, "服务器未确认删除"))
	}
	return payload.Message, nil
}

func (d *Downloader) serverDownloadPlan(folder string, filename string) (nativePlan, error) {
	folder = strings.TrimSpace(folder)
	filename = strings.TrimSpace(filename)
	if folder == "" || filename == "" {
		return nativePlan{}, errors.New("缺少服务器视频信息")
	}
	if folder == ".." || filename == ".." || strings.ContainsAny(folder, "/\\") || strings.ContainsAny(filename, "/\\") {
		return nativePlan{}, errors.New("服务器视频路径无效")
	}
	site := d.providerBaseURL(sourceHongguotv)
	values := url.Values{}
	values.Set("folder", folder)
	values.Set("filename", filename)
	values.Set("api_key", hongguotvAPIKey)
	return nativePlan{
		ServerDownloaded: true,
		URL:              site + "/download/file?" + values.Encode(),
		Headers:          map[string]string{"Referer": site + "/"},
		Qualities:        []int{},
		RouteCount:       1,
	}, nil
}

func serverDownloadMatches(drama serverDownloadDrama, sourceID string) bool {
	if strings.HasPrefix(sourceID, "server-download/") {
		folder, err := url.PathUnescape(strings.TrimPrefix(sourceID, "server-download/"))
		return err == nil && folder == drama.Folder
	}
	return drama.SeriesID != "" && drama.SeriesID == sourceID
}

func (d *Downloader) preferredServerDownload(ctx context.Context, input nativeInput) (nativePlan, bool, error) {
	source, sourceID, valid := splitProviderDramaID(input.Drama.ID)
	if !valid || source != sourceHongguotv {
		return nativePlan{}, false, nil
	}
	lookup, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	list, err := d.serverDownloadList(lookup)
	if err != nil {
		return nativePlan{}, false, ctx.Err()
	}
	number, err := strconv.Atoi(input.Chapter.EpisodeString(input.Index))
	if err != nil || number < 1 {
		return nativePlan{}, false, nil
	}
	var plans []nativePlan
	for _, drama := range list.Dramas {
		if !serverDownloadMatches(drama, sourceID) {
			continue
		}
		for _, episode := range drama.Episodes {
			if episode.Index != number {
				continue
			}
			if plan, err := d.serverDownloadPlan(drama.Folder, episode.Filename); err == nil {
				plans = append(plans, plan)
			}
		}
	}
	if len(plans) != 1 {
		return nativePlan{}, false, nil
	}
	plan := plans[0]
	readable := d.serverFileReadable(lookup, plan)
	return plan, readable, ctx.Err()
}

func (d *Downloader) serverFileReadable(ctx context.Context, plan nativePlan) bool {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, plan.URL, nil)
	if err != nil {
		return false
	}
	request.Header.Set("Range", "bytes=0-0")
	request.Header.Set("Referer", plan.Headers["Referer"])
	response, err := d.doMediaRequest(request)
	if err != nil {
		return false
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK && response.StatusCode != http.StatusPartialContent {
		return false
	}
	var first [1]byte
	if _, err := io.ReadFull(response.Body, first[:]); err != nil {
		return false
	}
	return true
}

func (d *Downloader) preferredServerCache(ctx context.Context, input nativeInput) (nativePlan, bool, error) {
	task := Task{DramaID: input.Drama.ID, Chapter: input.Chapter, Index: input.Index}
	_, vid, valid := hongguotvPlaybackIDs(task)
	if !valid {
		return nativePlan{}, false, nil
	}
	lookup, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	values := url.Values{}
	values.Set("vid", vid)
	values.Set("quality", strconv.Itoa(input.Quality))
	var cached struct {
		Profile string `json:"profile"`
		VID     string `json:"vid"`
		Hit     bool   `json:"hit"`
		CacheID string `json:"cacheId"`
		Width   int    `json:"width"`
		Height  int    `json:"height"`
		Quality int    `json:"quality"`
	}
	if err := d.hongguotvJSON(lookup, "/stream/cache", values, &cached); err != nil {
		return nativePlan{}, false, ctx.Err()
	}
	if !cached.Hit || cached.Profile != "server-cache-v1" || cached.VID != vid ||
		cached.Quality < input.Quality || cached.Quality <= 0 || min(cached.Width, cached.Height) < cached.Quality || len(cached.CacheID) != 64 {
		return nativePlan{}, false, nil
	}
	for _, char := range cached.CacheID {
		if !strings.ContainsRune("0123456789abcdef", char) {
			return nativePlan{}, false, nil
		}
	}
	values = url.Values{}
	values.Set("vid", vid)
	values.Set("cache_id", cached.CacheID)
	values.Set("api_key", hongguotvAPIKey)
	site := d.providerBaseURL(sourceHongguotv)
	plan := nativePlan{URL: site + "/stream/cache/file?" + values.Encode(),
		Headers: map[string]string{"Referer": site + "/"}, ServerDownloaded: true,
		Quality: cached.Quality, Qualities: []int{cached.Quality}, RouteCount: 1}
	readable := d.serverFileReadable(lookup, plan)
	return plan, readable, ctx.Err()
}

func (d *Downloader) resolveDownloadMedia(ctx context.Context, job nativeDownloadJob) (providerMedia, error) {
	if source, _, valid := splitProviderDramaID(job.Drama.ID); valid && source == sourceHongguotv {
		input := nativeInput{Drama: job.Drama, Chapter: job.Chapter, Index: job.Index, Quality: job.Quality}
		for _, lookup := range []func(context.Context, nativeInput) (nativePlan, bool, error){d.preferredServerDownload, d.preferredServerCache} {
			plan, hit, err := lookup(ctx, input)
			if err != nil {
				return providerMedia{}, err
			}
			if hit {
				return providerMedia{URL: plan.URL, Referer: plan.Headers["Referer"], Quality: plan.Quality,
					Duration: time.Duration(job.Chapter.MediaSize) * time.Second}, nil
			}
		}
	}
	if err := ctx.Err(); err != nil {
		return providerMedia{}, err
	}
	return d.resolveProviderMedia(ctx, Task{
		DramaID: job.Drama.ID, DramaTitle: job.Drama.Title, Chapter: job.Chapter, Index: job.Index})
}

func (engine *nativeEngine) serverDownloadPlayback(ctx context.Context, input nativeInput) (nativePlan, error) {
	if err := ctx.Err(); err != nil {
		return nativePlan{}, err
	}
	plan, err := engine.downloader.serverDownloadPlan(input.Directory, input.Command)
	if err == nil {
		engine.registerServerPlayback(&plan, input)
	}
	return plan, err
}

func (engine *nativeEngine) registerServerPlayback(plan *nativePlan, input nativeInput) {
	engine.registerLocalPlayback(plan, input)
}

func (d *Downloader) serverDownloadDetail(ctx context.Context, sourceID string) (Drama, []Chapter, error) {
	list, err := d.serverDownloadList(ctx)
	if err != nil {
		return Drama{}, nil, err
	}
	var matched []serverDownloadDrama
	for _, drama := range list.Dramas {
		if serverDownloadMatches(drama, sourceID) {
			matched = append(matched, drama)
		}
	}
	if len(matched) != 1 {
		return Drama{}, nil, errors.New("无法唯一定位服务器下载的剧集")
	}
	item := matched[0]
	var chapters []Chapter
	seen := map[int]bool{}
	for _, episode := range item.Episodes {
		if episode.Index < 1 || seen[episode.Index] {
			return Drama{}, nil, errors.New("服务器下载集号无效或重复")
		}
		if _, err := d.serverDownloadPlan(item.Folder, episode.Filename); err != nil {
			return Drama{}, nil, err
		}
		seen[episode.Index] = true
		chapters = append(chapters, Chapter{
			ID:     providerChapterID(sourceHongguotv, sourceID, episode.Filename),
			Source: sourceHongguotv, Title: fmt.Sprintf("第%d集", episode.Index),
			CurrentEpisode: json.RawMessage(strconv.Itoa(episode.Index)),
		})
	}
	sort.Slice(chapters, func(i, j int) bool {
		left, _ := strconv.Atoi(chapters[i].EpisodeString(0))
		right, _ := strconv.Atoi(chapters[j].EpisodeString(0))
		return left < right
	})
	if len(chapters) == 0 {
		return Drama{}, nil, errors.New("服务器上没有可播放的已下载分集")
	}
	return Drama{ID: providerDramaID(sourceHongguotv, sourceID), Source: sourceHongguotv,
		SourceID: sourceID, Title: item.Title, EpisodeCount: max(item.TotalEpisodes, len(chapters))}, chapters, nil
}
