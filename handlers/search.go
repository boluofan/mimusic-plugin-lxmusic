//go:build wasip1

package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
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

	// 第一步：为每首歌曲生成 hash，收集成功项到 batch 列表
	type batchItem struct {
		song     musicsdk.SearchItem
		hash     string
		musicUrl string
	}
	var batch []batchItem

	for _, song := range request.Songs {
		result := ImportResult{Name: song.Name}

		// 构建 songInfo
		songInfo := map[string]interface{}{
			"name":    song.Name,
			"singer":  song.Singer,
			"album":   song.Album,
			"source":  song.Source,
			"musicId": song.MusicID,
		}
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

		hash, err := h.urlmapStore.Put(songInfo, quality, song.Source)
		if err != nil {
			slog.Error("生成 URL hash 失败", "name", song.Name, "error", err)
			result.Success = false
			result.Error = "生成 URL 映射失败: " + err.Error()
			results = append(results, result)
			failedCount++
			continue
		}

		musicUrl := "/api/v1/plugin/lxmusic/api/music/url/" + hash
		batch = append(batch, batchItem{song: song, hash: hash, musicUrl: musicUrl})
	}

	// 第二步：如果有成功生成 hash 的条目，一次批量调用主程序 API
	if len(batch) > 0 {
		var batchBody []map[string]interface{}
		for _, item := range batch {
			body := map[string]interface{}{
				"title":     item.song.Name,
				"artist":    item.song.Singer,
				"album":     item.song.Album,
				"url":       item.musicUrl,
				"cover_url": item.song.Img,
			}
			batchBody = append(batchBody, body)
		}

		bodyBytes, _ := json.Marshal(batchBody)
		slog.Info("批量调用主程序 API 添加歌曲", "count", len(batch))

		resp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
			Method: "POST",
			Path:   "/api/v1/songs/remote",
			Body:   bodyBytes,
		})

		if err != nil || !resp.Success {
			errMsg := "调用主程序 API 失败"
			if err != nil {
				errMsg += ": " + err.Error()
			} else {
				errMsg += ": " + resp.Message
			}
			slog.Error(errMsg, "count", len(batch))
			// 批量请求失败，所有 batch 项均为失败
			for _, item := range batch {
				results = append(results, ImportResult{
					Name:    item.song.Name,
					Success: false,
					Error:   "添加失败: " + errMsg,
				})
				failedCount++
			}
		} else {
			for _, item := range batch {
				results = append(results, ImportResult{
					Name:    item.song.Name,
					Success: true,
				})
				successCount++
				slog.Info("歌曲导入成功", "name", item.song.Name, "hash", item.hash)
			}
		}
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
// 流程：检查主程序缓存 → 命中则重定向到缓存接口 → 未命中则获取 CDN URL 并重定向到缓存接口（带 url 参数）
func (h *SearchHandler) HandleGetMusicUrl(req *http.Request) (*plugin.RouterResponse, error) {
	// 1. 从 URL path 提取 hash
	path := req.URL.Path
	hash := path[strings.LastIndex(path, "/")+1:]
	if hash == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 hash 参数"), nil
	}

	// 透传 access_token
	accessToken := req.URL.Query().Get("access_token")

	// 2. 通过 CallRouter HEAD 请求检查主程序缓存是否存在（内部调用，无网络开销）
	hostFunctions := pbplugin.NewHostFunctions()
	cachePath := "/api/v1/cache/" + hash
	if accessToken != "" {
		cachePath += "?access_token=" + accessToken
	}
	cacheResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
		Method: "HEAD",
		Path:   cachePath,
	})
	if err == nil && cacheResp.StatusCode == http.StatusOK {
		// 缓存命中：直接重定向到 cache 接口，不调用 JS runtime
		slog.Info("缓存命中，跳过 URL 解析", "hash", hash)
		redirectURL := fmt.Sprintf("/api/v1/cache/%s", hash)
		if accessToken != "" {
			redirectURL += "?access_token=" + url.QueryEscape(accessToken)
		}
		return &plugin.RouterResponse{
			StatusCode: http.StatusFound,
			Headers:    map[string]string{"Location": redirectURL},
		}, nil
	}

	// 3. 缓存未命中：查 urlmap
	mapping, exists := h.urlmapStore.Get(hash)
	if !exists {
		return plugin.ErrorResponse(http.StatusNotFound, "URL 映射不存在"), nil
	}

	slog.Info("缓存未命中，获取播放 URL", "hash", hash, "platform", mapping.Platform, "quality", mapping.Quality)

	// 4. 调用 JS runtime 获取 CDN URL
	musicUrl, err := h.runtimeManager.GetMusicUrl(mapping.Platform, mapping.Quality, mapping.SongInfo)
	if err != nil {
		slog.Error("获取播放 URL 失败", "hash", hash, "error", err)
		return plugin.ErrorResponse(http.StatusBadGateway, "获取播放 URL 失败: "+err.Error()), nil
	}
	if musicUrl == "" {
		return plugin.ErrorResponse(http.StatusBadGateway, "获取到的播放 URL 为空"), nil
	}

	slog.Info("获取播放 URL 成功，重定向到缓存接口", "hash", hash, "url", musicUrl)

	// 5. 302 重定向到主程序缓存接口，带上 CDN URL
	redirectURL := fmt.Sprintf("/api/v1/cache/%s?url=%s", hash, url.QueryEscape(musicUrl))
	if accessToken != "" {
		redirectURL += "&access_token=" + url.QueryEscape(accessToken)
	}
	if req.URL.Query().Get("prefetch") == "true" {
		redirectURL += "&prefetch=true"
	}
	return &plugin.RouterResponse{
		StatusCode: http.StatusFound,
		Headers:    map[string]string{"Location": redirectURL},
	}, nil
}
