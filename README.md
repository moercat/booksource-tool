# booksource-tool

> Legado 书源探测服务 — 单台设备具备全部能力：探测源 + 提供文件下载

![Go Version](https://img.shields.io/badge/Go-1.25-blue)
![Status](https://img.shields.io/badge/Status-活跃-brightgreen)
![License](https://img.shields.io/badge/License-MIT-green)
[![CI](https://github.com/moercat/booksource-tool/actions/workflows/go.yml/badge.svg)](https://github.com/moercat/booksource-tool/actions/workflows/go.yml)
[![Release](https://github.com/moercat/booksource-tool/actions/workflows/release.yml/badge.svg)](https://github.com/moercat/booksource-tool/releases)

## 这是什么

**一台设备跑这个 = 完整的 Legado 书源工作流**：

1. **拉源** — 从本地文件 / HTTP URL / Git 仓库 / 监听目录自动拉取书源
2. **去重** — 按规范化后的 `searchUrl` 去重（不是简单 URL 去重）
3. **探测** — 对每个唯一搜索请求做多关键词 HTTP 搜索响应可信度探测
4. **评分** — 输出 `app_usability_report.json`，标记有风控/失效的源
5. **发布** — 常驻模式下增量探测，最终 JSON 直接通过 HTTP 服务给 Legado 导入

fork 这个项目后，只需在一台 Linux 机器上跑，就有"书源源池 + 文件下载"全功能。

## ✨ 核心能力

| 能力 | 说明 |
| --- | --- |
| **多源拉取** | 本地文件 / HTTP URL / Git 仓库 / 监听目录 |
| **智能去重** | 按 `searchUrl` 结构规范化后去重，非简单 URL 去重 |
| **搜索探测** | HEAD 快速过滤 + GET 复核，区分"真搜索响应"和"静态页" |
| **质量评分** | 多关键词样本搜索（默认 279 个关键词），输出风险报告 |
| **名称清洗** | 清 emoji / 质量标记 / 来源尾巴 |
| **增量调度** | 常驻模式增量探测，避免全量阻塞 |
| **Mongo 缓存** | 源状态 / 异常次数 / 探测结果持久化 |
| **GBK 支持** | 自动检测源编码（UTF-8 / GBK） |
| **退避策略** | 异常源间隔指数拉长，长期异常自动停止探测 |

## 🚀 快速开始

### 方式一：单设备部署（推荐）

部署在一台 Linux 机器上，**这一个进程就具备全部能力**：

```bash
# 1. 准备 MongoDB
docker run -d --name mongo -p 27017:27017 mongo:6

# 2. 复制配置
cp etc/config.example.json etc/config.local.json
vim etc/config.local.json   # 填你的源 URL / 输出路径

# 3. 启动常驻模式
make daemon
# 或前台跑：MONGO_URI=mongodb://localhost:27017 ./booksource-tool --daemon

# 4. 浏览器访问产出文件
ls -lh output/
#   usable_dedup_sources.json           ← Legado 直接导入
#   app_usability_report.json
#   duplicate_report.json
#   connectivity_report.json
#   summary.json

# 5. 把 output/ 通过 nginx / caddy / python -m http.server 暴露
# Legado 网络导入 URL: http://this-host:8080/usable_dedup_sources.json
```

### 方式二：一次性使用

```bash
go run . --input /path/to/shareBookSource.json --output-dir output
```

### 方式三：Docker

```bash
docker compose up -d mongodb
docker compose run --rm cleaner --input /data/shareBookSource.json \
  --output-dir /output \
  --retries 3 \
  --mongo-uri mongodb://mongodb:27017
```

## 🛠 生产部署（systemd）

`deploy/` 目录提供完整的 systemd 单元：

```bash
# 1. 部署二进制
make build
sudo cp booksource-tool /usr/local/bin/

# 2. 安装 systemd 单元
sudo cp deploy/booksource-tool.service /etc/systemd/system/
sudo cp deploy/booksource-tool.timer /etc/systemd/system/
sudo systemctl daemon-reload

# 3. 启用 timer（开机自启 + 每 2 小时跑一次）
sudo systemctl enable --now booksource-tool.timer
sudo systemctl enable booksource-tool.service

# 4. 看日志
sudo journalctl -u booksource-tool -f
```

配置定时：
- `OnBootSec=5min` — 开机 5 分钟后跑
- `OnUnitActiveSec=2h` — 每 2 小时跑一次

## 📊 输出文件

`output/` 目录产出 5 类 JSON：

| 文件 | 用途 |
| --- | --- |
| `usable_dedup_sources.json` | **主要产物** — Legado 网络导入用 |
| `app_usability_report.json` | 全量源 App 可用性风险报告 |
| `duplicate_report.json` | 重复搜索链接分组 |
| `connectivity_report.json` | 探测结果明细 |
| `summary.json` | 汇总统计 |

## 🏗 架构

```
┌─────────────────────────────────────┐
│  本机进程 booksource-tool           │
│                                     │
│  ┌─────────┐  ┌──────┐  ┌────────┐  │
│  │ fetch   │→ │ dedup │→ │ probe  │  │
│  │ 拉源   │  │ 去重  │  │ 探测   │  │
│  └─────────┘  └──────┘  └────────┘  │
│       ↓              ↓        ↓      │
│       └──────────────┴────────┘      │
│                    ↓                  │
│             ┌──────────────┐          │
│             │   MongoDB    │          │
│             │   源池缓存    │          │
│             └──────────────┘          │
│                    ↓                  │
│             output/*.json             │
└────────────────────┬────────────────┘
                     │ HTTP / 文件下载
                     ↓
            ┌─────────────────┐
            │  Legado 客户端   │
            │  网络导入源 URL  │
            └─────────────────┘
```

## 🔑 关键设计

- **书源以 `searchUrl` 为唯一标识**（不是 `bookSourceUrl`）—— 同样搜索逻辑的源归一
- **探测分两级**：HEAD 快速过滤 → GET 复核
- **退避策略**：异常越久间隔越长，长期异常自动停止探测（避免无效请求浪费）
- **增量调度**：常驻模式只探测新源或失败重试，不全量跑
- **GBK 兼容**：自动检测编码，GBK 源也能正确清洗

## 🧰 常用命令

```bash
make help        # 所有命令
make build       # 编译
make run         # 一次性使用
make daemon      # 常驻模式（要 MongoDB）
make test        # 跑测试
make vet         # 静态检查
make docker      # 构建 Docker 镜像
make up          # docker compose up -d
make down        # 停
make logs        # 看日志
make release TAG=v1.0.0   # 打 tag 触发自动发布
```

## 🛠 技术栈

| 组件 | 选型 |
| --- | --- |
| 语言 | Go 1.25 |
| 存储 | MongoDB（去重 / 探测结果） |
| 部署 | systemd timer |
| 容器 | Docker + docker-compose |
| 解析 | goquery（HTML）+ go.mongodb.org/mongo-driver |
| 编码 | 自动 UTF-8 / GBK 检测 |

## 📦 状态

| 维度 | 说明 |
| --- | --- |
| 当前阶段 | v1.x 稳定运行 |
| 最后更新 | 2026-07 |
| 活跃度 | ✅ 活跃（生产在用） |
| 部署 | 单 Linux 机器 + MongoDB |

## 🔮 后续方向

- [ ] 支持更多搜索引擎的探测兼容
- [ ] 优化 @js 规则的处理能力（当前会降级）
- [ ] Web 管理界面查看源池状态
- [ ] 更丰富的质量评分维度