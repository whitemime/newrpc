package codec

import (
	"bufio"
	"google.golang.org/protobuf/proto"
	"io"
	"log"
	"newrpc/protomsg"
)

type ProtobufCodec struct {
	conn io.ReadWriteCloser
	buf  *bufio.Writer
}

var _ Codec = (*ProtobufCodec)(nil)

func NewProtobufCodec(conn io.ReadWriteCloser) Codec {
	buf := bufio.NewWriter(conn)
	return &ProtobufCodec{
		conn: conn,
		buf:  buf,
	}
}

func (p *ProtobufCodec) ReadHeader(h *Header) error {
	pb := &protomsg.ProtoHeader{}
	data := make([]byte, 4)
	if _, err := io.ReadFull(p.conn, data); err != nil {
		return err
	}
	length := bytesToUint32(data)
	// 读取消息内容
	data = make([]byte, length)
	if _, err := io.ReadFull(p.conn, data); err != nil {
		return err
	}
	if err := proto.Unmarshal(data, pb); err != nil {
		return err
	}
	h.Seq = pb.Seq
	h.Error = pb.Error
	h.ServiceMethod = pb.ServiceMethod
	return nil
}
func (p *ProtobufCodec) ReadBody(body interface{}) error {
	//msg, ok := body.(proto.Message)
	//if !ok {
	//	return errors.New("protobuf codec requires proto.Message")
	//}
	data := make([]byte, 4)
	if _, err := io.ReadFull(p.conn, data); err != nil {
		return err
	}
	length := bytesToUint32(data)

	// 读取消息体
	data = make([]byte, length)
	if _, err := io.ReadFull(p.conn, data); err != nil {
		return err
	}

	// 反序列化
	return proto.Unmarshal(data, body.(proto.Message))
}
func (p *ProtobufCodec) Write(h *Header, body interface{}) (err error) {
	defer func() {
		_ = p.buf.Flush()
		if err != nil {
			_ = p.Close()
		}
	}()
	ph := &protomsg.ProtoHeader{ServiceMethod: h.ServiceMethod, Seq: h.Seq, Error: h.Error}
	headerData, err := proto.Marshal(ph)
	if err != nil {
		log.Println("rpc: protobuf error encoding header:", err)
		return err
	}
	// 写入Header长度前缀和数据
	lenData := uint32ToBytes(uint32(len(headerData)))
	if _, err = p.buf.Write(lenData); err != nil {
		return err
	}
	if _, err = p.buf.Write(headerData); err != nil {
		return err
	}

	// 处理Body
	//msg, ok := body.(proto.Message)
	//if !ok {
	//	return errors.New("rpc: protobuf codec requires body to be proto.Message type")
	//}

	// 序列化Body
	bodyData, err := proto.Marshal(body.(proto.Message))
	if err != nil {
		log.Println("rpc: protobuf error encoding body:", err)
		return err
	}

	// 写入Body长度前缀和数据
	lenData = uint32ToBytes(uint32(len(bodyData)))
	if _, err = p.buf.Write(lenData); err != nil {
		return err
	}
	if _, err = p.buf.Write(bodyData); err != nil {
		return err
	}

	return nil
}
func (p *ProtobufCodec) Close() error {
	return p.conn.Close()
}

// 辅助函数，将字节数组转换为uint32（大端序）
func bytesToUint32(b []byte) uint32 {
	return uint32(b[0])<<24 | uint32(b[1])<<16 | uint32(b[2])<<8 | uint32(b[3])
}

// 辅助函数，将uint32转换为字节数组（大端序）
func uint32ToBytes(n uint32) []byte {
	return []byte{
		byte(n >> 24),
		byte(n >> 16),
		byte(n >> 8),
		byte(n),
	}
}
