package trash

// RecordShellProvisions is a no-op in the phase-1 layout.
// The old manifest-based provision tracking has been removed.
// Kept for call-site compatibility during the migration.
func RecordShellProvisions(_ string) error {
	return nil
}

// RecordExplorerProvision is a no-op in the phase-1 layout.
// The old manifest-based provision tracking has been removed.
// Kept for call-site compatibility during the migration.
func RecordExplorerProvision(_, _ string) error {
	return nil
}
