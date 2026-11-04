package server

import (
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// limiter is a set of token buckets, one per key (a client address, an identity). Each bucket holds up to
// `burst` tokens and refills at `perMinute` tokens a minute. In memory only: a restart forgets it.
type limiter struct {
	mu        sync.Mutex
	perMinute float64
	burst     float64
	buckets   map[string]*bucket
	now       func() time.Time
}

type bucket struct {
	tokens float64
	at     time.Time
}

func newLimiter(perMinute int, now func() time.Time) *limiter {
	if perMinute <= 0 {
		return nil // off
	}
	if now == nil {
		now = time.Now
	}
	return &limiter{perMinute: float64(perMinute), burst: float64(perMinute), buckets: map[string]*bucket{}, now: now}
}

// refill brings the bucket of key up to date. Callers hold l.mu.
func (l *limiter) refill(key string) *bucket {
	t := l.now()
	b, ok := l.buckets[key]
	if !ok {
		if len(l.buckets) > 4096 { // forget buckets that are full again: they carry no information
			for k, o := range l.buckets {
				if o.tokens+t.Sub(o.at).Minutes()*l.perMinute >= l.burst {
					delete(l.buckets, k)
				}
			}
		}
		b = &bucket{tokens: l.burst, at: t}
		l.buckets[key] = b
		return b
	}
	b.tokens += t.Sub(b.at).Minutes() * l.perMinute
	if b.tokens > l.burst {
		b.tokens = l.burst
	}
	b.at = t
	return b
}

// peek reports whether key may act now, without using a token; retry is how long until it may.
func (l *limiter) peek(key string) (ok bool, retry time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key)
	if b.tokens >= 1 {
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.perMinute * float64(time.Minute))
}

// take uses one token if there is one.
func (l *limiter) take(key string) (ok bool, retry time.Duration) {
	if l == nil {
		return true, 0
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	b := l.refill(key)
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.perMinute * float64(time.Minute))
}

// clientAddr is who is calling: the connection's address, or with trust_proxy the last address the
// proxy put in X-Forwarded-For (the one it saw; earlier ones are written by the client and can be faked).
func clientAddr(r *http.Request, trustProxy bool) string {
	if trustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func tooMany(w http.ResponseWriter, retry time.Duration, why string) {
	secs := int(retry.Seconds()) + 1
	w.Header().Set("Retry-After", itoa(secs))
	writeError(w, http.StatusTooManyRequests, why+": retry in "+itoa(secs)+" s")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
