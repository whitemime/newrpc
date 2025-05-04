package xclient

import (
	"context"
	"io"
	"net/rpc"

	"newrpc"
	"reflect"
	"sync"
)

type XClient struct {
	d       Discovery      //服务发现实例
	mode    SelectMode     //负载均衡模式
	opt     *newrpc.Option //协议选项
	mu      sync.Mutex
	clients map[string]*rpc.Client
}

var _ io.Closer = (*XClient)(nil)

func NewXClient(d Discovery, mode SelectMode, opt *newrpc.Option) *XClient {
	return &XClient{d: d, mode: mode, opt: opt, clients: make(map[string]*rpc.Client)}
}
func (xc *XClient) Close() error {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	for k, c := range xc.clients {
		_ = c.Close()
		delete(xc.clients, k)
	}
	return nil
}
func (xc *XClient) dial(rpcAddr string) (*rpc.Client, error) {
	xc.mu.Lock()
	defer xc.mu.Unlock()
	client, ok := xc.clients[rpcAddr]
	if ok && !client.IsAvailable() {
		_ = client.Close()
		delete(xc.clients, rpcAddr)
		client = nil
	}
	if client == nil {
		var err error
		client, err = newrpc.XDial(rpcAddr, xc.opt)
		if err != nil {
			return nil, err
		}
		xc.clients[rpcAddr] = client
	}
	return client, nil
}
func (xc *XClient) call(ctx context.Context, serviceMethod string, args, reply interface{}, rpcAddr string) error {
	client, err := xc.dial(rpcAddr)
	if err != nil {
		return err
	}
	return client.Call(ctx, serviceMethod, args, reply)
}
func (xc *XClient) Call(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	rpcAddr, err := xc.d.Get(xc.mode)
	if err != nil {
		return err
	}
	return xc.call(ctx, serviceMethod, args, reply, rpcAddr)
}

// 将请求广播到所有服务实例
func (xc *XClient) Broadcast(ctx context.Context, serviceMethod string, args, reply interface{}) error {
	servers, err := xc.d.GetAll()
	if err != nil {
		return err
	}
	replyDone := reply == nil
	ctx, cancel := context.WithCancel(ctx)
	var mu sync.Mutex
	var e error
	var wg sync.WaitGroup
	for _, addr := range servers {
		wg.Add(1)
		go func(rpcAddr string) {
			defer wg.Done()
			var clonedReply interface{}
			if reply != nil {
				clonedReply = reflect.New(reflect.ValueOf(reply).Elem().Type()).Interface()
			}
			err := xc.call(ctx, serviceMethod, args, clonedReply, addr)
			mu.Lock()
			if err != nil && e == nil {
				e = err
				cancel() // if any call failed, cancel unfinished calls
			}
			if err == nil && !replyDone {
				reflect.ValueOf(reply).Elem().Set(reflect.ValueOf(clonedReply).Elem())
				replyDone = true
			}
			mu.Unlock()
		}(addr)
	}
	wg.Wait()
	return e
}
