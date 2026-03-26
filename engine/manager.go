//go:build wasip1

// Package engine 封装 goja JS 运行时，用于执行洛雪音源脚本。
package engine

import (
	"fmt"
	"log/slog"
	"math/rand"
)

// RuntimeManager 管理所有已加载的音源运行时
type RuntimeManager struct {
	runtimes map[string]*SourceRuntime // sourceID → runtime
}

// NewRuntimeManager 创建新的 RuntimeManager
func NewRuntimeManager() *RuntimeManager {
	return &RuntimeManager{
		runtimes: make(map[string]*SourceRuntime),
	}
}

// LoadSource 创建音源运行时并缓存
func (rm *RuntimeManager) LoadSource(sourceID string, script string) error {
	// 如果已存在，先卸载
	if _, exists := rm.runtimes[sourceID]; exists {
		rm.UnloadSource(sourceID)
	}

	sr, err := NewSourceRuntime(sourceID, script)
	if err != nil {
		return fmt.Errorf("create source runtime for %s: %w", sourceID, err)
	}

	rm.runtimes[sourceID] = sr
	slog.Info("RuntimeManager: 音源已加载", "sourceID", sourceID)
	return nil
}

// UnloadSource 卸载音源运行时
func (rm *RuntimeManager) UnloadSource(sourceID string) {
	if sr, exists := rm.runtimes[sourceID]; exists {
		sr.Close()
		delete(rm.runtimes, sourceID)
		slog.Info("RuntimeManager: 音源已卸载", "sourceID", sourceID)
	}
}

// ReloadSource 卸载并重新加载
func (rm *RuntimeManager) ReloadSource(sourceID string, script string) error {
	rm.UnloadSource(sourceID)
	return rm.LoadSource(sourceID, script)
}

// GetRuntime 获取指定源的运行时
func (rm *RuntimeManager) GetRuntime(sourceID string) (*SourceRuntime, bool) {
	sr, ok := rm.runtimes[sourceID]
	return sr, ok
}

// GetMusicUrl 多源轮询获取播放 URL
// 1. 收集所有支持该 platform 的已加载 runtime
// 2. 随机打乱顺序（math/rand）
// 3. 轮询调用：
//   - 只有 1 个源：重试 3 次
//   - 多个源：每个源各试 1 次
//
// 4. 成功即返回 URL，全部失败返回错误
func (rm *RuntimeManager) GetMusicUrl(platform, quality string, musicInfo map[string]interface{}) (string, error) {
	// 1. 收集所有支持该 platform 的 runtime
	var candidates []*SourceRuntime
	for _, sr := range rm.runtimes {
		if sr.SupportsPlatform(platform) {
			candidates = append(candidates, sr)
		}
	}

	if len(candidates) == 0 {
		return "", fmt.Errorf("no source supports platform: %s", platform)
	}

	// 2. 随机打乱顺序
	rand.Shuffle(len(candidates), func(i, j int) {
		candidates[i], candidates[j] = candidates[j], candidates[i]
	})

	var lastErr error

	// 3. 轮询调用
	if len(candidates) == 1 {
		// 只有 1 个源：重试 3 次
		sr := candidates[0]
		const maxRetries = 3
		for i := 0; i < maxRetries; i++ {
			slog.Info("GetMusicUrl: 尝试获取",
				"sourceID", sr.SourceID(),
				"platform", platform,
				"quality", quality,
				"attempt", i+1,
				"maxRetries", maxRetries)

			url, err := sr.GetMusicUrl(platform, quality, musicInfo)
			if err == nil && url != "" {
				slog.Info("GetMusicUrl: 成功获取 URL", "sourceID", sr.SourceID(), "attempt", i+1)
				return url, nil
			}
			lastErr = err
			slog.Warn("GetMusicUrl: 尝试失败",
				"sourceID", sr.SourceID(),
				"attempt", i+1,
				"error", err)
		}
	} else {
		// 多个源：每个源各试 1 次
		for _, sr := range candidates {
			slog.Info("GetMusicUrl: 尝试获取",
				"sourceID", sr.SourceID(),
				"platform", platform,
				"quality", quality)

			url, err := sr.GetMusicUrl(platform, quality, musicInfo)
			if err == nil && url != "" {
				slog.Info("GetMusicUrl: 成功获取 URL", "sourceID", sr.SourceID())
				return url, nil
			}
			lastErr = err
			slog.Warn("GetMusicUrl: 尝试失败",
				"sourceID", sr.SourceID(),
				"error", err)
		}
	}

	// 4. 全部失败
	if lastErr != nil {
		return "", fmt.Errorf("all %d sources failed for platform %s: %w", len(candidates), platform, lastErr)
	}
	return "", fmt.Errorf("all %d sources failed for platform %s", len(candidates), platform)
}

// LoadedSources 返回所有已加载的源 ID 列表
func (rm *RuntimeManager) LoadedSources() []string {
	ids := make([]string, 0, len(rm.runtimes))
	for id := range rm.runtimes {
		ids = append(ids, id)
	}
	return ids
}

// IsLoaded 检查某源是否已加载
func (rm *RuntimeManager) IsLoaded(sourceID string) bool {
	_, exists := rm.runtimes[sourceID]
	return exists
}

// Count 返回已加载的源数量
func (rm *RuntimeManager) Count() int {
	return len(rm.runtimes)
}

// Close 关闭所有运行时
func (rm *RuntimeManager) Close() {
	for id, sr := range rm.runtimes {
		sr.Close()
		delete(rm.runtimes, id)
	}
	slog.Info("RuntimeManager: 所有运行时已关闭")
}
