# Codex 糖果测试（CLIProxyAPI 插件）

在 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)（CPA）的管理面板里，用一道糖果数学题测试你的 Codex 账号是否降智。题目来自 [codex-candy-eval](https://github.com/haowang02/codex-candy-eval)，正确答案是 **21**。

![example](./images/example.png)

## 安装

1. 从 [Releases](https://github.com/haowang02/cpa-plugin-codex-candy-eval/releases/latest) 下载与 CPA 运行平台对应的压缩包，解压后把插件文件放进 CPA 的 `plugins` 目录（Docker 部署时放进映射到 `/CLIProxyAPI/plugins` 的目录）。
2. 在 CPA 管理面板的「插件管理」中启用 `cpa-codex-candy-eval`。

## 使用

选择模型、推理强度和测试次数，测试单个账号或全部已启用的账号。

- 回答中出现 21 即判为答对，正确率按当前所选的模型和推理强度统计。
- 每个账号保留最近 20 次测试记录。
- 点击「脱敏」可以模糊账号列，方便截图分享。
- 每次测试都会消耗被测账号的额度。

## 致谢

- [codex-candy-eval](https://github.com/haowang02/codex-candy-eval)
- [LINUX DO](https://linux.do/) - 新的理想型社区
