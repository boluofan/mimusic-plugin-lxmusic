// API 基础路径
const API_BASE = '/api/v1/plugin/lxmusic/api';

// 当前状态
let currentSources = [];
let currentPlatforms = [];
let searchResults = [];
let currentPage = 1;
let totalResults = 0;
let currentPlatformId = '';
let currentKeyword = '';

/**
 * 从 localStorage 获取认证 Token
 */
function getAuthToken() {
    try {
        var authData = localStorage.getItem('mimusic-auth');
        if (authData) {
            var auth = JSON.parse(authData);
            return auth.accessToken || '';
        }
    } catch (e) {
        console.error('获取 Token 失败:', e);
    }
    return '';
}

/**
 * 获取认证头
 */
function getAuthHeaders(isJson = true) {
    var headers = {};
    if (isJson) {
        headers['Content-Type'] = 'application/json';
    }
    var token = getAuthToken();
    if (token) {
        headers['Authorization'] = 'Bearer ' + token;
    }
    return headers;
}

/**
 * 显示 Toast 消息
 */
function showToast(message, type = 'info') {
    const toast = document.getElementById('toast');
    toast.textContent = message;
    toast.className = 'toast ' + type + ' show';
    
    setTimeout(() => {
        toast.className = 'toast';
    }, 3000);
}

/**
 * 格式化时长为 mm:ss
 */
function formatDuration(seconds) {
    if (!seconds || seconds <= 0) return '--:--';
    const mins = Math.floor(seconds / 60);
    const secs = seconds % 60;
    return `${mins}:${secs.toString().padStart(2, '0')}`;
}

/**
 * HTML 转义
 */
function escapeHtml(text) {
    if (!text) return '';
    const div = document.createElement('div');
    div.textContent = text;
    return div.innerHTML;
}

// ============ 平台管理 ============

/**
 * 加载内置平台列表
 */
async function loadPlatforms() {
    try {
        const response = await fetch(`${API_BASE}/platforms`, {
            headers: getAuthHeaders()
        });
        const result = await response.json();
        
        if (result.code === 0) {
            currentPlatforms = result.data || [];
            updatePlatformSelect();
        } else {
            showToast('加载平台列表失败: ' + (result.msg || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('加载平台列表失败:', error);
        showToast('加载平台列表失败: ' + error.message, 'error');
    }
}

/**
 * 更新平台下拉选择
 */
function updatePlatformSelect() {
    const select = document.getElementById('platformSelect');
    const searchBtn = document.getElementById('searchBtn');
    
    if (currentPlatforms.length === 0) {
        select.innerHTML = '<option value="">暂无可用平台</option>';
        searchBtn.disabled = true;
    } else {
        let html = '';
        for (const platform of currentPlatforms) {
            html += `<option value="${escapeHtml(platform.id)}">${escapeHtml(platform.name)}</option>`;
        }
        select.innerHTML = html;
        searchBtn.disabled = false;
    }
}

// ============ 音源管理 ============

/**
 * 加载音源列表
 */
async function loadSources() {
    try {
        const response = await fetch(`${API_BASE}/sources`, {
            headers: getAuthHeaders()
        });
        const result = await response.json();
        
        if (result.success) {
            currentSources = result.data || [];
            renderSources();
        } else {
            showToast('加载音源失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('加载音源失败:', error);
        showToast('加载音源失败: ' + error.message, 'error');
    }
}

/**
 * 渲染音源列表
 */
function renderSources() {
    const container = document.getElementById('sourceList');
    
    if (currentSources.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="icon">📦</div><div>暂无音源，请导入音源脚本以获取播放 URL</div></div>';
        return;
    }
    
    let html = '';
    for (const source of currentSources) {
        const checkedAttr = source.enabled ? 'checked' : '';
        html += `
            <div class="source-item" data-id="${escapeHtml(source.id)}">
                <div class="source-info">
                    <div class="source-name">${escapeHtml(source.name)}</div>
                    <div class="source-meta">
                        版本: ${escapeHtml(source.version || '-')} | 
                        作者: ${escapeHtml(source.author || '-')} | 
                        导入时间: ${source.imported_at ? new Date(source.imported_at).toLocaleString() : '-'}
                    </div>
                </div>
                <div class="source-actions">
                    <label class="toggle-switch" title="${source.enabled ? '已启用' : '已禁用'}">
                        <input type="checkbox" ${checkedAttr} onchange="toggleSource('${escapeHtml(source.id)}', this.checked)">
                        <span class="toggle-slider"></span>
                    </label>
                    <button class="btn btn-danger btn-small" onclick="deleteSource('${escapeHtml(source.id)}')">删除</button>
                </div>
            </div>
        `;
    }
    container.innerHTML = html;
}

/**
 * 切换音源启用/禁用状态
 */
async function toggleSource(id, enabled) {
    try {
        const response = await fetch(`${API_BASE}/sources/toggle`, {
            method: 'PUT',
            headers: getAuthHeaders(),
            body: JSON.stringify({ id: id, enabled: enabled })
        });
        const result = await response.json();
        
        if (result.success) {
            showToast(enabled ? '音源已启用' : '音源已禁用', 'success');
            // 更新本地状态
            const source = currentSources.find(s => s.id === id);
            if (source) {
                source.enabled = enabled;
            }
        } else {
            showToast('操作失败: ' + (result.message || result.error || '未知错误'), 'error');
            // 恢复 checkbox 状态
            loadSources();
        }
    } catch (error) {
        console.error('切换音源状态失败:', error);
        showToast('操作失败: ' + error.message, 'error');
        // 恢复 checkbox 状态
        loadSources();
    }
}

/**
 * 导入音源
 */
async function importSource(file) {
    const formData = new FormData();
    formData.append('file', file);
    
    try {
        showToast('正在导入...', 'info');
        
        const headers = {};
        const token = getAuthToken();
        if (token) {
            headers['Authorization'] = 'Bearer ' + token;
        }
        
        const response = await fetch(`${API_BASE}/sources/import`, {
            method: 'POST',
            headers: headers,
            body: formData
        });
        const result = await response.json();
        
        if (result.success) {
            showToast('导入成功', 'success');
            loadSources();
        } else {
            showToast('导入失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('导入音源失败:', error);
        showToast('导入失败: ' + error.message, 'error');
    }
}

/**
 * 从 URL 导入音源
 */
async function importSourceFromURL(url) {
    if (!url || !url.trim()) {
        showToast('请输入音源 URL', 'warning');
        return;
    }
    
    // 验证 URL 格式
    if (!url.startsWith('http://') && !url.startsWith('https://')) {
        showToast('URL 必须以 http:// 或 https:// 开头', 'warning');
        return;
    }
    
    try {
        showToast('正在从 URL 导入...', 'info');
        
        const response = await fetch(`${API_BASE}/sources/import-url`, {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify({ url: url.trim() })
        });
        const result = await response.json();
        
        if (result.success) {
            showToast('导入成功', 'success');
            document.getElementById('sourceUrl').value = ''; // 清空输入框
            loadSources();
        } else {
            showToast('导入失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('从 URL 导入音源失败:', error);
        showToast('导入失败: ' + error.message, 'error');
    }
}

/**
 * 删除音源
 */
async function deleteSource(id) {
    if (!confirm('确定要删除这个音源吗？')) {
        return;
    }
    
    try {
        const response = await fetch(`${API_BASE}/sources?id=${encodeURIComponent(id)}`, {
            method: 'DELETE',
            headers: getAuthHeaders()
        });
        const result = await response.json();
        
        if (result.success) {
            showToast('删除成功', 'success');
            loadSources();
        } else {
            showToast('删除失败: ' + (result.message || result.error || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('删除音源失败:', error);
        showToast('删除失败: ' + error.message, 'error');
    }
}

// ============ 歌曲搜索 ============

/**
 * 搜索歌曲
 */
async function search(keyword, platformId, page = 1) {
    if (!keyword.trim()) {
        showToast('请输入搜索关键词', 'warning');
        return;
    }
    
    if (!platformId) {
        showToast('请选择平台', 'warning');
        return;
    }
    
    currentKeyword = keyword;
    currentPlatformId = platformId;
    currentPage = page;
    
    const searchBtn = document.getElementById('searchBtn');
    searchBtn.disabled = true;
    searchBtn.innerHTML = '<span class="loading"></span>搜索中...';
    
    try {
        const params = new URLSearchParams({
            keyword: keyword,
            source_id: platformId,
            page: page
        });
        
        const response = await fetch(`${API_BASE}/search?${params}`, {
            headers: getAuthHeaders()
        });
        const result = await response.json();
        
        if (result.code === 0) {
            searchResults = result.data.list || [];
            totalResults = result.data.total || 0;
            renderResults();
            document.getElementById('resultSection').style.display = 'block';
        } else {
            showToast('搜索失败: ' + (result.msg || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('搜索失败:', error);
        showToast('搜索失败: ' + error.message, 'error');
    } finally {
        searchBtn.disabled = false;
        searchBtn.textContent = '搜索';
    }
}

/**
 * 渲染搜索结果
 */
function renderResults() {
    const container = document.getElementById('resultList');
    const countEl = document.getElementById('resultCount');
    
    countEl.textContent = `(共 ${totalResults} 条)`;
    
    if (searchResults.length === 0) {
        container.innerHTML = '<div class="empty-state"><div class="icon">🔍</div><div>没有找到相关歌曲</div></div>';
        return;
    }
    
    let html = '';
    for (let i = 0; i < searchResults.length; i++) {
        const song = searchResults[i];
        html += `
            <div class="result-item">
                <input type="checkbox" class="result-checkbox" data-index="${i}" onchange="updateSelectedCount()">
                <div class="result-info">
                    <div class="result-name">${escapeHtml(song.name)}</div>
                    <div class="result-meta">${escapeHtml(song.singer)} - ${escapeHtml(song.album || '未知专辑')}</div>
                </div>
                <div class="result-duration">${formatDuration(song.duration)}</div>
            </div>
        `;
    }
    container.innerHTML = html;
    
    // 重置全选状态
    document.getElementById('selectAll').checked = false;
    updateSelectedCount();
    
    // 渲染分页
    renderPagination();
}

/**
 * 渲染分页
 */
function renderPagination() {
    const container = document.getElementById('pagination');
    const totalPages = Math.ceil(totalResults / 30);
    
    if (totalPages <= 1) {
        container.innerHTML = '';
        return;
    }
    
    let html = `
        <button ${currentPage <= 1 ? 'disabled' : ''} onclick="search('${escapeHtml(currentKeyword)}', '${escapeHtml(currentPlatformId)}', ${currentPage - 1})">上一页</button>
        <span class="page-info">第 ${currentPage} / ${totalPages} 页</span>
        <button ${currentPage >= totalPages ? 'disabled' : ''} onclick="search('${escapeHtml(currentKeyword)}', '${escapeHtml(currentPlatformId)}', ${currentPage + 1})">下一页</button>
    `;
    container.innerHTML = html;
}

/**
 * 全选/反选
 */
function toggleSelectAll() {
    const checked = document.getElementById('selectAll').checked;
    const checkboxes = document.querySelectorAll('.result-checkbox');
    checkboxes.forEach(cb => cb.checked = checked);
    updateSelectedCount();
}

/**
 * 更新已选数量
 */
function updateSelectedCount() {
    const checkboxes = document.querySelectorAll('.result-checkbox:checked');
    const count = checkboxes.length;
    document.getElementById('selectedCount').textContent = `已选 ${count} 首`;
    document.getElementById('importSongsBtn').disabled = count === 0;
}

// ============ 批量导入 ============

/**
 * 导入选中的歌曲
 */
async function importSelectedSongs() {
    const checkboxes = document.querySelectorAll('.result-checkbox:checked');
    if (checkboxes.length === 0) {
        showToast('请选择要导入的歌曲', 'warning');
        return;
    }
    
    // 获取选择的音质
    const quality = document.getElementById('qualitySelect').value;
    
    const songs = [];
    checkboxes.forEach(cb => {
        const index = parseInt(cb.dataset.index);
        const song = searchResults[index];
        songs.push({
            name: song.name,
            singer: song.singer,
            album: song.album,
            source: song.source,
            music_id: song.music_id,
            img: song.img,
            // 平台特有字段
            hash: song.hash,
            songmid: song.songmid,
            strMediaMid: song.strMediaMid,
            albumMid: song.albumMid,
            copyrightId: song.copyrightId,
            albumId: song.albumId
        });
    });
    
    // 显示进度区域
    const progressSection = document.getElementById('importProgress');
    const progressFill = document.getElementById('progressFill');
    const progressText = document.getElementById('progressText');
    const importResults = document.getElementById('importResults');
    
    progressSection.style.display = 'block';
    progressFill.style.width = '0%';
    progressText.textContent = '正在导入...';
    importResults.innerHTML = '';
    
    // 禁用导入按钮
    const importBtn = document.getElementById('importSongsBtn');
    importBtn.disabled = true;
    importBtn.textContent = '导入中...';
    
    try {
        const response = await fetch(`${API_BASE}/songs/import`, {
            method: 'POST',
            headers: getAuthHeaders(),
            body: JSON.stringify({
                songs: songs,
                quality: quality
            })
        });
        const result = await response.json();
        
        if (result.code === 0) {
            const data = result.data;
            progressFill.style.width = '100%';
            progressText.textContent = `导入完成: 成功 ${data.success} 首, 失败 ${data.failed} 首`;
            
            // 显示详细结果
            let html = '';
            for (const item of data.results) {
                if (item.success) {
                    html += `<div class="import-result-item success">✓ ${escapeHtml(item.name)}</div>`;
                } else {
                    html += `<div class="import-result-item error">✗ ${escapeHtml(item.name)}: ${escapeHtml(item.error)}</div>`;
                }
            }
            importResults.innerHTML = html;
            
            if (data.success > 0) {
                showToast(`成功导入 ${data.success} 首歌曲`, 'success');
            }
            if (data.failed > 0) {
                showToast(`${data.failed} 首歌曲导入失败`, 'error');
            }
        } else {
            progressText.textContent = '导入失败: ' + (result.msg || '未知错误');
            showToast('导入失败: ' + (result.msg || '未知错误'), 'error');
        }
    } catch (error) {
        console.error('导入失败:', error);
        progressText.textContent = '导入失败: ' + error.message;
        showToast('导入失败: ' + error.message, 'error');
    } finally {
        importBtn.disabled = false;
        importBtn.textContent = '导入选中歌曲';
        updateSelectedCount();
    }
}

// ============ 初始化 ============

document.addEventListener('DOMContentLoaded', function() {
    // 加载内置平台列表
    loadPlatforms();
    
    // 加载音源列表
    loadSources();
    
    // 导入音源按钮
    document.getElementById('importBtn').addEventListener('click', function() {
        document.getElementById('sourceFile').click();
    });
    
    // 文件选择
    document.getElementById('sourceFile').addEventListener('change', function(e) {
        const file = e.target.files[0];
        if (file) {
            importSource(file);
            e.target.value = ''; // 清空以允许重复选择同一文件
        }
    });
    
    // 从 URL 导入按钮
    document.getElementById('importUrlBtn').addEventListener('click', function() {
        const url = document.getElementById('sourceUrl').value;
        importSourceFromURL(url);
    });
    
    // URL 输入框回车导入
    document.getElementById('sourceUrl').addEventListener('keypress', function(e) {
        if (e.key === 'Enter') {
            const url = document.getElementById('sourceUrl').value;
            importSourceFromURL(url);
        }
    });
    
    // 搜索按钮
    document.getElementById('searchBtn').addEventListener('click', function() {
        const keyword = document.getElementById('keyword').value;
        const platformId = document.getElementById('platformSelect').value;
        search(keyword, platformId, 1);
    });
    
    // 回车搜索
    document.getElementById('keyword').addEventListener('keypress', function(e) {
        if (e.key === 'Enter') {
            const keyword = document.getElementById('keyword').value;
            const platformId = document.getElementById('platformSelect').value;
            search(keyword, platformId, 1);
        }
    });
    
    // 全选
    document.getElementById('selectAll').addEventListener('change', toggleSelectAll);
    
    // 导入歌曲按钮
    document.getElementById('importSongsBtn').addEventListener('click', importSelectedSongs);
});
