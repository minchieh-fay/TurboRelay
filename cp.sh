# 原有的测试脚本 (MCU发送 -> 终端接收)
scp mcu_send.py root@10.35.146.7:/root/ 
scp ter_receive.py root@10.35.146.7:/root/ 
scp -P 10810 mcu_send.py root@10.35.146.109:/root/
scp -P 10810 ter_receive.py root@10.35.146.109:/root/

# 新的反向测试脚本 (终端发送 -> MCU接收)
scp ter_send.py root@10.35.146.7:/root/
scp mcu_receive.py root@10.35.146.7:/root/
scp -P 10810 ter_send.py root@10.35.146.109:/root/
scp -P 10810 mcu_receive.py root@10.35.146.109:/root/