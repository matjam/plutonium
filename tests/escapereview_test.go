package tests

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The programs of the adversarial reviews of the escape analysis: each of
// review2, review3 and review4 (testdata/escape) keeps, returns or stores a
// pointer into a frame that is gone, and must be rejected by the escape
// analysis (internal/escape2); the controls (want.txt
// "accepted") must be accepted. The fourth review's programs have no
// controls beyond those its want.txt names.
//
// A program the analysis cannot reject yet for a reason outside the solver
// (a gap of a stage not built, named in knownEscapeGaps) is listed there
// with the reason; the test fails when such a program starts being
// rejected, so that the list stays true.

// knownEscapeGaps are the programs not rejected yet, with the reason.
var knownEscapeGaps = map[string]string{
	// The contract table gives rt.ReleaseValues and ZeroValues no effects,
	// though they run the elements' Drop methods.
	"review3/o14_drop_in_vec_of_owners_local": "contract: ReleaseValues/ZeroValues run element drops",
}

// rejectedBeforeEscape are the programs a check that runs before the
// escape analysis rejects (the lowering's closure and scope rules), with
// the code it reports. The build stops there, so the analysis's own verdict
// on them cannot be seen through pluto; the Go test ran the analysis on the
// loaded program directly. The test requires that code, so that the list
// stays true.
var rejectedBeforeEscape = map[string]string{
	"review3/b03_task_func_param":                         "E2594",
	"review3/b05_scope_returned":                          "E3100",
	"review3/b09_scope_task_afterfunc":                    "E2594",
	"review3/b11_scope_in_owner_struct_returned":          "E2594",
	"review3/b13_waitgroup_task_closure":                  "E2594",
	"review3/b15_scope_through_pointer_param":             "E2594",
	"review3/b17_scope_loop_body_ptr_via_helper":          "E3401",
	"review3/b18_scope_loop_body_slice_in_struct":         "E3401",
	"review3/d09_defer_named_result_closure":              "E3100",
	"review3/d10_defer_method_value_named_result":         "E3100",
	"review3/x02_region_refill_in_loop":                   "E3101",
	"review3/x03_region_closure_called_after_second_call": "E3101",
}

// knownEscapeFalseRejections are controls the analysis rejects, with the
// reason.
var knownEscapeFalseRejections = map[string]string{}

// reviewControls reads the names a want.txt marks "accepted".
func reviewControls(dir string) map[string]bool {
	out := map[string]bool{}
	data, err := os.ReadFile(filepath.Join(dir, "want.txt"))
	if err != nil {
		return out
	}
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		name, first, ok := strings.Cut(line, ": ")
		if ok && first == "accepted" {
			out[name] = true
		}
	}
	return out
}

// escapeDiag matches the code of an escape analysis error (E3001-E3099) at
// the end of a diagnostic's explanation.
var escapeDiag = regexp.MustCompile(`run "pluto explain E30\d\d"`)

// escapeErrorsInMain returns the escape analysis errors in out, pluto's
// output, that are about main.p: the library's are false rejections of
// their own.
func escapeErrorsInMain(out string) []string {
	return errorsInMain(out, escapeDiag)
}

// errorsInMain returns the errors in out about main.p whose explanation
// matches code.
func errorsInMain(out string, code *regexp.Regexp) []string {
	var errs []string
	head := ""
	for _, line := range strings.Split(out, "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, " "):
			if head != "" && code.MatchString(line) {
				errs = append(errs, head)
				head = ""
			}
		case strings.Contains(line, "/main.p:"):
			head = line
		default:
			head = ""
		}
	}
	return errs
}

func TestEscapeReviews(t *testing.T) {
	if testing.Short() {
		t.Skip("builds every review program")
	}
	t.Parallel()
	for _, review := range []string{"review2", "review3", "review4"} {
		dir := filepath.Join("testdata", "escape", review)
		files, _ := filepath.Glob(filepath.Join(dir, "*.p"))
		if len(files) == 0 {
			t.Fatalf("no programs in %s", dir)
		}
		ctl := reviewControls(dir)
		t.Run(review, func(t *testing.T) {
			t.Parallel()
			for _, f := range files {
				name := strings.TrimSuffix(filepath.Base(f), ".p")
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					src, err := os.ReadFile(f)
					if err != nil {
						t.Fatal(err)
					}
					pkg := writeProgram(t, string(src))
					// pluto doc loads and type-checks the program without
					// the later passes: one the type checker rejects is not
					// this analysis's case.
					if out, status := runPluto(t, pkg, nil, "doc", "-brief"); status != 0 {
						t.Skipf("load: %s", out)
					}
					out, _ := runPluto(t, pkg, nil, "build", "-o", filepath.Join(t.TempDir(), "a.out"), ".")
					if strings.Contains(out, "internal compiler error") {
						t.Fatalf("internal error: %s", out)
					}
					key := review + "/" + name
					if code, ok := rejectedBeforeEscape[key]; ok {
						if len(errorsInMain(out, regexp.MustCompile(`run "pluto explain `+code+`"`))) == 0 {
							t.Errorf("not rejected with %s before the escape analysis; update rejectedBeforeEscape\n%s", code, out)
						}
						return
					}
					errs := escapeErrorsInMain(out)
					gap, isGap := knownEscapeGaps[key]
					switch {
					case ctl[name] && len(errs) > 0 && knownEscapeFalseRejections[key] == "":
						t.Errorf("control rejected: %s", strings.Join(errs, "\n"))
					case ctl[name] && len(errs) == 0 && knownEscapeFalseRejections[key] != "":
						t.Errorf("control accepted now; remove it from knownEscapeFalseRejections")
					case !ctl[name] && len(errs) == 0 && !isGap:
						t.Errorf("accepted: the program keeps a pointer into a dead frame\n%s", out)
					case !ctl[name] && len(errs) > 0 && isGap:
						t.Errorf("rejected now; remove it from knownEscapeGaps (%s)", gap)
					}
				})
			}
		})
	}
}
