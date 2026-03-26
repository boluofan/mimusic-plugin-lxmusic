//go:build wasip1

package handlers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/mimusic-org/plugin/api/plugin"
	pluginhttp "github.com/mimusic-org/plugin/pkg/go-plugin-http/http"
)

const (
	// CacheDir 缓存目录名
	CacheDir = "music_cache"
	// DataDir 数据目录（与 main.go 中保持一致）
	DataDir = "/lxmusic"
	// MinAudioSize 最小有效音频文件大小（1KB），低于此值认为是错误响应
	MinAudioSize = 1024
)

// MusicCache 音乐缓存管理器
type MusicCache struct {
	cacheRoot string
}

// NewMusicCache 创建音乐缓存管理器
func NewMusicCache() *MusicCache {
	cacheRoot := filepath.Join(DataDir, CacheDir)
	return &MusicCache{
		cacheRoot: cacheRoot,
	}
}

// getCachePath 根据 hash 生成缓存路径
// 将 16 位 hash 分割为两级目录：前 7 位 / 后 9 位
// 例如：5d7677b2ca1b8b1f -> {cache_root}/5d7677b/2ca1b8b1f/
func (c *MusicCache) getCachePath(hash string) string {
	if len(hash) < 16 {
		// hash 长度不足，直接使用 hash 作为目录
		return filepath.Join(c.cacheRoot, hash)
	}
	prefix := hash[:7]
	suffix := hash[7:]
	return filepath.Join(c.cacheRoot, prefix, suffix)
}

// FindCachedFile 查找缓存文件
// 返回文件路径和是否存在
func (c *MusicCache) FindCachedFile(hash string) (string, bool) {
	dir := c.getCachePath(hash)

	// 检查目录是否存在
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", false
	}

	// 查找以 hash 为前缀的文件
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		// 检查文件名是否以 hash 开头
		if strings.HasPrefix(name, hash) {
			return filepath.Join(dir, name), true
		}
	}

	return "", false
}

// GetContentTypeFromExt 根据文件扩展名获取 Content-Type
func GetContentTypeFromExt(ext string) string {
	switch strings.ToLower(ext) {
	case ".mp3":
		return "audio/mpeg"
	case ".flac":
		return "audio/flac"
	case ".ogg":
		return "audio/ogg"
	case ".m4a":
		return "audio/mp4"
	case ".wav":
		return "audio/wav"
	default:
		return "audio/mpeg"
	}
}

// isAudioContentType 检查 Content-Type 是否为音频类型
func isAudioContentType(contentType string) bool {
	ct := strings.ToLower(contentType)
	return strings.Contains(ct, "audio/") ||
		strings.Contains(ct, "video/mp4") ||
		strings.Contains(ct, "application/octet-stream")
}

// GetExtFromContentType 根据 Content-Type 获取文件扩展名
func GetExtFromContentType(contentType string) string {
	ct := strings.ToLower(contentType)
	switch {
	case strings.Contains(ct, "audio/mpeg"):
		return ".mp3"
	case strings.Contains(ct, "audio/flac"):
		return ".flac"
	case strings.Contains(ct, "audio/ogg"):
		return ".ogg"
	case strings.Contains(ct, "audio/x-m4a"), strings.Contains(ct, "audio/mp4"), strings.Contains(ct, "video/mp4"):
		return ".m4a"
	case strings.Contains(ct, "audio/wav"):
		return ".wav"
	default:
		return ".mp3"
	}
}

// ServeCachedFile 返回缓存文件的流式响应
func (c *MusicCache) ServeCachedFile(filePath string) (*plugin.RouterResponse, error) {
	// 读取整个文件内容（WASM 环境下无法流式返回，只能读取全部）
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("读取缓存文件失败: %w", err)
	}

	// 获取文件扩展名
	ext := filepath.Ext(filePath)
	contentType := GetContentTypeFromExt(ext)

	slog.Info("返回缓存文件", "path", filePath, "size", len(content), "contentType", contentType)

	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":   contentType,
			"Content-Length": fmt.Sprintf("%d", len(content)),
		},
		Body: content,
	}, nil
}

// DownloadAndCache 下载音乐并缓存，同时返回响应
// 流程：解析重定向获取真实 URL → GET 下载 → 写入缓存 → 返回响应
// 如果下载失败，返回 nil 以便调用者回退到 302 重定向
func (c *MusicCache) DownloadAndCache(hash string, musicUrl string) (*plugin.RouterResponse, error) {
	slog.Info("开始下载并缓存", "hash", hash, "url", musicUrl)

	// 1. 解析重定向获取真实 URL
	realUrl, err := resolveRedirects(musicUrl)
	if err != nil {
		slog.Warn("解析重定向失败", "url", musicUrl, "error", err)
		return nil, err
	}
	if realUrl != musicUrl {
		slog.Info("重定向解析完成", "originalUrl", musicUrl, "realUrl", realUrl)
	}

	// 2. 创建 HTTP 请求（使用解析后的真实 URL）
	req, err := pluginhttp.NewRequest("GET", realUrl, nil)
	if err != nil {
		slog.Error("创建 HTTP 请求失败", "error", err)
		return nil, err
	}

	// 设置常用请求头
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

	// 发起请求
	resp, err := pluginhttp.DefaultClient.Do(req)
	if err != nil {
		slog.Error("下载音乐文件失败", "error", err)
		return nil, err
	}
	defer resp.Body.Close()

	// 检查状态码
	if resp.StatusCode != http.StatusOK {
		slog.Error("下载音乐文件失败", "statusCode", resp.StatusCode)
		return nil, fmt.Errorf("下载失败，状态码: %d", resp.StatusCode)
	}

	// 获取 Content-Type
	contentType := resp.Header.Get("Content-Type")

	// 检查 Content-Type 是否为音频类型
	// 如果返回 JSON/HTML/text 等非音频类型，说明是错误响应（如 PHP 代理返回的 JSON 错误）
	if !isAudioContentType(contentType) {
		// 读取 body 用于错误日志
		body, _ := io.ReadAll(resp.Body)
		slog.Warn("下载返回非音频内容，可能是错误响应",
			"hash", hash,
			"contentType", contentType,
			"bodyLen", len(body),
			"body", string(body))
		return nil, fmt.Errorf("下载返回非音频内容 (Content-Type: %s, body: %s)", contentType, string(body))
	}

	ext := GetExtFromContentType(contentType)

	// 确保缓存目录存在
	cacheDir := c.getCachePath(hash)
	if err := os.MkdirAll(cacheDir, 0755); err != nil {
		slog.Warn("创建缓存目录失败", "dir", cacheDir, "error", err)
		// 继续处理，不影响返回响应
	}

	// 临时文件路径
	tempPath := filepath.Join(cacheDir, hash+".tmp")
	finalPath := filepath.Join(cacheDir, hash+ext)

	// 创建临时文件
	tmpFile, err := os.Create(tempPath)
	if err != nil {
		slog.Warn("创建临时文件失败", "path", tempPath, "error", err)
		// 缓存失败，但仍然读取并返回数据
		content, readErr := io.ReadAll(resp.Body)
		if readErr != nil {
			return nil, readErr
		}
		return &plugin.RouterResponse{
			StatusCode: http.StatusOK,
			Headers: map[string]string{
				"Content-Type": contentType,
			},
			Body: content,
		}, nil
	}

	// 读取响应并同时写入文件
	// 由于 WASM 环境下 RouterResponse 需要完整 body，无法真正流式返回
	// 这里使用 io.TeeReader 同时写入文件和内存
	var content []byte
	content, err = io.ReadAll(io.TeeReader(resp.Body, tmpFile))
	tmpFile.Close()

	if err != nil {
		slog.Warn("读取响应失败", "error", err)
		os.Remove(tempPath)
		return nil, err
	}

	// 验证下载内容大小：低于 MinAudioSize 认为不是有效音频
	if len(content) < MinAudioSize {
		os.Remove(tempPath)
		slog.Warn("下载内容过小，可能是错误响应",
			"hash", hash,
			"size", len(content),
			"body", string(content))
		return nil, fmt.Errorf("下载内容过小 (%d bytes)，可能是错误响应: %s", len(content), string(content))
	}

	// 重命名临时文件为正式文件
	if err := os.Rename(tempPath, finalPath); err != nil {
		slog.Warn("重命名缓存文件失败", "from", tempPath, "to", finalPath, "error", err)
		// 尝试删除临时文件
		os.Remove(tempPath)
	} else {
		slog.Info("音乐已缓存", "path", finalPath, "size", len(content))
	}

	// 返回响应
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers: map[string]string{
			"Content-Type":   contentType,
			"Content-Length": fmt.Sprintf("%d", len(content)),
		},
		Body: content,
	}, nil
}

// resolveRedirects 使用 HEAD 请求解析 URL 重定向，返回最终真实 URL
// 参考 lxserver server.ts checkRedirect 实现
// 最多跟随 5 层重定向，超限则返回最后一个 URL
func resolveRedirects(rawUrl string) (string, error) {
	const maxRedirects = 5
	currentUrl := rawUrl

	for depth := 0; depth < maxRedirects; depth++ {
		req, err := pluginhttp.NewRequest("HEAD", currentUrl, nil)
		if err != nil {
			return "", fmt.Errorf("create HEAD request: %w", err)
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36")

		resp, err := pluginhttp.DefaultClient.Do(req)
		if err != nil {
			return "", fmt.Errorf("HEAD request failed: %w", err)
		}
		resp.Body.Close()

		slog.Info("resolveRedirects: HEAD 响应",
			"depth", depth,
			"url", currentUrl,
			"statusCode", resp.StatusCode,
			"contentType", resp.Header.Get("Content-Type"),
			"location", resp.Header.Get("Location"))

		// 检查重定向
		switch resp.StatusCode {
		case http.StatusMovedPermanently, http.StatusFound, http.StatusSeeOther,
			http.StatusTemporaryRedirect, http.StatusPermanentRedirect:

			location := resp.Header.Get("Location")
			if location == "" {
				return "", fmt.Errorf("redirect without Location header")
			}
			// 处理相对路径
			if !strings.HasPrefix(location, "http") {
				idx := strings.Index(currentUrl[8:], "/") // 跳过 https://
				if idx > 0 {
					location = currentUrl[:8+idx] + location
				}
			}
			slog.Info("resolveRedirects: 跟随重定向", "from", currentUrl, "to", location)
			currentUrl = location
			continue

		default:
			// 非重定向状态，检查是否为错误状态码
			if resp.StatusCode >= 400 {
				return "", fmt.Errorf("HEAD request returned status %d", resp.StatusCode)
			}
			// 200 或其他成功状态码，返回当前 URL
			return currentUrl, nil
		}
	}

	// 重定向层数超限，返回最后的 URL
	return currentUrl, nil
}
