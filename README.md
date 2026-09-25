# Codex 糖果测试（CLIProxyAPI 插件）

在 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)（CPA）的管理面板里，用一道糖果数学题测试你的 Codex 账号是否降智。题目来自 [codex-candy-eval](https://github.com/haowang02/codex-candy-eval)，正确答案是 **21**。

![example](./images/example.png)

- 测试单个 Codex 账号，或一键测试全部已启用的 Codex 账号
- 可选择模型、推理强度和每个账号的测试次数
- 显示每次回答的结果、用时和推理 token 数，并统计正确率
- 每个账号保留最近 20 次测试记录，重启 CPA 后仍然保留

## 安装

### 1. 下载插件

在 [Releases](https://github.com/haowang02/cpa-plugin-codex-candy-eval/releases/latest) 页面下载与 CPA 运行平台对应的压缩包：

| CPA 运行平台 | 压缩包 |
| --- | --- |
| Linux x86_64（包括常见的 Docker 部署） | `cpa-codex-candy-eval_linux_amd64.tar.gz` |
| Linux ARM64 | `cpa-codex-candy-eval_linux_arm64.tar.gz` |
| macOS Apple 芯片 | `cpa-codex-candy-eval_darwin_arm64.tar.gz` |
| macOS Intel 芯片 | `cpa-codex-candy-eval_darwin_amd64.tar.gz` |
| Windows x64 | `cpa-codex-candy-eval_windows_amd64.zip` |

### 2. 放入 CPA 的插件目录

解压后，把得到的插件文件（`cpa-codex-candy-eval.so`、`.dylib` 或 `.dll`）放进 CPA 的 `plugins` 目录。

- 直接运行 CPA：`plugins` 目录位于 CPA 程序所在目录。
- Docker 部署：放进映射到容器内 `/CLIProxyAPI/plugins` 的宿主机目录。如果还没有映射，请在 `docker-compose.yml` 的 `volumes` 中加入一行 `- ./plugins:/CLIProxyAPI/plugins`，然后重建容器。

Linux x86_64 可以在 `plugins` 目录中直接执行：

```sh
curl -fsSL https://github.com/haowang02/cpa-plugin-codex-candy-eval/releases/latest/download/cpa-codex-candy-eval_linux_amd64.tar.gz | tar -xz
```

### 3. 启用插件

打开 CPA 管理面板，进入「插件管理」，找到 `cpa-codex-candy-eval` 并启用。如果还没有启用过插件，请先在「配置面板」中打开「启用插件系统」。

也可以直接编辑 CPA 的 `config.yaml`：

```yaml
plugins:
  enabled: true
  dir: "plugins"
  configs:
    cpa-codex-candy-eval:
      enabled: true
```

保存后 CPA 会自动加载插件，无需重启。插件没有需要填写的配置项。

## 使用

1. 在 CPA 管理面板的侧边栏打开「Codex 糖果测试」。
2. 选择模型、推理强度和每个账号的测试次数（默认 gpt-5.6-sol、low、1 次）。
3. 点击「测试全部」测试所有已启用的 Codex 账号，或点击某个账号右侧的「测试」只测这一个账号。不同账号同时测试，同一账号的多次测试依次进行，每次通常需要几十秒到几分钟。
4. 点击账号名称可以展开查看每次测试的完整回答。「查看题目」中可以一键复制题目。

结果说明：

- 回答中出现 21 即判为答对。
- 「最近 20 次」一栏中，绿色对勾表示答对，红色叉号表示答错，灰色感叹号表示请求出错（例如额度用尽）；颜色较浅的记录来自其他模型或推理强度。
- 正确率只统计当前所选模型和推理强度下成功完成的测试。

也可以在浏览器中直接打开 `https://<你的 CPA 地址>/v0/resource/plugins/cpa-codex-candy-eval/ui`，输入 CPA 管理密钥后使用。

## 注意事项

- 每次测试都会消耗被测账号的额度，高推理强度下一次测试通常会用掉数千 token。
- 测试记录保存在 CPA 插件目录下的 `cpa-codex-candy-eval-state.json`，删除该文件即可清空全部记录。
- 升级插件：停止 CPA，用新版本文件覆盖旧文件，再启动 CPA。

## 致谢

- [codex-candy-eval](https://github.com/haowang02/codex-candy-eval)
- [LINUX DO](https://linux.do/) - 新的理想型社区
