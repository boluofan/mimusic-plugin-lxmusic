//go:build wasip1

// Package urlmap 提供歌曲 URL 哈希映射功能。
package urlmap

// MusicUrlMapping 歌曲 URL 映射
type MusicUrlMapping struct {
	SongInfo  map[string]interface{} `json:"songInfo"`  // 歌曲信息（平台特有字段）
	Quality   string                 `json:"quality"`   // 音质 "128k", "320k", "flac" 等
	Platform  string                 `json:"platform"`  // 平台标识 "kg", "kw" 等
	CreatedAt string                 `json:"createdAt"` // RFC3339 时间戳
}

// urlMapIndex 持久化索引结构
type urlMapIndex struct {
	Version  string                      `json:"version"`
	Mappings map[string]*MusicUrlMapping `json:"mappings"` // hash → mapping
}
