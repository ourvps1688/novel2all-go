import { useState, useEffect, useRef, useMemo } from 'react';
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
    CreateChapter,
    GetChapterContent,
    UpdateChapter,
    DeleteChapter,
    // Module B: LLM API key 配置
    SupportedProviders,
    GetLLMKeys,
    SetLLMKey,
    HasLLMKey,
    ClearLLMKeys,
    // Module C: 章节 action (LLM 调用)
    ExpandChapter,
    RewriteChapter,
    ReviewChapter,
    InsertChapter,
    RollbackChapter,
    // Module D: 知识管理 (人物/关系/伏笔)
    ListCharacters,
    GetCharacter,
    CreateCharacter,
    UpdateCharacter,
    DeleteCharacter,
    ListRelationships,
    GetRelationship,
    CreateRelationship,
    UpdateRelationship,
    DeleteRelationship,
    ListForeshadows,
    GetForeshadow,
    CreateForeshadow,
    UpdateForeshadow,
    DeleteForeshadow,
    // Module E: 章节大纲
    ListOutlines,
    GetOutline,
    CreateOutline,
    UpdateOutline,
    DeleteOutline,
    // Module H: Skills 调用
    ListSkills,
    ExecuteSkillSync,
    GetSkill,
    // Module J: Settings 持久化 + 自动启动
    GetSettings,
    UpdateSettings,
    GetAutoStart,
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

    // Module D: 知识管理面板可见性
    const [showKnowledge, setShowKnowledge] = useState(false);

    // Module E: 章节大纲面板可见性
    const [showOutline, setShowOutline] = useState(false);

    // Module H: Skills 面板可见性
    const [showSkills, setShowSkills] = useState(false);

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

    // Module A: Chapter CRUD state
    const [chapterModal, setChapterModal] = useState<{
        mode: 'create' | 'edit' | null;
        chapter?: Chapter;
        projectID?: number;
    }>({ mode: null });
    const [chapterContent, setChapterContent] = useState<string>('');
    const [loadingChapter, setLoadingChapter] = useState(false);

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
        await refreshChapters(p.id);
    }

    // ============= Chapter CRUD (Module A) =============

    async function refreshChapters(projectID: number) {
        try {
            const list = await ListChapters(projectID);
            setChapters(list ?? []);
        } catch (e: any) {
            setError(`获取章节失败: ${e?.message ?? e}`);
        }
    }

    function openChapterModal(mode: 'create' | 'edit', chapter?: Chapter) {
        setChapterModal({ mode, chapter, projectID: selectedProject?.id });
        setError('');
    }
    function closeChapterModal() {
        setChapterModal({ mode: null });
        setChapterContent('');
    }

    async function openChapterEditor(c: Chapter) {
        if (!selectedProject) return;
        setLoadingChapter(true);
        setError('');
        try {
            const content = await GetChapterContent(selectedProject.id, c.chapter);
            setChapterContent(content?.content ?? '');
            openChapterModal('edit', c);
        } catch (e: any) {
            setError(`读取章节失败: ${e?.message ?? e}`);
        } finally {
            setLoadingChapter(false);
        }
    }

    async function deleteChapter(c: Chapter) {
        if (!selectedProject) return;
        if (!confirm(`确认删除章节 "${c.title || '第' + c.chapter + '章'}" (id=${c.id})? 此操作不可恢复!`)) return;
        try {
            await DeleteChapter(selectedProject.id, c.chapter);
            await refreshChapters(selectedProject.id);
        } catch (e: any) {
            setError(`删除章节失败: ${e?.message ?? e}`);
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
                    {selectedProject && (
                        <button className="btn" onClick={() => setShowKnowledge(true)}>📚 知识管理</button>
                    )}
                    {selectedProject && (
                        <button className="btn" onClick={() => setShowOutline(true)}>📝 章节大纲</button>
                    )}
                    <button className="btn" onClick={() => setShowSkills(true)}>⚡ Skills</button>
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

            {showKnowledge && selectedProject && (
                <KnowledgePanel
                    projectID={selectedProject.id}
                    onClose={() => setShowKnowledge(false)}
                />
            )}

            {showOutline && selectedProject && (
                <OutlinePanel
                    projectID={selectedProject.id}
                    onClose={() => setShowOutline(false)}
                />
            )}

            {showSkills && (
                <SkillsPanel
                    onClose={() => setShowSkills(false)}
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
                            <h3>📖 {selectedProject.name}</h3>
                            <div className="chapters-section">
                                <div className="chapters-header">
                                    <h3>章节 <span className="count">({chapters.length})</span></h3>
                                    <button
                                        className="btn-add"
                                        onClick={() => openChapterModal('create')}
                                        title="新建章节"
                                    >+</button>
                                </div>
                                {chapters.length === 0 ? (
                                    <div className="empty-state">
                                        <div className="empty-state-icon">📄</div>
                                        <div className="empty-state-title">暂无章节</div>
                                        <div className="empty-state-desc">点击右上角 + 创建第一个章节</div>
                                    </div>
                                ) : (
                                    <ul className="chapter-list">
                                        {chapters.map((c) => (
<li key={c.id} className="chapter-item">
                                                    <div className="chapter-info">
                                                        <div className="chapter-title">
                                                            {c.title || `第${c.chapter}章`}
                                                        </div>
                                                        <div className="chapter-meta">
                                                            <span className="chapter-num-tag">第{c.chapter}章</span>
                                                            {' '}{c.char_count ?? 0} 字
                                                        </div>
                                                    </div>
                                                <div className="chapter-actions">
                                                    <button
                                                        className="chapter-action-btn"
                                                        onClick={(e) => { e.stopPropagation(); openChapterEditor(c); }}
                                                        title="编辑章节"
                                                        disabled={loadingChapter}
                                                    >✎</button>
                                                    <button
                                                        className="chapter-action-btn danger"
                                                        onClick={(e) => { e.stopPropagation(); deleteChapter(c); }}
                                                        title="删除章节"
                                                    >🗑</button>
                                                </div>
                                            </li>
                                        ))}
                                    </ul>
                                )}
                            </div>
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

            {chapterModal.mode && (
                <ChapterModal
                    mode={chapterModal.mode}
                    chapter={chapterModal.chapter}
                    projectID={chapterModal.projectID}
                    initialContent={chapterContent}
                    onClose={closeChapterModal}
                    onSaved={async () => {
                        closeChapterModal();
                        if (selectedProject) {
                            await refreshChapters(selectedProject.id);
                        }
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

    // Module B: LLM API key 配置状态
    const [llmProviders, setLlmProviders] = useState<string[]>([]);
    const [llmConfigured, setLlmConfigured] = useState<Set<string>>(new Set());
    const [llmKeys, setLlmKeys] = useState<Record<string, string>>({});
    const [llmSaving, setLlmSaving] = useState<Record<string, boolean>>({});
    const [llmError, setLlmError] = useState<string>('');
    const [llmSuccess, setLlmSuccess] = useState<string>('');

    // Module J: Settings 持久化 (开机自启 + 启动时最小化)
    const [settings, setSettings] = useState<{ auto_start: boolean; start_minimized: boolean }>({
        auto_start: false,
        start_minimized: false,
    });
    const [settingsLoading, setSettingsLoading] = useState(true);
    const [settingsSaving, setSettingsSaving] = useState(false);
    const [settingsError, setSettingsError] = useState<string>('');
    const [settingsSuccess, setSettingsSuccess] = useState<string>('');
    const [actualAutoStart, setActualAutoStart] = useState<boolean | null>(null); // null=未知

    useEffect(() => {
        (async () => {
            try {
                const s = await GetSettings();
                setSettings({ auto_start: !!s?.auto_start, start_minimized: !!s?.start_minimized });
                // 额外查询实际系统状态 (registry), 跟持久化设置可能不一致
                const actual = await GetAutoStart();
                setActualAutoStart(actual);
            } catch (e: any) {
                setSettingsError(`加载设置失败: ${e?.message ?? e}`);
            } finally {
                setSettingsLoading(false);
            }
        })();
    }, []);

    async function saveSettings(newSettings: { auto_start: boolean; start_minimized: boolean }) {
        setSettingsSaving(true);
        setSettingsError('');
        setSettingsSuccess('');
        try {
            await UpdateSettings(newSettings);
            setSettings(newSettings);
            setSettingsSuccess('已保存');
            // 刷新实际状态 (registry 写后)
            try {
                const actual = await GetAutoStart();
                setActualAutoStart(actual);
            } catch {
                // ignore
            }
        } catch (e: any) {
            setSettingsError(`保存失败: ${e?.message ?? e}`);
        } finally {
            setSettingsSaving(false);
        }
    }

    useEffect(() => {
        (async () => {
            try {
                const v = await CurrentVersion();
                setCurrentVer(v);
            } catch {
                setCurrentVer('未知');
            }
        })();
        // Module B: 加载已配置的 LLM providers
        (async () => {
            try {
                const providers = await SupportedProviders();
                setLlmProviders(providers);
                const configured = await GetLLMKeys();
                setLlmConfigured(new Set(configured ?? []));
            } catch (e: any) {
                setLlmError(`加载 LLM keys 失败: ${e?.message ?? e}`);
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

    // Module B: LLM key handlers
    async function doSaveLLMKey(provider: string) {
        const key = llmKeys[provider]?.trim() ?? '';
        if (!key) {
            setLlmError(`${provider}: key 不能为空`);
            return;
        }
        setLlmError('');
        setLlmSuccess('');
        setLlmSaving(prev => ({ ...prev, [provider]: true }));
        try {
            await SetLLMKey(provider, key);
            // 清空输入框 + 标记为已配置
            setLlmKeys(prev => {
                const next = { ...prev };
                delete next[provider];
                return next;
            });
            setLlmConfigured(prev => new Set(prev).add(provider));
            setLlmSuccess(`${provider} 已保存`);
        } catch (e: any) {
            setLlmError(`${provider} 保存失败: ${e?.message ?? e}`);
        } finally {
            setLlmSaving(prev => ({ ...prev, [provider]: false }));
        }
    }

    async function doDeleteLLMKey(provider: string) {
        if (!confirm(`确认删除 ${provider} 的 API key? 此操作不可恢复!`)) return;
        setLlmError('');
        setLlmSuccess('');
        try {
            // SetLLMKey(provider, "") 在后端 = 删除该 provider
            await SetLLMKey(provider, '');
            setLlmConfigured(prev => {
                const next = new Set(prev);
                next.delete(provider);
                return next;
            });
            setLlmSuccess(`${provider} 已删除`);
        } catch (e: any) {
            setLlmError(`${provider} 删除失败: ${e?.message ?? e}`);
        }
    }

    async function doClearLLMKeys() {
        if (!confirm('确认清除所有 LLM API keys? 后端 admin key 仍可用, 你的自定义 key 将全部删除.')) return;
        setLlmError('');
        setLlmSuccess('');
        try {
            await ClearLLMKeys();
            setLlmConfigured(new Set());
            setLlmKeys({});
            setLlmSuccess('已清除所有 LLM keys');
        } catch (e: any) {
            setLlmError(`清除失败: ${e?.message ?? e}`);
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
                    <h3>LLM API Keys</h3>
                    <p className="settings-help">
                        每个 provider 的 API key 单独加密存储在本机 (AES-256-GCM + PBKDF2, 机器绑定).
                        留空 = 保留已配置. 后端 admin key 仍作为 fallback.
                    </p>
                    {llmProviders.length === 0 ? (
                        <p className="info">加载中...</p>
                    ) : (
                        <>
                            <div className="llm-key-list">
                                {llmProviders.map(p => (
                                    <div className="llm-key-row" key={p}>
                                        <div className="llm-key-label">
                                            <span className="llm-key-name">{p}</span>
                                            {llmConfigured.has(p) && (
                                                <span className="llm-key-status">已配置 ✓</span>
                                            )}
                                        </div>
                                        <div className="llm-key-input-group">
                                            <input
                                                type="password"
                                                className="llm-key-input"
                                                placeholder={llmConfigured.has(p) ? '(已配置, 输入新 key 覆盖)' : '粘贴 API key'}
                                                value={llmKeys[p] ?? ''}
                                                onChange={(e) => setLlmKeys(prev => ({ ...prev, [p]: e.target.value }))}
                                                autoComplete="off"
                                                spellCheck={false}
                                            />
                                            <button
                                                className="btn primary small"
                                                onClick={() => doSaveLLMKey(p)}
                                                disabled={!llmKeys[p]?.trim() || llmSaving[p]}
                                            >
                                                {llmSaving[p] ? '保存中...' : '保存'}
                                            </button>
                                            <button
                                                className="btn danger small"
                                                onClick={() => doDeleteLLMKey(p)}
                                                disabled={!llmConfigured.has(p)}
                                                title={llmConfigured.has(p) ? '删除此 provider 的 key' : '未配置, 无需删除'}
                                            >
                                                删除
                                            </button>
                                        </div>
                                    </div>
                                ))}
                            </div>
                            {llmConfigured.size > 0 && (
                                <button
                                    className="btn danger small"
                                    onClick={doClearLLMKeys}
                                    style={{ marginTop: '8px' }}
                                >
                                    清除全部
                                </button>
                            )}
                        </>
                    )}
                    {llmError && <div className="error" style={{ marginTop: '8px' }}>{llmError}</div>}
                    {llmSuccess && <div className="info ok" style={{ marginTop: '8px' }}>{llmSuccess}</div>}
                </section>

                <section className="settings-section">
                    <h3>桌面设置</h3>
                    {settingsLoading ? (
                        <div className="info">加载设置中...</div>
                    ) : (
                        <>
                            <label className="form-label setting-toggle-row">
                                <input
                                    type="checkbox"
                                    checked={settings.auto_start}
                                    disabled={settingsSaving}
                                    onChange={(e) => setSettings({ ...settings, auto_start: e.target.checked })}
                                />
                                <span>开机自启 (Windows Registry HKCU\...\Run)</span>
                            </label>
                            {actualAutoStart !== null && actualAutoStart !== settings.auto_start && (
                                <div className="info" style={{ fontSize: '11px', marginLeft: '24px' }}>
                                    系统当前状态: {actualAutoStart ? '已启用' : '未启用'} (与设置不同步, 可能被外部修改)
                                </div>
                            )}

                            <label className="form-label setting-toggle-row">
                                <input
                                    type="checkbox"
                                    checked={settings.start_minimized}
                                    disabled={settingsSaving}
                                    onChange={(e) => setSettings({ ...settings, start_minimized: e.target.checked })}
                                />
                                <span>启动时最小化到托盘 (不弹主窗口)</span>
                            </label>

                            <div className="settings-actions">
                                <button
                                    className="btn primary small"
                                    onClick={() => saveSettings(settings)}
                                    disabled={settingsSaving}
                                >
                                    {settingsSaving ? '保存中...' : '保存设置'}
                                </button>
                            </div>

                            {settingsError && <div className="error" style={{ marginTop: '8px' }}>{settingsError}</div>}
                            {settingsSuccess && <div className="info ok" style={{ marginTop: '8px' }}>{settingsSuccess}</div>}
                            <div className="info" style={{ fontSize: '11px', marginTop: '12px' }}>
                                提示: 开机自启下次启动时生效. 启动时最小化需要重启 app.
                            </div>
                        </>
                    )}
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

// ChapterModal 章节创建/编辑模态 (Module A: Chapter CRUD).
//
// 调用: CreateChapter (mode='create') 或 UpdateChapter (mode='edit').
// onSaved 回调触发 refreshChapters + 关闭 modal.
//
// Module C.1 (2026-09-20): Markdown 编辑器升级
// - 工具栏: B/I/H1/H2/列表/引用/代码/链接 (插入 Markdown 语法)
// - 实时预览: 右侧 marked 渲染 (左右分栏)
// - 字数统计: 中文字符 + 总字符数 + 行数
// - 存储仍是 .md, 零格式转换, LLM 调用照常
import { marked } from 'marked';

// 单条 toolbar 按钮定义: type 决定 wrap vs prefix 行为
type ToolbarAction =
    | { kind: 'wrap'; prefix: string; suffix: string; placeholder: string; label: string; title: string }
    | { kind: 'prefix'; prefix: string; placeholder: string; label: string; title: string }
    | { kind: 'link'; label: string; title: string };

const TOOLBAR: ToolbarAction[] = [
    { kind: 'wrap', prefix: '**', suffix: '**', placeholder: '粗体', label: 'B', title: '粗体 (Ctrl+B)' },
    { kind: 'wrap', prefix: '*', suffix: '*', placeholder: '斜体', label: 'I', title: '斜体 (Ctrl+I)' },
    { kind: 'prefix', prefix: '# ', placeholder: '标题', label: 'H1', title: '一级标题' },
    { kind: 'prefix', prefix: '## ', placeholder: '子标题', label: 'H2', title: '二级标题' },
    { kind: 'prefix', prefix: '- ', placeholder: '列表项', label: '·列表', title: '无序列表' },
    { kind: 'prefix', prefix: '> ', placeholder: '引用', label: '"', title: '引用' },
    { kind: 'wrap', prefix: '`', suffix: '`', placeholder: 'code', label: '</>', title: '行内代码' },
    { kind: 'link', label: '🔗', title: '插入链接' },
];

// Module C.2: AI 辅助 section 按钮定义
type AIAction = 'expand' | 'rewrite' | 'review' | 'insert' | 'rollback';

const AI_BUTTONS: Array<{ action: AIAction; icon: string; label: string; destructive: boolean; needsPosition: boolean; needsInstruction: boolean }> = [
    { action: 'expand', icon: '✨', label: '扩写', destructive: false, needsPosition: false, needsInstruction: true },
    { action: 'rewrite', icon: '🔄', label: '重写', destructive: true, needsPosition: false, needsInstruction: true },
    { action: 'review', icon: '📋', label: 'Review', destructive: false, needsPosition: false, needsInstruction: false },
    { action: 'insert', icon: '➕', label: '插入', destructive: true, needsPosition: true, needsInstruction: true },
    { action: 'rollback', icon: '⏪', label: '回滚', destructive: true, needsPosition: false, needsInstruction: false },
];

function ChapterModal(props: {
    mode: 'create' | 'edit';
    chapter?: Chapter;
    projectID?: number;
    initialContent?: string;
    onClose: () => void;
    onSaved: () => void;
}) {
    const [number, setNumber] = useState(props.chapter?.chapter ?? 1);
    const [title, setTitle] = useState(props.chapter?.title ?? '');
    const [content, setContent] = useState(props.initialContent ?? '');
    const [saving, setSaving] = useState(false);
    const [err, setErr] = useState('');

    // Module C: AI 辅助状态
    const [aiInstruction, setAiInstruction] = useState('');
    const [aiPosition, setAiPosition] = useState(1);
    const [aiBusy, setAiBusy] = useState<AIAction | null>(null); // 哪个 action 在跑
    const [aiError, setAiError] = useState(''); // AI 操作错误 (独立于表单 err)
    const [aiSuccess, setAiSuccess] = useState(''); // 操作成功摘要
    const [aiElapsedSec, setAiElapsedSec] = useState(0); // AI 操作已等待秒数
    const [reviewResult, setReviewResult] = useState<main.ReviewResult | null>(null); // Review modal 数据

    // Module C.5: elapsed timer — aiBusy 时每秒递增, 结束后重置
    useEffect(() => {
        if (aiBusy === null) {
            setAiElapsedSec(0);
            return;
        }
        const startTime = Date.now();
        setAiElapsedSec(0);
        const id = setInterval(() => {
            setAiElapsedSec(Math.floor((Date.now() - startTime) / 1000));
        }, 1000);
        return () => clearInterval(id);
    }, [aiBusy]);

    // Module C.1: 编辑器增强
    const textareaRef = useRef<HTMLTextAreaElement>(null);
    const previewHtml = useMemo(() => {
        // marked.parse 在 marked v18 返 string (默认 sync)
        // 用 try/catch 防止用户输入一半时的边缘 case
        try {
            return marked.parse(content || '', { async: false, breaks: true, gfm: true });
        } catch {
            return '<p style="color:#999">预览渲染失败</p>';
        }
    }, [content]);

    // 字数统计: 总字符 + 中文字符 + 行数
    const stats = useMemo(() => {
        const total = content.length;
        const chinese = (content.match(/[\u4e00-\u9fff]/g) || []).length;
        const lines = content.split('\n').length;
        return { total, chinese, lines };
    }, [content]);

    // 工具栏点击: 插入 Markdown 语法
    function applyToolbar(action: ToolbarAction) {
        const ta = textareaRef.current;
        if (!ta) return;
        const start = ta.selectionStart;
        const end = ta.selectionEnd;
        const selected = content.substring(start, end);

        let newText: string;
        let cursorStart: number;
        let cursorEnd: number;

        if (action.kind === 'wrap') {
            const inner = selected || action.placeholder;
            newText = action.prefix + inner + action.suffix;
            // 光标: 选中时放在选区末尾 (wrapped text 后), 没选中时在 placeholder 中间
            cursorStart = start + action.prefix.length;
            cursorEnd = cursorStart + inner.length;
        } else if (action.kind === 'prefix') {
            // 行首插入 prefix; 找到当前行起始位置
            const lineStart = content.lastIndexOf('\n', start - 1) + 1;
            newText = action.prefix + content.substring(lineStart);
            cursorStart = start + action.prefix.length;
            cursorEnd = cursorStart + (selected ? selected.length : 0);
        } else { // link
            const linkText = selected || 'link text';
            const insertion = `[${linkText}](url)`;
            newText = content.substring(0, start) + insertion + content.substring(end);
            // 光标定位到 url 处
            cursorStart = start + 1 + linkText.length + 2; // "[" + text + "]("
            cursorEnd = cursorStart + 3; // "url"
        }

        const nextContent = content.substring(0, start) + newText + content.substring(end);
        setContent(nextContent);

        // 恢复光标 + focus (下一帧 React 渲染完)
        requestAnimationFrame(() => {
            ta.focus();
            ta.setSelectionRange(cursorStart, cursorEnd);
        });
    }

    async function save() {
        if (!props.projectID) {
            setErr('未选中项目');
            return;
        }
        if (number <= 0) {
            setErr('章节号必须 > 0');
            return;
        }
        if (!title.trim() && !content.trim()) {
            setErr('章节标题或内容至少填一个');
            return;
        }
        setSaving(true);
        setErr('');
        try {
            const input = {
                project_id: props.projectID,
                chapter: number,
                title: title.trim(),
                content: content,
            };
            if (props.mode === 'create') {
                await CreateChapter(input);
            } else if (props.chapter) {
                await UpdateChapter(input);
            }
            props.onSaved();
        } catch (e: any) {
            setErr(`保存失败: ${e?.message ?? e}`);
        } finally {
            setSaving(false);
        }
    }

    // Module C.2: AI 辅助 handlers
    //
    // 通用 AI 操作执行入口 (5 个 action 走同一逻辑):
    // 1. validate (project_id / chapter / position for insert)
    // 2. confirm for destructive actions (rewrite/insert/rollback)
    // 3. call wails method
    // 4. 处理响应:
    //    - expand/rewrite/insert/rollback: 重新拉取章节内容 (服务器改了文件)
    //    - review: 显示独立 modal
    // 5. 显示结果摘要 + 触发 onSaved 刷新列表
    async function refreshChapterContent() {
        if (!props.projectID || !props.chapter) return;
        try {
            const content = await GetChapterContent(props.projectID, props.chapter.chapter);
            if (content?.content !== undefined) {
                setContent(content.content);
            }
        } catch (e: any) {
            setAiError(`刷新章节失败: ${e?.message ?? e}`);
        }
    }

    async function doAI(action: AIAction) {
        if (!props.projectID || !props.chapter) {
            setAiError('请先选中项目并打开已有章节');
            return;
        }

        const buttonDef = AI_BUTTONS.find(b => b.action === action)!;

        // validate
        const instruction = aiInstruction.trim();
        if (buttonDef.needsInstruction && !instruction) {
            setAiError(`${buttonDef.label} 需要指令 (instruction)`);
            return;
        }
        if (buttonDef.needsPosition && aiPosition <= 0) {
            setAiError(`插入位置必须 > 0 (1-based 行号)`);
            return;
        }

        // confirm for destructive
        if (buttonDef.destructive) {
            const actionLabels: Record<AIAction, string> = {
                expand: '扩写 (追加内容, 不覆盖)',
                rewrite: '重写 (覆盖整章)',
                review: 'Review (评估, 不修改)',
                insert: '插入 (在指定行插入新段落)',
                rollback: '回滚 (从最新备份恢复)',
            };
            const msg = `${actionLabels[action]}\n\n当前章节 ${number} 字数: ${stats.total} 字\n\n将调用 LLM (可能 10-60 秒).\n确认执行?`;
            if (!confirm(msg)) return;
        }

        setAiBusy(action);
        setAiError('');
        setAiSuccess('');

        try {
            let resp: any;
            let label = buttonDef.label;
            switch (action) {
                case 'expand':
                    resp = await ExpandChapter(props.projectID, number, instruction);
                    label = '扩写';
                    break;
                case 'rewrite':
                    resp = await RewriteChapter(props.projectID, number, instruction);
                    label = '重写';
                    break;
                case 'review':
                    resp = await ReviewChapter(props.projectID, number);
                    label = 'Review';
                    break;
                case 'insert':
                    resp = await InsertChapter(props.projectID, number, aiPosition, instruction);
                    label = '插入';
                    break;
                case 'rollback':
                    resp = await RollbackChapter(props.projectID, number);
                    label = '回滚';
                    break;
            }

            // 处理响应
            if (action === 'review') {
                // review 返 ReviewResult, 显示独立 modal
                setReviewResult(resp as main.ReviewResult);
                setAiSuccess(`Review 完成 (${resp.elapsed_ms}ms)`);
            } else {
                // 其他 4 个修改文件, 重新拉取内容 + 通知父组件刷新列表
                const ar = resp as main.ActionResponse;
                await refreshChapterContent();
                const summary = `${label} 完成: ${ar.chars_before}→${ar.chars_after} 字 (${ar.elapsed_ms}ms)`;
                if (ar.issues && ar.issues.length > 0) {
                    setAiSuccess(`${summary} · ${ar.issues.length} 条 verifier 提醒`);
                } else {
                    setAiSuccess(summary);
                }
                // 触发章节列表刷新 (Module A refreshChapters)
                props.onSaved();
            }
            // 清空输入 (instruction 用过不保留)
            if (buttonDef.needsInstruction) setAiInstruction('');
        } catch (e: any) {
            setAiError(`${buttonDef.label} 失败: ${e?.message ?? e}`);
        } finally {
            setAiBusy(null);
        }
    }

    return (
        <div className="modal-bg" onClick={props.onClose}>
            <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                    <h2>{props.mode === 'create' ? '新建章节' : `编辑第${props.chapter?.chapter}章`}</h2>
                    <button className="modal-close" onClick={props.onClose} aria-label="关闭">✕</button>
                </div>

                <div className="modal-body">
                    <div className="form-row">
                        <label className="form-label form-label-narrow">
                            章节号 *
                            <input
                                type="number"
                                min="1"
                                value={number}
                                onChange={(e) => setNumber(parseInt(e.target.value) || 1)}
                                disabled={props.mode === 'edit'}
                                placeholder="1"
                            />
                        </label>
                        <label className="form-label form-label-grow">
                            标题
                            <input
                                type="text"
                                value={title}
                                onChange={(e) => setTitle(e.target.value)}
                                placeholder="本章标题（可选）"
                            />
                        </label>
                    </div>

                    <label className="form-label">
                        内容
                        <div className="md-editor-toolbar" aria-label="Markdown 工具栏">
                            {TOOLBAR.map((a, i) => (
                                <button
                                    key={i}
                                    type="button"
                                    className="md-toolbar-btn"
                                    onClick={() => applyToolbar(a)}
                                    title={a.title}
                                >
                                    {a.label}
                                </button>
                            ))}
                            <span className="md-toolbar-spacer" />
                            <span className="md-stats">
                                {stats.chinese} 中文字 · {stats.total} 字符 · {stats.lines} 行
                            </span>
                        </div>
                    </label>

                    <div className="md-editor-split">
                        <textarea
                            ref={textareaRef}
                            className="md-editor-textarea"
                            value={content}
                            onChange={(e) => setContent(e.target.value)}
                            placeholder="章节内容（Markdown）...&#10;&#10;支持语法: **粗体** *斜体* # 标题 - 列表 > 引用 `代码` [链接](url)&#10;右侧实时预览"
                            spellCheck={false}
                        />
                        <div
                            className="md-editor-preview"
                            dangerouslySetInnerHTML={{ __html: previewHtml }}
                        />
                    </div>

                    {/* Module C.2: AI 辅助 section */}
                    <div className="ai-section">
                        <div className="ai-section-header">
                            <span className="ai-section-title">🤖 AI 辅助</span>
                            <span className="ai-section-hint">调用 LLM 修改章节 (后端 admin key, Module B.2 后用本地 key)</span>
                        </div>

                        <div className="ai-instruction-row">
                            <input
                                type="text"
                                className="ai-instruction-input"
                                placeholder="指令 (扩写/重写/插入用, 如: '增加主角与师父的对决')"
                                value={aiInstruction}
                                onChange={(e) => setAiInstruction(e.target.value)}
                                disabled={aiBusy !== null || props.mode === 'create'}
                            />
                            <input
                                type="number"
                                className="ai-position-input"
                                min="1"
                                placeholder="位置 (insert)"
                                value={aiPosition}
                                onChange={(e) => setAiPosition(parseInt(e.target.value) || 1)}
                                disabled={aiBusy !== null || props.mode === 'create'}
                                title="插入位置 (1-based 行号, 仅 insert 用)"
                            />
                        </div>

                        <div className="ai-actions">
                            {AI_BUTTONS.map(b => (
                                <button
                                    key={b.action}
                                    className={`btn ${b.destructive ? 'danger' : 'primary'} ai-action-btn`}
                                    onClick={() => doAI(b.action)}
                                    disabled={aiBusy !== null || props.mode === 'create'}
                                    title={b.destructive ? `${b.label} (破坏性, 会弹确认)` : b.label}
                                >
                                    {aiBusy === b.action ? (
                                        <>⏳ {b.label}中... {aiElapsedSec}s</>
                                    ) : (
                                        <>{b.icon} {b.label}</>
                                    )}
                                </button>
                            ))}
                        </div>

                        {aiError && <div className="error" style={{ marginTop: '8px' }}>{aiError}</div>}
                        {aiSuccess && <div className="info ok" style={{ marginTop: '8px' }}>{aiSuccess}</div>}
                        {aiBusy !== null && aiElapsedSec >= 30 && (
                            <div className="ai-wait-hint">
                                ⏳ LLM 首次调用可能 1-2 分钟 (冷启动), 正在等待... ({aiElapsedSec}s)
                            </div>
                        )}
                    </div>

                    {err && <div className="error">{err}</div>}
                </div>

                <div className="modal-footer">
                    <button className="btn" onClick={props.onClose}>取消</button>
                    <button className="btn primary" onClick={save} disabled={saving || aiBusy !== null}>
                        {saving ? '保存中...' : '保存'}
                    </button>
                </div>
            </div>

            {/* Module C.2: Review 结果独立 modal (叠在 ChapterModal 上) */}
            {reviewResult && (
                <div className="modal-bg" onClick={() => setReviewResult(null)}>
                    <div className="modal review-modal" onClick={(e) => e.stopPropagation()}>
                        <div className="modal-header">
                            <h2>📋 Review 结果 (第{reviewResult.chapter}章)</h2>
                            <button className="modal-close" onClick={() => setReviewResult(null)}>✕</button>
                        </div>
                        <div className="modal-body">
                            <div className="review-summary">
                                <div className={`review-score review-score-${verdictClass(reviewResult.overall_verdict)}`}>
                                    <div className="review-score-num">{reviewResult.quality_score.toFixed(1)}</div>
                                    <div className="review-score-verdict">{verdictLabel(reviewResult.overall_verdict)}</div>
                                </div>
                                <div className="review-stats">
                                    <div>{reviewResult.content_chars} 字 · {reviewResult.elapsed_ms}ms</div>
                                    <div className="review-issues-count">
                                        🔴 {reviewResult.critical_issues.length} 严重 ·
                                        🟡 {reviewResult.major_issues.length} 重要 ·
                                        🟢 {reviewResult.minor_issues.length} 轻微
                                    </div>
                                </div>
                            </div>

                            {reviewResult.critical_issues.length > 0 && (
                                <div className="review-issues-section">
                                    <h4>🔴 严重问题</h4>
                                    {reviewResult.critical_issues.map((it, i) => (
                                        <div key={i} className="review-issue critical">
                                            {it.location && <span className="review-issue-loc">[{it.location}]</span>}
                                            {it.category && <span className="review-issue-cat">{it.category}</span>}
                                            <span className="review-issue-note">{it.note}</span>
                                        </div>
                                    ))}
                                </div>
                            )}

                            {reviewResult.major_issues.length > 0 && (
                                <div className="review-issues-section">
                                    <h4>🟡 重要问题</h4>
                                    {reviewResult.major_issues.map((it, i) => (
                                        <div key={i} className="review-issue major">
                                            {it.location && <span className="review-issue-loc">[{it.location}]</span>}
                                            {it.category && <span className="review-issue-cat">{it.category}</span>}
                                            <span className="review-issue-note">{it.note}</span>
                                        </div>
                                    ))}
                                </div>
                            )}

                            {reviewResult.minor_issues.length > 0 && (
                                <div className="review-issues-section">
                                    <h4>🟢 轻微问题</h4>
                                    {reviewResult.minor_issues.map((it, i) => (
                                        <div key={i} className="review-issue minor">
                                            {it.location && <span className="review-issue-loc">[{it.location}]</span>}
                                            {it.category && <span className="review-issue-cat">{it.category}</span>}
                                            <span className="review-issue-note">{it.note}</span>
                                        </div>
                                    ))}
                                </div>
                            )}

                            {reviewResult.critical_issues.length === 0 && reviewResult.major_issues.length === 0 && reviewResult.minor_issues.length === 0 && (
                                <div className="info ok">✅ Review 通过, 未发现严重问题</div>
                            )}
                        </div>
                        <div className="modal-footer">
                            <button className="btn primary" onClick={() => setReviewResult(null)}>关闭</button>
                        </div>
                    </div>
                </div>
            )}
        </div>
    );
}

// review verdict 显示辅助函数 (Module C.2)
function verdictClass(verdict: string): string {
    if (verdict === 'pass') return 'good';
    if (verdict === 'fail') return 'bad';
    return 'warn';
}

function verdictLabel(verdict: string): string {
    const map: Record<string, string> = {
        pass: '通过',
        fail: '不通过',
        needs_revision: '需修订',
    };
    return map[verdict] || verdict;
}

// ---------------------------------------------------------------------------
// Module E: OutlinePanel (章节大纲) — CRUD UI
// ---------------------------------------------------------------------------

// OutlinePanel 章节大纲列表 + 创建/编辑表单 + 删除.
// 与 KnowledgePanel 风格一致. props.projectID 必填.
function OutlinePanel(props: {
    projectID?: number;
    onClose: () => void;
}) {
    const [items, setItems] = useState<any[]>([]);
    const [loading, setLoading] = useState(false);
    const [err, setErr] = useState('');
    const [editingId, setEditingId] = useState<number | null>(null);
    const [form, setForm] = useState<Record<string, any>>({});

    async function refresh() {
        if (!props.projectID) return;
        setLoading(true);
        setErr('');
        try {
            const list = await ListOutlines(props.projectID);
            // 后端 ListOutlines 已按 chapter 排序 (桌面端冒泡排序), 这里不需再排
            setItems(list ?? []);
        } catch (e: any) {
            setErr(`加载失败: ${e?.message ?? e}`);
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        refresh();
        setEditingId(null);
        setForm({});
    }, [props.projectID]);

    function startCreate() {
        // 默认下一个未用章节号
        const usedChapters = items.map((it) => it.chapter);
        let nextChapter = 1;
        while (usedChapters.includes(nextChapter)) nextChapter++;
        setEditingId(null);
        setForm({
            chapter: nextChapter,
            title: '',
            summary: '',
            key_events: [],
            status: 'planned',
            notes: '',
            characters: [],
            foreshadows: [],
        });
    }

    function startEdit(item: any) {
        setEditingId(item.id);
        setForm({
            chapter: item.chapter,
            title: item.title ?? '',
            summary: item.summary ?? '',
            key_events: item.key_events ?? [],
            status: item.status ?? 'planned',
            notes: item.notes ?? '',
            characters: item.characters ?? [],
            foreshadows: item.foreshadows ?? [],
        });
    }

    function cancelEdit() {
        setEditingId(null);
        setForm({});
    }

    async function save() {
        if (!props.projectID) return;
        setErr('');
        if (!form.title?.toString().trim() || !form.chapter) {
            setErr('章节号和标题必填');
            return;
        }
        try {
            const input = {
                project_id: props.projectID,
                chapter: Number(form.chapter) || 0,
                title: form.title?.toString().trim() ?? '',
                summary: form.summary?.toString() ?? '',
                key_events: form.key_events ?? [],
                status: form.status?.toString() || 'planned',
                notes: form.notes?.toString() ?? '',
                characters: form.characters ?? [],
                foreshadows: form.foreshadows ?? [],
            };
            if (editingId) await UpdateOutline(editingId, input);
            else await CreateOutline(input);
            cancelEdit();
            await refresh();
        } catch (e: any) {
            setErr(`保存失败: ${e?.message ?? e}`);
        }
    }

    async function remove(id: number) {
        if (!props.projectID) return;
        if (!confirm(`确认删除大纲 id=${id}? 不可恢复`)) return;
        setErr('');
        try {
            await DeleteOutline(id);
            await refresh();
        } catch (e: any) {
            setErr(`删除失败: ${e?.message ?? e}`);
        }
    }

    // Key events / characters / foreshadows 用 textarea 多行字符串 (\n 分隔)
    function parseListField(s: string): string[] {
        return s.split('\n').map((l) => l.trim()).filter((l) => l.length > 0);
    }

    return (
        <div className="modal-bg" onClick={props.onClose}>
            <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                    <h2>📝 章节大纲</h2>
                    <button className="modal-close" onClick={props.onClose} aria-label="关闭">✕</button>
                </div>

                <div className="modal-body">
                    {!props.projectID ? (
                        <div className="info">请先选择项目</div>
                    ) : (
                        <>
                            <div className="outline-toolbar">
                                <span className="outline-toolbar-hint">每章一行, 章节号项目内唯一</span>
                                <span className="knowledge-tab-spacer" />
                                <button className="btn primary small" onClick={startCreate}>+ 新建</button>
                            </div>

                            {Object.keys(form).length > 0 && (
                                <div className="knowledge-form">
                                    <h4>{editingId ? `编辑大纲 #${editingId}` : '新建大纲'}</h4>
                                    <div className="form-row">
                                        <label className="form-label form-label-narrow">
                                            章节号 *
                                            <input type="number" min="1" value={form.chapter ?? 1} onChange={(e) => setForm({ ...form, chapter: parseInt(e.target.value) || 1 })} />
                                        </label>
                                        <label className="form-label form-label-grow">
                                            标题 *
                                            <input type="text" value={form.title ?? ''} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="如: 王小毛初遇师父" />
                                        </label>
                                    </div>
                                    <label className="form-label">
                                        梗概 (一两段)
                                        <textarea value={form.summary ?? ''} onChange={(e) => setForm({ ...form, summary: e.target.value })} rows={3} placeholder="这一章讲什么..." />
                                    </label>
                                    <label className="form-label">
                                        关键事件 (一行一个)
                                        <textarea value={(form.key_events ?? []).join('\n')} onChange={(e) => setForm({ ...form, key_events: parseListField(e.target.value) })} rows={3} placeholder="主角觉醒异能&#10;遇见反派&#10;..." />
                                    </label>
                                    <div className="form-row">
                                        <label className="form-label form-label-narrow">
                                            状态
                                            <select value={form.status ?? 'planned'} onChange={(e) => setForm({ ...form, status: e.target.value })}>
                                                <option value="planned">计划</option>
                                                <option value="in_progress">创作中</option>
                                                <option value="done">已完成</option>
                                            </select>
                                        </label>
                                    </div>
                                    <label className="form-label">
                                        涉及人物 (一行一个, 与 Module D 关联)
                                        <textarea value={(form.characters ?? []).join('\n')} onChange={(e) => setForm({ ...form, characters: parseListField(e.target.value) })} rows={2} placeholder="王小毛&#10;师父" />
                                    </label>
                                    <label className="form-label">
                                        相关伏笔 (一行一个, 与 Module D 关联)
                                        <textarea value={(form.foreshadows ?? []).join('\n')} onChange={(e) => setForm({ ...form, foreshadows: parseListField(e.target.value) })} rows={2} placeholder="神秘身世之谜&#10;古剑来历" />
                                    </label>
                                    <label className="form-label">
                                        作者备注
                                        <textarea value={form.notes ?? ''} onChange={(e) => setForm({ ...form, notes: e.target.value })} rows={2} placeholder="创作时提醒自己 (如: 这一章要埋伏笔)" />
                                    </label>
                                    <div className="knowledge-form-actions">
                                        <button className="btn primary" onClick={save}>保存</button>
                                        <button className="btn" onClick={cancelEdit}>取消</button>
                                    </div>
                                </div>
                            )}

                            {loading ? (
                                <div className="info">加载中...</div>
                            ) : items.length === 0 ? (
                                <div className="empty-state">
                                    <div className="empty-state-icon">📝</div>
                                    <div className="empty-state-title">暂无大纲</div>
                                    <div className="empty-state-desc">点击右上角"+ 新建"开始规划章节大纲</div>
                                </div>
                            ) : (
                                <ul className="knowledge-list">
                                    {items.map((item) => (
                                        <li key={item.id} className="knowledge-row">
                                            <div className="outline-row-badge">
                                                <div className="outline-row-chapter">第</div>
                                                <div className="outline-row-num">{item.chapter}</div>
                                                <div className="outline-row-chapter">章</div>
                                            </div>
                                            <div className="knowledge-row-info">
                                                <div className="knowledge-row-title">
                                                    {item.title}
                                                    {item.status && <span className={`status-tag status-${item.status}`}>{item.status}</span>}
                                                </div>
                                                {item.summary && (
                                                    <div className="knowledge-row-meta">{item.summary}</div>
                                                )}
                                                {item.key_events && item.key_events.length > 0 && (
                                                    <div className="outline-key-events">
                                                        {item.key_events.map((evt: string, i: number) => (
                                                            <span key={i} className="outline-event">• {evt}</span>
                                                        ))}
                                                    </div>
                                                )}
                                            </div>
                                            <div className="knowledge-row-actions">
                                                <button className="chapter-action-btn" onClick={() => startEdit(item)} title="编辑">✎</button>
                                                <button className="chapter-action-btn danger" onClick={() => remove(item.id)} title="删除">🗑</button>
                                            </div>
                                        </li>
                                    ))}
                                </ul>
                            )}

                            {err && <div className="error" style={{ marginTop: '8px' }}>{err}</div>}
                        </>
                    )}
                </div>

                <div className="modal-footer">
                    <button className="btn" onClick={props.onClose}>关闭</button>
                </div>
            </div>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Module D: KnowledgePanel (人物 / 关系 / 伏笔) — CRUD UI
// ---------------------------------------------------------------------------

// KnowledgePanel 3 tab (characters / relationships / foreshadows) 知识管理.
// 复用 ai-section 紫色样式风格. props.projectID 必填.
function KnowledgePanel(props: {
    projectID?: number;
    onClose: () => void;
}) {
    const [activeTab, setActiveTab] = useState<'characters' | 'relationships' | 'foreshadows'>('characters');
    const [items, setItems] = useState<any[]>([]);
    const [loading, setLoading] = useState(false);
    const [err, setErr] = useState('');
    const [editingId, setEditingId] = useState<number | null>(null); // null = 新建
    const [form, setForm] = useState<Record<string, any>>({});

    async function refresh() {
        if (!props.projectID) return;
        setLoading(true);
        setErr('');
        try {
            let list: any[] = [];
            if (activeTab === 'characters') list = await ListCharacters(props.projectID);
            else if (activeTab === 'relationships') list = await ListRelationships(props.projectID);
            else list = await ListForeshadows(props.projectID);
            setItems(list ?? []);
        } catch (e: any) {
            setErr(`加载失败: ${e?.message ?? e}`);
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        refresh();
        setEditingId(null);
        setForm({});
    }, [activeTab, props.projectID]);

    function startCreate() {
        setEditingId(null);
        if (activeTab === 'characters') {
            setForm({ name: '', role: 'supporting', description: '', first_chapter: 0, last_chapter: 0, traits: [] });
        } else if (activeTab === 'relationships') {
            setForm({ character_a: '', character_b: '', type: 'friend', description: '' });
        } else {
            setForm({ title: '', planted_chapter: 1, payoff_chapter: 0, status: 'active', description: '' });
        }
    }

    function startEdit(item: any) {
        setEditingId(item.id);
        setForm({ ...item, traits: item.traits ?? [] });
    }

    function cancelEdit() {
        setEditingId(null);
        setForm({});
    }

    async function save() {
        if (!props.projectID) return;
        setErr('');
        try {
            if (activeTab === 'characters') {
                if (!form.name?.toString().trim()) {
                    setErr('人物名字必填');
                    return;
                }
                const input = {
                    name: form.name?.toString().trim() ?? '',
                    role: form.role?.toString().trim() || 'supporting',
                    description: form.description?.toString() ?? '',
                    first_chapter: Number(form.first_chapter) || 0,
                    last_chapter: Number(form.last_chapter) || 0,
                    traits: form.traits ?? [],
                };
                if (editingId) await UpdateCharacter(editingId, input);
                else await CreateCharacter(input);
            } else if (activeTab === 'relationships') {
                if (!form.character_a?.toString().trim() || !form.character_b?.toString().trim()) {
                    setErr('人物 A 和人物 B 必填');
                    return;
                }
                const input = {
                    character_a: form.character_a?.toString().trim() ?? '',
                    character_b: form.character_b?.toString().trim() ?? '',
                    type: form.type?.toString().trim() || 'friend',
                    description: form.description?.toString() ?? '',
                };
                if (editingId) await UpdateRelationship(editingId, input);
                else await CreateRelationship(input);
            } else {
                if (!form.title?.toString().trim() || !form.planted_chapter) {
                    setErr('伏笔标题和埋设章节必填');
                    return;
                }
                const input = {
                    title: form.title?.toString().trim() ?? '',
                    planted_chapter: Number(form.planted_chapter) || 1,
                    payoff_chapter: Number(form.payoff_chapter) || 0,
                    status: form.status?.toString().trim() || 'active',
                    description: form.description?.toString() ?? '',
                };
                if (editingId) await UpdateForeshadow(editingId, input);
                else await CreateForeshadow(input);
            }
            cancelEdit();
            await refresh();
        } catch (e: any) {
            setErr(`保存失败: ${e?.message ?? e}`);
        }
    }

    async function remove(id: number) {
        if (!props.projectID) return;
        if (!confirm(`确认删除 id=${id}? 不可恢复`)) return;
        setErr('');
        try {
            if (activeTab === 'characters') await DeleteCharacter(id);
            else if (activeTab === 'relationships') await DeleteRelationship(id);
            else await DeleteForeshadow(id);
            await refresh();
        } catch (e: any) {
            setErr(`删除失败: ${e?.message ?? e}`);
        }
    }

    return (
        <div className="modal-bg" onClick={props.onClose}>
            <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                    <h2>📚 知识管理 (人物 / 关系 / 伏笔)</h2>
                    <button className="modal-close" onClick={props.onClose} aria-label="关闭">✕</button>
                </div>

                <div className="modal-body">
                    {!props.projectID ? (
                        <div className="info">请先选择项目</div>
                    ) : (
                        <>
                            {/* Tab 切换 */}
                            <div className="knowledge-tabs">
                                <button
                                    className={`knowledge-tab ${activeTab === 'characters' ? 'active' : ''}`}
                                    onClick={() => setActiveTab('characters')}
                                >
                                    人物
                                </button>
                                <button
                                    className={`knowledge-tab ${activeTab === 'relationships' ? 'active' : ''}`}
                                    onClick={() => setActiveTab('relationships')}
                                >
                                    关系
                                </button>
                                <button
                                    className={`knowledge-tab ${activeTab === 'foreshadows' ? 'active' : ''}`}
                                    onClick={() => setActiveTab('foreshadows')}
                                >
                                    伏笔
                                </button>
                                <span className="knowledge-tab-spacer" />
                                <button className="btn primary small" onClick={startCreate}>+ 新建</button>
                            </div>

                            {/* 编辑表单 */}
                            {Object.keys(form).length > 0 && (
                                <div className="knowledge-form">
                                    <h4>{editingId ? `编辑 ${activeTab === 'foreshadows' ? '伏笔' : activeTab === 'characters' ? '人物' : '关系'} #${editingId}` : `新建${activeTab === 'foreshadows' ? '伏笔' : activeTab === 'characters' ? '人物' : '关系'}`}</h4>
                                    {activeTab === 'characters' && (
                                        <>
                                            <label className="form-label">
                                                名字 *
                                                <input type="text" value={form.name ?? ''} onChange={(e) => setForm({ ...form, name: e.target.value })} placeholder="如: 王小毛" />
                                            </label>
                                            <label className="form-label">
                                                角色
                                                <select value={form.role ?? 'supporting'} onChange={(e) => setForm({ ...form, role: e.target.value })}>
                                                    <option value="protagonist">主角</option>
                                                    <option value="antagonist">反派</option>
                                                    <option value="supporting">配角</option>
                                                </select>
                                            </label>
                                            <label className="form-label">
                                                首次出现章节
                                                <input type="number" min="0" value={form.first_chapter ?? 0} onChange={(e) => setForm({ ...form, first_chapter: e.target.value })} />
                                            </label>
                                            <label className="form-label">
                                                最后出现章节
                                                <input type="number" min="0" value={form.last_chapter ?? 0} onChange={(e) => setForm({ ...form, last_chapter: e.target.value })} />
                                            </label>
                                            <label className="form-label">
                                                描述
                                                <textarea value={form.description ?? ''} onChange={(e) => setForm({ ...form, description: e.target.value })} rows={2} />
                                            </label>
                                        </>
                                    )}
                                    {activeTab === 'relationships' && (
                                        <>
                                            <label className="form-label">
                                                人物 A *
                                                <input type="text" value={form.character_a ?? ''} onChange={(e) => setForm({ ...form, character_a: e.target.value })} placeholder="如: 王小毛" />
                                            </label>
                                            <label className="form-label">
                                                人物 B *
                                                <input type="text" value={form.character_b ?? ''} onChange={(e) => setForm({ ...form, character_b: e.target.value })} placeholder="如: 林轩" />
                                            </label>
                                            <label className="form-label">
                                                关系类型
                                                <select value={form.type ?? 'friend'} onChange={(e) => setForm({ ...form, type: e.target.value })}>
                                                    <option value="friend">朋友</option>
                                                    <option value="foe">敌人</option>
                                                    <option value="family">家人</option>
                                                    <option value="romantic">恋人</option>
                                                    <option value="rival">对手</option>
                                                    <option value="mentor">师徒</option>
                                                </select>
                                            </label>
                                            <label className="form-label">
                                                描述
                                                <textarea value={form.description ?? ''} onChange={(e) => setForm({ ...form, description: e.target.value })} rows={2} />
                                            </label>
                                        </>
                                    )}
                                    {activeTab === 'foreshadows' && (
                                        <>
                                            <label className="form-label">
                                                标题 *
                                                <input type="text" value={form.title ?? ''} onChange={(e) => setForm({ ...form, title: e.target.value })} placeholder="如: 主角的神秘身世" />
                                            </label>
                                            <label className="form-label">
                                                埋设章节 *
                                                <input type="number" min="1" value={form.planted_chapter ?? 1} onChange={(e) => setForm({ ...form, planted_chapter: e.target.value })} />
                                            </label>
                                            <label className="form-label">
                                                揭示章节 (可选)
                                                <input type="number" min="0" value={form.payoff_chapter ?? 0} onChange={(e) => setForm({ ...form, payoff_chapter: e.target.value })} />
                                            </label>
                                            <label className="form-label">
                                                状态
                                                <select value={form.status ?? 'active'} onChange={(e) => setForm({ ...form, status: e.target.value })}>
                                                    <option value="active">活跃</option>
                                                    <option value="resolved">已揭示</option>
                                                    <option value="abandoned">已废弃</option>
                                                </select>
                                            </label>
                                            <label className="form-label">
                                                描述
                                                <textarea value={form.description ?? ''} onChange={(e) => setForm({ ...form, description: e.target.value })} rows={2} />
                                            </label>
                                        </>
                                    )}
                                    <div className="knowledge-form-actions">
                                        <button className="btn primary" onClick={save}>保存</button>
                                        <button className="btn" onClick={cancelEdit}>取消</button>
                                    </div>
                                </div>
                            )}

                            {/* 列表 */}
                            {loading ? (
                                <div className="info">加载中...</div>
                            ) : items.length === 0 ? (
                                <div className="empty-state">
                                    <div className="empty-state-icon">📚</div>
                                    <div className="empty-state-title">暂无{activeTab === 'characters' ? '人物' : activeTab === 'relationships' ? '关系' : '伏笔'}</div>
                                    <div className="empty-state-desc">点击右上角"+ 新建"开始</div>
                                </div>
                            ) : (
                                <ul className="knowledge-list">
                                    {items.map((item) => (
                                        <li key={item.id} className="knowledge-row">
                                            <div className="knowledge-row-info">
                                                {activeTab === 'characters' && (
                                                    <>
                                                        <div className="knowledge-row-title">
                                                            {item.name}
                                                            {item.role && <span className={`role-tag role-${item.role}`}>{item.role}</span>}
                                                        </div>
                                                        <div className="knowledge-row-meta">
                                                            {item.first_chapter > 0 && `首现 #${item.first_chapter}`}
                                                            {item.last_chapter > 0 && ` → 末现 #${item.last_chapter}`}
                                                            {item.description && ` · ${item.description.slice(0, 80)}`}
                                                        </div>
                                                    </>
                                                )}
                                                {activeTab === 'relationships' && (
                                                    <>
                                                        <div className="knowledge-row-title">
                                                            {item.character_a} ↔ {item.character_b}
                                                            {item.type && <span className={`type-tag type-${item.type}`}>{item.type}</span>}
                                                        </div>
                                                        <div className="knowledge-row-meta">
                                                            {item.description && item.description.slice(0, 100)}
                                                        </div>
                                                    </>
                                                )}
                                                {activeTab === 'foreshadows' && (
                                                    <>
                                                        <div className="knowledge-row-title">
                                                            {item.title}
                                                            {item.status && <span className={`status-tag status-${item.status}`}>{item.status}</span>}
                                                        </div>
                                                        <div className="knowledge-row-meta">
                                                            埋设 #第{item.planted_chapter}章
                                                            {item.payoff_chapter > 0 && ` → 揭示 #第${item.payoff_chapter}章`}
                                                            {item.description && ` · ${item.description.slice(0, 80)}`}
                                                        </div>
                                                    </>
                                                )}
                                            </div>
                                            <div className="knowledge-row-actions">
                                                <button className="chapter-action-btn" onClick={() => startEdit(item)} title="编辑">✎</button>
                                                <button className="chapter-action-btn danger" onClick={() => remove(item.id)} title="删除">🗑</button>
                                            </div>
                                        </li>
                                    ))}
                                </ul>
                            )}

                            {err && <div className="error" style={{ marginTop: '8px' }}>{err}</div>}
                        </>
                    )}
                </div>

                <div className="modal-footer">
                    <button className="btn" onClick={props.onClose}>关闭</button>
                </div>
            </div>
        </div>
    );
}

// ---------------------------------------------------------------------------
// Module H: SkillsPanel (13 个 LLM-driven skills 调用)
// ---------------------------------------------------------------------------

// SkillsPanel 13 skill 卡片网格 + 运行 modal (input textarea + run + 结果).
// props.projectID 不必填 (skills 与项目无关, 走后端全局 skills API).
function SkillsPanel(props: {
    onClose: () => void;
}) {
    const [skills, setSkills] = useState<any[]>([]);
    const [loading, setLoading] = useState(false);
    const [err, setErr] = useState('');
    const [runningSkill, setRunningSkill] = useState<string>(''); // 当前选中的 skill name
    const [input, setInput] = useState('');
    const [provider, setProvider] = useState('');
    const [model, setModel] = useState('');
    const [result, setResult] = useState<any | null>(null);
    const [running, setRunning] = useState(false);

    async function refresh() {
        setLoading(true);
        setErr('');
        try {
            const list = await ListSkills();
            setSkills(list ?? []);
        } catch (e: any) {
            setErr(`加载 skills 失败: ${e?.message ?? e}`);
        } finally {
            setLoading(false);
        }
    }

    useEffect(() => {
        refresh();
    }, []);

    function openSkill(skillName: string) {
        setRunningSkill(skillName);
        setInput('');
        setProvider('');
        setModel('');
        setResult(null);
    }

    function closeSkill() {
        setRunningSkill('');
        setResult(null);
    }

    async function execute() {
        if (!input.trim()) {
            setErr('输入不能为空');
            return;
        }
        setRunning(true);
        setErr('');
        try {
            const r = await ExecuteSkillSync(runningSkill, input, provider, model);
            setResult(r);
        } catch (e: any) {
            setErr(`执行失败: ${e?.message ?? e}`);
        } finally {
            setRunning(false);
        }
    }

    // Skill 卡片 icon 映射 (Module H - 用 emoji 简单占位)
    function skillIcon(name: string): string {
        const icons: Record<string, string> = {
            'expand': '✨',
            'rewrite': '🔄',
            'review': '📋',
            'consistency-review': '🔍',
            'compress': '📦',
            'translate': '🌐',
            'outline': '📝',
            'brainstorm': '💡',
            'character': '👤',
            'worldbuild': '🌍',
            'foreshadow': '🔮',
            'refine': '✨',
            'diagnose': '🩺',
        };
        return icons[name] || '⚡';
    }

    return (
        <div className="modal-bg" onClick={props.onClose}>
            <div className="modal modal-wide" onClick={(e) => e.stopPropagation()}>
                <div className="modal-header">
                    <h2>⚡ Skills (13 个 LLM-driven 工具)</h2>
                    <button className="modal-close" onClick={props.onClose} aria-label="关闭">✕</button>
                </div>

                <div className="modal-body">
                    {!runningSkill && (
                        <>
                            {loading ? (
                                <div className="info">加载中...</div>
                            ) : skills.length === 0 ? (
                                <div className="info">未加载到 skills (检查后端)</div>
                            ) : (
                                <div className="skills-grid">
                                    {skills.map((s) => (
                                        <div key={s.name} className="skill-card" onClick={() => openSkill(s.name)}>
                                            <div className="skill-icon">{skillIcon(s.name)}</div>
                                            <div className="skill-info">
                                                <div className="skill-name">{s.name}</div>
                                                <div className="skill-desc">{s.description}</div>
                                            </div>
                                            <button className="skill-run-btn">▶ 运行</button>
                                        </div>
                                    ))}
                                </div>
                            )}
                            {err && <div className="error" style={{ marginTop: '8px' }}>{err}</div>}
                        </>
                    )}

                    {runningSkill && (
                        <div className="skill-execute-panel">
                            <button className="btn small" onClick={closeSkill}>← 返回 skills 列表</button>
                            <h3 style={{ marginTop: '12px' }}>{skillIcon(runningSkill)} {runningSkill}</h3>

                            <label className="form-label">
                                输入 prompt *
                                <textarea
                                    value={input}
                                    onChange={(e) => setInput(e.target.value)}
                                    placeholder="传给 skill 的输入 (如: '扩写这段战斗描写')"
                                    rows={5}
                                    disabled={running}
                                />
                            </label>

                            <details className="skill-advanced">
                                <summary>高级选项 (provider / model)</summary>
                                <div className="form-row">
                                    <label className="form-label form-label-narrow">
                                        Provider (留空用默认)
                                        <select value={provider} onChange={(e) => setProvider(e.target.value)} disabled={running}>
                                            <option value="">(默认)</option>
                                            <option value="dashscope">dashscope</option>
                                            <option value="deepseek">deepseek</option>
                                            <option value="minimax">minimax</option>
                                        </select>
                                    </label>
                                    <label className="form-label form-label-grow">
                                        Model (留空用 provider 默认)
                                        <input
                                            type="text"
                                            value={model}
                                            onChange={(e) => setModel(e.target.value)}
                                            placeholder="如: claude-opus-4-5-20250929"
                                            disabled={running}
                                        />
                                    </label>
                                </div>
                            </details>

                            <div className="skill-actions">
                                <button className="btn primary" onClick={execute} disabled={running || !input.trim()}>
                                    {running ? '⏳ 执行中 (10-60s)...' : '▶ 运行'}
                                </button>
                            </div>

                            {err && <div className="error" style={{ marginTop: '12px' }}>{err}</div>}

                            {result && (
                                <div className="skill-result">
                                    <div className="skill-result-meta">
                                        <span className="role-tag">{result.provider}</span>
                                        <span className="role-tag">{result.model}</span>
                                        <span>{result.tokens_in} → {result.tokens_out} tokens</span>
                                    </div>
                                    <pre className="skill-result-content">{result.content}</pre>
                                </div>
                            )}
                        </div>
                    )}
                </div>

                <div className="modal-footer">
                    <button className="btn" onClick={props.onClose}>关闭</button>
                </div>
            </div>
        </div>
    );
}

export default App;