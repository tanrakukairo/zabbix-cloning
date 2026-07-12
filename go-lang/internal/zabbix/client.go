package zabbix

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type Client struct {
	endpoint  string
	frontend  string
	token     string
	http      *http.Client
	requestID atomic.Uint64
	dryRunMu  sync.Mutex
	dryRun    bool
	dryRunOps map[string]int
}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params"`
	Auth    string `json:"auth,omitempty"`
	ID      uint64 `json:"id"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
	Error   *RPCError       `json:"error"`
	ID      uint64          `json:"id"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    string `json:"data"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("Zabbix API error %d: %s: %s", e.Code, e.Message, e.Data)
}

type Version struct{ Major, Minor, Patch int }

func ParseVersion(value string) Version {
	parts := strings.Split(value, ".")
	version := Version{}
	if len(parts) > 0 {
		version.Major, _ = strconv.Atoi(parts[0])
	}
	if len(parts) > 1 {
		version.Minor, _ = strconv.Atoi(parts[1])
	}
	if len(parts) > 2 {
		version.Patch, _ = strconv.Atoi(parts[2])
	}
	return version
}

func (v Version) Float() float64 { return float64(v.Major) + float64(v.Minor)/10 }
func (v Version) String() string { return fmt.Sprintf("%d.%d.%d", v.Major, v.Minor, v.Patch) }
func (v Version) AtLeast(major, minor int) bool {
	return v.Major > major || v.Major == major && v.Minor >= minor
}

func New(endpoint string, insecure bool) (*Client, error) {
	frontend := strings.TrimRight(endpoint, "/")
	api := frontend
	if !strings.HasSuffix(api, "api_jsonrpc.php") {
		api += "/api_jsonrpc.php"
	}
	if _, err := url.ParseRequestURI(api); err != nil {
		return nil, fmt.Errorf("invalid Zabbix endpoint: %w", err)
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec -- explicit CLI option
	return &Client{
		endpoint: api, frontend: frontend,
		http: &http.Client{Transport: transport, Timeout: 120 * time.Second}, dryRunOps: map[string]int{},
	}, nil
}

func (c *Client) SetDryRun(enabled bool) {
	c.dryRunMu.Lock()
	defer c.dryRunMu.Unlock()
	c.dryRun = enabled
}

func (c *Client) DryRunMethods() map[string]int {
	c.dryRunMu.Lock()
	defer c.dryRunMu.Unlock()
	result := make(map[string]int, len(c.dryRunOps))
	for method, count := range c.dryRunOps {
		result[method] = count
	}
	return result
}

func (c *Client) Authenticate(ctx context.Context, token, user, password string) error {
	if token != "" {
		c.token = token
		if _, err := c.Call(ctx, "user.get", map[string]any{"output": []string{"userid"}, "limit": 1}); err == nil {
			return nil
		} else if password == "" {
			c.token = ""
			return fmt.Errorf("token authentication failed: %w", err)
		}
		c.token = ""
	}
	if user == "" || password == "" {
		return fmt.Errorf("no valid Zabbix credentials")
	}
	result, err := c.Call(ctx, "user.login", map[string]any{"username": user, "password": password})
	if err != nil {
		result, err = c.Call(ctx, "user.login", map[string]any{"user": user, "password": password})
	}
	if err != nil {
		return err
	}
	c.token = stringResult(result)
	if c.token == "" {
		return fmt.Errorf("user.login returned an empty token")
	}
	return nil
}

func (c *Client) CheckServerName(ctx context.Context, expected, token, user, password string) error {
	if expected == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.frontend+"/index.php?form=default", nil)
	if err != nil {
		return err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	} else if user != "" {
		req.Header.Set("Authorization", "Basic "+base64.StdEncoding.EncodeToString([]byte(user+":"+password)))
	}
	response, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("get Zabbix server name: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("get Zabbix server name: HTTP %s", response.Status)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 4<<20))
	if err != nil {
		return err
	}
	pattern := regexp.MustCompile(`<div class="server-name">\s*([^<]+?)\s*</div>`)
	match := pattern.FindSubmatch(body)
	if len(match) != 2 {
		return fmt.Errorf("Zabbix server name not found")
	}
	actual := strings.TrimSpace(string(match[1]))
	if actual != expected {
		return fmt.Errorf("wrong target node %q (expected %q)", actual, expected)
	}
	return nil
}

func (c *Client) Version(ctx context.Context) (Version, error) {
	result, err := c.Call(ctx, "apiinfo.version", map[string]any{})
	if err != nil {
		return Version{}, err
	}
	return ParseVersion(stringResult(result)), nil
}

func (c *Client) Call(ctx context.Context, method string, params any) (any, error) {
	if c.skipDryRunMutation(method) {
		return map[string]any{"dry_run": true}, nil
	}
	id := c.requestID.Add(1)
	auth := c.token
	if method == "apiinfo.version" {
		auth = ""
	}
	payload, err := json.Marshal(rpcRequest{JSONRPC: "2.0", Method: method, Params: params, Auth: auth, ID: id})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json-rpc")
	response, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", method, err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 8<<10))
		return nil, fmt.Errorf("%s: HTTP %s: %s", method, response.Status, strings.TrimSpace(string(body)))
	}
	decoder := json.NewDecoder(response.Body)
	decoder.UseNumber()
	var rpc rpcResponse
	if err := decoder.Decode(&rpc); err != nil {
		return nil, fmt.Errorf("%s: decode response: %w", method, err)
	}
	if rpc.Error != nil {
		return nil, rpc.Error
	}
	var result any
	decoder = json.NewDecoder(bytes.NewReader(rpc.Result))
	decoder.UseNumber()
	if err := decoder.Decode(&result); err != nil {
		return nil, fmt.Errorf("%s: decode result: %w", method, err)
	}
	return result, nil
}

func (c *Client) skipDryRunMutation(method string) bool {
	if !isMutationMethod(method) {
		return false
	}
	c.dryRunMu.Lock()
	defer c.dryRunMu.Unlock()
	if !c.dryRun {
		return false
	}
	c.dryRunOps[method]++
	return true
}

func isMutationMethod(method string) bool {
	action := method
	if index := strings.LastIndexByte(method, '.'); index >= 0 {
		action = method[index+1:]
	}
	switch action {
	case "create", "update", "delete", "createglobal", "updateglobal", "deleteglobal", "import":
		return true
	default:
		return false
	}
}

func (c *Client) CallObjects(ctx context.Context, method string, params any) ([]map[string]any, error) {
	result, err := c.Call(ctx, method, params)
	if err != nil {
		return nil, err
	}
	values, ok := result.([]any)
	if !ok {
		return nil, fmt.Errorf("%s returned %T, expected array", method, result)
	}
	objects := make([]map[string]any, 0, len(values))
	for _, value := range values {
		object, ok := value.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s returned a non-object item", method)
		}
		objects = append(objects, object)
	}
	return objects, nil
}

func stringResult(value any) string {
	if str, ok := value.(string); ok {
		return str
	}
	return fmt.Sprint(value)
}
