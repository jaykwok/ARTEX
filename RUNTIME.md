# 运行时治理与浏览器协同：修订方案及启用说明

评估日期：2026-11-10。基线：ARTEX `25c34ee`，Norma `v0.4.3`。

## 评估结论与修订

六个借鉴方向有价值，重点是补齐现有系统的边界，而非复制一套执行引擎。源码核对后修正如下：

| 方向 | 原方案需要修正的假设 | 本次实现 |
| --- | --- | --- |
| 弹性执行 | Bash 已有后台任务机制；仅按连接实例限流不能限制多个 worker | 复用 Bash 的 TaskOutput/TaskStop；MCP/配方增加会话级执行管理；HTTP/SSE MCP 跨会话共享信号量及熔断器 |
| 输出与历史 | Norma Capture 已能落盘，消息发送前也已清理孤立工具调用；归档恢复已分批写库 | MCP 首尾预览及完整文件引用，限制伪造标记绕过截断；保留 SDK 历史配对逻辑与审计原文，用回归测试锁定行为 |
| YAML 配方 | 全量兼容模板、additional_args 会把任意命令拼接重新带回来 | 独立 version 1 参数契约、argv 执行、缺失依赖跳过、短描述目录与按需完整 schema |
| review_edit | 模型生成的替换命令不能直接视为更安全 | 可选自动数值收窄；其余修改转人工；重新匹配静态规则；审计保存原参数、执行参数、规则编号 |
| 终态验收 | 文字覆盖率、自然语言结论和节点存在都不等于满足目标 | 显式 coverage_min_pct、实测覆盖率核查、已确认 finding、shell 类别与证据检查、原子证明写入、最多两轮收尾续跑 |
| 视觉与浏览器 | 登录路径应为 /api/auth/login；采集流量不是已验证事实 | 独立视觉配置页与单次图像通道；MV3 DevTools 扩展、鉴权导入、幂等线索与高优意图 |

设计参考：[CyberStrikeAI 配置](https://github.com/AIPentest/CyberStrikeAI/blob/main/config.example.yaml)、[终态校验实现](https://github.com/AIPentest/CyberStrikeAI/blob/main/internal/agentfinalizer/decision.go)、[配方文档](https://github.com/AIPentest/CyberStrikeAI/blob/main/tools/README.md)。实现沿用 ARTEX 的双图、任务删除屏障、工具绑定与审计结构。

明确保留的边界：不伪造中断工具的成功结果，不截坏历史审计原文，不自动删除七天前的原始证据。输出文件随既有任务工作区管理；恢复会话后旧 execution_id 不会复活，也不自动重放可能产生副作用的调用。

## 1. 运行时配置

在现有 config.json 合并 config.example.json 的 runtime 段，保留已有数据库配置。建议重启后生效；运行中的会话与已建立的 MCP 熔断器保留原设置。

| 配置 | 默认值 | 含义 |
| --- | --- | --- |
| tool_wait_seconds | 120 | MCP/配方等待多久后返回 execution_id |
| tool_timeout_seconds | 1800 | 单次 MCP/配方的总时限 |
| no_output_seconds | 0 | 关闭静默终止；设正整数后监测 Bash/配方的输出活动 |
| mcp_concurrency | 5 | 同一 HTTP/SSE MCP 服务与凭据组合的并发上限 |
| mcp_failure_threshold | 5 | 连续传输/协议错误达到此次数后熔断 |
| mcp_cooldown_seconds | 60 | 冷却后只允许一个半开探测 |
| recipe_dir | 空 | 使用可执行文件同级 tools/；可设绝对目录 |

后台调用使用 get_tool_execution / wait_tool_execution / cancel_tool_execution；单次 wait 最多 60 秒。每个会话最多保留 128 个执行记录，并发执行最多 5 个。关闭会话取消本地等待与配方进程；远端服务器是否停止其实际工作取决于它自身，不把客户端取消当成远端已停止。

无输出不等于挂死，默认保持关闭。MCP 不透明的远端执行无法使用本机 stdout 看门狗，依赖总时限和连接取消。stdio MCP 的传输仍通过原 SDK；跨 worker 熔断与信号量针对 mcphttp 的 HTTP/SSE 传输。

配方本地进程使用 Unix 进程组或 Windows Job Object 清理进程树。Windows 配方 Job 限制 256 个进程、2 GiB 内存；Job 在启动后立即绑定，不能视为运行不可信二进制的安全沙箱。SDK Bash 继续使用自身进程管理。Linux 上覆盖 Bash、MCP 子进程和配方的资源上限由可选容器配置提供：

```bash
docker compose -f docker-compose.yml -f docker-compose.runtime.yml up -d --build
```

该配置为整个 ARTEX 容器设置 2 GiB / 256 PID 和 init 进程。没有隐式修改宿主机 cgroup，也不宣称裸机 Unix 进程组能限制内存。

## 2. 输出与历史

MCP 结果默认仅将约 30 KB 首尾预览送入主模型，原文写到当前运行目录的 cmd-output。JSON 响应/SSE 事件最多 16 MiB，配方输出最多 32 MiB，超过配方输出上限会终止执行。磁盘失败会明确报告“未能保存”，不会声称有完整证据。

Norma 的 Bash、自定义工具 Capture、消息配对修复及分批归档恢复继续复用。原始输出与历史不由本次变更定时删除。

## 3. 声明式工具

详见 [tools/README.md](tools/README.md)。内置两项小配方：HTTP HEAD、TLS 证书。加载器只发布本机可执行文件存在且校验通过的配方；错误记录到服务日志。

worker/retester 的初始工具目录展示名称与 short_description，SearchExtraTools 加载完整说明和参数，ExecuteExtraTool 执行。延迟调用会用真实工具名、真实参数进入 guard，避免分派器绕过审计。配方是管理员可执行配置，修改 command/args 等同于修改本地程序调用。

## 4. 审计改参

系统 → 拦截规则 → 模型审批中打开“允许审计改参”；默认关闭。已有 intercept_enabled_tools 配置的安装需把 recipe__* / analyze_image 加到受审工具集合。

模型可返回 decision=allow、comment、rule_id 和完整 edited_arguments。自动放行只接受参数键集合不变、其他值不变，以及已有正数 timeout / timeout_seconds / max_time / max_rate / rate / threads / concurrency / limit 的降低。命令、目标、路径、身份信息及其他结构修改必须人工审批；此类改参审批超时固定拒绝。修改后的参数重新命中 deny 会直接拒绝，命中 ask 则必须审批。

这是一套保守的参数协议，不是任意 shell 命令的语义证明。原有模型失败策略仍由安装配置决定。新增能力不会强行改变既有失败策略。

## 5. 证据验收

任务总览的目标管理可新增、修改或移除“最低覆盖率 %”。coverage_min_pct 的单位是 0–100；底层 coverage.pct 是 0–1，比对时转换。目标拆解器只为明确要求覆盖百分比的目标登记此字段；不从任意文字猜指标。空资产范围、关闭覆盖统计或覆盖不足均拒绝证明。

fact 必须 confirmed；finding 还必须在漏洞库 confirmed 且有非空证据。getshell/webshell/reverse_shell 目标只能使用同组 shell finding，不能用普通事实、RCE 或上传成功替代。目标状态和 proves 边在同一事务写入，并核对证据状态、内容和直接关联任务范围。修改目标后重新打开并删除旧证明边；整体完成时再次核验，goal_met 无法绕过。

worker 已进行 Bash/WebFetch/MCP/配方探测但没有任何事实、资产或 finding 写回，或仍有后台执行时，停止钩子最多要求两轮收尾：处理已有执行、记录已有结果或失败，不追加新的探测。仍未满足则返回失败。SDK 的预算与取消仍有效。

确定性门控能约束结构、状态、覆盖率及引用，不能从非空文字证明真实 shell 交互，也不能替代人工复核或漏洞复测。

## 6. 视觉与浏览器

在系统 → LLM 配置创建支持图片输入的模型配置，再在“视觉模型”页绑定。绑定存入现有 SQLite settings，复用模型配置的凭据；无需在 config.json 重复保存密钥。0 表示关闭，关闭时新会话不暴露 analyze_image。

analyze_image 只读当前运行目录内的 PNG/JPEG，拒绝越界及指向目录外的符号链接。限制原始文件 5 MiB、解码 2400 万像素、长边 2048、JPEG 质量 82、Base64 不超过 512 KiB。支持 OpenAI Chat Completions / Responses / Anthropic 协议，使用独立单次请求，最多 2048 输出 token、90 秒、4 个并发；无自动模型回退。只把文字摘要送回主 Agent，使用量进入账本，Base64 不进入原始 LLM 录制。

图片协议参考 [OpenAI 图像输入](https://developers.openai.com/api/docs/guides/images-vision) 与 [Anthropic 视觉](https://platform.claude.com/docs/en/build-with-claude/vision)。已用本地协议替身验证三种请求；真实账号需选择支持图片的模型后验证。

浏览器安装与使用见 [扩展说明](plugins/artex-browser-extension/README.md)。插件使用 /api/auth/login 与现有 Bearer 鉴权，导入接口为 POST /api/browser/import。导入请求不会重放；写入流量库、资产和来源为 browser 的 hint，选择探索时再创建 priority=10 意图并唤醒任务。

流量与主业务库属于两个 SQLite，不能形成单库原子事务；失败响应携带已保存 traffic_id，可重试补齐。内容哈希使流量去重，图谱写事务串行检查导入键，重复请求返回原节点。只有事实等后续验证写回才影响测试覆盖率。响应缺失和身份头省略都属于导入限制，不能被当作目标的真实空响应。

## 验证及交付边界

管理界面配套、导航与表单易用性改进，以及浏览器复查的具体边界，见 [UI_REVIEW.md](UI_REVIEW.md)。

测试覆盖 MCP 取消/熔断、完整输出与 UTF-8、literal argv、配方参数、跨会话执行隔离、静态规则复检、覆盖率与 shell 验收、证明事务回滚、并发幂等导入、接口鉴权和图片目录边界。扩展有 Node 的归一化与权限地址测试；前端使用项目现有 typecheck、lint、test、静态构建检查。

这次交付源码、前端构建和本地安装包。浏览器扩展未发布商店；开发者模式加载及真实浏览器采集、真实视觉模型的效果需在选定后端/模型上验收。没有替换正在运行的部署或修改现有业务数据库。
