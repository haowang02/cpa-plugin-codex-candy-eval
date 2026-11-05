"use strict";

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

// null uses the host session; an empty string requires a management key.
function initialKey() {
  // OMC projects location.pathname onto CPA paths; href keeps the host mount.
  if (new URL(location.href).pathname.includes("/api/v1/plugin-host/")) return null;
  const saved = panelValue("cli-proxy-auth")?.state?.managementKey;
  if (typeof saved === "string" && saved.trim()) return saved.trim();
  try { return sessionStorage.getItem(KEY_STORE) || ""; } catch (_) { return ""; }
}

function followPanelTheme() {
  const root = document.documentElement;
  const ancestors = [];
  let frame = window;
  try {
    while (frame.parent !== frame) {
      frame = frame.parent;
      ancestors.push(frame.document.documentElement);
    }
  } catch (_) {} // Cross-origin embedding keeps the local theme fallback.
  const media = matchMedia("(prefers-color-scheme: dark)");
  const tokens = ["bg", "surface", "elevated", "fg", "fg-2", "muted", "meta", "border", "hover", "accent",
    "success", "warn", "danger", "font-ui", "font-mono", "radius-surface", "shadow-float"];
  const sync = () => {
    const omc = ancestors.find((element) => {
      const style = element.ownerDocument.defaultView.getComputedStyle(element);
      return ["light", "dark"].includes(element.dataset.themeMode) && style.getPropertyValue("--fg").trim() && style.getPropertyValue("--font-ui").trim();
    });
    const themed = ancestors.find((element) => ["light", "white", "dark"].includes(element.dataset.theme));
    const theme = omc?.dataset.themeMode || themed?.dataset.theme || panelValue("cli-proxy-theme")?.state?.theme;
    root.dataset.host = omc ? "omc" : root.hasAttribute("data-cpamp-plugin-host") ? "cpamp" : "cpamc";
    root.dataset.theme = ["light", "white", "dark"].includes(theme) ? theme : media.matches ? "dark" : "white";
    const style = omc?.ownerDocument.defaultView.getComputedStyle(omc);
    for (const token of tokens) {
      const value = style?.getPropertyValue("--" + token).trim();
      if (value) root.style.setProperty("--omc-" + token, value);
      else root.style.removeProperty("--omc-" + token);
    }
    omc?.ownerDocument.fonts?.forEach((face) => document.fonts.add(face));
  };
  const observer = new MutationObserver(sync);
  for (const ancestor of ancestors) observer.observe(ancestor, { attributes: true, attributeFilter: ["data-theme", "data-theme-mode", "style", "class"] });
  observer.observe(root, { attributes: true, attributeFilter: ["data-cpamp-plugin-host"] });
  addEventListener("storage", sync);
  media.addEventListener("change", sync);
  sync();
}
