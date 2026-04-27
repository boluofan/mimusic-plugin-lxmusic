//go:build wasip1

package handlers

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/md5"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	pluginhttp "github.com/mimusic-org/plugin/pkg/go-plugin-http/http"

	"github.com/mimusic-org/musicsdk"
	"github.com/mimusic-org/plugin/api/plugin"
)

// LeaderboardHandler 排行榜处理器
type LeaderboardHandler struct {
	registry *musicsdk.Registry
}

// NewLeaderboardHandler 创建排行榜处理器
func NewLeaderboardHandler(registry *musicsdk.Registry) *LeaderboardHandler {
	return &LeaderboardHandler{registry: registry}
}

// BoardItem 排行榜分类项
type BoardItem struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	BangID string `json:"bangid"`
}

// QualityItem 音质项
type QualityItem struct {
	Type string `json:"type"`
	Size string `json:"size"`
}

// SongItem 歌曲项
type SongItem struct {
	Singer    string        `json:"singer"`
	Name      string        `json:"name"`
	AlbumName string        `json:"albumName"`
	AlbumID   string        `json:"albumId"`
	SongMID   string        `json:"songmid"`
	Source    string        `json:"source"`
	Interval  string        `json:"interval"`
	Img       string        `json:"img"`
	Lrc       any           `json:"lrc"`
	Types     []QualityItem `json:"types"`
}

// HandleGetBoards 获取排行榜分类
// GET /lxmusic/api/leaderboard/boards?source=kw
func (h *LeaderboardHandler) HandleGetBoards(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	var boards []musicsdk.BoardItem
	var err error

	provider, ok := h.registry.GetLeaderboardProvider("leaderboard")
	if !ok {
		return plugin.ErrorResponse(http.StatusInternalServerError, "leaderboard provider not found"), nil
	}
	boards, err = provider.GetBoards(source)
	if err != nil {
		slog.Error("获取排行榜分类失败", "source", source, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取排行榜分类失败: "+err.Error()), nil
	}

	// TV模式返回lxserver原始格式
	if isTVRequest(req) {
		response := map[string]interface{}{
			"source": source,
			"list":   boards,
		}
		body, _ := json.Marshal(response)
		return &plugin.RouterResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
		}, nil
	}

	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": map[string]interface{}{
			"source": source,
			"list":   boards,
		},
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// getKuwoBoards 获取酷我排行榜分类（硬编码）
func (h *LeaderboardHandler) getKuwoBoards() ([]BoardItem, error) {
	return []BoardItem{
		{ID: "kw__93", Name: "飙升榜", BangID: "93"},
		{ID: "kw__17", Name: "新歌榜", BangID: "17"},
		{ID: "kw__16", Name: "热歌榜", BangID: "16"},
		{ID: "kw__158", Name: "抖音热歌榜", BangID: "158"},
		{ID: "kw__194", Name: "综艺榜", BangID: "194"},
		{ID: "kw__95", Name: "华语榜", BangID: "95"},
		{ID: "kw__10", Name: "欧美榜", BangID: "10"},
		{ID: "kw__12", Name: "韩国榜", BangID: "12"},
		{ID: "kw__11", Name: "日本榜", BangID: "11"},
		{ID: "kw__107", Name: "苦情歌榜", BangID: "107"},
		{ID: "kw__108", Name: "网络歌榜", BangID: "108"},
		{ID: "kw__105", Name: "经典老歌榜", BangID: "105"},
	}, nil
}

// getKgBoards 获取全民K歌排行榜分类（硬编码）
func (h *LeaderboardHandler) getKgBoards() ([]BoardItem, error) {
	return []BoardItem{
		{ID: "kg__8888", Name: "TOP500", BangID: "8888"},
		{ID: "kg__6666", Name: "飙升榜", BangID: "6666"},
		{ID: "kg__59703", Name: "蜂鸟流行音乐榜", BangID: "59703"},
		{ID: "kg__52144", Name: "抖音热歌榜", BangID: "52144"},
		{ID: "kg__36890", Name: "中文DJ榜", BangID: "36890"},
		{ID: "kg__66662", Name: "听书排行榜", BangID: "66662"},
		{ID: "kg__32016", Name: "广场舞排行榜", BangID: "32016"},
		{ID: "kg__34567", Name: "K歌金曲榜", BangID: "34567"},
	}, nil
}

// getTxBoards 获取QQ音乐排行榜分类（硬编码）
func (h *LeaderboardHandler) getTxBoards() ([]BoardItem, error) {
	// 参考 lxserver，硬编码 QQ 音乐排行榜
	boards := []BoardItem{
		{ID: "tx__4", Name: "流行指数榜", BangID: "4"},
		{ID: "tx__26", Name: "热歌榜", BangID: "26"},
		{ID: "tx__27", Name: "新歌榜", BangID: "27"},
		{ID: "tx__62", Name: "飙升榜", BangID: "62"},
		{ID: "tx__58", Name: "说唱榜", BangID: "58"},
		{ID: "tx__57", Name: "电音榜", BangID: "57"},
		{ID: "tx__28", Name: "网络歌曲榜", BangID: "28"},
		{ID: "tx__5", Name: "内地榜", BangID: "5"},
		{ID: "tx__3", Name: "欧美榜", BangID: "3"},
		{ID: "tx__59", Name: "香港地区榜", BangID: "59"},
		{ID: "tx__16", Name: "韩国榜", BangID: "16"},
		{ID: "tx__60", Name: "抖音热歌榜", BangID: "60"},
		{ID: "tx__29", Name: "影视金曲榜", BangID: "29"},
		{ID: "tx__17", Name: "日本榜", BangID: "17"},
		{ID: "tx__36", Name: "K歌金曲榜", BangID: "36"},
		{ID: "tx__61", Name: "台湾地区榜", BangID: "61"},
		{ID: "tx__63", Name: "DJ舞曲榜", BangID: "63"},
		{ID: "tx__64", Name: "综艺新歌榜", BangID: "64"},
		{ID: "tx__65", Name: "国风热歌榜", BangID: "65"},
		{ID: "tx__67", Name: "听歌识曲榜", BangID: "67"},
		{ID: "tx__72", Name: "动漫音乐榜", BangID: "72"},
		{ID: "tx__73", Name: "游戏音乐榜", BangID: "73"},
		{ID: "tx__75", Name: "有声榜", BangID: "75"},
	}
	return boards, nil
}

// getWyBoards 获取网易云排行榜分类（动态获取）
func (h *LeaderboardHandler) getWyBoards() ([]BoardItem, error) {
	// 网易云需要 weapi 加密，使用 JS runtime
	return h.getWyBoardsFromJS()
}

// getMgBoards 获取咪咕排行榜分类（硬编码）
func (h *LeaderboardHandler) getMgBoards() ([]BoardItem, error) {
	return []BoardItem{
		{ID: "mg__27553319", Name: "新歌榜", BangID: "27553319"},
		{ID: "mg__27186466", Name: "热歌榜", BangID: "27186466"},
		{ID: "mg__27553408", Name: "原创榜", BangID: "27553408"},
		{ID: "mg__75959118", Name: "音乐风向榜", BangID: "75959118"},
		{ID: "mg__76557036", Name: "彩铃分贝榜", BangID: "76557036"},
		{ID: "mg__76557745", Name: "会员臻爱榜", BangID: "76557745"},
		{ID: "mg__23189800", Name: "港台榜", BangID: "23189800"},
		{ID: "mg__23189399", Name: "内地榜", BangID: "23189399"},
		{ID: "mg__19190036", Name: "欧美榜", BangID: "19190036"},
		{ID: "mg__83176390", Name: "国风金曲榜", BangID: "83176390"},
	}, nil
}

// HandleGetList 获取排行榜歌曲
// GET /lxmusic/api/leaderboard/list?source=kw&boardId=kw__93&page=1
func (h *LeaderboardHandler) HandleGetList(req *http.Request) (*plugin.RouterResponse, error) {
	source := req.URL.Query().Get("source")
	if source == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 source 参数"), nil
	}

	// TV模式使用bangid参数，默认使用boardId
	boardID := req.URL.Query().Get("boardId")
	if boardID == "" {
		boardID = req.URL.Query().Get("bangid")
	}
	if boardID == "" {
		return plugin.ErrorResponse(http.StatusBadRequest, "缺少 boardId/bangid 参数"), nil
	}

	page, _ := strconv.Atoi(req.URL.Query().Get("page"))
	if page < 1 {
		page = 1
	}

	var list []musicsdk.SearchItem
	var total int
	var err error

	provider, ok := h.registry.GetLeaderboardProvider("leaderboard")
	if !ok {
		return plugin.ErrorResponse(http.StatusInternalServerError, "leaderboard provider not found"), nil
	}
	list, total, err = provider.GetList(source, boardID, page)
	if err != nil {
		slog.Error("获取排行榜歌曲失败", "source", source, "boardId", boardID, "error", err)
		return plugin.ErrorResponse(http.StatusInternalServerError, "获取排行榜歌曲失败: "+err.Error()), nil
	}

	// TV模式返回lxserver原始格式
	if isTVRequest(req) {
		response := map[string]interface{}{
			"source": source,
			"total":  total,
			"list":   list,
			"limit":  100,
			"page":   page,
		}
		body, _ := json.Marshal(response)
		return &plugin.RouterResponse{
			StatusCode: http.StatusOK,
			Headers:    map[string]string{"Content-Type": "application/json"},
			Body:       body,
		}, nil
	}

	response := map[string]interface{}{
		"code": 0,
		"msg":  "success",
		"data": map[string]interface{}{
			"source": source,
			"total":  total,
			"list":   list,
			"limit":  100,
			"page":   page,
		},
	}
	body, _ := json.Marshal(response)
	return &plugin.RouterResponse{
		StatusCode: http.StatusOK,
		Headers:    map[string]string{"Content-Type": "application/json"},
		Body:       body,
	}, nil
}

// getKuwoBoardList 获取酷我排行榜歌曲
func (h *LeaderboardHandler) getKuwoBoardList(bangID string, page int) ([]SongItem, int, error) {
	params := map[string]interface{}{
		"uid":       "",
		"devId":     "",
		"sFrom":     "kuwo_sdk",
		"user_type": "AP",
		"carSource": "kwplayercar_ar_6.0.1.0_apk_keluze.apk",
		"id":        bangID,
		"pn":        page - 1,
		"rn":        100,
	}

	// 构建加密参数并发送请求
	body, err := h.callWbdApi("https://wbd.kuwo.cn/api/bd/bang/bang_info", params)
	if err != nil {
		return nil, 0, fmt.Errorf("调用 wbd API 失败: %w", err)
	}

	// 解析响应
	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, 0, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	code, ok := rawData["code"].(float64)
	if !ok || code != 200 {
		return nil, 0, fmt.Errorf("API 返回错误: code=%v", rawData["code"])
	}

	// 解析音乐列表
	data, ok := rawData["data"].(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("响应数据格式错误")
	}

	musicList, ok := data["musiclist"].([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("musiclist 格式错误")
	}

	totalStr, _ := data["total"].(string)
	total, _ := strconv.Atoi(totalStr)

	list := make([]SongItem, 0, len(musicList))
	for _, item := range musicList {
		song := h.parseSongItem(item.(map[string]interface{}))
		list = append(list, song)
	}

	return list, total, nil
}

// getKgBoardList 获取全民K歌排行榜歌曲
func (h *LeaderboardHandler) getKgBoardList(bangID string, page int) ([]SongItem, int, error) {
	urlStr := fmt.Sprintf("http://mobilecdnbj.kugou.com/api/v3/rank/song?version=9108&ranktype=1&plat=0&pagesize=100&area_code=1&page=%d&rankid=%s&with_res_tag=0&show_portrait_mv=1",
		page, bangID)

	resp, err := pluginhttp.Get(urlStr)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP 状态码错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应体失败: %w", err)
	}

	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, 0, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	errcode, _ := rawData["errcode"].(float64)
	if errcode != 0 {
		return nil, 0, fmt.Errorf("API 返回错误: errcode=%v", errcode)
	}

	data, ok := rawData["data"].(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("data 格式错误")
	}

	info, ok := data["info"].([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("info 格式错误")
	}

	total, _ := strconv.Atoi(h.getString(data["total"]))

	list := make([]SongItem, 0, len(info))
	for _, item := range info {
		m := item.(map[string]interface{})
		song := SongItem{
			Name:      h.decodeName(m["songname"]),
			AlbumName: h.decodeName(m["remark"]),
			AlbumID:   h.getString(m["album_id"]),
			SongMID:   h.getString(m["audio_id"]),
			Source:    "kg",
			Interval:  h.formatPlayTime(h.getInt(m["duration"])),
			Img:       h.getKgImg(m),
			Lrc:       nil,
		}

		// 解析歌手
		authors, _ := m["authors"].([]interface{})
		var singers []string
		for _, a := range authors {
			if author, ok := a.(map[string]interface{}); ok {
				singers = append(singers, h.decodeName(author["author_name"]))
			}
		}
		song.Singer = strings.Join(singers, "、")

		// 解析音质
		song.Types = h.parseKgTypes(m)

		list = append(list, song)
	}

	return list, total, nil
}

// parseKgTypes 解析全民K歌音质信息
func (h *LeaderboardHandler) parseKgTypes(item map[string]interface{}) []QualityItem {
	var result []QualityItem

	if filesize, ok := item["filesize"].(float64); ok && filesize > 0 {
		result = append(result, QualityItem{Type: "128k", Size: h.sizeFormate(int64(filesize))})
	}
	if size320, ok := item["320filesize"].(float64); ok && size320 > 0 {
		result = append(result, QualityItem{Type: "320k", Size: h.sizeFormate(int64(size320))})
	}
	if sqsize, ok := item["sqfilesize"].(float64); ok && sqsize > 0 {
		result = append(result, QualityItem{Type: "flac", Size: h.sizeFormate(int64(sqsize))})
	}
	if sizeHigh, ok := item["filesize_high"].(float64); ok && sizeHigh > 0 {
		result = append(result, QualityItem{Type: "flac24bit", Size: h.sizeFormate(int64(sizeHigh))})
	}

	return result
}

// getKgImg 获取全民K歌封面
func (h *LeaderboardHandler) getKgImg(item map[string]interface{}) string {
	albumSizableCover, _ := item["album_sizable_cover"].(string)
	if albumSizableCover != "" {
		return strings.Replace(albumSizableCover, "{size}", "400", 1)
	}
	if transParam, ok := item["trans_param"].(map[string]interface{}); ok {
		if unionCover, _ := transParam["union_cover"].(string); unionCover != "" {
			return strings.Replace(unionCover, "{size}", "400", 1)
		}
	}
	return ""
}

// getTxBoardList 获取QQ音乐排行榜歌曲
func (h *LeaderboardHandler) getTxBoardList(bangID string, page int) ([]SongItem, int, error) {
	// 转换 bangID 为整数（API 要求数字类型）
	topIDInt, err := strconv.Atoi(bangID)
	if err != nil {
		return nil, 0, fmt.Errorf("bangID 无效: %w", err)
	}

	// 获取最新的 period 信息
	period := h.getTxPeriod(bangID)
	if period == "" {
		slog.Warn("getTxBoardList: 无法获取period，使用默认值", "bangID", bangID)
		period = "2026-04-22" // 默认值
	}

	// 构建请求体（与 lxserver 保持一致）
	reqData := map[string]interface{}{
		"toplist": map[string]interface{}{
			"module": "musicToplist.ToplistInfoServer",
			"method": "GetDetail",
			"param": map[string]interface{}{
				"topid":  topIDInt, // 使用整数类型
				"num":    100,
				"period": period,
			},
		},
		"comm": map[string]interface{}{
			"uin":    0,
			"format": "json",
			"ct":     20,
			"cv":     1602, // 使用 1602 而不是 1859
		},
	}

	reqJSON, _ := json.Marshal(reqData)

	// 使用 GET + URL 参数方式（与 musicsdk tx_songlist.go 保持一致）
	apiURL := fmt.Sprintf("https://u.y.qq.com/cgi-bin/musicu.fcg?loginUin=0&hostUin=0&format=json&inCharset=utf-8&outCharset=utf-8&notice=0&platform=wk_v15.json&needNewCode=0&data=%s",
		url.QueryEscape(string(reqJSON)))

	resp, err := pluginhttp.Get(apiURL)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP 状态码错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应体失败: %w", err)
	}

	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, 0, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	// 检查 code 字段（添加 nil 检查防止 panic）
	if codeVal, ok := rawData["code"].(float64); ok && codeVal != 0 {
		slog.Error("getTxBoardList: API返回错误", "bangID", bangID, "code", codeVal)
		return nil, 0, fmt.Errorf("API 返回错误: code=%v", codeVal)
	}

	toplist, ok := rawData["toplist"].(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("toplist 格式错误")
	}

	// 检查 toplist.code
	if codeVal, ok := toplist["code"].(float64); ok && codeVal != 0 {
		slog.Error("getTxBoardList: toplist返回错误", "bangID", bangID, "code", codeVal)
		return nil, 0, fmt.Errorf("toplist 返回错误: code=%v", codeVal)
	}

	data, ok := toplist["data"]
	if !ok {
		slog.Error("getTxBoardList: toplist.data不存在", "bangID", bangID)
		return nil, 0, fmt.Errorf("toplist.data 格式错误")
	}

	dataMap, ok := data.(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("toplist.data 格式错误")
	}

	// dataMap 里面还有个 data 层
	innerData, ok := dataMap["data"].(map[string]interface{})
	if !ok {
		slog.Error("getTxBoardList: toplist.data.data类型错误", "bangID", bangID)
		return nil, 0, fmt.Errorf("toplist.data.data 格式错误")
	}

	// 获取总数量
	totalNum := 100
	if total, ok := innerData["totalNum"].(float64); ok {
		totalNum = int(total)
	}

	songList, ok := innerData["song"].([]interface{})
	if !ok {
		slog.Error("getTxBoardList: song格式错误", "bangID", bangID)
		return nil, 0, fmt.Errorf("song 格式错误")
	}

	list := make([]SongItem, 0, len(songList))
	for _, item := range songList {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		song := SongItem{
			Name:   h.getString(m["title"]),
			Source: "tx",
			Lrc:    nil,
		}

		// 解析歌手（singerName 是字符串）
		song.Singer = h.getString(m["singerName"])

		// 解析专辑ID和封面
		song.AlbumID = h.getString(m["albumMid"])
		if albumMid := song.AlbumID; albumMid != "" {
			song.Img = fmt.Sprintf("https://y.gtimg.cn/music/photo_new/T002R500x500M000%s.jpg", albumMid)
		}

		// 解析歌曲ID（songId 是数字）
		if songId, ok := m["songId"].(float64); ok {
			song.SongMID = fmt.Sprintf("%.0f", songId)
		}

		list = append(list, song)
	}

	return list, totalNum, nil
}

// parseTxTypes 解析QQ音乐音质信息
func (h *LeaderboardHandler) parseTxTypes(file map[string]interface{}) []QualityItem {
	var result []QualityItem

	if size128, ok := file["size_128mp3"].(float64); ok && size128 > 0 {
		result = append(result, QualityItem{Type: "128k", Size: h.sizeFormate(int64(size128))})
	}
	if size320, ok := file["size_320mp3"].(float64); ok && size320 > 0 {
		result = append(result, QualityItem{Type: "320k", Size: h.sizeFormate(int64(size320))})
	}
	if sizeFlac, ok := file["size_flac"].(float64); ok && sizeFlac > 0 {
		result = append(result, QualityItem{Type: "flac", Size: h.sizeFormate(int64(sizeFlac))})
	}
	if sizeHires, ok := file["size_hires"].(float64); ok && sizeHires > 0 {
		result = append(result, QualityItem{Type: "flac24bit", Size: h.sizeFormate(int64(sizeHires))})
	}

	return result
}

// getTxPeriod 获取QQ音乐排行榜period（简化实现）
func (h *LeaderboardHandler) getTxPeriod(bangID string) string {
	// 从QQ音乐网页爬取period信息
	urlStr := "https://c.y.qq.com/node/pc/wk_v15/top.html"

	resp, err := pluginhttp.Get(urlStr)
	if err != nil {
		slog.Warn("getTxPeriod: 请求失败", "error", err)
		return ""
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		slog.Warn("getTxPeriod: HTTP状态码错误", "status", resp.StatusCode)
		return ""
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		slog.Warn("getTxPeriod: 读取响应失败", "error", err)
		return ""
	}

	html := string(body)

	// 解析period列表
	// 格式: data-listname="xxx" data-tid="top/27" data-date="2026-04-19"
	periodRegex := regexp.MustCompile(`data-listname="[^"]*" data-tid="[^/]*/` + bangID + `" data-date="([^"]+)"`)
	matches := periodRegex.FindStringSubmatch(html)
	if len(matches) < 2 {
		slog.Warn("getTxPeriod: 未找到匹配的period", "bangID", bangID)
		return ""
	}

	period := matches[1]
	slog.Info("getTxPeriod: 找到period", "bangID", bangID, "period", period)
	return period
}

// getWyBoardList 获取网易云排行榜歌曲（使用weapi加密）
func (h *LeaderboardHandler) getWyBoardList(bangID string, page int) ([]SongItem, int, error) {
	// 使用 weapi 加密调用网易云 API
	params, encSecKey, err := h.weapiEncrypt(map[string]interface{}{
		"id": bangID,
		"n":  100000,
		"p":  1,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("weapi加密失败: %w", err)
	}

	// 发送请求
	formData := url.Values{}
	formData.Set("params", params)
	formData.Set("encSecKey", encSecKey)

	req, err := pluginhttp.NewRequest("POST", "https://music.163.com/weapi/v3/playlist/detail", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, 0, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := pluginhttp.DefaultClient.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP 状态码错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应体失败: %w", err)
	}

	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, 0, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	code, _ := rawData["code"].(float64)
	if code != 200 {
		return nil, 0, fmt.Errorf("API 返回错误: code=%v", code)
	}

	playlist, ok := rawData["playlist"].(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("playlist 格式错误")
	}

	trackIds, ok := playlist["trackIds"].([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("trackIds 格式错误")
	}

	// 获取歌曲详情（批量获取）
	ids := make([]int64, 0, len(trackIds))
	for _, t := range trackIds {
		if tid, ok := t.(map[string]interface{}); ok {
			if id, ok := tid["id"].(float64); ok {
				ids = append(ids, int64(id))
			}
		}
	}

	// 获取歌曲详细信息
	songs, err := h.getWySongDetails(ids)
	if err != nil {
		return nil, 0, fmt.Errorf("获取歌曲详情失败: %w", err)
	}

	list := make([]SongItem, 0, len(songs))
	for _, song := range songs {
		list = append(list, song)
	}

	return list, len(list), nil
}

// getWySongDetails 批量获取网易云歌曲详情
func (h *LeaderboardHandler) getWySongDetails(ids []int64) ([]SongItem, error) {
	if len(ids) == 0 {
		return []SongItem{}, nil
	}

	// 网易云歌曲详情 API，每次最多获取1000首
	const batchSize = 1000
	allSongs := make([]SongItem, 0)

	for i := 0; i < len(ids); i += batchSize {
		end := i + batchSize
		if end > len(ids) {
			end = len(ids)
		}
		batchIds := ids[i:end]

		songs, err := h.getWySongDetailsBatch(batchIds)
		if err != nil {
			slog.Warn("getWySongDetails: 批量获取失败", "error", err, "start", i, "end", end)
			continue
		}
		allSongs = append(allSongs, songs...)
	}

	return allSongs, nil
}

// getWySongDetailsBatch 批量获取网易云歌曲详情（单次最多1000首）
func (h *LeaderboardHandler) getWySongDetailsBatch(ids []int64) ([]SongItem, error) {
	if len(ids) == 0 {
		return []SongItem{}, nil
	}

	// 构建 c 参数： [{"id":3368793123},{"id":3370116562},...]
	cParts := make([]string, len(ids))
	for i, id := range ids {
		cParts[i] = fmt.Sprintf(`{"id":%d}`, id)
	}
	cStr := "[" + strings.Join(cParts, ",") + "]"

	// 构建 ids 参数： [3368793123,3370116562,...]
	idsStr := "["
	for i, id := range ids {
		if i > 0 {
			idsStr += ","
		}
		idsStr += fmt.Sprintf("%d", id)
	}
	idsStr += "]"

	params, encSecKey, err := h.weapiEncrypt(map[string]interface{}{
		"c":   cStr,
		"ids": idsStr,
	})
	if err != nil {
		return nil, fmt.Errorf("weapi加密失败: %w", err)
	}

	slog.Info("getWySongDetailsBatch: 请求歌曲详情", "idsCount", len(ids), "cStrLen", len(cStr), "idsStrLen", len(idsStr))

	formData := url.Values{}
	formData.Set("params", params)
	formData.Set("encSecKey", encSecKey)

	req, err := pluginhttp.NewRequest("POST", "https://music.163.com/weapi/v3/song/detail", strings.NewReader(formData.Encode()))
	if err != nil {
		return nil, fmt.Errorf("创建请求失败: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := pluginhttp.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	slog.Info("getWySongDetailsBatch: 响应长度", "len", len(body))

	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		slog.Error("getWySongDetailsBatch: JSON解析失败", "error", err, "bodyLen", len(body))
		if len(body) > 0 {
			bodyStr := string(body)
			if len(bodyStr) > 500 {
				bodyStr = bodyStr[:500]
			}
			slog.Info("getWySongDetailsBatch: body前500字符", "body", bodyStr)
		}
		return nil, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	code, _ := rawData["code"].(float64)
	slog.Info("getWySongDetailsBatch: code", "code", code)

	if code != 200 {
		bodyStr := string(body)
		if len(bodyStr) > 500 {
			bodyStr = bodyStr[:500]
		}
		slog.Error("getWySongDetailsBatch: API错误", "code", code, "body", bodyStr)
		return nil, fmt.Errorf("API 返回错误: code=%v", code)
	}

	songs, ok := rawData["songs"].([]interface{})
	if !ok {
		slog.Error("getWySongDetailsBatch: songs字段解析失败", "rawData", rawData)
		return []SongItem{}, nil
	}

	slog.Info("getWySongDetailsBatch: 获取到歌曲数", "count", len(songs))

	list := make([]SongItem, 0, len(songs))
	for _, s := range songs {
		song, ok := s.(map[string]interface{})
		if !ok {
			continue
		}

		item := SongItem{
			Name:   h.decodeName(song["name"]),
			Source: "wy",
			Lrc:    nil,
		}

		// 解析歌手
		if singers, ok := song["ar"].([]interface{}); ok {
			var singerNames []string
			for _, singer := range singers {
				if s, ok := singer.(map[string]interface{}); ok {
					singerNames = append(singerNames, h.decodeName(s["name"]))
				}
			}
			item.Singer = strings.Join(singerNames, "、")
		}

		// 解析专辑
		if album, ok := song["al"].(map[string]interface{}); ok {
			item.AlbumName = h.decodeName(album["name"])
			if albumMid, ok := album["id"].(float64); ok {
				item.AlbumID = fmt.Sprintf("%.0f", albumMid)
			}
			if imgs, ok := album["picUrl"].(string); ok && imgs != "" {
				item.Img = imgs
			}
		}

		// 解析时长
		if dt, ok := song["dt"].(float64); ok {
			item.Interval = h.formatPlayTime(int(dt / 1000))
		}

		// 解析歌曲ID
		if id, ok := song["id"].(float64); ok {
			item.SongMID = fmt.Sprintf("%.0f", id)
		}

		list = append(list, item)
	}

	return list, nil
}

// weapiEncrypt 网易云 weapi 加密
func (h *LeaderboardHandler) weapiEncrypt(object interface{}) (params string, encSecKey string, err error) {
	text, err := json.Marshal(object)
	if err != nil {
		return "", "", fmt.Errorf("marshal: %w", err)
	}

	secKey := randomString(16)

	// 第一次 AES-CBC 加密
	encText, err := h.aesCBCEncrypt(string(text), "0CoJUm6Qyw8W8jud", "0102030405060708")
	if err != nil {
		return "", "", fmt.Errorf("first aes: %w", err)
	}

	// 第二次 AES-CBC 加密
	params, err = h.aesCBCEncrypt(encText, secKey, "0102030405060708")
	if err != nil {
		return "", "", fmt.Errorf("second aes: %w", err)
	}

	// RSA 加密 secKey
	encSecKey = h.rsaEncrypt(secKey)

	return params, encSecKey, nil
}

// randomString 生成随机字符串
func randomString(size int) string {
	const letters = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, size)
	for i := range b {
		b[i] = letters[i%len(letters)]
	}
	return string(b)
}

// pkcs7Pad PKCS7 填充
func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	padtext := make([]byte, padding)
	for i := range padtext {
		padtext[i] = byte(padding)
	}
	return append(data, padtext...)
}

// aesCBCEncrypt AES-128-CBC 加密
func (h *LeaderboardHandler) aesCBCEncrypt(text, key, iv string) (string, error) {
	keyBytes := []byte(key)
	ivBytes := []byte(iv)
	srcBytes := []byte(text)

	block, err := aes.NewCipher(keyBytes)
	if err != nil {
		return "", err
	}

	srcBytes = pkcs7Pad(srcBytes, block.BlockSize())
	blockMode := cipher.NewCBCEncrypter(block, ivBytes)
	crypted := make([]byte, len(srcBytes))
	blockMode.CryptBlocks(crypted, srcBytes)

	return base64.StdEncoding.EncodeToString(crypted), nil
}

// rsaEncrypt RSA 加密（网易云 weapi 使用）
func (h *LeaderboardHandler) rsaEncrypt(text string) string {
	pubKey := "010001"
	modulus := "00e0b509f6259df8642dbc35662901477df22677ec152b5ff68ace615bb7b725152b3ab17a876aea8a5aa76d2e417629ec4ee341f56135fccf695280104e0312ecbda92557c93870114af6c9d05c4f7f0c3685b7a46bee255932575cce10b424d813cfe4875d3e82047b97ddef52741d546b8e289dc6935b3ece0462db0a22b8e7"

	// 反转字符串
	runes := []rune(text)
	for i, j := 0, len(runes)-1; i < j; i, j = i+1, j-1 {
		runes[i], runes[j] = runes[j], runes[i]
	}
	text = string(runes)

	// hex 编码
	hexText := hex.EncodeToString([]byte(text))

	// 大数运算
	biText := new(big.Int)
	biText.SetString(hexText, 16)

	biPub := new(big.Int)
	biPub.SetString(pubKey, 16)

	biMod := new(big.Int)
	biMod.SetString(modulus, 16)

	biRet := new(big.Int).Exp(biText, biPub, biMod)

	return fmt.Sprintf("%0256x", biRet)
}

// getWyBoardsFromJS 通过JS runtime获取网易云排行榜分类
func (h *LeaderboardHandler) getWyBoardsFromJS() ([]BoardItem, error) {
	// 参考 lxserver，硬编码网易云排行榜列表
	return []BoardItem{
		{ID: "wy__19723756", Name: "飙升榜", BangID: "19723756"},
		{ID: "wy__3779629", Name: "新歌榜", BangID: "3779629"},
		{ID: "wy__2884035", Name: "原创榜", BangID: "2884035"},
		{ID: "wy__3778678", Name: "热歌榜", BangID: "3778678"},
		{ID: "wy__991319590", Name: "说唱榜", BangID: "991319590"},
		{ID: "wy__71384707", Name: "古典榜", BangID: "71384707"},
		{ID: "wy__1978921795", Name: "电音榜", BangID: "1978921795"},
		{ID: "wy__5453912201", Name: "黑胶VIP爱听榜", BangID: "5453912201"},
		{ID: "wy__71385702", Name: "ACG榜", BangID: "71385702"},
		{ID: "wy__745956260", Name: "韩语榜", BangID: "745956260"},
		{ID: "wy__10520166", Name: "国电榜", BangID: "10520166"},
		{ID: "wy__180106", Name: "UK排行榜周榜", BangID: "180106"},
		{ID: "wy__60198", Name: "美国Billboard榜", BangID: "60198"},
		{ID: "wy__3812895", Name: "Beatport全球电子舞曲榜", BangID: "3812895"},
		{ID: "wy__21845217", Name: "KTV唛榜", BangID: "21845217"},
		{ID: "wy__60131", Name: "日本Oricon榜", BangID: "60131"},
		{ID: "wy__2809513713", Name: "欧美热歌榜", BangID: "2809513713"},
		{ID: "wy__2809577409", Name: "欧美新歌榜", BangID: "2809577409"},
		{ID: "wy__3001835560", Name: "ACG动画榜", BangID: "3001835560"},
		{ID: "wy__3001795926", Name: "ACG游戏榜", BangID: "3001795926"},
		{ID: "wy__3001890046", Name: "ACG VOCALOID榜", BangID: "3001890046"},
		{ID: "wy__3112516681", Name: "中国新乡村音乐排行榜", BangID: "3112516681"},
		{ID: "wy__5059644681", Name: "日语榜", BangID: "5059644681"},
		{ID: "wy__5059633707", Name: "摇滚榜", BangID: "5059633707"},
		{ID: "wy__5059642708", Name: "国风榜", BangID: "5059642708"},
		{ID: "wy__5338990334", Name: "潜力爆款榜", BangID: "5338990334"},
		{ID: "wy__5059661515", Name: "民谣榜", BangID: "5059661515"},
		{ID: "wy__6688069460", Name: "听歌识曲榜", BangID: "6688069460"},
		{ID: "wy__6723173524", Name: "网络热歌榜", BangID: "6723173524"},
		{ID: "wy__6732051320", Name: "俄语榜", BangID: "6732051320"},
		{ID: "wy__6886768100", Name: "中文DJ榜", BangID: "6886768100"},
	}, nil
}

// getMgBoardList 获取咪咕排行榜歌曲
func (h *LeaderboardHandler) getMgBoardList(bangID string, page int) ([]SongItem, int, error) {
	urlStr := fmt.Sprintf("https://app.c.nf.migu.cn/MIGUM2.0/v1.0/content/querycontentbyId.do?columnId=%s&needAll=0", bangID)

	resp, err := pluginhttp.Get(urlStr)
	if err != nil {
		return nil, 0, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("HTTP 状态码错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("读取响应体失败: %w", err)
	}

	var rawData map[string]interface{}
	if err := json.Unmarshal(body, &rawData); err != nil {
		return nil, 0, fmt.Errorf("解析响应 JSON 失败: %w", err)
	}

	code := h.getString(rawData["code"])
	if code != "000000" {
		return nil, 0, fmt.Errorf("API 返回错误: code=%s", code)
	}

	columnInfo, ok := rawData["columnInfo"].(map[string]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("columnInfo 格式错误")
	}

	contents, ok := columnInfo["contents"].([]interface{})
	if !ok {
		return nil, 0, fmt.Errorf("contents 格式错误")
	}

	list := make([]SongItem, 0, len(contents))
	for _, item := range contents {
		m := item.(map[string]interface{})
		objInfo, ok := m["objectInfo"].(map[string]interface{})
		if !ok {
			continue
		}

		song := SongItem{
			Name:   h.decodeName(objInfo["songName"]),
			Source: "mg",
			Lrc:    nil,
		}

		// 解析时长 - mg 的 length 是 "03:45" 格式
		if length, ok := objInfo["length"].(string); ok {
			song.Interval = length
		}

		// 解析歌手 - mg 的 artists 是数组
		if artists, ok := objInfo["artists"].([]interface{}); ok {
			var singerNames []string
			for _, a := range artists {
				if artist, ok := a.(map[string]interface{}); ok {
					singerNames = append(singerNames, h.decodeName(artist["name"]))
				}
			}
			song.Singer = strings.Join(singerNames, "、")
		}

		// 解析专辑 - mg 的 album 是字符串
		if album, ok := objInfo["album"].(string); ok {
			song.AlbumName = h.decodeName(album)
		}

		// 解析图片
		if imgItems, ok := objInfo["albumImgs"].([]interface{}); ok && len(imgItems) > 0 {
			if img, ok := imgItems[0].(map[string]interface{}); ok {
				song.Img = h.getString(img["img"])
			}
		}

		// 解析音质 - mg 的 newRateFormats
		song.Types = h.parseMgTypes(objInfo)

		list = append(list, song)
	}

	return list, len(list), nil
}

// parseMgTypes 解析咪咕音质信息
func (h *LeaderboardHandler) parseMgTypes(objInfo map[string]interface{}) []QualityItem {
	var result []QualityItem

	// 咪咕的音质信息在 newRateFormats 字段中
	// formatType: PQ=128k, HQ=320k, SQ=flac, ZQ=flac24bit
	if rateFormats, ok := objInfo["newRateFormats"].([]interface{}); ok {
		for _, rf := range rateFormats {
			if format, ok := rf.(map[string]interface{}); ok {
				formatType := h.getString(format["formatType"])
				var size int64
				if s, ok := format["size"].(float64); ok {
					size = int64(s)
				} else if s, ok := format["androidSize"].(float64); ok {
					size = int64(s)
				}
				sizeStr := h.sizeFormate(size)
				switch formatType {
				case "PQ":
					result = append(result, QualityItem{Type: "128k", Size: sizeStr})
				case "HQ":
					result = append(result, QualityItem{Type: "320k", Size: sizeStr})
				case "SQ":
					result = append(result, QualityItem{Type: "flac", Size: sizeStr})
				case "ZQ":
					result = append(result, QualityItem{Type: "flac24bit", Size: sizeStr})
				}
			}
		}
	}

	return result
}

// sizeFormate 格式化文件大小
func (h *LeaderboardHandler) sizeFormate(size int64) string {
	if size <= 0 {
		return "0B"
	}
	const unit = 1024
	if size < unit {
		return fmt.Sprintf("%dB", size)
	}
	div, exp := int64(unit), 0
	for n := size / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%cB", float64(size)/float64(div), "KMGTPE"[exp])
}

// wbd API 调用（Go 端实现加密请求和解密响应）
var wbdAesKey = []byte{112, 87, 39, 61, 199, 250, 41, 191, 57, 68, 45, 114, 221, 94, 140, 228}
var wbdAppID = "y67sprxhhpws"

// callWbdApi 调用 wbd API（Go 端处理加密解密）
func (h *LeaderboardHandler) callWbdApi(apiURL string, params map[string]interface{}) ([]byte, error) {
	// 1. 序列化参数为 JSON
	jsonData, _ := json.Marshal(params)

	// 2. AES-ECB 加密 + PKCS7 padding
	encrypted := h.aesECBEncrypt(jsonData)

	// 3. 生成签名
	timestamp := time.Now().UnixMilli()
	sign := h.createWbdSign(encrypted, timestamp)

	// 4. 构建请求 URL
	requestURL := fmt.Sprintf("%s?data=%s&time=%d&appId=%s&sign=%s",
		apiURL,
		url.QueryEscape(encrypted),
		timestamp,
		wbdAppID,
		sign,
	)

	// 5. 发送 HTTP GET 请求
	resp, err := pluginhttp.Get(requestURL)
	if err != nil {
		return nil, fmt.Errorf("HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP 状态码错误: %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应体失败: %w", err)
	}

	slog.Info("wbd API 原始响应", "bodyLen", len(body), "bodyPrefix", string(body)[:min(100, len(body))])

	// 6. 解密响应
	decrypted, err := h.decodeWbdResponse(string(body))
	if err != nil {
		return nil, fmt.Errorf("解密响应失败: %w", err)
	}

	return decrypted, nil
}

// aesECBEncrypt AES-128-ECB 加密 + PKCS7 padding
func (h *LeaderboardHandler) aesECBEncrypt(plaintext []byte) string {
	block, err := aes.NewCipher(wbdAesKey)
	if err != nil {
		return ""
	}

	blockSize := aes.BlockSize
	padded := h.pkcs7Pad(plaintext, blockSize)

	ciphertext := make([]byte, len(padded))
	for i := 0; i < len(padded); i += blockSize {
		block.Encrypt(ciphertext[i:i+blockSize], padded[i:i+blockSize])
	}

	return base64.StdEncoding.EncodeToString(ciphertext)
}

// pkcs7Pad PKCS7 填充
func (h *LeaderboardHandler) pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	if padding == 0 {
		padding = blockSize
	}
	pad := make([]byte, padding)
	for i := range pad {
		pad[i] = byte(padding)
	}
	return append(data, pad...)
}

// createWbdSign 生成 wbd API 签名
func (h *LeaderboardHandler) createWbdSign(encodeData string, timestamp int64) string {
	str := wbdAppID + encodeData + fmt.Sprintf("%d", timestamp)
	hash := md5.Sum([]byte(str))
	return strings.ToUpper(hex.EncodeToString(hash[:]))
}

// decodeWbdResponse 解码 wbd API 响应
func (h *LeaderboardHandler) decodeWbdResponse(body string) ([]byte, error) {
	// 1. URL decode
	decoded, err := url.QueryUnescape(body)
	if err != nil {
		return nil, fmt.Errorf("URL decode 失败: %w", err)
	}

	// 2. Base64 decode（可能需要补齐 padding）
	ciphertext, err := base64.StdEncoding.DecodeString(decoded)
	if err != nil {
		// 尝试添加 padding
		padLen := (4 - len(decoded)%4) % 4
		padded := decoded + strings.Repeat("=", padLen)
		ciphertext, err = base64.StdEncoding.DecodeString(padded)
		if err != nil {
			return nil, fmt.Errorf("Base64 decode 失败: %w", err)
		}
	}

	// 3. AES-ECB 解密 + PKCS7 unpadding
	plaintext, err := h.aesECBDecrypt(ciphertext)
	if err != nil {
		return nil, fmt.Errorf("AES 解密失败: %w", err)
	}

	return plaintext, nil
}

// aesECBDecrypt AES-128-ECB 解密 + PKCS7 unpadding
func (h *LeaderboardHandler) aesECBDecrypt(ciphertext []byte) ([]byte, error) {
	block, err := aes.NewCipher(wbdAesKey)
	if err != nil {
		return nil, err
	}

	blockSize := aes.BlockSize
	if len(ciphertext)%blockSize != 0 {
		return nil, fmt.Errorf("密文长度 %d 不是块大小 %d 的倍数", len(ciphertext), blockSize)
	}

	plaintext := make([]byte, len(ciphertext))
	for i := 0; i < len(ciphertext); i += blockSize {
		block.Decrypt(plaintext[i:i+blockSize], ciphertext[i:i+blockSize])
	}

	// PKCS7 unpadding
	padLen := int(plaintext[len(plaintext)-1])
	if padLen > blockSize || padLen == 0 {
		return nil, fmt.Errorf("无效的 PKCS7 padding: %d", padLen)
	}
	// 验证所有 padding 字节
	for i := len(plaintext) - padLen; i < len(plaintext); i++ {
		if plaintext[i] != byte(padLen) {
			return nil, fmt.Errorf("padding 内容无效")
		}
	}

	return plaintext[:len(plaintext)-padLen], nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// parseSongItem 解析歌曲项
func (h *LeaderboardHandler) parseSongItem(item map[string]interface{}) SongItem {
	song := SongItem{
		Name:      h.decodeName(item["name"]),
		AlbumName: h.decodeName(item["album"]),
		AlbumID:   h.getString(item["albumId"]),
		SongMID:   h.getString(item["id"]),
		Source:    "kw",
		Interval:  h.formatPlayTime(h.getInt(item["duration"])),
		Img:       h.getString(item["pic"]),
		Lrc:       nil,
	}

	// 解析歌手
	singer := h.decodeName(item["artist"])
	song.Singer = strings.ReplaceAll(singer, "&", "、")

	// 解析音质
	minfo, _ := item["n_minfo"].(string)
	song.Types = h.parseMinfo(minfo)

	return song
}

// parseMinfo 解析音质信息
func (h *LeaderboardHandler) parseMinfo(minfo string) []QualityItem {
	var result []QualityItem
	qualityMap := map[string]string{
		"4000": "flac24bit",
		"2000": "flac",
		"320":  "320k",
		"128":  "128k",
	}

	// 格式: level:xxx,bitrate:xxx,format:xxx,size:xxx;...
	parts := strings.Split(minfo, ";")
	for _, part := range parts {
		var bitrate, size string
		for _, kv := range strings.Split(part, ",") {
			kv = strings.TrimSpace(kv)
			if strings.HasPrefix(kv, "bitrate:") {
				bitrate = strings.TrimPrefix(kv, "bitrate:")
			}
			if strings.HasPrefix(kv, "size:") {
				size = strings.TrimPrefix(kv, "size:")
			}
		}
		if qt, ok := qualityMap[bitrate]; ok {
			result = append(result, QualityItem{Type: qt, Size: size})
		}
	}
	return result
}

// decodeName 解码歌曲名
func (h *LeaderboardHandler) decodeName(name interface{}) string {
	if name == nil {
		return ""
	}
	// 洛雪使用 &&& 替换特殊字符
	s := strings.ReplaceAll(fmt.Sprintf("%v", name), "&&&", "/")
	// 解码 HTML 实体
	s = strings.ReplaceAll(s, "&amp;", "&")
	s = strings.ReplaceAll(s, "&lt;", "<")
	s = strings.ReplaceAll(s, "&gt;", ">")
	s = strings.ReplaceAll(s, "&quot;", "\"")
	return s
}

// getString 获取字符串值
func (h *LeaderboardHandler) getString(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

// getInt 获取整数值
func (h *LeaderboardHandler) getInt(v interface{}) int {
	if v == nil {
		return 0
	}
	switch val := v.(type) {
	case float64:
		return int(val)
	case int:
		return val
	case string:
		i, _ := strconv.Atoi(val)
		return i
	}
	return 0
}

// formatPlayTime 格式化播放时间
func (h *LeaderboardHandler) formatPlayTime(seconds int) string {
	if seconds <= 0 {
		return "00:00"
	}
	m := seconds / 60
	s := seconds % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}
