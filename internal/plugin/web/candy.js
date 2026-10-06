"use strict";

const DEFAULT_EFFORT = "low";
const DEFAULT_EFFORTS = ["none", "low", "medium", "high", "xhigh", "max"];

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
  return historyCard(r, `${verdict(r)}
    <div class="answer-preview ${answerOpen ? "expanded" : ""}">
      <button class="answer-toggle" type="button" data-answer="${esc(key)}" aria-expanded="${answerOpen}" aria-controls="${esc(bodyID)}" aria-label="${answerOpen ? "收起文本" : "展开文本"}">${answerToggle(answerOpen)}</button>
      <div id="${esc(bodyID)}" class="answer-content ${r.error ? "error" : "markdown"}">${answerHTML(r)}</div>
    </div>`);
}

function renderCandyRow(a) {
  const results = a.results;
  const last = results[results.length - 1];
  const graded = results.filter((r) => scopeMatch(r) && !r.error && !r.skipped);
  const correct = graded.filter((r) => r.ok).length;

  const latest = last
    ? `<div class="latest-top">${verdict(last)}
         ${modelMeta(last)}</div>
       <div class="metrics">${last.skipped ? "" : metrics(last)}</div>`
    : `<span class="none">尚未测试</span>`;
  const rate = graded.length
    ? `<b class="mono">${Math.round((correct / graded.length) * 100)}%</b><small class="mono">${correct}/${graded.length}</small>`
    : `<span class="none">—</span>`;
  const marks = results.map((r, i) => `<button class="mark-hit" type="button" data-auth="${esc(a.id)}" data-i="${i}" aria-label="${esc(`${fmtTime(r.time)} ${modelName(r)} ${VERDICT[kind(r)]}`)}">${icon(MARK[kind(r)], `mark ${kind(r)} ${scopeMatch(r) ? "" : "dim"}`)}</button>`).join("");
  const progress = a.running && `${a.running.done}/${a.running.total}`;
  const blocker = runBlocker("", a);
  const button = progress
    ? `<button class="btn ghost" type="button" aria-label="测试中 ${progress}" data-tip disabled>${icon("loader-circle", "spin")}<span class="mono">${progress}</span></button>`
    : `<button class="btn" type="button" data-candy-run="${esc(a.id)}" ${disabledFor(blocker)}>${icon("play")}测试</button>`;

  return credentialRow({
    type: "candy", credential: a, selected: candySelected.has(a.id), open: candyExpanded.has(a.id), selectable: !blocker, button,
    result: latest, meta: `<div class="rate">${rate}</div><div class="marks">${marks}</div>`, tested: results.length > 0,
    history: () => [...results].reverse().map((r, i) => candyHistory(a, r, i)).join(""),
  });
}

function runCandy(body) {
  if (!$("toolbar").reportValidity()) return;
  candySavePrefs();
  return update("/run", { method: "POST", body: { ...body, model: $("model").value, effort: $("effort").value, runs: candyRuns() } }, "开始测试失败");
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
      setLabel($("copy"), "复制失败，请手动复制题目");
      return;
    }
    clearTimeout(copyTimer);
    $("copy").innerHTML = icon("check");
    $("copy").classList.add("copied");
    setLabel($("copy"), "已复制");
    copyTimer = setTimeout(() => {
      $("copy").innerHTML = COPY_LABEL;
      $("copy").classList.remove("copied");
      setLabel($("copy"), "复制题目");
    }, 1600);
  });

  $("model").addEventListener("change", () => { candySavePrefs(); render(); });
  $("effort").addEventListener("change", () => { candySavePrefs(); render(); });
  $("runs").addEventListener("change", () => { $("runs").value = candyRuns(); candySavePrefs(); });
  $("rows").addEventListener("click", (e) => {
    if (e.target.closest("label")) return;
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
    const btn = e.target.closest("[data-candy-run]");
    if (btn) return runCandy({ auth_ids: [btn.dataset.candyRun] });
    const row = e.target.closest("[data-row]");
    if (!row) return;
    const id = row.dataset.row;
    candyExpanded.has(id) ? candyExpanded.delete(id) : candyExpanded.add(id);
    render();
  });
}
