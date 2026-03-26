//go:build wasip1

// Package engine 封装 goja JS 运行时，用于执行洛雪音源脚本。
package engine

// SourceConfig 从 JS inited 事件解析的音源配置
type SourceConfig struct {
	Sources map[string]SourceEntry `json:"sources"`
}

// SourceEntry 单个音源平台配置
type SourceEntry struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Actions  []string `json:"actions"`
	Qualitys []string `json:"qualitys"`
}

// MusicInfo 歌曲信息（用于 musicUrl 请求）
type MusicInfo struct {
	Source  string `json:"source"`   // 来源平台
	MusicID string `json:"music_id"` // 平台歌曲ID
	Name    string `json:"name"`     // 歌曲名
	Singer  string `json:"singer"`   // 歌手
	Quality string `json:"quality"`  // 音质
}

// RequestPayload JS request 事件的负载
type RequestPayload struct {
	Source string                 `json:"source"` // 来源平台
	Action string                 `json:"action"` // 动作类型
	Info   map[string]interface{} `json:"info"`   // 请求信息
}

// HTTPOptions HTTP 请求选项
type HTTPOptions struct {
	Method  string            `json:"method"`
	Headers map[string]string `json:"headers"`
	Body    string            `json:"body"`
	Timeout int               `json:"timeout"` // 毫秒
}

// HTTPResponse HTTP 响应
type HTTPResponse struct {
	StatusCode int               `json:"statusCode"`
	Headers    map[string]string `json:"headers"`
	Body       string            `json:"body"`
}

// ScriptInfo 脚本元数据，用于 lx.currentScriptInfo 注入
type ScriptInfo struct {
	Name        string // 音源名称
	Description string // 描述
	Version     string // 版本
	Author      string // 作者
	Homepage    string // 主页
	RawScript   string // 原始脚本内容
}
