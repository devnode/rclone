package rcd

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/Unknwon/goconfig"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
)

const configFile = "rcd.conf"

var ErrNotFound = errors.New("does not exist")

func NewConfig() (*Config, error) {
	s := &Config{}

	mainConfigPath := config.GetConfigPath()
	baseDir := filepath.Dir(mainConfigPath)
	s.path = filepath.Join(baseDir, configFile)

	fmt.Println(s.path)

	return s, s.Load()
}

type Config struct {
	mu   sync.Mutex
	gc   *goconfig.ConfigFile
	fi   os.FileInfo
	path string
}

func (s *Config) Load() (err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s._load()
}

func (s *Config) _load() error {
	defer func() {
		if s.gc == nil {
			s.gc, _ = goconfig.LoadFromReader(bytes.NewReader([]byte{}))
		}
	}()

	fd, err := os.Open(s.path)
	if err != nil {
		return err
	}
	defer fs.CheckClose(fd, &err)

	// Update s.fi with the current file info
	s.fi, _ = os.Stat(s.path)

	gc, err := goconfig.LoadFromReader(fd)
	if err != nil {
		return err
	}
	s.gc = gc

	return nil
}

// HasSection returns true if section exists in the config file
func (s *Config) HasSection(section string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	_, err := s.gc.GetSection(section)
	return err == nil
}

// GetSectionList returns a slice of strings with names for all the
// sections
func (s *Config) GetSectionList() []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.gc.GetSectionList()
}

// GetKeyList returns the keys in this section
func (s *Config) GetKeyList(section string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.gc.GetKeyList(section)
}

// GetSectionValue returns the key in section with a found flag
func (s *Config) GetSectionValue(section string, key string) (value string, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, err := s.gc.GetValue(section, key)
	if err != nil {
		return "", false
	}
	return value, true
}

func (s *Config) GetValue(key string) (value string, found bool) {
	return s.GetSectionValue("", key)
}
