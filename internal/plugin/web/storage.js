"use strict";
const VIEW_STORE = "cpa-codex-candy-eval.view.v1.";

function stored(name) {
  try { return JSON.parse(localStorage.getItem(name)); } catch (_) { return null; }
}
function store(name, value) {
  try { localStorage.setItem(name, JSON.stringify(value)); } catch (_) {}
}
function preference(name, fallback) {
  const value = stored(VIEW_STORE + name);
  return value !== null && typeof value === typeof fallback ? value : fallback;
}
function savePreference(name, value) {
  store(VIEW_STORE + name, value);
}

// CPAMC and CPAMP share the v2 prefix but use different host-based keys.
function panelValue(name) {
  try {
    const raw = localStorage.getItem(name);
    const prefix = /^enc::v([12])::/.exec(raw);
    if (!prefix) return JSON.parse(raw);
    const scopes = {
      1: [location.host + "|" + navigator.userAgent],
      2: [location.host, "v2|" + location.host],
    }[prefix[1]];
    const bytes = Uint8Array.from(atob(raw.slice(prefix[0].length)), (c) => c.charCodeAt(0));
    for (const scope of scopes) {
      const secret = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + scope);
      const decoded = bytes.map((byte, i) => byte ^ secret[i % secret.length]);
      try {
        return JSON.parse(new TextDecoder("utf-8", { fatal: true }).decode(decoded));
      } catch (_) {}
    }
  } catch (_) {}
  return null;
}

function panelKey() {
  const k = panelValue("cli-proxy-auth")?.state?.managementKey;
  return typeof k === "string" ? k.trim() : "";
}
