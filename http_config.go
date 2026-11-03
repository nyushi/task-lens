package main

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

type HTTPOptions struct {
	Listen       string   `json:"listen"`
	AllowRemote  bool     `json:"allow_remote"`
	AllowedHosts []string `json:"allowed_hosts"`
}

func (o HTTPOptions) validate() (string, error) {
	if o.Listen == "" {
		o.Listen = "127.0.0.1:8765"
	}
	host, port, e := net.SplitHostPort(o.Listen)
	if e != nil {
		return "", fmt.Errorf("listen must be numeric IP:port: %w", e)
	}
	ip := net.ParseIP(host)
	n, e := strconv.Atoi(port)
	if ip == nil || e != nil || n < 0 || n > 65535 {
		return "", fmt.Errorf("invalid listen IP or port")
	}
	if !ip.IsLoopback() && (!o.AllowRemote || len(o.AllowedHosts) == 0) {
		return "", fmt.Errorf("non-loopback listen requires allow_remote=true and explicit allowed_hosts; clients can read task details without authentication")
	}
	for _, authority := range o.AllowedHosts {
		h, p, e := net.SplitHostPort(authority)
		if e != nil || h == "" || p != port || n == 0 {
			return "", fmt.Errorf("allowed_hosts must contain exact host:listen-port (no wildcard or port 0)")
		}
		if net.ParseIP(h) == nil {
			for _, c := range h {
				if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '.' || c == '-') {
					return "", fmt.Errorf("invalid allowed host")
				}
			}
		}
	}
	return o.Listen, nil
}
func (o HTTPOptions) hosts(port int) map[string]bool {
	hosts := map[string]bool{}
	if len(o.AllowedHosts) == 0 {
		for _, h := range []string{"127.0.0.1", "localhost", "::1"} {
			hosts[net.JoinHostPort(h, strconv.Itoa(port))] = true
		}
	} else {
		for _, h := range o.AllowedHosts {
			hosts[strings.ToLower(h)] = true
		}
	}
	return hosts
}
