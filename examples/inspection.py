#!/usr/bin/env python3
"""糖果巡检：确认账号恢复或降智后调整 priority，并通过 ntfy 或 Bark 通知。"""
import base64
import fnmatch
import json
import time
import urllib.error
import urllib.parse
import urllib.request

CPA_URL = "https://cpa.example.com"  # CPA 地址
MANAGEMENT_KEY = ""                  # CPA 管理密钥
MODEL = "gpt-6.1-sol"                # 糖果测试模型
EFFORT = "low"                      # 推理强度：none、low、medium、high、xhigh 或 max
INCLUDE = ["codex-*"]                # 邮箱或名称匹配的凭证；留空则不限
EXCLUDE = []                         # 排除邮箱或名称匹配的凭证
SKIP_FREE = True                     # 跳过已识别的 Free 订阅；订阅未知时仍测试
ADJUST_PRIORITY = False              # 自动调整 priority，并为成功调整的账号通知
DEGRADED_PRIORITY = 0                # 降智账号的 priority
NORMAL_PRIORITY = 99                 # 正常账号的 priority
RECOVERY_DELAY_SECONDS = 240         # 降智账号首测答对后，复测前等待的秒数
NTFY_SERVER = "https://ntfy.sh"      # ntfy 服务器地址
NTFY_TOPIC = ""                      # ntfy 主题；留空不通知
NTFY_USERNAME = ""                   # ntfy 用户名；使用访问令牌时留空
NTFY_PASSWORD = ""                   # ntfy 密码或访问令牌
BARK_URL = ""                        # Bark 推送地址；留空不通知
MAX_WAIT_MINUTES = 30                # 每轮测试完成的等待上限（分钟）

PLUGIN = "/plugins/cpa-codex-candy-eval"


def api(method, path, body=None):
    request = urllib.request.Request(
        CPA_URL.rstrip("/") + "/v0/management" + path,
        method=method,
        data=None if body is None else json.dumps(body).encode(),
        headers={"Authorization": "Bearer " + MANAGEMENT_KEY, "Content-Type": "application/json"},
    )
    try:
        with urllib.request.urlopen(request, timeout=60) as response:
            return json.load(response)
    except urllib.error.HTTPError as error:
        raise SystemExit(f"{method} {path} 失败（HTTP {error.code}）：{error.read().decode(errors='replace')}")
    except urllib.error.URLError as error:
        raise SystemExit(f"{method} {path} 失败：{error.reason}")


def notify(title, message):
    if NTFY_SERVER and NTFY_TOPIC:
        url = f"{NTFY_SERVER.rstrip('/')}/{urllib.parse.quote(NTFY_TOPIC)}?" + urllib.parse.urlencode({"title": title})
        request = urllib.request.Request(url, data=message.encode())
        if NTFY_PASSWORD:
            credentials = base64.b64encode(f"{NTFY_USERNAME}:{NTFY_PASSWORD}".encode()).decode()
            request.add_header("Authorization", "Basic " + credentials)
        with urllib.request.urlopen(request, timeout=30):
            pass
    if BARK_URL:
        data = urllib.parse.urlencode({"title": title, "body": message}).encode()
        with urllib.request.urlopen(BARK_URL, data=data, timeout=30):
            pass


def state():
    return {c["id"]: c for c in api("GET", PLUGIN + "/state")["auths"]}


def is_free(c):
    return SKIP_FREE and str(c.get("plan_type", "")).strip().lower() == "free"


def available(c):
    return c is not None and not c.get("disabled") and not c.get("unavailable") and not any(
        c.get(key) for key in ("running", "fingerprint_running", "modeltrace_running", "pelican_running")
    )


def matches(c, patterns):
    return any(fnmatch.fnmatchcase(c.get(key, ""), pattern)
               for key in ("name", "email") for pattern in patterns)


def run_candy(accounts, runs):
    """按凭证 ID 测试，只返回本轮完整、有效的答题结果。"""
    before = state()
    eligible = [c for c in accounts if available(before.get(c["id"])) and not is_free(before[c["id"]])]
    answers = {c["id"]: None for c in accounts}
    if not eligible:
        return answers
    known = {c["id"]: {r["time"] for r in before[c["id"]].get("results", [])} for c in eligible}
    options = {"model": MODEL.strip(), "effort": EFFORT.strip(), "runs": runs}
    run = api("POST", PLUGIN + "/run", {**options, "auth_ids": [c["id"] for c in eligible]})
    phase = "首测" if runs == 1 else "复测"
    print(f"  {phase}：{run['started']} 个账号，每个账号 {runs} 次")

    deadline = time.monotonic() + MAX_WAIT_MINUTES * 60
    while True:
        time.sleep(15)
        after = state()
        if not any(after.get(c["id"], {}).get("running") for c in eligible):
            break
        if time.monotonic() > deadline:
            raise SystemExit(f"等待{phase}完成超时")

    for c in eligible:
        results = [r for r in after.get(c["id"], {}).get("results", [])
                   if r["time"] not in known[c["id"]]
                   and r.get("model") == options["model"] and r.get("effort") == options["effort"]]
        if len(results) == runs and all(not r.get("skipped") and not r.get("error")
                                       and isinstance(r.get("ok"), bool) for r in results):
            answers[c["id"]] = [r["ok"] for r in results]
    return answers


def inspect(accounts):
    first = run_candy(accounts, 1)
    decisions = {}
    groups = {DEGRADED_PRIORITY: [], NORMAL_PRIORITY: []}
    for c in accounts:
        answers = first[c["id"]]
        reason = "无有效结论" if answers is None else ("首测答对" if answers[0] else "首测答错")
        decisions[c["id"]] = (reason, None)
        if answers is None or c["source"] != "auth_files":
            continue
        if c["priority"] == DEGRADED_PRIORITY and answers[0]:
            groups[NORMAL_PRIORITY].append(c)
        elif c["priority"] == NORMAL_PRIORITY and not answers[0]:
            groups[DEGRADED_PRIORITY].append(c)

    for priority in (DEGRADED_PRIORITY, NORMAL_PRIORITY):
        group = groups[priority]
        if not group:
            continue
        correct = priority == NORMAL_PRIORITY
        if correct:
            print(f"  恢复确认：{len(group)} 个账号，等待 {RECOVERY_DELAY_SECONDS} 秒后复测")
            time.sleep(RECOVERY_DELAY_SECONDS)
        repeated = run_candy(group, 2)
        for c in group:
            answers = repeated[c["id"]]
            if answers is None:
                decisions[c["id"]] = ("复测无有效结论", None)
            elif answers == [correct, correct]:
                decisions[c["id"]] = ("三次均答对" if correct else "三次均答错", priority)
            else:
                decisions[c["id"]] = ("三次答题结果不一致", None)
    return decisions


def notify_changes(changed):
    for priority, title, verdict, action in (
        (NORMAL_PRIORITY, "账号恢复正常", "答对", "已提高使用优先级。"),
        (DEGRADED_PRIORITY, "账号确认降智", "答错", "已降低使用优先级；后续巡检确认恢复后自动提权。"),
    ):
        if changed[priority]:
            message = f"糖果测试 · {MODEL}\n判定：连续 3 次{verdict}\n\n" + "\n".join(changed[priority])
            notify("CPA 巡检：" + title, message + "\n\n" + action)


def main():
    scoped = [c for c in state().values()
              if (not INCLUDE or matches(c, INCLUDE)) and not matches(c, EXCLUDE)]
    accounts = [c for c in scoped if available(c) and not is_free(c)]
    free_count = sum(is_free(c) for c in scoped)
    print(f"{time.strftime('%F %T')} 糖果巡检 · {MODEL}：{len(accounts)} 个账号，跳过 {free_count} 个 Free 账号")
    if not accounts:
        return
    decisions = inspect(accounts)
    current = state() if ADJUST_PRIORITY and any(p is not None for _, p in decisions.values()) else {}
    changed = {DEGRADED_PRIORITY: [], NORMAL_PRIORITY: []}
    for c in accounts:
        reason, priority = decisions[c["id"]]
        status = f"{reason}，保持 priority {c['priority']}"
        if priority is not None:
            transition = f"priority {c['priority']} → {priority}"
            if not ADJUST_PRIORITY:
                status = f"{reason}，建议 {transition}（自动调整未开启）"
            elif not available(current.get(c["id"])) or is_free(current[c["id"]]):
                status = f"{reason}，账号不可测试或为 Free，未调整 priority"
            else:
                api("PATCH", "/auth-files/fields", {"name": c["name"], "priority": priority})
                status = f"{reason}，{transition}"
                changed[priority].append(f"- {c['name']}：{transition}")
        print(f"  {c['name']}：{status}")
    notify_changes(changed)


if __name__ == "__main__":
    main()
