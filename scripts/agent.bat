cd ../modules/agent/cmd/red-panda-agent

go build -o ../../../../bin/red-panda-agent.exe -ldflags "-w -s" .

cd ../../../../bin
