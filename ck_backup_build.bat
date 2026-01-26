@echo off
setlocal
cd /d %~dp0

echo Building ck-cluster-backup for Linux...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64

if not exist dist mkdir dist

go build -buildvcs=false -trimpath -o dist\ck_backup ./cmd/ck_cluster_backup
if errorlevel 1 (
  echo Build failed!
  exit /b 1
)

echo Build completed!
dir dist
endlocal
