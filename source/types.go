//go:build wasip1
// +build wasip1

// Package source 提供洛雪音源管理功能。
package source

// SourceInfo 音源信息
type SourceInfo struct {
	ID          string `json:"id"`          // 唯一标识（基于文件名生成）
	Name        string `json:"name"`        // 从 JSDoc @name 解析
	Version     string `json:"version"`     // 从 JSDoc @version 解析
	Description string `json:"description"` // 从 JSDoc @description 解析
	Author      string `json:"author"`      // 从 JSDoc @author 解析
	Homepage    string `json:"homepage"`    // 从 JSDoc @homepage 解析
	Filename    string `json:"filename"`    // 原始文件名
	Script      string `json:"-"`           // JS 源码（不序列化到 JSON 响应）
	ImportedAt  string `json:"imported_at"` // 导入时间
	Enabled     bool   `json:"enabled"`     // 是否启用
}

// SourceMetadata 从 JS 文件头部 JSDoc 解析的元数据
type SourceMetadata struct {
	Name        string // @name
	Version     string // @version
	Description string // @description
	Author      string // @author
	Homepage    string // @homepage
}

// SourceIndex 音源索引（用于持久化）
type SourceIndex struct {
	Version string        `json:"version"`
	Sources []*SourceInfo `json:"sources"`
}
