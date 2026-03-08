# 4subs

`4subs` 现在是一套面向本地媒体的双语字幕生成工具：

- 后端：`Go`
- 前端：`Vue 3 + PrimeVue`
- 部署：`Docker` 优先
- 翻译：`DeepSeek API`
- 转写：`OpenAI 兼容 ASR API`
- 硬字幕识别：`OpenAI 兼容视觉 OCR API`

## 当前已实现

这一版已经跑通了新的处理链路：

1. 扫描本地媒体目录
2. 创建字幕翻译任务
3. 优先提取同名外挂字幕，或抽取视频内嵌文本字幕轨
4. 找不到文本字幕时，优先抽取底部字幕区域关键帧并调用远程 OCR
5. 如果 OCR 仍未产出有效字幕，则提取音频并调用远程 ASR 转写
6. 解析为标准 `SRT` 字幕块
7. 按批次调用 `DeepSeek Chat Completions` 翻译
8. 生成双语 `SRT` 与 `ASS` 到输出目录
9. 在任务详情页预览源字幕、双语 `SRT/ASS`，并支持人工校对后保存
10. 支持并发执行多个任务，并可取消排队中或运行中的任务
11. 任务详情页支持查看执行日志，便于定位失败阶段和人工修改记录
12. 设置页支持翻译风格模板、自定义风格要求和术语表

## 当前 API

- `GET /api/v1/health`（包含 `ocr_ready`）
- `GET /api/v1/overview`
- `GET /api/v1/pipeline`
- `GET /api/v1/settings`
- `PUT /api/v1/settings`
- `GET /api/v1/media`
- `POST /api/v1/media/scan`
- `GET /api/v1/jobs`
- `GET /api/v1/jobs/{id}`
- `GET /api/v1/jobs/{id}/logs`
- `POST /api/v1/jobs`
- `POST /api/v1/jobs/{id}/retry`
- `POST /api/v1/jobs/{id}/cancel`
- `GET /api/v1/jobs/{id}/download?kind=output|srt|ass`
- `GET /api/v1/jobs/{id}/preview?kind=source|output|srt|ass`
- `PUT /api/v1/jobs/{id}/preview?kind=srt|ass`

## 关键能力边界

当前版本已支持：

- 同名外挂字幕提取（`.srt`、`.ass`、`.ssa`、`.vtt`）
- 视频内嵌文本字幕轨提取
- 找不到文本字幕时自动回退到远程 OCR 硬字幕识别
- OCR 失败时自动回退到远程 ASR 转写
- DeepSeek 批量翻译
- 双语 `SRT` 输出
- 双语 `ASS` 输出
- 在线预览与人工校对保存
- 任务取消
- 后台并发执行
- 任务日志追踪
- 术语表与翻译风格模板

当前版本暂未支持：

- OCR 结果缓存与批量复用
- 多人协作审校

## 任务状态

- `queued`：已进入队列，等待执行
- `running`：正在处理
- `cancelling`：已收到取消请求，等待任务中断
- `cancelled`：任务已取消
- `completed`：任务已完成
- `failed`：任务执行失败

## 新目录职责

- `cmd/server`：服务启动入口
- `internal/config`：环境变量与目录配置
- `internal/db`：SQLite 持久化、任务状态与设置存储
- `internal/library`：本地媒体扫描
- `internal/media`：字幕源提取、音频提取、OCR 抽帧与结果落盘
- `internal/ocr`：OCR 时间轴恢复与远程视觉识别适配
- `internal/subtitle`：SRT/ASS 渲染与字幕解析
- `internal/jobrunner`：后台任务执行器
- `internal/translator/deepseek`：DeepSeek 翻译接入
- `internal/asr/openai`：OpenAI 兼容音频转写接入
- `internal/server`：HTTP API 与静态页面托管
- `web/src/views`：PrimeVue 工作台页面与任务校对页

## 环境变量

至少需要配置：

- `DEEPSEEK_API_KEY`
- `MEDIA_HOST_PATH`

按需配置：

- `ASR_API_KEY`
- `OCR_API_KEY`

推荐同时确认：

- `DEEPSEEK_MODEL`，默认 `deepseek-chat`
- `ASR_MODEL`，默认 `whisper-1`
- `ASR_BASE_URL`，默认 `https://api.openai.com/v1`
- `OCR_MODEL`，默认 `gpt-4.1-mini`
- `OCR_BASE_URL`，默认 `https://api.openai.com/v1`
- `OCR_FRAME_INTERVAL_MS`，默认 `1000`
- `OCR_CROP_TOP_PERCENT`，默认 `72`
- `OCR_CROP_HEIGHT_PERCENT`，默认 `22`
- `JOB_CONCURRENCY`，默认 `2`
- `MEDIA_PATHS`，本地直接运行时可配置多个媒体目录

## Docker 启动

1. 复制配置：

```bash
cp .env.example .env
```

2. 填写 API Key 与媒体目录：

```bash
docker compose up -d --build
```

3. 打开：

- UI：`http://localhost:8080`
- Health：`http://localhost:8080/api/v1/health`

## GitHub Actions 镜像构建

工作流文件：`.github/workflows/docker-image.yml:1`

规则：

- 推送到 `main`：构建并推送 `main`、`edge` 与 `sha-<commit>` 标签
- 推送 `v*` 标签：构建并推送版本标签、次版本标签与 `sha-<commit>`
- Pull Request：只构建验证，不推送镜像
- 支持手动触发 `workflow_dispatch`

镜像地址：

```text
ghcr.io/<owner>/<repo>
```

默认产物平台：

- `linux/amd64`
- `linux/arm64`

## 下一步建议

最值得继续做的功能顺序：

1. OCR 结果缓存与重试策略
2. 术语表导入 / 导出
3. 任务日志检索与筛选
4. 字幕版本管理
