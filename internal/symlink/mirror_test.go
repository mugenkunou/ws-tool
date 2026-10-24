package symlink

import "testing"

func TestMirrorPath(t *testing.T) {
	const home = "/home/u"
	for _, c := range []struct{ host, want string }{
		{"/home/u/.gitconfig", "symlinks/home/.gitconfig"},
		{"/home/u/.ssh/id_rsa", "symlinks/home/.ssh/id_rsa"},
		{"/home/u/.config/nvim", "symlinks/home/.config/nvim"},
		{"/home/u/.config/nvim/", "symlinks/home/.config/nvim"}, // trailing slash
		{"/home/u/.config//Code - Insiders/User/settings.json", "symlinks/home/.config/Code - Insiders/User/settings.json"},
		{"/home/u/My Docs/a b.txt", "symlinks/home/My Docs/a b.txt"}, // spaces
		{"/home/u/./.bashrc", "symlinks/home/.bashrc"},
		{"/etc/hosts", "symlinks/system/etc/hosts"},
		{"/etc/docker/daemon.json", "symlinks/system/etc/docker/daemon.json"},
		{"/home/user2/.bashrc", "symlinks/system/home/user2/.bashrc"}, // a sibling of home is not inside it
		{"/home/u/../v/.x", "symlinks/system/home/v/.x"},              // cleaned first: lands outside home
		// no storage path
		{"/home/u", ""},
		{"/home/u/", ""},
		{"/", ""},
		{"relative/path", ""},
		{"", ""},
		{"~/.bashrc", ""},
	} {
		if got := MirrorPath(c.host, home); got != c.want {
			t.Errorf("MirrorPath(%q) = %q, want %q", c.host, got, c.want)
		}
	}
	// A path that still holds .. after cleaning cannot be absolute-clean; relative ones are refused.
	if got := MirrorPath("../x", home); got != "" {
		t.Errorf("MirrorPath(../x) = %q", got)
	}
	// With no home known, everything is system.
	if got := MirrorPath("/home/u/.bashrc", ""); got != "symlinks/system/home/u/.bashrc" {
		t.Errorf("no home: %q", got)
	}
}

func TestHostPath(t *testing.T) {
	const home = "/home/u"
	for _, c := range []struct{ original, want string }{
		{"symlinks/home/.gitconfig", "/home/u/.gitconfig"},
		{"symlinks/home/.config/nvim", "/home/u/.config/nvim"},
		{"symlinks/home/.config/nvim/", "/home/u/.config/nvim"},
		{"symlinks/home/My Docs/a b.txt", "/home/u/My Docs/a b.txt"},
		{"symlinks/system/etc/hosts", "/etc/hosts"},
		{"symlinks/system/home/user2/.bashrc", "/home/user2/.bashrc"},
		// not storage paths
		{"symlinks", ""},
		{"symlinks/home", ""},
		{"symlinks/system", ""},
		{"symlinks/other/x", ""},
		{"configs/bashrc", ""},
		{"symlinks/home/../../etc/passwd", ""},
		{"", ""},
	} {
		if got := HostPath(c.original, home); got != c.want {
			t.Errorf("HostPath(%q) = %q, want %q", c.original, got, c.want)
		}
	}
	if got := HostPath("symlinks/home/.bashrc", ""); got != "" {
		t.Errorf("a home path with no home: %q", got)
	}
}

// TestMirrorRoundTrip: HostPath(MirrorPath(p)) is the cleaned p, for paths with
// spaces, dots, a trailing slash and unusual characters.
func TestMirrorRoundTrip(t *testing.T) {
	const home = "/home/u"
	for _, p := range []string{
		"/home/u/.gitconfig", "/home/u/dir with spaces/file name.txt", "/home/u/.config/Code - Insiders/User/",
		"/home/u/a/b/c/d/e/f.txt", "/home/u/ünï/çödé", "/etc/hosts", "/etc/my conf/a b", "/opt/x/", "/var/lib/a.b/.hidden",
		"/home/u2/x", "/home/u/.a.b",
	} {
		m := MirrorPath(p, home)
		if m == "" {
			t.Fatalf("MirrorPath(%q) is empty", p)
		}
		want := p
		if want != "/" && want[len(want)-1] == '/' {
			want = want[:len(want)-1]
		}
		if got := HostPath(m, home); got != want {
			t.Errorf("%q → %q → %q", p, m, got)
		}
	}
}
