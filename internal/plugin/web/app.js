"use strict";
const BASE = "/v0/management/plugins/cpa-codex-candy-eval";
const KEY_STORE = "cpa-codex-candy-eval.key";
const PREF_STORE = "cpa-codex-candy-eval.prefs";
const MASK_STORE = "cpa-codex-candy-eval.masked";
const DEFAULT_MODEL = "gpt-5.6-sol";
const DEFAULT_EFFORT = "low";
const DEFAULT_EFFORTS = ["none", "low", "medium", "high", "xhigh", "max"];
const HIDDEN_MODEL = (id) => id.toLowerCase().includes("image") || id.toLowerCase().split("/").pop().split("(")[0] === "codex-auto-review";
const FP_CONFIG = /*FINGERPRINT_CONFIG*/{};
const fpModes = Object.fromEntries(FP_CONFIG.modes.map((m) => [m.id, { name: m.name, requests: m.cells * m.samples_per_cell }]));

// Lucide icons (https://lucide.dev, ISC license).
const ICONS = {
  ban: '<circle cx="12" cy="12" r="10"/><path d="M4.929 4.929 19.07 19.071"/>',
  "file-key": '<path d="M14 2v5a1 1 0 0 0 1 1h5"/><path d="M4 12v6"/><path d="M4 14h2"/><path d="M9.65 22H18a2 2 0 0 0 2-2V8a2.4 2.4 0 0 0-.706-1.706l-3.588-3.588A2.4 2.4 0 0 0 14 2H6a2 2 0 0 0-2 2v4"/><circle cx="4" cy="20" r="2"/>',
  "key": '<path d="M2.586 17.414A2 2 0 0 0 2 18.828V21a1 1 0 0 0 1 1h3a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h1a1 1 0 0 0 1-1v-1a1 1 0 0 1 1-1h.172a2 2 0 0 0 1.414-.586l.814-.814a6.5 6.5 0 1 0-4-4z"/><circle cx="16.5" cy="7.5" r=".5" fill="currentColor"/>',
  candy: '<path d="M10 7v10.9"/><path d="M14 6.1V17"/><path d="M16 7V3a1 1 0 0 1 1.707-.707 2.5 2.5 0 0 0 2.152.717 1 1 0 0 1 1.131 1.131 2.5 2.5 0 0 0 .717 2.152A1 1 0 0 1 21 8h-4"/><path d="M16.536 7.465a5 5 0 0 0-7.072 0l-2 2a5 5 0 0 0 0 7.07 5 5 0 0 0 7.072 0l2-2a5 5 0 0 0 0-7.07"/><path d="M8 17v4a1 1 0 0 1-1.707.707 2.5 2.5 0 0 0-2.152-.717 1 1 0 0 1-1.131-1.131 2.5 2.5 0 0 0-.717-2.152A1 1 0 0 1 3 16h4"/>',
  "chevron-right": '<path d="m9 18 6-6-6-6"/>',
  copy: '<rect width="14" height="14" x="8" y="8" rx="2" ry="2"/><path d="M4 16c-1.1 0-2-.9-2-2V4c0-1.1.9-2 2-2h10c1.1 0 2 .9 2 2"/>',
  check: '<path d="M20 6 9 17l-5-5"/>',
  clock: '<circle cx="12" cy="12" r="10"/><path d="M12 6v6l4 2"/>',
  "arrow-down": '<path d="M12 5v14"/><path d="m19 12-7 7-7-7"/>',
  "arrow-up": '<path d="m5 12 7-7 7 7"/><path d="M12 19V5"/>',
  brain: '<path d="M12 18V5a3 3 0 0 0-5.997-.125 4 4 0 0 0-2.526 5.77 4 4 0 0 0 .556 6.588A4 4 0 1 0 12 18Z"/><path d="M12 18V5a3 3 0 0 1 5.997-.125 4 4 0 0 1 2.526 5.77 4 4 0 0 1-.556 6.588A4 4 0 1 1 12 18Z"/><path d="M15 13a4.5 4.5 0 0 1-3-4 4.5 4.5 0 0 1-3 4M6 17a4 4 0 0 1-1.967-.767M18 17a4 4 0 0 0 1.967-.767"/>',
  calendar: '<rect x="3" y="5" width="18" height="16" rx="2"/><path d="M16 3v4M8 3v4M3 11h18"/>',
  astroid: '<path d="M12.983 21.186a1 1 0 0 1-1.966 0 10 10 0 0 0-8.203-8.203 1 1 0 0 1 0-1.966 10 10 0 0 0 8.203-8.203 1 1 0 0 1 1.966 0 10 10 0 0 0 8.203 8.203 1 1 0 0 1 0 1.966 10 10 0 0 0-8.203 8.203"/>',
  "refresh-cw": '<path d="M3 12a9 9 0 0 1 15.36-6.36L21 8"/><path d="M21 3v5h-5"/><path d="M21 12a9 9 0 0 1-15.36 6.36L3 16"/><path d="M8 16H3v5"/>',
  "circle-check": '<circle cx="12" cy="12" r="10"/><path d="m16 9-5.5 5.5L8 12"/>',
  "circle-x": '<circle cx="12" cy="12" r="10"/><path d="m15 9-6 6"/><path d="m9 9 6 6"/>',
  "circle-alert": '<circle cx="12" cy="12" r="10"/><line x1="12" x2="12" y1="8" y2="12"/><line x1="12" x2="12.01" y1="16" y2="16"/>',
  "circle-help": '<circle cx="12" cy="12" r="10"/><path d="M9.1 9a3 3 0 0 1 5.8 1c0 2-3 3-3 3"/><path d="M12 17h.01"/>',
  "arrow-right": '<path d="M5 12h14m-6-6 6 6-6 6"/>',
  "shuffle": '<path d="m18 14 4 4-4 4m0-20 4 4-4 4M2 18h2.5c6 0 9-12 15-12H22M2 6h2.5c2 0 3.7 1.3 5.2 3.2M14.3 14.8c1.5 1.9 3.2 3.2 5.2 3.2H22"/>',
  "fingerprint": '<path d="M12 11a2 2 0 0 1 2 2c0 4-1 6-2 8M8 16c.4-1 .5-2 .5-3a3.5 3.5 0 0 1 7 0c0 4-1 7-2 9M5 16c.4-1 .5-2 .5-3a6.5 6.5 0 0 1 13 0c0 3-.4 5-1 7M2 12a10 10 0 0 1 20 0M8 20l1-2"/>',
  "pause": '<path d="M9 5H6v14h3zm9 0h-3v14h3z"/>',
  "x": '<path d="m18 6-12 12M6 6l12 12"/>',
  "chart": '<path d="M3 3v18h18M7 14v3m5-8v8m5-12v12"/>',
  "loader-circle": '<path d="M21 12a9 9 0 1 1-6.219-8.56"/>',
  play: '<path d="M5 5a2 2 0 0 1 3.008-1.728l11.997 6.998a2 2 0 0 1 .003 3.458l-12 7A2 2 0 0 1 5 19z"/>',
  eye: '<path d="M2.062 12.348a1 1 0 0 1 0-.696 10.75 10.75 0 0 1 19.876 0 1 1 0 0 1 0 .696 10.75 10.75 0 0 1-19.876 0"/><circle cx="12" cy="12" r="3"/>',
  "eye-off": '<path d="M10.733 5.076a10.744 10.744 0 0 1 11.205 6.575 1 1 0 0 1 0 .696 10.747 10.747 0 0 1-1.444 2.49"/><path d="M14.084 14.158a3 3 0 0 1-4.242-4.242"/><path d="M17.479 17.499a10.75 10.75 0 0 1-15.417-5.151 1 1 0 0 1 0-.696 10.75 10.75 0 0 1 4.446-5.143"/><path d="m2 2 20 20"/>',
  "trash-2": '<path d="M10 11v6"/><path d="M14 11v6"/><path d="M19 6v14a2 2 0 0 1-2 2H7a2 2 0 0 1-2-2V6"/><path d="M3 6h18"/><path d="M8 6V4a2 2 0 0 1 2-2h4a2 2 0 0 1 2 2v2"/>',
};
const icon = (name, cls = "") =>
  `<svg class="icon ${cls}" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">${ICONS[name]}</svg>`;

const $ = (id) => document.getElementById(id);
const esc = (s) => String(s ?? "").replace(/[&<>"']/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" })[c]);

let key = "";
let credentials = [];
let pollTimer = 0;
let loadError = "";
let loadController = null;
let pending = false;
let storageError = "";
let clearScope = "candy";
let tipTarget = null;
let copyTimer = 0;
const COPY_LABEL = icon("copy");
const candyExpanded = new Set();
const candyAnswersExpanded = new Set();
const candySelected = new Set();
const fpSelected = new Set();
const fpExpanded = new Set();
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
  if (!data || typeof data !== "object") throw new Error("服务器返回了无效数据，请稍后重试。");
  return data;
}

function stored(name) {
  try { return JSON.parse(localStorage.getItem(name)); } catch (_) { return null; }
}
function store(name, value) {
  try { localStorage.setItem(name, JSON.stringify(value)); } catch (_) {}
}
const prefs = () => stored(PREF_STORE) || {};
const savePrefs = () => store(PREF_STORE, { model: $("model").value, effort: $("effort").value, runs: runs() });
const runs = () => Math.min(Math.max(parseInt($("runs").value, 10) || 1, 1), 10);

function fillSelect(id, options, preferred, fallback) {
  const select = $(id);
  const entries = options.map((option) => typeof option === "string" ? [option, option] : option);
  const markup = entries.map(([value, label]) => `<option value="${esc(value)}">${esc(label)}</option>`).join("");
  if (select.innerHTML !== markup) select.innerHTML = markup;
  const values = entries.map(([value]) => value);
  select.value = values.includes(preferred) ? preferred : values.includes(fallback) ? fallback : values[0] || "";
}
function fillModels() {
  const ids = [...new Set(catalogCache.models?.ids || [])].sort();
  fillSelect("model", ids, $("model").value || prefs().model, DEFAULT_MODEL);
  fillSelect("fp-model", ids.filter((id) => !/[()]/.test(id)), $("fp-model").value || stored(PREF_STORE + ".fingerprint")?.model, DEFAULT_MODEL);
  fillSelect("mt-model", ids.filter((id) => !/[()]/.test(id)), $("mt-model").value || stored(PREF_STORE + ".modeltrace")?.model, DEFAULT_MODEL);
}
function initializeControls() {
  const saved = prefs(), fpSaved = stored(PREF_STORE + ".fingerprint") || {};
  fillSelect("effort", DEFAULT_EFFORTS, saved.effort, DEFAULT_EFFORT);
  $("effort").title = "none：不发送推理参数，由 CPA 处理；其他强度由 CPA 或上游验证";
  $("runs").value = saved.runs || 1;
  $("runs").value = runs();
  fillSelect("fp-mode", Object.entries(fpModes).map(([id, mode]) => [id, `${mode.name} · ${mode.requests} 次`]), fpSaved.mode, "quick");
  $("fp-concurrency").max = FP_CONFIG.max_concurrency;
  $("fp-concurrency").value = fpSaved.concurrency || FP_CONFIG.default_concurrency;
  $("fp-concurrency").value = fpConcurrency();
}

const scopeMatch = (r) => r.model === $("model").value && (r.effort || "none") === $("effort").value;
const kind = (r) => (r.skipped ? "skip" : r.error ? "err" : r.ok ? "ok" : "bad");
const VERDICT = { ok: "答对", bad: "答错", err: "出错", skip: "已跳过" };
const MARK = { ok: "circle-check", bad: "circle-x", err: "circle-alert", skip: "ban" };
const fmtNum = (n) => (n ? Number(n).toLocaleString("en-US") : "0");
const fmtSec = (ms) => (ms / 1000).toFixed(1) + "s";
function fmtTime(iso) {
  const d = new Date(iso);
  const p = (n) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}
const oneLine = (s) => String(s || "").replace(/\s+/g, " ").trim();
const modelName = (r) => r.effort ? `${r.model}(${r.effort})` : r.model;
const verdict = (r) => `<span class="verdict ${kind(r)}">${icon(MARK[kind(r)])}${VERDICT[kind(r)]}</span>`;
const metric = (name, label, value, cls = "") => `<span class="metric ${cls}" title="${esc(`${label} ${value}`)}" aria-label="${esc(`${label} ${value}`)}">${icon(name)}<span class="meta mono">${esc(value)}</span></span>`;
const modelMeta = (r) => metric("astroid", "模型", modelName(r), "model-meta");
const resultMeta = (r) => `<div class="result-meta">${verdict(r)}${metric("calendar", "测试时间", fmtTime(r.time))}${modelMeta(r)}</div>`;
const metrics = (r) => r.skipped ? "" : metric("clock", "用时", fmtSec(r.duration_ms)) +
  metric("arrow-down", "输入 tokens", fmtNum(r.input_tokens)) +
  metric("arrow-up", "输出 tokens", fmtNum(r.output_tokens)) +
  (r.reasoning_tokens > 0 ? metric("brain", "推理 tokens", fmtNum(r.reasoning_tokens)) : "");
const PLAN_NAMES = {
  free: "Free", plus: "Plus", team: "Team", pro: "Pro 20x",
  prolite: "Pro 5x", "pro-lite": "Pro 5x", pro_lite: "Pro 5x",
  self_serve_business_prolite: "Business Premium",
};
function planName(plan) {
  const name = String(plan || "").trim();
  return Object.hasOwn(PLAN_NAMES, name.toLowerCase()) ? PLAN_NAMES[name.toLowerCase()] : name || "其他";
}
const answerKey = (id, r) => JSON.stringify([id, r.time, r.model, r.effort]);
const answerToggle = (open) => `${icon("chevron-right", "chev")}${open ? "收起" : "展开"}`;

function credentialView(a, open, toggleAttribute) {
  return `<div class="credential"><button class="toggle" type="button" ${toggleAttribute}="${esc(a.id)}" aria-expanded="${open}">${icon("chevron-right", "chev")}<span class="name">${esc(a.email || a.name)}</span></button>
    <div class="tags"><span class="tag source-tag" title="${esc(credentialTypeLabel(a))}" aria-label="${esc(credentialTypeLabel(a))}">${icon(a.source === "auth_files" ? "file-key" : "key")}<span>${esc(a.provider)}</span></span>${a.source === "auth_files" && a.provider === "codex" ? `<span class="tag">${esc(planName(a.plan_type))}</span>` : ""}${a.disabled ? `<span class="tag">已停用</span>` : ""}</div></div>`;
}

function candyHistory(a, r, i) {
  const key = answerKey(a.id, r);
  const answerOpen = candyAnswersExpanded.has(key);
  const bodyID = `answer-${encodeURIComponent(a.id)}-${i}`;
  return `
  <div class="entry">
    <div class="entry-head">${resultMeta(r)}
      <span class="metrics">${metrics(r)}</span></div>
    <div class="entry-answer ${answerOpen ? "expanded" : ""}">
      <button class="answer-toggle" type="button" data-answer="${esc(key)}" aria-expanded="${answerOpen}" aria-controls="${esc(bodyID)}" aria-label="${answerOpen ? "收起文本" : "展开文本"}">${answerToggle(answerOpen)}</button>
      <pre id="${esc(bodyID)}" class="entry-body ${r.error ? "error" : ""}">${esc(r.error || r.answer)}</pre>
    </div>
  </div>`;
}

function renderCandyRow(a) {
  const results = a.results;
  const last = results[results.length - 1];
  const graded = results.filter((r) => scopeMatch(r) && !r.error && !r.skipped);
  const correct = graded.filter((r) => r.ok).length;
  const open = candyExpanded.has(a.id);

  const latest = last
    ? `<div class="latest-top">${verdict(last)}
         ${modelMeta(last)}</div>
       <div class="metrics">${metrics(last)}</div>`
    : `<span class="none">尚未测试</span>`;
  const rate = graded.length
    ? `<b class="mono">${Math.round((correct / graded.length) * 100)}%</b><small class="mono">${correct}/${graded.length}</small>`
    : `<span class="none">—</span>`;
  const marks = results.map((r, i) => `<button class="mark-hit" type="button" data-auth="${esc(a.id)}" data-i="${i}" aria-label="${esc(`${fmtTime(r.time)} ${modelName(r)} ${VERDICT[kind(r)]}`)}">${icon(MARK[kind(r)], `mark ${kind(r)} ${scopeMatch(r) ? "" : "dim"}`)}</button>`).join("");
  const action = a.running
    ? `<button class="btn ghost" type="button" disabled>${icon("loader-circle", "spin")}测试中 <span class="mono">${a.running.done}/${a.running.total}</span></button>`
    : `<button class="btn ghost" type="button" data-run="${esc(a.id)}" ${pending || !$("model").value || !availableCredential(a) ? "disabled" : ""}>${icon("play")}${a.fingerprint_running ? "指纹采集中" : "测试"}</button>`;

  return `<div class="row ${open ? "open" : ""}">
    <div class="list-row row-main" data-row="${esc(a.id)}">
      <input type="checkbox" data-candy-select="${esc(a.id)}" aria-label="选择此凭证" ${candySelected.has(a.id) ? "checked" : ""} ${pending || !$("model").value || !availableCredential(a) ? "disabled" : ""}>
      ${credentialView(a, open, "data-toggle")}
      <div class="latest">${latest}</div>
      <div class="row-summary ${results.length ? "" : "empty-history"}"><div class="rate">${rate}</div><div class="marks">${marks}</div></div>
      <div class="action">${action}</div>
    </div>
    ${open ? `<div class="history-panel candy-history"><div class="history">${[...results].reverse().map((r, i) => candyHistory(a, r, i)).join("") || `<div class="empty">还没有测试记录，点击「测试」开始。</div>`}</div></div>` : ""}
  </div>`;
}

function renderList(prefix, renderRow) {
  const id = prefix + "rows", list = $(id);
  const visible = visibleCredentials(prefix);
  const html = visible.length ? visible.map(renderRow).join("") : `<div class="empty">${esc(loadError || "当前类型没有凭证，请切换类型或在 CPA 中添加凭证。")}</div>`;
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
  $("rate-scope").textContent = `正确率按 ${modelName({ model: $("model").value, effort: $("effort").value })} 统计`;
  renderSelection("", candySelected, "测试");
  $("clear").disabled = pending || credentials.every((a) => !a.results.length);
  setNotice("storage-error", storageError);
  renderList("", renderCandyRow);
  renderFingerprints();
  renderModelTrace();
}

function stopPolling() {
  clearTimeout(pollTimer);
  loadController?.abort();
  loadController = null;
  renderRefresh();
}
async function load({ refresh = false } = {}) {
  stopPolling();
  const controller = new AbortController();
  loadController = controller;
  renderRefresh();
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
    if (!Array.isArray(data.auths)) throw new Error("服务器返回的测试记录格式无效。");
    credentials = data.auths;
    pruneCredentialCatalog();
    fillCredentialTypes();
    storageError = data.storage_error || "";
    setNotice("load-error", "");
    loadError = "";
    const ids = new Set(credentials.map((a) => a.id));
    const answers = new Set(credentials.flatMap((a) => a.results.map((r) => answerKey(a.id, r))));
    for (const id of candyExpanded) if (!ids.has(id)) candyExpanded.delete(id);
    for (const id of fpExpanded) if (!ids.has(id)) fpExpanded.delete(id);
    for (const id of mtExpanded) if (!ids.has(id)) mtExpanded.delete(id);
    for (const selection of [candySelected, fpSelected, mtSelected]) {
      for (const id of selection) if (!credentials.some((a) => a.id === id && !a.disabled)) selection.delete(id);
    }
    for (const id of candyAnswersExpanded) if (!answers.has(id)) candyAnswersExpanded.delete(id);
  } catch (err) {
    if (controller.signal.aborted) return;
    if (err instanceof AuthError) return showLogin(err.message);
    loadError = "读取测试结果失败：" + err.message;
    setNotice("load-error", loadError);
  }
  loadController = null;
  render();
  pollTimer = setTimeout(load, credentials.some((a) => a.running || a.fingerprint_running || a.modeltrace_running) ? 2500 : 20000);
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

function runCandy(body) {
  if (!$("toolbar").reportValidity()) return;
  savePrefs();
  return update("/run", { method: "POST", body: { ...body, model: $("model").value, effort: $("effort").value, runs: runs() } }, "开始测试失败");
}

function showLogin(message) {
  stopPolling();
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

function showTip(target) {
  const r = credentials.find((a) => a.id === target.dataset.auth)?.results[target.dataset.i];
  if (!r) return;
  hideTip();
  const tip = $("tip");
  tip.innerHTML = `${resultMeta(r)}
    <div class="metrics">${metrics(r)}</div>
    <div class="tip-text ${r.error ? "error" : ""}">${esc(oneLine(r.error || r.answer))}</div>`;
  tip.hidden = false;
  const box = target.getBoundingClientRect();
  const { width, height } = tip.getBoundingClientRect();
  const left = Math.min(Math.max(box.left + box.width / 2 - width / 2, 8), innerWidth - width - 8);
  const top = box.top - height - 8 >= 8 ? box.top - height - 8 : Math.max(8, Math.min(box.bottom + 8, innerHeight - height - 8));
  tip.style.left = left + "px";
  tip.style.top = top + "px";
  target.setAttribute("aria-describedby", "tip");
  tipTarget = target;
}
function hideTip() {
  $("tip").hidden = true;
  tipTarget?.removeAttribute("aria-describedby");
  tipTarget = null;
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
  for (const tab of ["candy", "fingerprint", "modeltrace"]) {
    const active = name === tab;
    $("tab-" + tab).setAttribute("aria-selected", active);
    $("tab-" + tab).tabIndex = active ? 0 : -1;
    $(tab + "-panel").hidden = !active;
  }
  hideTip();
  store(PREF_STORE + ".tab", name);
}
const fpModeName = (mode) => fpModes[mode]?.name || mode || "—";
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
const fpConcurrency = () => Math.min(Math.max(parseInt($("fp-concurrency").value, 10) || FP_CONFIG.default_concurrency, 1), FP_CONFIG.max_concurrency);
const fpSavePrefs = () => store(PREF_STORE + ".fingerprint", { model: $("fp-model").value, mode: $("fp-mode").value, concurrency: fpConcurrency() });

function resultOutcome({ tone = "", symbol, titleHTML, detailHTML = "", progressHTML = "" }) {
  return `<div class="outcome ${tone}"><span class="outcome-icon">${icon(symbol, progressHTML ? "spin" : "")}</span><div class="outcome-copy"><div class="outcome-title">${titleHTML}</div>${detailHTML ? `<div class="outcome-detail">${detailHTML}</div>` : ""}${progressHTML}</div></div>`;
}
function collectionOutcome(p, total, showModel = false) {
  const phase = p.phase === "cancelling" ? "正在停止" : p.phase === "comparing" ? "正在分析指纹" : "正在采集";
  return resultOutcome({
    symbol: "loader-circle",
    titleHTML: `${phase} <span class="meta mono">${p.done}/${total}</span>`,
    detailHTML: showModel ? `<span class="mono">${esc(p.model)}</span>` : "",
    progressHTML: `<progress class="collection-progress" value="${p.done}" max="${total}" aria-label="采集进度"></progress>`,
  });
}
function historyEntry(type, credentialID, r, outcomeHTML, metaHTML) {
  return `<article class="history-entry"><div class="history-info"><time class="mono" datetime="${esc(r.time)}">${esc(fmtTime(r.time))}</time><div class="meta">${metaHTML}</div></div>
    ${outcomeHTML}<button class="btn ghost" type="button" data-${type}-detail="${esc(r.id)}" data-${type}-credential="${esc(credentialID)}">${icon("chart")}查看详情</button></article>`;
}
function historyPanel(entries) {
  return `<div class="history-panel"><div class="history-title">历史记录</div><div class="result-history">${entries || `<div class="empty">暂无记录</div>`}</div></div>`;
}
function openResultDetail(type, recordID, credentialID) {
  const dialog = $(type + "-detail");
  dialog.dataset.record = recordID;
  dialog.dataset.credential = credentialID;
  dialog.showModal();
}
function bindCollectionActions({ type, scope, historyKey, expanded, renderRows, run, showDetail }) {
  const rows = $(type + "-rows"), dialog = $(type + "-detail");
  $(type + "-detail-close").addEventListener("click", () => dialog.close());
  dialog.addEventListener("close", () => {
    rows.querySelector(`[data-${type}-detail="${CSS.escape(dialog.dataset.record)}"][data-${type}-credential="${CSS.escape(dialog.dataset.credential)}"]`)?.focus();
  });
  rows.addEventListener("click", (e) => {
    const action = (name) => e.target.closest(`[data-${type}-${name}]`)?.getAttribute(`data-${type}-${name}`);
    const runID = action("run");
    if (runID) return run({ auth_ids: [runID] });
    const cancelID = action("cancel");
    if (cancelID) return update(`/${scope}/cancel`, { method: "POST", body: { auth_ids: [cancelID] } }, "停止测试失败");
    const toggleID = action("toggle");
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

function fpOutcome(r) {
  const status = r.attribution?.status;
  const model = `<span class="mono">${esc(r.model)}</span>`;
  const retry = r.mode === "strict" ? "建议稍后重新采集" : "建议用严格模式复测";
  const states = {
    consistent: ["ok", "circle-check", "与所选模型一致", model],
    substitution: ["warn", "shuffle", "疑似模型替换", `${model}${icon("arrow-right")}<strong class="mono">${esc(r.attribution?.nearest)}</strong>`],
    different: ["bad", "circle-alert", "与所选模型有差异", "暂无法确定实际模型"],
    ambiguous: ["neutral", "circle-help", "暂无法判断", retry],
    insufficient: ["neutral", "circle-help", "有效回答不足", "建议重新采集"],
    no_baseline: ["neutral", "circle-help", "暂不支持判断此模型", model],
    unstable: ["warn", "circle-alert", "结果不稳定", retry],
    cancelled: ["neutral", "pause", "采集已停止", model],
    failed: ["bad", "circle-x", "采集失败", "请稍后重试"],
    skipped: ["neutral", "ban", "已跳过", "此凭证不含所选模型"],
  };
  const [tone, symbol, title, detail] = states[status] || ["idle", "fingerprint", "等待采集", ""];
  return resultOutcome({ tone, symbol, titleHTML: title, detailHTML: detail });
}
function fpHistory(a, r) {
  return historyEntry("fp", a.id, r, fpOutcome(r), `<span class="mono">${esc(r.model)}</span> · ${esc(fpModeName(r.mode))}`);
}
const fpMetric = (n) => n == null ? "—" : Number(n).toFixed(4);
const fpComparisonNames = { match: "相似", uncertain: "不确定", mismatch: "不同", insufficient: "样本不足" };
const fpProbeNames = { "random-number-1-100": "随机数 1–100", "random-number-1-10": "随机数 1–10", "random-letter": "随机字母", "random-color": "随机颜色", "coin-flip": "抛硬币", "random-animal": "随机动物", "random-city": "随机城市", "favorite-number": "喜欢的数字" };
let fpDetailRecord = null;
function showFingerprintDetail(r, credentialID) {
  fpDetailRecord = r;
  const attr = r.attribution || {};
  const comparisons = attr.comparisons || [];
  const stat = (label, value) => `<div class="fp-stat"><dt>${label}</dt><dd class="mono">${esc(value)}</dd></div>`;
  $("fp-detail-body").innerHTML = `<p class="result-dialog-meta"><span class="mono">${esc(r.model)}</span> · ${esc(fpModeName(r.mode))}模式 · <time class="mono" datetime="${esc(r.time)}">${esc(fmtTime(r.time))}</time></p>
    ${fpOutcome(r)}
    <p class="detail-note">${esc(attr.message || "尚无归因结果")}</p>
    ${r.error ? `<p class="detail-note detail-warning">${esc(r.error)}</p>` : ""}
    ${r.status === "skipped" ? "" : `<dl class="fp-stats">${stat("采集进度", `${r.done} / ${r.total}`)}${stat("有效回答", r.valid)}${stat("请求失败", r.errors)}${stat("用时", fmtSec(r.duration_ms))}</dl>
    <section class="result-dialog-section"><h3>基准比对</h3>
      <div class="fp-detail-scroll"><table class="fp-detail-table"><thead><tr><th>基准模型</th><th>距离 JSD ↓</th><th>p 值</th><th>有效探针</th><th>距离判断</th></tr></thead><tbody>
        ${comparisons.map((c) => `<tr class="${c.model === attr.nearest ? "closest" : ""}"><td class="mono">${esc(c.model)}${c.model === r.model ? " · 所选" : ""}${c.model === attr.nearest ? " · 最近" : ""}</td><td class="mono">${fpMetric(c.mean_jsd)}</td><td class="mono">${fpMetric(c.p_value)}</td><td>${c.cells?.length || 0}</td><td>${esc(fpComparisonNames[c.verdict] || c.verdict)}</td></tr>`).join("") || `<tr><td colspan="5">暂无可比数据</td></tr>`}
      </tbody></table></div>
      <p class="detail-note">距离越小，回答分布越接近。p 值低于 ${fpMetric(attr.alpha)} 时视为差异显著；归因同时考虑距离与统计检验。</p>
    </section>
    <section class="result-dialog-section"><h3>结果稳定性</h3>
      <p class="detail-note">本次自一致性 JSD：<span class="mono">${fpMetric(attr.self_jsd)}</span>（越小越稳定）${attr.reference_p_value != null ? `<br>所选模型与最近模型的基准差异 p 值：<span class="mono">${fpMetric(attr.reference_p_value)}</span>` : ""}</p>
      ${(attr.warnings || []).map((w) => `<p class="detail-note detail-warning">${esc(w)}</p>`).join("")}
    </section>
    <section class="result-dialog-section"><h3>逐项比对</h3><label class="field fp-probe-select"><span class="field-label">对比模型</span><span class="native-select"><select id="fp-detail-baseline">${comparisons.map((c) => `<option>${esc(c.model)}</option>`).join("")}</select></span></label><div id="fp-detail-probes" class="fp-detail-scroll"></div></section>`}`;
  if (r.status !== "skipped") renderFingerprintProbes();
  openResultDetail("fp", r.id, credentialID);
}
function renderFingerprintProbes() {
  const comparison = fpDetailRecord?.attribution?.comparisons?.find((c) => c.model === $("fp-detail-baseline").value);
  const rows = (comparison?.cells || []).map((c) => {
    const [task, lang] = String(c.cell || "").split(":");
    return `<tr><td>${esc(fpProbeNames[task] || task)} · ${lang === "zh" ? "中文" : "英文"}</td><td class="mono">${fpMetric(c.jsd)}</td><td>${esc(c.valid_a ?? "—")}</td><td>${esc(c.valid_b ?? "—")}</td></tr>`;
  }).join("");
  $("fp-detail-probes").innerHTML = `<table class="fp-detail-table"><thead><tr><th>探针</th><th>距离 JSD</th><th>本次有效回答</th><th>基准有效回答</th></tr></thead><tbody>${rows || `<tr><td colspan="4">有效样本不足，暂无可比探针</td></tr>`}</tbody></table>`;
}
function renderFingerprintRow(a) {
  const history = a.fingerprints || [];
  const last = history[history.length - 1];
  const p = a.fingerprint_running;
  const open = fpExpanded.has(a.id);
  const latest = p ? collectionOutcome(p, p.total, true) : fpOutcome(last || {});
  return `<div class="row ${open ? "open" : ""}"><div class="list-row collection-row">
    <input type="checkbox" data-fp-select="${esc(a.id)}" aria-label="选择此凭证" ${fpSelected.has(a.id) ? "checked" : ""} ${pending || !$("fp-model").value || !availableCredential(a) ? "disabled" : ""}>
    ${credentialView(a, open, "data-fp-toggle")}
    <div class="latest">${latest}</div>
    <div class="fp-mode">${esc(fpModeName(p?.mode || last?.mode))}</div>
    <div class="test-time mono" ${last && !p ? `title="${esc(fmtTime(last.time))}"` : ""}>${last && !p ? esc(fmtTime(last.time)) : "—"}</div>
    <div class="action">${p ? `<button class="btn ghost" type="button" data-fp-cancel="${esc(a.id)}" ${pending || p.phase === "cancelling" ? "disabled" : ""}>停止</button>` : `<button class="btn" type="button" data-fp-run="${esc(a.id)}" ${pending || !$("fp-model").value || !availableCredential(a) ? "disabled" : ""}>${icon("play")}采集</button>`}</div></div>
    ${open ? historyPanel([...history].reverse().map((r) => fpHistory(a, r)).join("")) : ""}</div>`;
}
function renderFingerprints() {
  renderSelection("fp-", fpSelected, "采集");
  const selected = selectedCredentials("fp-", fpSelected).filter(availableCredential);
  $("fp-clear").disabled = pending || !credentials.some((a) => a.fingerprints?.length);
  const count = fpModes[$("fp-mode").value]?.requests || fpModes.quick.requests;
  $("fp-cost").textContent = `每个凭证 ${count} 次请求（失败会重试）${selected.length ? ` · 已选 ${selected.length} 个，共 ${count * selected.length} 次` : ""}`;
  renderList("fp-", renderFingerprintRow);
}
function runFingerprint(body) {
  if (!$("fp-toolbar").reportValidity()) return;
  fpSavePrefs();
  return update("/fingerprint/run", { method: "POST", body: { ...body, model: $("fp-model").value, mode: $("fp-mode").value, concurrency: fpConcurrency() } }, "开始采集失败");
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

const notices = new Map();
function hideNotice(id) {
  const notice = notices.get(id);
  if (notice) { clearTimeout(notice.timer); notice.timer = 0; }
  $(id).hidden = true;
}
function setNotice(id, message, tone = "error") {
  message = String(message || "");
  const previous = notices.get(id);
  // Polling must not reopen a dismissed or expired copy of the same error.
  if (previous?.message === message && previous?.tone === tone) return;
  hideNotice(id);
  const notice = { message, tone, timer: 0 };
  notices.set(id, notice);
  const node = $(id);
  node.querySelector(".notice-message").textContent = message;
  if (!message) return;
  node.querySelector(".notice-symbol").innerHTML = icon(tone === "error" ? "circle-alert" : tone === "warning" ? "circle-help" : "circle-check");
  node.dataset.tone = tone;
  node.setAttribute("role", tone === "error" ? "alert" : "status");
  node.hidden = false;
  notice.timer = setTimeout(() => hideNotice(id), 8000);
}

function credentialCard(prefix, columns) {
  const label = prefix === "mt-" ? "ModelTrace" : prefix ? "指纹" : "糖果";
  return `<div class="section-head"><div class="credential-heading"><h2>凭证</h2><span class="native-select credential-filter"><select id="${prefix}credential-type" aria-label="凭证类型"><option value="all">全部凭证</option><option value="auth_files:codex" selected>认证文件 · codex</option></select></span></div>
    <div class="list-actions"><button id="${prefix}mask" class="btn ghost icon-button" type="button" title="脱敏" aria-label="脱敏"></button><button id="${prefix}clear" class="btn ghost icon-button" type="button" title="清空${label}历史" aria-label="清空${label}历史" disabled>${icon("trash-2")}</button></div></div>
    <div class="list-head"><input id="${prefix}select-all" type="checkbox" aria-label="选择全部可测试凭证">${columns.map((label) => `<div>${label}</div>`).join("")}</div>
    <div id="${prefix}rows"><div class="notice">正在加载…</div></div>`;
}
function initializeLayout() {
  $("candy-credentials").innerHTML = credentialCard("", ["凭证", "最近一次", "正确率", "最近 20 次", ""]);
  $("fp-credentials").innerHTML = credentialCard("fp-", ["凭证", "指纹结果", "模式", "测试时间", ""]);
  $("mt-credentials").innerHTML = credentialCard("mt-", ["凭证", "测试模型", "归因结果", "测试时间", ""]);
  $("notifications").innerHTML = ["flash", "load-error", "catalog-error", "storage-error"].map((id) => `<div id="${id}" class="global-flash" role="alert" hidden><span class="notice-symbol" aria-hidden="true"></span><span class="notice-message"></span><button class="notice-close" type="button" title="隐藏提示" aria-label="隐藏提示">${icon("x")}</button></div>`).join("");
}

// Events and initialization.
initializeLayout();
initializeControls();
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

$("copy").innerHTML = COPY_LABEL;
$("copy").addEventListener("click", async () => {
  const prompt = $("question").textContent;
  try {
    if (navigator.clipboard) {
      await navigator.clipboard.writeText(prompt);
    } else {
      const area = Object.assign(document.createElement("textarea"), { value: prompt, className: "clipboard-buffer" });
      document.body.append(area);
      try {
        area.select();
        if (!document.execCommand("copy")) throw new Error("copy failed");
      } finally {
        area.remove();
        $("copy").focus();
      }
    }
  } catch (_) {
    $("copy").setAttribute("aria-label", "复制失败，请手动选择题目复制");
    $("copy").title = "复制失败，请手动选择题目复制";
    return;
  }
  clearTimeout(copyTimer);
  $("copy").innerHTML = icon("check");
  $("copy").classList.add("copied");
  $("copy").setAttribute("aria-label", "已复制");
  $("copy").title = "已复制";
  copyTimer = setTimeout(() => {
    $("copy").innerHTML = COPY_LABEL;
    $("copy").classList.remove("copied");
    $("copy").setAttribute("aria-label", "复制题目");
    $("copy").title = "复制题目";
  }, 1600);
});

applyTheme();
addEventListener("storage", applyTheme);
matchMedia("(prefers-color-scheme: dark)").addEventListener("change", applyTheme);

$("rows").addEventListener("mouseover", (e) => {
  const target = e.target.closest("[data-auth]");
  if (target?.contains(e.relatedTarget)) return;
  target ? showTip(target) : hideTip();
});
$("rows").addEventListener("mouseleave", hideTip);
$("rows").addEventListener("focusin", (e) => {
  const target = e.target.closest("[data-auth]");
  target ? showTip(target) : hideTip();
});
$("rows").addEventListener("focusout", hideTip);
$("rows").addEventListener("keydown", (e) => { if (e.key === "Escape") hideTip(); });
addEventListener("scroll", hideTip, true);
addEventListener("resize", hideTip);

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

$("model").addEventListener("change", () => { savePrefs(); render(); });
$("effort").addEventListener("change", () => { savePrefs(); render(); });
$("runs").addEventListener("change", () => { $("runs").value = runs(); savePrefs(); });
$("rows").addEventListener("click", (e) => {
  if (e.target.closest("[data-candy-select]")) return;
  const hit = e.target.closest("[data-auth]");
  if (hit) return showTip(hit);
  const answer = e.target.closest("[data-answer]");
  if (answer) {
    const key = answer.dataset.answer;
    const open = !candyAnswersExpanded.has(key);
    open ? candyAnswersExpanded.add(key) : candyAnswersExpanded.delete(key);
    answer.closest(".entry-answer").classList.toggle("expanded", open);
    answer.setAttribute("aria-expanded", open);
    answer.setAttribute("aria-label", open ? "收起文本" : "展开文本");
    answer.innerHTML = answerToggle(open);
    return;
  }
  const btn = e.target.closest("[data-run]");
  if (btn) return runCandy({ auth_ids: [btn.dataset.run] });
  const row = e.target.closest("[data-row]");
  if (!row) return;
  const id = row.dataset.row;
  candyExpanded.has(id) ? candyExpanded.delete(id) : candyExpanded.add(id);
  render();
});

const tabNames = ["candy", "fingerprint", "modeltrace"];
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

$("fp-detail-body").addEventListener("change", (e) => { if (e.target.id === "fp-detail-baseline") renderFingerprintProbes(); });
$("fp-detail").addEventListener("close", () => { fpDetailRecord = null; });

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
for (const id of ["fp-model", "fp-mode"]) $(id).addEventListener("change", () => { fpSavePrefs(); renderFingerprints(); });
$("fp-concurrency").addEventListener("change", () => { $("fp-concurrency").value = fpConcurrency(); fpSavePrefs(); });
bindCollectionActions({ type: "fp", scope: "fingerprint", historyKey: "fingerprints", expanded: fpExpanded, renderRows: renderFingerprints, run: runFingerprint, showDetail: showFingerprintDetail });

key = panelKey();
if (!key) { try { key = sessionStorage.getItem(KEY_STORE) || ""; } catch (_) {} }
key ? start() : showLogin("");
