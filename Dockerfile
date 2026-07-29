# booksource-tool Dockerfile — 多阶段构建
#
# 构建：
#   docker build -t booksource-tool:latest .
#
# 运行：
#   docker run -v ./inbox:/data/inbox -v ./output:/output \
#     -v ./etc:/app/etc:ro booksource-tool:latest --config /app/etc/config.json

# ---- 构建阶段 ----
FROM golang:1.25-alpine AS builder

WORKDIR /src

# 单独缓存依赖
COPY go.mod go.sum ./
RUN go mod download

# 复制源码并构建
COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -a -installsuffix cgo -ldflags="-s -w" -o /out/booksource-tool .

# ---- 运行时阶段（distroless） ----
FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app

# 从构建阶段复制二进制
COPY --from=builder /out/booksource-tool /app/booksource-tool

# 准备运行时需要的目录（volumes 挂载）
# /data/inbox  - 监听目录输入
# /output      - 输出 JSON
# /app/etc     - 配置文件（read-only）

USER nonroot:nonroot

# 默认以 daemon 模式启动（通过环境变量覆盖）
ENV CONFIGOR_MONGO_URI=mongodb://mongo:27017
ENV BOOKSOURCE_OUTPUT_DIR=/output
ENV BOOKSOURCE_WATCH_DIR=/data/inbox

ENTRYPOINT ["/app/booksource-tool"]
CMD ["--config", "/app/etc/config.json"]


# 历史版本（保留参考）:
# 单层 alpine:3.22 COPY 方式已废弃 — 假设你已经 build 完，导致新手 clone 后跑不起来
# 现改为多阶段：clone 即可 docker build，不用先 make build