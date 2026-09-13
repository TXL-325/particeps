# particeps 基础交付

本规格负责交付物、部署运维的用户可见行为和资源边界。需求来源、决定与验收见[brief](../../brief.md)；管理操作由[管理规格](../agent-management/spec.md)定义，工程机制由[架构规格](../agent-architecture/spec.md)定义。阶段、确认边界与验收结果由 Runtime 管理。

## 交付物与归属

particeps 在独立工作区维护源码、需求与证据；runman-agent 是参考项目，不因本 change 被修改。交付 Linux amd64 Go Agent，内嵌 Vue Web 并提供 /api/v1，在已准备的 Debian 13 Incus 实验母机运行。对应本次 A1。

Agent 常规操作只管理本系统登记并能核对归属的 Incus 系统容器。管理 ID、Incus 对象、端口记录和查询对象一致；其他工具或预先存在的实例不自动接管、修改或删除。当前操作均受认证、权限与来源校验约束。脚本的显式全卸及独立 Incus 清除按下文单独限定，不赋予 Web/API 接管外部实例的权限。对应本次 A1、A2、A13。

## 首期承诺与本次边界

本次完整交付包含 GitHub Release 安装运维脚本，以及基础实例管理和指定 IPv4 入口的端口闭环：初始系统创建、共享转发、端口追加/编辑、单台停止/启动、删除清理、当前凭据交付及对应 Web/API。Alpine 与 Debian 13 的实际系统都需要核对。对应本次 A13、A3–A10。

## 部署与发布

安装入口是 GitHub Release 上的 `install.sh`。打版本 tag 后，GitHub Actions 构建 linux amd64 Agent，并上传二进制、`install.sh` 和 SHA256。脚本只从 Release 下载；校验失败或没有可用资产时失败，不改已有程序、配置和 Incus 资源。对应本次 A13。

首次安装在 Debian 13 amd64 上以 root 执行，安装 Incus 依赖并准备 `particeps` 项目、`particepsbr0` 和 LVM thin 的 `particeps-pool`。新池默认建议为根分区可用整 GiB 的 30% 向下取整，最少 1 GiB、最多 25 GiB、不超过实际可用量；不足 1 GiB 拒绝。交互允许输入 1 至可用 GiB 的整数并确认，非交互用建议值。已有 lvm 池及项目保留，池为其他驱动时拒绝，不改既有池大小。

网桥 IPv4 固定为 `10.80.0.1/24`、NAT 开启。母机非回环、非容器/虚拟网桥接口存在 scope global IPv6（含 ULA）时，新桥启用 `ipv6.address=auto` 和 `ipv6.nat=true`；没有时新桥禁用 IPv6。既有桥的 IPv4 地址/NAT 不符合预期则拒绝；仅在检测到母机 IPv6 且原桥 IPv6 为空/none 时补配，其余既有 IPv6 配置保留。地址检测不证明公网 IPv6 出口，也不代表 Agent 已实现 IPv6 实例管理。

已有 `/etc/particeps/config.yaml` 不覆盖。新配置默认监听 `0.0.0.0:8792`，HTTP Cookie 非 Secure；HTTPS 部署显式设为 Secure。程序、配置和数据仍使用 `/opt/particeps`、`/etc/particeps`、`/var/lib/particeps`。首装无管理员时生成 20 位字母数字密码，经 `--bootstrap-only` 保存哈希，安装终端展示面板 IPv4 地址与密码，不写 `admin-bootstrap.txt`；已有管理员不重置、不重复交付。管理员密码不进入普通日志或 API。

升级只替换 Agent 程序：先备份程序、systemd 单元、配置和管理库（SQLite 在线备份含 WAL，存在的 metrics.db 也备份，目录 `0700`），不含小鸡磁盘；不改实例、网桥、存储池和配置值。原已停止的服务保持停止；重启失败退出非零并给出备份位置。`--status` 显示 Agent/Incus 状态、监听地址和最近备份。`--rollback` 从最近一次升级备份恢复上述四类文件。无参数且在终端时提供五项菜单：安装/升级、状态、回滚、全部卸载、只卸 Agent。

全卸执行前必须输入 `PURGE`，删除 Agent、配置、数据、particeps 项目内全部实例、`particepsbr0`、`particeps-pool` 和项目。此步骤保留其他项目及 Incus；随后列出其他项目实例并说明数据清除影响，再单独询问是否卸载 Incus。默认否，非交互保留；明确输入 y/Y 后才 apt purge Incus/lxcfs 并清除 Incus 数据目录，此选择会清除其他项目实例。`--keep-instances` 只停止并移除程序与服务，保留配置和管理数据，不进入 Incus 清除流程。并发安装/升级使用锁，第二路被拒绝。

本切片不提供平台 Token、完整 IPv6 安装向导、本地二进制旁路或自动升级服务。自动网桥 IPv6 准备不代替后续网络交付。原 A56 仍由 S7 主归属；8–16 台集成和完整剩余空间预算不因 A13 完成。

[完整首期基线](../../../../../planning/first-release/brief.md)保留原产品目标、用户来源、D1–D34、原 A1–A81 与非目标。[交付计划](../../../../../planning/delivery-plan.md)为每个原 ID 保存唯一主归属及当前门槛的部分覆盖关系。原完整要求没有取消；当前编号为本 change 的局部编号。

资源压力、完整任务/重装、SSH/终端、指定出口与 IPv6 网络、完整监控和 8–16 台集成由后续独立交付完成。未完成的功能不能宣称可用或返回虚假完成。当前采用顺序普通 Native change，不增加 Supervisor/Child。

## 验证与后续边界

实验基线沿用 Win11 VMware Workstation 中的 Debian 13 x86_64、2 vCPU、4GB RAM、50GB 虚拟磁盘。每次验证记录实际配置、Incus 版本、源代码/二进制、客户端和实例位置、检查结果与清理情况。实际环境准备属于前置条件，不因现有历史记录而假定当前仍完全一致。对应本次 A12。

当前至少两实例的共享入口实验不代替原 8 台与 16 台管理/监控目标。私网 IPv4 和 ULA IPv6 不证明真实公网；TCP 22 探针不证明 SSH 登录；静态限额不证明压力结果。

实际停止/重启 Agent 时核对实例、IPv4 转发、外部既有连接与管理数据。管理进程停止不应拆除数据通路。对应本次 A11。

基础交付完成需要本次 A1–A13、对应完整规格和必要检查全部满足，并经 Runtime 接受独立验收。A13 优先。原完整首期由最后集成交付全量复核原 81 项，不因本次归档而自动完成。
