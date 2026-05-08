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

// LeaderboardHandler 排行榜处理器
type LeaderboardHandler struct {
	registry *musicsdk.Registry
}

// NewLeaderboardHandler 创建排行榜处理器
func NewLeaderboardHandler(registry *musicsdk.Registry) *LeaderboardHandler {
	return &LeaderboardHandler{
		registry: registry,
	}
}

// HandleGetBoards 获取排行榜分类
// GET /lxmusic/api/leaderboard/boards?source_id=xx
func (h *LeaderboardHandler) HandleGetBoards(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := req.URL.Query().Get("source_id")
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetLeaderboardProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	boards, err := provider.GetBoards(sourceID)
	if err != nil {
		slog.Error("获取排行榜分类失败", "source_id", sourceID, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取排行榜分类失败: "+err.Error()), nil
	}

	return h.jsonResponse(boards)
}

// HandleGetList 获取排行榜歌曲列表
// GET /lxmusic/api/leaderboard/list?source_id=xx&board_id=xxx&page=1
func (h *LeaderboardHandler) HandleGetList(req *http.Request) (*plugin.RouterResponse, error) {
	sourceID := req.URL.Query().Get("source_id")
	if sourceID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source_id 参数"), nil
	}

	provider, ok := h.registry.GetLeaderboardProvider(sourceID)
	if !ok {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的平台: "+sourceID), nil
	}

	boardID := req.URL.Query().Get("board_id")
	if boardID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 board_id 参数"), nil
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	list, total, err := provider.GetList(sourceID, boardID, page)
	if err != nil {
		slog.Error("获取排行榜列表失败", "source_id", sourceID, "board_id", boardID, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取排行榜列表失败: "+err.Error()), nil
	}

	return h.jsonResponse(map[string]interface{}{
		"list":  list,
		"total": total,
		"page":  page,
	})
}

// jsonResponse 构建 JSON 成功响应
func (h *LeaderboardHandler) jsonResponse(data interface{}) (*plugin.RouterResponse, error) {
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
