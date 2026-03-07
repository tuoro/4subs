# 4subs

`4subs` 现在是一套面向本地媒体的双语字幕生成工具：

- 后端：`Go`
- 前端：`Vue 3 + PrimeVue`
- 部署：`Docker` 优先
- 翻译：`DeepSeek API`

## 当前已实现

这一版已经不再是旧的字幕下载聚合器，而是新的处理链路：

1. 扫描本地媒体目录
2. 创建字幕翻译任务
3. 提取同名外挂字幕，或尝试抽取视频内嵌文本字幕轨
4. 解析为标准 `SRT` 字幕块
5. 按批次调用 `DeepSeek Chat Completions` 翻译
6. 生成双语 `SRT` 到输出目录

## 当前 API

- `GET /api/v1/health`
- `GET /api/v1/overview`
- `GET /api/v1/pipeline`
- `GET /api/v1/settings`
- `PUT /api/v1/settings`
- `GET /api/v1/media`
- `POST /api/v1/media/scan`
- `GET /api/v1/jobs`
- `GET /api/v1/jobs/{id}`
- `POST /api/v1/jobs`
- `POST /api/v1/jobs/{id}/retry`
- `GET /api/v1/jobs/{id}/download`

## 关键能力边界

当前版本已支持：

- 同名外挂字幕提取（`.srt`、`.ass`、`.ssa`、`.vtt`）
- 视频内嵌文本字幕轨提取
- DeepSeek 批量翻译
- 双语 `SRT` 输出

当前版本暂未支持：

- 纯音频 `ASR` 识别
- 图片字幕 `OCR`
- `ASS` 样式导出
- 人工逐条校对工作台

如果视频既没有外挂字幕，也没有可提取的内嵌文本字幕轨，任务会失败并提示“当前版本尚未接入 ASR”。

## 新目录职责

- `cmd/server`：服务启动入口
- `internal/config`：环境变量与目录配置
- `internal/db`：SQLite 持久化、任务状态与设置存储
- `internal/library`：本地媒体扫描
- `internal/media`：字幕源提取与结果落盘
- `internal/subtitle`：SRT 解析与双语渲染
- `internal/jobrunner`：后台任务执行器
- `internal/translator/deepseek`：DeepSeek 翻译接入
- `internal/server`：HTTP API 与静态页面托管
- `web/src/views`：PrimeVue 工作台页面

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

## 本地验证结果

我已经完成：

- `go build -buildvcs=false ./cmd/server`
- `npm install`
- `npm run build`

说明：当前环境没有 `docker` 命令，所以我没有实际执行本地镜像构建，但 GitHub Actions 工作流已经就位。

## 下一步建议

最值得继续做的功能顺序：

1. 接入 `ASR`，覆盖“无源字幕”视频
2. 增加任务并发控制和取消能力
3. 增加字幕预览与人工修订
4. 增加 `ASS` 导出
