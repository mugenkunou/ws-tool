package workspace

import _ "embed"

// RunbooksFile is the runbooks file, relative to the workspace. Its table maps
// what a user asks, in any words, to the task's entry; the entry holds the
// route to follow (spec §2.2).
const RunbooksFile = "references/RUNBOOKS.md"

// RunbooksSeed is what `ws init` writes when RunbooksFile is missing: the
// purpose, and an empty table for runs to fill in.
//
//go:embed runbooks.md
var RunbooksSeed string
