# 来源格式支持 (Sprint 35)

## txt 格式
- 编码: UTF-8 / GBK (自动检测)
- 章节: "第N章" / "Chapter N"
- 处理: 按行读, 切分, 清理

## docx 格式
- 库: python-docx / zipfile
- 解析: document.xml 段落 + 样式
- 章节: Heading 1 样式 + "第N章"

## md 格式
- 章节: # / ## / ###
- 处理: 按 markdown 解析

## epub 格式
- 结构: META-INF + OEBPS
- 解析: container.xml → OPF → spine
- 章节: HTML <h1>/<h2>

## 链接抓取 (在线)
- 用 browser-cdp skill (Playwright)
- 加载页面 → 提取正文 → 切分章节
- 登录态: storage_state 复用

## 来源识别
- 文件后缀: .txt / .docx / .md / .epub
- URL 模式: 起点/番茄/七猫 (平台特征)
- 文件头: BOM 检测 + 编码检测
