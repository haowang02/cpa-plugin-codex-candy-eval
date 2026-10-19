"use strict";
const PELICAN_VARIANTS = /*PELICAN_VARIANTS*/[];
const pelicanVariant = (id) => PELICAN_VARIANTS.find((v) => v.id === id);
const pelicanSelected = new Set();
const pelicanExpanded = new Set();

const PELICAN_STATUS = { completed: ["ok", "已生成"], failed: ["err", "未生成"], skipped: ["skip", "已跳过"] };
const pelicanKind = (r) => (PELICAN_STATUS[r.status] || PELICAN_STATUS.failed)[0];
const pelicanVerdict = (r) => statusPill(pelicanKind(r), (PELICAN_STATUS[r.status] || PELICAN_STATUS.failed)[1]);
const pelicanVariantMeta = (r) => metric("layers", "版本", pelicanVariant(r.variant)?.label || r.variant);

// Lists carry summaries, so each page loads once per record: undefined while loading, an Error when it failed.
const pelicanPages = new Map();
const pelicanKey = (credentialID, recordID) => JSON.stringify([credentialID, recordID]);
function pelicanPage(credentialID, recordID) {
  const key = pelicanKey(credentialID, recordID);
  if (!pelicanPages.has(key)) {
    pelicanPages.set(key, undefined);
    const settle = (page) => {
      if (!pelicanPages.has(key)) return;
      pelicanPages.set(key, page);
      renderPelican();
    };
    api(`${BASE}/pelican/record?${new URLSearchParams({ credential_id: credentialID, id: recordID })}`)
      .then((r) => settle(r.html), (err) => err instanceof AuthError ? showLogin(err.message) : settle(err));
  }
  return pelicanPages.get(key);
}

// Model pages run sandboxed: their scripts work, but this page's origin and storage stay out of reach, and
// the CSP they inherit from it blocks external assets.
const pelicanFrame = (html) => `<iframe sandbox="allow-scripts" srcdoc="${esc(html)}" title="鹈鹕画面" loading="lazy" tabindex="-1"></iframe>`;
// Previews show a page as it looks a second after it is first drawn, then hold its animations and stop its
// timers, so a long list costs no CPU; only the dialog plays the page. Frames in a hidden tab can load without
// being drawn, and Chrome restarts CSS animations whenever a frame is drawn again, hence the wait for a frame
// and the hold on every animation start.
const PREVIEW_FREEZE = `<script>addEventListener("load", () => requestAnimationFrame(() => setTimeout(() => {
  const hold = () => document.getAnimations().forEach((animation) => { animation.pause(); animation.currentTime = 1000; });
  hold();
  addEventListener("animationstart", hold);
  document.querySelectorAll("svg").forEach((svg) => svg.pauseAnimations());
  for (let id = setTimeout(() => {}); id > 0; id--) { clearTimeout(id); clearInterval(id); }
  window.requestAnimationFrame = window.setTimeout = window.setInterval = () => 0;
}, 1000)));<\/script>`;
function pelicanPreview(credentialID, r) {
  if (r.error) return `<div class="preview" data-tip="${esc(r.error)}">${icon(STATUS_ICONS[pelicanKind(r)])}</div>`;
  const page = pelicanPage(credentialID, r.id);
  if (page instanceof Error) return `<div class="preview" data-tip="${esc("画面加载失败：" + page.message)}">${icon("circle-alert")}</div>`;
  return `<div class="preview">${page === undefined ? icon("loader-circle", "spin") : pelicanFrame(page + PREVIEW_FREEZE)}
    <button class="preview-open" type="button" data-pelican-detail="${esc(r.id)}" data-pelican-credential="${esc(credentialID)}" aria-label="${esc(`放大查看 ${fmtTime(r.time)} 的画面`)}"></button></div>`;
}

function pelicanHistory(a, r) {
  return historyCard(r, r.error ? `${pelicanVerdict(r)}<span class="${r.status === "skipped" ? "meta" : "detail-warning"}">${esc(r.error)}</span>` : pelicanPreview(a.id, r), pelicanVariantMeta(r));
}

function renderPelicanRow(a) {
  const records = a.pelicans, last = records.at(-1);
  const button = a.running?.test === "pelican"
    ? `<button class="btn ghost" type="button" data-tip="正在等待模型作画" disabled>${icon("loader-circle", "spin")}测试中</button>`
    : collectionButton("pelican", a, null, "测试");
  return credentialRow({
    test: "pelican", credential: a, selected: pelicanSelected.has(a.id), open: pelicanExpanded.has(a.id), selectable: runnable("pelican", a), button,
    result: latestResult(last, pelicanVerdict), tested: !!last, meta: last ? pelicanPreview(a.id, last) : `<span class="none">—</span>`,
    history: () => [...records].reverse().map((r) => pelicanHistory(a, r)).join(""),
  });
}

function renderPelican() {
  renderSelection("pelican", pelicanSelected, "测试");
  $("pelican-clear").disabled = pending || !credentials.some((a) => a.pelicans.length);
  renderList("pelican", renderPelicanRow);
}

function runPelican(body) {
  if (!$("pelican-toolbar").reportValidity()) return;
  return startTest("pelican", { ...body, model: $("pelican-model").value, effort: $("pelican-effort").value, variant: $("pelican-variant").value });
}

function showPelicanDetail(r) {
  $("pelican-detail-body").innerHTML = `<div class="result-dialog-meta record-meta">${recordMeta(r, pelicanVariantMeta(r))}</div>
    <div class="preview pelican-stage">${pelicanFrame(r.html)}</div>`;
  fitPelicanStage();
}
function fitPelicanStage() {
  const stage = $("pelican-detail-body").querySelector(".pelican-stage");
  stage?.style.setProperty("--preview-scale", stage.clientWidth / 800);
}

function initializePelican() {
  fillSelect("pelican-effort", DEFAULT_EFFORTS, preference("pelican-effort", DEFAULT_EFFORT), DEFAULT_EFFORT);
  fillSelect("pelican-variant", PELICAN_VARIANTS.map((v) => [v.id, v.label]), preference("pelican-variant", ""));
  const showPrompt = () => { $("pelican-prompt").textContent = pelicanVariant($("pelican-variant").value).prompt; };
  showPrompt();
  $("pelican-variant").addEventListener("change", showPrompt);
  // Closing the dialog also stops its animation.
  $("pelican-detail").addEventListener("close", () => { $("pelican-detail-body").innerHTML = ""; });
  addEventListener("resize", fitPelicanStage);
  bindCollectionActions({ test: "pelican", expanded: pelicanExpanded, renderRows: renderPelican, run: runPelican, showDetail: showPelicanDetail });
}
