package licensing

import (
	"encoding/json"
	"errors"
	"os"
	"sync"
	"time"
)

// Record is the server-side representation of a subscription license.
type Record struct {
	Key         string    `json:"key"`
	Features    []string  `json:"features"`
	DEK         string    `json:"dek"` // hex-encoded AES-256 key for skill decryption
	ExpiresAt   time.Time `json:"expires_at"`
	MaxMachines int       `json:"max_machines"`
	Machines    []string  `json:"machines"`
	Revoked     bool      `json:"revoked"`
}

// Store persists license records.
type Store interface {
	Get(key string) (*Record, error)
	Save(record *Record) error
}

// FileStore is a JSON-file-backed Store.
type FileStore struct {
	path    string
	mu      sync.RWMutex
	records map[string]*Record
}

// NewFileStore creates or loads a store at path.
func NewFileStore(path string) (*FileStore, error) {
	s := &FileStore{
		path:    path,
		records: make(map[string]*Record),
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return nil, err
		}
		return s, nil
	}
	if err := json.Unmarshal(data, &s.records); err != nil {
		return nil, err
	}
	return s, nil
}

// Get returns a record by key.
func (s *FileStore) Get(key string) (*Record, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.records[key]
	if !ok {
		return nil, errors.New("license not found")
	}
	return r.clone(), nil
}

// Save writes a record and persists the store.
func (s *FileStore) Save(record *Record) error {
	s.mu.Lock()
	s.records[record.Key] = record
	s.mu.Unlock()
	return s.persist()
}

func (s *FileStore) persist() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s.records, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (r *Record) clone() *Record {
	c := *r
	c.Features = make([]string, len(r.Features))
	copy(c.Features, r.Features)
	c.Machines = make([]string, len(r.Machines))
	copy(c.Machines, r.Machines)
	return &c
}
