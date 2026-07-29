# deploy/ — systemd 部署

完整 30 分钟部署步骤。

## 0. 前置：MongoDB

```bash
# 方式 A：用 docker 起一个 mongo（推荐，跟主程序隔离）
docker run -d --name booksource-mongo \
  --restart unless-stopped \
  -p 127.0.0.1:27017:27017 \
  -v mongo_data:/data/db \
  mongo:6

# 验证
docker exec booksource-mongo mongosh --quiet --eval 'db.runCommand({ping:1}).ok'
```

主程序 systemd service 用 `MONGO_URI=mongodb://127.0.0.1:27017`。

## 1. 部署主程序 booksource-tool

```bash
# 1. 准备部署用户（不要用 root 跑）
sudo useradd -r -s /usr/bin/nologin booksource

# 2. 部署目录
sudo mkdir -p /opt/booksource-tool/{inbox,output,etc}
sudo chown -R booksource:booksource /opt/booksource-tool

# 3. 编译 + 复制二进制
cd /path/to/booksource-tool
make build
sudo install -m 0755 booksource-tool /usr/local/bin/booksource-tool

# 4. 复制配置示例 + 编辑
sudo cp etc/config.example.json /opt/booksource-tool/etc/config.local.json
sudo chown booksource:booksource /opt/booksource-tool/etc/config.local.json
sudo chmod 600 /opt/booksource-tool/etc/config.local.json
sudo -e /opt/booksource-tool/etc/config.local.json
#   至少填：
#     "watchDir": "/opt/booksource-tool/inbox",
#     "outputDir": "/opt/booksource-tool/output",
#     "mongoURI": "mongodb://127.0.0.1:27017"

# 5. 安装 systemd 单元
sudo cp deploy/booksource-tool.service /etc/systemd/system/
sudo systemctl daemon-reload

# 6. 启动 + 开机自启
sudo systemctl enable --now booksource-tool.service
sudo systemctl status booksource-tool.service
```

## 2. 验证

```bash
# 看日志
sudo journalctl -u booksource-tool -f

# 几秒后看到：
#   config loaded: keywords=N workers=80 timeout=5s interval=5m
#   run completed

# 验证 output 产出
ls -lh /opt/booksource-tool/output/

# 应该至少有：
#   usable_dedup_sources.json
#   app_usability_report.json
#   duplicate_report.json
#   connectivity_report.json
#   summary.json
```

## 3. 暴露 output 给 Legado

```bash
# 方式 A：python http server（最快）
# 注意只在内部网络开
sudo apt install -y nginx
sudo tee /etc/nginx/sites-available/booksource <<EOF
server {
    listen 8080;
    location / {
        root /opt/booksource-tool/output;
        autoindex on;
    }
}
EOF
sudo ln -sf /etc/nginx/sites-available/booksource /etc/nginx/sites-enabled/
sudo systemctl reload nginx

# 现在 Legado 网络导入 URL: http://host:8080/usable_dedup_sources.json
```

## 4. 可选：跨机发布（scp 到另一台服务器）

如果想让 output 自动 scp 到另一台（阿里云 filedown），装 timer：

```bash
sudo -u booksource mkdir -p /home/booksource/.ssh
sudo -u booksource ssh-keygen -f /home/booksource/.ssh/deploy_key -N ''
# 把公钥加到目标服务器 authorized_keys

sudo cp deploy/booksource-publish.sh /usr/local/bin/
sudo chmod +x /usr/local/bin/booksource-publish.sh
sudo -e /usr/local/bin/booksource-publish.sh   # 改 REMOTE_DIR / SSH_KEY

sudo cp deploy/booksource-publish.{service,timer} /etc/systemd/system/
sudo systemctl daemon-reload
sudo systemctl enable --now booksource-publish.timer
sudo systemctl list-timers booksource-publish.timer
```

不跨机就别装这个 timer，主程序 + nginx 暴露 output/ 就够。

## 5. 故障排查

| 症状 | 原因 | 解决 |
| --- | --- | --- |
| booksource-tool 启动后立刻退出 | MongoDB 没启 / URI 错 | `docker logs booksource-mongo`，检查 URI |
| Mongo 重启后 booksource-tool 不恢复 | Restart=on-failure 只在错误时重启 | Mongo 重启后 `systemctl restart booksource-tool` |
| output 目录没文件 | 第一次跑要等 5-10 分钟（探测全网源耗时） | `journalctl -u booksource-tool -f` 看进度 |
| Legado 网络导入失败 | output URL 不可达 / 配置路径错 | `curl http://host:8080/usable_dedup_sources.json` 验证 |