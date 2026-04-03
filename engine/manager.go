//go:build wasip1

// Package engine 通过 cqjs proto 接口执行洛雪音源脚本。
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"sync/atomic"

	"github.com/mimusic-org/plugin/api/pbplugin"
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
func (rm *RuntimeManager) LoadSource(sourceID string, script string, pluginID int64) error {
	// 如果已存在，先卸载
	if _, exists := rm.runtimes[sourceID]; exists {
		rm.UnloadSource(sourceID)
	}

	sr, err := NewSourceRuntime(sourceID, script, pluginID)
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
func (rm *RuntimeManager) ReloadSource(sourceID string, script string, pluginID int64) error {
	rm.UnloadSource(sourceID)
	return rm.LoadSource(sourceID, script, pluginID)
}

// GetRuntime 获取指定源的运行时
func (rm *RuntimeManager) GetRuntime(sourceID string) (*SourceRuntime, bool) {
	sr, ok := rm.runtimes[sourceID]
	return sr, ok
}

// GetMusicUrl 多源并行获取播放 URL
// 1. 收集所有支持该 platform 的已加载 runtime
// 2. 按成功率降序排序
// 3. 构建并行 JS 调用请求
// 4. 调用 ExecuteJSParallel（窗口并发 maxConcurrent=3）
// 5. 解析首个成功结果，更新成功率统计
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

	// 2. 按成功率降序排序
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].SuccessRate() > candidates[j].SuccessRate()
	})

	slog.Info("GetMusicUrl: 开始并行获取",
		"platform", platform,
		"quality", quality,
		"candidateCount", len(candidates))

	// 3. 为每个 candidate 构建 JS 调用请求
	info := map[string]interface{}{
		"musicInfo": musicInfo,
		"type":      quality,
	}
	payload := map[string]interface{}{
		"source": platform,
		"action": "musicUrl",
		"info":   info,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("marshal payload: %w", err)
	}

	calls := make([]*pbplugin.ExecuteJSRequest, len(candidates))
	requestIDs := make([]string, len(candidates))
	for i, sr := range candidates {
		requestID := fmt.Sprintf("req_%d", atomic.AddUint64(&requestIDCounter, 1))
		requestIDs[i] = requestID

		code := fmt.Sprintf(`lx._dispatch(%s, "request", %s);`, jsonString(requestID), string(payloadJSON))

		calls[i] = &pbplugin.ExecuteJSRequest{
			EnvId:          sr.EnvID(),
			Code:           code,
			TimeoutMs:      30000,
			PluginId:       sr.PluginID(),
			WaitEventNames: []string{"dispatchResult", "dispatchError"},
		}

		slog.Info("GetMusicUrl: 构建调用",
			"index", i,
			"sourceID", sr.SourceID(),
			"envID", sr.EnvID(),
			"requestID", requestID,
			"successRate", fmt.Sprintf("%.2f", sr.SuccessRate()))
	}

	// 4. 调用 ExecuteJSParallel
	hostFunctions := pbplugin.NewHostFunctions()
	resp, err := hostFunctions.ExecuteJSParallel(context.Background(), &pbplugin.ExecuteJSParallelRequest{
		Calls:         calls,
		MaxConcurrent: 3,
	})
	if err != nil {
		// 所有音源标记失败
		for _, sr := range candidates {
			sr.RecordFailure()
		}
		return "", fmt.Errorf("ExecuteJSParallel failed: %w", err)
	}

	// 5. 更新成功率统计
	successIdx := int(resp.GetSuccessIndex())
	for i, sr := range candidates {
		if i == successIdx {
			sr.RecordSuccess()
		} else {
			// 对于失败的和未执行的（后续批次被跳过的），都记录失败
			errMsg := ""
			if i < len(resp.GetErrors()) {
				errMsg = resp.GetErrors()[i]
			}
			if errMsg != "" {
				sr.RecordFailure()
			}
			// 未执行的（errMsg 为空且非成功索引）不记录，保持原有成功率
		}
	}

	// 6. 解析成功结果
	if !resp.GetSuccess() || successIdx < 0 {
		errMsgs := resp.GetErrors()
		return "", fmt.Errorf("all %d sources failed for platform %s, errors: %v", len(candidates), platform, errMsgs)
	}

	// 从成功结果的 events 中提取 URL
	result := resp.GetResult()
	if result == nil {
		return "", fmt.Errorf("success result is nil")
	}

	url, err := extractURLFromEvents(result.GetEvents(), requestIDs[successIdx])
	if err != nil {
		return "", fmt.Errorf("extract URL from events: %w", err)
	}

	slog.Info("GetMusicUrl: 成功获取 URL",
		"sourceID", candidates[successIdx].SourceID(),
		"successIndex", successIdx)
	return url, nil
}

// extractURLFromEvents 从 JS 执行事件中提取播放 URL
func extractURLFromEvents(events []*pbplugin.JSEvent, requestID string) (string, error) {
	for _, evt := range events {
		switch evt.GetName() {
		case "dispatchResult":
			var result struct {
				ID     string      `json:"id"`
				Result interface{} `json:"result"`
			}
			if err := json.Unmarshal([]byte(evt.GetData()), &result); err != nil {
				continue
			}
			if result.ID != requestID {
				continue
			}
			// 解析 URL
			switch v := result.Result.(type) {
			case string:
				if v != "" {
					return v, nil
				}
			case map[string]interface{}:
				if url, ok := v["url"].(string); ok && url != "" {
					return url, nil
				}
			}
			return "", fmt.Errorf("dispatchResult has empty or invalid URL")
		case "dispatchError":
			var errResult struct {
				ID    string `json:"id"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(evt.GetData()), &errResult); err != nil {
				continue
			}
			if errResult.ID == requestID {
				return "", fmt.Errorf("dispatch error: %s", errResult.Error)
			}
		}
	}
	return "", fmt.Errorf("no matching dispatch event found for request %s", requestID)
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
