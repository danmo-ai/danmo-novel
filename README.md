# Danmo Novel

受控 AI 小说生产与创作工作台。

- 固定阶段流水线：规划 → 细纲 → 写正文 → 审校/定稿
- 有预算的无工具 LLM 调用（无 Agent Loop）
- AI 结果先成为候选，作者接受后才进入正文/canon
- 多提供商：OpenAI / Anthropic / OpenAI 兼容（DeepSeek、Qwen、Kimi、GLM、Gemini、Ollama、OpenRouter…）
- 兼容 Danmo Work 的 `novel/<book-id>/` 书籍目录

## Dev

```bash
make dev
# API :7804  UI :5804

make dev-desktop   # Tauri 桌面壳 + Vite HMR
```

首次使用在「模型设置」添加提供商、启用模型，并配置创作阶段模型。

## Desktop / 发布

```bash
make pack-macos-desktop RELEASE_VERSION=0.1.0
# 或 pack-linux-desktop / pack-windows-desktop
```

打 `v*` tag 触发 GitHub Actions，产出 macOS / Linux / Windows 安装包，并生成 Tauri 自升级清单 `latest.json`。详见 [docs/desktop-release.md](docs/desktop-release.md)。

## 导入已有书

书架 → 导入已有书目录，填 `novel/<book-id>/` 的绝对路径。默认只读；需要写回时在书内启用写入。

## 数据

- `~/.danmo-novel/novel.db` — provider、任务、候选
- `~/.danmo-novel/library/novel/<book-id>/` — 新建书籍
- 导入书籍仍读原目录文件

## 测试

```bash
make test
```
