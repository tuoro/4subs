# 4subs 部署说明

## 方式一：本地源码构建

适合你在本机或服务器上直接拉代码、改代码、再自己构建镜像。

### 步骤

1. 复制环境变量模板：

```bash
cp .env.example .env
```

2. 填写以下关键变量：

- `DEEPSEEK_API_KEY`
- `ASR_API_KEY`（可选，但建议配置）
- `OCR_API_KEY`（可选，但建议配置）
- `MEDIA_HOST_PATH`

3. 启动：

```bash
docker compose up -d --build
```

## 方式二：直接拉取 GHCR 镜像

适合生产环境和不想在服务器上编译的人。

### 步骤

1. 复制部署环境模板：

```bash
cp deploy/.env.ghcr.example .env
```

2. 按需修改：

- `GHCR_IMAGE`，例如 `gayhub/4subs`
- `IMAGE_TAG`，例如 `edge` 或 `v0.1.0`
- `MEDIA_HOST_PATH`
- 各类 API Key

3. 启动：

```bash
docker compose -f docker-compose.ghcr.yml up -d
```

4. 更新镜像：

```bash
docker compose -f docker-compose.ghcr.yml pull
docker compose -f docker-compose.ghcr.yml up -d
```

## 目录挂载说明

- `./deploy/config`：应用配置目录
- `./deploy/data`：数据库与状态数据
- `./deploy/work`：中间处理文件
- `./deploy/subtitles`：导出的双语字幕成品
- `${MEDIA_HOST_PATH}`：你的影片目录，只读挂载到容器内 `/media`

## 建议配置策略

### 最轻本地负担

这是最符合你当前目标的方案：

- 翻译走 `DeepSeek API`
- 语音识别走远程 `ASR API`
- 硬字幕识别走远程 `OCR API`
- 本地只负责扫描文件、抽帧、提取音频、写出字幕

### 推荐优先级

1. 一定配置 `DeepSeek API`
2. 建议同时配置 `OCR API`
3. 再配置 `ASR API` 作为最终兜底

这样可以形成最完整的回退链路：

`文本字幕 -> OCR -> ASR`

## GitHub Actions 出镜像规则

- 推送到 `main`：构建并推送 `main`、`edge`、`sha-*`
- 推送 `v*` 标签：构建并推送版本标签与次版本标签
- Pull Request：只构建验证，不推送

默认镜像地址：

```text
ghcr.io/<owner>/<repo>
```
