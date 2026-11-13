package clientroute

import (
	"encoding/base64"
	"encoding/json"
	"strconv"
	"strings"
)

const (
	simpleXrayMaxBlocks   = 32
	simpleXrayMaxCompiled = 64
	simpleXrayChunk       = 128
)

type simpleXrayServer struct {
	Name string `json:"name"`
}

type simpleXrayBlock struct {
	Target string            `json:"target"`
	Text   string            `json:"text"`
	Server *simpleXrayServer `json:"server,omitempty"`

	kinds map[string]int
}

type simpleXrayPreset struct {
	Format  string `json:"format"`
	Version int    `json:"version"`
	Routing struct {
		Blocks []*simpleXrayBlock `json:"blocks"`
	} `json:"routing"`
	GeoipURL   string `json:"geoipUrl"`
	GeositeURL string `json:"geositeUrl"`
}

var simpleXrayLine = map[string]string{"domain": "suffix", "full": "full", "geosite": "geosite", "geoip": "geoip"}

func simpleXrayTarget(r Resolved) (string, string) {
	switch r.Action {
	case ActionDirect:
		return "DIRECT", ""
	case ActionBlock:
		return "BLOCK", ""
	}
	return "PROXY", r.ServerName
}

func simpleXrayBlocks(rules []Resolved) []*simpleXrayBlock {
	var blocks []*simpleXrayBlock
	index := map[[2]string]*simpleXrayBlock{}
	for _, r := range rules {
		target, server := simpleXrayTarget(r)
		key := [2]string{target, server}
		b := index[key]
		if b == nil {
			b = &simpleXrayBlock{Target: target, kinds: map[string]int{}}
			if server != "" {
				b.Server = &simpleXrayServer{Name: server}
			}
			index[key] = b
			blocks = append(blocks, b)
		}
		lines := []string{"# " + r.Name}
		for _, v := range append(append([]string{}, r.Domain...), r.IP...) {
			kind, line := "ip", "ip:"+v
			if prefix, value, ok := strings.Cut(v, ":"); ok && !isAddress(v) {
				kind, line = prefix, simpleXrayLine[prefix]+":"+value
			}
			b.kinds[kind]++
			lines = append(lines, line)
		}
		if b.Text != "" {
			b.Text += "\n"
		}
		b.Text += strings.Join(lines, "\n")
	}
	return blocks
}

func simpleXrayCompiled(blocks []*simpleXrayBlock) int {
	n := 0
	for _, b := range blocks {
		for _, c := range b.kinds {
			n += (c + simpleXrayChunk - 1) / simpleXrayChunk
		}
	}
	return n
}

func renderSimpleXray(rules []Resolved) (string, error) {
	blocks := simpleXrayBlocks(rules[:simpleXrayFit(rules)])
	if len(blocks) == 0 {
		return "", nil
	}
	p := simpleXrayPreset{Format: "simplexray-routing", Version: 1, GeoipURL: GeoipURL, GeositeURL: GeositeURL}
	for _, b := range blocks {
		if b.Server != nil {
			p.Version = 2
		}
	}
	p.Routing.Blocks = blocks
	raw, err := json.Marshal(p)
	if err != nil {
		return "", err
	}
	return "simplexray://routing/" + base64.RawURLEncoding.EncodeToString(raw), nil
}

func simpleXrayFit(rules []Resolved) int {
	n := len(rules)
	for n > 0 {
		blocks := simpleXrayBlocks(rules[:n])
		if len(blocks) <= simpleXrayMaxBlocks && simpleXrayCompiled(blocks) <= simpleXrayMaxCompiled {
			break
		}
		n--
	}
	return n
}

func SimpleXrayTruncated(rules []Rule) []int {
	var kept []Resolved
	var index []int
	for i, r := range rules {
		if skipped(r, SimpleXray) {
			continue
		}
		res := Resolved{Rule: r}
		if r.Action == ActionNode {
			res.ServerName = "node-" + strconv.FormatInt(r.NodeID, 10)
		}
		kept = append(kept, res)
		index = append(index, i)
	}
	return index[simpleXrayFit(kept):]
}
