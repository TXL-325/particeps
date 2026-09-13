# 当前交接

更新：2026-09-13。此页记录接续线索；阶段、确认边界与正式验收结果从 Runtime 读取，不在此维护第二套状态计数。

## 接续入口

当前 change 为 `particeps-foundation`，目标与验收见[brief](../comet/changes/particeps-foundation/brief.md)。文档编号 A13 优先列在验收首位，Runtime 的顺序 ID 应按正文匹配，不能直接套用文档编号。原 81 项的归属和后续依赖见[交付计划](../planning/delivery-plan.md)。

先执行 Runtime 允许的下一步；实现阶段优先完成部署验收 A13，再继续基础管理与 IPv4 端口闭环。另有菜单交互优化在并行推进，其规格与实现以对应提交和当前工作区为准。

## 最近核对的候选与证据

| 核对时间与对象 | 已知结果及限制 |
| --- | --- |
| 2026-09-13，发布 v0.1.1 / 源码 58bab0a | [Release](https://github.com/TXL-325/particeps/releases/tag/v0.1.1) 资产完整；[对应 CI](https://github.com/TXL-325/particeps/actions/runs/34713809393) 的 12 项 installer mock、前端类型/构建及 Linux amd64 构建成功 |
| 同日，实验母机 192.168.143.145 | Debian 13.6 amd64、Incus 6.0.4；Agent/Incus active，已安装二进制摘要匹配 Release；当时 particeps 无实例、无 latest 升级备份记录 |

以上为日期快照；当前工作区候选及后续检查另行核对。详细摘要、配置和证明范围保存在[恢复记录](native-progress-sync-2026-09-12.md#2026-09-13-恢复与安装规格同步)。这些结果未覆盖真实安装/回滚/卸载、浏览器流程或 Agent 停止期间的连续性。

## 待处理问题

以下为上次源码调查线索，需要在 Build 核对，不是正式 Verifier 结论：

- 首装在下载校验前准备后端资源，需满足校验失败不改配置/资源的要求。
- 回滚直接覆盖程序和数据库，需核对运行中二进制、SQLite 活连接/WAL 与实际恢复结果。
- 全卸在后端删除后才停止 Agent，部分查询错误被当作空结果；需验证并发和失败保护。
- 既有 mock 跳过真实首装准备并模拟锁成功；需要补齐并发锁、池/IPv6/管理员分支、菜单交互及真实生命周期证据。

A13 后继续两种系统创建、认证与凭据、真实 Web/API、共享端口的冲突/编辑/启停/删除隔离，以及 Agent 真正停止期间的既有连接。完整条件以 brief/Spec 为准，检查选择与重跑遵循[协作约束](../knowledge/verification-and-workflow-constraints.md)。

## 按需追溯

- 认证与凭据：[F01 修复和部署记录](f01-fixes-2026-09-12.md)。
- 端口与连接：[F03 实测](f03-port-forwarding-2026-09-11.md)、[实验检查指南](../development/network-lab.md)。
- 原 81 项：[第一轮验收快照](native-verification-baseline-2026-09-11.md)、[逐项证据对照](native-progress-sync-2026-09-12.md#原-81-项的历史结果与当前证据)。
- 早期组织与调查：[F01–F08 历史审查计划](functional-audit.md)、[阶段审查](stage-review-2026-09-11.md)。
