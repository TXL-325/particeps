# 网络实验检查指南

Windows 通过 VMware VMnet2 访问 Debian 实验母机，母机通过 Incus 网桥将流量送到实例。Windows 可作为实例外部的 TCP/UDP 客户端；本地 IPv4 与 ULA 实验只证明对应本地路径。

## 使用前核对

地址、实例及网桥可能已变化。先结合[当前交接](../quality/current-handoff.md)确定母机，再核对实际状态；不要直接复用历史实例名或 IPv6 地址。

在 Windows 查看 VMnet2 地址与路由：

```powershell
Get-NetIPAddress | Where-Object InterfaceAlias -Like '*VMnet2*'
Get-NetRoute | Where-Object InterfaceAlias -Like '*VMnet2*'
```

在 Debian 母机执行只读检查：

```sh
ip -br addr
ip route
ip -6 route
incus version
incus --project particeps list
incus network show particepsbr0
incus storage show particeps-pool
systemctl is-active particeps-agent incus
```

实例查询明确使用 `particeps` 项目；既有实验网桥属于 `default` 项目。检查时一并核对项目归属，不因名字相似接管资源。涉及持续连接时，记录客户端、入口、实例地址及源/目标端口，分别观察操作前、操作中、操作后的结果。

## 现有工具与配置

- [netprobe 服务](../../tests/netprobe/main.go)与[客户端](../../tests/netprobe/client.cjs)：TCP/UDP 目标及连接连续性检查。
- [VMnet2 转发脚本](../../deploy/lab/vmnet2-forwarding.sh)与[服务单元](../../deploy/lab/particeps-vmnet2-forwarding.service)：既有实验的定向转发配置；使用前核对实际接口、地址与防火墙，不能直接作为任意环境的通用规则。
- [构建与测试](build-and-test.md)：工具链与可选 Incus 检查入口；检查范围遵循[协作约束](../knowledge/verification-and-workflow-constraints.md)。

## 端口映射与防火墙的边界

Incus 的原生 nftables DNAT 和 iptables/nftables 的过滤规则都由 Linux 内核 Netfilter 执行；Agent 不逐包代理。`iptables -t nat -L` 未展示 Incus 原生 nftables 表，不表示映射绕过防火墙。Docker 与 Incus 没有依赖关系，但共用母机网络；Docker 设置的 `FORWARD DROP` 会拒绝在转发路径中未获允许的流量，影响实例入站和出网。

本实验入站允许规则限定 `ens37 → particepsbr0`、原目标 `10.90.0.10/.11/.12`、DNAT 状态及目的网段 `10.80.0.0/24`。它不逐条匹配数据库 `enabled` 或枚举应用端口；实际安装的 DNAT 规则决定哪些端口会被转向实例，最终访问仍需其他过滤规则和实例服务允许。预留但未启用的号码不会因此自动映射。

`particepsbr0 → ens33` 的 `10.80.0.0/24` 规则允许实例普通 IPv4 出网，另允许相应回包。20 个预留号码是入站分配，不是出口端口限制；在同一次过滤链遍历中，把 DROP 追加到已匹配的 ACCEPT 之后不会生效，限制须置于该允许之前或另一处适用的过滤链。

IPv4 iptables 不覆盖 IPv6。2026-09-13 只读核对时，既有桥配置了 `fd42:d79d:a631:31c0::1/64` 和 IPv6 NAT；IPv6 默认路由查询为空，不能据此称 IPv6 已禁用或所有 IPv6 路径均不可达。后续检查仍需重新核对地址、路由及两套过滤规则。

## 历史环境与证据

[2026-09-11 网络快照](../quality/network-lab-2026-09-11.md)完整保留当时拓扑、IPv4/IPv6 地址、路由、备份位置、临时资源清理和实验结果。[F03 报告](../quality/f03-port-forwarding-2026-09-11.md)记录该环境中的端口实测；后续母机核对见[恢复记录](../quality/native-progress-sync-2026-09-12.md#2026-09-13-恢复与安装规格同步)。

恢复或撤销历史配置前核对其后的变更和当前使用者，按确切资源执行。实验完成后在日期记录中保存候选版本、实际环境、前后结果及清理情况，再更新当前交接的证据链接。
