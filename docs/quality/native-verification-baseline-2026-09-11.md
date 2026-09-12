# Native 第一轮验收历史快照

原报告完成时间：2026-09-11T11:32:26.559Z；原状态版本：20；来源：Git `8795041:docs/comet/changes/particeps-foundation/verification.md`。

本文件保存范围调整前的报告正文与逐项原因，结果为原 81 项中的 17 passed、56 failed、8 blocked。它是历史快照，不是当前新范围的验收报告。2026-09-12 重新 Shape 后，Runtime 移除了活动目录中的旧报告；新报告需等新的正式验收生成，不能手工恢复旧报告冒充当前结果。

后续实现和证据修正见[进度同步](native-progress-sync-2026-09-12.md)。下面保留原报告中的旧失败原因和阶段说明，不能当作当前源码结论。

## 验证

### 当前结果

- 结果: **未通过**
- 验证情况: **修复未通过的验收项后重新验证**
- 目标周期: 8
- 迭代: 1
- 验证器尝试次数: 1
- 完成时间: 2026-09-11T11:32:26.559Z
- 摘要: 第一轮候选交付了独立 Go/Vue 母机 Agent，并在 Debian 13 VM 上跑通登录、Alpine 创建、0.5 核 allowance、父 cgroup cpu.max、密码重置和小鸡进程列表；但批量生命周期、端口编辑、来源 IP 限制、NAT66/独立 IPv6、Web 终端、分钟汇总/流量重置、OpenAPI 与 8–16 台规模均未达到验收。另：曾误把母机 LAN 地址做成 Incus 默认转发，已删除。

### 验收

| 编号 | 结果 | 来源 | 验收项 | 原因 |
| --- | --- | --- | --- | --- |
| A1 | passed | brief.md | A1：particeps 的需求、规格和后续实现位于独立项目工作区；参考 runman-agent 工作区不会因本 change 被修改。 | 需求、规格与实现均在 particeps 工作区，本 change 未改 runman-agent。 |
| A2 | passed | brief.md | A2：需求记录区分已核对事实、候选建议、已确认决定和未解决问题；用户确认的功能逐项进入目标规格及可观察验收，未确认选择保持开放。 | brief 区分事实、建议、已确认决定与待解决问题，确认项已映射到三份 spec 与 A1–A81。 |
| A3 | passed | brief.md | A3：交付和验收对象为母机 Agent；确认范围内的管理与监控能力能在母机侧通过 Agent 的 Web 面板和 API 验证。 | 交付物是带嵌入式 Web 与 /api/v1 的母机 Agent，验收入口在母机侧而非外部平台。 |
| A4 | failed | brief.md | A4：管理员可以通过 Agent 内置 Web 面板访问本母机的管理和监控功能，包括确认范围内的小鸡批量操作与监控查询。 | 嵌入式面板存在，但缺少终端/批量改配/流量等范围内能力，且 Windows Chrome 访问 :8792 超时。 |
| A5 | failed | brief.md | A5：外部程序可以通过 Agent 的 API 调用确认范围内的管理与监控能力；与 Web 面板执行相同操作或查询时遵循一致的业务规则与数据口径，无需人工操作浏览器。 | 仅有部分 /api/v1 路由，端口编辑、终端、流量重置、来源 IP、批量任务等范围内接口缺失。 |
| A6 | passed | brief.md | A6：首期小鸡由 Incus 管理且类型为系统容器，其运行资源可在 Incus 侧对应核对；首期验收不要求安装 Podman 或虚拟机后端。 | 创建 type=container，VM 上 Alpine 系统容器 RUNNING，未要求 Podman/VM 后端。 |
| A7 | passed | brief.md | A7：在选定小鸡的详情中查询该小鸡内部进程，结果明确标识该实例；切换小鸡后返回新选小鸡内部进程，而不是母机进程，也不是仅返回母机上的容器运行进程。 | 进程 API 带 instanceId，经容器 PID 命名空间读取；实机列表为小鸡 PID，切换 id 会重拉。 |
| A8 | failed | brief.md | A8：一次批量创建请求能够创建多个可分别识别的 Incus 系统容器，Agent 与 Incus 中的实例一一对应。 | Count 可>1，但只观察到 1 台；且每台 POST 整份 forward 到同一 listen IP，多台会冲突。 |
| A9 | failed | brief.md | A9：对多个已停止的小鸡提交批量启动后，每台目标小鸡进入运行状态。 | 批量启动只是浏览器串行单台 API，无持久任务，也无多台已停止实例被拉起的证据。 |
| A10 | failed | brief.md | A10：对多个运行中的小鸡提交批量停止后，每台目标小鸡进入停止状态。 | 批量停止同样不是持久批次，且没有多台运行中实例被停掉的证据。 |
| A11 | failed | brief.md | A11：对多个运行中的小鸡提交批量重启后，每台目标小鸡完成重启并恢复运行。 | 列表无批量重启；无多台重启并恢复运行的任务或实机结果。 |
| A12 | failed | brief.md | A12：对多个小鸡提交批量 CPU 调整后，每台目标小鸡应用所提交的有效 CPU 配置，Agent 展示值与 Incus 实际配置相符。 | 无批量 CPU 调整任务/UI，仅有单台 PATCH，未核对多台 Incus 实际配置。 |
| A13 | failed | brief.md | A13：对多个小鸡提交批量重装后，每台目标小鸡完成所选系统镜像的重装，并能分别核对重装结果。 | 无批量重装；单台 rebuild 不带镜像源且不恢复 SSH/转发，未逐台核对。 |
| A14 | failed | brief.md | A14：对多个小鸡提交批量删除后，成功删除的小鸡在 Agent 和 Incus 中均不再作为存续实例出现。 | 无批量删除任务，删除是单台同步 API，未证明多台从 Agent 与 Incus 同时消失。 |
| A15 | failed | brief.md | A15：批次执行期间能够逐台查看执行进度，执行完成后能够逐台查看结果，结果与对应实例关联。 | 仅创建任务有逐台进度；启动/停止等批次没有与实例关联的持久结果。 |
| A16 | passed | brief.md | A16：批量创建小鸡时能够为每台分配连续端口号段，创建后可查看每台小鸡对应的号段起止及归属。 | 创建时分配连续号码并写入 ports，详情可查看各号归属。 |
| A17 | failed | brief.md | A17：已有小鸡能够单独追加端口或指定待分配端口，并可查看新增端口的归属。 | 没有追加或指定单端口的 API/UI。 |
| A18 | passed | brief.md | A18：可分别为小鸡设置 0.5、1、2 核等共享算力上限；该额度表示 CPU 时间硬上限而非 vCPU 个数。首期 Incus 应用对应的时间硬上限（0.5 核为 50ms/100ms），Agent 展示额度与实际配置一致；额度未配置为绑核时由系统调度。 | 额度写入 limits.cpu.allowance（0.5=50ms/100ms），默认不绑核；VM 上该实例配置已核对。 |
| A19 | failed | brief.md | A19：可为小鸡指定逻辑 CPU 编号，生效的 CPU 集合与选择一致；取消绑核后解除该实例的指定编号限制，并保留其算力上限。 | 可写 limits.cpu，但不校验可用编号，无取消绑核 UI/验证，无效集合不会被拒绝。 |
| A20 | failed | brief.md | A20：能够分别查看母机与各小鸡的网络实时图表，图表随新采样更新并明确对应对象。 | ResourceCharts 只画 CPU 折线，无网络实时图，系列也不随新采样轮询。 |
| A21 | failed | brief.md | A21：能够查询母机与各小鸡在保留范围内的网络历史曲线，按时间展示已记录的历史数据。 | 样本含 rx/tx 但界面不展示网络历史，也无按时间粒度查询。 |
| A22 | failed | brief.md | A22：能够分别查看母机与各小鸡的 CPU 实时图表，图表随新采样更新并明确对应对象。 | CPU 图只在进入页面加载一次，不随 5 秒采样更新，也未分对象实时刷新。 |
| A23 | failed | brief.md | A23：能够查询母机与各小鸡在保留范围内的 CPU 历史曲线，按时间展示已记录的历史数据。 | 仅有 24h 原始点且图无时间轴，不能按保留范围查询 CPU 历史曲线。 |
| A24 | failed | brief.md | A24：选定小鸡的内部进程列表显示实时 CPU、内存用量，支持按 CPU 或内存排序，以定位该小鸡内的高占用进程。 | 进程表可按 CPU/RSS 排序，但 CPU 是累计 jiffies 而非 2 秒实时占用。 |
| A25 | blocked | brief.md | A25：首期功能验证以 Win11 VMware Workstation 中的 Debian 13 x86_64、2 vCPU / 4GB RAM 母机虚拟机为基线，实际测试配置与结果明确记录；数量目标按 A28 的 8–16 台执行。 | Agent 装在 Debian 13 VM，但未记录 2vCPU/4GB/VMware 基线与完整测试结果。 |
| A26 | blocked | brief.md | A26：通过 VMware 本地模拟的 IPv4、IPv6 和 NAT 网络验证确认范围内的连接路径，记录客户端、母机及小鸡的测试位置与结果，并明确模拟网络证据的范围。 | 没有客户端/母机/小鸡的 IPv4、IPv6、NAT 路径测试记录。 |
| A27 | passed | brief.md | A27：Agent 能够识别和管理由 particeps 新建的小鸡；同母机上预先存在或由其他工具创建的 Incus 实例不会被自动接管、修改或清理。 | 只管理库内带 user.particeps.id 的新建实例，代码不会扫描或清理未知 Incus 对象。 |
| A28 | failed | brief.md | A28：在指定的本地母机基线上覆盖 8 台与 16 台轻负载小鸡的管理和监控场景，记录实例数量、资源配置、运行负载和实际结果；CPU 限额的压力验证单独进行，不将该数量表述为全部实例同时满载的性能保证。 | 本轮只有 1 台轻负载小鸡，未覆盖 8 台与 16 台管理监控场景。 |
| A29 | passed | brief.md | A29：默认端口池为 20000–59999，每台新建小鸡默认分配 20 个连续端口号，池范围与数量可调整；一个号码的 TCP/UDP 归同一台小鸡，配额按端口号计数。 | 默认池 20000–59999、每台 20 号、TCP/UDP 同号计数，范围与数量可在配置调整。 |
| A30 | passed | brief.md | A30：新建小鸡号段内首个 TCP 端口默认转发到该小鸡的 22，其他 TCP 端口和全部 UDP 端口默认同号转发，不额外分配 SSH 号码。 | 号段首个 TCP 目标为 22，其余 TCP/全部 UDP 同号转发，不另占 SSH 号。 |
| A31 | failed | brief.md | A31：修改已分配端口的目标端口后，展示与实际转发使用新目标，端口号码的归属不变。 | 没有修改已分配端口目标的 API/UI，详情表只读。 |
| A32 | failed | brief.md | A32：小鸡停止、重启或重装后保留已分配号段、单端口和用户配置的转发目标；需要变更实例内部地址时更新规则关联。 | 停机保留库内端口，但重装/地址变化不会更新 Incus forward 关联。 |
| A33 | failed | brief.md | A33：仅在实例删除成功且相应转发清理完成后回收端口；仍待清理的号码保持占用并能看到待处理状态，不分配给另一台小鸡。 | 删除时忽略转发清理失败仍删 ports，没有待清理占用状态。 |
| A34 | failed | brief.md | A34：管理员已将至少一个 IPv4 纳入可用池时，可创建 IPv4 NAT 小鸡，出站经 SNAT，入站经所选 IPv4 的 DNAT/端口映射。 | 出站依赖桥 ipv4.nat 而非指定 NAT IPv4；入站 forward 不完整。 |
| A35 | failed | brief.md | A35：管理员已将 IPv6 前缀纳入可用池时，新建小鸡优先获得独立公网 IPv6；若同时允许 IPv4，则 IPv4 NAT/映射仍可使用。 | 有前缀时只把拼出的地址写入数据库，未给实例配置独立公网 IPv6。 |
| A36 | passed | brief.md | A36：创建界面和 API 的初始系统选项包含 Alpine 与 Debian 13，默认系统为 Alpine。 | 创建表单与镜像列表含 alpine/3.21/cloud 与 debian/13/cloud，默认 Alpine。 |
| A37 | blocked | brief.md | A37：分别创建 Alpine 和 Debian 13 小鸡时，实际运行系统与选择一致，实例记录能够对应所使用的镜像。 | Alpine 已创建并记录镜像，但本候选没有 Debian 13 小鸡的运行证据。 |
| A38 | blocked | brief.md | A38：创建小鸡时可导入 SSH 公钥，初始化后能够使用对应私钥通过 SSH 登录目标小鸡。该验收验证的是持有对应私钥的网络 SSH，不替代管理员 Web 终端。 | 可导入公钥并尝试写入 authorized_keys，但没有对应私钥经 SSH 登录成功的证据。 |
| A39 | passed | brief.md | A39：每台小鸡可单独设置或自动生成初始密码，启用密码登录时所配置凭据可用于目标小鸡；自动生成不使用所有实例共用的固定默认密码。 | 空密码时按实例生成随机口令并经 chpasswd 设置，不是全局固定默认密码。 |
| A40 | failed | brief.md | A40：配置关闭密码登录后，SSH 密码认证不再可用，已配置公钥仍可用于登录。 | passwordLogin 只入库，未配置 sshd 关闭密码认证。 |
| A41 | failed | brief.md | A41：有管理权限的管理员可在实例详情打开运行中小鸡的交互式 Web 终端，执行输入并查看输出。该通道经 Agent 与 Incus 进入实例，不使用导入的 SSH 公钥或私钥。即使仅导入他人公钥、关闭密码登录或 SSH 映射不可用，只要 Agent 与 Incus 可用，管理员仍可进入该小鸡。 | 无 Web 终端组件、无 Incus 交互会话 API。 |
| A42 | blocked | brief.md | A42：本地验证母机的虚拟磁盘目标容量为 50GB，存储池与日志、指标预算依据实际可用空间确定，并记录实际验证配置。 | 未记录 50GB 虚拟盘与存储池实际可用空间；安装脚本默认只建 16GiB 池。 |
| A43 | failed | brief.md | A43：批量任务默认最多并发处理 2 台且可调整，任务与逐实例进度持久保存；关闭浏览器后已提交任务仍可继续并重新查询。 | 仅创建任务持久且默认并发 2；列表批量启停随浏览器中断，不能关闭后再查。 |
| A44 | failed | brief.md | A44：单台失败时继续其他独立任务，保留成功结果并逐台给出失败原因；发起失败项重试时不重新执行成功项。 | 创建项失败可继续，但没有只重试失败项、跳过成功项的接口。 |
| A45 | failed | brief.md | A45：相同幂等键和相同请求重复提交时返回同一任务，不重复创建或重装；同键不同请求被明确拒绝。 | 幂等键只用于创建；重装等变更没有同键去重/冲突拒绝。 |
| A46 | failed | brief.md | A46：Agent 中断重启后先核对 Incus 实际操作和实例状态，再恢复任务；结果不明的重装不自动重复执行，能查看待核对状态。 | 重启只恢复电源，不核对未完成任务；needs-review 重装记录无逐实例待核对视图。 |
| A47 | passed | brief.md | A47：允许 CPU 额度合计超过母机逻辑 CPU 数以及超过小鸡合计硬上限，面板显示已配置额度、母机容量、合计硬上限和超分比例；2 vCPU 母机配置合计 8 核时显示 4 倍额度配置。 | 允许额度之和超过总帽，概览展示已配置额度、母机核数、总帽和超分倍率。 |
| A48 | failed | brief.md | A48：有效 CPU 改配在运行中应用并核对实际值，重启后保持；无效或不可用 CPU 编号被拒绝，不能只改变界面显示或静默重启小鸡。 | 改配写库并 PATCH Incus，但不回读核对，也不拒绝无效/不可用 CPU 编号。 |
| A49 | failed | brief.md | A49：默认后台每 5 秒采集 CPU/网络；打开选定小鸡的进程视图时每 2 秒刷新该小鸡，离开后停止该视图的高频查询；参数可调整。 | 默认 5 秒采集与详情 2 秒刷进程存在，但进程间隔写死，采样参数不能在面板调整。 |
| A50 | failed | brief.md | A50：原始资源样本默认保留 24 小时，分钟汇总保留 30 天；历史查询选择相应粒度，过期清理不删除管理数据或未完成任务。 | 只删 24h 前原始样本，没有 30 天分钟汇总及按粒度查询。 |
| A51 | failed | brief.md | A51：首次差分不足、计数器重置、采集失败或缺测被正确标识，不制造负速率或用伪造零值补齐历史；界面提供最后有效采样时间。 | 后端能标 first/reset/missing，界面不展示质量缺口和最后有效采样时间。 |
| A52 | failed | brief.md | A52：CPU 图表按实际有效额度表达使用比例，0.5 核额度使用 0.25 核时显示 50%；历史依据采样时配置，不被后续改配重新解释。 | API 虽算 quotaPercent，图表按绝对核数缩放，不能显示 0.5 核用 0.25 为 50%。 |
| A53 | passed | brief.md | A53：未认证的管理数据请求被拒绝；只读 Token 能查询但不能变更实例或打开终端，管理 Token 按权限工作，撤销后立即失效。 | 未认证 401；只读 Token 不能写操作；管理 Token 可写，撤销后 hash 立即失效。 |
| A54 | failed | brief.md | A54：管理员密码、API Token 和小鸡密码不在普通查询或日志中明文回显；初始化凭据仅向有管理权限的调用者交付，临时执行所需凭据受到保护并在完成后清除。 | 管理会话 GET /tasks/{id} 长期回显初始明文密码，不是一次性受保护交付。 |
| A55 | failed | brief.md | A55：交付 /api/v1 的接口契约和示例；外部程序能按文档提交任务、查询逐实例结果与监控，行为与 Web 面板一致。 | docs/api/openapi.yaml 只有提纲级路径，不能按文档覆盖任务与监控契约。 |
| A56 | failed | brief.md | A56：在指定 Debian 13 环境可安装并启动 Agent 与专用后端资源；手动更新前备份管理数据并校验新包，不接管或覆盖归属不明的已有资源。 | Debian 13 安装脚本可装 Agent/Incus 并跳过已有同名资源，但没有更新前备份与新包校验。 |
| A57 | blocked | brief.md | A57：Agent 停止或重启不主动停止已运行小鸡、撤销有效转发或删除管理数据；通过外部测试端验证已有业务连接的实际情况。 | Agent 停止不会主动删实例，但无外部探测证明转发连接在重启后仍存活。 |
| A58 | failed | brief.md | A58：创建模板可调整 CPU、内存和系统盘额度；Alpine 默认 0.5 核 / 128 MiB / 1 GiB，Debian 13 默认 0.5 核 / 256 MiB / 4 GiB。实际 Incus/cgroup/文件系统限制与配置一致；资源不足或参数无效时明确失败，不显示虚假的生效结果。 | Alpine 默认 0.5/128/1 正确，但选 Debian 仍用 128MiB/1GiB，也无资源不足失败路径。 |
| A59 | blocked | brief.md | A59：对运行中小鸡提交强制停止后，目标小鸡进入停止状态；普通停止先尝试优雅停止。 | 普通 stop 与 force-stop 已分发 Incus force 标志，但没有运行中实例被停掉的实机结果。 |
| A60 | failed | brief.md | A60：管理员可对运行中小鸡重置密码；启用密码登录时新密码可用于 SSH；仅公钥模式仍可在系统内设置密码，但 SSH 密码认证保持关闭。新密码按受保护方式交付，不在普通列表或日志中明文回显。 | 重置可返回新密码并 chpasswd，但未证明可用于 SSH，仅公钥模式也不会保持关闭密码认证。 |
| A61 | failed | brief.md | A61：对小鸡提交有效内存上限后，运行中应用，Agent 展示值与 Incus 实际配置相符，重启后保持。 | 有内存 PATCH，无界面改配，也不回读 Incus limits.memory 展示。 |
| A62 | failed | brief.md | A62：可提高小鸡系统盘额度，实际 Incus 磁盘限制与配置一致；降低额度被拒绝并说明原因，不显示虚假缩小结果。 | API 拒绝缩小磁盘，但无提高额度的 UI/实机核对，失败时仍可能先写库。 |
| A63 | failed | brief.md | A63：可为小鸡设置网络带宽上限（Mbps，0 表示不限制）；Incus 网卡入出站限制与配置一致，运行中可改配。 | 带宽可 PATCH 网卡限制，无 UI，且错误被忽略，未证明与 Incus 一致。 |
| A64 | failed | brief.md | A64：概览显示母机 CPU、内存、磁盘、负载、运行时间、上行实时速率，以及受管小鸡合计 CPU 用量与合计硬上限，并随新采样更新。 | 概览有 CPU/内存/磁盘/负载/上行与总帽，但无运行时间，合计 CPU 是额度而非实际用量。 |
| A65 | failed | brief.md | A65：实例列表显示每台当前 CPU、内存占用和网络速率，支持按这些字段排序。 | 列表显示当前 CPU/内存/网络，但不支持按这些字段排序。 |
| A66 | failed | brief.md | A66：保存每台小鸡的期望运行或停止状态；母机或 Agent 重启后，期望运行的恢复运行，用户停止的保持停止。 | desired_power 会保存并在启动时对齐，但 boot.autostart 恒 true，期望停止会被自动拉起。 |
| A67 | failed | brief.md | A67：除 Alpine 与 Debian 13 外，可登记本机已导入的 Incus 镜像供创建和重装选择；未登记镜像不会出现在默认选项中。 | 登记镜像不核对本机 Incus 别名/指纹，任意字符串都会进入选项。 |
| A68 | failed | brief.md | A68：可查看当前统计周期的累计观测流量，并手动重置周期计数；重置不影响历史曲线、管理数据或未完成任务。 | traffic 表未写入，无周期累计流量查询或手动重置 API。 |
| A69 | failed | brief.md | A69：可为全部小鸡设置默认的唯一来源 IP 上限，也可按小鸡覆盖；0 表示不限制。达到上限后，新的来源 IP 被拒绝，已在限额内的来源仍可建立连接。面板能查看当前活跃来源数与生效限额。 | source_ip_limit 仅配置/列占位，没有 conntrack/nft 限制和活跃来源展示。 |
| A70 | passed | brief.md | A70：Agent 能检测母机上行接口上的全局 IPv4、全局 IPv6 地址以及可识别的 IPv6 前缀，并在 Web 中展示检测结果与来源接口。 | 启动/设置页扫描非回环全局 IPv4/IPv6 及可识别前缀，并显示接口。 |
| A71 | passed | brief.md | A71：管理员可在 Web 勾选小鸡允许使用的 IPv4 地址、IPv6 地址和 IPv6 前缀；未勾选的地址不用于新的分配或映射。 | 设置页可勾选 IPv4/IPv6/前缀并保存到 network_pool，创建使用池内地址。 |
| A72 | failed | brief.md | A72：池中有 IPv4 时，小鸡 IPv4 出站使用 SNAT，源地址为管理员指定的 NAT IPv4。 | 出站 SNAT 来自桥 ipv4.nat 伪装，不是管理员指定的 NAT IPv4。 |
| A73 | failed | brief.md | A73：池中有 IPv4 时，端口映射监听在管理员指定的公网 IPv4 上，DNAT 到对应小鸡。 | Agent CreateForward 不完整，曾误用 CLI 对母机主地址做默认转发。 |
| A74 | failed | brief.md | A74：池中有可分配的额外公网 IPv4 时，可为小鸡分配独立公网 IPv4；该地址不再用于其他小鸡的独占分配。 | dedicatedV4 从未在创建时独占分配。 |
| A75 | failed | brief.md | A75：池中有 IPv6 地址但没有可用前缀、或管理员选择 NAT66 时，小鸡 IPv6 出站 SNAT、入站可在该 IPv6 上做 DNAT/端口映射。 | NAT66 只把地址写入记录，没有 IPv6 SNAT/DNAT 编程。 |
| A76 | failed | brief.md | A76：池中有 IPv6 前缀时，默认优先为小鸡分配独立公网 IPv6，而不是 NAT66。 | 有前缀时只拼接字符串当 ipv6 字段，未做独立公网 IPv6 路由/邻居代理。 |
| A77 | passed | brief.md | A77：池中仅有 IPv4、或创建时选择 IPv4-only 时，小鸡只有 IPv4 连通性。 | v4 模式不分配 IPv6，并为 eth0 接入 IPv4 桥；本轮 Alpine 即此路径。 |
| A78 | failed | brief.md | A78：池中仅有 IPv6、或创建时选择 IPv6-only 时，小鸡只有 IPv6 连通性；同族 IPv6 入站可用，不把 IPv4 映射到 IPv6-only 小鸡。 | v6-only 不挂 nic，桥接也禁用 IPv6，没有同族 IPv6 入站。 |
| A79 | failed | brief.md | A79：池中同时有 IPv4 与 IPv6 能力且选择双栈时，小鸡同时具备已启用的 IPv4 与 IPv6 连通性。 | dual 只配置 IPv4 网卡并把 IPv6 写入库，不具备已启用的双栈连通。 |
| A80 | failed | brief.md | A80：Web 创建设置只列出当前池能支持的网络模式；不支持的组合被拒绝并说明缺少哪类地址或前缀。 | 创建表单固定列出 v4/v6/dual，不按当前池过滤；仅 API 校验会拒绝。 |
| A81 | blocked | brief.md | A81：管理员可设置所有受管小鸡的合计 CPU 时间硬上限。无论实例数量以及单台额度之和是否超过该上限，这些小鸡的实际 CPU 时间合计不超过该上限。出厂默认为母机逻辑 CPU 数的 75%，可在 Web 调整。单台额度仍为该台硬上限。单台额度大于合计硬上限的配置被拒绝。该上限不限制 Agent、Incus 守护进程和母机系统进程。 当前范围对应 A3。验收以当前连续编号为准，当前尚未进行最终 Shape 确认。 | Web 可调总帽、默认 75%、单台超额拒绝、父组 cpu.max 与嵌套路径已见，但无负载下合计 CPU 不超过上限的测量。 |

### 检查

_没有记录 Runtime 检查。_

### 阻塞项

_无。_

### 风险与跳过的工作

- 多台小鸡共用 NAT IPv4 时对同一 listen_address 重复 POST network forward，第二台会冲突
- IPv6-only 创建不挂网卡且桥接 ipv6.address=none，无 IPv6 连通性
- 启动失败会清掉 raw.lxc，小鸡可能掉出 particeps-guests 父组
- boot.autostart 恒为 true，与期望停止冲突
- 任务结果长期保存明文初始密码
- 禁止再对母机主地址建立无端口列表的默认 target 转发

### 之前的迭代

| 目标周期 | 迭代 | 尝试 | 结果 | 未解决项 | 摘要 | 完成时间 |
| ---: | ---: | ---: | --- | --- | --- | --- |
| 1 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T08:04:59.919Z |
| 2 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T08:20:50.348Z |
| 3 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T08:35:27.677Z |
| 4 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T09:05:11.448Z |
| 5 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T09:20:00.489Z |
| 6 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T09:31:11.122Z |
| 7 | 0 | 0 | recovery | — | Native confirmed acceptance criteria changed | 2026-09-11T10:06:14.971Z |
| 8 | 1 | 1 | fail | A4, A5, A8, A9, A10, A11, A12, A13, A14, A15, A17, A19, A20, A21, A22, A23, A24, A25, A26, A28, A31, A32, A33, A34, A35, A37, A38, A40, A41, A42, A43, A44, A45, A46, A48, A49, A50, A51, A52, A54, A55, A56, A57, A58, A59, A60, A61, A62, A63, A64, A65, A66, A67, A68, A69, A72, A73, A74, A75, A76, A78, A79, A80, A81 | 第一轮候选交付了独立 Go/Vue 母机 Agent，并在 Debian 13 VM 上跑通登录、Alpine 创建、0.5 核 allowance、父 cgroup cpu.max、密码重置和小鸡进程列表；但批量生命周期、端口编辑、来源 IP 限制、NAT66/独立 IPv6、Web 终端、分钟汇总/流量重置、OpenAPI 与 8–16 台规模均未达到验收。另：曾误把母机 LAN 地址做成 Incus 默认转发，已删除。 | 2026-09-11T11:32:26.559Z |



### 结论

第一轮候选交付了独立 Go/Vue 母机 Agent，并在 Debian 13 VM 上跑通登录、Alpine 创建、0.5 核 allowance、父 cgroup cpu.max、密码重置和小鸡进程列表；但批量生命周期、端口编辑、来源 IP 限制、NAT66/独立 IPv6、Web 终端、分钟汇总/流量重置、OpenAPI 与 8–16 台规模均未达到验收。另：曾误把母机 LAN 地址做成 Incus 默认转发，已删除。

