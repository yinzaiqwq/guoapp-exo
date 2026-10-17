package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
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
		URL:        site + "/download/file?" + values.Encode(),
		Headers:    map[string]string{"Referer": site + "/"},
		Qualities:  []int{},
		RouteCount: 1,
	}, nil
}
