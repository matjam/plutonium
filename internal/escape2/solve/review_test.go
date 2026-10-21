package solve_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	plutonium "github.com/matjam/plutonium"
	"github.com/matjam/plutonium/internal/escape2"
	"github.com/matjam/plutonium/internal/load"
	"github.com/matjam/plutonium/internal/types2"
)

// The programs of the adversarial reviews of the escape analysis: each of
// review2, review3 (internal/escape/testdata) and review4 (testdata/review4,
// the fourth review's programs) keeps, returns or stores a pointer into a
// frame that is gone, and must be rejected by this analysis; the controls
// (want.txt "accepted") must be accepted. The fourth review's programs have
// no controls.
//
// A program this analysis cannot reject yet for a reason outside the solver
// (a gap of a stage not built, named in knownGaps) is listed there with the
// reason; the test fails when such a program starts being rejected, so that
// the list stays true.

// knownGaps are the programs not rejected yet, with the reason.
// untriaged marks a program not yet looked at: the two examined of each
// kind were gaps of the EIR builder (named in the entries below), not of
// the solver.
const untriaged = "untriaged: EIR builder, or a later stage (3: tags; 4: loops, owners, arenas)"

var knownGaps = map[string]string{
	"review2/f21_fmt_sharpv_gostring_nested":                  untriaged,
	"review2/f38_fmt_named_slice_type_string":                 untriaged,
	"review2/k08_key_elem_assert_in_callee":                   untriaged,
	"review2/k26_key_elem_after_assert_in_callee":             untriaged,
	"review2/s10_namedfunc_method_value":                      "EIR: a literal that captures x is built with no record (OpClosure Obj NoObj, no Args)",
	"review2/s11_struct_of_closure_iface":                     untriaged,
	"review2/s12_named_array_of_closure_iface":                untriaged,
	"review2/s13_generic_conv_to_named":                       untriaged,
	"review2/s14_closure_in_any_assert_named":                 untriaged,
	"review2/s15_closure_elem_key":                            untriaged,
	"review2/v01_method_expr_value":                           untriaged,
	"review3/a12_chan_closure":                                untriaged,
	"review3/b03_task_func_param":                             untriaged,
	"review3/b09_scope_task_afterfunc":                        untriaged,
	"review3/b13_waitgroup_task_closure":                      untriaged,
	"review3/b15_scope_through_pointer_param":                 untriaged,
	"review3/b17_scope_loop_body_ptr_via_helper":              untriaged,
	"review3/b18_scope_loop_body_slice_in_struct":             untriaged,
	"review3/b19_scope_task_stores_to_outer_var":              untriaged,
	"review3/c06_vec_local_at_to_global":                      untriaged,
	"review3/c07_vec_local_returned_holding_frame_ptr":        untriaged,
	"review3/c09_vec_of_closure_push":                         untriaged,
	"review3/d09_defer_named_result_closure":                  untriaged,
	"review3/e07_local_map_returned":                          untriaged,
	"review3/g09_generic_method_through_iface_type_param":     untriaged,
	"review3/g13_generic_ptr_constraint_store_typed":          untriaged,
	"review3/k01_atomic_pointer_load":                         untriaged,
	"review3/k02_atomic_pointer_swap":                         untriaged,
	"review3/k03_atomic_pointer_global_store":                 untriaged,
	"review3/k04_atomic_pointer_returned":                     untriaged,
	"review3/k05_atomic_cas_then_load":                        untriaged,
	"review3/l06_loop_ptr_to_outer_via_alias":                 "EIR: loop variables are not LifeIter (fnb.finish sets every Object.Loop to -1)",
	"review3/l16_rangefunc_yield_closure":                     untriaged,
	"review3/l22_loop_ptr_kept_via_method_on_outer":           untriaged,
	"review3/l23_loop_closure_into_outer_array_elem":          untriaged,
	"review3/l24_loop_header_var_holds_body_ptr":              untriaged,
	"review3/l25_loop_ptr_via_helper_storing_into_param":      untriaged,
	"review3/l26_loop_iface_copy_kept":                        untriaged,
	"review3/l27_loop_range_array_value_addr":                 untriaged,
	"review3/l29_loop_view_kept_through_two_vars":             untriaged,
	"review3/l30_loop_kept_via_closure_returned_ptr":          untriaged,
	"review3/n10_value_method_keeps_receiver_addr":            "EIR: v, ok := a.(I) loads through the interface value instead of copying it",
	"review3/n21_generic_chain_promoted_value_method":         untriaged,
	"review3/n25_generic_chain_promoted_no_sink":              untriaged,
	"review3/n26_generic_direct_promoted_value_method":        untriaged,
	"review3/n28_nongeneric_promoted_value_method":            untriaged,
	"review3/n29_generic_direct_promoted_pointer_method":      untriaged,
	"review3/n32_generic_ptr_tparam_renamed":                  untriaged,
	"review3/n33_generic_ptr_tparam_stringer_ordinary":        untriaged,
	"review3/o05_shared_holds_frame_ptr":                      untriaged,
	"review3/o14_drop_in_vec_of_owners_local":                 untriaged,
	"review3/o17_shared_field_view_returned":                  untriaged,
	"review3/o18_owned_field_view_returned":                   untriaged,
	"review3/p01_init_local_closure":                          untriaged,
	"review3/r04_method_expr_in_pkg_var":                      untriaged,
	"review3/x03_region_closure_called_after_second_call":     untriaged,
	"review3/x04_region_iface_method_returns_closure":         untriaged,
	"review4/cd01_newcond_local_mutex":                        untriaged,
	"review4/e03_errorbuf_wrapped_by_errorf":                  untriaged,
	"review4/e04_errorbuf_joined":                             untriaged,
	"review4/e08_errorbuf_sprint_control":                     untriaged,
	"review4/ea01_errors_as_target_frame":                     untriaged,
	"review4/hs12_local_vec_ptr_store_then_moved_global":      untriaged,
	"review4/hs13_vec_in_struct_behind_ptr_push_then_ptr":     untriaged,
	"review4/it01_iter_concat_returned":                       untriaged,
	"review4/l15j_yield_ptr_body_stores_yield_value_closure":  untriaged,
	"review4/m01_method_expr_keeps_recv":                      untriaged,
	"review4/m03_method_expr_value_returns_field":             untriaged,
	"review4/m05_method_expr_in_pkg_var_generic":              untriaged,
	"review4/n08_method_expr_generic_type":                    untriaged,
	"review4/n22_iface_embedded_struct_generic_constraint":    untriaged,
	"review4/o10_owner_param_generic_moved_into_holder":       untriaged,
	"review4/o12_owner_in_vec_drop_keeps_field":               untriaged,
	"review4/n28_index_sugar_reader_tparam_embedded":          "sched: a type-parameter call reaches no candidate for a method promoted from an embedded interface (W{R}.Index); the candidates must include the interface call's",
	"review4/s21_fromutf8_local_bytes":                        untriaged,
	"review4/s22_fromutf8lossy_local_bytes":                   untriaged,
	"review4/s23_builder_string_kept":                         untriaged,
	"review4/ss11_link_closure_records":                       "EIR: a literal that captures x is built with no record (OpClosure Obj NoObj, no Args)",
	"review4/ss15_link_vec_moved_into_other":                  untriaged,
	"review4/tp14_func_tparam_closure_capturing_frame_called": untriaged,
	"review4/w01_local_waitgroup_task_no_wait":                untriaged,
	"review4/w02_local_waitgroup_task_wait_in_branch":         untriaged,
	"review4/w03_local_waitgroup_ptr_to_helper_task":          untriaged,
}

// knownFalseRejections are controls this analysis rejects, with the reason.
var knownFalseRejections = map[string]string{
	"review3/p05_pkg_init_iter_seq": "stage 2 unions every func value of the iterator's signature at a call through a package variable, and one of them keeps its argument (D47 settles calls through func values)",
}

func loadFile(t *testing.T, path string) *load.Program {
	t.Helper()
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "main.p"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := &load.Config{Tags: load.BuildTags(false, false, nil), Stdlib: plutonium.Stdlib(), Sizes: types2.SizesFor("gc", "amd64")}
	prog, err := load.Load(cfg, dir)
	if err != nil {
		t.Skipf("load: %v", err) // the type checker rejects it: not this analysis's case
	}
	return prog
}

// controls reads the names a want.txt marks "accepted".
func controls(t *testing.T, dir string) map[string]bool {
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

func TestReviews(t *testing.T) {
	if testing.Short() {
		t.Skip("loads every review program")
	}
	for _, dir := range []string{
		"../../escape/testdata/review2",
		"../../escape/testdata/review3",
		"testdata/review4",
	} {
		files, _ := filepath.Glob(filepath.Join(dir, "*.p"))
		if len(files) == 0 {
			t.Fatalf("no programs in %s", dir)
		}
		ctl := controls(t, dir)
		t.Run(filepath.Base(dir), func(t *testing.T) {
			for _, f := range files {
				name := strings.TrimSuffix(filepath.Base(f), ".p")
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					prog := loadFile(t, f)
					err := escape2.Check(prog)
					if err != nil && strings.Contains(err.Error(), "internal compiler error") {
						t.Fatalf("internal error: %v", err)
					}
					// Only errors in the program itself count: the library's are
					// false rejections of their own (listed by TestLibrary).
					if err != nil && !strings.Contains(err.Error(), "/main.p:") {
						err = nil
					}
					gap, isGap := knownGaps[filepath.Base(dir)+"/"+name]
					switch {
					case ctl[name] && err != nil && knownFalseRejections[filepath.Base(dir)+"/"+name] == "":
						t.Errorf("control rejected: %v", err)
					case ctl[name] && err == nil && knownFalseRejections[filepath.Base(dir)+"/"+name] != "":
						t.Errorf("control accepted now; remove it from knownFalseRejections")
					case !ctl[name] && err == nil && !isGap:
						t.Errorf("accepted: the program keeps a pointer into a dead frame")
					case !ctl[name] && err != nil && isGap:
						t.Errorf("rejected now; remove it from knownGaps (%s)", gap)
					}
				})
			}
		})
	}
}
