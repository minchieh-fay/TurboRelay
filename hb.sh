ssh -p 10810 root@10.35.147.52 "rm -rf /root/turbo_relay"
scp -P 10810 turbo_relay root@10.35.147.52:/root/
ssh -p 10810 root@10.35.147.52 "chmod +x /root/turbo_relay"
