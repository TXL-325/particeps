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
bash install.sh --tag v0.1.0
bash install.sh --update-only
bash install.sh --status
bash install.sh --rollback
bash install.sh --uninstall --confirm PURGE
bash install.sh --uninstall --keep-instances
```

无参数且在终端中运行时提供五项菜单。卸载默认删除 Agent、数据、`particeps` 项目内实例以及专用网桥/存储池/项目，执行前必须输入 `PURGE`；`--keep-instances` 只卸程序和服务。不 apt 卸载 Incus。

[安装脚本](deploy/install.sh)使用 `particeps` Incus 项目、`particepsbr0` 网桥和 `particeps-pool` 存储池；新建存储池大小为 16 GiB，默认网段为 `10.80.0.0/24`。同名资源已存在则不修改，配置冲突则拒绝。打 `v*` tag 后 GitHub Actions 构建并上传 Agent、`install.sh` 和 SHA256。

Agent 默认监听 `127.0.0.1:8792`。将 HTTPS 反向代理的后端指向该地址，即可通过浏览器访问面板；首次启动生成的管理员密码保存在上述 `admin-bootstrap.txt` 文件中。

配置文件位于 `/etc/particeps/config.yaml`，数据目录为 `/var/lib/particeps`。配置示例见 [deploy/config.yaml](deploy/config.yaml)。会话默认使用 Secure Cookie，HTTPS 部署保持 `session_cookie_secure: true`；隔离的 HTTP 开发环境配置见[认证说明](docs/development/authentication.md)。修改配置后执行：

```sh
sudo systemctl restart particeps-agent
```

## 文档

- [构建与测试](docs/development/build-and-test.md)
- [认证、会话与密码操作](docs/development/authentication.md)
- [HTTP API（OpenAPI）](docs/api/openapi.yaml)

## 许可证

本项目采用 [GPL-3.0](LICENSE) 许可证。
