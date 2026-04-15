//go:build wasip1

package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"math/rand"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"mimusic-plugin-lxmusic/engine"
	"mimusic-plugin-lxmusic/urlmap"

	"github.com/mimusic-org/musicsdk"

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
	Songs           []musicsdk.SearchItem `json:"songs"`
	Quality         string                `json:"quality"`
	PlaylistID      int64                 `json:"playlist_id"`
	NewPlaylistName string                `json:"new_playlist_name"`
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
	var importedSongIDs []int64

	// 第一步：为每首歌曲构建 songInfo，收集到批处理列表
	type batchItem struct {
		song     musicsdk.SearchItem
		hash     string
		musicUrl string
		songInfo map[string]interface{}
	}
	var batch []batchItem

	// 收集所有待写入的 PutBatchItem
	var putBatchItems []urlmap.PutBatchItem

	for _, song := range request.Songs {
		// 归一化：musicId 和 songmid 互为 fallback（wy/kw 的 musicId 与 songmid 是同一个值）
		musicID := song.MusicID
		if musicID == "" {
			musicID = song.Songmid
		}
		songmid := song.Songmid
		if songmid == "" {
			songmid = song.MusicID
		}

		slog.Info("导入歌曲原始数据", "name", song.Name, "source", song.Source, "musicId", musicID, "songmid", songmid, "hash", song.Hash, "copyrightId", song.CopyrightId)

		// 构建 songInfo（包含各平台歌词获取器所需的所有字段）
		// - wy: musicId
		// - tx: songmid
		// - kg: name, singer, hash, duration
		// - kw: musicId
		// - mg: copyrightId
		songInfo := map[string]interface{}{
			"name":     song.Name,
			"singer":   song.Singer,
			"album":    song.Album,
			"source":   song.Source,
			"musicId":  musicID,
			"duration": song.Duration, // kg 平台歌词获取需要
		}
		if song.Hash != "" {
			songInfo["hash"] = song.Hash
		}
		if songmid != "" {
			songInfo["songmid"] = songmid
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

		putBatchItems = append(putBatchItems, urlmap.PutBatchItem{
			SongInfo: songInfo,
			Quality:  quality,
			Platform: song.Source,
		})
		batch = append(batch, batchItem{song: song, songInfo: songInfo})
	}

	// 批量生成 hash，只执行一次磁盘持久化（替代循环内逐个 Put + save）
	if len(putBatchItems) > 0 {
		hashes, err := h.urlmapStore.PutBatch(putBatchItems)
		if err != nil {
			slog.Error("批量生成 URL hash 失败", "error", err)
			for _, item := range batch {
				results = append(results, ImportResult{
					Name:    item.song.Name,
					Success: false,
					Error:   "生成 URL 映射失败: " + err.Error(),
				})
				failedCount++
			}
			batch = nil // 清空 batch，跳过后续步骤
		} else {
			for i := range batch {
				batch[i].hash = hashes[i]
				batch[i].musicUrl = "/api/v1/plugin/lxmusic/api/music/url/" + hashes[i]
			}
		}
	}

	// 第二步：如果有成功生成 hash 的条目，一次批量调用主程序 API
	if len(batch) > 0 {
		var batchBody []map[string]interface{}
		for _, item := range batch {
			body := map[string]interface{}{
				"title":        item.song.Name,
				"artist":       item.song.Singer,
				"album":        item.song.Album,
				"url":          item.musicUrl,
				"cover_url":    item.song.Img,
				"duration":     float64(item.song.Duration),
				"cache_hash":   item.hash,
				"lyric_source": "url",
				"lyric":        "/api/v1/plugin/lxmusic/api/lyric/url/" + item.hash,
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
			// 解析响应获取新添加歌曲的 ID
			var addResp struct {
				Songs []struct {
					ID int64 `json:"id"`
				} `json:"songs"`
			}
			if jsonErr := json.Unmarshal(resp.Body, &addResp); jsonErr != nil {
				slog.Error("解析添加歌曲响应失败", "error", jsonErr)
			}

			for i, item := range batch {
				results = append(results, ImportResult{
					Name:    item.song.Name,
					Success: true,
				})
				successCount++
				slog.Info("歌曲导入成功", "name", item.song.Name, "hash", item.hash)

				// 第三步：收集成功导入的歌曲 ID（歌词改为延迟加载）
				if i < len(addResp.Songs) {
					songID := addResp.Songs[i].ID
					if songID > 0 {
						importedSongIDs = append(importedSongIDs, songID)
					}
				}
			}
		}
	}

	// 第四步：歌单处理
	playlistID := request.PlaylistID
	playlistName := ""

	// 如果需要新建歌单
	if request.NewPlaylistName != "" {
		createBody, _ := json.Marshal(map[string]string{
			"name": request.NewPlaylistName,
			"type": "normal",
		})
		createResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
			Method: "POST",
			Path:   "/api/v1/playlists",
			Body:   createBody,
		})
		if err == nil && createResp.Success {
			var plResp struct {
				ID   int64  `json:"id"`
				Name string `json:"name"`
			}
			if json.Unmarshal(createResp.Body, &plResp) == nil {
				playlistID = plResp.ID
				playlistName = plResp.Name
				slog.Info("新建歌单成功", "id", playlistID, "name", playlistName)
			}
		} else {
			slog.Error("新建歌单失败", "name", request.NewPlaylistName, "error", err)
		}
	}

	// 将成功导入的歌曲添加到歌单
	if playlistID > 0 && len(importedSongIDs) > 0 {
		addToPlaylistBody, _ := json.Marshal(map[string]interface{}{
			"song_ids": importedSongIDs,
		})
		plSongsResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
			Method: "POST",
			Path:   fmt.Sprintf("/api/v1/playlists/%d/songs", playlistID),
			Body:   addToPlaylistBody,
		})
		if err != nil || !plSongsResp.Success {
			slog.Error("添加歌曲到歌单失败", "playlistID", playlistID, "error", err)
		} else {
			slog.Info("歌曲已添加到歌单", "playlistID", playlistID, "count", len(importedSongIDs))

			// 如果歌单没有封面，随机选一个导入歌曲的封面设置到歌单
			h.setPlaylistCoverIfEmpty(req, hostFunctions, playlistID, request.Songs)
		}
	}

	// 返回结果统计
	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": map[string]interface{}{
			"total":         len(request.Songs),
			"success":       successCount,
			"failed":        failedCount,
			"results":       results,
			"playlist_id":   playlistID,
			"playlist_name": playlistName,
		},
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// setPlaylistCoverIfEmpty 如果歌单没有封面，随机选一个导入歌曲的封面设置到歌单
func (h *SearchHandler) setPlaylistCoverIfEmpty(req *http.Request, hostFunctions pbplugin.HostFunctions, playlistID int64, songs []musicsdk.SearchItem) {
	// 收集有封面的歌曲
	var songsWithCover []musicsdk.SearchItem
	for _, song := range songs {
		if song.Img != "" {
			songsWithCover = append(songsWithCover, song)
		}
	}
	if len(songsWithCover) == 0 {
		return
	}

	// 获取歌单详情，检查是否已有封面
	getResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
		Method: "GET",
		Path:   fmt.Sprintf("/api/v1/playlists/%d", playlistID),
	})
	if err != nil || !getResp.Success {
		slog.Warn("获取歌单详情失败，跳过封面设置", "playlistID", playlistID, "error", err)
		return
	}

	var playlist struct {
		CoverPath string `json:"cover_path"`
		CoverURL  string `json:"cover_url"`
		Name      string `json:"name"`
		Type      string `json:"type"`
	}
	if err := json.Unmarshal(getResp.Body, &playlist); err != nil {
		slog.Warn("解析歌单详情失败，跳过封面设置", "playlistID", playlistID, "error", err)
		return
	}

	// 歌单已有封面，无需设置
	if playlist.CoverPath != "" || playlist.CoverURL != "" {
		return
	}

	// 随机选一个有封面的歌曲
	selectedSong := songsWithCover[rand.Intn(len(songsWithCover))]

	// 更新歌单封面
	updateBody, _ := json.Marshal(map[string]interface{}{
		"name":      playlist.Name,
		"type":      playlist.Type,
		"cover_url": selectedSong.Img,
	})
	updateResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
		Method: "PUT",
		Path:   fmt.Sprintf("/api/v1/playlists/%d", playlistID),
		Body:   updateBody,
	})
	if err != nil || !updateResp.Success {
		slog.Warn("更新歌单封面失败", "playlistID", playlistID, "error", err)
		return
	}

	slog.Info("已为歌单设置封面", "playlistID", playlistID, "coverURL", selectedSong.Img)
}

// fetchAndUpdateLyric 获取歌词并更新到歌曲库（失败时静默跳过）
func (h *SearchHandler) fetchAndUpdateLyric(req *http.Request, hostFunctions pbplugin.HostFunctions, songID int64, source string, songInfo map[string]interface{}) {
	// 获取对应平台的 LyricFetcher
	fetcher, ok := h.registry.GetLyricFetcher(source)
	if !ok {
		slog.Debug("平台不支持歌词获取", "source", source)
		return
	}

	// 获取歌词
	result, err := fetcher.GetLyric(songInfo)
	if err != nil {
		slog.Warn("获取歌词失败", "songID", songID, "source", source, "error", err)
		return
	}

	// 检查歌词是否为空
	if result.Lyric == "" {
		slog.Debug("歌词为空", "songID", songID, "source", source)
		return
	}

	// 调用 PUT /api/v1/songs/{id}/lyrics 更新歌词
	lyricPayload := map[string]string{
		"lyrics":       result.Lyric,
		"lyric_source": "scraped",
	}
	lyricBody, _ := json.Marshal(lyricPayload)

	lyricResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
		Method: "PUT",
		Path:   fmt.Sprintf("/api/v1/songs/%d/lyrics", songID),
		Body:   lyricBody,
	})

	if err != nil || !lyricResp.Success {
		errMsg := "更新歌词失败"
		if err != nil {
			errMsg += ": " + err.Error()
		} else {
			errMsg += ": " + lyricResp.Message
		}
		slog.Warn(errMsg, "songID", songID)
		return
	}

	slog.Info("歌词更新成功", "songID", songID, "source", source)
}

// HandleGetLyric 通过 hash 获取歌词（延迟加载 + 缓存写回）
// GET /lxmusic/api/lyric/url/{hash}
// 流程：
// 1. 通过 cache_hash 查主程序 DB 中的歌曲
// 2. 如果 lyric_source != "url"（已缓存），直接返回 lyric 文本
// 3. 如果 lyric_source == "url"，从平台获取歌词，写回 DB，返回歌词
func (h *SearchHandler) HandleGetLyric(req *http.Request) (*plugin.RouterResponse, error) {
	// 1. 从 URL path 提取 hash
	path := req.URL.Path
	hash := path[strings.LastIndex(path, "/")+1:]
	if hash == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 hash 参数"), nil
	}

	hostFunctions := pbplugin.NewHostFunctions()

	// 2. 查主程序 DB：GET /api/v1/songs?cache_hash={hash}&limit=1
	queryPath := "/api/v1/songs?cache_hash=" + hash + "&limit=1"
	songResp, err := hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
		Method: "GET",
		Path:   queryPath,
	})

	if err == nil && songResp.Success {
		var listResp struct {
			Songs []struct {
				ID          int64  `json:"id"`
				Lyric       string `json:"lyric"`
				LyricSource string `json:"lyric_source"`
			} `json:"songs"`
		}
		if json.Unmarshal(songResp.Body, &listResp) == nil && len(listResp.Songs) > 0 {
			song := listResp.Songs[0]

			// 3. 已缓存：lyric_source 不是 "url"，直接返回歌词文本
			if song.LyricSource != "url" {
				return h.lyricResponse(song.Lyric), nil
			}

			// 4. 未缓存：从平台获取歌词
			mapping, exists := h.urlmapStore.Get(hash)
			if !exists {
				return plugin.ErrorResponse(http.StatusNotFound, "URL mapping not found"), nil
			}

			fetcher, ok := h.registry.GetLyricFetcher(mapping.Platform)
			if !ok {
				return plugin.ErrorResponse(http.StatusBadRequest, "platform does not support lyric fetching"), nil
			}

			result, err := fetcher.GetLyric(mapping.SongInfo)
			if err != nil {
				return plugin.ErrorResponse(http.StatusInternalServerError, "failed to fetch lyric: "+err.Error()), nil
			}

			// 5. 写回 DB：PUT /api/v1/songs/{id}/lyrics
			if result.Lyric != "" && song.ID > 0 {
				lyricPayload, _ := json.Marshal(map[string]string{
					"lyrics":       result.Lyric,
					"lyric_source": "cached",
				})
				_, _ = hostFunctions.CallRouter(req.Context(), &pbplugin.CallRouterRequest{
					Method: "PUT",
					Path:   fmt.Sprintf("/api/v1/songs/%d/lyrics", song.ID),
					Body:   lyricPayload,
				})
			}

			return h.lyricResponse(result.Lyric), nil
		}
	}

	// 回退：DB 中没有对应歌曲记录，直接从平台获取（不写回）
	mapping, exists := h.urlmapStore.Get(hash)
	if !exists {
		return plugin.ErrorResponse(http.StatusNotFound, "URL mapping not found"), nil
	}
	fetcher, ok := h.registry.GetLyricFetcher(mapping.Platform)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "platform does not support lyric fetching"), nil
	}
	result, err := fetcher.GetLyric(mapping.SongInfo)
	if err != nil {
		return plugin.ErrorResponse(http.StatusInternalServerError, "failed to fetch lyric: "+err.Error()), nil
	}
	return h.lyricResponse(result.Lyric), nil
}

// lyricResponse 构建歌词 JSON 响应
// 歌词一旦获取就不会变化（首次从平台获取后写回 DB），因此使用永久缓存头
func (h *SearchHandler) lyricResponse(lyric string) *plugin.RouterResponse {
	response := map[string]interface{}{
		"code": 0,
		"data": map[string]string{"lyric": lyric},
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":  "application/json",
			"Cache-Control": "public, max-age=31536000, immutable",
		},
		Body: body,
	}
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
