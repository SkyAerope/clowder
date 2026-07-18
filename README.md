# Clowder

LLM 聚合代理 —— 聚合多个 LLM provider，暴露统一的 OpenAI-compatible API，自动处理 key 轮换、故障转移与 provider 切换。

## 前置要求

- Go >= 1.25
- Node.js >= 22，pnpm >= 10
- [air](https://github.com/air-verse/air)（热重载开发，可选）

## 快速启动

### Docker（推荐）

```bash
docker build -t clowder .
docker run -d --name clowder -p 12100:12100 \
  -v clowder-data:/home/clowder/.clowder clowder
```

服务启动后监听 `http://localhost:12100`，配置与数据持久化在 `clowder-data` 卷中。

### 本地开发

需要两个终端分别运行后端和前端。

**后端**

```bash
# 安装 air（如未安装）
go install github.com/air-verse/air@latest

# 启动后端（热重载）
export PATH=$PATH:$(go env GOPATH)/bin
air
```

后端监听 `0.0.0.0:12100`，地址可通过 `~/.clowder/config.yaml` 的 `bind_addr` / `port` 字段修改。

**前端**

```bash
cd web
pnpm install
pnpm dev
```

前端开发服务器监听 `localhost:5173`，API 请求自动代理到后端。

### 生产构建

```bash
cd web && pnpm install && pnpm build && cd ..
CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/clowder ./cmd/clowder
./bin/clowder serve
```

前端产物通过 `go:embed` 内嵌进二进制，无需额外部署。

## CLI 命令

```
clowder start                    启动代理（后台）
clowder stop                     停止
clowder status                   状态一览
clowder register <provider> [N]  手动注册 N 个 key（默认 1）
clowder keys [provider]          列出 key
clowder providers                列出 provider 及状态
clowder reload                   热加载配置 + 重新扫描 provider
clowder serve                    前台运行（start 的实际工作进程）
```

## 配置

配置文件位于 `~/.clowder/config.yaml`，首次启动自动生成。

## Provider 配置

Provider 定义在 `providers/` 目录下，每个 provider 一个子目录，包含 `manifest.yaml`。

Key 来源由 manifest 中的脚本和数据库共同决定：有 `scripts.register` 时支持注册脚本；手动 key 通过 `clowder import <provider> <file>` 或管理后台添加，均写入数据库。运行时不会自动读取 provider 目录下的 `keys.txt`。

详细格式参见 `providers/` 目录下示例。

## 项目结构

```
cmd/clowder/       # 入口
internal/
  app/             # 应用层（路由、action）
  cli/             # CLI 命令
  config/          # 配置加载
  health/          # 健康检查
  models/          # 数据模型
  pool/            # Key 池管理
  provider/        # Provider manifest
  proxy/           # 代理核心（SSE、并行转发）
  rules/           # 路由规则引擎
  script/          # Provider 外挂脚本
  server/          # HTTP 服务 & Dashboard
  store/           # SQLite 存储
web/               # 前端
```
