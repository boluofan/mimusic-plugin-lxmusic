//go:build wasip1

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/mimusic-org/musicsdk"
	"github.com/mimusic-org/plugin/api/plugin"
)

// SongListHandler 歌单处理器
type SongListHandler struct {
	registry *musicsdk.Registry
}

// NewSongListHandler 创建歌单处理器
func NewSongListHandler(registry *musicsdk.Registry) *SongListHandler {
	return &SongListHandler{
		registry: registry,
	}
}

// HandleGetTags 获取指定平台的歌单标签
// GET /lxmusic/api/songlist/tags?source_id=xx
func (h *SongListHandler) HandleGetTags(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := getSourceID(req)
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetSongListProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	result, err := provider.GetTags()
	if err != nil {
		slog.Error("获取歌单标签失败", "source_id", sourceID, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取标签失败: "+err.Error()), nil
	}

	return h.buildResponse(req, result, sourceID)
}

// HandleGetList 获取歌单列表
// GET /lxmusic/api/songlist/list?source_id=xx&sort_id=xx&tag_id=xx&page=1
func (h *SongListHandler) HandleGetList(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := getSourceID(req)
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetSongListProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	sortID := req.URL.Query().Get("sort_id")
	if sortID == "" {
		sortID = req.URL.Query().Get("sortId")
	}
	tagID := req.URL.Query().Get("tag_id")
	if tagID == "" {
		tagID = req.URL.Query().Get("tagId")
	}
	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	result, err := provider.GetList(sortID, tagID, page)
	if err != nil {
		slog.Error("获取歌单列表失败", "source_id", sourceID, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取歌单列表失败: "+err.Error()), nil
	}

	return h.buildResponse(req, result, sourceID)
}

// HandleGetDetail 获取歌单详情
// GET /lxmusic/api/songlist/detail?source_id=xx&id=xxx&page=1
func (h *SongListHandler) HandleGetDetail(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := getSourceID(req)
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetSongListProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	id := req.URL.Query().Get("id")
	if id == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 id 参数"), nil
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	result, err := provider.GetListDetail(id, page)
	if err != nil {
		slog.Error("获取歌单详情失败", "source_id", sourceID, "id", id, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取歌单详情失败: "+err.Error()), nil
	}

	return h.buildResponse(req, result, sourceID)
}

// HandleSearch 搜索歌单
// GET /lxmusic/api/songlist/search?source_id=xx&keyword=xxx&page=1
func (h *SongListHandler) HandleSearch(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := getSourceID(req)
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetSongListProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	keyword := req.URL.Query().Get("keyword")
	if keyword == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 keyword 参数"), nil
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	limit, _ := strconv.Atoi(req.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = 20
	}

	result, err := provider.SearchSongList(keyword, page, limit)
	if err != nil {
		slog.Error("搜索歌单失败", "source_id", sourceID, "keyword", keyword, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "搜索歌单失败: "+err.Error()), nil
	}

	return h.buildResponse(req, result, sourceID)
}

// HandleGetSorts 获取排序选项
// GET /lxmusic/api/songlist/sorts?source_id=xx
func (h *SongListHandler) HandleGetSorts(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := getSourceID(req)
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetSongListProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	sortList := provider.GetSortList()

	return h.buildResponse(req, sortList, sourceID)
}

func (h *SongListHandler) buildResponse(req *http.Request, data interface{}, sourceID string) (*plugin.RouterResponse, error) {
	if isTVRequest(req) {
		return h.tvJsonResponse(data, sourceID)
	}
	return h.jsonResponse(data)
}

func (h *SongListHandler) jsonResponse(data interface{}) (*plugin.RouterResponse, error) {
	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": data,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

func (h *SongListHandler) tvJsonResponse(data interface{}, sourceID string) (*plugin.RouterResponse, error) {
	if sourceID == "" {
		sourceID = "unknown"
	}

	dataMap := convertToMap(data)
	dataMap["source"] = sourceID

	if list, ok := dataMap["list"].([]interface{}); ok {
		for i := range list {
			if item, ok := list[i].(map[string]interface{}); ok {
				item["source"] = sourceID
			}
		}
	}

	body, _ := json.Marshal(dataMap)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

func convertToMap(data interface{}) map[string]interface{} {
	var result map[string]interface{}
	jsonBytes, _ := json.Marshal(data)
	json.Unmarshal(jsonBytes, &result)
	return result
}
