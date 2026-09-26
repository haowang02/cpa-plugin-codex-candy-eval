# Codex 糖果测试（CLIProxyAPI 插件）

在 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)（CPA）的管理面板里，用一道糖果数学题测试你的 Codex 账号是否降智。题目来自 [codex-candy-eval](https://github.com/haowang02/codex-candy-eval)，正确答案是 **21**。

![example](./images/example.png)

## 安装

在 CPA 工作目录运行，插件会安装到 `plugins/<系统>/<架构>/`，文件名包含版本号，与 CPA 插件商店保持一致。例如 Linux x64 的 v0.1.8 安装路径为 `plugins/linux/amd64/cpa-codex-candy-eval-v0.1.8.so`；macOS 使用 `darwin/<架构>/` 和 `.dylib`，Windows 使用 `windows/amd64/` 和 `.dll`。

Docker 部署时，在挂载到容器 `/CLIProxyAPI/plugins` 的 `plugins` 目录的上一级目录运行。脚本按执行环境选择系统和架构，因此执行环境需与 CPA 容器匹配；跨平台部署可通过 CPA 管理面板安装，或手动下载容器平台对应的插件。

macOS 和 Linux：

```sh
curl -fsSL https://raw.githubusercontent.com/haowang02/cpa-plugin-codex-candy-eval/main/install.sh | sh
```

Windows 请先停止 CPA，再在 PowerShell 中运行：

```powershell
irm https://raw.githubusercontent.com/haowang02/cpa-plugin-codex-candy-eval/main/install.ps1 | iex
```

也可以从 [Releases](https://github.com/haowang02/cpa-plugin-codex-candy-eval/releases/latest) 下载对应平台的压缩包，解压后将插件文件重命名为 `cpa-codex-candy-eval-v<版本号>.<扩展名>`，放入对应的 `plugins/<系统>/<架构>/` 目录。

CPA 仍兼容直接放在 `plugins/` 根目录的插件，但同一插件的带版本号文件优先于无版本号文件。测试记录仍保存在 `plugins/cpa-codex-candy-eval-state.json`，无需随动态库移动。

## 配置

在 CPA 的 `config.yaml` 中启用插件，保存后 CPA 会自动加载。`plugins.dir` 保持为插件根目录，CPA 会自动查找当前系统和架构的子目录：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-codex-candy-eval:
      enabled: true
```

升级插件后需要重启 CPA。

若此前通过插件商店安装，`plugins.configs.cpa-codex-candy-eval.store` 中的 `version`（或 `release-tag`）可能固定了加载版本。脚本只安装文件；升级时需通过管理面板切换版本，或同步更新配置中的 `store.version` 和 `store.release-tag`。

## 使用

选择模型、推理强度和测试次数，测试单个账号或全部已启用的账号。

- 回答中出现 21 即判为答对，正确率按当前所选的模型和推理强度统计。
- 每个账号保留最近 20 次测试记录。
- 点击「脱敏」可以模糊账号列，方便截图分享。
- 每次测试都会消耗被测账号的额度。

## 致谢

- [codex-candy-eval](https://github.com/haowang02/codex-candy-eval)
- [LINUX DO](https://linux.do/) - 新的理想型社区
