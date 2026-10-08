"use strict";

const candyExpanded = new Set();
const candyAnswersExpanded = new Set();
const candySelected = new Set();

const candyPrefs = () => stored(PREF_STORE) || {};
const candySavePrefs = () => store(PREF_STORE, { model: $("model").value, effort: $("effort").value, language: $("language").value, runs: candyRuns() });
const candyRuns = () => boundedInput("runs", 1, 10);

const scopeMatch = (r) => r.model === $("model").value && (r.effort || "none") === $("effort").value && (r.language || "zh") === $("language").value;
const kind = (r) => (r.skipped ? "skip" : r.error ? "err" : r.ok ? "ok" : "bad");
const VERDICT = { ok: "答对", bad: "答错", err: "出错", skip: "已跳过" };
const verdict = (r) => statusPill(kind(r), VERDICT[kind(r)]);
const resultMeta = (r) => `<div class="result-meta">${verdict(r)}${metric("calendar", "测试时间", fmtTime(r.time))}${modelMeta(r)}</div>`;
// Answers come from untrusted upstreams: raw HTML is escaped, links and images stay text, and Temml
// refuses commands that need trust, such as \href. The CJK plugin lets emphasis close after full-width
// punctuation, as in **答案：**21.
const markdown = markdownit({ breaks: true }).disable(["link", "image", "autolink"])
  .use(markdownItCjkFriendly).use(texmath, { engine: temml, delimiters: ["dollars", "brackets"] });
const answerHTML = (r) => r.error ? esc(r.error) : markdown.render(r.answer || "");
const answerKey = (id, r) => JSON.stringify([id, r.time, r.model, r.effort]);
const answerToggle = (open) => `${icon("chevron-right", "chev")}${open ? "收起" : "展开"}`;

function candyHistory(a, r, i) {
  const key = answerKey(a.id, r);
  const answerOpen = candyAnswersExpanded.has(key);
  const bodyID = `answer-${encodeURIComponent(a.id)}-${i}`;
  return historyCard(r, `<div class="answer ${answerOpen ? "expanded" : ""}">${verdict(r)}
    <button class="answer-toggle" type="button" data-answer="${esc(key)}" aria-expanded="${answerOpen}" aria-controls="${esc(bodyID)}" aria-label="${answerOpen ? "收起文本" : "展开文本"}">${answerToggle(answerOpen)}</button>
    <div id="${esc(bodyID)}" class="answer-content ${r.error ? "error" : "markdown"}">${answerHTML(r)}</div>
  </div>`);
}

function renderCandyRow(a) {
  const results = a.results;
  const last = results[results.length - 1];
  const graded = results.filter((r) => scopeMatch(r) && !r.error && !r.skipped);
  const correct = graded.filter((r) => r.ok).length;

  const rate = graded.length
    ? `<b class="mono">${Math.round((correct / graded.length) * 100)}%</b><small class="mono">${correct}/${graded.length}</small>`
    : `<span class="none">—</span>`;
  const marks = results.map((r, i) => `<button class="mark-hit" type="button" data-auth="${esc(a.id)}" data-i="${i}" aria-label="${esc(`${fmtTime(r.time)} ${modelName(r)} ${VERDICT[kind(r)]}`)}">${icon(STATUS_ICONS[kind(r)], `mark ${kind(r)} ${scopeMatch(r) ? "" : "dim"}`)}</button>`).join("");
  const progress = a.running && `${a.running.done}/${a.running.total}`;
  const blocker = runBlocker("", a);
  const button = progress
    ? `<button class="btn ghost" type="button" aria-label="测试中 ${progress}" data-tip disabled>${icon("loader-circle", "spin")}<span class="mono">${progress}</span></button>`
    : `<button class="btn" type="button" data-candy-run="${esc(a.id)}" ${disabledFor(blocker)}>${icon("play")}测试</button>`;

  return credentialRow({
    type: "candy", credential: a, selected: candySelected.has(a.id), open: candyExpanded.has(a.id), selectable: !blocker, button,
    result: latestResult(last, verdict), meta: `<div class="rate">${rate}</div><div class="marks">${marks}</div>`, tested: results.length > 0,
    history: () => [...results].reverse().map((r, i) => candyHistory(a, r, i)).join(""),
  });
}

function runCandy(body) {
  if (!$("toolbar").reportValidity()) return;
  candySavePrefs();
  return update("/run", { method: "POST", body: { ...body, model: $("model").value, effort: $("effort").value, language: $("language").value, runs: candyRuns() } }, "开始测试失败");
}

function candyTip(target) {
  const r = credentials.find((a) => a.id === target.dataset.auth)?.results[target.dataset.i];
  return r ? `${resultMeta(r)}
    <div class="metrics">${r.skipped ? "" : metrics(r)}</div>
    <div class="tip-text ${r.error ? "error" : "markdown"}">${answerHTML(r)}</div>` : "";
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
  $("language").value = saved.language || "zh";
  $("candy-prompt-en").previousElementSibling.hidden = $("language").value === "en";
  $("candy-prompt-en").hidden = $("language").value !== "en";
  $("runs").value = saved.runs || 1;
  $("runs").value = candyRuns();

  $("model").addEventListener("change", () => { candySavePrefs(); render(); });
  $("effort").addEventListener("change", () => { candySavePrefs(); render(); });
  $("language").addEventListener("change", () => { candySavePrefs(); $("candy-prompt-en").previousElementSibling.hidden = $("language").value === "en"; $("candy-prompt-en").hidden = $("language").value !== "en"; render(); });
  $("runs").addEventListener("change", () => { $("runs").value = candyRuns(); candySavePrefs(); });
  $("rows").addEventListener("click", (e) => {
    if (e.target.closest("label")) return;
    const hit = e.target.closest("[data-auth]");
    if (hit) return showTip(hit);
    const toggle = e.target.closest("[data-answer]");
    if (toggle) {
      const key = toggle.dataset.answer;
      const open = !candyAnswersExpanded.has(key);
      open ? candyAnswersExpanded.add(key) : candyAnswersExpanded.delete(key);
      toggle.closest(".answer").classList.toggle("expanded", open);
      toggle.setAttribute("aria-expanded", open);
      toggle.setAttribute("aria-label", open ? "收起文本" : "展开文本");
      toggle.innerHTML = answerToggle(open);
      return;
    }
    const btn = e.target.closest("[data-candy-run]");
    if (btn) return runCandy({ auth_ids: [btn.dataset.candyRun] });
    const row = e.target.closest("[data-row]");
    if (!row) return;
    const id = row.dataset.row;
    candyExpanded.has(id) ? candyExpanded.delete(id) : candyExpanded.add(id);
    render();
  });
}
