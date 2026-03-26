//go:build wasip1

package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"mimusic-plugin-lxmusic/engine"
	"mimusic-plugin-lxmusic/musicsdk"
	"mimusic-plugin-lxmusic/urlmap"

	"github.com/mimusic-org/plugin/api/pbplugin"
	"github.com/mimusic-org/plugin/api/plugin"
)

// SearchHandler 搜索处理器
type SearchHandler struct {
	registry       *musicsdk.Registry
	runtimeManager *engine.RuntimeManager
	urlmapStore    *urlmap.Store
}

// NewSearchHandler 创建搜索处理器
func NewSearchHandler(registry *musicsdk.Registry, runtimeManager *engine.RuntimeManager, urlmapStore *urlmap.Store) *SearchHandler {
	return &SearchHandler{
		registry:       registry,
		runtimeManager: runtimeManager,
		urlmapStore:    urlmapStore,
	}
}

// HandleSearch 搜索歌曲（使用 musicsdk 原生搜索）
// GET /lxmusic/api/search?keyword=xxx&source_id=xxx&page=1
func (h *SearchHandler) HandleSearch(req *http.Request) (*plugin.RouterResponse, error) {
	keyword := req.URL.Query().Get("keyword")
	sourceID := req.URL.Query().Get("source_id") // 平台 ID: kg/kw/tx/wy/mg
	pageStr := req.URL.Query().Get("page")

	if keyword == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 keyword 参数"), nil
	}

	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	// 解析页码
	page := 1
	if pageStr != "" {
		if p, err := strconv.Atoi(pageStr); err == nil && p > 0 {
			page = p
		}
	}

	// 从 registry 获取对应平台的 Searcher
	searcher, ok := h.registry.Get(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	// 执行搜索
	result, err := searcher.Search(keyword, page, 30)
	if err != nil {
		slog.Error("搜索失败", "source_id", sourceID, "keyword", keyword, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "搜索失败: "+err.Error()), nil
	}

	// 返回结果
	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": result,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// HandleListPlatforms 列出内置平台
// GET /lxmusic/api/platforms
func (h *SearchHandler) HandleListPlatforms(req *http.Request) (*plugin.RouterResponse, error) {
	platforms := h.registry.All()

	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": platforms,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// ImportSongsRequest 导入歌曲请求
type ImportSongsRequest struct {
	Songs      []musicsdk.SearchItem `json:"songs"`
	Quality    string                `json:"quality"`
	PlaylistID int64                 `json:"playlist_id"`
}

// ImportResult 导入结果
type ImportResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

// HandleImportSongs 批量导入歌曲
// POST /lxmusic/api/songs/import
func (h *SearchHandler) HandleImportSongs(req *http.Request) (*plugin.RouterResponse, error) {
	var request ImportSongsRequest
	if err := json.NewDecoder(req.Body).Decode(&request); err != nil {
		return plugin.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()), nil
	}

	if len(request.Songs) == 0 {
		return plugin.ErrorResponse(http.StatusBadRequest, "请选择至少一首歌曲"), nil
	}

	quality := request.Quality
	if quality == "" {
		quality = "320k"
	}

	hostFunctions := pbplugin.NewHostFunctions()

	var results []ImportResult
	successCount := 0
	failedCount := 0

	// 逐个处理歌曲
	for _, song := range request.Songs {
		result := ImportResult{Name: song.Name}

		// 1. 构建 songInfo（包含平台特有字段）
		songInfo := map[string]interface{}{
			"name":    song.Name,
			"singer":  song.Singer,
			"album":   song.Album,
			"source":  song.Source,
			"musicId": song.MusicID,
		}
		// 添加平台特有字段
		if song.Hash != "" {
			songInfo["hash"] = song.Hash
		}
		if song.Songmid != "" {
			songInfo["songmid"] = song.Songmid
		}
		if song.StrMediaMid != "" {
			songInfo["strMediaMid"] = song.StrMediaMid
		}
		if song.AlbumMid != "" {
			songInfo["albumMid"] = song.AlbumMid
		}
		if song.CopyrightId != "" {
			songInfo["copyrightId"] = song.CopyrightId
		}
		if song.AlbumID != "" {
			songInfo["albumId"] = song.AlbumID
		}

		// 2. 用 urlmap.Store.Put 生成 hash
		hash, err := h.urlmapStore.Put(songInfo, quality, song.Source)
		if err != nil {
			slog.Error("生成 URL hash 失败", "name", song.Name, "error", err)
			result.Success = false
			result.Error = "生成 URL 映射失败: " + err.Error()
			results = append(results, result)
			failedCount++
			continue
		}

		// 3. 构造歌曲 URL 为 /api/v1/plugin/lxmusic/api/music/url/{hash}
		musicUrl := "/api/v1/plugin/lxmusic/api/music/url/" + hash

		// 4. 调用主程序 API 添加远程歌曲
		addSongBody := map[string]interface{}{
			"title":     song.Name,
			"artist":    song.Singer,
			"album":     song.Album,
			"url":       musicUrl,
			"cover_url": song.Img,
		}
		// 如果有 playlist_id，添加到请求中
		if request.PlaylistID > 0 {
			addSongBody["playlist_id"] = request.PlaylistID
		}
		bodyBytes, _ := json.Marshal(addSongBody)

		resp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
			Method: "POST",
			Path:   "/api/v1/songs/remote",
			Body:   bodyBytes,
		})

		if err != nil {
			slog.Error("调用主程序 API 失败", "name", song.Name, "error", err)
			result.Success = false
			result.Error = "添加歌曲失败: " + err.Error()
			results = append(results, result)
			failedCount++
			continue
		}

		if !resp.Success {
			result.Success = false
			result.Error = "添加歌曲失败: " + resp.Message
			results = append(results, result)
			failedCount++
			continue
		}

		result.Success = true
		results = append(results, result)
		successCount++
		slog.Info("歌曲导入成功", "name", song.Name, "hash", hash)
	}

	// 返回结果统计
	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": map[string]interface{}{
			"total":   len(request.Songs),
			"success": successCount,
			"failed":  failedCount,
			"results": results,
		},
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// HandleGetMusicUrl 获取播放 URL（通过 hash 查找）
// GET /lxmusic/api/music/url/{hash}
// 此路由不需要认证，主程序播放时直接调用
// 流程：hash查找 → 检查缓存 → 不存在则下载并保存到缓存 → 返回文件 stream
func (h *SearchHandler) HandleGetMusicUrl(req *http.Request) (*plugin.RouterResponse, error) {
	// 从 URL path 提取最后一段作为 hash
	path := req.URL.Path
	hash := path[strings.LastIndex(path, "/")+1:]
	if hash == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 hash 参数"), nil
	}

	// 从 urlmap 查找映射
	mapping, exists := h.urlmapStore.Get(hash)
	if !exists {
		return plugin.ErrorResponse(http.StatusNotFound, "URL 映射不存在"), nil
	}

	slog.Info("获取播放 URL", "hash", hash, "platform", mapping.Platform, "quality", mapping.Quality)

	// 初始化缓存管理器
	cache := NewMusicCache()

	// 1. 检查缓存是否存在
	if cachedPath, found := cache.FindCachedFile(hash); found {
		slog.Info("命中缓存", "hash", hash, "path", cachedPath)
		resp, err := cache.ServeCachedFile(cachedPath)
		if err == nil {
			return resp, nil
		}
		slog.Warn("读取缓存文件失败，将重新下载", "error", err)
	}

	// 2. 获取 URL + 下载缓存，带重试逻辑
	// GetMusicUrl 内部已有多源轮询/重试，但下载可能因非音频响应失败，此时需要重新获取 URL
	const maxRetries = 3
	var lastMusicUrl string
	var lastErr error

	for attempt := 1; attempt <= maxRetries; attempt++ {
		musicUrl, err := h.runtimeManager.GetMusicUrl(mapping.Platform, mapping.Quality, mapping.SongInfo)
		if err != nil {
			slog.Warn("获取播放 URL 失败", "hash", hash, "attempt", attempt, "maxRetries", maxRetries, "error", err)
			lastErr = err
			continue
		}
		if musicUrl == "" {
			slog.Warn("获取到的播放 URL 为空", "hash", hash, "attempt", attempt, "maxRetries", maxRetries)
			lastErr = fmt.Errorf("获取到的播放 URL 为空")
			continue
		}

		lastMusicUrl = musicUrl
		slog.Info("获取播放 URL 成功", "hash", hash, "url", musicUrl, "attempt", attempt)

		resp, err := cache.DownloadAndCache(hash, musicUrl)
		if err != nil {
			slog.Warn("下载并缓存失败", "hash", hash, "attempt", attempt, "maxRetries", maxRetries, "error", err)
			lastErr = err
			continue
		}

		return resp, nil
	}

	// 全部重试失败，回退到 302 重定向
	if lastMusicUrl != "" {
		slog.Warn("全部重试失败，回退到 302 重定向", "hash", hash, "attempts", maxRetries, "error", lastErr)
		return &plugin.RouterResponse{
			StatusCode: http.StatusFound,
			Headers: map[string]string{
				"Location": lastMusicUrl,
			},
			Body: nil,
		}, nil
	}

	slog.Error("获取播放 URL 全部失败", "hash", hash, "attempts", maxRetries, "error", lastErr)
	return plugin.ErrorResponse(http.StatusInternalServerError, "获取播放 URL 失败: "+lastErr.Error()), nil
}
