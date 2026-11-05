"use strict";

const siwcSelected = new Set();
const siwcExpanded = new Set();
const SIWC_STATUS = {
  available: ["ok", "SIWC 可用"], restricted: ["bad", "订阅共享受限"],
  limited: ["skip", "额度或频率受限"], auth_error: ["err", "认证异常"],
  failed: ["err", "检测未完成"], skipped: ["skip", "已跳过"],
};
const siwcVerdict = (r) => statusPill(...(SIWC_STATUS[r.status] || SIWC_STATUS.failed));

function renderSIWCRow(a) {
  const records = a.siwc, last = records.at(-1), progress = a.running?.test === "siwc" && a.running;
  return credentialRow({
    test: "siwc", credential: a, selected: siwcSelected.has(a.id), open: siwcExpanded.has(a.id), selectable: runnable("siwc", a),
    result: !a.siwc_bound ? `${statusPill("skip", "需先绑定 SIWC")}<div class="meta">请在 CPA 的 OAuth 登录中绑定 SIWC</div>` : progress ? collectionOutcome(progress) : latestResult(last, siwcVerdict),
    meta: credentialSwitch(a, "use_siwc"), tested: records.length > 0,
    button: collectionButton("siwc", a, progress, "检测"),
    history: () => [...records].reverse().map((r) => historyCard(r, `${siwcVerdict(r)}${r.code ? `<p class="mono">${esc(r.code)}</p>` : ""}${r.error ? `<p class="error">${esc(r.error)}</p>` : ""}`, r.http_status ? `HTTP ${esc(r.http_status)}` : "")).join(""),
  });
}

function renderSIWC() {
  renderSelection("siwc", siwcSelected, "检测");
  $("siwc-clear").disabled = pending || credentials.every((a) => !a.siwc.length);
  renderList("siwc", renderSIWCRow);
}

function runSIWC(body) {
  return startTest("siwc", { ...body, model: $("siwc-model").value });
}

function initializeSIWC() {
  $("siwc-model").addEventListener("change", render);
  bindTestActions({ test: "siwc", expanded: siwcExpanded, renderRows: renderSIWC, run: runSIWC });
}
