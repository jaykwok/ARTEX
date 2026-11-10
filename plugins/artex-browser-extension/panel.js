import { normalize, backendOrigin } from "./normalize.mjs";
const $ = (id) => document.getElementById(id);
const encoder = new TextEncoder();
let entries = [], selected = null, session = null, bytes = 0, busy = false, alive = true;
let generation = 0, pendingBodies = 0;
const port = chrome.runtime.connect({ name: "artex-panel" });
const heartbeat = setInterval(() => { if (alive) port.postMessage({ ping: true }); }, 20000);
port.onMessage.addListener((message) => { if (message.error) { alive = false; clearInterval(heartbeat); status(message.error, true); } });
port.onDisconnect.addListener(() => { alive = false; clearInterval(heartbeat); status("采集连接已关闭，请重新打开面板。", true); });
function status(message, error = false) { $("status").textContent = message; $("status").className = error ? "error" : ""; }
async function api(path, body, auth = session) {
  if (!auth) throw new Error("请先连接 ARTEX");
  const response = await fetch(`${auth.base}/api${path}`, { method: body ? "POST" : "GET", credentials: "omit", redirect: "error", signal: AbortSignal.timeout(30000),
    headers: { ...(body ? { "Content-Type": "application/json" } : {}), ...(auth.token ? { Authorization: `Bearer ${auth.token}` } : {}) }, body: body ? JSON.stringify(body) : undefined });
  const data = await response.json();
  if (!response.ok) { if (response.status === 401) await logout(); throw new Error(data.error || `HTTP ${response.status}`); }
  return data;
}
async function loadTasks() {
  const data = await api("/tasks");
  const tasks = Array.isArray(data) ? data : data.tasks || [];
  $("task").replaceChildren(new Option("选择任务", ""), ...tasks.map((t) => new Option(`${t.id} · ${t.name || t.description || t.goal || "任务"}`, String(t.id))));
}
async function logout() { session = null; await chrome.storage.session.remove("artexConnection"); $("task").replaceChildren(new Option("连接后选择任务", "")); refresh(); }
$("logout").onclick = () => logout().then(() => status("已退出"));
$("connect").onclick = async () => {
  if (busy) return;
  busy = true; $("connect").disabled = true; refresh();
  $("connect").textContent = "连接中…";
  try {
    const base = backendOrigin($("base").value);
    if (!await chrome.permissions.request({ origins: [`${base}/*`] })) throw new Error("未授予此 ARTEX 地址的连接权限");
    const password = $("password").value; $("password").value = "";
    await logout();
    const result = await api("/auth/login", { username: "ARTEX", password }, { base });
    if (!result.token) throw new Error("登录未返回 token");
    session = { base, token: result.token };
    await chrome.storage.session.set({ artexConnection: session });
    await loadTasks(); status("已连接。选择请求与目标任务后可导入。"); refresh();
  } catch (error) { status(error.message, true); }
  finally { busy = false; $("connect").disabled = false; $("connect").textContent = "连接"; refresh(); }
};
function current() { if (!selected) throw new Error("请选择请求"); return normalize(selected, $("credentials").checked); }
function refresh() {
  let valid = false;
  if (selected) { try { $("preview").textContent = JSON.stringify(current(), null, 2) + (selected.bodyNote ? `\n\n${selected.bodyNote}` : ""); valid = true; } catch (error) { $("preview").textContent = error.message; } }
  else $("preview").textContent = "选择请求后预览即将导入的内容";
  for (const id of ["sync", "explore"]) $(id).disabled = busy || !valid || !session || !$("task").value;
  $("task").disabled = busy || !session;
  $("credentials").disabled = busy;
  $("logout").disabled = busy || !session;
  $("selection-hint").textContent = busy ? "正在处理，请稍候…" : !session ? "请先连接 ARTEX。" : !$("task").value ? "请选择要接收流量的目标任务。" : !valid ? "请选择左侧的一条请求。" : "可以导入。指派高优意图会同时导入请求并安排任务探索。";
}
$("credentials").onchange = refresh; $("task").onchange = refresh;
function render() {
  $("request-count").textContent = `${entries.length} 条`;
  $("requests").replaceChildren(...entries.map((entry) => {
    const button = document.createElement("button"); button.type = "button"; button.textContent = `${entry.request.method} ${entry.response.status} ${entry.request.url}`;
    button.setAttribute("aria-pressed", String(entry === selected)); button.title = entry.request.url;
    button.onclick = () => { selected = entry; render(); refresh(); }; return button;
  }));
  if (!entries.length) {
    const empty = document.createElement("p"); empty.className = "empty-state"; empty.textContent = "暂无请求。保持此面板打开，再刷新目标页面。"; $("requests").append(empty);
  }
}
$("clear").onclick = () => { generation++; pendingBodies = 0; entries = []; selected = null; bytes = 0; render(); refresh(); };
chrome.devtools.network.onRequestFinished.addListener((request) => {
  if (!alive || pendingBodies >= 32 || !/^https?:/.test(request.request.url)) return;
  const metadata = JSON.stringify({ request: request.request, response: request.response });
  if (encoder.encode(metadata).length > 600 * 1024) return;
  const capturedGeneration = generation;
  pendingBodies++;
  request.getContent((content, encoding) => {
    if (!alive || capturedGeneration !== generation) return;
    pendingBodies--;
    const entry = JSON.parse(metadata);
    const unavailable = encoding === "base64" || typeof content !== "string" || encoder.encode(content).length > 512 * 1024;
    entry.responseBody = unavailable ? "" : content;
    entry.bodyNote = unavailable ? "响应正文未导入（浏览器未提供、二进制或超过 512 KiB）；不会将空正文作为漏洞证据。" : "";
    entry.size = encoder.encode(JSON.stringify(entry)).length;
    entries.push(entry); bytes += entry.size;
    while (entries.length > 200 || bytes > 4 * 1024 * 1024) { const old = entries.shift(); bytes -= old.size; if (selected === old) selected = null; }
    render(); refresh();
  });
});
async function send(createIntent) {
  if (busy) return;
  busy = true; refresh();
  try { const result = await api("/browser/import", { task_id: $("task").value, exchange: current(), create_intent: createIntent }); status(`已导入：流量 ${result.traffic_id}，资产 ${result.asset_id}${result.intent_id ? `，意图 ${result.intent_id}` : ""}`); }
  catch (error) { status(error.message, true); }
  finally { busy = false; refresh(); }
}
$("sync").onclick = () => send(false); $("explore").onclick = () => send(true);
chrome.storage.onChanged.addListener((changes, area) => {
  if (area !== "session" || !changes.artexConnection) return;
  if (!changes.artexConnection.newValue) { session = null; $("task").replaceChildren(new Option("连接后选择任务", "")); refresh(); }
});
chrome.storage.session.get("artexConnection").then(async ({ artexConnection }) => {
  if (!artexConnection) return;
  session = artexConnection; $("base").value = session.base;
  try { await loadTasks(); status("已恢复连接"); refresh(); } catch (error) { status(error.message, true); }
});
render(); refresh();
