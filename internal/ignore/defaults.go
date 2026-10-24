package ignore

// defaultExcludeRules are the built-in exclude rules baked into the ws binary.
// Users can suppress
// individual rules via suppress_defaults in ws/ignore.json.
var defaultExcludeRules = []Rule{
	// OS junk
	{Raw: "-:Thumbs.db", Include: false, Glob: false, Pattern: "Thumbs.db"},
	{Raw: "-:desktop.ini", Include: false, Glob: false, Pattern: "desktop.ini"},
	{Raw: "-:~*", Include: false, Glob: false, Pattern: "~*"},
	{Raw: "-g:*~", Include: false, Glob: true, Pattern: "*~"},
	{Raw: "-:.*", Include: false, Glob: false, Pattern: ".*"},

	// Temp and partial files (hidden ones, such as .foo.swp, .#foo and
	// .~lock.*#, are already caught by -:.*)
	{Raw: "-g:*.tmp", Include: false, Glob: true, Pattern: "*.tmp"},
	{Raw: "-g:*.temp", Include: false, Glob: true, Pattern: "*.temp"},
	{Raw: "-g:*.swp", Include: false, Glob: true, Pattern: "*.swp"},
	{Raw: "-g:*.swo", Include: false, Glob: true, Pattern: "*.swo"},
	{Raw: "-g:*.part", Include: false, Glob: true, Pattern: "*.part"},
	{Raw: "-g:*.crdownload", Include: false, Glob: true, Pattern: "*.crdownload"},
	{Raw: "-g:#*#", Include: false, Glob: true, Pattern: "#*#"},

	// Build artifact directories
	{Raw: "-:node_modules", Include: false, Glob: false, Pattern: "node_modules"},
	{Raw: "-:__pycache__", Include: false, Glob: false, Pattern: "__pycache__"},
	{Raw: "-:.venv", Include: false, Glob: false, Pattern: ".venv"},
	{Raw: "-:venv", Include: false, Glob: false, Pattern: "venv"},
	{Raw: "-:.jekyll-cache", Include: false, Glob: false, Pattern: ".jekyll-cache"},
	{Raw: "-:_site", Include: false, Glob: false, Pattern: "_site"},
	{Raw: "-g:*.egg-info", Include: false, Glob: true, Pattern: "*.egg-info"},

	// Compiled output
	{Raw: "-g:*.class", Include: false, Glob: true, Pattern: "*.class"},
	{Raw: "-g:*.pyc", Include: false, Glob: true, Pattern: "*.pyc"},
	{Raw: "-g:*.pyo", Include: false, Glob: true, Pattern: "*.pyo"},
	{Raw: "-g:*.pyd", Include: false, Glob: true, Pattern: "*.pyd"},
	{Raw: "-g:*.o", Include: false, Glob: true, Pattern: "*.o"},
	{Raw: "-g:*.obj", Include: false, Glob: true, Pattern: "*.obj"},
	{Raw: "-g:*.so", Include: false, Glob: true, Pattern: "*.so"},
	{Raw: "-g:*.a", Include: false, Glob: true, Pattern: "*.a"},
	{Raw: "-g:*.dll", Include: false, Glob: true, Pattern: "*.dll"},
	{Raw: "-g:*.dylib", Include: false, Glob: true, Pattern: "*.dylib"},
	{Raw: "-g:*.exe", Include: false, Glob: true, Pattern: "*.exe"},

	// Logs and crash dumps
	{Raw: "-g:*.log", Include: false, Glob: true, Pattern: "*.log"},
	{Raw: "-g:hs_err_pid*", Include: false, Glob: true, Pattern: "hs_err_pid*"},

	// Lock files
	{Raw: "-g:Gemfile.lock", Include: false, Glob: true, Pattern: "Gemfile.lock"},
	{Raw: "-g:package-lock.json", Include: false, Glob: true, Pattern: "package-lock.json"},
	{Raw: "-g:yarn.lock", Include: false, Glob: true, Pattern: "yarn.lock"},
	{Raw: "-g:poetry.lock", Include: false, Glob: true, Pattern: "poetry.lock"},
	{Raw: "-g:pnpm-lock.yaml", Include: false, Glob: true, Pattern: "pnpm-lock.yaml"},
	{Raw: "-g:composer.lock", Include: false, Glob: true, Pattern: "composer.lock"},
	{Raw: "-g:go.sum", Include: false, Glob: true, Pattern: "go.sum"},

	// Packages and archives
	{Raw: "-g:*.tar", Include: false, Glob: true, Pattern: "*.tar"},
	{Raw: "-g:*.tar.gz", Include: false, Glob: true, Pattern: "*.tar.gz"},
	{Raw: "-g:*.tgz", Include: false, Glob: true, Pattern: "*.tgz"},
	{Raw: "-g:*.deb", Include: false, Glob: true, Pattern: "*.deb"},
	{Raw: "-g:*.rpm", Include: false, Glob: true, Pattern: "*.rpm"},
	{Raw: "-g:*.run", Include: false, Glob: true, Pattern: "*.run"},
	{Raw: "-g:*.zip", Include: false, Glob: true, Pattern: "*.zip"},
	{Raw: "-g:*.rar", Include: false, Glob: true, Pattern: "*.rar"},
	{Raw: "-g:*.7z", Include: false, Glob: true, Pattern: "*.7z"},
	{Raw: "-g:*.jar", Include: false, Glob: true, Pattern: "*.jar"},
	{Raw: "-g:*.war", Include: false, Glob: true, Pattern: "*.war"},
	{Raw: "-g:*.ear", Include: false, Glob: true, Pattern: "*.ear"},
	{Raw: "-g:*.whl", Include: false, Glob: true, Pattern: "*.whl"},
	{Raw: "-g:*.egg", Include: false, Glob: true, Pattern: "*.egg"},
	{Raw: "-g:*.gem", Include: false, Glob: true, Pattern: "*.gem"},

	// Large datasets
	{Raw: "-g:*.csv", Include: false, Glob: true, Pattern: "*.csv"},
	{Raw: "-g:*.orc", Include: false, Glob: true, Pattern: "*.orc"},
	{Raw: "-g:*.parquet", Include: false, Glob: true, Pattern: "*.parquet"},
}
