package diff

import "testing"

func TestDiff(t *testing.T) {
	tests := []struct {
		name     string
		old, new string
		want     string
	}{
		{"same", "a\nb\n", "a\nb\n", ""},
		{
			"change",
			"a\nb\nc\n",
			"a\nB\nc\n",
			"diff old new\n--- old\n+++ new\n@@ -1,3 +1,3 @@\n a\n-b\n+B\n c\n",
		},
		{
			"missing newline",
			"a\nb",
			"a\nb\n",
			"diff old new\n--- old\n+++ new\n@@ -1,2 +1,2 @@\n a\n-b\n\\ No newline at end of file\n+b\n",
		},
		{
			"insert and delete",
			"x\n1\n2\n3\n4\n5\n6\n7\n8\n9\ny\n",
			"1\n2\n3\n4\n5\n6\n7\n8\n9\nz\n",
			"diff old new\n--- old\n+++ new\n@@ -1,4 +1,3 @@\n-x\n 1\n 2\n 3\n@@ -8,4 +7,4 @@\n 7\n 8\n 9\n-y\n+z\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := string(Diff("old", []byte(tt.old), "new", []byte(tt.new)))
			if got != tt.want {
				t.Errorf("got:\n%s\nwant:\n%s", got, tt.want)
			}
		})
	}
}
