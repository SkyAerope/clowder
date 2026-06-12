# syntax=docker/dockerfile:1
#
# 构建一个可直接运行的 clowder 服务镜像（前端已内嵌进二进制）：
#   docker build -t clowder .
#   docker run -d --name clowder -p 12100:12100 \
#     -v clowder-data:/home/clowder/.clowder clowder
#
# 配置 / sqlite 数据 / providers 都落在 ~/.clowder，用具名卷持久化即可。
# 各平台独立二进制由 GitHub Actions 构建发布，不在本文件职责内。

# ---- 阶段 1：构建前端 (web/dist) ----
FROM node:22-alpine AS web
WORKDIR /app/web
RUN corepack enable
# 先装依赖以利用缓存；packageManager 字段已在 package.json 中锁定 pnpm 版本
COPY web/package.json web/pnpm-lock.yaml ./
RUN --mount=type=cache,target=/root/.local/share/pnpm/store \
    pnpm install --frozen-lockfile
COPY web/ ./
RUN pnpm build

# ---- 阶段 2：编译 Go 二进制（内嵌前端）----
# modernc.org/sqlite 为纯 Go 实现，CGO_ENABLED=0 即可静态编译。
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
# 用上一阶段产物覆盖 //go:embed all:dist 的占位目录
COPY --from=web /app/web/dist ./web/dist
# 跨架构构建时由 BuildKit 注入 TARGETARCH（docker build --platform ...）
ARG TARGETOS=linux
ARG TARGETARCH=amd64
RUN --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} \
    go build -trimpath -ldflags="-s -w" -o /out/clowder ./cmd/clowder

# ---- 阶段 3：运行时镜像（默认目标）----
FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata \
    && adduser -D -u 10001 clowder
COPY --from=build /out/clowder /usr/local/bin/clowder
# 预建数据目录并赋权，使匿名/具名卷初始化时继承 clowder 所有权（否则卷归 root，非 root 用户无法写）
RUN mkdir -p /home/clowder/.clowder && chown -R clowder:clowder /home/clowder
USER clowder
WORKDIR /home/clowder
# 配置、sqlite 数据库、providers 目录均位于 ~/.clowder，挂卷以持久化
VOLUME ["/home/clowder/.clowder"]
EXPOSE 12100
ENTRYPOINT ["clowder"]
CMD ["serve"]
