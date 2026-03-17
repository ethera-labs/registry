package registry

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	assets "github.com/ethera-labs/registry"
)

// Sentinel errors for not-found cases. Use errors.Is to test them.
var (
	ErrNetworkNotFound = errors.New("network not found")
	ErrChainNotFound   = errors.New("chain not found")
)

// Registry provides access to the embedded registry (default) or a directory on disk.
// It owns a normalized fs rooted at the data/ folder, so lookups use paths like
// "networks/<network>/<chain>.toml".
type Registry struct{ fs fs.FS }

// New returns a Registry backed by the embedded assets under data/.
func New() Registry {
	sub, _ := fs.Sub(assets.FS, "data")
	return Registry{fs: sub}
}

// NewFromDir returns a Registry backed by a directory on disk that contains
// a data layout compatible with the embedded one (expects a "networks/" directory).
func NewFromDir(dir string) (Registry, error) {
	// Validate that dir contains a networks/ directory
	if fi, err := os.Stat(filepath.Join(dir, "networks")); err != nil || !fi.IsDir() {
		return Registry{}, fmt.Errorf("registry: networks directory not found in %q", dir)
	}
	return Registry{fs: os.DirFS(dir)}, nil
}

// Network is a lightweight network handle (slug-only). Use LoadConfig to decode TOML.
type Network struct {
	slug string
	r    Registry
}

// Slug returns the network slug.
func (n Network) Slug() string { return n.slug }

// Chain is a lightweight chain handle (slug + parent network).
type Chain struct {
	slug string
	n    Network
}

// Slug returns the chain slug.
func (c Chain) Slug() string { return c.slug }

// Network returns the parent network handle.
func (c Chain) Network() Network { return c.n }

// Identifier returns "<network>/<slug>".
func (c Chain) Identifier() string { return c.n.slug + "/" + c.slug }

// ChainConfig is decoded from networks/<network>/<slug>.toml.
type ChainConfig struct {
	Name      string `toml:"name"`
	ChainID   uint64 `toml:"chain_id"`
	PublicRPC string `toml:"public_rpc"`
	Explorer  string `toml:"explorer"`
	Addresses struct {
		Mailbox string `toml:"Mailbox"`
	} `toml:"addresses"`
	Genesis struct {
		L2Time uint64 `toml:"l2_time"`
	} `toml:"genesis"`
	Sequencer struct {
		Host        string   `toml:"host"`
		Port        int      `toml:"port"`
		AuthPubkeys []string `toml:"auth_pubkeys"`
	} `toml:"sequencer"`
}

// NetworkConfig is decoded from networks/<slug>/compose.toml.
type NetworkConfig struct {
	Name string `toml:"name"`
	L1   struct {
		ChainID   uint64 `toml:"chain_id"`
		PublicRPC string `toml:"public_rpc"`
		Explorer  string `toml:"explorer"`
	} `toml:"l1"`
	Publisher struct {
		SuperblockContract string   `toml:"superblock_contract"`
		DisputeGameFactory string   `toml:"dispute_game_factory"`
		AuthPubkeys        []string `toml:"auth_pubkeys"`
	} `toml:"publisher"`
}

// ListNetworks lists all available networks as handles.
func (r Registry) ListNetworks() ([]Network, error) {
	entries, err := fs.ReadDir(r.fs, "networks")
	if err != nil {
		return nil, fmt.Errorf("list networks: %w", err)
	}
	slugs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			slugs = append(slugs, e.Name())
		}
	}
	sort.Strings(slugs)
	out := make([]Network, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, Network{slug: s, r: r})
	}
	return out, nil
}

// GetNetworkBySlug returns a handle if networks/<slug> exists.
func (r Registry) GetNetworkBySlug(slug string) (Network, error) {
	if _, err := fs.ReadDir(r.fs, path.Join("networks", slug)); err != nil {
		return Network{}, fmt.Errorf("%w: %s", ErrNetworkNotFound, slug)
	}
	return Network{slug: slug, r: r}, nil
}

// GetNetworkById returns the first network whose L1.ChainID matches.
func (r Registry) GetNetworkById(l1ChainId uint64) (Network, error) {
	nets, err := r.ListNetworks()
	if err != nil {
		return Network{}, err
	}
	for _, n := range nets {
		cfg, err := n.LoadConfig()
		if err != nil {
			return Network{}, err
		}
		if cfg.L1.ChainID == l1ChainId {
			return n, nil
		}
	}
	return Network{}, ErrNetworkNotFound
}

// ListChains returns chain handles in this network.
func (n Network) ListChains() ([]Chain, error) {
	entries, err := fs.ReadDir(n.r.fs, path.Join("networks", n.slug))
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNetworkNotFound, n.slug)
	}
	slugs := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if strings.EqualFold(name, "compose.toml") || !strings.HasSuffix(name, ".toml") {
			continue
		}
		slugs = append(slugs, strings.TrimSuffix(name, ".toml"))
	}
	sort.Strings(slugs)
	out := make([]Chain, 0, len(slugs))
	for _, s := range slugs {
		out = append(out, Chain{slug: s, n: n})
	}
	return out, nil
}

// GetChainBySlug returns a chain handle if <slug>.toml exists.
func (n Network) GetChainBySlug(slug string) (Chain, error) {
	s := strings.TrimSpace(slug)
	if s == "" {
		return Chain{}, errors.New("empty chain slug")
	}
	// existence check only
	if _, err := fs.ReadFile(n.r.fs, path.Join("networks", n.slug, s+".toml")); err != nil {
		return Chain{}, fmt.Errorf("%w: %s/%s", ErrChainNotFound, n.slug, s)
	}
	return Chain{slug: s, n: n}, nil
}

// GetChainById returns the first chain in this network whose ChainID matches.
func (n Network) GetChainById(l2ChainId uint64) (Chain, error) {
	chains, err := n.ListChains()
	if err != nil {
		return Chain{}, err
	}
	for _, ch := range chains {
		cfg, err := ch.LoadConfig()
		if err != nil {
			return Chain{}, err
		}
		if cfg.ChainID == l2ChainId {
			return ch, nil
		}
	}
	return Chain{}, ErrChainNotFound
}

// ListChains returns all chain handles across all networks.
func (r Registry) ListChains() ([]Chain, error) {
	nets, err := r.ListNetworks()
	if err != nil {
		return nil, err
	}
	var out []Chain
	for _, n := range nets {
		cs, err := n.ListChains()
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

// GetChainByIdentifier returns a chain handle for "<network>/<slug>".
func (r Registry) GetChainByIdentifier(identifier string) (Chain, error) {
	s := strings.TrimSpace(identifier)
	if s == "" {
		return Chain{}, errors.New("empty identifier")
	}
	i := strings.IndexByte(s, '/')
	if i <= 0 || i >= len(s)-1 {
		return Chain{}, fmt.Errorf("invalid identifier %q: want <network>/<slug>", identifier)
	}
	netSlug := s[:i]
	chainSlug := s[i+1:]
	n, err := r.GetNetworkBySlug(netSlug)
	if err != nil {
		return Chain{}, err
	}
	return n.GetChainBySlug(chainSlug)
}

// GetChainById returns the first chain across all networks whose ChainID matches.
func (r Registry) GetChainById(l2ChainId uint64) (Chain, error) {
	nets, err := r.ListNetworks()
	if err != nil {
		return Chain{}, err
	}
	for _, n := range nets {
		ch, err := n.GetChainById(l2ChainId)
		if err == nil {
			return ch, nil
		}
		if !errors.Is(err, ErrChainNotFound) {
			return Chain{}, err
		}
	}
	return Chain{}, ErrChainNotFound
}

// LoadConfig decodes networks/<network>/<slug>.toml for this chain.
func (c Chain) LoadConfig() (ChainConfig, error) {
	s := strings.TrimSpace(c.slug)
	if s == "" {
		return ChainConfig{}, errors.New("empty chain slug")
	}
	p := path.Join("networks", c.n.slug, s+".toml")
	b, err := fs.ReadFile(c.n.r.fs, p)
	if err != nil {
		return ChainConfig{}, fmt.Errorf("%w: %s/%s", ErrChainNotFound, c.n.slug, s)
	}
	var cfg ChainConfig
	if _, err := toml.Decode(string(b), &cfg); err != nil {
		return ChainConfig{}, fmt.Errorf("decode %s: %w", p, err)
	}
	return cfg, nil
}

// LoadConfig decodes networks/<slug>/compose.toml for this network.
func (n Network) LoadConfig() (NetworkConfig, error) {
	b, err := fs.ReadFile(n.r.fs, path.Join("networks", n.slug, "compose.toml"))
	if err != nil {
		return NetworkConfig{}, fmt.Errorf("read compose.toml for %s: %w", n.slug, err)
	}
	var cfg NetworkConfig
	if _, err := toml.Decode(string(b), &cfg); err != nil {
		return NetworkConfig{}, fmt.Errorf("decode compose.toml for %s: %w", n.slug, err)
	}
	return cfg, nil
}
