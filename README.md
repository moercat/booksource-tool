# booksource-tool

> Legado 书源清理探测发布服务

![Go Version](https://img.shields.io/badge/Go-1.25-blue)
![Status](https://img.shields.io/badge/Status-活跃-brightgreen)
![License](https://img.shields.io/badge/License-MIT-green)

## 定位

**解决了什么问题**：Legado 阅读 APP 的书源质量参差不齐，大量重复、失效、风控源。本工具自动拉取书源 → 去重 → 探测可用性 → 发布可用源，保持一个高质量的书源池。

**部署方式**：腾讯云常驻运行（Mongo 源池），阿里云 filedown 发布最终 JSON。

## 快速开始

### 一次性使用

```bash
git clone https://github.com/moercat/booksource-tool.git
cd booksource-tool
go run . --input /path/to/shareBookSource.json --output-dir output
```

### Docker 运行

```bash
docker compose up -d mongodb
docker compose run --rm cleaner --input /data/shareBookSource.json \
  --output-dir /output \
  --retries 3 \
  --mongo-uri mongodb://mongodb:27017
```

环境要求：Go 1.25+, Docker（可选）。

## 核心能力

| 功能 | 说明 |
|------|------|
| **多源拉取** | 支持本地文件、HTTP URL、Git 仓库、监听目录 |
| **智能去重** | 按 `searchUrl` 结构规范化后去重，非简单 URL 去重 |
| **搜索探测** | 对每个唯一 `searchUrl` 做 HTTP 搜索响应可信度探测 |
| **质量评分** | 多关键词样本搜索，输出 `app_usability_report.json` |
| **名称清洗** | 清理 emoji、质量标记、来源尾巴等 |
| **增量调度** | 常驻模式下增量探测，避免全量阻塞 |
| **Mongo 缓存** | 去重源状态、异常次数、探测结果持久化 |

## 架构

```
booksource-tool/
├── main.go              ← 入口（CLI + 常驻调度）
├── internal/
│   ├── collector/       ← 输入源采集（本地/HTTP/Git）
│   ├── dedup/           ← searchUrl 去重逻辑
│   ├── probe/           ← HTTP 探测引擎
│   ├── quality/         ← 多关键词质量评分
│   ├── cleaner/         ← 名称清洗
│   ├── mongo/           ← MongoDB 缓存层
│   └── publish/         ← 输出发布
├── deploy/              ← 部署配置（systemd timer）
└── docs/                ← 文档
```

**关键设计**：
- 书源以 `searchUrl` 为唯一标识，非 `bookSourceUrl`
- 探测分两级：先 HEAD 快速过滤，再 GET 复核
- 异常源退避策略：异常越久间隔越长，长期异常停止探测
- 发布走 hash 判断：只有 output 变化才 scp 到阿里云

## 输出文件

| 文件 | 说明 |
|------|------|
| `usable_dedup_sources.json` | 去重后可通书源，可直接导入 Legado |
| `app_usability_report.json` | 全量源 App 可用性风险报告 |
| `duplicate_report.json` | 重复搜索链接分组 |
| `connectivity_report.json` | 探测结果明细 |
| `summary.json` | 汇总统计 |

## 技术栈

| 组件 | 选型 |
|------|------|
| 语言 | Go 1.25 |
| 存储 | MongoDB |
| 部署 | 腾讯云 systemd + Docker |
| 发布 | 阿里云 filedown（HTTP） |

## 状态

| 维度 | 说明 |
|------|------|
| 当前阶段 | v1.0 稳定运行 |
| 最后更新 | 2026-06-12 |
| 活跃度 | ✅ 活跃（生产在用） |
| 部署 | 腾讯云常驻 + 阿里云发布 |

## 后续方向

- [ ] 支持更多搜索引擎的探测兼容
- [ ] 优化 @js 规则的处理能力（当前会降级）
- [ ] Web 管理界面查看源池状态
- [ ] 更丰富的质量评分维度
