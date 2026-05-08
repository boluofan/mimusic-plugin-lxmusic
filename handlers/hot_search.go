//go:build wasip1

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/mimusic-org/musicsdk"
	"github.com/mimusic-org/plugin/api/plugin"
)

type HotSearchHandler struct {
	registry *musicsdk.Registry
}

func NewHotSearchHandler(registry *musicsdk.Registry) *HotSearchHandler {
	return &HotSearchHandler{registry: registry}
}

func (h *HotSearchHandler) HandleHotSearch(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	fetcher, ok := h.registry.GetHotSearchFetcher("hotsearch")
	if !ok {
		return plugin.ErrorResponse(http.StatusInternalServerError, "hotsearch fetcher not found"), nil
	}
	list, err := fetcher.GetHotSearch(source)
	if err != nil {
		slog.Error("获取热搜失败", "source", source, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取热搜失败: "+err.Error()), nil
	}

	response := map[string]interface{}{
		"source": source,
		"list":   list,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}
