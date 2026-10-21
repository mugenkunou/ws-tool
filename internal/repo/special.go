package repo

import (
	"fmt"
)

// DescribeSync is the human description of a sync action; short is the repo
// path as the caller wants it displayed.
func DescribeSync(sp SyncPlan, short string, rebase bool) string {
	mode := "merge"
	if rebase {
		mode = "rebase"
	}
	switch sp.Strategy {
	case SyncPull:
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", mode, short, sp.Detail)
		}
		return fmt.Sprintf("Pull %s  (%s, ff)", short, sp.Detail)
	case SyncPush:
		return fmt.Sprintf("Push %s  (%s)", short, sp.Detail)
	case SyncCommitPush:
		return fmt.Sprintf("Commit and push %s  (%s)", short, sp.Detail)
	case SyncPullPush:
		if sp.Status.Dirty {
			return fmt.Sprintf("Commit, pull (%s), push %s  (%s)", mode, short, sp.Detail)
		}
		return fmt.Sprintf("Pull (%s) + push %s  (%s)", mode, short, sp.Detail)
	default:
		return fmt.Sprintf("Sync %s", short)
	}
}
