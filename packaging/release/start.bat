@echo off
rem UapClaw 快捷启动：双击即可，等价于 uapclaw.exe app
rem 额外参数会原样透传，例如：start.bat --port 19001
cd /d "%~dp0"
uapclaw.exe app %*
echo.
echo UapClaw 已退出（按任意键关闭窗口）
pause >nul
