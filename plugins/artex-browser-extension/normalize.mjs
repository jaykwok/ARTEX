const sensitive = /^(cookie|set-cookie|authorization|proxy-authorization|x-api-key|api-key)$/i;
export function normalize(entry, includeCredentials = false) {
  const req = entry.request;
  const url = new URL(req.url);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password) throw new Error("只支持无 URL 身份信息的 HTTP/HTTPS 请求");
  const headers = (items = []) => items.filter((h) => !h.name.startsWith(":") && (includeCredentials || !sensitive.test(h.name)))
    .map((h) => ({ name: h.name, value: h.value }));
  const body = req.postData?.text || "";
  if (req.postData && typeof req.postData.text !== "string") throw new Error("浏览器未提供完整请求体，无法导入");
  const result = {
    url: url.href, method: req.method, headers: headers(req.headers), body,
    status: entry.response.status || 0, response_headers: headers(entry.response.headers), response_body: entry.responseBody || "",
    response_body_omitted: Boolean(entry.bodyNote),
  };
  if (new TextEncoder().encode(body).length > 512 * 1024 || new TextEncoder().encode(result.response_body).length > 512 * 1024) throw new Error("请求或响应正文超过 512 KiB，请缩小采集内容");
  return result;
}
export function backendOrigin(value) {
  const url = new URL(value);
  if (!["http:", "https:"].includes(url.protocol) || url.username || url.password || (url.pathname !== "/" && url.pathname !== "") || url.search || url.hash) throw new Error("ARTEX 地址需为完整站点地址，不含路径或身份信息");
  if (url.protocol === "http:" && !["localhost", "127.0.0.1", "[::1]"].includes(url.hostname)) throw new Error("远程 ARTEX 请使用 HTTPS");
  return url.origin;
}
