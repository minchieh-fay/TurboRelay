package rtp

import (
	"time"

	"github.com/google/gopacket"
)

// 远端过来的rtp包的缓存器 排序器
// 远端过来的rtp包
// 设置一个expectedSeq， 用于记录下一个期望的seq
// 如果进来的包和expectedSeq一致，直接发送
// 如果进来的包比expectedSeq大，则需要等待，存入map,但是如果lastSeqChangeTime已经超过300ms，递增expectedSeq，取一个存在的发出去
// 如果进来的包比expectedSeq小，直接丢弃（大概率是重复包）

type FarRTPPackeManger struct {
	expectedSeq uint16
	// expectedSeq 变动的时间
	lastSeqChangeTime time.Time
	packets           map[uint16]gopacket.Packet
}

func NewFarRTPPackeManger(expectedSeq uint16) *FarRTPPackeManger {
	// TODO:
	// expectedSeq第一次初始化的值 是不是本来就是乱序的
	// AddPacket会把小的seq直接丢掉
	// 所以以后考虑下 怎么处理刚建立的时候的数据处理
	return &FarRTPPackeManger{
		expectedSeq:       expectedSeq,
		lastSeqChangeTime: time.Now(),
		packets:           make(map[uint16]gopacket.Packet),
	}
}

// 这个函数完美的实现了 push和pop， push = 入参  pop=返回值
func (m *FarRTPPackeManger) AddPacket(packet gopacket.Packet, header RTPHeader) []gopacket.Packet {
	retpkts := make([]gopacket.Packet, 0)
	if header.SequenceNumber == m.expectedSeq { // 要的就是这个包，看看有没有和他连续的包 一起给他
		retpkts = append(retpkts, packet)
		delete(m.packets, m.expectedSeq) // 这句话大概率是多余的， 就怕上一次循环出现异常后，残留的包
		m.expectedSeq++
		// 判断下一个seq是否是连续的
		for {
			if _, exists := m.packets[m.expectedSeq]; exists {
				retpkts = append(retpkts, m.packets[m.expectedSeq])
				delete(m.packets, m.expectedSeq)
				m.expectedSeq++
			} else {
				break
			}
		}
		m.lastSeqChangeTime = time.Now()
	} else if header.SequenceNumber > m.expectedSeq { // 来的包比expectedSeq大
		// 先将当前包 存入map
		m.packets[header.SequenceNumber] = packet
		// 如果expect等待了300ms，递增expectedSeq，取一个存在的发出去
		if time.Since(m.lastSeqChangeTime) > 300*time.Millisecond { // 300ms 后，递增expectedSeq，取一个存在的发出去
			m.lastSeqChangeTime = time.Now()

			// 寻找下一个可用的包
			for len(m.packets) > 0 {
				if _, exists := m.packets[m.expectedSeq]; exists {
					// 找到了expectedSeq对应的包，发送它和所有连续的包
					retpkts = append(retpkts, m.packets[m.expectedSeq])
					delete(m.packets, m.expectedSeq)
					m.expectedSeq++

					// 继续发送连续的包
					for {
						if _, exists := m.packets[m.expectedSeq]; exists {
							retpkts = append(retpkts, m.packets[m.expectedSeq])
							delete(m.packets, m.expectedSeq)
							m.expectedSeq++
						} else {
							break
						}
					}
					break // 找到并处理了包，退出外层循环
				} else {
					// 当前expectedSeq没有对应的包，跳过并尝试下一个
					m.expectedSeq++
				}
			}
		}
	}
	// 这里省略了  比expectedSeq小的包， 直接丢弃
	return retpkts
}
