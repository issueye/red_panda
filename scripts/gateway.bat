cd ../modules/gateway/cmd/red-panda-gateway

go build -o ../../../../bin/red-panda-gateway.exe -ldflags "-w -s" .

cd ../../../../bin
