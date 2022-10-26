package rcd

import (
	"bytes"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"time"

	"github.com/Unknwon/goconfig"
	"github.com/rclone/rclone/fs"
	"github.com/rclone/rclone/fs/config"
	"github.com/rclone/rclone/fs/rc/rcflags"
	"github.com/spf13/pflag"
)

const configFile = "rcd.conf"

var ErrNotFound = errors.New("does not exist")

func NewConfig() (*Config, error) {
	s := &Config{}

	mainConfigPath := config.GetConfigPath()
	baseDir := filepath.Dir(mainConfigPath)
	s.path = filepath.Join(baseDir, configFile)

	fmt.Println(s.path)

	s.Load()
	s.init(&rcflags.Opt, "")

	return s, nil
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

func (s *Config) GetSection(section string) (map[string]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.gc.GetSection(section)
}

// GetKeyList returns the keys in this section
func (s *Config) GetKeyList(section string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()

	return s.gc.GetKeyList(section)
}

// GetValue returns the key in section with a found flag
func (s *Config) GetValue(section string, key string) (value string, found bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	value, err := s.gc.GetValue(section, key)
	if err != nil {
		return "", false
	}
	return value, true
}

// TODO: fix this up
func (s *Config) init(target interface{}, prefix string) {
	values, err := s.gc.GetSection("DEFAULT")
	if err != nil {
		log.Fatalf("bad stuff: %s", err)
	}

	if len(values) == 0 {
		return
	}

	var t reflect.Value

	if v, ok := target.(reflect.Value); ok {
		t = v
	} else {
		t = reflect.ValueOf(target).Elem()
		if !t.CanAddr() {
			log.Println("can't addr, skipping...")
			return
		}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Type().Field(i)

		if prefix, ok := field.Tag.Lookup("prefix"); ok {
			// RECURSION
			s.init(t.Field(i), prefix)
			continue
		}

		tag, ok := field.Tag.Lookup("flag")
		if !ok {
			continue
		}

		if prefix != "" {
			tag = prefix + tag
		}

		value, ok := values[tag]
		if !ok {
			continue
		}

		flag := pflag.Lookup(tag)
		if flag == nil || flag.Changed {
			continue
		}

		f := t.Field(i)
		if !f.CanSet() {
			continue
		}

		// fmt.Println(flag.Name, f.CanSet(), field.Type.String(), value)

		switch field.Type.String() {
		case "string":
			f.SetString(value)

		case "int":
			if v, err := strconv.Atoi(value); err == nil {
				f.SetInt(int64(v))
			}

		case "time.Duration":
			if d, err := time.ParseDuration(value); err == nil {
				f.SetInt(int64(d))
			}
		}
	}
}

// rc
// rc-addr
// rc-allow-origin
// rc-baseurl
// rc-cert
// rc-client-ca
// rc-enable-metrics
// rc-files
// rc-htpasswd
// rc-job-expire-duration
// rc-job-expire-interval
// rc-key
// rc-max-header-bytes
// rc-net
// rc-no-auth
// rc-pass
// rc-realm
// rc-serve
// rc-server-read-timeout
// rc-server-write-timeout
// rc-template
// rc-user
// rc-web-fetch-url
// rc-web-gui
// rc-web-gui-force-update
// rc-web-gui-no-open-browser
// rc-web-gui-update
