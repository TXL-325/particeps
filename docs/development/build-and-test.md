# 构建与测试

本页记录工具链、执行目录、命令、产物和测试环境。检查的选择、执行时机与重跑条件统一引用[按任务风险控制验证范围与流程成本](../knowledge/verification-and-workflow-constraints.md)。

## 工具链与目录

| 项目 | 定义与位置 |
| --- | --- |
| Go 工具链 | [go.mod](../../go.mod) 的 `toolchain go1.26.6`；Go 命令在仓库根目录执行。 |
| 前端依赖与脚本 | [web/package.json](../../web/package.json) 和 [web/package-lock.json](../../web/package-lock.json)；npm 命令在 `web/` 执行。 |
| 前端输出 | [web/vite.config.ts](../../web/vite.config.ts) 将输出写入 `internal/web/dist/`。 |
| Agent 入口与产物 | `./cmd/particeps-agent`；构建目标为 Linux amd64，产物为 `dist/particeps-agent`。 |

## 前端命令

| 命令（在 `web/` 执行） | 作用 |
| --- | --- |
| `npm ci` | 按锁文件安装依赖。 |
| `npm run dev` | 启动 Vite 开发服务器与热更新。 |
| `node --test tests/settings.test.cjs` | 运行设置页行为测试文件；其他文件位于 `web/tests/`。 |
| `npm test` | 执行 `node --test tests/*.test.cjs`，运行前端全部行为测试文件。 |
| `npx --no-install vue-tsc --noEmit` | 使用本地依赖执行 Vue/TypeScript 类型检查。 |
| `npm run build` | 依次执行 `vue-tsc --noEmit` 和 Vite 构建。 |

开发服务器将 `/api` 代理到 `http://127.0.0.1:8792`，页面的业务请求需要该地址可访问的 Agent。前端行为测试使用 Node.js test runner、Vue 自定义渲染器或 SSR；浏览器操作使用开发服务器或 Agent 提供的页面。

## Go 命令（Linux）

| 命令（在仓库根目录执行） | 作用 |
| --- | --- |
| `go test ./internal/core` | 运行指定包测试；示例包为 `internal/core`。 |
| `go test ./...` | 运行仓库全部 Go 包的测试。 |
| `go vet ./internal/core` | 静态检查指定包。 |
| `go vet ./...` | 静态检查仓库全部 Go 包。 |
| `CGO_ENABLED=1 go test -race ./internal/core` | 在 Linux shell 中启用指定包的 race detector，需要可用的 C 编译器。 |

普通单元测试使用临时数据库与模拟后端。Linux 测试环境可以是 Linux 主机或 Linux 容器；容器内的 race detector 同样需要 C 编译器。

## Linux amd64 构建

先用前端构建命令生成 `internal/web/dist/`，Go 的 [embed 声明](../../internal/web/embed.go)会将该目录嵌入二进制。在仓库根目录使用 Linux shell 执行：

```sh
mkdir -p dist
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o dist/particeps-agent ./cmd/particeps-agent
```

Windows PowerShell 中的对应命令为：

```powershell
$env:GOOS="linux"
$env:GOARCH="amd64"
$env:CGO_ENABLED="0"
New-Item -ItemType Directory -Force dist | Out-Null
go build -trimpath -ldflags="-s -w" -o dist/particeps-agent ./cmd/particeps-agent
```

这些环境变量作用于当前 PowerShell 会话；切换到其他构建目标或 race 检查前恢复原值。

## Windows 编译、Linux 执行测试

在已设置上述 Linux 目标参数的 PowerShell 会话中，可为指定包生成测试程序：

```powershell
New-Item -ItemType Directory -Force dist/audit-tests | Out-Null
go test -c -o dist/audit-tests/core.test ./internal/core
```

`go test -c` 只编译测试程序。将 `core.test` 复制到 Linux 测试环境后执行：

```sh
chmod +x core.test
./core.test -test.v
```

## 安装脚本测试

[tests/installer/test_install.py](../../tests/installer/test_install.py)在临时目录中用 mock 的 `curl`/`systemctl`/`incus` 覆盖升级备份、校验失败、回滚和卸载路径。需要 bash、python3 和 sha256sum。在仓库根目录执行：

```sh
python3 tests/installer/test_install.py
```

该测试不连接 GitHub，也不改动真实 Incus。发布流程在打 `v*` tag 时由 [.github/workflows/release.yml](../../.github/workflows/release.yml) 先跑上述测试，再构建并上传 Release。

## Incus 集成测试

[TestIncusReadOnlyIntegration](../../internal/incusx/exec_test.go)在已有测试实例内执行只读命令，需要以下环境变量：

| 变量 | 内容 |
| --- | --- |
| `PARTICEPS_TEST_INCUS_SOCKET` | 专用测试 Incus socket 路径。 |
| `PARTICEPS_TEST_GUEST` | `particeps` 项目内已有测试实例的名称。 |

配置后，在连接该 socket 的 Linux 环境、仓库根目录执行：

```sh
go test ./internal/incusx -run '^TestIncusReadOnlyIntegration$' -v
```

任一变量未设置时，此项测试跳过。实验母机的网络拓扑与配置记录见[本地网络实验环境](network-lab.md)。
