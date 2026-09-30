"use strict";
const MT_CONFIG = /*MODELTRACE_CONFIG*/{};
const mtSelected = new Set();
const mtExpanded = new Set();
const mtPercent = (value) => Number.isFinite(value) && value >= 0 && value <= 1 ? `${(value * 100).toFixed(1)}%` : "—";
const mtConcurrency = () => boundedInput("mt-concurrency", MT_CONFIG.default_concurrency, MT_CONFIG.max_concurrency);
const mtSavePrefs = () => store(PREF_STORE + ".modeltrace", { model: $("mt-model").value, concurrency: mtConcurrency() });

function mtComparison(r) {
  if (!r?.attribution || !["completed", "partial"].includes(r.status)) return { tone: "neutral", symbol: "chart", label: "" };
  const modelID = (model) => String(model || "").trim().toLowerCase().split("/").pop();
  return modelID(r.model) === modelID(r.attribution.prediction)
    ? { tone: "ok", symbol: "circle-check", label: "与测试模型一致" }
    : { tone: "warn", symbol: "shuffle", label: "与测试模型不一致" };
}

function mtOutcome(r) {
  const a = r?.attribution;
  const comparison = mtComparison(r);
  const state = { failed: ["bad", "circle-x", "测试失败", "查看详情了解原因"], cancelled: ["neutral", "pause", "测试已停止", ""], skipped: ["neutral", "ban", "已跳过", "此凭证不含所选模型"] }[r?.status];
  const [tone, symbol, title] = state || (a ? [comparison.tone, comparison.symbol, a.prediction] : ["idle", "fingerprint", "等待测试"]);
  const detail = state ? state[3] : a ? `${comparison.label}${r.status === "partial" ? " · 部分结果" : ""}` : "";
  return resultOutcome({
    tone, symbol,
    titleHTML: `<span class="mt-prediction"><span>${esc(title)}</span>${a && !state ? `<span class="mt-probability mono">${mtPercent(a.probability)}</span>` : ""}</span>`,
    detailHTML: esc(detail),
  });
}

function renderModelTraceRow(a) {
  const history = a.modeltraces || [], last = history.at(-1), p = a.modeltrace_running, open = mtExpanded.has(a.id);
  const disabled = pending || !$("mt-model").value || !availableCredential(a);
  const latest = p ? collectionOutcome(p, MT_CONFIG.requests) : mtOutcome(last);
  return `<div class="row ${open ? "open" : ""}"><div class="list-row row-main" data-row="${esc(a.id)}">
    <div class="selection-cell"><input type="checkbox" data-mt-select="${esc(a.id)}" aria-label="选择此凭证" ${mtSelected.has(a.id) ? "checked" : ""} ${disabled ? "disabled" : ""}></div>
    ${credentialView(a, open)}
    <div class="mt-test-model mono">${esc(p?.model || last?.model || "—")}</div>
    <div class="latest">${latest}</div>
    <div class="test-time mono">${!p && last ? esc(fmtTime(last.time)) : "—"}</div>
    <div class="action">${collectionButton("mt", a, p, "测试")}</div></div>
    ${open ? historyPanel([...history].reverse().map((r) => historyEntry("mt", a.id, r, mtOutcome(r))).join("")) : ""}</div>`;
}

function renderModelTrace() {
  renderSelection("mt-", mtSelected, "测试");
  const selected = selectedCredentials("mt-", mtSelected).filter(availableCredential);
  $("mt-cost").textContent = `每个凭证 ${MT_CONFIG.requests} 次请求（失败会重试）${selected.length ? ` · 已选 ${selected.length} 个，共 ${MT_CONFIG.requests * selected.length} 次` : ""}`;
  $("mt-clear").disabled = pending || !credentials.some((a) => a.modeltraces?.length);
  renderList("mt-", renderModelTraceRow);
}

function runModelTrace(body) {
  if (!$("mt-toolbar").reportValidity()) return;
  mtSavePrefs();
  return update("/modeltrace/run", { method: "POST", body: { ...body, model: $("mt-model").value, concurrency: mtConcurrency() } }, "开始测试失败");
}

function mtResultHTML(r) {
  const a = r.attribution, comparison = mtComparison(r);
  return `<dl class="mt-summary ${comparison.tone}"><div><dt>${comparison.label ? `${icon(comparison.symbol)}${comparison.label}` : "最接近的模型"}</dt><dd class="mono">${esc(a.prediction)}<small>${mtPercent(a.probability)}</small></dd></div><div><dt>模型家族</dt><dd>${esc(a.family_prediction_name)}<small class="mono">${mtPercent(a.family_probability)}</small></dd></div><div><dt>有效回答</dt><dd class="mono">${a.used_outputs} / ${MT_CONFIG.requests}</dd></div></dl>
    <div class="mt-families">${(a.family_probabilities || []).map((f) => `<span>${esc(f.display_name)}<b class="mono">${mtPercent(f.probability)}</b></span>`).join("")}</div>
    <div class="mt-result-heading"><h3>候选模型排行</h3><span class="meta">归因概率</span></div>
    <div class="mt-ranking">${(a.results || []).map((r, i) => `<div class="mt-rank"><span class="meta mono">${String(i + 1).padStart(2, "0")}</span><span class="mono mt-rank-name">${esc(r.display_name || r.model)}</span><div class="mt-bar" aria-hidden="true"><span style="width:${Math.max(0, Math.min(100, (r.probability || 0) * 100))}%"></span></div><span class="mono">${mtPercent(r.probability)}</span></div>`).join("")}</div>`;
}

function showModelTraceDetail(r, credentialID) {
  const samples = r.samples || [];
  const sampleEntries = samples.map((s, i) => `<details class="mt-sample">
    <summary>${icon("chevron-right", "chev")}挑战 ${i + 1}<span class="meta">${s.parsed_numbers} / ${s.expected_count} 个数字${s.attempts > 1 ? ` · 重试 ${s.attempts - 1} 次` : ""}</span><span class="verdict ${s.accepted ? "ok" : "err"}">${s.accepted ? "有效" : "未计入"}</span></summary>
    <pre>${esc(s.prompt)}</pre>${s.error ? `<p class="detail-warning">${esc(s.error)}</p>` : ""}${s.text ? `<pre class="mono">${esc(s.text)}</pre>` : ""}
  </details>`).join("");
  $("mt-detail-body").innerHTML = `<p class="result-dialog-meta"><span class="mono">${esc(r.model)}</span> · ${esc(fmtTime(r.time))} · ${fmtSec(r.duration_ms)}${r.concurrency ? ` · 并发 ${esc(r.concurrency)}` : ""}</p>
    ${r.attribution && r.status !== "completed" ? `<p class="detail-note">${esc({ partial: "部分结果", cancelled: "测试已停止", failed: "测试失败", skipped: "已跳过" }[r.status] || r.status)}</p>` : ""}
    ${r.attribution ? mtResultHTML(r) : mtOutcome(r)}
    ${r.error ? `<p class="detail-note detail-warning">${esc(r.error)}</p>` : ""}
    ${samples.length ? `<div class="mt-result-heading"><h3>挑战记录</h3><span class="meta">${samples.reduce((n, s) => n + (s.attempts || 1), 0)} 次请求</span></div>${sampleEntries}` : ""}`;
  openResultDetail("mt", r.id, credentialID);
}

function initializeModelTrace() {
  $("mt-concurrency").max = MT_CONFIG.max_concurrency;
  $("mt-concurrency").value = stored(PREF_STORE + ".modeltrace")?.concurrency || MT_CONFIG.default_concurrency;
  $("mt-concurrency").value = mtConcurrency();
  $("mt-model").addEventListener("change", () => { mtSavePrefs(); renderModelTrace(); });
  $("mt-concurrency").addEventListener("change", () => { $("mt-concurrency").value = mtConcurrency(); mtSavePrefs(); });
  bindCollectionActions({ type: "mt", scope: "modeltrace", historyKey: "modeltraces", expanded: mtExpanded, renderRows: renderModelTrace, run: runModelTrace, showDetail: showModelTraceDetail });
}
