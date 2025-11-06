package main

import (
	"fmt"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/gobwas/ws"
	"github.com/panjf2000/gnet/v2"
)

type (
	Conns struct {
		v map[gnet.Conn]struct{}
		sync.RWMutex
	}
	Server struct {
		*gnet.BuiltinEventEngine
		Routes
	}
	Routes []struct {
		P string
		H func(*RW) gnet.Action
	}
	RW struct {
		gnet.Conn
		bs []byte
		l  int
	}
	W struct {
		arr [10]byte
		l   int
	}
)

func (r *RW) Read(
	bs []byte,
) (int, error) {
	n := copy(bs, r.bs[r.l:])
	r.l += n
	return n, nil
}
func (w *W) Write(
	bs []byte,
) (int, error) {
	n := copy(w.arr[w.l:], bs)
	w.l += n
	return n, nil
}
func ResHdr(
	ver, status, ct string, cl int,
) (bs []byte) {
	const (
		dtK = "\r\nDate: "
		ctK = "\r\nContent-Type: "
		clK = "\r\nContent-Length: "
	)
	clStr := strconv.Itoa(cl)
	bs = make([]byte,
		len(ver)+1+len(status)+
			len(dtK)+len(http.TimeFormat)+
			len(ctK)+len(ct)+
			len(clK)+len(clStr)+4,
	)
	l := copy(bs, ver)
	bs[l] = ' '
	l++
	l += copy(bs[l:], status)
	l += copy(bs[l:], dtK)
	l += copy(bs[l:],
		time.Now().Format(
			http.TimeFormat,
		),
	)
	l += copy(bs[l:], ctK)
	l += copy(bs[l:], ct)
	l += copy(bs[l:], clK)
	l += copy(bs[l:], clStr)
	copy(bs[l:], "\r\n\r\n")
	return
}

var NotFound = []byte(
	"HTTP/1.1 404 NOT FOUND\r\n\r\n",
)

func Index(
	bs []byte, c byte, n int,
) (i int) {
	for n > 0 {
		for i < len(bs) && bs[i] != ' ' {
			i++
		}
		i++
		n--
	}
	return
}

type Htm []byte

func (body Htm) Func(
	rw *RW,
) gnet.Action {
	println("/")
	hdr := ResHdr(
		"HTTP/1.1", "200 OK",
		"text/html; charset=utf-8",
		len(body),
	)
	println("resHdr:", string(hdr))
	rw.AsyncWrite(hdr, func(
		c gnet.Conn, err error,
	) error {
		c.AsyncWrite(body, nil)
		return nil
	})
	return gnet.None
}

var conns = Conns{
	make(
		map[gnet.Conn]struct{}, 9999,
	),
	sync.RWMutex{},
}

func Ws(rw *RW) gnet.Action {
	println("/ws")
	_, err := ws.Upgrade(rw)
	if err != nil {
		println("err:", err.Error())
		return gnet.Close
	}
	rw.SetContext(struct{}{})
	println("u p g r a d e")
	conns.Lock()
	conns.v[rw.Conn] = struct{}{}
	conns.Unlock()
	return gnet.None
}
func Message(c gnet.Conn) gnet.Action {
	header, err := ws.ReadHeader(c)
	if err != nil {
		println(err.Error())
		return gnet.Close
	}
	fmt.Println("frame rd:", header)
	if header.OpCode == ws.OpClose {
		return gnet.Close
	}
	if header.Length != 0 {
		println(
			"payload len:", header.Length,
		)
		payload, _ := c.Next(
			int(header.Length),
		)
		if header.Masked {
			ws.Cipher(
				payload, header.Mask, 0,
			)
		}
		header.Masked = false
		w := &W{[10]byte{}, 0}
		ws.WriteHeader(w, header)
		fmt.Println("frame wr:", header)
		println("frame len:", w.l)
		println("payload:", string(payload))
		println("conns len:", len(conns.v))
		conns.RLock()
		for c, _ = range conns.v {
			conns.RUnlock()
			c.AsyncWrite(w.arr[:w.l], func(
				c gnet.Conn, err error,
			) error {
				c.AsyncWrite(payload, nil)
				return nil
			})
			conns.RLock()
		}
		conns.RUnlock()
	}
	return gnet.None
}
func (s *Server) OnTraffic(
	c gnet.Conn,
) gnet.Action {
	if c.Context() != nil {
		return Message(c)
	}
	buf, _ := c.Next(-1)
	if len(buf) == 0 {
		return gnet.None
	}
	println("buffer:", string(buf))
	path := string(
		buf[:Index(buf, ' ', 2)],
	)
	println("path:", path)
	l := len(s.Routes) - 1
	for l > -1 && s.Routes[l].P != path {
		l--
	}
	if l == -1 {
		c.AsyncWrite(NotFound, nil)
		return gnet.Close
	}
	return s.Routes[l].H(
		&RW{c, buf, 0},
	)
}
func (s *Server) OnClose(
	c gnet.Conn, _ error,
) gnet.Action {
	conns.Lock()
	delete(conns.v, c)
	conns.Unlock()
	return gnet.Close
}
func main() {
	rootHtm, _ := os.ReadFile(
		"root.html",
	)
	gnet.Run(
		&Server{
			nil,
			Routes{
				{"GET / ", Htm(rootHtm).Func},
				{"GET /ws ", Ws},
			},
		},
		"tcp://:9000",
		gnet.WithMulticore(true),
	)
}
