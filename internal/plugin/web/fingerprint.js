"use strict";

const FP_CONFIG = /*FINGERPRINT_CONFIG*/{};
const fpModes = Object.fromEntries(FP_CONFIG.modes.map((m) => [m.id, { name: m.name, requests: m.cells * m.samples_per_cell }]));
const fpSelected = new Set();
const fpExpanded = new Set();

const fpModeName = (mode) => fpModes[mode]?.name || mode || "—";
const fpConcurrency = () => boundedInput("fp-concurrency", FP_CONFIG.default_concurrency, FP_CONFIG.max_concurrency);
const fpSavePrefs = () => store(PREF_STORE + ".fingerprint", { model: $("fp-model").value, mode: $("fp-mode").value, concurrency: fpConcurrency() });

function fpOutcome(r) {
  const status = r.status && r.status !== "completed" ? r.status : r.attribution?.status;
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
    ${r.status === "cancelled" || r.status === "failed" ? `<p class="detail-note">已有样本仅供参考</p>` : ""}
    ${attr.message ? `<p class="detail-note">${esc(attr.message)}</p>` : ""}
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
  return `<div class="row ${open ? "open" : ""}"><div class="list-row row-main" data-row="${esc(a.id)}">
    <input type="checkbox" data-fp-select="${esc(a.id)}" aria-label="选择此凭证" ${fpSelected.has(a.id) ? "checked" : ""} ${pending || !$("fp-model").value || !availableCredential(a) ? "disabled" : ""}>
    ${credentialView(a, open, "data-fp-toggle")}
    <div class="latest">${latest}</div>
    <div class="fp-mode">${esc(fpModeName(p?.mode || last?.mode))}</div>
    <div class="test-time mono" ${last && !p ? `title="${esc(fmtTime(last.time))}"` : ""}>${last && !p ? esc(fmtTime(last.time)) : "—"}</div>
    <div class="action">${collectionButton("fp", a, p, "采集")}</div></div>
    ${open ? historyPanel([...history].reverse().map((r) => historyEntry("fp", a.id, r, fpOutcome(r), metric("fingerprint", "采集模式", fpModeName(r.mode)))).join("")) : ""}</div>`;
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

function initializeFingerprint() {
  const saved = stored(PREF_STORE + ".fingerprint") || {};
  fillSelect("fp-mode", Object.entries(fpModes).map(([id, mode]) => [id, `${mode.name} · ${mode.requests} 次`]), saved.mode, "quick");
  $("fp-concurrency").max = FP_CONFIG.max_concurrency;
  $("fp-concurrency").value = saved.concurrency || FP_CONFIG.default_concurrency;
  $("fp-concurrency").value = fpConcurrency();

  $("fp-detail-body").addEventListener("change", (e) => { if (e.target.id === "fp-detail-baseline") renderFingerprintProbes(); });
  $("fp-detail").addEventListener("close", () => { fpDetailRecord = null; });

  for (const id of ["fp-model", "fp-mode"]) $(id).addEventListener("change", () => { fpSavePrefs(); renderFingerprints(); });
  $("fp-concurrency").addEventListener("change", () => { $("fp-concurrency").value = fpConcurrency(); fpSavePrefs(); });
  bindCollectionActions({ type: "fp", scope: "fingerprint", historyKey: "fingerprints", expanded: fpExpanded, renderRows: renderFingerprints, run: runFingerprint, showDetail: showFingerprintDetail });
}
