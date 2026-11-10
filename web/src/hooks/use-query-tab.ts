"use client";

import { useSearchParams } from "next/navigation";

/** Keep tabs shareable and restore them on refresh and browser back/forward. */
export function useQueryTab(values: readonly string[], fallback: string) {
  const params = useSearchParams();
  const requested = params.get("tab") ?? fallback;
  const tab = values.includes(requested) ? requested : fallback;
  const setTab = (next: string) => {
    if (!values.includes(next) || next === tab) return;
    const url = new URL(window.location.href);
    if (next === fallback) url.searchParams.delete("tab");
    else url.searchParams.set("tab", next);
    window.history.pushState(null, "", `${url.pathname}${url.search}${url.hash}`);
  };
  return [tab, setTab] as const;
}
