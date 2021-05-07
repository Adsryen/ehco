package transporter

import (
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/Ehco1996/ehco/internal/constant"
	"github.com/Ehco1996/ehco/internal/lb"
	"github.com/Ehco1996/ehco/internal/logger"
	"github.com/Ehco1996/ehco/internal/web"
	"github.com/gobwas/ws"
)

type Raw struct {
	TCPRemotes     lb.RoundRobin
	UDPRemotes     lb.RoundRobin
	UDPBufferChMap map[string]*BufferCh
}

// NOTE not thread safe
func (raw *Raw) GetOrCreateBufferCh(uaddr *net.UDPAddr) *BufferCh {
	bc, found := raw.UDPBufferChMap[uaddr.String()]
	if !found {
		bc := newudpBufferCh()
		raw.UDPBufferChMap[uaddr.String()] = bc
		return bc
	}
	return bc
}

func (raw *Raw) HandleUDPConn(uaddr *net.UDPAddr, local *net.UDPConn) {
	web.CurUDPNum.Inc()
	defer web.CurUDPNum.Dec()

	bc := raw.GetOrCreateBufferCh(uaddr)
	remote := raw.UDPRemotes.Next()

	rc, err := net.Dial("udp", remote)
	if err != nil {
		logger.Info(err)
		return
	}
	defer func() {
		rc.Close()
		delete(raw.UDPBufferChMap, uaddr.String())
	}()

	logger.Infof("[raw] HandleUDPConn from %s to %s", local.LocalAddr().String(), remote)

	var wg sync.WaitGroup
	wg.Add(1)

	go func() {
		buf := OutboundBufferPool.Get().([]byte)
		wt := 0
		for {
			i, err := rc.Read(buf)
			if err != nil {
				logger.Info(err)
				break
			}
			rc.SetReadDeadline(time.Now().Add(constant.MaxConKeepAlive))
			if _, err := local.WriteToUDP(buf[0:i], uaddr); err != nil {
				logger.Info(err)
				break
			}
			wt += i
		}
		web.NetWorkTransmitBytes.Add(float64(wt * 2))
		OutboundBufferPool.Put(buf)
		wg.Done()
		close(bc.Ch)
	}()

	wt := 0
	for b := range bc.Ch {
		wt += len(b)
		rc.SetReadDeadline(time.Now().Add(constant.MaxConKeepAlive))
		if _, err := rc.Write(b); err != nil {
			logger.Info(err)
			close(bc.Ch)
			break
		}
	}
	web.NetWorkTransmitBytes.Add(float64(wt * 2))
	wg.Wait()
}

func (raw *Raw) HandleTCPConn(c *net.TCPConn) error {
	defer c.Close()
	web.CurTCPNum.Inc()
	defer web.CurTCPNum.Dec()
	remote := raw.TCPRemotes.Next()

	rc, err := net.Dial("tcp", remote)
	if err != nil {
		return err
	}
	logger.Infof("[raw] HandleTCPConn from %s to %s", c.LocalAddr().String(), remote)
	defer rc.Close()

	return transport(c, rc)
}

func (raw *Raw) HandleWsRequset(w http.ResponseWriter, req *http.Request) {
	web.CurTCPNum.Inc()
	defer web.CurTCPNum.Dec()
	wsc, _, _, err := ws.UpgradeHTTP(req, w)
	if err != nil {
		return
	}
	defer wsc.Close()
	remote := raw.TCPRemotes.Next()

	rc, err := net.Dial("tcp", remote)
	if err != nil {
		logger.Infof("dial error: %s", err)
		return
	}
	defer rc.Close()

	logger.Infof("[tun] HandleWsRequset from:%s to:%s", wsc.RemoteAddr(), rc.RemoteAddr())
	if err := transport(rc, wsc); err != nil {
		logger.Infof("[tun] HandleWsRequset err: %s", err.Error())
	}
}

func (raw *Raw) HandleWssRequset(w http.ResponseWriter, req *http.Request) {
	web.CurTCPNum.Inc()
	defer web.CurTCPNum.Dec()
	wsc, _, _, err := ws.UpgradeHTTP(req, w)
	if err != nil {
		return
	}
	defer wsc.Close()
	remote := raw.TCPRemotes.Next()

	rc, err := net.Dial("tcp", remote)
	if err != nil {
		logger.Infof("dial error: %s", err)
		return
	}
	defer rc.Close()

	logger.Infof("[tun] HandleWssRequset from:%s to:%s", wsc.RemoteAddr(), rc.RemoteAddr())
	if err := transport(rc, wsc); err != nil {
		logger.Infof("[tun] HandleWssRequset err: %s", err.Error())
	}
}

func (raw *Raw) HandleMWssRequset(c net.Conn) {
	web.CurTCPNum.Inc()
	defer web.CurTCPNum.Dec()
	defer c.Close()
	remote := raw.TCPRemotes.Next()

	rc, err := net.Dial("tcp", remote)
	if err != nil {
		logger.Infof("dial error: %s", err)
		return
	}
	defer rc.Close()

	logger.Infof("[tun] HandleMWssRequset from:%s to:%s", c.RemoteAddr(), rc.RemoteAddr())
	if err := transport(rc, c); err != nil {
		logger.Infof("[tun] HandleMWssRequset err: %s", err.Error())
	}
}
