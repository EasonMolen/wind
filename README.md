# NewWind

Windows x64 文件启动器，使用 Everything SDK 提供实时文件搜索。

除 Everything 的索引结果外，NewWind 还会补充扫描进程、当前用户和系统
`PATH` 中的一层可执行文件；这些命中不会被普通系统目录过滤丢弃。同时会读取
Windows 的 **App Paths** 注册项、开始菜单和开始应用列表，因此可发现 Office（如
Word、Excel、PowerPoint）的安装入口，以及微软商店应用的可启动项。

`internal/search/sdk/` 是最小化的编译期 SDK：`include/Everything.h` 提供 CGO
头文件，`lib/Everything64.lib` 提供 x64 导入库。二者都不能删除；运行时 DLL
仍由项目根目录的 `Everything64.dll` 在打包时复制到发布目录。

## 开发要求

- Go 1.25+
- Windows x64
- `internal/search/sdk/` 中的 Everything SDK 文件

运行时 `Everything64.dll` 必须和 `NewWind.exe` 位于同一目录。它仅是
SDK 的 IPC 客户端；实际搜索还需要一个正在后台运行的 Everything 进程。

## 便携版发布

1. 从 voidtools 下载官方 x64 portable Everything 包，审阅其再分发许可。
2. 将 `Everything.exe`（以及可选的 `Everything.lng`）放入 `runtime/`，并将官方许可证文本放入
   `third_party/licenses/`。
3. 本地打包时，执行：

   ```powershell
   .\scripts\package.ps1 -Version 1.0.1
   ```

版本号使用 `major.minor.patch`，正式发布以 Git 标签为唯一来源。不要使用随机数或构建时间
作为产品版本：它们不能替代发布顺序，也可能与更新检查读取的 Release 标签不一致。本地包的
版本由 `-Version` 指定；正式包则由发布标签自动生成。

GitHub Actions 中的 `CI` 工作流会在推送和 Pull Request 上准备校验过的 Everything 依赖并运行
`go test ./...`。向 `main` 推送普通提交只运行 CI；只有使用发布提交信息时才会发布：

```powershell
git add .
git commit -m "new version"
git push github main
```

`new version` 默认升 patch 版本。需要升 minor 或 major 时，提交信息分别使用
`release: minor ...` 或 `release: major ...`，例如 `git commit -m "release: minor add plugin API"`。
工作流从现有 Git 标签计算下一个 `major.minor.patch` 版本，测试通过后把该版本注入程序并生成 ZIP，
然后在 GitHub 自动创建 `v<版本号>` 标签和 Release，附带 ZIP 与 SHA-256 文件。你不需要本地手写版本号或
执行 `git tag`。普通提交不会发布；失败的测试或打包也不会创建版本标签。

版本标签是正式版本号的唯一来源。Release 工作流兼容仓库里已有的四段历史标签（如
`v1.0.1.2026100602`），但新版本统一采用三段 SemVer。不要复用已发布的版本标签；修复或重发内容时应
递增版本。`config.json` 不保存程序版本；其中的 `update.manifestUrl` 只指定更新检查地址。
自动创建的远端标签不会自动出现在你的本地仓库；需要时可执行 `git fetch github --tags`。

产物为 `dist/NewWind-<version>-windows-amd64.zip`，包含：

```text
NewWind.exe
Everything64.dll
runtime/Everything.exe
VERSION.txt
licenses/
README.md
```

NewWind 会优先连接已运行的 Everything；如果没有可用 IPC，则先尝试启动已安装的
Everything，找不到已安装版本时再启动同包的 `runtime/Everything.exe`。数据库初次
建立索引时，NewWind 会提示索引仍在加载，而不会把它报告为搜索不可用。NewWind 不会
安装服务、修改注册表项或设置开机启动项。

## 窗口位置

窗口会在鼠标所在显示器的可用工作区内按比例定位，任务栏区域不会计入。配置文件
中的 `window.positionX` 和 `window.positionY` 控制窗口左上角在可移动范围内的位置：
`0` 表示左侧/顶部，`0.5` 表示居中，`1` 表示右侧/底部。默认值为 `0.5` 和 `0.35`。
也可以在设置窗口中修改这两个值并保存；窗口会立即移动，新位置同时保存到配置文件。

```json
{
  "window": {
    "positionX": 0.5,
    "positionY": 0.35
  }
}
```

## 更新清单

默认更新源已指向本项目的 GitHub Release API：

```text
https://api.github.com/repos/EasonMolen/wind/releases/latest
```

不需要另建 manifest。Release 工作流使用 Git 标签（例如 `v1.0.2`）作为版本，并自动上传同版本
的 ZIP 和 SHA-256 校验文件。程序会读取 GitHub 的 `tag_name`、发布说明和对应 ZIP 下载地址。

如需改用自建更新源，可在 `config.json` 中覆盖：

```json
{
  "update": {
    "manifestUrl": "https://api.github.com/repos/EasonMolen/wind/releases/latest"
  }
}
```

自建源也可继续使用以下紧凑 JSON 格式：

```json
{
  "version": "0.1.1",
  "downloadUrl": "https://releases.example.com/NewWind-0.1.1-windows-amd64.zip",
  "sha256": "<release zip sha256>",
  "notes": "修复搜索稳定性"
}
```

当前实现已支持检查与版本比较；在确定发布站点和签名方案前，不会下载或替换用户文件。
