"use strict";

const DEFAULT_EFFORT = "low";
const DEFAULT_EFFORTS = ["none", "low", "medium", "high", "xhigh", "max"];

let tipTarget = null;
let copyTimer = 0;
const COPY_LABEL = icon("copy");
const candyExpanded = new Set();
const candyAnswersExpanded = new Set();
const candySelected = new Set();

const candyPrefs = () => stored(PREF_STORE) || {};
const candySavePrefs = () => store(PREF_STORE, { model: $("model").value, effort: $("effort").value, runs: candyRuns() });
const candyRuns = () => boundedInput("runs", 1, 10);

const scopeMatch = (r) => r.model === $("model").value && (r.effort || "none") === $("effort").value;
const kind = (r) => (r.skipped ? "skip" : r.error ? "err" : r.ok ? "ok" : "bad");
const VERDICT = { ok: "答对", bad: "答错", err: "出错", skip: "已跳过" };
const MARK = { ok: "circle-check", bad: "circle-x", err: "circle-alert", skip: "ban" };
const verdict = (r) => `<span class="verdict ${kind(r)}">${icon(MARK[kind(r)])}${VERDICT[kind(r)]}</span>`;
const resultMeta = (r) => `<div class="result-meta">${verdict(r)}${metric("calendar", "测试时间", fmtTime(r.time))}${modelMeta(r)}</div>`;
const oneLine = (s) => String(s || "").replace(/\s+/g, " ").trim();
const answerKey = (id, r) => JSON.stringify([id, r.time, r.model, r.effort]);
const answerToggle = (open) => `${icon("chevron-right", "chev")}${open ? "收起" : "展开"}`;

function candyHistory(a, r, i) {
  const key = answerKey(a.id, r);
  const answerOpen = candyAnswersExpanded.has(key);
  const bodyID = `answer-${encodeURIComponent(a.id)}-${i}`;
  return historyCard(r, `${verdict(r)}
    <div class="answer-preview ${answerOpen ? "expanded" : ""}">
      <button class="answer-toggle" type="button" data-answer="${esc(key)}" aria-expanded="${answerOpen}" aria-controls="${esc(bodyID)}" aria-label="${answerOpen ? "收起文本" : "展开文本"}">${answerToggle(answerOpen)}</button>
      <pre id="${esc(bodyID)}" class="answer-content ${r.error ? "error" : ""}">${esc(r.error || r.answer)}</pre>
    </div>`);
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
       <div class="metrics">${last.skipped ? "" : metrics(last)}</div>`
    : `<span class="none">尚未测试</span>`;
  const rate = graded.length
    ? `<b class="mono">${Math.round((correct / graded.length) * 100)}%</b><small class="mono">${correct}/${graded.length}</small>`
    : `<span class="none">—</span>`;
  const marks = results.map((r, i) => `<button class="mark-hit" type="button" data-auth="${esc(a.id)}" data-i="${i}" aria-label="${esc(`${fmtTime(r.time)} ${modelName(r)} ${VERDICT[kind(r)]}`)}">${icon(MARK[kind(r)], `mark ${kind(r)} ${scopeMatch(r) ? "" : "dim"}`)}</button>`).join("");
  const action = a.running
    ? `<button class="btn ghost" type="button" disabled>${icon("loader-circle", "spin")}测试中 <span class="mono">${a.running.done}/${a.running.total}</span></button>`
    : `<button class="btn" type="button" data-run="${esc(a.id)}" ${pending || !$("model").value || !availableCredential(a) ? "disabled" : ""}>${icon("play")}测试</button>`;

  return `<div class="row ${open ? "open" : ""}">
    <div class="list-row row-main" data-row="${esc(a.id)}">
      <div class="selection-cell"><input type="checkbox" data-candy-select="${esc(a.id)}" aria-label="选择此凭证" ${candySelected.has(a.id) ? "checked" : ""} ${pending || !$("model").value || !availableCredential(a) ? "disabled" : ""}></div>
      ${credentialView(a, open)}
      <div class="latest">${latest}</div>
      <div class="row-summary ${results.length ? "" : "empty-history"}"><div class="rate">${rate}</div><div class="marks">${marks}</div></div>
      <div class="action">${action}<button class="btn ghost ${a.disabled ? "" : "danger"}" type="button" data-status="${esc(a.id)}" ${pending || !canToggleCredential(a) ? "disabled" : ""} ${a.source !== "auth_files" ? 'title="请在 AI 提供商设置中启用或停用此凭证"' : ""}>${a.disabled ? "启用账户" : "停用账户"}</button></div>
    </div>
    ${open ? historyPanel([...results].reverse().map((r, i) => candyHistory(a, r, i)).join("")) : ""}
  </div>`;
}

function runCandy(body) {
  if (!$("toolbar").reportValidity()) return;
  candySavePrefs();
  return update("/run", { method: "POST", body: { ...body, model: $("model").value, effort: $("effort").value, runs: candyRuns() } }, "开始测试失败");
}

function showTip(target) {
  const r = credentials.find((a) => a.id === target.dataset.auth)?.results[target.dataset.i];
  if (!r) return;
  hideTip();
  const tip = $("tip");
  tip.innerHTML = `${resultMeta(r)}
    <div class="metrics">${r.skipped ? "" : metrics(r)}</div>
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

function renderCandy() {
  $("rate-scope").textContent = `正确率按 ${modelName({ model: $("model").value, effort: $("effort").value })} 统计`;
  renderSelection("", candySelected, "测试");
  $("clear").disabled = pending || credentials.every((a) => !a.results.length);
  renderList("", renderCandyRow);
}

function initializeCandy() {
  const saved = candyPrefs();
  fillSelect("effort", DEFAULT_EFFORTS, saved.effort, DEFAULT_EFFORT);
  $("effort").title = "none：不发送推理参数，由 CPA 处理；其他强度由 CPA 或上游验证";
  $("runs").value = saved.runs || 1;
  $("runs").value = candyRuns();

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

  $("model").addEventListener("change", () => { candySavePrefs(); render(); });
  $("effort").addEventListener("change", () => { candySavePrefs(); render(); });
  $("runs").addEventListener("change", () => { $("runs").value = candyRuns(); candySavePrefs(); });
  $("rows").addEventListener("click", (e) => {
    if (e.target.closest(".selection-cell")) return;
    const hit = e.target.closest("[data-auth]");
    if (hit) return showTip(hit);
    const answer = e.target.closest("[data-answer]");
    if (answer) {
      const key = answer.dataset.answer;
      const open = !candyAnswersExpanded.has(key);
      open ? candyAnswersExpanded.add(key) : candyAnswersExpanded.delete(key);
      answer.closest(".answer-preview").classList.toggle("expanded", open);
      answer.setAttribute("aria-expanded", open);
      answer.setAttribute("aria-label", open ? "收起文本" : "展开文本");
      answer.innerHTML = answerToggle(open);
      return;
    }
    const status = e.target.closest("[data-status]");
    if (status) return toggleCredentialStatus(status.dataset.status);
    const btn = e.target.closest("[data-run]");
    if (btn) return runCandy({ auth_ids: [btn.dataset.run] });
    const row = e.target.closest("[data-row]");
    if (!row) return;
    const id = row.dataset.row;
    candyExpanded.has(id) ? candyExpanded.delete(id) : candyExpanded.add(id);
    render();
  });
}
