package core

import (
	"context"
	"errors"
	"net/url"
	"strings"
)

func (d *Downloader) fetchHongguotvRanking(ctx context.Context, board rankingBoard, page int) (rankingPage, error) {
	if page != 1 {
		return rankingPage{}, errors.New("该榜单只有一页")
	}
	var payload struct {
		Board string           `json:"board"`
		Items []map[string]any `json:"items"`
	}
	if err := d.hongguotvJSON(ctx, "/rank", url.Values{"board": {board.upstreamKey}, "limit": {"100"}}, &payload); err != nil {
		return rankingPage{}, err
	}
	if payload.Board != board.upstreamKey || payload.Items == nil || len(payload.Items) > 100 {
		return rankingPage{}, errors.New("红果TV榜单响应无效，请重试")
	}
	result := rankingPage{Items: []rankingItem{}, TotalPages: 1}
	seen := map[string]bool{}
	previousRank := 0
	for _, row := range payload.Items {
		id, _ := row["series_id"].(string)
		title, _ := row["title"].(string)
		rank := serverInt(row, "rank")
		if !hongguoNumericID.MatchString(id) || strings.TrimSpace(title) == "" || rank <= previousRank || rank > 100 {
			return rankingPage{}, errors.New("红果TV榜单条目无效，请重试")
		}
		previousRank = rank
		if seen[id] {
			result.Warning = "服务返回了重复剧目，已去重并保留原始名次。"
			continue
		}
		seen[id] = true
		hot, _ := row["hot"].(string)
		score, _ := row["score"].(string)
		category, _ := row["copyright"].(string)
		intro, _ := row["intro"].(string)
		drama := Drama{ID: providerDramaID(sourceHongguotv, id), Source: sourceHongguotv, SourceID: id,
			Title: truncate(title, 512), Name: truncate(title, 512), ChannelName: "红果TV",
			EpisodeCount: serverInt(row, "episode_cnt"), TotalEpisode: serverInt(row, "episode_cnt"),
			Score: score, Views: hot, Category: category, CategoryName: category,
			Intro: truncate(intro, 512), Desc: truncate(intro, 512)}
		result.Items = append(result.Items, rankingItem{Rank: rank, Drama: drama, Metric: hot})
	}
	return result, nil
}
