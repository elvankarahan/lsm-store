package memtable

import (
	"bytes"
	"sync/atomic"

	"github.com/elvan/lsm-store/internal/keys"
	"github.com/elvan/lsm-store/internal/skiplist"
)

// MemTable holds recent writes in sorted order.
type MemTable struct {
	list *skiplist.SkipList[[]byte, []byte]
	size atomic.Int64
}

func New() *MemTable {
	return &MemTable{
		list: skiplist.New[[]byte, []byte](keys.Compare),
	}
}

func (m *MemTable) Add(seq uint64, kind keys.Kind, userKey, value []byte) {
	buf := make([]byte, 0, len(userKey)+keys.TrailerLen+len(value))
	buf = keys.Append(buf, userKey, seq, kind)
	key := buf[:len(userKey)+keys.TrailerLen]
	buf = append(buf, value...)
	val := buf[len(key):]
	m.list.Insert(key, val)
	m.size.Add(int64(len(key) + len(val) + 32))
}

func (m *MemTable) Get(userKey []byte, seq uint64) (value []byte, kind keys.Kind, ok bool) {
	it := m.list.NewIterator()
	it.SeekGE(keys.SeekKey(userKey, seq))
	if !it.Valid() {
		return nil, 0, false
	}

	foundKey, _, k, _ := keys.Decode(it.Key())
	if !bytes.Equal(foundKey, userKey) {
		return nil, 0, false
	}
	if k == keys.KindDelete {
		return nil, k, true
	}
	return it.Value(), k, true
}

func (m *MemTable) Len() int {
	return m.list.Len()
}

func (m *MemTable) ApproximateSize() int {
	return int(m.size.Load())
}

func (m *MemTable) NewIterator() *skiplist.Iterator[[]byte, []byte] {
	return m.list.NewIterator()
}
