package monitor

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"time"
)

// ProbeResult is the outcome of a single probe attempt.
type ProbeResult struct {
	Up      bool
	Latency time.Duration
	Error   string
}

// Prober is the interface for all probe type implementations.
type Prober interface {
	Probe(ctx context.Context, target string) ProbeResult
}

// --- HTTP Prober ---

type HTTPProber struct {
	IgnoreTLS bool
}

func (p *HTTPProber) Probe(ctx context.Context, target string) ProbeResult {
	start := time.Now()

	transport := &http.Transport{
		TLSClientConfig: &tls.Config{InsecureSkipVerify: p.IgnoreTLS},
	}
	client := &http.Client{Transport: transport}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target, nil)
	if err != nil {
		return ProbeResult{Up: false, Error: fmt.Sprintf("create request: %v", err)}
	}

	resp, err := client.Do(req)
	if err != nil {
		return ProbeResult{
			Up:      false,
			Latency: time.Since(start),
			Error:   fmt.Sprintf("request failed: %v", err),
		}
	}
	defer resp.Body.Close()
	latency := time.Since(start)

	if resp.StatusCode >= 400 {
		return ProbeResult{
			Up:      false,
			Latency: latency,
			Error:   fmt.Sprintf("HTTP %d", resp.StatusCode),
		}
	}

	return ProbeResult{Up: true, Latency: latency}
}

// --- TCP Prober ---

type TCPProber struct{}

func (p *TCPProber) Probe(ctx context.Context, target string) ProbeResult {
	start := time.Now()

	var d net.Dialer
	conn, err := d.DialContext(ctx, "tcp", target)
	if err != nil {
		return ProbeResult{
			Up:      false,
			Latency: time.Since(start),
			Error:   fmt.Sprintf("tcp dial: %v", err),
		}
	}
	conn.Close()

	return ProbeResult{Up: true, Latency: time.Since(start)}
}

// --- ICMP Ping Prober (system ping) ---

type ICMPProber struct{}

// pingLatencyRe matches RTT from ping output across platforms.
// Linux:   rtt min/avg/max/mdev = 1.234/1.234/1.234/0.000 ms
// macOS:   round-trip min/avg/max/stddev = 1.234/1.234/1.234/0.000 ms
// Windows: Average = 1ms
var pingLatencyRe = regexp.MustCompile(`(?:rtt|round-trip).*?=\s*[\d.]+/([\d.]+)/|Average\s*=\s*(\d+)\s*ms`)

// Probe calls the system ping command and parses the result.
func (p *ICMPProber) Probe(ctx context.Context, target string) ProbeResult {
	var args []string
	if runtime.GOOS == "windows" {
		args = []string{"ping", "-n", "1", "-w", "5000", target}
	} else {
		args = []string{"ping", "-c", "1", "-W", "5", target}
	}

	start := time.Now()
	cmd := exec.CommandContext(ctx, args[0], args[1:]...)
	out, err := cmd.CombinedOutput()
	latency := time.Since(start)

	if err != nil {
		return ProbeResult{Up: false, Latency: latency, Error: fmt.Sprintf("ping: %v", err)}
	}

	// Parse latency from ping output.
	if m := pingLatencyRe.FindSubmatch(out); m != nil {
		s := string(m[1])
		if s == "" {
			s = string(m[2])
		}
		if ms, err := strconv.ParseFloat(s, 64); err == nil {
			latency = time.Duration(ms*1000) * time.Microsecond
		}
	}

	return ProbeResult{Up: true, Latency: latency}
}

// NewProber creates the appropriate prober for a monitor type.
func NewProber(monitorType string, ignoreTLS bool) Prober {
	switch monitorType {
	case "http":
		return &HTTPProber{IgnoreTLS: ignoreTLS}
	case "tcp":
		return &TCPProber{}
	case "ping":
		return &ICMPProber{}
	default:
		return &HTTPProber{}
	}
}
