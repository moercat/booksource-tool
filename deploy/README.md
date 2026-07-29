# deploy/ — systemd 部署

部署两件事：

1. **booksource-tool.service** — 主程序常驻（守护）
2. **booksource-publish.service + .timer** — 可选：把 output scp 到另一台

## 1. booksource-tool.service（主程序）

```bash
# 创建用户
sudo useradd -r -s /usr/bin/nologin booksource

# 部署
sudo mkdir -p /opt/booksource-tool/{inbox,output,etc}
sudo cp booksource-tool /usr/local/bin/
sudo cp etc/config.local.json /opt/booksource-tool/etc/
sudo chown -R booksource:booksource /opt/booksource-tool
sudo chmod 600 /opt/booksource-tool/etc/config.local.json

# 启动
sudo cp deploy/booksource-tool.service /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now booksource-tool.service

# 看日志
sudo journalctl -u booksource-tool -f
```

依赖：
- MongoDB（本地 `mongod` 或单独容器）
- `/opt/booksource-tool/inbox` 目录（监听）
- `/opt/booksource-tool/output` 目录（输出 JSON）

## 2. booksource-publish.service + .timer（可选）

每 2 小时把 `output/usable_dedup_sources.json` scp 到另一台服务器：

```bash
# 配置 SSH key（deploy 用户）
sudo -u booksource mkdir -p /home/booksource/.ssh
sudo -u booksource ssh-keygen -f /home/booksource/.ssh/deploy_key -N ''
# 把公钥加到目标服务器的 authorized_keys

# 部署脚本
sudo cp deploy/booksource-publish.sh /usr/local/bin/
sudo chmod +x /usr/local/bin/booksource-publish.sh
sudo -e /usr/local/bin/booksource-publish.sh   # 改 REMOTE_DIR / SSH_KEY

# 启动 timer
sudo cp deploy/booksource-publish.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now booksource-publish.timer
sudo systemctl list-timers booksource-publish.timer
```

如果不需要跨机发布，**不要装** booksource-publish.timer——只装 booksource-tool.service + nginx/caddy 暴露 output/ 即可。

## 完整部署流程

```
一台 Linux 机器
├── MongoDB (mongod 或 docker)
├── booksource-tool.service (常驻)
└── nginx / caddy / python -m http.server (暴露 output/)
       ↓
   Legado 网络导入：http://host/output/usable_dedup_sources.json
```

更详细的部署文档见 README.md § 生产部署。