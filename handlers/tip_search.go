//go:build wasip1

package handlers

import (
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/mimusic-org/musicsdk"
	"github.com/mimusic-org/plugin/api/plugin"
)

type TipSearchHandler struct {
	registry *musicsdk.Registry
}

func NewTipSearchHandler(registry *musicsdk.Registry) *TipSearchHandler {
	return &TipSearchHandler{registry: registry}
}

func (h *TipSearchHandler) HandleTipSearch(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	name := req.URL.Query().Get("name")
	if name == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 name 参数"), nil
	}

	provider, ok := h.registry.GetTipSearchProvider("tipsearch")
	if !ok {
		return plugin.ErrorResponse(http.StatusInternalServerError, "tipsearch provider not found"), nil
	}
	list, err := provider.GetTips(source, name)
	if err != nil {
		slog.Error("获取搜索联想失败", "source", source, "name", name, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取搜索联想失败: "+err.Error()), nil
	}

	body, _ := json.Marshal(list)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}
