package selfupdate

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	Asset = "sylph-linux-amd64"
	Sums  = "SHA256SUMS"
)

var ErrChecksum = errors.New("SHA256 скачанного бинаря не сходится с SHA256SUMS релиза")

var releaseTag = regexp.MustCompile(`^v([0-9]+\.[0-9]+\.[0-9]+)$`)

type Client struct {
	HTTP     *http.Client
	API      string
	Download string
	Agent    string
	MaxSize  int64
}

func New(version string) *Client {
	return &Client{
		HTTP: &http.Client{
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if req.URL.Scheme != "https" {
					return fmt.Errorf("редирект на %s: только https", req.URL.Scheme)
				}
				if len(via) >= 10 {
					return errors.New("слишком много редиректов")
				}
				return nil
			},
		},
		API:      "https://api.github.com/repos/ITG-Labs/sylph/releases/latest",
		Download: "https://github.com/ITG-Labs/sylph/releases/download",
		Agent:    "sylph/" + version,
		MaxSize:  100 << 20,
	}
}

func (c *Client) get(ctx context.Context, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", c.Agent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", req.URL.Host+req.URL.Path, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) Latest(ctx context.Context) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	resp, err := c.get(ctx, c.API)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var body struct {
		Tag string `json:"tag_name"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("ответ GitHub о релизе: %w", err)
	}
	m := releaseTag.FindStringSubmatch(body.Tag)
	if m == nil {
		return "", fmt.Errorf("последний релиз с непонятным тегом %q", body.Tag)
	}
	return m[1], nil
}

func (c *Client) Fetch(ctx context.Context, version string, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	base := c.Download + "/v" + version + "/"
	want, err := c.checksum(ctx, base+Sums)
	if err != nil {
		return "", err
	}
	resp, err := c.get(ctx, base+Asset)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	tmp, err := os.CreateTemp(dir, ".sylph-new-*")
	if err != nil {
		return "", err
	}
	name := tmp.Name()
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, c.MaxSize+1))
	switch {
	case err != nil:
	case n > c.MaxSize:
		err = fmt.Errorf("бинарь больше %d МиБ", c.MaxSize>>20)
	case hex.EncodeToString(h.Sum(nil)) != want:
		err = ErrChecksum
	}
	if err == nil {
		err = tmp.Chmod(0o755)
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(name)
		return "", err
	}
	return name, nil
}

func (c *Client) checksum(ctx context.Context, url string) (string, error) {
	resp, err := c.get(ctx, url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	sc := bufio.NewScanner(io.LimitReader(resp.Body, 64<<10))
	for sc.Scan() {
		sum, file, ok := strings.Cut(strings.TrimSpace(sc.Text()), " ")
		file = strings.TrimPrefix(strings.TrimSpace(file), "*")
		if !ok || file != Asset {
			continue
		}
		if b, err := hex.DecodeString(sum); err != nil || len(b) != sha256.Size {
			return "", fmt.Errorf("%s: битая строка для %s", Sums, Asset)
		}
		return strings.ToLower(sum), nil
	}
	if err := sc.Err(); err != nil {
		return "", err
	}
	return "", fmt.Errorf("в %s релиза нет %s", Sums, Asset)
}

func Install(ctx context.Context, tmp string, exe string, version string) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, tmp, "version").Output()
	if err == nil && !bytes.HasPrefix(out, []byte("sylph "+version+" ")) {
		err = fmt.Errorf("новый бинарь назвался %q, а ждали %s", strings.TrimSpace(string(out)), version)
	}
	if err == nil && filepath.Dir(tmp) != filepath.Dir(exe) {
		err = errors.New("новый бинарь лежит не рядом со старым: подмена не будет атомарной")
	}
	if err == nil {
		err = sameOwner(tmp, exe)
	}
	if err == nil {
		err = os.Rename(tmp, exe)
	}
	if err != nil {
		os.Remove(tmp)
		return fmt.Errorf("новый бинарь не встал: %w", err)
	}
	return nil
}

func sameOwner(tmp string, exe string) error {
	st, err := os.Stat(exe)
	if err != nil {
		return err
	}
	sys, ok := st.Sys().(*syscall.Stat_t)
	if !ok {
		return nil
	}
	return os.Lchown(tmp, int(sys.Uid), int(sys.Gid))
}
