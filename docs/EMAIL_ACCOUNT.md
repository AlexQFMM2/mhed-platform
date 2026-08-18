# 邮箱账号与 AOKSend 接入

状态：2026-08-18 已实现并随 MH3G 修改器 `v1.3.0-beta.2` 部署；生产邮件服务需由超级管理员
在 Admin 中填写 AOKSend 密钥和模板、完成测试后启用。

## 官方接口

- 邮件发送 API v2：<https://www.aoksend.com/api.html>
- 账户余额 API v2：<https://www.aoksend.com/check_balance_api.html>
- 发送地址固定为 `https://apiv2.aoksend.com/index/api/send_email`。
- 余额地址固定为 `https://apiv2.aoksend.com/index/api/check_account`。

接口地址不允许从 Admin 修改。验证码模板固定使用 `code`、`username`、`userinfo` 三个变量。

## 安全配置

生产环境必须提供由 32 个随机字节进行 Base64 编码得到的 `MHED_SECRET_ENCRYPTION_KEY`。
AOKSend API 密钥只能由超级管理员在“系统设置 → 邮件服务”填写，经 AES-256-GCM 加密后进入
PostgreSQL；查询接口只返回是否已配置，不返回密钥。邮件 Outbox 中的模板参数同样加密，发送
成功或最终失败后清除密文。

Admin 邮件设置页保留最近 100 条发送元数据，包括场景、收件地址、状态、尝试次数、AOKSend
消息 ID、错误码和时间。测试邮件同样记入记录；记录不包含验证码、模板正文或 API 密钥。

验证码为六位数字，10 分钟失效，最多尝试 5 次。同一邮箱 60 秒内不能重发，每小时最多 5 次；
同一来源 IP 每小时最多 10 次。首版不使用图形验证码。

新建或修改密码统一为 8～16 位 ASCII 字符，必须同时包含英文字母、数字和特殊符号。支持的特殊
符号固定为：`! @ # $ % ^ & * _ - + =`。旧规则创建的密码仍可正常登录，用户下次修改密码时
采用新规则。

## 本地验收顺序

1. 生成本地加密主密钥并写入未提交的 `.env`。
2. 启动本地 Compose，确认数据库迁移到版本 4。
3. 使用 Admin 配置 API 密钥和模板 ID，先查询余额，再向自己的邮箱发送测试邮件。
4. 在 Desk 验证注册自动登录、邮箱登录、绑定/更换邮箱、忘记密码和获赞数。
5. 本地验收完成后再单独制定生产上线步骤；本地完成不代表已部署。

本地入口为 Admin `http://127.0.0.1:18102`、API `http://127.0.0.1:18100`。宿主机启动
MH3G 修改器时显式指向本地 API：

```bash
cd ../mh3u-se
MHED_DESK_API_URL=http://127.0.0.1:18100 ./bin/MH3USaveEditorGUI
```

`mhed.mall.65h26i.top` 只用于发信域名认证，不是 HTTP API 地址。
