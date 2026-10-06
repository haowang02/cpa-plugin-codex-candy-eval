# 定时巡检

巡检脚本用 ModelTrace、指纹测试或糖果测试检查 CPA 凭证，配合 crontab 定时运行。发现降智后，脚本可以：

- 调低该认证文件的 priority，让 CPA 优先使用正常的号；之后巡检结果恢复正常时再调回来。
- 通过 ntfy 或 Bark 发送通知。

两者可以都开启，也可以只开一个；都不开时脚本只记录结果。

**降智**指 ModelTrace 或指纹测试判断模型与所测模型不一致，或者糖果测试没有答出 21。认证失效、额度用尽等原因导致请求失败时得不出结论，脚本会忽略，不会调整 priority，也不会发送通知。

## 准备

- 插件 v0.3.13 或更高版本。
- 一台装有 Python 3.8 或更高版本、能使用 crontab 的 Linux 或 macOS 机器，能访问 CPA 管理面板即可，也可以就是运行 CPA 的服务器。脚本只用 Python 标准库，不需要安装依赖。
- CPA 管理密钥。

## 1. 下载脚本

```sh
mkdir -p ~/cpa-inspection
curl -fsSL -o ~/cpa-inspection/inspection.py https://raw.githubusercontent.com/haowang02/cpa-plugin-codex-candy-eval/main/examples/inspection.py
chmod 600 ~/cpa-inspection/inspection.py
```

脚本中会写入管理密钥，`chmod 600` 可以防止其他用户读取。

## 2. 修改配置

打开 `~/cpa-inspection/inspection.py`，修改开头的配置。必须填写的只有两项：

- `CPA_URL`：CPA 地址，即打开管理面板时网址中 `/management.html` 之前的部分，例如 `https://cpa.example.com`。
- `MANAGEMENT_KEY`：CPA 管理密钥。

其余配置项都已写好默认值，说明见各行注释，下面分类介绍。

### 测试

`TEST` 选择巡检使用的测试。`OPTIONS` 中是三项测试各自的参数，包括测试模型，巡检时只使用 `TEST` 对应的一项。

ModelTrace 和指纹测试只能识别各自模型库中的模型。建议先在插件页面上用正常的号测一次，确认结果是「与测试模型一致」，再用该模型巡检。

每次巡检，每个凭证的请求数为：ModelTrace 3 次，糖果测试为 `runs` 的值，指纹测试快速、标准、严格模式分别为 60、200、400 次，失败的请求还会重试。巡检越频繁，消耗的额度越多。

### 范围

脚本只巡检已启用、且没有处于冷却中的凭证，`INCLUDE` 和 `EXCLUDE` 进一步限定范围。规则匹配凭证的邮箱或名称，`*` 匹配任意字符：

- 邮箱就是插件页面上显示的邮箱。
- 认证文件的名称是文件名，例如 `codex-xxxx@gmail.com-pro.json`，可以在 CPA 管理面板的「认证文件」页面查看。
- 配置中的 API Key 的名称与插件页面上显示的一致。

默认的 `["codex-*"]` 表示所有 Codex 认证文件。其他写法例如：

```python
INCLUDE = ["alice@gmail.com", "bob@outlook.com"]  # 只巡检这两个账号
INCLUDE = ["*-pro.json", "*-team.json"]           # 只巡检 Pro 和 Business 账号
EXCLUDE = ["*@example.com"]                       # 不巡检某个域名的邮箱
```

不支持所测模型的凭证会请求失败、得不出结论，可以用这两项把它们排除。

### priority

CPA 会优先使用 priority 较大的凭证。`ADJUST_PRIORITY` 设为 `True` 后：

- 降智的认证文件，priority 调为 `DEGRADED_PRIORITY`（默认 `0`）。
- 结果正常、且 priority 等于 `DEGRADED_PRIORITY` 的认证文件，priority 调为 `NORMAL_PRIORITY`（默认 `99`）。priority 为其他值的不会被改动。

没有设置过 priority 的认证文件视为 `0`，所以按默认值开启后，第一次巡检就会把结果正常、没有设置过 priority 的认证文件调为 `99`。如果你已经用 priority 安排了凭证的使用顺序，请按自己的安排修改这两个值。配置中的 API Key 不会被调整。

### 通知

ntfy 需要填写服务器地址和主题；主题需要登录时再填写账号密码，使用访问令牌时用户名留空、密码填令牌。Bark 填写 App 中的推送地址。两者都配置时会同时发送，都不配置则不发送通知。

## 3. 手动运行一次

```sh
python3 ~/cpa-inspection/inspection.py
```

测试完成后逐个输出结果，调整了 priority 的会一并注明，例如：

```text
2026-10-07 12:00:03 ModelTrace · gpt-6-luna：开始测试 3 个凭证
  codex-xxxx@gmail.com-pro.json：正常
  codex-yyyy@gmail.com-plus.json：降智，priority 99 → 0
  codex-zzzz@gmail.com-team.json：没有结论，已忽略
```

运行失败时脚本会输出原因，例如管理密钥错误或无法连接 CPA。

## 4. 配置 crontab

先用 `command -v python3` 查看 Python 的完整路径，然后运行 `crontab -e`，加入一行，例如：

```text
0 * * * * /usr/bin/python3 $HOME/cpa-inspection/inspection.py >> $HOME/cpa-inspection/inspection.log 2>&1
```

把 `/usr/bin/python3` 换成你的路径。开头五项依次是分钟、小时、日、月、星期，上例表示每小时整点运行一次；`*/30 * * * *` 表示每 30 分钟，`0 9 * * *` 表示每天 9 点。两次巡检的间隔最好长于一次巡检所需的时间，否则上一次还在测试的凭证会被跳过。

用 `crontab -l` 确认已经保存，用 `tail -f ~/cpa-inspection/inspection.log` 查看运行记录。

## API

需要自己编写脚本时，可以直接调用插件的 API。请求地址的前缀是 `<CPA 地址>/v0/management/plugins/cpa-codex-candy-eval`，认证方式与管理面板相同：在请求头中带上 `Authorization: Bearer <管理密钥>`。

### 开始巡检

`POST /inspection`

```json
{"test": "modeltrace", "include": ["codex-*"], "exclude": [], "model": "gpt-6-luna", "concurrency": 3}
```

`test`、`include`、`exclude` 与脚本中的同名配置相同，其余字段是该测试的参数，与脚本 `OPTIONS` 中对应的一项相同。返回的 `started` 是开始测试的凭证数，正在进行其他测试的凭证会被跳过。

### 查看结果

`GET /inspection?test=modeltrace`

返回该测试最近一次巡检的结果：`running` 表示是否仍在测试，`credentials` 列出这次巡检的凭证，每个凭证包含：

| 字段 | 说明 |
| --- | --- |
| `name`、`email` | 凭证名称和邮箱 |
| `source` | `auth_files` 为认证文件，`ai_providers` 为配置中的 API Key |
| `priority` | 凭证在 CPA 中的 priority |
| `results` | 这次巡检得到的测试记录 |
| `degraded` | `true` 为降智，`false` 为正常，`null` 为没有结论（如请求失败） |

调整 priority 使用 CPA 自带的 API：`PATCH <CPA 地址>/v0/management/auth-files/fields`，请求体为 `{"name": "<认证文件名>", "priority": 0}`。
