"use strict";
// Appearance only: use public DOM attributes and CSS tokens, including through
// OMC's intermediate iframe shell. Cross-origin ancestors are optional.
(() => {
  const root = document.documentElement;
  const ancestors = [];
  let frame = window;
  try {
    while (frame.parent !== frame && ancestors.length < 8) {
      frame = frame.parent;
      ancestors.push(frame.document.documentElement);
    }
  } catch (_) { /* Keep any accessible ancestors before the origin boundary. */ }
  const media = matchMedia("(prefers-color-scheme: dark)");
  const tokens = ["bg", "surface", "elevated", "fg", "fg-2", "muted", "meta", "border", "hover", "accent", "accent-hover", "accent-on", "success", "warn", "danger", "font-ui", "font-mono", "radius-control", "radius-surface", "shadow-float", "overlay-mask"];
  const validTheme = (value) => ["light", "white", "dark"].includes(value);
  const initialTheme = root.dataset.theme;
  const sync = () => {
    const hosts = ancestors.flatMap((el) => {
      // A frame can navigate after setup. Recheck access before reading it.
      try { return [{ el, css: el.ownerDocument.defaultView.getComputedStyle(el) }]; }
      catch (_) { return []; }
    });
    const omc = hosts.find(({ el, css }) => ["light", "dark"].includes(el.dataset.themeMode)
      && css.getPropertyValue("--fg").trim() && css.getPropertyValue("--font-ui").trim());
    const cpamp = root.hasAttribute("data-cpamp-plugin-host")
      || hosts.some(({ el }) => el.hasAttribute("data-cpamp-plugin-host"));
    const panel = omc ? "omc" : cpamp ? "cpamp" : "cpamc";
    const hostTheme = omc?.el.dataset.themeMode
      || hosts.map(({ el }) => el.dataset.theme).find(validTheme);
    const savedTheme = panelValue("cli-proxy-theme")?.state?.theme;
    const theme = hostTheme || (validTheme(savedTheme) ? savedTheme
      : validTheme(initialTheme) ? initialTheme : media.matches ? "dark" : "white");
    root.dataset.pluginPanel = panel;
    root.dataset.theme = theme;
    for (const token of tokens) {
      const name = "--panel-" + token;
      const value = omc?.css.getPropertyValue("--" + token).trim();
      if (value) root.style.setProperty(name, value);
      else root.style.removeProperty(name);
    }
    // Share already-loaded same-origin fonts without fetching host assets.
    if (omc) omc.el.ownerDocument.fonts?.forEach((face) => {
      try { document.fonts.add(face); } catch (_) {}
    });
  };
  const observer = new MutationObserver(sync);
  for (const ancestor of ancestors) observer.observe(ancestor, {
    attributes: true,
    attributeFilter: ["data-theme", "data-theme-mode", "data-cpamp-plugin-host", "style", "class"],
  });
  // Do not observe our own theme/style writes, which would loop indefinitely.
  observer.observe(root, { attributes: true, attributeFilter: ["data-cpamp-plugin-host"] });
  addEventListener("storage", sync);
  media.addEventListener("change", sync);
  sync();
})();
