"use strict";
const BASE = "/v0/management/plugins/cpa-codex-candy-eval";
const KEY_STORE = "cpa-codex-candy-eval.key";
const VIEW_STORE = "cpa-codex-candy-eval.view.v1.";
const DEFAULT_MODEL = "gpt-6.1-sol";
const DEFAULT_EFFORT = "low";
const DEFAULT_EFFORTS = ["none", "low", "medium", "high", "xhigh", "max"];
const HIDDEN_MODEL = (id) => id.toLowerCase().includes("image");
// Each test has a tab, a panel and controls whose IDs start with its name.
const TESTS = ["candy", "fingerprint", "pelican"];
const TEST_LABELS = { candy: "糖果测试", fingerprint: "指纹检测", pelican: "鹈鹕测试" };

let key = "";
let credentials = [];
let pollTimer = 0;
let loadError = "";
let loadController = null;
// Whether a refresh started with the button is running; polling never shows progress.
let refreshing = false;
let pending = false;
let storageError = "";
let clearPath = "";
// Enable changes in flight, by credential ID; reloads keep these requested states.
const switching = new Map();
const credentialFilter = {
  enabledOnly: preference("credential-filter.enabledOnly", false),
  type: preference("credential-filter.type", "auth_files:codex"),
  plan: preference("credential-filter.plan", "all"),
};

// The CPA management panel keeps its state in same-origin localStorage, optionally obfuscated.
function panelValue(name) {
  try {
    let raw = localStorage.getItem(name);
    if (!raw) return null;
    // CPAMC writes enc::v1:: (keyed by host and user agent), CPAMP enc::v2:: (keyed by host).
    const prefix = /^enc::v([12])::/.exec(raw);
    if (prefix) {
      const scope = prefix[1] === "2" ? "v2|" + location.host : location.host + "|" + navigator.userAgent;
      const secret = new TextEncoder().encode("cli-proxy-api-webui::secure-storage|" + scope);
      const bin = atob(raw.slice(prefix[0].length));
      const bytes = Uint8Array.from(bin, (c, i) => c.charCodeAt(0) ^ secret[i % secret.length]);
      raw = new TextDecoder().decode(bytes);
    }
    return JSON.parse(raw);
  } catch (_) { return null; }
}

// Panels resolve their "auto" theme to white or dark; CPAMC also has a light (paper) theme.
function applyTheme() {
  const theme = panelValue("cli-proxy-theme")?.state?.theme;
  document.documentElement.dataset.theme = ["light", "white", "dark"].includes(theme) ? theme
    : matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "white";
}
function panelKey() {
  const k = panelValue("cli-proxy-auth")?.state?.managementKey;
  return typeof k === "string" ? k.trim() : "";
}

class AuthError extends Error {}

async function api(path, { method = "GET", body, apiKey, signal } = {}) {
  const timeout = AbortSignal.timeout(30000);
  const requestKey = apiKey ?? key;
  const init = { method, cache: "no-store", signal: signal ? AbortSignal.any([signal, timeout]) : timeout, headers: { Authorization: "Bearer " + requestKey } };
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
  // A 401 for a key replaced by a newer login must not sign that login out.
  if (resp.status === 401 && apiKey === undefined && requestKey === key) throw new AuthError("管理密钥无效，请重新输入。");
  if (!resp.ok) {
    const err = data?.error;
    throw Object.assign(new Error(String((typeof err === "string" ? err : err?.message) || text || "HTTP " + resp.status).slice(0, 500)), { status: resp.status });
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
// Page choices are kept across visits; a saved value replaces its default only when the types match.
function preference(name, fallback) {
  const value = stored(VIEW_STORE + name);
  return value !== null && typeof value === typeof fallback ? value : fallback;
}
function savePreference(name, value) {
  store(VIEW_STORE + name, value);
}
function fillModels() {
  const ids = [...new Set(catalogCache.models?.ids || [])].sort(compareModels);
  const unsuffixed = ids.filter((id) => !/[()]/.test(id));
  for (const [id, models] of [["candy-model", ids], ["fingerprint-model", unsuffixed], ["pelican-model", ids]]) {
    fillSelect(id, models, $(id).value || preference(id, DEFAULT_MODEL), DEFAULT_MODEL);
  }
}
// Rows are keyed by credential, and an update patches only the markup that changed, so the rest keeps its
// state, such as focus or a loaded preview.
const renderedLists = new Map();
function renderList(test, renderRow) {
  const list = $(test + "-rows"), previous = renderedLists.get(list) || { rows: new Map(), empty: "" };
  const visible = visibleCredentials();
  if (!visible.length) {
    const message = loadController && !credentials.length ? "正在读取凭证和测试记录…" : "没有符合筛选条件的凭证，请调整筛选或在 CPA 中添加凭证。";
    const empty = `<div class="empty">${esc(loadError || message)}</div>`;
    if (previous.empty !== empty) list.innerHTML = empty;
    renderedLists.set(list, { rows: new Map(), empty });
    return;
  }
  const rows = new Map(visible.map((a) => {
    const html = renderRow(a), row = previous.rows.get(a.id);
    if (row?.html === html) return [a.id, row];
    return [a.id, { html, node: row ? patch(row.node, rowNode(html)) : rowNode(html) }];
  }));
  renderedLists.set(list, { rows, empty: "" });
  const kept = new Set([...rows.values()].map((row) => row.node));
  let cursor = list.firstChild;
  const dropStale = () => {
    while (cursor && !kept.has(cursor)) {
      const next = cursor.nextSibling;
      cursor.remove();
      cursor = next;
    }
  };
  for (const { node } of rows.values()) {
    dropStale();
    if (node === cursor) cursor = cursor.nextSibling;
    else list.insertBefore(node, cursor);
  }
  dropStale();
  refreshTip();
}
// patch updates node to match next and returns the node now in place.
function patch(node, next) {
  if (node.isEqualNode(next)) return node;
  if (node.nodeName !== next.nodeName) {
    node.replaceWith(next);
    return next;
  }
  if (node.nodeType !== Node.ELEMENT_NODE) {
    node.nodeValue = next.nodeValue;
    return node;
  }
  for (const { name } of [...node.attributes]) if (!next.hasAttribute(name)) node.removeAttribute(name);
  for (const { name, value } of next.attributes) if (node.getAttribute(name) !== value) node.setAttribute(name, value);
  // Once clicked, a checkbox no longer follows its checked attribute.
  if (node instanceof HTMLInputElement) node.checked = next.checked;
  const children = [...node.childNodes], updates = [...next.childNodes];
  updates.forEach((update, i) => children[i] ? patch(children[i], update) : node.append(update));
  for (const child of children.slice(updates.length)) child.remove();
  return node;
}
function rowNode(html) {
  const template = document.createElement("template");
  template.innerHTML = html;
  return template.content.firstElementChild;
}

function renderPageActions() {
  $("clear-all").disabled = pending || credentials.every((a) => !a.candies.length && !a.fingerprints.length && !a.pelicans.length);
  $("refresh").disabled = pending || refreshing;
  $("refresh").setAttribute("aria-busy", refreshing);
}

function render() {
  renderPageActions();
  setNotice("storage-error", storageError);
  renderCredentialFilters();
  renderCandy();
  renderFingerprint();
  renderPelican();
}

function stopPolling() {
  clearTimeout(pollTimer);
  loadController?.abort();
  loadController = null;
}

function stateCredentials(data) {
  const validRecord = (r) => r !== null && typeof r === "object" && !Array.isArray(r);
  if (!Array.isArray(data.credentials) || data.credentials.some((a) => !a || typeof a.id !== "string" || !a.id.trim() ||
    ["candies", "fingerprints", "pelicans"].some((field) => !Array.isArray(a[field]) || !a[field].every(validRecord)))) {
    throw new Error("服务器返回的测试记录格式无效。");
  }
  return data.credentials;
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
      setNotice("catalog-error", "读取凭证和模型列表失败：" + err.message);
    }
    const data = await api(BASE + "/state", { signal: controller.signal });
    if (controller.signal.aborted) return;
    credentials = stateCredentials(data);
    for (const a of credentials) if (switching.has(a.id)) a.disabled = switching.get(a.id);
    pruneCredentialCatalog();
    storageError = data.storage_error || "";
    loadError = "";
    setNotice("load-error", "");
    const ids = new Set(credentials.map((a) => a.id));
    const usable = new Set(credentials.filter(usableCredential).map((a) => a.id));
    const answers = new Set(credentials.flatMap((a) => a.candies.map((r) => answerKey(a.id, r))));
    const pages = new Set(credentials.flatMap((a) => a.pelicans.map((r) => pelicanKey(a.id, r.id))));
    for (const expanded of [candyExpanded, fingerprintExpanded, pelicanExpanded]) {
      for (const id of expanded) if (!ids.has(id)) expanded.delete(id);
    }
    for (const selection of [candySelected, fingerprintSelected, pelicanSelected]) {
      for (const id of selection) if (!usable.has(id)) selection.delete(id);
    }
    for (const id of candyAnswersExpanded) if (!answers.has(id)) candyAnswersExpanded.delete(id);
    // Failed page loads retry with the next state.
    for (const [key, page] of pelicanPages) if (!pages.has(key) || page instanceof Error) pelicanPages.delete(key);
  } catch (err) {
    if (controller.signal.aborted) return;
    if (err instanceof AuthError) return showLogin(err.message);
    loadError = "读取测试记录失败：" + err.message;
    setNotice("load-error", loadError);
  } finally {
    if (loadController === controller) {
      loadController = null;
      render();
      pollTimer = setTimeout(load, credentials.some(credentialBusy) ? 2500 : 20000);
    }
  }
}

// update makes one change at a time, reports its failure, and reloads the state afterwards.
async function update(errorPrefix, change) {
  if (pending) return;
  pending = true;
  stopPolling();
  setNotice("flash", "");
  render();
  try {
    await change();
  } catch (err) {
    if (err instanceof AuthError) return showLogin(err.message);
    setNotice("flash", errorPrefix + "：" + err.message);
  } finally {
    pending = false;
    if (!$("app").hidden) await load();
  }
}
// startTest starts test with the credentials' model lists, so the plugin skips credentials without the model.
function startTest(test, body) {
  return update("开始测试失败", async () => {
    await refreshCatalog();
    fillModels();
    setNotice("catalog-error", "");
    if (!catalogCache.models?.ids.includes(body.model)) throw new Error("所选模型已不在模型列表中，请重新选择。");
    const response = await api(`${BASE}/${test}/run`, { method: "POST", body: { ...body, model_catalog: await modelCatalog(body.credential_ids) } });
    const messages = [`已为 ${response.started || 0} 个凭证开始测试`];
    if (response.skipped) messages.push(`${response.skipped} 个凭证不支持所选模型，已跳过`);
    if (response.unchecked) messages.push(`${response.unchecked} 个凭证无法读取模型列表，仍会尝试测试`);
    if (response.busy) messages.push(`${response.busy} 个凭证正在测试，已跳过`);
    setNotice("flash", messages.join("；"), response.unchecked ? "warning" : "info");
  });
}

function showLogin(message) {
  stopPolling();
  key = "";
  try { sessionStorage.removeItem(KEY_STORE); } catch (_) {}
  resetCatalogCache();
  credentials = [];
  loadError = storageError = "";
  for (const collection of [candySelected, fingerprintSelected, pelicanSelected, candyExpanded, fingerprintExpanded, pelicanExpanded, candyAnswersExpanded, pelicanPages]) collection.clear();
  for (const id of notices.keys()) setNotice(id, "");
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
  const managementKey = key;
  await openCatalogCache(managementKey);
  if (managementKey !== key) return;
  fillModels();
  await load({ refresh: true });
}

function setMasked(masked) {
  document.body.classList.toggle("masked", masked);
  for (const test of TESTS) {
    const button = $(test + "-mask");
    button.setAttribute("aria-pressed", masked);
    button.innerHTML = icon(masked ? "eye" : "eye-off");
    setLabel(button, masked ? "取消脱敏" : "脱敏");
  }
}
function switchTab(name) {
  for (const test of TESTS) {
    const active = name === test;
    $("tab-" + test).setAttribute("aria-selected", active);
    $("tab-" + test).tabIndex = active ? 0 : -1;
    $(test + "-panel").hidden = !active;
  }
  hideTip();
  savePreference("tab", name);
}
const busyTest = (a) => TEST_LABELS[a.running?.test] || "";
const credentialBusy = (a) => !!a.running;
const usableCredential = (a) => !a.disabled && !a.unavailable;
const availableCredential = (a) => usableCredential(a) && !credentialBusy(a);
// Why a test cannot start, shown on its disabled button; empty when it can.
const pageBlocker = (test) => !$(test + "-model").value ? "模型列表为空，请刷新后重试" : pending ? "正在处理上一个操作，请稍候" : "";
function runBlocker(test, a) {
  if (a.disabled) return "凭证已停用，启用后才能测试";
  if (a.unavailable) return "凭证暂不可用：" + unavailableReason(a);
  if (credentialBusy(a)) return `${busyTest(a)}进行中，结束后才能开始新测试`;
  return pageBlocker(test);
}
const runnable = (test, a) => !runBlocker(test, a);
const selectedCredentials = (selection) => visibleCredentials().filter((a) => selection.has(a.id));
const batchCredentials = (selection) => {
  const selected = selectedCredentials(selection);
  return (selected.length ? selected : visibleCredentials()).filter(availableCredential);
};
async function setCredentialEnabled(id, enabled) {
  const a = credentials.find((entry) => entry.id === id);
  if (!a || switching.has(id)) return;
  switching.set(id, !enabled);
  a.disabled = !enabled;
  render();
  try {
    await api("/v0/management/auth-files/status", { method: "PATCH", body: { name: id, disabled: !enabled } });
  } catch (err) {
    if (err instanceof AuthError) return showLogin(err.message);
    setNotice("flash", (enabled ? "启用凭证失败：" : "停用凭证失败：") + err.message);
    const current = credentials.find((entry) => entry.id === id);
    if (current) current.disabled = enabled;
  } finally {
    switching.delete(id);
  }
  render();
  // Config API keys live in CPA's config, which the page syncs to the plugin.
  if (!$("app").hidden) await load({ refresh: a.source !== "auth_files" });
}
function renderSelection(test, selection, action) {
  const available = visibleCredentials().filter(availableCredential);
  const selected = selectedCredentials(selection);
  const targets = batchCredentials(selection);
  const button = $(test + "-run-batch");
  button.innerHTML = `${icon("play")}${action}${selected.length ? "所选" : "全部"} (${targets.length})`;
  const blocker = pageBlocker(test) || (targets.length ? "" : selected.length ? "所选凭证均已停用、不可用或正在测试" : "当前列表没有可测试的凭证");
  button.disabled = !!blocker;
  button.dataset.tip = blocker;
  const checkbox = $(test + "-select-all");
  checkbox.checked = available.length > 0 && available.every((a) => selection.has(a.id));
  checkbox.indeterminate = selected.length > 0 && !checkbox.checked;
  checkbox.disabled = pending || (!available.length && !selected.length);
}

function bindCollectionActions({ test, expanded, renderRows, run, showDetail }) {
  const rows = $(test + "-rows"), dialog = $(test + "-detail");
  $(test + "-detail-close").addEventListener("click", () => dialog.close());
  dialog.addEventListener("close", () => {
    rows.querySelector(`[data-${test}-detail="${CSS.escape(dialog.dataset.record)}"][data-${test}-credential="${CSS.escape(dialog.dataset.credential)}"]`)?.focus();
  });
  rows.addEventListener("click", (e) => {
    if (e.target.closest("label")) return;
    const action = (name) => e.target.closest(`[data-${test}-${name}]`)?.getAttribute(`data-${test}-${name}`);
    const runID = action("run");
    if (runID) return run({ credential_ids: [runID] });
    const cancelID = action("cancel");
    if (cancelID) return update("停止测试失败", () => api(`${BASE}/cancel`, { method: "POST", body: { credential_ids: [cancelID] } }));
    const recordID = action("detail"), credentialID = action("credential");
    if (recordID && credentialID) return openResultDetail(test, credentialID, recordID, showDetail);
    const toggleID = e.target.closest("[data-row]")?.dataset.row;
    if (toggleID) {
      expanded.has(toggleID) ? expanded.delete(toggleID) : expanded.add(toggleID);
      renderRows();
    }
  });
}

// CPA names each OpenAI-compatible provider after its config entry; they share one type.
const providerType = (a) => a.provider === "openai-compatibility" || a.provider.startsWith("openai-compatible-") ? "openai-compatibility" : a.provider;
const credentialType = (a) => a.source + ":" + providerType(a);
function credentialTypeLabel(type) {
  const [source, provider] = type.split(/:(.*)/);
  return `${source === "ai_providers" ? "AI 提供商" : "认证文件"} · ${provider}`;
}
const ofType = (a, type) => type === "all" || credentialType(a) === type;
const PRO_ALL = "Pro ALL";
// Plans of one credential type, best first, grouping Pro plans when there are several.
function planChoices(type) {
  const plans = [...new Map(credentials.filter((a) => ofType(a, type)).map(credentialPlan).filter(Boolean).map((plan) => [plan.label, plan])).values()]
    .sort((a, b) => a.rank - b.rank || a.label.localeCompare(b.label));
  return [...(plans.filter((plan) => plan.pro).length > 1 ? [PRO_ALL] : []), ...plans.map((plan) => plan.label)];
}
function visibleCredentials() {
  const { enabledOnly, type, plan } = credentialFilter;
  const matches = (p) => plan === "all" || (plan === PRO_ALL ? p?.pro : p?.label === plan);
  return credentials.filter((a) => ofType(a, type) && (!enabledOnly || !a.disabled) && matches(credentialPlan(a))).sort((a, b) => planRank(a) - planRank(b));
}
// Every card shows the filter; chosen values stay listed while no credential matches them.
function renderCredentialFilters() {
  const { enabledOnly, type, plan } = credentialFilter;
  const types = [...new Set([type, ...credentials.map(credentialType)])].filter((t) => t !== "all").sort((a, b) => a.localeCompare(b));
  const plans = [...new Set([...planChoices(type), plan])].filter((p) => p !== "all");
  for (const test of TESTS) {
    $(test + "-enabled-only").checked = enabledOnly;
    fillSelect(test + "-credential-type", [["all", "全部凭证"], ...types.map((t) => [t, credentialTypeLabel(t)])], type);
    fillSelect(test + "-credential-plan", [["all", "所有订阅类型"], ...plans], plan);
    $(test + "-plan-filter").hidden = !plans.length;
  }
}
function setCredentialFilter(change) {
  for (const [name, value] of Object.entries(change)) {
    credentialFilter[name] = value;
    savePreference("credential-filter." + name, value);
  }
  for (const selection of [candySelected, fingerprintSelected, pelicanSelected]) selection.clear();
  render();
}

function initializeLayout() {
  $("candy-credentials").innerHTML = credentialCard("candy", ["凭证", "最近一次", "正确率", "最近 20 次"]);
  $("fingerprint-credentials").innerHTML = credentialCard("fingerprint", ["凭证", "归因结果", "测试模型", "测试时间"]);
  $("pelican-credentials").innerHTML = credentialCard("pelican", ["凭证", "最近一次", "画面"]);
  $("notifications").innerHTML = ["flash", "load-error", "catalog-error", "storage-error"].map((id) => `<div id="${id}" class="global-flash" role="alert" hidden><span class="notice-symbol" aria-hidden="true"></span><span class="notice-message"></span><button class="notice-close" type="button" aria-label="隐藏提示" data-tip>${icon("x")}</button></div>`).join("");
}

// Events and initialization.
initializeLayout();
initializeTips();
initializeCopyButtons();
initializeCandy();
initializeFingerprint();
initializePelican();
document.querySelectorAll("[data-icon]").forEach((el) => { el.outerHTML = icon(el.dataset.icon, el.dataset.class); });

for (const id of ["flash", "load-error", "catalog-error", "storage-error"]) {
  $(id).querySelector(".notice-close").addEventListener("click", () => hideNotice(id));
}

$("refresh").addEventListener("click", async () => {
  if (pending) return;
  resetCatalogCache();
  refreshing = true;
  try {
    await load({ refresh: true });
  } finally {
    refreshing = false;
    renderPageActions();
  }
});

applyTheme();
addEventListener("storage", applyTheme);
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", applyTheme);

// Masking and credential filters apply to every tab, whichever card changes them.
setMasked(preference("masked", false));
renderCredentialFilters();
for (const test of TESTS) {
  $(test + "-mask").addEventListener("click", () => {
    const masked = !document.body.classList.contains("masked");
    setMasked(masked);
    savePreference("masked", masked);
  });
  $(test + "-enabled-only").addEventListener("change", (e) => setCredentialFilter({ enabledOnly: e.target.checked }));
  $(test + "-credential-type").addEventListener("change", (e) => {
    const type = e.target.value;
    setCredentialFilter({ type, plan: planChoices(type).includes(credentialFilter.plan) ? credentialFilter.plan : "all" });
  });
  $(test + "-credential-plan").addEventListener("change", (e) => setCredentialFilter({ plan: e.target.value }));
}

for (const [id, path] of [["candy-clear", "/candy/records"], ["fingerprint-clear", "/fingerprint/records"], ["pelican-clear", "/pelican/records"], ["clear-all", "/records"]]) {
  $(id).addEventListener("click", () => {
    clearPath = path;
    $("clear-title").textContent = `${$(id).getAttribute("aria-label")}？`;
    $("confirm-clear").returnValue = ""; // Esc keeps the previous returnValue.
    $("confirm-clear").showModal();
  });
}
$("confirm-clear").addEventListener("close", () => {
  if ($("confirm-clear").returnValue !== "confirm") return;
  update("清空测试记录失败", () => api(BASE + clearPath, { method: "DELETE" }));
});

$("login-form").addEventListener("submit", (e) => {
  e.preventDefault();
  key = $("login-key").value.trim();
  try { sessionStorage.setItem(KEY_STORE, key); } catch (_) {}
  start();
});

for (const name of TESTS) {
  $("tab-" + name).addEventListener("click", () => switchTab(name));
  $("tab-" + name).addEventListener("keydown", (e) => {
    if (!["ArrowLeft", "ArrowRight", "Home", "End"].includes(e.key)) return;
    e.preventDefault();
    const next = e.key === "Home" ? TESTS[0] : e.key === "End" ? TESTS.at(-1) : TESTS[(TESTS.indexOf(name) + (e.key === "ArrowRight" ? 1 : -1) + TESTS.length) % TESTS.length];
    switchTab(next); $("tab-" + next).focus();
  });
}
const savedTab = preference("tab", "candy");
switchTab(TESTS.includes(savedTab) ? savedTab : "candy");

for (const [test, selection, submit] of [["candy", candySelected, runCandy], ["fingerprint", fingerprintSelected, runFingerprint], ["pelican", pelicanSelected, runPelican]]) {
  // Each test keeps its own settings, saved by control after the control normalizes its value.
  $(test + "-toolbar").addEventListener("change", (e) => savePreference(e.target.id, e.target.type === "number" ? e.target.valueAsNumber : e.target.value));
  $(test + "-toolbar").addEventListener("submit", (e) => {
    e.preventDefault();
    const ids = batchCredentials(selection).map((a) => a.id);
    if (ids.length) submit({ credential_ids: ids });
  });
  $(test + "-select-all").addEventListener("change", (e) => {
    for (const a of visibleCredentials()) {
      if (!e.target.checked) selection.delete(a.id);
      else if (availableCredential(a)) selection.add(a.id);
    }
    render();
  });
  $(test + "-rows").addEventListener("change", (e) => {
    if (e.target.dataset.enable) return setCredentialEnabled(e.target.dataset.enable, e.target.checked);
    const id = e.target.getAttribute(`data-${test}-select`);
    if (!id) return;
    e.target.checked ? selection.add(id) : selection.delete(id);
    render();
  });
}
key = panelKey();
if (!key) { try { key = sessionStorage.getItem(KEY_STORE) || ""; } catch (_) {} }
key ? start() : showLogin("");
