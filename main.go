//go:build wasip1
// +build wasip1

package main

import (
	"context"
	"embed"
	"fmt"
	"log/slog"

	"mimusic-plugin-lxmusic/engine"
	"mimusic-plugin-lxmusic/handlers"
	"mimusic-plugin-lxmusic/musicsdk"
	"mimusic-plugin-lxmusic/source"
	"mimusic-plugin-lxmusic/urlmap"

	"github.com/knqyf263/go-plugin/types/known/emptypb"
	"github.com/mimusic-org/plugin/api/pbplugin"
	"github.com/mimusic-org/plugin/api/plugin"
)

func main() {}

//go:embed static/*
var staticFS embed.FS

// Plugin 插件结构体
type Plugin struct {
	Version string

	staticHandler  *plugin.StaticHandler
	sourceManager  *source.Manager
	sourceHandler  *handlers.SourceHandler
	searchHandler  *handlers.SearchHandler
	runtimeManager *engine.RuntimeManager
	registry       *musicsdk.Registry
	urlmapStore    *urlmap.Store
}

func init() {
	plugin.RegisterPlugin(&Plugin{
		Version: "1.0.0",
	})
}

func (p *Plugin) GetPluginInfo(ctx context.Context, request *emptypb.Empty) (*pbplugin.GetPluginInfoResponse, error) {
	return &pbplugin.GetPluginInfoResponse{
		Success: true,
		Message: "成功获取插件信息",
		Info: &pbplugin.PluginInfo{
			Name:        "洛雪音源",
			Version:     p.Version,
			Description: "管理洛雪音源，搜索和导入网络歌曲",
			Author:      "MiMusic Team",
			Homepage:    "https://github.com/mimusic-org/mimusic",
			EntryPath:   "/lxmusic",
		},
	}, nil
}

func (p *Plugin) Init(ctx context.Context, request *pbplugin.InitRequest) (*emptypb.Empty, error) {
	slog.Info("正在初始化洛雪音源插件", "version", p.Version)

	const dataDir = "/lxmusic"

	// 1. 初始化音源管理器（使用 WASM 沙箱内路径）
	var err error
	p.sourceManager, err = source.NewManager(dataDir)
	if err != nil {
		return &emptypb.Empty{}, fmt.Errorf("failed to init source manager: %w", err)
	}

	// 2. 初始化 RuntimeManager
	p.runtimeManager = engine.NewRuntimeManager()

	// 3. 初始化 musicsdk Registry 并注册 5 个平台搜索器
	p.registry = musicsdk.NewRegistry()
	p.registry.Register(musicsdk.NewKgSearcher()) // 酷狗音乐
	p.registry.Register(musicsdk.NewKwSearcher()) // 酷我音乐
	p.registry.Register(musicsdk.NewTxSearcher()) // QQ音乐
	p.registry.Register(musicsdk.NewWySearcher()) // 网易云音乐
	p.registry.Register(musicsdk.NewMgSearcher()) // 咪咕音乐
	slog.Info("已注册内置平台搜索器", "count", 5)

	// 4. 初始化 urlmap.Store
	p.urlmapStore, err = urlmap.NewStore(dataDir)
	if err != nil {
		return &emptypb.Empty{}, fmt.Errorf("failed to init urlmap store: %w", err)
	}

	// 5. 启动时加载已启用的音源到 RuntimeManager
	enabledSources := p.sourceManager.GetEnabledSources()
	for _, src := range enabledSources {
		if err := p.runtimeManager.LoadSource(src.ID, src.Script); err != nil {
			slog.Warn("加载已启用音源失败", "id", src.ID, "name", src.Name, "error", err)
		} else {
			slog.Info("已加载音源", "id", src.ID, "name", src.Name)
		}
	}
	if len(enabledSources) > 0 {
		slog.Info("启动时加载已启用音源完成", "loaded", p.runtimeManager.Count(), "total", len(enabledSources))
	}

	// 6. 初始化处理器
	p.sourceHandler = handlers.NewSourceHandler(p.sourceManager, p.runtimeManager)
	p.searchHandler = handlers.NewSearchHandler(p.registry, p.runtimeManager, p.urlmapStore)

	// 7. 获取路由管理器
	routerManager := plugin.GetRouterManager()

	// 初始化静态文件处理器
	p.staticHandler = plugin.NewStaticHandler(staticFS, "/lxmusic", routerManager, ctx)

	// 8. 注册 API 路由
	// 音源管理（需要认证）
	routerManager.RegisterRouter(ctx, "GET", "/lxmusic/api/sources", p.sourceHandler.HandleListSources, true)
	routerManager.RegisterRouter(ctx, "POST", "/lxmusic/api/sources/import", p.sourceHandler.HandleImportSource, true)
	routerManager.RegisterRouter(ctx, "POST", "/lxmusic/api/sources/import-url", p.sourceHandler.HandleImportSourceFromURL, true)
	routerManager.RegisterRouter(ctx, "DELETE", "/lxmusic/api/sources", p.sourceHandler.HandleDeleteSource, true)
	routerManager.RegisterRouter(ctx, "PUT", "/lxmusic/api/sources/toggle", p.sourceHandler.HandleToggleSource, true)

	// 搜索和导入（需要认证）
	routerManager.RegisterRouter(ctx, "GET", "/lxmusic/api/search", p.searchHandler.HandleSearch, true)
	routerManager.RegisterRouter(ctx, "GET", "/lxmusic/api/platforms", p.searchHandler.HandleListPlatforms, true)
	routerManager.RegisterRouter(ctx, "POST", "/lxmusic/api/songs/import", p.searchHandler.HandleImportSongs, true)

	// 获取播放 URL（不需要认证，主程序播放时直接调用）
	routerManager.RegisterRouter(ctx, "GET", "/lxmusic/api/music/url/{hash}", p.searchHandler.HandleGetMusicUrl, false)

	slog.Info("洛雪音源插件路由注册完成")
	return &emptypb.Empty{}, nil
}

func (p *Plugin) Deinit(ctx context.Context, request *emptypb.Empty) (*emptypb.Empty, error) {
	slog.Info("正在反初始化洛雪音源插件")

	// 清理 RuntimeManager
	if p.runtimeManager != nil {
		p.runtimeManager.Close()
	}

	// 清理音源管理器
	if p.sourceManager != nil {
		p.sourceManager.Close()
	}

	return &emptypb.Empty{}, nil
}
