@echo off
echo Building for Linux...
set CGO_ENABLED=0
set GOOS=linux
set GOARCH=amd64
go build -a -o dbbackup dbbackup.go
echo Build completed!
dir dbbackup-linux-demo