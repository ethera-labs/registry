package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"
	t "github.com/ethera-labs/registry/internal/types"
)

type chainCfg struct {
	Name                 string `toml:"name"`
	ChainID              uint64 `toml:"chain_id"`
	PublicRPC            string `toml:"public_rpc"`
	Explorer             string `toml:"explorer"`
	DataAvailabilityType string `toml:"data_availability_type"`
}

func main() {
	var base string
	var outToml string
	var outJSON string
	flag.StringVar(&base, "base", ".", "repository root (registry module)")
	flag.StringVar(&outToml, "out-toml", "data/chainList.toml", "output TOML path")
	flag.StringVar(&outJSON, "out-json", "data/chainList.json", "output JSON path")
	flag.Parse()

	cfgRoot := filepath.Join(base, "data", "networks")
	networks, err := os.ReadDir(cfgRoot)
	if err != nil {
		fatalf("read configs dir: %v", err)
	}

	var out t.ChainListTOML
	expected := 0
	for _, n := range networks {
		if !n.IsDir() {
			continue
		}
		network := n.Name()
		dir := filepath.Join(cfgRoot, network)
		entries, err := os.ReadDir(dir)
		if err != nil {
			fatalf("read %s: %v", dir, err)
		}
		for _, e := range entries {
			name := e.Name()
			if e.IsDir() || !strings.HasSuffix(name, ".toml") {
				continue
			}
			if strings.EqualFold(name, "compose.toml") { // network-level file, skip
				continue
			}
			path := filepath.Join(dir, name)
			var cfg chainCfg
			if _, err := toml.DecodeFile(path, &cfg); err != nil {
				fatalf("decode %s: %v", path, err)
			}
			slug := strings.TrimSuffix(name, ".toml")
			entry := t.ChainListEntry{
				Name:                 cfg.Name,
				Identifier:           network + "/" + slug,
				ChainID:              cfg.ChainID,
				RPC:                  []string{},
				Explorers:            []string{},
				DataAvailabilityType: defaultDA(cfg.DataAvailabilityType),
				Parent:               t.ChainListEntryParent{Type: "L2", Chain: network},
				GasPayingToken:       "",
				FaultProofs:          nil,
			}
			if strings.TrimSpace(cfg.PublicRPC) != "" {
				entry.RPC = []string{cfg.PublicRPC}
			}
			if strings.TrimSpace(cfg.Explorer) != "" {
				entry.Explorers = []string{cfg.Explorer}
			}
			out.Chains = append(out.Chains, entry)
			expected++
		}
	}
	// Stable sort by identifier
	sort.Slice(out.Chains, func(i, j int) bool { return out.Chains[i].Identifier < out.Chains[j].Identifier })

	if expected != len(out.Chains) {
		fatalf("chainlist-gen: expected %d per-chain configs, built %d entries", expected, len(out.Chains))
	}

	// Write TOML
	dest := filepath.Join(base, outToml)
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		fatalf("mkdir data: %v", err)
	}
	f, err := os.Create(dest)
	if err != nil {
		fatalf("create %s: %v", dest, err)
	}
	defer func() { _ = f.Close() }()
	enc := toml.NewEncoder(f)
	if err := enc.Encode(out); err != nil {
		fatalf("encode %s: %v", dest, err)
	}
	fmt.Printf("wrote %s (chains=%d)\n", dest, len(out.Chains))

	// Write JSON
	jdest := filepath.Join(base, outJSON)
	if err := os.MkdirAll(filepath.Dir(jdest), 0o755); err != nil {
		fatalf("mkdir data: %v", err)
	}
	jf, err := os.Create(jdest)
	if err != nil {
		fatalf("create %s: %v", jdest, err)
	}
	defer func() { _ = jf.Close() }()
	jenc := json.NewEncoder(jf)
	jenc.SetIndent("", "  ")
	if err := jenc.Encode(out.Chains); err != nil {
		fatalf("encode %s: %v", jdest, err)
	}
	fmt.Printf("wrote %s (chains=%d)\n", jdest, len(out.Chains))
}

func defaultDA(in string) string {
	if strings.TrimSpace(in) == "" {
		return "eth-da"
	}
	return in
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
