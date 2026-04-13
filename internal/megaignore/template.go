package megaignore

import "strings"

const Template = `# OS junk
-:Thumbs.db
-:desktop.ini
-:~*
-g:*~
-:.*

# Build artifact directories
-:node_modules
-:__pycache__
-:.venv
-:venv
-:.jekyll-cache
-:_site
-:*.egg-info

# Compiled output
-g:*.class
-g:*.pyc
-g:*.pyo
-g:*.pyd
-g:*.o
-g:*.obj
-g:*.so
-g:*.a
-g:*.dll
-g:*.dylib
-g:*.exe

# Logs and crash dumps
-g:*.log
-g:hs_err_pid*

# Lock files
-g:Gemfile.lock
-g:package-lock.json
-g:yarn.lock
-g:poetry.lock
-g:pnpm-lock.yaml
-g:composer.lock
-g:go.sum

# Packages and archives
-g:*.tar
-g:*.tar.gz
-g:*.tgz
-g:*.deb
-g:*.rpm
-g:*.run
-g:*.zip
-g:*.rar
-g:*.7z
-g:*.jar
-g:*.war
-g:*.ear
-g:*.whl
-g:*.egg
-g:*.gem

# Large datasets
-g:*.csv
-g:*.orc
-g:*.parquet

# Safe harbors
+:ws
+g:ws/**

# Sync everything else
-s:*
`

func EnsureFinalSentinel(content string) string {
	trimmed := strings.TrimSpace(content)
	if trimmed == "" {
		return "-s:*\n"
	}
	if strings.HasSuffix(trimmed, "-s:*") {
		return trimmed + "\n"
	}
	return trimmed + "\n-s:*\n"
}
