# particeps 项目知识

本页是项目上下文和来源入口，更新于 2026-09-12。交付边界来自用户已确认的正式文档；当前阶段、候选和通过数量必须查询 Runtime。知识被收录或检索到不代表功能通过验收。

## 已确认的交付边界

用户已明确确认 `particeps-foundation` 进入 Build，当前交付为已准备的 Debian 13 实验母机上的基础实例管理与指定 IPv4 入口的端口闭环，共 12 项局部验收。

- A1–A4：独立 Go Agent 与受管实例身份、当前操作的认证授权、一次性初始凭据保护、Alpine/Debian 13 实际创建及至少两台的正常批量结果。
- A5–A9：连续号码分配、两实例共享 IPv4 转发、追加/编辑、停止/启动与地址重绑、删除清理和失败/结果不明保护。
- A10–A12：一致的 Web/API 操作、真实浏览器验证、Agent 实际停止/重启的业务连续性，以及候选、环境、结果和清理的可追溯证据。

原完整首期 A1–A81 持续有效，全文和来源保存在[首期基线](../planning/first-release/brief.md)。[交付计划](../planning/delivery-plan.md)保留每项唯一主归属：S0 基础管理与 IPv4 端口；S1 资源上限；S2 持久任务和完整生命周期；S3 SSH、凭据与终端；S4 IPv4 出入站与独立地址；S5 IPv6、能力组合和来源限制；S6 监控与诊断；S7 部署和完整集成。

当前顺序推进一个普通 Native change。S0–S7 是交付归属，不是已创建的 Supervisor/Child 或未来 change；不提前创建其他 change 或 worktree。当前 12 项通过也不代表原 81 项全部完成。

## 正式依据与恢复入口

| 需要确定的事实 | 来源 |
| --- | --- |
| 当前目标、范围、决定、约束和 A1–A12 全文 | [当前 brief](../comet/changes/particeps-foundation/brief.md) |
| 当前完整行为与实现约束 | [管理规格](../comet/changes/particeps-foundation/specs/agent-management/spec.md)、[架构规格](../comet/changes/particeps-foundation/specs/agent-architecture/spec.md)、[项目规格](../comet/changes/particeps-foundation/specs/project-foundation/spec.md) |
| 原 81 项承诺及后续归属、依赖和局部覆盖关系 | [首期基线](../planning/first-release/brief.md)、[交付计划](../planning/delivery-plan.md) |
| 当前阶段、候选与正式验收结论 | Runtime 管理的 [comet-state.yaml](../comet/changes/particeps-foundation/comet-state.yaml)；恢复时运行 `comet native status particeps-foundation --json` |
| 历史结果与后续证据各自证明了什么 | [逐项进度同步](../quality/native-progress-sync-2026-09-12.md)、[F03 端口实验记录](../quality/f03-port-forwarding-2026-09-11.md) |
| 实验网络、测试路径与环境限制 | [本地网络实验环境](../development/network-lab.md)，使用前核对实时配置 |

## 架构与资源边界

详细目录职责、Go/Vue 分层、构建输出与管理调用关系见[项目结构](project-structure.md)。

Go Agent 通过本机 Incus Unix socket 管理系统容器，同一 Linux amd64 二进制嵌入 Vue 3/TypeScript/Vite 前端并提供 `/api/v1`。正常业务流量经 Incus 和内核转发，Agent 不逐包代理。

源码入口为 `cmd/`，后端为 `internal/`，前端为 `web/`，部署文件为 `deploy/`；`internal/web/dist/` 是嵌入二进制的前端产物。配置、程序和数据沿用 `/etc/particeps`、`/opt/particeps`、`/var/lib/particeps`。只操作可核对归属的本系统资源，保留其他 Incus 实例及 runman-agent 参考仓库。

共享入口更新必须保留其他实例规则和连接；数据目录单写者、端口归属账本、写前记录和结果回读共同保护修改。结果不明或清理失败时保留占用和可查询原因，禁止盲目重试及重新分配。固定 Incus 6.0.4 的 If-Match 不作为已经验证的并发保障。

当前 IPv4 入站实验不能扩展为指定出口 SNAT、严格 IPv4-only、独立 IPv4 或 IPv6/NAT66 已完成。具体行为始终核对当前 Spec 与实现。

## 构建与验证入口

[构建与测试](../development/build-and-test.md)记录工具链、命令、执行目录、产物和环境要求；Comet 的「构建与测试方式」条目提供这份文档的检索摘要。

验证范围、执行时机、重跑条件和流程成本统一由[按任务风险控制验证范围与流程成本](verification-and-workflow-constraints.md)维护。构建文档及其知识条目通过链接引用该约束。

## 证据与验收

2026-09-11 F03 的 Go 79 项、Vue 16 项和本地 TCP/UDP 结果是留存证据；旧 17 passed / 56 failed / 8 blocked 是原 81 项的第一轮结果。它们不能换算为当前 12 项的通过数。原 A71 和 A77 的历史通过仍需在对应网络交付中复验。

当前候选必须补齐所需的真实系统核对、凭据领取/过期清除、浏览器操作和 Agent 实际停止/重启证据。HTTP 200、任务受理、running、端口 22 探针、静态额度、私网/ULA 或历史结果，各自不能替代完整业务、SSH、负载、公网或规模验证。

## 知识维护

README 是开源项目的对外入口，只保留项目介绍、环境要求、上手命令、使用文档入口和许可证。不得用 README 维护 Comet 阶段、验收计数、批次结果或实验环境进度；动态信息按上表分别维护在 Runtime、brief/Spec 和质量记录中。

项目使用本地知识 Provider。[Comet 配置](../../.comet/config.yaml)通过 `knowledge.local.include` 收录 README、构建与测试文档、本页、当前 brief/Spec、交付计划、进度同步和相关实验记录。README 只提供公开介绍与入门信息；默认正式 Spec 和归档仍由 Comet 自动发现。

更新交付依据或证据后执行 `comet knowledge rebuild . --json`，再用 `comet knowledge query . --task '<需要核对的问题>' --phase build --json` 核对实际召回的来源和内容。归档或移动来源后同步链接和显式收录路径。知识检索是进入正式依据的入口，不代替 Runtime 状态、源码调查或验收。

「构建与测试方式」的稳定条目 ID 为 `generated-build-test`。命令或环境变化时更新构建与测试文档，涉及入门命令时同步 README，再用 `comet knowledge correct . --id generated-build-test --text '<更新后的说明>' --json` 纠正检索摘要。验证与流程规则只在原约束正文和对应约束条目中维护。重建后通过 `comet knowledge get . --id generated-build-test --json` 和带 `--operation build` 或 `--operation test` 的知识查询核对各自内容。
