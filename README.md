# NewWind

Windows x64 文件启动器，使用 Everything SDK 提供实时文件搜索。

## 开发要求

- Go 1.25+
- Windows x64
- `internal/search/sdk/` 中的 Everything SDK 文件

运行时 `Everything64.dll` 必须和 `NewWind.exe` 位于同一目录。它仅是
SDK 的 IPC 客户端；实际搜索还需要一个正在后台运行的 Everything 进程。

## 便携版发布

1. 从 voidtools 下载官方 x64 portable Everything 包，审阅其再分发许可。
2. 将 `Everything.exe` 放入 `runtime/Everything.exe`，并将官方许可证文本放入
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

自动检查通过配置文件的 `update.manifestUrl` 获取 HTTPS JSON。清单格式：

```json
{
  "version": "0.1.1",
  "downloadUrl": "https://releases.example.com/NewWind-0.1.1-windows-amd64.zip",
  "sha256": "<release zip sha256>",
  "notes": "修复搜索稳定性"
}
```

当前实现已支持检查与版本比较；在确定发布站点和签名方案前，不会下载或替换用户文件。
