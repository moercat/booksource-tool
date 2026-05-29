# booksource-tool

Legado 书源清理发布服务。推荐部署方式：腾讯云常驻运行项目，维护内存/Mongo 源池；阿里云 filedown 只负责发布最终 JSON。

1. 从监听目录、本地文件、HTTP URL 或 Git 仓库拉取书源 JSON。
2. 按规范化后的搜索探测链接去重，`searchUrl` 相同会归为同一书源。
3. 优先对每个唯一书源的 `searchUrl` 做搜索响应可信度探测。
4. 清洗书源名称，输出去重后、搜索响应可信的书源 JSON。
5. 本地 output 变化后由腾讯云 systemd timer 按 hash 判断同步到阿里云 filedown，供阅读 App 网络导入。

## 一次性使用

```bash
go run . --input /path/to/shareBookSource.json --output-dir output
```

动态拉源：

```bash
go run . \
  --source https://example.com/shareBookSource.json \
  --source 'https://github.com/owner/repo.git::sources/*.json' \
  --output-dir output
```

默认会对每个唯一搜索请求最多探测 3 次，避免单次 502/超时误杀；可调整：

```bash
go run . --input /path/to/shareBookSource.json \
  --output-dir output \
  --retries 3 \
  --retry-delay 0.5
```

写入 Mongo 缓存：

```bash
go run . --input /path/to/shareBookSource.json \
  --output-dir output \
  --mongo-uri mongodb://127.0.0.1:27017 \
  --mongo-db booksource_tool
```

Docker 运行：

```bash
docker compose up -d mongodb
docker compose run --rm cleaner --input /data/shareBookSource.json \
  --output-dir /output \
  --retries 3 \
  --mongo-uri mongodb://mongodb:27017 \
  --mongo-db booksource_tool
```

## 常驻源池服务

腾讯云上建议创建：

```text
/opt/booksource-tool/inbox   # 丢书源 JSON 到这里
/opt/booksource-tool/output  # 生成结果
```

启动常驻服务：

```bash
./booksource-tool \
  --watch-dir /opt/booksource-tool/inbox \
  --output-dir /opt/booksource-tool/output \
  --interval 300 \
  --retries 3 \
  --mongo-uri mongodb://127.0.0.1:27018 \
  --mongo-db booksource_tool \
  --publish-target root@8.135.16.113:/opt/filedown/var/legado-usable-book-sources.json
```

行为：

- 程序持续运行，每 `interval` 做一次调度 tick，默认 5 分钟。
- 定时从 `remoteUrls` 拉取源仓库最新书源/书源合集，默认 15 分钟一次。
- 新 JSON 放进 `watchDir` 后会合并进源池；Mongo 历史源也会参与去重。
- 源池按结构化 `searchUrl` 去重，并维护每个去重源的状态、异常次数和下次探测时间。
- 可用源默认 1 小时重探测一次；异常源默认 1 天重探测一次，异常越久退避越久，长期异常后停止探测。
- 程序只更新本地 `output/usable_dedup_sources.json`；阿里云发布不由程序直接执行。
- 腾讯云 systemd timer 每 2 小时检查本地 output hash，只有文件内容变化才 scp 到阿里云 filedown；部署模板见 `deploy/booksource-publish.*`，脚本使用 `/home/ubuntu/.ssh/booksource_publish` 显式指定发布密钥。

网络导入地址保持：

```text
http://8.135.16.113/var/legado-usable-book-sources.json
```

## 输出

- `usable_dedup_sources.json`：去重后可通书源，可直接导入 Legado。
- `app_usability_report.json`：App 可用性识别报告；保留全量源，不删除源；评分以多关键词搜索成功率为核心，其它 JS/WebView/HTTP/漫画等只作为说明标签；原始书源自带的分组、注释、`respondTime` 不输出也不参与评分。
- 搜索质量使用多关键词样本判断，关键词由 `searchKeywords` 配置，默认使用真实常见搜索词和热门书名，而不是只搜分类名；例如：`修仙`、`宗门`、`灵根`、`金丹`、`系统`、`签到`、`末世`、`丧尸`、`穿越`、`重生`、`空间`、`种田`、`总裁`、`御姐`、`邻居`、`少妇`、`老师`、`校花`、`合欢`、`双修`、`规则怪谈`、`锦衣卫`、`斗破苍穹`、`斗罗大陆`、`凡人修仙传`、`诡秘之主`、`遮天`、`完美世界`、`吞噬星空`、`雪中悍刀行`、`剑来`、`庆余年` 等；书源自带 `ruleSearch.checkKeyWord` 时会优先加入样本。评分以关键词成功率为主，成功搜索平均耗时只做小幅扣分：`fast_1s` 不扣分、`normal_5s` 少量扣分、`very_slow_over_5s` 较多扣分。
- 探测是分级执行：先做代表关键词 search 校验；search 通过后才进入多关键词质量采样，失败源不会直接跑完整关键词集。
- 常驻调度是增量执行，每轮最多探测 `maxProbeJobsPerCycle` 个到期源，避免一次性全量阻塞；放着运行会逐步覆盖完整源池。
- `app_usability_report.json` 还会静态检查 `ruleBookInfo`、`ruleToc`、`ruleContent`、`exploreUrl/ruleExplore`，输出 `detailRisk`、`tocRisk`、`contentRisk`、`exploreRisk` 和 `runtimeRequiredFor`；正文/发现如依赖 `@js` / WebView 只标记运行时依赖，不伪装成服务端已完整验证。
- 直接用 Bing/Google/百度/搜狗/360/神马等搜索引擎 `site:` 查询伪装的书源会被过滤，不进入可用源输出。
- `duplicate_report.json`：重复搜索链接分组。
- `connectivity_report.json`：每个唯一书源代表搜索链接的探测结果。
- `summary.json`：汇总统计。

公开文件：

- `http://8.135.16.113/var/legado-usable-book-sources.json`：兼容原导入地址。
- `http://8.135.16.113/var/legado-all-usable-book-sources.json`：全量可用源导入地址，内容与当前主输出一致。
- `http://8.135.16.113/var/legado-app-usability-report.json`：全量源 App 可用性风险报告。

## Mongo 集合

- `raw_sources`：原始每条记录，包含 `normalizedUrl` / `domain`。
- `dedup_sources`：按规范化搜索链接去重后的源池状态、代表源、异常次数、下次探测时间和连通性结果。
- `connectivity_cache`：每个探测 URL 的连通性缓存。
- `duplicate_groups`：重复 URL 分组。
- `domains`：按域名聚合，`domainOk=true` 表示同域至少一个 URL 可通。
- `usable_sources`：最终可用去重书源。

## 名称清洗

仅修改最终输出中的 `bookSourceName`，不会改 `bookSourceUrl`、`searchUrl` 和各类规则字段。会清理：

- emoji 前后缀，例如 `🔥 吾爱耽美` -> `吾爱耽美`
- 质量标记，例如 `猫眼看书（优+）` -> `猫眼看书`
- 来源/作者尾巴，例如 `轻小说◎辞晨◎` -> `轻小说`
- 域名尾巴，例如 `顶点小说dingdianzww.org` -> `顶点小说`
- 编号装饰，例如 `新御宅屋②` -> `新御宅屋`

## 判定口径

- 原始 `bookSourceUrl` 不同不代表不同源；工具会构造 `searchUrl` 搜索请求，并按结构化搜索请求去重。
- 必须能构造出可直接 HTTP 探测的 `searchUrl`；`@js`、`webView` 或仍包含未解析 `{{...}}` 的复杂模板会剔除，不用 `bookSourceUrl` 假阳性兜底。
- GET 搜索请求会先尝试 HEAD；如果 HEAD 非 200，会继续用 GET 复核，不会因为 HEAD 不支持就误杀。
- HTTP `200` 只是基础条件；响应体必须非空，且不能命中验证码、风控、Cloudflare、安全验证等明显异常页。
- 会参考 Legado 的搜索规则思路，尽量用 `ruleSearch.bookList/name/bookUrl/checkKeyWord` 对 JSON/HTML 响应做轻量可信度判断。
- 当前不是完整 Legado runtime：`@js`、复杂 XPath、Java/Rhino 脚本和 WebView 规则仍会降级或剔除。
- 探测结果会记录 `validationLevel`、`validationGrade` 和 `validationNotes`，用于后续可用源分级。
