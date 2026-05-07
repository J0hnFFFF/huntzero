# ─── Stage 0: Build Web UI (Vue 3 + Vite) ────────────────────────────────────
FROM node:22-slim AS frontend
WORKDIR /build
COPY web_ui_v2/package.json web_ui_v2/package-lock.json ./
RUN npm ci --silent
COPY web_ui_v2/ ./
RUN npm run build
# 产物在 /build/../web_ui/dist/ → 即 /web_ui/dist/

# ─── Stage 1: Python base ────────────────────────────────────────────────────
FROM python:3.12-slim AS base

# 系统依赖（git 用于 clone 目标仓库；build-essential 用于 tree-sitter 编译；nodejs/npm 用于安装 kimi cli）
RUN apt-get update && apt-get install -y --no-install-recommends \
    git \
    build-essential \
    curl \
    nodejs \
    npm \
    && rm -rf /var/lib/apt/lists/*

# 全局安装 kimi cli
RUN pip install --no-cache-dir kimi-cli

# 构建参数：可在 docker build --build-arg 中覆盖
ARG INSTALL_TREE_SITTER=true

WORKDIR /app

# ─── Stage 2: 安装 Python 依赖 ───────────────────────────────────────────────
COPY requirements.txt ./

# 核心依赖
RUN pip install --no-cache-dir -r requirements.txt

# 可选语义分析依赖（tree-sitter）
RUN if [ "$INSTALL_TREE_SITTER" = "true" ]; then \
      pip install --no-cache-dir \
        "tree-sitter>=0.23" \
        tree-sitter-python \
        tree-sitter-javascript \
        tree-sitter-typescript \
        tree-sitter-c \
        tree-sitter-go \
        tree-sitter-rust \
        tree-sitter-java; \
    fi

# Redis 异步库（Worker 模式需要）
RUN pip install --no-cache-dir "redis[asyncio]>=5.0"

# ─── 安装 OSV-Scanner ────────────────────────────────────────────────────────
ARG OSV_VERSION="v2.0.0"
ARG TARGETARCH
RUN curl -fsSL -o /tmp/osv-scanner.tar.gz \
      "https://github.com/google/osv-scanner/releases/download/${OSV_VERSION}/osv-scanner_${OSV_VERSION}_linux_${TARGETARCH}.tar.gz" \
    && tar -xzf /tmp/osv-scanner.tar.gz -C /usr/local/bin --strip-components=1 \
    && chmod +x /usr/local/bin/osv-scanner \
    && rm -f /tmp/osv-scanner.tar.gz \
    && osv-scanner --version

# ─── Stage 3: 拷贝项目文件 ───────────────────────────────────────────────────
COPY engine/        ./engine/
COPY server/        ./server/
COPY skills/        ./skills/
COPY tools/         ./tools/
COPY kimi.py        ./
COPY kimi_hive.py   ./
# 前端静态产物（来自 Stage 0 构建）
COPY --from=frontend /web_ui/dist/ ./web_ui/dist/
COPY .bots.md       ./

# ─── 环境变量默认值（可在 docker run -e 中覆盖）──────────────────────────────
ENV PYTHONUNBUFFERED=1
ENV KIMI_API_KEY=""
ENV KIMI_BASE_URL="https://api.kimi.com/coding/v1"
ENV KIMI_REDIS_URL="redis://redis:6379/0"
ENV KIMI_WORK_DIR="/data/workspace"
ENV KIMI_WORKERS="5"

# 工作目录挂载点（存放分析产物和 Blackboard 文件）
VOLUME ["/data/workspace"]

# ─── 默认启动：Worker 模式（使用统一入口 kimi.py）────────────────────────────
# 以 Redis 队列方式运行，等待集群调度器下发目标。
# 其他启动模式：
#   docker run kimisec python kimi.py scan <target>
#   docker run kimisec python kimi.py serve --mcp-port 9999 --http-port 8080
CMD python kimi.py worker \
    --redis "${KIMI_REDIS_URL}" \
    --work-dir "${KIMI_WORK_DIR}"

# ─── 元数据标签 ──────────────────────────────────────────────────────────────
LABEL org.opencontainers.image.title="kimiSec Hive-Mind Worker"
LABEL org.opencontainers.image.description="Autonomous 0-day vulnerability research node"
LABEL org.opencontainers.image.version="9.0.0"
