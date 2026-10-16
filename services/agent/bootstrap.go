package agent

import (
	"errors"
	"os"
)

func Bootstrap(cfg *Config) (bool, error) {
	if _, err := os.Stat(cfg.XrayConfig); !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	frame, err := BuildXray([]Inbound{}, cfg.XrayAPI)
	if err != nil {
		return false, err
	}
	if err := writeFileAtomic(cfg.XrayConfig, frame, xrayFileMode); err != nil {
		return false, err
	}
	return true, nil
}
