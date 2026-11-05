"use strict";

const credentialChanges = new Map();
const credentialFilter = {
  enabledOnly: preference("credential-filter.enabledOnly", false),
  type: preference("credential-filter.type", "auth_files:codex"),
  plan: preference("credential-filter.plan", "all"),
  siwcPlan: preference("credential-filter.siwcPlan", "all"),
};

const busyTest = (a) => TEST_LABELS[a.running?.test] || "";
const credentialBusy = (a) => !!a.running;
const usableCredential = (a) => !a.disabled && !a.unavailable;
function credentialBlocker(a, test) {
  if (a.disabled) return "凭证已停用，启用后才能测试";
  if (a.unavailable) return "凭证暂不可用：" + unavailableReason(a);
  if (credentialBusy(a)) return `${busyTest(a)}进行中，结束后才能开始新测试`;
  if (credentialChanges.has(a.id)) return "正在保存凭证设置…";
  if (test === "siwc" && !a.siwc_bound) return "请先在 CPA 的 OAuth 登录中绑定 SIWC";
  if (test === "siwc" && !a.use_siwc) return "请先开启「使用 SIWC」";
  return "";
}
const availableCredential = (a, test) => !credentialBlocker(a, test);
const pageBlocker = (test) => !$(test + "-model").value ? "模型列表为空，请刷新后重试" : pending ? "正在处理上一个操作，请稍候" : "";
const runBlocker = (test, a) => credentialBlocker(a, test) || pageBlocker(test);
const runnable = (test, a) => !runBlocker(test, a);
const selectedCredentials = (selection, test) => visibleCredentials(test).filter((a) => selection.has(a.id));
const batchCredentials = (selection, test) => {
  const selected = selectedCredentials(selection, test);
  return (selected.length ? selected : visibleCredentials(test)).filter((a) => availableCredential(a, test));
};
function credentialSwitchBlocker(a, field) {
  if (field === "use_siwc" && !a.siwc_bound) return "请先在 CPA 的 OAuth 登录中绑定 SIWC";
  if (field === "disabled" && a.source !== "auth_files" && !a.id.startsWith(a.provider + ":apikey:")) return "请在 CPA 中启用或停用此凭证";
  if (credentialBusy(a)) return `${busyTest(a)}进行中，结束后才能切换`;
  if (credentialChanges.has(a.id) || pending) return "正在保存，请稍候";
  return "";
}

function credentialSwitch(a, field) {
  const change = credentialChanges.get(a.id);
  const value = change?.field === field ? change.value : a[field];
  const siwc = field === "use_siwc";
  return `<label class="switch ${siwc ? "siwc-switch" : ""}"><input type="checkbox" role="switch" data-credential="${esc(a.id)}" data-credential-field="${field}" aria-label="${siwc ? "使用 SIWC" : "启用此凭证"}" aria-busy="${!!change}" ${(siwc ? value : !value) ? "checked" : ""} ${disabledFor(credentialSwitchBlocker(a, field))}></label>`;
}

async function setCredentialField(id, field, value) {
  const a = credentials.find((entry) => entry.id === id);
  if (!a || !["disabled", "use_siwc"].includes(field) || credentialSwitchBlocker(a, field)) return;
  credentialChanges.set(id, { field, value });
  stopPolling();
  render();
  try {
    const path = field === "disabled" ? "status" : "fields";
    await api("/v0/management/auth-files/" + path, { method: "PATCH", body: { name: id, [field]: value } });
    const current = credentials.find((entry) => entry.id === id);
    if (current) current[field] = value;
    delete catalogCache.credentials[id];
  } catch (err) {
    if (err instanceof AuthError) return showLogin(err.message);
    setNotice("flash", "保存凭证设置失败：" + err.message);
  } finally {
    credentialChanges.delete(id);
    render();
  }
  // Config credentials must be resynced after changing their enabled state.
  if (!$("app").hidden) await load({ refresh: a.source !== "auth_files" });
}

function renderSelection(test, selection, action) {
  const available = visibleCredentials(test).filter((a) => availableCredential(a, test));
  const selected = selectedCredentials(selection, test);
  const targets = batchCredentials(selection, test);
  const button = $(test + "-run-batch");
  button.innerHTML = `${icon("play")}${action}${selected.length ? "所选" : "全部"} (${targets.length})`;
  const blocker = pageBlocker(test) || (targets.length ? "" : selected.length ? "所选凭证均已停用、不可用或正在测试" : "当前列表没有可测试的凭证");
  button.disabled = !!blocker;
  button.dataset.tip = blocker;
  const checkbox = $(test + "-select-all");
  checkbox.checked = available.length > 0 && available.every((a) => selection.has(a.id));
  checkbox.indeterminate = selected.length > 0 && !checkbox.checked;
  checkbox.disabled = pending || (!available.length && !selected.length);
}

// CPA names each OpenAI-compatible provider after its config entry; they share one type.
const providerType = (a) => a.provider === "openai-compatibility" || a.provider.startsWith("openai-compatible-") ? "openai-compatibility" : a.provider;
const credentialType = (a) => a.source + ":" + providerType(a);
function credentialTypeLabel(type) {
  const [source, provider] = type.split(/:(.*)/);
  return `${source === "ai_providers" ? "AI 提供商" : "认证文件"} · ${provider}`;
}
const ofType = (a, type) => type === "all" || credentialType(a) === type;
const PRO_ALL = "Pro ALL";
// Plans of one credential type, best first, grouping Pro plans when there are several.
function planChoices(type) {
  const plans = [...new Map(credentials.filter((a) => ofType(a, type)).map(credentialPlan).filter(Boolean).map((plan) => [plan.label, plan])).values()]
    .sort((a, b) => a.rank - b.rank || a.label.localeCompare(b.label));
  return [...(plans.filter((plan) => plan.pro).length > 1 ? [PRO_ALL] : []), ...plans.map((plan) => plan.label)];
}
function credentialScope(test) {
  return test === "siwc"
    ? { ...credentialFilter, type: "auth_files:codex", plan: credentialFilter.siwcPlan }
    : credentialFilter;
}
function visibleCredentials(test) {
  const { enabledOnly, type, plan } = credentialScope(test);
  const matches = (p) => plan === "all" || (plan === PRO_ALL ? p?.pro : p?.label === plan);
  return credentials.filter((a) => ofType(a, type) && (!enabledOnly || !a.disabled) && matches(credentialPlan(a))).sort((a, b) => planRank(a) - planRank(b));
}
function renderCredentialFilters() {
  for (const test of TESTS) {
    const { enabledOnly, type, plan } = credentialScope(test);
    const types = [...new Set([type, ...credentials.map(credentialType)])].filter((t) => t !== "all").sort();
    const plans = [...new Set([...planChoices(type), plan])].filter((p) => p !== "all");
    $(test + "-enabled-only").checked = enabledOnly;
    fillSelect(test + "-credential-type", [["all", "全部凭证"], ...types.map((t) => [t, credentialTypeLabel(t)])], type);
    $(test + "-type-filter").hidden = test === "siwc";
    fillSelect(test + "-credential-plan", [["all", "所有订阅类型"], ...plans], plan);
    $(test + "-plan-filter").hidden = !plans.length;
  }
}
function setCredentialFilter(change) {
  for (const [name, value] of Object.entries(change)) {
    credentialFilter[name] = value;
    savePreference("credential-filter." + name, value);
  }
  for (const selection of [candySelected, fingerprintSelected, pelicanSelected, siwcSelected]) selection.clear();
  render();
}
