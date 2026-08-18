# mhed-platform

MHED 是面向 Monster Hunter 存档修改器的在线配装平台。仓库统一维护 API、下载介绍页、
管理后台、接口契约和 Docker 部署；MH3G、MH4G、MHGU/XX 修改器通过稳定的桌面 API 接入。

## 技术栈

- API：Go、`chi`、`pgx`、REST JSON、OpenAPI 3.1。
- 在线业务数据：PostgreSQL 17。
- 游戏静态资料：按游戏和数据版本发布的只读 SQLite。
- Web：Astro 静态站点。
- Admin：React、Vite、Ant Design。
- 部署：Docker Compose，宿主机 Nginx + Certbot 终止 HTTPS。

在线平台只接收标准化逻辑配装，不接收完整存档、角色名、平台偏移或原始装备字节。

## 当前进度

截至 2026-08-18，公开大厅、MH3G 修改器接入、账号与昵称、发布查重、点赞、举报、角色权限和
管理后台均已完成。邮箱注册、邮箱验证、邮箱登录、绑定邮箱、密码找回和邮件发送记录随
`v1.3.0-beta.2` 发布；生产邮件服务由超级管理员配置并启用。

邮件接入、AOKSend 官方接口和安全配置见 [邮箱账号与 AOKSend 接入](docs/EMAIL_ACCOUNT.md)。

## 工作区

```text
api/         Go API、数据库迁移和查询
web/         下载与项目介绍页
admin/       管理后台
contracts/   OpenAPI 唯一接口契约
game-data/   游戏数据 manifest 与跨实现测试夹具
deploy/      Compose、静态前端容器和宿主机 Nginx 模板
docs/        架构、开发和部署文档
```

## 本地启动

复制环境变量模板、导入经过校验的 MH3G 数据，然后启动服务：

```bash
cp .env.example .env
cd api
go run ./cmd/mhedctl import-game-data --source ../../mh3u-se/data/mh3g.sqlite --manifest ../../mh3u-se/data/manifest.json --destination ../game-data/runtime
cd ..
docker compose --profile ops run --rm game-data-import
docker compose up --build
```

首次启动后引导创建超级管理员：

```bash
docker compose run --rm --no-deps --user "$(id -u):$(id -g)" \
  --entrypoint /mhedctl -v "$PWD:/workspace" api \
  bootstrap-super-admin --username alex_admin --password-file /workspace/test-password
```

`test-password` 只在本地保留，权限为 `0600` 且已忽略于 Git。首次登录后后台会强制修改密码。

默认只监听本机回环地址：

- API：`http://127.0.0.1:18100`
- Web：`http://127.0.0.1:18101`
- Admin：`http://127.0.0.1:18102`

检查服务：

```bash
curl http://127.0.0.1:18100/health/live
curl http://127.0.0.1:18100/health/ready
```

当前 Compose 按 4 核 4G 服务器设置均衡限额：常驻服务上限约 2.3 CPU、1.47 GiB 内存，
不计一次性迁移和游戏数据导入。数据库、API 和前端仍与同机其他服务保持资源隔离。

生产入口为 `mhed.web.65h26i.top`、`mhed.api.65h26i.top`、`mhed.admin.65h26i.top` 和
`mhed.desk.65h26i.top`。应用、PostgreSQL、游戏数据和每日备份均运行在独立 `mhed` Compose
项目或 Docker named volume 中；宿主机不安装 Go、Node.js 或 PostgreSQL。

开发顺序和阶段验收以 [配装大厅实施计划](docs/IMPLEMENTATION_PLAN.md) 为准。其他说明见
[架构](docs/ARCHITECTURE.md)、[开发](docs/DEVELOPMENT.md)和[部署](docs/DEPLOYMENT.md)。
