package main

import (
	"context"
	"log"
	"net"
	"net/http"
	"net/rpc"
	"sync"
	"time"
)

type Foo int
type Args struct {
	Num1, Num2 int
}

func (f Foo) Sum(args Args, reply *int) error {
	*reply = args.Num2 + args.Num1
	return nil
}
func startServer(addr chan string) {
	var foo Foo
	l, _ := net.Listen("tcp", ":9999")
	_ = rpc.Register(&foo)
	rpc.HandleHTTP()
	addr <- l.Addr().String()
	_ = http.Serve(l, nil)
}
func call(addr chan string) {
	client, _ := rpc.DialHTTP("tcp", <-addr)
	defer client.Close()
	time.Sleep(time.Second)
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			args := &Args{Num1: i, Num2: i * i}
			ctx, _ := context.WithTimeout(context.Background(), time.Second)
			var reply int
			if err := client.Call(ctx, "Foo.Sum", args, &reply); err != nil {
				log.Fatal("call Foo.Sum error:", err)
			}
			log.Printf("%d + %d = %d", args.Num1, args.Num2, reply)
		}(i)
	}
	wg.Wait()
	//创建TCP连接，数据都通过conn的底层IO传输
	//conn, _ := net.Dial("tcp", <-addr)
	//defer conn.Close()
	//time.Sleep(time.Second)
	//_ = json.NewEncoder(conn).Encode(rpc.DefaultOption)
	//cc := codec.NewJsonCodec(conn)
	//for i := 0; i < 5; i++ {
	//	h := &codec.Header{
	//		ServiceMethod: "Foo.Sum",
	//		Seq:           uint64(i),
	//		Error:         "",
	//	}
	//	_ = cc.Write(h, fmt.Sprintf("geerpc req %d", h.Seq))
	//	_ = cc.ReadHeader(h)
	//	var reply string
	//	_ = cc.ReadBody(&reply)
	//	log.Println("reply:", reply)
	//}
}
func main() {
	log.SetFlags(0)
	ch := make(chan string)
	go call(ch)
	startServer(ch)
}
