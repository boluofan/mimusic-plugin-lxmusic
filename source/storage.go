//go:build wasip1
// +build wasip1

package source

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Storage 处理音源数据的文件 I/O
type Storage struct {
	baseDir    string
	indexPath  string
	scriptsDir string
}

// NewStorage 创建存储实例
func NewStorage(baseDir string) (*Storage, error) {
	scriptsDir := filepath.Join(baseDir, "scripts")
	if err := os.MkdirAll(scriptsDir, 0755); err != nil {
		return nil, fmt.Errorf("create scripts dir: %w", err)
	}
	return &Storage{
		baseDir:    baseDir,
		indexPath:  filepath.Join(baseDir, "index.json"),
		scriptsDir: scriptsDir,
	}, nil
}

// LoadIndex 加载音源索引，文件不存在返回空列表
func (s *Storage) LoadIndex() ([]*SourceInfo, error) {
	data, err := os.ReadFile(s.indexPath)
	if err != nil {
		if os.IsNotExist(err) {
			return []*SourceInfo{}, nil
		}
		return nil, fmt.Errorf("read index: %w", err)
	}
	var index SourceIndex
	if err := json.Unmarshal(data, &index); err != nil {
		return nil, fmt.Errorf("parse index: %w", err)
	}
	return index.Sources, nil
}

// SaveIndex 保存音源索引
func (s *Storage) SaveIndex(sources []*SourceInfo) error {
	index := &SourceIndex{
		Version: "1.0",
		Sources: sources,
	}
	data, err := json.MarshalIndent(index, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal index: %w", err)
	}
	return os.WriteFile(s.indexPath, data, 0644)
}

// SaveScript 保存 JS 脚本文件
func (s *Storage) SaveScript(id string, content []byte) error {
	path := filepath.Join(s.scriptsDir, id+".js")
	return os.WriteFile(path, content, 0644)
}

// LoadScript 加载 JS 脚本文件
func (s *Storage) LoadScript(id string) ([]byte, error) {
	path := filepath.Join(s.scriptsDir, id+".js")
	return os.ReadFile(path)
}

// DeleteScript 删除 JS 脚本文件
func (s *Storage) DeleteScript(id string) error {
	path := filepath.Join(s.scriptsDir, id+".js")
	return os.Remove(path)
}
