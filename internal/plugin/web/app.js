"use strict";
const BASE = "/v0/management/plugins/cpa-codex-candy-eval";
const KEY_STORE = "cpa-codex-candy-eval.key";
const PREF_STORE = "cpa-codex-candy-eval.prefs";
const MASK_STORE = "cpa-codex-candy-eval.masked";
const DEFAULT_MODEL = "gpt-6.1-sol";
const HIDDEN_MODEL = (id) => id.toLowerCase().includes("image") || id.toLowerCase().split("/").pop().split("(")[0] === "codex-auto-review";
const tabNames = ["candy", "fingerprint", "modeltrace"];

let key = "";
let credentials = [];
let pollTimer = 0;
let loadError = "";
let loadController = null;
let pending = false;
let storageError = "";
let clearScope = "candy";
const listMarkup = new Map();

// The CPA management panel keeps its state in same-origin localStorage, optionally obfuscated.
function panelValue(name) {
  try {
    let raw = localStorage.getItem(name);
    if (!raw) return null;
    const prefix = "enc::v1::";
    if (raw.startsWith(prefix)) {
      const secret = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + location.host + "|" + navigator.userAgent);
      const bin = atob(raw.slice(prefix.length));
      const bytes = Uint8Array.from(bin, (c, i) => c.charCodeAt(0) ^ secret[i % secret.length]);
      raw = new TextDecoder().decode(bytes);
    }
    return JSON.parse(raw);
  } catch (_) { return null; }
}

function applyTheme() {
  const theme = panelValue("cli-proxy-theme")?.state?.theme;
  const dark = theme === "dark" || (theme !== "white" && theme !== "light" && matchMedia("(prefers-color-scheme: dark)").matches);
  document.documentElement.dataset.theme = dark ? "dark" : "light";
}
function panelKey() {
  const k = panelValue("cli-proxy-auth")?.state?.managementKey;
  return typeof k === "string" ? k.trim() : "";
}

class AuthError extends Error {}

async function api(path, { method = "GET", body, apiKey, signal } = {}) {
  const timeout = AbortSignal.timeout(30000);
  const init = { method, cache: "no-store", signal: signal ? AbortSignal.any([signal, timeout]) : timeout, headers: { Authorization: "Bearer " + (apiKey ?? key) } };
  if (body !== undefined) {
    init.headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(body);
  }
  let resp, text;
  try {
    resp = await fetch(path, init);
    text = await resp.text();
  } catch (err) {
    if (signal?.aborted) throw err;
    throw new Error(err.name === "TimeoutError" ? "连接超时，请稍后重试。" : "无法连接 CPA，请检查网络后重试。");
  }
  let data = null;
  try { data = JSON.parse(text); } catch (_) {}
  if (resp.status === 401 && apiKey === undefined) throw new AuthError("管理密钥无效，请重新输入。");
  if (!resp.ok) {
    const err = data?.error;
    throw new Error(String((typeof err === "string" ? err : err?.message) || text || "HTTP " + resp.status).slice(0, 500));
  }
  if (!data || typeof data !== "object" || Array.isArray(data)) throw new Error("服务器返回了无效数据，请稍后重试。");
  return data;
}

function stored(name) {
  try { return JSON.parse(localStorage.getItem(name)); } catch (_) { return null; }
}
function store(name, value) {
  try { localStorage.setItem(name, JSON.stringify(value)); } catch (_) {}
}
function fillModels() {
  const ids = [...new Set(catalogCache.models?.ids || [])].sort(compareModels);
  const probeModels = ids.filter((id) => !/[()]/.test(id));
  fillSelect("model", ids, $("model").value || candyPrefs().model, DEFAULT_MODEL);
  fillSelect("fp-model", probeModels, $("fp-model").value || stored(PREF_STORE + ".fingerprint")?.model, DEFAULT_MODEL);
  fillSelect("mt-model", probeModels, $("mt-model").value || stored(PREF_STORE + ".modeltrace")?.model, DEFAULT_MODEL);
}
function renderList(prefix, renderRow) {
  const id = prefix + "rows", list = $(id);
  const visible = visibleCredentials(prefix);
  const empty = loadController && !credentials.length ? "正在读取凭证与历史记录…" : "当前类型没有凭证，请切换类型或在 CPA 中添加凭证。";
  const html = visible.length ? visible.map(renderRow).join("") : `<div class="empty">${esc(loadError || empty)}</div>`;
  if (listMarkup.get(id) === html) return;
  const focused = list.contains(document.activeElement) ? document.activeElement : null;
  const selector = focused && [...focused.attributes].filter((a) => a.name.startsWith("data-"))
    .map((a) => `[${a.name}="${CSS.escape(a.value)}"]`).join("");
  if (id === "rows") hideTip();
  list.innerHTML = html;
  listMarkup.set(id, html);
  if (selector) list.querySelector(selector)?.focus({ preventScroll: true });
}

function renderRefresh() {
  $("refresh").disabled = pending || !!loadController;
  $("refresh").setAttribute("aria-busy", !!loadController);
}

function render() {
  renderRefresh();
  setNotice("storage-error", storageError);
  renderCandy();
  renderFingerprints();
  renderModelTrace();
}

function stopPolling() {
  clearTimeout(pollTimer);
  loadController?.abort();
  loadController = null;
  renderRefresh();
}

function stateCredentials(data) {
  const validRecord = (r) => r !== null && typeof r === "object" && !Array.isArray(r);
  if (!Array.isArray(data.auths) || data.auths.some((a) => !a || typeof a.id !== "string" || !a.id.trim() ||
    ["results", "fingerprints", "modeltraces"].some((field) => !Array.isArray(a[field]) || !a[field].every(validRecord)))) {
    throw new Error("服务器返回的测试记录格式无效。");
  }
  return data.auths;
}

async function load({ refresh = false } = {}) {
  stopPolling();
  const controller = new AbortController();
  loadController = controller;
  render();
  try {
    try {
      await refreshCatalog({ signal: controller.signal, force: refresh });
      if (controller.signal.aborted) return;
      fillModels();
      setNotice("catalog-error", "");
    } catch (err) {
      if (controller.signal.aborted) return;
      if (err instanceof AuthError) return showLogin(err.message);
      setNotice("catalog-error", "读取凭证与模型目录失败：" + err.message);
    }
    const data = await api(BASE + "/state", { signal: controller.signal });
    if (controller.signal.aborted) return;
    credentials = stateCredentials(data);
    pruneCredentialCatalog();
    fillCredentialTypes();
    storageError = data.storage_error || "";
    loadError = "";
    setNotice("load-error", "");
    const ids = new Set(credentials.map((a) => a.id));
    const enabled = new Set(credentials.filter((a) => !a.disabled).map((a) => a.id));
    const answers = new Set(credentials.flatMap((a) => a.results.map((r) => answerKey(a.id, r))));
    for (const expanded of [candyExpanded, fpExpanded, mtExpanded]) {
      for (const id of expanded) if (!ids.has(id)) expanded.delete(id);
    }
    for (const selection of [candySelected, fpSelected, mtSelected]) {
      for (const id of selection) if (!enabled.has(id)) selection.delete(id);
    }
    for (const id of candyAnswersExpanded) if (!answers.has(id)) candyAnswersExpanded.delete(id);
  } catch (err) {
    if (controller.signal.aborted) return;
    if (err instanceof AuthError) return showLogin(err.message);
    loadError = "读取测试结果失败：" + err.message;
    setNotice("load-error", loadError);
  } finally {
    if (loadController === controller) {
      loadController = null;
      render();
      pollTimer = setTimeout(load, credentials.some((a) => a.running || a.fingerprint_running || a.modeltrace_running) ? 2500 : 20000);
    }
  }
}

async function update(path, options, errorPrefix) {
  if (pending) return;
  pending = true;
  stopPolling();
  setNotice("flash", "");
  render();
  try {
    if (path.endsWith("/run")) {
      await refreshCatalog();
      fillModels();
      setNotice("catalog-error", "");
      if (!catalogCache.models?.ids.includes(options.body.model)) throw new Error("所选模型已不在模型目录中，请重新选择。");
      options.body.model_catalog = await modelCatalog(options.body.auth_ids || []);
    }
    const response = await api(BASE + path, options);
    if (path.endsWith("/run")) {
      const messages = [`已启动 ${response.started || 0} 个凭证`];
      if (response.skipped) messages.push(`${response.skipped} 个凭证不含所选模型，已跳过`);
      if (response.unchecked) messages.push(`${response.unchecked} 个凭证未能检查模型目录，已交由 CPA 处理`);
      if (response.busy) messages.push(`${response.busy} 个凭证正在测试`);
      setNotice("flash", messages.join("；"), response.unchecked ? "warning" : "info");
    }
  } catch (err) {
    if (err instanceof AuthError) return showLogin(err.message);
    setNotice("flash", errorPrefix + "：" + err.message);
  } finally {
    pending = false;
    if (!$("app").hidden) await load();
  }
}

function showLogin(message) {
  stopPolling();
  key = "";
  try { sessionStorage.removeItem(KEY_STORE); } catch (_) {}
  resetCatalogCache();
  credentials = [];
  loadError = storageError = "";
  for (const selection of [candySelected, fpSelected, mtSelected, candyExpanded, fpExpanded, mtExpanded, candyAnswersExpanded]) selection.clear();
  for (const id of notices.keys()) setNotice(id, "");
  $("refresh").hidden = true;
  hideTip();
  document.querySelectorAll("dialog[open]").forEach((dialog) => dialog.close());
  render();
  $("app").hidden = true;
  $("login").hidden = false;
  $("login-error").hidden = !message;
  $("login-error").textContent = message || "";
  $("login-key").focus();
}

async function start() {
  setNotice("flash", "");
  $("login").hidden = true;
  $("app").hidden = false;
  $("refresh").hidden = false;
  const managementKey = key;
  await openCatalogCache(managementKey);
  if (managementKey !== key) return;
  fillModels();
  await load({ refresh: true });
}

function setMasked(masked) {
  document.body.classList.toggle("masked", masked);
  for (const id of ["mask", "fp-mask", "mt-mask"]) {
    $(id).setAttribute("aria-pressed", masked);
    const label = masked ? "取消脱敏" : "脱敏";
    $(id).innerHTML = icon(masked ? "eye" : "eye-off");
    $(id).setAttribute("aria-label", label);
    $(id).title = label;
  }
}
function switchTab(name) {
  for (const tab of tabNames) {
    const active = name === tab;
    $("tab-" + tab).setAttribute("aria-selected", active);
    $("tab-" + tab).tabIndex = active ? 0 : -1;
    $(tab + "-panel").hidden = !active;
  }
  hideTip();
  store(PREF_STORE + ".tab", name);
}
const availableCredential = (a) => !a.disabled && !a.running && !a.fingerprint_running && !a.modeltrace_running;
const selectedCredentials = (prefix, selection) => visibleCredentials(prefix).filter((a) => selection.has(a.id));
const batchCredentials = (prefix, selection) => {
  const selected = selectedCredentials(prefix, selection);
  return (selected.length ? selected : visibleCredentials(prefix)).filter(availableCredential);
};
function renderSelection(prefix, selection, action) {
  const available = visibleCredentials(prefix).filter(availableCredential);
  const selected = selectedCredentials(prefix, selection);
  const targets = batchCredentials(prefix, selection);
  const button = $(prefix + "run-batch");
  button.innerHTML = `${icon("play")}${action}${selected.length ? "所选" : "全部"} (${targets.length})`;
  button.disabled = pending || !targets.length || !$(prefix + "model").value;
  const checkbox = $(prefix + "select-all");
  checkbox.checked = available.length > 0 && available.every((a) => selection.has(a.id));
  checkbox.indeterminate = selected.length > 0 && !checkbox.checked;
  checkbox.disabled = pending || (!available.length && !selected.length);
}

function bindCollectionActions({ type, scope, historyKey, expanded, renderRows, run, showDetail }) {
  const rows = $(type + "-rows"), dialog = $(type + "-detail");
  $(type + "-detail-close").addEventListener("click", () => dialog.close());
  dialog.addEventListener("close", () => {
    rows.querySelector(`[data-${type}-detail="${CSS.escape(dialog.dataset.record)}"][data-${type}-credential="${CSS.escape(dialog.dataset.credential)}"]`)?.focus();
  });
  rows.addEventListener("click", (e) => {
    if (e.target.closest("input")) return;
    const action = (name) => e.target.closest(`[data-${type}-${name}]`)?.getAttribute(`data-${type}-${name}`);
    const runID = action("run");
    if (runID) return run({ auth_ids: [runID] });
    const cancelID = action("cancel");
    if (cancelID) return update(`/${scope}/cancel`, { method: "POST", body: { auth_ids: [cancelID] } }, "停止测试失败");
    const toggleID = e.target.closest("[data-row]")?.dataset.row;
    if (toggleID) {
      expanded.has(toggleID) ? expanded.delete(toggleID) : expanded.add(toggleID);
      return renderRows();
    }
    const recordID = action("detail"), credentialID = action("credential");
    if (recordID && credentialID) {
      const record = credentials.find((a) => a.id === credentialID)?.[historyKey]?.find((r) => r.id === recordID);
      if (record) showDetail(record, credentialID);
    }
  });
}

const credentialType = (a) => a.source + ":" + a.provider;
const credentialTypeLabel = (a) => `${a.source === "ai_providers" ? "AI 提供商" : "认证文件"} · ${a.provider}`;
const visibleCredentials = (prefix) => credentials.filter((a) => $(prefix + "credential-type").value === "all" || credentialType(a) === $(prefix + "credential-type").value);
function fillCredentialTypes() {
  const types = new Map(credentials.map((a) => [credentialType(a), credentialTypeLabel(a)]));
  // Keep the requested default visible even when there are no Codex files.
  types.set("auth_files:codex", "认证文件 · codex");
  const options = [["all", "全部凭证"], ...[...types].sort(([a], [b]) => a.localeCompare(b))];
  for (const prefix of ["", "fp-", "mt-"]) fillSelect(prefix + "credential-type", options, $(prefix + "credential-type").value, "auth_files:codex");
}

function initializeLayout() {
  $("candy-credentials").innerHTML = credentialCard("", ["凭证", "最近一次", "正确率", "最近 20 次", ""]);
  $("fp-credentials").innerHTML = credentialCard("fp-", ["凭证", "指纹结果", "模式", "测试时间", ""]);
  $("mt-credentials").innerHTML = credentialCard("mt-", ["凭证", "测试模型", "归因结果", "测试时间", ""]);
  $("notifications").innerHTML = ["flash", "load-error", "catalog-error", "storage-error"].map((id) => `<div id="${id}" class="global-flash" role="alert" hidden><span class="notice-symbol" aria-hidden="true"></span><span class="notice-message"></span><button class="notice-close" type="button" title="隐藏提示" aria-label="隐藏提示">${icon("x")}</button></div>`).join("");
}

// Events and initialization.
initializeLayout();
initializeCandy();
initializeFingerprint();
initializeModelTrace();
document.querySelectorAll("[data-icon]").forEach((el) => { el.outerHTML = icon(el.dataset.icon, el.dataset.class); });

for (const id of ["flash", "load-error", "catalog-error", "storage-error"]) {
  $(id).querySelector(".notice-close").addEventListener("click", () => hideNotice(id));
}

$("refresh").addEventListener("click", () => {
  if (pending) return;
  resetCatalogCache();
  load({ refresh: true });
});

applyTheme();
addEventListener("storage", applyTheme);
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", applyTheme);

setMasked(stored(MASK_STORE) === true);
$("mask").addEventListener("click", () => {
  const masked = !document.body.classList.contains("masked");
  setMasked(masked);
  store(MASK_STORE, masked);
});
for (const id of ["fp-mask", "mt-mask"]) $(id).addEventListener("click", () => $("mask").click());

for (const [id, scope, label] of [["clear", "candy", "糖果"], ["fp-clear", "fingerprint", "指纹"], ["mt-clear", "modeltrace", "ModelTrace"]]) {
  $(id).addEventListener("click", () => {
    clearScope = scope;
    $("clear-title").textContent = `清空${label}测试记录？`;
    $("confirm-clear").returnValue = ""; // Esc keeps the previous returnValue.
    $("confirm-clear").showModal();
  });
}
$("confirm-clear").addEventListener("close", () => {
  if ($("confirm-clear").returnValue !== "confirm") return;
  update(clearScope === "candy" ? "/results" : `/${clearScope}/results`, { method: "DELETE" }, "清空历史失败");
});

$("login-form").addEventListener("submit", (e) => {
  e.preventDefault();
  key = $("login-key").value.trim();
  try { sessionStorage.setItem(KEY_STORE, key); } catch (_) {}
  start();
});

for (const name of tabNames) {
  $("tab-" + name).addEventListener("click", () => switchTab(name));
  $("tab-" + name).addEventListener("keydown", (e) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
    e.preventDefault();
    const next = e.key === "Home" ? tabNames[0] : e.key === "End" ? tabNames.at(-1) : tabNames[(tabNames.indexOf(name) + (e.key === "ArrowRight" ? 1 : -1) + tabNames.length) % tabNames.length];
    switchTab(next); $("tab-" + next).focus();
  });
}
switchTab(tabNames.includes(stored(PREF_STORE + ".tab")) ? stored(PREF_STORE + ".tab") : "candy");

for (const [prefix, type, selection, submit] of [["", "candy", candySelected, runCandy], ["fp-", "fp", fpSelected, runFingerprint], ["mt-", "mt", mtSelected, runModelTrace]]) {
  $(prefix + "credential-type").addEventListener("change", () => { selection.clear(); render(); });
  $(prefix + "toolbar").addEventListener("submit", (e) => {
    e.preventDefault();
    const ids = batchCredentials(prefix, selection).map((a) => a.id);
    if (ids.length) submit({ auth_ids: ids });
  });
  $(prefix + "select-all").addEventListener("change", (e) => {
    for (const a of visibleCredentials(prefix)) {
      if (!e.target.checked) selection.delete(a.id);
      else if (availableCredential(a)) selection.add(a.id);
    }
    render();
  });
  $(prefix + "rows").addEventListener("change", (e) => {
    const id = e.target.getAttribute(`data-${type}-select`);
    if (!id) return;
    e.target.checked ? selection.add(id) : selection.delete(id);
    render();
  });
}
key = panelKey();
if (!key) { try { key = sessionStorage.getItem(KEY_STORE) || ""; } catch (_) {} }
key ? start() : showLogin("");
