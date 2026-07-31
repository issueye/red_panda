set AGENT_HOME=%~dp0

cd %AGENT_HOME%

cd ../modules/agent/cmd/red-panda-agent

go build -o ../../../../bin/red-panda-agent.exe -ldflags "-w -s" -trimpath main.go

