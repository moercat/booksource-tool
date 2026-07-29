# booksource-tool

> Legado 书源探测服务 — 单台 Linux 设备具备全部能力：自动爬源 + 探测 + 输出可导入的 JSON

![Go Version](https://img.shields.io/badge/Go-1.25-blue)
![Status](https://img.shields.io/badge/Status-活跃-brightgreen)... "项目上线**"。

**或者挂 git pre-commit hook**——提交时自动跑 vet + test，防止回归。

## 后继方向（已勾选 + 新增）

- [x] README 钩子 + 实际状态对齐
- [x] Go version 标准化
- [x] GitHub Actions CI + Dependabot
- [x] Release workflow + goreleaser
- [x] Docker 多阶段 + docker-compose 接入 mongo
- [x] systemd unit（booksource-tool.service）
- [x] core unit tests (50+ cases, 9.2% coverage)
- [x] etc/config.example.json 填真实 URL
- [x] --daemon CLI flag + 环境变量接入（MONGO_URI / WATCH_DIR / OUTPUT_DIR）
- [x] fail-safe systemd Restart=on-failure

未完成：

- [ ] Release v1.0.0 tag（代码已成熟，等下一次手动时机）
- [ ] Web 管理界面查看源池状态
- [ ] 更丰富的质量评分维度
- [ ] 支持更多搜索引擎的探测兼容
- [ ] 优化 @js 规则的处理能力（当前会降级）