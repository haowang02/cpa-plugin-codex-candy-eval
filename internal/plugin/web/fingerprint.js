"use strict";
const FINGERPRINT_CONFIG = /*FINGERPRINT_CONFIG*/{};
const fingerprintSelected = new Set();
const fingerprintExpanded = new Set();
const fingerprintConcurrency = () => boundedInput("fingerprint-concurrency", FINGERPRINT_CONFIG.requests, FINGERPRINT_CONFIG.requests);

// The candidate a model name stands for, as fingerprintModelID compares them on the server.
function fingerprintModelID(model) {
  const id = String(model || "").trim().toLowerCase().split("/").pop();
  return Object.hasOwn(FINGERPRINT_CONFIG.aliases, id) ? FINGERPRINT_CONFIG.aliases[id] : id;
}

function fingerprintComparison(r) {
  const a = r?.attribution;
  if (!a || !["completed", "partial"].includes(r.status)) return { tone: "neutral", symbol: "chart", label: "" };
  if (a.decision === "not_model") return { tone: "warn", symbol: "circle-alert", label: "不像语言模型回答" };
  return fingerprintModelID(r.model) === fingerprintModelID(a.prediction)
    ? { tone: "ok", symbol: "circle-check", label: "与测试模型一致" }
    : { tone: "warn", symbol: "shuffle", label: "与测试模型不一致" };
}

const FINGERPRINT_STATES = { failed: ["bad", "circle-x", "测试失败", "查看详情了解原因"], cancelled: ["neutral", "circle-pause", "测试已停止", ""], skipped: ["neutral", "ban", "已跳过", "此凭证不支持所选模型"] };

function fingerprintOutcome(r) {
  const a = r?.attribution, state = FINGERPRINT_STATES[r?.status];
  if (state || !a) {
    const [tone, symbol, title, detail] = state || ["idle", "fingerprint", "等待测试", ""];
    return resultOutcome({ tone, symbol, titleHTML: title, detailHTML: detail });
  }
  const { tone, symbol, label } = fingerprintComparison(r);
  if (a.decision === "not_model") return resultOutcome({ tone, symbol, titleHTML: label, detailHTML: "相邻数字的变化方式与语言模型不同，未生成排名" });
  const probability = a.probability != null ? `<span class="fingerprint-probability mono">${fmtPercent(a.probability)}</span>` : "";
  return resultOutcome({
    tone, symbol,
    titleHTML: `<span class="fingerprint-prediction"><span>${esc(a.prediction_name)}</span>${probability}</span>`,
    detailHTML: `${label}${r.status === "partial" ? " · 部分结果" : ""}`,
  });
}

function renderFingerprintRow(a) {
  const records = a.fingerprints, last = records.at(-1), p = a.running?.test === "fingerprint" ? a.running : null;
  return credentialRow({
    test: "fingerprint", credential: a, selected: fingerprintSelected.has(a.id), open: fingerprintExpanded.has(a.id), selectable: runnable("fingerprint", a),
    button: collectionButton("fingerprint", a, p, "测试"), result: p ? collectionOutcome(p) : fingerprintOutcome(last), tested: !!(last || p),
    meta: `<div class="fingerprint-test-model mono">${esc(p?.model || last?.model || "—")}</div>${listTime(!p && last?.time)}`,
    history: () => [...records].reverse().map((r) => historyEntry("fingerprint", a.id, r, fingerprintOutcome(r))).join(""),
  });
}

function renderFingerprint() {
  renderSelection("fingerprint", fingerprintSelected, "测试");
  const selected = selectedCredentials(fingerprintSelected).filter(availableCredential);
  $("fingerprint-cost").textContent = `每个凭证 ${FINGERPRINT_CONFIG.requests} 次请求（失败或数字不足时重试）${selected.length ? ` · 已选 ${selected.length} 个，共 ${FINGERPRINT_CONFIG.requests * selected.length} 次` : ""}`;
  $("fingerprint-clear").disabled = pending || !credentials.some((a) => a.fingerprints.length);
  renderList("fingerprint", renderFingerprintRow);
}

function runFingerprint(body) {
  if (!$("fingerprint-toolbar").reportValidity()) return;
  return startTest("fingerprint", { ...body, model: $("fingerprint-model").value, concurrency: fingerprintConcurrency() });
}

function fingerprintResultHTML(r) {
  const a = r.attribution, comparison = fingerprintComparison(r), scored = a.probability != null;
  if (a.decision === "not_model") return `${fingerprintOutcome(r)}<p class="detail-note">相邻数字的变化方式与参考库中的语言模型回答不同，常见于程序生成、排序或打乱顺序的数字，因此未生成排名。</p>`;
  return `<dl class="fingerprint-summary ${comparison.tone}"><div><dt>${comparison.label ? `${icon(comparison.symbol)}${comparison.label}` : "最接近的模型"}</dt><dd class="mono">${esc(a.prediction_name)}${scored ? `<small>${fmtPercent(a.probability)}</small>` : ""}</dd></div><div><dt>模型家族</dt><dd>${esc(a.family_prediction_name)}</dd></div><div><dt>有效回答</dt><dd class="mono">${a.used_outputs} / ${FINGERPRINT_CONFIG.requests}</dd></div></dl>
    <div class="fingerprint-heading"><h3>候选模型排行</h3><span class="meta">${scored ? "归因概率" : "有效回答不足三条，不计算归因概率"}</span></div>
    <div class="fingerprint-ranking">${(a.results || []).map((c, i) => `<div class="fingerprint-rank"><span class="meta mono">${String(i + 1).padStart(2, "0")}</span><span class="mono fingerprint-rank-name">${esc(c.display_name)}</span><div class="fingerprint-bar" aria-hidden="true"><span style="width:${Math.max(0, Math.min(100, (c.probability || 0) * 100))}%"></span></div><span class="mono">${fmtPercent(c.probability)}</span></div>`).join("")}</div>`;
}

const fingerprintSamplePart = (label, text, mono = false) => `<div class="fingerprint-sample-part"><div class="meta">${label}</div><pre${mono ? ' class="mono"' : ""}>${esc(text)}</pre></div>`;

function showFingerprintDetail(r) {
  const samples = r.samples || [];
  const sampleEntries = samples.map((s, i) => `<details class="fingerprint-sample">
    <summary>${icon("chevron-right", "chev")}挑战 ${i + 1}<span class="meta">${s.parsed_numbers} / ${s.expected_count} 个数字${s.attempts > 1 ? ` · 重试 ${s.attempts - 1} 次` : ""}</span><span class="verdict ${s.accepted ? "ok" : "err"}">${s.accepted ? "有效" : "未计入"}</span></summary>
    ${s.instructions ? fingerprintSamplePart("开发者指令", s.instructions) : ""}${fingerprintSamplePart("提示词", s.prompt)}${s.text ? fingerprintSamplePart("回答", s.text, true) : ""}${s.error ? `<p class="detail-warning">${esc(s.error)}</p>` : ""}
  </details>`).join("");
  $("fingerprint-detail-body").innerHTML = `<p class="result-dialog-meta"><span class="mono">${esc(r.model)}</span> · ${esc(fmtTime(r.time))} · ${fmtSec(r.duration_ms)}${r.concurrency ? ` · 并发 ${esc(r.concurrency)}` : ""}</p>
    ${r.attribution && r.status !== "completed" ? `<p class="detail-note">${{ partial: "部分结果", cancelled: "测试已停止" }[r.status]}</p>` : ""}
    ${r.attribution ? fingerprintResultHTML(r) : fingerprintOutcome(r)}
    ${r.error ? `<p class="detail-note detail-warning">${esc(r.error)}</p>` : ""}
    ${samples.length ? `<div class="fingerprint-heading"><h3>挑战记录</h3><span class="meta">${samples.reduce((n, s) => n + s.attempts, 0)} 次请求</span></div>${sampleEntries}` : ""}`;
}

function initializeFingerprint() {
  $("fingerprint-concurrency").max = FINGERPRINT_CONFIG.requests;
  $("fingerprint-concurrency").value = preference("fingerprint-concurrency", FINGERPRINT_CONFIG.requests);
  $("fingerprint-concurrency").value = fingerprintConcurrency();
  $("fingerprint-model").addEventListener("change", renderFingerprint);
  $("fingerprint-concurrency").addEventListener("change", () => { $("fingerprint-concurrency").value = fingerprintConcurrency(); });
  bindTestActions({ test: "fingerprint", expanded: fingerprintExpanded, renderRows: renderFingerprint, run: runFingerprint, showDetail: showFingerprintDetail });
}
