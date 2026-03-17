package main

import (
	"errors"
	"flag"
	"fmt"
	"net/url"
	"os"
	"regexp"
	"strings"

	"github.com/BurntSushi/toml"
	t "github.com/ethera-labs/registry/internal/types"
)

var identRe = regexp.MustCompile(`^[a-z0-9-]+/[a-z0-9-]+$`)

func main() {
	var in string
	flag.StringVar(&in, "in", "data/chainList.toml", "input TOML path")
	flag.Parse()
	var cl t.ChainListTOML
	if _, err := toml.DecodeFile(in, &cl); err != nil {
		fatalf("decode TOML: %v", err)
	}
	if err := validate(cl); err != nil {
		fatalf("validation failed: %v", err)
	}
	fmt.Println("validation ok")
}

func validate(cl t.ChainListTOML) error {
	seenIdent := map[string]bool{}
	seenID := map[uint64]bool{}
	for i, c := range cl.Chains {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("chain[%d]: name required", i)
		}
		if !identRe.MatchString(strings.TrimSpace(c.Identifier)) {
			return fmt.Errorf("chain[%d]: identifier must be '<network>/<slug>'", i)
		}
		if c.ChainID == 0 {
			return fmt.Errorf("chain[%d]: invalid chain_id", i)
		}
		if seenID[c.ChainID] {
			return fmt.Errorf("duplicate chain_id: %d", c.ChainID)
		}
		if seenIdent[c.Identifier] {
			return fmt.Errorf("duplicate identifier: %s", c.Identifier)
		}
		seenIdent[c.Identifier], seenID[c.ChainID] = true, true

		// Require parent object
		if strings.TrimSpace(c.Parent.Type) == "" || strings.TrimSpace(c.Parent.Chain) == "" {
			return fmt.Errorf("chain[%d]: parent.type/parent.chain required", i)
		}
		if strings.ToUpper(c.Parent.Type) != "L2" {
			return fmt.Errorf("chain[%d]: parent.type must be L2", i)
		}

		// RPC should be non-empty list of URLs
		if len(c.RPC) == 0 {
			return fmt.Errorf("chain[%d]: rpc must contain at least one URL", i)
		}
		for _, r := range c.RPC {
			if err := mustURL(r); err != nil {
				return fmt.Errorf("chain[%d] rpc: %w", i, err)
			}
		}
		// Explorers optional but if present must be URLs
		for _, e := range c.Explorers {
			if err := mustURL(e); err != nil {
				return fmt.Errorf("chain[%d] explorers: %w", i, err)
			}
		}
		// dataAvailabilityType one of known values (align with OP: eth-da or alt-da)
		switch strings.ToLower(c.DataAvailabilityType) {
		case "eth-da", "alt-da":
		default:
			return fmt.Errorf("chain[%d]: data_availability_type must be 'eth-da' or 'alt-da'", i)
		}
	}
	return nil
}

func mustURL(s string) error {
	if s == "" {
		return errors.New("empty URL")
	}
	u, err := url.Parse(s)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("invalid url: %s", s)
	}
	return nil
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
