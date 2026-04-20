//go:build wasip1

package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/mimusic-org/plugin/api/plugin"
	pluginhttp "github.com/mimusic-org/plugin/pkg/go-plugin-http/http"
)

// TipSearchHandler 搜索联想处理器
type TipSearchHandler struct{}

// NewTipSearchHandler 创建搜索联想处理器
func NewTipSearchHandler() *TipSearchHandler {
	return &TipSearchHandler{}
}

// isTVRequest 检查请求是否为TV模式（根据路径前缀 /tv/ 判断）
func (h *TipSearchHandler) isTVRequest(req *http.Request) bool {
	return strings.HasPrefix(req.URL.Path, "/tv/")
}

// HandleTipSearch 获取搜索联想
// GET /lxmusic/api/tipSearch?source=kw&name=xxx
func (h *TipSearchHandler) HandleTipSearch(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	name := req.URL.Query().Get("name")
	if name == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 name 参数"), nil
	}

	if source != "kw" {
		return plugin.ErrorResponse(http.StatusBadRequest, "暂不支持该平台: "+source), nil
	}

	list, err := h.getKuwoTipSearch(name)
	if err != nil {
		slog.Error("获取酷我搜索联想失败", "name", name, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取搜索联想失败: "+err.Error()), nil
	}

	// TV模式返回lxserver原始格式
	if h.isTVRequest(req) {
		body, _ := json.Marshal(list)
		return &plugin.RouterResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
		}, nil
	}

	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": list,
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// getKuwoTipSearch 获取酷我搜索联想
func (h *TipSearchHandler) getKuwoTipSearch(name string) ([]string, error) {
	urlStr := "https://tips.kuwo.cn/t.s?corp=kuwo&newver=3&p2p=1&notrace=0&c=mbox&w=" +
		url.QueryEscape(name) + "&encoding=utf8&rformat=json"

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
		WORDITEMS []struct {
			RELWORD string `json:"RELWORD"`
		} `json:"WORDITEMS"`
	}

	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}

	if len(result.WORDITEMS) == 0 {
		return []string{}, nil
	}

	list := make([]string, 0, len(result.WORDITEMS))
	for _, item := range result.WORDITEMS {
		list = append(list, item.RELWORD)
	}

	return list, nil
}
