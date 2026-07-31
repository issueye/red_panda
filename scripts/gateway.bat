set AGENT_HOME=%~dp0

cd %AGENT_HOME%

cd ../modules/gateway/cmd/red-panda-gateway

go build -o ../../../../bin/red-panda-gateway.exe -ldflags "-w -s" -trimpath main.go

