/** Match the most specific navigation entry, including static-export trailing slashes. */
export function activeNavigationUrl(pathname: string, urls: readonly string[]): string | undefined {
  const normalize = (value: string) => {
    const path = value.split(/[?#]/, 1)[0];
    try {
      return decodeURIComponent(path).replace(/\/+$/, "") || "/";
    } catch {
      return path.replace(/\/+$/, "") || "/";
    }
  };
  const path = normalize(pathname);
  return urls
    .filter((url) => {
      const candidate = normalize(url);
      return path === candidate || (candidate !== "/" && path.startsWith(`${candidate}/`));
    })
    .sort((a, b) => normalize(b).length - normalize(a).length)[0];
}
