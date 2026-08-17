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

当前 Compose 为 2核2G 测试服务器设置了约 1 CPU、768MB 内存的总硬上限（不计一次性迁移）。服务器升级后只需
调整资源额度，不需要更换数据库或应用架构。

生产入口为 `mhed.web.65h26i.top`、`mhed.api.65h26i.top`、`mhed.admin.65h26i.top` 和
`mhed.desk.65h26i.top`。应用、PostgreSQL、游戏数据和每日备份均运行在独立 `mhed` Compose
项目或 Docker named volume 中；宿主机不安装 Go、Node.js 或 PostgreSQL。

开发顺序和阶段验收以 [配装大厅实施计划](docs/IMPLEMENTATION_PLAN.md) 为准。其他说明见
[架构](docs/ARCHITECTURE.md)、[开发](docs/DEVELOPMENT.md)和[部署](docs/DEPLOYMENT.md)。
