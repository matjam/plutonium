package eir

// Conventions of the desugarer that types.go does not state yet. They are
// proposed for types.go (which piece A owns, internal/escape2 package
// comment); until then this file is where they are written down.
//
//   - Registers. An expression whose type holds no pointers has the
//     register 0, the empty value; so may an argument in Call.Args and an
//     operand of OpStore, OpCompose and OpPhi.
//   - Slices. A slice value is the pointer to its backing array: OpElem's X
//     may be a slice value as well as a pointer to an array, and a slice
//     literal's value is OpAddr of its ObjSliceLit.
//   - Closure records. OpClosure and OpMethodVal with Obj NoObj make a
//     static record (a declared function or method expression used as a
//     value, a literal that captures nothing, any record in a package
//     initializer): heap memory. A literal's record holds at path [i] the
//     address of the i'th location it captures (a variable of any type,
//     or a frame object of the enclosing function: a deferred argument, a
//     result); the literal's last input is the record. A method value's
//     record holds the receiver X at path [0]; OpMethodVal.Callee is the
//     synthesized wrapper the value calls (SynthMethodValue or
//     SynthIfaceMethodValue), or NoFunc for a method of a type parameter,
//     which each instance resolves.
//   - Calls of literals. A CallStatic of a literal or range body (a
//     scoped task, a range over a static iterator) passes its closure
//     record as the last argument, as its inputs list it.
//   - CallTypeParam. The receiver is Args[0] (Target is 0): the operand's
//     address when it has one, its value otherwise.
//   - CallIface with Method nil is the drop of an owning interface's
//     dynamic value (lower/owniface.go): its candidates are the drop glue of
//     every type converted to an owning interface.
//   - CallContract names an entry of internal/escape2/contract: Inline (the
//     lowerer makes no call), Trusted (the lowerer calls the function; its
//     effects are the entry's), Special (ring methods).
//   - SynthVariant's variant is Path[0], its index in the union; SynthDropGlue
//     of a type parameter has no body (each instance's glue).
//   - Lifetimes. LifeBlock is a local variable's: it lives until its block
//     ends; the solver may take it as the frame's. An object made in a loop,
//     or a variable a loop's header declares, is LifeIter with Loop set to
//     that loop (section 18.4). LifeCaller is an object in the caller's
//     region (section 11.5): a closure record or method value record a
//     return statement returns directly, and the variables such a closure
//     captures. Every object of a package initializer is LifeHeap.
//   - Bodies. A function without a body has no blocks (Bodyless), except
//     the ones the lowerer gives a body (the scheduler hooks, rt.Sleep and
//     rt.mainWait, pluto.main as rt.programMain), which EIR builds the same.
//     Blocks unreachable from the entry are removed, as the lowerer removes
//     them; a call of a function that never returns ends its block.
//   - Exits. After a call that may end with rt.ExitTask, and after a
//     statement that may release a counted reference or drop an owner, a
//     function that names rt.ExitTask may leave through its return path:
//     deferred calls, drops of its scopes and parameters, and drops of its
//     owner results (lower/goexit.go).
//   - Releases. The release of a counted reference (a shared, synced or weak
//     box, a channel, a counted error, a closure region) is not an EIR
//     call: what it passes is the counted object, heap memory, so nothing
//     the release calls (the object's Drop) can keep frame memory.
