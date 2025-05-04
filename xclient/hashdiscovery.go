package xclient

import (
	"hash/crc32"
	"sort"
	"strconv"
)

type HashDiscovery interface {
	AddNode(...string)
	GetNode(string) string
	RemoveNode(string)
}
type Hash func(data []byte) uint32
type HashMap struct {
	keys     []uint32          //放置节点的哈希环
	nodeMap  map[uint32]string //每个哈希值和真实节点的映射
	replicas int               //一个真实节点有这些个虚拟节点
	//哈希函数
	hash Hash
}

var _ HashDiscovery = (*HashMap)(nil)

func NewHashMap(re int, f Hash) *HashMap {
	m := &HashMap{
		keys:     make([]uint32, 0),
		nodeMap:  make(map[uint32]string),
		replicas: re,
		hash:     f,
	}
	if f == nil {
		m.hash = crc32.ChecksumIEEE
	}
	return m
}
func (h *HashMap) AddNode(key ...string) {
	for _, v := range key {
		//添加虚拟节点
		for i := 0; i < h.replicas; i++ {
			hs := h.hash([]byte(strconv.Itoa(i) + v))
			h.keys = append(h.keys, hs)
			h.nodeMap[hs] = v
		}
	}
	sort.Slice(h.keys, func(i, j int) bool {
		return h.keys[i] < h.keys[j]
	})
}
func (h *HashMap) RemoveNode(key string) {
	for i := 0; i < h.replicas; i++ {
		hs := h.hash([]byte(strconv.Itoa(i) + key))
		for j, v := range h.keys {
			if v == hs {
				h.keys = append(h.keys[:j], h.keys[j+1:]...)
			}
		}
		delete(h.nodeMap, hs)
	}
}
func (h *HashMap) GetNode(key string) string {
	hs := h.hash([]byte(key))
	id := sort.Search(len(h.keys), func(i int) bool {
		return h.keys[i] >= hs
	})
	return h.nodeMap[h.keys[id%len(h.keys)]]
}
