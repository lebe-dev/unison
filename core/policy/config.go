package policy

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/lebe-dev/unison/core/model"
)

// Default eager limits.
const (
	// DefaultLANMaxSize is the eager limit on an unmetered local network.
	DefaultLANMaxSize int64 = 10 << 20
	// DefaultRelayMaxSize is the eager limit over the relay or a metered
	// network.
	DefaultRelayMaxSize int64 = 1 << 20
)

// RelayMaxSizeKey is the settings key of the eager limit over the relay.
const RelayMaxSizeKey = "policy.relay.max_size"

// Cell is the policy for one content kind under one network condition.
type Cell struct {
	// Action is the outcome for an item that arrives without an explicit
	// request of the user.
	Action Action
	// MaxSize is the largest item that is fetched eagerly. A share is applied
	// automatically up to this size.
	MaxSize int64
}

// Row is the policy for one network condition.
type Row struct {
	// Text is the policy for text items.
	Text Cell
	// Image is the policy for image items.
	Image Cell
	// Files is the policy for file items.
	Files Cell
	// Relay reports whether the relay path may be used.
	Relay bool
}

// Cell returns the cell of kind.
func (r Row) Cell(kind model.Kind) (Cell, bool) {
	switch kind {
	case model.KindText:
		return r.Text, true
	case model.KindImage:
		return r.Image, true
	case model.KindFiles:
		return r.Files, true
	default:
		return Cell{}, false
	}
}

// SetCell replaces the cell of kind.
func (r *Row) SetCell(kind model.Kind, cell Cell) {
	switch kind {
	case model.KindText:
		r.Text = cell
	case model.KindImage:
		r.Image = cell
	case model.KindFiles:
		r.Files = cell
	}
}

// Config holds the policy rows and the thresholds.
type Config struct {
	// Unmetered is the row of an online network without restrictions.
	Unmetered Row
	// Metered is the row of a network billed by volume.
	Metered Row
	// Roaming is the row of a roaming network.
	Roaming Row
	// DataSaver is the row of a network with the system data saver on.
	DataSaver Row
	// RelayMaxSize limits the item fetched eagerly over the relay.
	RelayMaxSize int64
}

// Default returns the default configuration of a device: text is applied on
// every network, images up to the eager limit, files only on request, and the
// relay is off where the network is restricted.
func Default() Config {
	return Config{
		Unmetered: Row{
			Text:  Cell{Action: ActionAutoApply, MaxSize: DefaultLANMaxSize},
			Image: Cell{Action: ActionAutoApply, MaxSize: DefaultLANMaxSize},
			Files: Cell{Action: ActionMetadataOnly, MaxSize: DefaultLANMaxSize},
			Relay: true,
		},
		Metered: Row{
			Text:  Cell{Action: ActionAutoApply, MaxSize: DefaultRelayMaxSize},
			Image: Cell{Action: ActionAutoApply, MaxSize: DefaultRelayMaxSize},
			Files: Cell{Action: ActionMetadataOnly, MaxSize: DefaultRelayMaxSize},
			Relay: true,
		},
		Roaming: Row{
			Text:  Cell{Action: ActionAutoApply, MaxSize: DefaultRelayMaxSize},
			Image: Cell{Action: ActionMetadataOnly, MaxSize: DefaultRelayMaxSize},
			Files: Cell{Action: ActionMetadataOnly, MaxSize: DefaultRelayMaxSize},
			Relay: false,
		},
		DataSaver: Row{
			Text:  Cell{Action: ActionAutoApply, MaxSize: DefaultRelayMaxSize},
			Image: Cell{Action: ActionMetadataOnly, MaxSize: DefaultRelayMaxSize},
			Files: Cell{Action: ActionMetadataOnly, MaxSize: DefaultRelayMaxSize},
			Relay: false,
		},
		RelayMaxSize: DefaultRelayMaxSize,
	}
}

// Row returns the row of condition.
func (c Config) Row(condition Condition) (Row, bool) {
	switch condition {
	case ConditionUnmetered:
		return c.Unmetered, true
	case ConditionMetered:
		return c.Metered, true
	case ConditionRoaming:
		return c.Roaming, true
	case ConditionDataSaver:
		return c.DataSaver, true
	default:
		return Row{}, false
	}
}

// SetRow replaces the row of condition.
func (c *Config) SetRow(condition Condition, row Row) {
	switch condition {
	case ConditionUnmetered:
		c.Unmetered = row
	case ConditionMetered:
		c.Metered = row
	case ConditionRoaming:
		c.Roaming = row
	case ConditionDataSaver:
		c.DataSaver = row
	}
}

// Validate checks every row and threshold. An automatic apply needs a positive
// size limit, otherwise the configuration can never apply an item.
func (c Config) Validate() error {
	var errs []error
	for _, condition := range Conditions() {
		row, _ := c.Row(condition)
		for _, kind := range Kinds() {
			cell, _ := row.Cell(kind)
			if cell.Action > ActionAutoApply {
				errs = append(errs, fmt.Errorf("policy: %s: unknown action %d", ModeKey(condition, kind), cell.Action))
			}
			if cell.MaxSize < 0 {
				errs = append(errs, fmt.Errorf("policy: %s: negative size limit", MaxSizeKey(condition, kind)))
			}
			if cell.Action == ActionAutoApply && cell.MaxSize == 0 {
				errs = append(errs, fmt.Errorf("policy: %s: automatic apply without a size limit", MaxSizeKey(condition, kind)))
			}
		}
	}
	if c.RelayMaxSize <= 0 {
		errs = append(errs, fmt.Errorf("policy: %s: size limit must be positive", RelayMaxSizeKey))
	}
	return errors.Join(errs...)
}

// ModeKey returns the settings key of the action of kind under condition.
func ModeKey(condition Condition, kind model.Kind) string {
	return "policy.network." + condition.String() + "." + kind.String() + ".mode"
}

// MaxSizeKey returns the settings key of the size limit of kind under
// condition.
func MaxSizeKey(condition Condition, kind model.Kind) string {
	return "policy.network." + condition.String() + "." + kind.String() + ".max_size"
}

// RelayKey returns the settings key of the relay permission of condition.
func RelayKey(condition Condition) string {
	return "policy.network." + condition.String() + ".relay"
}

// Settings is the subset of the store settings the policy reads.
type Settings interface {
	GetString(ctx context.Context, key, def string) (string, error)
	GetInt(ctx context.Context, key string, def int64) (int64, error)
	GetBool(ctx context.Context, key string, def bool) (bool, error)
}

// LoadConfig reads the overrides from settings over Default. A missing key
// keeps its default. A value that is out of range keeps its default and is
// reported in the returned error, so a broken setting never changes the policy
// silently.
func LoadConfig(ctx context.Context, s Settings) (Config, error) {
	cfg := Default()
	var errs []error
	for _, condition := range Conditions() {
		row, _ := cfg.Row(condition)
		relay, err := s.GetBool(ctx, RelayKey(condition), row.Relay)
		if err != nil {
			return Config{}, err
		}
		row.Relay = relay

		for _, kind := range Kinds() {
			cell, _ := row.Cell(kind)
			mode, err := s.GetString(ctx, ModeKey(condition, kind), cell.Action.String())
			if err != nil {
				return Config{}, err
			}
			if action, err := ParseAction(mode); err != nil {
				errs = append(errs, fmt.Errorf("policy: %s: %w", ModeKey(condition, kind), err))
			} else {
				cell.Action = action
			}

			size, err := s.GetInt(ctx, MaxSizeKey(condition, kind), cell.MaxSize)
			if err != nil {
				return Config{}, err
			}
			if size < 0 {
				errs = append(errs, fmt.Errorf("policy: %s: negative size limit %d", MaxSizeKey(condition, kind), size))
			} else {
				cell.MaxSize = size
			}
			row.SetCell(kind, cell)
		}
		cfg.SetRow(condition, row)
	}

	relayMax, err := s.GetInt(ctx, RelayMaxSizeKey, cfg.RelayMaxSize)
	if err != nil {
		return Config{}, err
	}
	if relayMax <= 0 {
		errs = append(errs, fmt.Errorf("policy: %s: size limit must be positive, got %d", RelayMaxSizeKey, relayMax))
	} else {
		cfg.RelayMaxSize = relayMax
	}

	if err := cfg.Validate(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return cfg, errors.Join(errs...)
	}
	return cfg, nil
}

// Keys returns every settings key the policy reads.
func Keys() []string {
	keys := make([]string, 0, 2*len(Conditions())*(len(Kinds())+1))
	for _, condition := range Conditions() {
		keys = append(keys, RelayKey(condition))
		for _, kind := range Kinds() {
			keys = append(keys, ModeKey(condition, kind), MaxSizeKey(condition, kind))
		}
	}
	keys = append(keys, RelayMaxSizeKey)
	slices.Sort(keys)
	return keys
}
