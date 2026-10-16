package transform

import (
	"encoding/json"
	"fmt"
	"hash/fnv"
	"math"
	"strings"
)

type sampleConfig struct {
	KeepPercent number         `json:"keep_percent"`
	AlwaysKeep  *ruleSetConfig `json:"always_keep"`
	Key         string         `json:"key"`
}

// sampleNode keeps a deterministic share of records: a record passes when
// FNV-1a 64 of its key (or of its canonical JSON) mod 10000 is below
// keep_percent*100. Records matching always_keep always pass.
type sampleNode struct {
	threshold uint64
	always    *ruleSet
	key       *Path
}

func newSample(raw map[string]any, opts Options) (processor, error) {
	var cfg sampleConfig
	if err := decodeConfig(raw, &cfg); err != nil {
		return nil, err
	}
	if !cfg.KeepPercent.set || cfg.KeepPercent.v < 0 || cfg.KeepPercent.v > 100 || math.IsNaN(cfg.KeepPercent.v) {
		return nil, fmt.Errorf("keep_percent must be a number from 0 to 100")
	}
	n := &sampleNode{threshold: uint64(math.Round(cfg.KeepPercent.v * 100))}
	if cfg.AlwaysKeep != nil && len(cfg.AlwaysKeep.Rules) > 0 {
		rs, err := compileRuleSet(*cfg.AlwaysKeep, opts)
		if err != nil {
			return nil, fmt.Errorf("always_keep: %w", err)
		}
		n.always = &rs
	}
	if strings.TrimSpace(cfg.Key) != "" {
		p, err := ParsePath(cfg.Key, opts)
		if err != nil {
			return nil, fmt.Errorf("key: %w", err)
		}
		n.key = &p
	}
	return n, nil
}

func (n *sampleNode) process(rec map[string]any) (bool, string, error) {
	if n.always != nil && n.always.match(rec) {
		return true, "", nil
	}
	if sampleBucket(rec, n.key) < n.threshold {
		return true, "", nil
	}
	return false, "not sampled", nil
}

// sampleBucket is the record's bucket in [0, 10000).
func sampleBucket(rec map[string]any, key *Path) uint64 {
	h := fnv.New64a()
	var data []byte
	if key != nil {
		if v, ok := key.get(rec); ok {
			data = []byte(Stringify(v))
		}
	}
	if len(data) == 0 {
		// encoding/json sorts map keys, so this is canonical.
		data, _ = json.Marshal(rec)
	}
	h.Write(data)
	return h.Sum64() % 10000
}
