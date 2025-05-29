#!/bin/bash

# RTP测试启动脚本
# 快速启动各种RTP测试

echo "=== TurboRelay RTP序列号窗口测试 ==="
echo ""

# 配置
NEAR_END_IP="10.35.146.109"
FAR_END_IP="10.35.146.7"
TEST_PORT="5004"

echo "测试环境:"
echo "  近端设备: $NEAR_END_IP:10810"
echo "  远端设备: $FAR_END_IP:22"
echo "  测试端口: $TEST_PORT"
echo ""

# 菜单选择
echo "请选择测试类型:"
echo "1. 远端包测试 (从远端发送到近端，测试序列号窗口)"
echo "2. 近端包测试 (从近端发送到远端，测试直接转发)"
echo "3. 双向测试 (同时测试两个方向)"
echo "4. 特定场景测试"
echo "5. 查看测试指南"
echo "0. 退出"
echo ""

read -p "请输入选择 (0-5): " choice

case $choice in
    1)
        echo ""
        echo "=== 远端包测试 ==="
        echo "从远端($FAR_END_IP)发送RTP包到近端($NEAR_END_IP)"
        echo "测试序列号窗口、乱序处理、NACK机制等"
        echo ""
        
        read -p "按Enter开始测试..."
        python3 rtp_test_sender.py $NEAR_END_IP $TEST_PORT --source "远端MCU" --test all --interval 1.0
        ;;
        
    2)
        echo ""
        echo "=== 近端包测试 ==="
        echo "从近端($NEAR_END_IP)发送RTP包到远端($FAR_END_IP)"
        echo "测试直接转发和缓存机制"
        echo ""
        
        read -p "按Enter开始测试..."
        python3 rtp_test_sender.py $FAR_END_IP $TEST_PORT --source "近端终端" --test normal --interval 0.5
        ;;
        
    3)
        echo ""
        echo "=== 双向测试 ==="
        echo "同时从两个方向发送包，测试SSRC冲突处理"
        echo ""
        
        read -p "按Enter开始测试..."
        
        # 启动远端测试 (后台)
        echo "启动远端包测试..."
        python3 rtp_test_sender.py $NEAR_END_IP $TEST_PORT --source "远端MCU" --test normal --interval 2.0 &
        REMOTE_PID=$!
        
        sleep 1
        
        # 启动近端测试
        echo "启动近端包测试..."
        python3 rtp_test_sender.py $FAR_END_IP $TEST_PORT --source "近端终端" --test normal --interval 2.0
        
        # 等待远端测试完成
        wait $REMOTE_PID
        echo "双向测试完成"
        ;;
        
    4)
        echo ""
        echo "=== 特定场景测试 ==="
        echo "请选择测试场景:"
        echo "1. 正常序列"
        echo "2. 乱序包"
        echo "3. 丢包场景"
        echo "4. 序列号回绕"
        echo "5. 重复包"
        echo "6. 大跳跃"
        echo ""
        
        read -p "请选择场景 (1-6): " scenario
        
        case $scenario in
            1) test_type="normal" ;;
            2) test_type="ooo" ;;
            3) test_type="loss" ;;
            4) test_type="wrap" ;;
            5) test_type="dup" ;;
            6) test_type="jump" ;;
            *) echo "无效选择"; exit 1 ;;
        esac
        
        echo ""
        echo "运行 $test_type 测试..."
        read -p "按Enter开始..."
        
        python3 rtp_test_sender.py $NEAR_END_IP $TEST_PORT --source "远端MCU" --test $test_type --interval 1.0
        ;;
        
    5)
        echo ""
        echo "=== 测试指南 ==="
        if [ -f "RTP_TESTING_GUIDE.md" ]; then
            cat RTP_TESTING_GUIDE.md
        else
            echo "测试指南文件不存在"
        fi
        ;;
        
    0)
        echo "退出测试"
        exit 0
        ;;
        
    *)
        echo "无效选择"
        exit 1
        ;;
esac

echo ""
echo "=== 测试完成 ==="
echo ""
echo "验证要点:"
echo "1. 检查turbo_relay的调试日志"
echo "2. 观察序列号窗口状态变化"
echo "3. 验证包转发行为是否符合预期"
echo "4. 检查统计信息是否正确"
echo ""
echo "如需查看详细测试指南，请运行: cat RTP_TESTING_GUIDE.md" 