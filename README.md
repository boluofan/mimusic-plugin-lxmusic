# mimusic-plugin-lxmusic

[![Release](https://img.shields.io/github/v/release/mimusic-org/mimusic-plugin-lxmusic)](https://github.com/mimusic-org/mimusic-plugin-lxmusic/releases)
[![License](https://img.shields.io/github/license/mimusic-org/mimusic-plugin-lxmusic)](LICENSE)

> 仓库地址：https://github.com/mimusic-org/mimusic-plugin-lxmusic

MiMusic 洛雪音源插件，提供多平台音乐搜索和播放 URL 获取功能。搜索使用原生 Go 实现（musicsdk），播放 URL 通过 JS 音源脚本获取。本插件基于 [MiMusic](https://github.com/mimusic-org/mimusic) 插件系统开发。

## 功能特性

- **多平台搜索**：内置各大平台的搜索（无内置音源）
- **JS 音源管理**：导入洛雪音源脚本（.js / .zip），用于获取播放 URL
- **持久化运行时**：每个启用的音源维护一个独立的 goja VM 实例，启动时加载，无需每次调用重建
- **URL 代理映射**：导入歌曲时生成稳定的代理 URL，播放时实时解析获取真实播放地址
- **音乐缓存**：下载的音频文件缓存到本地，避免重复请求；支持 HEAD 重定向解析、Content-Type 校验和自动重试
- **音源启停控制**：支持启用/禁用音源，联动 RuntimeManager 加载/卸载
- **批量导入**：将搜索结果批量导入到 MiMusic 音乐库
- **Web 管理界面**：可视化的平台搜索、音源管理和歌曲导入界面

## 技术栈

- **语言**：Go（编译目标为 WASM/WASI）
- **搜索引擎**：原生 Go 实现（musicsdk），直接调用各平台 HTTP API
- **JS 引擎**：[goja](https://github.com/dop251/goja) JavaScript 运行时（用于音源脚本获取播放 URL）
- **插件框架**：[mimusic-org/plugin](https://github.com/mimusic-org/plugin)
- **前端**：原生 HTML + CSS + JavaScript

## 架构概览

```
搜索流程：用户搜索 → musicsdk 原生调用平台 API → 返回搜索结果
导入流程：选择歌曲 → 生成 URL hash 映射 → 调用主程序 API 添加歌曲
播放流程：主程序请求 /music/url/{hash} → 查缓存 → 未命中则 JS 音源获取 URL
         → HEAD 解析重定向 → GET 下载并缓存 → 返回音频流（失败回退 302 重定向）
```

## 项目结构

```
mimusic-plugin-lxmusic/
├── main.go                 # 插件入口，初始化和路由注册
├── musicsdk/               # 原生 Go 搜索实现
│   ├── types.go            # 公共类型 (SearchResult, SearchItem, QualityInfo)
│   ├── searcher.go         # Searcher 接口 + Registry 注册表
│   ├── http.go             # HTTP 客户端封装 (基于 go-plugin-http)
│   ├── util.go             # 工具函数
│   └── *_search.go         # 各平台搜索实现
├── engine/                 # 持久化 JS 运行时
│   ├── runtime.go          # SourceRuntime 单源持久化 VM
│   ├── manager.go          # RuntimeManager 多源管理和轮询
│   ├── lxapi.go            # lx.* API 注入
│   └── types.go            # 数据类型定义
├── urlmap/                 # URL hash 映射
│   ├── store.go            # hash→{songInfo, quality, platform} JSON 存储
│   └── types.go            # 映射类型定义
├── source/                 # JS 音源管理
│   ├── manager.go          # 音源生命周期管理 (CRUD + 启用/禁用)
│   ├── parser.go           # JSDoc 元数据解析、JS 校验
│   ├── storage.go          # 文件 I/O 与持久化
│   └── types.go            # 音源数据类型 (含 Enabled 字段)
├── handlers/
│   ├── search.go           # 搜索、导入、播放 URL 处理器
│   ├── cache.go            # 音乐缓存管理（下载、重定向解析、Content-Type 校验）
│   └── source.go           # 音源管理处理器
├── static/
│   ├── index.html          # 插件页面
│   ├── css/style.css       # 样式
│   └── js/app.js           # 前端逻辑
├── go.mod
├── go.sum
├── Makefile                # 构建脚本
└── README.md
```

## API 接口

### 搜索和导入

| 方法 | 路径 | 认证 | 描述 |
|------|------|------|------|
| `GET` | `/lxmusic/api/platforms` | 是 | 列出内置搜索平台 |
| `GET` | `/lxmusic/api/search?keyword=xxx&source_id=xx&page=1` | 是 | 搜索歌曲（source_id 通过 platforms 接口获取） |
| `POST` | `/lxmusic/api/songs/import` | 是 | 批量导入歌曲到音乐库 |
| `GET` | `/lxmusic/api/music/url/{hash}` | 否 | 获取播放音频（优先返回缓存流，失败回退 302 重定向） |

### 音源管理

| 方法 | 路径 | 认证 | 描述 |
|------|------|------|------|
| `GET` | `/lxmusic/api/sources` | 是 | 列出所有已导入的 JS 音源 |
| `POST` | `/lxmusic/api/sources/import` | 是 | 导入音源文件（.js 或 .zip） |
| `POST` | `/lxmusic/api/sources/import-url` | 是 | 通过 URL 导入音源 |
| `DELETE` | `/lxmusic/api/sources?id=xxx` | 是 | 删除指定音源 |
| `PUT` | `/lxmusic/api/sources/toggle` | 是 | 启用/禁用音源 |

## 构建

```bash
# WASM 编译
GOOS=wasip1 GOARCH=wasm go build -o lxmusic.wasm .

# 或使用 Makefile
make build
```

## 测试资源

以下是一些公开的洛雪音源资源，可用于测试：

- https://github.com/guoyue2010/lxmusic-
- https://github.com/pdone/lx-music-source

## 免责声明

- 本项目**仅供个人学习研究技术使用**，严禁任何形式的商业用途，包括但不限于售卖、牟利，不得使用本代码进行任何形式的牟利/贩卖/传播。
- 本项目数据来源原理是从各第三方音源中获取数据，因此本项目不对数据的准确性、合法性负责。使用本项目的过程中可能会产生版权数据，对于这些版权数据，本项目不拥有它们的所有权，为了避免侵权，使用者务必在 24 小时内清除使用本项目过程中所产生的版权数据。
- 本项目完全免费，仅供个人私下范围研究交流学习技术使用，并开源发布于 GitHub 面向全世界人用作对技术的学习交流，本项目不对项目内的技术可能存在违反当地法律法规的行为作保证，禁止在违反当地法律法规的情况下使用本项目，对于使用者在明知或不知当地法律法规不允许的情况下使用本项目所造成的任何违法违规行为由使用者承担，本项目不承担由此造成的任何直接、间接、特殊、偶然或结果性责任。
- 若你使用了本项目，将代表你接受以上声明。

> **注意**：本项目仅作为示例项目用于学习研究，请勿用于任何商业或违法用途。如有侵犯到任何人的合法权益，请联系作者，将在第一时间修改删除相关代码。

## 致谢

本项目参考了以下开源项目的代码实现：

- [XCQ0607/lxserver](https://github.com/XCQ0607/lxserver) - 洛雪音乐服务端实现

## License

本项目基于 [Apache License 2.0](LICENSE) 开源。
