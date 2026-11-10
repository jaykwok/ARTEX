# ARTEX 工具配方 v1

启动/装配会话时读取 runtime.recipe_dir（默认可执行文件同级 tools/）的 *.yaml。文件须不超过 128 KiB、仅一个 YAML 文档；未知字段、重复名称、缺失可执行文件均跳过并记录日志。修改后新会话加载新定义。

这是 ARTEX 的严格 argv 格式，不直接兼容 CyberStrikeAI 的 template / additional_args。模板插值、shell 拼接和自由追加参数不支持。静态子命令放在 args 中。

```yaml
version: 1
name: http_headers
enabled: true
command: curl
args: ["--head", "--max-time", "30", "--proto", "=http,https"]
short_description: 读取一个 HTTP 端点的响应头。
description: 请求只发送一次，不自动跟随重定向。
agents: [worker, retester]
parameters:
  - name: url
    type: string
    flag: --url
    required: true
    description: 完整 HTTP/HTTPS URL
```

- 类型：string / integer / boolean；拒绝未知参数。integer 可使用 minimum / maximum，非布尔参数可使用字符串 enum。
- flag 或 position 必须且只能设置一个。position 从 0 排序，位置参数放在所有 flag 参数后；不允许位置值以 `-` 开头。固定子命令放在 args，不能让模型选择可执行程序。
- flag 的普通形式为两个 argv 元素；combined: true 使用单个 `--flag=value`。boolean 为 true 添加 flag，为 false 省略。
- 短描述最多 200 字符，初始工具目录只展示名称和短描述；SearchExtraTools 返回完整 schema。
- 配方名称发布为 recipe__<name>，默认只分配 worker/retester；可通过 agents 明确指定。
- 输出落盘、总时限与静默看门狗见 [RUNTIME.md](../RUNTIME.md)。先在隔离测试目录中验证自定义配方。

内置 http_headers 依赖 curl，tls_certificate 依赖 openssl；本机缺少其中一个不会阻止 ARTEX 启动。
