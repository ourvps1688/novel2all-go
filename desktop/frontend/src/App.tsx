import { useState, useEffect } from 'react';
import './App.css';
import {
    BackendURL,
    HealthCheck,
    Login,
    Logout,
    IsLoggedIn,
    CurrentUser,
    ListProjects,
    ListChapters,
    CheckForUpdate,
    CurrentVersion,
    DownloadUpdate,
    ApplyUpdate,
    CreateProject,
    UpdateProject,
    DeleteProject,
    GetProject,
} from '../wailsjs/go/main/App';
import type { main } from '../wailsjs/go/models';

type User = main.User;
type Project = main.Project;
type Chapter = main.Chapter;

function App() {
    // Auth 状态
    const [loggedIn, setLoggedIn] = useState(false);
    const [user, setUser] = useState<User | null>(null);

    // 表单状态
    const [username, setUsername] = useState('');
    const [password, setPassword] = useState('');
    const [loading, setLoading] = useState(false);
    const [error, setError] = useState<string>('');

    // 设置页可见性
    const [showSettings, setShowSettings] = useState(false);

    // 项目 Modal (Phase F: CRUD)
    const [projectModal, setProjectModal] = useState<{
        mode: 'create' | 'edit' | null;
        project?: Project;
    }>({ mode: null });

    // 后端连接状态
    const [backendURL, setBackendURL] = useState<string>('');
    const [healthMsg, setHealthMsg] = useState<string>('未检查');

    // 项目列表
    const [projects, setProjects] = useState<Project[]>([]);
    const [selectedProject, setSelectedProject] = useState<Project | null>(null);
    const [chapters, setChapters] = useState<Chapter[]>([]);

    // 启动时检查后端 + 是否已登录
    useEffect(() => {
        (async () => {
            try {
                const url = await BackendURL();
                setBackendURL(url);
                // HealthCheck() 返回 Promise<boolean|string>: Go nil error → true, Go error → string.
                const result = await HealthCheck();
                if (typeof result === 'boolean') {
                    setHealthMsg(result ? '✅ 后端 OK' : '❌ 后端返回 false');
                } else {
                    setHealthMsg(`❌ ${result}`);
                }
            } catch (e: any) {
                setHealthMsg(`❌ 无法连接: ${e?.message ?? e}`);
            }
            try {
                const ok = await IsLoggedIn();
                if (ok) {
                    setLoggedIn(true);
                    const u = await CurrentUser();
                    setUser(u);
                }
            } catch {}
        })();
    }, []);

    // 登录
    async function doLogin() {
        if (!username || !password) {
            setError('请输入用户名和密码');
            return;
        }
        setLoading(true);
        setError('');
        try {
            const u = await Login(username, password);
            setUser(u);
            setLoggedIn(true);
            setError('');
            await refreshProjects();
        } catch (e: any) {
            setError(`登录失败: ${e?.message ?? e}`);
        } finally {
            setLoading(false);
        }
    }

    // 登出
    async function doLogout() {
        await Logout();
        setUser(null);
        setLoggedIn(false);
        setProjects([]);
        setSelectedProject(null);
        setChapters([]);
    }

    // 刷新项目列表
    async function refreshProjects() {
        try {
            const list = await ListProjects();
            setProjects(list ?? []);
        } catch (e: any) {
            setError(`获取项目失败: ${e?.message ?? e}`);
        }
    }

    // Phase F: 项目 CRUD
    function openProjectModal(mode: 'create' | 'edit', project?: Project) {
        setProjectModal({ mode, project });
        setError('');
    }

    function closeProjectModal() {
        setProjectModal({ mode: null });
    }

    async function deleteProject(p: Project) {
        if (!confirm(`确认删除项目 "${p.name}" (id=${p.id})? 此操作不可恢复!`)) {
            return;
        }
        try {
            await DeleteProject(p.id);
            // 删除后如果当前选中此项目, 清除选择
            if (selectedProject?.id === p.id) {
                setSelectedProject(null);
                setChapters([]);
            }
            await refreshProjects();
        } catch (e: any) {
            setError(`删除项目失败: ${e?.message ?? e}`);
        }
    }

    // 选项目 + 加载章节
    async function selectProject(p: Project) {
        setSelectedProject(p);
        try {
            const list = await ListChapters(p.id);
            setChapters(list ?? []);
        } catch (e: any) {
            setError(`获取章节失败: ${e?.message ?? e}`);
        }
    }

    // ================== 渲染 ==================
    if (!loggedIn) {
        return (
            <div id="App">
                <div className="header">
                    <h1><span className="logo-accent">Novel</span>2ALL</h1>
                    <p className="subtitle">本地优先 + 云端同步的小说创作工具</p>
                </div>

                <div className="login-container">
                    <div className="login-box">
                        <h2><span className="logo-accent">Novel</span>2ALL</h2>
                        <p className="subtitle">登录以继续</p>
                        <p className="hint">Phase 1 mock: 任何用户名密码都接受</p>
                        <input
                            type="text"
                            placeholder="用户名"
                            value={username}
                            onChange={(e) => setUsername(e.target.value)}
                            autoComplete="username"
                            disabled={loading}
                        />
                        <input
                            type="password"
                            placeholder="密码"
                            value={password}
                            onChange={(e) => setPassword(e.target.value)}
                            autoComplete="current-password"
                            disabled={loading}
                            onKeyDown={(e) => e.key === 'Enter' && doLogin()}
                        />
                        <button className="btn primary" onClick={doLogin} disabled={loading}>
                            {loading ? '登录中...' : '登录'}
                        </button>
                        {error && <div className="error">{error}</div>}
                    </div>
                </div>

                <div className="footer">
                    <span>Phase 1: 账号体系 mock · Phase 2 接入真实 JWT auth</span>
                </div>
            </div>
        );
    }

    return (
        <div id="App">
            <div className="header">
                <h1><span className="logo-accent">Novel</span>2ALL</h1>
                <div className="user-bar">
                    {user && (
                        <>
                            <span>👤 {user.username}</span>
                            <span className="role">{user.role}</span>
                        </>
                    )}
                    <button className="btn" onClick={() => setShowSettings(true)}>⚙ 设置</button>
                    <button className="btn" onClick={doLogout}>登出</button>
                </div>
            </div>

            {showSettings && (
                <SettingsPage
                    onClose={() => setShowSettings(false)}
                    backendURL={backendURL}
                />
            )}

            <div className="status-bar">
                <span>后端:</span>
                <code>{backendURL}</code>
                <span className={`health ${healthMsg.includes('OK') ? 'ok' : ''}`}>{healthMsg}</span>
                <button className="btn small" onClick={refreshProjects} title="刷新项目列表">🔄 刷新</button>
            </div>

            <div className="main">
                {/* 左: 项目列表 (Stripe-style compact rows) */}
                <div className="sidebar">
                    <div className="sidebar-header">
                        <h3>项目 <span className="count">({projects.length})</span></h3>
                        <div className="sidebar-actions">
                            <button
                                className="btn-add"
                                onClick={() => openProjectModal('create')}
                                title="新建项目"
                            >
                                +
                            </button>
                        </div>
                    </div>
                    {projects.length === 0 ? (
                        <div className="empty-state">
                            <div className="empty-state-icon">📚</div>
                            <div className="empty-state-title">暂无项目</div>
                            <div className="empty-state-desc">点击右上角 + 创建第一个项目</div>
                        </div>
                    ) : (
                        <ul className="project-list">
                            {projects.map((p) => (
                                <li
                                    key={p.id}
                                    className={`project-item ${selectedProject?.id === p.id ? 'selected' : ''}`}
                                    onClick={() => selectProject(p)}
                                >
                                    <div className="project-info">
                                        <div className="project-name">{p.name}</div>
                                        <div className="project-meta">
                                            <span className="project-genre-tag">{p.genre || '未分类'}</span>
                                            {' '}#{p.id}
                                        </div>
                                    </div>
                                    <div className="project-actions">
                                        <button
                                            className="project-action-btn"
                                            onClick={(e) => { e.stopPropagation(); openProjectModal('edit', p); }}
                                            title="编辑项目"
                                        >✎</button>
                                        <button
                                            className="project-action-btn danger"
                                            onClick={(e) => { e.stopPropagation(); deleteProject(p); }}
                                            title="删除项目"
                                        >🗑</button>
                                    </div>
                                </li>
                            ))}
                        </ul>
                    )}
                </div>

                {/* 右: 章节列表 */}
                <div className="content">
                    {selectedProject ? (
                        <>
                            <h3>📖 {selectedProject.name} — 章节 ({chapters.length})</h3>
                            {chapters.length === 0 ? (
                                <p className="empty">该项目暂无章节</p>
                            ) : (
                                <table>
                                    <thead>
                                        <tr>
                                            <th>#</th>
                                            <th>文件名</th>
                                            <th>字数</th>
                                            <th>更新时间</th>
                                        </tr>
                                    </thead>
                                    <tbody>
                                        {chapters.map((c) => (
                                            <tr key={c.id}>
                                                <td>{c.number}</td>
                                                <td>{c.filename}</td>
                                                <td>{c.char_count || '-'}</td>
                                                <td>{c.updated_at || '-'}</td>
                                            </tr>
                                        ))}
                                    </tbody>
                                </table>
                            )}
                        </>
                    ) : (
                        <p className="placeholder">← 选择左侧项目查看章节</p>
                    )}
                </div>
            </div>

            {projectModal.mode && (
                <ProjectModal
                    mode={projectModal.mode}
                    project={projectModal.project}
                    onClose={closeProjectModal}
                    onSaved={async () => {
                        closeProjectModal();
                        await refreshProjects();
                    }}
                />
            )}

            <div className="footer">
                <span>Novel2ALL Desktop · Phase 1 · Backend: {backendURL}</span>
            </div>
        </div>
    );
}

// SettingsPage 设置页: 显示版本 + 检查更新 + 下载/应用更新.
//
// Phase 3 自动更新: 用户手动点 "检查更新" → 后端 API 查 GitHub Releases →
// 弹通知 + 下载按钮 → 应用 (启动 NSIS installer, 当前 app 退出).
function SettingsPage(props: { onClose: () => void; backendURL: string }) {
    const [currentVer, setCurrentVer] = useState<string>('加载中...');
    const [checking, setChecking] = useState(false);
    const [updateInfo, setUpdateInfo] = useState<any>(null);
    const [downloading, setDownloading] = useState(false);
    const [downloadProgress, setDownloadProgress] = useState<number>(0);
    const [error, setError] = useState<string>('');
    const [downloadedPath, setDownloadedPath] = useState<string>('');

    useEffect(() => {
        (async () => {
            try {
                const v = await CurrentVersion();
                setCurrentVer(v);
            } catch {
                setCurrentVer('未知');
            }
        })();
    }, []);

    async function doCheckUpdate() {
        setChecking(true);
        setError('');
        setUpdateInfo(null);
        try {
            const info: any = await CheckForUpdate();
            setUpdateInfo(info);
        } catch (e: any) {
            setError(`检查更新失败: ${e?.message ?? e}`);
        } finally {
            setChecking(false);
        }
    }

    async function doDownload() {
        if (!updateInfo?.Available) return;
        setDownloading(true);
        setError('');
        setDownloadProgress(0);
        try {
            // 监听 progress 事件 (Phase 3.1 简化: 轮询不可行, 用 event listener)
            const path: any = await DownloadUpdate();
            setDownloadedPath(String(path));
            setDownloadProgress(100);
        } catch (e: any) {
            setError(`下载失败: ${e?.message ?? e}`);
        } finally {
            setDownloading(false);
        }
    }

    async function doApply() {
        if (!downloadedPath) return;
        try {
            await ApplyUpdate(downloadedPath);
            // ApplyUpdate 成功后 app 退出, 不会到这里
        } catch (e: any) {
            setError(`应用失败: ${e?.message ?? e}`);
        }
    }

    return (
        <div className="modal-bg" onClick={props.onClose}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                    <h2>设置</h2>
                    <button className="btn" onClick={props.onClose}>✕</button>
                </div>

                <section className="settings-section">
                    <h3>版本</h3>
                    <p>当前: <strong>{currentVer}</strong></p>
                    <p>后端: <code>{props.backendURL}</code></p>
                </section>

                <section className="settings-section">
                    <h3>更新</h3>
                    <button className="btn" onClick={doCheckUpdate} disabled={checking}>
                        {checking ? '检查中...' : '🔄 检查更新'}
                    </button>

                    {updateInfo && (
                        <div className="update-info">
                            {updateInfo.Available ? (
                                <>
                                    <p className="ok">✅ 有新版本: <strong>{updateInfo.LatestVer}</strong></p>
                                    <p>当前: {updateInfo.CurrentVer}</p>
                                    <p>大小: {Math.round((updateInfo.DownloadSize ?? 0) / 1024 / 1024 * 100) / 100} MB</p>
                                    {updateInfo.Notes && (
                                        <details>
                                            <summary>更新日志</summary>
                                            <pre>{updateInfo.Notes}</pre>
                                        </details>
                                    )}
                                    {!downloadedPath ? (
                                        <button className="btn primary" onClick={doDownload} disabled={downloading}>
                                            {downloading ? '下载中...' : '⬇️ 下载更新'}
                                        </button>
                                    ) : (
                                        <button className="btn primary" onClick={doApply}>
                                            🚀 安装并重启
                                        </button>
                                    )}
                                </>
                            ) : (
                                <p className="info">已是最新版本 ({updateInfo.LatestVer ?? currentVer})</p>
                            )}
                        </div>
                    )}
                </section>

                {error && <div className="error">{error}</div>}
            </div>
        </div>
    );
}

// ProjectModal 项目创建/编辑模态 (Phase F: Project CRUD).
//
// 调用: CreateProject (mode='create') 或 UpdateProject (mode='edit').
// onSaved 回调触发 refreshProjects + 关闭 modal.
function ProjectModal(props: {
    mode: 'create' | 'edit';
    project?: Project;
    onClose: () => void;
    onSaved: () => void;
}) {
    const [name, setName] = useState(props.project?.name ?? '');
    const [description, setDescription] = useState(props.project?.description ?? '');
    const [genre, setGenre] = useState(props.project?.genre ?? '');
    const [saving, setSaving] = useState(false);
    const [err, setErr] = useState('');

    async function save() {
        if (!name.trim()) {
            setErr('项目名称不能为空');
            return;
        }
        setSaving(true);
        setErr('');
        try {
            const input = {
                name: name.trim(),
                slug: name.trim().toLowerCase().replace(/\s+/g, '-'),
                description: description.trim(),
                genre: genre.trim(),
            };
            if (props.mode === 'create') {
                await CreateProject(input);
            } else if (props.project) {
                await UpdateProject(props.project.id, input);
            }
            props.onSaved();
        } catch (e: any) {
            setErr(`保存失败: ${e?.message ?? e}`);
        } finally {
            setSaving(false);
        }
    }

    return (
        <div className="modal-bg" onClick={props.onClose}>
            <div className="modal" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                    <h2>{props.mode === 'create' ? '新建项目' : '编辑项目'}</h2>
                    <button className="modal-close" onClick={props.onClose} aria-label="关闭">✕</button>
                </div>

                <div className="modal-body">
                    <label className="form-label">
                        项目名称 *
                        <input
                            type="text"
                            value={name}
                            onChange={(e) => setName(e.target.value)}
                            placeholder="我的小说"
                            autoFocus
                        />
                    </label>
                    <label className="form-label">
                        简介
                        <textarea
                            value={description}
                            onChange={(e) => setDescription(e.target.value)}
                            placeholder="一句话描述你的小说..."
                            rows={3}
                        />
                    </label>
                    <label className="form-label">
                        类型
                        <input
                            type="text"
                            value={genre}
                            onChange={(e) => setGenre(e.target.value)}
                            placeholder="玄幻 / 都市 / 科幻 / ..."
                        />
                    </label>

                    {err && <div className="error">{err}</div>}
                </div>

                <div className="modal-footer">
                    <button className="btn" onClick={props.onClose}>取消</button>
                    <button className="btn primary" onClick={save} disabled={saving}>
                        {saving ? '保存中...' : '保存'}
                    </button>
                </div>
            </div>
        </div>
    );
}

export default App;