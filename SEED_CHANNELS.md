# 渠道预置（Seed Channels）——Gemini 文本/图片 + xAI Grok 视频

> 本文档描述本 fork 相对上游 [ddcat-ai/open-ai-canvas](https://github.com/ddcat-ai/open-ai-canvas) 新增的一个能力：
> **服务启动时按环境变量自动预置系统渠道**，免去每次部署都要登录后台手动点"新增渠道"的步骤。
> 当前预置的默认组合是：**Gemini 做文本对话 + 图片生成，xAI Grok 做视频生成**。

## 为什么要做这个

上游项目的模型渠道全部通过后台"渠道管理"界面手动配置：注册第一个管理员账号 → 登录 → 系统设置 → 新增渠道 → 填 Base URL / API Key / 接口类型 / 模型列表。每次重新部署（换服务器、CI 里起临时环境、多套环境反复初始化）都要重复点一遍。

这个 fork 加了一层"启动时自动建渠道"的逻辑：只要在部署环境里设置好对应的 API Key 环境变量，服务第一次启动时就会自动把渠道建好、模型启用好，登录后台就能直接用，不用再点一遍。

## 核心设计原则

1. **完全可选**：不设置任何 `CANVAS_SEED_*` 环境变量，行为和上游原版完全一致，不会有任何副作用。
2. **幂等、不覆盖手工配置**：每次启动都会检查"是否已存在同名渠道"，存在就跳过。也就是说，就算你后台手动改了预置渠道的 Base URL 或模型列表，下次重启也不会被这段代码覆盖回去。
3. **失败不阻塞启动**：单个渠道的配置校验失败（比如 Base URL 格式不对）只会打日志跳过，不会导致整个服务起不来。

## 预置了哪些渠道

| 渠道名称 | 接口类型 | 默认 Base URL | 默认模型 | 触发的环境变量 |
|---|---|---|---|---|
| Gemini 文本 | `chat-completion` | `https://generativelanguage.googleapis.com/v1beta/openai/` | `gemini-3.5-flash` | `CANVAS_SEED_GEMINI_API_KEY` |
| Gemini 图片 | `openai-image` | 同上 | `gemini-3-pro-image-preview`, `gemini-2.5-flash-image` | `CANVAS_SEED_GEMINI_API_KEY`（和文本共用同一把 Key） |
| xAI Grok 视频 | `xai-video` | `https://api.x.ai/v1` | `grok-video-1.5`, `grok-video-1.5-1080p` | `CANVAS_SEED_XAI_API_KEY` |

**为什么 Gemini 要拆成两个渠道？** 因为本项目里一个"渠道"只对应一种接口类型（文本 / 图片 / 视频三选一），Google 的 OpenAI 兼容层虽然文本和图片走的是同一个 Base URL，但在这个项目的模型里仍然需要分别建两个渠道条目。

**为什么视频用的是 xAI 而不是 Gemini 的 Veo？** Google 的 OpenAI 兼容层（`/v1beta/openai/`）官方文档目前只覆盖 Chat Completions、Embeddings 和部分图片生成，**不包含视频生成**；Veo 走的是完全不同的异步长任务协议。而 xAI Grok 的视频生成协议已经被上游项目原生适配好了（`xai-video` 接口类型），所以视频这块用 xAI 是当前最省事、最稳的选择。

## 快速开始

### 1. 拿到 API Key

- **Gemini**：去 [Google AI Studio](https://aistudio.google.com/apikey) 申请
- **xAI（Grok）**：去 [xAI 控制台](https://console.x.ai/) 申请

### 2. 设置环境变量

在 `.env`（配合 `docker-compose.local.yml` / `docker-compose.deploy.yml`）或者部署脚本里加上：

```bash
CANVAS_SEED_GEMINI_API_KEY=你的Gemini Key
CANVAS_SEED_XAI_API_KEY=你的xAI Key
```

只想要文本+图片、暂时不接视频？只设置 `CANVAS_SEED_GEMINI_API_KEY` 即可，xAI 那个渠道会自动跳过。

### 3. 启动服务

```bash
docker compose -f docker-compose.local.yml up -d --build
```

或本地开发模式：

```bash
cd backend
go run ./cmd/server
```

启动日志里会看到：

```
seed channel "Gemini 文本" created with 1 model(s)
seed channel "Gemini 图片" created with 2 model(s)
seed channel "xAI Grok 视频" created with 2 model(s)
```

### 4. 登录后台确认

注册第一个账号（自动成为管理员）→ 系统设置 → 渠道管理，应该能看到上面三个渠道，模型均为启用状态。画布右上角的模型选择器里也能直接选到 Gemini / Grok 的模型了。

## 完整环境变量列表

| 环境变量 | 是否必填 | 默认值 | 说明 |
|---|---|---|---|
| `CANVAS_SEED_GEMINI_API_KEY` | 否 | 无 | 不填则 Gemini 文本、图片两个渠道都不会创建 |
| `CANVAS_SEED_GEMINI_BASE_URL` | 否 | `https://generativelanguage.googleapis.com/v1beta/openai/` | 覆盖 Gemini 的 Base URL（比如你用了自建中转） |
| `CANVAS_SEED_GEMINI_TEXT_MODELS` | 否 | `gemini-3.5-flash` | 逗号分隔，覆盖文本渠道的默认模型列表 |
| `CANVAS_SEED_GEMINI_IMAGE_MODELS` | 否 | `gemini-3-pro-image-preview,gemini-2.5-flash-image` | 逗号分隔，覆盖图片渠道的默认模型列表 |
| `CANVAS_SEED_XAI_API_KEY` | 否 | 无 | 不填则 xAI 视频渠道不会创建 |
| `CANVAS_SEED_XAI_BASE_URL` | 否 | `https://api.x.ai/v1` | 覆盖 xAI 的 Base URL |
| `CANVAS_SEED_XAI_VIDEO_MODELS` | 否 | `grok-video-1.5,grok-video-1.5-1080p` | 逗号分隔，覆盖视频渠道的默认模型列表 |
| `CANVAS_SEED_DEFAULT_PRICE_MICROCREDITS` | 否 | `0` | 预置模型的默认积分单价（自用场景保持 0 即可；对外开放收费服务时应设为真实值） |

## 关于计费/定价的一个细节

上游项目里，系统渠道下的模型如果是"自动发现"出来的（管理员点"拉取模型目录"），会默认是**禁用 + 未定价**状态，需要管理员手动启用并填价格，否则任务会被计费流程拦截报错"当前模型尚未配置用户积分价格"。

这个 fork 的预置逻辑走的是"显式指定模型列表"的创建路径，模型会直接是**启用**状态；同时额外做了一步，把这些模型标记为"已定价"，单价默认 **0 积分**（自用免费，不会被计费流程拦截）。

> ⚠️ 如果你打算把这套服务开放给其他用户使用并按次收费，记得部署前把 `CANVAS_SEED_DEFAULT_PRICE_MICROCREDITS` 设置成真实的价格，或者部署后去后台"渠道管理"里手动改价格——不要让别人白嫖你的 Gemini / xAI 额度。

## 已知限制

- **不支持原生 Veo**：如前所述，Google 的 Veo 视频生成走的是独立的异步协议，不在 OpenAI 兼容层范围内，这个项目目前没有原生适配。如果需要，得自己写一个类似 `xai-video` 的新 `InterfaceType` 适配器。
- **图片生成的参数集比原生 Gemini API 窄**：走 OpenAI 兼容层调用图片生成时，Google 官方文档说明部分原生参数会被静默忽略，复杂的图像编辑控制建议还是走原生 API。
- **模型名称需要跟着 Google/xAI 的更新走**：默认模型列表是文档编写时的可用模型，如果后续对方下线或改名，需要通过 `CANVAS_SEED_GEMINI_TEXT_MODELS` 等环境变量手动更新，或者登录后台手动调整。

## 涉及的代码

- `backend/internal/service/channel_seed.go`：新增文件，渠道预置的核心逻辑
- `backend/cmd/server/main.go`：在启动流程里插入一行 `svc.EnsureSeedSystemChannels()` 调用

## 致谢

本 fork 基于 [ddcat-ai/open-ai-canvas](https://github.com/ddcat-ai/open-ai-canvas)（AGPL-3.0）二次开发，而该项目本身又是 [basketikun/infinite-canvas](https://github.com/basketikun/infinite-canvas) 的二次开发。感谢上游所有作者和贡献者。
