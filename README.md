# particeps

母机 Agent：用 Incus 系统容器管理小鸡，内置 Web 面板与 `/api/v1`。

当前为 foundation 的 Build 修复阶段。创建、配额、认证与采样已有实现；共享 IPv4 端口转发、追加/编辑和清理已完成本地实测，见 [F03 端口批次记录](docs/quality/f03-port-forwarding-2026-09-11.md)。IPv6/NAT66、完整任务恢复、重装与 Web 终端仍有明显缺口。架构与代码基线见 [阶段技术审阅](docs/quality/stage-review-2026-09-11.md)，分批验收见 [功能检查记录](docs/quality/functional-audit.md)。

VMnet2 的三个本地 IPv4 地址及两段小鸡 IPv6 测试网络已配置，地址、路由、实际验证和备份位置见 [本地网络实验环境](docs/development/network-lab.md)。这属于开发环境准备，面板自动分配 IPv6 等能力仍待实现。

## 构建

构建使用 Node.js/npm 和 Go。`go.mod` 指定 `toolchain go1.26.6`；支持自动工具链选择的 Go 会下载该版本。不要用旧版本强制覆盖：本轮扫描在 Go 1.26.4 产物中发现了已由 1.26.6 修复的标准库漏洞。母机运行静态 Linux 二进制，无需安装 Node.js 或 Go。

Windows（构建前端并交叉编译 Linux amd64）：

```powershell
cd web
npm ci
npm test
npm run build
cd ..
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
go vet ./...
go build -trimpath -ldflags="-s -w" -o dist/particeps-agent ./cmd/particeps-agent
```

Go 行为测试在 Linux 执行 `go test ./...`。Windows 上如果安全软件拦截 Go 测试可执行文件，可以先交叉编译测试，再把生成的 `.test` 文件复制到测试母机逐包运行：

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
New-Item -ItemType Directory -Force dist/audit-tests
go test -c -o dist/audit-tests/ ./internal/...
```

例如 Linux 上运行 `./api.test -test.v`。普通单元测试使用临时数据库和模拟后端；Incus 实机只读检查需要明确指定测试 socket 和已有测试实例，见 `internal/incusx/exec_test.go`。

按功能划分的 81 项验收、当前缺陷及本轮实际检查结果见 [功能检查记录](docs/quality/functional-audit.md)。当前实现仍有未完成能力，编译或单元测试通过不代表全功能验收完成。

## 安装（Debian 13）

```sh
sudo ./deploy/install.sh dist/particeps-agent
sudo cat /var/lib/particeps/admin-bootstrap.txt
```

默认监听 `127.0.0.1:8792`，可经 SSH 隧道或 HTTPS 反向代理访问。需要直连实验网卡时显式修改监听地址。已有安装的配置不会因为修改示例而自动改变。

安装脚本面向 Debian 13 amd64 实验环境，会创建 Incus 项目、IPv4 网桥和 16 GiB LVM thin 存储池，并安装 conntrack 用于连接清理。同名资源的归属检查、通用升级/回滚与网段冲突检查尚未完成；运行脚本前应阅读阶段审阅中的部署限制。本次 F03 版本已在实验母机备份后更新，具体版本、检查和备份位置见批次记录。

自动生成的小鸡初始密码通过任务页的“领取初始凭据”按钮领取一次，有效期为创建完成后 15 分钟；普通任务查询不包含密码。领取后或过期后可通过实例密码重置设置新密码。

## 仓库内容

`cmd/` 是 Agent 入口，`internal/` 是 Go 实现，`web/` 是 Vue 源码，`deploy/` 是实验部署文件。`internal/web/dist/` 是随 Go 二进制嵌入的前端产物，应与 `web/` 一起更新。

`docs/comet/` 和 `.comet/config.yaml` 保存规格及可携带进度；正式进度以 Runtime 管理的 `comet-state.yaml` 为准。其他本机开发工具接入、运行日志、数据库、私钥、初始凭据和编译二进制不纳入 Git。仓库沿用远程已有的 [GPL-3.0 许可证](LICENSE)。
