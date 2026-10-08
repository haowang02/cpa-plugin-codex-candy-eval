# 定时糖果巡检

[巡检脚本](../examples/inspection.py) 用糖果题检查账号，每次先测一次。默认只记录结果；开启 `ADJUST_PRIORITY` 后，确认账号恢复或降智时自动调整 priority，并通过 ntfy 或 Bark 通知。

## 巡检规则

以巡检开始时的 priority 为准，默认 `0` 表示降智账号，`99` 表示正常账号：

| 原 priority | 首测结果 | 处理 |
| --- | --- | --- |
| `0` | 答错 | 保持 `0`，不复测、不通知 |
| `0` | 答对 | 等待 240 秒，再测两次；三次都答对才调为 `99`，通知恢复正常 |
| `99` | 答对 | 保持 `99`，不复测、不通知 |
| `99` | 答错 | 直接再测两次；三次都答错才调为 `0`，通知确认降智 |

三次结果不一致、请求失败或测试记录不完整时，保持 priority，不通知。答对与否以插件判定为准：答案中包含不与其他数字相连的 `21`，即视为答对。

没有设置过 priority 的认证文件视为 `0`。其他 priority 和配置中的 API Key 只测试，不自动调整。priority 只在巡检开始时读取，后续不重复校验；历史答题不计入本次判定。

默认跳过插件已识别的 Free 账号，以及禁用、冷却或正在测试的凭证。插件通过 `/state` 的 `plan_type` 返回订阅类型，来源为 CPA 的订阅字段、认证文件的 `plan_type` 或 ID Token 中的 `chatgpt_plan_type`。订阅未知时仍测试。

## 部署

需要插件 v0.3.13 或更高版本、CPA 管理密钥，以及能访问 CPA 的 Linux 或 macOS 机器。脚本使用 Python 3.8 或更高版本，只依赖标准库。

### 1. 下载脚本

```sh
mkdir -p ~/cpa-inspection
curl -fsSL -o ~/cpa-inspection/inspection.py https://raw.githubusercontent.com/haowang02/cpa-plugin-codex-candy-eval/main/examples/inspection.py
chmod 600 ~/cpa-inspection/inspection.py
```

脚本保存管理密钥和推送配置，文件权限应保持为 `600`。

### 2. 配置

编辑脚本开头的配置，填写 `CPA_URL` 和 `MANAGEMENT_KEY`。`CPA_URL` 是管理面板网址中 `/management.html` 之前的部分，例如 `https://cpa.example.com`。

| 配置 | 默认值与用途 |
| --- | --- |
| `MODEL`、`EFFORT` | `gpt-6.1-sol`、`low`；首测和复测使用相同模型与推理强度 |
| `INCLUDE`、`EXCLUDE` | `["codex-*"]`、`[]`；按邮箱或名称选择、排除凭证 |
| `SKIP_FREE` | `True`；跳过已识别的 Free 订阅 |
| `ADJUST_PRIORITY` | `False`；设为 `True` 后自动调整并通知，否则只记录结果和调整建议 |
| `DEGRADED_PRIORITY`、`NORMAL_PRIORITY` | `0`、`99`；降智和正常账号的 priority |
| `RECOVERY_DELAY_SECONDS` | `240`；降智账号首测答对后的复测等待时间 |
| `MAX_WAIT_MINUTES` | `30`；每轮测试完成的等待上限，不含恢复前的等待时间 |

范围规则支持 `*`、`?` 和字符集合，匹配邮箱或凭证名称。`INCLUDE` 留空表示不限范围；`EXCLUDE` 优先排除。例如：

```python
INCLUDE = ["alice@gmail.com", "bob@outlook.com"]
INCLUDE = ["*-pro.json", "*-team.json"]
EXCLUDE = ["*@example.com"]
```

每个账号首测一次，需要确认状态变化的认证文件再测两次。请求会消耗账号额度。

### 3. 配置通知

ntfy 填写 `NTFY_SERVER` 和 `NTFY_TOPIC`；需要认证时填写 `NTFY_USERNAME` 和 `NTFY_PASSWORD`，使用访问令牌时用户名留空、密码填令牌。Bark 填写 `BARK_URL`。两个渠道都配置时都会推送，均留空则不推送。

只有成功调整 priority 的账号才通知。同一轮的恢复和降智分别汇总，文案包含模型、判定和账号的 priority 变化：

```text
CPA 巡检：账号恢复正常
糖果测试 · gpt-6.1-sol
判定：连续 3 次答对

- codex-alice@example.com-pro.json：priority 0 → 99

已提高使用优先级。
```

降智通知使用「CPA 巡检：账号确认降智」标题，判定为「连续 3 次答错」，列出 `priority 99 → 0`，并说明后续确认恢复后自动提权。

### 4. 手动验证

```sh
python3 ~/cpa-inspection/inspection.py
```

日志逐个记录答题判定和 priority，例如：

```text
2026-10-08 12:00:03 糖果巡检 · gpt-6.1-sol：3 个账号，跳过 1 个 Free 账号
  首测：3 个账号，每个账号 1 次
  复测：1 个账号，每个账号 2 次
  codex-alice@example.com-pro.json：首测答对，保持 priority 99
  codex-bob@example.com-plus.json：三次均答错，priority 99 → 0
  codex-carol@example.com-team.json：无有效结论，保持 priority 0
```

### 5. 定时运行

用 `command -v python3` 确认 Python 路径，再运行 `crontab -e` 添加任务。例如每小时整点运行一次：

```text
0 * * * * /usr/bin/python3 -u $HOME/cpa-inspection/inspection.py >> $HOME/cpa-inspection/inspection.log 2>&1
```

将 `/usr/bin/python3` 换成实际路径。巡检间隔应长于一次完整巡检的耗时，包括恢复确认前的 240 秒等待。用 `crontab -l` 查看任务，用 `tail -f ~/cpa-inspection/inspection.log` 查看日志。

## 使用的 API

以下请求使用 CPA 管理认证：`Authorization: Bearer <管理密钥>`，路径前缀为 `<CPA 地址>/v0/management`。

- `GET /plugins/cpa-codex-candy-eval/state`：返回 `auths`，包含凭证 `id`、`name`、`email`、`source`、`priority`、`plan_type`，以及糖果测试的 `running` 进度和 `results` 历史。
- `POST /plugins/cpa-codex-candy-eval/run`：按凭证 ID 发起糖果测试，返回 `started`。首测 `runs=1`，复测 `runs=2`。
- `PATCH /auth-files/fields`：修改认证文件的 priority。

糖果测试请求示例：

```json
{"auth_ids":["<凭证 ID>"],"model":"gpt-6.1-sol","effort":"low","runs":1}
```

调整 priority 的请求示例：

```json
{"name":"<认证文件名>","priority":99}
```

脚本轮询 `/state`，等待所选账号的 `running` 结束，再读取本轮新增且模型、推理强度一致的记录。只有记录数量完整、没有错误或跳过标记时，答题结果才参与判定。
