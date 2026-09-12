# particeps

particeps 是基于 Incus 的 Linux 母机管理 Agent，通过内置 Web 面板和 HTTP API 提供系统容器的管理入口，面向实例管理、资源配置与端口转发等运维场景。

后端使用 Go，前端使用 Vue 3 和 TypeScript。前端静态资源嵌入同一个 Go 二进制，母机部署时无需运行 Node.js 服务。

## 环境要求

- 运行环境：Debian 13 amd64，使用 systemd 管理服务。
- 构建工具：Node.js/npm，以及 [go.mod](go.mod) 指定的 Go 工具链。
- 安装权限：root 或 sudo；安装脚本会准备 Incus、网络和存储资源。

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

将源码目录和构建好的二进制放到 Debian 13 母机，在仓库根目录执行：

```sh
sudo ./deploy/install.sh dist/particeps-agent
sudo systemctl status particeps-agent --no-pager
sudo cat /var/lib/particeps/admin-bootstrap.txt
```

[安装脚本](deploy/install.sh)使用 `particeps` Incus 项目、`particepsbr0` 网桥和 `particeps-pool` 存储池；新建存储池大小为 16 GiB，默认网段为 `10.80.0.0/24`。安装前核对这些名称、网段及根分区可用空间与现有环境是否兼容。

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
