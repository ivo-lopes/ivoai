package context

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"golang.org/x/sys/unix"
)

// Catalog stores normalized document data. The vector index is rebuildable
// from this authoritative catalog and the configured connector sources.
type Catalog interface {
	ReplaceSource(string, []Document) error
	Get(string) (Document, bool, error)
	Recent(int) ([]Document, error)
	BySource(string) ([]Document, error)
	Delete([]string) error
	Count() (int, error)
}

type MemoryCatalog struct {
	mu   sync.RWMutex
	docs map[string]Document
}

func NewMemoryCatalog() *MemoryCatalog { return &MemoryCatalog{docs: make(map[string]Document)} }

func (c *MemoryCatalog) ReplaceSource(source string, documents []Document) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for id, document := range c.docs {
		if document.Source == source {
			delete(c.docs, id)
		}
	}
	for _, document := range documents {
		document.Metadata = cloneMap(document.Metadata)
		c.docs[document.ID] = document
	}
	return nil
}

func (c *MemoryCatalog) Get(id string) (Document, bool, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	doc, found := c.docs[id]
	doc.Metadata = cloneMap(doc.Metadata)
	return doc, found, nil
}

func (c *MemoryCatalog) Recent(limit int) ([]Document, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	docs := make([]Document, 0, len(c.docs))
	for _, doc := range c.docs {
		doc.Metadata = cloneMap(doc.Metadata)
		docs = append(docs, doc)
	}
	sort.Slice(docs, func(i, j int) bool { return docs[i].IngestedAt.After(docs[j].IngestedAt) })
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if len(docs) > limit {
		docs = docs[:limit]
	}
	return docs, nil
}

func (c *MemoryCatalog) Count() (int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.docs), nil
}

func (c *MemoryCatalog) BySource(source string) ([]Document, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	var documents []Document
	for _, document := range c.docs {
		if document.Source == source {
			document.Metadata = cloneMap(document.Metadata)
			documents = append(documents, document)
		}
	}
	return documents, nil
}

func (c *MemoryCatalog) Delete(ids []string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, id := range ids {
		delete(c.docs, id)
	}
	return nil
}

// FileCatalog persists the catalog using atomic replacement. Production assigns
// the writer and read-only gateway distinct users in one private service group.
type FileCatalog struct {
	Path string
	mu   sync.Mutex
}

func (c *FileCatalog) load() (map[string]Document, error) {
	docs := make(map[string]Document)
	fd, err := unix.Open(c.Path, unix.O_RDONLY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, unix.ENOENT) {
		return docs, nil
	}
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), c.Path)
	if file == nil {
		_ = unix.Close(fd)
		return nil, errors.New("open context catalog")
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("catalog path is not a regular file")
	}
	if info.Size() > 512<<20 {
		return nil, errors.New("context catalog exceeds safety limit")
	}
	decoder := json.NewDecoder(file)
	if err := decoder.Decode(&docs); err != nil {
		return nil, fmt.Errorf("decode context catalog: %w", err)
	}
	return docs, nil
}

func (c *FileCatalog) save(docs map[string]Document) error {
	dir := filepath.Dir(c.Path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	temp, err := os.CreateTemp(dir, ".catalog-*")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName)
	if err := temp.Chmod(0o640); err != nil {
		temp.Close()
		return err
	}
	encoder := json.NewEncoder(temp)
	encoder.SetEscapeHTML(true)
	if err := encoder.Encode(docs); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	if info, err := os.Lstat(c.Path); err == nil && info.Mode()&os.ModeSymlink != 0 {
		return errors.New("refusing to replace symlink catalog")
	}
	return os.Rename(tempName, c.Path)
}

func (c *FileCatalog) ReplaceSource(source string, documents []Document) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, err := c.load()
	if err != nil {
		return err
	}
	for id, document := range docs {
		if document.Source == source {
			delete(docs, id)
		}
	}
	for _, document := range documents {
		docs[document.ID] = document
	}
	return c.save(docs)
}

func (c *FileCatalog) Get(id string) (Document, bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, err := c.load()
	if err != nil {
		return Document{}, false, err
	}
	doc, found := docs[id]
	return doc, found, nil
}

func (c *FileCatalog) Recent(limit int) ([]Document, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, err := c.load()
	if err != nil {
		return nil, err
	}
	list := make([]Document, 0, len(docs))
	for _, doc := range docs {
		list = append(list, doc)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].IngestedAt.After(list[j].IngestedAt) })
	if limit <= 0 || limit > 100 {
		limit = 10
	}
	if len(list) > limit {
		list = list[:limit]
	}
	return list, nil
}

func (c *FileCatalog) Count() (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, err := c.load()
	return len(docs), err
}

func (c *FileCatalog) BySource(source string) ([]Document, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, err := c.load()
	if err != nil {
		return nil, err
	}
	var documents []Document
	for _, document := range docs {
		if document.Source == source {
			documents = append(documents, document)
		}
	}
	return documents, nil
}

func (c *FileCatalog) Delete(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	docs, err := c.load()
	if err != nil {
		return err
	}
	for _, id := range ids {
		delete(docs, id)
	}
	return c.save(docs)
}
