#!/bin/bash

set -e  # 遇到错误立即退出

echo "=== TurboRelay 远程编译脚本 (ARM64) ==="

# 配置变量
REMOTE_HOST="root@10.35.148.59"
REMOTE_PORT="22"
REMOTE_DIR="/root/ff/cap/src"
BINARY_NAME="turbo_relay_arm64"

# 编译模式：dynamic 或 static
BUILD_MODE="${1:-dynamic}"

echo "1. 清理远程目录..."
ssh -p $REMOTE_PORT $REMOTE_HOST "rm -rf $REMOTE_DIR && mkdir -p $REMOTE_DIR"

echo "2. 拷贝源代码到远程服务器..."
scp -P $REMOTE_PORT -r . $REMOTE_HOST:$REMOTE_DIR

if [ "$BUILD_MODE" = "static" ]; then
    echo "3. 在远程服务器编译（静态链接，ARM64）..."
    echo "   目标: 静态链接，无外部依赖 (ARM64)"
    ssh -p $REMOTE_PORT $REMOTE_HOST "cd $REMOTE_DIR && \
        export PATH=/usr/local/go/bin:\$PATH && \
        export GOROOT=/usr/local/go && \
        echo \"检查Go环境...\" && \
        go version && \
        \
        echo \"尝试静态编译...\" && \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -a -ldflags \"-linkmode external -extldflags \\\"-static\\\" -s -w\" -tags \"netgo osusergo static_build\" -o $BINARY_NAME && \
        \
        echo \"检查二进制文件依赖...\" && \
        ldd $BINARY_NAME || echo \"静态链接成功，无外部依赖\" && \
        \
        echo \"测试二进制文件...\" && \
        ./$BINARY_NAME --help || echo \"帮助信息显示完成\""
else
    echo "3. 在远程服务器编译（动态链接，ARM64）..."
    echo "   目标: 动态链接，需要系统库 (ARM64)"
    ssh -p $REMOTE_PORT $REMOTE_HOST "cd $REMOTE_DIR && \
        export PATH=/usr/local/go/bin:\$PATH && \
        export GOROOT=/usr/local/go && \
        echo \"检查Go环境...\" && \
        go version && \
        \
        echo \"动态编译...\" && \
        CGO_ENABLED=1 GOOS=linux GOARCH=arm64 go build -ldflags \"-s -w\" -o $BINARY_NAME && \
        \
        echo \"检查二进制文件依赖...\" && \
        ldd $BINARY_NAME && \
        \
        echo \"测试二进制文件...\" && \
        ./$BINARY_NAME --help || echo \"帮助信息显示完成\""
fi

echo "4. 从远程服务器下载编译好的二进制文件..."
scp -P $REMOTE_PORT $REMOTE_HOST:$REMOTE_DIR/$BINARY_NAME .

echo "5. 验证本地二进制文件..."
ls -la $BINARY_NAME
file $BINARY_NAME

echo ""
echo "=== 编译完成 (ARM64) ==="
echo "二进制文件: $BINARY_NAME"
echo "编译模式: $BUILD_MODE"
echo "远程服务器: $REMOTE_HOST:$REMOTE_PORT"
echo "远程目录: $REMOTE_DIR"
if [ "$BUILD_MODE" = "static" ]; then
    echo "静态链接: 无外部依赖，可在任何Linux ARM64系统运行"
else
    echo "动态链接: 需要目标系统有相应的共享库 (ARM64)"
fi
echo ""

# 判断/Volumes/ff是否存在
if [ -d "/Volumes/ff" ]; then
    cp $BINARY_NAME /Volumes/ff/
    ls -l /Volumes/ff  | grep $BINARY_NAME
fi 