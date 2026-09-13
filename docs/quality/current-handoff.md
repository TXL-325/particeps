# 当前交接

更新：2026-09-13。此页记录接续线索；阶段、确认边界与正式验收结果从 Runtime 读取，不在此维护第二套状态计数。

## 接续入口

当前 change 为 `particeps-foundation`，目标与验收见[brief](../comet/changes/particeps-foundation/brief.md)。文档编号 A13 优先列在验收首位，Runtime 的顺序 ID 应按正文匹配，不能直接套用文档编号。原 81 项的归属和后续依赖见[交付计划](../planning/delivery-plan.md)。

2026-09-13 用户明确反馈“A13验收完毕”，随后要求开始 A4，并指定子 agent 实现和独立 review、主 agent 统筹。A4 的 HTTP 创建修复见[创建验证记录](a4-creation-2026-09-13.md)；用户修订的 A5 端口默认值、Windows SSH 修复及实机清理见[A5 记录](a5-ports-ssh-2026-09-13.md)。用户随后要求对子 agent review 的问题自主修复、整理文档并 commit，当前候选以[A4/A5 提交前审查](a45-review-2026-09-13.md)为接续入口。局部结果不改写整个 change 的 Runtime 正式验收结论。

## 最近核对的候选与证据

| 核对时间与对象 | 已知结果及限制 |
| --- | --- |
| 2026-09-13，发布 v0.1.1 / 源码 58bab0a | [Release](https://github.com/TXL-325/particeps/releases/tag/v0.1.1) 资产完整；[对应 CI](https://github.com/TXL-325/particeps/actions/runs/34713809393) 的 12 项 installer mock、前端类型/构建及 Linux amd64 构建成功 |
| 同日，实验母机 192.168.143.145 | Debian 13.6 amd64、Incus 6.0.4；Agent/Incus active，已安装二进制摘要匹配 Release；当时 particeps 无实例、无 latest 升级备份记录 |
| 2026-09-13，源码 49e6a48 / 用户 A13 验收反馈 | 该提交包含 Incus/Agent 启停子菜单；用户明确反馈 A13 验收完毕。该次接续仅核对本地提交、规格和顺序 |
| 同日，基于 49e6a48 的 A4 历史候选 | HTTP 原报错已复现并修复；独立代码复核通过；面板批量两台 Alpine 与一台 Debian 13 的实际系统、任务、身份及凭据领取核对通过。实验母机当时已运行测试构建，摘要见[验证数据](evidence/a4-2026-09-13/verification.json)；当时未发布新 Release |
| 同日，基于 49e6a48 的 A5 历史候选 | 新预留/可选 UDP 规则、按需映射及 Windows SSH 验证完成，独立 review 通过，实验母机已更新；二进制 4ba29c0d…2f74ef375。完整摘要、38 个源文件/产物摘要和实测结果见[A5 验证数据](evidence/a5-2026-09-13/verification.json)。记录当时未 commit、push 或发布新 Release |
| 同日，A4/A5 提交前审查候选 | Web 等待地址时解除映射、SSH exec 未确认结果的持久保护两项 P2 均已修复，三位独立 Reviewer 复核 passed。Web 定向检查、前端类型/构建、Linux core/incusx 受影响包与 Agent 构建通过；新二进制 `db04ab7f…558673d7` 未部署。44 项候选文件摘要及检查见[本轮报告](a45-review-2026-09-13.md)与[新证据](evidence/a45-2026-09-13/verification.json) |

以上为日期快照；当前工作区候选及后续检查另行核对。v0.1.1 的详细摘要、配置和证明范围保存在[恢复记录](native-progress-sync-2026-09-12.md#2026-09-13-恢复与安装规格同步)，该历史检查未覆盖真实安装/回滚/卸载。最新用户验收反馈只对应 A13，不能扩展为 A1–A12 的浏览器流程或 Agent 停止期间连续性已通过。

## 本轮进展与下一任务

本次提交包含 A4/A5 累积改动与后续两项 P2 修复：默认每台预留 20 个连续 TCP 号码、创建时可选同段 UDP，仅首个 TCP 到 22 默认映射；前端显示可用号段并按协议启停。等待内部地址时可解除已设为启用的映射；SSH 初始化在派发 exec 前保存保护，未知结果保留 `needs-reconciliation`，确认终态后才清除。最终候选、检查与复核以[本轮报告](a45-review-2026-09-13.md)为准。

本轮仅构建验证修复并只读检查实验母机。Agent、Incus 和实验转发服务均 active，运行摘要仍为 A5 的 `4ba29c0d59e8850b79baa6aff8875e8071bd461c13b41632109c6332f74ef375`；新候选未部署。当前 `particeps` 项目有四台 Running：`p-befae687800f`、`p-e7ade30abd02`、`p-232ca9b0caac`、`p-6d04bc1cdf9e`。后两台是用户后续资源，不是测试残留。A5 的 80 条旧记录、40 条旧规则和两台保留实例只表示当时清理快照。

A5 三台测试实例、卷、端口及队列已在当时清理，三条任务历史凭据为空，本地临时私钥已删除。guest 历史 `needs-reconciliation`、`uncertain` 电源结果留待 A8 调查；本轮未重查该数据库状态，不能宣称已解除，也不得盲目重放。`InstallRootKey` / `FilePush` 的既有未确认写入边界另保留为后续线索，不因本轮 SSH exec 修复而视为完成。

A5 备份 `/root/particeps-backups/a5-20260913-153837/` 和 A4 备份 `/root/particeps-backups/a4-20260913-1333/` 均早于当前部分用户资源。A4 的 guest、guest1 名称也不代表当前同名管理身份。继续工作前重新核对当前资源及备份后变化，不能盲目恢复任何历史数据库或照旧名单清理。

正式端口规则见 brief 的 N20/N21、D62/D63，接口见[OpenAPI](../api/openapi.yaml)。映射启停和目标编辑已有 A5 实机证据；A6/A7 还须补齐共享入口、宿主/外部规则与并发冲突、自动/指定追加及编辑隔离的剩余场景，不能把局部操作扩展为全项通过。

随后按 A8–A9（实例启停、删除与隔离）、A11（Agent 停止/重启连续性）推进。A1–A3 的归属、认证与凭据要求，以及 A10/A12 的 Web/API 和证据要求贯穿各流程。完整 SSH/S3、公网及 IPv6 没有因本轮通过；foundation 完成后按交付计划进入 S1 资源上限。

完整条件以 brief/Spec 为准，检查选择与重跑遵循[协作约束](../knowledge/verification-and-workflow-constraints.md)。

## 按需追溯

- 认证与凭据：[F01 修复和部署记录](f01-fixes-2026-09-12.md)。
- 端口与连接：[F03 实测](f03-port-forwarding-2026-09-11.md)、[实验检查指南](../development/network-lab.md)。
- 原 81 项：[第一轮验收快照](native-verification-baseline-2026-09-11.md)、[逐项证据对照](native-progress-sync-2026-09-12.md#原-81-项的历史结果与当前证据)。
- 早期组织与调查：[F01–F08 历史审查计划](functional-audit.md)、[阶段审查](stage-review-2026-09-11.md)。
