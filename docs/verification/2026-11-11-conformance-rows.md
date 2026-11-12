# Conformance rows of 19.3, 2026-11-11

The conformance table of section 19.3 (rows 1 to 129) against the race check on `main` at `ed3c19d6`.
Each new test was built with that compiler (`~/build/integ4/pluto` on kraken), once plain and once with
`--brief`, the way `go test ./tests -run TestCorpus -update` builds a corpus case. Accepted programs were
also run, each finishing in under a second.

## Counts

- Rows with a test on `main` before this work: 71.
- Rows given a test here: 24, in 24 directories (row 104 has two, rows 127 and 128 share one).
- Rows that fail: 9 (rows 2, 3, 9, 44, 68, 99, 116, 117 and 121).
- Rows ambiguous: 1 (row 4).
- Rows with a test only on a branch (`work/race3b`, the same commits as `work/wait1`): 24.

The programs of the failing rows and of row 4 are in `docs/verification/2026-11-11-row-failures/row_N/`,
each with a `README` that gives the required and the printed result and the likely roadmap item.

## Notes on the shapes

- Rows 97, 98, 99 and 104: `make([]int, n)` is E2014 in Plutonium. Rows 97 and 104 (rejected) use a
  package slice of a package array (`var store [4]int; var results = store[:]`); rows 98, 99 and 104
  (accepted) take `results` as `v.Slice()` of a local `vec.Make[int](n)`.
- Row 104 (accepted) passes `&results[i]` to the worker, a form 19.4 Partitions names; passing the whole
  slice is E3403.
- Row 104 (rejected) uses `wg.Add(1)` and `task worker(i)` with `defer wg.Done()`: `wg.Task` takes a
  `func()`, so with no capture its only form is row 97.
- Rows 109, 116 and 117: `Scores` is declared `owner`, as E3202 requires for its `vec.Vec` field.
- Row 120: `Cache`, its methods and the two static objects are in package `cache` of a module; `main`
  calls `Put` from two tasks and `Dump` after. The E3502 names `return c.m`.
- Row 129: the pool is `func get() *Counter { return alloc.New[Counter]() }`.
- Row 7: the reload and the readers use a mutex, so that the row's E3501 is the only error (with no lock,
  rule 1 adds E3502).
- Row 6: the program has no reader of `cfg`, so that E3003 is the only error.

## Failing rows

| Row | Required | Printed | Likely item |
|---|---|---|---|
| 2 | accepted | E3502 at `e.hits++` under `e.mu` (a `synced[Entry]` copied out of a package map) | 214 (maybe 192) |
| 3 | accepted | E3501: `std.w` (an `*os.File`) loaded as a view that may change | 214 |
| 9 | accepted | E3502 at `c.id++` on the `owned[Conn]` that `Get` popped; compiles without that write | 214 |
| 44 | a compile error | compiles | 215 (and 210) |
| 68 | accepted | E3314: the move of `b` after `&b.mu` was passed to `b.mu.Lock` | none open (moves check, E3314) |
| 99 | E2590 | E2594 | none open |
| 116 | E3502 | compiles | 232 (M2) |
| 117 | E3502 | compiles | 232 (M2) |
| 121 | E3503 | compiles | 232 (G3) |

Row 4 is ambiguous: it names E3002, but the 18.3 table gives E3003 for passing a frame view to a function
that stores it, which is what `Register("echo", &echoHandler{})` does. The compiler prints E3003.

## Known spec examples

The four `known:` entries of `tests/testdata/specexamples/examples.txt`, each block built as a complete
program with the `main` compiler:

- `type Config struct{ n int }` (srv and watch): prints E3501 at the task statement and E3502 at the
  second assignment. The stated reason is still true. It waits on no roadmap item: the spec block omits
  rule 1's E3502.
- `var events = make(chan int, 16)` (tick): compiles. The stated reason (item 215) is still true.
- peekWait: the stated reason (item 214, "not yet E3501") is no longer true. With `type Queue owner
  struct` the program prints E3501 at `v := *p`, as `tests/race/viewrejected_row_56_...` shows. The block
  as written declares `type Queue struct` with a `vec.Vec[int]` field, which is E3202 ("Queue must be
  declared owner"), so the block still fails, now for a spec defect and not for item 214.
- par: compiles. The stated reason (item 223) is still true.

## Table

| Row | Result required | Test or failure directory | Status |
|---|---|---|---|
| 1 | E3002 | `tests/race/pkgrejected_row_1_a_package_map_caches_a_frame_view_stored_under_its_lock` | passes (added) |
| 2 | accepted | `docs/verification/2026-11-11-row-failures/row_2` | fails |
| 3 | accepted | `docs/verification/2026-11-11-row-failures/row_3` | fails |
| 4 | E3002 | `docs/verification/2026-11-11-row-failures/row_4` | ambiguous (row says E3002, the 18.3 table gives E3003; prints E3003) |
| 5 | accepted | `tests/race/pkgaccepted_row_5_a_package_handler_table_initialized_with_static_objects` | passes (added) |
| 6 | E3003 | `tests/race/pkgrejected_row_6_a_reload_task_stores_a_frame_view_in_an_atomic_pointer` | passes (added) |
| 7 | E3501 | `tests/race/pkgrejected_row_7_a_reload_task_replaces_an_owned_config_readers_view` | passes (added) |
| 8 | accepted | `tests/race/pkgaccepted_row_8_a_synced_config_copied_out_and_replaced_under_a_mutex` | passes (added) |
| 9 | accepted | `docs/verification/2026-11-11-row-failures/row_9` | fails |
| 10 | accepted | `tests/race/pkgaccepted_row_10_package_stats_of_only_atomic_fields` | passes (added) |
| 11 | accepted | `tests/race/onceaccepted_row_11_an_owner_opened_inside_do` | passes |
| 12 | accepted | `tests/race/condaccepted_row_12_workers_wait_on_a_vector_queue` | passes |
| 13 | accepted | `tests/race/releaseaccepted_row_13_lock_then_defer_unlock` | passes |
| 14 | accepted; the second `Unlock` is a compile error | `tests/race/releaseaccepted_row_14_early_returns_that_each_unlock_once`<br>`tests/race/releaserejected_row_14_a_branch_that_unlocks_twice` | passes |
| 15 | accepted; the `Unlock` is a compile error | `tests/race/releaseaccepted_row_15_rlock_then_runlock`<br>`tests/race/releaserejected_row_15_rlock_then_unlock` | passes |
| 16 | E3501 | `tests/race/viewrejected_row_16_a_view_loaded_under_rlock_used_under_lock` | passes |
| 17 | accepted | `tests/race/releaseaccepted_row_17_an_embedded_mutex` | passes |
| 18 | accepted; the second is a compile error | `tests/race/releaseaccepted_row_18_hand_over_hand_with_locals`<br>`tests/race/releaserejected_row_18_an_unlock_through_a_loaded_pointer` | passes |
| 19 | a compile error; the pointer form is accepted | `tests/race/releaseaccepted_row_19_a_pointer_to_the_stripe`<br>`tests/race/releaserejected_row_19_a_variable_index_names_no_lock` | passes |
| 20 | accepted | `tests/race/condaccepted_row_20_a_wait_loop_keeps_its_lock` | passes |
| 21 | accepted | `tests/race/releaseaccepted_row_21_with_lock_and_defer` | passes |
| 22 | a compile error | `tests/race/releaserejected_row_22_two_parameters_for_one_mutex` | passes |
| 23 | a compile error | `tests/race/releaserejected_row_23_a_lock_through_a_pointer_loaded_from_memory` | passes |
| 24 | E3502 | `tests/race/releaserejected_row_24_assigning_a_lock_another_task_uses` | passes |
| 25 | a compile error | `tests/race/releaserejected_row_25_a_scoped_task_unlocks_the_starter_s_lock` | passes |
| 26 | a compile error | `tests/race/releaserejected_row_26_a_handler_unlocks_the_interrupted_code_s_lock` | passes |
| 27 | a compile error | `tests/race/releaserejected_row_27_an_unlock_through_a_method_value` | passes |
| 28 | a compile error | `tests/race/releaserejected_row_28_a_recursive_call_releases_the_lock_first` | passes |
| 29 | a compile error | `tests/race/releaserejected_row_29_a_task_unlocks_the_lock_its_starter_took` | passes |
| 30 | E3501 | `tests/race/freerejected_row_30_a_task_reads_an_arena_object_the_starter_resets` | passes |
| 31 | E3501 | `tests/race/loadrejected_row_31_a_view_loaded_after_the_owner_is_replaced` | passes |
| 32 | E3501 | `tests/race/loadrejected_row_32_an_atomic_load_of_a_view_main_later_drops` | passes |
| 33 | E2010 | `tests/race/sharedrejected_row_33_a_package_shared_reference_copied_by_two_tasks` | passes |
| 34 | accepted | `tests/race/loadaccepted_row_34_a_handler_table_filled_before_the_install` | passes |
| 35 | E3501 | `tests/race/loadrejected_row_35_a_handler_loads_a_view_main_then_replaces` | passes |
| 36 | E3501 | `tests/race/staticrejected_row_36_take_of_stdout_while_another_task_prints` | passes |
| 37 | accepted | `tests/race/staticaccepted_row_37_a_task_drops_its_own_file_and_prints` | passes |
| 38 | E3501 | `tests/race/viewrejected_row_38_a_view_loaded_under_the_lock_passed_to_a_task` | passes |
| 39 | accepted; E3501 | `tests/race/viewaccepted_row_39_a_deferred_argument_after_the_deferred_unlock`<br>`tests/race/viewrejected_row_39_a_deferred_argument_before_the_deferred_unlock` | passes |
| 40 | E3501 | `tests/race/viewrejected_row_40_a_view_loaded_with_the_lock_held_on_one_path` | passes |
| 41 | E3502 | `tests/race/guardrejected_row_41_two_tasks_push_under_different_locks` | passes |
| 42 | accepted | `tests/race/mapsaccepted_row_42_a_task_copies_a_synced_map_value_another_deletes_it` | passes (added) |
| 43 | E3501 | `tests/race/mapsrejected_row_43_a_map_view_into_a_vec_of_owners_used_after_unlock` | passes (added) |
| 44 | a compile error | `docs/verification/2026-11-11-row-failures/row_44` | fails |
| 45 | accepted (panics at run time if it parks) | `tests/race/waitaccepted_row_45_the_flush_example_a_send_behind_an_interface_call` | passes (added) |
| 46 | E3503 | `tests/race/oncerejected_row_46_a_second_task_reads_with_no_do` | passes |
| 47 | E3503 | `tests/race/oncerejected_row_47_a_helper_calls_do_on_some_paths` | passes |
| 48 | E3502 | `tests/race/oncerejected_row_48_reload_writes_outside_the_once` | passes |
| 49 | E3501 | `tests/race/viewrejected_row_49_the_second_example_a_view_kept_across_wait` | passes (added) |
| 50 | a compile error | `tests/race/condrejected_row_50_a_wait_given_an_rlocker` | passes |
| 51 | accepted | `tests/race/condaccepted_row_51_a_deferred_wait_after_a_deferred_unlock` | passes |
| 52 | a compile error | on branch `work/race3b` (also `work/wait1`) | on a branch (item 223) |
| 53 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 223) |
| 54 | a compile error | on branch `work/race3b` (also `work/wait1`) | on a branch (item 223) |
| 55 | accepted | `tests/race/staticaccepted_row_55_a_static_logger_guards_its_buffer` | passes |
| 56 | E3501 | `tests/race/viewrejected_row_56_a_view_kept_across_a_helper_that_waits` | passes |
| 57 | E3502 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 156, 224, 235) |
| 58 | E3402 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 227) |
| 59 | accepted | `tests/race/guardaccepted_row_59_a_datum_written_under_two_locks` | passes |
| 60 | E3502 | `tests/race/guardrejected_row_60_the_only_writer_uses_two_locks` | passes |
| 61 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 228) |
| 62 | accepted | `tests/race/onceaccepted_row_62_a_range_body_after_do` | passes |
| 63 | E3501 | `tests/race/viewrejected_row_63_a_view_used_after_the_lock_is_taken_again` | passes |
| 64 | accepted | `tests/race/staticaccepted_row_64_a_static_object_with_an_atomic_counter` | passes |
| 65 | a compile error | `tests/race/condrejected_row_65_wait_with_a_read_lock` | passes |
| 66 | accepted | `tests/race/onceaccepted_row_66_do_two_calls_deep` | passes |
| 67 | a compile error | `tests/race/copyrejected_row_67_a_value_receiver_on_a_struct_that_holds_a_mutex`<br>`tests/race/copyrejected_row_67_copying_a_cond` | passes |
| 68 | accepted (the receiver starts with nothing held) | `docs/verification/2026-11-11-row-failures/row_68` | fails |
| 69 | E3502 | `tests/race/guardrejected_row_69_one_helper_locks_two_mutexes` | passes |
| 70 | accepted | `tests/race/freeaccepted_row_70_a_writer_frees_under_the_write_lock` | passes |
| 71 | a compile error | `tests/race/releaserejected_row_71_a_scoped_task_calls_a_helper_that_unlocks` | passes |
| 72 | accepted (no party) | `tests/race/partyaccepted_row_72_a_task_statement_in_a_function_nothing_calls` | passes (added) |
| 73 | a compile error | on branch `work/race3b` (also `work/wait1`) | on a branch (item 229) |
| 74 | accepted; panics "blocking with preemption disabled" if the candidate yields | on branch `work/race3b` (also `work/wait1`) | on a branch (item 229) |
| 75 | a compile error | on branch `work/race3b` (also `work/wait1`) | on a branch (item 229) |
| 76 | accepted (the handler's region ends at the call) | on branch `work/race3b` (also `work/wait1`) | on a branch (item 229) |
| 77 | E3502 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 230) |
| 78 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 230) |
| 79 | E3501 | `tests/race/syncrejected_row_79_an_owner_holding_a_lock_a_task_uses_is_replaced` | passes |
| 80 | E3502 | `tests/race/syncrejected_row_80_a_free_under_the_lock_conflicts_with_the_lock_calls` | passes |
| 81 | accepted | `tests/race/syncaccepted_row_81_two_tasks_write_a_field_under_the_box_s_lock` | passes |
| 82 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 232) |
| 83 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 232) |
| 84 | E3501 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 232) |
| 85 | accepted | `tests/race/onceaccepted_row_85_a_literal_each_calls_on_the_caller_party` | passes |
| 86 | accepted | `tests/race/mapsaccepted_row_86_two_caches_each_guard_their_map_with_their_own_lock` | passes |
| 87 | E3502 | `tests/race/mapsrejected_row_87_a_method_returns_the_map_of_a_cache` | passes |
| 88 | accepted | `tests/race/mapsaccepted_row_88_len_range_and_a_parameter_that_keeps_the_map_in_place` | passes |
| 89 | E3502 | `tests/race/mapsrejected_row_89_reset_stores_a_map_that_is_not_new_where_it_is_stored` | passes |
| 90 | accepted | `tests/race/viewsaccepted_row_90_the_only_writer_ranges_over_its_vector_with_no_lock` | passes |
| 91 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 234) |
| 92 | not checked (unsafe code) | `tests/race/partyaccepted_row_92_an_rt_timer_function_is_not_checked` | passes (added) |
| 93 | E3502 | `tests/race/ownedrejected_row_93_hand_over_hand_on_owned_links` | passes |
| 94 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 234) |
| 95 | accepted by the race rules; the use panics if the push moved the storage (section 18.11) | `tests/race/viewaccepted_row_95_a_view_loaded_before_the_start_of_sharing` | passes |
| 96 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 234) |
| 97 | E3502 | `tests/race/partitionrejected_row_97_waitgroup_tasks_write_a_package_slice_by_atomic_index` | passes (added) |
| 98 | accepted | `tests/race/partitionaccepted_row_98_scope_tasks_write_their_own_result_elements` | passes (added) |
| 99 | E2590 | `docs/verification/2026-11-11-row-failures/row_99` | fails |
| 100 | E3004 | `tests/race/goshaperejected_row_100_a_frame_counter_passed_to_a_task` | passes (added) |
| 101 | accepted | `tests/race/mapsaccepted_row_101_shards_each_guard_their_map_with_their_own_lock` | passes |
| 102 | accepted | `tests/race/chanaccepted_row_102_a_worker_pool_with_jobs_and_results_channels` | passes |
| 103 | E2590 | `tests/race/goshaperejected_row_103_a_task_literal_capturing_loop_data` | passes (added) |
| 104 | E3502 with `wg.Task` (row 97); accepted with a `sync.Scope` (row 98) | `tests/race/goshapeaccepted_row_104_workers_write_results_by_index_with_a_scope`<br>`tests/race/goshaperejected_row_104_workers_write_results_by_index_with_a_waitgroup` | passes (added) |
| 105 | accepted | `tests/race/chanaccepted_row_105_a_done_channel_closed_to_cancel` | passes |
| 106 | accepted | `tests/race/goshapeaccepted_row_106_an_atomic_stop_flag_polled_by_tasks` | passes (added) |
| 107 | accepted | `tests/race/onceaccepted_row_107_a_singleton_returns_a_pointer_to_its_data` | passes |
| 108 | accepted | `tests/race/goshapeaccepted_row_108_a_read_mostly_config_map_under_an_rwmutex` | passes (added) |
| 109 | accepted (the body holds `sc.mu`, so every write of `sc.v` does) | `tests/race/iteraccepted_row_109_a_returned_literal_iterator_locks_the_receiver` | passes (added) |
| 110 | accepted | on branch `work/race3b` (also `work/wait1`) | on a branch (item 156, 224, 235) |
| 111 | E2022 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 156, 224, 235) |
| 112 | E2023 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 156, 224, 235) |
| 113 | E2022 | on branch `work/race3b` (also `work/wait1`) | on a branch (item 156, 224, 235) |
| 114 | a compile error | on branch `work/race3b` (also `work/wait1`) | on a branch (item 229) |
| 115 | E2584, reported once | on branch `work/race3b` (also `work/wait1`) | on a branch (item 156, 224, 235) |
| 116 | E3502 (`*p += 1` writes `sc.v` with no lock) | `docs/verification/2026-11-11-row-failures/row_116` | fails |
| 117 | E3502 (`*p += 1` writes with no lock) | `docs/verification/2026-11-11-row-failures/row_117` | fails |
| 118 | E3503 | `tests/race/oncerejected_row_118_a_task_entry_has_no_returned_do` | passes |
| 119 | E3503 (C code may call `get` first) | `tests/race/oncerejected_row_119_an_exported_function_is_an_entry` | passes |
| 120 | E3502, naming `return c.m` as the copy that takes the map out of `Cache.m` | `tests/race/mapsrejected_row_120_a_method_in_another_package_returns_the_map` | passes (added) |
| 121 | E3503 | `docs/verification/2026-11-11-row-failures/row_121` | fails |
| 122 | E3503 (the deferred read runs when `load` ends its task, before any `Do` has returned, while the other task's `Do` runs `load` again) | `tests/race/oncerejected_row_122_a_deferred_read_before_do` | passes |
| 123 | accepted | `tests/race/onceaccepted_row_123_a_deferred_read_after_do` | passes |
| 124 | accepted (the literal runs before the deferred `Unlock`, and `mu` is held at its `defer` statement and every point after it) | `tests/race/deferaccepted_row_124_a_deferred_literal_inside_a_deferred_unlock` | passes |
| 125 | E3502 (`n++` runs with no lock if `step` ends the task before `mu.Lock`, or after the deferred `Unlock`) | `tests/race/deferrejected_row_125_a_deferred_literal_before_the_lock` | passes |
| 126 | E3503 (the deferred write runs after the task start, so it is a shared write) | `tests/race/deferrejected_row_126_a_deferred_write_after_the_task_start` | passes |
| 127 | E3501 at the task statement | `tests/race/typerejected_row_127_and_row_128_the_counter_example` | passes (added) |
| 128 | E3503 | `tests/race/typerejected_row_127_and_row_128_the_counter_example` | passes (added) |
| 129 | E3501 at the task statement, and no rule 1 error | `tests/race/typerejected_row_129_a_counter_from_an_alloc_new_pool` | passes (added) |
