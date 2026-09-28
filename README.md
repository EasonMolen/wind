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
3. 执行：

   ```powershell
   .\scripts\package.ps1 -Version 0.1.0
   ```

产物为 `dist/NewWind-<version>-windows-amd64.zip`，包含：

```text
NewWind.exe
Everything64.dll
runtime/Everything.exe
licenses/
README.md
```

NewWind 会在 SDK 报告 IPC 不可用时启动同包的
`runtime/Everything.exe -startup -first-instance`。它不会安装服务、注册表项或
开机启动项。

## 更新清单

默认更新源已指向本项目的 GitHub Release API：

```text
https://api.github.com/repos/EasonMolen/wind/releases/latest
```

不需要另建 manifest。请为每个 Release 使用语义化标签（例如 `v0.1.0`），并上传
名称包含 `windows-amd64` 的 ZIP 资源；程序会读取 GitHub 的 `tag_name`、发布说明和
对应 ZIP 下载地址。

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
