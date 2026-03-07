# 4subs

`4subs` 现已重置为一个全新的双语字幕生成项目骨架：

- 后端继续使用 `Go`
- 前端继续使用 `Vue 3 + PrimeVue`
- 服务形态继续保持 `Docker` 优先
- 目标工作流聚焦“本地媒体 -> 音频提取 -> 原文识别 -> DeepSeek 翻译 -> 双语字幕导出”

## 当前重构结果

这次已经完成旧字幕下载模块清理，并重建了以下核心骨架：

- 新的 Go API 服务
- 新的 SQLite 数据结构
- 新的 PrimeVue 工作台界面
- 新的媒体扫描、任务创建、设置管理接口
- 新的 Docker 镜像打包基础
- 新的 GitHub Actions 镜像构建工作流

## 当前 API

- `GET /api/v1/health`
- `GET /api/v1/overview`
- `GET /api/v1/pipeline`
- `GET /api/v1/settings`
- `PUT /api/v1/settings`
- `GET /api/v1/media`
- `POST /api/v1/media/scan`
- `GET /api/v1/jobs`
- `POST /api/v1/jobs`

## 新目录职责

- `cmd/server`：服务启动入口
- `internal/config`：环境变量与目录配置
- `internal/db`：SQLite 持久化与默认数据
- `internal/library`：本地媒体扫描
- `internal/pipeline`：字幕处理流水线定义
- `internal/translator`：翻译适配层抽象
- `internal/server`：HTTP API 与静态页面托管
- `web/src/views`：PrimeVue 管理界面

## Docker 启动

1. 复制配置：

```bash
cp .env.example .env
```

2. 至少填写：

- `APP_SECRET`
- `DEEPSEEK_API_KEY`
- `MEDIA_HOST_PATH`

3. 启动：

```bash
docker compose up -d --build
```

4. 打开：

- UI：`http://localhost:8080`
- Health：`http://localhost:8080/api/v1/health`

## GitHub Actions 镜像构建

工作流文件：`.github/workflows/docker-image.yml:1`

规则：

- 推送到 `main`：构建并推送 `edge`
- 推送 `v*` 标签：构建并推送版本标签
- Pull Request：只构建验证，不推送镜像

镜像地址：

```text
ghcr.io/<owner>/<repo>
```

## 当前状态说明

当前版本已经是“新项目骨架”，不是旧项目小修小补。

已实现：

- 本地媒体扫描
- 任务创建与任务列表
- 设置持久化
- PrimeVue 新工作台
- DeepSeek 配置入口

尚未实现：

- 实际音频提取执行器
- 实际 ASR 识别接入
- 实际 DeepSeek 翻译调用
- 双语 SRT/ASS 渲染
- 任务后台执行与进度推进

## 下一步建议

建议接下来按这个顺序继续：

1. 增加任务执行器
2. 接入 ASR 适配层
3. 接入 DeepSeek 翻译实现
4. 生成双语 `SRT`
5. 增加字幕预览与人工校对

