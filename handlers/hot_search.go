//go:build wasip1

package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"strings"

	"github.com/mimusic-org/plugin/api/plugin"
	pluginhttp "github.com/mimusic-org/plugin/pkg/go-plugin-http/http"
)

// HotSearchHandler 热搜处理器
type HotSearchHandler struct{}

// NewHotSearchHandler 创建热搜处理器
func NewHotSearchHandler() *HotSearchHandler {
	return &HotSearchHandler{}
}

// isTVRequest 检查请求是否为TV模式（根据路径前缀 /tv/ 判断）
func (h *HotSearchHandler) isTVRequest(req *http.Request) bool {
	return strings.HasPrefix(req.URL.Path, "/tv/")
}

// HandleHotSearch 获取热搜榜
// GET /lxmusic/api/hotSearch?source=kw
func (h *HotSearchHandler) HandleHotSearch(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	if source != "kw" {
		return plugin.ErrorResponse(http.StatusBadRequest, "暂不支持该平台: "+source), nil
	}

	list, err := h.getKuwoHotSearch()
	if err != nil {
		slog.Error("获取酷我热搜失败", "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取热搜失败: "+err.Error()), nil
	}

	// TV模式返回lxserver原始格式
	if h.isTVRequest(req) {
		response := map[string]interface{}{
			"source": "kw",
			"list":   list,
		}
		body, _ := json.Marshal(response)
		return &plugin.RouterResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
		}, nil
	}

	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": map[string]interface{}{
			"source": "kw",
			"list":   list,
		},
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// getKuwoHotSearch 获取酷我热搜
func (h *HotSearchHandler) getKuwoHotSearch() ([]string, error) {
	urlStr := "http://hotword.kuwo.cn/hotword.s?prod=kwplayer_ar_9.3.0.1&corp=kuwo&newver=2&vipver=9.3.0.1&source=kwplayer_ar_9.3.0.1_40.apk&p2p=1&notrace=0&uid=0&plat=kwplayer_ar&rformat=json&encoding=utf8&tabid=1"

	resp, err := pluginhttp.Get(urlStr)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, err
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// 解析响应
	var result struct {
		Status   string `json:"status"`
		Tagvalue []struct {
			Key string `json:"key"`
		} `json:"tagvalue"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if result.Status != "ok" {
		return nil, err
	}

	list := make([]string, 0, len(result.Tagvalue))
	for _, item := range result.Tagvalue {
		list = append(list, item.Key)
	}

	return list, nil
}
