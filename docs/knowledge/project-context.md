# particeps 项目入口

particeps 是 Go + Vue 的 Incus 母机 Agent，提供嵌入式 Web 与 API。本页负责导航；需求正文、执行进度和历史证据由下列来源分别维护。

## 恢复顺序

1. 按仓库 AGENTS.md 进入适用流程；进入 Native 后先读紧凑状态，确定 change、阶段和下一步。
2. 阅读[当前交接](../quality/current-handoff.md)，找到最近核对的候选、有效证据及待处理问题。
3. 按当前动作展开相关规格、源码和证据。需要完整验收时遵循 Native Skill；已经读过的内容用路径和验收 ID 引用，避免重复输出全文。

## 权威来源

| 要确定的事实 | 维护位置 |
| --- | --- |
| 阶段、确认边界、验收结果 | Runtime：`comet native status particeps-foundation --json` |
| 本次目标、来源、决定及验收 | [当前 brief](../comet/changes/particeps-foundation/brief.md)；完整行为由其链接的三份 Spec 定义 |
| 候选、检查证据、遗留问题、下一步 | [当前交接](../quality/current-handoff.md)；历史报告从该页按需进入 |
| 原首期承诺、交付归属及依赖 | [首期基线](../planning/first-release/brief.md)、[交付计划](../planning/delivery-plan.md) |
| 代码职责与调用关系 | [项目结构](project-structure.md) |
| 安装使用、认证、构建与实验检查 | [README](../../README.md)、[认证说明](../development/authentication.md)、[构建与测试](../development/build-and-test.md)、[网络实验指南](../development/network-lab.md) |
| 验证范围、重跑条件与流程成本 | [协作约束](verification-and-workflow-constraints.md) |

## 维护约定

README 面向使用者；本页不复制安装参数、阶段计数或实验结果。交接记录当前事实并链接证据，日期报告保存当时结果。新结论不改写旧验收记录；历史通过不自动成为当前通过。

[检索配置](../../.comet/config.yaml)优先收录入口、交接、正式依据和操作文档，日期报告通过链接按需读取。变更来源后运行 `comet knowledge rebuild . --json`，再以具体问题执行 `comet knowledge query` 检查召回；移动文件时同步链接和收录路径。知识检索用于定位依据，验收仍以 Runtime 和实际证据为准。
