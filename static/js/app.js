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

function showDialog(title, content) {
    return new Promise((resolve) => {
        document.getElementById('dialogTitle').textContent = title;
        document.getElementById('dialogContent').textContent = content;
        const overlay = document.getElementById('dialogOverlay');
        overlay.style.display = 'flex';
        document.getElementById('dialogConfirm').onclick = () => { hideDialog(); resolve(true); };
        document.getElementById('dialogCancel').onclick = () => { hideDialog(); resolve(false); };
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

// ============ 音源管理 ============

async function loadSources() {
    const container = document.getElementById('sourceList');
    container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">hourglass_empty</span><p>加载中...</p></div>';
    try {
        const response = await fetch(`${API_BASE}/sources`, { headers: getAuthHeaders() });
        const result = await response.json();
        if (result.success) {
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
        container.innerHTML = '<div class="empty-state"><span class="material-symbols-outlined">inbox</span><p>暂无音源，请导入音源脚本以获取播放 URL</p></div>';
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
            if (data.success > 0) {
                showSnackbar(`成功导入 ${data.success} 首歌曲`, 'success');
                selectedSongs.clear();
                updateSelectedCount();
                loadPlaylists();
            }
            if (data.failed > 0) showSnackbar(`${data.failed} 首歌曲导入失败`, 'error');
        } else {
            progressText.textContent = '导入失败: ' + (result.msg || '未知错误');
            showSnackbar('导入失败: ' + (result.msg || '未知错误'), 'error');
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
    initTabs();
    loadPlatforms();
    loadPlaylists();

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
});
