//go:build wasip1

package handlers

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path"
	"strings"

	"mimusic-plugin-lxmusic/engine"
	"mimusic-plugin-lxmusic/source"

	"github.com/mimusic-org/plugin/api/plugin"
	pluginhttp "github.com/mimusic-org/plugin/pkg/go-plugin-http/http"
)

// SourceHandler 音源管理处理器
type SourceHandler struct {
	manager        *source.Manager
	runtimeManager *engine.RuntimeManager
	pluginID       int64
}

// NewSourceHandler 创建音源处理器
func NewSourceHandler(manager *source.Manager, runtimeManager *engine.RuntimeManager, pluginID int64) *SourceHandler {
	return &SourceHandler{
		manager:        manager,
		runtimeManager: runtimeManager,
		pluginID:       pluginID,
	}
}

// HandleListSources 列出所有音源
// GET /lxmusic/api/sources
func (h *SourceHandler) HandleListSources(req *http.Request) (*plugin.RouterResponse, error) {
	sources := h.manager.ListSources()

	type SourceItem struct {
		ID          string   `json:"id"`
		Name        string   `json:"name"`
		Version     string   `json:"version"`
		Description string   `json:"description"`
		Author      string   `json:"author"`
		Filename    string   `json:"filename"`
		ImportedAt  string   `json:"imported_at"`
		Enabled     bool     `json:"enabled"`
		Platforms   []string `json:"platforms"`
	}

	items := make([]SourceItem, 0, len(sources))
	for _, s := range sources {
		var platforms []string
		if sr, ok := h.runtimeManager.GetRuntime(s.ID); ok {
			if cfg := sr.Config(); cfg != nil && cfg.Sources != nil {
				platforms = make([]string, 0, len(cfg.Sources))
				for platform := range cfg.Sources {
					platforms = append(platforms, platform)
				}
			}
		}
		if platforms == nil {
			platforms = []string{}
		}

		items = append(items, SourceItem{
			ID:          s.ID,
			Name:        s.Name,
			Version:     s.Version,
			Description: s.Description,
			Author:      s.Author,
			Filename:    s.Filename,
			ImportedAt:  s.ImportedAt,
			Enabled:     s.Enabled,
			Platforms:   platforms,
		})
	}

	body, _ := json.Marshal(items)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// HandleImportSource 导入音源
// POST /lxmusic/api/sources/import
func (h *SourceHandler) HandleImportSource(req *http.Request) (*plugin.RouterResponse, error) {
	// 解析 multipart form，32MB 限制
	if err := req.ParseMultipartForm(32 << 20); err != nil {
		slog.Error("解析 multipart form 失败", "error", err)
		return plugin.ErrorResponse(http.StatusBadRequest, "无效的请求格式: "+err.Error()), nil
	}

	// 获取上传的文件
	file, header, err := req.FormFile("file")
	if err != nil {
		slog.Error("获取上传文件失败", "error", err)
		return plugin.ErrorResponse(http.StatusBadRequest, "请上传文件"), nil
	}
	defer file.Close()

	// 读取文件内容
	content, err := io.ReadAll(file)
	if err != nil {
		slog.Error("读取文件内容失败", "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "读取文件失败"), nil
	}

	filename := header.Filename
	slog.Info("收到文件上传", "filename", filename, "size", len(content))

	// 根据文件扩展名判断类型
	if strings.HasSuffix(strings.ToLower(filename), ".js") {
		// 导入单个 JS 文件
		info, err := h.manager.ImportFromJS(filename, content)
		if err != nil {
			slog.Error("导入 JS 文件失败", "error", err)
			return plugin.ErrorResponse(http.StatusBadRequest, "导入失败: "+err.Error()), nil
		}
		// 如果音源默认启用，自动加载到 RuntimeManager
		if info.Enabled {
			if loadErr := h.runtimeManager.LoadSource(info.ID, info.Script, h.pluginID); loadErr != nil {
				slog.Warn("自动加载音源失败", "id", info.ID, "error", loadErr)
			}
		}
		return plugin.SuccessResponse(info), nil

	} else if strings.HasSuffix(strings.ToLower(filename), ".zip") {
		// 导入 ZIP 文件
		sources, err := h.manager.ImportFromZIP(content)
		if err != nil {
			slog.Error("导入 ZIP 文件失败", "error", err)
			return plugin.ErrorResponse(http.StatusBadRequest, "导入失败: "+err.Error()), nil
		}
		// 对每个导入的音源，如果默认启用，自动加载到 RuntimeManager
		for _, info := range sources {
			if info.Enabled {
				if loadErr := h.runtimeManager.LoadSource(info.ID, info.Script, h.pluginID); loadErr != nil {
					slog.Warn("自动加载音源失败", "id", info.ID, "error", loadErr)
				}
			}
		}
		return plugin.SuccessResponse(sources), nil

	} else {
		return plugin.ErrorResponse(http.StatusBadRequest, "不支持的文件格式，请上传 .js 或 .zip 文件"), nil
	}
}

// HandleImportSourceFromURL 从 URL 导入音源
// POST /lxmusic/api/sources/import-url
func (h *SourceHandler) HandleImportSourceFromURL(req *http.Request) (*plugin.RouterResponse, error) {
	// 解析请求体
	var body struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		slog.Error("解析请求体失败", "error", err)
		return plugin.ErrorResponse(http.StatusBadRequest, "无效的请求格式: "+err.Error()), nil
	}

	// 验证 URL 格式
	if body.URL == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 url 参数"), nil
	}

	parsedURL, err := url.Parse(body.URL)
	if err != nil {
		return plugin.ErrorResponse(http.StatusBadRequest, "无效的 URL 格式: "+err.Error()), nil
	}

	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return plugin.ErrorResponse(http.StatusBadRequest, "URL 必须以 http:// 或 https:// 开头"), nil
	}

	slog.Info("开始从 URL 下载音源", "url", body.URL)

	// 使用插件 HTTP 客户端下载文件
	httpReq, err := pluginhttp.NewRequest("GET", body.URL, nil)
	if err != nil {
		slog.Error("创建 HTTP 请求失败", "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "创建请求失败: "+err.Error()), nil
	}

	// 设置 User-Agent
	httpReq.Header.Set("User-Agent", "mimusic-plugin-lxmusic/1.0")

	resp, err := pluginhttp.DefaultClient.Do(httpReq)
	if err != nil {
		slog.Error("下载文件失败", "error", err)
		return plugin.ErrorResponse(http.StatusBadGateway, "下载文件失败: "+err.Error()), nil
	}
	defer resp.Body.Close()

	// 检查响应状态码
	if resp.StatusCode != http.StatusOK {
		slog.Error("下载文件失败", "statusCode", resp.StatusCode)
		return plugin.ErrorResponse(http.StatusBadGateway, "下载失败，远程服务器返回状态码: "+http.StatusText(resp.StatusCode)), nil
	}

	// 读取响应内容
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Error("读取响应内容失败", "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "读取文件内容失败: "+err.Error()), nil
	}

	// 从 URL 路径提取文件名
	filename := path.Base(parsedURL.Path)
	if filename == "" || filename == "." || filename == "/" {
		filename = "source.js"
	}
	// 确保文件名以 .js 结尾
	if !strings.HasSuffix(strings.ToLower(filename), ".js") {
		filename += ".js"
	}

	slog.Info("下载完成", "filename", filename, "size", len(content))

	// 复用现有的导入逻辑
	info, err := h.manager.ImportFromJS(filename, content)
	if err != nil {
		slog.Error("导入 JS 文件失败", "error", err)
		return plugin.ErrorResponse(http.StatusBadRequest, "导入失败: "+err.Error()), nil
	}

	// 如果音源默认启用，自动加载到 RuntimeManager
	if info.Enabled {
		if err := h.runtimeManager.LoadSource(info.ID, info.Script, h.pluginID); err != nil {
			slog.Warn("自动加载音源失败", "id", info.ID, "error", err)
			// 不影响导入结果，继续返回成功
		}
	}

	return plugin.SuccessResponse(info), nil
}

// HandleToggleSource 启用/禁用音源
// PUT /lxmusic/api/sources/toggle
func (h *SourceHandler) HandleToggleSource(req *http.Request) (*plugin.RouterResponse, error) {
	var body struct {
		ID      string `json:"id"`
		Enabled bool   `json:"enabled"`
	}
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		return plugin.ErrorResponse(http.StatusBadRequest, "无效的请求格式: "+err.Error()), nil
	}

	if body.ID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 id 参数"), nil
	}

	if body.Enabled {
		// 启用音源
		if err := h.manager.EnableSource(body.ID); err != nil {
			return plugin.ErrorResponse(http.StatusNotFound, "启用音源失败: "+err.Error()), nil
		}

		// 加载到 RuntimeManager
		script, err := h.manager.GetSourceScript(body.ID)
		if err != nil {
			return plugin.ErrorResponse(http.StatusInternalServerError, "获取音源脚本失败: "+err.Error()), nil
		}
		if err := h.runtimeManager.LoadSource(body.ID, script, h.pluginID); err != nil {
			// 加载失败，回滚启用状态
			_ = h.manager.DisableSource(body.ID)
			return plugin.ErrorResponse(http.StatusInternalServerError, "加载音源失败: "+err.Error()), nil
		}

		slog.Info("音源已启用并加载", "id", body.ID)
	} else {
		// 禁用音源
		if err := h.manager.DisableSource(body.ID); err != nil {
			return plugin.ErrorResponse(http.StatusNotFound, "禁用音源失败: "+err.Error()), nil
		}

		// 从 RuntimeManager 卸载
		h.runtimeManager.UnloadSource(body.ID)

		slog.Info("音源已禁用并卸载", "id", body.ID)
	}

	response := map[string]interface{}{
		"success": true,
		"message": "操作成功",
	}
	rspBody, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       rspBody,
	}, nil
}

// HandleDeleteSource 删除音源
// DELETE /lxmusic/api/sources?id=xxx
func (h *SourceHandler) HandleDeleteSource(req *http.Request) (*plugin.RouterResponse, error) {
	// 从 query parameter 获取 id
	id := req.URL.Query().Get("id")
	if id == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 id 参数"), nil
	}

	// 删除前先从 RuntimeManager 卸载
	h.runtimeManager.UnloadSource(id)

	if err := h.manager.DeleteSource(id); err != nil {
		slog.Error("删除音源失败", "id", id, "error", err)
		return plugin.ErrorResponse(http.StatusNotFound, "删除失败: "+err.Error()), nil
	}

	// 返回成功消息
	response := map[string]interface{}{
		"success": true,
		"message": "删除成功",
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}
