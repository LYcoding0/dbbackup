@echo off
setlocal
cd /d %~dp0

echo Building for Linux...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64

if not exist dist mkdir dist

go build -buildvcs=false -trimpath -o dist\pg_backup ./cmd/postgresql_pgbackrest
if errorlevel 1 (
  echo Build failed!
  exit /b 1
)

echo Build completed!
dir dist
endlocal