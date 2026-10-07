package store

import (
	"fmt"

	"github.com/JaviCss/eco/port"
)

type Profile int

const (
	ProfileAgent Profile = iota
	ProfileHuman
	ProfileRuntime
)

func (p Profile) String() string {
	switch p {
	case ProfileHuman:
		return "human"
	case ProfileRuntime:
		return "runtime"
	default:
		return "agent"
	}
}

var profileAxes = map[Profile]map[port.Axis]bool{
	ProfileAgent: {
		port.AxisZ2: true,
		port.AxisZ3: true,
		port.AxisY:  true,
		port.AxisX:  true,
	},
	ProfileHuman: {
		port.AxisZ2: true,
		port.AxisZ3: true,
		port.AxisY:  true,
		port.AxisX:  true,
	},
	ProfileRuntime: {
		port.AxisZ2:       true,
		port.AxisZ3:       true,
		port.AxisY:        true,
		port.AxisX:        true,
		port.AxisR:        true,
		port.AxisV:        true,
		port.AxisManifest: true,
	},
}

var profileWrites = map[Profile]map[port.Axis]bool{
	ProfileAgent: {
		port.AxisZ3: true,
		port.AxisY:  true,
		port.AxisX:  true,
	},
	ProfileHuman: {
		port.AxisZ3: true,
		port.AxisY:  true,
		port.AxisX:  true,
	},
	ProfileRuntime: {
		port.AxisZ2:       true,
		port.AxisZ3:       true,
		port.AxisY:        true,
		port.AxisX:        true,
		port.AxisR:        true,
		port.AxisV:        true,
		port.AxisManifest: true,
	},
}

func (p Profile) mayRead(axis port.Axis) bool {
	return profileAxes[p][axis]
}

func (p Profile) mayWrite(axis port.Axis) bool {
	return profileWrites[p][axis]
}

func (p Profile) mayPromote() bool {
	return p == ProfileHuman || p == ProfileRuntime
}

func (s *Store) checkRead(axis port.Axis) error {
	if s.cfg.Profile.mayRead(axis) {
		return nil
	}
	return fmt.Errorf("eco: %s: %w: profile %s does not read %s", axis, port.ErrForbidden, s.cfg.Profile, axis)
}

func (s *Store) checkWrite(axis port.Axis) error {
	if s.cfg.Profile.mayWrite(axis) {
		return nil
	}
	return fmt.Errorf("eco: %s: %w: profile %s does not write %s", axis, port.ErrForbidden, s.cfg.Profile, axis)
}
