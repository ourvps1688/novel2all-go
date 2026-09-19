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
                    <h1>Novel2ALL</h1>
                    <p className="subtitle">本地优先 + 云端同步的小说创作工具</p>
                </div>

                <div className="status-bar">
                    <span>后端:</span>
                    <code>{backendURL || '...'}</code>
                    <span className="health">{healthMsg}</span>
                </div>

                <div className="login-box">
                    <h2>登录</h2>
                    <p className="hint">Phase 1 简化版: 任何用户名密码都接受 (mock)</p>
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

                <div className="footer">
                    <span>Phase 1: 账号体系 mock · Phase 2 接入真实 JWT auth</span>
                </div>
            </div>
        );
    }

    return (
        <div id="App">
            <div className="header">
                <h1>Novel2ALL</h1>
                <div className="user-bar">
                    <span>👤 {user?.username ?? '?'} ({user?.role ?? '?'})</span>
                    <button className="btn" onClick={() => setShowSettings(true)}>⚙️ 设置</button>
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
                <span className="health">{healthMsg}</span>
                <button className="btn small" onClick={refreshProjects}>🔄 刷新项目</button>
            </div>

            <div className="main">
                {/* 左: 项目列表 */}
                <div className="sidebar">
                    <h3>📚 项目 ({projects.length})</h3>
                    {projects.length === 0 ? (
                        <p className="empty">暂无项目</p>
                    ) : (
                        <ul>
                            {projects.map((p) => (
                                <li
                                    key={p.id}
                                    className={selectedProject?.id === p.id ? 'selected' : ''}
                                    onClick={() => selectProject(p)}
                                >
                                    <strong>{p.name}</strong>
                                    <small>{p.genre || '未分类'} · #{p.id}</small>
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

export default App;