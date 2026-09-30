"use strict";

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
  "square-arrow-right-enter": '<path d="m10 16 4-4-4-4"/><path d="M3 12h11"/><path d="M3 8V5a2 2 0 0 1 2-2h14a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-3"/>',
  "square-arrow-right-exit": '<path d="M10 12h11"/><path d="m17 16 4-4-4-4"/><path d="M21 6.344V5a2 2 0 0 0-2-2H5a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-1.344"/>',
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

function fillSelect(id, options, preferred, fallback) {
  const select = $(id);
  const entries = options.map((option) => typeof option === "string" ? [option, option] : option);
  const markup = entries.map(([value, label]) => `<option value="${esc(value)}">${esc(label)}</option>`).join("");
  if (select.innerHTML !== markup) select.innerHTML = markup;
  const values = entries.map(([value]) => value);
  select.value = values.includes(preferred) ? preferred : values.includes(fallback) ? fallback : values[0] || "";
}

const boundedInput = (id, fallback, maximum) => Math.min(Math.max(parseInt($(id).value, 10) || fallback, 1), maximum);

const fmtNum = (n) => (n ? Number(n).toLocaleString("en-US") : "0");
const fmtSec = (ms) => (ms / 1000).toFixed(1) + "s";
function fmtTime(iso) {
  const d = new Date(iso);
  if (!Number.isFinite(d.getTime())) return "—";
  const p = (n) => String(n).padStart(2, "0");
  return `${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`;
}
const modelName = (r) => r.effort ? `${r.model}(${r.effort})` : r.model;
const metric = (name, label, value, cls = "") => `<span class="metric ${cls}" title="${esc(`${label} ${value}`)}" aria-label="${esc(`${label} ${value}`)}">${icon(name)}<span class="meta mono">${esc(value)}</span></span>`;
const modelMeta = (r) => metric("astroid", "模型", modelName(r), "model-meta");
const metrics = (r) =>
  metric("clock", "耗时", r.duration_ms != null ? fmtSec(r.duration_ms) : "—") +
  metric("square-arrow-right-enter", "输入 tokens", r.input_tokens != null ? fmtNum(r.input_tokens) : "—") +
  metric("square-arrow-right-exit", "输出 tokens", r.output_tokens != null ? fmtNum(r.output_tokens) : "—") +
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

function credentialPlan(a) {
  if (a.source !== "auth_files" || a.provider !== "codex") return "other";
  const type = String(a.plan_type || "").trim().toLowerCase();
  return ["pro", "prolite", "pro-lite", "pro_lite"].includes(type) ? "pro" : ["plus", "team", "free"].includes(type) ? type : "other";
}
const credentialPlanRank = (a) => ({ pro: 0, plus: 1, team: 2, other: 2, free: 3 })[credentialPlan(a)];

function credentialView(a, open) {
  return `<div class="credential"><button class="toggle" type="button" data-toggle="${esc(a.id)}" aria-expanded="${open}">${icon("chevron-right", "chev")}<span class="name">${esc(a.email || a.name)}</span></button>
    <div class="tags"><span class="tag source-tag" title="${esc(credentialTypeLabel(a))}" aria-label="${esc(credentialTypeLabel(a))}">${icon(a.source === "auth_files" ? "file-key" : "key")}<span>${esc(a.provider)}</span></span>${a.source === "auth_files" && a.provider === "codex" ? `<span class="tag plan-tag plan-${credentialPlan(a)}">${esc(planName(a.plan_type))}</span>` : ""}${a.disabled ? `<span class="tag">已停用</span>` : ""}</div></div>`;
}

function resultOutcome({ tone = "", symbol, titleHTML, detailHTML = "", progressHTML = "" }) {
  return `<div class="outcome ${tone}"><span class="outcome-icon">${icon(symbol, progressHTML ? "spin" : "")}</span><div class="outcome-copy"><div class="outcome-title">${titleHTML}</div>${detailHTML ? `<div class="outcome-detail">${detailHTML}</div>` : ""}${progressHTML}</div></div>`;
}
function collectionOutcome(p, total, showModel = false) {
  const stopping = p.phase === "cancelling";
  const phase = stopping ? "等待当前请求结束" : p.phase === "comparing" ? "正在分析指纹" : "正在采集";
  return resultOutcome({
    symbol: "loader-circle",
    titleHTML: `${phase} <span class="meta mono">${p.done}/${total}</span>`,
    detailHTML: stopping ? "已停止后续请求，当前请求返回后保存结果" : showModel ? `<span class="mono">${esc(p.model)}</span>` : "",
    progressHTML: `<progress class="collection-progress" value="${p.done}" max="${total}" aria-label="采集进度"></progress>`,
  });
}
function collectionButton(type, credential, progress, label) {
  if (progress) return `<button class="btn ghost" type="button" data-${type}-cancel="${esc(credential.id)}" title="停止后续请求；已发出的请求需等待返回" ${pending || progress.phase === "cancelling" ? "disabled" : ""}>停止</button>`;
  const disabled = pending || !$(type + "-model").value || !availableCredential(credential);
  return `<button class="btn" type="button" data-${type}-run="${esc(credential.id)}" ${disabled ? "disabled" : ""}>${icon("play")}${esc(label)}</button>`;
}
function historyCard(r, contentHTML, extraMetaHTML = "") {
  return `<article class="history-card">
    <div class="history-card-head"><div class="result-meta">${metric("calendar", "测试时间", fmtTime(r.time))}${modelMeta(r)}${extraMetaHTML}</div><span class="metrics">${metrics(r)}</span></div>
    <div class="history-card-body">${contentHTML}</div>
  </article>`;
}
function historyEntry(type, credentialID, r, outcomeHTML, extraMetaHTML = "") {
  return historyCard(r, `${outcomeHTML}<button class="btn ghost" type="button" data-${type}-detail="${esc(r.id)}" data-${type}-credential="${esc(credentialID)}">${icon("chart")}查看详情</button>`, extraMetaHTML);
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
  return `<div class="section-head"><div class="credential-heading"><h2>凭证</h2><span class="native-select credential-filter"><select id="${prefix}credential-type" aria-label="凭证类型"><option value="all">全部凭证</option><option value="auth_files:codex" selected>认证文件 · codex</option></select></span><span class="native-select credential-filter"><select id="${prefix}select-plan" aria-label="快速选取凭证"><option value="">快速选取凭证</option><option value="plus">选取所有 PLUS</option><option value="pro">选取所有 PRO</option><option value="team">选取所有 TEAM</option></select></span></div>
    <div class="list-actions"><button id="${prefix}mask" class="btn ghost icon-button" type="button" title="脱敏" aria-label="脱敏"></button><button id="${prefix}clear" class="btn ghost icon-button" type="button" title="清空${label}历史" aria-label="清空${label}历史" disabled>${icon("trash-2")}</button></div></div>
    <div class="list-head"><input id="${prefix}select-all" type="checkbox" aria-label="选择全部可测试凭证">${columns.map((label) => `<div>${label}</div>`).join("")}</div>
    <div id="${prefix}rows"><div class="empty">正在加载…</div></div>`;
}
