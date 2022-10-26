package rcd2

import (
	"fmt"
	"net"
	"net/http"
	"path/filepath"
)

func NewListener(in string) (net.Listener, error) {
	if filepath.IsAbs(in) {
		return net.Listen("unix", in)
	}
	return net.Listen("tcp", in)
}

func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {

		fmt.Println(r.RemoteAddr, r.URL)
	})
}
