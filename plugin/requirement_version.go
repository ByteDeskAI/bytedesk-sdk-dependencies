package plugin

import (
	"fmt"
	"strings"

	semver "github.com/Masterminds/semver/v3"
)

// ValidateVersionConstraint rejects malformed requires.version expressions.
// Empty constraints retain compatibility with unversioned requirements.
func (r Requirement) ValidateVersionConstraint() error {
	constraint := strings.TrimSpace(r.Version)
	if constraint == "" {
		return nil
	}
	if _, err := semver.NewConstraint(constraint); err != nil {
		return fmt.Errorf("requires %s version constraint: %w", r.ID, err)
	}
	return nil
}

// MatchesVersion evaluates requires.version using Masterminds semantic-version
// ranges (comparisons, AND/OR, caret, tilde and wildcards). Ranges exclude
// prereleases unless explicitly admitted by a prerelease comparator. A leading
// v and short numeric versions are normalized by the library. Empty constraints
// accept legacy version strings without parsing them. Invalid constrained
// versions and malformed expressions return an error, never a match.
func (r Requirement) MatchesVersion(actual string) (bool, error) {
	constraint := strings.TrimSpace(r.Version)
	if constraint == "" {
		return true, nil
	}
	parsed, err := semver.NewConstraint(constraint)
	if err != nil {
		return false, fmt.Errorf("requires %s version constraint: %w", r.ID, err)
	}
	version, err := semver.NewVersion(strings.TrimSpace(actual))
	if err != nil {
		return false, fmt.Errorf("required plugin %s version: %w", r.ID, err)
	}
	return parsed.Check(version), nil
}
