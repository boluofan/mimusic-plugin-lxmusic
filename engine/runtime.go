//go:build wasip1

// Package engine 通过 cqjs proto 接口执行洛雪音源脚本。
package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync/atomic"

	"github.com/mimusic-org/plugin/api/pbplugin"
)

// requestIDCounter 全局请求 ID 计数器
var requestIDCounter uint64

// nextRequestID 生成唯一的请求 ID
func nextRequestID() string {
	id := atomic.AddUint64(&requestIDCounter, 1)
	return fmt.Sprintf("req_%d", id)
}

// SourceRuntime 单个音源的持久化运行时
// 通过 proto 接口与主程序的 cqjs JS 运行时通信
type SourceRuntime struct {
	sourceID     string
	envID        string // cqjs 环境 ID
	config       *SourceConfig
	pluginID     int64
	totalCalls   uint64 // 总调用次数（atomic 操作）
	successCalls uint64 // 成功次数（atomic 操作）
}

// NewSourceRuntime 创建并初始化一个音源运行时
func NewSourceRuntime(sourceID string, script string, pluginID int64) (*SourceRuntime, error) {
	return NewSourceRuntimeWithContext(context.Background(), sourceID, script, pluginID)
}

// NewSourceRuntimeWithContext 创建并初始化一个音源运行时（携带 context）
// 流程：CreateJSEnv(prelude) → ExecuteJS(设置 scriptInfo) → ExecuteJS(脚本) → 解析 inited 事件
func NewSourceRuntimeWithContext(ctx context.Context, sourceID string, script string, pluginID int64) (*SourceRuntime, error) {
	hostFunctions := pbplugin.NewHostFunctions()

	envID := fmt.Sprintf("lx_%s_%d", sourceID, pluginID)

	// 1. 创建 JS 环境，注入 lx_prelude.js 作为初始化代码
	createResp, err := hostFunctions.CreateJSEnv(ctx, &pbplugin.CreateJSEnvRequest{
		EnvId:    envID,
		InitCode: LxPreludeJS,
		PluginId: pluginID,
	})
	if err != nil {
		return nil, fmt.Errorf("create JS env: %w", err)
	}
	if !createResp.GetSuccess() {
		return nil, fmt.Errorf("create JS env failed: %s", createResp.GetMessage())
	}

	// 2. 解析脚本元数据并注入 lx.currentScriptInfo
	scriptInfo := parseScriptInfo(script)
	injectCode := fmt.Sprintf(
		`globalThis.lx.currentScriptInfo = {name: %s, description: %s, version: %s, author: %s, homepage: %s, rawScript: ""};`,
		jsonString(scriptInfo.Name),
		jsonString(scriptInfo.Description),
		jsonString(scriptInfo.Version),
		jsonString(scriptInfo.Author),
		jsonString(scriptInfo.Homepage),
	)

	injectResp, err := hostFunctions.ExecuteJS(ctx, &pbplugin.ExecuteJSRequest{
		EnvId:     envID,
		Code:      injectCode,
		TimeoutMs: 5000,
		PluginId:  pluginID,
	})
	if err != nil {
		// 清理环境
		hostFunctions.DestroyJSEnv(ctx, &pbplugin.DestroyJSEnvRequest{EnvId: envID, PluginId: pluginID})
		return nil, fmt.Errorf("inject scriptInfo: %w", err)
	}
	if !injectResp.GetSuccess() {
		hostFunctions.DestroyJSEnv(ctx, &pbplugin.DestroyJSEnvRequest{EnvId: envID, PluginId: pluginID})
		return nil, fmt.Errorf("inject scriptInfo failed: %s", injectResp.GetMessage())
	}

	// 3. 执行音源脚本
	execResp, err := hostFunctions.ExecuteJS(ctx, &pbplugin.ExecuteJSRequest{
		EnvId:     envID,
		Code:      script,
		TimeoutMs: 30000,
		PluginId:  pluginID,
	})
	if err != nil {
		hostFunctions.DestroyJSEnv(ctx, &pbplugin.DestroyJSEnvRequest{EnvId: envID, PluginId: pluginID})
		return nil, fmt.Errorf("execute script: %w", err)
	}
	if !execResp.GetSuccess() {
		hostFunctions.DestroyJSEnv(ctx, &pbplugin.DestroyJSEnvRequest{EnvId: envID, PluginId: pluginID})
		return nil, fmt.Errorf("execute script failed: %s", execResp.GetMessage())
	}

	// 4. 从 events 中解析 "inited" 事件获取 SourceConfig
	var config *SourceConfig
	for _, evt := range execResp.GetEvents() {
		if evt.GetName() == "inited" {
			var cfg SourceConfig
			if err := json.Unmarshal([]byte(evt.GetData()), &cfg); err != nil {
				slog.Warn("解析 inited 事件数据失败", "error", err, "data", evt.GetData())
				// 尝试从嵌套结构解析
				var wrapper map[string]json.RawMessage
				if err2 := json.Unmarshal([]byte(evt.GetData()), &wrapper); err2 == nil {
					if sourcesData, ok := wrapper["sources"]; ok {
						var sources map[string]SourceEntry
						if err3 := json.Unmarshal(sourcesData, &sources); err3 == nil {
							cfg.Sources = sources
							config = &cfg
						}
					}
				}
			} else {
				config = &cfg
			}
			break
		}
	}

	if config == nil {
		hostFunctions.DestroyJSEnv(ctx, &pbplugin.DestroyJSEnvRequest{EnvId: envID, PluginId: pluginID})
		return nil, fmt.Errorf("script did not call send('inited', ...)")
	}

	sr := &SourceRuntime{
		sourceID: sourceID,
		envID:    envID,
		config:   config,
		pluginID: pluginID,
	}

	slog.Info("SourceRuntime 创建成功", "sourceID", sourceID, "envID", envID, "sources", len(config.Sources))

	return sr, nil
}

// CallRequest 调用已加载脚本的 request handler
// 通过 lx._dispatch(requestId, "request", payload) 触发 JS 侧的事件处理器
// source: 来源平台标识（如 "kw", "tx"）
// action: 动作类型（如 "musicUrl"）
// info: 请求信息
func (sr *SourceRuntime) CallRequest(source string, action string, info map[string]interface{}) (interface{}, error) {
	hostFunctions := pbplugin.NewHostFunctions()
	ctx := context.Background()

	// 构建请求参数
	payload := map[string]interface{}{
		"source": source,
		"action": action,
		"info":   info,
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}

	requestID := nextRequestID()

	// 构建 JS 调用代码
	code := fmt.Sprintf(`lx._dispatch(%s, "request", %s);`, jsonString(requestID), string(payloadJSON))

	slog.Info("CallRequest: 调用 request handler", "sourceID", sr.sourceID, "source", source, "action", action, "requestID", requestID)

	resp, err := hostFunctions.ExecuteJS(ctx, &pbplugin.ExecuteJSRequest{
		EnvId:          sr.envID,
		Code:           code,
		TimeoutMs:      30000,
		PluginId:       sr.pluginID,
		WaitEventNames: []string{"dispatchResult", "dispatchError"},
	})
	if err != nil {
		slog.Error("CallRequest: ExecuteJS 失败", "error", err)
		return nil, fmt.Errorf("execute JS: %w", err)
	}

	// 从 events 中查找 dispatchResult 或 dispatchError
	for _, evt := range resp.GetEvents() {
		switch evt.GetName() {
		case "dispatchResult":
			var result struct {
				ID     string      `json:"id"`
				Result interface{} `json:"result"`
			}
			if err := json.Unmarshal([]byte(evt.GetData()), &result); err != nil {
				slog.Warn("CallRequest: 解析 dispatchResult 失败", "error", err, "data", evt.GetData())
				continue
			}
			if result.ID == requestID {
				slog.Info("CallRequest: 成功获取结果", "sourceID", sr.sourceID, "requestID", requestID)
				return result.Result, nil
			}
		case "dispatchError":
			var errResult struct {
				ID    string `json:"id"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal([]byte(evt.GetData()), &errResult); err != nil {
				slog.Warn("CallRequest: 解析 dispatchError 失败", "error", err, "data", evt.GetData())
				continue
			}
			if errResult.ID == requestID {
				slog.Error("CallRequest: handler 返回错误", "error", errResult.Error)
				return nil, fmt.Errorf("request handler error: %s", errResult.Error)
			}
		}
	}

	// 如果没有找到匹配的事件，检查执行是否成功
	if !resp.GetSuccess() {
		return nil, fmt.Errorf("execute JS failed: %s", resp.GetMessage())
	}

	slog.Warn("CallRequest: 未收到 dispatch 事件", "requestID", requestID)
	return nil, fmt.Errorf("no dispatch result received for request %s", requestID)
}

// GetMusicUrl 获取播放 URL
func (sr *SourceRuntime) GetMusicUrl(source, quality string, musicInfo map[string]interface{}) (string, error) {
	// 构建请求信息
	info := map[string]interface{}{
		"musicInfo": musicInfo,
		"type":      quality,
	}

	// 调用 request 处理器
	slog.Info("GetMusicUrl: 调用 request handler", "info", info)
	result, err := sr.CallRequest(source, "musicUrl", info)
	if err != nil {
		return "", err
	}

	// 处理返回值
	if result == nil {
		return "", fmt.Errorf("no result returned")
	}

	// 尝试获取 URL
	switch v := result.(type) {
	case string:
		return v, nil
	case map[string]interface{}:
		if url, ok := v["url"].(string); ok {
			return url, nil
		}
	}

	return "", fmt.Errorf("unexpected result type: %T", result)
}

// SupportsPlatform 检查此音源是否支持某平台
func (sr *SourceRuntime) SupportsPlatform(platform string) bool {
	if sr.config == nil || sr.config.Sources == nil {
		return false
	}
	_, ok := sr.config.Sources[platform]
	return ok
}

// SupportsAction 检查是否支持某平台的某个 action
func (sr *SourceRuntime) SupportsAction(platform, action string) bool {
	if sr.config == nil || sr.config.Sources == nil {
		return false
	}
	entry, ok := sr.config.Sources[platform]
	if !ok {
		return false
	}
	for _, a := range entry.Actions {
		if a == action {
			return true
		}
	}
	return false
}

// Config 返回音源配置
func (sr *SourceRuntime) Config() *SourceConfig {
	return sr.config
}

// SourceID 返回音源 ID
func (sr *SourceRuntime) SourceID() string {
	return sr.sourceID
}

// EnvID 返回 JS 环境 ID
func (sr *SourceRuntime) EnvID() string {
	return sr.envID
}

// PluginID 返回插件 ID
func (sr *SourceRuntime) PluginID() int64 {
	return sr.pluginID
}

// RecordSuccess 记录一次成功调用
func (sr *SourceRuntime) RecordSuccess() {
	atomic.AddUint64(&sr.totalCalls, 1)
	atomic.AddUint64(&sr.successCalls, 1)
}

// RecordFailure 记录一次失败调用
func (sr *SourceRuntime) RecordFailure() {
	atomic.AddUint64(&sr.totalCalls, 1)
}

// SuccessRate 返回成功率 [0.0, 1.0]，无调用记录时返回 0.5（中性值，不影响排序）
func (sr *SourceRuntime) SuccessRate() float64 {
	total := atomic.LoadUint64(&sr.totalCalls)
	if total == 0 {
		return 0.5
	}
	success := atomic.LoadUint64(&sr.successCalls)
	return float64(success) / float64(total)
}

// Close 关闭并清理运行时资源
func (sr *SourceRuntime) Close() {
	hostFunctions := pbplugin.NewHostFunctions()
	ctx := context.Background()

	_, err := hostFunctions.DestroyJSEnv(ctx, &pbplugin.DestroyJSEnvRequest{
		EnvId:    sr.envID,
		PluginId: sr.pluginID,
	})
	if err != nil {
		slog.Warn("销毁 JS 环境失败", "envID", sr.envID, "error", err)
	}

	sr.config = nil
	slog.Info("SourceRuntime 已关闭", "sourceID", sr.sourceID, "envID", sr.envID)
}

// jsonString 将字符串转为 JSON 字符串字面量（带引号和转义）
func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// jsdocPattern 匹配 JSDoc 注释块 (/** ... */)
var jsdocPattern = regexp.MustCompile(`(?s)/\*[!*][\s\S]*?\*/`)

// tagPatterns 各标签的正则表达式
var tagPatterns = map[string]*regexp.Regexp{
	"name":        regexp.MustCompile(`@name\s+(.+)`),
	"version":     regexp.MustCompile(`@version\s+(.+)`),
	"description": regexp.MustCompile(`@description\s+(.+)`),
	"author":      regexp.MustCompile(`@author\s+(.+)`),
	"homepage":    regexp.MustCompile(`@homepage\s+(.+)`),
}

// parseScriptInfo 解析 JS 文件头部的 JSDoc 注释块，提取元数据
func parseScriptInfo(script string) *ScriptInfo {
	info := &ScriptInfo{
		RawScript: script,
	}

	// 查找第一个 JSDoc 注释块
	match := jsdocPattern.FindString(script)
	if match == "" {
		return info
	}

	// 解析各标签
	for tag, pattern := range tagPatterns {
		if m := pattern.FindStringSubmatch(match); len(m) > 1 {
			value := strings.TrimSpace(m[1])
			switch tag {
			case "name":
				info.Name = value
			case "version":
				info.Version = value
			case "description":
				info.Description = value
			case "author":
				info.Author = value
			case "homepage":
				info.Homepage = value
			}
		}
	}

	return info
}
