package messenger

import "testing"

func TestMediaExtensionIgnoresActiveSuffixes(t *testing.T) {
	for _, tc := range []struct{ mime, name, want string }{
		{"video/mp4", "untrusted.desktop", ".video"},
		{"application/x-desktop", "clip.desktop", ".bin"},
		{"application/x-shellscript", "run.sh", ".bin"},
		{"image/jpeg", "photo.JPG", ".jpg"},
		{"video/x-f4v", "clip.f4v", ".f4v"},
		{"image/png", "", ".png"},
		{"audio/ogg", "voice", ".audio"},
	} {
		if got := mediaExtension(tc.mime, tc.name); got != tc.want {
			t.Errorf("mediaExtension(%q, %q) = %q, want %q", tc.mime, tc.name, got, tc.want)
		}
	}
}
