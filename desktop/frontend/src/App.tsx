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
                    <button className="btn" onClick={doLogout}>登出</button>
                </div>
            </div>

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

export default App;