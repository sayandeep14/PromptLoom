package locker

import "time"

// State holds the current lock state and the secret mapping.
// This lives entirely in process memory — never written to disk in non-recoverable mode.
type State struct {
	Locked   bool
	LockedAt time.Time
	// Mapping: absolute file path → locked text (as written into the file) → original text
	Mapping map[string]map[string]string
}

// NewState returns an empty unlocked state.
func NewState() *State {
	return &State{
		Mapping: make(map[string]map[string]string),
	}
}

// CopyMapping returns a deep copy of the mapping.
func (s *State) CopyMapping() map[string]map[string]string {
	out := make(map[string]map[string]string, len(s.Mapping))
	for f, m := range s.Mapping {
		c := make(map[string]string, len(m))
		for k, v := range m {
			c[k] = v
		}
		out[f] = c
	}
	return out
}

// RecordOriginal stores the original value before it is replaced with a token.
func (s *State) RecordOriginal(file, key, original string) {
	if s.Mapping[file] == nil {
		s.Mapping[file] = make(map[string]string)
	}
	s.Mapping[file][key] = original
}

// Original returns the stored original value for file+key.
func (s *State) Original(file, key string) (string, bool) {
	if m, ok := s.Mapping[file]; ok {
		v, ok := m[key]
		return v, ok
	}
	return "", false
}

// SecretCount returns the total number of individual locked key entries.
func (s *State) SecretCount() int {
	n := 0
	for _, m := range s.Mapping {
		n += len(m)
	}
	return n
}

// Files returns the list of files that have locked entries.
func (s *State) Files() []string {
	files := make([]string, 0, len(s.Mapping))
	for f := range s.Mapping {
		files = append(files, f)
	}
	return files
}

// Clear wipes the mapping (called after unlock restores all values).
func (s *State) Clear() {
	s.Mapping = make(map[string]map[string]string)
	s.Locked = false
	s.LockedAt = time.Time{}
}
