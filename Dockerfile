# syntax=docker/dockerfile:1
#
# 两种出镜像方式，共用同一个 runtime 基础层（工具链）：
#
# ① 默认（最后一个 stage `prebuilt`，CI 用）：不在镜像里编译，放入**预编译好的
#    Linux 单二进制**。二进制由 CI 的 binaries job 交叉编译（纯 Go、无 QEMU），按目标
#    架构放在构建上下文的 dist/<TARGETARCH>/artex。这样多架构构建时 arm64 只需模拟
#    apt 层，不再模拟 Next/Go 编译，速度快得多。手动准备二进制：
#      cd web && npm run build:static && cd ..
#      cp -r web/out server/webui/dist
#      CGO_ENABLED=0 GOARCH=amd64 go build -tags embedui -o dist/amd64/artex ./cmd/artex
#      docker build -t artex:local .
#
# ② `--target source`（docker-compose.yml 用）：在镜像里从源码编译前端与 Go，
#    宿主机只需要 docker，不依赖任何第三方预构建镜像：
#      docker build --target source -t artex:local .
#    BuildKit 只构建目标 stage 依赖的部分，① 不会触发这里的 web/go 编译 stage。

# ── 源码编译：前端静态导出 ─────────────────────────
FROM node:22-bookworm-slim AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build:static

# ── 源码编译：内嵌前端的 Go 单二进制 ────────────────
FROM golang:1.26-bookworm AS gobuild
# .dockerignore 排除了 .git，镜像内拿不到 git describe，版本号由构建参数传入。
# 缺省 dev：页面一键更新会对 dev 构建禁用，Docker 部署本就应通过重建镜像升级。
ARG ARTEX_VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/out ./server/webui/dist
RUN CGO_ENABLED=0 go build -tags embedui -trimpath \
      -ldflags "-s -w -buildid= -X main.version=${ARTEX_VERSION#v}" \
      -o /out/artex ./cmd/artex

# ── 运行时基础层（两种方式共用）─────────────────────
FROM python:3.12-slim-bookworm AS runtime
# 常用工具：ripgrep / curl / vim，加一批 recon 常备件（按需增删）。
# Node 从 NodeSource 装 20.x：bookworm 自带的 apt nodejs 是 18，Playwright 要求 >=20。
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates ripgrep curl wget vim git jq unzip \
      dnsutils iputils-ping netcat-openbsd inetutils-telnet whois nmap \
    && curl -fsSL https://deb.nodesource.com/setup_20.x | bash - \
    && apt-get install -y --no-install-recommends nodejs \
    && rm -rf /var/lib/apt/lists/*
# 预装 Playwright MCP 与 CLI（全局），运行时不再 npx 联网下载。
# @playwright/mcp：browser MCP 直接 `npx @playwright/mcp`（已全局装好，无需 -y/@latest）。
# @playwright/cli：提供 playwright-cli，装完顺带 --help 验证可执行。
# 再装 playwright（提供浏览器管理），装完用 --with-deps 预置 chromium 及其系统依赖，
# 这样容器内 MCP/CLI 首次启动即可用，不再联网下载浏览器。
RUN npm install -g @playwright/mcp@latest @playwright/cli@latest playwright@latest \
    && playwright-cli --help \
    && playwright install --with-deps chromium \
    && rm -rf /var/lib/apt/lists/*
WORKDIR /app
# 守护启动脚本：进程退出后按退出码决定是否重新拉起，页面一键更新靠它完成换装。
# 它同时负责把 SIGTERM 转发给 artex —— docker stop 只把信号发给 PID 1，
# 不转发的话 artex 收不到、做不了优雅关闭，10 秒后被 SIGKILL 硬杀。
COPY start.sh /app/start.sh
COPY skills/ /app/skills/
# data/（SQLite + jwt.key）持久化点
VOLUME ["/app/data"]
EXPOSE 8787 8788
ENTRYPOINT ["/app/start.sh"]
CMD ["-addr", ":8787", "-proxy", ":8788"]

# ── ② 源码编译出的二进制 ────────────────────────────
FROM runtime AS source
COPY --from=gobuild /out/artex /app/artex
RUN chmod +x /app/artex /app/start.sh

# ── ① 预编译二进制（默认 stage，保持在最后）──────────
FROM runtime AS prebuilt
ARG TARGETARCH
# 预编译好的对应架构二进制（dist/amd64/artex 或 dist/arm64/artex）
COPY dist/${TARGETARCH}/artex /app/artex
RUN chmod +x /app/artex /app/start.sh
