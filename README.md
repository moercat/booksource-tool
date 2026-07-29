# booksource-tool

> Legado 书源探测服务 — 单台 Linux 设备具备全部能力：自动爬源 + 探测 + 输出可导入的 JSON

![Go Version](https://img.shields.io/badge/Go-1.25-blue)
![Status](https://img.shields.io/badge/Status-活跃-brightgreen)
![License](https://img.shields.io/badge/License-MIT-green)
[![CI](https://github.com/moercat/booksource-tool/actions/workflows/go.yml/badge.svg)](https://github.com/moercat/booksource-tool/actions/workflows/go.yml)
[![Release](https://github.com/moercat/booksource-tool/actions/workflows/release.yml/badge.svg)](https://github.com/moercat/booksource-tool/releases)
[![Go Reference](https://pkg.go.dev/badge/github.com/moercat/booksource-tool.svg)](https://pkg.go.dev/github.com/moercat/booksource-tool)

## 这是什么

**一台设备跑这个 = 完整的 Legado 书源工作流**：

1. **拉源** — 从本地文件 / HTTP URL / Git 仓库 / 监听目录自动拉取书源
2. **去重** — 按规范化后的 `searchUrl` 去重（不是简单 URL 去重）
3. **探测** — 对每个唯一搜索请求做多关键词 HTTP 搜索响应可信度探测
4. **评分** — 输出 `app_usability_report.json`，标记有风控/失效的源
5. **产出** — `output/usable_dedup_sources.json` 通过 nginx / caddy / python http.server 暴露给 Legado 网络导入

fork 这个项目后，只需在一台 Linux 机器上跑，就有"书源源池 + HTTP 文件下载"全功能。

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

## 🚀 30 秒跑通

```bash
git clone https://github.com/moercat/booksource-tool.git
cd booksource-tool
cp etc/config.example.json etc/config.local.json
make run
```

`etc/config.example.json` 已含生产在用的真实书源列表页 URL，**fork 用户无需改任何配置即可启动**。

想换源？改 `config.local.json` 的 `remoteUrls` / `sources` / `watchDir`。

## 🛠 完整安装

### 方式一：单设备部署（推荐）

```bash
# 1. 准备 MongoDB
docker run -d --name mongo -p 27017:27017 mongo:6

# 2. 复制配置
cp etc/config.example.json etc/config.local.json

# 3. 编译
make build

# 4. 常驻模式
MONGO_URI=mongodb://localhost:27017 make daemon
# 或：./booksource-tool --daemon --config etc/config.local.json

# 5. 部署 HTTP 文件下载（任选一种）
# nginx: location /output/ { alias /opt/booksource-tool/output/; }
# caddy:   handle_path /output/* { file_server { root /opt/booksource-tool/output } }
# python:  cd output && python3 -m http.server 8080

# 6. Legado 网络导入 URL：http://this-host/usable_dedup_sources.json
```

### 方式二：一次性使用

```bash
go run . --input /path/to/shareBookSource.json --output-dir output
```

### 方式三：Docker

```bash
docker compose up -d mongodb
docker compose run --rm cleaner --input /data/shareBookSource.json \
  --output-dir /output --retries 3 --mongo-uri mongodb://mongodb:27017
```

### 方式四：systemd（生产长期跑）

```bash
# 1. 部署二进制
make build
sudo cp booksource-tool /usr/local/bin/

# 2. 安装 systemd 单元
sudo useradd -r -s /usr/bin/nologin booksource   # 创建非 root 用户
sudo mkdir -p /opt/booksource-tool /opt/booksource-tool/inbox /opt/booksource-tool/output
sudo cp etc/config.example.json /opt/booksource-tool/etc/config.local.json
sudo chown -R booksource:booksource /opt/booksource-tool
sudo chmod 600 /opt/booksource-tool/etc/config.local.json
sudo cp deploy/booksource-tool.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now booksource-tool.service

# 3. 看日志
sudo journalctl -u booksource-tool -f

# 4. 故障排查
sudo systemctl status booksource-tool
sudo journalctl -u booksource-tool -n 200
docker logs mongo
```

完整 systemd 部署 + 故障排查见 [deploy/README.md](./deploy/README.md)。

## 📊 输出文件

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
│  │ 拉源    │  │ 去重  │  │ 探测   │  │
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
make build       # 编译二进制
make run         # 一次性使用（需 --input 或配置）
make daemon      # 常驻模式（要 MongoDB）
make test        # 跑测试
make cover       # 跑测试 + coverage.html
make vet         # 静态检查
make docker      # 构建 Docker 镜像
make up          # docker compose up -d
make down        # docker compose down
make logs        # 看 docker logs
make release TAG=v1.0.0   # 打 tag 触发 GitHub Actions
make snapshot    # goreleaser 本地试发布（不发 GitHub）
```

## 🛠 技术栈

| 组件 | 选型 |
| --- | --- |
| 语言 | Go 1.25 |
| 存储 | MongoDB（去重 / 探测结果） |
| 部署 | systemd / Docker + docker-compose / nginx |
| 解析 | goquery（HTML）+ go.mongodb.org/mongo-driver |
| 编码 | 自动 UTF-8 / GBK 检测 |
| 发布 | GitHub Actions + goreleaser + ghcr.io |

## 📦 状态

| 维度 | 说明 |
| --- | --- |
| 当前阶段 | v1.x 稳定运行 |
| 最后更新 | 2026-07 |
| 活跃度 | ✅ 活跃（生产在用） |
| 部署 | 单 Linux 机器 + MongoDB |
| 单测 | 50+ 用例覆盖核心模块（9.2% 行覆盖） |
| CI | GitHub Actions（go vet + test + docker build） |
| Release | goreleaser 自动化（tag → 多平台二进制 + Docker 镜像） |

## 🆘 故障排查

| 症状 | 原因 | 解决 |
| --- | --- | --- |
| `--watch-dir requires --mongo-uri` 错误 | 没连 MongoDB | `docker run -d --name mongo -p 27017:27017 mongo:6` |
| 输出文件没变化 | 探测还在第一轮 | 5-10 分钟后再看 |
| `output/usable_dedup_sources.json` 为空 | 探测全失败 / 源全部失效 | `cat output/connectivity_report.json` 看明细 |
| systemd 频繁重启 | restart=on-failure 默认 5 分钟 10 次限制 | `journalctl -u booksource-tool -n 100` 看 panic 错误 |
| Legado 导入 0 源 | HTTP 文件下载路径配置错 | `curl http://host/usable_dedup_sources.json` 验证 |
| Mongo 连接被拒 | `MONGO_URI` 没注入 | systemd service 加 `Environment=MONGO_URI=...` 或 `daemon` flag 从 `--daemon` 自动读 |

## 🔮 后续方向

已完成（v1.x 阶段）：

- [x] README 钩子 + 实际状态对齐
- [x] Go version 标准化（1.25）
- [x] GitHub Actions CI + Dependabot
- [x] Release workflow + goreleaser（多平台二进制 + Docker 多架构）
- [x] Docker 多阶段 + docker-compose 接入 mongo（带 healthcheck）
- [x] systemd unit（booksource-tool.service + Restart=on-failure fail-safe）
- [x] 核心单测（name / source_filter / target 三模块 50+ 用例）
- [x] etc/config.example.json 填真实生产 URL（fork 用户零配置启动）
- [x] `--daemon` CLI flag + 环境变量自动接入（MONGO_URI / WATCH_DIR / OUTPUT_DIR）
- [x] 故障排查 README 段

未完成（v2.0 候选）：

- [ ] Release v1.0.0 tag（代码已成熟，下一次手动时机）
- [ ] Web 管理界面查看源池状态
- [ ] 更丰富的质量评分维度（关键词矩阵 / 响应时间统计）
- [ ] 支持更多搜索引擎的探测兼容
- [ ] 优化 @js 规则的处理能力（当前会降级）
- [ ] 输出文件支持更多格式（YAML / TOML）
- [ ] 指标暴露（Prometheus /metrics）

## 🤝 贡献指南

提交信息请遵循 [Conventional Commits](https://www.conventionalcommits.org/)：

```
feat: 加新功能
fix:  修 bug
docs: 文档改动
refactor: 重构
test: 测试改动
ci:   CI 改动
```

新功能或 Bug 修复请先开 issue 讨论。

## 📄 许可证

MIT
