package main

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/BurntSushi/toml"
	t "github.com/compose-network/registry/internal/types"
	reg "github.com/compose-network/registry/registry"
	"github.com/klauspost/compress/zstd"
)

// minimal view of Ethereum genesis we care about
type genesisCfg struct {
	Config struct {
		ChainID any `json:"chainId"`
	} `json:"config"`
	Timestamp any `json:"timestamp"`
}

func main() {
	var base string
	flag.StringVar(&base, "base", ".", "repository root")
	flag.Parse()

	// Load compose chains from embedded TOMLs (generic networks API)
	r := reg.New()
	networks, err := r.ListNetworks()
	if err != nil {
		fatalf("list networks: %v", err)
	}
	if len(networks) == 0 {
		fatalf("no networks found")
	}

	// Load chainList for cross-check
	var cl t.ChainListTOML
	if _, err := toml.DecodeFile(filepath.Join(base, "data/chainList.toml"), &cl); err != nil {
		fatalf("decode chainList.toml: %v", err)
	}
	idsByIdentifier := make(map[string]int64)
	for _, c := range cl.Chains {
		idsByIdentifier[c.Identifier] = int64(c.ChainID)
	}

	// For each network, check all chains
	for _, n := range networks {
		networkSlug := n.Slug()
		chains, err := n.ListChains()
		if err != nil {
			fatalf("list chains for %s: %v", networkSlug, err)
		}

		// For each chain, ensure genesis exists, decode and compare ids & time
		for _, c := range chains {
			slug := c.Slug()
			identifier := c.Identifier()
			ccfg, err := c.LoadConfig()
			if err != nil {
				fatalf("load chain %s: %v", identifier, err)
			}
			expectID := int64(ccfg.ChainID)
			if id2, ok := idsByIdentifier[identifier]; ok && id2 != expectID {
				fatalf("identifier %s: chainList chain_id=%d != compose chain_id=%d", identifier, id2, expectID)
			}

			genPath := filepath.Join(base, "data/genesis", networkSlug, slug+".json.zst")
			f, err := os.Open(genPath)
			if err != nil {
				fatalf("%s missing: %v", genPath, err)
			}
			zr, err := zstd.NewReader(f)
			var raw []byte
			if err == nil {
				raw, err = io.ReadAll(zr)
				zr.Close()
				if err != nil {
					// Fallback to plain JSON if decode fails
					if _, serr := f.Seek(0, 0); serr != nil {
						fatalf("seek %s: %v", genPath, serr)
					}
					raw, err = io.ReadAll(f)
					if err != nil {
						fatalf("read %s: %v", genPath, err)
					}
				}
				if cerr := f.Close(); cerr != nil {
					fatalf("close %s: %v", genPath, cerr)
				}
			} else {
				// Fallback to plain JSON for dev files
				if _, serr := f.Seek(0, 0); serr != nil {
					fatalf("seek %s: %v", genPath, serr)
				}
				raw, err = io.ReadAll(f)
				if cerr := f.Close(); cerr != nil {
					fatalf("close %s: %v", genPath, cerr)
				}
				if err != nil {
					fatalf("read %s: %v", genPath, err)
				}
			}

			var g genesisCfg
			if err := json.Unmarshal(raw, &g); err != nil {
				fatalf("decode genesis %s: %v", genPath, err)
			}
			gotID, err := anyToInt64(g.Config.ChainID)
			if err != nil {
				fatalf("%s chainId parse: %v", genPath, err)
			}
			if gotID != expectID {
				fatalf("%s chainId=%d, want %d", genPath, gotID, expectID)
			}
			// Compare timestamp vs compose TOML genesis.l2_time if present
			if ccfg.Genesis.L2Time != 0 {
				ts, err := anyToInt64(g.Timestamp)
				if err != nil {
					fatalf("%s timestamp parse: %v", genPath, err)
				}
				if ts != int64(ccfg.Genesis.L2Time) {
					fatalf("%s timestamp=%d, want %d", genPath, ts, ccfg.Genesis.L2Time)
				}
			}
		}
	}
	fmt.Println("checkgenesis ok")
}

func deriveSlug(identifier string) string {
	i := strings.LastIndex(identifier, "/")
	if i >= 0 && i < len(identifier)-1 {
		return identifier[i+1:]
	}
	return identifier
}

func anyToInt64(v any) (int64, error) {
	switch x := v.(type) {
	case float64:
		return int64(x), nil
	case int64:
		return x, nil
	case int:
		return int64(x), nil
	case string:
		s := strings.TrimSpace(x)
		if strings.HasPrefix(s, "0x") || strings.HasPrefix(s, "0X") {
			// hex string, may be odd-length
			// fast path: parse as uint64 from hex (without 0x)
			u, err := strconv.ParseUint(s[2:], 16, 64)
			if err != nil {
				// fallback: decode bytes then big-endian
				b, err2 := hex.DecodeString(strings.TrimPrefix(s, "0x"))
				if err2 != nil {
					return 0, err
				}
				var u2 uint64
				for _, by := range b {
					u2 = (u2 << 8) | uint64(by)
				}
				return int64(u2), nil
			}
			return int64(u), nil
		}
		// decimal
		u, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return 0, err
		}
		return u, nil
	default:
		return 0, errors.New("unsupported number type")
	}
}

func fatalf(format string, a ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", a...)
	os.Exit(1)
}
