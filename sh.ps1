# 设置环境变量 GOOS 为 linux
$env:GOOS = "linux"

# 编译 Go 代码，生成二进制文件 main
go build -o main main.go

# 创建一个 zip 文件并将二进制文件 main 放入其中
Compress-Archive -Path .\main -DestinationPath ..\iac-pulumi\function.zip
