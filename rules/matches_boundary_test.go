package rules

import "testing"

func TestRuleMatchesTrailingSlashAndCaseBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		rulePath string
		testPath string
		fold     bool
		want     bool
	}{
		{
			name:     "plain directory matches itself",
			rulePath: "/private",
			testPath: "/private",
			fold:     false,
			want:     true,
		},
		{
			name:     "plain directory matches trailing-slash form",
			rulePath: "/private",
			testPath: "/private/",
			fold:     false,
			want:     true,
		},
		{
			name:     "trailing-slash rule matches directory without slash",
			rulePath: "/private/",
			testPath: "/private",
			fold:     false,
			want:     true,
		},
		{
			name:     "trailing-slash rule matches child",
			rulePath: "/private/",
			testPath: "/private/secret.txt",
			fold:     false,
			want:     true,
		},
		{
			name:     "trailing-slash rule rejects underscore sibling",
			rulePath: "/private/",
			testPath: "/private_secret/secret.txt",
			fold:     false,
			want:     false,
		},
		{
			name:     "trailing-slash rule rejects hyphen sibling",
			rulePath: "/private/",
			testPath: "/private-public/secret.txt",
			fold:     false,
			want:     false,
		},
		{
			name:     "repeated trailing slashes are harmless",
			rulePath: "/private///",
			testPath: "/private/secret.txt",
			fold:     false,
			want:     true,
		},
		{
			name:     "exact case remains case-sensitive",
			rulePath: "/Private/",
			testPath: "/Private",
			fold:     false,
			want:     true,
		},
		{
			name:     "case variant is not widened without folding",
			rulePath: "/Private/",
			testPath: "/private",
			fold:     false,
			want:     false,
		},
		{
			name:     "folded trailing-slash rule matches directory",
			rulePath: "/Private/",
			testPath: "/PRIVATE",
			fold:     true,
			want:     true,
		},
		{
			name:     "folded trailing-slash rule matches slashed directory",
			rulePath: "/Private/",
			testPath: "/pRiVaTe/",
			fold:     true,
			want:     true,
		},
		{
			name:     "folded trailing-slash rule matches child",
			rulePath: "/Private/",
			testPath: "/PRIVATE/secret.txt",
			fold:     true,
			want:     true,
		},
		{
			name:     "folded trailing-slash rule rejects sibling",
			rulePath: "/Private/",
			testPath: "/PRIVATE_backup/secret.txt",
			fold:     true,
			want:     false,
		},
		{
			name:     "folded nested rule rejects sibling segment",
			rulePath: "/projects/Private/",
			testPath: "/projects/PRIVATE_evil/secret.txt",
			fold:     true,
			want:     false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rule := &Rule{Path: tc.rulePath}
			if got := rule.Matches(tc.testPath, tc.fold); got != tc.want {
				t.Fatalf("Rule{Path: %q}.Matches(%q, fold=%v) = %v; want %v",
					tc.rulePath, tc.testPath, tc.fold, got, tc.want)
			}
		})
	}
}

// The pre-folding API accepted only the path. Keep that call form working for
// older callers and tests; omitted folding means exact-case matching.
func TestRuleMatchesOmittingFoldIsCaseSensitive(t *testing.T) {
	t.Parallel()

	rule := &Rule{Path: "/Private/"}

	if !rule.Matches("/Private") {
		t.Error("trailing-slash rule did not match the exact directory")
	}
	if rule.Matches("/private") {
		t.Error("one-argument matching unexpectedly folded case")
	}
	if rule.Matches("/Private_evil/file") {
		t.Error("rule matched a sibling directory")
	}
}
