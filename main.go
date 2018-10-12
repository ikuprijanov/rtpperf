package main

import (
	"flag"
	"fmt"
	"math"
	"net"
	"time"

	"github.com/wernerd/GoRTP/src/net/rtp"
)

var (
	stopLocalRecv  chan bool
	payload        []byte
	localAddr      = flag.String(`local-addr`, `0.0.0.0`, `local IPv4 address`)
	localPort      = flag.Int(`local-port`, 19080, `local UDP port`)
	remoteAddr     = flag.String(`remote-addr`, `127.0.0.1`, `remote IPv4 address`)
	remotePort     = flag.Int(`remote-port`, 19080, `remote UDP port`)
	verbose        = flag.Bool(`verbose`, false, `Verbose output`)
	server         = flag.Bool(`server`, false, `Wait packets like server`)
	reportInterval = flag.Int(`interval`, 10, `Report interval`)
	maxJ           float64
	maxSkew        float64
	lost           int
	total          int
	waitClient     = make(chan bool)
)

func receivePacket(session *rtp.Session) {
	dataReceiver := session.CreateDataReceiveChan()
	var firstPacket *rtp.DataPacket
	var firstSequence uint16
	var lastSequence uint16
	var firstTimestamp time.Time
	var J float64
	var skew float64
	var packets [200]*rtp.DataPacket
	var timestamps [200]time.Time
	// var cnt int

	for {
		select {
		case packet := <-dataReceiver:
			timestamp := time.Now()
			if firstPacket == nil {
				firstPacket = packet
				firstSequence = packet.Sequence()
				firstTimestamp = timestamp
				lastSequence = packet.Sequence()
				close(waitClient)
			}
			if packet.Sequence() >= firstSequence+200 {
				for i := 0; i < 100; i++ {
					if packets[i] == nil {
						lost++
						if *verbose {
							fmt.Printf("Lost\n")
						}
					} else {
						if packets[i+1] == nil {
							if *verbose {
								fmt.Printf("????\n")
							}
						} else {
							rDelta := float64(timestamps[i+1].UnixNano()-timestamps[i].UnixNano()) / math.Pow10(6)
							pDelta := float64(packets[i+1].Timestamp()-packets[i].Timestamp()) / 8
							D := rDelta - pDelta
							J = J + (math.Abs(D)-J)/16
							if J > maxJ {
								maxJ = J
							}
							rD := float64(timestamps[i+1].UnixNano()-firstTimestamp.UnixNano()) / math.Pow10(6)
							pD := float64(packets[i+1].Timestamp()-firstPacket.Timestamp()) / 8
							skew = rD - pD
							if math.Abs(skew) > math.Abs(maxSkew) {
								maxSkew = skew
							}
							if *verbose {
								fmt.Printf("D: %.2f, J: %.2f, skew: %.2f\n", D, J, skew)
							}
						}
					}
				}
				for i := 0; i < 100; i++ {
					if packets[i] != firstPacket && packets[i] != nil {
						packets[i].FreePacket()
					}
					packets[i] = packets[i+100]
					timestamps[i] = timestamps[i+100]
					packets[i+100] = nil
					timestamps[i+100] = time.Time{}
				}
				firstSequence = firstSequence + 100
			}
			packets[packet.Sequence()-firstSequence] = packet
			timestamps[packet.Sequence()-firstSequence] = timestamp
			total = int(packet.Sequence()) - int(firstPacket.Sequence()) + 1
			if lastSequence < packet.Sequence() {
				lastSequence = packet.Sequence()
			}
			// fmt.Print(".")
		case <-stopLocalRecv:
			return
		}
	}
}

func sendPacket(session *rtp.Session, n uint32) {
	rp := session.NewDataPacket(n * 160)
	rp.SetPayload(payload)
	rp.SetSequence(uint16(n))
	session.WriteData(rp)
	rp.FreePacket()
}

func report() {
	ticker := time.NewTicker(time.Duration(*reportInterval) * time.Second)
	for {
		select {
		case ts := <-ticker.C:
			fmt.Printf("%s: maxJ: %.2f, maxSkew: %.2f, lost: %d (%.2f), total: %d\n", ts.Format("2006-01-02T15:04:05"), maxJ, maxSkew, lost, (float64(lost*100) / float64(total)), total)
		}
	}
}

func main() {
	var i byte
	var n uint32
	for i = 0; i < 160; i++ {
		payload = append(payload, i)
	}
	flag.Parse()

	local, err := net.ResolveIPAddr(`ip`, *localAddr)
	if err != nil {
		fmt.Printf(`Error localIp: %s`, err)
		return
	}
	remote, err := net.ResolveIPAddr(`ip`, *remoteAddr)
	if err != nil {
		fmt.Printf(`Error remoteIp: %s`, err)
		return
	}
	tpLocal, err := rtp.NewTransportUDP(local, *localPort, ``)
	if err != nil {
		fmt.Printf(`Error tpLocal: %s`, err)
		return
	}
	rsLocal := rtp.NewSession(tpLocal, tpLocal)
	strLocalIdx, sErr := rsLocal.NewSsrcStreamOut(&rtp.Address{local.IP, *localPort, *localPort + 1, ``}, 0, 0)
	if sErr != `` {
		fmt.Printf(`Error strLocalIdx: %s`, err)
		return
	}
	rsLocal.SsrcStreamOutForIndex(strLocalIdx).SetPayloadType(8)
	rsLocal.AddRemote(&rtp.Address{remote.IP, *remotePort, *remotePort + 1, ``})
	stopLocalRecv = make(chan bool)

	go receivePacket(rsLocal)
	rsLocal.StartSession()
	if *server {
		<-waitClient
	}
	go report()

	ticker := time.NewTicker(20 * time.Millisecond)
	for {
		select {
		case <-ticker.C:
			sendPacket(rsLocal, n)
			n++
		}
	}

}
