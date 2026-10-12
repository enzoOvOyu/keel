package platform

import (
	"errors"
	"net"
	"net/url"
	"strings"
	"time"
)

const IdleTimeout = 30 * time.Minute
const AbsoluteTimeout = 12 * time.Hour
const PreSessionTimeout = 15 * time.Minute
const LockoutWindow = 15 * time.Minute
const MaxLoginFailures = 5

type Config struct {
	Environment       string
	PublicOrigins     []string
	InsecureDevCookie bool
}

func (c Config) Validate() error {
	if c.Environment == "" {
		return errors.New("KEEL_ENV must be development, staging, or production")
	}
	if c.Environment != "development" && c.Environment != "staging" && c.Environment != "production" {
		return errors.New("invalid KEEL_ENV")
	}
	if len(c.PublicOrigins) == 0 {
		return errors.New("KEEL_PUBLIC_ORIGINS is required for configured authentication")
	}
	for _, origin := range c.PublicOrigins {
		u, e := url.Parse(origin)
		if e != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Path != "" || u.Opaque != "" {
			return errors.New("public origins must be exact scheme://host[:port]")
		}
		if c.InsecureDevCookie {
			if c.Environment != "development" || u.Scheme != "http" || !IsLoopbackHost(u.Hostname()) {
				return errors.New("insecure cookies require explicit development and HTTP loopback origins")
			}
		} else if u.Scheme != "https" {
			return errors.New("secure authentication requires HTTPS origins")
		}
	}
	return nil
}
func IsLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}
func (c Config) AllowedOrigin(origin string) bool {
	for _, v := range c.PublicOrigins {
		if origin == v {
			return true
		}
	}
	return false
}
func (c Config) AllowedHost(host string) bool {
	for _, v := range c.PublicOrigins {
		u, _ := url.Parse(v)
		if u != nil && u.Host == host {
			return true
		}
	}
	return false
}
