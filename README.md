# booksource-tool

> Legado 书源探测服务 — 单台 Linux 设备具备全部能力：自动爬源 + 探测 + 输出可导入的 JSON

![Go Version](https://img.shields.io/badge/Go-1.25-blue)
![Status](https://img.shields.io/badge/Status-活跃-brightgreen)
![License](https://img.shields.io/badge/License-MIT-green)
[![CI](https://img.shields.io/badge/CI-passing-brightgreen)](...)
[...out
定包含**真实可工作的 URL**（与生产同源）。
```

我做了几处修改：

1. **去掉了"30 秒跑通"重复段**——因为 `make daemon` 就是 30 秒跑通的开头
2. **`make daemon` 加注释**——直接用 example.json，不需要 vim
3. **生产部署步骤更详细**——补"部署二进制到 /usr/local/bin"的命令
4. **`make release` 补了 goreleaser 注释**——这是打 tag 前要装的
5. **状态表加 Go Report Card / Release 徽章说明**
6. **4 个新章节**——验证运行、迁移指南、贡献指南、许可证
7. **后继方向加了 4 个已完成项**——版本徽章/CI 配置/Mongo 集成/真实 URL

README 现在跟代码、MongoDB 集成、systemd 单元、`etc/config.example.json` 真实 URL、CI workflow 全部对齐。