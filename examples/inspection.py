#!/usr/bin/env python3
"""Codex 降智测试巡检：测试 CPA 凭证，发现降智时通过 ntfy 或 Bark 通知。部署方法见 docs/inspection.md。"""
import base64
import json
import time
import urllib.error
import urllib.parse
import urllib.request

CPA_URL = "https://cpa.example.com"  # CPA 地址
MANAGEMENT_KEY = ""                  # CPA 管理密钥
TEST = "modeltrace"                  # 巡检测试：modeltrace、fingerprint（指纹测试）或 candy（糖果测试）
OPTIONS = {                          # 各项测试的参数，巡检时只使用 TEST 对应的一项
    "modeltrace": {
        "model": "gpt-6-luna",       # 测试模型
        "concurrency": 3,            # 每凭证并发：1–3
    },
    "fingerprint": {
        "model": "gpt-6-luna",       # 测试模型
        "mode": "quick",             # 采集模式：quick、standard 或 strict
        "concurrency": 2,            # 每凭证并发：1–6
    },
    "candy": {
        "model": "gpt-6.1-sol",      # 测试模型
        "effort": "low",             # 推理强度：none（由 CPA 决定）、low、medium、high、xhigh 或 max
        "runs": 1,                   # 每凭证次数：1–10，答错任意一次即为降智
    },
}
INCLUDE = ["codex-*"]                # 只巡检邮箱或名称匹配的凭证，* 匹配任意字符；留空则不限
EXCLUDE = []                         # 不巡检邮箱或名称匹配的凭证
ADJUST_PRIORITY = False              # 是否按结果调整认证文件的 priority
DEGRADED_PRIORITY = 0                # 降智的认证文件调为此 priority
NORMAL_PRIORITY = 99                 # priority 为 DEGRADED_PRIORITY 的认证文件恢复正常后调为此 priority
NTFY_SERVER = "https://ntfy.sh"      # ntfy 服务器地址
NTFY_TOPIC = ""                      # ntfy 主题；与服务器地址都填写时才发送通知
NTFY_USERNAME = ""                   # ntfy 用户名；使用访问令牌时留空
NTFY_PASSWORD = ""                   # ntfy 密码或访问令牌
BARK_URL = ""                        # Bark 推送地址，例如 https://api.day.app/your-key；留空不发送通知
MAX_WAIT_MINUTES = 30                # 等待测试完成的最长时间（分钟）

INSPECTION = "/plugins/cpa-codex-candy-eval/inspection"
TEST_NAMES = {"modeltrace": "ModelTrace", "fingerprint": "指纹测试", "candy": "糖果测试"}


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
        urllib.request.urlopen(request, timeout=30)
    if BARK_URL:
        urllib.request.urlopen(BARK_URL, data=urllib.parse.urlencode({"title": title, "body": message}).encode(), timeout=30)


def main():
    label = f"{TEST_NAMES[TEST]} · {OPTIONS[TEST]['model']}"
    run = api("POST", INSPECTION, {"test": TEST, "include": INCLUDE, "exclude": EXCLUDE, **OPTIONS[TEST]})
    print(f"{time.strftime('%F %T')} {label}：开始测试 {run['started']} 个凭证")

    deadline = time.time() + MAX_WAIT_MINUTES * 60
    while True:
        time.sleep(15)
        report = api("GET", f"{INSPECTION}?test={TEST}")
        if not report["running"]:
            break
        if time.time() > deadline:
            raise SystemExit("等待测试完成超时")

    degraded = []
    for c in report["credentials"]:
        if c["degraded"] is None:
            print(f"  {c['name']}：没有结论，已忽略")
            continue
        status, priority = "正常", c["priority"]
        if c["degraded"]:
            status, priority = "降智", DEGRADED_PRIORITY
        elif c["priority"] == DEGRADED_PRIORITY:
            priority = NORMAL_PRIORITY
        if ADJUST_PRIORITY and c["source"] == "auth_files" and priority != c["priority"]:
            api("PATCH", "/auth-files/fields", {"name": c["name"], "priority": priority})
            status += f"，priority {c['priority']} → {priority}"
        print(f"  {c['name']}：{status}")
        if c["degraded"]:
            degraded.append(f"{c['name']}：{status}")

    if degraded:
        notify("CPA 巡检发现降智", label + "\n" + "\n".join(degraded))


if __name__ == "__main__":
    main()
