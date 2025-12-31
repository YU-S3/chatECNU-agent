#!/bin/bash

# 构建脚本 - 编译Go程序为可执行文件

set -e

echo "开始构建 ChatECNU Agent..."

# 检查Go是否安装
if ! command -v go &> /dev/null; then
    echo "错误: 未找到Go编译器"
    echo "请安装Go: https://go.dev/dl/"
    exit 1
fi

# 配置Go代理（使用国内镜像源）
echo "配置Go代理..."
export GOPROXY=https://goproxy.cn,direct
export GOSUMDB=sum.golang.google.cn
echo "GOPROXY设置为: $GOPROXY"

# 下载依赖
echo "下载依赖..."
go mod download
go mod tidy

# 构建可执行文件
echo "编译中..."
go build -ldflags="-s -w" -o chatecnu-agent main.go

# 检查构建是否成功
if [ -f "./chatecnu-agent" ]; then
    echo "构建成功！"
    echo "可执行文件: ./chatecnu-agent"
    echo ""
    echo "使用方法:"
    echo "  1. 设置环境变量: export ECNU_API_KEY='your_api_key'"
    echo "  2. 运行: ./chatecnu-agent"
    echo ""
    echo "或者创建 .env 文件:"
    echo "  cp env.example .env"
    echo "  # 编辑 .env 文件，填入你的API密钥"
    echo "  ./chatecnu-agent"
else
    echo "构建失败！"
    exit 1
fi

