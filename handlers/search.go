//go:build wasip1

package handlers

import (
	"encoding/json"
	"errors"
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

type SearchHandler struct {
	registry       *musicsdk.Registry
	runtimeManager *engine.RuntimeManager
	urlmapStore    *urlmap.Store
}

func NewSearchHandler(registry *musicsdk.Registry, runtimeManager *engine.RuntimeManager, urlmapStore *urlmap.Store) *SearchHandler {
	return &SearchHandler{
		registry:       registry,
		runtimeManager: runtimeManager,
		urlmapStore:    urlmapStore,
	}
}

func (h *SearchHandler) HandleSearch(req *http.Request) (*plugin.RouterResponse, error) {
	keyword := req.URL.Query().Get("keyword")
	if keyword == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 keyword 参数"), nil
	}

	sourceID := req.URL.Query().Get("source")
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	searcher, ok := h.registry.Get(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	result, err := searcher.Search(keyword, page, 30)
	if err != nil {
		slog.Error("搜索失败", "source", sourceID, "keyword", keyword, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "搜索失败: "+err.Error()), nil
	}

	body, _ := json.Marshal(result)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

func (h *SearchHandler) HandleListPlatforms(req *http.Request) (*plugin.RouterResponse, error) {
	platforms := h.registry.All()

	body, _ := json.Marshal(platforms)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

type ImportSongsRequest struct {
	Songs           []musicsdk.SearchItem `json:"songs"`
	Quality         string                `json:"quality"`
	PlaylistID      int64                 `json:"playlist_id"`
	NewPlaylistName string                `json:"new_playlist_name"`
}

type ImportResult struct {
	Name    string `json:"name"`
	Success bool   `json:"success"`
	Error   string `json:"error,omitempty"`
}

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

	type batchItem struct {
		song     musicsdk.SearchItem
		hash     string
		musicUrl string
		songInfo map[string]interface{}
	}
	var batch []batchItem

	var putBatchItems []urlmap.PutBatchItem

	for _, song := range request.Songs {
		musicID := song.MusicID
		if musicID == "" {
			musicID = song.Songmid
		}
		songmid := song.Songmid
		if songmid == "" {
			songmid = song.MusicID
		}

		songInfo := map[string]interface{}{
			"name":     song.Name,
			"singer":   song.Singer,
			"album":    song.Album,
			"source":   song.Source,
			"musicId":  musicID,
			"duration": song.Duration,
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
			batch = nil
		} else {
			for i := range batch {
				batch[i].hash = hashes[i]
				batch[i].musicUrl = "/api/v1/plugin/lxmusic/api/music/url/" + hashes[i]
			}
		}
	}

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
			for _, item := range batch {
				results = append(results, ImportResult{
					Name:    item.song.Name,
					Success: false,
					Error:   "添加失败: " + errMsg,
				})
				failedCount++
			}
		} else {
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

				if i < len(addResp.Songs) {
					songID := addResp.Songs[i].ID
					if songID > 0 {
						importedSongIDs = append(importedSongIDs, songID)
					}
				}
			}
		}
	}

	playlistID := request.PlaylistID
	playlistName := ""

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
			h.setPlaylistCoverIfEmpty(req, hostFunctions, playlistID, request.Songs)
		}
	}

	responseData := map[string]interface{}{
		"total":         len(request.Songs),
		"success":       successCount,
		"failed":        failedCount,
		"results":       results,
		"playlist_id":   playlistID,
		"playlist_name": playlistName,
	}

	if h.runtimeManager.Count() == 0 {
		responseData["warning"] = "注意：当前未配置有效的洛雪音源，导入的歌曲暂时无法播放。请在「音源管理」中导入音源脚本。"
	}

	body, _ := json.Marshal(responseData)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

func (h *SearchHandler) setPlaylistCoverIfEmpty(req *http.Request, hostFunctions pbplugin.HostFunctions, playlistID int64, songs []musicsdk.SearchItem) {
	var songsWithCover []musicsdk.SearchItem
	for _, song := range songs {
		if song.Img != "" {
			songsWithCover = append(songsWithCover, song)
		}
	}
	if len(songsWithCover) == 0 {
		return
	}

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

	if playlist.CoverPath != "" || playlist.CoverURL != "" {
		return
	}

	selectedSong := songsWithCover[rand.Intn(len(songsWithCover))]

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

func (h *SearchHandler) HandleGetLyric(req *http.Request) (*plugin.RouterResponse, error) {
	path := req.URL.Path
	hash := path[strings.LastIndex(path, "/")+1:]
	if hash == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 hash 参数"), nil
	}

	hostFunctions := pbplugin.NewHostFunctions()

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

			if song.LyricSource != "url" {
				return h.lyricResponse(song.Lyric), nil
			}

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

func (h *SearchHandler) HandleGetMusicUrl(req *http.Request) (*plugin.RouterResponse, error) {
	path := req.URL.Path
	hash := path[strings.LastIndex(path, "/")+1:]
	if hash == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 hash 参数"), nil
	}

	accessToken := req.URL.Query().Get("access_token")

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

	mapping, exists := h.urlmapStore.Get(hash)
	if !exists {
		return plugin.ErrorResponse(http.StatusNotFound, "URL 映射不存在"), nil
	}

	slog.Info("缓存未命中，获取播放 URL", "hash", hash, "platform", mapping.Platform, "quality", mapping.Quality)

	musicUrl, err := h.runtimeManager.GetMusicUrl(mapping.Platform, mapping.Quality, mapping.SongInfo)
	if err != nil {
		slog.Error("获取播放 URL 失败", "hash", hash, "error", err)
		if errors.Is(err, engine.ErrNoSourceLoaded) {
			return plugin.ErrorResponse(http.StatusServiceUnavailable, "尚未配置有效的洛雪音源，无法获取播放链接。请在「音源管理」中导入并启用音源脚本。"), nil
		}
		if errors.Is(err, engine.ErrPlatformNotSupported) {
			return plugin.ErrorResponse(http.StatusServiceUnavailable, "当前没有支持该平台的音源，请导入支持该平台的音源脚本。"), nil
		}
		return plugin.ErrorResponse(http.StatusBadGateway, "获取播放 URL 失败: "+err.Error()), nil
	}
	if musicUrl == "" {
		return plugin.ErrorResponse(http.StatusBadGateway, "获取到的播放 URL 为空"), nil
	}

	slog.Info("获取播放 URL 成功，重定向到缓存接口", "hash", hash, "url", musicUrl)

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

// HandleMusicUrl 获取播放链接
// POST /lxmusic/api/music/url
// Body: {"source":"mg","songmid":"xxx","quality":"320k"}
// Response: {"url": "https://...", "type": "320k", "source": "mg"}
func (h *SearchHandler) HandleMusicUrl(req *http.Request) (*plugin.RouterResponse, error) {
	if req.Method != http.MethodPost {
		return plugin.ErrorResponse(http.StatusMethodNotAllowed, "只支持 POST 方法"), nil
	}

	var body struct {
		Source  string `json:"source"`
		Songmid string `json:"songmid"`
		Quality string `json:"quality"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return plugin.ErrorResponse(http.StatusBadRequest, "无效的请求参数: "+err.Error()), nil
	}

	if body.Source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}
	if body.Songmid == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 songmid 参数"), nil
	}

	quality := body.Quality
	if quality == "" {
		quality = "320k"
	}

	songInfo := map[string]interface{}{
		"source":  body.Source,
		"songmid": body.Songmid,
	}

	musicUrl, err := h.runtimeManager.GetMusicUrl(body.Source, quality, songInfo)
	if err != nil {
		slog.Error("获取播放 URL 失败", "source", body.Source, "songmid", body.Songmid, "error", err)
		if errors.Is(err, engine.ErrNoSourceLoaded) {
			return plugin.ErrorResponse(http.StatusServiceUnavailable, "尚未配置有效的洛雪音源，无法获取播放链接。请在「音源管理」中导入并启用音源脚本。"), nil
		}
		if errors.Is(err, engine.ErrPlatformNotSupported) {
			return plugin.ErrorResponse(http.StatusServiceUnavailable, "当前没有支持该平台的音源，请导入支持该平台的音源脚本。"), nil
		}
		return plugin.ErrorResponse(http.StatusBadGateway, "获取播放 URL 失败: "+err.Error()), nil
	}

	if musicUrl == "" {
		return plugin.ErrorResponse(http.StatusBadGateway, "获取到的播放 URL 为空"), nil
	}

	response := map[string]interface{}{
		"url":    musicUrl,
		"type":   quality,
		"source": body.Source,
	}
	respBody, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       respBody,
	}, nil
}

// HandleLyric 获取歌词
// GET /lxmusic/api/music/lyric?source=kw&songmid=xxx
// Response: {"lyric": "...", "tlyric": "...", "rlyric": "...", "lxlyric": "..."}
func (h *SearchHandler) HandleLyric(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	songmid := req.URL.Query().Get("songmid")
	if songmid == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 songmid 参数"), nil
	}

	songInfo := map[string]interface{}{
		"source":  source,
		"songmid": songmid,
	}

	fetcher, ok := h.registry.GetLyricFetcher(source)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "平台不支持歌词获取: "+source), nil
	}

	result, err := fetcher.GetLyric(songInfo)
	if err != nil {
		slog.Error("获取歌词失败", "source", source, "songmid", songmid, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取歌词失败: "+err.Error()), nil
	}

	response := map[string]interface{}{
		"lyric":   result.Lyric,
		"tlyric":  result.TLyric,
		"rlyric":  result.RLyric,
		"lxlyric": result.LxLyric,
	}
	respBody, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       respBody,
	}, nil
}
