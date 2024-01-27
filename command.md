1. $env:GOOS="linux" 
2. go build -o main main.go
3. Compress-Archive -Path .\main -DestinationPath ../iac-pulumi/function.zip