package shelf

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
)

type opCode byte

const (
	opSet opCode = 0x01
	opDel opCode = 0x02
)

const walFileName = "wal.log"

type Store struct {
	ht  *HashTable[string, []byte]
	dir string
	wal *os.File
	mu  sync.Mutex
}

type walReader struct {
	data []byte
	pos  int
}

func (r *walReader) readByte() (byte, bool) {
	if r.pos >= len(r.data) {
		return 0, false
	}
	b := r.data[r.pos]
	r.pos++
	return b, true
}

func (r *walReader) readUint32() (uint32, bool) {
	if r.pos+4 > len(r.data) {
		return 0, false
	}
	v := binary.BigEndian.Uint32(r.data[r.pos : r.pos+4])
	r.pos += 4
	return v, true
}

func (r *walReader) readN(n int) ([]byte, bool) {
	if r.pos+n > len(r.data) {
		return nil, false
	}
	b := r.data[r.pos : r.pos+n]
	r.pos += n
	return b, true
}

func Open(dir string, numShards int) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	walPath := filepath.Join(dir, walFileName)
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)

	if err != nil {
		return nil, fmt.Errorf("open WAL: %w", err)
	}

	ht := NewHashTable[string, []byte](numShards)
	data, err := os.ReadFile(walPath)

	if err != nil {
		f.Close()
		return nil, fmt.Errorf("read WAL: %w", err)
	}

	wr := &walReader{data: data}

	for wr.pos < len(wr.data) {
		entryStart := wr.pos
		op, ok := wr.readByte()

		if !ok {
			break
		}

		if op != byte(opSet) && op != byte(opDel) {
			break
		}

		keyLen, ok := wr.readUint32()

		if !ok {
			break
		}

		key, ok := wr.readN(int(keyLen))

		if !ok {
			break
		}

		valLen, ok := wr.readUint32()

		if !ok {
			break
		}

		value, ok := wr.readN(int(valLen))

		if !ok {
			break
		}

		storedCRC, ok := wr.readUint32()

		if !ok {
			break
		}

		computedCRC := crc32.ChecksumIEEE(data[entryStart : wr.pos-4])

		if storedCRC != computedCRC {
			break
		}

		switch opCode(op) {
		case opSet:
			ht.Insert(string(key), value)
		case opDel:
			ht.Delete(string(key))
		}
	}

	if wr.pos < len(data) {
		f.Truncate(int64(wr.pos))
	}

	return &Store{
		ht:  ht,
		dir: dir,
		wal: f,
	}, nil
}

func (s *Store) Set(key string, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := make([]byte, 1+4+len(key)+4+len(value)+4)
	entry[0] = byte(opSet)
	binary.BigEndian.PutUint32(entry[1:5], uint32(len(key)))
	copy(entry[5:5+len(key)], key)
	binary.BigEndian.PutUint32(entry[5+len(key):9+len(key)], uint32(len(value)))
	copy(entry[9+len(key):9+len(key)+len(value)], value)
	crc := crc32.ChecksumIEEE(entry[:9+len(key)+len(value)])
	binary.BigEndian.PutUint32(entry[9+len(key)+len(value):], crc)

	if _, err := s.wal.Write(entry); err != nil {
		return fmt.Errorf("write WAL: %w", err)
	}

	s.ht.Insert(key, value)
	return nil
}

func (s *Store) Get(key string) ([]byte, bool) {
	return s.ht.Get(key)
}

func (s *Store) Delete(key string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	entry := make([]byte, 1+4+len(key)+4+0+4)
	entry[0] = byte(opDel)
	binary.BigEndian.PutUint32(entry[1:5], uint32(len(key)))
	copy(entry[5:5+len(key)], key)
	binary.BigEndian.PutUint32(entry[5+len(key):9+len(key)], 0)
	crc := crc32.ChecksumIEEE(entry[:9+len(key)])
	binary.BigEndian.PutUint32(entry[9+len(key):], crc)

	if _, err := s.wal.Write(entry); err != nil {
		return false, fmt.Errorf("write WAL: %w", err)
	}

	return s.ht.Delete(key), nil
}

func (s *Store) Size() int {
	return s.ht.Size()
}

func (s *Store) Keys() []string {
	return s.ht.Keys()
}

func (s *Store) Close() error {
	if err := s.wal.Sync(); err != nil {
		return fmt.Errorf("sync WAL: %w", err)
	}
	return s.wal.Close()
}
