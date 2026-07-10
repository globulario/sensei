// SPDX-License-Identifier: Apache-2.0

package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// ── Minimal CDP client (adapted from Globular MCP tools_browser.go) ─────────

type cdpClient struct {
	mu      sync.Mutex
	conn    *websocket.Conn
	nextID  int
	pending map[int]chan cdpResult
}

type cdpErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cdpMsg struct {
	ID     int             `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *cdpErr         `json:"error,omitempty"`
}

type cdpResult struct {
	result json.RawMessage
	err    error
}

func newCDP() *cdpClient {
	return &cdpClient{pending: make(map[int]chan cdpResult)}
}

func (c *cdpClient) connect(ctx context.Context, port int) error {
	listURL := fmt.Sprintf("http://localhost:%d/json", port)
	req, _ := http.NewRequestWithContext(ctx, "GET", listURL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return fmt.Errorf("Chrome not reachable on port %d: %w", port, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	var targets []struct {
		WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
		Type                 string `json:"type"`
		URL                  string `json:"url"`
	}
	json.Unmarshal(body, &targets)

	var wsURL string
	for _, t := range targets {
		if t.Type == "page" && !strings.HasPrefix(t.URL, "devtools://") && !strings.HasPrefix(t.URL, "chrome://") {
			wsURL = t.WebSocketDebuggerURL
			break
		}
	}
	if wsURL == "" && len(targets) > 0 {
		wsURL = targets[0].WebSocketDebuggerURL
	}
	if wsURL == "" {
		return fmt.Errorf("no debuggable targets on port %d", port)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 5 * time.Second}
	conn, _, err := dialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("CDP connect: %w", err)
	}
	c.conn = conn
	go c.readLoop()
	return nil
}

func (c *cdpClient) close() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn != nil {
		c.conn.Close()
		c.conn = nil
	}
}

func (c *cdpClient) send(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
	c.mu.Lock()
	if c.conn == nil {
		c.mu.Unlock()
		return nil, fmt.Errorf("not connected")
	}
	c.nextID++
	id := c.nextID
	var rawParams json.RawMessage
	if params != nil {
		b, _ := json.Marshal(params)
		rawParams = b
	}
	ch := make(chan cdpResult, 1)
	c.pending[id] = ch
	err := c.conn.WriteJSON(cdpMsg{ID: id, Method: method, Params: rawParams})
	c.mu.Unlock()
	if err != nil {
		c.removePending(id)
		return nil, err
	}
	select {
	case res := <-ch:
		return res.result, res.err
	case <-ctx.Done():
		c.removePending(id)
		return nil, ctx.Err()
	case <-time.After(15 * time.Second):
		c.removePending(id)
		return nil, fmt.Errorf("CDP %s timed out", method)
	}
}

func (c *cdpClient) readLoop() {
	for {
		c.mu.Lock()
		conn := c.conn
		c.mu.Unlock()
		if conn == nil {
			return
		}
		var msg cdpMsg
		if err := conn.ReadJSON(&msg); err != nil {
			c.failPending(err)
			return
		}
		c.dispatch(msg)
	}
}

func (c *cdpClient) dispatch(msg cdpMsg) {
	if msg.ID <= 0 {
		return
	}
	ch, ok := c.removePending(msg.ID)
	if !ok {
		return
	}
	if msg.Error != nil {
		ch <- cdpResult{err: fmt.Errorf("CDP error %d: %s", msg.Error.Code, msg.Error.Message)}
		return
	}
	ch <- cdpResult{result: msg.Result}
}

func (c *cdpClient) removePending(id int) (chan cdpResult, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	ch, ok := c.pending[id]
	if ok {
		delete(c.pending, id)
	}
	return ch, ok
}

func (c *cdpClient) failPending(err error) {
	c.mu.Lock()
	pending := c.pending
	c.pending = make(map[int]chan cdpResult)
	c.mu.Unlock()
	for _, ch := range pending {
		ch <- cdpResult{err: err}
	}
}

func (c *cdpClient) navigate(ctx context.Context, url string) error {
	_, err := c.send(ctx, "Page.navigate", map[string]string{"url": url})
	return err
}

func (c *cdpClient) evaluate(ctx context.Context, expr string) error {
	_, err := c.send(ctx, "Runtime.evaluate", map[string]interface{}{
		"expression":    expr,
		"returnByValue": true,
	})
	return err
}

func (c *cdpClient) screenshot(ctx context.Context) ([]byte, error) {
	result, err := c.send(ctx, "Page.captureScreenshot", map[string]string{"format": "png"})
	if err != nil {
		return nil, err
	}
	var resp struct {
		Data string `json:"data"`
	}
	json.Unmarshal(result, &resp)
	if resp.Data == "" {
		return nil, fmt.Errorf("empty screenshot")
	}
	return base64.StdEncoding.DecodeString(resp.Data)
}

// ── Visual audit command ────────────────────────────────────────────────────

func runVisualAudit(args []string) int {
	fs := flag.NewFlagSet("awg visual-audit", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	chromePort := fs.Int("chrome-port", 9222, "Chrome debugging port")
	routesStr := fs.String("routes", "", "comma-separated hash routes to screenshot")
	goldenDir := fs.String("golden-dir", ".awg/golden", "directory for golden screenshots")
	baseURL := fs.String("base-url", "http://localhost:5173", "app base URL")
	update := fs.Bool("update", false, "save current screenshots as new golden images")
	threshold := fs.Float64("threshold", 1.0, "pixel difference % to flag as FAIL")
	waitSec := fs.Int("wait", 3, "seconds to wait after navigation for page render")
	fs.Usage = func() {
		fmt.Fprint(os.Stderr, `Usage: awg visual-audit --routes "#/dashboard,#/cluster/nodes" [flags]

Capture screenshots of each route and compare against golden images.
Requires Chrome running with --remote-debugging-port.

Flags:
`)
		fs.PrintDefaults()
	}

	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *routesStr == "" {
		fs.Usage()
		return 2
	}

	routes := strings.Split(*routesStr, ",")
	for i := range routes {
		routes[i] = strings.TrimSpace(routes[i])
	}

	ctx := context.Background()
	cdp := newCDP()
	if err := cdp.connect(ctx, *chromePort); err != nil {
		fmt.Fprintf(os.Stderr, "visual-audit: %v\n", err)
		return 1
	}
	defer cdp.close()

	// Enable Page domain
	cdp.send(ctx, "Page.enable", nil)

	// Ensure golden dir exists
	os.MkdirAll(*goldenDir, 0755)

	type result struct {
		route   string
		status  string // PASS, FAIL, NEW, ERROR
		diffPct float64
		err     error
	}
	var results []result
	failures := 0

	for _, route := range routes {
		slug := strings.ReplaceAll(strings.TrimPrefix(route, "#/"), "/", "_")
		if slug == "" {
			slug = "root"
		}
		goldenPath := filepath.Join(*goldenDir, slug+".png")

		// Navigate
		fullURL := *baseURL + route
		if err := cdp.evaluate(ctx, fmt.Sprintf("window.location.href = %q", fullURL)); err != nil {
			results = append(results, result{route, "ERROR", 0, err})
			failures++
			continue
		}
		time.Sleep(time.Duration(*waitSec) * time.Second)

		// Screenshot
		pngData, err := cdp.screenshot(ctx)
		if err != nil {
			results = append(results, result{route, "ERROR", 0, err})
			failures++
			continue
		}

		if *update {
			if err := os.WriteFile(goldenPath, pngData, 0644); err != nil {
				results = append(results, result{route, "ERROR", 0, err})
				failures++
			} else {
				results = append(results, result{route, "UPDATED", 0, nil})
			}
			continue
		}

		// Compare with golden
		goldenData, err := os.ReadFile(goldenPath)
		if err != nil {
			results = append(results, result{route, "NEW", 0, nil})
			continue
		}

		diffPct := compareImages(goldenData, pngData)
		if diffPct > *threshold {
			results = append(results, result{route, "FAIL", diffPct, nil})
			// Save the failed screenshot for inspection
			failPath := filepath.Join(*goldenDir, slug+"_actual.png")
			os.WriteFile(failPath, pngData, 0644)
			failures++
		} else {
			results = append(results, result{route, "PASS", diffPct, nil})
		}
	}

	// Report
	fmt.Printf("visual-audit: %d routes checked\n", len(results))
	for _, r := range results {
		switch r.status {
		case "PASS":
			fmt.Printf("  %-40s PASS (%.1f%% diff)\n", r.route, r.diffPct)
		case "FAIL":
			fmt.Printf("  %-40s FAIL (%.1f%% diff -- threshold %.1f%%)\n", r.route, r.diffPct, *threshold)
		case "NEW":
			fmt.Printf("  %-40s NEW (no golden image -- run with --update)\n", r.route)
		case "UPDATED":
			fmt.Printf("  %-40s UPDATED (golden saved)\n", r.route)
		case "ERROR":
			fmt.Printf("  %-40s ERROR: %v\n", r.route, r.err)
		}
	}

	if failures > 0 {
		return 1
	}
	return 0
}

// compareImages decodes two PNGs and returns the percentage of pixels that differ.
func compareImages(goldenPNG, actualPNG []byte) float64 {
	goldenImg, err := png.Decode(strings.NewReader(string(goldenPNG)))
	if err != nil {
		return 100.0
	}
	actualImg, err := png.Decode(strings.NewReader(string(actualPNG)))
	if err != nil {
		return 100.0
	}

	gb := goldenImg.Bounds()
	ab := actualImg.Bounds()

	// Different dimensions = 100% diff
	if gb.Dx() != ab.Dx() || gb.Dy() != ab.Dy() {
		return 100.0
	}

	total := gb.Dx() * gb.Dy()
	if total == 0 {
		return 0
	}

	diffCount := 0
	for y := gb.Min.Y; y < gb.Max.Y; y++ {
		for x := gb.Min.X; x < gb.Max.X; x++ {
			gr, gg, gb2, _ := goldenImg.At(x, y).RGBA()
			ar, ag, ab2, _ := actualImg.At(x, y).RGBA()
			if gr != ar || gg != ag || gb2 != ab2 {
				diffCount++
			}
		}
	}

	return float64(diffCount) / float64(total) * 100.0
}

// decodeImage is a helper that wraps png.Decode with a bytes.Reader.
func decodeImage(data []byte) (image.Image, error) {
	return png.Decode(strings.NewReader(string(data)))
}
