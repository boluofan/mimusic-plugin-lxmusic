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

type LeaderboardHandler struct {
	registry *musicsdk.Registry
}

func NewLeaderboardHandler(registry *musicsdk.Registry) *LeaderboardHandler {
	return &LeaderboardHandler{registry: registry}
}

func (h *LeaderboardHandler) HandleGetBoards(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	provider, ok := h.registry.GetLeaderboardProvider("leaderboard")
	if !ok {
		return plugin.ErrorResponse(http.StatusInternalServerError, "leaderboard provider not found"), nil
	}
	boards, err := provider.GetBoards(source)
	if err != nil {
		slog.Error("获取排行榜分类失败", "source", source, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取排行榜分类失败: "+err.Error()), nil
	}

	response := map[string]interface{}{
		"source": source,
		"list":   boards,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

func (h *LeaderboardHandler) HandleGetList(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	boardID := req.URL.Query().Get("boardId")
	if boardID == "" {
		boardID = req.URL.Query().Get("bangid")
	}
	if boardID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 boardId/bangid 参数"), nil
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	provider, ok := h.registry.GetLeaderboardProvider("leaderboard")
	if !ok {
		return plugin.ErrorResponse(http.StatusInternalServerError, "leaderboard provider not found"), nil
	}
	list, total, err := provider.GetList(source, boardID, page)
	if err != nil {
		slog.Error("获取排行榜歌曲失败", "source", source, "boardId", boardID, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取排行榜歌曲失败: "+err.Error()), nil
	}

	response := map[string]interface{}{
		"source": source,
		"total":  total,
		"list":   list,
		"limit":  100,
		"page":   page,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}
