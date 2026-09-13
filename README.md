# particeps

particeps 是基于 Incus 的 Linux 母机管理 Agent，通过内置 Web 面板和 HTTP API 提供系统容器的管理入口，面向实例管理、资源配置与端口转发等运维场景。

后端使用 Go，前端使用 Vue 3 和 TypeScript。前端静态资源嵌入同一个 Go 二进制，母机部署时无需运行 Node.js 服务。

## 环境要求

- 运行环境：Debian 13 amd64，使用 systemd 管理服务。
- 构建工具：Node.js/npm，以及 [go.mod](go.mod) 指定的 Go 工具链。
- 安装权限：root；安装脚本从 GitHub Release 下载 linux amd64 二进制，并准备 Incus、网络和存储资源。

## 从源码构建

首次构建时，在仓库根目录使用 Linux shell 执行：

```sh
cd web
npm ci
npm run build
cd ..
mkdir -p dist
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/particeps-agent ./cmd/particeps-agent
```

前端输出位于 `internal/web/dist/`，Agent 产物为 `dist/particeps-agent`。Windows 交叉编译、前端开发和测试命令见[构建与测试](docs/development/build-and-test.md)。

## 安装与访问

在 Debian 13 amd64 母机以 root 执行。脚本只从 GitHub Release 下载二进制：

```sh
bash <(curl -fsSL https://github.com/TXL-325/particeps/releases/latest/download/install.sh)
```

固定版本、只升级、状态、回滚和卸载：

```sh
curl -fsSL -o install.sh https://github.com/TXL-325/particeps/releases/latest/download/install.sh
bash install.sh --tag v0.1.1
bash install.sh --update-only
bash install.sh --status
bash install.sh --rollback
bash install.sh --uninstall -y --confirm PURGE
bash install.sh --uninstall --keep-instances
```

无参数且在终端中运行时提供五项菜单。全卸删除 Agent、配置、数据、`particeps` 项目内全部实例以及专用网桥/存储池/项目，执行前必须输入 `PURGE`。完成后另行询问是否卸载 Incus，并列出其他项目实例；选择 `y` 会清除 Incus 及这些实例，默认回车保留，`-y` 非交互模式也保留 Incus。`--keep-instances` 只卸程序和服务，保留配置、数据及实例，不进入 Incus 清除流程。

[安装脚本](deploy/install.sh)使用 `particeps` 项目、`particepsbr0` 网桥和 LVM thin 的 `particeps-pool`。首装可选择新池大小，脚本按根分区余量建议，`-y` 使用建议值；已有 lvm 池保持原样。默认 IPv4 网段为 `10.80.0.0/24`，NAT 开启。检测到母机全局范围 IPv6（含 ULA）时，新桥或未配置 IPv6 的既有桥启用 auto/NAT；公网连通仍需验证。已有桥的 IPv4 配置冲突则拒绝。

打 `v*` tag 后 GitHub Actions 构建并上传 Agent、`install.sh` 和 SHA256。安装/升级从 Release 下载并校验；升级保留配置、实例、网络和存储池，提前备份程序、单元、配置及管理数据库。

Agent 默认监听 `0.0.0.0:8792`，可通过安装输出中的 `http://<母机IPv4>:8792` 访问。首次安装生成的管理员密码只在安装终端展示，不写入 `admin-bootstrap.txt`，请当场保存；已有管理员不会被重置。

配置文件位于 `/etc/particeps/config.yaml`，数据目录为 `/var/lib/particeps`。已有配置不覆盖，配置示例见 [deploy/config.yaml](deploy/config.yaml)。默认 HTTP 使用非 Secure Cookie；HTTPS（包括反向代理终止 TLS）部署需设置 `session_cookie_secure: true`，详见[认证说明](docs/development/authentication.md)。修改配置后执行：

```sh
sudo systemctl restart particeps-agent
```

## 文档

- [构建与测试](docs/development/build-and-test.md)
- [认证、会话与密码操作](docs/development/authentication.md)
- [HTTP API（OpenAPI）](docs/api/openapi.yaml)

## 许可证

本项目采用 [GPL-3.0](LICENSE) 许可证。
