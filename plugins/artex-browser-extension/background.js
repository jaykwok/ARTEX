// Cap simultaneous recording panels. No inspected-page scripts or network proxy.
const panels = new Set();
chrome.runtime.onConnect.addListener((port) => {
  if (port.name !== "artex-panel") return;
  if (panels.size >= 20) { port.postMessage({ error: "最多同时打开 20 个 ARTEX 面板" }); port.disconnect(); return; }
  panels.add(port);
  port.onMessage.addListener(() => {}); // Panel heartbeat keeps the MV3 worker's slot registry alive.
  port.onDisconnect.addListener(() => panels.delete(port));
});
