package usage

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"
)

// AgyPool is one Antigravity quota pool: models that share a remaining
// fraction and reset time (Gemini, and the third-party Claude/GPT-OSS pool).
type AgyPool struct {
	Name string `json:"name" doc:"Model families in the pool, e.g. Gemini or Claude & GPT-OSS"`
	// Percent of the pool used, 0-100.
	Percent  float64   `json:"percent" doc:"Percent of the pool used, 0-100"`
	ResetsAt time.Time `json:"resetsAt"`
	Models   []string  `json:"models" doc:"Display names of the selectable models in the pool"`
}

// AgySnapshot is the last Antigravity quota probe.
type AgySnapshot struct {
	Pools     []AgyPool `json:"pools"`
	CheckedAt time.Time `json:"checkedAt"`
}

// AgyProber reads the Antigravity quota the agy CLI shows, from the same
// fetchAvailableModels call it makes. The CLI's own token is used; when it
// is about to expire, `agy models` (no quota spent) refreshes it in place.
type AgyProber struct {
	Bin string
	// StateDir is the signed-in agy state directory (~/.gemini/antigravity-cli).
	StateDir string
	Timeout  time.Duration
	MaxAge   time.Duration
	MinGap   time.Duration
	// Endpoint defaults to the Cloud Code fetchAvailableModels URL.
	Endpoint string

	mu      sync.Mutex
	last    *AgySnapshot
	refresh func(ctx context.Context) error // for tests
}

const agyModelsURL = "https://cloudcode-pa.googleapis.com/v1internal:fetchAvailableModels"

// Get returns the cached snapshot, probing like Prober.Get.
func (p *AgyProber) Get(ctx context.Context, force bool) (AgySnapshot, error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.last != nil {
		age := time.Since(p.last.CheckedAt)
		if age < p.MinGap || (!force && age < p.MaxAge) {
			return *p.last, nil
		}
	}
	ctx, cancel := context.WithTimeout(ctx, p.Timeout)
	defer cancel()
	tok, err := p.token(ctx)
	if err != nil {
		return AgySnapshot{}, err
	}
	out, err := p.fetch(ctx, tok)
	if err != nil {
		return AgySnapshot{}, err
	}
	s, err := ParseAgy(out)
	if err != nil {
		return AgySnapshot{}, err
	}
	s.CheckedAt = time.Now()
	p.last = &s
	return s, nil
}

type agyToken struct {
	Token struct {
		AccessToken string    `json:"access_token"`
		Expiry      time.Time `json:"expiry"`
	} `json:"token"`
}

func (p *AgyProber) readToken() (agyToken, error) {
	var t agyToken
	b, err := os.ReadFile(filepath.Join(p.StateDir, "antigravity-oauth-token"))
	if err != nil {
		return t, fmt.Errorf("agy is not signed in: %w", err)
	}
	return t, json.Unmarshal(b, &t)
}

// token returns a usable access token, letting agy refresh it first when
// it expires within two minutes.
func (p *AgyProber) token(ctx context.Context) (string, error) {
	t, err := p.readToken()
	if err != nil {
		return "", err
	}
	if time.Until(t.Token.Expiry) > 2*time.Minute {
		return t.Token.AccessToken, nil
	}
	run := p.refresh
	if run == nil {
		run = p.runModels
	}
	if err := run(ctx); err != nil {
		return "", fmt.Errorf("refreshing the agy token: %w", err)
	}
	if t, err = p.readToken(); err != nil {
		return "", err
	}
	if time.Until(t.Token.Expiry) <= 0 {
		return "", errors.New("agy token is expired; sign in to agy again")
	}
	return t.Token.AccessToken, nil
}

func (p *AgyProber) runModels(ctx context.Context) error {
	dir, err := os.MkdirTemp("", "fbw-agy-usage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(dir)
	cmd := exec.CommandContext(ctx, p.Bin, "models")
	cmd.Dir = dir
	// The state dir lives at $HOME/.gemini/antigravity-cli.
	cmd.Env = append(os.Environ(), "HOME="+filepath.Dir(filepath.Dir(p.StateDir)))
	cmd.WaitDelay = 5 * time.Second
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%w: %s", err, tail(string(out)))
	}
	return nil
}

func (p *AgyProber) fetch(ctx context.Context, tok string) ([]byte, error) {
	url := p.Endpoint
	if url == "" {
		url = agyModelsURL
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader("{}"))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	req.Header.Set("Content-Type", "application/json")
	// The endpoint rejects the default Go user agent with 403.
	req.Header.Set("User-Agent", "antigravity")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("antigravity quota: HTTP %d: %s", res.StatusCode, tail(string(body)))
	}
	return body, nil
}

// ParseAgy groups the selectable models (those with a display name) of a
// fetchAvailableModels response into quota pools: Gemini, and everything
// else. A pool's usage is that of its most used model.
func ParseAgy(out []byte) (AgySnapshot, error) {
	var resp struct {
		Models map[string]struct {
			DisplayName   string `json:"displayName"`
			ModelProvider string `json:"modelProvider"`
			QuotaInfo     *struct {
				RemainingFraction *float64 `json:"remainingFraction"`
				ResetTime         string   `json:"resetTime"`
			} `json:"quotaInfo"`
		} `json:"models"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return AgySnapshot{}, fmt.Errorf("antigravity quota: %w", err)
	}
	type acc struct {
		pool     AgyPool
		families []string
	}
	pools := map[bool]*acc{} // keyed by "is Google"
	for _, m := range resp.Models {
		if m.DisplayName == "" || m.QuotaInfo == nil {
			continue
		}
		remaining := 1.0
		if m.QuotaInfo.RemainingFraction != nil {
			remaining = *m.QuotaInfo.RemainingFraction
		}
		google := m.ModelProvider == "MODEL_PROVIDER_GOOGLE"
		a := pools[google]
		if a == nil {
			a = &acc{pool: AgyPool{Percent: -1}}
			pools[google] = a
		}
		a.pool.Models = append(a.pool.Models, m.DisplayName)
		if f := strings.Fields(m.DisplayName)[0]; !slices.Contains(a.families, f) {
			a.families = append(a.families, f)
		}
		used := (1 - remaining) * 100
		reset, _ := time.Parse(time.RFC3339, m.QuotaInfo.ResetTime)
		if used > a.pool.Percent || (used == a.pool.Percent && reset.After(a.pool.ResetsAt)) {
			a.pool.Percent, a.pool.ResetsAt = used, reset
		}
	}
	if len(pools) == 0 {
		return AgySnapshot{}, errors.New("antigravity quota: no models with quota info")
	}
	var s AgySnapshot
	for _, google := range []bool{true, false} {
		a := pools[google]
		if a == nil {
			continue
		}
		sort.Strings(a.families)
		sort.Strings(a.pool.Models)
		a.pool.Models = slices.Compact(a.pool.Models)
		a.pool.Name = strings.Join(a.families, " & ")
		s.Pools = append(s.Pools, a.pool)
	}
	return s, nil
}
