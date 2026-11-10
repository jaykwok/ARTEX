# ARTEX DevTools（Manifest V3）

## 安装与连接

1. 在 Chrome 的扩展管理页启用开发者模式，选择“加载已解压的扩展程序”，选择本目录（其中应直接包含 manifest.json）。Edge 可在对应扩展页加载。
2. 打开目标站点的 DevTools，选择 ARTEX 面板；面板打开后刷新目标页面以采集请求。
3. 输入 ARTEX 后端的站点地址和管理员密码。默认 http://127.0.0.1:8787；远程后端需 HTTPS。不支持带路径的部署前缀。
4. 点击连接，授权该后端的访问权限，选择 ARTEX 中已有任务，再选择一条请求。预览展示将上传的内容。
5. “同步资产与流量”保存选中请求；“指派高优意图”同时创建 priority=10 意图并恢复/唤醒任务。后者需在 ARTEX 启用流量录制，确保 worker 能调用 traffic_get。

扩展复用 POST /api/auth/login（username=ARTEX）。JWT 只保存在 chrome.storage.session，密码在登录时清空，不使用网页内容脚本，不将令牌发到被检查站点。只请求指定后端 origin 的权限；退出时清除会话。权限机制依据 [Chrome 官方 permissions API](https://developer.chrome.com/docs/extensions/reference/api/permissions)。

## 采集与导入约定

- 采集来自 [DevTools Network API](https://developer.chrome.com/docs/extensions/reference/api/devtools/network)，不是通用抓包代理。只保存面板打开后完成的 HTTP/HTTPS 请求。
- 每个面板最多 200 条、4 MiB；最多 20 个面板，最多 32 个待取正文回调。关闭面板清除内存流量；清空后旧回调不会重新填入。
- 默认删除 Cookie / Set-Cookie / Authorization / Proxy-Authorization / X-API-Key / API-Key。需要测试登录态时显式勾选“包含身份头”。URL 参数与正文仍按预览上传，须查看预览后选取。
- HTTP/2 伪首部被移除，后端保存规范化 HTTP/1.1 表示。DevTools 常提供解码后的正文，因此不是原始 wire bytes，不保证可重放。
- 请求和响应正文各最多 512 KiB。未提供的请求体拒绝导入；二进制、不可用或过大的响应正文省略并显示说明。此类流量不能作为真实空响应证据。
- 导入不会重放请求。内容哈希及图谱幂等逻辑保证重试不重复生成意图；同内容再次导入不会自动生成第二个探索任务。
- 服务工作进程重启导致面板连接中断时会提示重新打开；不会悄悄继续采集。

## 验证

```bash
node --test plugins/artex-browser-extension/normalize.test.mjs
```

本地验收：连接测试 ARTEX → 打开测试页面并刷新 → 选择 GET 请求 → 默认身份头过滤 → 连续两次同步返回相同 traffic_id / hint_id → 打开 ARTEX 流量页核对 → 开启流量录制后指派意图。未经浏览器加载的源码检查不等于真实浏览器端到端验收。

图片识别需在「系统 → LLM 模型 → 视觉模型」中选择支持图片输入的配置并保存；审计改参在「系统 → 拦截规则 → 模型审批」中单独启用。两项能力默认关闭。
