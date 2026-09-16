---
name: browser-cdp
description: "novel2all 浏览器能力适配：通过 Playwright（不是 CDP 自启动）实现网页抓取、登录态复用。"
---

# browser-cdp：浏览器能力

novel2all 的浏览器抓取能力。**不启动 Chrome / CDP 端口 / 独立浏览器**——使用 Playwright Python SDK。

## 工作流

1. **优先级**：先看 LLM 内置知识 / 用户提供的数据，再用浏览器抓取验证
2. **Playwright Python SDK**：
   - 启动 chromium 内核（不是 Chrome 系统安装）
   - 复用 storage_state 维持登录态
   - 滚动 + 等待 + 提取
3. **严格遵守**：
   - 站点 robots.txt
   - 访问频率限制（≥ 3 秒）
   - 用户授权（涉及登录态必须问）
   - 不绕过验证码 / 付费墙 / IP 限制

## 用法

```python
from playwright.async_api import async_playwright

async with async_playwright() as p:
    browser = await p.chromium.launch(headless=True)
    context = await browser.new_context(
        storage_state="auth.json",  # 登录态复用
    )
    page = await context.new_page()
    await page.goto("https://example.com/data")
    
    # 抓取
    data = await page.locator(".item").all_text_contents()
    
    await browser.close()
```

## 关键原则

- **采集来源 + 时间 + URL + 失败项** 必须记录
- **不绕过验证码 / 反爬**——失败时告诉用户手动操作
- **不在 prompt 里贴整页 HTML**——只贴提取的关键字段
- **存储原文 / 摘要 / 评估三档**——按可信度使用

## 输出格式

```
抓取报告.md
├── 1. 任务描述
├── 2. 数据来源 URL + 采集时间
├── 3. 抓取内容（结构化）
├── 4. 失败项 + 重试建议
└── 5. 数据质量评估
```

## 适用场景

- 榜单采集（起点 / 番茄 / 晋江排行）
- 网文评论情感分析
- 对标书自动抓章节
- 关键词搜索（起点站内）

## 不适用

- 实时数据（用站内 API 或 RSS）
- 反爬严格的网站（需要专业爬虫工具）
- 隐私敏感内容（绝对不抓）
