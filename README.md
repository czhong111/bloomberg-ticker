# Northstar 实时新闻条

Windows 原生置顶新闻条，汇集美国 NPR、欧洲 The Guardian/France 24、香港 RTHK/SCMP、新加坡 CNA、日本 NHK World/Japan Times 的公开 RSS，不使用中国大陆新闻源。每条新闻会显示地区和媒体名；每 5 分钟更新一次，连接失败时保留已加载新闻并显示状态，不生成示例新闻。能否连通这些境外新闻站点取决于运行设备的网络环境。

## 使用

双击 `NorthstarTicker.exe` 启动。窗口默认贴在屏幕工作区底部、紧挨任务栏；拖动上方空白区域可移动窗口，拖动窗口边缘或角落可改变大小。点击右侧“贴底”恢复默认位置，“暂停/继续”控制滚动，“关闭”退出。首次获取新闻需要网络连接。

## 从源码构建

需要 Go 1.23 或更新版本：

```powershell
go test ./...
go build -ldflags="-H windowsgui" -o NorthstarTicker.exe .
```
