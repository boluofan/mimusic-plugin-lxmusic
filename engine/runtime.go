//go:build wasip1

// Package engine 封装 goja JS 运行时，用于执行洛雪音源脚本。
package engine

import (
	"fmt"
	"log/slog"
	"regexp"
	"strings"

	"github.com/dop251/goja"
)

// SourceRuntime 单个音源的持久化运行时
type SourceRuntime struct {
	sourceID    string
	vm          *goja.Runtime
	lxAPI       *LxAPI
	config      *SourceConfig
	flushTimers func() // setTimeout/setInterval 的 flush 函数
}

// NewSourceRuntime 创建并初始化一个音源运行时
// 流程：创建 VM → 注入 lx.* API → setupGlobals → 执行脚本 → 等待 inited → 返回
func NewSourceRuntime(sourceID string, script string) (*SourceRuntime, error) {
	vm := goja.New()

	// 解析脚本元数据
	scriptInfo := parseScriptInfo(script)

	// 创建并注入 lx API
	lxAPI := NewLxAPI(vm)
	if err := lxAPI.InjectLxAPI(scriptInfo); err != nil {
		return nil, fmt.Errorf("inject lx API: %w", err)
	}

	// 设置 console.log 等
	setupConsole(vm)

	// 设置全局 API（setTimeout/setInterval/require 等）
	flushTimers := setupGlobals(vm)

	// 执行脚本
	_, err := vm.RunString(script)
	if err != nil {
		return nil, fmt.Errorf("execute script: %w", err)
	}

	// 刷新 Promise microtask 队列（处理脚本中 lx.request().then() 等异步链）
	_, _ = vm.RunString("")

	// 执行 setTimeout 注册的回调
	flushTimers()

	// 再次刷新 microtask 队列（timer 回调可能产生新的 Promise 链）
	_, _ = vm.RunString("")

	// 获取 SourceConfig
	config := lxAPI.GetSourceConfig()
	if config == nil {
		return nil, fmt.Errorf("script did not call send('inited', ...)")
	}

	sr := &SourceRuntime{
		sourceID:    sourceID,
		vm:          vm,
		lxAPI:       lxAPI,
		config:      config,
		flushTimers: flushTimers,
	}

	slog.Info("SourceRuntime 创建成功", "sourceID", sourceID, "sources", len(config.Sources))

	return sr, nil
}

// CallRequest 调用已加载脚本的 request handler
// 不再重新创建 VM，直接在已有 VM 上调用
// source: 来源平台标识（如 "kw", "tx"）
// action: 动作类型（如 "musicUrl"）
// info: 请求信息
func (sr *SourceRuntime) CallRequest(source string, action string, info map[string]interface{}) (interface{}, error) {
	// 构建请求参数
	payload := map[string]interface{}{
		"source": source,
		"action": action,
		"info":   info,
	}

	// 调用 request 事件处理器
	slog.Info("CallRequest: 调用 request handler", "sourceID", sr.sourceID, "source", source, "action", action)
	result, err := sr.lxAPI.CallEventHandler("request", payload)
	if err != nil {
		slog.Error("CallRequest: handler 返回错误", "error", err)
		return nil, fmt.Errorf("call request handler: %w", err)
	}

	// 处理返回值
	if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
		slog.Warn("CallRequest: handler 返回 nil/undefined/null")
		return nil, nil
	}

	// 检查是否为 Promise
	exported := result.Export()
	slog.Debug("CallRequest: handler 返回值类型", "type", fmt.Sprintf("%T", exported))
	if p, ok := exported.(*goja.Promise); ok {
		slog.Debug("CallRequest: 开始解析 Promise", "state", p.State())
		return resolvePromise(sr.vm, p)
	}

	return exported, nil
}

// GetMusicUrl 获取播放 URL
func (sr *SourceRuntime) GetMusicUrl(source, quality string, musicInfo map[string]interface{}) (string, error) {
	// 构建请求信息
	info := map[string]interface{}{
		"musicInfo": musicInfo,
		"type":      quality,
	}

	// 调用 request 处理器
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

// Close 关闭并清理运行时资源
func (sr *SourceRuntime) Close() {
	// goja.Runtime 没有显式的 Close 方法
	// 将引用置为 nil，让 GC 回收
	sr.vm = nil
	sr.lxAPI = nil
	sr.config = nil
	sr.flushTimers = nil
	slog.Info("SourceRuntime 已关闭", "sourceID", sr.sourceID)
}

// resolvePromise 等待 goja Promise 解析完成
// 通过反复执行 vm.RunString("") 来 flush microtask 队列
// 由于 lx.request() 是同步的，Promise 链不涉及真正的异步等待
func resolvePromise(vm *goja.Runtime, p *goja.Promise) (interface{}, error) {
	const maxIterations = 1000
	for i := 0; i < maxIterations; i++ {
		switch p.State() {
		case goja.PromiseStateFulfilled:
			result := p.Result()
			if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
				slog.Warn("resolvePromise: Promise fulfilled with nil/undefined")
				return nil, nil
			}
			// 如果结果仍然是 Promise，递归解析
			if nestedP, ok := result.Export().(*goja.Promise); ok {
				return resolvePromise(vm, nestedP)
			}
			return result.Export(), nil
		case goja.PromiseStateRejected:
			result := p.Result()
			if result == nil || goja.IsUndefined(result) || goja.IsNull(result) {
				slog.Error("resolvePromise: Promise rejected", "result", nil)
				return nil, fmt.Errorf("promise rejected")
			}
			// 优先使用 result.String() 提取 JS Error 的完整信息（如 "Error: HTTP 403"）
			// result.Export() 对 Error 对象会返回空 map，丢失错误消息
			errMsg := result.String()
			slog.Error("resolvePromise: Promise rejected", "result", errMsg)
			return nil, fmt.Errorf("promise rejected: %s", errMsg)
		default:
			// Promise 仍然 Pending，flush microtask 队列
			_, _ = vm.RunString("")
		}
	}
	return nil, fmt.Errorf("promise did not resolve after %d iterations", maxIterations)
}

// setupConsole 设置 console 对象
func setupConsole(vm *goja.Runtime) {
	console := vm.NewObject()

	logFunc := func(level string) func(goja.FunctionCall) goja.Value {
		return func(call goja.FunctionCall) goja.Value {
			args := make([]interface{}, len(call.Arguments))
			for i, arg := range call.Arguments {
				args[i] = arg.Export()
			}
			switch level {
			case "debug":
				slog.Debug("JS console", "args", args)
			case "info":
				slog.Info("JS console", "args", args)
			case "warn":
				slog.Warn("JS console", "args", args)
			case "error":
				slog.Error("JS console", "args", args)
			default:
				slog.Info("JS console", "args", args)
			}
			return goja.Undefined()
		}
	}

	_ = console.Set("log", logFunc("info"))
	_ = console.Set("debug", logFunc("debug"))
	_ = console.Set("info", logFunc("info"))
	_ = console.Set("warn", logFunc("warn"))
	_ = console.Set("error", logFunc("error"))

	_ = vm.Set("console", console)
}

// setupGlobals 注入浏览器/Node.js 全局 API 到 goja 运行时
// 返回 flushTimers 函数，需在 vm.RunString(script) 之后调用以执行 setTimeout 注册的回调
func setupGlobals(vm *goja.Runtime) (flushTimers func()) {
	var timerID int64
	var pendingCallbacks []goja.Callable

	// setTimeout(callback, delay) -> timerId
	// 在同步环境中，收集回调，脚本执行后统一执行
	vm.Set("setTimeout", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			return goja.Undefined()
		}
		timerID++
		pendingCallbacks = append(pendingCallbacks, fn)
		return vm.ToValue(timerID)
	})

	// clearTimeout - no-op
	vm.Set("clearTimeout", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	// setInterval - 收集回调但不真正循环
	vm.Set("setInterval", func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}
		fn, ok := goja.AssertFunction(call.Argument(0))
		if !ok {
			return goja.Undefined()
		}
		timerID++
		pendingCallbacks = append(pendingCallbacks, fn)
		return vm.ToValue(timerID)
	})

	// clearInterval - no-op
	vm.Set("clearInterval", func(call goja.FunctionCall) goja.Value {
		return goja.Undefined()
	})

	// require - 返回空对象，用于 feature detection
	vm.Set("require", func(call goja.FunctionCall) goja.Value {
		return vm.NewObject()
	})

	// console.group / console.groupEnd - no-op（一些音源使用）
	consoleVal := vm.Get("console")
	if consoleObj, ok := consoleVal.(*goja.Object); ok {
		consoleObj.Set("group", func(call goja.FunctionCall) goja.Value {
			return goja.Undefined()
		})
		consoleObj.Set("groupEnd", func(call goja.FunctionCall) goja.Value {
			return goja.Undefined()
		})
	}

	// 返回 flush 函数
	return func() {
		for i := 0; i < 10; i++ { // 最多 10 轮，防止无限循环
			if len(pendingCallbacks) == 0 {
				break
			}
			// 取出当前所有待执行的回调
			callbacks := pendingCallbacks
			pendingCallbacks = nil
			for _, cb := range callbacks {
				_, err := cb(goja.Undefined())
				if err != nil {
					slog.Warn("执行定时器回调失败", "error", err)
				}
			}
			// 每轮回调执行后刷新 Promise microtask 队列
			_, _ = vm.RunString("")
		}
	}
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
