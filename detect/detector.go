// Package detect holds the failure-mode detectors. Adding a detector
// is a single-file contribution: implement Detector, register it in
// All(). Detectors must be deterministic and require no network.
package detect

import "github.com/surajsrivastav/agentlens/model"

// Detector scans a normalized session and returns findings. The full
// Session (not just events) is passed so detectors can honor
// session-level context such as compaction (reduced confidence).
type Detector interface {
	Name() string
	Scan(s *model.Session) []model.Finding
}

// All returns the v0.1 detector set, in report order.
func All() []Detector {
	return []Detector{
		&Instructions{},
		&TestIntegrity{},
		&Rework{},
	}
}
