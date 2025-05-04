package codec

import "io"

// 先定义RPC请求的头部信息
type Header struct {
	ServiceMethod string //服务方法名
	Seq           uint64 //请求序列号
	Error         string //错误信息
}

// 定义编解码器要实现的方法
type Codec interface {
	io.Closer
	ReadHeader(*Header) error
	ReadBody(interface{}) error
	Write(*Header, interface{}) error
}

// 创建编解码器的函数的类型
type NewCodecFunc func(closer io.ReadWriteCloser) Codec

// 支持的编解码器类型
type Type string

const (
	ProtobufType Type = "application/protobuf"
)

// 存不同的编解码器的创建函数
var NewCodecFuncMap map[Type]NewCodecFunc

func init() {
	NewCodecFuncMap = make(map[Type]NewCodecFunc)
	NewCodecFuncMap[ProtobufType] = NewProtobufCodec
}
