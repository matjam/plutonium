package diag

import typerr "github.com/matjam/plutonium/internal/types2/errors"

// whyOwners is why Plutonium has owners where Go has none.
const whyOwners = "Go copies such values freely and its garbage collector frees what they refer to when nothing refers to it any more. Plutonium has no garbage collector: an owner frees what it owns when it is dropped, so there must be exactly one."

// The explanations of the ownership and lifetime rules (language spec
// section 6). The errors themselves carry explanations specialised to the
// code at fault, built where they are found; these texts are what pluto
// explain prints, and what an error prints without one of its own.

// whyFrame is why a view of the frame cannot outlive it.
const whyFrame = "Go would move this memory to the heap, and its garbage collector would free it later. Plutonium has no garbage collector and never moves memory to the heap behind the program's back, so that every allocation is visible in the code."

func init() {
	register(
		&Entry{
			Code: ReturnFrameView, Name: "ReturnFrameView",
			Text:    "A function returns a view of its own frame: a pointer to one of its local variables, a slice of a local array, a function literal that uses local variables, or an interface holding a copy of a value in the frame. The frame is reclaimed when the function returns, so the caller would get a view of memory that no longer holds the value.\nReturn the value itself, which copies it, or moves it if it is an owner. To keep it at a fixed address after the function returns, allocate it.",
			Why:     whyFrame,
			Spec:    pspec("6.3", "Views"),
			Example: "func load() *Config {\n\tvar c Config\n\tc.timeout = 30\n\treturn &c\n}",
			Fixed:   "func load() Config {\n\tvar c Config\n\tc.timeout = 30\n\treturn c\n}",
		},
		&Entry{
			Code: StoreFrameView, Name: "StoreFrameView",
			Text:    "A view of the frame, such as a pointer to a local variable, is stored in memory that lives longer than the frame: a package variable, memory reached through a pointer the function was given, or memory that may be on the heap. When the function returns, that memory would still point at the reclaimed frame.\nStore the value instead of a pointer to it, or allocate the value so that it lives as long as the place that keeps it.",
			Why:     whyFrame,
			Spec:    pspec("6.3", "Views"),
			Example: "var last *Entry\n\nfunc add(e Entry) {\n\tlast = &e\n}",
			Fixed:   "var last Entry\n\nfunc add(e Entry) {\n\tlast = e\n}",
		},
		&Entry{
			Code: PassFrameView, Name: "PassFrameView",
			Text: "A view of the frame is passed to a function that keeps it after returning: the function stores it in a package variable, in memory that outlives the call, or passes it on to something that does. The compiler knows this from the function's body, so no annotation is needed. Passing a view to a function that only uses it during the call is always allowed.\nGive the function something that owns its memory, such as a buffer or a value allocated for it.",
			Why:  whyFrame,
			Spec: pspec("6.3", "Views"),
		},
		&Entry{
			Code: GoFrameView, Name: "GoFrameView",
			Text:    "A go statement starts a puroutine that runs on its own, and may still be running after the function that started it returns. A view of that function's frame passed to it could then point at reclaimed memory.\nPass the value itself, which copies it, or moves it if it is an owner.",
			Why:     whyFrame,
			Spec:    pspec("6.3", "Views"),
			Example: "go handle(&req)",
			Fixed:   "go handle(req)",
		},
		&Entry{
			Code: SendFrameView, Name: "SendFrameView",
			Text:    "A value sent on a channel may be received after the function that sent it returns. A view of that function's frame sent this way could then point at reclaimed memory.\nSend the value itself, which copies it, or moves it if it is an owner.",
			Why:     whyFrame,
			Spec:    pspec("6.3", "Views"),
			Example: "ch <- &req",
			Fixed:   "ch <- req",
		},
		&Entry{
			Code: LoopFrameView, Name: "LoopFrameView",
			Text:    "A pointer to a variable declared in a loop, or a function literal that uses one, is kept after the iteration that made it: stored in a variable declared outside the loop body, for example. Each iteration reuses the memory of the loop's variables, so what was kept would see the values of later iterations instead of its own.\nUse the pointer or call the function literal within the iteration, store values instead, or pass the value to the function literal as a parameter.",
			Why:     "Go gives each iteration of a loop its own variables, so a function literal made in one iteration keeps that iteration's values. Plutonium keeps variables in the function's frame, which has room for one copy of each; giving every iteration its own copy would need memory for each iteration, allocated behind the program's back.",
			Spec:    pspec("4.5", "Closures do not outlive their frame"),
			Example: "var first func() int\nfor i := 0; i < 3; i++ {\n\tif i == 0 {\n\t\tfirst = func() int { return i * 10 }\n\t}\n}",
			Fixed:   "var first int\nfor i := 0; i < 3; i++ {\n\tif i == 0 {\n\t\tfirst = i * 10\n\t}\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.DropNotOwner), Name: "DropNotOwner",
			Text:    "A method named Drop runs automatically when a value is destroyed. That only makes sense for a type whose values have exactly one owner: if the values were copied as ordinary values are, every copy would run Drop and release the same resource. A type with a Drop method must be declared owner, so that its values move instead of being copied.",
			Why:     whyOwners,
			Spec:    pspec("6.4", "Owners"),
			Example: "type File struct{ fd int32 }\n\nfunc (f *File) Drop() { syscall.Close(f.fd) }",
			Fixed:   "type File owner struct{ fd int32 }\n\nfunc (f *File) Drop() { syscall.Close(f.fd) }",
		},
		&Entry{
			Code: CheckerCode(typerr.MustBeOwner), Name: "MustBeOwner",
			Text:    "An owner owns memory or another resource and releases it when it is dropped, so it moves instead of being copied. A struct with an owner field, an array of owners, an interface that embeds an owning interface, or a type defined from an owner owns the same things, and copying it would leave two values that both release them. Plutonium never makes a type an owner silently: its declaration must say owner.\nIf the type should only look at a value that something else owns, store a pointer to it instead.",
			Why:     whyOwners,
			Spec:    pspec("6.4", "Owners"),
			Example: "type Conn struct {\n\tbuf strings.Buf\n}",
			Fixed:   "type Conn owner struct {\n\tbuf strings.Buf\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.UnnamedOwnerStruct), Name: "UnnamedOwnerStruct",
			Text:    "A struct that contains an owner owns what that owner owns, so it must be an owner too, and only a named type can be declared an owner. A struct type written without a name, as the type of a variable or a field, cannot have owner fields.\nDeclare a named owner type for the struct.",
			Why:     whyOwners,
			Spec:    pspec("6.4", "Owners"),
			Example: "var conn struct {\n\tbuf strings.Buf\n}",
			Fixed:   "type Conn owner struct {\n\tbuf strings.Buf\n}\n\nvar conn Conn",
		},
		&Entry{
			Code: CheckerCode(typerr.BadOwnerKind), Name: "BadOwnerKind",
			Text:    "owner may mark a struct, an array, an integer, a pointer or unsafe.Pointer, or an interface type. Other kinds cannot own anything: a slice or an unsafe string is a view of memory something else owns; numbers, booleans, enums, packed types and strings are values that copy; owned, shared, synced and weak already have kinds of their own.\nTo attach a resource to such a value, wrap both in a struct declared owner.",
			Spec:    pspec("6.4", "Owners"),
			Example: "type Bytes owner []byte",
			Fixed:   "type Bytes owner struct {\n\tb []byte\n}",
		},
		&Entry{
			Code: CheckerCode(typerr.OwnerAlias), Name: "OwnerAlias",
			Text:    "owner can only appear in a type definition, type T owner .... An alias (type A = T) is another name for an existing type and has that type's kind; it cannot add a mark of its own.",
			Spec:    pspec("6.4", "Owners"),
			Example: "type A = owner Buf",
			Fixed:   "type A = Buf // an owner if Buf is",
		},
		&Entry{
			Code: CheckerCode(typerr.BadDropSignature), Name: "BadDropSignature",
			Text:    "Drop runs automatically when an owner is destroyed. The compiler calls it with no arguments and ignores any result, so it must have no parameters and no results. If the method is not meant to run when a value is destroyed, give it another name.",
			Spec:    pspec("6.4", "Owners"),
			Example: "func (f *File) Drop() error",
			Fixed:   "func (f *File) Drop()\nfunc (f *File) Close() error",
		},
		&Entry{
			Code: CheckerCode(typerr.OwnerTypeArg), Name: "OwnerTypeArg",
			Text:    "Generic code copies the values of a type parameter freely unless its constraint is marked owner. Copying an owner would leave several values that all free the same memory, so an owner type can only be the type argument of a type parameter declared [T owner any] (or owner with another constraint). Inside such code, values of T move as owners do.\nIf the generic code moves each value at most once, mark its type parameter owner. Otherwise pass pointers to the owners, or copies made with Clone.",
			Why:     whyOwners,
			Spec:    pspec("6.4", "Owners"),
			Example: "func Fill[T any](s []T, v T)\n\nFill(bufs, strings.Buf{})",
			Fixed:   "func Fill[T owner any](s []T, v T)",
		},
		&Entry{
			Code: BadResultHolds, Name: "BadResultHolds",
			Text:    "//pu:resultholds p says that a function keeps its parameter p only for as long as its result lives: the result, an owner, holds p and gives it back when it is dropped, as alloc.Use's Restore does. The compiler then treats the call as returning p instead of keeping it. The directive must name a parameter, and the function must store that parameter in its result.",
			Spec:    pspec("9", "Directives"),
			Example: "//pu:resultholds a\nfunc Use(a Allocator) Restore {\n\tSetDefault(a)\n\treturn Restore{}\n}",
			Fixed:   "//pu:resultholds a\nfunc Use(a Allocator) Restore {\n\tr := Restore{prev: Default(), cur: a}\n\tSetDefault(a)\n\treturn r\n}",
		},
	)
}
