@echo off
setlocal
cd /d "%~dp0"
where go >nul 2>nul
if errorlevel 1 (
  echo Install Go from https://go.dev/dl/ to rebuild the source code.
  exit /b 1
)
set CGO_ENABLED=0
set GOOS=windows
set GOARCH=amd64
go test ./internal/decoder
if errorlevel 1 exit /b 1
if not exist dist mkdir dist
go build -buildvcs=false -trimpath -ldflags="-H=windowsgui -s -w" -o dist\Linkcleaner.exe ./cmd/enlacelimpio
if errorlevel 1 exit /b 1
echo Executable created: dist\Linkcleaner.exe
endlocal
