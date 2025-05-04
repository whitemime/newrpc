package newrpc

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"reflect"

	"newrpc/codec"
	"strings"
	"sync"
	"time"
)

const MagicNumber = 0x3bef5c

// 客户端和服务端通信时需要协商，例如http，body的格式和长度就是从head中获取的
// 对于rpc来说这部分要自定义
// 对于简易rpc来说，规定报文的第一个一定是option,客户端发送json编码的option,option中有head和body的编码方式
// 服务端json解码option，然后通过option中规定的解码格式解码其余内容

type Option struct {
	MagicNumber    int
	CodecType      codec.Type
	ConnectTimeout time.Duration //连接时间限制
	HandleTimeout  time.Duration //服务端处理时间限制
}

var DefaultOption = &Option{
	MagicNumber:    MagicNumber,
	CodecType:      codec.ProtobufType,
	ConnectTimeout: time.Second * 10,
}

// 服务端实现

func NewServer() *Server {
	return &Server{}
}

var DefaultServer = NewServer()

const (
	connected        = "200 Connected to Gee RPC"
	defaultRPCPath   = "/_geeprc_"
	defaultDebugPath = "/debug/geerpc"
)

// 客户端发来connect请求，服务端接收返回建立连接成功，然后接着处理RPC请求
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != "CONNECT" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.WriteHeader(http.StatusMethodNotAllowed)
		_, _ = io.WriteString(w, "405 must CONNECT\n")
		return
	}
	//拦截http请求得到底层tcp连接
	conn, _, err := w.(http.Hijacker).Hijack()
	if err != nil {
		log.Print("rpc hijacking ", r.RemoteAddr, ": ", err.Error())
		return
	}
	//返回一个简单的http响应，表示连接建立成功
	_, _ = io.WriteString(conn, "HTTP/1.0 "+connected+"\n\n")
	s.ServerConn(conn)
}
func (s *Server) HandleHTTP() {
	http.Handle(defaultRPCPath, s)
}
func HandleHTTP() {
	DefaultServer.HandleHTTP()
}
func (s *Server) Accept(lis net.Listener) {
	//等待socket连接建立
	for {
		//会阻塞并且等待接入网络连接
		//有客户端的请求时会返回一个网络连接conn
		conn, err := lis.Accept()
		if err != nil {
			log.Println("rpc server: accept error:", err)
			return
		}
		//开启子协程处理
		go s.ServerConn(conn)
	}
}
func Accept(lis net.Listener) { DefaultServer.Accept(lis) }

func (s *Server) ServerConn(conn io.ReadWriteCloser) {
	defer func() {
		_ = conn.Close()
	}()
	var option Option
	if err := json.NewDecoder(conn).Decode(&option); err != nil {
		log.Println("rpc server: options error: ", err)
		return
	}
	if option.MagicNumber != MagicNumber {
		log.Printf("rpc server: invalid magic number %x", option.MagicNumber)
		return
	}
	f := codec.NewCodecFuncMap[option.CodecType]
	if f == nil {
		log.Printf("rpc server: invalid codec type %s", option.CodecType)
		return
	}
	s.ServerCodec(f(conn))
}

var invalidRequest = struct{}{}

func (s *Server) ServerCodec(cc codec.Codec) {
	sending := new(sync.Mutex)
	wg := new(sync.WaitGroup)
	//一次连接中可能收到多个请求，for无限等待请求，可并发处理请求，但回复请求必须一个一个发，这里用互斥锁保证
	for {
		req, err := s.readRequest(cc)
		if err != nil {
			if req == nil {
				break
			}
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			continue
		}
		wg.Add(1)
		go s.handleRequest(cc, req, sending, wg, time.Second)
	}
	wg.Wait()
	_ = cc.Close()
}

type request struct {
	h            *codec.Header
	argv, replyv reflect.Value
	mtype        *methodType
	svc          *service
}

func (s *Server) readRequestHeader(cc codec.Codec) (*codec.Header, error) {
	var h codec.Header
	if err := cc.ReadHeader(&h); err != nil {
		if err != io.EOF && err != io.ErrUnexpectedEOF {
			log.Println("rpc server: read header error:", err)
		}
		return nil, err
	}
	return &h, nil
}
func (s *Server) readRequest(cc codec.Codec) (*request, error) {
	h, err := s.readRequestHeader(cc)
	if err != nil {
		return nil, err
	}
	req := &request{h: h}
	req.svc, req.mtype, err = s.findService(h.ServiceMethod)
	if err != nil {
		return req, err
	}
	req.argv = req.mtype.newArgv()
	req.replyv = req.mtype.newReplyv()
	argvi := req.argv.Interface()
	if req.argv.Type().Kind() != reflect.Ptr {
		argvi = req.argv.Addr().Interface()
	}
	if err = cc.ReadBody(argvi); err != nil {
		log.Println("rpc server: read body err:", err)
		return req, err
	}
	return req, nil
}
func (s *Server) sendResponse(cc codec.Codec, h *codec.Header, body interface{}, sending *sync.Mutex) {
	sending.Lock()
	defer sending.Unlock()
	if err := cc.Write(h, body); err != nil {
		log.Println("rpc server: write response error:", err)
	}
}
func (s *Server) handleRequest(cc codec.Codec, req *request, sending *sync.Mutex, wg *sync.WaitGroup, timeout time.Duration) {
	defer wg.Done()
	call := make(chan struct{})
	sent := make(chan struct{})
	go func() {
		err := req.svc.Call(req.mtype, req.argv, req.replyv)
		call <- struct{}{}
		if err != nil {
			req.h.Error = err.Error()
			s.sendResponse(cc, req.h, invalidRequest, sending)
			sent <- struct{}{}
			return
		}
		s.sendResponse(cc, req.h, req.replyv.Interface(), sending)
		sent <- struct{}{}
	}()
	if timeout == 0 {
		<-call
		<-sent
	}
	select {
	case <-time.After(timeout):
		req.h.Error = fmt.Sprintf("rpc server: request handle timeout: expect within %s", timeout)
		s.sendResponse(cc, req.h, invalidRequest, sending)
	case <-call:
		<-sent
	}
}

type Server struct {
	serviceMap sync.Map
}

func (s *Server) Register(rcvr interface{}) error {
	se := newService(rcvr)
	if _, dup := s.serviceMap.LoadOrStore(se.name, se); dup {
		return errors.New("rpc: service already defined: " + se.name)
	}
	return nil
}
func Register(rcvr interface{}) error {
	return DefaultServer.Register(rcvr)
}
func (s *Server) findService(serviceMethod string) (svc *service, mtype *methodType, err error) {
	dot := strings.LastIndex(serviceMethod, ".")
	if dot < 0 {
		err = errors.New("rpc server: service/method request ill-formed: " + serviceMethod)
		return
	}
	serviceName, methodName := serviceMethod[:dot], serviceMethod[dot+1:]
	svci, ok := s.serviceMap.Load(serviceName)
	if !ok {
		err = errors.New("rpc server: can't find service " + serviceName)
		return
	}
	svc = svci.(*service)
	mtype = svc.method[methodName]
	if mtype == nil {
		err = errors.New("rpc server: can't find method " + methodName)
	}
	return
}
