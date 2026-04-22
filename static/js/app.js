// API 基础路径
const API_BASE = '/api/v1/plugin/lxmusic/api';
const MAIN_API = '/api/v1';

// 状态
let currentSources = [];
let currentPlatforms = [];
let searchResults = [];
let currentPage = 1;
let totalResults = 0;
let currentPlatformId = '';
let currentKeyword = '';
let playlists = [];
let sourcesLoaded = false;
let hasEnabledSources = true; // 是否有启用的音源，默认 true 避免闪烁

// 跨页持久选择
const selectedSongs = new Map();

// ============ 工具函数 ============

function getAuthToken() {
    try {
        const authData = localStorage.getItem('mimusic-auth');
        if (authData) {
            return JSON.parse(authData).accessToken || '';
        }
    } catch (e) {}
    return '';
}

function getAuthHeaders(isJson = true) {
    const headers = {};
    if (isJson) headers['Content-Type'] = 'application/json';
    const token = getAuthToken();
    if (token) headers['Authorization'] = 'Bearer ' + token;
    return headers;
}

function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

function formatDuration(seconds) {
    if (!seconds || seconds <= 0) return '--:--';
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, '0')}`;
}

function getSongKey(song) {
    return `${song.source}:${song.musicId || song.music_id || ''}`;
}

// ============ Snackbar ============

let snackbarTimer = null;

function showSnackbar(message, type = 'info', duration = 3000) {
    const el = document.getElementById('snackbar');
    if (snackbarTimer) clearTimeout(snackbarTimer);
    el.textContent = message;
    el.className = `snackbar ${type} show`;
    snackbarTimer = setTimeout(() => { el.className = 'snackbar'; }, duration);
}

// ============ Dialog ============

function showDialog(title, content, options) {
    return new Promise((resolve) => {
        document.getElementById('dialogTitle').textContent = title;
        const contentEl = document.getElementById('dialogContent');
        contentEl.style.whiteSpace = 'pre-line';
        contentEl.textContent = content;
        const overlay = document.getElementById('dialogOverlay');
        overlay.style.display = 'flex';
        const confirmBtn = document.getElementById('dialogConfirm');
        const cancelBtn = document.getElementById('dialogCancel');
        confirmBtn.textContent = (options && options.confirmText) || '确定';
        cancelBtn.textContent = (options && options.cancelText) || '取消';
        confirmBtn.onclick = () => { hideDialog(); resolve(true); };
        cancelBtn.onclick = () => { hideDialog(); resolve(false); };
    });
}

function hideDialog() {
    document.getElementById('dialogOverlay').style.display = 'none';
}

// ============ Tab 切换 ============

function initTabs() {
    document.querySelectorAll('.tab-item').forEach(btn => {
        btn.addEventListener('click', function () {
            const tab = this.dataset.tab;
            // 推入历史记录（popstate 触发时跳过）
            if (!window._isPopState) {
                history.pushState({ tab: tab }, '', '#' + tab);
            }
            document.querySelectorAll('.tab-item').forEach(b => b.classList.remove('active'));
            document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
            this.classList.add('active');
            document.getElementById(`tab-${tab}`).classList.add('active');
            if (tab === 'sources' && !sourcesLoaded) {
                loadSources();
                sourcesLoaded = true;
            }
        });
    });
}

/**
 * 切换到指定 Tab（供 popstate 回调使用）
 * @param {string} tab - Tab ID: 'search', 'songlist', 'sources'
 */
function switchToTab(tab) {
    document.querySelectorAll('.tab-item').forEach(b => b.classList.remove('active'));
    document.querySelectorAll('.tab-content').forEach(c => c.classList.remove('active'));
    const btn = document.querySelector(`.tab-item[data-tab="${tab}"]`);
    if (btn) btn.classList.add('active');
    const content = document.getElementById(`tab-${tab}`);
    if (content) content.classList.add('active');
    // 如果切回 songlist tab，恢复列表视图（而非详情）
    if (tab === 'songlist') {
        const detailCard = document.getElementById('slDetailCard');
        if (detailCard) detailCard.style.display = 'none';
        const listCard = document.getElementById('slListCard');
        if (listCard) listCard.style.display = '';
        const tagCard = document.getElementById('slTagCard');
        if (tagCard) tagCard.style.display = '';
    }
}

// ============ 平台管理 ============

async function loadPlatforms() {
    try {
        const response = await fetch(`${API_BASE}/platforms`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (result.code === 0) {
            currentPlatforms = result.data || [];
            renderPlatformSelect();
        } else {
            showSnackbar('加载平台列表失败: ' + (result.msg || '未知错误'), 'error');
        }
    } catch (e) {
        showSnackbar('加载平台列表失败: ' + e.message, 'error');
    }
}

function renderPlatformSelect() {
    const select = document.getElementById('platformSelect');
    const searchBtn = document.getElementById('searchBtn');
    if (currentPlatforms.length === 0) {
        select.innerHTML = '<option value="">暂无可用平台</option>';
        searchBtn.disabled = true;
    } else {
        select.innerHTML = currentPlatforms.map(p =>
            `<option value="${escapeHtml(p.id)}">${escapeHtml(p.name)}</option>`
        ).join('');
        searchBtn.disabled = false;
    }
}

// ============ 歌单管理 ============

async function loadPlaylists() {
    try {
        const resp = await fetch(`${MAIN_API}/playlists`, { headers: getAuthHeaders() });
        const data = await resp.json();
        playlists = data.playlists || data.data || data || [];
        renderPlaylistSelect();
    } catch (e) {
        console.error('加载歌单失败:', e);
    }
}

function renderPlaylistSelect() {
    const select = document.getElementById('playlistSelect');
    let html = '<option value="">不添加到歌单</option>';
    if (Array.isArray(playlists)) {
        for (const pl of playlists) {
            html += `<option value="${pl.id}">${escapeHtml(pl.name)}</option>`;
        }
    }
    html += '<option value="__new__">+ 新建歌单...</option>';
    select.innerHTML = html;
}

// ============ 音源状态检查 ============

async function checkSourceStatus() {
    try {
        const response = await fetch(`${API_BASE}/sources`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (result.code === 0) {
            const sources = result.data || [];
            hasEnabledSources = result.has_enabled || sources.some(s => s.enabled);
        } else {
            hasEnabledSources = false;
        }
    } catch (e) {
        console.error('检查音源状态失败:', e);
    }
    updateWarningBanners();
}

function updateWarningBanners() {
    const searchBanner = document.getElementById('searchWarningBanner');
    const songlistBanner = document.getElementById('songlistWarningBanner');
    if (searchBanner) searchBanner.classList.toggle('hidden', hasEnabledSources);
    if (songlistBanner) songlistBanner.classList.toggle('hidden', hasEnabledSources);
}

// ============ 音源管理 ============

async function loadSources() {
    const container = document.getElementById('sourceList');
    container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">hourglass_empty</span><p>加载中...</p></div>';
    try {
        const response = await fetch(`${API_BASE}/sources`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (result.code === 0) {
            currentSources = result.data || [];
            renderSources();
        } else {
            showSnackbar('加载音源失败: ' + (result.message || result.error || '未知错误'), 'error');
            container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败</p></div>';
        }
    } catch (e) {
        showSnackbar('加载音源失败: ' + e.message, 'error');
    }
}

function renderSources() {
    const container = document.getElementById('sourceList');
    if (currentSources.length === 0) {
        container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">inbox</span><p>暂无音源。导入洛雪音源脚本后，即可获取歌曲播放链接。<br>支持导入 .js 脚本文件或 .zip 压缩包。</p></div>';
        return;
    }
    container.innerHTML = currentSources.map(source => `
        <div class="list-item" data-id="${escapeHtml(source.id)}">
            <div class="list-item-info">
                <div class="list-item-title">${escapeHtml(source.name)}</div>
                <div class="list-item-subtitle">
                    版本: ${escapeHtml(source.version || '-')} &nbsp;|&nbsp;
                    作者: ${escapeHtml(source.author || '-')} &nbsp;|&nbsp;
                    ${source.imported_at ? new Date(source.imported_at).toLocaleString() : '-'}
                </div>
                ${source.platforms && source.platforms.length > 0
                    ? '<div class="platform-chips">' + source.platforms.map(p => '<span class="chip chip-platform">' + escapeHtml(p) + '</span>').join('') + '</div>'
                    : ''}
            </div>
            <div class="list-item-trailing">
                <label class="md-switch" title="${source.enabled ? '已启用' : '已禁用'}">
                    <input type="checkbox" ${source.enabled ? 'checked' : ''}
                        onchange="toggleSource('${escapeHtml(source.id)}', this.checked)">
                    <span class="switch-track"></span>
                    <span class="switch-thumb"></span>
                </label>
                <button class="btn-icon danger" onclick="deleteSource('${escapeHtml(source.id)}')" title="删除">
                    <span class="material-symbols-outlined">delete</span>
                </button>
            </div>
        </div>
    `).join('');
}

async function toggleSource(id, enabled) {
    try {
        const response = await fetch(`${API_BASE}/sources/toggle`, {
            method: 'PUT',
            headers: getAuthHeaders(),
            body: JSON.stringify({ id, enabled })
        });
        const result = await response.json();
        if (result.success) {
            showSnackbar(enabled ? '音源已启用' : '音源已禁用', 'success');
            const source = currentSources.find(s => s.id === id);
            if (source) source.enabled = enabled;
            checkSourceStatus();
        } else {
            showSnackbar('操作失败: ' + (result.message || result.error || '未知错误'), 'error');
            loadSources();
        }
    } catch (e) {
        showSnackbar('操作失败: ' + e.message, 'error');
        loadSources();
    }
}

async function importSource(file) {
    const formData = new FormData();
    formData.append('file', file);
    const headers = {};
    const token = getAuthToken();
    if (token) headers['Authorization'] = 'Bearer ' + token;
    try {
        showSnackbar('正在导入...', 'info');
        const response = await fetch(`${API_BASE}/sources/import`, { method: 'POST', headers, body: formData });
        const result = await response.json();
        if (result.success) {
            showSnackbar('导入成功', 'success');
            loadSources();
            checkSourceStatus();
        } else {
            showSnackbar('导入失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (e) {
        showSnackbar('导入失败: ' + e.message, 'error');
    }
}

async function importSourceFromURL(url) {
    if (!url || !url.trim()) { showSnackbar('请输入音源 URL', 'warning'); return; }
    if (!url.startsWith('http://') && !url.startsWith('https://')) {
        showSnackbar('URL 必须以 http:// 或 https:// 开头', 'warning'); return;
    }
    try {
        showSnackbar('正在从 URL 导入...', 'info');
        const response = await fetch(`${API_BASE}/sources/import-url`, {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify({ url: url.trim() })
        });
        const result = await response.json();
        if (result.success) {
            showSnackbar('导入成功', 'success');
            document.getElementById('sourceUrl').value = '';
            loadSources();
            checkSourceStatus();
        } else {
            showSnackbar('导入失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (e) {
        showSnackbar('导入失败: ' + e.message, 'error');
    }
}

async function deleteSource(id) {
    const confirmed = await showDialog('确认删除', '确定要删除这个音源吗？删除后不可恢复。');
    if (!confirmed) return;
    try {
        const response = await fetch(`${API_BASE}/sources?id=${encodeURIComponent(id)}`, {
            method: 'DELETE',
            headers: getAuthHeaders()
        });
        const result = await response.json();
        if (result.success) {
            showSnackbar('删除成功', 'success');
            loadSources();
            checkSourceStatus();
        } else {
            showSnackbar('删除失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (e) {
        showSnackbar('删除失败: ' + e.message, 'error');
    }
}

// ============ 搜索 ============

async function search(keyword, platformId, page = 1) {
    if (!keyword.trim()) { showSnackbar('请输入搜索关键词', 'warning'); return; }
    if (!platformId) { showSnackbar('请选择平台', 'warning'); return; }

    currentKeyword = keyword;
    currentPlatformId = platformId;
    currentPage = page;

    const searchBtn = document.getElementById('searchBtn');
    searchBtn.disabled = true;
    searchBtn.innerHTML = '<span class="spinner"></span>搜索中...';

    try {
        const params = new URLSearchParams({ keyword, source_id: platformId, page });
        const response = await fetch(`${API_BASE}/search?${params}`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (result.code === 0) {
            searchResults = result.data.list || [];
            totalResults = result.data.total || 0;
            renderResults();
            document.getElementById('resultSection').style.display = '';
        } else {
            showSnackbar('搜索失败: ' + (result.msg || '未知错误'), 'error');
        }
    } catch (e) {
        showSnackbar('搜索失败: ' + e.message, 'error');
    } finally {
        searchBtn.disabled = false;
        searchBtn.textContent = '搜索';
    }
}

// ============ 搜索结果渲染 ============

function renderResults() {
    const container = document.getElementById('resultList');
    document.getElementById('resultCount').textContent = `共 ${totalResults} 条`;

    if (searchResults.length === 0) {
        container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">search_off</span><p>没有找到相关歌曲</p></div>';
        renderPagination();
        return;
    }

    container.innerHTML = searchResults.map((song, i) => {
        const key = getSongKey(song);
        const checked = selectedSongs.has(key) ? 'checked' : '';
        const selectedClass = selectedSongs.has(key) ? ' selected' : '';
        const imgHtml = song.img
            ? `<img src="${escapeHtml(song.img)}" alt="" loading="lazy" onerror="this.parentNode.innerHTML='<span class=\\'material-symbols-outlined\\'>music_note</span>'">`
            : '<span class="material-symbols-outlined">music_note</span>';
        return `
            <div class="result-item${selectedClass}" data-index="${i}">
                <input type="checkbox" class="result-checkbox" data-index="${i}" ${checked}
                    onchange="onSongCheckChanged(${i}, this.checked)" style="accent-color:var(--md-primary);width:18px;height:18px;cursor:pointer;flex-shrink:0">
                <div class="result-thumb">${imgHtml}</div>
                <div class="result-info">
                    <div class="result-name">${escapeHtml(song.name)}</div>
                    <div class="result-meta">${escapeHtml(song.singer || '')}${song.album ? ' — ' + escapeHtml(song.album) : ''}</div>
                </div>
                <div class="result-duration">${formatDuration(song.duration)}</div>
            </div>
        `;
    }).join('');

    // 同步全选状态
    const allChecked = searchResults.every(s => selectedSongs.has(getSongKey(s)));
    document.getElementById('selectAll').checked = allChecked && searchResults.length > 0;

    updateSelectedCount();
    renderPagination();
}

function onSongCheckChanged(index, checked) {
    const song = searchResults[index];
    const key = getSongKey(song);
    if (checked) selectedSongs.set(key, song);
    else selectedSongs.delete(key);
    // 更新行样式
    const row = document.querySelector(`.result-item[data-index="${index}"]`);
    if (row) row.classList.toggle('selected', checked);
    updateSelectedCount();
    // 检查全选状态
    const allChecked = searchResults.every(s => selectedSongs.has(getSongKey(s)));
    document.getElementById('selectAll').checked = allChecked;
}

function toggleSelectAll() {
    const checked = document.getElementById('selectAll').checked;
    searchResults.forEach(song => {
        const key = getSongKey(song);
        if (checked) selectedSongs.set(key, song);
        else selectedSongs.delete(key);
    });
    renderResults();
}

function updateSelectedCount() {
    const count = selectedSongs.size;
    const badge = document.getElementById('selectedBadge');
    if (badge) badge.textContent = count;
    document.getElementById('importSongsBtn').disabled = count === 0;
    document.getElementById('clearSelectionBtn').style.display = count > 0 ? '' : 'none';
}

function clearSelection() {
    selectedSongs.clear();
    renderResults();
}

// ============ 分页 ============

function renderPagination() {
    const container = document.getElementById('pagination');
    const totalPages = Math.ceil(totalResults / 30);
    if (totalPages <= 1) { container.innerHTML = ''; return; }

    container.innerHTML = `
        <button class="btn-icon" title="上一页" ${currentPage <= 1 ? 'disabled' : ''}
            onclick="search('${escapeHtml(currentKeyword)}','${escapeHtml(currentPlatformId)}',${currentPage - 1})">
            <span class="material-symbols-outlined">chevron_left</span>
        </button>
        <span class="page-info">第 ${currentPage} / ${totalPages} 页</span>
        <input type="number" class="text-field page-jump" value="${currentPage}" min="1" max="${totalPages}"
            onchange="jumpToPage(this.value, ${totalPages})">
        <button class="btn-icon" title="下一页" ${currentPage >= totalPages ? 'disabled' : ''}
            onclick="search('${escapeHtml(currentKeyword)}','${escapeHtml(currentPlatformId)}',${currentPage + 1})">
            <span class="material-symbols-outlined">chevron_right</span>
        </button>
    `;
}

function jumpToPage(val, totalPages) {
    const page = Math.min(totalPages, Math.max(1, parseInt(val) || 1));
    if (page !== currentPage) {
        search(currentKeyword, currentPlatformId, page);
    }
}

// ============ 批量导入 ============

async function importSelectedSongs() {
    const songs = Array.from(selectedSongs.values());
    if (songs.length === 0) { showSnackbar('请选择要导入的歌曲', 'warning'); return; }

    if (!hasEnabledSources) {
        const proceed = await showDialog(
            '未配置音源',
            '当前未配置有效的洛雪音源，导入的歌曲将无法播放。\n\n是否仍要继续导入？',
            { confirmText: '继续导入', cancelText: '去配置音源' }
        );
        if (!proceed) { switchToTab('sources'); return; }
    }

    const quality = document.getElementById('qualitySelect').value;
    const playlistSelect = document.getElementById('playlistSelect');
    let playlistId = 0;
    let newPlaylistName = '';

    if (playlistSelect.value === '__new__') {
        newPlaylistName = document.getElementById('newPlaylistName').value.trim();
        if (!newPlaylistName) { showSnackbar('请输入歌单名称', 'error'); return; }
    } else if (playlistSelect.value) {
        playlistId = parseInt(playlistSelect.value);
    }

    const progressSection = document.getElementById('importProgress');
    const progressFill = document.getElementById('progressFill');
    const progressText = document.getElementById('progressText');
    const importResultsEl = document.getElementById('importResults');

    progressSection.style.display = '';
    progressFill.style.width = '0%';
    progressText.textContent = '正在导入...';
    importResultsEl.innerHTML = '';

    const importBtn = document.getElementById('importSongsBtn');
    importBtn.disabled = true;
    importBtn.innerHTML = '<span class="spinner"></span>导入中...';

    const requestBody = {
        songs: songs.map(song => ({
            name: song.name,
            singer: song.singer,
            album: song.album,
            source: song.source,
            musicId: song.musicId || song.music_id,
            img: song.img,
            hash: song.hash,
            songmid: song.songmid,
            strMediaMid: song.strMediaMid,
            albumMid: song.albumMid,
            copyrightId: song.copyrightId,
            albumId: song.albumId,
            duration: song.duration,
            types: song.types
        })),
        quality,
        playlist_id: playlistId,
        new_playlist_name: newPlaylistName
    };

    try {
        const response = await fetch(`${API_BASE}/songs/import`, {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify(requestBody)
        });
        const result = await response.json();
        if (result.code === 0) {
            const data = result.data;
            progressFill.style.width = '100%';
            progressText.textContent = `导入完成：成功 ${data.success} 首，失败 ${data.failed} 首`;
            importResultsEl.innerHTML = (data.results || []).map(item =>
                item.success
                    ? `<div class="import-result-item success">✓ ${escapeHtml(item.name)}</div>`
                    : `<div class="import-result-item error">✗ ${escapeHtml(item.name)}: ${escapeHtml(item.error)}</div>`
            ).join('');
            if (result.data && result.data.warning) {
                showSnackbar(result.data.warning, 'warning', 5000);
            }
            if (data.success > 0) {
                showSnackbar(`成功导入 ${data.success} 首歌曲`, 'success');
                selectedSongs.clear();
                updateSelectedCount();
                loadPlaylists();
            }
            if (data.failed > 0) showSnackbar(`${data.failed} 首歌曲导入失败`, 'error');
        } else {
            const msg = result.msg || '未知错误';
            const friendlyMsg = msg.includes('音源') ? '未配置有效的音源，无法获取播放链接。请先前往音源管理导入音源。' : msg;
            progressText.textContent = '导入失败: ' + friendlyMsg;
            showSnackbar('导入失败: ' + friendlyMsg, 'error');
        }
    } catch (e) {
        progressText.textContent = '导入失败: ' + e.message;
        showSnackbar('导入失败: ' + e.message, 'error');
    } finally {
        importBtn.disabled = selectedSongs.size === 0;
        importBtn.innerHTML = `导入选中 <span id="selectedBadge" class="badge">${selectedSongs.size}</span>`;
    }
}

// ============ 初始化 ============

document.addEventListener('DOMContentLoaded', function () {
    // 设置初始历史状态（使用 replaceState 避免多余条目）
    history.replaceState({ tab: 'search' }, '', '#search');

    // 监听浏览器返回/前进，恢复对应 Tab/子页面
    window.addEventListener('popstate', (event) => {
        if (event.state && event.state.tab) {
            window._isPopState = true;
            if (event.state.detail) {
                // 返回到歌单详情（前进时）
                slOpenDetail(event.state.detail);
            } else {
                switchToTab(event.state.tab);
            }
            window._isPopState = false;
        }
    });

    initTabs();
    loadPlatforms();
    loadPlaylists();
    checkSourceStatus();

    // 导入音源文件
    document.getElementById('importBtn').addEventListener('click', () => {
        document.getElementById('sourceFile').click();
    });
    document.getElementById('sourceFile').addEventListener('change', function (e) {
        const file = e.target.files[0];
        if (file) { importSource(file); e.target.value = ''; }
    });

    // 从 URL 导入音源
    document.getElementById('importUrlBtn').addEventListener('click', () => {
        importSourceFromURL(document.getElementById('sourceUrl').value);
    });
    document.getElementById('sourceUrl').addEventListener('keypress', function (e) {
        if (e.key === 'Enter') importSourceFromURL(this.value);
    });

    // 搜索
    document.getElementById('searchBtn').addEventListener('click', () => {
        search(document.getElementById('keyword').value, document.getElementById('platformSelect').value, 1);
    });
    document.getElementById('keyword').addEventListener('keypress', function (e) {
        if (e.key === 'Enter') {
            search(this.value, document.getElementById('platformSelect').value, 1);
        }
    });

    // 全选
    document.getElementById('selectAll').addEventListener('change', toggleSelectAll);

    // 导入歌曲
    document.getElementById('importSongsBtn').addEventListener('click', importSelectedSongs);

    // 清除选择
    document.getElementById('clearSelectionBtn').addEventListener('click', clearSelection);

    // 歌单选择变化
    document.getElementById('playlistSelect').addEventListener('change', function () {
        const wrapper = document.getElementById('newPlaylistWrapper');
        wrapper.style.display = this.value === '__new__' ? 'flex' : 'none';
    });

    // Dialog 点击遮罩关闭
    document.getElementById('dialogOverlay').addEventListener('click', function (e) {
        if (e.target === this) hideDialog();
    });

    // 初始化歌单 Tab
    initSonglistTab();
});

// ============ 歌单 Tab ============

// 歌单状态
let slCurrentPlatform = 'kg';
let slCurrentMode = 'recommend';
let slCurrentSortId = '';
let slCurrentTagId = '';
let slSortList = [];
let slTags = null;
let slSongLists = [];
let slCurrentPage = 1;
let slTotalResults = 0;
let slDetailSongs = [];
let slDetailInfo = null;
let slDetailPage = 1;
let slDetailTotal = 0;
let slSelectedSongs = new Map();
let slTagsLoaded = false;

function initSonglistTab() {
    // 模式切换
    document.querySelectorAll('.segment-btn').forEach(btn => {
        btn.addEventListener('click', function () {
            const mode = this.dataset.mode;
            switchSonglistMode(mode);
        });
    });

    // 平台切换
    document.getElementById('slPlatformSelect').addEventListener('change', function () {
        slCurrentPlatform = this.value;
        slTagsLoaded = false;
        slCurrentSortId = '';
        slCurrentTagId = '';
        if (slCurrentMode === 'recommend') {
            loadSonglistTagsAndList();
        }
    });

    // 搜索按钮
    document.getElementById('slActionBtn').addEventListener('click', slDoAction);

    // 搜索输入框回车
    document.getElementById('slSearchInput').addEventListener('keypress', function (e) {
        if (e.key === 'Enter') slDoAction();
    });
    document.getElementById('slParseInput').addEventListener('keypress', function (e) {
        if (e.key === 'Enter') slDoAction();
    });

    // 全选
    document.getElementById('slSelectAll').addEventListener('change', slToggleSelectAll);

    // 导入
    document.getElementById('slImportBtn').addEventListener('click', slImportSelectedSongs);

    // 歌单选择变化
    document.getElementById('slPlaylistSelect').addEventListener('change', function () {
        const wrapper = document.getElementById('slNewPlaylistWrapper');
        wrapper.style.display = this.value === '__new__' ? 'flex' : 'none';
    });
}

function switchSonglistMode(mode) {
    slCurrentMode = mode;
    document.querySelectorAll('.segment-btn').forEach(b => b.classList.toggle('active', b.dataset.mode === mode));

    const searchWrapper = document.getElementById('slSearchWrapper');
    const parseWrapper = document.getElementById('slParseWrapper');
    const actionBtn = document.getElementById('slActionBtn');
    const tagCard = document.getElementById('slTagCard');

    searchWrapper.style.display = 'none';
    parseWrapper.style.display = 'none';
    actionBtn.style.display = 'none';
    tagCard.style.display = 'none';

    // 切换模式时隐藏详情，显示列表
    document.getElementById('slDetailCard').style.display = 'none';

    if (mode === 'recommend') {
        if (slTagsLoaded) {
            tagCard.style.display = '';
        }
        loadSonglistTagsAndList();
    } else if (mode === 'search') {
        searchWrapper.style.display = '';
        actionBtn.style.display = '';
        actionBtn.textContent = '搜索';
        document.getElementById('slListCard').style.display = 'none';
    } else if (mode === 'parse') {
        parseWrapper.style.display = '';
        actionBtn.style.display = '';
        actionBtn.textContent = '解析';
        document.getElementById('slListCard').style.display = 'none';
    }
}

function slDoAction() {
    if (slCurrentMode === 'search') {
        const keyword = document.getElementById('slSearchInput').value.trim();
        if (!keyword) { showSnackbar('请输入搜索关键词', 'warning'); return; }
        slSearchSonglist(keyword, 1);
    } else if (slCurrentMode === 'parse') {
        const link = document.getElementById('slParseInput').value.trim();
        if (!link) { showSnackbar('请输入歌单链接', 'warning'); return; }
        slParseSonglistLink(link);
    }
}

// 加载标签和列表
async function loadSonglistTagsAndList() {
    if (!slTagsLoaded) {
        await Promise.all([slLoadTags(), slLoadSorts()]);
        slTagsLoaded = true;
    }
    slLoadList(1);
}

async function slLoadTags() {
    try {
        const resp = await fetch(`${API_BASE}/songlist/tags?source_id=${slCurrentPlatform}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slTags = result.data;
            slRenderTagChips();
            document.getElementById('slTagCard').style.display = '';
        }
    } catch (e) {
        console.error('加载标签失败:', e);
    }
}

async function slLoadSorts() {
    try {
        const resp = await fetch(`${API_BASE}/songlist/sorts?source_id=${slCurrentPlatform}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slSortList = result.data || [];
            if (slSortList.length > 0) slCurrentSortId = slSortList[0].id;
            slRenderSortChips();
        }
    } catch (e) {
        console.error('加载排序失败:', e);
    }
}

function slRenderSortChips() {
    const container = document.getElementById('slSortChips');
    if (!slSortList || slSortList.length === 0) {
        container.innerHTML = '';
        return;
    }
    container.innerHTML = slSortList.map(s =>
        `<button class="tag-chip${s.id === slCurrentSortId ? ' active' : ''}" data-sort-id="${escapeHtml(s.id)}">${escapeHtml(s.name)}</button>`
    ).join('');
    container.querySelectorAll('.tag-chip').forEach(chip => {
        chip.addEventListener('click', function () {
            slCurrentSortId = this.dataset.sortId;
            slRenderSortChips();
            slLoadList(1);
        });
    });
}

function slRenderTagChips() {
    const container = document.getElementById('slTagChips');
    if (!slTags) { container.innerHTML = ''; return; }

    let html = '';

    // 热门标签
    if (slTags.hot && slTags.hot.length > 0) {
        html += '<div class="tag-group-title">热门</div><div class="tag-group-chips">';
        html += `<button class="tag-chip${slCurrentTagId === '' ? ' active' : ''}" data-tag-id="">全部</button>`;
        html += slTags.hot.map(t =>
            `<button class="tag-chip${t.id === slCurrentTagId ? ' active' : ''}" data-tag-id="${escapeHtml(t.id)}">${escapeHtml(t.name)}</button>`
        ).join('');
        html += '</div>';
    }

    // 分组标签
    if (slTags.tags && slTags.tags.length > 0) {
        slTags.tags.forEach(group => {
            if (!group.list || group.list.length === 0) return;
            html += `<div class="tag-group-title">${escapeHtml(group.name)}</div><div class="tag-group-chips">`;
            html += group.list.map(t =>
                `<button class="tag-chip${t.id === slCurrentTagId ? ' active' : ''}" data-tag-id="${escapeHtml(t.id)}">${escapeHtml(t.name)}</button>`
            ).join('');
            html += '</div>';
        });
    }

    container.innerHTML = html;
    container.querySelectorAll('.tag-chip').forEach(chip => {
        chip.addEventListener('click', function () {
            slCurrentTagId = this.dataset.tagId;
            slRenderTagChips();
            slLoadList(1);
        });
    });
}

async function slLoadList(page) {
    slCurrentPage = page;
    const listCard = document.getElementById('slListCard');
    const grid = document.getElementById('slGrid');
    listCard.style.display = '';
    grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">hourglass_empty</span><p>加载中...</p></div>';

    try {
        const params = new URLSearchParams({
            source_id: slCurrentPlatform,
            sort_id: slCurrentSortId,
            tag_id: slCurrentTagId,
            page: page
        });
        const resp = await fetch(`${API_BASE}/songlist/list?${params}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slSongLists = result.data.list || [];
            slTotalResults = result.data.total || 0;
            slRenderGrid();
        } else {
            grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败</p></div>';
        }
    } catch (e) {
        grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败: ' + escapeHtml(e.message) + '</p></div>';
    }
}

async function slSearchSonglist(keyword, page) {
    slCurrentPage = page;
    const listCard = document.getElementById('slListCard');
    const grid = document.getElementById('slGrid');
    listCard.style.display = '';
    grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">hourglass_empty</span><p>搜索中...</p></div>';

    const actionBtn = document.getElementById('slActionBtn');
    actionBtn.disabled = true;
    actionBtn.innerHTML = '<span class="spinner"></span>';

    try {
        const params = new URLSearchParams({ source_id: slCurrentPlatform, keyword, page });
        const resp = await fetch(`${API_BASE}/songlist/search?${params}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slSongLists = result.data.list || [];
            slTotalResults = result.data.total || 0;
            slRenderGrid();
            document.getElementById('slListCount').textContent = `共 ${slTotalResults} 条`;
        } else {
            grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">search_off</span><p>搜索失败</p></div>';
        }
    } catch (e) {
        grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>搜索失败: ' + escapeHtml(e.message) + '</p></div>';
    } finally {
        actionBtn.disabled = false;
        actionBtn.textContent = '搜索';
    }
}

async function slParseSonglistLink(link) {
    const actionBtn = document.getElementById('slActionBtn');
    actionBtn.disabled = true;
    actionBtn.innerHTML = '<span class="spinner"></span>';

    try {
        const params = new URLSearchParams({ source_id: slCurrentPlatform, id: link, page: 1 });
        const resp = await fetch(`${API_BASE}/songlist/detail?${params}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slDetailSongs = result.data.list || [];
            slDetailInfo = result.data.info || {};
            slDetailPage = 1;
            slDetailTotal = result.data.total || 0;
            slShowDetail();
        } else {
            showSnackbar('解析失败: ' + (result.msg || '未知错误'), 'error');
        }
    } catch (e) {
        showSnackbar('解析失败: ' + e.message, 'error');
    } finally {
        actionBtn.disabled = false;
        actionBtn.textContent = '解析';
    }
}

function slRenderGrid() {
    const grid = document.getElementById('slGrid');
    const countEl = document.getElementById('slListCount');

    if (slSongLists.length === 0) {
        grid.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">queue_music</span><p>暂无歌单</p></div>';
        countEl.textContent = '';
        document.getElementById('slPagination').innerHTML = '';
        return;
    }

    countEl.textContent = slTotalResults > 0 ? `共 ${slTotalResults} 条` : '';

    grid.innerHTML = slSongLists.map((item, i) => {
        const img = item.img
            ? `<img class="songlist-cover" src="${escapeHtml(item.img)}" alt="" loading="lazy" onerror="this.style.display='none'">`
            : '<div class="songlist-cover" style="display:flex;align-items:center;justify-content:center"><span class="material-symbols-outlined" style="font-size:40px;color:var(--md-outline)">queue_music</span></div>';
        return `
            <div class="songlist-card" data-index="${i}" onclick="slOpenDetail('${escapeHtml(item.id)}')">
                ${img}
                <div class="songlist-card-body">
                    <div class="songlist-name">${escapeHtml(item.name)}</div>
                    <div class="songlist-meta">
                        ${item.play_count || item.playCount ? `<span class="songlist-play-count"><span class="material-symbols-outlined">play_arrow</span>${escapeHtml(item.play_count || item.playCount)}</span>` : ''}
                        ${item.author ? `<span>${escapeHtml(item.author)}</span>` : ''}
                    </div>
                </div>
            </div>
        `;
    }).join('');

    slRenderListPagination();
}

function slRenderListPagination() {
    const container = document.getElementById('slPagination');
    const limit = 30;
    const totalPages = Math.max(1, Math.ceil(slTotalResults / limit));
    if (totalPages <= 1) { container.innerHTML = ''; return; }

    const prevDisabled = slCurrentPage <= 1 ? 'disabled' : '';
    const nextDisabled = slCurrentPage >= totalPages ? 'disabled' : '';

    container.innerHTML = `
        <button class="btn-icon" title="上一页" ${prevDisabled} onclick="slPageNav(${slCurrentPage - 1})">
            <span class="material-symbols-outlined">chevron_left</span>
        </button>
        <span class="page-info">第 ${slCurrentPage} / ${totalPages} 页</span>
        <button class="btn-icon" title="下一页" ${nextDisabled} onclick="slPageNav(${slCurrentPage + 1})">
            <span class="material-symbols-outlined">chevron_right</span>
        </button>
    `;
}

function slPageNav(page) {
    if (slCurrentMode === 'search') {
        const keyword = document.getElementById('slSearchInput').value.trim();
        slSearchSonglist(keyword, page);
    } else {
        slLoadList(page);
    }
}

async function slOpenDetail(id) {
    // 推入子页面历史记录
    if (!window._isPopState) {
        history.pushState({ tab: 'songlist', detail: id }, '', '#songlist-detail');
    }
    const detailCard = document.getElementById('slDetailCard');
    const listCard = document.getElementById('slListCard');
    const tagCard = document.getElementById('slTagCard');
    const detailList = document.getElementById('slDetailList');

    detailList.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">hourglass_empty</span><p>加载中...</p></div>';
    detailCard.style.display = '';
    listCard.style.display = 'none';
    tagCard.style.display = 'none';

    slSelectedSongs.clear();
    slUpdateSelectedCount();

    try {
        const params = new URLSearchParams({ source_id: slCurrentPlatform, id, page: 1 });
        const resp = await fetch(`${API_BASE}/songlist/detail?${params}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slDetailSongs = result.data.list || [];
            slDetailInfo = result.data.info || {};
            slDetailPage = 1;
            slDetailTotal = result.data.total || 0;
            slCurrentDetailId = id;
            slShowDetail();
        } else {
            detailList.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败</p></div>';
        }
    } catch (e) {
        detailList.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败: ' + escapeHtml(e.message) + '</p></div>';
    }
}

let slCurrentDetailId = '';

function slShowDetail() {
    const detailCard = document.getElementById('slDetailCard');
    const listCard = document.getElementById('slListCard');
    const tagCard = document.getElementById('slTagCard');

    detailCard.style.display = '';
    listCard.style.display = 'none';
    tagCard.style.display = 'none';

    // 渲染歌单信息
    const infoEl = document.getElementById('slDetailInfo');
    if (slDetailInfo && (slDetailInfo.name || slDetailInfo.img)) {
        const img = slDetailInfo.img
            ? `<img class="songlist-info-cover" src="${escapeHtml(slDetailInfo.img)}" alt="" onerror="this.style.display='none'">`
            : '';
        infoEl.innerHTML = `
            ${img}
            <div class="songlist-info-detail">
                <div class="songlist-info-name">${escapeHtml(slDetailInfo.name || '')}</div>
                <div class="songlist-info-author">
                    ${slDetailInfo.author ? escapeHtml(slDetailInfo.author) : ''}
                    ${slDetailInfo.play_count || slDetailInfo.playCount ? ' · ' + escapeHtml(slDetailInfo.play_count || slDetailInfo.playCount) + ' 播放' : ''}
                </div>
                ${slDetailInfo.desc ? `<div class="songlist-info-desc" onclick="this.classList.toggle('expanded')">${escapeHtml(slDetailInfo.desc)}</div>` : ''}
            </div>
        `;
    } else {
        infoEl.innerHTML = '';
    }

    // 渲染歌曲列表
    slRenderDetailList();

    // 渲染歌单选择（复用主页的歌单数据）
    const select = document.getElementById('slPlaylistSelect');
    let html = '<option value="">不添加到歌单</option>';
    if (Array.isArray(playlists)) {
        for (const pl of playlists) {
            html += `<option value="${pl.id}">${escapeHtml(pl.name)}</option>`;
        }
    }
    html += '<option value="__new__">+ 新建歌单...</option>';
    select.innerHTML = html;
}

function slRenderDetailList() {
    const container = document.getElementById('slDetailList');

    if (slDetailSongs.length === 0) {
        container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">music_off</span><p>暂无歌曲</p></div>';
        document.getElementById('slDetailPagination').innerHTML = '';
        return;
    }

    container.innerHTML = slDetailSongs.map((song, i) => {
        const key = getSongKey(song);
        const checked = slSelectedSongs.has(key) ? 'checked' : '';
        const selectedClass = slSelectedSongs.has(key) ? ' selected' : '';
        const imgHtml = song.img
            ? `<img src="${escapeHtml(song.img)}" alt="" loading="lazy" onerror="this.parentNode.innerHTML='<span class=\\'material-symbols-outlined\\'>music_note</span>'">`
            : '<span class="material-symbols-outlined">music_note</span>';
        return `
            <div class="result-item${selectedClass}" data-index="${i}">
                <input type="checkbox" class="sl-detail-checkbox" data-index="${i}" ${checked}
                    onchange="slOnSongCheckChanged(${i}, this.checked)" style="accent-color:var(--md-primary);width:18px;height:18px;cursor:pointer;flex-shrink:0">
                <div class="result-thumb">${imgHtml}</div>
                <div class="result-info">
                    <div class="result-name">${escapeHtml(song.name)}</div>
                    <div class="result-meta">${escapeHtml(song.singer || '')}${song.album ? ' — ' + escapeHtml(song.album) : ''}</div>
                </div>
                <div class="result-duration">${formatDuration(song.duration)}</div>
            </div>
        `;
    }).join('');

    // 同步全选状态
    const allChecked = slDetailSongs.every(s => slSelectedSongs.has(getSongKey(s)));
    document.getElementById('slSelectAll').checked = allChecked && slDetailSongs.length > 0;

    slRenderDetailPagination();
}

function slOnSongCheckChanged(index, checked) {
    const song = slDetailSongs[index];
    const key = getSongKey(song);
    if (checked) slSelectedSongs.set(key, song);
    else slSelectedSongs.delete(key);
    const row = document.querySelectorAll('#slDetailList .result-item')[index];
    if (row) row.classList.toggle('selected', checked);
    slUpdateSelectedCount();
    const allChecked = slDetailSongs.every(s => slSelectedSongs.has(getSongKey(s)));
    document.getElementById('slSelectAll').checked = allChecked;
}

function slToggleSelectAll() {
    const checked = document.getElementById('slSelectAll').checked;
    slDetailSongs.forEach(song => {
        const key = getSongKey(song);
        if (checked) slSelectedSongs.set(key, song);
        else slSelectedSongs.delete(key);
    });
    slRenderDetailList();
    slUpdateSelectedCount();
}

function slUpdateSelectedCount() {
    const count = slSelectedSongs.size;
    const badge = document.getElementById('slSelectedBadge');
    if (badge) badge.textContent = count;
    document.getElementById('slImportBtn').disabled = count === 0;
}

function slRenderDetailPagination() {
    const container = document.getElementById('slDetailPagination');
    const limit = 50;
    const totalPages = Math.max(1, Math.ceil(slDetailTotal / limit));
    if (totalPages <= 1) { container.innerHTML = ''; return; }

    const prevDisabled = slDetailPage <= 1 ? 'disabled' : '';
    const nextDisabled = slDetailPage >= totalPages ? 'disabled' : '';

    container.innerHTML = `
        <button class="btn-icon" title="上一页" ${prevDisabled} onclick="slDetailPageNav(${slDetailPage - 1})">
            <span class="material-symbols-outlined">chevron_left</span>
        </button>
        <span class="page-info">第 ${slDetailPage} / ${totalPages} 页</span>
        <button class="btn-icon" title="下一页" ${nextDisabled} onclick="slDetailPageNav(${slDetailPage + 1})">
            <span class="material-symbols-outlined">chevron_right</span>
        </button>
    `;
}

async function slDetailPageNav(page) {
    if (!slCurrentDetailId) return;
    slDetailPage = page;
    const detailList = document.getElementById('slDetailList');
    detailList.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">hourglass_empty</span><p>加载中...</p></div>';

    try {
        const params = new URLSearchParams({ source_id: slCurrentPlatform, id: slCurrentDetailId, page });
        const resp = await fetch(`${API_BASE}/songlist/detail?${params}`, { headers: getAuthHeaders() });
        const result = await resp.json();
        if (result.code === 0) {
            slDetailSongs = result.data.list || [];
            slDetailTotal = result.data.total || 0;
            slRenderDetailList();
        } else {
            detailList.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败</p></div>';
        }
    } catch (e) {
        detailList.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">error</span><p>加载失败</p></div>';
    }
}

let slTagCardCollapsed = false;

function slToggleTagCard() {
    slTagCardCollapsed = !slTagCardCollapsed;
    const body = document.getElementById('slTagCardBody');
    const icon = document.querySelector('#slTagToggleBtn .material-symbols-outlined');
    if (slTagCardCollapsed) {
        body.style.display = 'none';
        icon.textContent = 'expand_more';
    } else {
        body.style.display = '';
        icon.textContent = 'expand_less';
    }
}

function slBackToList() {
    // 通过 history.back() 触发 popstate 来返回，避免重复 pushState
    if (!window._isPopState) {
        history.back();
        return;
    }
    document.getElementById('slDetailCard').style.display = 'none';
    if (slCurrentMode === 'recommend') {
        document.getElementById('slListCard').style.display = '';
        document.getElementById('slTagCard').style.display = '';
    } else if (slCurrentMode === 'search') {
        document.getElementById('slListCard').style.display = '';
    }
    // parse 模式返回时不显示列表
}

async function slImportSelectedSongs() {
    const songs = Array.from(slSelectedSongs.values());
    if (songs.length === 0) { showSnackbar('请选择要导入的歌曲', 'warning'); return; }

    if (!hasEnabledSources) {
        const proceed = await showDialog(
            '未配置音源',
            '当前未配置有效的洛雪音源，导入的歌曲将无法播放。\n\n是否仍要继续导入？',
            { confirmText: '继续导入', cancelText: '去配置音源' }
        );
        if (!proceed) { switchToTab('sources'); return; }
    }

    const quality = document.getElementById('slQualitySelect').value;
    const playlistSelect = document.getElementById('slPlaylistSelect');
    let playlistId = 0;
    let newPlaylistName = '';

    if (playlistSelect.value === '__new__') {
        newPlaylistName = document.getElementById('slNewPlaylistName').value.trim();
        if (!newPlaylistName) { showSnackbar('请输入歌单名称', 'error'); return; }
    } else if (playlistSelect.value) {
        playlistId = parseInt(playlistSelect.value);
    }

    const progressSection = document.getElementById('slImportProgress');
    const progressFill = document.getElementById('slProgressFill');
    const progressText = document.getElementById('slProgressText');
    const importResultsEl = document.getElementById('slImportResults');

    progressSection.style.display = '';
    progressFill.style.width = '0%';
    progressText.textContent = '正在导入...';
    importResultsEl.innerHTML = '';

    const importBtn = document.getElementById('slImportBtn');
    importBtn.disabled = true;
    importBtn.innerHTML = '<span class="spinner"></span>导入中...';

    const requestBody = {
        songs: songs.map(song => ({
            name: song.name,
            singer: song.singer,
            album: song.album,
            source: song.source,
            musicId: song.musicId || song.music_id,
            img: song.img,
            hash: song.hash,
            songmid: song.songmid,
            strMediaMid: song.strMediaMid,
            albumMid: song.albumMid,
            copyrightId: song.copyrightId,
            albumId: song.albumId,
            duration: song.duration,
            types: song.types
        })),
        quality,
        playlist_id: playlistId,
        new_playlist_name: newPlaylistName
    };

    try {
        const response = await fetch(`${API_BASE}/songs/import`, {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify(requestBody)
        });
        const result = await response.json();
        if (result.code === 0) {
            const data = result.data;
            progressFill.style.width = '100%';
            progressText.textContent = `导入完成：成功 ${data.success} 首，失败 ${data.failed} 首`;
            importResultsEl.innerHTML = (data.results || []).map(item =>
                item.success
                    ? `<div class="import-result-item success">✓ ${escapeHtml(item.name)}</div>`
                    : `<div class="import-result-item error">✗ ${escapeHtml(item.name)}: ${escapeHtml(item.error)}</div>`
            ).join('');
            if (result.data && result.data.warning) {
                showSnackbar(result.data.warning, 'warning', 5000);
            }
            if (data.success > 0) {
                showSnackbar(`成功导入 ${data.success} 首歌曲`, 'success');
                slSelectedSongs.clear();
                slUpdateSelectedCount();
                loadPlaylists();
            }
            if (data.failed > 0) showSnackbar(`${data.failed} 首歌曲导入失败`, 'error');
        } else {
            const msg = result.msg || '未知错误';
            const friendlyMsg = msg.includes('音源') ? '未配置有效的音源，无法获取播放链接。请先前往音源管理导入音源。' : msg;
            progressText.textContent = '导入失败: ' + friendlyMsg;
            showSnackbar('导入失败: ' + friendlyMsg, 'error');
        }
    } catch (e) {
        progressText.textContent = '导入失败: ' + e.message;
        showSnackbar('导入失败: ' + e.message, 'error');
    } finally {
        importBtn.disabled = slSelectedSongs.size === 0;
        importBtn.innerHTML = `导入选中 <span id="slSelectedBadge" class="badge">${slSelectedSongs.size}</span>`;
    }
}
