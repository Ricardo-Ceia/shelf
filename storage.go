package shelf

import (
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"os"
	"path/filepath"
	"sync"
)

const walFileName = "wal.log"
const snapshotFileName = "snapshot.db"

type opCode byte

const (
	opSet opCode = 0x01
	opDel opCode = 0x02
)

type Store struct {
	ht                  *HashTable[string, []byte]
	dir                 string
	wal                 *os.File
	mu                  sync.Mutex
	writesSinceSnapshot int
	snapshotThreshold   int
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

func Open(dir string, numShards int, snapshotThreshold int) (*Store, error) {
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create directory: %w", err)
	}

	walPath := filepath.Join(dir, walFileName)
	f, err := os.OpenFile(walPath, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0600)
	if err != nil {
		return nil, fmt.Errorf("open WAL: %w", err)
	}

	ht := NewHashTable[string, []byte](numShards)

	snapshotPath := filepath.Join(dir, snapshotFileName)
	if data, err := os.ReadFile(snapshotPath); err == nil {
		if err := loadSnapshot(ht, data); err != nil {
			os.Remove(snapshotPath)
		}
	}

	data, err := os.ReadFile(walPath)
	if err != nil {
		f.Close()
		return nil, fmt.Errorf("read WAL: %w", err)
	}

	writesSinceSnapshot := 0

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

		writesSinceSnapshot++
	}

	if wr.pos < len(data) {
		f.Truncate(int64(wr.pos))
	}

	return &Store{
		ht:                  ht,
		dir:                 dir,
		wal:                 f,
		writesSinceSnapshot: writesSinceSnapshot,
		snapshotThreshold:   snapshotThreshold,
	}, nil
}

func loadSnapshot(ht *HashTable[string, []byte], data []byte) error {
	if len(data) < 4 {
		return fmt.Errorf("snapshot too short")
	}

	storedCRC := binary.BigEndian.Uint32(data[len(data)-4:])
	computedCRC := crc32.ChecksumIEEE(data[:len(data)-4])
	if storedCRC != computedCRC {
		return fmt.Errorf("snapshot CRC mismatch")
	}

	wr := &walReader{data: data[:len(data)-4]}

	numEntries, ok := wr.readUint32()
	if !ok {
		return fmt.Errorf("snapshot missing entry count")
	}

	for i := uint32(0); i < numEntries; i++ {
		keyLen, ok := wr.readUint32()
		if !ok {
			return fmt.Errorf("snapshot truncated at entry %d key_len", i)
		}

		key, ok := wr.readN(int(keyLen))
		if !ok {
			return fmt.Errorf("snapshot truncated at entry %d key", i)
		}

		valLen, ok := wr.readUint32()
		if !ok {
			return fmt.Errorf("snapshot truncated at entry %d val_len", i)
		}

		value, ok := wr.readN(int(valLen))
		if !ok {
			return fmt.Errorf("snapshot truncated at entry %d value", i)
		}

		ht.Insert(string(key), value)
	}

	return nil
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

	s.writesSinceSnapshot++
	if s.snapshotThreshold > 0 && s.writesSinceSnapshot >= s.snapshotThreshold {
		if err := s.snapshotLocked(); err != nil {
			return fmt.Errorf("auto-snapshot: %w", err)
		}
	}

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

	existed := s.ht.Delete(key)

	s.writesSinceSnapshot++
	if s.snapshotThreshold > 0 && s.writesSinceSnapshot >= s.snapshotThreshold {
		if err := s.snapshotLocked(); err != nil {
			return existed, fmt.Errorf("auto-snapshot: %w", err)
		}
	}

	return existed, nil
}

func (s *Store) Size() int {
	return s.ht.Size()
}

func (s *Store) Keys() []string {
	return s.ht.Keys()
}

func (s *Store) Snapshot() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.snapshotLocked()
}

func (s *Store) snapshotLocked() error {
	snapshotPath := filepath.Join(s.dir, snapshotFileName)
	tmpPath := snapshotPath + ".tmp"

	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0600)
	if err != nil {
		return fmt.Errorf("create snapshot temp file: %w", err)
	}

	keys := s.ht.Keys()
	numEntries := uint32(len(keys))

	header := make([]byte, 4)
	binary.BigEndian.PutUint32(header, numEntries)

	if _, err := f.Write(header); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write snapshot header: %w", err)
	}

	for _, key := range keys {
		value, _ := s.ht.Get(key)

		keyLen := uint32(len(key))
		valLen := uint32(len(value))

		entryBuf := make([]byte, 4+int(keyLen)+4+int(valLen))
		binary.BigEndian.PutUint32(entryBuf[0:4], keyLen)
		copy(entryBuf[4:4+int(keyLen)], key)
		binary.BigEndian.PutUint32(entryBuf[4+int(keyLen):8+int(keyLen)], valLen)
		if valLen > 0 {
			copy(entryBuf[8+int(keyLen):], value)
		}

		if _, err := f.Write(entryBuf); err != nil {
			f.Close()
			os.Remove(tmpPath)
			return fmt.Errorf("write snapshot entry: %w", err)
		}
	}

	data, err := os.ReadFile(tmpPath)
	if err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("read snapshot for CRC: %w", err)
	}

	crc := crc32.ChecksumIEEE(data)
	crcBytes := make([]byte, 4)
	binary.BigEndian.PutUint32(crcBytes, crc)

	if _, err := f.Write(crcBytes); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("write snapshot CRC: %w", err)
	}

	if err := f.Sync(); err != nil {
		f.Close()
		os.Remove(tmpPath)
		return fmt.Errorf("sync snapshot: %w", err)
	}

	if err := f.Close(); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("close snapshot: %w", err)
	}

	if err := os.Rename(tmpPath, snapshotPath); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename snapshot: %w", err)
	}

	if err := s.wal.Truncate(0); err != nil {
		return fmt.Errorf("truncate WAL: %w", err)
	}

	if _, err := s.wal.Seek(0, 0); err != nil {
		return fmt.Errorf("seek WAL: %w", err)
	}

	s.writesSinceSnapshot = 0

	return nil
}

func (s *Store) Close() error {
	if err := s.wal.Sync(); err != nil {
		return fmt.Errorf("sync WAL: %w", err)
	}
	return s.wal.Close()
}
