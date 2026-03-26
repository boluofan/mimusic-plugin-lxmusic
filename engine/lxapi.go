//go:build wasip1

// Package engine 封装 goja JS 运行时，用于执行洛雪音源脚本。
package engine

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"github.com/dop251/goja"
	"github.com/mimusic-org/plugin/pkg/go-plugin-http/http"
)

// LxAPI 管理 globalThis.lx 的注入
type LxAPI struct {
	vm            *goja.Runtime
	eventHandlers map[string]goja.Callable // on() 注册的处理器
	sourceConfig  *SourceConfig            // inited 事件接收的配置
}

// NewLxAPI 创建新的 LxAPI 实例
func NewLxAPI(vm *goja.Runtime) *LxAPI {
	return &LxAPI{
		vm:            vm,
		eventHandlers: make(map[string]goja.Callable),
	}
}

// GetSourceConfig 获取从 inited 事件解析的音源配置
func (l *LxAPI) GetSourceConfig() *SourceConfig {
	return l.sourceConfig
}

// CallEventHandler 调用注册的事件处理器
func (l *LxAPI) CallEventHandler(event string, args ...interface{}) (goja.Value, error) {
	handler, ok := l.eventHandlers[event]
	if !ok {
		return nil, fmt.Errorf("no handler registered for event: %s", event)
	}

	// 转换参数为 goja.Value
	jsArgs := make([]goja.Value, len(args))
	for i, arg := range args {
		jsArgs[i] = l.vm.ToValue(arg)
	}

	return handler(goja.Undefined(), jsArgs...)
}

// InjectLxAPI 注入 globalThis.lx 对象到 goja 运行时
func (l *LxAPI) InjectLxAPI(scriptInfo *ScriptInfo) error {
	// 创建 lx 对象
	lxObj := l.vm.NewObject()

	// lx.version
	if err := lxObj.Set("version", "2.1.0"); err != nil {
		return fmt.Errorf("set lx.version: %w", err)
	}

	// lx.env
	if err := lxObj.Set("env", "desktop"); err != nil {
		return fmt.Errorf("set lx.env: %w", err)
	}

	// lx.currentScriptInfo - 注入脚本元数据
	if scriptInfo != nil {
		scriptInfoObj := l.vm.NewObject()
		_ = scriptInfoObj.Set("name", scriptInfo.Name)
		_ = scriptInfoObj.Set("description", scriptInfo.Description)
		_ = scriptInfoObj.Set("version", scriptInfo.Version)
		_ = scriptInfoObj.Set("author", scriptInfo.Author)
		_ = scriptInfoObj.Set("homepage", scriptInfo.Homepage)
		_ = scriptInfoObj.Set("rawScript", scriptInfo.RawScript)
		if err := lxObj.Set("currentScriptInfo", scriptInfoObj); err != nil {
			return fmt.Errorf("set lx.currentScriptInfo: %w", err)
		}
	}

	// lx.EVENT_NAMES
	eventNames := l.vm.NewObject()
	_ = eventNames.Set("inited", "inited")
	_ = eventNames.Set("request", "request")
	if err := lxObj.Set("EVENT_NAMES", eventNames); err != nil {
		return fmt.Errorf("set lx.EVENT_NAMES: %w", err)
	}

	// lx.on(event, handler) - 注册事件处理器
	if err := lxObj.Set("on", l.createOnFunc()); err != nil {
		return fmt.Errorf("set lx.on: %w", err)
	}

	// lx.send(event, data) - 发送事件
	if err := lxObj.Set("send", l.createSendFunc()); err != nil {
		return fmt.Errorf("set lx.send: %w", err)
	}

	// lx.request(url, options, callback) - HTTP 请求
	if err := lxObj.Set("request", l.createRequestFunc()); err != nil {
		return fmt.Errorf("set lx.request: %w", err)
	}

	// lx.utils
	utils := l.vm.NewObject()
	if err := l.setupUtils(utils); err != nil {
		return fmt.Errorf("setup lx.utils: %w", err)
	}
	if err := lxObj.Set("utils", utils); err != nil {
		return fmt.Errorf("set lx.utils: %w", err)
	}

	// 注入到 globalThis
	global := l.vm.GlobalObject()
	if err := global.Set("lx", lxObj); err != nil {
		return fmt.Errorf("set globalThis.lx: %w", err)
	}

	return nil
}

// createOnFunc 创建 lx.on 函数
func (l *LxAPI) createOnFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			slog.Warn("lx.on: 参数不足")
			return goja.Undefined()
		}

		event := call.Argument(0).String()
		handlerVal := call.Argument(1)

		handler, ok := goja.AssertFunction(handlerVal)
		if !ok {
			slog.Warn("lx.on: 第二个参数不是函数", "event", event)
			return goja.Undefined()
		}

		l.eventHandlers[event] = handler
		slog.Debug("lx.on: 注册事件处理器", "event", event)

		return goja.Undefined()
	}
}

// createSendFunc 创建 lx.send 函数
func (l *LxAPI) createSendFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			slog.Warn("lx.send: 参数不足")
			return goja.Undefined()
		}

		event := call.Argument(0).String()
		data := call.Argument(1).Export()

		slog.Debug("lx.send: 收到事件", "event", event)

		switch event {
		case "inited":
			l.handleInitedEvent(data)
		default:
			slog.Debug("lx.send: 未知事件", "event", event)
		}

		return goja.Undefined()
	}
}

// handleInitedEvent 处理 inited 事件
func (l *LxAPI) handleInitedEvent(data interface{}) {
	slog.Debug("处理 inited 事件", "data", data)

	// 将 data 转换为 JSON 再解析为 SourceConfig
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		slog.Warn("inited 事件: JSON 序列化失败", "error", err)
		return
	}

	var config SourceConfig
	if err := json.Unmarshal(jsonBytes, &config); err != nil {
		slog.Warn("inited 事件: JSON 反序列化失败", "error", err)
		return
	}

	l.sourceConfig = &config
	slog.Info("音源配置已加载", "sources", len(config.Sources))
}

// createRequestFunc 创建 lx.request 函数
// 支持两种调用方式：
//  1. lx.request(url, options, callback) — 回调风格
//  2. lx.request(url, options) — Promise 风格，返回 Promise<response>
func (l *LxAPI) createRequestFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			slog.Warn("lx.request: 参数不足，至少需要 url 和 options")
			return goja.Undefined()
		}

		url := call.Argument(0).String()
		optionsVal := call.Argument(1).Export()

		// 判断是否有回调参数（兼容回调风格）
		var callback goja.Callable
		var usePromise bool
		if len(call.Arguments) >= 3 {
			if cb, ok := goja.AssertFunction(call.Argument(2)); ok {
				callback = cb
			} else {
				usePromise = true
			}
		} else {
			usePromise = true
		}

		// 解析 options
		options := l.parseHTTPOptions(optionsVal)

		slog.Debug("lx.request: 发起请求", "url", url, "method", options.Method, "promise", usePromise)

		// 构建响应对象的辅助函数
		buildResponseObj := func(resp *HTTPResponse) goja.Value {
			respObj := l.vm.NewObject()
			_ = respObj.Set("statusCode", resp.StatusCode)
			_ = respObj.Set("headers", resp.Headers)
			var jsonBody interface{}
			if jsonErr := json.Unmarshal([]byte(resp.Body), &jsonBody); jsonErr == nil {
				slog.Debug("lx.request: 响应体 JSON 解析成功")
				_ = respObj.Set("body", jsonBody)
			} else {
				slog.Debug("lx.request: 响应体 JSON 解析失败，保留原始字符串", "error", jsonErr)
				_ = respObj.Set("body", resp.Body)
			}
			return l.vm.ToValue(respObj)
		}

		// 执行 HTTP 请求
		resp, err := l.doHTTPRequest(url, options)

		if usePromise {
			// Promise 模式
			promise, resolve, reject := l.vm.NewPromise()
			if err != nil {
				slog.Info("lx.request: 请求失败(Promise)", "url", url, "error", err)
				reject(l.vm.NewGoError(err))
			} else {
				slog.Info("lx.request: 请求完成(Promise)", "url", url, "statusCode", resp.StatusCode, "bodyLen", len(resp.Body))
				resolve(buildResponseObj(resp))
			}
			return l.vm.ToValue(promise)
		}

		// 回调模式
		if err != nil {
			slog.Info("lx.request: 请求失败(Callback)", "url", url, "error", err)
			errObj := l.vm.NewObject()
			_ = errObj.Set("message", err.Error())
			_, _ = callback(goja.Undefined(), l.vm.ToValue(errObj), goja.Null(), goja.Null())
			return goja.Undefined()
		}

		slog.Info("lx.request: 请求完成(Callback)", "url", url, "statusCode", resp.StatusCode, "bodyLen", len(resp.Body))
		_, _ = callback(goja.Undefined(), goja.Null(), buildResponseObj(resp), l.vm.ToValue(resp.Body))
		return goja.Undefined()
	}
}

// parseHTTPOptions 解析 HTTP 选项
func (l *LxAPI) parseHTTPOptions(val interface{}) HTTPOptions {
	options := HTTPOptions{
		Method:  "GET",
		Headers: make(map[string]string),
		Timeout: 30000,
	}

	if val == nil {
		return options
	}

	optMap, ok := val.(map[string]interface{})
	if !ok {
		return options
	}

	if method, ok := optMap["method"].(string); ok {
		options.Method = strings.ToUpper(method)
	}

	if headers, ok := optMap["headers"].(map[string]interface{}); ok {
		for k, v := range headers {
			if vs, ok := v.(string); ok {
				options.Headers[k] = vs
			}
		}
	}

	if body, ok := optMap["body"].(string); ok {
		options.Body = body
	}

	if timeout, ok := optMap["timeout"].(float64); ok {
		options.Timeout = int(timeout)
	}

	return options
}

// doHTTPRequest 执行 HTTP 请求
func (l *LxAPI) doHTTPRequest(url string, options HTTPOptions) (*HTTPResponse, error) {
	var bodyReader io.Reader
	if options.Body != "" {
		bodyReader = strings.NewReader(options.Body)
	}

	req, err := http.NewRequest(options.Method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}

	// 设置请求头
	for k, v := range options.Headers {
		req.Header.Set(k, v)
	}

	// 设置默认 User-Agent
	if req.Header.Get("User-Agent") == "" {
		req.Header.Set("User-Agent", "lx-music-desktop/2.1.0")
	}

	// 执行请求
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer resp.Body.Close()

	// 读取响应体
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	// 构建响应头 map
	headers := make(map[string]string)
	for k, v := range resp.Header {
		if len(v) > 0 {
			headers[k] = v[0]
		}
	}

	return &HTTPResponse{
		StatusCode: resp.StatusCode,
		Headers:    headers,
		Body:       string(body),
	}, nil
}

// setupUtils 设置 lx.utils 对象
func (l *LxAPI) setupUtils(utils *goja.Object) error {
	// lx.utils.buffer
	buffer := l.vm.NewObject()
	if err := buffer.Set("from", l.createBufferFromFunc()); err != nil {
		return err
	}
	if err := buffer.Set("bufToString", l.createBufToStringFunc()); err != nil {
		return err
	}
	if err := utils.Set("buffer", buffer); err != nil {
		return err
	}

	// lx.utils.crypto
	crypto := l.vm.NewObject()
	if err := crypto.Set("md5", l.createMD5Func()); err != nil {
		return err
	}
	if err := crypto.Set("aesEncrypt", l.createAESEncryptFunc()); err != nil {
		return err
	}
	if err := crypto.Set("aesDecrypt", l.createAESDecryptFunc()); err != nil {
		return err
	}
	if err := crypto.Set("rsaEncrypt", l.createRSAEncryptFunc()); err != nil {
		return err
	}
	if err := crypto.Set("randomBytes", l.createRandomBytesFunc()); err != nil {
		return err
	}
	if err := utils.Set("crypto", crypto); err != nil {
		return err
	}

	return nil
}

// createBufferFromFunc 创建 lx.utils.buffer.from 函数
func (l *LxAPI) createBufferFromFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}

		data := call.Argument(0).String()
		encoding := "utf8"
		if len(call.Arguments) > 1 {
			encoding = call.Argument(1).String()
		}

		// 创建一个简单的 buffer 对象
		bufObj := l.vm.NewObject()
		_ = bufObj.Set("data", data)
		_ = bufObj.Set("encoding", encoding)
		_ = bufObj.Set("toString", func(call goja.FunctionCall) goja.Value {
			return l.vm.ToValue(data)
		})

		return bufObj
	}
}

// createMD5Func 创建 lx.utils.crypto.md5 函数
func (l *LxAPI) createMD5Func() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}

		str := call.Argument(0).String()
		hash := md5.Sum([]byte(str))
		return l.vm.ToValue(hex.EncodeToString(hash[:]))
	}
}

// createBufToStringFunc 创建 lx.utils.buffer.bufToString 函数
func (l *LxAPI) createBufToStringFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}

		bufArg := call.Argument(0)
		format := "utf8"
		if len(call.Arguments) > 1 {
			format = call.Argument(1).String()
		}

		// 尝试从 buffer 对象获取数据
		var data []byte
		switch v := bufArg.Export().(type) {
		case string:
			data = []byte(v)
		case []byte:
			data = v
		case map[string]interface{}:
			if d, ok := v["data"].(string); ok {
				data = []byte(d)
			} else if d, ok := v["data"].([]byte); ok {
				data = d
			}
		default:
			return l.vm.ToValue("")
		}

		switch format {
		case "hex":
			return l.vm.ToValue(hex.EncodeToString(data))
		case "base64":
			return l.vm.ToValue(base64.StdEncoding.EncodeToString(data))
		default: // "utf8"
			return l.vm.ToValue(string(data))
		}
	}
}

// createAESEncryptFunc 创建 lx.utils.crypto.aesEncrypt 函数
// aesEncrypt(buffer, mode, key, iv)
func (l *LxAPI) createAESEncryptFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 4 {
			slog.Warn("aesEncrypt: 参数不足")
			return goja.Undefined()
		}

		data := l.extractBytes(call.Argument(0))
		// mode := call.Argument(1).String() // e.g., "aes-128-cbc"
		key := l.extractBytes(call.Argument(2))
		iv := l.extractBytes(call.Argument(3))

		if len(key) == 0 || len(iv) == 0 {
			slog.Warn("aesEncrypt: key 或 iv 为空")
			return goja.Undefined()
		}

		// 创建 AES cipher
		block, err := aes.NewCipher(key)
		if err != nil {
			slog.Warn("aesEncrypt: 创建 cipher 失败", "error", err)
			return goja.Undefined()
		}

		// PKCS7 padding
		blockSize := block.BlockSize()
		padding := blockSize - len(data)%blockSize
		padText := make([]byte, padding)
		for i := range padText {
			padText[i] = byte(padding)
		}
		data = append(data, padText...)

		// CBC 加密
		encrypted := make([]byte, len(data))
		mode := cipher.NewCBCEncrypter(block, iv)
		mode.CryptBlocks(encrypted, data)

		// 返回 buffer 对象
		return l.createBufferObject(encrypted)
	}
}

// createAESDecryptFunc 创建 lx.utils.crypto.aesDecrypt 函数
// aesDecrypt(buffer, mode, key, iv)
func (l *LxAPI) createAESDecryptFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 4 {
			slog.Warn("aesDecrypt: 参数不足")
			return goja.Undefined()
		}

		data := l.extractBytes(call.Argument(0))
		// mode := call.Argument(1).String() // e.g., "aes-128-cbc"
		key := l.extractBytes(call.Argument(2))
		iv := l.extractBytes(call.Argument(3))

		if len(key) == 0 || len(iv) == 0 || len(data) == 0 {
			slog.Warn("aesDecrypt: key、iv 或 data 为空")
			return goja.Undefined()
		}

		// 创建 AES cipher
		block, err := aes.NewCipher(key)
		if err != nil {
			slog.Warn("aesDecrypt: 创建 cipher 失败", "error", err)
			return goja.Undefined()
		}

		// CBC 解密
		if len(data)%block.BlockSize() != 0 {
			slog.Warn("aesDecrypt: 数据长度不是块大小的倍数")
			return goja.Undefined()
		}

		decrypted := make([]byte, len(data))
		mode := cipher.NewCBCDecrypter(block, iv)
		mode.CryptBlocks(decrypted, data)

		// PKCS7 unpadding
		if len(decrypted) > 0 {
			padding := int(decrypted[len(decrypted)-1])
			if padding > 0 && padding <= block.BlockSize() && padding <= len(decrypted) {
				decrypted = decrypted[:len(decrypted)-padding]
			}
		}

		// 返回 buffer 对象
		return l.createBufferObject(decrypted)
	}
}

// createRSAEncryptFunc 创建 lx.utils.crypto.rsaEncrypt 函数
// rsaEncrypt(buffer, key)
func (l *LxAPI) createRSAEncryptFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 2 {
			slog.Warn("rsaEncrypt: 参数不足")
			return goja.Undefined()
		}

		data := l.extractBytes(call.Argument(0))
		keyStr := call.Argument(1).String()

		// 解析 PEM 格式的公钥
		block, _ := pem.Decode([]byte(keyStr))
		if block == nil {
			slog.Warn("rsaEncrypt: 无法解析 PEM 格式的密钥")
			return goja.Undefined()
		}

		pub, err := x509.ParsePKIXPublicKey(block.Bytes)
		if err != nil {
			// 尝试解析为 PKCS1 格式
			pub, err = x509.ParsePKCS1PublicKey(block.Bytes)
			if err != nil {
				slog.Warn("rsaEncrypt: 解析公钥失败", "error", err)
				return goja.Undefined()
			}
		}

		rsaPub, ok := pub.(*rsa.PublicKey)
		if !ok {
			slog.Warn("rsaEncrypt: 不是 RSA 公钥")
			return goja.Undefined()
		}

		// RSA PKCS1v15 加密
		encrypted, err := rsa.EncryptPKCS1v15(rand.Reader, rsaPub, data)
		if err != nil {
			slog.Warn("rsaEncrypt: 加密失败", "error", err)
			return goja.Undefined()
		}

		return l.createBufferObject(encrypted)
	}
}

// createRandomBytesFunc 创建 lx.utils.crypto.randomBytes 函数
// randomBytes(size) -> 返回十六进制字符串
func (l *LxAPI) createRandomBytesFunc() func(goja.FunctionCall) goja.Value {
	return func(call goja.FunctionCall) goja.Value {
		if len(call.Arguments) < 1 {
			return goja.Undefined()
		}

		size := int(call.Argument(0).ToInteger())
		if size <= 0 {
			return l.vm.ToValue("")
		}

		bytes := make([]byte, size)
		_, err := rand.Read(bytes)
		if err != nil {
			slog.Warn("randomBytes: 生成随机字节失败", "error", err)
			return goja.Undefined()
		}

		return l.vm.ToValue(hex.EncodeToString(bytes))
	}
}

// extractBytes 从 goja.Value 提取字节数据
func (l *LxAPI) extractBytes(val goja.Value) []byte {
	switch v := val.Export().(type) {
	case string:
		return []byte(v)
	case []byte:
		return v
	case map[string]interface{}:
		if data, ok := v["data"].(string); ok {
			return []byte(data)
		}
		if data, ok := v["data"].([]byte); ok {
			return data
		}
	}
	return nil
}

// createBufferObject 创建 buffer 对象
func (l *LxAPI) createBufferObject(data []byte) goja.Value {
	bufObj := l.vm.NewObject()
	_ = bufObj.Set("data", data)
	_ = bufObj.Set("toString", func(call goja.FunctionCall) goja.Value {
		format := "utf8"
		if len(call.Arguments) > 0 {
			format = call.Argument(0).String()
		}
		switch format {
		case "hex":
			return l.vm.ToValue(hex.EncodeToString(data))
		case "base64":
			return l.vm.ToValue(base64.StdEncoding.EncodeToString(data))
		default:
			return l.vm.ToValue(string(data))
		}
	})
	return bufObj
}
