package core

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"sync"
	"time"
)

const hongguoAccountFavoritesURL = "https://api5-normal-lq.fqnovel.com/reading/bookapi/bookshelf/video/list/v"
const hongguoAccountHistoryURL = "https://api5-normal-sinfonlineb.fqnovel.com/reading/bookapi/read_history/list/v"
const hongguoAccountFavoritesPageSize = 20

type hongguoAccountLibraryError struct {
	kind    string
	message string
}

func (err *hongguoAccountLibraryError) Error() string {
	return err.message
}

func hongguoAccountLibraryLabel(kind string) string {
	if kind == "favorites" {
		return "红果收藏"
	}
	return "红果观看历史"
}

func hongguoAccountLibraryFailure(kind, message string) error {
	return &hongguoAccountLibraryError{kind: kind, message: hongguoAccountLibraryLabel(kind) + message + "；原列表和登录已保留"}
}

func hongguoAccountFlexibleInteger(value any) (int64, bool) {
	if text, ok := value.(string); ok {
		result, err := strconv.ParseInt(text, 10, 64)
		return result, err == nil
	}
	return hongguoAccountInteger(value)
}

func hongguoAccountLibraryBool(value any) (bool, bool) {
	if flag, ok := value.(bool); ok {
		return flag, true
	}
	if number, ok := hongguoAccountFlexibleInteger(value); ok && (number == 0 || number == 1) {
		return number == 1, true
	}
	return false, false
}

func hongguoAccountLibraryData(root map[string]any, kind string) (map[string]any, error) {
	data, valid := root["data"].(map[string]any)
	codes := []any{root["code"]}
	for _, container := range []map[string]any{root, data} {
		if value, exists := container["error_code"]; exists && value != nil {
			codes = append(codes, value)
		}
	}
	for _, value := range codes {
		code, ok := hongguoAccountFlexibleInteger(value)
		if !ok {
			return nil, hongguoAccountLibraryFailure(kind, "返回状态未确认，请重试")
		}
		if code == 1009 {
			return nil, hongguoAccountLibraryFailure(kind, "接口暂未接受当前会话，请稍后重试")
		}
		if code != 0 {
			return nil, hongguoAccountLibraryFailure(kind, "请求暂未被接受，请稍后重试")
		}
	}
	if !valid {
		return nil, hongguoAccountLibraryFailure(kind, "资料不完整，请重试")
	}
	return data, nil
}

func prepareHongguoAccountLibrary(ctx context.Context, credentials *hongguoAccountCredentials, device *hongguoAccountDevice, kind string, offset int) (*http.Request, error) {
	if (kind != "favorites" && kind != "history") || offset < 0 || offset > 2000 {
		return nil, hongguoAccountLibraryFailure(kind, "分页参数无效")
	}
	query := url.Values{"limit": {"20"}, "offset": {strconv.Itoa(offset)}, "book_type": {"2"}, "full_field": {"false"}}
	endpoint := hongguoAccountFavoritesURL
	if kind == "history" {
		endpoint = hongguoAccountHistoryURL
		query.Set("query_soft_deleted", "false")
		query.Set("last_min_read_timestamp_ms", "0")
		query.Set("is_first_load", strconv.FormatBool(offset == 0))
	} else {
		query.Set("offset", "0")
	}
	request, err := prepareHongguoAccountGet(ctx, credentials, device, endpoint, query)
	if err != nil {
		return nil, err
	}
	return request, nil
}

func fetchHongguoAccountLibrary(ctx context.Context, credentials *hongguoAccountCredentials, device *hongguoAccountDevice, kind string, offset int, metadata func(context.Context, string) (map[string]any, error)) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	request, err := prepareHongguoAccountLibrary(ctx, credentials, device, kind, offset)
	if err != nil {
		return nil, err
	}
	root, err := fetchHongguoAccountGet(request, hongguoAccountLibraryLabel(kind))
	if err != nil {
		return nil, &hongguoAccountLibraryError{kind: kind, message: err.Error()}
	}
	if kind == "favorites" {
		return parseHongguoAccountFavorites(ctx, root, offset, metadata)
	}
	return parseHongguoAccountHistory(root, offset)
}

func hongguoAccountLibraryEnvelope(kind string, offset int, next int64, more bool, retrieved, unsupported int, items []map[string]any) map[string]any {
	return map[string]any{
		"kind": kind, "items": items, "offset": offset, "nextOffset": next, "hasMore": more,
		"retrievedCount": retrieved, "unsupportedCount": unsupported, "online": false, "syncedAt": time.Now().Unix(),
	}
}

func hongguoAccountLibraryCover(value any) string {
	cover := hongguoAccountText(value, 8192)
	parsed, err := url.Parse(cover)
	if err != nil || parsed.Host == "" || parsed.User != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		return ""
	}
	return cover
}

func hongguoAccountLibrarySeries(id string, row map[string]any) map[string]any {
	title := hongguoAccountText(hongguoAccountField(row, row, "series_title", "book_name"), 200)
	if title == "" {
		return nil
	}
	count, valid := hongguoAccountFlexibleInteger(row["episode_cnt"])
	if !valid || count < 0 || count > 100000 {
		count = 0
	}
	return map[string]any{
		"seriesId": id, "title": title,
		"cover": hongguoAccountLibraryCover(hongguoAccountField(row, row, "series_cover", "thumb_url")),
		"intro": hongguoAccountText(hongguoAccountField(row, row, "series_intro", "video_desc"), 4000), "episodeCount": count,
	}
}

func parseHongguoAccountFavorites(ctx context.Context, root map[string]any, offset int, metadata func(context.Context, string) (map[string]any, error)) (map[string]any, error) {
	data, err := hongguoAccountLibraryData(root, "favorites")
	if err != nil {
		return nil, err
	}
	rows, valid := data["video_shelf_info"].([]any)
	if !valid || len(rows) > 2000 || offset < 0 || offset > len(rows) {
		return nil, hongguoAccountLibraryFailure("favorites", "片单格式或分页未识别，请刷新")
	}
	ordered := make([]map[string]any, len(rows))
	for index, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, hongguoAccountLibraryFailure("favorites", "片单条目格式异常，请重试")
		}
		ordered[index] = row
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		leftTime, _ := hongguoAccountFlexibleInteger(ordered[left]["collect_time"])
		rightTime, _ := hongguoAccountFlexibleInteger(ordered[right]["collect_time"])
		return leftTime > rightTime
	})
	end := offset + hongguoAccountFavoritesPageSize
	if end > len(ordered) {
		end = len(ordered)
	}
	page := ordered[offset:end]
	resolved := make([]map[string]any, len(page))
	attempted := 0
	var workers sync.WaitGroup
	gate := make(chan struct{}, 3)
	for index, row := range page {
		id := hongguoAccountID(row["series_id"])
		medium, validMedium := hongguoAccountFlexibleInteger(row["content_type"])
		if id == "" || !validMedium || (medium != 1 && medium != 1004) || metadata == nil {
			continue
		}
		attempted++
		workers.Add(1)
		go func(index int, id string, row map[string]any) {
			defer workers.Done()
			select {
			case gate <- struct{}{}:
				defer func() { <-gate }()
			case <-ctx.Done():
				return
			}
			detail, err := metadata(ctx, id)
			if err != nil || firstNonEmpty(hongguoAccountID(detail["series_id_str"]), hongguoAccountID(detail["series_id"])) != id {
				return
			}
			series := hongguoAccountLibrarySeries(id, detail)
			if series == nil {
				return
			}
			disabled, _ := hongguoAccountLibraryBool(detail["disable_play"])
			note := ""
			if disabled {
				note = "剧集暂不可播放"
			}
			resolved[index] = map[string]any{"series": series, "playable": !disabled, "online": true, "note": note, "contentKind": hongguoAccountContentKind(row, detail)}
		}(index, id, row)
	}
	workers.Wait()
	if ctx.Err() != nil {
		return nil, hongguoAccountLibraryFailure("favorites", "详情读取超时或已取消，请重试")
	}
	items := make([]map[string]any, 0, len(page))
	seen := map[string]bool{}
	unsupported := 0
	for _, item := range resolved {
		if item == nil {
			unsupported++
			continue
		}
		series := item["series"].(map[string]any)
		id := series["seriesId"].(string)
		if !seen[id] {
			seen[id] = true
			items = append(items, item)
		}
	}
	if attempted > 0 && len(items) == 0 {
		return nil, hongguoAccountLibraryFailure("favorites", "剧集详情暂未读取成功，请稍后重试")
	}
	more := end < len(rows)
	result := hongguoAccountLibraryEnvelope("favorites", offset, int64(end), more, len(page), unsupported, items)
	result["total"] = len(rows)
	return result, nil
}

func hongguoAccountHistoryProgress(row map[string]any, count int64) map[string]any {
	progress, ok := row["read_progress"].(map[string]any)
	if !ok {
		progress, ok = row["progress"].(map[string]any)
	}
	if !ok {
		progress = row
	}
	result := map[string]any{}
	index, valid := hongguoAccountFlexibleInteger(progress["vid_index"])
	if valid && index >= 0 && index < 100000 && (count == 0 || index < count) {
		result["episodeIndex"] = index
	}
	position, valid := hongguoAccountFlexibleInteger(hongguoAccountField(progress, progress, "current_play_position", "current_play_time"))
	if valid && position >= 0 && position <= 24*60*60*1000 {
		result["positionSeconds"] = position / 1000
	}
	var watched int64
	for _, container := range []map[string]any{progress, row} {
		for _, name := range []string{"read_timestamp_ms", "update_timestamp_ms"} {
			if value, ok := hongguoAccountFlexibleInteger(container[name]); ok && value > watched && value <= 4102444800000 {
				watched = value
			}
		}
	}
	if watched > 0 {
		result["watchedAt"] = watched
	}
	return result
}

func parseHongguoAccountHistory(root map[string]any, offset int) (map[string]any, error) {
	data, err := hongguoAccountLibraryData(root, "history")
	if err != nil {
		return nil, err
	}
	rows, valid := data["data_list"].([]any)
	if !valid {
		rows, valid = data["history_list"].([]any)
	}
	more, validMore := hongguoAccountLibraryBool(data["has_more"])
	next, validNext := hongguoAccountFlexibleInteger(data["next_offset"])
	if !valid || len(rows) > 500 || !validMore || offset < 0 || offset > 2000 {
		return nil, hongguoAccountLibraryFailure("history", "片单格式尚未识别，请重试")
	}
	if more && (len(rows) == 0 || !validNext || next <= int64(offset) || next > 2000) {
		return nil, hongguoAccountLibraryFailure("history", "分页没有前进，请刷新")
	}
	if !more && (!validNext || next < int64(offset) || next > 2000) {
		next = int64(offset)
	}
	items := make([]map[string]any, 0, len(rows))
	indices := map[string]int{}
	unsupported := 0
	for _, value := range rows {
		row, ok := value.(map[string]any)
		if !ok {
			return nil, hongguoAccountLibraryFailure("history", "片单条目格式异常，请重试")
		}
		deleted, validDelete := hongguoAccountLibraryBool(row["is_delete"])
		if _, exists := row["is_delete"]; exists && !validDelete {
			return nil, hongguoAccountLibraryFailure("history", "条目状态未确认，请重试")
		}
		if deleted {
			continue
		}
		book, ok := row["book_info"].(map[string]any)
		if !ok {
			book, ok = row["book_data"].(map[string]any)
		}
		if !ok {
			book = row
		}
		id := firstNonEmpty(hongguoAccountID(book["series_id_str"]), hongguoAccountID(book["series_id"]), hongguoAccountID(book["book_id_str"]), hongguoAccountID(book["book_id"]))
		medium, validMedium := hongguoAccountFlexibleInteger(hongguoAccountField(book, row, "content_type", "content_type"))
		if id == "" || !validMedium || (medium != 1 && medium != 1004) {
			unsupported++
			continue
		}
		series := hongguoAccountLibrarySeries(id, book)
		if series == nil {
			unsupported++
			continue
		}
		count, _ := series["episodeCount"].(int64)
		disabled, _ := hongguoAccountLibraryBool(book["disable_play"])
		item := map[string]any{"series": series, "playable": !disabled, "online": true, "note": "", "contentKind": hongguoAccountContentKind(book, book)}
		progress := hongguoAccountHistoryProgress(row, count)
		if len(progress) > 0 {
			item["history"] = progress
		}
		if index, duplicate := indices[id]; duplicate {
			previous, _ := items[index]["history"].(map[string]any)
			previousAt, _ := previous["watchedAt"].(int64)
			watchedAt, _ := progress["watchedAt"].(int64)
			if watchedAt > previousAt {
				items[index] = item
			}
		} else {
			indices[id] = len(items)
			items = append(items, item)
		}
	}
	result := hongguoAccountLibraryEnvelope("history", offset, next, more, len(rows), unsupported, items)
	if total, valid := hongguoAccountFlexibleInteger(data["total"]); valid && total >= 0 && total <= 1000000 {
		result["total"] = total
	}
	return result, nil
}

func (downloader *Downloader) hongguoAccountLibraryMetadata(ctx context.Context, id string) (map[string]any, error) {
	if !hongguoAccountSeriesIDPattern.MatchString(id) {
		return nil, errors.New("红果收藏详情编号无效")
	}
	payload, err := downloader.hongguotvEpisodes(ctx, id)
	if err != nil {
		return nil, err
	}
	if payload.Meta.SeriesID != id {
		return nil, errors.New("红果收藏详情编号不匹配")
	}
	if hongguoAccountText(payload.Meta.Title, 200) == "" {
		return nil, errors.New("红果收藏详情剧名不完整")
	}
	playable := false
	for _, episode := range payload.Episodes {
		if hongguoAccountSeriesIDPattern.MatchString(episode.VID) {
			playable = true
			break
		}
	}
	return map[string]any{
		"series_id_str": payload.Meta.SeriesID, "series_title": payload.Meta.Title,
		"series_cover": payload.Meta.Cover, "series_intro": payload.Meta.Intro,
		"episode_cnt": len(payload.Episodes), "disable_play": !playable,
	}, nil
}
