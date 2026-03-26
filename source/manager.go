//go:build wasip1
// +build wasip1

package source

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"time"
)

// Manager 管理已导入的音源
type Manager struct {
	sources   map[string]*SourceInfo // ID -> SourceInfo
	idCounter int                    // ID 计数器（用于生成唯一 ID）
	storage   *Storage               // 持久化存储
}

// NewManager 创建一个新的音源管理器
// baseDir: 存储目录的基础路径（WASM 沙盒内路径）
func NewManager(baseDir string) (*Manager, error) {
	// 创建存储实例
	storage, err := NewStorage(baseDir)
	if err != nil {
		return nil, fmt.Errorf("create storage: %w", err)
	}

	m := &Manager{
		sources:   make(map[string]*SourceInfo),
		idCounter: 0,
		storage:   storage,
	}

	// 从磁盘加载已持久化的音源
	if err := m.loadFromDisk(); err != nil {
		slog.Warn("加载持久化音源失败", "error", err)
		// 不返回错误，允许继续使用空的音源列表
	}

	return m, nil
}

// loadFromDisk 从磁盘加载已持久化的音源
func (m *Manager) loadFromDisk() error {
	// 加载索引
	sources, err := m.storage.LoadIndex()
	if err != nil {
		return fmt.Errorf("load index: %w", err)
	}

	loadedCount := 0
	for _, info := range sources {
		// 加载对应的 JS 脚本
		script, err := m.storage.LoadScript(info.ID)
		if err != nil {
			slog.Warn("加载音源脚本失败，跳过", "id", info.ID, "name", info.Name, "error", err)
			continue
		}

		info.Script = string(script)
		m.sources[info.ID] = info
		loadedCount++
	}

	if loadedCount > 0 {
		slog.Info("已从磁盘加载音源", "count", loadedCount)
	}
	return nil
}

// ImportFromJS 导入单个 JS 文件
// filename: 原始文件名
// content: JS 文件内容
func (m *Manager) ImportFromJS(filename string, content []byte) (*SourceInfo, error) {
	// 1. 校验 JS 内容
	if err := ValidateJSContent(content); err != nil {
		return nil, fmt.Errorf("invalid javascript: %w", err)
	}

	// 2. 解析元数据
	metadata, err := ParseMetadata(content)
	if err != nil {
		return nil, fmt.Errorf("parse metadata: %w", err)
	}

	// 3. 如果没有 @name，从文件名推断
	name := metadata.Name
	if name == "" {
		name = InferNameFromFilename(filename)
	}

	// 4. 生成唯一 ID
	id := m.generateID(name)

	// 5. 创建 SourceInfo
	info := &SourceInfo{
		ID:          id,
		Name:        name,
		Version:     metadata.Version,
		Description: metadata.Description,
		Author:      metadata.Author,
		Filename:    filename,
		Script:      string(content),
		ImportedAt:  time.Now().Format(time.RFC3339),
		Enabled:     true, // 导入时默认启用
	}

	// 6. 存入 map
	m.sources[id] = info

	// 7. 持久化保存
	if err := m.storage.SaveScript(id, content); err != nil {
		// 回滚内存状态
		delete(m.sources, id)
		return nil, fmt.Errorf("save script: %w", err)
	}
	if err := m.saveIndex(); err != nil {
		// 回滚：删除脚本文件和内存状态
		_ = m.storage.DeleteScript(id)
		delete(m.sources, id)
		return nil, fmt.Errorf("save index: %w", err)
	}

	slog.Info("音源导入成功", "id", id, "name", name, "filename", filename)
	return info, nil
}

// ImportFromZIP 从 ZIP 文件导入音源
// content: ZIP 文件内容
// 返回所有成功导入的 SourceInfo 列表
func (m *Manager) ImportFromZIP(content []byte) ([]*SourceInfo, error) {
	// 使用 bytes.NewReader 创建 zip.Reader
	reader, err := zip.NewReader(bytes.NewReader(content), int64(len(content)))
	if err != nil {
		return nil, fmt.Errorf("open zip: %w", err)
	}

	var imported []*SourceInfo

	// 遍历 ZIP 中的所有文件
	for _, file := range reader.File {
		// 跳过目录
		if file.FileInfo().IsDir() {
			continue
		}

		// 只处理 .js 文件
		if !strings.HasSuffix(strings.ToLower(file.Name), ".js") {
			slog.Debug("跳过非 JS 文件", "filename", file.Name)
			continue
		}

		// 读取文件内容
		fileContent, err := m.readZipFile(file)
		if err != nil {
			slog.Warn("读取 ZIP 文件失败", "filename", file.Name, "error", err)
			continue
		}

		// 导入 JS 文件
		info, err := m.ImportFromJS(filepath.Base(file.Name), fileContent)
		if err != nil {
			slog.Warn("导入音源失败", "filename", file.Name, "error", err)
			continue
		}

		imported = append(imported, info)
	}

	slog.Info("ZIP 导入完成", "total", len(imported))
	return imported, nil
}

// readZipFile 读取 ZIP 中的单个文件内容
func (m *Manager) readZipFile(file *zip.File) ([]byte, error) {
	rc, err := file.Open()
	if err != nil {
		return nil, fmt.Errorf("open file in zip: %w", err)
	}
	defer rc.Close()

	content, err := io.ReadAll(rc)
	if err != nil {
		return nil, fmt.Errorf("read file in zip: %w", err)
	}

	return content, nil
}

// ListSources 返回所有已导入的音源
func (m *Manager) ListSources() []*SourceInfo {
	sources := make([]*SourceInfo, 0, len(m.sources))
	for _, info := range m.sources {
		sources = append(sources, info)
	}
	return sources
}

// GetSource 按 ID 获取音源
func (m *Manager) GetSource(id string) *SourceInfo {
	return m.sources[id]
}

// GetSourceScript 获取音源的 JS 源码
func (m *Manager) GetSourceScript(id string) (string, error) {
	info := m.sources[id]
	if info == nil {
		return "", fmt.Errorf("source not found: %s", id)
	}
	return info.Script, nil
}

// DeleteSource 删除音源
func (m *Manager) DeleteSource(id string) error {
	info, exists := m.sources[id]
	if !exists {
		return fmt.Errorf("source not found: %s", id)
	}

	// 先从内存删除
	delete(m.sources, id)

	// 持久化：删除脚本文件
	if err := m.storage.DeleteScript(id); err != nil {
		slog.Warn("删除脚本文件失败", "id", id, "error", err)
		// 继续执行，不影响索引更新
	}

	// 持久化：更新索引
	if err := m.saveIndex(); err != nil {
		// 回滚内存状态
		m.sources[id] = info
		return fmt.Errorf("save index: %w", err)
	}

	slog.Info("音源已删除", "id", id)
	return nil
}

// EnableSource 启用音源
func (m *Manager) EnableSource(id string) error {
	info, exists := m.sources[id]
	if !exists {
		return fmt.Errorf("source not found: %s", id)
	}

	if info.Enabled {
		return nil // 已经启用，无需操作
	}

	info.Enabled = true

	if err := m.saveIndex(); err != nil {
		info.Enabled = false // 回滚
		return fmt.Errorf("save index: %w", err)
	}

	slog.Info("音源已启用", "id", id)
	return nil
}

// DisableSource 禁用音源
func (m *Manager) DisableSource(id string) error {
	info, exists := m.sources[id]
	if !exists {
		return fmt.Errorf("source not found: %s", id)
	}

	if !info.Enabled {
		return nil // 已经禁用，无需操作
	}

	info.Enabled = false

	if err := m.saveIndex(); err != nil {
		info.Enabled = true // 回滚
		return fmt.Errorf("save index: %w", err)
	}

	slog.Info("音源已禁用", "id", id)
	return nil
}

// GetEnabledSources 返回所有已启用的音源列表
func (m *Manager) GetEnabledSources() []*SourceInfo {
	var sources []*SourceInfo
	for _, info := range m.sources {
		if info.Enabled {
			sources = append(sources, info)
		}
	}
	return sources
}

// saveIndex 保存音源索引到磁盘
func (m *Manager) saveIndex() error {
	sources := make([]*SourceInfo, 0, len(m.sources))
	for _, info := range m.sources {
		sources = append(sources, info)
	}
	return m.storage.SaveIndex(sources)
}

// Close 清理管理器
func (m *Manager) Close() {
	m.sources = make(map[string]*SourceInfo)
	m.idCounter = 0
	slog.Info("音源管理器已关闭")
}

// generateID 生成唯一 ID
// 使用 name 的 slug 形式，如果重复则添加计数器后缀
func (m *Manager) generateID(name string) string {
	// 生成基础 slug
	slug := m.toSlug(name)
	if slug == "" {
		slug = "source"
	}

	// 检查是否已存在
	id := slug
	counter := 1
	for {
		if _, exists := m.sources[id]; !exists {
			break
		}
		counter++
		id = fmt.Sprintf("%s_%d", slug, counter)
	}

	return id
}

// toSlug 将名称转换为 slug 形式
// 例如: "My Source" -> "my-source", "网易云" -> "网易云"
func (m *Manager) toSlug(name string) string {
	// 转小写
	slug := strings.ToLower(name)
	// 替换空格为横线
	slug = strings.ReplaceAll(slug, " ", "-")
	// 移除特殊字符（保留字母、数字、横线、下划线、中文）
	var result strings.Builder
	for _, r := range slug {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' || r == '_' || r >= 0x4e00 {
			result.WriteRune(r)
		}
	}
	return result.String()
}
